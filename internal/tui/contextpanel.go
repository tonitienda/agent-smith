package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/render"
)

// contextpanel.go renders the rich /context dashboard (AS-128, §7.3): a single
// proportional segmented bar of the token budget, a legend grid, and an inset
// stats rail. It consumes the face-agnostic command.ContextView (the handler in
// cmd/smith builds it from the composition engine) and owns all colour here, so
// the engine stays face-neutral and a headless face renders the plain Text form
// instead. Every figure degrades to a placeholder ("—") when its source data is
// unavailable rather than printing a misleading zero.

// contextBarMinWidth keeps the segmented bar legible on a narrow panel; above it
// the bar spans the full panel width (§7.3 "a single horizontal bar spanning the
// panel width").
const (
	contextBarMinWidth = 20
	contextPlaceholder = "—"
)

// renderContextPanel formats a ContextView as the styled /context panel body,
// sized to width columns (the panel viewport width). It is pure: the same view
// and width yield the same string, so it is straightforward to snapshot-test.
func renderContextPanel(v command.ContextView, width int) string {
	barWidth := width
	if barWidth < contextBarMinWidth {
		barWidth = contextBarMinWidth
	}

	var b strings.Builder
	writeCompactMarker(&b, v, barWidth)
	b.WriteString(segmentedBar(v, barWidth))
	b.WriteByte('\n')
	b.WriteString(scaleLine(v, barWidth))
	b.WriteString("\n\n")
	b.WriteString(legendGrid(v))
	if v.MemoryLoaded {
		b.WriteByte('\n')
		b.WriteString(StyleSuccess.Render("✓ CLAUDE.md loaded"))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString(StyleDim.Render("tip: /compact to reclaim tokens"))
	b.WriteString("\n\n")
	b.WriteString(statsRail(v))
	return b.String()
}

// barDenom is the token count the bar and marker scale against: the full window
// when known (so free space shows), else just the live occupancy. It returns 0
// when there is nothing to scale, so callers render an empty/placeholder bar.
func barDenom(v command.ContextView) int {
	if v.Window > 0 {
		return v.Window
	}
	return v.Used
}

// segmentedBar draws the proportional coloured bar: one run of █ per category
// sized by its window share via largest-remainder rounding (so the runs sum to
// exactly barWidth), then free space in the near-background fill. With no token
// data it renders an empty (all-free) bar.
func segmentedBar(v command.ContextView, barWidth int) string {
	if barWidth <= 0 {
		return ""
	}
	colors, cells := barRuns(v, barWidth)
	var b strings.Builder
	for i, n := range cells {
		if n <= 0 {
			continue
		}
		b.WriteString(lipgloss.NewStyle().Foreground(colors[i]).Render(strings.Repeat("█", n)))
	}
	return b.String()
}

// barRuns computes the bar's runs as parallel colour and cell-count slices: one
// run per category (in order), then free space when the window size is known.
// Cell counts are assigned by largest-remainder rounding so they sum to exactly
// barWidth. With no token data the whole bar is a single free run, so the bar is
// always barWidth cells wide. Pure and total-preserving, so it is unit-testable
// without parsing ANSI.
func barRuns(v command.ContextView, barWidth int) (colors []lipgloss.Color, cells []int) {
	denom := barDenom(v)
	if denom <= 0 || barWidth <= 0 {
		return []lipgloss.Color{ColorFree}, []int{max(barWidth, 0)}
	}

	tokens := make([]int, 0, len(v.Segments)+1)
	for _, s := range v.Segments {
		colors = append(colors, segmentColor(s.Label))
		tokens = append(tokens, s.Tokens)
	}
	// Free space is a run only when the window size is known; otherwise the bar is
	// entirely categories scaled to the live total.
	if v.Window > 0 {
		free := v.Window - v.Used
		if free < 0 {
			free = 0
		}
		colors = append(colors, ColorFree)
		tokens = append(tokens, free)
	}

	cells = make([]int, len(tokens))
	rem := make([]float64, len(tokens))
	total := 0
	for i, tk := range tokens {
		exact := float64(tk) / float64(denom) * float64(barWidth)
		cells[i] = int(exact)
		rem[i] = exact - float64(cells[i])
		total += cells[i]
	}
	// Distribute the rounding leftover to the runs with the largest fractional
	// remainder so the coloured cells total exactly barWidth.
	for leftover := barWidth - total; leftover > 0; leftover-- {
		best := -1
		for i := range rem {
			if best == -1 || rem[i] > rem[best] {
				best = i
			}
		}
		if best == -1 {
			break
		}
		cells[best]++
		rem[best] = -1 // spent; don't pick it again before others
	}
	return colors, cells
}

// writeCompactMarker draws the amber auto-compact marker row above the bar: a │
// at the threshold column with a label, shown only when the threshold and window
// are known so there is a real column to point at.
func writeCompactMarker(b *strings.Builder, v command.ContextView, barWidth int) {
	denom := barDenom(v)
	if v.CompactThreshold <= 0 || denom <= 0 || barWidth <= 0 {
		return
	}
	col := int(float64(v.CompactThreshold)/float64(denom)*float64(barWidth) + 0.5)
	if col < 0 {
		col = 0
	}
	if col > barWidth-1 {
		col = barWidth - 1
	}
	label := "auto-compact " + humanTokens(v.CompactThreshold)
	// Place the label left of the marker when it would overflow the right edge,
	// else to its right.
	var line string
	if col+1+len(label) <= barWidth {
		line = strings.Repeat(" ", col) + StyleRunning.Render("│ "+label)
	} else {
		start := col - len(label) - 1
		if start < 0 {
			start = 0
		}
		line = strings.Repeat(" ", start) + StyleRunning.Render(label+" │")
	}
	b.WriteString(line)
	b.WriteByte('\n')
}

// scaleLine renders the muted scale under the bar: 0 on the left, the used/total
// usage in the centre, and the window total on the right. Without a known window
// it collapses to the live token count.
func scaleLine(v command.ContextView, barWidth int) string {
	if v.Window <= 0 {
		return StyleMuted.Render(fmt.Sprintf("0  ·  %s used", render.Tokens(v.Used)))
	}
	pct := int(float64(v.Used)/float64(v.Window)*100 + 0.5)
	left := "0"
	center := fmt.Sprintf("%s / %s used · %d%%", render.Tokens(v.Used), render.Tokens(v.Window), pct)
	right := render.Tokens(v.Window)
	gapL := (barWidth - len(center)) / 2
	if gapL < 1 {
		gapL = 1
	}
	gapR := barWidth - len(left) - gapL - len(center) - len(right)
	if gapR < 1 {
		gapR = 1
	}
	return StyleMuted.Render(left + strings.Repeat(" ", gapL) + center + strings.Repeat(" ", gapR) + right)
}

// legendGrid lays the categories out in a two-column grid: a coloured swatch, the
// category name, its token count, and its window share.
func legendGrid(v command.ContextView) string {
	if len(v.Segments) == 0 {
		return StyleMuted.Render("(context is empty)")
	}
	cells := make([]string, 0, len(v.Segments))
	for _, s := range v.Segments {
		swatch := lipgloss.NewStyle().Foreground(segmentColor(s.Label)).Render("█")
		name := StyleNeutral.Render(fmt.Sprintf("%-14s", s.Label))
		val := StyleMuted.Render(fmt.Sprintf("%7s  %s", render.Tokens(s.Tokens), sharePct(s.Tokens, v.Window, v.Used)))
		cells = append(cells, swatch+" "+name+" "+val)
	}
	var b strings.Builder
	for i := 0; i < len(cells); i += 2 {
		b.WriteString("  " + cells[i])
		if i+1 < len(cells) {
			b.WriteString("    " + cells[i+1])
		}
		if i+2 < len(cells) {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// statsRail renders the inset stats card: window/used/free, cache read/write and
// hit-rate, and the session cost in the cost colour. Unknown figures show a
// placeholder so the card never implies a misleading zero.
func statsRail(v command.ContextView) string {
	windowStr, freeStr := contextPlaceholder, contextPlaceholder
	if v.Window > 0 {
		windowStr = render.Tokens(v.Window)
		free := v.Window - v.Used
		if free < 0 {
			free = 0
		}
		freeStr = render.Tokens(free)
	}
	cacheStr := contextPlaceholder
	if v.CacheKnown {
		cacheStr = fmt.Sprintf("read %s · write %s · %d%% hit",
			render.Tokens(v.CacheReadTokens), render.Tokens(v.CacheWriteTokens), int(v.CacheHitRate*100+0.5))
	}
	costStr := contextPlaceholder
	if v.Priced {
		costStr = render.Money(v.Currency, v.CostUSD)
	}

	var b strings.Builder
	row := func(label, val string) {
		b.WriteString(StyleMuted.Render(fmt.Sprintf("%-8s", label)) + StyleNeutral.Render(val) + "\n")
	}
	row("window", windowStr)
	row("used", render.Tokens(v.Used))
	row("free", freeStr)
	row("cache", cacheStr)
	b.WriteString(StyleMuted.Render(fmt.Sprintf("%-8s", "cost")) + StyleCost.Render(costStr))

	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Background(BgInset).
		Padding(0, 1)
	return card.Render(strings.TrimRight(b.String(), "\n"))
}

// sharePct formats a category's window share, preferring the window as the base
// (so shares sum to occupancy, not to 100%) and falling back to the live total.
func sharePct(tokens, window, used int) string {
	base := window
	if base <= 0 {
		base = used
	}
	if base <= 0 {
		return contextPlaceholder
	}
	return fmt.Sprintf("%d%%", int(float64(tokens)/float64(base)*100+0.5))
}

// segmentColor maps a composition group label to its stable phosphor fill,
// honouring the semantic role→colour rule (user amber, assistant green,
// reasoning muted). Unknown labels fall back to neutral so a new group still
// renders. Tokens live in colors.go per the palette invariant.
func segmentColor(label string) lipgloss.Color {
	switch label {
	case "assistant":
		return ColorBrand
	case "tool result":
		return ColorDone
	case "skill":
		return ColorCommand
	case "system+memory":
		return ColorAmberMuted
	case "user":
		return ColorAmberBright
	case "file read":
		return ColorSegFile
	case "reasoning":
		return ColorMuted
	default:
		return ColorNeutral
	}
}
