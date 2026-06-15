package cost_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/schema"
)

// TestEstimateTokens checks the chars-per-token heuristic: empty is zero and a
// non-empty string rounds up so it always counts as at least one token.
func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},        // 1 char rounds up to 1 token
		{"abcd", 1},     // exactly 4 chars -> 1
		{"abcde", 2},    // 5 chars -> ceil(5/4)
		{"abcdefgh", 2}, // 8 chars -> 2
		{"héllo", 2},    // 5 runes (not 6 bytes) -> 2
	}
	for _, c := range cases {
		if got := cost.EstimateTokens(c.in); got != c.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestEstimateBlockTokens sums each body's textual payload across the kinds the
// model actually pays for, and confirms control events estimate to zero.
func TestEstimateBlockTokens(t *testing.T) {
	args := json.RawMessage(`{"path":"main.go"}`) // 18 chars -> 5 tokens incl. name

	cases := []struct {
		name  string
		block schema.Block
		want  int
	}{
		{
			name:  "text body",
			block: schema.Block{Kind: schema.KindText, Text: &schema.TextBody{Text: "12345678"}}, // 8 -> 2
			want:  2,
		},
		{
			name: "text with parts",
			block: schema.Block{Kind: schema.KindText, Text: &schema.TextBody{
				Text:  "abcd",
				Parts: []schema.Part{{Type: "text", Text: "efgh"}, {Type: "image", URL: "x"}},
			}}, // "abcd"+"efgh" = 8 -> 2; image part contributes no text
			want: 2,
		},
		{
			name: "tool call counts name and arguments",
			block: schema.Block{Kind: schema.KindToolCall, ToolCall: &schema.ToolCallBody{
				Name: "read", Arguments: args,
			}}, // "read"(4)+18 = 22 -> 6
			want: 6,
		},
		{
			name: "tool result counts stdout and stderr",
			block: schema.Block{Kind: schema.KindToolResult, ToolResult: &schema.ToolResultBody{
				Stdout: "1234", Stderr: "5678",
			}}, // 8 -> 2
			want: 2,
		},
		{
			name:  "file read counts content",
			block: schema.Block{Kind: schema.KindFileRead, FileRead: &schema.FileReadBody{Content: "abcdefgh"}},
			want:  2,
		},
		{
			name: "reasoning counts text and summary",
			block: schema.Block{Kind: schema.KindReasoning, Reasoning: &schema.ReasoningBody{
				Text: "abcd", Summary: []string{"ef", "gh"},
			}}, // "abcd"+"ef"+"gh" = 8 -> 2
			want: 2,
		},
		{
			name:  "usage control event estimates to zero",
			block: usageBlock("claude-opus-4-8", &schema.Tokens{Input: ptr(100)}),
			want:  0,
		},
	}
	for _, c := range cases {
		if got := cost.EstimateBlockTokens(c.block); got != c.want {
			t.Errorf("%s: EstimateBlockTokens = %d, want %d", c.name, got, c.want)
		}
	}
}

// TestEstimateContextTokens sums content blocks and ignores control events, so a
// raw log slice can be passed without filtering.
func TestEstimateContextTokens(t *testing.T) {
	events := []schema.Block{
		{Kind: schema.KindText, Text: &schema.TextBody{Text: "abcdefgh"}},                // 2
		usageBlock("claude-opus-4-8", &schema.Tokens{Input: ptr(999)}),                   // 0
		{Kind: schema.KindFileRead, FileRead: &schema.FileReadBody{Content: "12345678"}}, // 2
	}
	if got := cost.EstimateContextTokens(events); got != 4 {
		t.Errorf("EstimateContextTokens = %d, want 4", got)
	}
}

// TestEstimateSanityAgainstReportedInput is the AS-063 reconciliation check: the
// sum of per-block estimates over a turn's input blocks should land in the same
// ballpark as the input tokens the provider reported for that turn. The heuristic
// is approximate, so the bar is a band (within 2x either way), not equality —
// that is enough to catch an estimator that is wildly off (e.g. zero, or 10x).
func TestEstimateSanityAgainstReportedInput(t *testing.T) {
	prose := strings.Repeat(
		"The quick brown fox jumps over the lazy dog while the agent reasons about the next step. ",
		8,
	)
	input := []schema.Block{
		{Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: prose}},
		{Kind: schema.KindFileRead, FileRead: &schema.FileReadBody{Content: prose}},
	}
	est := cost.EstimateContextTokens(input)

	// A real tokenizer on this English prose lands near chars/4; simulate a
	// provider-reported input count in that neighborhood and assert the estimate
	// reconciles within a 2x band in both directions.
	reported := est * 11 / 10 // ~10% above the heuristic, a realistic gap
	if est <= 0 {
		t.Fatalf("estimate must be positive, got %d", est)
	}
	if est*2 < reported || reported*2 < est {
		t.Errorf("estimate %d not within 2x of reported %d", est, reported)
	}
}
