package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/run"
)

// Worker-pool timings (AS-132). heartbeat refreshes a claimed run's liveness;
// staleAfter is how long a run may go without a heartbeat before a survivor
// reclaims it (comfortably larger than heartbeat); pollEvery is how often a
// --watch worker checks for new work while idle.
const (
	heartbeatEvery = 5 * time.Second
	staleAfter     = 30 * time.Second
	pollEvery      = time.Second
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

// runsCommand groups the background-runner verbs (AS-054, AS-132): `list` and
// `status` inspect the queue, `work` drains it unattended (or `--watch`es it with
// `--concurrency` workers), and `resume` re-queues an interrupted run. All are scriptable — the runner's whole point is unattended,
// auditable operation (§3 Async Ana) — and emit machine-readable JSON under
// `--output json`.
func runsCommand() *cli.Command {
	var watch bool
	var concurrency int
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
				Summary:       "Execute queued runs unattended (--watch to stay running)",
				Usage:         "[--watch] [--concurrency N]",
				Examples:      []string{"smith runs work", "smith runs work --watch --concurrency 4"},
				Scriptability: command.Scriptable.String(),
				OutputSchema:  "runs[]: id, status, cost_usd, … (one per processed run)",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&watch, "watch", false, "stay running and pick up runs as they are enqueued (Ctrl+C to stop)")
					fs.IntVar(&concurrency, "concurrency", 1, "number of runs to execute in parallel")
				},
				Run: func(c *cli.Context) error { return runsWork(c, watch, concurrency) },
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
	rec.WorkerID = ""
	rec.StartedAt = nil
	rec.FinishedAt = nil
	rec.HeartbeatAt = nil
	if err := store.Save(rec); err != nil {
		return err
	}
	// Clear any lingering lease so a worker can re-claim the now-queued run.
	if err := store.Release(rec.ID); err != nil {
		return err
	}
	if c.Globals.Output == cli.OutputJSON || c.Globals.Output == cli.OutputStreamJSON {
		return c.WriteJSON(runViewOf(rec))
	}
	return c.Emit(rec.ID)
}

// runsWork drains the queue and, with --watch, stays up to pick newly enqueued
// runs. --concurrency N runs N runs in parallel; each is claimed through an atomic
// per-run lease so no two workers ever execute the same run, and a crashed
// worker's in-flight run is reclaimed as `interrupted` (its heartbeat goes stale).
// Ctrl+C stops claiming new runs and lets in-flight runs unwind cleanly. It prints
// one result line/object per processed run.
func runsWork(c *cli.Context, watch bool, concurrency int) error {
	if concurrency < 1 {
		return cli.Usagef("runs work: --concurrency must be >= 1")
	}
	store, err := runStore()
	if err != nil {
		return err
	}
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	processed, err := workQueue(ctx, c.Globals.Config, store, wd, c.Stderr, workOpts{
		concurrency: concurrency,
		watch:       watch,
		staleAfter:  staleAfter,
		heartbeat:   heartbeatEvery,
		pollEvery:   pollEvery,
		workerID:    newWorkerID(),
	})
	if err != nil {
		return err
	}
	return emitProcessed(c, processed)
}

// workOpts configures a worker pool. workerID is the pool's base id; each worker
// goroutine appends its index so two workers never share a lease identity.
type workOpts struct {
	concurrency int
	watch       bool
	staleAfter  time.Duration
	heartbeat   time.Duration
	pollEvery   time.Duration
	workerID    string
}

// newWorkerID is a per-process base id for a worker pool. Uniqueness only needs to
// hold among workers contending for one project's queue (typically one host), so
// pid + start nanos suffices.
func newWorkerID() string {
	return fmt.Sprintf("worker-%d-%d", os.Getpid(), time.Now().UnixNano())
}

// workQueue runs opts.concurrency worker goroutines that drain the project's run
// queue, optionally staying up to pick newly enqueued runs (watch). Each worker
// claims runs through the store's atomic lease, so no two workers execute the same
// run; a crashed worker's in-flight run is reclaimed as interrupted (its heartbeat
// goes stale) by a single periodic reclaimer. It returns the runs it processed.
func workQueue(ctx context.Context, configOverride string, store *run.Store, wd string, stderr io.Writer, opts workOpts) ([]runView, error) {
	// Reclaim crashed-worker runs before starting. A live peer's run has a fresh
	// heartbeat and is left alone, so this is safe even as peers start concurrently.
	if _, err := store.Reclaim(opts.staleAfter, time.Now().UTC()); err != nil {
		return nil, err
	}

	// A worker's terminal error (or Ctrl+C) cancels the pool, so siblings stop
	// claiming and unwind instead of blocking wg.Wait — and an in-flight run sees
	// the cancellation and ends as interrupted.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu        sync.Mutex
		processed []runView
		firstErr  error
		wg        sync.WaitGroup
	)
	fail := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
		cancel()
	}

	// A single reclaimer recovers peers that crash mid-session; running it once per
	// pool (not once per idle worker) keeps the queue-directory scan off the hot
	// idle path. Only watch mode lives long enough to need it — a drain pass already
	// reclaimed once at the top.
	if opts.watch {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(opts.staleAfter / 2)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if _, err := store.Reclaim(opts.staleAfter, time.Now().UTC()); err != nil {
						fail(err)
						return
					}
				}
			}
		}()
	}

	for i := 0; i < opts.concurrency; i++ {
		workerID := fmt.Sprintf("%s-%d", opts.workerID, i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				rec, ok, err := claimNext(store, workerID)
				if err != nil {
					fail(err)
					return
				}
				if !ok {
					if !opts.watch {
						return // queue drained
					}
					select {
					case <-ctx.Done():
						return
					case <-time.After(opts.pollEvery):
						continue
					}
				}
				done, err := workOne(ctx, configOverride, store, wd, rec, workerID, opts.heartbeat, stderr)
				if err != nil {
					// A bookkeeping write failed (full disk, permissions): stop rather
					// than loop forever on a run whose on-disk status never advanced.
					fail(err)
					return
				}
				mu.Lock()
				processed = append(processed, runViewOf(done))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return processed, firstErr
}

// claimNext claims the oldest queued run not already taken by a peer, or reports
// ok=false when none remain. store.List is newest-first, so it scans backwards to
// honour FIFO order.
func claimNext(store *run.Store, workerID string) (run.Record, bool, error) {
	recs, err := store.List()
	if err != nil {
		return run.Record{}, false, err
	}
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].Status != run.StatusQueued {
			continue
		}
		rec, ok, err := store.Claim(recs[i].ID, workerID)
		if err != nil {
			return run.Record{}, false, err
		}
		if ok {
			return rec, true, nil
		}
		// Claimed by a peer between List and Claim — keep scanning.
	}
	return run.Record{}, false, nil
}

// workOne executes an already-claimed (running) run and persists its outcome,
// heartbeating it while it executes so a survivor's Reclaim never steals a run
// that is still progressing, and releasing its lease when done so a later `runs
// resume` can re-claim it. configOverride is the worker's --config path, threaded
// into the execution core so a queued run honors the same custom
// model/provider/pricing the worker was invoked with. Transient provider/network
// failures are already retried inside the turn by the loop's backoff policy
// (AS-018), so a provider error here is terminal — recorded as a failure,
// recoverable by `runs resume`. A non-nil error means a record write failed (the
// on-disk status may be stale); the caller stops that worker.
func workOne(ctx context.Context, configOverride string, store *run.Store, wd string, rec run.Record, workerID string, heartbeat time.Duration, stderr io.Writer) (run.Record, error) {
	defer func() { _ = store.Release(rec.ID) }()

	// Heartbeat until execution returns so the run's liveness timestamp stays fresh.
	// Stopped and joined before the final Save below, so a heartbeat write never
	// races the outcome write.
	hbCtx, hbStop := context.WithCancel(context.Background())
	var hbWG sync.WaitGroup
	hbWG.Add(1)
	go func() {
		defer hbWG.Done()
		t := time.NewTicker(heartbeat)
		defer t.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-t.C:
				_ = store.Heartbeat(rec.ID, workerID)
			}
		}
	}()

	out, setupErr := executeRun(ctx, configOverride, wd, rec.Prompt, headlessOpts{budgetUSD: rec.BudgetUSD, auto: rec.Auto}, nil, stderr)
	hbStop()
	hbWG.Wait()
	fin := time.Now().UTC()
	rec.FinishedAt = &fin
	if setupErr != nil {
		rec.Status = run.StatusFailed
		rec.Error = setupErr.Error()
		if err := store.Save(rec); err != nil {
			return rec, fmt.Errorf("save setup-failure status for %s: %w", rec.ID, err)
		}
		return rec, nil
	}

	code, reason := classifyExit(out.res, out.runErr, out.denied)
	rec.SessionID = out.sessionID
	rec.CostUSD = out.costUSD
	rec.StopReason = out.res.StopReason
	rec.ExitCode = code
	rec.Error = reason
	rec.Status = statusFromExit(code)
	if err := store.Save(rec); err != nil {
		return rec, fmt.Errorf("save final status for %s: %w", rec.ID, err)
	}
	return rec, nil
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

// oneLine collapses a prompt's whitespace runs to single spaces for table/summary
// rendering, truncating long prompts so a row stays readable. Truncation is by
// rune, not byte, so a multi-byte character is never split into invalid UTF-8.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 60
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max-1]) + "…"
	}
	return s
}
