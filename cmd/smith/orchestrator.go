package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/orchestrator"
	"github.com/tonitienda/agent-smith/internal/orchestrator/store"
	"github.com/tonitienda/agent-smith/internal/run"
	"github.com/tonitienda/agent-smith/internal/session"
)

// Orchestrator daemon timings (AS-161). The tick interval bounds cron resolution
// (the DSL's finest cron grain is one minute) and how promptly queued work is
// drained; staleAfter is how long a claimed run may go without a heartbeat before
// the daemon reclaims it for a crashed worker.
const (
	orchTickInterval = 15 * time.Second
	orchStaleAfter   = 60 * time.Second
)

// daemonCommand is the orchestrator surface (AS-161, ADR D-ORCH-2). It nests under
// `runs` as `runs daemon` so it does not overload the AS-054 async prompt-run verbs
// (`runs list/status/work/resume`), which mean a different kind of "run". `runs
// daemon start` is the long-lived process; the remaining verbs are operator control
// over orchestrated jobs and runs.
func daemonCommand() *cli.Command {
	return &cli.Command{
		Name:    "daemon",
		Summary: "Run and operate the always-on job orchestrator",
		Sub: []*cli.Command{
			{
				Name:          "start",
				Summary:       "Start the long-lived orchestrator (loads .agent-smith/jobs, Ctrl+C to stop)",
				Examples:      []string{"smith runs daemon start"},
				Scriptability: command.Scriptable.String(),
				Run:           runsDaemonStart,
			},
			{
				Name:          "list",
				Summary:       "List orchestrated runs",
				Examples:      []string{"smith runs daemon list", "smith runs daemon list --output json"},
				Scriptability: command.Scriptable.String(),
				OutputSchema:  "runs[]: id, job_id, status, failure_class, attempt, cost_usd, …",
				Run:           runsDaemonList,
			},
			{
				Name:          "inspect",
				Summary:       "Show one run and its attempt history",
				Usage:         "<run-id>",
				Examples:      []string{"smith runs daemon inspect run_…"},
				Scriptability: command.Scriptable.String(),
				Run:           runsDaemonInspect,
			},
			{
				Name:          "rerun",
				Summary:       "Enqueue a fresh run cloning a terminal run",
				Usage:         "<run-id>",
				Scriptability: command.Scriptable.String(),
				Run:           runsDaemonRerun,
			},
			{
				Name:          "cancel",
				Summary:       "Cancel a queued or running run",
				Usage:         "<run-id>",
				Scriptability: command.Scriptable.String(),
				Run:           runsDaemonCancel,
			},
			{
				Name:          "pause",
				Summary:       "Pause a job so its triggers stop enqueuing",
				Usage:         "<job-id>",
				Scriptability: command.Scriptable.String(),
				Run:           func(c *cli.Context) error { return runsDaemonSetPaused(c, true) },
			},
			{
				Name:          "resume",
				Summary:       "Resume a paused job",
				Usage:         "<job-id>",
				Scriptability: command.Scriptable.String(),
				Run:           func(c *cli.Context) error { return runsDaemonSetPaused(c, false) },
			},
			{
				Name:          "health",
				Summary:       "Report loaded jobs and run counts by status",
				Examples:      []string{"smith runs daemon health --output json"},
				Scriptability: command.Scriptable.String(),
				OutputSchema:  "jobs, runs{queued, running, succeeded, failed, canceled}",
				Run:           runsDaemonHealth,
			},
		},
	}
}

// orchStore opens the project-scoped orchestrator run-control database, creating
// its directory under the shared Smith data root (alongside sessions and the async
// run queue).
func orchStore() (*store.Store, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	abs, _ := filepath.Abs(wd)
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	dir := filepath.Join(run.DefaultRoot(), "orchestrator", hex.EncodeToString(sum[:8]))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create orchestrator dir: %w", err)
	}
	return store.Open(filepath.Join(dir, "runs.db"))
}

func runsDaemonStart(c *cli.Context) error {
	st, err := orchStore()
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	// Persist every run as a normal Smith session (AS-151) so /cost, /insights,
	// and replay reach orchestrated runs through the existing readers.
	sessions, err := session.NewStore("", wd)
	if err != nil {
		return err
	}
	d := orchestrator.New(st, orchestrator.Options{
		Executor: orchestrator.NewSessionExecutor(sessions, nil),
	})
	if err := d.LoadDir(wd); err != nil {
		// Per-spec fail-closed: warn about rejected specs but serve the clean ones.
		_, _ = fmt.Fprintf(c.Stderr, "smith: %v\n", err)
	}
	h, _ := d.Health()
	if err := c.Emit(fmt.Sprintf("orchestrator started: %d job(s) loaded; Ctrl+C to stop", h.Jobs)); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := d.Serve(ctx, orchTickInterval, orchStaleAfter); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// orchRunView is the machine-readable projection of an orchestrated run (additive,
// PRD D2).
type orchRunView struct {
	ID           string  `json:"id"`
	JobID        string  `json:"job_id"`
	TriggerKind  string  `json:"trigger_kind,omitempty"`
	Status       string  `json:"status"`
	FailureClass string  `json:"failure_class,omitempty"`
	Attempt      int     `json:"attempt"`
	MaxAttempts  int     `json:"max_attempts"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	SessionID    string  `json:"session_id,omitempty"`
	Error        string  `json:"error,omitempty"`
}

func orchRunViewOf(r store.Run) orchRunView {
	return orchRunView{
		ID: r.ID, JobID: r.JobID, TriggerKind: r.TriggerKind, Status: string(r.Status),
		FailureClass: string(r.FailureClass), Attempt: r.Attempt, MaxAttempts: r.MaxAttempts,
		CostUSD: r.CostUSD, SessionID: r.SessionID, Error: r.Error,
	}
}

func runsDaemonList(c *cli.Context) error {
	st, err := orchStore()
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	runs, err := st.Runs("")
	if err != nil {
		return err
	}
	if isJSON(c) {
		views := make([]orchRunView, 0, len(runs))
		for _, r := range runs {
			views = append(views, orchRunViewOf(r))
		}
		return c.WriteJSON(struct {
			Runs []orchRunView `json:"runs"`
		}{views})
	}
	if len(runs) == 0 {
		return c.Emit("no runs")
	}
	var b strings.Builder
	for _, r := range runs {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", r.ID, r.JobID, r.Status, r.FailureClass)
	}
	return c.Emit(strings.TrimRight(b.String(), "\n"))
}

func runsDaemonInspect(c *cli.Context) error {
	if len(c.Args) != 1 {
		return cli.Usagef("runs daemon inspect: want exactly one run id")
	}
	st, err := orchStore()
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	r, err := st.Run(c.Args[0])
	if err != nil {
		return err
	}
	atts, err := st.Attempts(r.ID)
	if err != nil {
		return err
	}
	if isJSON(c) {
		return c.WriteJSON(struct {
			Run      orchRunView `json:"run"`
			Attempts int         `json:"attempts"`
		}{orchRunViewOf(r), len(atts)})
	}
	var b strings.Builder
	fmt.Fprintf(&b, "id:            %s\n", r.ID)
	fmt.Fprintf(&b, "job:           %s\n", r.JobID)
	fmt.Fprintf(&b, "trigger:       %s\n", r.TriggerKind)
	fmt.Fprintf(&b, "status:        %s\n", r.Status)
	if r.FailureClass != "" {
		fmt.Fprintf(&b, "failure_class: %s\n", r.FailureClass)
	}
	fmt.Fprintf(&b, "attempt:       %d/%d\n", r.Attempt, r.MaxAttempts)
	if r.SessionID != "" {
		fmt.Fprintf(&b, "session_id:    %s\n", r.SessionID)
	}
	if r.Error != "" {
		fmt.Fprintf(&b, "error:         %s\n", r.Error)
	}
	fmt.Fprintf(&b, "attempts:      %d recorded", len(atts))
	return c.Emit(b.String())
}

func runsDaemonRerun(c *cli.Context) error {
	if len(c.Args) != 1 {
		return cli.Usagef("runs daemon rerun: want exactly one run id")
	}
	st, err := orchStore()
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	fresh, err := st.Rerun(c.Args[0], time.Now().UTC())
	if err != nil {
		return err
	}
	if isJSON(c) {
		return c.WriteJSON(orchRunViewOf(fresh))
	}
	return c.Emit(fresh.ID)
}

func runsDaemonCancel(c *cli.Context) error {
	if len(c.Args) != 1 {
		return cli.Usagef("runs daemon cancel: want exactly one run id")
	}
	st, err := orchStore()
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	ok, err := st.Cancel(c.Args[0], time.Now().UTC())
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("run %s is already terminal", c.Args[0])
	}
	return c.Emit("canceled " + c.Args[0])
}

func runsDaemonSetPaused(c *cli.Context, paused bool) error {
	if len(c.Args) != 1 {
		return cli.Usagef("runs daemon pause/resume: want exactly one job id")
	}
	st, err := orchStore()
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	if err := st.SetJobPaused(c.Args[0], paused); err != nil {
		return err
	}
	verb := "resumed"
	if paused {
		verb = "paused"
	}
	return c.Emit(verb + " " + c.Args[0])
}

func runsDaemonHealth(c *cli.Context) error {
	st, err := orchStore()
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	jobs, err := st.Jobs()
	if err != nil {
		return err
	}
	runs, err := st.Runs("")
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, r := range runs {
		counts[string(r.Status)]++
	}
	if isJSON(c) {
		return c.WriteJSON(struct {
			Jobs int            `json:"jobs"`
			Runs map[string]int `json:"runs"`
		}{len(jobs), counts})
	}
	var b strings.Builder
	fmt.Fprintf(&b, "jobs: %d\n", len(jobs))
	for _, s := range []store.RunStatus{store.StatusQueued, store.StatusRunning, store.StatusSucceeded, store.StatusFailed, store.StatusCanceled} {
		fmt.Fprintf(&b, "%-10s %d\n", string(s)+":", counts[string(s)])
	}
	return c.Emit(strings.TrimRight(b.String(), "\n"))
}

func isJSON(c *cli.Context) bool {
	return c.Globals.Output == cli.OutputJSON || c.Globals.Output == cli.OutputStreamJSON
}
