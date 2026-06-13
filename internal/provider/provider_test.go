package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/schema"
)

// assembled is a tiny consumer that reduces a normalized stream back into the
// blocks it describes — the same job the loop (AS-018) does — so the tests
// assert the event set is sufficient to reconstruct a turn.
type assembled struct {
	model      string
	stopReason string
	usage      schema.Tokens
	blocks     []schema.Block
}

func assemble(t *testing.T, s provider.Stream) assembled {
	t.Helper()
	var a assembled
	// open holds the block currently being streamed, keyed by BlockIndex.
	open := map[int]*schema.Block{}
	rawArgs := map[int]string{}

	for s.Next() {
		ev := s.Event()
		switch ev.Type {
		case provider.EventTurnStart:
			if ev.Turn != nil {
				a.model = ev.Turn.Model
			}
		case provider.EventBlockStart:
			b := &schema.Block{Kind: ev.Header.Kind, Role: ev.Header.Role}
			switch ev.Header.Kind {
			case schema.KindText:
				b.Text = &schema.TextBody{}
			case schema.KindReasoning:
				b.Reasoning = &schema.ReasoningBody{}
			case schema.KindToolCall:
				b.ToolCall = &schema.ToolCallBody{
					ToolUseID: ev.Header.ToolUseID,
					Name:      ev.Header.ToolName,
					ToolKind:  ev.Header.ToolKind,
				}
			}
			open[ev.BlockIndex] = b
		case provider.EventTextDelta:
			open[ev.BlockIndex].Text.Text += ev.TextDelta
		case provider.EventReasoningDelta:
			open[ev.BlockIndex].Reasoning.Text += ev.TextDelta
			open[ev.BlockIndex].Reasoning.Signature += ev.SignatureDelta
		case provider.EventToolCallDelta:
			rawArgs[ev.BlockIndex] += ev.ArgumentsDelta
		case provider.EventBlockStop:
			b := open[ev.BlockIndex]
			if b.Kind == schema.KindToolCall {
				b.ToolCall.ArgumentsRaw = rawArgs[ev.BlockIndex]
				b.ToolCall.Arguments = json.RawMessage(rawArgs[ev.BlockIndex])
			}
			a.blocks = append(a.blocks, *b)
			delete(open, ev.BlockIndex)
		case provider.EventUsage:
			if ev.Usage != nil && ev.Usage.Input != nil {
				a.usage.Input = ev.Usage.Input
			}
			if ev.Usage != nil && ev.Usage.Output != nil {
				a.usage.Output = ev.Usage.Output
			}
		case provider.EventTurnStop:
			a.stopReason = ev.StopReason
		}
	}
	if err := s.Err(); err != nil {
		t.Fatalf("stream ended with error: %v", err)
	}
	return a
}

func intp(n int) *int { return &n }

func TestMockTextTurnAssembles(t *testing.T) {
	m := &provider.Mock{Events: provider.TextTurn("hello world", "")}

	s, err := m.Stream(context.Background(), provider.Request{Model: "test-model"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := assemble(t, s)

	if len(got.blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(got.blocks))
	}
	if got.blocks[0].Kind != schema.KindText || got.blocks[0].Text.Text != "hello world" {
		t.Errorf("block = %+v, want text 'hello world'", got.blocks[0])
	}
	if got.stopReason != provider.StopEndTurn {
		t.Errorf("stop reason = %q, want %q", got.stopReason, provider.StopEndTurn)
	}
}

func TestMockRecordsRequestsAndModel(t *testing.T) {
	m := &provider.Mock{Events: provider.TextTurn("ok", "")}

	for _, model := range []string{"model-a", "model-b"} {
		s, err := m.Stream(context.Background(), provider.Request{Model: model})
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		if _, err := provider.Collect(s); err != nil {
			t.Fatalf("Collect: %v", err)
		}
	}

	reqs := m.Requests()
	if len(reqs) != 2 {
		t.Fatalf("recorded %d requests, want 2", len(reqs))
	}
	// Per-request model selection: each call carried its own model, no global state.
	if reqs[0].Model != "model-a" || reqs[1].Model != "model-b" {
		t.Errorf("recorded models = %q, %q; want model-a, model-b", reqs[0].Model, reqs[1].Model)
	}
}

func TestMockToolCallTurn(t *testing.T) {
	args := json.RawMessage(`{"path":"main.go"}`)
	m := &provider.Mock{Events: provider.ToolCallTurn("toolu_1", "read_file", args)}

	s, err := m.Stream(context.Background(), provider.Request{Model: "test-model"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := assemble(t, s)

	if len(got.blocks) != 1 || got.blocks[0].Kind != schema.KindToolCall {
		t.Fatalf("got blocks %+v, want one tool_call", got.blocks)
	}
	tc := got.blocks[0].ToolCall
	if tc.ToolUseID != "toolu_1" || tc.Name != "read_file" {
		t.Errorf("tool call = %+v, want id toolu_1 name read_file", tc)
	}
	if string(tc.Arguments) != string(args) {
		t.Errorf("arguments = %s, want %s", tc.Arguments, args)
	}
	if got.stopReason != provider.StopToolUse {
		t.Errorf("stop reason = %q, want %q", got.stopReason, provider.StopToolUse)
	}
}

// TestEventSetCoversUnionStreaming drives one stream exercising every Event type
// (AS-002 §7) and asserts each is observed and reduces correctly — the
// acceptance criterion that the event set covers the union doc.
func TestEventSetCoversUnionStreaming(t *testing.T) {
	events := []provider.Event{
		{Type: provider.EventTurnStart, Turn: &provider.TurnInfo{ResponseID: "resp_1", Model: "served-model"}},
		{Type: provider.EventUsage, Usage: &schema.Tokens{Input: intp(42)}},
		// reasoning block
		{Type: provider.EventBlockStart, BlockIndex: 0, Header: &provider.BlockHeader{Kind: schema.KindReasoning, Role: schema.RoleAssistant}},
		{Type: provider.EventReasoningDelta, BlockIndex: 0, TextDelta: "think ", SignatureDelta: "sig"},
		{Type: provider.EventReasoningDelta, BlockIndex: 0, TextDelta: "more"},
		{Type: provider.EventBlockStop, BlockIndex: 0},
		// text block
		{Type: provider.EventBlockStart, BlockIndex: 1, Header: &provider.BlockHeader{Kind: schema.KindText, Role: schema.RoleAssistant}},
		{Type: provider.EventTextDelta, BlockIndex: 1, TextDelta: "answer"},
		{Type: provider.EventBlockStop, BlockIndex: 1},
		// tool call block
		{Type: provider.EventBlockStart, BlockIndex: 2, Header: &provider.BlockHeader{Kind: schema.KindToolCall, Role: schema.RoleAssistant, ToolUseID: "toolu_9", ToolName: "grep", ToolKind: schema.ToolKindClient}},
		{Type: provider.EventToolCallDelta, BlockIndex: 2, ArgumentsDelta: `{"q":`},
		{Type: provider.EventToolCallDelta, BlockIndex: 2, ArgumentsDelta: `"x"}`},
		{Type: provider.EventBlockStop, BlockIndex: 2},
		{Type: provider.EventUsage, Usage: &schema.Tokens{Output: intp(7)}},
		{Type: provider.EventTurnStop, StopReason: provider.StopToolUse},
	}
	got := assemble(t, provider.SliceStream(events, nil))

	if got.model != "served-model" {
		t.Errorf("model = %q, want served-model", got.model)
	}
	if got.usage.Input == nil || *got.usage.Input != 42 || got.usage.Output == nil || *got.usage.Output != 7 {
		t.Errorf("usage = %+v, want input 42 output 7", got.usage)
	}
	if len(got.blocks) != 3 {
		t.Fatalf("got %d blocks, want 3 (reasoning,text,tool_call)", len(got.blocks))
	}
	if got.blocks[0].Reasoning.Text != "think more" || got.blocks[0].Reasoning.Signature != "sig" {
		t.Errorf("reasoning = %+v, want text 'think more' sig 'sig'", got.blocks[0].Reasoning)
	}
	if got.blocks[1].Text.Text != "answer" {
		t.Errorf("text = %q, want 'answer'", got.blocks[1].Text.Text)
	}
	if got.blocks[2].ToolCall.ArgumentsRaw != `{"q":"x"}` {
		t.Errorf("tool args = %q, want {\"q\":\"x\"}", got.blocks[2].ToolCall.ArgumentsRaw)
	}
	if got.stopReason != provider.StopToolUse {
		t.Errorf("stop reason = %q, want %q", got.stopReason, provider.StopToolUse)
	}
}

func TestCollectClosesAndReturnsErr(t *testing.T) {
	wantErr := provider.New(provider.ErrOverloaded, "busy")
	m := &provider.Mock{Events: provider.TextTurn("partial", ""), StreamErr: wantErr}

	s, err := m.Stream(context.Background(), provider.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	evs, err := provider.Collect(s)
	if len(evs) != 5 {
		t.Errorf("collected %d events, want the 5 scripted before the error", len(evs))
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("Collect err = %v, want %v", err, wantErr)
	}
}

func TestStreamOpenError(t *testing.T) {
	openErr := provider.New(provider.ErrAuth, "bad key")
	m := &provider.Mock{OpenErr: openErr}

	if _, err := m.Stream(context.Background(), provider.Request{Model: "m"}); !errors.Is(err, openErr) {
		t.Errorf("Stream err = %v, want %v", err, openErr)
	}
}

func TestStreamHonorsContextCancellation(t *testing.T) {
	m := &provider.Mock{Events: provider.TextTurn("hi", "")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := m.Stream(ctx, provider.Request{Model: "m"}); !errors.Is(err, context.Canceled) {
		t.Errorf("Stream err = %v, want context.Canceled", err)
	}
}

func TestScriptFnSeesRequest(t *testing.T) {
	m := &provider.Mock{
		ScriptFn: func(_ context.Context, req provider.Request) ([]provider.Event, error) {
			return provider.TextTurn("echo:"+req.Model, ""), nil
		},
	}
	s, err := m.Stream(context.Background(), provider.Request{Model: "xyz"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := assemble(t, s)
	if got.blocks[0].Text.Text != "echo:xyz" {
		t.Errorf("text = %q, want echo:xyz", got.blocks[0].Text.Text)
	}
}
