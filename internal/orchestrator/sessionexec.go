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

	"github.com/tonitienda/agent-smith/internal/orchestrator/spec"
	"github.com/tonitienda/agent-smith/internal/orchestrator/store"
	"github.com/tonitienda/agent-smith/internal/session"
)

// SessionExecutor persists each run as a Smith session around an inner Executor.
// The zero value is invalid; use NewSessionExecutor.
type SessionExecutor struct {
	sessions *session.Store
	inner    Executor
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

	out, execErr := e.inner.Execute(ctx, run, job)
	if execErr != nil {
		// The inner failed without naming a terminal outcome; record an internal
		// failure so the session still closes with a terminal block.
		out = store.Outcome{Status: store.StatusFailed, FailureClass: store.FailureInternal, Error: execErr.Error()}
	}
	// Point the outcome at the run's real session, replacing any placeholder id the
	// inner returned (the stub returns "stub-<run>").
	out.SessionID = rec.SessionID()

	if err := rec.Finish(out); err != nil {
		return out, err
	}
	return out, execErr
}
