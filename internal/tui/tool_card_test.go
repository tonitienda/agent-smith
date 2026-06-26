package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/schema"
)

func TestBrailleSpinnerFrameWraps(t *testing.T) {
	n := len(brailleSpinnerFrames)
	if brailleSpinnerFrame(0) != brailleSpinnerFrame(n) {
		t.Fatalf("frame 0 (%q) != frame n (%q)", brailleSpinnerFrame(0), brailleSpinnerFrame(n))
	}
	// A negative index must stay within the cycle rather than panic or go empty.
	if got := brailleSpinnerFrame(-1); got != brailleSpinnerFrame(n-1) {
		t.Fatalf("frame -1 = %q, want %q", got, brailleSpinnerFrame(n-1))
	}
}

func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{1300 * time.Millisecond, "1.3 s"},
		{12 * time.Second, "12 s"},
		{90 * time.Second, "1m30s"},
	}
	for _, c := range cases {
		if got := formatElapsed(c.d); got != c.want {
			t.Errorf("formatElapsed(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}

// A running tool card shows the amber braille spinner and a live elapsed time;
// once it settles the spinner is replaced by a green ✓ and the elapsed freezes.
func TestToolCardRunningThenSettled(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITurnStart})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UIToolStarted, Tool: &loop.ToolEvent{
		Name: "read_file", ToolUseID: "t1", Arguments: json.RawMessage(`{"path":"foo.go"}`),
	}})

	running := stripANSI(m.renderTranscript())
	if !strings.Contains(running, brailleSpinnerFrame(m.spinnerFrame)) {
		t.Fatalf("running card missing spinner glyph: %q", running)
	}
	if strings.Contains(running, "✓") {
		t.Fatalf("running card should not show ✓ yet: %q", running)
	}

	res := toolResult("ok")
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UIToolFinished, Tool: &loop.ToolEvent{
		Name: "read_file", ToolUseID: "t1", Result: &res,
	}})

	done := stripANSI(m.renderTranscript())
	if !strings.Contains(done, "✓") {
		t.Fatalf("settled card missing ✓: %q", done)
	}
	for _, f := range brailleSpinnerFrames {
		if strings.ContainsRune(done, f) {
			t.Fatalf("settled card still shows spinner glyph %q: %q", string(f), done)
		}
	}
}

// A failed tool call renders a red ✗ instead of the success ✓.
func TestToolCardErrorIcon(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITurnStart})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UIToolStarted, Tool: &loop.ToolEvent{
		Name: "shell", ToolUseID: "t1", Arguments: json.RawMessage(`{}`),
	}})
	res := schema.ToolResultBody{ToolUseID: "t1", IsError: true, Content: []schema.Part{{Type: "text", Text: "boom"}}}
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UIToolFinished, Tool: &loop.ToolEvent{
		Name: "shell", ToolUseID: "t1", Result: &res,
	}})
	if got := stripANSI(m.renderTranscript()); !strings.Contains(got, "✗") {
		t.Fatalf("error card missing ✗: %q", got)
	}
}

// The output block is fronted by a left │ rule under the status row.
func TestToolCardOutputLeftRule(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITurnStart})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UIToolStarted, Tool: &loop.ToolEvent{
		Name: "read_file", ToolUseID: "t1", Arguments: json.RawMessage(`{}`),
	}})
	res := toolResult("line one\nline two")
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UIToolFinished, Tool: &loop.ToolEvent{
		Name: "read_file", ToolUseID: "t1", Result: &res,
	}})
	got := stripANSI(m.renderTranscript())
	if !strings.Contains(got, "│ line one") {
		t.Fatalf("output block missing left rule: %q", got)
	}
}

// A rehydrated card (no live start stamp) shows no elapsed time and never ticks.
func TestRehydratedCardHasNoElapsed(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	s := &segment{kind: segTool, toolName: "read_file", toolDone: true, toolSettled: true}
	if got := m.toolElapsedLabel(s); got != "" {
		t.Fatalf("rehydrated card elapsed = %q, want empty", got)
	}
}

// The spinner frame advances on each spinner tick while busy and holds when idle.
func TestSpinnerFrameAdvancesOnlyWhenBusy(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m.busy = true
	m = update(t, m, spinner.TickMsg{})
	if m.spinnerFrame != 1 {
		t.Fatalf("busy spinnerFrame = %d, want 1", m.spinnerFrame)
	}
	m.busy = false
	m = update(t, m, spinner.TickMsg{})
	if m.spinnerFrame != 1 {
		t.Fatalf("idle spinnerFrame advanced to %d, want held at 1", m.spinnerFrame)
	}
}

// toolResult builds a text-only schema.ToolResultBody for the card-render tests.
func toolResult(text string) schema.ToolResultBody {
	return schema.ToolResultBody{ToolUseID: "t1", Content: []schema.Part{{Type: "text", Text: text}}}
}
