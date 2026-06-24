package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonitienda/agent-smith/internal/loop"
)

// TestSplashRuleAndInvite covers AS-122 §7.1: with splash on the empty screen
// shows the logo, an underrule, the context line, the invite headline, and the
// static command-hint row.
func TestSplashRuleAndInvite(t *testing.T) {
	meta := staticMeta(Meta{Provider: "anthropic", Model: "claude-opus-4-8", Session: "s", Project: "agent-smith"})
	m := newModel(&fakeRunner{}, meta, make(chan loop.UIEvent), nil, nil, nil, true, nil, nil, nil)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	view := stripANSI(m.View())
	for _, want := range []string{
		"▞▞ AGENT SMITH",
		"claude-opus-4-8",
		"────────",
		"Ask Agent Smith anything to begin.",
		"type / for commands · Ctrl+G c context · /serious mute theme",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("splash view missing %q:\n%s", want, view)
		}
	}
}

// TestSplashRuleFullWidth covers AS-122: the underrule spans the transcript width.
func TestSplashRuleFullWidth(t *testing.T) {
	meta := staticMeta(Meta{Model: "m", Project: "p"})
	m := newModel(&fakeRunner{}, meta, make(chan loop.UIEvent), nil, nil, nil, true, nil, nil, nil)
	m = update(t, m, tea.WindowSizeMsg{Width: 60, Height: 24})

	if got := m.ruleWidth(); got != m.viewport.Width {
		t.Fatalf("ruleWidth() = %d, want viewport width %d", got, m.viewport.Width)
	}
	if got := stripANSI(m.headerView()); !strings.Contains(got, strings.Repeat("─", m.viewport.Width)) {
		t.Fatalf("header rule not full width (%d):\n%s", m.viewport.Width, got)
	}
}

// TestNoSplashSuppressesInvite covers AS-122 AC4: --no-splash hides everything
// above the input bar, including the invite copy.
func TestNoSplashSuppressesInvite(t *testing.T) {
	meta := staticMeta(Meta{Model: "m"})
	m := newModel(&fakeRunner{}, meta, make(chan loop.UIEvent), nil, nil, nil, false, nil, nil, nil)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	if view := stripANSI(m.View()); strings.Contains(view, "Ask Agent Smith") {
		t.Fatalf("invite shown with --no-splash:\n%s", view)
	}
}

// TestCaretBlinkTogglesColor covers AS-122: the gutter caret blinks brand/dim
// while empty and stays solid brand once the user types.
func TestCaretBlinkTogglesColor(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})

	m.caretVisible = true
	m.applyCaret()
	if got := m.textarea.FocusedStyle.Prompt.GetForeground(); got != ColorBrand {
		t.Fatalf("caret on: foreground = %v, want ColorBrand", got)
	}

	m.caretVisible = false
	m.applyCaret()
	if got := m.textarea.FocusedStyle.Prompt.GetForeground(); got != ColorDim {
		t.Fatalf("caret off (empty input): foreground = %v, want ColorDim", got)
	}

	// Typing forces it solid even on the off-beat.
	m.textarea.SetValue("hi")
	m.applyCaret()
	if got := m.textarea.FocusedStyle.Prompt.GetForeground(); got != ColorBrand {
		t.Fatalf("caret with input: foreground = %v, want solid ColorBrand", got)
	}

	// Whitespace is still a typed character: it must keep the caret solid too.
	m.textarea.SetValue("   ")
	m.applyCaret()
	if got := m.textarea.FocusedStyle.Prompt.GetForeground(); got != ColorBrand {
		t.Fatalf("caret with space input: foreground = %v, want solid ColorBrand", got)
	}

	// The blink tick flips visibility and re-arms.
	before := m.caretVisible
	m = update(t, m, caretBlinkMsg{})
	if m.caretVisible == before {
		t.Fatal("caretBlinkMsg did not toggle caretVisible")
	}
}
