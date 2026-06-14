// Package conformance is the shared provider conformance suite (AS-012).
//
// Provider API drift is the top risk in the PRD (§9). The mitigation is a single
// suite of behavioral expectations — streaming order, tool-call normalization,
// multi-tool turns, usage accounting, error mapping, context-too-long handling,
// and unicode content — that every provider adapter (anthropic AS-009, openai
// AS-010) must satisfy *identically*. Each Case declares the normalized turn the
// core must observe regardless of which vendor produced it; both adapters are
// driven through the same Cases so a divergence in normalization (e.g. tool-call
// arguments that are reformatted instead of preserved verbatim) fails the suite
// rather than leaking into the loop.
//
// The suite runs on recorded fixtures, so CI needs no API keys and no network
// (acceptance: zero network access). Each vendor stores one fixture per case — a
// raw HTTP response captured from the real API (see replay.go) — and the vendor
// test replays it through its adapter with Run. A documented refresh flow
// (`make record-fixtures`, Record) regenerates those fixtures against live keys
// when a provider changes.
//
// What is compared is deliberately the *normalized semantics*, not vendor
// identifiers: block kinds/roles, text, tool name/kind/arguments, the turn stop
// reason, and token usage must match across providers, while inherently
// vendor-specific values (the model id, the response/tool-use ids) are only
// required to be present. That is what lets the identical Want hold for two
// different wire formats.
package conformance

import (
	"context"
	"fmt"
	"testing"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// Case is one behavioral scenario every provider must normalize identically.
// Exactly one of Want (a successful turn) or WantErr (a typed failure) is set.
type Case struct {
	// Name identifies the case and names its fixture file (Name+".http").
	Name string
	// Desc is a short human description of what the case exercises.
	Desc string
	// Request is the turn issued to the provider. Replay ignores the request body
	// (the fixture transport answers regardless), but Record sends it live, so it
	// carries a realistic prompt/tools for re-recording.
	Request provider.Request
	// Recordable reports whether a live call can regenerate the fixture. Success
	// turns are recordable; error responses (rate limit, oversized prompt, bad
	// key) cannot be elicited on demand, so their fixtures are curated by hand.
	Recordable bool

	// Want is the expected normalized turn for a success case (nil for errors).
	Want *Want
	// WantErr is the expected typed failure for an error case (zero for success).
	WantErr ErrorExpectation
}

// ErrorExpectation is the typed failure an error case must produce. A zero Kind
// means "no error expected" (the case is a success case).
type ErrorExpectation struct {
	Kind      provider.ErrorKind
	Retryable bool
}

// Want is the normalized turn a success case must produce. Only the fields set
// here are asserted: the suite checks shared semantics and leaves vendor-specific
// values (model id, response/tool-use ids) to presence checks so one Want can
// hold for every provider.
type Want struct {
	// StopReason is the expected normalized turn stop (a provider.Stop* constant);
	// empty skips the check.
	StopReason string
	// RequireTurnID, when set, asserts the turn reported a non-empty response id
	// and served model (provenance round-trip) without pinning their values.
	RequireTurnID bool
	// Usage is the expected token accounting; nil fields are not checked.
	Usage UsageExpect
	// Blocks is the expected ordered, assembled content of the turn.
	Blocks []BlockExpect
}

// UsageExpect is the per-turn token accounting to assert. A nil field is not
// checked, so a case asserts only the counts its fixtures carry.
type UsageExpect struct {
	Input     *int
	Output    *int
	CacheRead *int
	Reasoning *int
}

// BlockExpect is one expected assembled block. Text is checked for text and
// reasoning blocks; ToolName/ToolKind/ArgumentsRaw are checked for tool calls,
// where RequireToolUseID asserts the call carried a (vendor-specific) id.
type BlockExpect struct {
	Kind             schema.Kind
	Role             schema.Role
	Text             string
	ToolName         string
	ToolKind         string
	ArgumentsRaw     string
	RequireToolUseID bool
}

// Cases returns the canonical conformance scenarios. Both providers are driven
// through this list; the same Want must hold for each.
func Cases() []Case {
	weather := provider.ToolDef{
		Name:        "get_weather",
		Description: "Get the current weather for a city.",
		InputSchema: []byte(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}
	readFile := provider.ToolDef{
		Name:        "read_file",
		Description: "Read a file from the workspace.",
		InputSchema: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
	}
	user := func(text string) []schema.Block {
		return []schema.Block{{ID: "u1", Kind: schema.KindText, Role: schema.RoleUser, Text: &schema.TextBody{Text: text}}}
	}

	return []Case{
		{
			Name:       "text",
			Desc:       "a streaming text turn assembles in order with usage and a clean stop",
			Recordable: true,
			Request: provider.Request{
				Model:   "", // Run/Record substitute the vendor model
				Context: user("Say 'Hello world' and nothing else."),
			},
			Want: &Want{
				StopReason:    provider.StopEndTurn,
				RequireTurnID: true,
				Usage:         UsageExpect{Input: ip(42), Output: ip(7), CacheRead: ip(10)},
				Blocks: []BlockExpect{
					{Kind: schema.KindText, Role: schema.RoleAssistant, Text: "Hello world"},
				},
			},
		},
		{
			Name:       "tool_call",
			Desc:       "a single tool call normalizes name/kind and preserves arguments verbatim",
			Recordable: true,
			Request: provider.Request{
				Model:   "", // Run/Record substitute the vendor model
				Tools:   []provider.ToolDef{weather},
				Context: user("Use get_weather to check the weather in Paris."),
			},
			Want: &Want{
				StopReason:    provider.StopToolUse,
				RequireTurnID: true,
				Usage:         UsageExpect{Input: ip(5), Output: ip(12)},
				Blocks: []BlockExpect{
					{
						Kind: schema.KindToolCall, Role: schema.RoleAssistant,
						ToolName: "get_weather", ToolKind: schema.ToolKindClient,
						ArgumentsRaw: `{"city":"Paris"}`, RequireToolUseID: true,
					},
				},
			},
		},
		{
			Name:       "multi_tool",
			Desc:       "two tool calls in one turn keep distinct blocks, ids, and arguments",
			Recordable: true,
			Request: provider.Request{
				Model:   "", // Run/Record substitute the vendor model
				Tools:   []provider.ToolDef{readFile},
				Context: user("Read both a.txt and b.txt using read_file."),
			},
			Want: &Want{
				StopReason:    provider.StopToolUse,
				RequireTurnID: true,
				Blocks: []BlockExpect{
					{
						Kind: schema.KindToolCall, Role: schema.RoleAssistant,
						ToolName: "read_file", ToolKind: schema.ToolKindClient,
						ArgumentsRaw: `{"path":"a.txt"}`, RequireToolUseID: true,
					},
					{
						Kind: schema.KindToolCall, Role: schema.RoleAssistant,
						ToolName: "read_file", ToolKind: schema.ToolKindClient,
						ArgumentsRaw: `{"path":"b.txt"}`, RequireToolUseID: true,
					},
				},
			},
		},
		{
			Name:       "reasoning",
			Desc:       "a reasoning span assembles its visible text ahead of the answer",
			Recordable: true,
			Request: provider.Request{
				Model:   "", // Run/Record substitute the vendor model
				Params:  provider.SamplingParams{Reasoning: &provider.ReasoningOpts{Effort: "low", BudgetTokens: 1024}},
				Context: user("Think briefly, then answer: what is 2+2?"),
			},
			Want: &Want{
				StopReason:    provider.StopEndTurn,
				RequireTurnID: true,
				Blocks: []BlockExpect{
					{Kind: schema.KindReasoning, Role: schema.RoleAssistant, Text: "thinking hard"},
					{Kind: schema.KindText, Role: schema.RoleAssistant, Text: "4"},
				},
			},
		},
		{
			Name:       "unicode",
			Desc:       "multibyte content split across deltas concatenates without corruption",
			Recordable: true,
			Request: provider.Request{
				Model:   "", // Run/Record substitute the vendor model
				Context: user("Reply with: café ☕ 日本語 😀"),
			},
			Want: &Want{
				StopReason:    provider.StopEndTurn,
				RequireTurnID: true,
				Blocks: []BlockExpect{
					{Kind: schema.KindText, Role: schema.RoleAssistant, Text: "café ☕ 日本語 😀"},
				},
			},
		},
		{
			Name:    "error_rate_limit",
			Desc:    "a 429 maps to a retryable rate-limit error",
			Request: provider.Request{Model: "PLACEHOLDER", Context: user("hi")},
			WantErr: ErrorExpectation{Kind: provider.ErrRateLimit, Retryable: true},
		},
		{
			Name:    "error_context_too_long",
			Desc:    "an oversized-prompt rejection maps to a non-retryable context-too-long error",
			Request: provider.Request{Model: "PLACEHOLDER", Context: user("hi")},
			WantErr: ErrorExpectation{Kind: provider.ErrContextTooLong, Retryable: false},
		},
		{
			Name:    "error_auth",
			Desc:    "a bad key maps to a non-retryable auth error",
			Request: provider.Request{Model: "PLACEHOLDER", Context: user("hi")},
			WantErr: ErrorExpectation{Kind: provider.ErrAuth, Retryable: false},
		},
	}
}

// ProviderFunc builds the provider a single case runs against. For replay it
// returns the adapter wired to the case's fixture (FileTransport); for recording
// it returns the live adapter wired to a RecordingTransport. The vendor test
// supplies it so the conformance package stays free of vendor imports.
type ProviderFunc func(t *testing.T, c Case) provider.Provider

// Run executes every case against newProvider and asserts the normalized result
// (or typed error). model is the vendor's model id, substituted into each case's
// request (it is the one inherently vendor-specific request field). It is the
// entry point a vendor's conformance test calls.
func Run(t *testing.T, model string, newProvider ProviderFunc) {
	t.Helper()
	for _, c := range Cases() {
		c.Request.Model = model
		t.Run(c.Name, func(t *testing.T) {
			Check(t, newProvider(t, c), c)
		})
	}
}

// Record drives every recordable case against newProvider (a live adapter wired
// to a RecordingTransport that writes the fixture) and sanity-checks the live
// response. It does not assert exact content — a live turn's text differs from
// the curated fixture — so after recording a human reconciles any wire-format
// change with the Want expectations. Error cases are skipped (not Recordable).
func Record(t *testing.T, model string, newProvider ProviderFunc) {
	t.Helper()
	for _, c := range Cases() {
		if !c.Recordable {
			continue
		}
		c.Request.Model = model
		t.Run(c.Name, func(t *testing.T) {
			s, err := newProvider(t, c).Stream(context.Background(), c.Request)
			if err != nil {
				t.Fatalf("live stream: %v", err)
			}
			got, err := Assemble(s)
			if err != nil {
				t.Fatalf("assembling live stream: %v", err)
			}
			t.Logf("recorded %q: blocks=%d stop=%s", c.Name, len(got.Blocks), got.StopReason)
		})
	}
}

// Check runs one case against p and reports every mismatch via t. Success cases
// assemble the stream and compare it to Want; error cases assert the typed
// failure (from Stream's immediate error, or the stream's terminating Err).
func Check(t *testing.T, p provider.Provider, c Case) {
	t.Helper()
	s, err := p.Stream(context.Background(), c.Request)

	if c.WantErr.Kind != "" {
		if err == nil {
			if s == nil {
				t.Fatalf("provider returned (nil, nil); want a *provider.Error of kind %q", c.WantErr.Kind)
			}
			// Some adapters surface the failure mid-stream rather than at open;
			// drain to reach the terminating error.
			_, err = provider.Collect(s)
		}
		pe, ok := provider.AsError(err)
		if !ok {
			t.Fatalf("want *provider.Error of kind %q, got %v", c.WantErr.Kind, err)
		}
		if pe.Kind != c.WantErr.Kind {
			t.Errorf("error kind = %q, want %q", pe.Kind, c.WantErr.Kind)
		}
		if pe.Retryable != c.WantErr.Retryable {
			t.Errorf("error retryable = %v, want %v", pe.Retryable, c.WantErr.Retryable)
		}
		return
	}

	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got, err := Assemble(s)
	if err != nil {
		t.Fatalf("assembling stream: %v", err)
	}
	for _, m := range Compare(c.Want, got) {
		t.Errorf("%s", m)
	}
}

// Compare reports every way got diverges from want, as human-readable messages
// (empty when they agree). It is exported so the regression test can prove the
// suite catches a normalization divergence.
func Compare(want *Want, got Result) []string {
	var diffs []string
	if want.StopReason != "" && got.StopReason != want.StopReason {
		diffs = append(diffs, fmt.Sprintf("stop_reason = %q, want %q", got.StopReason, want.StopReason))
	}
	if want.RequireTurnID {
		if got.ResponseID == "" {
			diffs = append(diffs, "response_id is empty, want the provider's turn id")
		}
		if got.Model == "" {
			diffs = append(diffs, "model is empty, want the served model")
		}
	}
	diffs = append(diffs, compareUsage(want.Usage, got.Usage)...)

	if len(got.Blocks) != len(want.Blocks) {
		diffs = append(diffs, fmt.Sprintf("block count = %d, want %d", len(got.Blocks), len(want.Blocks)))
		return diffs // index-wise comparison would be misleading once counts differ
	}
	for i, be := range want.Blocks {
		diffs = append(diffs, compareBlock(i, be, got.Blocks[i])...)
	}
	return diffs
}

func compareUsage(want UsageExpect, got schema.Tokens) []string {
	var diffs []string
	check := func(name string, want, got *int) {
		switch {
		case want == nil:
			return
		case got == nil:
			diffs = append(diffs, fmt.Sprintf("usage.%s missing, want %d", name, *want))
		case *got != *want:
			diffs = append(diffs, fmt.Sprintf("usage.%s = %d, want %d", name, *got, *want))
		}
	}
	check("input", want.Input, got.Input)
	check("output", want.Output, got.Output)
	check("cache_read", want.CacheRead, got.CacheRead)
	check("reasoning", want.Reasoning, got.Reasoning)
	return diffs
}

func compareBlock(i int, want BlockExpect, got ResultBlock) []string {
	var diffs []string
	p := func(format string, args ...any) {
		diffs = append(diffs, fmt.Sprintf("block[%d] %s", i, fmt.Sprintf(format, args...)))
	}
	if got.Kind != want.Kind {
		// Field-by-field comparison keyed on the expected kind is meaningless once
		// the kind itself diverges; report just the kind mismatch.
		p("kind = %q, want %q", got.Kind, want.Kind)
		return diffs
	}
	if want.Role != "" && got.Role != want.Role {
		p("role = %q, want %q", got.Role, want.Role)
	}
	switch want.Kind {
	case schema.KindText, schema.KindReasoning:
		if got.Text != want.Text {
			p("text = %q, want %q", got.Text, want.Text)
		}
	case schema.KindToolCall:
		if want.ToolName != "" && got.ToolName != want.ToolName {
			p("tool name = %q, want %q", got.ToolName, want.ToolName)
		}
		if want.ToolKind != "" && got.ToolKind != want.ToolKind {
			p("tool kind = %q, want %q", got.ToolKind, want.ToolKind)
		}
		if want.ArgumentsRaw != "" && got.ArgumentsRaw != want.ArgumentsRaw {
			p("arguments = %q, want %q", got.ArgumentsRaw, want.ArgumentsRaw)
		}
		if want.RequireToolUseID && got.ToolUseID == "" {
			p("tool_use_id is empty, want a provider id")
		}
	}
	return diffs
}

func ip(n int) *int { return &n }
