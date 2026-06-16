package composition

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// Render formats a Composition as the plain-text /context panel: the window
// total, the top consumers first (the PRD AC — top 3 identifiable in under 5s),
// the per-type breakdown, the duplicate-read and stale-candidate highlights,
// and the full segment list in the requested order. It is face-agnostic so the
// TUI panel (AS-026) and a headless face (AS-051) render the same view, with no
// markup the viewport would have to strip.
func Render(c Composition) string {
	// Empty window: still surface excluded blocks (e.g. everything was /clean'd
	// out) rather than claiming "empty" with no hint that blocks were dropped.
	if len(c.Segments) == 0 {
		if len(c.Excluded) == 0 {
			return "Context is empty — no segments occupy the window yet."
		}
		var b strings.Builder
		b.WriteString("Context window has no live segments.\n")
		renderExcluded(&b, c)
		return strings.TrimRight(b.String(), "\n")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Context composition — %s across %s\n",
		tokensLabel(c.TotalTokens), countLabel(len(c.Segments), "segment"))
	fmt.Fprintf(&b, "Window: %s · total %s\n", windowLabel(c.TotalTokens, c.Window), c.cost(c.TotalCostUSD, c.Priced))

	renderTopConsumers(&b, c)
	renderByGroup(&b, c)
	renderDuplicates(&b, c)
	renderStale(&b, c)
	renderAll(&b, c)
	renderExcluded(&b, c)

	if !c.Priced {
		b.WriteString("\nNote: the active model has no pricing entry, so dollar figures are blank.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderTopConsumers(b *strings.Builder, c Composition) {
	b.WriteString("\nTop consumers\n")
	tw, row := newTab(b)
	for i, s := range c.TopConsumers {
		share := percent(s.Tokens, c.TotalTokens)
		row("  %d.\t%s\t%s\t%s\t%s\t%s ago\t\n",
			i+1, s.Group, tokensLabel(s.Tokens), share, c.cost(s.CostUSD, s.Priced), ageLabel(s.Age))
		// Origin under the Group column: a long path/tool name there won't stretch
		// the narrow numeric columns (tokens/share/cost) out of alignment.
		row("  \t%s\t\t\t\t\t\n", s.Origin)
	}
	_ = tw.Flush()
}

func renderByGroup(b *strings.Builder, c Composition) {
	b.WriteString("\nBy type\n")
	tw, row := newTab(b)
	for _, g := range c.ByGroup {
		row("  %s\t%s\t%s\t%s\t\n",
			g.Group, tokensLabel(g.Tokens), percent(g.Tokens, c.TotalTokens), countLabel(g.Count, "segment"))
	}
	_ = tw.Flush()
}

func renderDuplicates(b *strings.Builder, c Composition) {
	if len(c.Duplicates) == 0 {
		return
	}
	b.WriteString("\nDuplicate reads (same file read more than once)\n")
	tw, row := newTab(b)
	for _, d := range c.Duplicates {
		row("  %s\t×%d\t%s combined\t%s\t\n",
			d.Path, d.Count, tokensLabel(d.Tokens), c.cost(d.CostUSD, d.Priced))
	}
	_ = tw.Flush()
}

func renderStale(b *strings.Builder, c Composition) {
	if len(c.Stale) == 0 {
		return
	}
	b.WriteString("\nStale candidates (large and untouched a while)\n")
	tw, row := newTab(b)
	for _, s := range c.Stale {
		row("  %s\t%s\t%s ago\t\n", s.Origin, tokensLabel(s.Tokens), ageLabel(s.Age))
	}
	_ = tw.Flush()
}

// renderExcluded lists the blocks that have been dropped from the window (the
// live/excluded dimension). They are not counted in the window total; the
// section shows what was removed and why, the restore candidates a later /clean
// undo (AS-028) acts on.
func renderExcluded(b *strings.Builder, c Composition) {
	if len(c.Excluded) == 0 {
		return
	}
	fmt.Fprintf(b, "\nExcluded from the window (%s, not counted in the total)\n",
		countLabel(len(c.Excluded), "segment"))
	tw, row := newTab(b)
	for _, s := range c.Excluded {
		reason := s.Reason
		if reason == "" {
			reason = "excluded"
		}
		row("  %s\t%s\t%s\t%s\t%s\t\n", handle(s.ID), s.Group, s.Origin, tokensLabel(s.Tokens), reason)
	}
	_ = tw.Flush()
	b.WriteString("  Restore the most recent /clean removal: /clean --undo\n")
}

func renderAll(b *strings.Builder, c Composition) {
	fmt.Fprintf(b, "\nAll segments (%s)\n", sortLabel(c.Sort))
	tw, row := newTab(b)
	// Handle is the block ID prefix /clean (AS-028) selects by.
	row("  #\tHandle\tType\tOrigin\tTokens\tShare\tCost\tAge\t\n")
	for i, s := range c.Segments {
		row("  %d\t%s\t%s\t%s\t%s\t%s\t%s\t%s ago\t\n",
			i+1, handle(s.ID), s.Group, s.Origin, tokensLabel(s.Tokens), percent(s.Tokens, c.TotalTokens),
			c.cost(s.CostUSD, s.Priced), ageLabel(s.Age))
	}
	_ = tw.Flush()
	b.WriteString("  Use the handle to remove a segment: /clean <handle>\n")
}

// handle shortens a block ID to the compact selection handle shown in the view;
// /clean resolves any unambiguous prefix back to the full block.
func handle(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// cost formats a dollar amount with the composition's currency prefix, or a
// blank dash when the active model is unpriced (mirrors /cost's unknown mark).
func (c Composition) cost(v float64, priced bool) string {
	if !priced {
		return unknownMark
	}
	return c.Currency + strconv.FormatFloat(v, 'f', 4, 64)
}

const unknownMark = "—"

// newTab returns a column writer over b and a row helper that discards the
// write result — writing to a strings.Builder through tabwriter never errors, so
// checking each Fprintf would only add noise (mirrors internal/cost/render.go).
func newTab(b *strings.Builder) (*tabwriter.Writer, func(string, ...any)) {
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	return tw, func(format string, a ...any) { _, _ = fmt.Fprintf(tw, format, a...) }
}

// tokensLabel formats a token count compactly: 1234 -> "1.2k", 12000 -> "12k".
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

// windowLabel shows the window occupancy as used/window with a percentage when
// the model's window size is known, else just the used token count.
func windowLabel(used, window int) string {
	if window <= 0 {
		return tokensLabel(used)
	}
	pct := int(float64(used)/float64(window)*100 + 0.5)
	return fmt.Sprintf("%s / %s (%d%%)", tokensLabel(used), tokensLabel(window), pct)
}

// percent formats part/whole as a rounded percentage, e.g. "42%".
func percent(part, whole int) string {
	if whole <= 0 {
		return "0%"
	}
	return strconv.Itoa(int(float64(part)/float64(whole)*100+0.5)) + "%"
}

// ageLabel formats a duration as a coarse human age: "12s", "3m", "2h", "1d".
func ageLabel(d time.Duration) string {
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
}

func countLabel(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

func sortLabel(s Sort) string {
	switch s {
	case SortAge:
		return "oldest first"
	case SortType:
		return "grouped by type"
	default:
		return "largest first"
	}
}
