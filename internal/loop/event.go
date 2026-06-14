package loop

import (
	"encoding/json"

	"github.com/tonitienda/agent-smith/schema"
)

// UIEventKind discriminates a face-agnostic UI event the loop emits as a turn
// progresses (AS-018, PRD §5). The set is deliberately UI-free: it describes
// what happened — text streamed, a tool started, a turn completed — not how to
// render it, so the TUI (AS-021), a headless face (AS-051), and an ACP server
// (AS-052) can all consume the same stream. New kinds are additive (PRD D2);
// observers must ignore kinds they do not recognize.
type UIEventKind string

const (
	// UITurnStart opens a model turn. Iteration is the 0-based turn index within
	// the run, so an observer can show "thinking" before the first delta arrives.
	UITurnStart UIEventKind = "turn_start"

	// UITextDelta is an incremental fragment of visible assistant text, in Text.
	UITextDelta UIEventKind = "text_delta"

	// UIReasoningDelta is an incremental fragment of visible reasoning text, in
	// Text. Opaque encrypted/redacted reasoning is never surfaced to a face.
	UIReasoningDelta UIEventKind = "reasoning_delta"

	// UIToolStarted reports that a client tool call is about to execute. Tool
	// carries the call's identity and arguments; Result is nil.
	UIToolStarted UIEventKind = "tool_started"

	// UIToolFinished reports that a client tool call finished and its result was
	// recorded on the log. Tool carries the call identity and the recorded
	// result body.
	UIToolFinished UIEventKind = "tool_finished"

	// UITurnComplete closes a model turn with its normalized StopReason (one of
	// the provider Stop* constants). It fires once per provider turn, before any
	// tool dispatch that the stop reason triggers.
	UITurnComplete UIEventKind = "turn_complete"
)

// UIEvent is one face-agnostic event emitted to the Observer. Kind selects which
// payload fields are populated; the rest are zero. It mirrors the schema's
// one-struct, typed-pointer-body convention rather than a Go sum type so adding
// kinds stays additive (PRD D2).
type UIEvent struct {
	// Kind discriminates the event.
	Kind UIEventKind

	// Iteration is the 0-based turn index within the run. Set on every event so
	// an observer can group events by turn.
	Iteration int

	// Text is the delta for UITextDelta and UIReasoningDelta.
	Text string

	// Tool is set on UIToolStarted and UIToolFinished.
	Tool *ToolEvent

	// StopReason is set on UITurnComplete (a provider Stop* constant).
	StopReason string
}

// ToolEvent describes a client tool call on UIToolStarted / UIToolFinished. On
// "started" Result is nil; on "finished" it carries the recorded tool_result
// body so a face can show success/error and (if it wishes) the output.
type ToolEvent struct {
	ToolUseID string
	Name      string
	Arguments json.RawMessage
	Result    *schema.ToolResultBody
}

// Observer receives UIEvents as a run progresses. It must be safe to call from
// the goroutine driving the run and must not block for long: the loop calls it
// inline while draining the provider stream. A nil Observer disables emission.
type Observer func(UIEvent)
