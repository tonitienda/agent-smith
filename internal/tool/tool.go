// Package tool is the runtime framework concrete tools plug into (AS-013, PRD
// §7.2). It is the harness side of the tool story: a Registry holds the tools
// offered to the model this turn (rendered into provider.ToolDef via AS-008),
// and a Runtime validates a model's tool call against the tool's schema, runs it
// under a permission gate with cancellation and a per-tool timeout, truncates
// oversized output, and records the result onto the append-only event log
// (AS-005) as a tool_result block linked to its call.
//
// The split mirrors the rest of the core: concrete tools (file/search AS-014,
// shell AS-015) implement the small Tool interface here and never touch the log,
// the provider wire format, or the permission policy directly; the Runtime owns
// that orchestration so every tool gets the same validation, permission check
// (AS-016), truncation, and provenance for free. The Runtime holds no mutable
// state after construction, so Execute is safe to call concurrently for the
// independent calls of a parallel-tool turn (AS-019).
//
// Errors follow one rule: a failure the model should see and react to — an
// unknown tool, invalid arguments, a denied permission, a per-tool timeout, or a
// tool reporting a domain error — becomes a tool_result with IsError set and a
// nil Go error, so the loop feeds it back to the model and continues. Only an
// infrastructure failure (the call is not a tool_call, the log rejects the
// result) or genuine cancellation of the surrounding turn returns a Go error.
package tool

import (
	"context"
	"encoding/json"
	"time"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// Tool is a client-side tool the harness executes on the model's behalf (union
// §6.2, ToolKindClient). Concrete tools implement it; the Registry exposes them
// to providers and the Runtime executes them. Implementations must be safe for
// concurrent use — the Runtime may run several calls against one Tool value at
// once (AS-019) — and Run must honor ctx cancellation so an aborted turn or an
// elapsed timeout stops in-flight work cleanly.
type Tool interface {
	// Def returns the tool's definition: its name, model-facing description, and
	// JSON-Schema parameters. It is read on every turn to build the provider
	// request, so it should be cheap and return a stable value.
	Def() Def

	// Run executes the tool. args is the model's arguments, already validated
	// against Def().InputSchema, as a JSON object. Returning an error reports that
	// the execution itself failed (I/O, etc.); the Runtime turns it into a
	// model-readable error result. A tool that ran but wants to report a domain
	// failure to the model returns a normal Output with IsError set instead.
	Run(ctx context.Context, args json.RawMessage) (Output, error)
}

// Def describes a tool to the model and the Registry. InputSchema is the tool's
// JSON Schema for its arguments object; the Runtime validates calls against a
// pragmatic subset of it (see validateArgs) before execution. Timeout is the
// per-tool execution budget; zero defers to the Runtime's default.
type Def struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	Timeout     time.Duration   `json:"timeout,omitempty"`
}

// ProviderDef renders the definition into the provider-facing shape (AS-008).
// Every tool registered here is a client tool the harness runs.
func (d Def) ProviderDef() provider.ToolDef {
	return provider.ToolDef{
		Name:        d.Name,
		Description: d.Description,
		InputSchema: d.InputSchema,
		Kind:        schema.ToolKindClient,
	}
}

// Output is the raw result a Tool produces, before the Runtime truncates it and
// records it onto the log. The common case is a single text result: set Text and
// leave Parts empty. A tool with typed/multimodal output sets Parts instead;
// when Parts is non-empty it is used and Text is ignored. IsError marks a
// domain-level failure the model should see (e.g. "file not found"), as opposed
// to an infrastructure error returned from Run.
//
// FileRead, when set, asks the Runtime to also append a dedicated file_read
// block (schema §6.4) carrying the read content, ahead of the tool_result. This
// is how the read tool (AS-014) records file content as a first-class file_read
// block — the block type exists so /context can attribute window cost to files
// and dedupe re-reads (PRD D3) — while the tool_result it is paired with stays a
// minimal loop-closer for the providers that require one per tool call. The
// Runtime fills in the block's provenance, attribution, ProducedBy, and Source;
// a tool need only supply the path, range, content, and hash.
type Output struct {
	Text     string
	Parts    []schema.Part
	IsError  bool
	FileRead *schema.FileReadBody

	// Attribution, when set, records what the result content should be attributed
	// to beyond the tool that produced it — e.g. the skill a "skill" tool loaded
	// (AS-034) or, later, an MCP server/tool (AS-036). The Runtime always stamps
	// the result with the tool's own name; these fields are merged on top so
	// /context and the living-skills analyzers can credit the underlying source.
	// The tool name on the result is never overwritten.
	Attribution *schema.Attribution
}

// parts returns the effective result content: Parts when set, otherwise a single
// text part built from Text (empty when both are empty).
func (o Output) parts() []schema.Part {
	if len(o.Parts) > 0 {
		return o.Parts
	}
	if o.Text == "" {
		return nil
	}
	return []schema.Part{{Type: "text", Text: o.Text}}
}

// Func adapts a plain function into a Tool, pairing it with a Def. It keeps
// simple tools and tests from each needing a named type.
type Func struct {
	Spec Def
	Fn   func(ctx context.Context, args json.RawMessage) (Output, error)
}

// Def returns the adapter's definition.
func (f Func) Def() Def { return f.Spec }

// Run invokes the wrapped function.
func (f Func) Run(ctx context.Context, args json.RawMessage) (Output, error) {
	return f.Fn(ctx, args)
}
