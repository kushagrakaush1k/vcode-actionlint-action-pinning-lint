package actionlint

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandMain(t *testing.T) {
	var output bytes.Buffer

	// Create command instance populating stdin/stdout/stderr
	cmd := Command{
		Stdin:  os.Stdin,
		Stdout: &output,
		Stderr: &output,
	}

	// Run the command end-to-end. Note that given args should contain program name
	workflow := filepath.Join("testdata", "examples", "main.yaml")
	status := cmd.Main([]string{"actionlint", "-shellcheck=", "-pyflakes=", "-ignore", `label .+ is unknown\.`, workflow})

	if status != 1 {
		t.Fatal("exit status should be 1 but got", status)
	}

	out := output.String()

	for _, s := range []string{
		"main.yaml:3:5:",
		"unexpected key \"branch\" for \"push\" section",
		"^~~~~~~~~~~~~~~",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("output should contain %q: %q", s, out)
		}
	}

	if strings.Contains(out, "[runner-label]") {
		t.Errorf("runner-label rule should be ignored by -ignore but it is included in output: %q", out)
	}
}

func TestCommandActionPinningLevel(t *testing.T) {
	const workflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: owner/repo@v1.2
  reuse:
    uses: owner/repo/.github/workflows/ci.yml@v1.2
`
	for _, tc := range []struct {
		name   string
		config string
		level  string
		flag   bool
		status int
	}{
		{name: "disabled by default"},
		{name: "flag enables semver", level: "semver", flag: true, status: ExitStatusSuccessProblemFound},
		{name: "major minor", level: "major-minor", flag: true},
		{name: "commit sha", level: "commit-sha", flag: true, status: ExitStatusSuccessProblemFound},
		{name: "null overridden", config: "action-pinning: null", level: "semver", flag: true, status: ExitStatusSuccessProblemFound},
		{name: "global level overridden", config: "action-pinning: {level: commit-sha}", level: "major-minor", flag: true},
		{name: "path-only enables rule", config: "paths:\n  '**/*.yml':\n    action-pinning: {level: semver}", status: ExitStatusSuccessProblemFound},
		{name: "path level overridden", config: "paths:\n  '**/*.yml':\n    action-pinning: {level: commit-sha}", level: "major-minor", flag: true},
		{name: "global owner allow list preserved", config: "action-pinning: {allowed-owners: [owner]}", level: "commit-sha", flag: true},
		{name: "global action allow list preserved", config: "action-pinning: {allowed-actions: [owner/repo]}", level: "commit-sha", flag: true},
		{name: "path owner allow list preserved", config: "paths:\n  '**/*.yml':\n    action-pinning: {allowed-owners: [owner]}", level: "commit-sha", flag: true},
		{name: "path action allow list preserved", config: "paths:\n  '**/*.yml':\n    action-pinning: {allowed-actions: [owner/repo]}", level: "commit-sha", flag: true},
		{name: "global allow list survives path override", config: "action-pinning: {allowed-owners: [owner]}\npaths:\n  '**/*.yml':\n    action-pinning: {level: commit-sha, allowed-owners: [other]}", level: "semver", flag: true},
		{name: "invalid flag", level: "bad", flag: true, status: ExitStatusInvalidCommandOption},
		{name: "empty flag", level: "", flag: true, status: ExitStatusInvalidCommandOption},
		{name: "invalid config level not hidden by flag", config: "action-pinning: {level: bad}", level: "semver", flag: true, status: ExitStatusFailure},
		{name: "invalid allow list not hidden by flag", config: "action-pinning: {allowed-actions: ['owner/repo@main']}", level: "semver", flag: true, status: ExitStatusFailure},
		{name: "invalid unmatched path config", config: "paths:\n  'unmatched/*.yml':\n    action-pinning: {allowed-owners: ['owner/repo']}", level: "semver", flag: true, status: ExitStatusFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			cmd := Command{Stdin: strings.NewReader(workflow), Stdout: &output, Stderr: &output}
			args := []string{"actionlint", "-shellcheck=", "-pyflakes=", "-no-color", "-stdin-filename", ".github/workflows/pinning.yml"}
			if tc.config != "" {
				path := filepath.Join(t.TempDir(), "actionlint.yaml")
				if err := os.WriteFile(path, []byte(tc.config), 0644); err != nil {
					t.Fatal(err)
				}
				args = append(args, "-config-file", path)
			}
			if tc.flag {
				args = append(args, "-action-pinning-level="+tc.level)
			}
			args = append(args, "-")
			if status := cmd.Main(args); status != tc.status {
				t.Fatalf("wanted status %d, got %d: %s", tc.status, status, output.String())
			}
			if tc.status == ExitStatusSuccessProblemFound {
				for _, label := range []string{"step action", "reusable workflow"} {
					if !strings.Contains(output.String(), label) {
						t.Errorf("missing %q in %s", label, output.String())
					}
				}
			}
		})
	}
}
