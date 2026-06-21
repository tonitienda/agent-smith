package mode

import (
	"testing"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

// mustAppend appends b to l, failing the test on error, and returns the stored
// block (with Seq/TS assigned).
func mustAppend(t *testing.T, l *eventlog.Log, b schema.Block) schema.Block {
	t.Helper()
	stored, err := l.Append(b)
	if err != nil {
		t.Fatalf("append %s: %v", b.ID, err)
	}
	return stored
}

// enter appends the entry events for the coding mode and returns the instance ID.
func enter(t *testing.T, l *eventlog.Log) string {
	t.Helper()
	var id string
	for _, b := range Enter(Coding, DefaultPhases()) {
		stored := mustAppend(t, l, b)
		if stored.Kind == eventlog.KindModeEnter {
			id = stored.ID
		}
	}
	return id
}

func TestEnterStartsAtFirstPhase(t *testing.T) {
	l := eventlog.New()
	id := enter(t, l)

	cur, ok := Current(l.Events())
	if !ok {
		t.Fatal("Current: want an active mode, got none")
	}
	if cur.InstanceID != id {
		t.Errorf("InstanceID = %q, want %q", cur.InstanceID, id)
	}
	if cur.Mode != Coding {
		t.Errorf("Mode = %q, want %q", cur.Mode, Coding)
	}
	if cur.Phase != DefaultPhases()[0] {
		t.Errorf("Phase = %q, want %q", cur.Phase, DefaultPhases()[0])
	}
	if !cur.Active {
		t.Error("Active = false, want true")
	}
}

func TestNoModeWhenNoneEntered(t *testing.T) {
	l := eventlog.New()
	if _, ok := Current(l.Events()); ok {
		t.Fatal("Current: want no active mode on an empty log")
	}
}

func TestSetPhaseAdvances(t *testing.T) {
	l := eventlog.New()
	id := enter(t, l)
	mustAppend(t, l, SetPhase(id, "plan"))

	cur, _ := Current(l.Events())
	if cur.Phase != "plan" {
		t.Errorf("Phase = %q, want %q", cur.Phase, "plan")
	}
}

func TestPhaseDerivedFromLogAlone(t *testing.T) {
	l := eventlog.New()
	id := enter(t, l)
	mustAppend(t, l, SetPhase(id, "implement"))
	mustAppend(t, l, SetPhase(id, "verify"))

	// "current phase" is the latest phase-change — a pure projection, no stored
	// field (PRD D3 / D-CODE-3).
	if got := currentPhase(l.Events(), 0, id); got != "verify" {
		t.Errorf("currentPhase = %q, want %q", got, "verify")
	}
}

func TestExitDeactivatesButKeepsHistory(t *testing.T) {
	l := eventlog.New()
	id := enter(t, l)
	mustAppend(t, l, SetPhase(id, "plan"))
	mustAppend(t, l, Exit(id))

	if _, ok := Current(l.Events()); ok {
		t.Fatal("Current: want no active mode after exit")
	}
	hist := History(l.Events())
	if len(hist) != 1 {
		t.Fatalf("History len = %d, want 1", len(hist))
	}
	if hist[0].Active {
		t.Error("exited instance should be inactive in history")
	}
	if hist[0].Phase != "plan" {
		t.Errorf("history Phase = %q, want %q (history survives exit)", hist[0].Phase, "plan")
	}
}

func TestReenterAfterExit(t *testing.T) {
	l := eventlog.New()
	first := enter(t, l)
	mustAppend(t, l, Exit(first))
	second := enter(t, l)

	cur, ok := Current(l.Events())
	if !ok {
		t.Fatal("Current: want the re-entered mode active")
	}
	if cur.InstanceID != second {
		t.Errorf("InstanceID = %q, want the second instance %q", cur.InstanceID, second)
	}
	if len(History(l.Events())) != 2 {
		t.Errorf("History len = %d, want 2 instances", len(History(l.Events())))
	}
}

func TestPhaseNavigation(t *testing.T) {
	if got, ok := NextPhase(DefaultPhases(), "think"); !ok || got != "analyse" {
		t.Errorf("NextPhase(think) = %q,%v, want analyse,true", got, ok)
	}
	if _, ok := NextPhase(DefaultPhases(), "reflect"); ok {
		t.Error("NextPhase at last phase should report false")
	}
	if got, ok := PrevPhase(DefaultPhases(), "analyse"); !ok || got != "think" {
		t.Errorf("PrevPhase(analyse) = %q,%v, want think,true", got, ok)
	}
	if _, ok := PrevPhase(DefaultPhases(), "think"); ok {
		t.Error("PrevPhase at first phase should report false")
	}
}

func TestCanonicalPhaseCaseInsensitive(t *testing.T) {
	got, ok := CanonicalPhase(DefaultPhases(), "VERIFY")
	if !ok || got != "verify" {
		t.Errorf("CanonicalPhase(VERIFY) = %q,%v, want verify,true", got, ok)
	}
	if _, ok := CanonicalPhase(DefaultPhases(), "ship"); ok {
		t.Error("CanonicalPhase should reject an unknown phase")
	}
}

func TestTrackerBracketsCurrent(t *testing.T) {
	got := Tracker([]string{"think", "plan"}, "plan")
	want := "think · [plan]"
	if got != want {
		t.Errorf("Tracker = %q, want %q", got, want)
	}
}
