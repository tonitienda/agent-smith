package insights

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/render"
)

// maxAnchors caps how many block references an evidence string lists, so a
// command run dozens of times cites a few jump points rather than a wall of ids.
const maxAnchors = 4

// Render formats a Report as the plain-text /insights dashboard: the measured
// signals first (cost, costliest turns, repeated work, oversized outputs, error
// loops, context health), then the grounded suggestions numbered for
// `/insights apply <n>`. It is face-agnostic so the TUI panel and a headless
// `smith insights` render the same retrospective, and it renders fully with no
// pricing (the zero-cost mode), marking unknown dollars rather than hiding work.
func Render(r Report) string {
	if r.Turns == 0 && r.LiveBlocks == 0 {
		return "No session activity yet — run a turn first."
	}
	sym := cost.Symbol(r.Currency)

	var b strings.Builder
	b.WriteString("Session insights\n\n")

	total := render.Money(sym, r.TotalUSD)
	if !r.AllPriced {
		total += " (lower bound — unpriced turns)"
	}
	fmt.Fprintf(&b, "%s · %s · %s\n",
		render.Count(r.Turns, "turn"), render.Tokens(r.TotalTokens), total)
	fmt.Fprintf(&b, "Context health: %d live / %d stale blocks\n",
		r.LiveBlocks, r.StaleBlocks)
	if r.Errors > 0 {
		fmt.Fprintf(&b, "Tool errors: %s\n", render.Count(r.Errors, "failure"))
	}

	if len(r.Costliest) > 0 {
		b.WriteString("\nCostliest turns\n")
		tw, row := render.Tab(&b, tabwriter.AlignRight)
		row("  #\tModel\tTokens\tCost\t\n")
		for _, t := range r.Costliest {
			row("  %d\t%s\t%s\t%s\t\n", t.Index, modelLabel(t.Model),
				render.Commas(t.Tokens.Total()), money(t.TotalUSD, t.Priced, sym))
		}
		_ = tw.Flush()
	}

	writeRepeats(&b, "Repeated commands", r.RepeatedCmds)
	writeRepeats(&b, "Re-read files", r.RepeatedReads)

	if len(r.BigOutputs) > 0 {
		b.WriteString("\nLargest tool outputs\n")
		for _, o := range r.BigOutputs {
			fmt.Fprintf(&b, "  %s — ~%s (%s)\n",
				toolLabel(o.Tool), render.Tokens(o.Tokens), anchors([]int{o.Seq}))
		}
	}

	b.WriteString("\nSuggestions\n")
	if len(r.Suggestions) == 0 {
		b.WriteString("  None — this session ran clean.\n")
	}
	for i, s := range r.Suggestions {
		fmt.Fprintf(&b, "  %d. %s\n     evidence: %s\n", i+1, s.Summary, s.Evidence)
		if s.Edit != nil {
			fmt.Fprintf(&b, "     %s: + %s\n     apply: /insights apply %d\n",
				s.Edit.Target, s.Edit.Line, i+1)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// writeRepeats renders a ranked repeated-value section, omitting it when empty.
func writeRepeats(b *strings.Builder, title string, reps []Repeat) {
	if len(reps) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s\n", title)
	for _, r := range reps {
		fmt.Fprintf(b, "  %s — %s (%s)\n", r.Value, times(r.Count), anchors(r.Seqs))
	}
}

// times renders an occurrence count as "4×".
func times(n int) string { return strconv.Itoa(n) + "×" }

// plural renders a counted noun ("3 tokens"), reusing the shared render helper so
// pluralization stays consistent with the rest of the CLI.
func plural(n int, noun string) string { return render.Count(n, noun) }

// anchors renders block sequence numbers as jump-to-transcript references
// ("#12, #18, …"), capped at maxAnchors so the evidence stays readable.
func anchors(seqs []int) string {
	if len(seqs) == 0 {
		return "—"
	}
	shown := seqs
	more := 0
	if len(shown) > maxAnchors {
		more = len(shown) - maxAnchors
		shown = shown[:maxAnchors]
	}
	parts := make([]string, len(shown))
	for i, s := range shown {
		parts[i] = "#" + strconv.Itoa(s)
	}
	out := strings.Join(parts, ", ")
	if more > 0 {
		out += fmt.Sprintf(", +%d more", more)
	}
	return out
}

// modelLabel shows a turn's model, or a dash when none was recorded.
func modelLabel(m string) string {
	if m == "" {
		return "—"
	}
	return m
}

// toolLabel shows a tool name, or a neutral label when the result was unpaired.
func toolLabel(t string) string {
	if t == "" {
		return "a tool"
	}
	return t
}

// money formats a turn's cost, or a dash when the turn is unpriced.
func money(v float64, priced bool, sym string) string {
	if !priced {
		return "—"
	}
	return render.Money(sym, v)
}
