package actionlint

import (
	"regexp"
	"sort"
	"strings"
)

var (
	actionPinningSHAPattern    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	actionPinningMajorPattern  = regexp.MustCompile(`^v?(0|[1-9][0-9]*)$`)
	actionPinningMinorPattern  = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	actionPinningSemverPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)\.)*(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
)

// RuleActionPinning checks that remote actions and reusable workflows use pinned refs.
type RuleActionPinning struct {
	RuleBase
}

// NewRuleActionPinning creates a new RuleActionPinning instance. The rule is disabled
// unless the action-pinning configuration is non-nil.
func NewRuleActionPinning() *RuleActionPinning {
	return &RuleActionPinning{
		RuleBase: NewRuleBase("action-pinning", "Checks version pinning of remote actions and reusable workflows at \"uses:\""),
	}
}

// VisitStep is callback when visiting Step node.
func (rule *RuleActionPinning) VisitStep(n *Step) error {
	if exec, ok := n.Exec.(*ExecAction); ok {
		rule.checkUses(exec.Uses, "step action")
	}
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleActionPinning) VisitJobPre(n *Job) error {
	if n.WorkflowCall != nil {
		rule.checkUses(n.WorkflowCall.Uses, "reusable workflow")
	}
	return nil
}

func (rule *RuleActionPinning) checkUses(u *String, kind string) {
	cfg := rule.Config()
	if cfg == nil || cfg.ActionPinning == nil || u == nil {
		return
	}
	spec := u.Value
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "docker://") {
		return
	}
	idx := strings.IndexByte(spec, '@')
	// An expression in the action name prevents identifying the referenced repository.
	// Check its start before splitting, since an expression may itself contain '@'.
	if expr := strings.Index(spec, "${{"); u.ContainsExpression() && expr >= 0 && (idx < 0 || expr < idx) {
		return
	}
	if idx <= 0 || idx == len(spec)-1 {
		// Missing names and refs are reported by the existing format checks.
		return
	}
	name, ref := spec[:idx], spec[idx+1:]
	owner, repo, ok := strings.Cut(name, "/")
	if !ok || owner == "" || repo == "" || strings.HasPrefix(repo, "/") {
		return
	}
	if idx := strings.IndexByte(repo, '/'); idx >= 0 {
		repo = repo[:idx]
	}
	for _, allowed := range cfg.ActionPinning.AllowedOwners {
		if strings.EqualFold(owner, allowed) {
			return
		}
	}
	for _, allowed := range cfg.ActionPinning.AllowedActions {
		if strings.EqualFold(owner+"/"+repo, allowed) {
			return
		}
	}
	level := cfg.ActionPinning.Level
	if ContainsExpression(ref) {
		rule.Errorf(u.Pos, "version pinning of %s %q cannot be verified because the ref contains an expression%s", kind, spec, actionPinningVersionHint(name, level))
		return
	}
	if actionPinningRefMeetsLevel(ref, level) {
		return
	}
	var required string
	switch level {
	case ActionPinningLevelMajorMinor:
		required = "a minor version such as v1.2, a full semantic version, or a full 40-character lowercase hexadecimal commit SHA"
	case ActionPinningLevelSemver:
		required = "a full semantic version such as v1.2.3 (optionally with a prerelease), or a full 40-character lowercase hexadecimal commit SHA"
	default:
		required = "a full 40-character lowercase hexadecimal commit SHA"
	}
	rule.Errorf(u.Pos, "ref %q in %s %q does not meet action-pinning level %q: expected %s%s", ref, kind, spec, level, required, actionPinningVersionHint(name, level))
}

func actionPinningRefMeetsLevel(ref string, level ActionPinningLevel) bool {
	return actionPinningSHAPattern.MatchString(ref) ||
		(level <= ActionPinningLevelSemver && actionPinningSemverPattern.MatchString(ref)) ||
		(level == ActionPinningLevelMajorMinor && actionPinningMinorPattern.MatchString(ref))
}

func actionPinningVersionHint(name string, level ActionPinningLevel) string {
	versions := []string{}
	for spec := range PopularActions {
		action, ref, ok := strings.Cut(spec, "@")
		if ok && strings.EqualFold(action, name) && (actionPinningMajorPattern.MatchString(ref) || actionPinningRefMeetsLevel(ref, ActionPinningLevelMajorMinor)) {
			versions = append(versions, ref)
		}
	}
	if len(versions) == 0 {
		return ""
	}
	sort.Slice(versions, func(i, j int) bool {
		return compareActionPinningVersions(versions[i], versions[j]) < 0
	})
	for i := len(versions) - 1; i >= 0; i-- {
		if actionPinningRefMeetsLevel(versions[i], level) {
			return "; use known version \"" + name + "@" + versions[i] + "\""
		}
	}
	return "; resolve known version \"" + name + "@" + versions[len(versions)-1] + "\" to a ref meeting this level"
}

// Compare numeric version components without integer overflow. Stable releases sort
// after prereleases; ties use the complete ref to keep hints deterministic.
func compareActionPinningVersions(a, b string) int {
	core := func(s string) []string {
		s = strings.TrimPrefix(s, "v")
		s, _, _ = strings.Cut(s, "-")
		s, _, _ = strings.Cut(s, "+")
		return strings.Split(s, ".")
	}
	x, y := core(a), core(b)
	for i := 0; i < 3; i++ {
		left, right := "0", "0"
		if i < len(x) {
			left = x[i]
		}
		if i < len(y) {
			right = y[i]
		}
		if len(left) < len(right) {
			return -1
		}
		if len(left) > len(right) {
			return 1
		}
		if order := strings.Compare(left, right); order != 0 {
			return order
		}
	}
	prerelease := func(s string) bool {
		s, _, _ = strings.Cut(s, "+")
		return strings.Contains(s, "-")
	}
	if prerelease(a) != prerelease(b) {
		if prerelease(a) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}
