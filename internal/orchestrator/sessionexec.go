package orchestrator

// SessionExecutor is the AS-151 wiring that makes every orchestrated run a normal
// Smith session. It decorates an inner Executor: it opens a Recorder (a fresh
// Smith session stamped with the run's linkage and an opening block), runs the
// inner executor's real work, then records the terminal outcome and hands back an
// Outcome whose SessionID points at that session — so the run store links to a
// readable, resumable session and /cost, /insights, and replay reach it through
// the existing readers with no second observability path (ADR D-ORCH-4).
//
// The inner executor is where AS-149/150 land the real GitHub and provider steps.
// Those step executors record their own policy decisions, GitHub actions, and
// provider usage against the run's session; the seam for that is the exported
// Recorder, which a future StepExecutor takes so its blocks land in the same log.
// Until then the inner is the MVP-0 StubExecutor and the session carries just the
// run-start and run-outcome lifecycle — already enough for the run to be a
// first-class, priceable session.

import (
	"context"
	"errors"

	"github.com/tonitienda/agent-smith/internal/orchestrator/spec"
	"github.com/tonitienda/agent-smith/internal/orchestrator/store"
	"github.com/tonitienda/agent-smith/internal/session"
)

// SessionExecutor persists each run as a Smith session around an inner Executor.
// The zero value is invalid; use NewSessionExecutor.
type SessionExecutor struct {
	sessions *session.Store
	inner    Executor
	actions  GitHubActions // optional AS-147 deterministic-hook port; nil = no hooks
}

// NewSessionExecutor wraps inner so its runs are recorded as Smith sessions in
// the given session store. A nil inner defaults to the MVP-0 StubExecutor, so the
// decorator is usable on its own to prove the session seam end to end.
func NewSessionExecutor(sessions *session.Store, inner Executor) *SessionExecutor {
	if inner == nil {
		inner = StubExecutor{}
	}
	return &SessionExecutor{sessions: sessions, inner: inner}
}

// WithGitHubActions wires the deterministic-hook port so the executor runs a job's
// github.* lifecycle hooks (add/remove label, comment, set status) around the
// inner work, recording each on the run's session (AS-147). It returns the
// executor for chaining. Left unset (or nil), hooks are skipped — an orchestrator
// with no GitHub credentials still runs every job's cognitive work cleanly.
func (e *SessionExecutor) WithGitHubActions(actions GitHubActions) *SessionExecutor {
	e.actions = actions
	return e
}

// Execute records the run as a session, delegates the real work to the inner
// executor, appends the terminal outcome, and returns an Outcome pointing at the
// recorded session. A failure to open or write the session is an internal error
// (returned) — the run is not silently detached from its narrative. If the inner
// executor errors, that error is preserved after the outcome is still recorded as
// a failure, so a crashed run remains inspectable.
func (e *SessionExecutor) Execute(ctx context.Context, run store.Run, job *spec.Spec) (store.Outcome, error) {
	rec, err := NewRecorder(e.sessions, run, job)
	if err != nil {
		return store.Outcome{}, err
	}
	tc := decodeTriggerContext(run.TriggerContext)

	// on_start hooks run before the cognitive work. A failing on_start hook (e.g.
	// a required "in-progress" label could not be set) fails the run closed: the
	// inner work never starts, and the failure is recorded on the session and, via
	// the terminal outcome, in the run store.
	out, execErr := store.Outcome{}, error(nil)
	if hookErr := runHooks(ctx, e.actions, rec, hooksAt(job, "on_start"), tc); hookErr != nil {
		out, execErr = store.Outcome{Status: store.StatusFailed, FailureClass: store.FailureInternal, Error: hookErr.Error()}, hookErr
	} else {
		out, execErr = e.inner.Execute(ctx, run, job)
	}
	if execErr != nil {
		// The inner failed without naming a terminal outcome; mark it a failure so
		// the session still closes with a terminal block, but preserve any fields it
		// did accumulate before failing (e.g. a partial CostUSD or a named class).
		if !out.Status.Terminal() {
			out.Status = store.StatusFailed
		}
		if out.FailureClass == store.FailureNone {
			out.FailureClass = store.FailureInternal
		}
		if out.Error == "" {
			out.Error = execErr.Error()
		}
	}
	// Point the outcome at the run's real session, replacing any placeholder id the
	// inner returned (the stub returns "stub-<run>").
	out.SessionID = rec.SessionID()

	// Terminal hooks (on_success/on_failure/on_cancel) run after the work concludes
	// but before the session closes, so their record lands in the same log. Unlike
	// on_start they are bookkeeping: a failing terminal hook does not rewrite the
	// run's already-decided status (the work is done) — it is recorded on the
	// session and surfaced alongside execErr, never swallowed.
	if hookErr := runHooks(ctx, e.actions, rec, hooksAt(job, terminalHookPoint(out.Status)), tc); hookErr != nil {
		execErr = errors.Join(execErr, hookErr)
	}

	if err := rec.Finish(out); err != nil {
		// Preserve the original execution error alongside the finish error.
		return out, errors.Join(execErr, err)
	}
	return out, execErr
}

// hooksAt returns the deterministic hook steps a job declares at point, or nil
// when the job (or the point) declares none. A nil job (the run store lost the
// spec) has no hooks.
func hooksAt(job *spec.Spec, point string) []spec.Step {
	if job == nil || point == "" {
		return nil
	}
	return job.Hooks[point]
}

// terminalHookPoint maps a run's terminal status to the lifecycle hook point that
// fires for it. A non-terminal status (should not reach here) fires no hook.
func terminalHookPoint(status store.RunStatus) string {
	switch status {
	case store.StatusSucceeded:
		return "on_success"
	case store.StatusCanceled:
		return "on_cancel"
	case store.StatusFailed:
		return "on_failure"
	default:
		return ""
	}
}
