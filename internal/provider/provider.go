// Package provider defines the abstraction every model provider implements
// (AS-008, PRD §7.1, D4). The agent core — the loop (AS-018), the TUI (AS-021),
// accounting (AS-020) — depends only on the interface in this package and the
// normalized event stream it returns; every vendor-specific normalization lives
// inside a concrete provider package (internal/provider/anthropic AS-009,
// internal/provider/openai AS-010) and never leaks out. That normalization is
// the product's core IP (PRD §9 risk table): two providers, one uniform stream.
//
// The request side carries the projected, model-facing context (schema.Blocks
// from the projection engine AS-006), the tool definitions available this turn,
// sampling parameters, and cache hints (union §9). Model selection is per
// request (Request.Model), so there is deliberately no global model state — the
// loop can route different turns to different models without reconfiguring a
// provider (a prerequisite for AS-042 routing).
//
// The response side is a Stream of normalized Events (event.go) covering every
// streaming concept in the AS-002 union doc (§7): turn start, block start, text
// / reasoning / tool-argument deltas, block stop, usage, and turn stop. Errors
// are surfaced uniformly through Stream.Err as a typed *Error (error.go) carrying
// the provider-agnostic taxonomy (auth, rate-limit, overloaded, context-too-long,
// invalid-request) so the loop can drive one retry/backoff policy regardless of
// vendor.
//
// A Mock provider (mock.go) implements the interface for tests — the loop tests
// (AS-018) and the conformance suite (AS-012) build on it — so the core can be
// exercised without importing a concrete provider.
package provider

import (
	"context"
	"encoding/json"

	"github.com/tonitienda/agent-smith/schema"
)

// Provider issues model turns and normalizes each vendor's wire format to the
// shared Stream of Events. Implementations must be safe for concurrent use:
// the loop may run turns for several sessions against one Provider value.
type Provider interface {
	// Name reports the provider/vendor identity, e.g. "anthropic", "openai",
	// "mock". It matches schema.Provider.Vendor on blocks this provider produces.
	Name() string

	// Stream issues a single model turn for req and returns a Stream of
	// normalized events, or an error if the request could not be started (the
	// same typed *Error taxonomy used for mid-stream failures). The returned
	// Stream must be drained and Closed by the caller; honoring ctx cancellation
	// is the implementation's responsibility.
	//
	// Per-request model selection (req.Model) is part of the contract: a
	// Provider holds no global model state, so the loop selects the model on
	// each call.
	Stream(ctx context.Context, req Request) (Stream, error)
}

// Request is one model turn's input (union §3, §6–§9). The projected context is
// the model-facing view computed by the projection engine (AS-006); adapters map
// roles, place cache breakpoints, and render tool definitions into the vendor's
// wire shape.
type Request struct {
	// Model is the per-request model ID (e.g. "claude-opus-4-8",
	// "gpt-5"). Required: there is no global default.
	Model string

	// Context is the ordered, model-facing context for this turn — typically
	// projection.Projection.Live(). It may include system-role blocks
	// (schema.RoleSystem); adapters extract them into the vendor's system field
	// (Anthropic top-level system, OpenAI developer/system messages) rather than
	// the caller passing a separate system string, keeping the projection the
	// single source of truth (PRD D3).
	Context []schema.Block

	// Tools are the tool definitions offered to the model this turn. Empty means
	// no tools are available.
	Tools []ToolDef

	// Params carries sampling knobs; the zero value means "provider defaults".
	Params SamplingParams

	// Cache carries cache hints for cache-aware assembly (AS-011, union §9).
	// The zero value lets the adapter apply its provider's default caching.
	Cache CacheHints

	// Metadata is optional passthrough (e.g. a request/turn correlation ID) that
	// adapters may forward to the provider when supported. Never required.
	Metadata map[string]string
}

// ToolDef is a tool offered to the model this turn (union §6.2). InputSchema is
// the tool's JSON Schema; adapters render it into each vendor's function/tool
// definition shape.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`

	// Kind distinguishes a client tool the harness executes from a server tool
	// the provider runs (web search, code execution). Empty defaults to
	// schema.ToolKindClient.
	Kind string `json:"kind,omitempty"`

	// Ext carries vendor-exclusive tool-definition fields not yet promoted to
	// first-class (forward-compat, union §10).
	Ext map[string]json.RawMessage `json:"ext,omitempty"`
}

// SamplingParams holds the per-turn sampling knobs. Pointer fields are nil when
// unset so a provider's own default is used rather than a misleading zero (the
// same "missing means unset, never zero" rule the schema applies to usage).
type SamplingParams struct {
	MaxTokens     int            `json:"max_tokens,omitempty"` // 0 = provider default
	Temperature   *float64       `json:"temperature,omitempty"`
	TopP          *float64       `json:"top_p,omitempty"`
	StopSequences []string       `json:"stop_sequences,omitempty"`
	Reasoning     *ReasoningOpts `json:"reasoning,omitempty"` // thinking/reasoning controls

	Ext map[string]json.RawMessage `json:"ext,omitempty"`
}

// ReasoningOpts requests extended reasoning/thinking for a turn (union §6.5).
// Adapters map it to each vendor's surface: BudgetTokens to Anthropic's thinking
// budget, Effort to OpenAI's reasoning effort. Either may be set; an adapter
// uses whichever its vendor supports.
type ReasoningOpts struct {
	Effort       string `json:"effort,omitempty"`        // low|medium|high (OpenAI)
	BudgetTokens int    `json:"budget_tokens,omitempty"` // thinking token budget (Anthropic)
}

// CacheHints tells the cache-aware assembler (AS-011) how to cache this request
// (union §9). Explicit-breakpoint providers (Anthropic) place breakpoints at the
// named blocks; automatic-cache providers (OpenAI, Grok) ignore breakpoints and
// simply observe cached-token counts in usage.
//
// The zero value defers to the adapter's default for its vendor: an
// explicit-breakpoint adapter places sensible breakpoints itself (the stable
// system/tools prefix and the conversation prefix) so caching is on by default
// without the caller computing breakpoints per turn. Provide Breakpoints to take
// over that placement, or set Disabled to opt out of caching entirely.
type CacheHints struct {
	Mode        string                   `json:"mode,omitempty"` // schema.CacheModeExplicit|CacheModeAutomatic
	Breakpoints []schema.CacheBreakpoint `json:"breakpoints,omitempty"`

	// Disabled turns prompt caching off for the request. The zero value (false)
	// leaves caching on: the adapter applies its vendor default placement unless
	// Breakpoints overrides it. Set it when caching is undesirable (e.g. a
	// one-shot turn whose prefix will never recur).
	Disabled bool `json:"disabled,omitempty"`
}

// Stream is a normalized, forward-only stream of Events for one model turn. It
// follows the Scanner idiom: drive it with Next, read the current event with
// Event, and after Next returns false call Err to distinguish a clean end (nil)
// from a failure (a typed *Error). Close releases resources and is safe to call
// more than once; callers should always Close, typically via defer.
type Stream interface {
	// Next advances to the next event, reporting whether one is available. It
	// returns false at the clean end of the stream or when an error terminates
	// it — Err disambiguates the two.
	Next() bool
	// Event returns the event the most recent successful Next advanced to. Its
	// result is unspecified before the first Next or after Next returns false.
	Event() Event
	// Err returns the first error that terminated the stream, or nil if the
	// stream ended cleanly. It is a *Error when the failure carries provider
	// taxonomy.
	Err() error
	// Close releases any resources held by the stream. It is idempotent.
	Close() error
}

// Collect drains s into a slice, always Closing it, and returns the stream's
// terminating error (nil on a clean end). It is a convenience for non-streaming
// callers and tests; streaming consumers (the TUI) drive the Stream directly.
func Collect(s Stream) ([]Event, error) {
	defer s.Close() //nolint:errcheck // Close is best-effort on a fully drained stream
	var out []Event
	for s.Next() {
		out = append(out, s.Event())
	}
	return out, s.Err()
}
