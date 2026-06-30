package main

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/orchestrator/store"
)

// jsonCtx builds a handler Context that captures stdout and requests JSON output.
func jsonCtx(args ...string) (*cli.Context, *bytes.Buffer) {
	var out bytes.Buffer
	return &cli.Context{
		Args:    args,
		Globals: cli.Globals{Output: cli.OutputJSON},
		Stdout:  &out,
		Stderr:  io.Discard,
	}, &out
}

func TestRunsDaemonOperatorVerbs(t *testing.T) {
	isolateHome(t)

	// Seed a job and a queued run into the same project-scoped DB the handlers open.
	st, err := orchStore()
	if err != nil {
		t.Fatalf("orchStore: %v", err)
	}
	if err := st.UpsertJob(
		store.Job{ID: "j", File: "j.yaml", Version: 1, Owner: "me", Repository: "o/r", LoadedAt: time.Now()},
		[]store.JobTrigger{{Kind: "manual"}},
	); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	r, err := st.Enqueue(store.NewRun{JobID: "j"}, time.Now())
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	_ = st.Close()

	// health: 1 job, 1 queued run.
	c, out := jsonCtx()
	if err := runsDaemonHealth(c); err != nil {
		t.Fatalf("health: %v", err)
	}
	var health struct {
		Jobs int            `json:"jobs"`
		Runs map[string]int `json:"runs"`
	}
	if err := json.Unmarshal(out.Bytes(), &health); err != nil {
		t.Fatalf("health json: %v (%s)", err, out)
	}
	if health.Jobs != 1 || health.Runs["queued"] != 1 {
		t.Fatalf("health = %+v", health)
	}

	// list: includes the queued run.
	c, out = jsonCtx()
	if err := runsDaemonList(c); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte(r.ID)) {
		t.Fatalf("list missing run %s: %s", r.ID, out)
	}

	// cancel: a queued run becomes terminal.
	c, _ = jsonCtx(r.ID)
	if err := runsDaemonCancel(c); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	// rerun: a terminal run can be re-enqueued.
	c, out = jsonCtx(r.ID)
	if err := runsDaemonRerun(c); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	var fresh orchRunView
	if err := json.Unmarshal(out.Bytes(), &fresh); err != nil {
		t.Fatalf("rerun json: %v (%s)", err, out)
	}
	if fresh.ID == r.ID || fresh.Status != string(store.StatusQueued) {
		t.Fatalf("rerun = %+v", fresh)
	}
}

func TestRunsDaemonInspectArgValidation(t *testing.T) {
	isolateHome(t)
	c, _ := jsonCtx() // no run id
	if err := runsDaemonInspect(c); err == nil {
		t.Fatal("inspect with no id should be a usage error")
	}
}
