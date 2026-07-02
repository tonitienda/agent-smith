package tidy_test

import (
	"encoding/json"
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

// shellCall builds a shell tool_call block running cmd, tagged with useID so a
// result can pair to it.
func shellCall(id, useID, cmd string, ageMinutes int) schema.Block {
	args, _ := json.Marshal(struct {
		Command string `json:"command"`
	}{Command: cmd})
	return schema.Block{
		ID:       id,
		Kind:     schema.KindToolCall,
		Role:     schema.RoleAssistant,
		TS:       base.Add(-time.Duration(ageMinutes) * time.Minute),
		ToolCall: &schema.ToolCallBody{ToolUseID: useID, Name: "shell", Arguments: args},
	}
}

// shellResult builds the tool_result paired to useID; failed sets a non-zero exit
// code so the detector reads it as a failure.
func shellResult(id, useID string, failed bool, chars, ageMinutes int) schema.Block {
	body := &schema.ToolResultBody{ToolUseID: useID, Stdout: strings.Repeat("z", chars)}
	if failed {
		code := 1
		body.ExitCode = &code
		body.IsError = true
	}
	return schema.Block{
		ID:         id,
		Kind:       schema.KindToolResult,
		Role:       schema.RoleTool,
		TS:         base.Add(-time.Duration(ageMinutes) * time.Minute),
		ToolResult: body,
	}
}

// grepCall builds a grep tool_call searching for pattern.
func grepCall(id, pattern string, ageMinutes int) schema.Block {
	args, _ := json.Marshal(struct {
		Pattern string `json:"pattern"`
	}{Pattern: pattern})
	return schema.Block{
		ID:       id,
		Kind:     schema.KindToolCall,
		Role:     schema.RoleAssistant,
		TS:       base.Add(-time.Duration(ageMinutes) * time.Minute),
		ToolCall: &schema.ToolCallBody{ToolUseID: "g-" + id, Name: "grep", Arguments: args},
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

// TestRepeatedFailureCollapses: a shell command that fails more than once has its
// earlier identical failures collapsed, keeping the latest failing attempt.
func TestRepeatedFailureCollapses(t *testing.T) {
	events := []schema.Block{
		shellCall("c1", "u1", "go test ./...", 10),
		shellResult("r1", "u1", true, 300, 10),
		shellCall("c2", "u2", "go test ./...", 5),
		shellResult("r2", "u2", true, 300, 5),
		shellCall("c3", "u3", "go test ./...", 1),
		shellResult("r3", "u3", true, 300, 1),
	}
	plan := preview(events)
	if len(plan.DeadEnds) != 1 {
		t.Fatalf("dead ends = %d, want 1", len(plan.DeadEnds))
	}
	de := plan.DeadEnds[0]
	if de.Kind != tidy.KindFailedCommand {
		t.Errorf("kind = %q, want %q", de.Kind, tidy.KindFailedCommand)
	}
	// The two earlier attempts (call+result each) drop; the latest attempt stays.
	dropped := map[string]bool{}
	for _, id := range plan.IDs() {
		dropped[id] = true
	}
	for _, id := range []string{"c1", "r1", "c2", "r2"} {
		if !dropped[id] {
			t.Errorf("expected %s dropped; drops=%v", id, plan.IDs())
		}
	}
	for _, id := range []string{"c3", "r3"} {
		if dropped[id] {
			t.Errorf("latest failing attempt %s should be kept; drops=%v", id, plan.IDs())
		}
	}
}

// TestSingleFailureNotCollapsed: one failed command is a fact the thread may act
// on, not a dead end.
func TestSingleFailureNotCollapsed(t *testing.T) {
	events := []schema.Block{
		shellCall("c1", "u1", "go build ./...", 5),
		shellResult("r1", "u1", true, 300, 5),
	}
	if plan := preview(events); !plan.Empty() {
		t.Fatalf("plan not empty for a single failure: %+v", plan)
	}
}

// TestAbandonedReadCollapses: a file read the thread moved on from — a later
// search still hunts it by name and the exact path is never referenced again —
// is surfaced as a dead end (once past the recency window).
func TestAbandonedReadCollapses(t *testing.T) {
	events := []schema.Block{
		fileRead("r", "scratch/tmp.go", 400, 10),
		grepCall("g", "tmp.go", 5), // still searching for it after reading it
		text("u", schema.RoleUser, 40, 4),
	}
	plan := preview(events)
	if len(plan.DeadEnds) != 1 {
		t.Fatalf("dead ends = %d, want 1", len(plan.DeadEnds))
	}
	if plan.DeadEnds[0].Kind != tidy.KindAbandonedRead {
		t.Errorf("kind = %q, want %q", plan.DeadEnds[0].Kind, tidy.KindAbandonedRead)
	}
	if got := plan.IDs(); len(got) != 1 || got[0] != "r" {
		t.Errorf("drops = %v, want [r]", got)
	}
}

// TestReferencedReadNotAbandoned: even with a later search naming the file, a read
// whose exact path a later block still uses is in use and must not be collapsed.
func TestReferencedReadNotAbandoned(t *testing.T) {
	events := []schema.Block{
		fileRead("r", "pkg/thing.go", 400, 10),
		grepCall("g", "thing.go", 6),
		shellCall("c1", "u1", "go vet ./pkg/thing.go", 5),
		shellResult("r1", "u1", false, 20, 5),
	}
	if plan := preview(events); !plan.Empty() {
		t.Fatalf("plan not empty; a referenced read was collapsed: %+v", plan)
	}
}

// TestReadNeverSearchedNotAbandoned: an ordinary read the thread simply keeps in
// context (no later search for it) is not a dead end — the precision bar.
func TestReadNeverSearchedNotAbandoned(t *testing.T) {
	events := []schema.Block{
		fileRead("r", "keep.go", 400, 10),
		text("u", schema.RoleUser, 40, 5),
	}
	if plan := preview(events); !plan.Empty() {
		t.Fatalf("plan not empty; a plain kept read was collapsed: %+v", plan)
	}
}

// TestRecentReadNotAbandoned: a just-read file is left alone even if unreferenced,
// mirroring the dedup recency caution.
func TestRecentReadNotAbandoned(t *testing.T) {
	events := []schema.Block{
		fileRead("r", "fresh.go", 400, 0), // within the recent window
	}
	if plan := preview(events); !plan.Empty() {
		t.Fatalf("plan not empty; a very recent read was collapsed: %+v", plan)
	}
}

// TestDeadEndApplyUndoRoundTrips: dead ends ride the same exclusion event as
// dedup, so a single --apply drops them and --undo restores them.
func TestDeadEndApplyUndoRoundTrips(t *testing.T) {
	log := eventlog.New()
	for _, b := range []schema.Block{
		shellCall("c1", "u1", "make lint", 10),
		shellResult("r1", "u1", true, 300, 10),
		shellCall("c2", "u2", "make lint", 1),
		shellResult("r2", "u2", true, 300, 1),
	} {
		if _, err := log.Append(b); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	plan := tidy.Preview(projection.Project(log.Events(), projection.Options{TargetModel: model}), cost.Embedded(), model, base)
	event, ok := tidy.Apply(plan)
	if !ok {
		t.Fatal("Apply returned false on a dead-end plan")
	}
	if _, err := log.Append(event); err != nil {
		t.Fatalf("append exclusion: %v", err)
	}
	if live := liveIDs(log.Events()); live["c1"] || live["r1"] {
		t.Errorf("earlier failing attempt still live after collapse; live=%v", live)
	}
	undo, _, ok := tidy.Undo(log.Events())
	if !ok {
		t.Fatal("Undo returned false; expected a collapse to undo")
	}
	if _, err := log.Append(undo); err != nil {
		t.Fatalf("append undo: %v", err)
	}
	if live := liveIDs(log.Events()); !live["c1"] || !live["r1"] {
		t.Errorf("collapse not restored after undo; live=%v", live)
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
