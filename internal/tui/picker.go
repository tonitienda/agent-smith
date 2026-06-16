package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonitienda/agent-smith/internal/command"
)

// picker is an interactive single-select list opened when a command returns a
// command.Picker (AS-064 /resume): the user moves the highlight and presses
// Enter to choose, which re-dispatches the originating command with the chosen
// item's Value as its sole argument. Esc closes it without choosing, so the
// active session is left untouched. It reuses the inspect-panel chrome (pinned
// status line + footer) rather than introducing a new full-screen surface.
type picker struct {
	cmd   command.Command
	title string
	items []command.PickerItem
	sel   int
}

// pickerOpen reports whether an interactive picker is showing.
func (m model) pickerOpen() bool { return m.picker != nil }

// openPicker shows p's items as a single-select list bound to command c, so a
// choice re-runs c with the item's Value.
func (m *model) openPicker(c command.Command, p command.Picker) {
	m.picker = &picker{cmd: c, title: p.Title, items: p.Items}
}

// closePicker dismisses the picker without choosing.
func (m *model) closePicker() { m.picker = nil }

// handlePickerKey drives the interactive picker: up/down move the highlight,
// Enter chooses and re-dispatches the command with the item's Value, Esc/q
// cancels without changing the active session, and Ctrl+C quits.
func (m model) handlePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.picker
	switch msg.String() {
	case "ctrl+c":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case "esc", "q":
		m.closePicker()
		return m, nil
	case "up":
		if p.sel > 0 {
			p.sel--
		}
		return m, nil
	case "down":
		if p.sel < len(p.items)-1 {
			p.sel++
		}
		return m, nil
	case "enter":
		if len(p.items) == 0 {
			m.closePicker()
			return m, nil
		}
		chosen := p.items[p.sel]
		cmd := p.cmd
		m.closePicker()
		// Re-dispatch the command with the chosen value; for /resume this is the
		// exact `/resume <id>` path, so loading and rehydration are unchanged.
		return m, runCommand(cmd, []string{chosen.Value})
	}
	return m, nil
}

// pickerView renders the visible window of items with the selection highlighted,
// one row per item. It windows around the selection like the palette so a long
// list scrolls within the panel body rather than overflowing the chrome.
func (m model) pickerView() string {
	p := m.picker
	if len(p.items) == 0 {
		return dimStyle.Render("No items.")
	}
	limit := m.panel.Height
	if limit < 1 {
		limit = 1
	}
	start := 0
	if p.sel >= limit {
		start = p.sel - limit + 1
	}
	end := start + limit
	if end > len(p.items) {
		end = len(p.items)
	}
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		if i == p.sel {
			lines = append(lines, paletteSelStyle.Render("▸ "+p.items[i].Label))
		} else {
			lines = append(lines, paletteItemStyle.Render("  "+p.items[i].Label))
		}
	}
	return strings.Join(lines, "\n")
}

// pickerFooter is the one-line key hint shown beneath the picker.
func (m model) pickerFooter() string {
	title := m.picker.title
	if title == "" {
		title = "select"
	}
	return statusBarStyle.Render(fmt.Sprintf("%s — ↑/↓ to move · Enter to load · Esc to cancel", title))
}
