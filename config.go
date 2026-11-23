package actionlint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"go.yaml.in/yaml/v4"
)

// IgnorePatterns is a list of regular expressions. These patterns are used for filtering errors by
// matching the error messages.
type IgnorePatterns []*regexp.Regexp

// Match returns whether the given error should be ignored due to the "ignore" configuration.
func (pats IgnorePatterns) Match(err *Error) bool {
	for _, r := range pats {
		if r.MatchString(err.Message) {
			return true
		}
	}
	return false
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (pats *IgnorePatterns) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("yaml: \"ignore\" must be a sequence node at line:%d,col:%d", n.Line, n.Column)
	}
	rs := make([]*regexp.Regexp, 0, len(n.Content))
	for _, p := range n.Content {
		r, err := regexp.Compile(p.Value)
		if err != nil {
			return fmt.Errorf("invalid regular expression %q in \"ignore\" at line%d,col:%d: %w", p.Value, n.Line, n.Column, err)
		}
		rs = append(rs, r)
	}
	*pats = rs
	return nil
}

// PathConfig is a configuration for specific file path pattern. This is for values of the "paths" mapping
// in the configuration file.
type PathConfig struct {
	// Ignore is a list of patterns. They are used for ignoring errors by matching to the error messages.
	// It is similar to the "-ignore" command line option.
	Ignore IgnorePatterns `yaml:"ignore"`
	// ActionPinning enables or customizes pinning for matching workflow paths.
	ActionPinning *ActionPinningConfig `yaml:"action-pinning"`
}

// ActionPinningLevel is the minimum strictness required for remote action and workflow refs.
type ActionPinningLevel uint8

const (
	// ActionPinningLevelMajorMinor accepts minor versions, semantic versions, and commit SHAs.
	ActionPinningLevelMajorMinor ActionPinningLevel = iota
	// ActionPinningLevelSemver accepts complete semantic versions and commit SHAs.
	ActionPinningLevelSemver
	// ActionPinningLevelCommitSHA accepts only full lowercase commit SHAs.
	ActionPinningLevelCommitSHA
)

// String returns the configuration value for the level.
func (level ActionPinningLevel) String() string {
	switch level {
	case ActionPinningLevelMajorMinor:
		return "major-minor"
	case ActionPinningLevelSemver:
		return "semver"
	case ActionPinningLevelCommitSHA:
		return "commit-sha"
	default:
		return "unknown"
	}
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (level *ActionPinningLevel) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
		if l, err := parseActionPinningLevel(n.Value); err == nil {
			*level = l
			return nil
		}
	}
	return fmt.Errorf("invalid action-pinning level %q at line:%d,col:%d: expected \"major-minor\", \"semver\", or \"commit-sha\"", n.Value, n.Line, n.Column)
}

func parseActionPinningLevel(value string) (ActionPinningLevel, error) {
	switch value {
	case "major-minor":
		return ActionPinningLevelMajorMinor, nil
	case "semver":
		return ActionPinningLevelSemver, nil
	case "commit-sha":
		return ActionPinningLevelCommitSHA, nil
	default:
		return 0, fmt.Errorf("invalid action-pinning level %q: expected \"major-minor\", \"semver\", or \"commit-sha\"", value)
	}
}

// ActionPinningConfig enables pinning checks and configures their minimum strictness.
type ActionPinningConfig struct {
	Level          ActionPinningLevel `yaml:"level"`
	AllowedOwners  []string           `yaml:"allowed-owners"`
	AllowedActions []string           `yaml:"allowed-actions"`
	levelSet       bool
}

var (
	actionPinningOwnerPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$`)
	actionPinningRepoPattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

func validActionPinningOwner(owner string) bool {
	return actionPinningOwnerPattern.MatchString(owner)
}

func validActionPinningAction(action string) bool {
	owner, repo, ok := strings.Cut(action, "/")
	return ok && validActionPinningOwner(owner) && actionPinningRepoPattern.MatchString(repo) && repo != "." && repo != ".."
}

// UnmarshalYAML implements yaml.Unmarshaler, defaulting an omitted level to semver.
func (cfg *ActionPinningConfig) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("\"action-pinning\" must be a mapping at line:%d,col:%d", n.Line, n.Column)
	}
	type plain ActionPinningConfig
	c := plain{Level: ActionPinningLevelSemver}
	if err := n.Decode(&c); err != nil {
		return err
	}
	for i := 0; i < len(n.Content); i += 2 {
		key, value := n.Content[i].Value, n.Content[i+1]
		for value.Kind == yaml.AliasNode {
			value = value.Alias
		}
		switch key {
		case "level":
			c.levelSet = value.Tag != "!!null"
		case "allowed-owners", "allowed-actions":
			if value.Tag == "!!null" {
				continue
			}
			if value.Kind != yaml.SequenceNode {
				return fmt.Errorf("action-pinning %q must be a sequence at line:%d,col:%d", key, value.Line, value.Column)
			}
			for _, item := range value.Content {
				for item.Kind == yaml.AliasNode {
					item = item.Alias
				}
				valid := validActionPinningAction(item.Value)
				expected := "a clean owner/repo without a ref or path"
				if key == "allowed-owners" {
					valid = validActionPinningOwner(item.Value)
					expected = "an owner name without a slash"
				}
				if item.Kind != yaml.ScalarNode || item.Tag != "!!str" || !valid {
					return fmt.Errorf("invalid action-pinning %q entry %q at line:%d,col:%d: expected %s", key, item.Value, item.Line, item.Column, expected)
				}
			}
		default:
			return fmt.Errorf("unexpected key %q in action-pinning at line:%d,col:%d", key, n.Content[i].Line, n.Content[i].Column)
		}
	}
	*cfg = ActionPinningConfig(c)
	return nil
}

// ActionPinningForPath combines global and matching path pinning settings. Explicit
// path levels override the global level; overlapping path levels use the strictest.
// All allow lists are merged. The returned configuration can be modified by the caller.
func (cfg *Config) ActionPinningForPath(path string) *ActionPinningConfig {
	if cfg == nil {
		return nil
	}
	var result *ActionPinningConfig
	merge := func(c *ActionPinningConfig) {
		if result == nil {
			result = &ActionPinningConfig{Level: ActionPinningLevelSemver}
		}
		result.AllowedOwners = append(result.AllowedOwners, c.AllowedOwners...)
		result.AllowedActions = append(result.AllowedActions, c.AllowedActions...)
	}
	if cfg.ActionPinning != nil {
		merge(cfg.ActionPinning)
		result.Level = cfg.ActionPinning.Level
	}
	// Sort patterns so merged lists are deterministic despite map iteration order.
	patterns := make([]string, 0, len(cfg.Paths))
	for p := range cfg.Paths {
		patterns = append(patterns, p)
	}
	sort.Strings(patterns)
	hasPathLevel := false
	for _, pattern := range patterns {
		c := cfg.Paths[pattern].ActionPinning
		if c == nil || !doublestar.MatchUnvalidated(pattern, filepath.ToSlash(path)) {
			continue
		}
		merge(c)
		if c.levelSet && (!hasPathLevel || c.Level > result.Level) {
			result.Level = c.Level
		}
		hasPathLevel = hasPathLevel || c.levelSet
	}
	return result
}

// Config is configuration of actionlint. This struct instance is parsed from "actionlint.yaml"
// file usually put in ".github" directory.
type Config struct {
	// SelfHostedRunner is configuration for self-hosted runner.
	SelfHostedRunner struct {
		// Labels is label names for self-hosted runner.
		Labels []string `yaml:"labels"`
	} `yaml:"self-hosted-runner"`
	// ConfigVariables is names of configuration variables used in the checked workflows. When this value is nil,
	// property names of `vars` context will not be checked. Otherwise actionlint will report a name which is not
	// listed here as undefined config variables.
	// https://docs.github.com/en/actions/learn-github-actions/variables
	ConfigVariables []string `yaml:"config-variables"`
	// ActionPinning checks remote uses references when non-nil. Nil disables the rule.
	ActionPinning *ActionPinningConfig `yaml:"action-pinning"`
	// Paths is a "paths" mapping in the configuration file. The keys are glob patterns to match file paths.
	// And the values are corresponding configurations applied to the file paths.
	Paths map[string]PathConfig `yaml:"paths"`
}

// PathConfigs returns a list of all PathConfig values matching to the given file path. The path must
// be relative to the root of the project.
func (cfg *Config) PathConfigs(path string) []PathConfig {
	path = filepath.ToSlash(path)

	var ret []PathConfig
	if cfg != nil {
		for p, c := range cfg.Paths {
			// Glob patterns were validated in `ParseConfig()`
			if doublestar.MatchUnvalidated(p, path) {
				ret = append(ret, c)
			}
		}
	}
	return ret
}

// ParseConfig parses the given bytes as an actionlint config file. When deserializing the YAML file
// or the config validation fails, this function returns an error.
func ParseConfig(b []byte) (*Config, error) {
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		msg := strings.ReplaceAll(err.Error(), "\n", " ")
		return nil, errors.New(msg)
	}
	for pat := range c.Paths {
		if !doublestar.ValidatePattern(pat) {
			return nil, fmt.Errorf("invalid glob pattern %q in \"paths\"", pat)
		}
	}
	return &c, nil
}

// ReadConfigFile reads actionlint config file (actionlint.yaml) from the given file path.
func ReadConfigFile(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read config file %q: %w", path, err)
	}
	c, err := ParseConfig(b)
	if err != nil {
		return nil, fmt.Errorf("could not parse config file %q: %w", path, err)
	}
	return c, nil
}

// loadRepoConfig reads config file from the repository's .github/actionlint.yml or
// .github/actionlint.yaml.
func loadRepoConfig(root string) (*Config, error) {
	for _, f := range []string{"actionlint.yaml", "actionlint.yml"} {
		p := filepath.Join(root, ".github", f)
		c, err := ReadConfigFile(p)
		switch {
		case errors.Is(err, os.ErrNotExist):
			continue
		case err != nil:
			return nil, fmt.Errorf("could not parse config file %q: %w", p, err)
		default:
			return c, nil
		}
	}
	return nil, nil
}

func writeDefaultConfigFile(path string) error {
	b := []byte(`self-hosted-runner:
  # Labels of self-hosted runner in array of strings.
  labels: []

# Configuration variables in array of strings defined in your repository or
# organization. ` + "`null`" + ` means disabling configuration variables check.
# Empty array means no configuration variable is allowed.
config-variables: null

# Pin remote actions and reusable workflows. null disables the check.
# Use {} for the default semver level, or set level to major-minor, semver, or commit-sha.
# allowed-owners lists owner names; allowed-actions lists exact owner/repo names.
action-pinning: null

# Configuration for file paths. The keys are glob patterns to match to file
# paths relative to the repository root. The values are the configurations for
# the file paths. Note that the path separator is always '/'.
# The following configurations are available.
#
# "ignore" is an array of regular expression patterns. Matched error messages
# are ignored. This is similar to the "-ignore" command line option.
# "action-pinning" configures a level and additional allow lists for matching files.
paths:
#  .github/workflows/**/*.yml:
#    ignore: []
`)
	if err := os.WriteFile(path, b, 0644); err != nil {
		return fmt.Errorf("could not write default configuration file at %q: %w", path, err)
	}
	return nil
}
