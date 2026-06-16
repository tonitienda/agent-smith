package clean

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
)

// RenderPreview formats a plan as the plain-text preview the user confirms
// before anything changes (AC: show tokens + $ reclaimed up front). It is
// face-agnostic so the TUI and a headless face render the same panel.
func RenderPreview(p Plan) string {
	var b strings.Builder
	if p.Empty() {
		b.WriteString("Nothing to clean — no live segment matched your selection.\n")
		renderUnknown(&b, p)
		return strings.TrimRight(b.String(), "\n")
	}

	fmt.Fprintf(&b, "Preview: removing %s would reclaim %s%s\n",
		countLabel(len(p.Items), "segment"), tokensLabel(p.Tokens), costSuffix(p))

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	row := func(format string, a ...any) { _, _ = fmt.Fprintf(tw, format, a...) }
	row("  Handle\tType\tOrigin\tTokens\tNote\t\n")
	for _, it := range p.Items {
		note := ""
		if it.Paired {
			note = "paired (tool call/result)"
		}
		row("  %s\t%s\t%s\t%s\t%s\t\n",
			handle(it.ID), it.Kind, it.Origin, tokensLabel(it.Tokens), note)
	}
	_ = tw.Flush()

	for _, w := range p.Warnings {
		fmt.Fprintf(&b, "\n⚠  %s\n", w)
	}
	renderUnknown(&b, p)

	b.WriteString("\nNothing has changed yet. /clean --apply to confirm · /clean --cancel to discard.\n")
	return strings.TrimRight(b.String(), "\n")
}

func renderUnknown(b *strings.Builder, p Plan) {
	if len(p.Unknown) == 0 {
		return
	}
	fmt.Fprintf(b, "\nNo live segment matched: %s\n", strings.Join(p.Unknown, ", "))
	b.WriteString("Run /context to see the current segments and their handles.\n")
}

// costSuffix appends the dollar amount reclaimed when the model is priced.
func costSuffix(p Plan) string {
	if !p.Priced {
		return ""
	}
	return " (" + p.Currency + strconv.FormatFloat(p.CostUSD, 'f', 4, 64) + ")"
}

// handle shortens a block ID to the compact form shown in /context, so the
// preview and the composition view speak the same handles.
func handle(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
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
