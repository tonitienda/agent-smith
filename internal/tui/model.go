package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonitienda/agent-smith/internal/loop"
)

// inputHeight is the fixed height of the multi-line input editor; statusHeight is
// the one-row status line. The transcript viewport takes whatever remains.
const (
	inputHeight  = 3
	statusHeight = 1
)

// markdownRenderer renders final assistant text as a terminal-styled string.
// It is an interface so the model can fall back to raw text when no renderer is
// available (e.g. a too-narrow viewport, or unit tests).
type markdownRenderer interface {
	Render(string) (string, error)
}

// rendererFactory builds a markdownRenderer sized for width columns, or nil when
// markdown rendering is unavailable. It is injected so tests construct a model
// without touching a real terminal.
type rendererFactory func(width int) markdownRenderer

// uiEventMsg carries one loop.UIEvent into the Update loop.
type uiEventMsg loop.UIEvent

// turnDoneMsg reports that the in-flight turn's Run returned.
type turnDoneMsg struct {
	res loop.Result
	err error
}

// model is the Bubble Tea state for the chat face. It holds the rendered
// transcript, the input/scrollback/spinner components, input history, and the
// cancel func for the in-flight turn.
type model struct {
	runner   Runner
	meta     Meta
	events   <-chan loop.UIEvent
	newRend  rendererFactory
	renderer markdownRenderer

	textarea textarea.Model
	viewport viewport.Model
	spinner  spinner.Model

	segs         []segment
	curAssistant int
	curReasoning int

	history []string
	histIdx int
	draft   string

	busy   bool
	cancel context.CancelFunc

	width  int
	height int
	ready  bool
}

// newModel builds the chat model. newRend may be nil to disable markdown
// rendering (the transcript then shows raw text).
func newModel(runner Runner, meta Meta, events <-chan loop.UIEvent, newRend rendererFactory) model {
	ta := textarea.New()
	ta.Placeholder = "Send a message (Enter to send, Ctrl+J for newline)…"
	ta.Prompt = "┃ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(inputHeight)
	// Enter is reserved for submit (handled in Update); newlines go on Ctrl+J.
	ta.KeyMap.InsertNewline.SetKeys("ctrl+j")
	ta.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return model{
		runner:       runner,
		meta:         meta,
		events:       events,
		newRend:      newRend,
		textarea:     ta,
		spinner:      sp,
		curAssistant: -1,
		curReasoning: -1,
		histIdx:      0,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, waitForEvent(m.events))
}

// waitForEvent blocks on the next loop event and re-arms itself, so loop
// progress streams into the Update loop for the life of the program.
func waitForEvent(ch <-chan loop.UIEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			// Channel closed: stop draining rather than spin on zero values.
			return nil
		}
		return uiEventMsg(ev)
	}
}

// runTurn drives one user turn on the runner; its result becomes a turnDoneMsg.
func runTurn(ctx context.Context, r Runner, text string) tea.Cmd {
	return func() tea.Msg {
		res, err := r.Run(ctx, text)
		return turnDoneMsg{res: res, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.resize(msg.Width, msg.Height), nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case uiEventMsg:
		m.apply(loop.UIEvent(msg))
		m.refresh()
		// Re-arm the drain so the next event streams in too.
		return m, waitForEvent(m.events)

	case turnDoneMsg:
		return m.finishTurn(msg), nil

	case spinner.TickMsg:
		if !m.busy {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	// Mouse and other messages drive scrollback.
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// handleKey routes key presses: control keys first, then input vs. scrollback.
func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit

	case "esc":
		// Cancel the in-flight turn; the session stays usable.
		if m.busy && m.cancel != nil {
			m.cancel()
		}
		return m, nil

	case "enter":
		return m.submit()

	case "up", "down":
		// History navigation while the draft is a single line; otherwise the key
		// moves the cursor within the multi-line editor.
		if !strings.Contains(m.textarea.Value(), "\n") {
			m.navigateHistory(msg.String() == "up")
			return m, nil
		}

	case "pgup", "pgdown", "ctrl+u", "ctrl+d":
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

// submit sends the current input as a user turn, unless empty or a turn is
// already running.
func (m model) submit() (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" {
		return m, nil
	}

	m.history = append(m.history, text)
	m.histIdx = len(m.history)
	m.draft = ""
	m.textarea.Reset()

	m.segs = append(m.segs, segment{kind: segUser, text: text, done: true})
	m.curAssistant, m.curReasoning = -1, -1

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.busy = true
	m.refresh()

	return m, tea.Batch(m.spinner.Tick, runTurn(ctx, m.runner, text))
}

// finishTurn folds a completed (or cancelled) turn back into an idle session.
func (m model) finishTurn(msg turnDoneMsg) model {
	m.busy = false
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.finalizeText()

	switch {
	case msg.err != nil && errors.Is(msg.err, context.Canceled):
		m.segs = append(m.segs, segment{kind: segNotice, text: "turn cancelled", done: true})
	case msg.err != nil:
		m.segs = append(m.segs, segment{kind: segError, text: msg.err.Error(), done: true})
	}
	m.refresh()
	return m
}

// navigateHistory walks the submitted-message history, preserving the live draft
// at the bottom of the stack.
func (m *model) navigateHistory(up bool) {
	if len(m.history) == 0 {
		return
	}
	if up {
		if m.histIdx == len(m.history) {
			m.draft = m.textarea.Value()
		}
		if m.histIdx > 0 {
			m.histIdx--
		}
	} else {
		if m.histIdx < len(m.history) {
			m.histIdx++
		}
	}
	if m.histIdx >= len(m.history) {
		m.textarea.SetValue(m.draft)
	} else {
		m.textarea.SetValue(m.history[m.histIdx])
	}
	m.textarea.CursorEnd()
}

// resize recomputes the layout for a new terminal size and re-renders the
// transcript (markdown wrap width changes, so caches are invalidated).
func (m model) resize(width, height int) model {
	m.width, m.height = width, height
	m.ready = true

	// View joins the viewport, status line, and input with single newlines and no
	// trailing padding, so the three heights sum to the full terminal height.
	vpHeight := height - inputHeight - statusHeight
	if vpHeight < 1 {
		vpHeight = 1
	}
	if m.viewport.Width == 0 {
		m.viewport = viewport.New(width, vpHeight)
	} else {
		m.viewport.Width = width
		m.viewport.Height = vpHeight
	}
	m.textarea.SetWidth(width)

	if m.newRend != nil {
		m.renderer = m.newRend(width)
	}
	for i := range m.segs {
		m.segs[i].rendered = "" // wrap width changed; drop cached markdown
	}
	m.refresh()
	return m
}

// refresh re-renders the transcript into the viewport, keeping the view pinned
// to the bottom when it was already there (chat-style auto-scroll).
func (m *model) refresh() {
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.renderTranscript())
	if atBottom {
		m.viewport.GotoBottom()
	}
}

func (m model) View() string {
	if !m.ready {
		return "starting…"
	}
	return strings.Join([]string{
		m.viewport.View(),
		m.statusLine(),
		m.textarea.View(),
	}, "\n")
}

// statusLine renders provider · model · session and the working/ready state.
func (m model) statusLine() string {
	left := strings.Join(nonEmpty(m.meta.Provider, m.meta.Model, m.meta.Session), " · ")
	state := "ready"
	if m.busy {
		state = m.spinner.View() + "working… (Esc to cancel)"
	}
	gap := m.width - lipglossWidth(left) - lipglossWidth(state)
	if gap < 1 {
		gap = 1
	}
	return statusBarStyle.Render(left + strings.Repeat(" ", gap) + state)
}

// nonEmpty returns the non-empty values among its arguments, preserving order.
func nonEmpty(vals ...string) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}
