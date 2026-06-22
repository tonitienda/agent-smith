package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/skillrollup"
	"github.com/tonitienda/agent-smith/internal/subagent"
)

// rollupWithFindings wires a durable rollup store onto the controller carrying the
// same rediscovered fact across the given sessions, so /skills has a cross-session
// corpus to render.
func rollupWithFindings(t *testing.T, ctl *chatSession, summary, target, diff string, sessions ...string) *skillrollup.Store {
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
	return store
}

// TestCmdSkillsReport asserts /skills renders the cross-session rollup with the
// 3+-session escalation and a numbered pending remedy (AS-050 AC 1, 2).
func TestCmdSkillsReport(t *testing.T) {
	ctl := newTestController(t)
	rollupWithFindings(t, ctl,
		"Rediscovered working command: make test", "AGENT.md", "+ `make test` — working command",
		"s1", "s2", "s3")

	out, err := ctl.cmdSkills(context.Background(), nil)
	if err != nil {
		t.Fatalf("cmdSkills: %v", err)
	}
	for _, want := range []string{"make test", "escalated", "apply: /skills apply 1"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("report missing %q:\n%s", want, out.Text)
		}
	}
}

// TestCmdSkillsApply asserts applying a remedy lands the line in its target file,
// marks the finding resolved (it drops from pending), and is idempotent (AC 3).
func TestCmdSkillsApply(t *testing.T) {
	ctl := newTestController(t)
	store := rollupWithFindings(t, ctl,
		"Rediscovered working command: make test", "AGENT.md", "+ `make test` — working command",
		"s1", "s2", "s3")

	out, err := ctl.cmdSkills(context.Background(), []string{"apply", "1"})
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

	again, err := ctl.cmdSkills(context.Background(), []string{"apply", "1"})
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if !strings.Contains(again.Text, "No remedy #1") {
		t.Errorf("re-apply should find no pending remedy:\n%s", again.Text)
	}
}

// TestCmdSkillsNoStore asserts /skills degrades gracefully when no durable store
// was wired (a face that opted out) rather than panicking.
func TestCmdSkillsNoStore(t *testing.T) {
	ctl := newTestController(t)
	out, err := ctl.cmdSkills(context.Background(), nil)
	if err != nil {
		t.Fatalf("cmdSkills: %v", err)
	}
	if !strings.Contains(out.Text, "Skills & findings") {
		t.Errorf("expected the report header even with no store:\n%s", out.Text)
	}
}
