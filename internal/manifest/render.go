package manifest

import (
	"fmt"
	"strings"

	"github.com/tonitienda/agent-smith/internal/render"
)

// Render returns a compact, human-readable summary of the manifest for the
// `smith replay` header and standalone display. It is plain text only (no
// personality — §7.21) so it stays clean for scripted and CI faces.
func Render(m Manifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "session    %s\n", m.SessionID)
	if m.ProjectPath != "" {
		fmt.Fprintf(&b, "project    %s\n", m.ProjectPath)
	}
	fmt.Fprintf(&b, "created    %s\n", render.Timestamp(m.CreatedAt))
	if m.BinaryVersion != "" {
		fmt.Fprintf(&b, "binary     %s\n", m.BinaryVersion)
	}
	if len(m.Models) > 0 {
		fmt.Fprintf(&b, "models     %s\n", strings.Join(m.Models, ", "))
	}
	if len(m.Tools) > 0 {
		fmt.Fprintf(&b, "tools      %s\n", strings.Join(m.Tools, ", "))
	}
	fmt.Fprintf(&b, "turns      %d over %s\n", m.Turns, render.Count(m.EventCount, "event"))
	fmt.Fprintf(&b, "tokens     %s\n", render.Tokens(m.Totals.TotalTokens))
	if m.Totals.Priced {
		fmt.Fprintf(&b, "cost       %s\n", money(m.Totals))
	} else {
		fmt.Fprintf(&b, "cost       %s (lower bound — some turns unpriced)\n", money(m.Totals))
	}
	return strings.TrimRight(b.String(), "\n")
}

// money renders the dollar total with the table's currency, falling back to a
// bare "$" symbol when no currency code was recorded.
func money(t Totals) string {
	symbol := "$"
	if t.Currency != "" && t.Currency != "USD" {
		symbol = t.Currency + " "
	}
	return render.Money(symbol, t.CostUSD)
}
