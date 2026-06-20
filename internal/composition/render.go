package composition

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/render"
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
		render.Tokens(c.TotalTokens), render.Count(len(c.Segments), "segment"))
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
	tw, row := render.Tab(b, 0)
	for i, s := range c.TopConsumers {
		share := percent(s.Tokens, c.TotalTokens)
		row("  %d.\t%s\t%s\t%s\t%s\t%s ago\t\n",
			i+1, s.Group, render.Tokens(s.Tokens), share, c.cost(s.CostUSD, s.Priced), ageLabel(s.Age))
		// Origin under the Group column: a long path/tool name there won't stretch
		// the narrow numeric columns (tokens/share/cost) out of alignment.
		row("  \t%s\t\t\t\t\t\n", s.Origin)
	}
	_ = tw.Flush()
}

func renderByGroup(b *strings.Builder, c Composition) {
	b.WriteString("\nBy type\n")
	tw, row := render.Tab(b, 0)
	for _, g := range c.ByGroup {
		row("  %s\t%s\t%s\t%s\t\n",
			g.Group, render.Tokens(g.Tokens), percent(g.Tokens, c.TotalTokens), render.Count(g.Count, "segment"))
	}
	_ = tw.Flush()
}

func renderDuplicates(b *strings.Builder, c Composition) {
	if len(c.Duplicates) == 0 {
		return
	}
	b.WriteString("\nDuplicate reads (same file read more than once)\n")
	tw, row := render.Tab(b, 0)
	for _, d := range c.Duplicates {
		row("  %s\t×%d\t%s combined\t%s\t\n",
			d.Path, d.Count, render.Tokens(d.Tokens), c.cost(d.CostUSD, d.Priced))
	}
	_ = tw.Flush()
}

func renderStale(b *strings.Builder, c Composition) {
	if len(c.Stale) == 0 {
		return
	}
	b.WriteString("\nStale candidates (large and untouched a while)\n")
	tw, row := render.Tab(b, 0)
	for _, s := range c.Stale {
		row("  %s\t%s\t%s ago\t\n", s.Origin, render.Tokens(s.Tokens), ageLabel(s.Age))
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
		render.Count(len(c.Excluded), "segment"))
	tw, row := render.Tab(b, 0)
	row("  Handle\tType\tOrigin\tTokens\tReason\t\n")
	for _, s := range c.Excluded {
		reason := s.Reason
		if reason == "" {
			reason = "excluded"
		}
		row("  %s\t%s\t%s\t%s\t%s\t\n", Handle(s.ID), s.Group, s.Origin, render.Tokens(s.Tokens), reason)
	}
	_ = tw.Flush()
	b.WriteString("  Restore the most recent /clean removal: /clean --undo\n")
}

func renderAll(b *strings.Builder, c Composition) {
	fmt.Fprintf(b, "\nAll segments (%s)\n", sortLabel(c.Sort))
	tw, row := render.Tab(b, 0)
	// Handle is the block ID prefix /clean (AS-028) selects by.
	row("  #\tHandle\tType\tOrigin\tTokens\tShare\tCost\tAge\t\n")
	for i, s := range c.Segments {
		row("  %d\t%s\t%s\t%s\t%s\t%s\t%s\t%s ago\t\n",
			i+1, Handle(s.ID), s.Group, s.Origin, render.Tokens(s.Tokens), percent(s.Tokens, c.TotalTokens),
			c.cost(s.CostUSD, s.Priced), ageLabel(s.Age))
	}
	_ = tw.Flush()
	b.WriteString("  Use the handle to remove a segment: /clean <handle>\n")
}

// Handle shortens a block ID to the compact selection handle shown in the view;
// /clean resolves any unambiguous prefix back to the full block. It is exported
// so the /clean preview (internal/clean) renders the same handles as /context —
// the single source of the handle width, so the two views can't drift.
func Handle(id string) string {
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
	return render.Money(c.Currency, v)
}

const unknownMark = "—"

// windowLabel shows the window occupancy as used/window with a percentage when
// the model's window size is known, else just the used token count.
func windowLabel(used, window int) string {
	if window <= 0 {
		return render.Tokens(used)
	}
	pct := int(float64(used)/float64(window)*100 + 0.5)
	return fmt.Sprintf("%s / %s (%d%%)", render.Tokens(used), render.Tokens(window), pct)
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
