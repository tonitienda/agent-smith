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

	sym := symbol(s.Currency)

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
			money(t.TotalUSD, t.Priced, sym))
	}
	row("  Σ\t\t%s\t%s\t%s\t%s\t%s\t\n",
		commas(s.Total.Input), commas(s.Total.Output),
		commas(s.Total.CacheRead), commas(s.Total.CacheWrite),
		money(s.TotalUSD, true, sym))
	_ = tw.Flush()

	// The cache-read token count is exact, but the dollar savings only sum the
	// priced turns — when a turn is unpriced its (possibly cached) reads add no
	// dollars, so the figure is a lower bound. Mark it so it never reads exact.
	savings := money(s.CacheSavingsUSD, true, sym)
	if !s.AllPriced {
		savings += " (lower bound — unpriced turns excluded)"
	}
	fmt.Fprintf(&b, "\nCache savings: %s tokens read from cache · %s\n",
		commas(s.CacheReadTokens), savings)

	if !s.AllPriced {
		fmt.Fprintf(&b, "\nNote: some turns ran on a model with no pricing entry (shown as %s);\n"+
			"their tokens are exact but the dollar totals above are a lower bound.\n"+
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

// Symbol returns the money prefix for a currency, exported so other faces — the
// always-visible context meter (AS-025) — format amounts consistently with the
// /cost report instead of hard-coding "$".
func Symbol(currency string) string { return symbol(currency) }

// symbol returns the money prefix for a currency: "$" for USD (and the empty
// default), otherwise the ISO code plus a space (e.g. "EUR 1.2345"), so the
// rendered amounts stay consistent with the currency named in the header.
func symbol(currency string) string {
	if currency == "" || currency == "USD" {
		return "$"
	}
	return currency + " "
}

// money formats an amount with the currency symbol, or the unknown mark when the
// turn is unpriced.
func money(v float64, priced bool, sym string) string {
	if !priced {
		return unknownMark
	}
	return sym + strconv.FormatFloat(v, 'f', 4, 64)
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
