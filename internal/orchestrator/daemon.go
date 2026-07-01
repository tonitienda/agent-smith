package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/orchestrator/spec"
	"github.com/tonitienda/agent-smith/internal/orchestrator/store"
)

// Daemon is the long-lived orchestrator process. It publishes loaded jobs to the
// run store, turns triggers into queued runs, and supervises execution under the
// per-job concurrency/timeout/retry/budget policy. It is safe to drive from one
// goroutine (Serve) or step manually (FireDueCron/RunOne) in tests; the store
// serialises the state it shares.
type Daemon struct {
	store    *store.Store
	exec     Executor
	now      func() time.Time
	jobs     map[string]*spec.Spec
	crons    []cronEntry
	workerID string
}

// cronEntry is a parsed cron trigger with its next fire time, held in memory so
// FireDueCron does not round-trip the store each tick. next_fire is also persisted
// on the trigger row for operator inspection.
type cronEntry struct {
	jobID string
	sched cronSchedule
	loc   *time.Location
	next  time.Time
}

// Options configure a Daemon. Now defaults to time.Now; Executor defaults to the
// MVP-0 StubExecutor.
type Options struct {
	Now      func() time.Time
	Executor Executor
	WorkerID string
}

// New builds a daemon over an open run store.
func New(st *store.Store, opts Options) *Daemon {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	exec := opts.Executor
	if exec == nil {
		exec = StubExecutor{}
	}
	wid := opts.WorkerID
	if wid == "" {
		wid = fmt.Sprintf("orchd-%d", os.Getpid())
	}
	return &Daemon{store: st, exec: exec, now: now, jobs: map[string]*spec.Spec{}, workerID: wid}
}

// LoadDir loads and publishes every job spec under root/.agent-smith/jobs. A
// non-nil error means at least one spec was rejected; the daemon publishes only
// the specs that loaded cleanly (fail-closed).
func (d *Daemon) LoadDir(root string) error {
	dir := filepath.Join(root, JobsDir)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("orchestrator: jobs dir %s: %w", dir, err)
	}
	specs, loadErr := LoadJobs(os.DirFS(dir))
	if pubErr := d.Publish(specs); pubErr != nil {
		return pubErr
	}
	return loadErr
}

// Publish records each job in the store (preserving an existing paused flag) and
// arms its cron triggers. Re-publishing replaces a job's trigger rows.
func (d *Daemon) Publish(specs []*spec.Spec) error {
	now := d.now()
	d.crons = nil
	for _, s := range specs {
		d.jobs[s.ID] = s
		triggers, crons, err := d.triggerRows(s, now)
		if err != nil {
			return err
		}
		specJSON, _ := json.Marshal(specView(s))
		j := store.Job{
			ID: s.ID, File: s.File, Version: s.Version, Owner: s.Owner,
			Repository: s.Repository, Org: s.Org, SpecJSON: string(specJSON), LoadedAt: now,
		}
		if err := d.store.UpsertJob(j, triggers); err != nil {
			return err
		}
		d.crons = append(d.crons, crons...)
	}
	sort.Slice(d.crons, func(i, j int) bool { return d.crons[i].next.Before(d.crons[j].next) })
	return nil
}

func (d *Daemon) triggerRows(s *spec.Spec, now time.Time) ([]store.JobTrigger, []cronEntry, error) {
	var rows []store.JobTrigger
	var crons []cronEntry
	for _, t := range s.Triggers {
		args, _ := json.Marshal(t.Args)
		row := store.JobTrigger{Kind: t.Kind, ArgsJSON: string(args)}
		if t.Kind == "cron" {
			schedule, _ := t.Args["schedule"].(string)
			tz, _ := t.Args["timezone"].(string)
			loc, err := time.LoadLocation(tz)
			if err != nil {
				return nil, nil, fmt.Errorf("orchestrator: job %s: bad timezone %q: %w", s.ID, tz, err)
			}
			sched, err := parseCron(schedule)
			if err != nil {
				return nil, nil, fmt.Errorf("orchestrator: job %s: %w", s.ID, err)
			}
			next, ok := sched.next(now, loc)
			if ok {
				row.NextFire = &next
				crons = append(crons, cronEntry{jobID: s.ID, sched: sched, loc: loc, next: next})
			}
		}
		rows = append(rows, row)
	}
	return rows, crons, nil
}

// FireDueCron enqueues a run for every cron trigger whose next fire time has
// arrived, advancing each to its following slot. It returns the runs it enqueued.
func (d *Daemon) FireDueCron(now time.Time) ([]store.Run, error) {
	var out []store.Run
	for i := range d.crons {
		for !d.crons[i].next.After(now) {
			s := d.jobs[d.crons[i].jobID]
			fired := d.crons[i].next
			run, enq, err := d.enqueue(s, "cron", nil, "", idemKey("cron", s.ID, fired.UTC().Format(time.RFC3339)))
			if err != nil {
				return out, err
			}
			if enq {
				out = append(out, run)
			}
			next, ok := d.crons[i].sched.next(fired, d.crons[i].loc)
			if !ok {
				d.crons[i].next = now.AddDate(100, 0, 0) // unsatisfiable schedule: park it
				break
			}
			d.crons[i].next = next
		}
	}
	return out, nil
}

// EnqueueManual dispatches a job's manual trigger with the given inputs (operator
// action / future API). It errors if the job has no manual trigger.
func (d *Daemon) EnqueueManual(jobID string, inputs map[string]string) (store.Run, error) {
	s, ok := d.jobs[jobID]
	if !ok {
		return store.Run{}, fmt.Errorf("orchestrator: unknown job %q", jobID)
	}
	if !hasTrigger(s, "manual") {
		return store.Run{}, fmt.Errorf("orchestrator: job %q has no manual trigger", jobID)
	}
	run, _, err := d.enqueue(s, "manual", inputs, "", "")
	return run, err
}

// GitHubEvent is a normalised inbound GitHub trigger — the stable Smith trigger
// record the scheduler matches against jobs. [Normalize] (webhook.go) maps a raw
// GitHub webhook delivery into this shape (AS-147); DeliveryID makes a re-delivered
// event idempotent so a duplicate delivery never enqueues duplicate work.
//
// The first six fields are what trigger *matching* needs; Number/Actor/Labels/
// EventTime are the additional trigger-record context (AS-147 AC) that downstream
// deterministic action steps (AS-149) and the session log (AS-151) consume — e.g.
// which issue/PR to label and who asked. They never influence matching, so a
// trigger's behaviour is not encoded in prompt content (AS-147 AC).
type GitHubEvent struct {
	DeliveryID string
	Kind       string // e.g. "github.issue_labeled"
	Repository string
	Label      string
	Base       string
	Command    string

	Number    int       // issue or PR number the event concerns (0 when N/A)
	Actor     string    // GitHub login that caused the event
	Labels    []string  // full label set on the issue/PR at event time
	EventTime time.Time // timestamp of the source object (zero when absent)
}

// EnqueueGitHub fans an inbound event out to every matching job trigger, deduped by
// the event's delivery id, and returns the runs it enqueued.
func (d *Daemon) EnqueueGitHub(ev GitHubEvent) ([]store.Run, error) {
	ids := make([]string, 0, len(d.jobs))
	for id := range d.jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	tctx := ev.triggerContextJSON()
	var out []store.Run
	for _, id := range ids {
		s := d.jobs[id]
		if s.Repository != "" && s.Repository != ev.Repository {
			continue
		}
		for ti, t := range s.Triggers {
			if t.Kind != ev.Kind || !githubArgsMatch(t, ev) {
				continue
			}
			run, enq, err := d.enqueue(s, ev.Kind, nil, tctx, idemKey(ev.DeliveryID, s.ID, fmt.Sprint(ti)))
			if err != nil {
				return out, err
			}
			if enq {
				out = append(out, run)
			}
		}
	}
	return out, nil
}

// enqueue applies the job's concurrency on_conflict policy then records the run.
// The second result reports whether a run was actually queued (a drop policy or an
// idempotency hit can suppress it).
func (d *Daemon) enqueue(s *spec.Spec, triggerKind string, inputs map[string]string, triggerContext, idem string) (store.Run, bool, error) {
	now := d.now()
	// Idempotency wins over on_conflict: a re-delivered trigger maps back to its
	// existing run instead of being counted as a fresh conflict.
	if existing, ok, err := d.store.RunByIdempotencyKey(idem); err != nil {
		return store.Run{}, false, err
	} else if ok {
		return existing, true, nil
	}
	key := d.concurrencyKey(s, inputs)
	switch s.Concurrency.OnConflict {
	case "drop":
		active, err := d.store.ActiveRunsByKey(key)
		if err != nil {
			return store.Run{}, false, err
		}
		if len(active) > 0 {
			return store.Run{}, false, nil
		}
	case "cancel-running":
		active, err := d.store.ActiveRunsByKey(key)
		if err != nil {
			return store.Run{}, false, err
		}
		for _, r := range active {
			if _, err := d.store.Cancel(r.ID, now); err != nil {
				return store.Run{}, false, err
			}
		}
	}
	nr := store.NewRun{
		JobID:            s.ID,
		TriggerKind:      triggerKind,
		ConcurrencyKey:   key,
		ConcurrencyLimit: s.Concurrency.Limit,
		IdempotencyKey:   idem,
		TriggerContext:   triggerContext,
		BudgetUSD:        s.Budget.Run,
		Timeout:          s.Timeout.Std(),
		MaxAttempts:      maxAttempts(s),
	}
	run, err := d.store.Enqueue(nr, now)
	return run, true, err
}

// RunOne claims and executes one ready run, applying the timeout and retry policy.
// ok is false when nothing was claimable. It is the unit Serve loops over.
func (d *Daemon) RunOne(ctx context.Context) (store.Run, bool, error) {
	run, ok, err := d.store.ClaimNext(d.workerID, d.now())
	if err != nil || !ok {
		return store.Run{}, false, err
	}
	// Fail closed if the claimed run's job is no longer loaded (spec deleted,
	// renamed, or rejected on a reload while runs for it remain queued): a missing
	// job must end the run, never pass a nil spec into the executor.
	job, ok := d.jobs[run.JobID]
	if !ok {
		out := store.Outcome{
			Status:       store.StatusFailed,
			FailureClass: store.FailureInvalidSpec,
			Error:        fmt.Sprintf("job spec %q not found", run.JobID),
		}
		if err := d.store.Finish(run.ID, out, d.now()); err != nil {
			return run, true, err
		}
		updated, err := d.store.Run(run.ID)
		return updated, true, err
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if run.TimeoutSecs > 0 {
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(run.TimeoutSecs)*time.Second)
		defer cancel()
	}
	out, execErr := d.exec.Execute(runCtx, run, job)
	if execErr != nil {
		out = store.Outcome{Status: store.StatusFailed, FailureClass: store.FailureInternal, Error: execErr.Error()}
	}
	now := d.now()

	if out.Status == store.StatusFailed && retryable(out.FailureClass) && run.Attempt < run.MaxAttempts {
		if err := d.store.FailAttempt(run.ID, out.FailureClass, out.Error, now); err != nil {
			return run, true, err
		}
		if _, err := d.store.Requeue(run.ID, now); err != nil {
			return run, true, err
		}
		updated, err := d.store.Run(run.ID)
		return updated, true, err
	}
	if err := d.store.Finish(run.ID, out, now); err != nil {
		return run, true, err
	}
	updated, err := d.store.Run(run.ID)
	return updated, true, err
}

// Tick advances the daemon once: recovers crashed-worker runs, fires due cron
// triggers, and drains every currently-claimable run. Serve calls it on an
// interval; tests call it directly.
func (d *Daemon) Tick(ctx context.Context, staleAfter time.Duration) error {
	if _, err := d.store.ReclaimStale(staleAfter, d.now()); err != nil {
		return err
	}
	if _, err := d.FireDueCron(d.now()); err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, ok, err := d.RunOne(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	}
}

// Serve runs the daemon until ctx is canceled, ticking every interval. Run
// execution is single-threaded per tick: bounded concurrency *across* jobs comes
// from the store's per-key limit, which is enough for the local dogfood daemon;
// a multi-worker pool (reusing the AS-132 pattern) is a later optimisation.
func (d *Daemon) Serve(ctx context.Context, interval, staleAfter time.Duration) error {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := d.Tick(ctx, staleAfter); err != nil && ctx.Err() == nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// Health is the daemon's run-count snapshot for `runs daemon health`.
type Health struct {
	Jobs   int
	Counts map[store.RunStatus]int
}

// Health reports loaded-job and per-status run counts.
func (d *Daemon) Health() (Health, error) {
	h := Health{Jobs: len(d.jobs), Counts: map[store.RunStatus]int{}}
	runs, err := d.store.Runs("")
	if err != nil {
		return Health{}, err
	}
	for _, r := range runs {
		h.Counts[r.Status]++
	}
	return h, nil
}

// Store exposes the underlying run store for the operator read/control verbs.
func (d *Daemon) Store() *store.Store { return d.store }

// --- helpers ---

var interpRe = regexp.MustCompile(`\$\{([^}]*)\}`)

// concurrencyKey interpolates a spec's concurrency.key against the closed variable
// namespace (§4.3). The key was validated at load, so an unknown variable here is
// already impossible; an unset trigger input resolves to empty.
func (d *Daemon) concurrencyKey(s *spec.Spec, inputs map[string]string) string {
	return interpRe.ReplaceAllStringFunc(s.Concurrency.Key, func(m string) string {
		name := strings.TrimSpace(interpRe.FindStringSubmatch(m)[1])
		switch name {
		case "repository":
			return s.Repository
		case "org":
			return s.Org
		case "id":
			return s.ID
		}
		if rest, ok := strings.CutPrefix(name, "trigger.inputs."); ok {
			return inputs[rest]
		}
		return ""
	})
}

// specView is the JSON projection persisted in jobs.spec_json for inspection.
func specView(s *spec.Spec) map[string]any {
	return map[string]any{
		"id": s.ID, "version": s.Version, "owner": s.Owner,
		"repository": s.Repository, "org": s.Org, "description": s.Description,
		"timeout": s.Timeout.String(), "budget_run": s.Budget.Run,
		"concurrency": map[string]any{"key": s.Concurrency.Key, "limit": s.Concurrency.Limit, "on_conflict": s.Concurrency.OnConflict},
		"triggers":    triggerKinds(s), "steps": len(s.Steps),
	}
}

func triggerKinds(s *spec.Spec) []string {
	out := make([]string, len(s.Triggers))
	for i, t := range s.Triggers {
		out[i] = t.Kind
	}
	return out
}

func hasTrigger(s *spec.Spec, kind string) bool {
	for _, t := range s.Triggers {
		if t.Kind == kind {
			return true
		}
	}
	return false
}

func githubArgsMatch(t spec.Trigger, ev GitHubEvent) bool {
	switch t.Kind {
	case "github.issue_labeled", "github.pr_labeled":
		label, _ := t.Args["label"].(string)
		return label == ev.Label
	case "github.pr_merged":
		base, _ := t.Args["base"].(string)
		return base == "" || base == ev.Base
	case "github.comment_command":
		cmd, _ := t.Args["command"].(string)
		return cmd == ev.Command
	default:
		return false
	}
}

// maxAttempts is 1 + the spec's retry count (a run with no retries gets one try).
func maxAttempts(s *spec.Spec) int {
	if s.Retries == nil {
		return 1
	}
	return s.Retries.Max + 1
}

// retryable reports whether a failure class is worth re-running. Deterministic,
// fail-closed failures (missing secret/permission, invalid spec, budget, blocked
// policy) are never retried — a retry would only repeat the same refusal or
// double-spend budget. Transient internal/timeout failures are.
func retryable(fc store.FailureClass) bool {
	switch fc {
	case store.FailureInternal, store.FailureTimeout:
		return true
	default:
		return false
	}
}

func idemKey(parts ...string) string { return strings.Join(parts, ":") }
