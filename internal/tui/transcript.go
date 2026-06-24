package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/personality"
	"github.com/tonitienda/agent-smith/schema"
)

// toolPreviewLines is how many lines of a tool result the collapsed card shows;
// the global expand toggle (Ctrl+G then t) reveals the rest (AS-024 AC1).
const toolPreviewLines = 6

// argValueByteCap bounds how many bytes of a single argument value the card
// summary inspects, so a huge value can't make the (once-per-call) summary scan
// the whole payload. It is comfortably larger than the final 72-rune cap.
const argValueByteCap = 256

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

	case loop.UIBudgetWarning:
		sym := m.budgetSymbol()
		m.segs = append(m.segs, segment{kind: segNotice, done: true, text: fmt.Sprintf(
			"budget warning: spent %s%.4f of %s%.2f ceiling", sym, ev.BudgetSpentUSD, sym, ev.BudgetLimitUSD)})

	case loop.UIBudgetHalt:
		sym := m.budgetSymbol()
		m.segs = append(m.segs, segment{kind: segNotice, done: true, text: fmt.Sprintf(
			"budget reached: spent %s%.4f of %s%.2f — turn halted. Raise it with /budget, or trim with /clean or /compact.",
			sym, ev.BudgetSpentUSD, sym, ev.BudgetLimitUSD)})

	case loop.UIBudgetUnpriced:
		sym := m.budgetSymbol()
		m.segs = append(m.segs, segment{kind: segNotice, done: true, text: fmt.Sprintf(
			"budget not enforceable: the active model has no pricing, so spend against your %s%.2f ceiling cannot be tracked. Set pricing for it (see $SMITH_PRICING) or switch models with /model.",
			sym, ev.BudgetLimitUSD)})

	case loop.UIAutoCompact:
		m.segs = append(m.segs, segment{kind: segNotice, done: true, text: ev.Text})
	}
}

// budgetSymbol is the currency prefix for budget notices, taken from the cached
// meter snapshot so the message agrees with /cost, /budget, and the status-line
// meter under a non-USD pricing table; it defaults to "$" before the first
// snapshot exists.
func (m *model) budgetSymbol() string {
	if s := m.meterState.Currency; s != "" {
		return s
	}
	return "$"
}

// segmentsFromBlocks rebuilds the visible transcript from a session's projected
// live blocks, so a resumed (or relaunched-with --resume) session shows its
// prior turns rendered exactly as they were live: user/assistant text,
// reasoning, and tool calls paired with their results in the AS-024 card. It is
// pure projection — no model calls — and mirrors apply's live folding so a
// rehydrated turn is indistinguishable from one that just streamed (AS-064).
func segmentsFromBlocks(blocks []schema.Block) []segment {
	segs := make([]segment, 0, len(blocks))
	for i := range blocks {
		b := blocks[i]
		switch b.Kind {
		case schema.KindText:
			// Only user and assistant text is conversation the user sees; system and
			// harness text blocks are model-facing context, not transcript.
			if b.Text == nil || b.Text.Text == "" {
				continue
			}
			switch b.Role {
			case schema.RoleUser:
				segs = append(segs, segment{kind: segUser, text: b.Text.Text, done: true})
			case schema.RoleAssistant:
				segs = appendMerged(segs, segAssistant, b.Text.Text)
			}

		case schema.KindReasoning:
			// Only visible assistant reasoning replays: redacted/encrypted spans carry
			// no Text, and non-assistant reasoning is never shown live.
			if b.Reasoning == nil || b.Reasoning.Text == "" || b.Role != schema.RoleAssistant {
				continue
			}
			segs = appendMerged(segs, segReasoning, b.Reasoning.Text)

		case schema.KindToolCall:
			// Server tool calls are provider-internal — the live loop never opens a
			// card for them (no UIToolStarted), so a replay must not conjure a ghost
			// card either.
			if b.ToolCall == nil || b.ToolCall.ToolKind == schema.ToolKindServer {
				continue
			}
			segs = append(segs, segment{
				kind:     segTool,
				toolName: b.ToolCall.Name,
				toolID:   b.ToolCall.ToolUseID,
				toolArgs: summarizeToolArgs(b.ToolCall.Arguments),
			})

		case schema.KindToolResult:
			if b.ToolResult != nil {
				foldToolResult(segs, b.ToolResult)
			}
		}
	}
	// A tool card left without a recorded result is an interrupted call (the turn
	// ended before the result landed); mark it done+error so a resumed transcript
	// never shows a permanently-pending (⋯) tool — the rehydration analogue of
	// markPendingToolsInterrupted.
	for i := range segs {
		if s := &segs[i]; s.kind == segTool && !s.toolDone {
			s.toolDone = true
			s.toolError = true
		}
	}
	return segs
}

// appendMerged adds text as a kind segment, merging into the immediately
// preceding segment when it is already the same kind — so consecutive assistant
// (or reasoning) blocks render under one header, the way the live loop folds
// consecutive deltas into a single segment. A tool card or a role change between
// two text blocks breaks the adjacency, exactly as it does live.
func appendMerged(segs []segment, kind segKind, text string) []segment {
	if n := len(segs); n > 0 && segs[n-1].kind == kind {
		segs[n-1].text += "\n\n" + text
		segs[n-1].rendered = "" // invalidate any cached markdown render
		return segs
	}
	return append(segs, segment{kind: kind, text: text, done: true})
}

// foldToolResult attaches a recorded tool result to its pending call card,
// mirroring apply's UIToolFinished handling so a replayed result settles the
// card exactly as a live one does. A result with no matching call card (e.g. a
// fused server-tool result) is ignored, as it is live.
func foldToolResult(segs []segment, r *schema.ToolResultBody) {
	for i := range segs {
		s := &segs[i]
		if s.kind == segTool && s.toolID == r.ToolUseID && !s.toolSettled {
			s.toolDone = true
			s.toolSettled = true
			s.toolError = r.IsError
			s.toolResult = toolResultText(r)
			return
		}
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
	if len(m.segs) == 0 {
		return m.emptyState()
	}
	parts := make([]string, 0, len(m.segs)+1)
	if m.splash {
		parts = append(parts, m.headerView())
	}
	for i := range m.segs {
		parts = append(parts, m.renderSegment(&m.segs[i]))
	}
	return strings.Join(parts, "\n\n")
}

// emptyState renders the launch/idle screen: the splash header and invite copy,
// with the AS-126 digital rain composited behind it at medium+ intensity. The
// foreground copy overwrites whole rows, so the chrome-only rain never bleeds
// over substance (internal/tui/CLAUDE.md invariant 3) — there is none here, but
// the same discipline keeps the copy legible.
func (m *model) emptyState() string {
	// --no-splash suppresses everything above the input bar (AS-122): no header, no
	// invite, no rain. The transcript stays blank until the first turn lands.
	if !m.splash {
		return ""
	}
	fg := append(strings.Split(m.headerView(), "\n"), "")
	fg = append(fg, StyleNeutral.Render("Ask Agent Smith anything to begin."), "")
	// The static command hint is the default invite; at medium+ intensity the
	// rotating idle phrase takes its place after a 3s beat (AS-122 §7.1). The rain
	// ticker re-renders the empty state every frame, so the swap happens on its own.
	if phrase := m.idlePhrase(); phrase != "" && time.Since(m.launched) >= 3*time.Second {
		fg = append(fg, StyleDim.Render("  "+phrase))
	} else {
		fg = append(fg, StyleDim.Render("  type / for commands · Ctrl+G c context · /serious mute theme"))
	}
	// Build (or resize) the rain here, not only on the tick, so the first frame and
	// every post-resize frame already carry correctly-sized rain — no 60ms startup
	// flash or one-frame layout glitch (Gemini review). ensureRain is gated on
	// rainActive so a muted theme stays plain.
	if m.rainActive() {
		m.ensureRain()
	}
	if m.rain == nil {
		return strings.Join(fg, "\n")
	}
	rows := strings.Split(m.rain.render(), "\n")
	for i, line := range fg {
		if i < len(rows) {
			rows[i] = line
		} else {
			rows = append(rows, line)
		}
	}
	return strings.Join(rows, "\n")
}

// idlePhrase is the rotating Matrix one-liner shown below the invite at medium+
// intensity (AS-126 §3); empty when the theme is muted. It reuses the clock-
// bucketed personality rotation so the phrase changes without per-render state.
func (m model) idlePhrase() string {
	if m.pers == nil || m.pers.Intensity() < personality.IntensityMedium {
		return ""
	}
	return m.pers.StatusLine()
}

// headerView is the small ASCII startup header: a banner plus project · model ·
// mode (D-TUI-10). No model call, no delay — it is pure projection of the cached
// status-line identity. While the one-shot glitch-in is active the logo renders
// with a couple of glyphs replaced by block noise (AS-126 §5).
func (m *model) headerView() string {
	meta := strings.Join(nonEmpty(m.meta.Project, m.meta.Model, "work mode"), " · ")
	logo := "▞▞ AGENT SMITH"
	if m.glitch {
		logo = glitchLogo(logo)
	}
	rule := StyleDividerLogo.Render(strings.Repeat("─", m.ruleWidth()))
	return StyleBanner.Render(logo) + "\n" + rule + "\n" + StyleMuted.Render(meta)
}

// ruleWidth is the cell width of the splash underrule: the full transcript width,
// falling back to the logo width before the first resize so the rule is never
// zero-length (AS-122 §7.1).
func (m *model) ruleWidth() int {
	if w := m.viewport.Width; w > 0 {
		return w
	}
	return lipglossWidth("▞▞ AGENT SMITH")
}

// glitchLogo replaces two glyphs in the banner with block noise for the one-shot
// startup glitch-in; a single frame later headerView settles to the clean logo.
func glitchLogo(s string) string {
	r := []rune(s)
	for _, i := range []int{6, 10} { // two letters inside "AGENT SMITH"
		if i >= 0 && i < len(r) && r[i] != ' ' {
			if i%2 == 0 {
				r[i] = '░'
			} else {
				r[i] = '▒'
			}
		}
	}
	return string(r)
}

// userLabel is the chrome display-name for the user's turns: the Matrix name
// (e.g. "Mr. Anderson") at medium+ intensity, the plain "you" when muted. It goes
// through personality.Name so the one name map stays the source of truth (AS-126
// §4); a nil personality (tests) keeps the plain label.
func (m model) userLabel() string {
	if m.pers != nil {
		return m.pers.Name(personality.RoleUser)
	}
	return "you"
}

// renderSegment styles one segment. Assistant bodies render as markdown once
// done (cached per wrap width); everything else is plain styled text.
func (m *model) renderSegment(s *segment) string {
	switch s.kind {
	case segUser:
		return userLabelStyle.Render(m.userLabel()) + "\n" + s.text

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
		var v any
		if err := json.Unmarshal(obj[k], &v); err != nil {
			continue
		}
		// A string value is shown directly; any other JSON type (number, bool,
		// array, object) is re-encoded so it appears in the summary rather than
		// being silently dropped.
		s, ok := v.(string)
		if !ok {
			b, _ := json.Marshal(v)
			s = string(b)
		}
		// Cap each value before whitespace-collapsing so a huge argument (an edit's
		// old_string/new_string can be large) doesn't get scanned in full just to
		// produce a 72-rune summary.
		if len(s) > argValueByteCap {
			s = s[:argValueByteCap]
		}
		parts = append(parts, fmt.Sprintf("%s: %s", k, oneLine(s)))
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

// Styles for transcript roles and the status bar. All colors come from the
// phosphor token table in colors.go (no raw hex here); the Matrix personality
// layer (AS-053) owns richer theming on top.
var (
	userLabelStyle      = StyleUser
	assistantLabelStyle = StyleAssistant
	reasoningLabelStyle = StyleThinking.Bold(true)
	toolLabelStyle      = StyleToolName
	commandLabelStyle   = StyleSlashCommand.Bold(true)
	errorStyle          = StyleError
	dimStyle            = StyleDim
	statusBarStyle      = lipgloss.NewStyle().Foreground(ColorFgDefault).Background(BgStatusLine)
	// modeBarStyle dresses the pinned Coding Mode phase tracker (AS-073) in a
	// distinct color from the status bar so entering the mode reads as crossing a
	// threshold (D-CODE-4), not just another status row.
	modeBarStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorFgDefault).Background(BgModeBar)
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
