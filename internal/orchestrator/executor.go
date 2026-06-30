package orchestrator

import (
	"context"

	"github.com/tonitienda/agent-smith/internal/orchestrator/spec"
	"github.com/tonitienda/agent-smith/internal/orchestrator/store"
)

// Executor runs the cognitive + deterministic steps of one claimed run and reports
// a terminal outcome. It is the seam between the orchestrator's deterministic shell
// (this package: scheduling, leasing, retry, policy) and the work itself — agent
// steps over a provider (AS-150), GitHub action steps (AS-147/149), and the
// session-log write (AS-151). Those tickets supply the real executor; AS-161 ships
// the shell plus a stub so the run-control plane is testable end to end.
//
// An Executor must honour ctx cancellation/deadline (the daemon bounds each run by
// the spec timeout) and must classify failures via [store.FailureClass] so the
// daemon, operators, and failure hooks can tell a missing secret from a blocked
// policy from a real error. Returning a non-nil error is treated as an internal
// failure; returning a terminal Outcome (the normal path) lets the executor name
// the failure class itself.
type Executor interface {
	Execute(ctx context.Context, run store.Run, job *spec.Spec) (store.Outcome, error)
}

// StubExecutor is the MVP-0 executor: it performs no model or GitHub work and
// immediately succeeds, standing in until AS-147/149/150/151 wire real steps. It
// lets the daemon, scheduler, store, and operator surfaces be exercised offline.
type StubExecutor struct{}

// Execute marks the run succeeded with a placeholder session id.
func (StubExecutor) Execute(ctx context.Context, run store.Run, _ *spec.Spec) (store.Outcome, error) {
	if err := ctx.Err(); err != nil {
		return store.Outcome{Status: store.StatusFailed, FailureClass: store.FailureTimeout, Error: err.Error()}, nil
	}
	return store.Outcome{Status: store.StatusSucceeded, SessionID: "stub-" + run.ID}, nil
}
