package goal

import (
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/schema"
)

func userText(id, body string) schema.Block {
	return schema.Block{ID: id, Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: body}}
}

func mustAppend(t *testing.T, l *eventlog.Log, b schema.Block) schema.Block {
	t.Helper()
	stored, err := l.Append(b)
	if err != nil {
		t.Fatalf("append %s: %v", b.ID, err)
	}
	return stored
}

func TestSetThenCurrent(t *testing.T) {
	l := eventlog.New()
	mustAppend(t, l, userText("u1", "hi"))
	g := mustAppend(t, l, Set("ship the parser"))

	cur, ok := Current(l.Events())
	if !ok {
		t.Fatal("Current: want a goal, got none")
	}
	if cur.Objective != "ship the parser" {
		t.Errorf("Objective = %q, want %q", cur.Objective, "ship the parser")
	}
	if cur.BlockID != g.ID {
		t.Errorf("BlockID = %q, want %q", cur.BlockID, g.ID)
	}
	if !cur.Active {
		t.Error("current goal should be Active")
	}
	if cur.SetAt.IsZero() {
		t.Error("SetAt should be the goal block's append time, not zero")
	}
}

// AC #1: the goal must be visible to the model — i.e. live in the projection —
// and carry the system role so it reads as a standing instruction.
func TestGoalBlockIsLiveAndModelFacing(t *testing.T) {
	l := eventlog.New()
	mustAppend(t, l, Set("ship it"))

	for _, b := range projection.Project(l.Events(), projection.Options{}).Live() {
		if b.Kind == schema.KindText && b.Text != nil && b.Text.Text == textPrefix+"ship it" {
			if b.Role != schema.RoleSystem {
				t.Errorf("goal role = %q, want %q", b.Role, schema.RoleSystem)
			}
			return
		}
	}
	t.Fatal("goal block not live in projection — the model would not see it")
}

func TestReplaceRetiresPrevious(t *testing.T) {
	l := eventlog.New()
	g1 := mustAppend(t, l, Set("first"))
	mustAppend(t, l, Retire(g1.ID)) // controller retires the active goal before re-setting
	g2 := mustAppend(t, l, Set("second"))

	cur, ok := Current(l.Events())
	if !ok || cur.BlockID != g2.ID || cur.Objective != "second" {
		t.Fatalf("Current = %+v, want the second goal (%s)", cur, g2.ID)
	}
	hist := History(l.Events())
	if len(hist) != 2 {
		t.Fatalf("History len = %d, want 2", len(hist))
	}
	if hist[0].Active {
		t.Error("first goal should be retired (inactive)")
	}
	if !hist[1].Active {
		t.Error("second goal should be active")
	}
}

func TestDoneRetiresGoal(t *testing.T) {
	l := eventlog.New()
	g := mustAppend(t, l, Set("finish"))
	mustAppend(t, l, Retire(g.ID))

	if _, ok := Current(l.Events()); ok {
		t.Error("Current should be empty after the goal is retired")
	}
	hist := History(l.Events())
	if len(hist) != 1 || hist[0].Active {
		t.Fatalf("History = %+v, want one inactive entry", hist)
	}
}

// AC #3: setting a goal mid-session appends and never reorders earlier blocks,
// so the cached prefix ahead of it stays stable.
func TestSetAppendsWithoutReorder(t *testing.T) {
	l := eventlog.New()
	mustAppend(t, l, userText("u1", "one"))
	mustAppend(t, l, userText("u2", "two"))
	before := projection.Project(l.Events(), projection.Options{}).Live()

	mustAppend(t, l, Set("a goal"))
	after := projection.Project(l.Events(), projection.Options{}).Live()

	if len(after) != len(before)+1 {
		t.Fatalf("live len = %d, want %d", len(after), len(before)+1)
	}
	for i := range before {
		if before[i].ID != after[i].ID {
			t.Errorf("block %d reordered: %s -> %s", i, before[i].ID, after[i].ID)
		}
	}
}

func TestRenderShowsCurrentAndHistory(t *testing.T) {
	l := eventlog.New()
	if got := Render(l.Events()); !strings.Contains(got, "No goal") {
		t.Errorf("empty render = %q, want a no-goal message", got)
	}
	mustAppend(t, l, Set("ship the parser"))
	if got := Render(l.Events()); !strings.Contains(got, "ship the parser") {
		t.Errorf("render = %q, want it to mention the goal", got)
	}
}
