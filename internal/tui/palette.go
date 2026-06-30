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
	// Re-scan for custom commands only as the palette transitions to open — not on
	// every keystroke while already typing a command — so a file dropped into the
	// commands dir is invocable without a restart (AS-033) without hammering the
	// disk on each key (which matters on slow/remote home dirs).
	if m.rescan != nil && !m.palette.open {
		m.rescan()
	}
	matches := m.commands.Match(v[1:])
	sel := m.palette.sel
	if sel >= len(matches) {
		sel = 0
	}
	m.palette = palette{open: len(matches) > 0, matches: matches, sel: sel}
}

// paletteChrome is the fixed row overhead of the modal surface: rounded border
// top+bottom (2) plus the search row, the rule, and the footer (3).
const paletteChrome = 5

// paletteHeight is the number of rows the palette occupies in the layout (zero
// when closed), so relayout can shrink the transcript to fit it. It is computed
// in O(1) from the same layout decision paletteView draws (paletteLayout), so the
// reserved height and the drawn block can never disagree without re-rendering the
// view on every keystroke.
func (m *model) paletteHeight() int {
	_, _, h := m.paletteLayout()
	return h
}

// paletteLayout decides how the palette will be drawn for the current row budget
// and returns (modal, rows, height): modal is false for the degraded plain list a
// tiny window falls back to; rows is how many match rows fit; height is the total
// rows the surface occupies (chrome included). It is the single source of truth
// for both paletteHeight (height) and paletteView (modal + rows), so the two stay
// in lockstep. Returns zeros when the palette is closed or can't fit even one row.
func (m *model) paletteLayout() (modal bool, rows, height int) {
	if !m.palette.open || len(m.palette.matches) == 0 {
		return false, 0, 0
	}
	budget := m.paletteBudget()
	if budget < 1 {
		return false, 0, 0
	}
	n := len(m.palette.matches)
	// Too short for the modal chrome plus a match: degrade to a plain list whose
	// rows fill the whole budget (D-TUI-11).
	if budget < paletteChrome+1 {
		if n > budget {
			n = budget
		}
		return false, n, n
	}
	limit := budget - paletteChrome
	if limit > paletteMaxRows {
		limit = paletteMaxRows
	}
	if n > limit {
		n = limit
	}
	return true, n, paletteChrome + n
}

// paletteBudget is the total rows the palette may occupy above the input/status
// chrome, keeping at least one transcript row (D-TUI-11). It mirrors relayout's
// viewport budgeting exactly — subtracting every chrome row relayout does (input,
// status, mode bar, permission card) — so a coding mode or a queued permission
// prompt can't leave the palette budgeted for rows that aren't there. On a tiny
// window the status line (and with it the mode bar) drops, so only the chrome that
// actually renders is reserved.
func (m *model) paletteBudget() int {
	return m.height - inputHeight - m.statusRows() - m.modeBarRows() - m.permRows() - 1
}

// paletteRange returns the [start,end) slice of matches to show for a row limit,
// scrolling the window to keep the selection in view.
func (m *model) paletteRange(limit int) (int, int) {
	if limit < 1 {
		limit = 1
	}
	start := 0
	if m.palette.sel >= limit {
		start = m.palette.sel - limit + 1
	}
	end := start + limit
	if end > len(m.palette.matches) {
		end = len(m.palette.matches)
	}
	return start, end
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

// paletteView renders the command palette as a first-class modal surface (§7.6):
// a rounded box wrapping a search-field row with a "N commands" count, a rule, the
// match window, and a footer hint row. When the terminal is too short to fit the
// chrome plus one match, it degrades to a plain bordered-less match list so a
// small window still shows the commands (D-TUI-11).
func (m *model) paletteView() string {
	modal, rows, _ := m.paletteLayout()
	if rows == 0 {
		return ""
	}
	matches := m.palette.matches
	start, end := m.paletteRange(rows)

	brand := lipgloss.NewStyle().Foreground(ColorBrand)

	if !modal {
		// Degraded plain list: a borderless run of match rows.
		lines := make([]string, 0, end-start)
		for i := start; i < end; i++ {
			lines = append(lines, m.paletteRow(matches[i], i == m.palette.sel, 0))
		}
		return strings.Join(lines, "\n")
	}

	// First pass: size the box to its widest line, then clamp to the terminal's
	// inner width (border + padding take 4 cells) so a long summary or a narrow
	// window can't overflow into garbled borders / wrapping (Gemini review).
	query := strings.TrimPrefix(m.textarea.Value(), "/")
	search := "❯ /" + query
	count := fmt.Sprintf("%d commands", len(matches))
	footer := "↑↓ move · ↵ run · tab complete · esc close"
	innerWidth := lipglossWidth(search + " █ " + count)
	if w := lipglossWidth(footer); w > innerWidth {
		innerWidth = w
	}
	for i := start; i < end; i++ {
		if w := lipglossWidth(paletteRowText(matches[i])); w > innerWidth {
			innerWidth = w
		}
	}
	if maxW := m.width - 4; maxW > 0 && innerWidth > maxW {
		innerWidth = maxW
	}

	// Search field: caret + typed text + block cursor on the left, count right.
	// The caret and cursor are brand-coloured per §7.6; only the typed text reads
	// in command-green.
	gap := innerWidth - lipglossWidth(search+"█") - lipglossWidth(count)
	if gap < 1 {
		gap = 1
	}
	searchRow := brand.Render("❯") + StyleSlashCommand.Render(" /"+query) +
		brand.Render("█") + strings.Repeat(" ", gap) + StyleDim.Render(count)

	// Every line is fit to innerWidth (truncate if long, pad if short) so the box
	// border draws as a clean rectangle without wrapping (lipgloss .Width
	// double-counts padding).
	lines := []string{searchRow, paletteRuleStyle.Render(strings.Repeat("─", innerWidth))}
	for i := start; i < end; i++ {
		lines = append(lines, m.paletteRow(matches[i], i == m.palette.sel, innerWidth))
	}
	lines = append(lines, StyleDim.Render(footer))
	for i, l := range lines {
		lines[i] = fitWidth(l, innerWidth)
	}

	border := ColorBorder
	if query != "" {
		border = ColorBorderSelect
	}
	return paletteBoxStyle.BorderForeground(border).Render(strings.Join(lines, "\n"))
}

// fitWidth truncates s to width cells (ANSI-aware) when too long, or right-pads it
// with plain spaces when too short, so every modal line is exactly width wide.
func fitWidth(s string, width int) string {
	if lipglossWidth(s) > width {
		return lipgloss.NewStyle().MaxWidth(width).Render(s)
	}
	if pad := width - lipglossWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// paletteRowText is the unstyled "/name args  summary" text of a match, used for
// width measurement so the box sizes to its widest row.
func paletteRowText(c command.Command) string {
	inv := "/" + c.Name
	if c.Args != "" {
		inv += " " + c.Args
	}
	return fmt.Sprintf("  %-14s  %s", inv, c.Summary)
}

// paletteRow renders one match. The selected row fills its full width with the
// mode-bar green and a brand caret; others read command-green (amber for the
// /serious toggle) with a muted description.
func (m *model) paletteRow(c command.Command, selected bool, width int) string {
	inv := "/" + c.Name
	if c.Args != "" {
		inv += " " + c.Args
	}
	if selected {
		sel := lipgloss.NewStyle().Background(BgModeBar)
		row := sel.Foreground(ColorBrand).Render("❯ ") +
			sel.Foreground(ColorFgDefault).Render(fmt.Sprintf("%-14s  %s", inv, c.Summary))
		if pad := width - lipglossWidth(row); pad > 0 {
			row += sel.Render(strings.Repeat(" ", pad))
		}
		return row
	}
	nameStyle := StyleSlashCommand
	if c.Name == "serious" {
		nameStyle = StyleRunning
	}
	// Left unpadded: the modal caller fits each line to innerWidth (fitWidth); the
	// plain fallback (width 0) wants a ragged, borderless list.
	return "  " + nameStyle.Render(fmt.Sprintf("%-14s", inv)) + "  " + StyleMuted.Render(c.Summary)
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
	// Parse the command's declared flags off the lexed tokens before arity: both
	// faces strip flags through this one path, so a slash command and its
	// subcommand never disagree, and no handler hand-matches --flag on args[0]
	// (AS-104). Slash lexing (command.Parse) already ran, so this only permutes
	// and parses the tokens.
	ctx, args, err := cmd.ParseFlags(context.Background(), args)
	if err != nil {
		m.appendSegment(segment{kind: segError, text: fmt.Sprintf("/%s: %v", cmd.Name, err), done: true})
		return m, nil
	}
	if err := cmd.CheckArity(args); err != nil {
		m.appendSegment(segment{kind: segError, text: fmt.Sprintf("/%s: %v", cmd.Name, err), done: true})
		return m, nil
	}
	return m, runCommand(ctx, cmd, args)
}

// runCommand executes a command's handler off the Update loop; the result
// returns as a commandDoneMsg. ctx carries any flags ParseFlags resolved.
func runCommand(ctx context.Context, c command.Command, args []string) tea.Cmd {
	return func() tea.Msg {
		out, err := c.Run(ctx, args)
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
	// Ctrl+G then m opens the Coding Mode panel (AS-073): the richer goal/tracker/
	// phases-visited view. It is built from the cached Meta render, not a command,
	// so it is handled here like the t toggle. With no mode active there is nothing
	// to show, so the chord is a no-op.
	if msg.String() == "m" {
		if m.meta.Mode != "" && m.meta.ModePanel != "" {
			m.openPanel("mode", m.meta.ModePanel)
		}
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
	return m, runCommand(context.Background(), cmd, nil)
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

// Palette styles (§7.6): a rounded modal box and the search-field rule. Per-row
// colours are applied in paletteRow; the box border colour is set per render
// (select-green while typing a query, idle border when empty).
var (
	paletteBoxStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	paletteRuleStyle = lipgloss.NewStyle().Foreground(ColorBorderSelect)

	// Shared selected/idle row styles, also reused by the picker and selector
	// surfaces (picker.go, selector.go) for their highlighted rows.
	paletteItemStyle = StyleNeutral
	paletteSelStyle  = lipgloss.NewStyle().Bold(true).Foreground(ColorCommand).Background(BgModeBar)
)
