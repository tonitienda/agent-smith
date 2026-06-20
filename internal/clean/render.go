package clean

import (
	"fmt"
	"strings"

	"github.com/tonitienda/agent-smith/internal/composition"
	"github.com/tonitienda/agent-smith/internal/render"
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
		render.Count(len(p.Items), "segment"), render.Tokens(p.Tokens), costSuffix(p))

	tw, row := render.Tab(&b, 0)
	row("  Handle\tType\tOrigin\tTokens\tNote\t\n")
	for _, it := range p.Items {
		note := ""
		if it.Paired {
			note = "paired (tool call/result)"
		}
		row("  %s\t%s\t%s\t%s\t%s\t\n",
			composition.Handle(it.ID), it.Kind, it.Origin, render.Tokens(it.Tokens), note)
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
	return " (" + render.Money(p.Currency, p.CostUSD) + ")"
}
