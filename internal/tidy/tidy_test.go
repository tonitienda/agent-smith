package tidy_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/internal/tidy"
	"github.com/tonitienda/agent-smith/schema"
)

const model = "claude-opus-4-8" // priced in the embedded table

var base = time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

// fileRead builds a file_read block for path with content sized to chars,
// appended ageMinutes before the reference now (base).
func fileRead(id, path string, chars, ageMinutes int) schema.Block {
	return schema.Block{
		ID:       id,
		Kind:     schema.KindFileRead,
		Role:     schema.RoleTool,
		TS:       base.Add(-time.Duration(ageMinutes) * time.Minute),
		FileRead: &schema.FileReadBody{Path: path, Content: strings.Repeat("y", chars)},
	}
}

func text(id string, role schema.Role, chars, ageMinutes int) schema.Block {
	return schema.Block{
		ID:   id,
		Kind: schema.KindText,
		Role: role,
		TS:   base.Add(-time.Duration(ageMinutes) * time.Minute),
		Text: &schema.TextBody{Text: strings.Repeat("x", chars)},
	}
}

func preview(events []schema.Block) tidy.Plan {
	proj := projection.Project(events, projection.Options{TargetModel: model})
	return tidy.Preview(proj, cost.Embedded(), model, base)
}

// TestKeepsLatestDropsOlder is the core dedup AC: a file read twice keeps the
// latest read and drops the earlier one.
func TestKeepsLatestDropsOlder(t *testing.T) {
	events := []schema.Block{
		fileRead("old", "main.go", 400, 10),
		text("u", schema.RoleUser, 40, 5),
		fileRead("new", "main.go", 400, 1),
	}
	plan := preview(events)
	if plan.Empty() {
		t.Fatal("plan is empty; expected a dedup")
	}
	if len(plan.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(plan.Groups))
	}
	g := plan.Groups[0]
	if g.Path != "main.go" {
		t.Errorf("path = %q, want main.go", g.Path)
	}
	if g.Keep.ID != "new" {
		t.Errorf("kept %q, want the latest read \"new\"", g.Keep.ID)
	}
	if got := plan.IDs(); len(got) != 1 || got[0] != "old" {
		t.Errorf("dropped = %v, want [old]", got)
	}
	if plan.Tokens <= 0 || plan.Tokens != g.Tokens {
		t.Errorf("plan.Tokens = %d, group.Tokens = %d; want equal and positive", plan.Tokens, g.Tokens)
	}
}

// TestFidelityDiffInventory checks the before/after inventory the §9 fidelity
// diff reports: after-tokens drop by exactly the reclaim, and after-segments by
// the dropped count.
func TestFidelityDiffInventory(t *testing.T) {
	events := []schema.Block{
		fileRead("a1", "a.go", 400, 10),
		fileRead("a2", "a.go", 400, 2),
		fileRead("b1", "b.go", 200, 9),
		fileRead("b2", "b.go", 200, 1),
	}
	plan := preview(events)
	if plan.BeforeSegments != 4 {
		t.Errorf("before segments = %d, want 4", plan.BeforeSegments)
	}
	if plan.AfterSegments != plan.BeforeSegments-plan.DroppedCount() {
		t.Errorf("after segments = %d, want %d", plan.AfterSegments, plan.BeforeSegments-plan.DroppedCount())
	}
	if plan.AfterTokens != plan.BeforeTokens-plan.Tokens {
		t.Errorf("after tokens = %d, want before %d − reclaim %d", plan.AfterTokens, plan.BeforeTokens, plan.Tokens)
	}
	if plan.DroppedCount() != 2 {
		t.Errorf("dropped = %d, want 2 (one per duplicated file)", plan.DroppedCount())
	}
	// Largest reclaim first.
	if len(plan.Groups) == 2 && plan.Groups[0].Tokens < plan.Groups[1].Tokens {
		t.Errorf("groups not ordered largest-reclaim-first: %d then %d", plan.Groups[0].Tokens, plan.Groups[1].Tokens)
	}
}

// TestNoDuplicatesIsEmpty: a window with no repeated read has nothing to tidy.
func TestNoDuplicatesIsEmpty(t *testing.T) {
	events := []schema.Block{
		fileRead("a", "a.go", 400, 5),
		fileRead("b", "b.go", 400, 4),
		text("u", schema.RoleUser, 40, 1),
	}
	plan := preview(events)
	if !plan.Empty() {
		t.Fatalf("plan not empty: %+v", plan)
	}
	if got := tidy.RenderPreview(plan); !strings.Contains(got, "Nothing to tidy") {
		t.Errorf("render = %q, want a 'Nothing to tidy' message", got)
	}
}

// TestApplyThenUndoRoundTrips: applying the dedup excludes the older reads, and
// undo restores them exactly — the reversibility AC (D3).
func TestApplyThenUndoRoundTrips(t *testing.T) {
	log := eventlog.New()
	for _, b := range []schema.Block{
		fileRead("old", "main.go", 400, 10),
		fileRead("new", "main.go", 400, 1),
	} {
		if _, err := log.Append(b); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	plan := tidy.Preview(projection.Project(log.Events(), projection.Options{TargetModel: model}), cost.Embedded(), model, base)
	event, ok := tidy.Apply(plan)
	if !ok {
		t.Fatal("Apply returned false on a non-empty plan")
	}
	if _, err := log.Append(event); err != nil {
		t.Fatalf("append exclusion: %v", err)
	}

	// After apply: the old read is excluded, the new one stays live.
	if live := liveIDs(log.Events()); live["old"] {
		t.Errorf("old read still live after dedup; live=%v", live)
	} else if !live["new"] {
		t.Errorf("new read should stay live after dedup; live=%v", live)
	}

	// A second preview finds nothing more to do.
	if p2 := tidy.Preview(projection.Project(log.Events(), projection.Options{TargetModel: model}), cost.Embedded(), model, base); !p2.Empty() {
		t.Errorf("re-preview after apply should be empty, got %+v", p2)
	}

	// Undo restores the old read.
	undo, removed, ok := tidy.Undo(log.Events())
	if !ok {
		t.Fatal("Undo returned false; expected a dedup to undo")
	}
	if removed != 1 {
		t.Errorf("undo removed = %d, want 1", removed)
	}
	if _, err := log.Append(undo); err != nil {
		t.Fatalf("append undo: %v", err)
	}
	if live := liveIDs(log.Events()); !live["old"] {
		t.Errorf("old read not restored after undo; live=%v", live)
	}
}

// TestUndoNothingToUndo: undo on a log with no tidy dedup reports not-ok.
func TestUndoNothingToUndo(t *testing.T) {
	log := eventlog.New()
	if _, err := log.Append(fileRead("a", "a.go", 100, 1)); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, _, ok := tidy.Undo(log.Events()); ok {
		t.Error("Undo returned ok on a log with no tidy dedup")
	}
}

// TestRecentDropWarns: a dropped older read that is very recent raises a
// live-fact warning rather than being removed silently.
func TestRecentDropWarns(t *testing.T) {
	events := []schema.Block{
		fileRead("old1", "main.go", 400, 1), // two older reads, both within the recent window
		fileRead("old2", "main.go", 400, 1),
		fileRead("new", "main.go", 400, 0),
	}
	plan := preview(events)
	// Exactly one warning per path, however many dropped reads are fresh.
	if len(plan.Warnings) != 1 {
		t.Fatalf("warnings = %d (%v), want exactly 1 per path", len(plan.Warnings), plan.Warnings)
	}
	if !strings.Contains(plan.Warnings[0], "main.go") {
		t.Errorf("warning = %q, want it to name the file", plan.Warnings[0])
	}
}

// liveIDs reports which block IDs are live in the projected window.
func liveIDs(events []schema.Block) map[string]bool {
	out := map[string]bool{}
	for _, b := range projection.Project(events, projection.Options{TargetModel: model}).Blocks() {
		if b.Live {
			out[b.ID] = true
		}
	}
	return out
}
