package insights

import (
	"context"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/subagent"
	"github.com/tonitienda/agent-smith/schema"
)

// fakeProposer is a scripted insights.Proposer for the writer tests: it returns
// the suggestions and spend it is configured with, regardless of the report.
type fakeProposer struct {
	suggestions []Suggestion
	spent       float64
	err         error
	called      bool
}

func (f *fakeProposer) Propose(context.Context, Report) ([]Suggestion, float64, error) {
	f.called = true
	return f.suggestions, f.spent, f.err
}

// TestWriterManifest pins the insights-writer's lifecycle posture: a passive,
// session-end analyzer that defaults on at zero model cost (Appendix C.3).
func TestWriterManifest(t *testing.T) {
	m := New().Manifest()
	if m.Name != Name || m.Schedule != subagent.AtSessionEnd || m.Scope != subagent.SessionScope {
		t.Fatalf("manifest = %+v", m)
	}
	if !m.EnabledByDefault || m.ModelTier != "" || m.BudgetUSD != 0 {
		t.Fatalf("want enabled, zero-cost manifest, got %+v", m)
	}
}

// TestWriterTeardownEmitsFindings asserts the writer turns a repeated-command
// session into a propose-only finding carrying the memory edit — the data the
// cross-session rollup (AS-050/AS-057) reads.
func TestWriterTeardownEmitsFindings(t *testing.T) {
	slice := []schema.Block{
		call(1, "c1", "make lint"), result(2, "c1", false, ""),
		call(3, "c2", "make lint"), result(4, "c2", false, ""),
		call(5, "c3", "make lint"), result(6, "c3", false, ""),
	}
	res := New().Teardown(subagent.Scope{Kind: subagent.SessionScope}, slice)
	if res.SpentUSD != 0 {
		t.Errorf("SpentUSD = %v, want 0 (no model calls)", res.SpentUSD)
	}
	var withEdit *subagent.Finding
	for i := range res.Findings {
		if res.Findings[i].Kind != FindingKind {
			t.Errorf("finding kind = %q, want %q", res.Findings[i].Kind, FindingKind)
		}
		if res.Findings[i].Proposal != nil {
			withEdit = &res.Findings[i]
		}
	}
	if withEdit == nil {
		t.Fatalf("want a finding with a propose-only edit, got %+v", res.Findings)
	}
	if !strings.Contains(withEdit.Proposal.Description, "make lint") {
		t.Errorf("proposal = %q, want it to mention 'make lint'", withEdit.Proposal.Description)
	}
}

// TestWriterModelManifest asserts the model-layer writer declares the cheap tier
// and its per-session budget cap, so the Runner enforces the cap (the §7.19 AC),
// while a nil proposer/empty tier falls back to the measured-first manifest.
func TestWriterModelManifest(t *testing.T) {
	m := NewWithModel(&fakeProposer{}, "cheap", 0.05).Manifest()
	if m.ModelTier != "cheap" || m.BudgetUSD != 0.05 {
		t.Fatalf("model manifest = %+v, want cheap tier + 0.05 budget", m)
	}
	if got := NewWithModel(nil, "cheap", 0.05).Manifest(); got.ModelTier != "" || got.BudgetUSD != 0 {
		t.Errorf("nil proposer should fall back to measured-first, got %+v", got)
	}
	if got := NewWithModel(&fakeProposer{}, "", 0.05).Manifest(); got.ModelTier != "" {
		t.Errorf("empty tier should fall back to measured-first, got %+v", got)
	}
}

// TestWriterModelLayerAppendsGrounded asserts the model layer's suggestions are
// appended as findings and the spend is charged — but only when grounded: a
// suggestion that cites no measured #seq anchor is dropped (§9), never recorded.
func TestWriterModelLayerAppendsGrounded(t *testing.T) {
	slice := []schema.Block{
		call(1, "c1", "make lint"), result(2, "c1", false, ""),
		call(3, "c2", "make lint"), result(4, "c2", false, ""),
		call(5, "c3", "make lint"), result(6, "c3", false, ""),
	}
	fp := &fakeProposer{
		spent: 0.002,
		suggestions: []Suggestion{
			{Summary: "Scope the grep tool", Evidence: "output ~5k tokens at #4"}, // grounded
			{Summary: "Generally be tidier", Evidence: "good practice"},           // ungrounded → dropped
		},
	}
	res := NewWithModel(fp, "cheap", 0.05).Teardown(subagent.Scope{Kind: subagent.SessionScope}, slice)
	if !fp.called {
		t.Fatal("proposer was not called")
	}
	if res.SpentUSD != 0.002 {
		t.Errorf("SpentUSD = %v, want 0.002 (the model call's charge)", res.SpentUSD)
	}
	var model, dropped int
	for _, f := range res.Findings {
		if strings.Contains(f.Summary, "Scope the grep tool") {
			model++
			if !strings.Contains(f.Summary, "(model)") {
				t.Errorf("model finding %q should be labelled (model)", f.Summary)
			}
		}
		if strings.Contains(f.Summary, "Generally be tidier") {
			dropped++
		}
	}
	if model != 1 {
		t.Errorf("want the grounded model suggestion recorded once, got %d", model)
	}
	if dropped != 0 {
		t.Errorf("ungrounded model suggestion must be dropped, got %d", dropped)
	}
}

// TestWriterModelLayerErrorDegrades asserts a proposer error leaves the measured
// findings intact and charges nothing — the dashboard already rendered for free.
func TestWriterModelLayerErrorDegrades(t *testing.T) {
	slice := []schema.Block{
		call(1, "c1", "make lint"), result(2, "c1", false, ""),
		call(3, "c2", "make lint"), result(4, "c2", false, ""),
		call(5, "c3", "make lint"), result(6, "c3", false, ""),
	}
	fp := &fakeProposer{err: context.DeadlineExceeded, spent: 0.5}
	res := NewWithModel(fp, "cheap", 0.05).Teardown(subagent.Scope{Kind: subagent.SessionScope}, slice)
	if res.SpentUSD != 0 {
		t.Errorf("SpentUSD = %v, want 0 on proposer error", res.SpentUSD)
	}
	if len(res.Findings) == 0 {
		t.Error("measured findings should survive a proposer error")
	}
}

// TestCitesMeasuredEvidence pins the §9 grounding gate: a #<seq> anchor counts,
// a bare '#' or prose does not.
func TestCitesMeasuredEvidence(t *testing.T) {
	cases := map[string]bool{
		"output ~5k tokens at #4": true,
		"ran 3× at #1, #3, #5":    true,
		"good practice, be tidy":  false,
		"see issue # for details": false,
		"":                        false,
	}
	for ev, want := range cases {
		if got := citesMeasuredEvidence(Suggestion{Evidence: ev}); got != want {
			t.Errorf("citesMeasuredEvidence(%q) = %v, want %v", ev, got, want)
		}
	}
}
