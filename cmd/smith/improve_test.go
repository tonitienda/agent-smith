package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/improve"
	"github.com/tonitienda/agent-smith/internal/skillrollup"
	"github.com/tonitienda/agent-smith/internal/subagent"
)

// improveCorpus wires a durable rollup store carrying the same finding across the
// given sessions plus an in-memory decision ledger, so /improve has a
// cross-session corpus and a place to remember dismissals.
func improveCorpus(t *testing.T, ctl *chatSession, summary, target, diff string, sessions ...string) *skillrollup.Store {
	t.Helper()
	store, err := skillrollup.Open(filepath.Join(t.TempDir(), "f.jsonl"))
	if err != nil {
		t.Fatalf("open rollup: %v", err)
	}
	for _, s := range sessions {
		store.Record(subagent.Finding{
			SubAgent: "rediscovered-fact-detector",
			Session:  s,
			Kind:     "rediscovered_fact",
			Summary:  summary,
			Proposal: &subagent.Edit{Target: target, Description: diff},
		})
	}
	ctl.setSubAgents(nil, store)
	ctl.setImproveLedger(improve.NewMemLedger())
	return store
}

// TestCmdImproveReport asserts /improve promotes a finding seen across ≥2
// sessions to a numbered proposal with cross-session evidence (AS-058 AC 1).
func TestCmdImproveReport(t *testing.T) {
	ctl := newTestController(t)
	improveCorpus(t, ctl,
		"Rediscovered working command: make test", "AGENT.md", "- `make test` — working command",
		"s1", "s2")

	out, err := ctl.cmdImprove(context.Background(), nil)
	if err != nil {
		t.Fatalf("cmdImprove: %v", err)
	}
	for _, want := range []string{"make test", "seen across 2 sessions", "apply: /improve apply 1"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("report missing %q:\n%s", want, out.Text)
		}
	}
}

// TestCmdImproveSingleSessionNotProposed asserts a finding seen in only one
// session is not yet a proposal (the MinSessions threshold).
func TestCmdImproveSingleSessionNotProposed(t *testing.T) {
	ctl := newTestController(t)
	improveCorpus(t, ctl, "one-off", "AGENT.md", "- one off", "s1")

	out, err := ctl.cmdImprove(context.Background(), nil)
	if err != nil {
		t.Fatalf("cmdImprove: %v", err)
	}
	if !strings.Contains(out.Text, "No proposals yet") {
		t.Errorf("single-session finding should not be proposed:\n%s", out.Text)
	}
}

// TestCmdImproveApply asserts applying a proposal lands the line in its target,
// marks the finding resolved, and is idempotent (AS-058 AC 2, 3 — propose-only
// write happens only here).
func TestCmdImproveApply(t *testing.T) {
	ctl := newTestController(t)
	store := improveCorpus(t, ctl,
		"Rediscovered working command: make test", "AGENT.md", "- `make test` — working command",
		"s1", "s2")

	out, err := ctl.cmdImprove(context.Background(), []string{"apply", "1"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !strings.Contains(out.Text, "Applied to") || !strings.Contains(out.Text, "resolved") {
		t.Errorf("apply output unexpected:\n%s", out.Text)
	}
	body, err := os.ReadFile(filepath.Join(ctl.wd, "AGENT.md"))
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !strings.Contains(string(body), "make test") {
		t.Errorf("target file missing applied line:\n%s", body)
	}
	if rep := store.Rollup(); len(rep.Pending) != 0 {
		t.Errorf("finding still pending after apply: %d", len(rep.Pending))
	}
}

// TestCmdImproveDismissPersists asserts a dismissed proposal drops out of the
// queue and stays out (the remembered-decision requirement, AS-058 AC 2).
func TestCmdImproveDismissPersists(t *testing.T) {
	ctl := newTestController(t)
	improveCorpus(t, ctl, "recurring gap", "AGENT.md", "- note the gap", "s1", "s2")

	if _, err := ctl.cmdImprove(context.Background(), []string{"dismiss", "1"}); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	out, err := ctl.cmdImprove(context.Background(), nil)
	if err != nil {
		t.Fatalf("cmdImprove: %v", err)
	}
	if !strings.Contains(out.Text, "No proposals yet") {
		t.Errorf("dismissed proposal should not reappear:\n%s", out.Text)
	}
}

// TestCmdImproveNoStore asserts /improve degrades gracefully when no durable
// store was wired rather than panicking.
func TestCmdImproveNoStore(t *testing.T) {
	ctl := newTestController(t)
	out, err := ctl.cmdImprove(context.Background(), nil)
	if err != nil {
		t.Fatalf("cmdImprove: %v", err)
	}
	if !strings.Contains(out.Text, "No cross-session findings store") {
		t.Errorf("expected the no-store message:\n%s", out.Text)
	}
}
