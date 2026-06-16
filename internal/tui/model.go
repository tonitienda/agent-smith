package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonitienda/agent-smith/internal/command"
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
	runner Runner
	// meta is the cached status-line identity; metaFn re-reads it (provider,
	// model, session) so a /model or /clear/-resume switch is reflected without
	// the face owning that state. metaFn may be nil, leaving meta at its zero.
	meta     Meta
	metaFn   MetaFunc
	events   <-chan loop.UIEvent
	newRend  rendererFactory
	renderer markdownRenderer
	commands *command.Registry
	// rehydrate yields the active session's projected live blocks; the face
	// replays them into the transcript on a session swap (/clear, /resume) and at
	// launch, so a resumed session shows its prior turns (AS-064). nil leaves the
	// transcript blank on a swap.
	rehydrate RehydrateFunc

	// meter computes the context/cost snapshot for the status line; nil disables
	// it. meterState caches the most recent snapshot so the status line renders
	// without recomputing on every keystroke — it is refreshed once per event.
	meter      MeterFunc
	meterState Meter

	textarea textarea.Model
	viewport viewport.Model
	spinner  spinner.Model

	segs         []segment
	curAssistant int
	curReasoning int

	history []string
	histIdx int
	draft   string

	// palette is the filterable slash-command list shown while typing a command.
	palette palette
	// panel holds a full-screen command output (e.g. /help) until dismissed.
	panel viewport.Model
	// panelTitle labels the open full-screen panel; empty means no panel.
	panelTitle string
	// leader is true after the panel leader key (ctrl+g) until the next key
	// selects a panel or cancels the chord (D-TUI-4 modifier hotkeys).
	leader bool
	// panelHotkeys maps a leader-chord key to the command name it opens, so common
	// inspect panels have a fast path that never steals bare-letter input (D-TUI-7).
	panelHotkeys map[string]string
	// modal, when non-nil, is the blocking permission overlay (D-TUI-8); it traps
	// focus and is rendered instead of the transcript until dismissed.
	modal *modal
	// picker, when non-nil, is the interactive single-select list a command
	// offered (AS-064 /resume); it traps focus like a panel until a choice or Esc.
	picker *picker
	// selector, when non-nil, is the interactive multi-select surface a command
	// offered (AS-068 /clean in-panel selection); it traps focus like the picker
	// until the user applies a selection, restores an archive block, or presses Esc.
	selector *selector
	// perm is the permission prompt currently shown (AS-024); permQueue holds
	// further prompts awaiting their turn, since parallel tool calls (AS-019) can
	// prompt concurrently but the user decides them one at a time. A non-destructive
	// prompt renders as an inline card, a destructive one as a blocking modal.
	perm      *pendingPerm
	permQueue []*pendingPerm
	// splash controls whether the startup header renders atop the transcript;
	// --no-splash and serious mode clear it (D-TUI-10).
	splash bool
	// expandTools, when set, shows tool results in full rather than a preview; the
	// leader chord Ctrl+G then t toggles it (AS-024 AC1).
	expandTools bool

	busy   bool
	cancel context.CancelFunc

	width  int
	height int
	ready  bool
}

// defaultPanelHotkeys binds the leader chord (ctrl+g, then this key) to the
// inspect panels worth a daily-speed shortcut. Bindings for panels that aren't
// registered yet (/context, /diff land in AS-026/AS-024) are harmless no-ops
// until their command exists, so the chord is stable as panels arrive.
func defaultPanelHotkeys() map[string]string {
	return map[string]string{
		"c": "context",
		"d": "diff",
		"h": "help",
		"$": "cost",
	}
}

// newModel builds the chat model. newRend may be nil to disable markdown
// rendering (the transcript then shows raw text); commands may be nil to run
// without any slash commands; meter may be nil to hide the context meter; splash
// shows the startup header.
func newModel(runner Runner, meta MetaFunc, events <-chan loop.UIEvent, newRend rendererFactory, commands *command.Registry, meter MeterFunc, splash bool, rehydrate RehydrateFunc) model {
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

	m := model{
		runner:       runner,
		metaFn:       meta,
		events:       events,
		newRend:      newRend,
		commands:     commands,
		meter:        meter,
		rehydrate:    rehydrate,
		panelHotkeys: defaultPanelHotkeys(),
		splash:       splash,
		textarea:     ta,
		spinner:      sp,
		curAssistant: -1,
		curReasoning: -1,
		histIdx:      0,
	}
	if meta != nil {
		m.meta = meta()
	}
	// Replay the active session's prior turns at launch, so a `--resume <id>`
	// start shows the conversation rather than a blank screen (AS-064). A fresh
	// session projects to no blocks, leaving the transcript empty.
	if rehydrate != nil {
		m.segs = segmentsFromBlocks(rehydrate())
	}
	m.refreshMeter()
	return m
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

	case permPromptMsg:
		m.enqueuePerm(msg)
		m.refresh()
		return m, nil

	case turnDoneMsg:
		return m.finishTurn(msg), nil

	case commandDoneMsg:
		return m.finishCommand(msg), nil

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

// handleKey routes key presses: the blocking modal first (focus trapped), then
// the full-screen panel, the panel leader chord, control keys, the command
// palette, and finally input vs. scrollback.
func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A blocking modal traps focus until it is decided (D-TUI-8).
	if m.modalOpen() {
		return m.handleModalKey(msg)
	}
	// A pending permission prompt captures decision keys before anything else, so
	// the user resolves it before typing or scrolling away (D-TUI-8).
	if m.permActive() {
		return m.handlePermKey(msg)
	}
	// An interactive picker captures navigation/selection until a choice or Esc.
	if m.pickerOpen() {
		return m.handlePickerKey(msg)
	}
	// An interactive selector captures navigation/selection/restore until the user
	// applies, restores, or presses Esc.
	if m.selectorOpen() {
		return m.handleSelectorKey(msg)
	}
	// A full-screen command panel captures navigation until dismissed.
	if m.panelOpen() {
		return m.handlePanelKey(msg)
	}
	// The leader chord (ctrl+g, then a key) opens a panel; the next key is the
	// panel selector, never bare input.
	if m.leader {
		return m.handleLeaderKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit

	case "ctrl+g":
		// Begin a panel leader chord; the next key picks a panel (D-TUI-4).
		m.leader = true
		return m, nil

	case "esc":
		// Abort an open palette first, then cancel an in-flight turn; either way
		// the session stays usable.
		if m.palette.open {
			m.resetInput()
			m.relayout()
			return m, nil
		}
		if m.busy && m.cancel != nil {
			m.cancel()
		}
		return m, nil

	case "enter":
		if strings.HasPrefix(strings.TrimSpace(m.textarea.Value()), "/") && m.commands != nil {
			return m.dispatchCommand()
		}
		return m.submit()

	case "tab":
		if m.palette.open {
			m.completeFromPalette()
			return m, nil
		}

	case "up", "down":
		if m.palette.open {
			m.palette.move(msg.String() == "up")
			return m, nil
		}
		// History navigation while the draft is a single line; otherwise the key
		// moves the cursor within the multi-line editor.
		if !strings.Contains(m.textarea.Value(), "\n") {
			m.navigateHistory(msg.String() == "up")
			// A recalled entry may be a "/…" command, so refresh the palette.
			m.syncPalette()
			m.relayout()
			return m, nil
		}

	case "pgup", "pgdown", "ctrl+u", "ctrl+d":
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	// Typing may have changed the command being entered; refresh the palette.
	m.syncPalette()
	m.relayout()
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
	m.resetInput()

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
	// Drop any prompt still on screen: the turn is over, so its Asker has already
	// unblocked and a lingering card would block the idle UI.
	m.clearPerms()
	m.finalizeText()

	switch {
	case msg.err != nil && errors.Is(msg.err, context.Canceled):
		m.markPendingToolsInterrupted()
		m.segs = append(m.segs, segment{kind: segNotice, text: "turn cancelled", done: true})
	case msg.err != nil:
		m.markPendingToolsInterrupted()
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

	if m.viewport.Width == 0 {
		m.viewport = viewport.New(width, 1)
		m.panel = viewport.New(width, 1)
	}
	m.viewport.Width = width
	m.panel.Width = width
	m.textarea.SetWidth(width)
	m.relayout()

	if m.newRend != nil {
		m.renderer = m.newRend(width)
	}
	for i := range m.segs {
		m.segs[i].rendered = "" // wrap width changed; drop cached markdown
	}
	m.refresh()
	return m
}

// relayout sizes the transcript viewport for the current chrome. The view joins
// the viewport, optional palette, status line, and input with single newlines,
// so their heights must sum to the terminal height. The full-screen panel, when
// open, takes the whole screen bar one footer row.
func (m *model) relayout() {
	if !m.ready {
		return
	}
	vpHeight := m.height - inputHeight - m.statusRows() - m.paletteHeight() - m.permRows()
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport.Height = vpHeight

	// The inspect panel keeps the pinned status line (when there's room) and a
	// one-row footer keybar; the rest is the panel body.
	panelHeight := m.height - m.statusRows() - m.panelFooterRows()
	if panelHeight < 1 {
		panelHeight = 1
	}
	m.panel.Height = panelHeight
}

// statusRows is the height the status line occupies: one row normally, zero when
// the terminal is too short to also fit the input and at least one body row. The
// status line is the first chrome to drop so a tiny window degrades to a usable
// prompt instead of a glitch (D-TUI-11).
func (m model) statusRows() int {
	if m.height < inputHeight+statusHeight+1 {
		return 0
	}
	return statusHeight
}

// panelFooterRows is the height the inspect-panel footer keybar occupies: one
// row normally, zero when there isn't room for at least one body row above it
// (after any pinned status row), so an extremely short terminal shows the panel
// body alone rather than overflowing (D-TUI-11).
func (m model) panelFooterRows() int {
	if m.height < m.statusRows()+2 {
		return 0
	}
	return 1
}

// refreshMeter recomputes the cached context/cost snapshot from the live log.
// It runs once per event rather than per render, so the status line stays within
// one event of any change (AS-025) without recomputing on every keystroke.
func (m *model) refreshMeter() {
	if m.metaFn != nil {
		m.meta = m.metaFn()
	}
	if m.meter != nil {
		m.meterState = m.meter(m.meta.Model)
	}
}

// refresh re-renders the transcript into the viewport, keeping the view pinned
// to the bottom when it was already there (chat-style auto-scroll).
func (m *model) refresh() {
	m.refreshMeter()
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.renderTranscript())
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// resetInput clears the editor draft and any open palette, returning the input
// to an empty ready state. History is left untouched.
func (m *model) resetInput() {
	m.textarea.Reset()
	m.draft = ""
	m.palette = palette{}
}

func (m model) View() string {
	if !m.ready {
		return "starting…"
	}
	if m.modalOpen() {
		return m.modalView()
	}
	// A destructive permission prompt is a focus-trapped overlay, like the modal
	// (D-TUI-8); a normal one renders inline below, in the work-mode stack.
	if m.permActive() && m.perm.prompt.Destructive {
		return m.permModalView()
	}
	if m.pickerOpen() {
		// The picker uses the same pinned-status + footer chrome as an inspect
		// panel (D-TUI-3/D-TUI-11), so it degrades identically on a short terminal.
		sections := make([]string, 0, 3)
		if m.statusRows() > 0 {
			sections = append(sections, m.statusLine())
		}
		sections = append(sections, m.pickerView())
		if m.panelFooterRows() > 0 {
			sections = append(sections, m.pickerFooter())
		}
		return strings.Join(sections, "\n")
	}
	if m.selectorOpen() {
		// The selector shares the picker's pinned-status + footer chrome so it
		// degrades identically on a short terminal (D-TUI-3/D-TUI-11).
		sections := make([]string, 0, 3)
		if m.statusRows() > 0 {
			sections = append(sections, m.statusLine())
		}
		sections = append(sections, m.selectorView())
		if m.panelFooterRows() > 0 {
			sections = append(sections, m.selectorFooter())
		}
		return strings.Join(sections, "\n")
	}
	if m.panelOpen() {
		// Inspect mode: the status line is pinned above the panel body (D-TUI-3),
		// then a footer keybar. The status line and then the footer drop, in that
		// order, when the terminal is too short to fit everything (D-TUI-11).
		sections := make([]string, 0, 3)
		if m.statusRows() > 0 {
			sections = append(sections, m.statusLine())
		}
		sections = append(sections, m.panel.View())
		if m.panelFooterRows() > 0 {
			sections = append(sections, m.panelFooter())
		}
		return strings.Join(sections, "\n")
	}
	sections := []string{m.viewport.View()}
	// Gate on the reserved height, not just open: a tiny window drops the palette
	// (paletteHeight()==0) so the rendered sections never exceed the terminal.
	if m.palette.open && m.paletteHeight() > 0 {
		sections = append(sections, m.paletteView())
	}
	// An inline permission card sits above the status line; like the palette it is
	// dropped when the window is too short to reserve room for it (D-TUI-11).
	if m.permActive() && !m.perm.prompt.Destructive && m.permRows() > 0 {
		sections = append(sections, m.permCardView())
	}
	if m.statusRows() > 0 {
		sections = append(sections, m.statusLine())
	}
	sections = append(sections, m.textarea.View())
	return strings.Join(sections, "\n")
}

// statusLine renders provider · model · session on the left and, on the right,
// the always-visible context meter followed by the working/ready state.
func (m model) statusLine() string {
	left := strings.Join(nonEmpty(m.meta.Provider, m.meta.Model, m.meta.Session, goalLabel(m.meta.Goal)), " · ")
	state := "ready"
	if m.busy {
		state = m.spinner.View() + "working… (Esc to cancel)"
	}
	right := state
	if gauge := m.meterState.render(); gauge != "" {
		right = gauge + "  " + state
	}
	gap := m.width - lipglossWidth(left) - lipglossWidth(right)
	if gap < 1 {
		gap = 1
	}
	return statusBarStyle.Render(left + strings.Repeat(" ", gap) + right)
}

// goalLabel formats the active session goal (AS-040) for the status line,
// truncated so a long objective cannot crowd out the rest of the bar. Empty
// stays empty so nonEmpty omits the segment entirely.
func goalLabel(goal string) string {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return ""
	}
	const max = 48
	// A string never has more runes than bytes, so only pay for the
	// rune-accurate truncation when the byte length could exceed the cap —
	// statusLine renders often, and the common short goal stays allocation-free.
	if len(goal) > max {
		if r := []rune(goal); len(r) > max {
			goal = string(r[:max-1]) + "…"
		}
	}
	return "goal: " + goal
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
