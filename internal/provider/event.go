package provider

import "github.com/tonitienda/agent-smith/schema"

// EventType discriminates a normalized streaming Event. The set covers every
// streaming concept in the AS-002 union doc (§7) so a consumer can assemble the
// same schema.Blocks from any provider's stream: a turn frames a sequence of
// blocks, each block opens, streams deltas, and closes, with usage and a stop
// reason reported for the turn. New kinds are additive (PRD D2); consumers must
// ignore Event types they do not recognize.
type EventType string

const (
	// EventTurnStart opens a turn. Event.Turn carries the provider's response /
	// turn IDs and the model that actually served the request, when known.
	// (Anthropic message_start, OpenAI response.created, §7 "turn start".)
	EventTurnStart EventType = "turn_start"

	// EventBlockStart opens a content block within the turn. Event.Header gives
	// its kind and, for a tool call, the tool_use_id and name. Event.BlockIndex
	// identifies the block for the deltas and stop that follow. (§7 "block opens".)
	EventBlockStart EventType = "block_start"

	// EventTextDelta is an incremental text fragment for the block at
	// Event.BlockIndex, in Event.TextDelta. (§7 "text delta".)
	EventTextDelta EventType = "text_delta"

	// EventReasoningDelta is an incremental reasoning fragment: visible/summary
	// text in Event.TextDelta, an Anthropic thinking signature fragment in
	// Event.SignatureDelta, and/or opaque encrypted/redacted reasoning in
	// Event.EncryptedDelta. Encrypted/redacted reasoning is carried on the
	// assembled block verbatim, never inspected. (§7 "reasoning delta".)
	EventReasoningDelta EventType = "reasoning_delta"

	// EventToolCallDelta is an incremental fragment of a tool call's JSON
	// arguments for the block at Event.BlockIndex, in Event.ArgumentsDelta.
	// Concatenated across events it yields the verbatim arguments string
	// (schema.ToolCallBody.ArgumentsRaw). (§7 "tool-args delta".)
	EventToolCallDelta EventType = "tool_call_delta"

	// EventBlockStop closes the block at Event.BlockIndex. (§7 "block closes".)
	EventBlockStop EventType = "block_stop"

	// EventUsage reports token usage and price-affecting metadata for the turn
	// (Event.Usage, Event.UsageMeta). It may arrive more than once per turn
	// (e.g. input at start, output at end, per-iteration during server-tool
	// loops); consumers accumulate. (union §8.)
	EventUsage EventType = "usage"

	// EventTurnStop closes the turn with Event.StopReason (one of the Stop*
	// constants). (§7 "turn end".)
	EventTurnStop EventType = "turn_stop"
)

// Normalized turn stop reasons (union §7). Adapters map each vendor's native
// value onto these so the loop reacts uniformly: StopToolUse drives another
// loop iteration, StopMaxTokens / StopContextWindowExceeded inform context
// management, StopPauseTurn marks a resumable server-tool continuation.
const (
	StopEndTurn               = "end_turn"                      // natural completion (Anthropic end_turn, OpenAI stop)
	StopToolUse               = "tool_use"                      // model is calling tools (Anthropic tool_use, OpenAI tool_calls)
	StopMaxTokens             = "max_tokens"                    // hit the output cap (OpenAI length)
	StopRefusal               = "refusal"                       // a refusal turn
	StopContentFilter         = "content_filter"                // filtered by the provider
	StopContextWindowExceeded = "model_context_window_exceeded" // input exceeded the context window
	StopPauseTurn             = "pause_turn"                    // resumable server-tool continuation (Anthropic pause_turn)
)

// Event is one normalized streaming event. Type selects which payload fields are
// populated; the rest are zero. The shape mirrors the schema's one-struct,
// typed-pointer-body convention (schema.Block) rather than a Go sum type, so
// adding event kinds stays additive (PRD D2).
type Event struct {
	// Type discriminates the event.
	Type EventType `json:"type"`

	// BlockIndex identifies the block a block-scoped event belongs to within the
	// turn (0-based, in stream order). Deltas and the stop for one block share an
	// index, so concurrently streamed blocks (parallel tool calls, AS-019) are
	// unambiguous. Zero for turn-scoped events.
	BlockIndex int `json:"block_index,omitempty"`

	// Header is set on EventBlockStart.
	Header *BlockHeader `json:"header,omitempty"`

	// TextDelta is set on EventTextDelta and EventReasoningDelta.
	TextDelta string `json:"text_delta,omitempty"`
	// SignatureDelta is set on EventReasoningDelta for an Anthropic thinking
	// signature fragment.
	SignatureDelta string `json:"signature_delta,omitempty"`
	// EncryptedDelta is set on EventReasoningDelta for opaque encrypted/redacted
	// reasoning content (Anthropic redacted_thinking `data`, OpenAI
	// encrypted_content). It is stored verbatim on the assembled block
	// (schema.ReasoningBody.Encrypted) and never inspected. Additive (PRD D2);
	// consumers that do not carry encrypted reasoning ignore it.
	EncryptedDelta string `json:"encrypted_delta,omitempty"`
	// ArgumentsDelta is set on EventToolCallDelta.
	ArgumentsDelta string `json:"arguments_delta,omitempty"`

	// Usage and UsageMeta are set on EventUsage (union §8).
	Usage     *schema.Tokens    `json:"usage,omitempty"`
	UsageMeta *schema.UsageMeta `json:"usage_meta,omitempty"`

	// StopReason is set on EventTurnStop (one of the Stop* constants).
	StopReason string `json:"stop_reason,omitempty"`

	// Turn is set on EventTurnStart.
	Turn *TurnInfo `json:"turn,omitempty"`
}

// BlockHeader describes a block opened by EventBlockStart. For a text or
// reasoning block only Kind and Role are meaningful; the tool-call fields are
// set when Kind is schema.KindToolCall.
type BlockHeader struct {
	Kind schema.Kind `json:"kind"`
	Role schema.Role `json:"role,omitempty"`

	// Tool-call fields (Kind == schema.KindToolCall).
	ToolUseID   string `json:"tool_use_id,omitempty"` // links the call to its result
	ToolName    string `json:"tool_name,omitempty"`
	ToolKind    string `json:"tool_kind,omitempty"`    // schema.ToolKindClient|ToolKindServer
	ToolSubtype string `json:"tool_subtype,omitempty"` // specific server-tool name
}

// TurnInfo carries turn-level identity reported at EventTurnStart. Fields are
// optional: a provider that does not surface a value leaves it empty.
type TurnInfo struct {
	ResponseID string `json:"response_id,omitempty"` // provider's response ID (provenance round-trip)
	TurnID     string `json:"turn_id,omitempty"`
	Model      string `json:"model,omitempty"` // the model that actually served the request
}
