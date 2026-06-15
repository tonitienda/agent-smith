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
			name: "tool call prefers verbatim arguments_raw over canonical",
			block: schema.Block{Kind: schema.KindToolCall, ToolCall: &schema.ToolCallBody{
				Name: "read", Arguments: args, ArgumentsRaw: `{ "path": "main.go" }`,
			}}, // "read"(4)+raw(21) = 25 -> 7, not the 18-char canonical
			want: 7,
		},
		{
			name: "tool result counts stdout and stderr",
			block: schema.Block{Kind: schema.KindToolResult, ToolResult: &schema.ToolResultBody{
				Stdout: "1234", Stderr: "5678",
			}}, // 8 -> 2
			want: 2,
		},
		{
			name: "tool result takes parts when present, not stdout too",
			block: schema.Block{Kind: schema.KindToolResult, ToolResult: &schema.ToolResultBody{
				Content: []schema.Part{{Type: "text", Text: "abcd"}},
				Stdout:  "this stdout is ignored when parts are present",
				// StructuredContent is never sent to the model, so it must not count.
				StructuredContent: json.RawMessage(`{"big":"ignored payload here"}`),
			}}, // only "abcd" = 4 -> 1
			want: 1,
		},
		{
			name:  "file read counts content",
			block: schema.Block{Kind: schema.KindFileRead, FileRead: &schema.FileReadBody{Content: "abcdefgh"}},
			want:  2,
		},
		{
			name: "file read includes the path prefix the adapter inserts",
			block: schema.Block{Kind: schema.KindFileRead, FileRead: &schema.FileReadBody{
				Path: "ab", Content: "cd",
			}}, // "ab"(2)+":\n"(2)+"cd"(2) = 6 -> 2
			want: 2,
		},
		{
			name: "reasoning counts text and summary",
			block: schema.Block{Kind: schema.KindReasoning, Reasoning: &schema.ReasoningBody{
				Text: "abcd", Summary: []string{"ef", "gh"},
			}}, // "abcd"+"ef"+"gh" = 8 -> 2
			want: 2,
		},
		{
			name: "redacted reasoning counts its encrypted blob with empty text",
			block: schema.Block{Kind: schema.KindReasoning, Reasoning: &schema.ReasoningBody{
				Encrypted: "abcdefgh", Redacted: true,
			}}, // 8 -> 2, not 0
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
// ballpark as the input tokens a provider would report for that turn. The
// heuristic is approximate, so the bar is a band (within 2x either way), not
// equality — that is enough to catch an estimator that is wildly off (e.g. zero,
// or 10x).
//
// The reference is computed *independently* of the chars/4 estimator under test:
// it uses the unrelated word-count rule of thumb (~0.75 words per token, i.e.
// ~1.33 tokens per word) over the same content. Deriving the reference from the
// estimate would make the check tautological — a broken estimator would still
// pass — so the two heuristics must agree without sharing math.
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

	// Independent ground truth: count words across the same content (prose appears
	// in both blocks) and apply the ~0.75-words-per-token rule. This shares no math
	// with the chars/4 estimator, so a grossly wrong estimate fails the band.
	words := 2 * len(strings.Fields(prose))
	reference := words * 4 / 3 // ~1.33 tokens per word

	if est <= 0 {
		t.Fatalf("estimate must be positive, got %d", est)
	}
	if est*2 < reference || reference*2 < est {
		t.Errorf("estimate %d not within 2x of independent reference %d", est, reference)
	}
}
