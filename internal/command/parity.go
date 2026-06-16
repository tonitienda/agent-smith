package command

import (
	"fmt"
	"strings"
)

// ParityTable renders the slash ↔ subcommand parity matrix (UX.md §17.5) from a
// registry, so the documented matrix is generated rather than hand-maintained
// and can't drift from the descriptors (AS-066). Each row states a command's
// scriptability and a note: the required reason for an interactive-only command,
// or its output schema when it declares one. Commands are listed in name order.
func ParityTable(r *Registry) string {
	var b strings.Builder
	b.WriteString("| Command | Scriptability | Notes |\n")
	b.WriteString("|---|---|---|\n")
	for _, c := range r.All() {
		notes := c.Reason
		if notes == "" && c.OutputSchema != "" {
			notes = "output: " + c.OutputSchema
		}
		fmt.Fprintf(&b, "| `/%s` | %s | %s |\n", c.Name, c.Scriptability, notes)
	}
	return strings.TrimRight(b.String(), "\n")
}
