package cost

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
)

// Render formats a Summary as the plain-text report the /cost command shows: a
// per-turn table broken down by input / output / cache, the session totals, and
// the cache savings in tokens and dollars. It is face-agnostic so the TUI
// (AS-025) and a headless face (AS-051) render the same accounting.
func Render(s Summary) string {
	if len(s.Turns) == 0 {
		return "No usage recorded yet — run a turn first."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Session cost (%s)\n\n", s.Currency)

	// tw writes through to the strings.Builder, which never errors, so the write
	// results are discarded (and Flush likewise).
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', tabwriter.AlignRight)
	row := func(format string, a ...any) { _, _ = fmt.Fprintf(tw, format, a...) }
	row("  #\tModel\tInput\tOutput\tCache rd\tCache wr\tCost\t\n")
	for _, t := range s.Turns {
		row("  %d\t%s\t%s\t%s\t%s\t%s\t%s\t\n",
			t.Index, modelLabel(t.Model),
			commas(t.Tokens.Input), commas(t.Tokens.Output),
			commas(t.Tokens.CacheRead), commas(t.Tokens.CacheWrite),
			dollars(t.TotalUSD, t.Priced))
	}
	row("  Σ\t\t%s\t%s\t%s\t%s\t%s\t\n",
		commas(s.Total.Input), commas(s.Total.Output),
		commas(s.Total.CacheRead), commas(s.Total.CacheWrite),
		dollars(s.TotalUSD, true))
	_ = tw.Flush()

	fmt.Fprintf(&b, "\nCache savings: %s tokens read from cache · %s\n",
		commas(s.CacheReadTokens), dollars(s.CacheSavingsUSD, true))

	if !s.AllPriced {
		fmt.Fprintf(&b, "\nNote: some turns ran on a model with no pricing entry (shown as %s);\n"+
			"their tokens are exact but the dollar total above is a lower bound.\n"+
			"Add rates via a %s override file to price them.\n", unknownMark, EnvPricingFile)
	}
	return strings.TrimRight(b.String(), "\n")
}

// unknownMark is shown in the cost column for a turn whose model has no price.
const unknownMark = "—"

func modelLabel(m string) string {
	if m == "" {
		return unknownMark
	}
	return m
}

// dollars formats a USD amount, or the unknown mark when the turn is unpriced.
func dollars(v float64, priced bool) string {
	if !priced {
		return unknownMark
	}
	return "$" + strconv.FormatFloat(v, 'f', 4, 64)
}

// commas formats n with thousands separators, e.g. 12000 -> "12,000".
func commas(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(c)
	}
	if neg {
		return "-" + out.String()
	}
	return out.String()
}
