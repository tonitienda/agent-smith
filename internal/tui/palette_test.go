package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/loop"
)

// recorder captures the args a command handler was called with.
type recorder struct {
	called bool
	args   []string
}

func (r *recorder) handler(out command.Output) command.Handler {
	return func(_ context.Context, args []string) (command.Output, error) {
		r.called = true
		r.args = args
		return out, nil
	}
}

// newCommandModel builds a sized model wired to the given registry.
func newCommandModel(t *testing.T, reg *command.Registry) model {
	t.Helper()
	m := newModel(&fakeRunner{}, staticMeta(Meta{Provider: "anthropic", Model: "m", Session: "s"}),
		make(chan loop.UIEvent), nil, reg, nil, false, nil, nil, nil)
	return update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
}

// typeString feeds s into the model one rune at a time, as the real key handler
// would, so the palette syncs on each keystroke.
func typeString(t *testing.T, m model, s string) model {
	t.Helper()
	for _, r := range s {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// runCmd executes a returned tea.Cmd and feeds its message back into the model,
// so a dispatched command's result is applied.
func runCmd(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command, got nil")
	}
	return update(t, m, cmd())
}

func sampleRegistry(t *testing.T, handlers ...command.Command) *command.Registry {
	t.Helper()
	reg := command.NewRegistry()
	for _, c := range handlers {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register %q: %v", c.Name, err)
		}
	}
	return reg
}

func TestPaletteOpensAndFilters(t *testing.T) {
	reg := sampleRegistry(t,
		command.Command{Name: "cost", Summary: "cost", Run: nopHandler},
		command.Command{Name: "context", Summary: "context", Run: nopHandler},
		command.Command{Name: "model", Summary: "model", Run: nopHandler},
	)
	m := newCommandModel(t, reg)

	// Typing "/" opens the palette listing every command.
	m = typeString(t, m, "/")
	if !m.palette.open || len(m.palette.matches) != 3 {
		t.Fatalf("after '/': open=%v matches=%d, want open with 3", m.palette.open, len(m.palette.matches))
	}

	// Narrowing filters to the matching subset, ranked.
	m = typeString(t, m, "co")
	got := paletteNames(m.palette.matches)
	if !reflect.DeepEqual(got, []string{"cost", "context"}) {
		t.Fatalf("after '/co': matches = %v, want [cost context]", got)
	}

	// A space (the first argument) closes the palette.
	m = typeString(t, m, "st ")
	if m.palette.open {
		t.Fatalf("palette still open after a space: matches=%v", paletteNames(m.palette.matches))
	}
}

func TestPaletteNavigationAndTabComplete(t *testing.T) {
	reg := sampleRegistry(t,
		command.Command{Name: "cost", Run: nopHandler},
		command.Command{Name: "context", Run: nopHandler},
	)
	m := newCommandModel(t, reg)
	m = typeString(t, m, "/co")

	if m.palette.sel != 0 {
		t.Fatalf("initial selection = %d, want 0", m.palette.sel)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.palette.sel != 1 {
		t.Fatalf("after down: selection = %d, want 1", m.palette.sel)
	}
	// Selection clamps at the bottom.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.palette.sel != 1 {
		t.Fatalf("after second down: selection = %d, want clamped 1", m.palette.sel)
	}

	// Tab completes the highlighted command with a trailing space.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if got := m.textarea.Value(); got != "/context " {
		t.Fatalf("after tab: input = %q, want %q", got, "/context ")
	}
	if m.palette.open {
		t.Fatal("palette should close after tab completion adds a space")
	}
}

func TestEnterDispatchesHighlightedCommand(t *testing.T) {
	rec := &recorder{}
	reg := sampleRegistry(t,
		command.Command{Name: "context", Run: nopHandler},
		command.Command{Name: "cost", Summary: "show cost", Mode: command.Inline, Run: rec.handler(command.Output{Text: "$0.01"})},
	)
	m := newCommandModel(t, reg)
	m = typeString(t, m, "/co") // palette: [cost, context], cost highlighted

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if got := strings.TrimSpace(m.textarea.Value()); got != "" {
		t.Fatalf("input not cleared after dispatch: %q", got)
	}
	m = runCmd(t, m, cmd)

	if !rec.called {
		t.Fatal("highlighted command handler not invoked")
	}
	if len(m.segs) != 1 || m.segs[0].kind != segCommand || !strings.Contains(m.segs[0].text, "$0.01") {
		t.Fatalf("inline output not rendered: segs = %+v", m.segs)
	}
}

// TestCommandBlockedWhileBusy guards the AS-023 race fix: a command must not
// dispatch while a turn is in flight (it could swap the session and clear the
// transcript under the still-streaming turn). The handler stays uncalled and the
// user gets a notice instead.
func TestCommandBlockedWhileBusy(t *testing.T) {
	rec := &recorder{}
	reg := sampleRegistry(t,
		command.Command{Name: "clear", Summary: "fresh session", Mode: command.Inline, Run: rec.handler(command.Output{Text: "new"})},
	)
	m := newCommandModel(t, reg)
	m.busy = true
	m = typeString(t, m, "/clear")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if cmd != nil {
		t.Fatal("a command was dispatched while a turn was in flight")
	}
	if rec.called {
		t.Fatal("command handler ran while busy")
	}
	if len(m.segs) != 1 || m.segs[0].kind != segNotice {
		t.Fatalf("expected a single notice segment, got %+v", m.segs)
	}
}

func TestCommandWithQuotedArgs(t *testing.T) {
	rec := &recorder{}
	reg := sampleRegistry(t,
		command.Command{Name: "clean", Args: `"<topic>"`, Mode: command.Inline, Run: rec.handler(command.Output{Text: "ok"})},
	)
	m := newCommandModel(t, reg)
	// A space closes the palette, so this dispatches via the parse path.
	m.textarea.SetValue(`/clean "old api"`)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	m = runCmd(t, m, cmd)

	if !reflect.DeepEqual(rec.args, []string{"old api"}) {
		t.Fatalf("handler args = %v, want [\"old api\"]", rec.args)
	}
}

func TestFullScreenCommandOpensAndClosesPanel(t *testing.T) {
	reg := sampleRegistry(t)
	if err := reg.Register(command.HelpCommand(reg)); err != nil {
		t.Fatalf("register help: %v", err)
	}
	m := newCommandModel(t, reg)
	m = typeString(t, m, "/help")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	m = runCmd(t, m, cmd)

	if !m.panelOpen() {
		t.Fatal("full-screen command did not open a panel")
	}
	if view := m.View(); !strings.Contains(view, "/help") {
		t.Fatalf("panel view missing command list:\n%s", view)
	}

	// Esc dismisses the panel and returns to the chat view.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.panelOpen() {
		t.Fatal("panel still open after esc")
	}
}

func TestUnknownCommandSuggestsNearest(t *testing.T) {
	reg := sampleRegistry(t,
		command.Command{Name: "cost", Run: nopHandler},
	)
	m := newCommandModel(t, reg)
	// "/cosr" has no match, so the palette is closed and Enter takes the parse path.
	m = typeString(t, m, "/cosr")
	if m.palette.open {
		t.Fatalf("palette open for non-matching input: %v", paletteNames(m.palette.matches))
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if cmd != nil {
		t.Fatal("unknown command should not run a handler")
	}
	if len(m.segs) != 1 || m.segs[0].kind != segError {
		t.Fatalf("expected one error segment, got %+v", m.segs)
	}
	if !strings.Contains(m.segs[0].text, "did you mean /cost?") {
		t.Fatalf("error missing suggestion: %q", m.segs[0].text)
	}
}

func TestSlashInputDoesNotStartTurn(t *testing.T) {
	rec := &recorder{}
	reg := sampleRegistry(t,
		command.Command{Name: "cost", Mode: command.Inline, Run: rec.handler(command.Output{Text: "ok"})},
	)
	runner := &fakeRunner{}
	m := newCommandModel(t, reg)
	m.runner = runner
	m = typeString(t, m, "/cost")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.busy {
		t.Fatal("a slash command must not start a runner turn")
	}
}

func TestPaletteSelectionRecordsResolvedInvocation(t *testing.T) {
	rec := &recorder{}
	reg := sampleRegistry(t,
		command.Command{Name: "context", Run: nopHandler},
		command.Command{Name: "cost", Mode: command.Inline, Run: rec.handler(command.Output{Text: "ok"})},
	)
	m := newCommandModel(t, reg)
	m = typeString(t, m, "/co") // palette: [cost, context], cost highlighted

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)

	// History recalls the resolved command, not the typed prefix.
	if len(m.history) != 1 || m.history[0] != "/cost" {
		t.Fatalf("history = %v, want [/cost]", m.history)
	}
}

func TestHistoryRecallReopensPalette(t *testing.T) {
	reg := sampleRegistry(t,
		command.Command{Name: "cost", Mode: command.Inline, Run: nopHandler},
	)
	m := newCommandModel(t, reg)
	m = typeString(t, m, "/cost")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.palette.open {
		t.Fatal("palette should be closed after dispatch")
	}

	// Up-arrow recalls "/cost"; the palette must re-sync open for it.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.textarea.Value() != "/cost" {
		t.Fatalf("recalled value = %q, want /cost", m.textarea.Value())
	}
	if !m.palette.open || len(m.palette.matches) != 1 {
		t.Fatalf("palette not reopened on recall: open=%v matches=%d", m.palette.open, len(m.palette.matches))
	}
}

func TestPaletteHeightClampedToShortTerminal(t *testing.T) {
	reg := command.NewRegistry()
	for _, n := range []string{"cmd1", "cmd2", "cmd3", "cmd4", "cmd5", "cmd6"} {
		if err := reg.Register(command.Command{Name: n, Run: nopHandler}); err != nil {
			t.Fatalf("register %q: %v", n, err)
		}
	}
	m := newModel(&fakeRunner{}, staticMeta(Meta{}), make(chan loop.UIEvent), nil, reg, nil, false, nil, nil, nil)
	// A short window: only a couple of rows beyond the input + status chrome.
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 8})
	m = typeString(t, m, "/cmd") // matches all six

	maxRows := 8 - inputHeight - statusHeight - 1 // one transcript row reserved
	if h := m.paletteHeight(); h > maxRows || h < 1 {
		t.Fatalf("paletteHeight() = %d, want within [1,%d]", h, maxRows)
	}
	// The rendered palette must not exceed the budgeted height.
	rows := strings.Count(m.paletteView(), "\n") + 1
	if rows > m.paletteHeight() {
		t.Fatalf("paletteView rendered %d rows, want <= %d", rows, m.paletteHeight())
	}
	// And the transcript viewport keeps at least one row.
	if m.viewport.Height < 1 {
		t.Fatalf("viewport height = %d, want >= 1", m.viewport.Height)
	}
}

// nopHandler is a do-nothing command handler for palette/registry tests.
func nopHandler(context.Context, []string) (command.Output, error) {
	return command.Output{}, nil
}

func paletteNames(cmds []command.Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}

// TestCustomCommandSubmitsExpandedPrompt verifies the AS-033 path: a command that
// returns Output.Prompt is submitted as a user turn — the expanded text becomes a
// user segment and a turn starts — rather than printed to the transcript.
func TestCustomCommandSubmitsExpandedPrompt(t *testing.T) {
	reg := sampleRegistry(t, command.Command{
		Name: "fixit",
		Mode: command.Inline,
		Run: func(_ context.Context, args []string) (command.Output, error) {
			return command.Output{Prompt: "please fix " + strings.Join(args, " ")}, nil
		},
	})
	m := newCommandModel(t, reg)
	m.textarea.SetValue("/fixit the bug")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	m = runCmd(t, m, cmd) // applies commandDoneMsg, which submits the prompt

	if !m.busy {
		t.Fatal("custom command did not start a turn (busy=false)")
	}
	last := m.segs[len(m.segs)-1]
	if last.kind != segUser || last.text != "please fix the bug" {
		t.Fatalf("last segment = %+v, want user segment %q", last, "please fix the bug")
	}
	// The command itself must not also render an inline command segment.
	for _, s := range m.segs {
		if s.kind == segCommand {
			t.Fatalf("unexpected command segment rendered for a prompt command: %+v", s)
		}
	}
}
