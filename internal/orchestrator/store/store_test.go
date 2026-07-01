package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedJob(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.UpsertJob(Job{ID: id, File: id + ".yaml", Version: 1, Owner: "me", Repository: "o/r", LoadedAt: time.Now()},
		[]JobTrigger{{Kind: "manual"}}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}
}

func TestEnqueueClaimFinish(t *testing.T) {
	s := openTest(t)
	seedJob(t, s, "job1")
	now := time.Unix(1000, 0)

	r, err := s.Enqueue(NewRun{JobID: "job1", TriggerKind: "manual", ConcurrencyKey: "k", ConcurrencyLimit: 1, MaxAttempts: 2}, now)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if r.Status != StatusQueued {
		t.Fatalf("status = %q, want queued", r.Status)
	}

	claimed, ok, err := s.ClaimNext("w1", now)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if claimed.ID != r.ID || claimed.Status != StatusRunning || claimed.Attempt != 1 {
		t.Fatalf("claimed = %+v", claimed)
	}

	// Second claim finds nothing (the only run is running).
	if _, ok, _ := s.ClaimNext("w2", now); ok {
		t.Fatal("second claim should find nothing")
	}

	if err := s.Finish(r.ID, Outcome{Status: StatusSucceeded, SessionID: "sess1", CostUSD: 1.5}, now); err != nil {
		t.Fatalf("finish: %v", err)
	}
	got, err := s.Run(r.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusSucceeded || got.SessionID != "sess1" || got.CostUSD != 1.5 || got.WorkerID != "" {
		t.Fatalf("finished run = %+v", got)
	}

	atts, err := s.Attempts(r.ID)
	if err != nil || len(atts) != 1 || atts[0].Status != StatusSucceeded {
		t.Fatalf("attempts = %+v err=%v", atts, err)
	}
}

func TestConcurrencyLimitGate(t *testing.T) {
	s := openTest(t)
	seedJob(t, s, "job1")
	now := time.Unix(1000, 0)
	for i := 0; i < 2; i++ {
		if _, err := s.Enqueue(NewRun{JobID: "job1", ConcurrencyKey: "same", ConcurrencyLimit: 1}, now); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	if _, ok, _ := s.ClaimNext("w1", now); !ok {
		t.Fatal("first claim should succeed")
	}
	// Key saturated at limit 1: the second queued run is not claimable.
	if _, ok, _ := s.ClaimNext("w2", now); ok {
		t.Fatal("claim past concurrency limit should be blocked")
	}
}

func TestIdempotentEnqueue(t *testing.T) {
	s := openTest(t)
	seedJob(t, s, "job1")
	now := time.Unix(1000, 0)
	a, err := s.Enqueue(NewRun{JobID: "job1", IdempotencyKey: "evt-42"}, now)
	if err != nil {
		t.Fatalf("enqueue a: %v", err)
	}
	b, err := s.Enqueue(NewRun{JobID: "job1", IdempotencyKey: "evt-42"}, now)
	if err != nil {
		t.Fatalf("enqueue b: %v", err)
	}
	if a.ID != b.ID {
		t.Fatalf("idempotency key produced two runs: %s != %s", a.ID, b.ID)
	}
	runs, _ := s.Runs("")
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
}

func TestReclaimStaleRequeuesThenFails(t *testing.T) {
	s := openTest(t)
	seedJob(t, s, "job1")
	start := time.Unix(1000, 0)
	r, _ := s.Enqueue(NewRun{JobID: "job1", MaxAttempts: 1}, start)
	if _, ok, _ := s.ClaimNext("w1", start); !ok {
		t.Fatal("claim")
	}
	// No heartbeat; reclaim well past the stale window. Attempt 1 == max, so the
	// run can't be requeued and is failed instead.
	acted, err := s.ReclaimStale(time.Minute, start.Add(time.Hour))
	if err != nil || len(acted) != 1 {
		t.Fatalf("reclaim acted=%v err=%v", acted, err)
	}
	got, _ := s.Run(r.ID)
	if got.Status != StatusFailed || got.FailureClass != FailureInternal {
		t.Fatalf("run after reclaim = %+v", got)
	}
}

func TestReclaimStaleRetries(t *testing.T) {
	s := openTest(t)
	seedJob(t, s, "job1")
	start := time.Unix(1000, 0)
	r, _ := s.Enqueue(NewRun{JobID: "job1", MaxAttempts: 2}, start)
	s.ClaimNext("w1", start) //nolint:errcheck // claim verified below
	acted, err := s.ReclaimStale(time.Minute, start.Add(time.Hour))
	if err != nil || len(acted) != 1 {
		t.Fatalf("reclaim acted=%v err=%v", acted, err)
	}
	got, _ := s.Run(r.ID)
	if got.Status != StatusQueued {
		t.Fatalf("run should be requeued, got %+v", got)
	}
	// Re-claim bumps the attempt to 2.
	c, ok, _ := s.ClaimNext("w2", start.Add(2*time.Hour))
	if !ok || c.Attempt != 2 {
		t.Fatalf("re-claim = %+v ok=%v", c, ok)
	}
}

func TestCancelAndRerun(t *testing.T) {
	s := openTest(t)
	seedJob(t, s, "job1")
	now := time.Unix(1000, 0)
	r, _ := s.Enqueue(NewRun{JobID: "job1", BudgetUSD: 3, MaxAttempts: 1}, now)
	ok, err := s.Cancel(r.ID, now)
	if err != nil || !ok {
		t.Fatalf("cancel ok=%v err=%v", ok, err)
	}
	got, _ := s.Run(r.ID)
	if got.Status != StatusCanceled {
		t.Fatalf("status = %q, want canceled", got.Status)
	}
	// A canceled run is terminal, so it can be rerun into a fresh queued run.
	fresh, err := s.Rerun(r.ID, now)
	if err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if fresh.ID == r.ID || fresh.Status != StatusQueued || fresh.BudgetUSD != 3 {
		t.Fatalf("rerun = %+v", fresh)
	}
}

func TestPauseFlagPersistsAcrossReload(t *testing.T) {
	s := openTest(t)
	seedJob(t, s, "job1")
	if err := s.SetJobPaused("job1", true); err != nil {
		t.Fatalf("pause: %v", err)
	}
	// Reload (UpsertJob) must preserve the operator's paused flag.
	seedJob(t, s, "job1")
	j, err := s.Job("job1")
	if err != nil {
		t.Fatalf("job: %v", err)
	}
	if !j.Paused {
		t.Fatal("paused flag lost on reload")
	}
}

// A run's opaque trigger context survives enqueue → read so a deterministic hook
// can recover the originating GitHub target (AS-147).
func TestTriggerContextRoundTrips(t *testing.T) {
	s := openTest(t)
	now := time.Now()
	ctx := `{"repository":"o/r","number":42}`
	r, err := s.Enqueue(NewRun{JobID: "job1", TriggerContext: ctx}, now)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	got, err := s.Run(r.ID)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.TriggerContext != ctx {
		t.Fatalf("trigger context = %q, want %q", got.TriggerContext, ctx)
	}
}

// migrate is idempotent: reopening the same on-disk store re-runs the schema and
// the additive ADD COLUMN without error (the duplicate column is tolerated).
func TestMigrateIsIdempotentOnReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("open 1: %v", err)
	}
	if _, err := s1.Enqueue(NewRun{JobID: "job1", TriggerContext: "{}"}, time.Now()); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("open 2 (re-migrate): %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })
}
