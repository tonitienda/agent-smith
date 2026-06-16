package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/loop"
)

// key builds a KeyMsg for a named control/navigation key (e.g. "ctrl+g").
func key(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "ctrl+g":
		return tea.KeyMsg{Type: tea.KeyCtrlG}
	}
	// Single-rune keys ("h", "$", …) arrive as KeyRunes.
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

// TestLeaderHotkeyOpensPanel covers AC1/AC2: the leader chord (ctrl+g, then a
// bound key) opens a full-screen panel through the same host the palette uses,
// while the chord key never leaks into the prompt.
func TestLeaderHotkeyOpensPanel(t *testing.T) {
	reg := command.NewRegistry()
	if err := reg.Register(command.HelpCommand(reg)); err != nil {
		t.Fatalf("register help: %v", err)
	}
	m := newCommandModel(t, reg)

	// ctrl+g arms the leader; nothing is typed and no panel is open yet.
	m = update(t, m, key("ctrl+g"))
	if !m.leader {
		t.Fatal("ctrl+g did not arm the leader chord")
	}
	if m.panelOpen() {
		t.Fatal("panel opened before the selector key")
	}

	// "h" is bound to /help: it opens the panel and is not typed into the input.
	next, cmd := m.Update(key("h"))
	m = next.(model)
	if m.leader {
		t.Fatal("leader still armed after the selector key")
	}
	if got := m.textarea.Value(); got != "" {
		t.Fatalf("selector key leaked into the prompt: %q", got)
	}
	m = runCmd(t, m, cmd)
	if !m.panelOpen() {
		t.Fatal("leader hotkey did not open the panel")
	}

	// The pinned status line renders above the panel body (D-TUI-3).
	view := m.View()
	if !strings.Contains(view, "anthropic") {
		t.Fatalf("panel view missing pinned status line:\n%s", view)
	}
}

// TestLeaderUnknownKeyCancelsWithoutTyping covers AC2: an unbound key after the
// leader cancels the chord and does not become input.
func TestLeaderUnknownKeyCancelsWithoutTyping(t *testing.T) {
	m := newCommandModel(t, command.NewRegistry())
	m = update(t, m, key("ctrl+g"))
	m = update(t, m, key("z")) // "z" is unbound
	if m.leader {
		t.Fatal("leader still armed after an unbound key")
	}
	if got := m.textarea.Value(); got != "" {
		t.Fatalf("unbound leader key leaked into the prompt: %q", got)
	}
}

// TestBareLettersAlwaysType is the AC2 invariant: outside a chord, letters go to
// the prompt — they never trigger a panel.
func TestBareLettersAlwaysType(t *testing.T) {
	m := newCommandModel(t, command.NewRegistry())
	m = typeString(t, m, "hi")
	if got := m.textarea.Value(); got != "hi" {
		t.Fatalf("bare letters did not reach the prompt: %q", got)
	}
	if m.panelOpen() {
		t.Fatal("bare letters opened a panel")
	}
}

// TestModalTrapsFocusAndDecides covers AC3: a modal swallows ordinary keys
// (focus trapped), moves selection only on arrows, and reports the chosen index.
func TestModalTrapsFocusAndDecides(t *testing.T) {
	m := newCommandModel(t, command.NewRegistry())

	decided := -1
	m.openModal(modal{
		title:   "Run shell command?",
		detail:  "rm -rf build/",
		choices: []string{"Deny", "Allow"},
		decide: func(choice int) tea.Cmd {
			decided = choice
			return nil
		},
	})
	if !m.modalOpen() {
		t.Fatal("openModal did not open the overlay")
	}

	// A bare letter is swallowed: focus is trapped and nothing is typed.
	m = update(t, m, key("a"))
	if !m.modalOpen() {
		t.Fatal("a bare key dismissed the modal")
	}
	if got := m.textarea.Value(); got != "" {
		t.Fatalf("modal leaked a key to the prompt: %q", got)
	}

	// The verbatim detail is shown (UX.md §11).
	if !strings.Contains(m.View(), "rm -rf build/") {
		t.Fatalf("modal view did not show the verbatim command:\n%s", m.View())
	}

	// Right then Enter selects "Allow" (index 1).
	m = update(t, m, key("right"))
	m = update(t, m, key("enter"))
	if m.modalOpen() {
		t.Fatal("modal still open after Enter")
	}
	if decided != 1 {
		t.Fatalf("decided = %d, want 1 (Allow)", decided)
	}
}

// TestModalEscDeniesByDefault covers AC3: Esc resolves to the safe deny choice.
func TestModalEscDeniesByDefault(t *testing.T) {
	m := newCommandModel(t, command.NewRegistry())
	got := -1
	m.openModal(modal{
		title:   "Delete?",
		choices: []string{"Deny", "Allow"},
		decide:  func(choice int) tea.Cmd { got = choice; return nil },
	})
	m = update(t, m, key("right")) // move to Allow…
	m = update(t, m, key("esc"))   // …but Esc still denies
	if m.modalOpen() {
		t.Fatal("Esc did not dismiss the modal")
	}
	if got != 0 {
		t.Fatalf("Esc decided %d, want 0 (deny default)", got)
	}
}

// TestModalWrapsLongDetail guards against the layout glitch where a long
// verbatim command/path overflows the terminal (D-TUI-11) — including very
// narrow terminals where a fixed minimum wrap width would itself overflow.
func TestModalWrapsLongDetail(t *testing.T) {
	for _, width := range []int{16, 40, 60, 80} {
		m := newCommandModel(t, command.NewRegistry())
		m = update(t, m, tea.WindowSizeMsg{Width: width, Height: 24})
		m.openModal(modal{
			title:   "Run shell command?",
			detail:  strings.Repeat("rm -rf /very/long/path/segment ", 10),
			choices: []string{"Deny", "Allow"},
		})
		for _, line := range strings.Split(m.View(), "\n") {
			if w := lipglossWidth(line); w > m.width {
				t.Fatalf("width %d: modal line overflows terminal: %d > %d:\n%q", width, w, m.width, line)
			}
		}
	}
}

// TestPanelFooterDropsBeforeOverflow covers the inspect-mode footer degrade: on
// an extremely short terminal the footer keybar is dropped so the panel body
// renders alone instead of overflowing (D-TUI-11).
func TestPanelFooterDropsBeforeOverflow(t *testing.T) {
	reg := command.NewRegistry()
	if err := reg.Register(command.HelpCommand(reg)); err != nil {
		t.Fatalf("register help: %v", err)
	}
	m := newCommandModel(t, reg)
	m = update(t, m, key("ctrl+g"))
	next, cmd := m.Update(key("h"))
	m = runCmd(t, next.(model), cmd)
	if !m.panelOpen() {
		t.Fatal("panel did not open")
	}
	m = update(t, m, tea.WindowSizeMsg{Width: 40, Height: 1})
	if m.panelFooterRows() != 0 {
		t.Fatalf("footer not dropped at height 1: panelFooterRows=%d", m.panelFooterRows())
	}
	if got := strings.Count(m.View(), "\n") + 1; got > 1 {
		t.Fatalf("inspect view is %d rows, want <= 1:\n%s", got, m.View())
	}
}

// TestPaletteFitsTinyWindow covers fix for paletteHeight reserving the status
// row unconditionally: with the palette open in a tiny window, the rendered
// sections must still fit the terminal height (D-TUI-11).
func TestPaletteFitsTinyWindow(t *testing.T) {
	reg := command.NewRegistry()
	for _, n := range []string{"cost", "context", "clean", "clear", "config"} {
		if err := reg.Register(command.Command{Name: n, Run: nopHandler}); err != nil {
			t.Fatalf("register %q: %v", n, err)
		}
	}
	m := newCommandModel(t, reg)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 4})
	m = typeString(t, m, "/c") // matches all five
	if got := strings.Count(m.View(), "\n") + 1; got > 4 {
		t.Fatalf("view with palette is %d rows, want <= 4:\n%s", got, m.View())
	}
}

// TestStartupHeaderRendersAndSuppresses covers AC4: the header shows by default
// and is hidden when splash is off.
func TestStartupHeaderRendersAndSuppresses(t *testing.T) {
	meta := staticMeta(Meta{Provider: "anthropic", Model: "claude-opus-4-8", Session: "s", Project: "agent-smith"})

	on := newModel(&fakeRunner{}, meta, make(chan loop.UIEvent), nil, nil, nil, true, nil)
	on = update(t, on, tea.WindowSizeMsg{Width: 80, Height: 24})
	view := on.View()
	if !strings.Contains(view, "AGENT SMITH") {
		t.Fatalf("startup header missing with splash on:\n%s", view)
	}
	if !strings.Contains(view, "agent-smith") {
		t.Fatalf("header missing project label:\n%s", view)
	}

	off := newModel(&fakeRunner{}, meta, make(chan loop.UIEvent), nil, nil, nil, false, nil)
	off = update(t, off, tea.WindowSizeMsg{Width: 80, Height: 24})
	if strings.Contains(off.View(), "AGENT SMITH") {
		t.Fatalf("startup header shown with splash off:\n%s", off.View())
	}
}

// TestDegradesBelowLayoutMinimum covers AC5: at a tiny height the status line is
// dropped (rather than glitching) but the prompt still renders.
func TestDegradesBelowLayoutMinimum(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})

	// At a comfortable height the status line shows its state word.
	if !strings.Contains(m.View(), "ready") {
		t.Fatalf("status line not shown at height 24:\n%s", m.View())
	}

	// Below the layout minimum the status line is the first chrome to drop, so the
	// prompt still renders instead of a glitch (D-TUI-11).
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 4})
	if m.statusRows() != 0 {
		t.Fatalf("status line not dropped at height 4: statusRows=%d", m.statusRows())
	}
	view := m.View()
	if strings.Contains(view, "ready") {
		t.Fatalf("status line still shown after degrade:\n%s", view)
	}
	if got := strings.Count(view, "\n") + 1; got > 4 {
		t.Fatalf("degraded view is %d rows, want <= 4:\n%s", got, view)
	}
}
