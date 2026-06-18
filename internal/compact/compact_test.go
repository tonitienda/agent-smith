package compact_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/compact"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/schema"
)

const model = "claude-opus-4-8" // priced in the embedded table

var base = time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

func text(id string, role schema.Role, chars int) schema.Block {
	return schema.Block{
		ID:   id,
		Kind: schema.KindText,
		Role: role,
		TS:   base,
		Text: &schema.TextBody{Text: strings.Repeat("x", chars)},
	}
}

func project(events []schema.Block) *projection.Projection {
	return projection.Project(events, projection.Options{TargetModel: model})
}

func preview(events []schema.Block) compact.Plan {
	return compact.Preview(project(events), cost.Embedded(), model, base)
}

// liveIDs returns the IDs of the live blocks in append order.
func liveIDs(events []schema.Block) []string {
	var out []string
	for _, b := range project(events).Live() {
		out = append(out, b.ID)
	}
	return out
}

// conversation is a system prefix, two old turns, and a recent turn. The recent
// turn (its user block onward) is kept; only the two old conversation blocks are
// compactable.
func conversation() []schema.Block {
	return []schema.Block{
		text("blk_sys0", schema.RoleSystem, 40),
		text("blk_usr1", schema.RoleUser, 200),
		text("blk_ast1", schema.RoleAssistant, 400),
		text("blk_usr2", schema.RoleUser, 40),
		text("blk_ast2", schema.RoleAssistant, 80),
	}
}

// TestPreviewSelectsOldConversation covers the span rule: the system/memory
// prefix and the most recent turn are kept; the older conversation is compacted.
func TestPreviewSelectsOldConversation(t *testing.T) {
	p := preview(conversation())
	if got := p.SourceIDs; len(got) != 2 || got[0] != "blk_usr1" || got[1] != "blk_ast1" {
		t.Fatalf("SourceIDs = %v, want [blk_usr1 blk_ast1]", got)
	}
	if p.Tokens != 150 { // (200 + 400) chars / 4
		t.Errorf("Tokens = %d, want 150", p.Tokens)
	}
	if !p.Priced || p.CostUSD <= 0 {
		t.Errorf("expected a priced, non-zero cost; got priced=%v cost=%v", p.Priced, p.CostUSD)
	}
}

// TestPreviewKeepsSystemAndRecent confirms a single-turn session has nothing to
// compact: the system prefix and the lone turn are both kept.
func TestPreviewKeepsSystemAndRecent(t *testing.T) {
	events := []schema.Block{
		text("blk_sys0", schema.RoleSystem, 40),
		text("blk_usr1", schema.RoleUser, 40),
		text("blk_ast1", schema.RoleAssistant, 80),
	}
	if p := preview(events); !p.Empty() {
		t.Fatalf("expected nothing to compact, got %v", p.SourceIDs)
	}
}

// TestApplyShrinksWindowAndProvenance covers AC1 (the window shrinks) and AC3
// (the compaction block's provenance lists every source ID).
func TestApplyShrinksWindowAndProvenance(t *testing.T) {
	events := conversation()
	before := project(events).LiveLen()

	plan := preview(events)
	block, ok := compact.Build(plan, "the user asked X; the assistant did Y")
	if !ok {
		t.Fatal("Build returned ok=false for a non-empty plan")
	}
	if block.Kind != schema.KindCompaction {
		t.Errorf("compaction kind = %q, want %q", block.Kind, schema.KindCompaction)
	}
	if block.Provenance == nil || len(block.Provenance.DerivedFrom) != 2 {
		t.Fatalf("provenance DerivedFrom = %+v, want the two source IDs", block.Provenance)
	}
	if block.Provenance.DerivedFrom[0] != "blk_usr1" || block.Provenance.DerivedFrom[1] != "blk_ast1" {
		t.Errorf("DerivedFrom = %v, want [blk_usr1 blk_ast1]", block.Provenance.DerivedFrom)
	}

	events = append(events, block)
	// The two source blocks leave the window; the summary block replaces them.
	got := liveIDs(events)
	for _, id := range []string{"blk_usr1", "blk_ast1"} {
		if contains(got, id) {
			t.Errorf("source %s still live after compaction: %v", id, got)
		}
	}
	if !contains(got, block.ID) {
		t.Errorf("compaction block %s not live: %v", block.ID, got)
	}
	if after := project(events).LiveLen(); after != before-1 {
		// two sources removed, one summary added => net -1.
		t.Errorf("live count = %d, want %d (before %d)", after, before-1, before)
	}
}

// TestUndoRestoresExactProjection covers AC2: undo restores the precise prior
// live set — the guardrail that differentiates us from destructive incumbents.
func TestUndoRestoresExactProjection(t *testing.T) {
	events := conversation()
	want := liveIDs(events)

	plan := preview(events)
	block, _ := compact.Build(plan, "summary text")
	events = append(events, block)

	undo, removed, ok := compact.Undo(events)
	if !ok {
		t.Fatal("Undo returned ok=false after a compaction")
	}
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	events = append(events, undo)

	if got := liveIDs(events); !equal(got, want) {
		t.Errorf("after undo live = %v, want exact prior %v", got, want)
	}
}

// TestUndoNoCompaction reports nothing to undo when none was applied.
func TestUndoNoCompaction(t *testing.T) {
	if _, _, ok := compact.Undo(conversation()); ok {
		t.Error("Undo reported a compaction to undo when none was applied")
	}
}

// TestBuildEmptySummary refuses to build a compaction from an empty summary, so
// a model that returns nothing never silently drops the conversation.
func TestBuildEmptySummary(t *testing.T) {
	plan := preview(conversation())
	if _, ok := compact.Build(plan, "   \n "); ok {
		t.Error("Build returned ok=true for an empty summary")
	}
}

// TestTranscriptRendersKinds renders each content kind into a transcript line.
func TestTranscriptRendersKinds(t *testing.T) {
	sources := []schema.Block{
		text("blk_u", schema.RoleUser, 0),
		{Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: "hello"}},
		{Kind: schema.KindToolCall, Role: schema.RoleAssistant, ToolCall: &schema.ToolCallBody{Name: "grep"}},
		{Kind: schema.KindToolResult, Role: schema.RoleTool, ToolResult: &schema.ToolResultBody{Stdout: "match"}},
		{Kind: schema.KindFileRead, Role: schema.RoleTool, FileRead: &schema.FileReadBody{Path: "main.go"}},
	}
	tr := compact.Transcript(sources)
	for _, want := range []string{"user: hello", "tool call: grep", "tool result: match", "file read: main.go"} {
		if !strings.Contains(tr, want) {
			t.Errorf("transcript missing %q:\n%s", want, tr)
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
