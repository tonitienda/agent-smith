package orchestrator_test

import (
	"context"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/orchestrator"
	"github.com/tonitienda/agent-smith/internal/orchestrator/spec"
	"github.com/tonitienda/agent-smith/internal/orchestrator/store"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/schema"
)

func newSessionStore(t *testing.T) *session.Store {
	t.Helper()
	st, err := session.NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	return st
}

func sampleRun() store.Run {
	return store.Run{ID: "run_1", JobID: "job_a", TriggerKind: "cron", Attempt: 2}
}

// A recorded run is an ordinary, resumable Smith session: it is discoverable by
// List and re-openable by OpenAt with its lifecycle blocks intact.
func TestRecorderRunIsANormalSession(t *testing.T) {
	sessions := newSessionStore(t)
	job := &spec.Spec{ID: "job_a", Repository: "acme/widgets", Owner: "acme"}

	rec, err := orchestrator.NewRecorder(sessions, sampleRun(), job)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	if rec.SessionID() == "" {
		t.Fatal("recorder returned an empty session id")
	}
	if err := rec.PolicyDecision(orchestrator.PolicyDecision{Policy: "merge_policy", Decision: "approved"}); err != nil {
		t.Fatalf("PolicyDecision: %v", err)
	}
	if err := rec.GitHubAction(orchestrator.GitHubAction{Action: "open_pr", PRNumber: 7, URL: "https://gh/pr/7"}); err != nil {
		t.Fatalf("GitHubAction: %v", err)
	}
	if err := rec.Artifact(orchestrator.ArtifactRef{ID: "art_1", Name: "build.log", URI: "blob://x", SHA256: "deadbeef", SizeBytes: 4096}); err != nil {
		t.Fatalf("Artifact: %v", err)
	}
	if err := rec.Finish(store.Outcome{Status: store.StatusSucceeded, CostUSD: 0.12}); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	// Discoverable by List with the run linkage on its metadata.
	summaries, err := sessions.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("want 1 session, got %d", len(summaries))
	}
	link, ok := orchestrator.RunLinkOf(summaries[0].Metadata)
	if !ok {
		t.Fatal("run linkage missing from session metadata")
	}
	if link.RunID != "run_1" || link.JobID != "job_a" || link.TriggerKind != "cron" || link.Attempt != 2 {
		t.Fatalf("unexpected linkage: %+v", link)
	}
	if link.Repository != "acme/widgets" || link.Owner != "acme" {
		t.Fatalf("spec fields not carried into linkage: %+v", link)
	}
	if len(link.PRLinks) != 1 || link.PRLinks[0] != "https://gh/pr/7" {
		t.Fatalf("PR link not folded into linkage: %+v", link.PRLinks)
	}
	if len(link.ArtifactIDs) != 1 || link.ArtifactIDs[0] != "art_1" {
		t.Fatalf("artifact id not folded into linkage: %+v", link.ArtifactIDs)
	}

	// Re-openable with lifecycle blocks decodable.
	reopened, err := session.OpenAt(summaries[0].Dir)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer func() { _ = reopened.Log.Close() }()
	events := reopened.Log.Events()

	var sawStart, sawPolicy, sawGitHub, sawArtifact, sawOutcome bool
	for _, b := range events {
		switch b.Kind {
		case orchestrator.KindRunStart:
			sawStart = true
		case orchestrator.KindPolicyDecision:
			d, ok := orchestrator.PolicyDecisionOf(b)
			if !ok || d.Decision != "approved" {
				t.Fatalf("policy block did not decode: %+v", b)
			}
			sawPolicy = true
		case orchestrator.KindGitHubAction:
			a, ok := orchestrator.GitHubActionOf(b)
			if !ok || a.PRNumber != 7 {
				t.Fatalf("github block did not decode: %+v", b)
			}
			sawGitHub = true
		case orchestrator.KindArtifactRef:
			a, ok := orchestrator.ArtifactRefOf(b)
			if !ok || a.SHA256 != "deadbeef" || a.URI != "blob://x" {
				t.Fatalf("artifact block did not decode: %+v", b)
			}
			sawArtifact = true
		case orchestrator.KindRunOutcome:
			o, ok := orchestrator.RunOutcomeOf(b)
			if !ok || o.Status != string(store.StatusSucceeded) || o.CostUSD != 0.12 {
				t.Fatalf("outcome block did not decode: %+v", b)
			}
			sawOutcome = true
		}
	}
	if !sawStart || !sawPolicy || !sawGitHub || !sawArtifact || !sawOutcome {
		t.Fatalf("missing lifecycle blocks: start=%v policy=%v github=%v artifact=%v outcome=%v",
			sawStart, sawPolicy, sawGitHub, sawArtifact, sawOutcome)
	}
}

// A large artifact must be referenced by uri+hash, never embedded: a missing
// integrity hash or uri is rejected.
func TestRecorderArtifactRequiresIntegrityReference(t *testing.T) {
	rec, err := orchestrator.NewRecorder(newSessionStore(t), sampleRun(), nil)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	if err := rec.Artifact(orchestrator.ArtifactRef{ID: "art", URI: "blob://x"}); err == nil {
		t.Fatal("artifact without a sha256 should be rejected")
	}
	if err := rec.Artifact(orchestrator.ArtifactRef{ID: "art", SHA256: "abc"}); err == nil {
		t.Fatal("artifact without a uri should be rejected")
	}
	_ = rec.Finish(store.Outcome{Status: store.StatusFailed, FailureClass: store.FailureInternal})
}

// Provider usage recorded on an orchestrated run is priced by cost.Summarize
// through the exact same path as an interactive session — no separate code path.
func TestOrchestratedRunPricedByCostReader(t *testing.T) {
	sessions := newSessionStore(t)
	rec, err := orchestrator.NewRecorder(sessions, sampleRun(), nil)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	in, out := 1000, 500
	tokens := &schema.Tokens{Input: &in, Output: &out}
	if err := rec.Usage("anthropic", "claude-x", "end_turn", tokens, nil); err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if err := rec.Finish(store.Outcome{Status: store.StatusSucceeded}); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	summaries, err := sessions.List()
	if err != nil || len(summaries) != 1 {
		t.Fatalf("List: %v (n=%d)", err, len(summaries))
	}
	reopened, err := session.OpenAt(summaries[0].Dir)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer func() { _ = reopened.Log.Close() }()

	summary := cost.Summarize(reopened.Log.Events(), nil)
	if summary.Total.Total() != in+out {
		t.Fatalf("cost reader did not price orchestrated usage: got %d tokens, want %d", summary.Total.Total(), in+out)
	}
	// The usage block is the standard eventlog.KindUsage, proving reuse.
	var usageBlocks int
	for _, b := range reopened.Log.Events() {
		if b.Kind == eventlog.KindUsage {
			usageBlocks++
		}
	}
	if usageBlocks != 1 {
		t.Fatalf("want 1 standard usage block, got %d", usageBlocks)
	}
}

// The SessionExecutor wraps an inner Executor so the daemon's Outcome points at a
// real recorded session rather than a placeholder id.
func TestSessionExecutorRecordsRun(t *testing.T) {
	sessions := newSessionStore(t)
	exec := orchestrator.NewSessionExecutor(sessions, orchestrator.StubExecutor{})

	out, err := exec.Execute(context.Background(), sampleRun(), &spec.Spec{ID: "job_a"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Status != store.StatusSucceeded {
		t.Fatalf("want succeeded, got %q", out.Status)
	}
	summaries, err := sessions.List()
	if err != nil || len(summaries) != 1 {
		t.Fatalf("List: %v (n=%d)", err, len(summaries))
	}
	if out.SessionID != summaries[0].ID {
		t.Fatalf("outcome session id %q does not match recorded session %q", out.SessionID, summaries[0].ID)
	}
	if out.SessionID == "stub-run_1" {
		t.Fatal("outcome still carries the stub placeholder session id")
	}
}
