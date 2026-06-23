package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/run"
)

// enqueueRun records a queued run for the background runner (AS-054, `smith run
// --queue`) and reports its ID. The run executes later under `smith runs work`;
// nothing here builds an engine or contacts a provider. The ID is the handle for
// `runs status`/`runs resume`.
func enqueueRun(c *cli.Context, prompt string, opts headlessOpts) error {
	store, err := runStore()
	if err != nil {
		return err
	}
	rec, err := store.Enqueue(run.Spec{
		Prompt:    prompt,
		BudgetUSD: opts.budgetUSD,
		Auto:      opts.auto,
	})
	if err != nil {
		return fmt.Errorf("enqueue run: %w", err)
	}
	switch c.Globals.Output {
	case cli.OutputJSON, cli.OutputStreamJSON:
		return c.WriteJSON(runViewOf(rec))
	default:
		return c.Emit(rec.ID)
	}
}

// runStore opens the project-scoped run queue rooted at the default data dir,
// alongside the sessions a worker creates.
func runStore() (*run.Store, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	store, err := run.NewStore("", wd)
	if err != nil {
		return nil, fmt.Errorf("open run store: %w", err)
	}
	return store, nil
}

// runsCommand groups the background-runner verbs (AS-054): `list` and `status`
// inspect the queue, `work` drains it unattended, and `resume` re-queues an
// interrupted run. All are scriptable — the runner's whole point is unattended,
// auditable operation (§3 Async Ana) — and emit machine-readable JSON under
// `--output json`.
func runsCommand() *cli.Command {
	return &cli.Command{
		Name:    "runs",
		Summary: "Inspect and drive the background run queue",
		Sub: []*cli.Command{
			{
				Name:          "list",
				Summary:       "List this project's queued and finished runs",
				Examples:      []string{"smith runs list", "smith runs list --output json"},
				Scriptability: command.Scriptable.String(),
				OutputSchema:  "runs[]: id, status, prompt, cost_usd, session_id, …",
				Run:           runsList,
			},
			{
				Name:          "status",
				Summary:       "Show one run's record",
				Usage:         "<id>",
				Examples:      []string{"smith runs status run_…"},
				Scriptability: command.Scriptable.String(),
				OutputSchema:  "id, status, prompt, cost_usd, session_id, stop_reason, exit_code, error",
				Run:           runsStatus,
			},
			{
				Name:          "work",
				Summary:       "Execute queued runs unattended, then exit",
				Examples:      []string{"smith runs work", "smith runs work --output json"},
				Scriptability: command.Scriptable.String(),
				OutputSchema:  "runs[]: id, status, cost_usd, … (one per processed run)",
				Run:           runsWork,
			},
			{
				Name:          "resume",
				Summary:       "Re-queue an interrupted or failed run",
				Usage:         "<id>",
				Examples:      []string{"smith runs resume run_…"},
				Scriptability: command.Scriptable.String(),
				Run:           runsResume,
			},
		},
	}
}

// runView is the machine-readable projection of a run record (D-CLI-4). It mirrors
// the durable record's fields; new fields stay additive (PRD D2).
type runView struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	Prompt     string    `json:"prompt"`
	BudgetUSD  float64   `json:"budget_usd,omitempty"`
	Auto       bool      `json:"auto,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	StopReason string    `json:"stop_reason,omitempty"`
	CostUSD    float64   `json:"cost_usd,omitempty"`
	ExitCode   int       `json:"exit_code,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func runViewOf(rec run.Record) runView {
	return runView{
		ID:         rec.ID,
		Status:     string(rec.Status),
		Prompt:     rec.Prompt,
		BudgetUSD:  rec.BudgetUSD,
		Auto:       rec.Auto,
		SessionID:  rec.SessionID,
		StopReason: rec.StopReason,
		CostUSD:    rec.CostUSD,
		ExitCode:   rec.ExitCode,
		Error:      rec.Error,
		CreatedAt:  rec.CreatedAt,
	}
}

// runsList prints the project's runs newest first: a tab-aligned table in plain
// mode, a JSON array under `--output json`.
func runsList(c *cli.Context) error {
	store, err := runStore()
	if err != nil {
		return err
	}
	recs, err := store.List()
	if err != nil {
		return err
	}
	if c.Globals.Output == cli.OutputJSON || c.Globals.Output == cli.OutputStreamJSON {
		views := make([]runView, 0, len(recs))
		for _, r := range recs {
			views = append(views, runViewOf(r))
		}
		return c.WriteJSON(struct {
			Runs []runView `json:"runs"`
		}{views})
	}
	if len(recs) == 0 {
		return c.Emit("no runs")
	}
	var b strings.Builder
	for _, r := range recs {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", r.ID, r.Status, oneLine(r.Prompt))
	}
	return c.Emit(strings.TrimRight(b.String(), "\n"))
}

// runsStatus prints one run's full record by ID.
func runsStatus(c *cli.Context) error {
	if len(c.Args) != 1 {
		return cli.Usagef("runs status: want exactly one run id")
	}
	store, err := runStore()
	if err != nil {
		return err
	}
	rec, err := store.Get(c.Args[0])
	if err != nil {
		return err
	}
	if c.Globals.Output == cli.OutputJSON || c.Globals.Output == cli.OutputStreamJSON {
		return c.WriteJSON(runViewOf(rec))
	}
	return c.Emit(formatRecord(rec))
}

// runsResume re-queues an interrupted, failed, budget-stopped, or canceled run so
// a worker picks it up again (AS-054: manual resume, no auto-restart). The run's
// prior outcome fields are cleared and its attempt counter reset; a queued or
// running record is left alone (nothing to resume).
func runsResume(c *cli.Context) error {
	if len(c.Args) != 1 {
		return cli.Usagef("runs resume: want exactly one run id")
	}
	store, err := runStore()
	if err != nil {
		return err
	}
	rec, err := store.Get(c.Args[0])
	if err != nil {
		return err
	}
	if rec.Status == run.StatusQueued || rec.Status == run.StatusRunning {
		return fmt.Errorf("run %s is %s, not resumable", rec.ID, rec.Status)
	}
	rec.Status = run.StatusQueued
	rec.SessionID = ""
	rec.StopReason = ""
	rec.CostUSD = 0
	rec.ExitCode = 0
	rec.Error = ""
	rec.StartedAt = nil
	rec.FinishedAt = nil
	if err := store.Save(rec); err != nil {
		return err
	}
	if c.Globals.Output == cli.OutputJSON || c.Globals.Output == cli.OutputStreamJSON {
		return c.WriteJSON(runViewOf(rec))
	}
	return c.Emit(rec.ID)
}

// runsWork drains the queue: it reclaims any run left `running` by a dead worker
// as `interrupted` (single-worker model — see the AS-054 decision), then executes
// each queued run in creation order within its budget ceiling, recording an
// auditable session per run. Ctrl+C cancels the in-flight run cleanly (marked
// interrupted) and stops the worker. It prints one result line/object per run.
func runsWork(c *cli.Context) error {
	store, err := runStore()
	if err != nil {
		return err
	}
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	if err := reclaimStale(store); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var processed []runView
	for {
		rec, ok, err := nextQueued(store)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		if ctx.Err() != nil {
			break
		}
		rec = workOne(ctx, store, wd, rec, c.Stderr)
		processed = append(processed, runViewOf(rec))
		if ctx.Err() != nil {
			break
		}
	}
	return emitProcessed(c, processed)
}

// workOne executes a single claimed run and persists its outcome. Transient
// provider/network failures are already retried inside the turn by the loop's
// backoff policy (AS-018), so a provider error here is terminal — recorded as a
// failure, recoverable by `runs resume`. The returned record is the final state.
func workOne(ctx context.Context, store *run.Store, wd string, rec run.Record, stderr io.Writer) run.Record {
	now := time.Now().UTC()
	rec.Status = run.StatusRunning
	rec.StartedAt = &now
	if err := store.Save(rec); err != nil {
		// A bookkeeping failure is fatal to this run; record best-effort and stop.
		rec.Status = run.StatusFailed
		rec.Error = err.Error()
		return rec
	}

	out, setupErr := executeRun(ctx, "", wd, rec.Prompt, headlessOpts{budgetUSD: rec.BudgetUSD, auto: rec.Auto}, nil, stderr)
	fin := time.Now().UTC()
	rec.FinishedAt = &fin
	if setupErr != nil {
		rec.Status = run.StatusFailed
		rec.Error = setupErr.Error()
		_ = store.Save(rec)
		return rec
	}

	code, reason := classifyExit(out.res, out.runErr, out.denied)
	rec.SessionID = out.sessionID
	rec.CostUSD = out.costUSD
	rec.StopReason = out.res.StopReason
	rec.ExitCode = code
	rec.Error = reason
	rec.Status = statusFromExit(code)
	_ = store.Save(rec)
	return rec
}

// statusFromExit maps the headless exit-code taxonomy (D-CLI-7) to a durable run
// status. A clean stop is done; a budget ceiling is its own terminal state; a
// cancellation means the worker was interrupted mid-run (resumable); everything
// else (permission, provider, internal) is a failure.
func statusFromExit(code int) run.Status {
	switch code {
	case cli.ExitOK:
		return run.StatusDone
	case cli.ExitBudget:
		return run.StatusBudget
	case cli.ExitCanceled:
		return run.StatusInterrupted
	default:
		return run.StatusFailed
	}
}

// reclaimStale marks any run still `running` as `interrupted` before a new worker
// starts. The runner is single-worker by default (AS-054 decision), so a leftover
// `running` record means the previous worker died mid-run; resume re-queues it.
func reclaimStale(store *run.Store) error {
	recs, err := store.List()
	if err != nil {
		return err
	}
	for _, rec := range recs {
		if rec.Status != run.StatusRunning {
			continue
		}
		rec.Status = run.StatusInterrupted
		if rec.Error == "" {
			rec.Error = "worker exited before the run finished"
		}
		if err := store.Save(rec); err != nil {
			return err
		}
	}
	return nil
}

// nextQueued returns the oldest queued run (FIFO by creation), reloading the store
// each call so it sees records a retry re-queued. ok is false when none remain.
func nextQueued(store *run.Store) (run.Record, bool, error) {
	recs, err := store.List()
	if err != nil {
		return run.Record{}, false, err
	}
	var oldest run.Record
	found := false
	for _, rec := range recs {
		if rec.Status != run.StatusQueued {
			continue
		}
		if !found || rec.CreatedAt.Before(oldest.CreatedAt) {
			oldest = rec
			found = true
		}
	}
	return oldest, found, nil
}

// emitProcessed renders the worker's batch result: a JSON object listing the
// processed runs, or a plain per-run summary line (none when the queue was empty).
func emitProcessed(c *cli.Context, processed []runView) error {
	if c.Globals.Output == cli.OutputJSON || c.Globals.Output == cli.OutputStreamJSON {
		return c.WriteJSON(struct {
			Runs []runView `json:"runs"`
		}{processed})
	}
	if len(processed) == 0 {
		return c.Emit("no queued runs")
	}
	var b strings.Builder
	for _, v := range processed {
		fmt.Fprintf(&b, "%s\t%s\t$%.4f\n", v.ID, v.Status, v.CostUSD)
	}
	return c.Emit(strings.TrimRight(b.String(), "\n"))
}

// formatRecord renders a run record as a plain key: value block for `runs status`.
func formatRecord(rec run.Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "id:          %s\n", rec.ID)
	fmt.Fprintf(&b, "status:      %s\n", rec.Status)
	fmt.Fprintf(&b, "prompt:      %s\n", oneLine(rec.Prompt))
	if rec.BudgetUSD > 0 {
		fmt.Fprintf(&b, "budget_usd:  %.4f\n", rec.BudgetUSD)
	}
	fmt.Fprintf(&b, "auto:        %t\n", rec.Auto)
	if rec.SessionID != "" {
		fmt.Fprintf(&b, "session_id:  %s\n", rec.SessionID)
	}
	if rec.StopReason != "" {
		fmt.Fprintf(&b, "stop_reason: %s\n", rec.StopReason)
	}
	fmt.Fprintf(&b, "cost_usd:    %.4f\n", rec.CostUSD)
	if rec.Error != "" {
		fmt.Fprintf(&b, "error:       %s\n", rec.Error)
	}
	fmt.Fprintf(&b, "created_at:  %s", rec.CreatedAt.Format(time.RFC3339))
	return b.String()
}

// oneLine collapses a multi-line prompt to a single trimmed line for table/summary
// rendering, truncating long prompts so a row stays readable.
func oneLine(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\t", " "))
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	const max = 60
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}
