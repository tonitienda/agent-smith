package orchestrator_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/orchestrator"
	"github.com/tonitienda/agent-smith/internal/orchestrator/spec"
	"github.com/tonitienda/agent-smith/internal/orchestrator/store"
	"github.com/tonitienda/agent-smith/internal/session"
)

// policyDecisionBlocks replays the single recorded session and returns its decoded
// policy-decision blocks, mirroring githubActionBlocks in hooks_test.go.
func policyDecisionBlocks(t *testing.T, sessions *session.Store) []orchestrator.PolicyDecision {
	t.Helper()
	summaries, err := sessions.List()
	if err != nil || len(summaries) != 1 {
		t.Fatalf("List: %v (n=%d)", err, len(summaries))
	}
	reopened, err := session.OpenAt(summaries[0].Dir)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer func() { _ = reopened.Log.Close() }()
	var out []orchestrator.PolicyDecision
	for _, b := range reopened.Log.Events() {
		if d, ok := orchestrator.PolicyDecisionOf(b); ok {
			out = append(out, d)
		}
	}
	return out
}

// fakePR is an in-memory PRActions port. It records every call so a test can
// assert the deterministic lifecycle without a live GitHub API, and can be seeded
// with an existing open PR (to exercise the update-vs-create and ownership paths)
// or forced to fail one method.
type fakePR struct {
	calls    []string
	branches []string // branches EnsureBranch created, in order
	existing *orchestrator.PullRequest
	updated  orchestrator.PRContent // last content passed to UpdatePR
	created  orchestrator.PRContent // last content passed to CreatePR
	failOn   string                 // method (ensure_branch|find|create|update) to error on
}

func (f *fakePR) EnsureBranch(_ context.Context, _, branch, _ string) (string, error) {
	f.calls = append(f.calls, "ensure_branch")
	if f.failOn == "ensure_branch" {
		return "", errors.New("boom")
	}
	f.branches = append(f.branches, branch)
	return "https://gh/tree/" + branch, nil
}

func (f *fakePR) FindOpenPR(_ context.Context, _, _ string) (*orchestrator.PullRequest, error) {
	f.calls = append(f.calls, "find")
	if f.failOn == "find" {
		return nil, errors.New("boom")
	}
	return f.existing, nil
}

func (f *fakePR) CreatePR(_ context.Context, _ string, c orchestrator.PRContent) (orchestrator.PullRequest, error) {
	f.calls = append(f.calls, "create")
	if f.failOn == "create" {
		return orchestrator.PullRequest{}, errors.New("boom")
	}
	f.created = c
	return orchestrator.PullRequest{Number: 101, URL: "https://gh/pr/101", Head: c.Head}, nil
}

func (f *fakePR) UpdatePR(_ context.Context, _ string, number int, c orchestrator.PRContent) (string, error) {
	f.calls = append(f.calls, "update")
	if f.failOn == "update" {
		return "", errors.New("boom")
	}
	f.updated = c
	return "https://gh/pr/" + strconv.Itoa(number), nil
}

func prJob() *spec.Spec {
	return &spec.Spec{
		ID:          "impl",
		Repository:  "acme/widgets",
		Description: "ship it",
		Budget:      spec.Budget{Run: 4},
		Routing:     map[string]spec.Route{"impl": {Provider: "anthropic", Model: "claude-opus-4-8"}},
		Steps: []spec.Step{
			{ID: "implement", Uses: "agent.implement", Role: "implementation", ProviderPolicy: "impl"},
			{ID: "open-pr", Uses: "github.create_or_update_pr"},
		},
	}
}

// A github-triggered run with no existing PR ensures its Smith-owned branch and
// opens a PR whose body carries the run summary; both actions are recorded and the
// PR link is folded into the session linkage.
func TestSessionExecutorOpensPR(t *testing.T) {
	sessions := newSessionStore(t)
	pr := &fakePR{}
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).WithPRActions(pr)

	out, err := exec.Execute(context.Background(), ghRun(t, 42), prJob())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Status != store.StatusSucceeded {
		t.Fatalf("want succeeded, got %q", out.Status)
	}
	want := []string{"ensure_branch", "find", "create"}
	if strings.Join(pr.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", pr.calls, want)
	}
	if len(pr.branches) != 1 || !strings.HasPrefix(pr.branches[0], "smith/impl/issue-42") {
		t.Fatalf("branch = %v, want smith/impl/issue-42", pr.branches)
	}
	if pr.created.Head != "smith/impl/issue-42" || pr.created.Base != "main" {
		t.Fatalf("content head/base = %q/%q", pr.created.Head, pr.created.Base)
	}
	for _, want := range []string{"Smith run summary", "`job_a`", "implementation → anthropic/claude-opus-4-8", "$4.00/run", "acme/widgets#42"} {
		if !strings.Contains(pr.created.Body, want) {
			t.Fatalf("body missing %q:\n%s", want, pr.created.Body)
		}
	}

	blocks := githubActionBlocks(t, sessions)
	if len(blocks) != 2 || blocks[0].Action != "create_branch" || blocks[1].Action != "open_pr" {
		t.Fatalf("recorded actions = %+v", blocks)
	}
	if blocks[1].PRNumber != 101 || blocks[1].URL != "https://gh/pr/101" {
		t.Fatalf("open_pr block = %+v", blocks[1])
	}
}

// A rerun for the same issue finds the existing Smith-owned PR and updates it
// rather than opening a second one.
func TestSessionExecutorUpdatesExistingSmithPR(t *testing.T) {
	sessions := newSessionStore(t)
	pr := &fakePR{existing: &orchestrator.PullRequest{Number: 7, URL: "https://gh/pr/7", Head: "smith/impl/issue-42"}}
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).WithPRActions(pr)

	out, err := exec.Execute(context.Background(), ghRun(t, 42), prJob())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Status != store.StatusSucceeded {
		t.Fatalf("want succeeded, got %q", out.Status)
	}
	want := []string{"ensure_branch", "find", "update"}
	if strings.Join(pr.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", pr.calls, want)
	}
	if pr.updated.Head != "smith/impl/issue-42" {
		t.Fatalf("update content head = %q", pr.updated.Head)
	}
	blocks := githubActionBlocks(t, sessions)
	if len(blocks) != 2 || blocks[1].Action != "update_pr" || blocks[1].PRNumber != 7 {
		t.Fatalf("recorded actions = %+v", blocks)
	}
}

// An open PR whose head is NOT the Smith-owned branch is refused fail-closed: the
// run fails blocked_policy, an ownership decision is recorded, and no update fires.
func TestSessionExecutorRefusesNonSmithPR(t *testing.T) {
	sessions := newSessionStore(t)
	pr := &fakePR{existing: &orchestrator.PullRequest{Number: 9, Head: "feature/human", Author: "human"}}
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).WithPRActions(pr)

	out, err := exec.Execute(context.Background(), ghRun(t, 42), prJob())
	if err == nil {
		t.Fatal("want error refusing non-Smith PR")
	}
	if out.Status != store.StatusFailed || out.FailureClass != store.FailureBlockedPolicy {
		t.Fatalf("out = %+v, want failed/blocked_policy", out)
	}
	for _, c := range pr.calls {
		if c == "update" {
			t.Fatal("updated a non-Smith PR")
		}
	}
}

// A github.merge / github.enable_auto_merge step is delegated to AS-157: it is not
// executed here, and a deferral decision is recorded so the punt is explicit.
func TestSessionExecutorDefersMergeToPolicy(t *testing.T) {
	sessions := newSessionStore(t)
	pr := &fakePR{}
	job := prJob()
	job.Steps = append(job.Steps, spec.Step{ID: "automerge", Uses: "github.enable_auto_merge"})
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).WithPRActions(pr)

	if _, err := exec.Execute(context.Background(), ghRun(t, 42), job); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	decisions := policyDecisionBlocks(t, sessions)
	var found bool
	for _, d := range decisions {
		if d.Policy == "merge_policy" && d.Decision == "deferred" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no merge deferral decision recorded: %+v", decisions)
	}
}

// A guarded PR step is skipped fail-closed until the policy engine can evaluate the
// guard — no branch or PR is created.
func TestSessionExecutorSkipsGuardedPRStep(t *testing.T) {
	sessions := newSessionStore(t)
	pr := &fakePR{}
	job := prJob()
	job.Steps[1].When = "policy.pr_allowed"
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).WithPRActions(pr)

	if _, err := exec.Execute(context.Background(), ghRun(t, 42), job); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(pr.calls) != 0 {
		t.Fatalf("guarded PR step acted: %v", pr.calls)
	}
}

// A failing PR action fails the run closed (internal) and records the failed
// action on the session.
func TestSessionExecutorPRFailureFailsRun(t *testing.T) {
	sessions := newSessionStore(t)
	pr := &fakePR{failOn: "create"}
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).WithPRActions(pr)

	out, err := exec.Execute(context.Background(), ghRun(t, 42), prJob())
	if err == nil {
		t.Fatal("want error from failed PR action")
	}
	if out.Status != store.StatusFailed || out.FailureClass != store.FailureInternal {
		t.Fatalf("out = %+v, want failed/internal", out)
	}
	blocks := githubActionBlocks(t, sessions)
	if len(blocks) == 0 || blocks[len(blocks)-1].Outcome != "failed" {
		t.Fatalf("want a recorded failed action, got %+v", blocks)
	}
}

// Without a wired PR port, a job's create_or_update_pr step is skipped so an
// orchestrator with no GitHub credentials still runs the run's work.
func TestSessionExecutorNoPRPortSkipsPRSteps(t *testing.T) {
	sessions := newSessionStore(t)
	out, err := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).
		Execute(context.Background(), ghRun(t, 1), prJob())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Status != store.StatusSucceeded {
		t.Fatalf("want succeeded, got %q", out.Status)
	}
	if len(githubActionBlocks(t, sessions)) != 0 {
		t.Fatal("recorded a PR action with no port")
	}
}
