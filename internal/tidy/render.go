package tidy

import (
	"fmt"
	"strings"

	"github.com/tonitienda/agent-smith/internal/composition"
	"github.com/tonitienda/agent-smith/internal/render"
)

// RenderPreview formats a plan as the fidelity diff the user confirms before
// anything changes (§9 mitigation: tidy must show exactly what it keeps and
// drops, never a lossy summary). It is face-agnostic so the TUI and a headless
// face render the same panel.
func RenderPreview(p Plan) string {
	var b strings.Builder
	if p.Empty() {
		b.WriteString("Nothing to tidy — no file is read more than once in the live window.\n")
		b.WriteString("Run /context to see what's filling the window.\n")
		return strings.TrimRight(b.String(), "\n")
	}

	fmt.Fprintf(&b, "Preview: deduping %s would reclaim %s%s\n",
		render.Count(len(p.Groups), "file"), render.Tokens(p.Tokens), costSuffix(p))

	// The fidelity diff: a before/after inventory so the reclaim is auditable and
	// can never read as a lossy compaction.
	fmt.Fprintf(&b, "  window: %s in %s → %s in %s\n",
		render.Tokens(p.BeforeTokens), render.Count(p.BeforeSegments, "segment"),
		render.Tokens(p.AfterTokens), render.Count(p.AfterSegments, "segment"))

	tw, row := render.Tab(&b, 0)
	row("  File\tKept\tDropped\tReclaim\t\n")
	for _, g := range p.Groups {
		row("  %s\t%s\t%s\t%s\t\n",
			g.Path, composition.Handle(g.Keep.ID),
			handles(g.Drop), render.Tokens(g.Tokens))
	}
	_ = tw.Flush()

	for _, w := range p.Warnings {
		fmt.Fprintf(&b, "\n⚠  %s\n", w)
	}

	b.WriteString("\nThe latest read of each file is kept, so no live fact is lost.\n")
	b.WriteString("Nothing has changed yet. /tidy --apply to confirm · /tidy --cancel to discard.\n")
	return strings.TrimRight(b.String(), "\n")
}

// handles renders the dropped reads' handles as a compact comma list for the
// "Dropped" column.
func handles(items []Item) string {
	hs := make([]string, len(items))
	for i, it := range items {
		hs[i] = composition.Handle(it.ID)
	}
	return strings.Join(hs, ", ")
}

// costSuffix appends the dollar amount reclaimed when the model is priced.
func costSuffix(p Plan) string {
	if !p.Priced {
		return ""
	}
	return " (" + render.Money(p.Currency, p.CostUSD) + ")"
}
