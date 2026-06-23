package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

// producer is stamped on every tool_result block's provenance so its origin is
// self-describing on the log.
const producer = "tool-runtime"

// hookProducer is stamped on a tool_call derived from a pre-tool-use hook's
// argument rewrite (AS-035), so the modification's origin is auditable on the log.
const hookProducer = "hook"

// DefaultTimeout is the per-tool execution budget when neither the tool's Def
// nor the Runtime overrides it.
const DefaultTimeout = 60 * time.Second

// DefaultMaxResultBytes caps the text content of a tool_result before it is
// truncated with an explicit marker. Oversized tool output is a top context-bloat
// source (PRD §2), so the runtime bounds it by default.
const DefaultMaxResultBytes = 32 * 1024

// DefaultMaxParallel bounds how many of one turn's independent tool calls
// ExecuteBatch runs at once (AS-019). It caps fan-out so a turn that emits many
// calls cannot spawn an unbounded number of goroutines — and with them an
// unbounded number of subprocesses or open files — at the same instant.
const DefaultMaxParallel = 8

// Runtime executes a model's tool calls: it validates arguments against the
// tool's schema, gates execution through the permission hook (AS-016), runs the
// tool under cancellation and a per-tool timeout, truncates oversized output, and
// records a tool_result block onto the log (AS-005) linked to its call. It holds
// no mutable state after construction; ExecuteBatch builds on that to run a
// turn's independent calls concurrently while keeping the log deterministic
// (AS-019).
type Runtime struct {
	registry    *Registry
	log         *eventlog.Log
	permission  PermissionFunc
	preHook     PreToolHook
	postHook    PostToolHook
	timeout     time.Duration
	maxBytes    int
	maxParallel int
}

// Option configures a Runtime.
type Option func(*Runtime)

// WithPermission sets the permission hook invoked before every execution. A nil
// hook is ignored (the default AllowAll stands).
func WithPermission(fn PermissionFunc) Option {
	return func(r *Runtime) {
		if fn != nil {
			r.permission = fn
		}
	}
}

// WithPreToolHook sets the lifecycle hook invoked after the permission gate and
// before a tool runs (AS-035). It may block the call or rewrite its arguments. A
// nil hook is ignored, leaving the runtime hook-free.
func WithPreToolHook(fn PreToolHook) Option {
	return func(r *Runtime) {
		if fn != nil {
			r.preHook = fn
		}
	}
}

// WithPostToolHook sets the lifecycle hook invoked after a tool's result is
// recorded (AS-035). It is observational only. A nil hook is ignored.
func WithPostToolHook(fn PostToolHook) Option {
	return func(r *Runtime) {
		if fn != nil {
			r.postHook = fn
		}
	}
}

// WithTimeout sets the default per-tool execution budget. A tool's own
// Def().Timeout, when set, overrides it. A non-positive d is ignored.
func WithTimeout(d time.Duration) Option {
	return func(r *Runtime) {
		if d > 0 {
			r.timeout = d
		}
	}
}

// WithMaxResultBytes sets the truncation threshold for tool_result text content.
// A non-positive n disables truncation.
func WithMaxResultBytes(n int) Option {
	return func(r *Runtime) { r.maxBytes = n }
}

// WithMaxParallel sets how many of one turn's independent tool calls ExecuteBatch
// runs concurrently (AS-019). A non-positive n is ignored, keeping the default.
func WithMaxParallel(n int) Option {
	return func(r *Runtime) {
		if n > 0 {
			r.maxParallel = n
		}
	}
}

// NewRuntime builds a Runtime over registry and log. The log receives every
// tool_result (and, as a safety net, any tool_call not already on it). Without
// WithPermission the runtime permits every call (AllowAll); AS-016 injects the
// real policy.
func NewRuntime(registry *Registry, log *eventlog.Log, opts ...Option) *Runtime {
	r := &Runtime{
		registry:    registry,
		log:         log,
		permission:  AllowAll,
		timeout:     DefaultTimeout,
		maxBytes:    DefaultMaxResultBytes,
		maxParallel: DefaultMaxParallel,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Execute runs the tool named by the tool_call block call, appends its
// tool_result to the log, and returns that result block. call must be a
// KindToolCall block; if it is not yet on the log it is appended first, so a
// completed Execute always leaves a tool_call + tool_result pair, the result
// linked to its call by ToolUseID and provenance (DerivedFrom the call).
//
// A failure the model should see — an unknown tool, invalid arguments, a denied
// permission, an elapsed per-tool timeout, or a tool reporting a domain error —
// is recorded as a tool_result with IsError set and returned with a nil Go
// error, so the loop feeds it back to the model. A non-nil Go error is reserved
// for an infrastructure failure (call is not a tool_call, the log rejects a
// write) or cancellation of the surrounding turn via ctx, where no result is
// recorded because the turn is being abandoned.
func (r *Runtime) Execute(ctx context.Context, call schema.Block) (schema.Block, error) {
	if call.Kind != schema.KindToolCall || call.ToolCall == nil {
		return schema.Block{}, fmt.Errorf("tool: Execute requires a %s block, got %q", schema.KindToolCall, call.Kind)
	}

	p, err := r.prepare(ctx, call)
	if err != nil {
		return schema.Block{}, err
	}
	if p.deny != nil {
		return r.record(p.call, *p.deny)
	}

	out, runErr := r.run(ctx, p.tool, p.call.ToolCall.Arguments, p.call.ToolCall.ToolUseID)
	return r.finishCall(ctx, p, out, runErr)
}

// plan is one call's state after the serial gating phase (prepare): the on-log
// tool_call, and exactly one of tool (the call is approved and runnable) or deny
// (the call resolved to a terminal result during gating — unknown tool, invalid
// arguments, or denied permission — that is recorded as-is without running).
type plan struct {
	call schema.Block
	tool Tool
	deny *Output
}

// prepare runs the serial gating phase for one call: it ensures the tool_call is
// on the log, then checks cancellation, resolves the tool, validates arguments,
// and asks the permission hook. A non-nil error means the surrounding turn was
// abandoned (cancelled) or the log rejected the call — no work should start and
// nothing is recorded. Otherwise it returns a plan that either carries a runnable
// tool or a terminal deny Output for the caller to record.
func (r *Runtime) prepare(ctx context.Context, call schema.Block) (plan, error) {
	logged, err := r.ensureLogged(call)
	if err != nil {
		return plan{}, err
	}
	tc := logged.ToolCall

	// An already-cancelled turn should not start new work.
	if err := ctx.Err(); err != nil {
		return plan{}, err
	}

	t, ok := r.registry.Get(tc.Name)
	if !ok {
		return r.denyPlan(logged, "unknown tool %q", tc.Name), nil
	}
	if err := validateArgs(t.Def().InputSchema, tc.Arguments); err != nil {
		return r.denyPlan(logged, "invalid arguments for tool %q: %v", tc.Name, err), nil
	}

	gate := Call{ToolUseID: tc.ToolUseID, Name: tc.Name, Arguments: tc.Arguments}
	if d := r.permission(ctx, gate); !d.Allow {
		return r.denyPlan(logged, "permission denied for tool %q: %s", tc.Name, d.Reason), nil
	}

	// Pre-tool-use hook (AS-035): runs after the permission gate. It may block the
	// call or rewrite its arguments. Hooks are automation, not the security
	// boundary — permission above is the gate.
	if r.preHook != nil {
		res := r.preHook(ctx, gate)
		if res.Block {
			reason := res.Reason
			if reason == "" {
				reason = "no reason given"
			}
			return r.denyPlan(logged, "tool %q blocked by hook: %s", tc.Name, reason), nil
		}
		if res.Modified != nil {
			modified, err := r.applyModifiedArgs(logged, t, res.Modified)
			if err != nil {
				return r.denyPlan(logged, "hook rewrote tool %q with invalid arguments: %v", tc.Name, err), nil
			}
			logged = modified
		}
	}
	return plan{call: logged, tool: t}, nil
}

// applyModifiedArgs records a pre-tool hook's rewrite of a call's arguments as a
// derived tool_call on the log (provenance: the hook), so the modification is
// visible and auditable (PRD D3) while the original is excluded from the
// projection. The rewritten arguments are validated against the tool's schema
// first, so a hook cannot smuggle a malformed call past validation. It returns
// the derived, on-log tool_call the runtime then executes and links the result
// to.
func (r *Runtime) applyModifiedArgs(call schema.Block, t Tool, args json.RawMessage) (schema.Block, error) {
	if err := validateArgs(t.Def().InputSchema, args); err != nil {
		return schema.Block{}, err
	}
	derived := call
	derived.ID = ""
	derived.TS = time.Time{} // let Derive stamp the rewrite with its own append time
	tc := *call.ToolCall
	tc.Arguments = append(json.RawMessage(nil), args...)
	// Drop the original verbatim arguments string: providers prefer ArgumentsRaw
	// when present, so leaving it would serialize the pre-rewrite arguments and
	// silently undo the hook's modification.
	tc.ArgumentsRaw = ""
	derived.ToolCall = &tc
	derived = eventlog.Derive(derived, hookProducer, call.ID)
	stored, err := r.log.Append(derived)
	if err != nil {
		return schema.Block{}, fmt.Errorf("tool: log hook-modified tool_call: %w", err)
	}
	return stored, nil
}

// denyPlan builds a terminal plan whose deny Output records a model-readable
// error for a call that did not pass gating.
func (r *Runtime) denyPlan(call schema.Block, format string, args ...any) plan {
	out := errorOutput(format, args...)
	return plan{call: call, deny: &out}
}

// finishCall turns a runnable plan's execution outcome into a recorded
// tool_result. A nil runErr records the tool's Output. On a non-nil runErr it
// distinguishes abandonment of the whole turn (ctx cancelled: propagate the error
// and record nothing, because the turn is being abandoned) from a per-tool
// timeout or a tool failure (record a model-readable error so the model can
// react). ctx is the surrounding turn's context, not the per-tool budget.
func (r *Runtime) finishCall(ctx context.Context, p plan, out Output, runErr error) (schema.Block, error) {
	var (
		res schema.Block
		err error
	)
	switch {
	case runErr == nil:
		res, err = r.record(p.call, out)
	case ctx.Err() != nil:
		return schema.Block{}, ctx.Err()
	case errors.Is(runErr, context.DeadlineExceeded):
		res, err = r.record(p.call, errorOutput("tool %q timed out after %s", p.call.ToolCall.Name, r.budget(p.tool)))
	default:
		res, err = r.record(p.call, errorOutput("tool %q failed: %v", p.call.ToolCall.Name, runErr))
	}
	if err != nil {
		return schema.Block{}, err
	}
	r.firePostHook(ctx, p.call, res.ToolResult)
	return res, nil
}

// firePostHook invokes the post-tool-use hook (AS-035) with the executed call
// and its recorded result. It is observational — the result is already on the
// log — so a nil hook or a nil result is simply a no-op.
func (r *Runtime) firePostHook(ctx context.Context, call schema.Block, result *schema.ToolResultBody) {
	if r.postHook == nil || call.ToolCall == nil || result == nil {
		return
	}
	r.postHook(ctx, Call{
		ToolUseID: call.ToolCall.ToolUseID,
		Name:      call.ToolCall.Name,
		Arguments: call.ToolCall.Arguments,
	}, result)
}

// ExecuteBatch runs one assistant turn's client tool calls, executing the
// independent ones concurrently while keeping the log deterministic (AS-019):
//
//   - Gating (cancellation check, argument validation, and the permission hook)
//     runs serially in call order, so in ask mode permission prompts happen one
//     at a time and in a predictable order — a denial never cancels a sibling
//     already approved.
//   - Approved tools then run concurrently under a bounded worker pool; a failing
//     or timing-out tool records its own error result and does not cancel its
//     siblings.
//   - Results are appended to the log in the original call order regardless of
//     which tool finished first, so the recorded history is reproducible.
//
// hooks, when set, are notified as each call starts and finishes, in call order,
// so a face can show progress. The returned blocks are the recorded tool_results
// aligned with calls. A non-nil error means the turn was abandoned (ctx cancelled)
// or the log rejected a write; the caller is responsible for reconciling any call
// left unanswered (results already appended are skipped, since they are answered).
func (r *Runtime) ExecuteBatch(ctx context.Context, calls []schema.Block, hooks BatchHooks) ([]schema.Block, error) {
	// Phase 1 — gate every call serially, in call order.
	plans := make([]plan, len(calls))
	for i, call := range calls {
		if call.Kind != schema.KindToolCall || call.ToolCall == nil {
			return nil, fmt.Errorf("tool: ExecuteBatch requires %s blocks, got %q at index %d", schema.KindToolCall, call.Kind, i)
		}
		p, err := r.prepare(ctx, call)
		if err != nil {
			return nil, err
		}
		plans[i] = p
	}

	// Phase 2 — run the approved tools concurrently under a bounded pool. Each
	// goroutine touches only its own slot, so the slices need no extra locking;
	// the log is untouched here, so concurrency cannot reorder it.
	outs := make([]Output, len(plans))
	runErrs := make([]error, len(plans))
	for i := range plans {
		hooks.start(i, plans[i].call)
	}
	r.runConcurrent(ctx, plans, outs, runErrs)

	// Phase 3 — record results in call order, so the log is reproducible.
	results := make([]schema.Block, len(plans))
	for i, p := range plans {
		var (
			res schema.Block
			err error
		)
		if p.deny != nil {
			res, err = r.record(p.call, *p.deny)
		} else {
			res, err = r.finishCall(ctx, p, outs[i], runErrs[i])
		}
		if err != nil {
			// The turn was abandoned (cancellation) or the log rejected a write:
			// stop recording and let the caller reconcile the rest. Calls already
			// recorded above stay answered.
			return results[:i], err
		}
		results[i] = res
		hooks.finish(i, p.call, res.ToolResult)
	}
	return results, nil
}

// runConcurrent runs the runnable plans' tools concurrently, bounded by
// r.maxParallel, writing each call's Output and error into its slot of outs and
// errs. Non-runnable plans (a terminal deny) are skipped. It returns once every
// launched tool has finished or been cancelled.
func (r *Runtime) runConcurrent(ctx context.Context, plans []plan, outs []Output, errs []error) {
	limit := r.maxParallel
	if limit <= 0 {
		limit = DefaultMaxParallel
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := range plans {
		if plans[i].tool == nil {
			continue // a denied/invalid call has no tool to run
		}
		// Acquire a worker slot, but stop launching new work the moment the turn
		// is cancelled: a not-yet-started call is marked cancelled (so phase 3
		// propagates the abandonment rather than recording an empty result)
		// instead of spawning a goroutine that would only run a doomed tool.
		select {
		case sem <- struct{}{}:
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				defer func() { <-sem }()
				outs[i], errs[i] = r.run(ctx, plans[i].tool, plans[i].call.ToolCall.Arguments, plans[i].call.ToolCall.ToolUseID)
			}(i)
		case <-ctx.Done():
			errs[i] = ctx.Err()
		}
	}
	wg.Wait()
}

// run executes the tool under a child context bounded by the tool's budget, so
// an elapsed budget cancels in-flight work and a cancelled parent does too. The
// executing call's tool_use id rides on the context so a tool can correlate a
// side effect to its call (AS-084 file snapshots).
func (r *Runtime) run(ctx context.Context, t Tool, args []byte, toolUseID string) (Output, error) {
	ctx = ContextWithToolUseID(ctx, toolUseID)
	ctx, cancel := context.WithTimeout(ctx, r.budget(t))
	defer cancel()
	return t.Run(ctx, args)
}

// budget is the effective per-tool timeout: the tool's own Def().Timeout when
// set, else the Runtime default.
func (r *Runtime) budget(t Tool) time.Duration {
	if d := t.Def().Timeout; d > 0 {
		return d
	}
	return r.timeout
}

// ensureLogged returns the on-log copy of call, appending it first if its ID is
// not already present. This makes Execute idempotent with a loop that already
// recorded the assistant turn's tool_call, while still guaranteeing the pair
// when called in isolation.
func (r *Runtime) ensureLogged(call schema.Block) (schema.Block, error) {
	if call.ID == "" {
		call.ID = schema.NewID()
	}
	if existing, ok := r.log.ByID(call.ID); ok {
		return existing, nil
	}
	stored, err := r.log.Append(call)
	if err != nil {
		return schema.Block{}, fmt.Errorf("tool: log tool_call: %w", err)
	}
	return stored, nil
}

// record records the result of a tool execution onto the log. When out carries
// a file_read body (the read tool, AS-014), a dedicated file_read block is
// appended first so the content lives in a block /context can attribute and
// dedupe (PRD D3); the tool_result that follows is then the loop-closer the
// providers require. It truncates oversized text content, appends the
// tool_result linked to its call, and returns the stored tool_result block.
func (r *Runtime) record(call schema.Block, out Output) (schema.Block, error) {
	if out.FileRead != nil {
		if err := r.recordFileRead(call, out.FileRead, out.Attribution); err != nil {
			return schema.Block{}, err
		}
	}

	parts, truncated := r.truncate(out.parts())
	result := schema.Block{
		ID:   schema.NewID(),
		Kind: schema.KindToolResult,
		Role: schema.RoleTool,
		Provenance: &schema.Provenance{
			Producer:    producer,
			DerivedFrom: []string{call.ID},
		},
		Attribution: attribution(call.ToolCall.Name, out.Attribution),
		ToolResult: &schema.ToolResultBody{
			ToolUseID: call.ToolCall.ToolUseID,
			Content:   parts,
			IsError:   out.IsError,
			Truncated: truncated,
		},
	}
	stored, err := r.log.Append(result)
	if err != nil {
		return schema.Block{}, fmt.Errorf("tool: log tool_result: %w", err)
	}
	return stored, nil
}

// recordFileRead appends a file_read block (schema §6.4) for a read tool's
// output, linked to its call by provenance and ProducedBy. The Runtime owns the
// block's envelope — provenance, attribution, the read call's tool_use_id, and a
// default Source — so the tool only supplies path, range, content, and hash. The
// content is bounded by the same byte budget as tool_result text, so a huge file
// cannot flood the window even if the tool failed to cap it itself. extra is the
// tool's own attribution (an MCP server for a resource read, AS-083), merged on
// top of the authoritative tool name so /context credits the read's cost to its
// source rather than only the tool.
func (r *Runtime) recordFileRead(call schema.Block, body *schema.FileReadBody, extra *schema.Attribution) error {
	fr := *body
	fr.Content = r.truncateText(fr.Content)
	if fr.ProducedBy == "" {
		fr.ProducedBy = call.ToolCall.ToolUseID
	}
	if fr.Source == "" {
		fr.Source = "tool"
	}
	block := schema.Block{
		ID:   schema.NewID(),
		Kind: schema.KindFileRead,
		Role: schema.RoleTool,
		Provenance: &schema.Provenance{
			Producer:    producer,
			DerivedFrom: []string{call.ID},
		},
		Attribution: attribution(call.ToolCall.Name, extra),
		FileRead:    &fr,
	}
	if _, err := r.log.Append(block); err != nil {
		return fmt.Errorf("tool: log file_read: %w", err)
	}
	return nil
}

// attribution builds the result block's attribution: the tool's own name always,
// merged with any extra source a tool supplied via Output.Attribution (a skill,
// AS-034; an MCP server/tool, AS-036). The tool name is authoritative and is
// never overwritten by the merge.
func attribution(toolName string, extra *schema.Attribution) *schema.Attribution {
	if extra == nil {
		return &schema.Attribution{Tool: toolName}
	}
	// Copy every field the tool supplied, then assert the authoritative tool name;
	// copying the whole struct stays correct as new attribution fields are added.
	attr := *extra
	attr.Tool = toolName
	return &attr
}

// truncateText bounds a single string at r.maxBytes on a UTF-8 rune boundary,
// appending the same explicit marker truncate uses for parts. A non-positive
// budget disables truncation.
func (r *Runtime) truncateText(s string) string {
	if r.maxBytes <= 0 || len(s) <= r.maxBytes {
		return s
	}
	return truncateUTF8(s, r.maxBytes) + fmt.Sprintf("\n\n[output truncated: showing %d of %d bytes]", r.maxBytes, len(s))
}

// truncate bounds the total bytes of text parts at r.maxBytes, cutting the
// content at the boundary and appending an explicit marker part. Non-text parts
// (images, files) are preserved and do not count against the budget. It reports
// whether truncation occurred.
func (r *Runtime) truncate(parts []schema.Part) ([]schema.Part, bool) {
	if r.maxBytes <= 0 || len(parts) == 0 {
		return parts, false
	}

	total := 0
	for _, p := range parts {
		if p.Type == "text" {
			total += len(p.Text)
		}
	}
	if total <= r.maxBytes {
		return parts, false
	}

	out := make([]schema.Part, 0, len(parts)+1)
	budget := r.maxBytes
	for _, p := range parts {
		if p.Type != "text" {
			out = append(out, p)
			continue
		}
		if budget <= 0 {
			continue // budget spent: drop further text parts
		}
		if len(p.Text) > budget {
			p.Text = truncateUTF8(p.Text, budget)
			budget = 0
		} else {
			budget -= len(p.Text)
		}
		out = append(out, p)
	}
	marker := fmt.Sprintf("\n\n[output truncated: showing %d of %d bytes]", r.maxBytes, total)
	out = append(out, schema.Part{Type: "text", Text: marker})
	return out, true
}

// truncateUTF8 returns s limited to at most n bytes without splitting a
// multi-byte UTF-8 rune: when the byte at the cut point is a continuation byte,
// the cut backs up to the start of that rune so the result is always valid
// UTF-8 (avoiding JSON-encode or TUI-render errors downstream).
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// errorOutput builds a model-readable error result with a single text part.
func errorOutput(format string, args ...any) Output {
	return Output{Text: fmt.Sprintf(format, args...), IsError: true}
}

// BatchHooks observes an ExecuteBatch run's progress. Both fields are optional; a
// nil hook is skipped. Started fires for every call in call order once gating is
// done and the batch is about to run its approved tools — before the concurrent
// execution begins, and including a call that was denied or failed validation
// (whose terminal result is still reported through Finished). Finished fires as
// each call's result is recorded, also in call order and regardless of which tool
// finished first, so a face can report tool progress deterministically.
type BatchHooks struct {
	Started  func(index int, call schema.Block)
	Finished func(index int, call schema.Block, result *schema.ToolResultBody)
}

func (h BatchHooks) start(i int, call schema.Block) {
	if h.Started != nil {
		h.Started(i, call)
	}
}

func (h BatchHooks) finish(i int, call schema.Block, result *schema.ToolResultBody) {
	if h.Finished != nil {
		h.Finished(i, call, result)
	}
}
