package tool

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

// producer is stamped on every tool_result block's provenance so its origin is
// self-describing on the log.
const producer = "tool-runtime"

// DefaultTimeout is the per-tool execution budget when neither the tool's Def
// nor the Runtime overrides it.
const DefaultTimeout = 60 * time.Second

// DefaultMaxResultBytes caps the text content of a tool_result before it is
// truncated with an explicit marker. Oversized tool output is a top context-bloat
// source (PRD §2), so the runtime bounds it by default.
const DefaultMaxResultBytes = 32 * 1024

// Runtime executes a model's tool calls: it validates arguments against the
// tool's schema, gates execution through the permission hook (AS-016), runs the
// tool under cancellation and a per-tool timeout, truncates oversized output, and
// records a tool_result block onto the log (AS-005) linked to its call. It holds
// no mutable state after construction, so Execute is safe to call concurrently
// for the independent calls of a parallel-tool turn (AS-019).
type Runtime struct {
	registry   *Registry
	log        *eventlog.Log
	permission PermissionFunc
	timeout    time.Duration
	maxBytes   int
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

// NewRuntime builds a Runtime over registry and log. The log receives every
// tool_result (and, as a safety net, any tool_call not already on it). Without
// WithPermission the runtime permits every call (AllowAll); AS-016 injects the
// real policy.
func NewRuntime(registry *Registry, log *eventlog.Log, opts ...Option) *Runtime {
	r := &Runtime{
		registry:   registry,
		log:        log,
		permission: AllowAll,
		timeout:    DefaultTimeout,
		maxBytes:   DefaultMaxResultBytes,
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

	logged, err := r.ensureLogged(call)
	if err != nil {
		return schema.Block{}, err
	}
	tc := logged.ToolCall

	// An already-cancelled turn should not start new work.
	if err := ctx.Err(); err != nil {
		return schema.Block{}, err
	}

	tool, ok := r.registry.Get(tc.Name)
	if !ok {
		return r.record(logged, errorOutput("unknown tool %q", tc.Name))
	}

	if err := validateArgs(tool.Def().InputSchema, tc.Arguments); err != nil {
		return r.record(logged, errorOutput("invalid arguments for tool %q: %v", tc.Name, err))
	}

	gate := Call{ToolUseID: tc.ToolUseID, Name: tc.Name, Arguments: tc.Arguments}
	if d := r.permission(ctx, gate); !d.Allow {
		return r.record(logged, errorOutput("permission denied for tool %q: %s", tc.Name, d.Reason))
	}

	out, runErr := r.run(ctx, tool, tc.Arguments)
	if runErr != nil {
		// Distinguish abandonment of the whole turn (propagate, record nothing)
		// from a per-tool timeout or a tool failure (record a model-readable
		// result so the model can react).
		if ctx.Err() != nil {
			return schema.Block{}, ctx.Err()
		}
		if errors.Is(runErr, context.DeadlineExceeded) {
			return r.record(logged, errorOutput("tool %q timed out after %s", tc.Name, r.budget(tool)))
		}
		return r.record(logged, errorOutput("tool %q failed: %v", tc.Name, runErr))
	}
	return r.record(logged, out)
}

// run executes the tool under a child context bounded by the tool's budget, so
// an elapsed budget cancels in-flight work and a cancelled parent does too.
func (r *Runtime) run(ctx context.Context, t Tool, args []byte) (Output, error) {
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

// record builds the tool_result for out, truncates oversized text content,
// appends it to the log linked to its call, and returns the stored block.
func (r *Runtime) record(call schema.Block, out Output) (schema.Block, error) {
	parts, truncated := r.truncate(out.parts())
	result := schema.Block{
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
