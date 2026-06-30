package tui

import "github.com/charmbracelet/lipgloss"

// colors.go is the single source of truth for the TUI's green + amber phosphor
// palette (docs/design/tui-visual-design.md §2-3, internal/tui/CLAUDE.md
// invariant 1-2). Every color used anywhere under internal/tui/ MUST come from a
// named token here — no raw hex or bare ANSI index at a call site. Later tickets
// that need an extra hue add a named token here rather than inlining a literal.
//
// Fallbacks: tokens are authored as truecolor hex. Lipgloss detects the active
// terminal color profile (via termenv) and degrades each hex to the nearest
// 256-color / 16-color value automatically, so 256-color terminals get a sane
// approximation without a hand-maintained parallel table. The fallback indices
// listed in AS-121 are the reference targets that this nearest-color degradation
// reproduces.
//
// Animation tick strategy (shared contract for AS-122/123/124/125/126/130):
// fan out a distinct tea.Tick per cadence (caret blink, typewriter, rain,
// spinner, pulse have incompatible periods). "Reuse the same tick" downstream
// means "do not add a second ticker for an existing cadence", not one global
// period.
var (
	// Green ramp (bright -> dark). The ramp ColorCommand -> ColorNeutral ->
	// ColorMuted -> ColorDim -> ColorDimmest stays monotonically darker.
	ColorBrand     = lipgloss.Color("#00ff66") // brand, assistant, key accent
	ColorDone      = lipgloss.Color("#00cc52") // success, completed work
	ColorCommand   = lipgloss.Color("#7dffa8") // slash commands, cost
	ColorFgDefault = lipgloss.Color("#c4e3cd") // primary body text
	ColorNeutral   = lipgloss.Color("#9fb4a3") // tool names, values, paths
	ColorMid       = lipgloss.Color("#7d9a84") // secondary detail
	ColorMuted     = lipgloss.Color("#5f7a66") // args, secondary chrome
	ColorDim       = lipgloss.Color("#4f6a57") // tool output body (brighter dim)
	ColorDimmest   = lipgloss.Color("#38503f") // placeholder / disabled (darkest)

	// Amber — the single warm accent.
	ColorAmberBright = lipgloss.Color("#ffb000") // user, warning, running
	ColorAmberMuted  = lipgloss.Color("#caa24a") // goal, "working…"

	// Chrome / surfaces.
	BgScreen          = lipgloss.Color("#0a0e0b")
	BgInset           = lipgloss.Color("#0c120e")
	BgModeBar         = lipgloss.Color("#103a22") // mode bar; also selected-row fill
	BgStatusLine      = lipgloss.Color("#16201a")
	ColorModeName     = lipgloss.Color("#bdf0cf") // Coding Mode name on the mode bar (AS-125)
	ColorPhaseIdle    = lipgloss.Color("#4f8a64") // idle phase names on the mode bar (AS-125)
	ColorModeHint     = lipgloss.Color("#2f7a4c") // mode-bar key hint (AS-125)
	ColorBorder       = lipgloss.Color("#16241b")
	ColorBorderActive = lipgloss.Color("#1c3322")
	ColorBorderSelect = lipgloss.Color("#1f6b3f")
	ColorTree         = lipgloss.Color("#314a3a")
	ColorDividerLogo  = lipgloss.Color("#1d2c22") // splash logo underrule (AS-122)

	// Diff — the desaturated red is the only non-phosphor hue in the system.
	ColorDiffAddedText   = lipgloss.Color("#7dffa8")
	ColorDiffRemovedText = lipgloss.Color("#e08a8a")
	ColorDiffContextText = lipgloss.Color("#6f8a76")
)

// Semantic role styles (docs/design/tui-visual-design.md §3). Roles map to color
// by meaning, never aesthetics. Downstream tickets
// (AS-122/124/125/127/128/129/130/131) rely on these names existing.
var (
	StyleUser         = lipgloss.NewStyle().Bold(true).Foreground(ColorAmberBright)
	StyleAssistant    = lipgloss.NewStyle().Bold(true).Foreground(ColorBrand)
	StyleThinking     = lipgloss.NewStyle().Foreground(ColorMuted)
	StyleToolName     = lipgloss.NewStyle().Foreground(ColorNeutral)
	StyleToolArgs     = lipgloss.NewStyle().Foreground(ColorMuted)
	StyleToolOutput   = lipgloss.NewStyle().Foreground(ColorDim)
	StyleSuccess      = lipgloss.NewStyle().Foreground(ColorDone)
	StyleRunning      = lipgloss.NewStyle().Foreground(ColorAmberBright)
	StyleSlashCommand = lipgloss.NewStyle().Foreground(ColorCommand)
	StyleFilePath     = lipgloss.NewStyle().Foreground(ColorNeutral)
	StyleGoal         = lipgloss.NewStyle().Foreground(ColorAmberMuted)
	StyleCost         = lipgloss.NewStyle().Foreground(ColorCommand)
	StyleError        = lipgloss.NewStyle().Foreground(ColorDiffRemovedText)
	StyleNeutral      = lipgloss.NewStyle().Foreground(ColorNeutral)
	StyleMuted        = lipgloss.NewStyle().Foreground(ColorMuted)
	StyleDim          = lipgloss.NewStyle().Foreground(ColorDim)
	StyleBanner       = lipgloss.NewStyle().Bold(true).Foreground(ColorBrand)
	StyleDividerLogo  = lipgloss.NewStyle().Foreground(ColorDividerLogo) // splash underrule (AS-122)

	// Tool-card left rule (AS-124): active while the call is in flight, idle once
	// it settles, so the border dims as the card freezes.
	StyleBorderActive = lipgloss.NewStyle().Foreground(ColorBorderActive)
	StyleBorderIdle   = lipgloss.NewStyle().Foreground(ColorBorder)
)
