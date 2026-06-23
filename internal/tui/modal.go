package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// modal is the blocking overlay (D-TUI-8) used for destructive or broad-scope
// permission prompts. Focus is trapped until the user decides, the exact
// command/path/scope is shown verbatim (UX.md §11), and the styling is severe so
// it can't be misclicked past. It is a reusable sub-model: AS-024's
// destructive-prompt path builds one and hands it to openModal, while the panel
// host owns focus trapping and rendering, so each caller is just a view model
// (D-TUI-2) rather than a bespoke screen.
type modal struct {
	title string
	// detail is shown verbatim — the literal command, path, or scope being
	// approved. Never a paraphrase (UX.md §11). May be empty.
	detail string
	// choices are the buttons, left to right (e.g. {"Deny", "Allow"}).
	choices []string
	sel     int
	// denyIdx is the choice Esc selects — the safe default for a destructive
	// prompt. It is index 0 (the leftmost, "deny") unless a caller overrides it.
	denyIdx int
	// decide is invoked with the chosen index when the user confirms (Enter) and
	// with denyIdx when they cancel (Esc); it returns any follow-up command. A nil
	// decide makes the modal informational (dismiss-only).
	decide func(choice int) tea.Cmd
}

// openModal shows md as a blocking overlay. Callers (AS-024) build the modal and
// hand it here; the host traps focus until decideModal fires.
func (m *model) openModal(md modal) { m.modal = &md }

// modalOpen reports whether a blocking modal overlay is showing.
func (m model) modalOpen() bool { return m.modal != nil }

// handleModalKey routes every key to the open modal: arrows/tab move the
// selection, Enter confirms, Esc cancels to denyIdx. Ctrl+C still quits the
// program; all other keys are swallowed, so focus is trapped (D-TUI-8).
func (m model) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	md := *m.modal // copy; selection moves produce a fresh modal, no shared mutation
	switch msg.String() {
	case "ctrl+c":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case "left", "up", "shift+tab":
		if md.sel > 0 {
			md.sel--
		}
		m.modal = &md
		return m, nil
	case "right", "down", "tab":
		if md.sel < len(md.choices)-1 {
			md.sel++
		}
		m.modal = &md
		return m, nil
	case "enter":
		return m.decideModal(md.sel)
	case "esc":
		return m.decideModal(md.denyIdx)
	}
	return m, nil
}

// decideModal closes the overlay and runs the decision callback with the chosen
// index, returning any follow-up command.
func (m model) decideModal(choice int) (tea.Model, tea.Cmd) {
	md := m.modal
	m.modal = nil
	m.relayout()
	if md == nil || md.decide == nil {
		return m, nil
	}
	return m, md.decide(choice)
}

// modalView renders the overlay centered on a blank screen — blocking by
// construction, since the host returns it instead of the transcript while a
// modal is open.
func (m model) modalView() string {
	md := m.modal
	var b strings.Builder
	b.WriteString(modalTitleStyle.Render("⚠  " + md.title))
	if md.detail != "" {
		b.WriteString("\n\n")
		// The detail is unbounded user content (a verbatim command or path), so
		// wrap it to the box's inner width — otherwise a long line overflows and
		// glitches a narrow terminal (D-TUI-11). 8 leaves room for the double
		// border + padding; the floor stays at 1 so an extremely narrow terminal
		// still wraps within its own width rather than forcing an over-wide line.
		detailWidth := m.width - 8
		if detailWidth < 1 {
			detailWidth = 1
		}
		b.WriteString(modalDetailStyle.Width(detailWidth).Render(md.detail))
	}
	b.WriteString("\n\n")
	buttons := make([]string, len(md.choices))
	for i, c := range md.choices {
		if i == md.sel {
			buttons[i] = modalChoiceSelStyle.Render(c)
		} else {
			buttons[i] = modalChoiceStyle.Render(c)
		}
	}
	b.WriteString(strings.Join(buttons, "  "))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("←/→ choose · Enter confirm · Esc cancel"))

	// Wrapping the detail keeps the box at a sane width; MaxWidth is the final
	// backstop so the fixed title/keybar lines truncate rather than overflow an
	// extremely narrow terminal — degrade, never glitch (D-TUI-11).
	box := lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(modalBoxStyle.Render(b.String()))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// Modal styles: a severe red border and reverse-video selected button so a
// destructive prompt can't be glossed over.
var (
	modalBoxStyle       = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(ColorDiffRemovedText).Padding(0, 2)
	modalTitleStyle     = lipgloss.NewStyle().Bold(true).Foreground(ColorDiffRemovedText)
	modalDetailStyle    = lipgloss.NewStyle().Foreground(ColorFgDefault)
	modalChoiceStyle    = StyleNeutral.Padding(0, 1)
	modalChoiceSelStyle = lipgloss.NewStyle().Bold(true).Foreground(BgScreen).Background(ColorDiffRemovedText).Padding(0, 1)
)
