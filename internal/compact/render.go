package compact

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/tonitienda/agent-smith/internal/composition"
)

// RenderPreview formats a plan as the plain-text preview the user confirms
// before anything changes (AC: tokens before/after, what's being summarized). It
// is face-agnostic so the TUI and a headless face render the same panel.
func RenderPreview(p Plan) string {
	var b strings.Builder
	if p.Empty() {
		b.WriteString("Nothing to compact — only the current turn and the system/memory prefix are live.\n")
		b.WriteString("Have a few turns of conversation first, or use /clean to remove specific segments.")
		return b.String()
	}

	fmt.Fprintf(&b, "Preview: compacting %s into one summary would reclaim %s%s\n",
		countLabel(len(p.SourceIDs), "block"), tokensLabel(p.Tokens), costSuffix(p))

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	row := func(format string, a ...any) { _, _ = fmt.Fprintf(tw, format, a...) }
	row("  Handle\tType\tTokens\t\n")
	for i, id := range p.SourceIDs {
		row("  %s\t%s\t%s\t\n", composition.Handle(id), p.Sources[i].Kind, tokensLabel(p.SourceTokens[i]))
	}
	_ = tw.Flush()

	b.WriteString("\nThe summarized blocks stay on the log — nothing is lost.\n")
	b.WriteString("Nothing has changed yet. /compact --apply to summarize · /compact --cancel to discard.")
	return b.String()
}

// costSuffix appends the dollar amount reclaimed when the model is priced.
func costSuffix(p Plan) string {
	if !p.Priced {
		return ""
	}
	return " (" + p.Currency + strconv.FormatFloat(p.CostUSD, 'f', 4, 64) + ")"
}

// tokensLabel mirrors composition's compact token formatting.
func tokensLabel(n int) string {
	switch {
	case n >= 1_000_000:
		return trimDotZero(strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64)) + "M tok"
	case n >= 1_000:
		return trimDotZero(strconv.FormatFloat(float64(n)/1e3, 'f', 1, 64)) + "k tok"
	default:
		return strconv.Itoa(n) + " tok"
	}
}

func trimDotZero(s string) string { return strings.TrimSuffix(s, ".0") }

func countLabel(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
