package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tonitienda/agent-smith/internal/command"
)

// paletteMaxRows caps how many matches the palette shows at once; the selection
// is kept in view as it moves past the cap.
const paletteMaxRows = 8

// palette is the filterable command list shown while the user types a slash
// command name. It is derived from the input on every keystroke (syncPalette),
// so it holds only the current matches and the highlighted index.
type palette struct {
	open    bool
	matches []command.Command
	sel     int
}

// move shifts the selection up or down, clamping at the ends.
func (p *palette) move(up bool) {
	if len(p.matches) == 0 {
		return
	}
	if up {
		if p.sel > 0 {
			p.sel--
		}
		return
	}
	if p.sel < len(p.matches)-1 {
		p.sel++
	}
}

// syncPalette recomputes the palette from the current input. It opens while the
// user is typing a command name — input starts with "/" and no space has been
// typed yet — and lists the fuzzy matches. Once a space (an argument) appears,
// or the input is no longer a command, the palette closes.
func (m *model) syncPalette() {
	v := m.textarea.Value()
	if m.commands == nil || !strings.HasPrefix(v, "/") || strings.ContainsAny(v, " \t\n") {
		m.palette = palette{}
		return
	}
	// Re-scan for custom commands as the palette opens, so a file dropped into the
	// commands dir is invocable without a restart (AS-033). It mutates the shared
	// registry; scanning a local dir per keystroke is cheap.
	if m.rescan != nil {
		m.rescan()
	}
	matches := m.commands.Match(v[1:])
	sel := m.palette.sel
	if sel >= len(matches) {
		sel = 0
	}
	m.palette = palette{open: len(matches) > 0, matches: matches, sel: sel}
}

// paletteHeight is the number of rows the palette occupies in the layout (zero
// when closed), so relayout can shrink the transcript to fit it. It is the cap
// (paletteMaxRows) and the match count, further clamped to the terminal so a
// short window can't push the palette past the input/status chrome. At least one
// transcript row is always reserved.
func (m *model) paletteHeight() int {
	if !m.palette.open {
		return 0
	}
	n := len(m.palette.matches)
	if n > paletteMaxRows {
		n = paletteMaxRows
	}
	// Reserve only the chrome that actually renders: on a tiny window the status
	// line is dropped (statusRows()==0), so reserving it unconditionally would let
	// viewport+palette+textarea exceed the terminal (D-TUI-11). The trailing -1
	// keeps at least one transcript row; when even that can't fit, the palette is
	// dropped entirely (0) rather than forced to overflow the input.
	maxRows := m.height - inputHeight - m.statusRows() - 1
	if maxRows < 1 {
		return 0
	}
	if n > maxRows {
		n = maxRows
	}
	return n
}

// completeFromPalette fills the input with the highlighted command name and a
// trailing space, ready for arguments. The space closes the palette.
func (m *model) completeFromPalette() {
	if !m.palette.open || len(m.palette.matches) == 0 {
		return
	}
	name := m.palette.matches[m.palette.sel].Name
	m.textarea.SetValue("/" + name + " ")
	m.textarea.CursorEnd()
	m.syncPalette()
	m.relayout()
}

// paletteView renders the visible window of matches with the selection
// highlighted, each line "  /name args   summary".
func (m *model) paletteView() string {
	matches := m.palette.matches
	if len(matches) == 0 {
		return ""
	}
	// Use the same (possibly terminal-clamped) row budget as the layout, so the
	// rendered window never exceeds the height relayout reserved for it.
	limit := m.paletteHeight()
	if limit < 1 {
		limit = 1
	}
	start := 0
	if m.palette.sel >= limit {
		start = m.palette.sel - limit + 1
	}
	end := start + limit
	if end > len(matches) {
		end = len(matches)
	}

	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		c := matches[i]
		inv := "/" + c.Name
		if c.Args != "" {
			inv += " " + c.Args
		}
		line := fmt.Sprintf("%-20s %s", inv, c.Summary)
		line = strings.TrimRight(line, " ")
		if i == m.palette.sel {
			lines = append(lines, paletteSelStyle.Render("▸ "+line))
		} else {
			lines = append(lines, paletteItemStyle.Render("  "+line))
		}
	}
	return strings.Join(lines, "\n")
}

// commandDoneMsg reports that a dispatched command's handler returned.
type commandDoneMsg struct {
	cmd command.Command
	out command.Output
	err error
}

// dispatchCommand parses the current input as a command line and runs it. When
// the palette is still open (the user pressed Enter while choosing a name) the
// highlighted command is used; otherwise the typed name is looked up. An unknown
// command yields an error line with a nearest-match suggestion.
func (m model) dispatchCommand() (tea.Model, tea.Cmd) {
	// A command must not run while a turn is in flight: /clear and /resume swap
	// the session (and clear the transcript), which would corrupt the view with
	// the still-streaming turn's events and race the engine swap. Mirror submit's
	// busy guard and ask the user to finish or cancel the turn first.
	if m.busy {
		m.resetInput()
		m.relayout()
		m.appendSegment(segment{kind: segNotice, text: "finish or cancel the current turn (Esc) before running a command", done: true})
		return m, nil
	}

	line := strings.TrimSpace(m.textarea.Value())

	var (
		name string
		args []string
	)
	// histLine is what up-arrow will recall. For a palette selection it is the
	// resolved invocation (e.g. "/cost"), not the typed prefix ("/co"), so recall
	// re-runs the same command rather than a misleading partial.
	histLine := line
	if m.palette.open && len(m.palette.matches) > 0 && !strings.ContainsAny(line, " \t") {
		name = m.palette.matches[m.palette.sel].Name
		histLine = "/" + name
	} else {
		n, a, err := command.Parse(line)
		if err != nil {
			m.resetInput()
			m.appendSegment(segment{kind: segError, text: "invalid command: " + err.Error(), done: true})
			return m, nil
		}
		name, args = n, a
	}

	m.history = append(m.history, histLine)
	m.histIdx = len(m.history)
	m.resetInput()
	m.relayout()

	cmd, ok := m.commands.Lookup(name)
	if !ok {
		msg := "unknown command: /" + name
		if suggestion, ok := m.commands.Suggest(name); ok {
			msg += "  (did you mean /" + suggestion + "?)"
		}
		m.appendSegment(segment{kind: segError, text: msg, done: true})
		return m, nil
	}
	return m, runCommand(cmd, args)
}

// runCommand executes a command's handler off the Update loop; the result
// returns as a commandDoneMsg.
func runCommand(c command.Command, args []string) tea.Cmd {
	return func() tea.Msg {
		out, err := c.Run(context.Background(), args)
		return commandDoneMsg{cmd: c, out: out, err: err}
	}
}

// finishCommand renders a completed command: an error line on failure, a
// full-screen panel for FullScreen commands, or an inline transcript segment.
func (m model) finishCommand(msg commandDoneMsg) model {
	if msg.err != nil {
		m.appendSegment(segment{kind: segError, text: "/" + msg.cmd.Name + ": " + msg.err.Error(), done: true})
		return m
	}
	// A custom command (AS-033) carries its expansion in Prompt, not Text: the
	// caller submits it as a user turn, which renders the prompt as the user
	// segment, so there is nothing to render here.
	if msg.out.Prompt != "" {
		return m
	}
	// An interactive picker takes over the screen until the user chooses an item
	// (which re-dispatches this command with the choice) or cancels (AS-064). A
	// face shows the picker instead of the command's text listing.
	if msg.out.Picker != nil && len(msg.out.Picker.Items) > 0 {
		m.openPicker(msg.cmd, *msg.out.Picker)
		return m
	}
	// An interactive multi-select surface (AS-068 /clean): the user selects
	// segments and applies, or restores an excluded block, without typing handles.
	// A face shows it instead of the command's text; a non-interactive face would
	// have ignored Selector and rendered Text.
	if msg.out.Selector != nil && (len(msg.out.Selector.Items) > 0 || len(msg.out.Selector.Archive) > 0) {
		m.openSelector(msg.cmd.Name, *msg.out.Selector)
		return m
	}
	// A session-resetting command (/clear, /resume) rebuilds the transcript from
	// the now-active session's projected blocks, so the view reflects the restored
	// conversation (a resume replays prior turns; a fresh /clear replays nothing,
	// AS-064). Without a rehydrate seam it simply clears.
	if msg.out.ResetView {
		if m.rehydrate != nil {
			m.segs = segmentsFromBlocks(m.rehydrate())
		} else {
			m.segs = nil
		}
		m.curAssistant, m.curReasoning = -1, -1
	}
	switch msg.cmd.Mode {
	case command.FullScreen:
		m.openPanel(msg.cmd.Name, msg.out.Text)
	default:
		m.appendSegment(segment{
			kind:     segCommand,
			toolName: msg.cmd.Name,
			text:     msg.out.Text,
			done:     true,
		})
	}
	return m
}

// appendSegment adds a finished segment and keeps the viewport pinned to bottom.
func (m *model) appendSegment(s segment) {
	m.segs = append(m.segs, s)
	m.curAssistant, m.curReasoning = -1, -1
	m.refresh()
}

// handleLeaderKey resolves the key pressed after the panel leader (ctrl+g): a
// bound key opens its panel, anything else cancels the chord without typing the
// key (the leader captured it, so bare-letter input stays unaffected, D-TUI-7).
func (m model) handleLeaderKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.leader = false
	if msg.String() == "ctrl+c" {
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	}
	// Ctrl+G then t toggles full tool output in the transcript (AS-024 AC1). It is
	// a view toggle, not a panel, so it is handled before the panel-name lookup.
	if msg.String() == "t" {
		m.expandTools = !m.expandTools
		m.refresh()
		return m, nil
	}
	if name, ok := m.panelHotkeys[msg.String()]; ok {
		return m.openPanelByName(name)
	}
	return m, nil
}

// openPanelByName dispatches the named full-screen command — the hotkey path
// into the same panel host the palette uses. A missing or non-full-screen
// command is a no-op (a binding for a panel that doesn't exist yet), and a turn
// in flight is declined like a palette dispatch so the view can't be swapped
// from under a streaming turn.
func (m model) openPanelByName(name string) (tea.Model, tea.Cmd) {
	if m.commands == nil {
		return m, nil
	}
	cmd, ok := m.commands.Lookup(name)
	if !ok || cmd.Mode != command.FullScreen {
		return m, nil
	}
	if m.busy {
		m.appendSegment(segment{kind: segNotice, text: "finish or cancel the current turn (Esc) before opening a panel", done: true})
		return m, nil
	}
	return m, runCommand(cmd, nil)
}

// panelOpen reports whether a full-screen command panel is showing.
func (m model) panelOpen() bool { return m.panelTitle != "" }

// openPanel shows text full-screen under the given title.
func (m *model) openPanel(title, text string) {
	m.panelTitle = title
	m.panel.SetContent(text)
	m.panel.GotoTop()
}

// closePanel dismisses the full-screen panel.
func (m *model) closePanel() { m.panelTitle = "" }

// handlePanelKey drives the full-screen panel: esc/q closes it, everything else
// scrolls.
func (m model) handlePanelKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case "esc", "q":
		m.closePanel()
		return m, nil
	}
	var cmd tea.Cmd
	m.panel, cmd = m.panel.Update(msg)
	return m, cmd
}

// panelFooter is the one-line hint shown beneath an open panel.
func (m model) panelFooter() string {
	return statusBarStyle.Render(fmt.Sprintf("/%s — esc or q to close · ↑/↓ to scroll", m.panelTitle))
}

// Palette styles. The selected row is reverse-video so it reads at a glance.
var (
	paletteItemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	paletteSelStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("14"))
)
