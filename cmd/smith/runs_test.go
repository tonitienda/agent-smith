package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/run"
)

// fastWorkOpts is a worker-pool config tuned for tests: sub-second heartbeat,
// stale, and poll intervals so watch/concurrency behaviour resolves quickly.
func fastWorkOpts(concurrency int, watch bool) workOpts {
	return workOpts{
		concurrency: concurrency,
		watch:       watch,
		staleAfter:  500 * time.Millisecond,
		heartbeat:   10 * time.Millisecond,
		pollEvery:   5 * time.Millisecond,
		workerID:    newWorkerID(),
	}
}

func testRunStore(t *testing.T) *run.Store {
	t.Helper()
	wd, _ := os.Getwd()
	s, err := run.NewStore("", wd)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

// TestWorkQueueConcurrent (AS-132 AC2): several workers drain a queue with every
// run executed exactly once — no run is processed twice and none is dropped.
func TestWorkQueueConcurrent(t *testing.T) {
	isolateHome(t)
	useMockProvider(t, &provider.Mock{NameValue: "anthropic", Events: provider.TextTurn("done", provider.StopEndTurn)})
	store := testRunStore(t)
	wd, _ := os.Getwd()

	const runs = 12
	want := map[string]bool{}
	for range runs {
		rec, err := store.Enqueue(run.Spec{Prompt: "work"})
		if err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
		want[rec.ID] = true
	}

	processed, err := workQueue(context.Background(), "", store, wd, io.Discard, fastWorkOpts(4, false))
	if err != nil {
		t.Fatalf("workQueue: %v", err)
	}
	if len(processed) != runs {
		t.Fatalf("processed %d runs, want %d", len(processed), runs)
	}
	seen := map[string]bool{}
	for _, v := range processed {
		if seen[v.ID] {
			t.Fatalf("run %s processed more than once", v.ID)
		}
		seen[v.ID] = true
		if v.Status != string(run.StatusDone) {
			t.Errorf("run %s status = %q, want done", v.ID, v.Status)
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("run %s was never processed", id)
		}
	}
}

// TestWorkQueueWatch (AS-132 AC1): a watch-mode worker stays up and executes a run
// enqueued after it started, then exits cleanly when its context is canceled.
func TestWorkQueueWatch(t *testing.T) {
	isolateHome(t)
	useMockProvider(t, &provider.Mock{NameValue: "anthropic", Events: provider.TextTurn("late", provider.StopEndTurn)})
	store := testRunStore(t)
	wd, _ := os.Getwd()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan []runView, 1)
	go func() {
		p, err := workQueue(ctx, "", store, wd, io.Discard, fastWorkOpts(1, true))
		if err != nil {
			t.Errorf("workQueue: %v", err)
		}
		done <- p
	}()

	// Enqueue only after the watcher is already running.
	time.Sleep(20 * time.Millisecond)
	rec, err := store.Enqueue(run.Spec{Prompt: "enqueued after start"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Wait for the watcher to pick it up and finish it.
	deadline := time.After(5 * time.Second)
	for {
		got, _ := store.Get(rec.ID)
		if got.Status == run.StatusDone {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("run not completed by watch worker; status=%q", got.Status)
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	select {
	case p := <-done:
		if len(p) != 1 || p[0].ID != rec.ID {
			t.Fatalf("processed = %+v, want the one enqueued run", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch worker did not exit after cancel")
	}
}

// isolateHome points the data dir at a temp HOME so the queue (and the sessions a
// worker creates) stay out of the developer's real ~/.agent-smith.
func isolateHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

// TestRunQueueEnqueues covers AC1 (first half): `smith run --queue` records a
// queued run and prints its ID rather than executing.
func TestRunQueueEnqueues(t *testing.T) {
	isolateHome(t)
	useMockProvider(t, &provider.Mock{
		NameValue: "anthropic",
		Events:    provider.TextTurn("unused", provider.StopEndTurn),
	})
	app, out, _ := testApp(false, false)

	if code := app.Run([]string{"run", "nightly triage", "--queue", "--output", "json"}); code != cli.ExitOK {
		t.Fatalf("exit = %d, want %d; stdout=%s", code, cli.ExitOK, out.String())
	}
	var v runView
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("parse enqueue JSON: %v; raw=%s", err, out.String())
	}
	if v.Status != string(run.StatusQueued) || v.ID == "" {
		t.Fatalf("enqueue view = %+v, want a queued id", v)
	}
}

// TestRunsWorkExecutesQueue covers AC1/AC4: a queued run is executed unattended by
// `runs work`, records an auditable session, and surfaces as done in `runs list`.
func TestRunsWorkExecutesQueue(t *testing.T) {
	isolateHome(t)
	useMockProvider(t, &provider.Mock{
		NameValue: "anthropic",
		Events:    provider.TextTurn("triaged", provider.StopEndTurn),
	})

	// Enqueue.
	enq, encOut, _ := testApp(false, false)
	if code := enq.Run([]string{"run", "do the thing", "--queue", "--output", "json"}); code != cli.ExitOK {
		t.Fatalf("enqueue exit = %d; %s", code, encOut.String())
	}
	var queued runView
	_ = json.Unmarshal(encOut.Bytes(), &queued)

	// Work the queue.
	work, workOut, _ := testApp(false, false)
	if code := work.Run([]string{"runs", "work", "--output", "json"}); code != cli.ExitOK {
		t.Fatalf("work exit = %d; %s", code, workOut.String())
	}
	var batch struct {
		Runs []runView `json:"runs"`
	}
	if err := json.Unmarshal(workOut.Bytes(), &batch); err != nil {
		t.Fatalf("parse work JSON: %v; raw=%s", err, workOut.String())
	}
	if len(batch.Runs) != 1 {
		t.Fatalf("processed %d runs, want 1: %+v", len(batch.Runs), batch.Runs)
	}
	done := batch.Runs[0]
	if done.ID != queued.ID {
		t.Errorf("processed id = %s, want %s", done.ID, queued.ID)
	}
	if done.Status != string(run.StatusDone) {
		t.Errorf("status = %q, want done", done.Status)
	}
	if done.SessionID == "" {
		t.Error("a worked run must record a resumable session")
	}

	// list shows the finished run.
	ls, lsOut, _ := testApp(false, false)
	if code := ls.Run([]string{"runs", "list", "--output", "json"}); code != cli.ExitOK {
		t.Fatalf("list exit = %d", code)
	}
	var listed struct {
		Runs []runView `json:"runs"`
	}
	if err := json.Unmarshal(lsOut.Bytes(), &listed); err != nil {
		t.Fatalf("parse list JSON: %v", err)
	}
	if len(listed.Runs) != 1 || listed.Runs[0].Status != string(run.StatusDone) {
		t.Fatalf("list = %+v, want one done run", listed.Runs)
	}
}

// TestRunsWorkEmptyQueue: working an empty queue is a clean no-op.
func TestRunsWorkEmptyQueue(t *testing.T) {
	isolateHome(t)
	useMockProvider(t, &provider.Mock{NameValue: "anthropic", Events: provider.TextTurn("x", provider.StopEndTurn)})
	app, out, _ := testApp(false, false)
	if code := app.Run([]string{"runs", "work", "--output", "json"}); code != cli.ExitOK {
		t.Fatalf("work exit = %d", code)
	}
	var batch struct {
		Runs []runView `json:"runs"`
	}
	if err := json.Unmarshal(out.Bytes(), &batch); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(batch.Runs) != 0 {
		t.Errorf("processed %d, want 0", len(batch.Runs))
	}
}

// TestRunsWorkBudgetStop covers AC2: a hard budget ceiling halts a background run
// cleanly and the stop is reported in the run's status.
func TestRunsWorkBudgetStop(t *testing.T) {
	isolateHome(t)
	useMockProvider(t, &provider.Mock{
		NameValue: "anthropic",
		Events:    provider.TextTurn("should never run", provider.StopEndTurn),
	})

	enq, _, _ := testApp(false, false)
	if code := enq.Run([]string{"run", "expensive", "--queue", "--budget", "0.0000001"}); code != cli.ExitOK {
		t.Fatalf("enqueue exit = %d", code)
	}
	work, workOut, _ := testApp(false, false)
	if code := work.Run([]string{"runs", "work", "--output", "json"}); code != cli.ExitOK {
		t.Fatalf("work exit = %d", code)
	}
	var batch struct {
		Runs []runView `json:"runs"`
	}
	if err := json.Unmarshal(workOut.Bytes(), &batch); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(batch.Runs) != 1 || batch.Runs[0].Status != string(run.StatusBudget) {
		t.Fatalf("runs = %+v, want one budget-stopped run", batch.Runs)
	}
}

// TestRunsResumeRequeues covers AC3: an interrupted run is re-queued by `runs
// resume` and then completes on the next `work`.
func TestRunsResumeRequeues(t *testing.T) {
	isolateHome(t)
	useMockProvider(t, &provider.Mock{
		NameValue: "anthropic",
		Events:    provider.TextTurn("recovered", provider.StopEndTurn),
	})

	// Seed a run stuck in `running` (a worker that died mid-flight), directly via
	// the store so we control its state.
	wd, _ := os.Getwd()
	store, err := run.NewStore("", wd)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	rec, err := store.Enqueue(run.Spec{Prompt: "interrupted task"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	rec.Status = run.StatusRunning
	if err := store.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A fresh worker reclaims the stale run as interrupted (and processes nothing).
	work, _, _ := testApp(false, false)
	if code := work.Run([]string{"runs", "work"}); code != cli.ExitOK {
		t.Fatalf("work exit = %d", code)
	}
	got, _ := store.Get(rec.ID)
	if got.Status != run.StatusInterrupted {
		t.Fatalf("status after reclaim = %q, want interrupted", got.Status)
	}

	// Resume re-queues it.
	res, _, _ := testApp(false, false)
	if code := res.Run([]string{"runs", "resume", rec.ID}); code != cli.ExitOK {
		t.Fatalf("resume exit = %d", code)
	}
	got, _ = store.Get(rec.ID)
	if got.Status != run.StatusQueued {
		t.Fatalf("status after resume = %q, want queued", got.Status)
	}

	// The next work run completes it.
	work2, _, _ := testApp(false, false)
	if code := work2.Run([]string{"runs", "work"}); code != cli.ExitOK {
		t.Fatalf("second work exit = %d", code)
	}
	got, _ = store.Get(rec.ID)
	if got.Status != run.StatusDone {
		t.Fatalf("status after resume+work = %q, want done", got.Status)
	}
}
