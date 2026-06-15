package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/schema"
)

// toolPreviewLines is how many lines of a tool result the collapsed card shows;
// the global expand toggle (Ctrl+G then t) reveals the rest (AS-024 AC1).
const toolPreviewLines = 6

// segKind is the role a transcript segment plays. Segments are appended as the
// loop streams events and rendered into the scrollback viewport.
type segKind int

const (
	segUser segKind = iota
	segAssistant
	segReasoning
	segTool
	segCommand // inline slash-command output
	segNotice  // a cancellation/info line
	segError
)

// segment is one rendered unit of the conversation: a user message, a streamed
// assistant/reasoning run, a tool invocation, or a notice/error line.
type segment struct {
	kind segKind
	text string

	// tool fields (segTool only)
	toolName  string
	toolID    string
	toolError bool
	toolDone  bool
	// toolArgs is a one-line summary of the call's arguments, shown on the card
	// while it runs; toolResult is the recorded result text, previewed when done
	// and expandable in full (AS-024).
	toolArgs   string
	toolResult string
	// toolSettled marks that the loop reported a definitive result for this call
	// (a UIToolFinished). A tool finalized only because its turn was interrupted
	// is done-but-not-settled, so a late authoritative result can still correct it.
	toolSettled bool

	// done marks a text run finalized, so the assistant body renders as markdown.
	done bool
	// rendered caches the markdown render of a done assistant segment for the
	// current wrap width; resize clears it.
	rendered string
}

// apply folds one loop UIEvent into the transcript. Unknown kinds are ignored,
// honoring the additive-event contract (event.go, PRD D2).
func (m *model) apply(ev loop.UIEvent) {
	switch ev.Kind {
	case loop.UITurnStart:
		m.curAssistant, m.curReasoning = -1, -1

	case loop.UITextDelta:
		if m.curAssistant < 0 {
			m.segs = append(m.segs, segment{kind: segAssistant})
			m.curAssistant = len(m.segs) - 1
		}
		m.segs[m.curAssistant].text += ev.Text

	case loop.UIReasoningDelta:
		if m.curReasoning < 0 {
			m.segs = append(m.segs, segment{kind: segReasoning})
			m.curReasoning = len(m.segs) - 1
		}
		m.segs[m.curReasoning].text += ev.Text

	case loop.UIToolStarted:
		name, id := toolIdentity(ev)
		args := ""
		if ev.Tool != nil {
			args = summarizeToolArgs(ev.Tool.Arguments)
		}
		m.segs = append(m.segs, segment{kind: segTool, toolName: name, toolID: id, toolArgs: args})
		// A tool call ends the current text run; later text starts a fresh bubble.
		m.curAssistant, m.curReasoning = -1, -1

	case loop.UIToolFinished:
		_, id := toolIdentity(ev)
		for i := range m.segs {
			s := &m.segs[i]
			// The loop's result is authoritative: apply it even if the segment was
			// already finalized as interrupted (done but not settled).
			if s.kind == segTool && s.toolID == id && !s.toolSettled {
				s.toolDone = true
				s.toolSettled = true
				if ev.Tool != nil && ev.Tool.Result != nil {
					s.toolError = ev.Tool.Result.IsError
					s.toolResult = toolResultText(ev.Tool.Result)
				}
				break
			}
		}

	case loop.UITurnComplete:
		m.finalizeText()
	}
}

// toolIdentity pulls the tool name and call ID out of a tool event, tolerating a
// nil payload.
func toolIdentity(ev loop.UIEvent) (name, id string) {
	if ev.Tool == nil {
		return "", ""
	}
	return ev.Tool.Name, ev.Tool.ToolUseID
}

// markPendingToolsInterrupted finalizes any tool segment still awaiting a result
// when a turn ends abnormally. The loop reconciles orphaned tool calls on the log
// without emitting UIToolFinished, so without this they would display as pending
// (⋯) forever after an Esc cancel or a surfaced error. They are left unsettled, so
// a late authoritative UIToolFinished can still correct the outcome.
func (m *model) markPendingToolsInterrupted() {
	for i := range m.segs {
		s := &m.segs[i]
		if s.kind == segTool && !s.toolDone {
			s.toolDone = true
			s.toolError = true
		}
	}
}

// finalizeText marks the current assistant and reasoning runs complete so they
// render as markdown, and ends the active text runs.
func (m *model) finalizeText() {
	if m.curAssistant >= 0 {
		m.segs[m.curAssistant].done = true
	}
	if m.curReasoning >= 0 {
		m.segs[m.curReasoning].done = true
	}
	m.curAssistant, m.curReasoning = -1, -1
}

// renderTranscript renders every segment into a single string for the viewport.
// The startup header (D-TUI-10) is the first thing in the scrollback when splash
// is on, so it shows on launch and scrolls away with the conversation.
func (m *model) renderTranscript() string {
	parts := make([]string, 0, len(m.segs)+1)
	if m.splash {
		parts = append(parts, m.headerView())
	}
	if len(m.segs) == 0 {
		parts = append(parts, dimStyle.Render("Ask Agent Smith anything to begin."))
		return strings.Join(parts, "\n\n")
	}
	for i := range m.segs {
		parts = append(parts, m.renderSegment(&m.segs[i]))
	}
	return strings.Join(parts, "\n\n")
}

// headerView is the small ASCII startup header: a banner plus project · model ·
// mode (D-TUI-10). No model call, no delay — it is pure projection of the cached
// status-line identity.
func (m *model) headerView() string {
	meta := strings.Join(nonEmpty(m.meta.Project, m.meta.Model, "work mode"), " · ")
	return bannerStyle.Render("▞▞ AGENT SMITH") + "\n" + dimStyle.Render(meta)
}

// renderSegment styles one segment. Assistant bodies render as markdown once
// done (cached per wrap width); everything else is plain styled text.
func (m *model) renderSegment(s *segment) string {
	switch s.kind {
	case segUser:
		return userLabelStyle.Render("you") + "\n" + s.text

	case segAssistant:
		body := s.text
		if s.done && m.renderer != nil {
			if s.rendered == "" {
				if out, err := m.renderer.Render(s.text); err == nil {
					s.rendered = strings.TrimRight(out, "\n")
				}
			}
			if s.rendered != "" {
				body = s.rendered
			}
		}
		return assistantLabelStyle.Render("smith") + "\n" + body

	case segReasoning:
		return reasoningLabelStyle.Render("thinking") + "\n" + dimStyle.Render(s.text)

	case segTool:
		icon := "⋯"
		style := toolLabelStyle
		if s.toolDone {
			icon = "✓"
			if s.toolError {
				icon = "✗"
				style = errorStyle
			}
		}
		name := s.toolName
		if name == "" {
			name = "tool"
		}
		head := style.Render(fmt.Sprintf("%s %s", icon, name))
		if s.toolArgs != "" {
			head += "  " + dimStyle.Render(s.toolArgs)
		}
		body := s.toolResult
		if body == "" {
			return head
		}
		if !m.expandTools {
			body = previewLines(body, toolPreviewLines)
		}
		return head + "\n" + indentBlock(body)

	case segCommand:
		body := strings.TrimRight(s.text, "\n")
		header := commandLabelStyle.Render("/" + s.toolName)
		if body == "" {
			return header
		}
		return header + "\n" + body

	case segNotice:
		return dimStyle.Render("— " + s.text + " —")

	case segError:
		return errorStyle.Render("! " + s.text)
	}
	return s.text
}

// summarizeToolArgs renders a call's JSON arguments as a compact one-line summary
// for the running tool card (AS-024). String fields are shown "key: value" in key
// order; the whole thing is truncated so the card stays one readable line. Args
// that aren't a JSON object (or are empty) yield no summary.
func summarizeToolArgs(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || len(obj) == 0 {
		return ""
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		var s string
		if err := json.Unmarshal(obj[k], &s); err == nil {
			parts = append(parts, fmt.Sprintf("%s: %s", k, oneLine(s)))
		}
	}
	return truncate(strings.Join(parts, ", "), 72)
}

// toolResultText extracts the human-readable text of a recorded tool result for
// the card preview: the text parts joined, falling back to stdout/stderr (shell
// results carry those) when there are none.
func toolResultText(r *schema.ToolResultBody) string {
	var b strings.Builder
	for _, p := range r.Content {
		if p.Type == "text" && p.Text != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(p.Text)
		}
	}
	if b.Len() == 0 {
		if r.Stdout != "" {
			b.WriteString(r.Stdout)
		}
		if r.Stderr != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(r.Stderr)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// previewLines keeps the first n lines of s, appending a dimmed marker naming how
// many were hidden and how to reveal them (Ctrl+G then t), so a long result stays
// scannable while remaining expandable (AS-024 AC1).
func previewLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	hidden := len(lines) - n
	kept := append([]string(nil), lines[:n]...)
	kept = append(kept, dimStyle.Render(fmt.Sprintf("… +%d more line(s) — Ctrl+G t to expand", hidden)))
	return strings.Join(kept, "\n")
}

// indentBlock prefixes every line with two spaces and dims it, so a tool result
// reads as subordinate detail under its card header.
func indentBlock(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = "  " + dimStyle.Render(ln)
	}
	return strings.Join(lines, "\n")
}

// oneLine collapses whitespace runs (including newlines) to single spaces so a
// multi-line argument value still fits on the one-line card summary.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// truncate caps s to n runes, appending an ellipsis when it had to cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Styles for transcript roles and the status bar. Colors are ANSI-256 so they
// degrade gracefully across terminals; the Matrix personality layer (AS-053)
// will own richer theming.
var (
	userLabelStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	assistantLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	reasoningLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("8"))
	toolLabelStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	commandLabelStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	errorStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	dimStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	bannerStyle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	statusBarStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("8"))
)

// lipglossWidth reports the rendered cell width of s, ignoring style escapes.
func lipglossWidth(s string) int { return lipgloss.Width(s) }

// newMarkdownRenderer builds a Glamour renderer for the given wrap width, or nil
// if one cannot be constructed (the transcript then shows raw text). A fixed
// dark style avoids probing the terminal background, keeping rendering
// deterministic.
func newMarkdownRenderer(width int) markdownRenderer {
	if width < 1 {
		width = 80
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil
	}
	return r
}
