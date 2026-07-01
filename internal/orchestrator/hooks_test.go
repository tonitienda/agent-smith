package orchestrator_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/tonitienda/agent-smith/internal/orchestrator"
	"github.com/tonitienda/agent-smith/internal/orchestrator/spec"
	"github.com/tonitienda/agent-smith/internal/orchestrator/store"
	"github.com/tonitienda/agent-smith/internal/session"
)

// fakeActions is an in-memory GitHubActions port that records every call so a test
// can assert which deterministic hooks fired, in order, without a live GitHub API.
type fakeActions struct {
	calls  []string
	target orchestrator.GitHubTarget
	label  string // last label passed to AddLabel/RemoveLabel
	failOn string // action name (e.g. "add_label") to return an error for
}

func (f *fakeActions) record(action string, t orchestrator.GitHubTarget) (string, error) {
	f.calls = append(f.calls, action)
	f.target = t
	if action == f.failOn {
		return "", errors.New("boom")
	}
	return "https://gh/" + action, nil
}

func (f *fakeActions) AddLabel(_ context.Context, t orchestrator.GitHubTarget, label string) (string, error) {
	f.label = label
	return f.record("add_label", t)
}
func (f *fakeActions) RemoveLabel(_ context.Context, t orchestrator.GitHubTarget, _ string) (string, error) {
	return f.record("remove_label", t)
}
func (f *fakeActions) Comment(_ context.Context, t orchestrator.GitHubTarget, _ string) (string, error) {
	return f.record("comment", t)
}
func (f *fakeActions) SetStatus(_ context.Context, t orchestrator.GitHubTarget, _ orchestrator.StatusUpdate) (string, error) {
	return f.record("set_status", t)
}

func ghRun(t *testing.T, number int) store.Run {
	t.Helper()
	r := sampleRun()
	r.TriggerKind = "github.issue_labeled"
	raw, err := json.Marshal(orchestrator.TriggerContext{Repository: "acme/widgets", Number: number, Actor: "octocat"})
	if err != nil {
		t.Fatalf("marshal trigger context: %v", err)
	}
	r.TriggerContext = string(raw)
	return r
}

func githubActionBlocks(t *testing.T, sessions *session.Store) []orchestrator.GitHubAction {
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
	var out []orchestrator.GitHubAction
	for _, b := range reopened.Log.Events() {
		if a, ok := orchestrator.GitHubActionOf(b); ok {
			out = append(out, a)
		}
	}
	return out
}

// A github-triggered run runs its declared on_start and terminal hooks against the
// originating issue, records each on the session, and targets the right number.
func TestSessionExecutorRunsGitHubHooks(t *testing.T) {
	sessions := newSessionStore(t)
	fake := &fakeActions{}
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).WithGitHubActions(fake)

	job := &spec.Spec{ID: "job_a", Repository: "acme/widgets", Hooks: map[string][]spec.Step{
		"on_start": {{Uses: "github.add_label", With: map[string]any{"label": "in-progress"}}},
		"on_success": {
			{Uses: "github.remove_label", With: map[string]any{"label": "in-progress"}},
			{Uses: "github.comment", With: map[string]any{"body": "done"}},
			{Uses: "github.set_status", With: map[string]any{"state": "success", "context": "smith"}},
		},
	}}

	out, err := exec.Execute(context.Background(), ghRun(t, 42), job)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Status != store.StatusSucceeded {
		t.Fatalf("want succeeded, got %q", out.Status)
	}
	want := []string{"add_label", "remove_label", "comment", "set_status"}
	if len(fake.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}
	for i := range want {
		if fake.calls[i] != want[i] {
			t.Fatalf("call[%d] = %q, want %q", i, fake.calls[i], want[i])
		}
	}
	if fake.target.Number != 42 || fake.target.Repository != "acme/widgets" {
		t.Fatalf("hook targeted wrong issue: %+v", fake.target)
	}
	blocks := githubActionBlocks(t, sessions)
	if len(blocks) != 4 {
		t.Fatalf("want 4 recorded actions, got %d", len(blocks))
	}
	for _, b := range blocks {
		if b.Outcome != "ok" || b.PRNumber != 42 {
			t.Fatalf("recorded action = %+v", b)
		}
	}
}

// A run with no GitHub target (cron/manual) skips github hooks entirely rather than
// acting on issue 0.
func TestSessionExecutorSkipsHooksWithoutTarget(t *testing.T) {
	sessions := newSessionStore(t)
	fake := &fakeActions{}
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).WithGitHubActions(fake)

	job := &spec.Spec{ID: "job_a", Hooks: map[string][]spec.Step{
		"on_success": {{Uses: "github.comment", With: map[string]any{"body": "hi"}}},
	}}
	if _, err := exec.Execute(context.Background(), sampleRun(), job); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("hooks fired without a target: %v", fake.calls)
	}
}

// A trigger context with a repository but no issue/PR number (number 0) skips
// hooks rather than calling GitHub with an invalid target.
func TestSessionExecutorSkipsHooksWithoutNumber(t *testing.T) {
	sessions := newSessionStore(t)
	fake := &fakeActions{}
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).WithGitHubActions(fake)

	run := sampleRun()
	raw, _ := json.Marshal(orchestrator.TriggerContext{Repository: "acme/widgets"}) // Number 0
	run.TriggerContext = string(raw)
	job := &spec.Spec{ID: "job_a", Repository: "acme/widgets", Hooks: map[string][]spec.Step{
		"on_success": {{Uses: "github.add_label", With: map[string]any{"label": "x"}}},
	}}
	if _, err := exec.Execute(context.Background(), run, job); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("hook fired for numberless target: %v", fake.calls)
	}
}

// A non-string scalar hook argument (unquoted YAML `label: 123`) is stringified
// rather than dropped to "".
func TestSessionExecutorStringifiesScalarArg(t *testing.T) {
	sessions := newSessionStore(t)
	fake := &fakeActions{}
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).WithGitHubActions(fake)

	job := &spec.Spec{ID: "job_a", Repository: "acme/widgets", Hooks: map[string][]spec.Step{
		"on_start": {{Uses: "github.add_label", With: map[string]any{"label": 123}}},
	}}
	if _, err := exec.Execute(context.Background(), ghRun(t, 5), job); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fake.label != "123" {
		t.Fatalf("scalar label = %q, want %q", fake.label, "123")
	}
}

// A failing on_start hook fails the run closed and records the failed action.
func TestSessionExecutorOnStartHookFailureFailsRun(t *testing.T) {
	sessions := newSessionStore(t)
	fake := &fakeActions{failOn: "add_label"}
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).WithGitHubActions(fake)

	job := &spec.Spec{ID: "job_a", Repository: "acme/widgets", Hooks: map[string][]spec.Step{
		"on_start": {{Uses: "github.add_label", With: map[string]any{"label": "in-progress"}}},
	}}
	out, err := exec.Execute(context.Background(), ghRun(t, 7), job)
	if err == nil {
		t.Fatal("want error from failed on_start hook")
	}
	if out.Status != store.StatusFailed || out.FailureClass != store.FailureInternal {
		t.Fatalf("out = %+v, want failed/internal", out)
	}
	blocks := githubActionBlocks(t, sessions)
	if len(blocks) != 1 || blocks[0].Outcome != "failed" || blocks[0].Error == "" {
		t.Fatalf("want one recorded failed action, got %+v", blocks)
	}
}

// A hook step carrying a `when` guard is skipped fail-closed until the policy
// engine (AS-157/AS-152) can evaluate the guard.
func TestSessionExecutorSkipsGuardedHook(t *testing.T) {
	sessions := newSessionStore(t)
	fake := &fakeActions{}
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).WithGitHubActions(fake)

	job := &spec.Spec{ID: "job_a", Repository: "acme/widgets", Hooks: map[string][]spec.Step{
		"on_success": {{Uses: "github.comment", When: "policy.auto_merge_allowed", With: map[string]any{"body": "hi"}}},
	}}
	if _, err := exec.Execute(context.Background(), ghRun(t, 3), job); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("guarded hook fired: %v", fake.calls)
	}
}

// Without a wired actions port, a job's hooks are silently skipped so an
// orchestrator with no GitHub credentials still runs the run's work.
func TestSessionExecutorNoActionsPortSkipsHooks(t *testing.T) {
	sessions := newSessionStore(t)
	job := &spec.Spec{ID: "job_a", Repository: "acme/widgets", Hooks: map[string][]spec.Step{
		"on_success": {{Uses: "github.comment", With: map[string]any{"body": "hi"}}},
	}}
	out, err := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{}).
		Execute(context.Background(), ghRun(t, 1), job)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Status != store.StatusSucceeded {
		t.Fatalf("want succeeded, got %q", out.Status)
	}
	if len(githubActionBlocks(t, sessions)) != 0 {
		t.Fatal("recorded a github action with no actions port")
	}
}
