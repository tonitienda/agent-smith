package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// permPromptMsg delivers a permission request from the Asker (on the turn
// goroutine, via App.Ask) into the Update loop. reply is buffered so the model
// can answer without blocking even if Ask has already given up (a cancelled turn).
type permPromptMsg struct {
	prompt PermissionPrompt
	reply  chan<- PermissionDecision
}

// pendingPerm is one queued approval the model is showing or about to show.
type pendingPerm struct {
	prompt PermissionPrompt
	reply  chan<- PermissionDecision
	sel    int // highlighted choice
}

// permChoices are the buttons for every prompt, left to right. The leftmost is
// the safe default Esc selects (permDenyIdx).
var permChoices = []string{"Deny", "Allow once", "Always allow"}

const (
	permDenyIdx       = 0
	permAllowIdx      = 1
	permAlwaysIdx     = 2
	permCardMaxHeight = 12 // cap the inline card so a long diff can't crowd out the transcript
)

// decisionForChoice maps a chosen button to the decision returned to the policy.
func decisionForChoice(choice int) PermissionDecision {
	switch choice {
	case permAllowIdx:
		return PermissionDecision{Allow: true}
	case permAlwaysIdx:
		return PermissionDecision{Allow: true, Remember: true}
	default:
		return PermissionDecision{Allow: false}
	}
}

// enqueuePerm records an incoming prompt. Prompts arrive only mid-turn; one that
// lands after the turn has already ended (a late delivery whose Asker has given
// up) is auto-denied so a stale card can't block the idle UI. Otherwise it joins
// the queue and, if nothing is showing, becomes the active prompt — parallel tool
// calls (AS-019) can prompt concurrently, so they are shown one at a time.
func (m *model) enqueuePerm(msg permPromptMsg) {
	if !m.busy {
		if msg.reply != nil {
			msg.reply <- PermissionDecision{Allow: false}
		}
		return
	}
	p := &pendingPerm{prompt: msg.prompt, reply: msg.reply}
	if m.perm == nil {
		m.perm = p
	} else {
		m.permQueue = append(m.permQueue, p)
	}
	m.relayout()
}

// permActive reports whether a permission prompt is showing.
func (m model) permActive() bool { return m.perm != nil }

// handlePermKey routes keys while a prompt is showing. Arrows/Tab move the
// choice, Enter confirms, Esc denies (the safe default). A destructive prompt
// traps focus (every other key swallowed, D-TUI-8); a normal inline card lets the
// scroll keys page the transcript so the user can read context before deciding.
func (m model) handlePermKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := *m.perm
	switch msg.String() {
	case "ctrl+c":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case "left", "up", "shift+tab":
		if p.sel > 0 {
			p.sel--
		}
		m.perm = &p
		return m, nil
	case "right", "down", "tab":
		if p.sel < len(permChoices)-1 {
			p.sel++
		}
		m.perm = &p
		return m, nil
	case "enter":
		return m.resolvePerm(p.sel)
	case "esc":
		return m.resolvePerm(permDenyIdx)
	}
	if !p.prompt.Destructive {
		switch msg.String() {
		case "pgup", "pgdown", "ctrl+u", "ctrl+d":
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	}
	// Focus stays on the decision: any other key is swallowed, never leaking to
	// the input behind the prompt (D-TUI-7/D-TUI-8).
	return m, nil
}

// resolvePerm answers the active prompt with the chosen decision and advances to
// the next queued one, if any.
func (m model) resolvePerm(choice int) (tea.Model, tea.Cmd) {
	if m.perm != nil && m.perm.reply != nil {
		m.perm.reply <- decisionForChoice(choice)
	}
	m.perm = nil
	if len(m.permQueue) > 0 {
		m.perm = m.permQueue[0]
		m.permQueue = m.permQueue[1:]
	}
	m.relayout()
	m.refresh()
	return m, nil
}

// clearPerms denies and drops every pending prompt. It runs when a turn ends
// (normally or by cancel): the Askers have already unblocked on the turn context,
// so this only clears the UI, but replying keeps the contract that every prompt is
// answered exactly once.
func (m *model) clearPerms() {
	if m.perm != nil {
		if m.perm.reply != nil {
			m.perm.reply <- PermissionDecision{Allow: false}
		}
		m.perm = nil
	}
	for _, p := range m.permQueue {
		if p.reply != nil {
			p.reply <- PermissionDecision{Allow: false}
		}
	}
	m.permQueue = nil
}

// permRows is the height the inline permission card reserves in the layout (zero
// when no card shows or when the prompt is a blocking modal that takes the whole
// screen). It mirrors paletteHeight: the card height, clamped so at least one
// transcript row survives, dropped entirely when even that can't fit (D-TUI-11).
func (m *model) permRows() int {
	if m.perm == nil || m.perm.prompt.Destructive {
		return 0
	}
	n := lipgloss.Height(m.permCardView())
	maxRows := m.height - inputHeight - m.statusRows() - 1
	if maxRows < 1 {
		return 0
	}
	if n > maxRows {
		n = maxRows
	}
	return n
}

// permCardView renders the inline approval card shown above the status line for a
// normal action. The verbatim subject and any diff are shown so the user approves
// the exact call (UX.md §11); the choices read left to right with the current one
// highlighted.
func (m model) permCardView() string {
	p := m.perm
	width := m.width - 2
	if width < 1 {
		width = 1
	}
	var b strings.Builder
	b.WriteString(permTitleStyle.Render("approve " + permLabel(p.prompt)))
	if p.prompt.Subject != "" {
		b.WriteString("\n")
		b.WriteString(permSubjectStyle.Width(width).Render(p.prompt.Subject))
	}
	if detail := renderPermDetail(p.prompt.Detail, width); detail != "" {
		b.WriteString("\n")
		b.WriteString(detail)
	}
	b.WriteString("\n")
	b.WriteString(permChoiceRow(p.sel))
	b.WriteString("  ")
	b.WriteString(dimStyle.Render("←/→ choose · Enter · Esc denies"))

	body := capHeight(b.String(), permCardMaxHeight)
	return lipgloss.NewStyle().MaxWidth(m.width).Render(permCardStyle.Width(width).Render(body))
}

// permModalView renders a destructive prompt as a centered, focus-trapped overlay
// reusing the severe modal styling (D-TUI-8). It mirrors modalView so the two
// surfaces look consistent.
func (m model) permModalView() string {
	p := m.perm
	var b strings.Builder
	b.WriteString(modalTitleStyle.Render("⚠  approve " + permLabel(p.prompt)))
	if p.prompt.Subject != "" {
		b.WriteString("\n\n")
		detailWidth := m.width - 8
		if detailWidth < 1 {
			detailWidth = 1
		}
		b.WriteString(modalDetailStyle.Width(detailWidth).Render(p.prompt.Subject))
	}
	if detail := renderPermDetail(p.prompt.Detail, m.width-8); detail != "" {
		b.WriteString("\n\n")
		b.WriteString(detail)
	}
	b.WriteString("\n\n")
	b.WriteString(permChoiceRow(p.sel))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("←/→ choose · Enter confirm · Esc denies"))

	box := lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(modalBoxStyle.Render(capHeight(b.String(), m.height-4)))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// permLabel names the action being approved: the tool, e.g. "shell command" or
// "edit".
func permLabel(p PermissionPrompt) string {
	switch p.Tool {
	case "shell":
		return "shell command"
	case "":
		return "action"
	default:
		return p.Tool
	}
}

// permChoiceRow renders the choice buttons with sel highlighted.
func permChoiceRow(sel int) string {
	buttons := make([]string, len(permChoices))
	for i, c := range permChoices {
		if i == sel {
			buttons[i] = modalChoiceSelStyle.Render(c)
		} else {
			buttons[i] = modalChoiceStyle.Render(c)
		}
	}
	return strings.Join(buttons, "  ")
}

// renderPermDetail colors a diff body line by line — "+" additions green, "-"
// removals red — so an edit's change reads at a glance (AS-024 AC2). Width bounds
// each line so a long one wraps instead of glitching a narrow terminal. Empty
// detail renders nothing.
func renderPermDetail(detail string, width int) string {
	detail = strings.TrimRight(detail, "\n")
	if detail == "" {
		return ""
	}
	if width < 1 {
		width = 1
	}
	lines := strings.Split(detail, "\n")
	out := make([]string, len(lines))
	for i, ln := range lines {
		style := dimStyle
		switch {
		case strings.HasPrefix(ln, "+"):
			style = diffAddStyle
		case strings.HasPrefix(ln, "-"):
			style = diffDelStyle
		}
		out[i] = style.Width(width).Render(ln)
	}
	return strings.Join(out, "\n")
}

// capHeight truncates s to at most max rendered rows, appending an ellipsis line
// when content was dropped, so an oversized diff degrades to a preview rather than
// overflowing the layout.
func capHeight(s string, max int) string {
	if max < 1 {
		max = 1
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= max {
		return s
	}
	kept := lines[:max-1]
	kept = append(kept, dimStyle.Render("…"))
	return strings.Join(kept, "\n")
}

// Permission card styles. The inline card uses a soft border so it reads as a
// pause rather than an alarm; the destructive modal reuses the severe red modal
// styling (modal.go).
var (
	permCardStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("11")).Padding(0, 1)
	permTitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	permSubjectStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	diffAddStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	diffDelStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)
