// Package loop is the agentic turn loop that ties the substrate together
// (AS-018, PRD §7.2, D6): a user message is projected into model-facing context
// (AS-006), streamed to a provider (AS-008), the model's tool calls are
// dispatched through the runtime (AS-013) and their results fed back, and the
// turn repeats until the model stops or a guard fires.
//
// The loop owns no model state and imports no face: the context is re-projected
// from the append-only log every turn (never cached), the model is selected per
// request, and progress is reported through face-agnostic UIEvents (event.go) so
// the TUI (AS-021), a headless face (AS-051), and an ACP server (AS-052) reuse
// the same engine (§5). Every block — user, assistant text, reasoning, tool
// call, tool result — is appended to the log as the turn streams, so the log is
// the single live record and a crash or cancellation leaves a consistent,
// replayable history.
//
// Stop conditions are uniform: the model ending its turn, the user cancelling
// via ctx, a max-iteration safety valve against runaway tool loops, or a
// provider error surviving the retry/backoff policy. Cancellation mid-stream or
// mid-tool never leaves a tool_call without a result: any call appended in the
// abandoned turn is reconciled with a cancellation-marker tool_result before the
// loop returns.
package loop

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tonitienda/agent-smith/internal/budget"
	"github.com/tonitienda/agent-smith/internal/projection"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// producer is stamped on blocks the loop itself appends (assistant blocks
// assembled from the stream, cancellation markers) so their origin is
// self-describing on the log.
const producer = "agent-loop"

// StopMaxIterations is the stop reason surfaced when the max-iteration safety
// valve fires: the model kept requesting tools past the configured budget, so
// the loop halts a potential runaway rather than continuing forever.
const StopMaxIterations = "max_iterations"

// StopBudget is the stop reason surfaced when the session budget ceiling is
// reached (AS-041): at a turn boundary the recorded spend met or passed the
// ceiling, so the loop finished the in-flight turn's tool calls and halted
// rather than starting another priced turn. Enforcement is boundary-based, so
// the turn that crossed the ceiling may have carried the total slightly past it.
const StopBudget = "budget_exceeded"

// Defaults for the engine's guards and retry policy.
const (
	// DefaultMaxIterations bounds how many provider turns one Run may drive
	// before the safety valve fires. Generous enough for real multi-tool work,
	// finite enough to stop a runaway loop.
	DefaultMaxIterations = 50
	// DefaultMaxAttempts is how many times a single turn's request is attempted
	// before a retryable provider error is surfaced (1 try + retries).
	DefaultMaxAttempts = 3
	// DefaultBackoffBase is the base delay for exponential backoff between retry
	// attempts; attempt n waits base*2^(n-1), capped at DefaultBackoffMax.
	DefaultBackoffBase = 500 * time.Millisecond
	// DefaultBackoffMax caps a single backoff wait.
	DefaultBackoffMax = 30 * time.Second
)

// EventLog is the append-and-read seam the loop needs from the session's event
// log: it appends each new block as a turn streams and reads the live log back to
// project model-facing context. Satisfied by *eventlog.Log; declared here, at the
// consumer, so the loop depends on the two methods it uses rather than the whole
// log type (AS-091).
type EventLog interface {
	Append(b schema.Block) (schema.Block, error)
	Events() []schema.Block
}

// ToolExecutor runs a batch of the model's client tool calls. Satisfied by
// *tool.Runtime; the loop only ever dispatches a batch, so it depends on that one
// method rather than the whole runtime (AS-091).
type ToolExecutor interface {
	ExecuteBatch(ctx context.Context, calls []schema.Block, hooks tool.BatchHooks) ([]schema.Block, error)
}

// ToolDefs supplies the registered tools' provider-facing definitions for each
// request. Satisfied by *tool.Registry; the loop only reads the definitions, so
// it depends on that one method rather than the whole registry (AS-091).
type ToolDefs interface {
	ProviderDefs() []provider.ToolDef
}

// Engine drives turns for one session against one provider. It is constructed
// once and reused across turns; it holds no mutable state of its own, so the
// log and the projection are the session's single source of truth.
type Engine struct {
	provider provider.Provider
	log      EventLog
	runtime  ToolExecutor
	registry ToolDefs
	model    string

	observer    Observer
	params      provider.SamplingParams
	cache       provider.CacheHints
	projectOpts projection.Options

	// project assembles the model-facing context for a turn from the current log.
	// It defaults to Smith's projection (exclusions honored, reasoning-replay
	// filtered for projectOpts.TargetModel); WithProjector overrides it so a
	// caller can drive the same loop with a different context policy — e.g. the
	// D5 benchmark's naive baseline harness (AS-030), which sends the raw window
	// with no context management for an apples-to-apples comparison.
	project Projector

	maxIterations int
	maxAttempts   int
	backoffBase   time.Duration
	backoffMax    time.Duration

	// Budget enforcement (AS-041). spent reports the session's total dollar spend
	// so far (nil disables enforcement); budgetDefaultUSD is the configured
	// default ceiling applied when the log carries no /budget override, and
	// budgetWarnFraction is the fraction of the ceiling at which warnings begin.
	spent              func() float64
	budgetDefaultUSD   float64
	budgetWarnFraction float64

	// Conservative pre-turn enforcement (AS-086). reserve estimates the next
	// turn's worst-case cost from the context about to be sent (nil disables the
	// strict path, leaving AS-041's boundary check as the only guard); ok is false
	// when the active model cannot be priced, in which case haltUnpriced decides
	// whether the run stops rather than spending blind.
	reserve      BudgetReservation
	haltUnpriced bool
}

// Option configures an Engine.
type Option func(*Engine)

// Projector assembles the model-facing context for the next turn from the
// current log events. It returns the live blocks the provider request carries,
// in order. The default (Smith's projection) honors exclusion/derived events
// and reasoning-replay scope; a custom projector lets the same loop run a
// different context policy (AS-030's naive baseline).
type Projector func(events []schema.Block) []schema.Block

// WithProjector overrides how the loop assembles each turn's context. A nil
// projector is ignored (the default projection stands). Used by the benchmark
// suite (AS-030) to drive a naive, no-context-management baseline through the
// same loop as the Smith path.
func WithProjector(p Projector) Option {
	return func(e *Engine) {
		if p != nil {
			e.project = p
		}
	}
}

// WithObserver sets the sink for face-agnostic UIEvents. A nil observer is
// ignored (the default no-op stands).
func WithObserver(o Observer) Option {
	return func(e *Engine) {
		if o != nil {
			e.observer = o
		}
	}
}

// WithMaxIterations sets the max-iteration safety valve. A non-positive n is
// ignored, keeping the default.
func WithMaxIterations(n int) Option {
	return func(e *Engine) {
		if n > 0 {
			e.maxIterations = n
		}
	}
}

// WithRetry sets the per-turn retry policy: maxAttempts total attempts (1 means
// no retry) and the exponential backoff base. Non-positive values are ignored.
func WithRetry(maxAttempts int, backoffBase time.Duration) Option {
	return func(e *Engine) {
		if maxAttempts > 0 {
			e.maxAttempts = maxAttempts
		}
		if backoffBase > 0 {
			e.backoffBase = backoffBase
		}
	}
}

// WithBudget installs budget enforcement (AS-041). spent reports the session's
// total dollar spend so far (typically cost.Summarize over the log); the loop
// consults it at each turn boundary and, against the active ceiling, warns near
// the limit and halts once spend reaches it. defaultLimitUSD is the configured
// default ceiling used when the log carries no /budget override; warnFraction is
// the fraction of the ceiling at which warnings begin (0 falls back to the
// package default). A nil spent leaves enforcement disabled.
//
// Enforcement is boundary-based and only as accurate as spent: the cost of a
// turn is known only after it completes, so a single turn can carry the total
// slightly past the ceiling before the next boundary halts the run, and a turn
// the pricing table cannot price contributes $0 to spent — an unpriced model is
// effectively unmetered. WithBudgetReservation (AS-086) closes both gaps with a
// conservative pre-turn estimate; without it this boundary path is the only
// guard and remains the graceful fallback when no estimate is available.
func WithBudget(spent func() float64, defaultLimitUSD, warnFraction float64) Option {
	return func(e *Engine) {
		if spent != nil {
			e.spent = spent
			e.budgetDefaultUSD = defaultLimitUSD
			e.budgetWarnFraction = warnFraction
		}
	}
}

// BudgetReservation estimates the worst-case dollar cost of the next turn from
// the model-facing context about to be sent (typically cost.EstimateTurnCostUSD:
// request-size input at the input rate plus the model's max output at the output
// rate). ok is false when the active model cannot be priced, so the loop handles
// the unpriced bypass conservatively instead of treating an unmetered turn as
// free.
type BudgetReservation func(ctx []schema.Block) (worstCaseUSD float64, ok bool)

// WithBudgetReservation installs conservative pre-turn budget enforcement
// (AS-086) on top of WithBudget's boundary check, closing its two honest gaps:
//
//   - Single-turn overshoot: before issuing a turn the loop reserves reserve's
//     worst-case estimate against the remaining budget and halts before the turn
//     if spend plus the reservation would reach the ceiling, so the total cannot
//     overshoot rather than catching it one boundary late.
//   - Unpriced-model bypass: when reserve reports ok=false the model is unpriced
//     and its spend is invisible to the guard; the loop surfaces a one-time
//     UIBudgetUnpriced notice and, when haltUnpriced is set, stops rather than
//     spending blind. Otherwise the turn proceeds after the notice.
//
// It is a no-op unless WithBudget also installed a spend source and a ceiling is
// active; a nil reserve leaves only the boundary check, so behavior degrades
// gracefully when no estimate is available.
func WithBudgetReservation(reserve BudgetReservation, haltUnpriced bool) Option {
	return func(e *Engine) {
		e.reserve = reserve
		e.haltUnpriced = haltUnpriced
	}
}

// WithSamplingParams sets the sampling knobs sent on every turn's request.
func WithSamplingParams(p provider.SamplingParams) Option {
	return func(e *Engine) { e.params = p }
}

// WithCacheHints sets the cache hints sent on every turn's request (AS-011).
func WithCacheHints(c provider.CacheHints) Option {
	return func(e *Engine) { e.cache = c }
}

// New builds an Engine over the given provider, log, tool runtime, and registry,
// issuing every turn against model. It returns an error if any required
// dependency is missing, so misconfiguration fails at construction rather than
// mid-turn.
func New(p provider.Provider, log EventLog, rt ToolExecutor, reg ToolDefs, model string, opts ...Option) (*Engine, error) {
	switch {
	case p == nil:
		return nil, errors.New("loop: provider is required")
	case log == nil:
		return nil, errors.New("loop: event log is required")
	case rt == nil:
		return nil, errors.New("loop: tool runtime is required")
	case reg == nil:
		return nil, errors.New("loop: tool registry is required")
	case model == "":
		return nil, errors.New("loop: model is required")
	}
	e := &Engine{
		provider:      p,
		log:           log,
		runtime:       rt,
		registry:      reg,
		model:         model,
		observer:      func(UIEvent) {},
		projectOpts:   projection.Options{TargetModel: model},
		maxIterations: DefaultMaxIterations,
		maxAttempts:   DefaultMaxAttempts,
		backoffBase:   DefaultBackoffBase,
		backoffMax:    DefaultBackoffMax,
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.project == nil {
		// Default: Smith's projection over the log (exclusions honored, reasoning
		// replay filtered for the target model). Resolved after options so a custom
		// projectOpts.TargetModel is respected.
		opts := e.projectOpts
		e.project = func(events []schema.Block) []schema.Block {
			return projection.Project(events, opts).Live()
		}
	}
	return e, nil
}

// Result reports how a Run ended. StopReason is the provider's terminal stop
// reason (e.g. end_turn, max_tokens) or a loop-defined one (StopMaxIterations);
// Iterations counts the provider turns driven; FinalText concatenates the
// visible assistant text of the final turn.
type Result struct {
	StopReason string
	Iterations int
	FinalText  string
}

// Run records userText as a user message and drives the turn loop to a stop
// condition, returning how it ended. It appends the user block, then repeatedly
// projects the log, streams a turn, and dispatches the model's client tool calls
// until the model stops, a guard fires, or an error/cancellation ends the run.
//
// On cancellation (ctx) it returns the context error alongside a Result marking
// the cancellation, having reconciled any tool_call appended in the abandoned
// turn with a cancellation-marker result so the log stays consistent. A
// surfaced provider error (after retries) is returned with the partial Result.
func (e *Engine) Run(ctx context.Context, userText string) (Result, error) {
	user := schema.Block{
		ID:   schema.NewID(),
		Kind: schema.KindText,
		Role: schema.RoleUser,
		Text: &schema.TextBody{Text: userText, Subtype: schema.TextSubtypeNormal},
	}
	if _, err := e.log.Append(user); err != nil {
		return Result{}, fmt.Errorf("loop: append user message: %w", err)
	}
	return e.drive(ctx)
}

// drive runs the turn loop over the current log until a stop condition.
func (e *Engine) drive(ctx context.Context) (Result, error) {
	res := Result{}
	warned := false
	unpricedWarned := false
	// The ceiling is static for the life of one Run — /budget cannot be issued
	// mid-turn — so resolve it once (a /budget override on the log wins over the
	// configured default) rather than rescanning the log every iteration. Only the
	// spend it is measured against changes per turn.
	limit := e.budgetDefaultUSD
	if e.spent != nil {
		if v, ok := budget.Current(e.log.Events()); ok {
			limit = v
		}
	}
	for iter := 0; ; iter++ {
		if err := ctx.Err(); err != nil {
			res.StopReason = StopCanceled
			return res, err
		}
		if iter >= e.maxIterations {
			res.StopReason = StopMaxIterations
			return res, nil
		}
		// Context is assembled once per iteration and reused for both the pre-turn
		// budget reservation (AS-086, which prices the window about to be sent) and
		// the request itself, so the estimate is measured against exactly what the
		// model receives. The projector is Smith's projection by default and the
		// naive baseline under the benchmark (AS-030).
		live := e.project(e.log.Events())

		// Budget enforcement (AS-041) runs at the turn boundary: the prior turn's
		// usage is already recorded on the log, and any in-flight tool call has
		// already been dispatched, so halting here finishes work in flight and
		// stops before starting another priced turn. The pre-turn reservation
		// (AS-086) then tightens this to halt *before* a turn whose worst-case cost
		// would overshoot, and to surface an unpriced model rather than spend blind.
		if e.spent != nil {
			guard := budget.Guard{LimitUSD: limit, WarnFraction: e.budgetWarnFraction}
			if guard.Enabled() {
				spent := e.spent()
				switch guard.Check(spent) {
				case budget.Halt:
					e.emit(UIEvent{Kind: UIBudgetHalt, Iteration: iter, BudgetSpentUSD: spent, BudgetLimitUSD: limit})
					res.StopReason = StopBudget
					return res, nil
				case budget.Warn:
					if !warned {
						warned = true
						e.emit(UIEvent{Kind: UIBudgetWarning, Iteration: iter, BudgetSpentUSD: spent, BudgetLimitUSD: limit})
					}
				}
				if e.reserve != nil {
					if worst, ok := e.reserve(live); ok {
						// Halt before issuing a turn whose worst-case cost would carry the
						// total to or past the ceiling, so spend cannot overshoot.
						if guard.Check(spent+worst) == budget.Halt {
							e.emit(UIEvent{Kind: UIBudgetHalt, Iteration: iter, BudgetSpentUSD: spent, BudgetLimitUSD: limit})
							res.StopReason = StopBudget
							return res, nil
						}
					} else {
						// The active model is unpriced: its spend is invisible to the guard.
						// Surface that once, and halt instead of spending blind when configured.
						if !unpricedWarned {
							unpricedWarned = true
							e.emit(UIEvent{Kind: UIBudgetUnpriced, Iteration: iter, BudgetSpentUSD: spent, BudgetLimitUSD: limit})
						}
						if e.haltUnpriced {
							res.StopReason = StopBudget
							return res, nil
						}
					}
				}
			}
		}

		req := e.requestFrom(live)
		turn, err := e.streamTurn(ctx, iter, req)
		res.Iterations = iter + 1
		res.StopReason = turn.stopReason
		res.FinalText = turn.text
		if err != nil {
			// The turn was abandoned (cancelled or a surfaced provider error):
			// reconcile any tool_call appended this turn so none is orphaned.
			e.reconcile(turn.clientCalls)
			if ctx.Err() != nil {
				res.StopReason = StopCanceled
			}
			return res, err
		}

		if !needsToolDispatch(turn) {
			return res, nil
		}
		if err := e.dispatch(ctx, iter, turn.clientCalls); err != nil {
			if ctx.Err() != nil {
				res.StopReason = StopCanceled
			}
			return res, err
		}
	}
}

// requestFrom builds the provider request for the next turn from the already-
// assembled live context: the model-facing blocks, the registered tools, and the
// engine's per-turn sampling and cache settings. drive assembles context once per
// iteration (so the budget reservation and the request see the same window) and
// hands the result here; context is always re-assembled per turn from the log —
// never cached state (PRD D3).
func (e *Engine) requestFrom(live []schema.Block) provider.Request {
	return provider.Request{
		Model:   e.model,
		Context: live,
		Tools:   e.registry.ProviderDefs(),
		Params:  e.params,
		Cache:   e.cache,
	}
}

// needsToolDispatch reports whether the turn ended asking for client tools the
// loop must run before continuing. A tool_use stop with no client calls (e.g. a
// server-tool turn) is terminal here: there is nothing for the loop to dispatch.
func needsToolDispatch(t turnResult) bool {
	return t.stopReason == provider.StopToolUse && len(t.clientCalls) > 0
}

// dispatch runs a turn's client tool calls through the runtime, which executes
// the independent ones concurrently while recording their results on the log in
// call order (AS-019). It emits UIToolStarted/UIToolFinished per call in that
// order. On cancellation or an infrastructure failure the runtime stops recording
// and returns the error; dispatch then reconciles every call not already answered
// with a cancellation marker, so the log never carries an orphaned tool_call.
func (e *Engine) dispatch(ctx context.Context, iter int, calls []schema.Block) error {
	hooks := tool.BatchHooks{
		Started: func(_ int, call schema.Block) {
			e.emit(UIEvent{Kind: UIToolStarted, Iteration: iter, Tool: toolEvent(call, nil)})
		},
		Finished: func(_ int, call schema.Block, result *schema.ToolResultBody) {
			e.emit(UIEvent{Kind: UIToolFinished, Iteration: iter, Tool: toolEvent(call, result)})
		},
	}
	if _, err := e.runtime.ExecuteBatch(ctx, calls, hooks); err != nil {
		e.reconcile(calls)
		return err
	}
	return nil
}

// reconcile appends a cancellation-marker tool_result for every call that has
// not already been answered on the log, so an abandoned turn never leaves a
// tool_call without a matching result (the consistency guarantee of AS-018). It
// is idempotent and best-effort: a call already answered is skipped, and a
// failed marker append is ignored because the turn is already being abandoned.
//
// The set of already-answered tool_use IDs is built once from a single log
// snapshot — keeping the cost O(N + M) for a turn with M calls over an N-event
// log rather than re-scanning the log per call — and is updated as markers are
// appended so a duplicated call in calls is not answered twice.
func (e *Engine) reconcile(calls []schema.Block) {
	if len(calls) == 0 {
		return
	}
	answered := make(map[string]bool)
	for _, b := range e.log.Events() {
		if b.Kind == schema.KindToolResult && b.ToolResult != nil {
			answered[b.ToolResult.ToolUseID] = true
		}
	}
	for _, call := range calls {
		if call.ToolCall == nil || answered[call.ToolCall.ToolUseID] {
			continue
		}
		marker := schema.Block{
			ID:   schema.NewID(),
			Kind: schema.KindToolResult,
			Role: schema.RoleTool,
			Provenance: &schema.Provenance{
				Producer:    producer,
				DerivedFrom: []string{call.ID},
			},
			Attribution: &schema.Attribution{Tool: call.ToolCall.Name},
			ToolResult: &schema.ToolResultBody{
				ToolUseID: call.ToolCall.ToolUseID,
				Content:   []schema.Part{{Type: "text", Text: "tool call canceled before completion"}},
				IsError:   true,
			},
		}
		if _, err := e.log.Append(marker); err == nil {
			answered[call.ToolCall.ToolUseID] = true
		}
	}
}

// emit forwards ev to the observer.
func (e *Engine) emit(ev UIEvent) { e.observer(ev) }

// toolEvent builds the UI payload for a tool call, optionally with its result.
func toolEvent(call schema.Block, result *schema.ToolResultBody) *ToolEvent {
	te := &ToolEvent{Result: result}
	if call.ToolCall != nil {
		te.ToolUseID = call.ToolCall.ToolUseID
		te.Name = call.ToolCall.Name
		te.Arguments = call.ToolCall.Arguments
	}
	return te
}

// firstNonZero returns a when non-empty, else b.
func firstNonZero(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
