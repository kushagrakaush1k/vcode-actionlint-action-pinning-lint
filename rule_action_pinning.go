package actionlint

import (
	"regexp"
	"strings"
)

var (
	actionPinningSHAPattern    = regexp.MustCompile(`^[0-9a-f]{40}$`)
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
		rule.checkUses(exec.Uses)
	}
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleActionPinning) VisitJobPre(n *Job) error {
	if n.WorkflowCall != nil {
		rule.checkUses(n.WorkflowCall.Uses)
	}
	return nil
}

func (rule *RuleActionPinning) checkUses(u *String) {
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
	if ContainsExpression(ref) {
		rule.Errorf(u.Pos, "version pinning of %q cannot be verified because the ref contains an expression", spec)
		return
	}
	level := cfg.ActionPinning.Level
	if actionPinningSHAPattern.MatchString(ref) ||
		(level <= ActionPinningLevelSemver && actionPinningSemverPattern.MatchString(ref)) ||
		(level == ActionPinningLevelMajorMinor && actionPinningMinorPattern.MatchString(ref)) {
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
	rule.Errorf(u.Pos, "ref %q in %q does not meet action-pinning level %q: expected %s", ref, spec, level, required)
}
