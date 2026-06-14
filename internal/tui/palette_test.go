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
	m := newModel(&fakeRunner{}, Meta{Provider: "anthropic", Model: "m", Session: "s"},
		make(chan loop.UIEvent), nil, reg)
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
