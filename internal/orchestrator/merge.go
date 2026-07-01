package orchestrator

// Auto-merge policy engine and safety gates (AS-157). This is the deterministic
// decision that lets a Smith-authored PR merge during dogfood without ever
// letting prompt text decide merge eligibility (ADR D-ORCH-6). The AS-149 PR
// lifecycle records a *deferral* for a github.merge / github.enable_auto_merge
// step; this file turns that deferral into a real, fail-closed verdict.
//
// The split mirrors the rest of the orchestrator: [EvaluateMerge] is a pure
// function of the job's frozen merge_policy (spec §4.10) plus the observed
// [MergeFacts], so the whole policy is offline-testable with no live GitHub. The
// authenticated transport that reads the facts and asks GitHub's *native*
// auto-merge to act is the [MergeActions] port (AS-148's domain), injected the
// same way as [PRActions]. Every decision — allow or deny — is recorded with all
// evaluated inputs and the final reason (AS-157 acceptance; run DB + session log
// via the shared [PolicyDecision] block), so the audit trail never depends on the
// side effect having landed.
//
// Fail-closed is the whole point: the forbidden invariants (unknown/failed checks,
// an unprotected base, a force push, a high-risk changed path) are enforced by the
// engine itself and cannot be spelled away by a spec — the DSL already guarantees
// they can never be listed as *permitted* (validate.go, rule 12). Auto-merge is
// off unless the job spec (mode: auto) and the repository (RepoAutoMerge) both
// say yes, and an agent PR still needs one independent human approval — the
// requester can never self-approve (mirrors the AS-158 research on Copilot's gate).

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tonitienda/agent-smith/internal/orchestrator/spec"
	"github.com/tonitienda/agent-smith/internal/orchestrator/store"
)

// Check is one CI/status check GitHub reports on the PR head: its name and a
// normalised state. Any state other than [CheckSuccess] blocks an auto-merge
// (AS-157 acceptance: failed, pending, missing, or unknown checks block merge).
type Check struct {
	Name  string
	State string // success | failure | pending | missing | unknown (or any raw GitHub state)
}

// CheckSuccess is the only check state that does not block a merge. Every other
// state — including an empty/unknown one — is treated as not-green, fail-closed.
const CheckSuccess = "success"

// ManualOverride is the explicit, audited escape hatch for a mode: manual policy
// (AS-157 acceptance 6). An override merges a PR that auto mode would leave for
// humans, but only when a human other than the PR author asked for it with a
// reason — the same "no self-approval" rule that gates auto mode.
type ManualOverride struct {
	Actor  string // login that requested the override; must not be the PR author
	Reason string // why; recorded verbatim in the audit trail, never invented
}

// MergeFacts is every input the merge decision evaluates, read from GitHub by the
// [MergeActions] port (or supplied by a test). It is a faithful snapshot: the
// engine never fetches, it only judges, so the same facts always yield the same
// verdict. Each field maps to an AS-157 acceptance input.
type MergeFacts struct {
	Repository string // owner/name, for the target and the record
	Number     int    // PR number
	Author     string // PR author login (branch ownership is derived from Head)
	Head       string // head (source) branch — Smith-owned iff smithBranchPrefix
	Base       string // target branch

	Labels       []string // labels present on the PR
	ChangedFiles []string // paths the PR changes (for the high-risk deny list)
	Checks       []Check  // required/observed checks on the head
	Approvals    []string // logins that approved the PR (review state)

	BranchProtected bool // the base branch has branch protection enabled
	RepoAutoMerge   bool // the repository allows auto-merge (repo setting)
	ForcePushed     bool // the run force-pushed the head (protection-bypass signal)
	BudgetExceeded  bool // the run overran its budget (budget outcome)

	Override *ManualOverride // present only for an explicit manual override
}

// MergeDecision is the verdict: whether the PR may merge, under which mode, the
// single decisive reason, and the full map of evaluated inputs for the audit
// record. Inputs is deterministic (sorted keys) so a recorded decision reads the
// same on every run.
type MergeDecision struct {
	Allowed bool
	Mode    string
	Reason  string
	Inputs  map[string]string
}

// highRiskPrefixes are the changed-path patterns that always block an auto-merge
// (AS-157 acceptance 4): CI/workflow definitions, the orchestrator's own job
// specs, and anything that looks secret-related. These are engine-owned safety
// gates, not spec-expressible — a spec can never permit them — so a Smith PR that
// touches them waits for a human even when every other gate is green.
var highRiskPrefixes = []string{
	".github/workflows/",
	".github/actions/",
	".agent-smith/jobs/",
}

// isHighRiskPath reports whether a changed file is one no auto-merge may carry: a
// workflow/action definition, a job spec, or a path whose name marks it as
// secret-bearing. Matching is on the normalised (forward-slash) path.
func isHighRiskPath(p string) bool {
	q := strings.ToLower(strings.TrimPrefix(filepathSlashClean(p), "/"))
	for _, pre := range highRiskPrefixes {
		if strings.HasPrefix(q, pre) {
			return true
		}
	}
	base := q
	if i := strings.LastIndex(q, "/"); i >= 0 {
		base = q[i+1:]
	}
	switch {
	case strings.HasPrefix(base, "id_rsa"), strings.HasPrefix(base, "id_dsa"),
		strings.HasPrefix(base, "id_ecdsa"), strings.HasPrefix(base, "id_ed25519"):
		// Common SSH private key filenames carry no secret/credential token or
		// .pem/.key suffix, so name them explicitly.
		return true
	}
	return strings.Contains(base, "secret") || strings.Contains(base, "credential") ||
		strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key")
}

// filepathSlashClean normalises a repo path to forward slashes without importing
// path/filepath semantics (repo paths are always slash-separated); it only guards
// against a stray leading "./".
func filepathSlashClean(p string) string {
	return strings.TrimPrefix(strings.ReplaceAll(p, "\\", "/"), "./")
}

// EvaluateMerge is the whole policy: a pure, deterministic verdict on whether the
// PR described by facts may auto-merge under policy. It never mutates and never
// reaches out — the caller supplies facts and records the returned decision.
//
// The order below is the audit order: the first failing gate is the decisive
// reason, checked most-fundamental first (is auto-merge even on?) down to the
// spec's own required predicates, so the recorded reason is stable and explains
// the strongest objection. A nil policy is mode off (nothing enabled merge).
func EvaluateMerge(policy *spec.MergePolicy, facts MergeFacts) MergeDecision {
	inputs := mergeInputs(facts)
	mode := "off"
	if policy != nil && policy.Mode != "" {
		mode = policy.Mode
	}
	inputs["mode"] = mode

	block := func(reason string) MergeDecision {
		return MergeDecision{Allowed: false, Mode: mode, Reason: reason, Inputs: inputs}
	}

	// An unknown author defeats the no-self-approval gates (sameLogin against "" is
	// always false), so any merge-enabling mode is refused fail-closed.
	if mode != "off" && facts.Author == "" {
		return block("forbidden: PR author is unknown (cannot verify self-approval gates)")
	}

	switch mode {
	case "off":
		return block("merge disabled: merge_policy mode is off")
	case "manual":
		// Manual mode never auto-merges. It merges only via an explicit, audited
		// override by a human other than the author (acceptance 6).
		ov := facts.Override
		if ov == nil || strings.TrimSpace(ov.Reason) == "" || ov.Actor == "" {
			return block("merge requires an explicit manual override (merge_policy mode is manual)")
		}
		if sameLogin(ov.Actor, facts.Author) {
			return block("manual override rejected: the PR author cannot override their own merge")
		}
		inputs["override_actor"] = ov.Actor
		inputs["override_reason"] = ov.Reason
		return MergeDecision{Allowed: true, Mode: mode, Inputs: inputs,
			Reason: fmt.Sprintf("manual override by %s: %s", ov.Actor, ov.Reason)}
	case "auto":
		// fall through to the auto gates below
	default:
		return block(fmt.Sprintf("merge disabled: unknown merge_policy mode %q", mode))
	}

	// --- auto mode: both the repo and the engine's invariants must agree ---

	if !facts.RepoAutoMerge {
		return block("repository settings do not allow auto-merge")
	}
	if facts.BudgetExceeded {
		return block("run exceeded its budget")
	}

	// Forbidden invariants — engine-owned, never spec-permittable.
	if facts.ForcePushed {
		return block("forbidden: the run force-pushed the PR head")
	}
	if !facts.BranchProtected {
		return block("forbidden: base branch is not protected (branch protection required)")
	}
	if notGreen := checksNotGreen(facts.Checks); notGreen != "" {
		return block("forbidden: checks are not all green: " + notGreen)
	}
	if risky := highRiskChanges(facts.ChangedFiles); len(risky) > 0 {
		return block("forbidden: high-risk paths changed: " + strings.Join(risky, ", "))
	}

	// Required predicates. pr_author_is_smith and all-checks-green are always
	// enforced for an agent PR; label_present gates come from the spec.
	if !smithAuthored(facts) {
		return block("required: PR is not Smith-authored")
	}
	if policy != nil {
		for _, p := range policy.Required {
			if p.Name != "label_present" {
				continue
			}
			label, ok := p.Arg.(string)
			if !ok || label == "" {
				// A validated spec always carries a string label (validate.go rule 12);
				// treat anything else as fail-closed rather than a silently-skipped gate.
				return block("forbidden: label_present predicate has a non-string/empty argument")
			}
			if !hasLabel(facts.Labels, label) {
				return block(fmt.Sprintf("required: missing label %q", label))
			}
		}
	}

	// Independent human approval — the requester can never self-approve
	// (AS-158 research: mirror Copilot's hard gate).
	if !hasIndependentApproval(facts) {
		return block("required: no independent human approval (the requester cannot self-approve)")
	}

	return MergeDecision{Allowed: true, Mode: mode, Inputs: inputs,
		Reason: "all merge gates satisfied"}
}

// mergeInputs snapshots every evaluated fact into a sorted string map so the
// recorded decision lists what was seen, not only the verdict (acceptance 5).
func mergeInputs(f MergeFacts) map[string]string {
	m := map[string]string{
		"pr":               fmt.Sprintf("%s#%d", f.Repository, f.Number),
		"author":           f.Author,
		"head":             f.Head,
		"base":             f.Base,
		"smith_authored":   fmt.Sprintf("%t", smithAuthored(f)),
		"labels":           strings.Join(sortedCopy(f.Labels), ","),
		"changed_files":    fmt.Sprintf("%d", len(f.ChangedFiles)),
		"checks":           checksSummary(f.Checks),
		"approvals":        strings.Join(sortedCopy(f.Approvals), ","),
		"branch_protected": fmt.Sprintf("%t", f.BranchProtected),
		"repo_auto_merge":  fmt.Sprintf("%t", f.RepoAutoMerge),
		"force_pushed":     fmt.Sprintf("%t", f.ForcePushed),
		"budget_exceeded":  fmt.Sprintf("%t", f.BudgetExceeded),
	}
	if risky := highRiskChanges(f.ChangedFiles); len(risky) > 0 {
		m["high_risk_files"] = strings.Join(risky, ",")
	}
	return m
}

// checksNotGreen returns a short description of the first not-green check (in a
// deterministic order), or "" when every check is success and at least one check
// exists. No checks at all is itself not-green: a merge with zero required checks
// is treated as missing, fail-closed.
func checksNotGreen(checks []Check) string {
	if len(checks) == 0 {
		return "no checks reported (missing)"
	}
	ordered := append([]Check(nil), checks...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	for _, c := range ordered {
		if c.State != CheckSuccess {
			state := c.State
			if state == "" {
				state = "unknown"
			}
			return fmt.Sprintf("%s=%s", c.Name, state)
		}
	}
	return ""
}

// checksSummary renders all checks as name=state in name order for the audit map.
func checksSummary(checks []Check) string {
	if len(checks) == 0 {
		return "none"
	}
	ordered := append([]Check(nil), checks...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	parts := make([]string, 0, len(ordered))
	for _, c := range ordered {
		state := c.State
		if state == "" {
			state = "unknown"
		}
		parts = append(parts, c.Name+"="+state)
	}
	return strings.Join(parts, " ")
}

// highRiskChanges returns the sorted subset of changed files that trip the
// engine-owned high-risk deny list.
func highRiskChanges(files []string) []string {
	var out []string
	for _, f := range files {
		if isHighRiskPath(f) {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// smithAuthored reports whether the PR is one Smith owns: its head is a
// smithBranchPrefix branch. Ownership is derived from the branch, not a trustable
// author string, matching the AS-149 PR-lifecycle ownership guarantee.
func smithAuthored(f MergeFacts) bool {
	return strings.HasPrefix(f.Head, smithBranchPrefix)
}

// hasIndependentApproval reports whether at least one approval came from a login
// that is neither the PR author nor Smith itself — an independent human gate.
func hasIndependentApproval(f MergeFacts) bool {
	for _, a := range f.Approvals {
		if a == "" || sameLogin(a, f.Author) {
			continue
		}
		// A bot approval is not an independent *human* review. Match the [bot] login
		// suffix rather than the substring "smith", which would falsely reject a human
		// reviewer named e.g. "johnsmith".
		if strings.HasSuffix(strings.ToLower(a), "[bot]") {
			continue
		}
		return true
	}
	return false
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func sameLogin(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func sortedCopy(xs []string) []string {
	out := append([]string(nil), xs...)
	sort.Strings(out)
	return out
}

// MergeActions is the authenticated merge port (AS-148's domain): it reads the
// facts the policy judges and, when the verdict allows, asks GitHub's native
// auto-merge/merge to act. Keeping it native means Smith never bypasses branch
// protection itself — GitHub still enforces the required checks at merge time.
// A test exercises the whole gate offline with a fake implementation.
type MergeActions interface {
	// MergeFacts reads the current merge-relevant state of the target PR.
	MergeFacts(ctx context.Context, t GitHubTarget) (MergeFacts, error)
	// EnableAutoMerge turns on GitHub native auto-merge for the PR (mode: auto):
	// GitHub merges once its own required checks pass. Returns the PR URL.
	EnableAutoMerge(ctx context.Context, t GitHubTarget) (url string, err error)
	// Merge merges the PR now (used for an approved manual override). Returns the
	// resulting merge/PR URL.
	Merge(ctx context.Context, t GitHubTarget) (url string, err error)
}

// runMergeStep evaluates the merge policy for a github.enable_auto_merge /
// github.merge step and acts on the verdict, recording the full decision either
// way. It is a no-op (records a deferral) when no merge port is wired, so an
// orchestrator without GitHub credentials still runs cleanly, and it skips a
// guarded step fail-closed because the guard namespace (policy.*) is AS-152's job.
//
// A blocked merge is a normal outcome, not a run failure: the PR simply waits for
// humans, so runMergeStep returns FailureNone. Only a port/transport error (or a
// failure to record) is a run failure.
func runMergeStep(ctx context.Context, mergePort MergeActions, rec prRecorder, job *spec.Spec, run store.Run, tc TriggerContext, st spec.Step) (store.FailureClass, error) {
	if mergePort == nil {
		if err := rec.PolicyDecision(PolicyDecision{
			Policy:   "merge_policy",
			Decision: "deferred",
			Reason:   fmt.Sprintf("%s: no merge port wired (no GitHub credentials)", st.Uses),
		}); err != nil {
			return store.FailureInternal, err
		}
		return store.FailureNone, nil
	}
	if st.When != "" {
		// The guard namespace (policy.*/trigger.*) is evaluated by AS-152; acting on
		// an unevaluated guard would be unsafe, so skip fail-closed and say so (D0).
		if err := rec.PolicyDecision(PolicyDecision{
			Policy:   "merge_policy",
			Decision: "deferred",
			Reason:   fmt.Sprintf("%s: guarded by when=%q (guard evaluation is AS-152)", st.Uses, st.When),
		}); err != nil {
			return store.FailureInternal, err
		}
		return store.FailureNone, nil
	}

	repo := prRepo(job, tc)
	if repo == "" || tc.Number <= 0 {
		// No concrete PR target (an org-scoped or targetless run); nothing to merge.
		return store.FailureNone, nil
	}
	target := GitHubTarget{Repository: repo, Number: tc.Number}

	facts, err := mergePort.MergeFacts(ctx, target)
	if err != nil {
		return recordPRFailure(rec, GitHubAction{Action: "merge_facts", Repository: repo, PRNumber: tc.Number}, err)
	}
	// The budget outcome is the run's, not something the PR port can see.
	if job.Budget.Run > 0 && run.CostUSD > job.Budget.Run {
		facts.BudgetExceeded = true
	}

	decision := EvaluateMerge(job.MergePolicy, facts)
	dec := "blocked"
	if decision.Allowed {
		dec = "approved"
	}
	if err := rec.PolicyDecision(PolicyDecision{
		Policy:   "merge_policy",
		Decision: dec,
		Reason:   decision.Reason,
		Inputs:   decision.Inputs,
	}); err != nil {
		return store.FailureInternal, err
	}
	if !decision.Allowed {
		// Fail-closed but not a run failure: the PR waits for humans.
		return store.FailureNone, nil
	}

	act := GitHubAction{Repository: repo, PRNumber: tc.Number, Ref: facts.Head}
	var url string
	if st.Uses == "github.merge" || decision.Mode == "manual" {
		act.Action = "merge"
		url, err = mergePort.Merge(ctx, target)
	} else {
		act.Action = "enable_auto_merge"
		url, err = mergePort.EnableAutoMerge(ctx, target)
	}
	if err != nil {
		return recordPRFailure(rec, act, err)
	}
	act.URL, act.Outcome = url, "ok"
	if err := rec.GitHubAction(act); err != nil {
		return store.FailureInternal, err
	}
	return store.FailureNone, nil
}
