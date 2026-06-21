package insights

import (
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/subagent"
	"github.com/tonitienda/agent-smith/schema"
)

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
