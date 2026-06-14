package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/tonitienda/agent-smith/internal/loop"
)

// segKind is the role a transcript segment plays. Segments are appended as the
// loop streams events and rendered into the scrollback viewport.
type segKind int

const (
	segUser segKind = iota
	segAssistant
	segReasoning
	segTool
	segNotice // a cancellation/info line
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
		m.segs = append(m.segs, segment{kind: segTool, toolName: name, toolID: id})
		// A tool call ends the current text run; later text starts a fresh bubble.
		m.curAssistant, m.curReasoning = -1, -1

	case loop.UIToolFinished:
		_, id := toolIdentity(ev)
		for i := range m.segs {
			s := &m.segs[i]
			if s.kind == segTool && s.toolID == id && !s.toolDone {
				s.toolDone = true
				if ev.Tool != nil && ev.Tool.Result != nil {
					s.toolError = ev.Tool.Result.IsError
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
func (m *model) renderTranscript() string {
	if len(m.segs) == 0 {
		return dimStyle.Render("Ask Agent Smith anything to begin.")
	}
	parts := make([]string, 0, len(m.segs))
	for i := range m.segs {
		parts = append(parts, m.renderSegment(&m.segs[i]))
	}
	return strings.Join(parts, "\n\n")
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
		return style.Render(fmt.Sprintf("%s %s", icon, name))

	case segNotice:
		return dimStyle.Render("— " + s.text + " —")

	case segError:
		return errorStyle.Render("! " + s.text)
	}
	return s.text
}

// Styles for transcript roles and the status bar. Colors are ANSI-256 so they
// degrade gracefully across terminals; the Matrix personality layer (AS-053)
// will own richer theming.
var (
	userLabelStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	assistantLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	reasoningLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("8"))
	toolLabelStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errorStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	dimStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
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
