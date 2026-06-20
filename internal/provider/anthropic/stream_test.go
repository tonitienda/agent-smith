package anthropic

import (
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/provider"
)

// SSE framing is covered by internal/streamio; this file tests only the
// Anthropic-specific normalization (stop-reason mapping, frame translation).

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
	s, err := p.Stream(t.Context(), provider.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if _, err := provider.Collect(s); err != nil {
		t.Errorf("Collect err = %v, want clean end", err)
	}
}
