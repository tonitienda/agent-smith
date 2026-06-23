package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/run"
)

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
