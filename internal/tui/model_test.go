package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/schema"
)

// fakeRunner records the turn it was asked to run. Run is invoked inside a
// tea.Cmd the tests never execute, so it only needs to satisfy the interface.
type fakeRunner struct {
	text string
	err  error
}

func (f *fakeRunner) Run(_ context.Context, text string) (loop.Result, error) {
	f.text = text
	return loop.Result{}, f.err
}

// newTestModel builds a sized, renderer-free model so the transcript is raw text
// (deterministic, no terminal probing).
func newTestModel(t *testing.T, runner Runner) model {
	t.Helper()
	m := newModel(runner, Meta{Provider: "anthropic", Model: "claude-opus-4-8", Session: "abc123"},
		make(chan loop.UIEvent), nil)
	return update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
}

// update applies one message and returns the next model, failing if Update does
// not return a *model.
func update(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	next, _ := m.Update(msg)
	got, ok := next.(model)
	if !ok {
		t.Fatalf("Update returned %T, want tui.model", next)
	}
	return got
}

func sendEvent(t *testing.T, m model, ev loop.UIEvent) model {
	t.Helper()
	return update(t, m, uiEventMsg(ev))
}

func TestStreamingTextBecomesAssistantSegment(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITurnStart})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITextDelta, Text: "Hello"})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITextDelta, Text: " world"})

	if len(m.segs) != 1 || m.segs[0].kind != segAssistant {
		t.Fatalf("segs = %+v, want one assistant segment", m.segs)
	}
	if m.segs[0].text != "Hello world" {
		t.Fatalf("assistant text = %q, want %q", m.segs[0].text, "Hello world")
	}
	if m.segs[0].done {
		t.Fatal("assistant segment marked done before UITurnComplete")
	}

	// StopReason is opaque to the model; any value finalizes the text run.
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITurnComplete, StopReason: "end_turn"})
	if !m.segs[0].done {
		t.Fatal("assistant segment not finalized on UITurnComplete")
	}
}

func TestToolEventsTrackLifecycle(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITurnStart})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UIToolStarted, Tool: &loop.ToolEvent{
		Name: "read", ToolUseID: "t1", Arguments: json.RawMessage(`{}`),
	}})

	if len(m.segs) != 1 || m.segs[0].kind != segTool || m.segs[0].toolDone {
		t.Fatalf("after start: segs = %+v, want one pending tool segment", m.segs)
	}

	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UIToolFinished, Tool: &loop.ToolEvent{
		Name: "read", ToolUseID: "t1", Result: &schema.ToolResultBody{IsError: true},
	}})
	if !m.segs[0].toolDone || !m.segs[0].toolError {
		t.Fatalf("after finish: tool segment = %+v, want done+error", m.segs[0])
	}

	// A finish for an unknown id must not panic or alter other segments.
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UIToolFinished, Tool: &loop.ToolEvent{ToolUseID: "ghost"}})
	if len(m.segs) != 1 {
		t.Fatalf("unknown finish changed segs: %+v", m.segs)
	}
}

func TestPendingToolFinalizedOnCancel(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m.busy = true
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UIToolStarted, Tool: &loop.ToolEvent{Name: "shell", ToolUseID: "t9"}})
	if m.segs[0].toolDone {
		t.Fatal("tool marked done before any result")
	}

	// The loop reconciles orphaned calls without a UIToolFinished, so the turn
	// ends (cancelled) with the tool still pending.
	m = update(t, m, turnDoneMsg{err: context.Canceled})

	tool := m.segs[0]
	if !tool.toolDone || !tool.toolError {
		t.Fatalf("pending tool not finalized on cancel: %+v", tool)
	}
	if tool.toolSettled {
		t.Fatal("interrupted tool should stay unsettled so a late result can correct it")
	}

	// A late authoritative finish (success) still wins over the interrupted guess.
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UIToolFinished, Tool: &loop.ToolEvent{
		Name: "shell", ToolUseID: "t9", Result: &schema.ToolResultBody{IsError: false},
	}})
	if got := m.segs[0]; !got.toolSettled || got.toolError {
		t.Fatalf("late finish did not correct interrupted tool: %+v", got)
	}
}

func TestTextAfterToolStartsNewSegment(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITurnStart})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITextDelta, Text: "let me look"})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITurnComplete})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UIToolStarted, Tool: &loop.ToolEvent{Name: "grep", ToolUseID: "t2"}})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITurnStart})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITextDelta, Text: "found it"})

	if len(m.segs) != 3 {
		t.Fatalf("got %d segs, want 3 (assistant, tool, assistant): %+v", len(m.segs), m.segs)
	}
	if m.segs[2].kind != segAssistant || m.segs[2].text != "found it" {
		t.Fatalf("third segment = %+v, want assistant 'found it'", m.segs[2])
	}
}

func TestSubmitStartsTurn(t *testing.T) {
	runner := &fakeRunner{}
	m := newTestModel(t, runner)
	m.textarea.SetValue("do the thing")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)

	if !m.busy {
		t.Fatal("model not busy after submit")
	}
	if cmd == nil {
		t.Fatal("submit returned no command")
	}
	if len(m.segs) != 1 || m.segs[0].kind != segUser || m.segs[0].text != "do the thing" {
		t.Fatalf("segs = %+v, want one user segment", m.segs)
	}
	if got := strings.TrimSpace(m.textarea.Value()); got != "" {
		t.Fatalf("input not cleared after submit: %q", got)
	}
	if len(m.history) != 1 || m.history[0] != "do the thing" {
		t.Fatalf("history = %v, want one entry", m.history)
	}
}

func TestSubmitIgnoredWhenBusyOrEmpty(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})

	// Empty input: no turn.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.busy || cmd != nil || len(m.segs) != 0 {
		t.Fatalf("empty submit started a turn: busy=%v segs=%d", m.busy, len(m.segs))
	}

	// While busy a second Enter is a no-op.
	m.textarea.SetValue("first")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m.textarea.SetValue("second")
	before := len(m.segs)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.segs) != before {
		t.Fatalf("submit while busy appended a segment: %+v", m.segs)
	}
}

func TestEscCancelsInFlightTurn(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m.textarea.SetValue("long task")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.busy || m.cancel == nil {
		t.Fatal("turn did not start")
	}

	cancelled := make(chan struct{})
	got := m.cancel
	m.cancel = func() { close(cancelled); got() }

	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	select {
	case <-cancelled:
	default:
		t.Fatal("Esc did not cancel the in-flight turn")
	}
}

func TestTurnDoneRecordsOutcome(t *testing.T) {
	cancelM := newTestModel(t, &fakeRunner{})
	cancelM.busy = true
	cancelM = update(t, cancelM, turnDoneMsg{err: context.Canceled})
	if cancelM.busy {
		t.Fatal("still busy after turn done")
	}
	if n := len(cancelM.segs); n != 1 || cancelM.segs[0].kind != segNotice {
		t.Fatalf("cancel outcome segs = %+v, want one notice", cancelM.segs)
	}

	errM := newTestModel(t, &fakeRunner{})
	errM.busy = true
	errM = update(t, errM, turnDoneMsg{err: errBoom})
	if n := len(errM.segs); n != 1 || errM.segs[0].kind != segError {
		t.Fatalf("error outcome segs = %+v, want one error", errM.segs)
	}
}

var errBoom = errString("boom")

type errString string

func (e errString) Error() string { return string(e) }

func TestHistoryNavigation(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	for _, msg := range []string{"first", "second"} {
		m.textarea.SetValue(msg)
		m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		// Each submit leaves busy=true; clear it so the next can run.
		m = update(t, m, turnDoneMsg{})
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if got := m.textarea.Value(); got != "second" {
		t.Fatalf("up once = %q, want %q", got, "second")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if got := m.textarea.Value(); got != "first" {
		t.Fatalf("up twice = %q, want %q", got, "first")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if got := m.textarea.Value(); got != "second" {
		t.Fatalf("down = %q, want %q", got, "second")
	}
}

func TestViewRendersStatusAndTranscript(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITurnStart})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITextDelta, Text: "streaming reply"})

	view := m.View()
	for _, want := range []string{"anthropic", "claude-opus-4-8", "streaming reply", "ready"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}
