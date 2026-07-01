package orchestrator

// PR lifecycle automation (AS-149). AS-147 delivered the reactive hook layer
// (label/comment/status) and deliberately left the richer PR lifecycle actions —
// branch + create-or-update PR — to this ticket. This file owns the deterministic
// shell for those actions: the [PRActions] port a step declares through
// github.create_or_update_pr, the stable Smith-owned branch/PR identity a rerun
// reuses, the safety check that a PR is Smith-owned before it is updated, the
// deterministic run-summary body, and the append-only session record of each
// action — all offline-testable with a fake port and no live credentials.
//
// Like the AS-147 hook runner it is split from its transport: the authenticated
// client that actually calls the GitHub API is AS-148's domain (scoped token in a
// proxy outside the runner, push restricted to the run's own branch), injected
// through [SessionExecutor.WithPRActions]. Merge and auto-merge are NOT performed
// here — they are delegated to the AS-157 policy engine (this file records a
// deferral decision rather than acting on a github.merge/enable_auto_merge step,
// so the punt is explicit per D0, never a silent side effect from prompt text).

import (
	"context"
	"fmt"
	"strings"

	"github.com/tonitienda/agent-smith/internal/orchestrator/spec"
	"github.com/tonitienda/agent-smith/internal/orchestrator/store"
)

// smithBranchPrefix marks a branch (and therefore its PR) as Smith-owned. The PR
// lifecycle only ever creates, looks up, and updates branches under this prefix,
// so an update can never touch a human-authored branch — the deterministic
// ownership guarantee AS-149's safety check rests on.
const smithBranchPrefix = "smith/"

// PullRequest is a pull request as the port reports it back: enough for the
// lifecycle to link it, decide create-vs-update, and verify Smith ownership.
type PullRequest struct {
	Number int
	URL    string
	Head   string // head (source) branch
	Author string // login of the PR author, when the port knows it
}

// PRContent is the create/update payload: the deterministic title, run-summary
// body, and the head→base branch pair the run works on.
type PRContent struct {
	Title string
	Body  string
	Head  string // Smith-owned source branch (always smithBranchPrefix-prefixed)
	Base  string // target branch the PR merges into
}

// PRActions is the deterministic PR-lifecycle port (AS-149), the branch/PR sibling
// of the AS-147 [GitHubActions] label/comment/status port. The authenticated
// transport implementing it against the GitHub API is AS-148; a fake exercises the
// lifecycle offline. Every method reports the affected resource's URL (for the
// session record) and an error the lifecycle records as a failed action.
type PRActions interface {
	// EnsureBranch creates branch off base in repo when it does not yet exist and
	// returns its URL. It must never move an existing branch's ref, so a rerun
	// leaves the run's branch — and every unrelated branch — untouched.
	EnsureBranch(ctx context.Context, repo, branch, base string) (url string, err error)
	// FindOpenPR returns the open PR whose head is `head`, or nil when none is
	// open. The lifecycle only ever queries a Smith-owned head.
	FindOpenPR(ctx context.Context, repo, head string) (*PullRequest, error)
	// CreatePR opens a PR from c.Head into c.Base and returns its ref.
	CreatePR(ctx context.Context, repo string, c PRContent) (PullRequest, error)
	// UpdatePR edits an existing PR's title and body and returns its URL.
	UpdatePR(ctx context.Context, repo string, number int, c PRContent) (url string, err error)
}

// prRecorder is the subset of *Recorder the PR lifecycle writes through, kept as an
// interface so the lifecycle is unit-testable without a session store.
type prRecorder interface {
	GitHubAction(a GitHubAction) error
	PolicyDecision(d PolicyDecision) error
	SessionID() string
	artifactIDs() []string
}

// smithBranch is the stable Smith-owned head branch for a run. Keying on the
// trigger's issue/PR number (when present) makes a rerun of the same issue reuse
// the same branch — and therefore update the same PR — while a run with no GitHub
// target falls back to the job id so repeated manual runs of one job still
// converge on a single Smith PR rather than opening a new one each time.
func smithBranch(job *spec.Spec, tc TriggerContext) string {
	if tc.Number > 0 {
		return fmt.Sprintf("%s%s/issue-%d", smithBranchPrefix, job.ID, tc.Number)
	}
	return smithBranchPrefix + job.ID
}

// prBase is the branch a Smith PR targets: the trigger's base ref when the event
// carried one (a PR event), else the conventional default. It is never a
// Smith-owned branch, so head and base cannot collide.
func prBase(tc TriggerContext) string {
	if tc.Base != "" {
		return tc.Base
	}
	return "main"
}

// prRepo resolves the "owner/name" a PR action targets: the spec's declared
// repository wins (the job is bound to it), falling back to the trigger's
// repository for a spec that scopes to an org.
func prRepo(job *spec.Spec, tc TriggerContext) string {
	if job.Repository != "" {
		return job.Repository
	}
	return tc.Repository
}

// runPRSteps performs the deterministic PR-lifecycle steps a job declares in its
// body. It is a no-op when no port is wired or the job declares no PR step, so an
// orchestrator without GitHub credentials still runs cleanly. A github.merge or
// github.enable_auto_merge step is not executed here: it is delegated to the
// AS-157 policy engine, and a deferral decision is recorded so the punt is
// explicit. The returned failure class is the terminal class when a PR action
// fails (blocked_policy for an ownership violation, internal otherwise); it is
// FailureNone on success.
func runPRSteps(ctx context.Context, pr PRActions, rec prRecorder, job *spec.Spec, run store.Run, tc TriggerContext) (store.FailureClass, error) {
	if pr == nil || job == nil {
		return store.FailureNone, nil
	}
	for _, st := range job.Steps {
		switch st.Uses {
		case "github.enable_auto_merge", "github.merge":
			// Merge decisions belong to the AS-157 policy engine, not this
			// deterministic shell; record the deferral rather than acting (D0).
			if err := rec.PolicyDecision(PolicyDecision{
				Policy:   "merge_policy",
				Decision: "deferred",
				Reason:   fmt.Sprintf("%s delegated to AS-157 policy engine", st.Uses),
			}); err != nil {
				return store.FailureInternal, err
			}
		case "github.create_or_update_pr":
			// A guarded PR step is skipped fail-closed until the policy engine can
			// evaluate the guard, mirroring the AS-147 hook runner: opening a PR on
			// an unevaluated guard would be an unsafe side effect.
			if st.When != "" {
				continue
			}
			if fc, err := createOrUpdatePR(ctx, pr, rec, job, run, tc); err != nil {
				return fc, err
			}
		}
	}
	return store.FailureNone, nil
}

// createOrUpdatePR ensures the run's Smith-owned branch exists, then opens a new PR
// or updates the existing Smith-owned PR for that branch, recording each action on
// the run's session. Before updating an existing PR it verifies the PR is
// Smith-owned (its head is the smithBranchPrefix branch we manage); a PR that is
// not is refused fail-closed so the lifecycle can never edit a human's PR.
func createOrUpdatePR(ctx context.Context, pr PRActions, rec prRecorder, job *spec.Spec, run store.Run, tc TriggerContext) (store.FailureClass, error) {
	repo := prRepo(job, tc)
	if repo == "" {
		// No repository to act on (an org-scoped spec on a targetless run); nothing
		// to open, and acting with an empty repo would only produce API errors.
		return store.FailureNone, nil
	}
	head := smithBranch(job, tc)
	base := prBase(tc)

	branchURL, err := pr.EnsureBranch(ctx, repo, head, base)
	if err != nil {
		return recordPRFailure(rec, GitHubAction{Action: "create_branch", Repository: repo, Ref: head}, err)
	}
	if err := rec.GitHubAction(GitHubAction{Action: "create_branch", Repository: repo, Ref: head, URL: branchURL, Outcome: "ok"}); err != nil {
		return store.FailureInternal, err
	}

	existing, err := pr.FindOpenPR(ctx, repo, head)
	if err != nil {
		return recordPRFailure(rec, GitHubAction{Action: "find_pr", Repository: repo, Ref: head}, err)
	}

	content := PRContent{
		Title: prTitle(job, run),
		Body:  renderPRBody(job, run, tc, rec.SessionID(), rec.artifactIDs()),
		Head:  head,
		Base:  base,
	}

	if existing != nil {
		if !smithOwned(*existing, head) {
			// Safety check (AS-149): only a Smith-owned PR may be updated. Refuse and
			// fail the run closed rather than editing a PR we do not own.
			reason := fmt.Sprintf("open PR #%d head %q is not the Smith-owned branch %q", existing.Number, existing.Head, head)
			_ = rec.PolicyDecision(PolicyDecision{Policy: "pr_ownership", Decision: "blocked", Reason: reason}) //nolint:errcheck // the ownership block is what we report
			return store.FailureBlockedPolicy, fmt.Errorf("orchestrator: refusing to update non-Smith PR: %s", reason)
		}
		url, err := pr.UpdatePR(ctx, repo, existing.Number, content)
		if err != nil {
			return recordPRFailure(rec, GitHubAction{Action: "update_pr", Repository: repo, Ref: head, PRNumber: existing.Number}, err)
		}
		return store.FailureNone, rec.GitHubAction(GitHubAction{Action: "update_pr", Repository: repo, Ref: head, PRNumber: existing.Number, URL: url, Outcome: "ok"})
	}

	opened, err := pr.CreatePR(ctx, repo, content)
	if err != nil {
		return recordPRFailure(rec, GitHubAction{Action: "open_pr", Repository: repo, Ref: head}, err)
	}
	return store.FailureNone, rec.GitHubAction(GitHubAction{Action: "open_pr", Repository: repo, Ref: head, PRNumber: opened.Number, URL: opened.URL, Outcome: "ok"})
}

// recordPRFailure records a failed PR action and returns the internal failure
// class with the wrapped error, mirroring the AS-147 hook runner's record-then-
// report posture so a failed action is never lost from the session.
func recordPRFailure(rec prRecorder, act GitHubAction, cause error) (store.FailureClass, error) {
	act.Outcome, act.Error = "failed", cause.Error()
	_ = rec.GitHubAction(act) //nolint:errcheck // record the failure; the original cause is what we report
	return store.FailureInternal, fmt.Errorf("orchestrator: pr %s on %s: %w", act.Action, act.Repository, cause)
}

// smithOwned reports whether an existing PR is the Smith-owned PR for head: its
// head must be exactly the smithBranchPrefix branch this lifecycle manages. This
// is what lets an update be safe without trusting the PR author field — we only
// ever act on our own branch.
func smithOwned(pr PullRequest, head string) bool {
	return pr.Head == head && strings.HasPrefix(head, smithBranchPrefix)
}

// prTitle is the deterministic PR title: the job's description when it has one,
// else the job id, tagged as a Smith PR so a maintainer can recognise it at a
// glance and a rerun produces a stable title.
func prTitle(job *spec.Spec, run store.Run) string {
	subject := job.Description
	if subject == "" {
		subject = job.ID
	}
	return fmt.Sprintf("Smith: %s (job %s)", subject, run.JobID)
}

// renderPRBody builds the deterministic run-summary body (AS-149 acceptance): job
// id, provider roles, budget/cost, artifacts, and the session link. It is a pure
// function of the run so a rerun re-emits the same body for the same state, and it
// carries a trailing marker so an update is idempotent and the PR is identifiable
// as Smith-authored.
func renderPRBody(job *spec.Spec, run store.Run, tc TriggerContext, sessionID string, artifactIDs []string) string {
	var b strings.Builder
	b.WriteString("### Smith run summary\n\n")
	fmt.Fprintf(&b, "- **Job:** `%s`\n", run.JobID)
	fmt.Fprintf(&b, "- **Run:** `%s` (attempt %d)\n", run.ID, run.Attempt)
	if tc.Number > 0 {
		fmt.Fprintf(&b, "- **Source:** %s#%d\n", prRepo(job, tc), tc.Number)
	}

	if roles := providerRoles(job); len(roles) > 0 {
		b.WriteString("- **Provider roles:**\n")
		for _, r := range roles {
			fmt.Fprintf(&b, "  - %s\n", r)
		}
	}

	fmt.Fprintf(&b, "- **Budget:** $%.2f/run", job.Budget.Run)
	if run.CostUSD > 0 {
		fmt.Fprintf(&b, " — **cost so far:** $%.2f", run.CostUSD)
	}
	b.WriteString("\n")

	if len(artifactIDs) > 0 {
		fmt.Fprintf(&b, "- **Artifacts:** %s\n", strings.Join(artifactIDs, ", "))
	}
	if sessionID != "" {
		fmt.Fprintf(&b, "- **Session:** `%s`\n", sessionID)
	}

	fmt.Fprintf(&b, "\n<!-- smith-run: %s -->\n", run.ID)
	return b.String()
}

// providerRoles renders each agent step's role and the provider it resolves to via
// the job's routing policies (an unresolved policy is shown by name), in step
// order. Deterministic-only steps (github.*) declare no role and are skipped.
func providerRoles(job *spec.Spec) []string {
	var out []string
	for _, st := range job.Steps {
		if st.Role == "" {
			continue
		}
		line := st.Role
		if st.ProviderPolicy != "" {
			if r, ok := job.Routing[st.ProviderPolicy]; ok {
				line += fmt.Sprintf(" → %s/%s", r.Provider, r.Model)
			} else {
				line += fmt.Sprintf(" → %s", st.ProviderPolicy)
			}
		}
		out = append(out, line)
	}
	return out
}
