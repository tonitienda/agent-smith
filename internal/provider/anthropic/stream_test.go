package anthropic

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/provider"
)

func TestReadSSEEventJoinsMultilineDataAndIgnoresComments(t *testing.T) {
	raw := strings.Join([]string{
		`: this is a comment`,
		`event: message_start`,
		`data: {"type":`,
		`data: "ping"}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")
	s := newSSEStream(io.NopCloser(strings.NewReader(raw)))

	first, err := s.readSSEEvent()
	if err != nil {
		t.Fatalf("readSSEEvent: %v", err)
	}
	// Per the SSE spec, multiple data lines are joined with "\n"; JSON tolerates
	// the embedded newline, so the frame still decodes.
	if string(first) != "{\"type\":\n\"ping\"}" {
		t.Errorf("first event data = %q, want newline-joined ping frame", first)
	}
	second, err := s.readSSEEvent()
	if err != nil {
		t.Fatalf("readSSEEvent: %v", err)
	}
	if string(second) != `{"type":"message_stop"}` {
		t.Errorf("second event data = %q, want message_stop frame", second)
	}
	if _, err := s.readSSEEvent(); err != io.EOF {
		t.Errorf("third read err = %v, want io.EOF", err)
	}
}

func TestMapStopReason(t *testing.T) {
	cases := map[string]string{
		"end_turn":                      provider.StopEndTurn,
		"stop_sequence":                 provider.StopEndTurn,
		"max_tokens":                    provider.StopMaxTokens,
		"tool_use":                      provider.StopToolUse,
		"pause_turn":                    provider.StopPauseTurn,
		"refusal":                       provider.StopRefusal,
		"model_context_window_exceeded": provider.StopContextWindowExceeded,
		"something_new":                 "something_new",
	}
	for in, want := range cases {
		if got := mapStopReason(in); got != want {
			t.Errorf("mapStopReason(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestStreamCleanEOFWithoutMessageStop ensures a stream that ends without a
// message_stop terminates cleanly (no spurious error) rather than hanging.
func TestStreamCleanEOFWithoutMessageStop(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"m","model":"m","usage":{"input_tokens":1}}}`,
		``,
	}, "\n")
	p := sseServer(t, body, nil)
	s, err := p.Stream(context.Background(), provider.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if _, err := provider.Collect(s); err != nil {
		t.Errorf("Collect err = %v, want clean end", err)
	}
}
