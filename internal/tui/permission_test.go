package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/schema"
)

// busyModel is a sized model marked busy, the state in which permission prompts
// arrive (only mid-turn). Tests drive prompts straight in via permPromptMsg.
func busyModel(t *testing.T) model {
	t.Helper()
	m := newTestModel(t, &fakeRunner{})
	m.busy = true
	return m
}

func sendPrompt(t *testing.T, m model, p PermissionPrompt, reply chan PermissionDecision) model {
	t.Helper()
	return update(t, m, permPromptMsg{prompt: p, reply: reply})
}

// TestInlinePromptShowsDiffAndRemembers covers AC2 and the "always allow" path: a
// non-destructive edit renders an inline card with a verbatim diff, and choosing
// "Always allow" returns a remembering decision.
func TestInlinePromptShowsDiffAndRemembers(t *testing.T) {
	m := busyModel(t)
	reply := make(chan PermissionDecision, 1)
	m = sendPrompt(t, m, PermissionPrompt{
		Tool:    "edit",
		Subject: "main.go",
		Detail:  "- old line\n+ new line",
	}, reply)

	if !m.permActive() {
		t.Fatal("prompt did not become active")
	}
	if m.perm.prompt.Destructive {
		t.Fatal("edit prompt should be inline, not destructive")
	}
	view := m.View()
	for _, want := range []string{"main.go", "- old line", "+ new line"} {
		if !strings.Contains(view, want) {
			t.Fatalf("inline card view missing %q:\n%s", want, view)
		}
	}

	// Move to "Always allow" (index 2) and confirm.
	m = update(t, m, key("right"))
	m = update(t, m, key("right"))
	m = update(t, m, key("enter"))

	d := <-reply
	if !d.Allow || !d.Remember {
		t.Fatalf("decision = %+v, want Allow+Remember", d)
	}
	if m.permActive() {
		t.Fatal("prompt still active after decision")
	}
}

// TestDestructivePromptTrapsFocusAndShowsCommand covers AC4 and D-TUI-8: a shell
// prompt is a focus-trapped modal that shows the exact command verbatim; a bare
// key is swallowed, and Esc denies.
func TestDestructivePromptTrapsFocusAndShowsCommand(t *testing.T) {
	m := busyModel(t)
	reply := make(chan PermissionDecision, 1)
	m = sendPrompt(t, m, PermissionPrompt{
		Tool:        "shell",
		Subject:     "rm -rf build/",
		Destructive: true,
	}, reply)

	if !strings.Contains(m.View(), "rm -rf build/") {
		t.Fatalf("modal did not show the verbatim command:\n%s", m.View())
	}

	// A bare letter is swallowed: focus is trapped, nothing is typed.
	m = update(t, m, key("a"))
	if !m.permActive() {
		t.Fatal("a bare key dismissed the destructive prompt")
	}
	if got := m.textarea.Value(); got != "" {
		t.Fatalf("destructive prompt leaked a key to the input: %q", got)
	}

	// Esc denies (the safe default).
	m = update(t, m, key("esc"))
	d := <-reply
	if d.Allow {
		t.Fatalf("Esc allowed the call; want deny, got %+v", d)
	}
}

// TestPromptsQueueOneAtATime covers the AS-019 concurrency case: a second prompt
// waits behind the first and becomes active once the first is decided.
func TestPromptsQueueOneAtATime(t *testing.T) {
	m := busyModel(t)
	r1 := make(chan PermissionDecision, 1)
	r2 := make(chan PermissionDecision, 1)
	m = sendPrompt(t, m, PermissionPrompt{Tool: "read", Subject: "a.go"}, r1)
	m = sendPrompt(t, m, PermissionPrompt{Tool: "read", Subject: "b.go"}, r2)

	if m.perm.prompt.Subject != "a.go" {
		t.Fatalf("active prompt = %q, want a.go", m.perm.prompt.Subject)
	}
	if len(m.permQueue) != 1 {
		t.Fatalf("queue len = %d, want 1", len(m.permQueue))
	}

	// Allow once on the first (index 1).
	m = update(t, m, key("right"))
	m = update(t, m, key("enter"))
	if d := <-r1; !d.Allow || d.Remember {
		t.Fatalf("first decision = %+v, want allow-once", d)
	}
	if m.perm == nil || m.perm.prompt.Subject != "b.go" {
		t.Fatalf("second prompt did not become active: %+v", m.perm)
	}

	m = update(t, m, key("enter")) // deny (index 0) the second
	if d := <-r2; d.Allow {
		t.Fatalf("second decision = %+v, want deny", d)
	}
	if m.permActive() {
		t.Fatal("a prompt is still active after both were decided")
	}
}

// TestPromptAfterTurnEndsAutoDenied guards the late-delivery race: a prompt that
// lands once the turn is no longer busy is auto-denied instead of blocking the
// idle UI.
func TestPromptAfterTurnEndsAutoDenied(t *testing.T) {
	m := newTestModel(t, &fakeRunner{}) // not busy
	reply := make(chan PermissionDecision, 1)
	m = sendPrompt(t, m, PermissionPrompt{Tool: "read", Subject: "a.go"}, reply)
	if m.permActive() {
		t.Fatal("a prompt activated after the turn ended")
	}
	if d := <-reply; d.Allow {
		t.Fatalf("late prompt = %+v, want auto-deny", d)
	}
}

// TestFinishTurnClearsPendingPrompts ensures a cancelled or completed turn drops a
// prompt still on screen, answering it as a denial.
func TestFinishTurnClearsPendingPrompts(t *testing.T) {
	m := busyModel(t)
	reply := make(chan PermissionDecision, 1)
	m = sendPrompt(t, m, PermissionPrompt{Tool: "shell", Subject: "ls", Destructive: true}, reply)
	if !m.permActive() {
		t.Fatal("prompt not active before turn end")
	}
	m = m.finishTurn(turnDoneMsg{})
	if m.permActive() {
		t.Fatal("prompt still active after the turn ended")
	}
	if d := <-reply; d.Allow {
		t.Fatalf("cleared prompt = %+v, want deny", d)
	}
}

// TestSummarizeToolArgsKeepsNonStrings guards the transparency requirement: a
// non-string argument (number, bool) is shown, not silently dropped.
func TestSummarizeToolArgsKeepsNonStrings(t *testing.T) {
	got := summarizeToolArgs([]byte(`{"path":"f.go","limit":40,"replace_all":true}`))
	for _, want := range []string{"path: f.go", "limit: 40", "replace_all: true"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary %q missing %q", got, want)
		}
	}
}

// TestToolCardShowsArgsAndExpandablePreview covers AC1: a tool call is visible
// with summarized args and a previewed result, expandable in full by the leader
// toggle.
func TestToolCardShowsArgsAndExpandablePreview(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UITurnStart})
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UIToolStarted, Tool: &loop.ToolEvent{
		Name: "read", ToolUseID: "t1", Arguments: json.RawMessage(`{"path":"main.go"}`),
	}})

	// The summarized argument is on the running card.
	if got := m.segs[0].toolArgs; !strings.Contains(got, "path: main.go") {
		t.Fatalf("tool args summary = %q, want it to mention path: main.go", got)
	}
	if !strings.Contains(m.View(), "path: main.go") {
		t.Fatalf("running tool card did not show the args:\n%s", m.View())
	}

	// A long result is previewed (collapsed) with an expand hint.
	body := "L1\nL2\nL3\nL4\nL5\nL6\nL7\nL8\nL9\nL10"
	m = sendEvent(t, m, loop.UIEvent{Kind: loop.UIToolFinished, Tool: &loop.ToolEvent{
		Name: "read", ToolUseID: "t1",
		Result: &schema.ToolResultBody{Content: []schema.Part{{Type: "text", Text: body}}},
	}})
	collapsed := m.View()
	if strings.Contains(collapsed, "L10") {
		t.Fatalf("collapsed card showed the full result:\n%s", collapsed)
	}
	if !strings.Contains(collapsed, "more line") {
		t.Fatalf("collapsed card missing the expand hint:\n%s", collapsed)
	}

	// Ctrl+G then t expands every tool result.
	m = update(t, m, key("ctrl+g"))
	m = update(t, m, key("t"))
	if !m.expandTools {
		t.Fatal("Ctrl+G t did not toggle tool expansion")
	}
	if !strings.Contains(m.View(), "L10") {
		t.Fatalf("expanded card did not show the full result:\n%s", m.View())
	}
}
