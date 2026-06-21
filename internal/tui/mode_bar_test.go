package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonitienda/agent-smith/internal/loop"
)

// modeMeta is a Meta with an active Coding Mode for the phase-tracker chrome
// tests (AS-073). The tracker and panel are pre-rendered, mirroring the
// controller (the face never imports the mode package).
func modeMeta() Meta {
	return Meta{
		Provider:     "anthropic",
		Model:        "claude-opus-4-8",
		Session:      "abc123",
		Mode:         "coding",
		PhaseTracker: "think · [analyse] · plan",
		ModePanel:    "Mode: coding · phase: analyse\nGoal: ship it",
	}
}

// newMetaModel builds a sized, renderer-free model over a fixed Meta.
func newMetaModel(t *testing.T, m Meta) model {
	t.Helper()
	mod := newModel(&fakeRunner{}, staticMeta(m), make(chan loop.UIEvent), nil, nil, nil, false, nil, nil, nil)
	return update(t, mod, tea.WindowSizeMsg{Width: 80, Height: 24})
}

// TestModeBarShownWhileInMode covers AC1: entering a mode pins the phase tracker
// with the current phase highlighted, and it carries the mode name plus the
// panel hint.
func TestModeBarShownWhileInMode(t *testing.T) {
	m := newMetaModel(t, modeMeta())
	if m.modeBarRows() != 1 {
		t.Fatalf("modeBarRows = %d, want 1 while in mode", m.modeBarRows())
	}
	view := m.View()
	for _, want := range []string{"coding", "[analyse]", "Ctrl+G m"} {
		if !strings.Contains(view, want) {
			t.Fatalf("mode bar view missing %q:\n%s", want, view)
		}
	}
}

// TestModeBarHiddenWithoutMode covers AC3: with no mode active there is no
// residual phase-tracker chrome.
func TestModeBarHiddenWithoutMode(t *testing.T) {
	m := newMetaModel(t, Meta{Provider: "anthropic", Model: "claude-opus-4-8", Session: "abc123"})
	if m.modeBarRows() != 0 {
		t.Fatalf("modeBarRows = %d, want 0 with no mode", m.modeBarRows())
	}
	if got := m.View(); strings.Contains(got, "Ctrl+G m") {
		t.Fatalf("mode bar chrome leaked with no mode active:\n%s", got)
	}
}

// TestLeaderMOpensModePanel covers AC2: the leader chord ctrl+g then m opens the
// richer mode panel (goal, tracker, phases) without typing the key, and the key
// does not leak when no mode is active.
func TestLeaderMOpensModePanel(t *testing.T) {
	m := newMetaModel(t, modeMeta())
	m = update(t, m, key("ctrl+g"))
	m = update(t, m, key("m"))
	if m.leader {
		t.Fatal("leader still armed after m")
	}
	if !m.panelOpen() {
		t.Fatal("ctrl+g m did not open the mode panel")
	}
	if got := m.panelTitle; got != "mode" {
		t.Fatalf("panel title = %q, want \"mode\"", got)
	}
	if got := m.panel.View(); !strings.Contains(got, "Goal: ship it") {
		t.Fatalf("mode panel missing detail:\n%s", got)
	}

	// With no mode active the chord is a no-op and the key never reaches input.
	off := newMetaModel(t, Meta{Provider: "anthropic", Model: "claude-opus-4-8", Session: "abc123"})
	off = update(t, off, key("ctrl+g"))
	off = update(t, off, key("m"))
	if off.panelOpen() {
		t.Fatal("ctrl+g m opened a panel with no mode active")
	}
	if got := off.textarea.Value(); got != "" {
		t.Fatalf("leader m leaked into the prompt: %q", got)
	}
}
