package rewind_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/internal/rewind"
	"github.com/tonitienda/agent-smith/schema"
)

const model = "claude-opus-4-8" // priced in the embedded table

var base = time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

func userMsg(id, body string) schema.Block {
	return schema.Block{ID: id, Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: body}}
}

func asstMsg(id, body string) schema.Block {
	return schema.Block{ID: id, Kind: schema.KindText, Role: schema.RoleAssistant, Text: &schema.TextBody{Text: body}}
}

func writeCall(id, path string) schema.Block {
	args, _ := json.Marshal(map[string]string{"path": path, "content": "x"})
	return schema.Block{
		ID: id, Kind: schema.KindToolCall, Role: schema.RoleAssistant,
		ToolCall: &schema.ToolCallBody{ToolUseID: id, Name: "write", Arguments: args},
	}
}

// fill builds a three-turn conversation on a fresh in-memory log and returns the
// stored events.
func fill(t *testing.T) (*eventlog.Log, []schema.Block) {
	t.Helper()
	l := eventlog.New()
	for _, b := range []schema.Block{
		userMsg("blk_u1", "first question"),
		asstMsg("blk_a1", "first answer"),
		userMsg("blk_u2", "second question"),
		asstMsg("blk_a2", "second answer"),
		userMsg("blk_u3", "third question"),
		asstMsg("blk_a3", "third answer"),
	} {
		if _, err := l.Append(b); err != nil {
			t.Fatalf("append %s: %v", b.ID, err)
		}
	}
	return l, l.Events()
}

func liveIDs(p *projection.Projection) []string {
	var ids []string
	for _, b := range p.Live() {
		ids = append(ids, b.ID)
	}
	return ids
}

func eq(a, b []string) bool {
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

// TestRewindMatchesPointInTimeProjection is the headline AC: rewinding to turn N
// yields a projection identical to the historical projection at turn N.
func TestRewindMatchesPointInTimeProjection(t *testing.T) {
	l, events := fill(t)

	cps := rewind.Checkpoints(events)
	if len(cps) != 3 {
		t.Fatalf("checkpoints = %d, want 3", len(cps))
	}
	// Rewind to turn 2 (its leading user message blk_u2).
	target := cps[1]
	if target.Turn != 2 || target.Anchor != "blk_u2" {
		t.Fatalf("checkpoint[1] = %+v, want turn 2 anchor blk_u2", target)
	}

	plan := rewind.Preview(events, cost.Embedded(), model, base, target)
	event, ok := rewind.Apply(plan)
	if !ok {
		t.Fatal("Apply returned ok=false")
	}
	if _, err := l.Append(event); err != nil {
		t.Fatalf("append rewind: %v", err)
	}

	got := liveIDs(projection.Project(l.Events(), projection.Options{TargetModel: model}))
	want := liveIDs(projection.ProjectAt(events, target.Index, projection.Options{TargetModel: model}))
	if !eq(got, want) {
		t.Fatalf("after rewind live = %v, want point-in-time %v", got, want)
	}
	// Concretely: only turn 1 survives.
	if !eq(got, []string{"blk_u1", "blk_a1"}) {
		t.Fatalf("after rewind live = %v, want [blk_u1 blk_a1]", got)
	}
}

// TestRewindIsReversible: an undo restores the exact pre-rewind projection, and
// no events are deleted (§6 no-data-loss guardrail).
func TestRewindIsReversible(t *testing.T) {
	l, events := fill(t)
	before := liveIDs(projection.Project(events, projection.Options{TargetModel: model}))

	target, ok := rewind.Find(events, "blk_u2")
	if !ok {
		t.Fatal("Find blk_u2 failed")
	}
	plan := rewind.Preview(events, cost.Embedded(), model, base, target)
	event, _ := rewind.Apply(plan)
	mustAppend(t, l, event)

	undo, ok := rewind.Undo(l.Events())
	if !ok {
		t.Fatal("Undo returned ok=false")
	}
	mustAppend(t, l, undo)

	after := liveIDs(projection.Project(l.Events(), projection.Options{TargetModel: model}))
	if !eq(after, before) {
		t.Fatalf("after undo live = %v, want pre-rewind %v", after, before)
	}
}

// TestModifiedFilesWarning: the preview lists files write/edit calls touched
// after the checkpoint, since a conversation rewind does not revert them.
func TestModifiedFilesWarning(t *testing.T) {
	l := eventlog.New()
	for _, b := range []schema.Block{
		userMsg("blk_u1", "edit the parser"),
		writeCall("blk_w1", "internal/parser/parse.go"),
		asstMsg("blk_a1", "done"),
		userMsg("blk_u2", "now the tests"),
		writeCall("blk_w2", "internal/parser/parse_test.go"),
	} {
		mustAppend(t, l, b)
	}
	events := l.Events()

	target, _ := rewind.Find(events, "blk_u2")
	plan := rewind.Preview(events, cost.Embedded(), model, base, target)
	if len(plan.Files) != 1 || plan.Files[0] != "internal/parser/parse_test.go" {
		t.Fatalf("files = %v, want [internal/parser/parse_test.go]", plan.Files)
	}

	// Rewinding to turn 1 should warn about both files.
	t1, _ := rewind.Find(events, "blk_u1")
	plan1 := rewind.Preview(events, cost.Embedded(), model, base, t1)
	if len(plan1.Files) != 2 {
		t.Fatalf("files = %v, want both files", plan1.Files)
	}
}

// TestManualMarkCheckpoint: a /rewind --mark records a checkpoint that the
// picker offers and that rewinds to the state as of the mark.
func TestManualMarkCheckpoint(t *testing.T) {
	l, _ := fill(t)
	mustAppend(t, l, rewind.Mark("before turn 3 follow-up"))
	// Continue the conversation past the mark.
	mustAppend(t, l, userMsg("blk_u4", "fourth question"))
	mustAppend(t, l, asstMsg("blk_a4", "fourth answer"))
	events := l.Events()

	cps := rewind.Checkpoints(events)
	var mark rewind.Checkpoint
	found := false
	for _, c := range cps {
		if c.Manual {
			mark, found = c, true
		}
	}
	if !found {
		t.Fatal("manual mark not in checkpoints")
	}
	if mark.Label != "before turn 3 follow-up" {
		t.Fatalf("mark label = %q", mark.Label)
	}

	plan := rewind.Preview(events, cost.Embedded(), model, base, mark)
	event, ok := rewind.Apply(plan)
	if !ok {
		t.Fatal("Apply ok=false")
	}
	mustAppend(t, l, event)

	got := liveIDs(projection.Project(l.Events(), projection.Options{TargetModel: model}))
	// The mark sits after turn 3, so turns 1–3 survive; turn 4 is dropped.
	want := []string{"blk_u1", "blk_a1", "blk_u2", "blk_a2", "blk_u3", "blk_a3"}
	if !eq(got, want) {
		t.Fatalf("after mark rewind live = %v, want %v", got, want)
	}
}

// TestEmptyAndUnknown: nothing to rewind at the latest point, and an unknown
// handle does not resolve.
func TestEmptyAndUnknown(t *testing.T) {
	_, events := fill(t)
	if _, ok := rewind.Find(events, "blk_nope"); ok {
		t.Fatal("Find matched an unknown handle")
	}
	// A checkpoint at the very end of the log drops nothing.
	end := rewind.Checkpoint{Index: len(events), Anchor: "blk_end"}
	plan := rewind.Preview(events, cost.Embedded(), model, base, end)
	if !plan.Empty() {
		t.Fatalf("plan at end of log should be empty, got %d drops", len(plan.DropIDs))
	}
	if _, ok := rewind.Apply(plan); ok {
		t.Fatal("Apply on empty plan returned ok=true")
	}
}

func mustAppend(t *testing.T, l *eventlog.Log, b schema.Block) {
	t.Helper()
	if _, err := l.Append(b); err != nil {
		t.Fatalf("append %s: %v", b.ID, err)
	}
}
