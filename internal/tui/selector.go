package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tonitienda/agent-smith/internal/command"
)

// selector is the interactive multi-select surface opened when a command returns
// a command.Selector (AS-068 /clean in-panel selection): the user moves a cursor
// over the live segments, toggles a selection with space, and watches the live
// reclaim preview in the footer; Enter applies the staged removal. The cursor
// also reaches an archive section where Enter (or r) restores a single excluded
// block. Esc closes it without changing anything. It reuses the inspect-panel
// chrome (pinned status line + footer) like the picker, so it degrades on a
// short terminal identically. The engine work lives in the Selector's closures
// (the command wiring closes over the session), so this surface holds only data
// and never imports the projection/clean packages (the AS-021 boundary).
type selector struct {
	// cmd is the originating command's name, captured when the surface opens, so
	// the applied/restored result renders under the right header even though
	// command.Selector is a generic surface (not tied to /clean).
	cmd     string
	sel     command.Selector
	cursor  int                   // index across Items then Archive
	checked map[string]bool       // selected Item Values
	preview command.SelectPreview // live reclaim feedback for the current selection
}

// selectorOpen reports whether an interactive selector is showing.
func (m model) selectorOpen() bool { return m.selector != nil }

// openSelector shows s as the interactive selection surface for the command
// named cmdName and seeds the live preview for the empty (nothing selected yet)
// state.
func (m *model) openSelector(cmdName string, s command.Selector) {
	sl := &selector{cmd: cmdName, sel: s, checked: map[string]bool{}}
	if s.Preview != nil {
		sl.preview = s.Preview(nil)
	}
	m.selector = sl
}

// closeSelector dismisses the selector without changing anything.
func (m *model) closeSelector() { m.selector = nil }

// selectedValues returns the checked item Values in display order — the input to
// the Selector's Preview and Apply closures.
func (m model) selectedValues() []string {
	s := m.selector
	var out []string
	for _, it := range s.sel.Items {
		if s.checked[it.Value] {
			out = append(out, it.Value)
		}
	}
	return out
}

// handleSelectorKey drives the selector: up/down move the cursor across the
// selectable items and the archive, space toggles a selection (refreshing the
// live preview), Enter applies the selection (or restores the archive row under
// the cursor), r restores an archive row, Esc/q cancels, and Ctrl+C quits.
func (m model) handleSelectorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.selector
	total := len(s.sel.Items) + len(s.sel.Archive)
	switch msg.String() {
	case "ctrl+c":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case "esc", "q":
		m.closeSelector()
		return m, nil
	case "up":
		if s.cursor > 0 {
			s.cursor--
		}
		return m, nil
	case "down":
		if s.cursor < total-1 {
			s.cursor++
		}
		return m, nil
	case " ":
		// Toggle only on a selectable item row; the archive is restore-only.
		if i := s.cursor; i < len(s.sel.Items) {
			v := s.sel.Items[i].Value
			if s.checked[v] {
				delete(s.checked, v)
			} else {
				s.checked[v] = true
			}
			if s.sel.Preview != nil {
				s.preview = s.sel.Preview(m.selectedValues())
			}
		}
		return m, nil
	case "r":
		return m.restoreUnderCursor()
	case "enter":
		// On an archive row Enter restores that single block; otherwise it applies
		// the staged selection.
		if s.cursor >= len(s.sel.Items) {
			return m.restoreUnderCursor()
		}
		return m.applySelection()
	}
	return m, nil
}

// applySelection commits the checked items through the Selector's Apply closure
// (the existing clean.Apply path — one exclusion event) and surfaces the result
// as an inline notice. Nothing checked is a no-op so Enter on an empty selection
// doesn't append an empty removal.
func (m model) applySelection() (tea.Model, tea.Cmd) {
	s := m.selector
	vals := m.selectedValues()
	if len(vals) == 0 || s.sel.Apply == nil {
		return m, nil
	}
	result := s.sel.Apply(vals)
	name := s.cmd
	m.closeSelector()
	m.appendSegment(segment{kind: segCommand, toolName: name, text: result, done: true})
	return m, nil
}

// restoreUnderCursor re-includes the single archive block under the cursor
// through the Selector's Restore closure, leaving every other removal in place.
func (m model) restoreUnderCursor() (tea.Model, tea.Cmd) {
	s := m.selector
	i := s.cursor - len(s.sel.Items)
	if i < 0 || i >= len(s.sel.Archive) || s.sel.Restore == nil {
		return m, nil
	}
	result := s.sel.Restore(s.sel.Archive[i].Value)
	name := s.cmd
	m.closeSelector()
	m.appendSegment(segment{kind: segCommand, toolName: name, text: result, done: true})
	return m, nil
}

// selectorView renders the selectable items (with checkboxes) followed by the
// restorable archive, windowed around the cursor so a long list scrolls within
// the panel body rather than overflowing the chrome (like the picker).
func (m model) selectorView() string {
	s := m.selector
	var lines []string
	cursorLine := 0

	if len(s.sel.Items) == 0 {
		lines = append(lines, dimStyle.Render("No live segments to remove."))
	}
	for i, it := range s.sel.Items {
		box := "[ ]"
		if s.checked[it.Value] {
			box = "[x]"
		}
		switch {
		case s.cursor == i:
			cursorLine = len(lines)
			lines = append(lines, paletteSelStyle.Render("▸ "+box+" "+it.Label))
		case s.checked[it.Value]:
			lines = append(lines, selectorCheckedStyle.Render("  "+box+" "+it.Label))
		default:
			lines = append(lines, paletteItemStyle.Render("  "+box+" "+it.Label))
		}
	}

	if len(s.sel.Archive) > 0 {
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render("Excluded from the window — Enter or r to restore one:"))
		for j, it := range s.sel.Archive {
			idx := len(s.sel.Items) + j
			if s.cursor == idx {
				cursorLine = len(lines)
				lines = append(lines, paletteSelStyle.Render("▸     "+it.Label))
			} else {
				lines = append(lines, paletteItemStyle.Render("      "+it.Label))
			}
		}
	}

	limit := m.panel.Height
	if limit < 1 {
		limit = 1
	}
	start := 0
	if cursorLine >= limit {
		start = cursorLine - limit + 1
	}
	end := start + limit
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n")
}

// selectorFooter is the one-line hint beneath the selector: the live reclaim
// preview (or the first warning) plus the key bindings.
func (m model) selectorFooter() string {
	s := m.selector
	left := s.preview.Summary
	if left == "" {
		left = "nothing selected"
	}
	if len(s.preview.Warnings) > 0 {
		left += "  ⚠ " + s.preview.Warnings[0]
	}
	return statusBarStyle.Render(left + " — space select · Enter apply · r restore · Esc cancel")
}

// selectorCheckedStyle marks a selected (but not cursored) row so the staged
// selection reads at a glance without the reverse-video cursor highlight.
var selectorCheckedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
