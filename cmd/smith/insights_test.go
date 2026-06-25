package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/insights"
	"github.com/tonitienda/agent-smith/schema"
)

// fakeRetro is a scripted insightsRetro for the on-demand /insights describe tests
// (AS-137): it returns canned suggestions plus a usage event to charge, or an error.
type fakeRetro struct {
	suggestions []insights.Suggestion
	usage       schema.Block
	err         error
	calls       int
}

func (f *fakeRetro) Describe(context.Context, insights.Report) ([]insights.Suggestion, schema.Block, error) {
	f.calls++
	return f.suggestions, f.usage, f.err
}

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
	reg, _, err := buildSubAgents(nil, nil, nil, nil, nil, nil, nil)
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

// TestCmdInsightsOffersDescribeWhenModelLayerOff asserts the base dashboard offers
// the on-demand retro only when a proposer is wired and the session-end model layer
// is off — and never makes the offer when the layer already runs at session end or
// no proposer is wired (AS-137 AC).
func TestCmdInsightsOffersDescribeWhenModelLayerOff(t *testing.T) {
	const offer = "/insights describe"

	t.Run("offered when off", func(t *testing.T) {
		ctl := newTestController(t)
		ctl.setInsightsRetro(&fakeRetro{}, false)
		out, err := ctl.cmdInsights(context.Background(), nil)
		if err != nil {
			t.Fatalf("cmdInsights: %v", err)
		}
		if !strings.Contains(out.Text, offer) {
			t.Errorf("expected the on-demand offer:\n%s", out.Text)
		}
	})

	t.Run("suppressed when model layer on", func(t *testing.T) {
		ctl := newTestController(t)
		ctl.setInsightsRetro(&fakeRetro{}, true)
		out, err := ctl.cmdInsights(context.Background(), nil)
		if err != nil {
			t.Fatalf("cmdInsights: %v", err)
		}
		if strings.Contains(out.Text, offer) {
			t.Errorf("offer should be suppressed when the session-end layer is on:\n%s", out.Text)
		}
	})

	t.Run("suppressed when no proposer", func(t *testing.T) {
		ctl := newTestController(t)
		out, err := ctl.cmdInsights(context.Background(), nil)
		if err != nil {
			t.Fatalf("cmdInsights: %v", err)
		}
		if strings.Contains(out.Text, offer) {
			t.Errorf("offer should be suppressed with no proposer:\n%s", out.Text)
		}
	})
}

// TestCmdInsightsDescribeMergesGroundedAndCharges asserts the on-demand retro keeps
// only evidence-citing suggestions (labelled model), drops ungrounded ones, and
// charges the spend by recording the usage on the session log (AS-137 AC).
func TestCmdInsightsDescribeMergesGroundedAndCharges(t *testing.T) {
	ctl := newTestController(t)
	repeatedCommandSession(t, ctl, "make test")

	usage := eventlog.NewUsage("insights-model-retro", "anthropic", "claude-opus-4-8", "",
		&schema.Tokens{Input: intp(1000), Output: intp(500)}, nil)
	ctl.setInsightsRetro(&fakeRetro{
		suggestions: []insights.Suggestion{
			{Summary: "grounded tip", Evidence: "command ran 3× at #1"},
			{Summary: "ungrounded vibe", Evidence: "just a feeling"},
		},
		usage: usage,
	}, false)

	before := cost.Summarize(ctl.sess.Log.Events(), ctl.pricing).TotalUSD

	out, err := ctl.cmdInsights(context.Background(), []string{"describe"})
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if !strings.Contains(out.Text, "grounded tip (model)") {
		t.Errorf("grounded model suggestion missing/unlabelled:\n%s", out.Text)
	}
	if strings.Contains(out.Text, "ungrounded vibe") {
		t.Errorf("ungrounded suggestion should be dropped:\n%s", out.Text)
	}
	after := cost.Summarize(ctl.sess.Log.Events(), ctl.pricing).TotalUSD
	if after <= before {
		t.Errorf("retro spend not charged to the log: before=%v after=%v", before, after)
	}
}

// TestCmdInsightsDescribeDegradesOnError asserts a proposer error leaves the
// measured dashboard intact (the retro is enrichment, never a gate) (AS-137).
func TestCmdInsightsDescribeDegradesOnError(t *testing.T) {
	ctl := newTestController(t)
	repeatedCommandSession(t, ctl, "make test")
	ctl.setInsightsRetro(&fakeRetro{err: errors.New("boom")}, false)

	out, err := ctl.cmdInsights(context.Background(), []string{"describe"})
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if !strings.Contains(out.Text, "make test") {
		t.Errorf("measured dashboard lost on retro error:\n%s", out.Text)
	}
	if !strings.Contains(out.Text, "model retro unavailable") {
		t.Errorf("error degrade note missing:\n%s", out.Text)
	}
}

// TestCmdInsightsDescribeSkippedOnBudget asserts the retro is refused — with no
// usage charged — when the session is already at its budget ceiling (AS-137 AC).
func TestCmdInsightsDescribeSkippedOnBudget(t *testing.T) {
	ctl := newTestController(t)
	repeatedCommandSession(t, ctl, "make test")
	// Drive spend onto the log, then set a ceiling at/under it so there is no room.
	mustAppend := eventlog.NewUsage("loop", "anthropic", "claude-opus-4-8", "",
		&schema.Tokens{Input: intp(100000), Output: intp(100000)}, nil)
	if _, err := ctl.sess.Log.Append(mustAppend); err != nil {
		t.Fatalf("append spend: %v", err)
	}
	ctl.setBudgetDefaults(0.0001, 0.8, false)

	retro := &fakeRetro{
		suggestions: []insights.Suggestion{{Summary: "tip", Evidence: "at #1"}},
		usage:       eventlog.NewUsage("insights-model-retro", "anthropic", "claude-opus-4-8", "", &schema.Tokens{Input: intp(1), Output: intp(1)}, nil),
	}
	ctl.setInsightsRetro(retro, false)

	out, err := ctl.cmdInsights(context.Background(), []string{"describe"})
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if !strings.Contains(out.Text, "Budget reached") {
		t.Errorf("expected a budget-skip message:\n%s", out.Text)
	}
	if retro.calls != 0 {
		t.Errorf("retro should not run with no budget room, ran %d times", retro.calls)
	}
}

// TestCmdInsightsDescribeNoProposer asserts /insights describe explains the model
// is unconfigured rather than erroring when no proposer is wired (AS-137).
func TestCmdInsightsDescribeNoProposer(t *testing.T) {
	ctl := newTestController(t)
	out, err := ctl.cmdInsights(context.Background(), []string{"describe"})
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if !strings.Contains(out.Text, "No model is configured") {
		t.Errorf("expected the unconfigured-model message:\n%s", out.Text)
	}
}
