package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/insights"
)

// repeatedCommandSession scripts the same command run three times so the
// retrospective produces an applicable memory-file suggestion.
func repeatedCommandSession(t *testing.T, ctl *chatSession, command string) {
	t.Helper()
	for i := 0; i < 3; i++ {
		appendBlock(t, ctl, shellCall("k", command))
		appendBlock(t, ctl, shellResult("k", false))
	}
}

// TestBuildSubAgentsRegistersInsightsWriter asserts the insights-writer ships as a
// built-in, defaults on, and costs nothing to leave enabled (AS-045 / §7.19).
func TestBuildSubAgentsRegistersInsightsWriter(t *testing.T) {
	reg, _, err := buildSubAgents(nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildSubAgents: %v", err)
	}
	m, ok := reg.Effective(insights.Name)
	if !ok {
		t.Fatalf("built-in %q not registered", insights.Name)
	}
	if !m.EnabledByDefault {
		t.Fatalf("built-in %q should default on", insights.Name)
	}
}

// TestCmdInsightsDashboard asserts /insights renders the measured retrospective
// with at least one applicable, numbered suggestion (PRD §7.14 AC).
func TestCmdInsightsDashboard(t *testing.T) {
	ctl := newTestController(t)
	repeatedCommandSession(t, ctl, "make test")

	out, err := ctl.cmdInsights(context.Background(), nil)
	if err != nil {
		t.Fatalf("cmdInsights: %v", err)
	}
	if !strings.Contains(out.Text, "make test") {
		t.Errorf("dashboard missing the repeated command:\n%s", out.Text)
	}
	if !strings.Contains(out.Text, "apply: /insights apply 1") {
		t.Errorf("dashboard missing an applicable suggestion:\n%s", out.Text)
	}
}

// TestCmdInsightsApply asserts applying a memory-file suggestion shows the diff
// and lands the line, and that re-applying is idempotent (PRD §7.14 AC).
func TestCmdInsightsApply(t *testing.T) {
	ctl := newTestController(t)
	repeatedCommandSession(t, ctl, "make test")

	out, err := ctl.cmdInsights(context.Background(), []string{"apply", "1"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !strings.Contains(out.Text, "Applied to") || !strings.Contains(out.Text, "+ ") {
		t.Errorf("apply output missing diff:\n%s", out.Text)
	}

	target := filepath.Join(ctl.wd, insights.DefaultMemoryTarget)
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read memory file: %v", err)
	}
	if !strings.Contains(string(body), "make test") {
		t.Errorf("memory file missing the applied line:\n%s", body)
	}

	again, err := ctl.cmdInsights(context.Background(), []string{"apply", "1"})
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if !strings.Contains(again.Text, "Already in") {
		t.Errorf("re-apply not idempotent:\n%s", again.Text)
	}
}
