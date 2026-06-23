package run

import (
	"sync"
	"sync/atomic"
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

// TestClaimMutualExclusion (AS-132 AC2): many workers racing for one queued run —
// exactly one wins; the rest get claimed=false with no error.
func TestClaimMutualExclusion(t *testing.T) {
	s := newStore(t)
	rec, _ := s.Enqueue(Spec{Prompt: "only one of us runs this"})

	const workers = 16
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, ok, err := s.Claim(rec.ID, fmtID(i))
			if err != nil {
				t.Errorf("Claim: %v", err)
				return
			}
			if ok {
				wins.Add(1)
				if got.Status != StatusRunning || got.HeartbeatAt == nil {
					t.Errorf("claimed record not running/heartbeated: %+v", got)
				}
			}
		}()
	}
	wg.Wait()
	if got := wins.Load(); got != 1 {
		t.Fatalf("claims won = %d, want exactly 1", got)
	}
}

// TestReclaimStaleOnly (AS-132 AC2): Reclaim flips a run whose heartbeat is missing
// or stale to interrupted, but leaves a freshly-heartbeated (live) peer alone.
func TestReclaimStaleOnly(t *testing.T) {
	s := newStore(t)
	now := time.Now().UTC()

	live, _, _ := s.Claim(mustEnqueue(t, s, "live"), "worker-live")
	stale, _, _ := s.Claim(mustEnqueue(t, s, "stale"), "worker-dead")
	old := now.Add(-time.Hour)
	stale.HeartbeatAt = &old
	if err := s.Save(stale); err != nil {
		t.Fatalf("Save: %v", err)
	}
	nilHB, _, _ := s.Claim(mustEnqueue(t, s, "nil-hb"), "worker-dead2")
	nilHB.HeartbeatAt = nil
	if err := s.Save(nilHB); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reclaimed, err := s.Reclaim(30*time.Second, now)
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if len(reclaimed) != 2 {
		t.Fatalf("reclaimed %d, want 2 (stale + nil-heartbeat)", len(reclaimed))
	}
	if got, _ := s.Get(live.ID); got.Status != StatusRunning {
		t.Errorf("live worker's run = %q, want still running", got.Status)
	}
	for _, id := range []string{stale.ID, nilHB.ID} {
		if got, _ := s.Get(id); got.Status != StatusInterrupted {
			t.Errorf("stale run %s = %q, want interrupted", id, got.Status)
		}
	}
}

// TestClaimSkipsNonQueued (AS-132): a terminal run cannot be claimed even if its
// lease was somehow released.
func TestClaimSkipsNonQueued(t *testing.T) {
	s := newStore(t)
	rec, _ := s.Enqueue(Spec{Prompt: "done already"})
	rec.Status = StatusDone
	if err := s.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, ok, err := s.Claim(rec.ID, "w"); err != nil || ok {
		t.Fatalf("Claim of a done run: ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}

// TestReleaseAllowsReclaim (AS-132): after Release a previously-leased run can be
// claimed again (the resume path).
func TestReleaseAllowsReclaim(t *testing.T) {
	s := newStore(t)
	rec, _ := s.Enqueue(Spec{Prompt: "retry me"})
	claimed, ok, err := s.Claim(rec.ID, "w1")
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	// Re-queue + release, as `runs resume` does.
	claimed.Status = StatusQueued
	claimed.WorkerID = ""
	claimed.HeartbeatAt = nil
	if err := s.Save(claimed); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Release(claimed.ID); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, ok, err := s.Claim(rec.ID, "w2"); err != nil || !ok {
		t.Fatalf("re-claim after release: ok=%v err=%v, want ok=true", ok, err)
	}
}

func mustEnqueue(t *testing.T, s *Store, prompt string) string {
	t.Helper()
	rec, err := s.Enqueue(Spec{Prompt: prompt})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	return rec.ID
}

func fmtID(i int) string { return "worker-" + string(rune('a'+i)) }

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
