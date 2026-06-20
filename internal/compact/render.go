package compact

import (
	"fmt"
	"strings"

	"github.com/tonitienda/agent-smith/internal/composition"
	"github.com/tonitienda/agent-smith/internal/render"
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
		render.Count(len(p.SourceIDs), "block"), render.Tokens(p.Tokens), costSuffix(p))

	tw, row := render.Tab(&b, 0)
	row("  Handle\tType\tTokens\t\n")
	for i, id := range p.SourceIDs {
		row("  %s\t%s\t%s\t\n", composition.Handle(id), p.Sources[i].Kind, render.Tokens(p.SourceTokens[i]))
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
	return " (" + render.Money(p.Currency, p.CostUSD) + ")"
}
