package clean_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/clean"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/schema"
)

const model = "claude-opus-4-8" // priced in the embedded table

var base = time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

func text(id string, role schema.Role, chars, ageMinutes int) schema.Block {
	return schema.Block{
		ID:   id,
		Kind: schema.KindText,
		Role: role,
		TS:   base.Add(-time.Duration(ageMinutes) * time.Minute),
		Text: &schema.TextBody{Text: strings.Repeat("x", chars)},
	}
}

func toolCall(id, useID, name string, argChars, ageMinutes int) schema.Block {
	return schema.Block{
		ID:       id,
		Kind:     schema.KindToolCall,
		Role:     schema.RoleAssistant,
		TS:       base.Add(-time.Duration(ageMinutes) * time.Minute),
		ToolCall: &schema.ToolCallBody{ToolUseID: useID, Name: name, ArgumentsRaw: strings.Repeat("a", argChars)},
	}
}

func toolResult(id, useID string, outChars, ageMinutes int) schema.Block {
	return schema.Block{
		ID:         id,
		Kind:       schema.KindToolResult,
		Role:       schema.RoleTool,
		TS:         base.Add(-time.Duration(ageMinutes) * time.Minute),
		ToolResult: &schema.ToolResultBody{ToolUseID: useID, Stdout: strings.Repeat("o", outChars)},
	}
}

func project(events []schema.Block) *projection.Projection {
	return projection.Project(events, projection.Options{TargetModel: model})
}

func preview(events []schema.Block, selectors ...string) clean.Plan {
	return clean.Preview(project(events), cost.Embedded(), model, base, selectors)
}

// TestPreviewReclaimsSelected covers the core AC: preview reports exactly the
// blocks removed and the tokens/$ reclaimed, before anything is applied.
func TestPreviewReclaimsSelected(t *testing.T) {
	events := []schema.Block{
		text("blk_aaa1", schema.RoleUser, 40, 30),
		text("blk_bbb2", schema.RoleAssistant, 200, 25),
	}
	p := preview(events, "blk_bbb2")
	if len(p.Items) != 1 || p.Items[0].ID != "blk_bbb2" {
		t.Fatalf("items = %+v, want only blk_bbb2", p.Items)
	}
	if p.Tokens != 50 { // 200 chars / 4
		t.Errorf("Tokens = %d, want 50", p.Tokens)
	}
	if !p.Priced || p.CostUSD <= 0 {
		t.Errorf("expected a priced, non-zero cost; got priced=%v cost=%v", p.Priced, p.CostUSD)
	}
}

// TestApplyDropsFromWindowAndKeepsPrefix covers the reclaim AC and the cache
// invariant (AS-011): excluding a mid-window block reclaims its tokens, leaves
// the rest of the thread live, and never reorders the prefix ahead of it.
func TestApplyDropsFromWindowAndKeepsPrefix(t *testing.T) {
	events := []schema.Block{
		text("blk_aaa1", schema.RoleUser, 40, 30),
		text("blk_bbb2", schema.RoleAssistant, 200, 25),
		text("blk_ccc3", schema.RoleUser, 80, 5),
	}
	before := project(events).Live()

	p := preview(events, "blk_bbb2")
	event, ok := clean.Apply(p)
	if !ok {
		t.Fatal("Apply returned ok=false for a non-empty plan")
	}
	after := project(append(events, event))

	live := after.Live()
	if len(live) != 2 {
		t.Fatalf("live count = %d, want 2", len(live))
	}
	for _, b := range live {
		if b.ID == "blk_bbb2" {
			t.Fatal("blk_bbb2 is still live after /clean apply")
		}
	}
	// Prefix ahead of the excluded block is byte-identical (cache invariant).
	if live[0].ID != before[0].ID {
		t.Errorf("prefix changed: live[0]=%s, want %s", live[0].ID, before[0].ID)
	}
	// Reclaimed exactly the plan's tokens.
	if got := cost.EstimateContextTokens(after.Live()); got != cost.EstimateContextTokens(before)-p.Tokens {
		t.Errorf("reclaimed tokens mismatch: after=%d before=%d plan=%d", got, cost.EstimateContextTokens(before), p.Tokens)
	}
}

// TestUndoRestoresExactly covers the no-data-loss AC: after applying and undoing,
// the projection equals the pre-clean live window exactly.
func TestUndoRestoresExactly(t *testing.T) {
	events := []schema.Block{
		text("blk_aaa1", schema.RoleUser, 40, 30),
		text("blk_bbb2", schema.RoleAssistant, 200, 25),
		text("blk_ccc3", schema.RoleUser, 80, 5),
	}
	before := project(events).Live()

	apply, _ := clean.Apply(preview(events, "blk_bbb2"))
	events = append(events, apply)

	undo, removed, ok := clean.Undo(events)
	if !ok {
		t.Fatal("Undo found no removal to reverse")
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	events = append(events, undo)

	after := project(events).Live()
	if len(after) != len(before) {
		t.Fatalf("after undo live=%d, want %d", len(after), len(before))
	}
	for i := range before {
		if after[i].ID != before[i].ID {
			t.Errorf("block %d: after undo %s, want %s", i, after[i].ID, before[i].ID)
		}
	}
}

// TestAtomicToolPair covers the guardrail: selecting a tool call also removes its
// result (and vice versa) so the window is never left with an orphaned half.
func TestAtomicToolPair(t *testing.T) {
	events := []schema.Block{
		text("blk_aaa1", schema.RoleUser, 40, 30),
		toolCall("blk_call1", "use-1", "grep", 40, 20),
		toolResult("blk_res1", "use-1", 120, 19),
	}
	for _, sel := range []string{"blk_call1", "blk_res1"} {
		p := preview(events, sel)
		ids := map[string]bool{}
		for _, it := range p.Items {
			ids[it.ID] = true
		}
		if !ids["blk_call1"] || !ids["blk_res1"] {
			t.Errorf("selecting %s: plan = %v, want both call and result", sel, p.IDs())
		}
	}
}

// TestResolveByPrefix covers handle ergonomics: an unambiguous prefix resolves,
// an ambiguous one is rejected rather than removing the wrong block.
func TestResolveByPrefix(t *testing.T) {
	events := []schema.Block{
		text("blk_abc1", schema.RoleUser, 40, 30),
		text("blk_abd2", schema.RoleAssistant, 80, 25),
	}
	// Unambiguous prefix (with the blk_ stripped).
	if p := preview(events, "abc1"); len(p.Items) != 1 || p.Items[0].ID != "blk_abc1" {
		t.Errorf("prefix abc1 -> %v, want blk_abc1", p.IDs())
	}
	// An exact ID that is also a prefix of a longer ID still wins outright.
	exact := []schema.Block{
		text("blk_abc12", schema.RoleUser, 40, 30),
		text("blk_abc123", schema.RoleAssistant, 80, 25),
	}
	if p := preview(exact, "abc12"); len(p.Items) != 1 || p.Items[0].ID != "blk_abc12" {
		t.Errorf("exact handle abc12 -> %v, want blk_abc12", p.IDs())
	}

	// Ambiguous prefix matches two blocks: rejected, reported unknown.
	p := preview(events, "ab")
	if !p.Empty() {
		t.Errorf("ambiguous prefix should match nothing, got %v", p.IDs())
	}
	if len(p.Unknown) != 1 || p.Unknown[0] != "ab" {
		t.Errorf("Unknown = %v, want [ab]", p.Unknown)
	}
}

// TestRecentWarning covers the soft guardrail: removing a very recent block warns.
func TestRecentWarning(t *testing.T) {
	events := []schema.Block{
		text("blk_old1", schema.RoleUser, 40, 30),
		text("blk_new2", schema.RoleAssistant, 80, 0), // just now
	}
	if p := preview(events, "blk_new2"); len(p.Warnings) == 0 {
		t.Error("expected a recency warning for a just-now block")
	}
	if p := preview(events, "blk_old1"); len(p.Warnings) != 0 {
		t.Errorf("did not expect a warning for an old block, got %v", p.Warnings)
	}
}

// TestUndoIsAStack covers repeated undo: two removals undo in reverse order, and
// undo stops once there is nothing left to reverse.
func TestUndoIsAStack(t *testing.T) {
	events := []schema.Block{
		text("blk_aaa1", schema.RoleUser, 40, 30),
		text("blk_bbb2", schema.RoleAssistant, 80, 25),
		text("blk_ccc3", schema.RoleUser, 120, 5),
	}
	a1, _ := clean.Apply(preview(events, "blk_bbb2"))
	events = append(events, a1)
	a2, _ := clean.Apply(preview(events, "blk_ccc3"))
	events = append(events, a2)

	// First undo reverses the most recent removal (blk_ccc3).
	u1, _, ok := clean.Undo(events)
	if !ok {
		t.Fatal("first undo failed")
	}
	events = append(events, u1)
	if blockLive(project(events), "blk_ccc3") != true || blockLive(project(events), "blk_bbb2") {
		t.Error("after first undo: want ccc3 live, bbb2 still excluded")
	}
	// Second undo reverses the earlier removal (blk_bbb2).
	u2, _, ok := clean.Undo(events)
	if !ok {
		t.Fatal("second undo failed")
	}
	events = append(events, u2)
	if !blockLive(project(events), "blk_bbb2") {
		t.Error("after second undo: want bbb2 live")
	}
	// Nothing left to undo.
	if _, _, ok := clean.Undo(events); ok {
		t.Error("third undo should find no removal to reverse")
	}
}

func blockLive(p *projection.Projection, id string) bool {
	for _, b := range p.Blocks() {
		if b.ID == id {
			return b.Live
		}
	}
	return false
}
