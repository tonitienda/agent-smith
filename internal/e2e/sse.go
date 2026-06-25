package e2e

import (
	"encoding/json"
	"fmt"
	"strings"
)

// This file builds Anthropic Messages SSE response bodies for the offline E2E
// scenarios (AS-134). A recorded vendor simulator (AS-133) replays these bytes
// in order, so each scenario scripts a full multi-turn session — a tool-use turn
// followed by a recovery/answer turn — without an SSE byte ever being
// hand-typed in a test. The frame shapes mirror the conformance fixtures under
// internal/provider/anthropic/testdata/conformance, which is the authority on
// the wire format the adapter normalizes.

// toolUse is one tool_use block the model emits in a turn: the call id the loop
// echoes back on the result, the registered tool name, and the verbatim
// argument JSON the runtime validates and the tool reads.
type toolUse struct {
	id   string
	name string
	args string // a JSON object literal, e.g. `{"path":"a.txt"}`
}

// sseFrame writes one `data: <json>` SSE event with the trailing blank line the
// Anthropic stream uses between events.
func sseFrame(b *strings.Builder, payload string) {
	b.WriteString("data: ")
	b.WriteString(payload)
	b.WriteString("\n\n")
}

// usageDelta is the message_delta frame carrying the turn's stop reason and
// output token count; input tokens ride on message_start.
func messageStart(b *strings.Builder, id, model string, inputTokens int) {
	sseFrame(b, fmt.Sprintf(
		`{"type":"message_start","message":{"id":%q,"model":%q,"usage":{"input_tokens":%d}}}`,
		id, model, inputTokens))
}

func messageStop(b *strings.Builder, stopReason string, outputTokens int) {
	sseFrame(b, fmt.Sprintf(
		`{"type":"message_delta","delta":{"stop_reason":%q},"usage":{"output_tokens":%d}}`,
		stopReason, outputTokens))
	sseFrame(b, `{"type":"message_stop"}`)
}

// textTurn builds a complete SSE body for a turn whose only content is visible
// assistant text ending with stop_reason "end_turn" — the recovery/answer turn
// that closes a scenario.
func textTurn(id, model, text string, inputTokens, outputTokens int) []byte {
	var b strings.Builder
	messageStart(&b, id, model, inputTokens)
	sseFrame(&b, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	// Stream the text as a single delta; the loop concatenates deltas, so one is
	// equivalent to many for the assembled block and keeps the fixture readable.
	delta, _ := json.Marshal(text)
	sseFrame(&b, fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`, delta))
	sseFrame(&b, `{"type":"content_block_stop","index":0}`)
	messageStop(&b, "end_turn", outputTokens)
	return []byte(b.String())
}

// toolTurn builds a complete SSE body for a turn that ends asking the loop to
// run one or more client tool calls (stop_reason "tool_use"). Each call's
// arguments are streamed as a single input_json_delta, matching the conformance
// fixtures, so multiple calls in one body exercise the parallel-dispatch path.
func toolTurn(id, model string, calls []toolUse, inputTokens, outputTokens int) []byte {
	var b strings.Builder
	messageStart(&b, id, model, inputTokens)
	for i, c := range calls {
		sseFrame(&b, fmt.Sprintf(
			`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":%q,"name":%q,"input":{}}}`,
			i, c.id, c.name))
		argDelta, _ := json.Marshal(c.args)
		sseFrame(&b, fmt.Sprintf(
			`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`,
			i, argDelta))
		sseFrame(&b, fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, i))
	}
	messageStop(&b, "tool_use", outputTokens)
	return []byte(b.String())
}
