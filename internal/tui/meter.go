package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/tonitienda/agent-smith/internal/budget"
)

// Meter is the always-visible context-and-cost snapshot the status line shows
// (AS-025, PRD §7.8): how full the current model's context window is and what the
// session has cost so far. It is plain data so the face stays decoupled from the
// accounting engine (internal/cost) — cmd/smith computes it from the session log
// on each event, the same single accounting source the /cost command reads (no
// drift), with no extra model calls.
type Meter struct {
	// Tokens is the estimated number of tokens occupying the context window.
	Tokens int
	// Window is the current model's context-window size; 0 when unknown, in which
	// case the meter shows the token count without a percentage or bar.
	Window int
	// CostUSD is the session's cost so far, matching /cost.
	CostUSD float64
	// CostKnown is false when an unpriced turn makes CostUSD a lower bound; the
	// meter then shows the cost as unknown rather than a misleadingly exact figure.
	CostKnown bool
	// Currency is the money prefix for CostUSD (e.g. "$" or "EUR "), supplied by
	// the accounting engine so the meter agrees with /cost under a non-USD pricing
	// override. Empty defaults to "$".
	Currency string
	// BudgetUSD is the session's active spend ceiling (AS-041); 0 means no budget
	// is set, in which case the meter shows cost without a ceiling. When set, the
	// meter appends "/<ceiling>" and colors the cost by how near the ceiling it
	// is (green/yellow/red), so an approaching budget is visible at a glance.
	BudgetUSD float64
	// BudgetWarnFraction is the fraction of BudgetUSD at which the cost turns
	// yellow; outside (0,1) it falls back to the budget default.
	BudgetWarnFraction float64
}

// MeterFunc yields the current Meter for the active model. The model passes the
// status line's current model so the window denominator rescales the moment the
// model is switched (AS-023 /model). It is called once per loop event (not per
// keystroke), so the status line stays current without measurable input latency
// or extra model calls. A nil MeterFunc disables the meter.
type MeterFunc func(model string) Meter

// empty reports whether the meter has nothing worth showing yet — no window
// known and no usage recorded — so the status line can omit it before the first
// turn rather than render "0 tok · $0".
func (mt Meter) empty() bool {
	return mt.Window <= 0 && mt.Tokens == 0 && mt.CostUSD == 0
}

// meterBarWidth is the cell width of the percentage bar in the status line.
const meterBarWidth = 8

// render formats the meter for the status line: a colored fill bar, the
// used/window token counts and percentage, and the session cost. When the window
// is unknown only the raw token count and cost are shown. The empty meter renders
// as "" so the caller can omit it entirely.
func (mt Meter) render() string {
	if mt.empty() {
		return ""
	}

	var gauge string
	if mt.Window > 0 {
		pct := float64(mt.Tokens) / float64(mt.Window) * 100
		text := fmt.Sprintf("%s %s/%s %d%%",
			meterBar(pct), humanTokens(mt.Tokens), humanTokens(mt.Window), int(pct+0.5))
		gauge = meterStyle(pct).Render(text)
	} else {
		gauge = fmt.Sprintf("%s tok", humanTokens(mt.Tokens))
	}

	prefix := mt.Currency
	if prefix == "" {
		prefix = "$"
	}
	cost := prefix + "?"
	if mt.CostKnown {
		cost = prefix + strconv.FormatFloat(mt.CostUSD, 'f', 4, 64)
	}
	// A set budget appends the ceiling and colors the cost by enforcement state
	// (AS-041), so nearing or exceeding the budget is visible without opening
	// /cost — the same green/yellow/red language as the context gauge.
	if g := (budget.Guard{LimitUSD: mt.BudgetUSD, WarnFraction: mt.BudgetWarnFraction}); g.Enabled() {
		cost += "/" + prefix + strconv.FormatFloat(mt.BudgetUSD, 'f', 2, 64)
		if mt.CostKnown {
			cost = budgetStyle(g.Check(mt.CostUSD)).Render(cost)
		}
	}
	return gauge + " · " + cost
}

// budgetStyle colors the cost segment by budget enforcement state: green under
// the warning threshold, yellow once warning, red at or past the ceiling.
func budgetStyle(state budget.State) lipgloss.Style {
	switch state {
	case budget.Halt:
		return meterRedStyle
	case budget.Warn:
		return meterYellowStyle
	default:
		return meterGreenStyle
	}
}

// meterBar draws a fixed-width fill bar for pct (0–100, clamped). The fill is
// rounded to the nearest cell so a non-zero percentage always shows at least a
// sliver once it rounds up.
func meterBar(pct float64) string {
	switch {
	case pct < 0:
		pct = 0
	case pct > 100:
		pct = 100
	}
	filled := int(pct/100*meterBarWidth + 0.5)
	if filled > meterBarWidth {
		filled = meterBarWidth
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", meterBarWidth-filled)
}

// meterStyle colors the gauge by how full the window is: green under 60%, yellow
// under 85%, red at or beyond — so nearing the window limit is visible at a
// glance. The background matches the status bar so the colored span blends in.
func meterStyle(pct float64) lipgloss.Style {
	switch {
	case pct >= 85:
		return meterRedStyle
	case pct >= 60:
		return meterYellowStyle
	default:
		return meterGreenStyle
	}
}

// humanTokens formats a token count compactly for the status line: 1_234 -> "1.2k",
// 200_000 -> "200k", 1_047_576 -> "1M". A trailing ".0" is dropped so round
// numbers read cleanly.
func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return trimDotZero(strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64)) + "M"
	case n >= 1_000:
		return trimDotZero(strconv.FormatFloat(float64(n)/1e3, 'f', 1, 64)) + "k"
	default:
		return strconv.Itoa(n)
	}
}

func trimDotZero(s string) string { return strings.TrimSuffix(s, ".0") }

// Meter color styles. Backgrounds match statusBarStyle so the colored gauge sits
// flush in the status bar; the Matrix personality layer (AS-053) will own richer
// theming.
var (
	meterGreenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Background(lipgloss.Color("8"))
	meterYellowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Background(lipgloss.Color("8"))
	meterRedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Background(lipgloss.Color("8"))
)
