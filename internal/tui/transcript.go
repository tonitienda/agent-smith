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
		m.segs = append(m.segs, segment{kind: segTool, toolName: name, toolID: id})
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
				s.toolError = ev.Tool != nil && ev.Tool.Result != nil && ev.Tool.Result.IsError
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
		return style.Render(fmt.Sprintf("%s %s", icon, name))

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
