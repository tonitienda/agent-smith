package run

import (
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestEnqueueAndGet(t *testing.T) {
	s := newStore(t)
	rec, err := s.Enqueue(Spec{Prompt: "triage the inbox", BudgetUSD: 0.5, Auto: true})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if rec.Status != StatusQueued {
		t.Errorf("status = %q, want queued", rec.Status)
	}
	if rec.ID == "" || rec.ProjectPath != s.ProjectPath() {
		t.Errorf("record identity not set: %+v", rec)
	}
	got, err := s.Get(rec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Prompt != "triage the inbox" || got.BudgetUSD != 0.5 || !got.Auto {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestEnqueueEmptyPromptRejected(t *testing.T) {
	s := newStore(t)
	if _, err := s.Enqueue(Spec{Prompt: "  "}); err == nil {
		t.Fatal("expected an error enqueuing an empty prompt")
	}
}

func TestListNewestFirst(t *testing.T) {
	s := newStore(t)
	first, _ := s.Enqueue(Spec{Prompt: "a"})
	// Force a later creation timestamp so ordering is deterministic regardless of
	// clock granularity.
	second, _ := s.Enqueue(Spec{Prompt: "b"})
	second.CreatedAt = first.CreatedAt.Add(time.Second)
	if err := s.Save(second); err != nil {
		t.Fatalf("Save: %v", err)
	}
	recs, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("len = %d, want 2", len(recs))
	}
	if recs[0].ID != second.ID {
		t.Errorf("newest first violated: got %s, want %s", recs[0].ID, second.ID)
	}
}

func TestSaveUpdatesStatus(t *testing.T) {
	s := newStore(t)
	rec, _ := s.Enqueue(Spec{Prompt: "x"})
	rec.Status = StatusDone
	rec.CostUSD = 0.12
	rec.SessionID = "sess_1"
	if err := s.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _ := s.Get(rec.ID)
	if got.Status != StatusDone || got.CostUSD != 0.12 || got.SessionID != "sess_1" {
		t.Errorf("update not persisted: %+v", got)
	}
}

func TestListEmptyStore(t *testing.T) {
	s := newStore(t)
	recs, err := s.List()
	if err != nil {
		t.Fatalf("List on empty store: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("len = %d, want 0", len(recs))
	}
}

func TestGetUnsafeID(t *testing.T) {
	s := newStore(t)
	if _, err := s.Get("../escape"); err == nil {
		t.Fatal("expected unsafe id to be rejected")
	}
}

func TestStatusTerminal(t *testing.T) {
	terminal := []Status{StatusDone, StatusFailed, StatusBudget, StatusCanceled, StatusInterrupted}
	for _, s := range terminal {
		if !s.Terminal() {
			t.Errorf("%q should be terminal", s)
		}
	}
	for _, s := range []Status{StatusQueued, StatusRunning} {
		if s.Terminal() {
			t.Errorf("%q should not be terminal", s)
		}
	}
}
