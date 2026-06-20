// Package render holds tiny, generic formatting primitives shared by the
// plain-text report renderers (/cost, /context, /compact, /clean, and friends).
// It carries only format helpers — currency, token counts, counts, timestamps,
// and tabwriter setup; feature-specific report logic stays in each feature
// package. It depends on nothing outside the standard library.
package render

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// Tokens formats a token count compactly: 1234 -> "1.2k tok", 12000 -> "12k
// tok", 2_500_000 -> "2.5M tok". The reports use it so a column of token counts
// stays narrow and scannable.
func Tokens(n int) string {
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

// Count formats a pluralized count: Count(1, "segment") -> "1 segment",
// Count(3, "segment") -> "3 segments".
func Count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// Commas formats n with thousands separators: 12000 -> "12,000", -1234 ->
// "-1,234".
func Commas(n int) string {
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

// Money formats an amount with a currency prefix and four decimals, e.g.
// Money("$", 1.5) -> "$1.5000". The prefix is whatever the caller's currency
// resolves to ("$", "EUR ", …); the empty-state and unpriced dashes stay with
// each feature.
func Money(symbol string, v float64) string {
	return symbol + strconv.FormatFloat(v, 'f', 4, 64)
}

// Timestamp formats t as the minute-resolution stamp the reports use.
func Timestamp(t time.Time) string { return t.Format("2006-01-02 15:04") }

// Tab returns a tabwriter over w and a row helper that discards the write
// result — writing to a strings.Builder through tabwriter never errors, so
// checking each Fprintf would only add noise. Flush the writer when the table is
// complete. flags is the tabwriter flag set (e.g. tabwriter.AlignRight, or 0).
func Tab(w io.Writer, flags uint) (*tabwriter.Writer, func(string, ...any)) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', flags)
	return tw, func(format string, a ...any) { _, _ = fmt.Fprintf(tw, format, a...) }
}
