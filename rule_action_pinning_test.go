package actionlint

import (
	"fmt"
	"strings"
	"testing"
)

func TestRuleActionPinning(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	tests := []struct {
		ref     string
		minor   bool
		semver  bool
		commit  bool
		message string
	}{
		{ref: "v1"},
		{ref: "main"},
		{ref: "release/v1.2.3"},
		{ref: "v1.2", minor: true},
		{ref: "1.2", minor: true},
		{ref: "v0.0", minor: true},
		{ref: "v1.2.3", minor: true, semver: true},
		{ref: "1.2.3", minor: true, semver: true},
		{ref: "v0.0.0", minor: true, semver: true},
		{ref: "v1.2.3-rc.1", minor: true, semver: true},
		{ref: "v1.2.3-0", minor: true, semver: true},
		{ref: "v1.2.3-alpha-beta.1+build.007", minor: true, semver: true},
		{ref: "v1.2.3+build.007", minor: true, semver: true},
		{ref: sha, minor: true, semver: true, commit: true},
		{ref: strings.Repeat("a", 40), minor: true, semver: true, commit: true},
		{ref: sha[:39]},
		{ref: sha + "0"},
		{ref: strings.ToUpper(sha)},
		{ref: strings.Repeat("g", 40)},
		{ref: sha + "\n"},
		{ref: " " + sha},
		{ref: "v01.2"},
		{ref: "v1.02"},
		{ref: "v01.2.3"},
		{ref: "v1.02.3"},
		{ref: "v1.2.03"},
		{ref: "v1.2.3-01"},
		{ref: "v1.2.3-rc..1"},
		{ref: "v1.2.3-"},
		{ref: "v1.2.3+"},
		{ref: "v1.2.3+build..1"},
		{ref: "v1.2.3.4"},
		{ref: "v1.2.3\n"},
		{ref: "${{ inputs.version }}", message: "cannot be verified"},
		{ref: "v1.2.${{ inputs.patch }}", message: "cannot be verified"},
	}
	levels := []ActionPinningLevel{ActionPinningLevelMajorMinor, ActionPinningLevelSemver, ActionPinningLevelCommitSHA}
	for _, level := range levels {
		for _, tc := range tests {
			for _, kind := range []string{"step", "job"} {
				t.Run(fmt.Sprintf("%s/%s/%s", level, kind, tc.ref), func(t *testing.T) {
					rule := NewRuleActionPinning()
					rule.SetConfig(&Config{ActionPinning: &ActionPinningConfig{Level: level}})
					u := &String{Value: "owner/repo/path@" + tc.ref, Pos: &Pos{Line: 12, Col: 9}}
					var err error
					if kind == "step" {
						err = rule.VisitStep(&Step{Exec: &ExecAction{Uses: u}})
					} else {
						err = rule.VisitJobPre(&Job{WorkflowCall: &WorkflowCall{Uses: u}})
					}
					if err != nil {
						t.Fatal(err)
					}
					valid := []bool{tc.minor, tc.semver, tc.commit}[level]
					errs := rule.Errs()
					if valid {
						if len(errs) != 0 {
							t.Fatal(errs)
						}
						return
					}
					if len(errs) != 1 {
						t.Fatalf("wanted one error, got %v", errs)
					}
					e := errs[0]
					if e.Kind != "action-pinning" || e.Line != 12 || e.Column != 9 {
						t.Fatalf("unexpected diagnostic location or kind: %v", e)
					}
					message := tc.message
					if message == "" {
						message = fmt.Sprintf("does not meet action-pinning level %q", level)
					}
					if !strings.Contains(e.Message, message) {
						t.Fatalf("wanted %q in %q", message, e.Message)
					}
				})
			}
		}
	}
}

func TestRuleActionPinningSkips(t *testing.T) {
	for _, spec := range []string{
		"", "owner/repo", "owner/repo@", "checkout@main", "/repo@main", "owner/@main",
		"./local-action", "./.github/workflows/ci.yml", "./local-action@main",
		"docker://alpine", "docker://alpine:latest", "docker://alpine:${{ inputs.tag }}",
		"${{ inputs.action }}", "${{ inputs.action }}@main",
		"owner/${{ inputs.action }}@main", "${{ 'owner/repo@main' }}",
		"${{ inputs.action }}@${{ inputs.version }}",
	} {
		t.Run(spec, func(t *testing.T) {
			rule := NewRuleActionPinning()
			rule.SetConfig(&Config{ActionPinning: &ActionPinningConfig{Level: ActionPinningLevelCommitSHA}})
			u := &String{Value: spec, Pos: &Pos{Line: 1, Col: 1}}
			if err := rule.VisitStep(&Step{Exec: &ExecAction{Uses: u}}); err != nil {
				t.Fatal(err)
			}
			if err := rule.VisitJobPre(&Job{WorkflowCall: &WorkflowCall{Uses: u}}); err != nil {
				t.Fatal(err)
			}
			if errs := rule.Errs(); len(errs) != 0 {
				t.Fatal(errs)
			}
		})
	}
}

func TestRuleActionPinningDisabledAndMissingUses(t *testing.T) {
	for _, cfg := range []*Config{nil, {}, {ActionPinning: &ActionPinningConfig{Level: ActionPinningLevelSemver}}} {
		rule := NewRuleActionPinning()
		rule.SetConfig(cfg)
		steps := []*Step{{}, {Exec: &ExecRun{}}, {Exec: &ExecAction{}}}
		jobs := []*Job{{}, {WorkflowCall: &WorkflowCall{}}}
		if cfg == nil || cfg.ActionPinning == nil {
			u := &String{Value: "owner/repo@main", Pos: &Pos{}}
			steps = append(steps, &Step{Exec: &ExecAction{Uses: u}})
			jobs = append(jobs, &Job{WorkflowCall: &WorkflowCall{Uses: u}})
		}
		for _, step := range steps {
			if err := rule.VisitStep(step); err != nil {
				t.Fatal(err)
			}
		}
		for _, job := range jobs {
			if err := rule.VisitJobPre(job); err != nil {
				t.Fatal(err)
			}
		}
		if errs := rule.Errs(); len(errs) != 0 {
			t.Fatal(errs)
		}
	}
}
