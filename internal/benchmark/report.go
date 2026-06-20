package benchmark

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/render"
)

// RegressionThreshold is the fractional increase in cost or context tokens at
// which Compare flags a directional change. V1 detection is report-only: a flag
// is a signal to look, not a CI failure (D5, AS-030: stochastic results are not
// failed on). A deliberate context-bloat regression clears this comfortably.
const RegressionThreshold = 0.15

// Report is one suite execution: the provider/model it ran against and every
// (harness, task) result. It serializes to JSON and renders to Markdown for the
// §6 metrics dashboard.
type Report struct {
	Provider    string      `json:"provider"`
	Model       string      `json:"model"`
	GeneratedAt time.Time   `json:"generated_at"`
	Runs        []RunResult `json:"runs"`
}

// JSON renders the report as indented JSON.
func (r Report) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

// Markdown renders the §6 metrics as a table grouped by harness, with per-harness
// totals (completed tasks, total cost, total tokens). It is the human-facing view
// of a run.
func (r Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Benchmark report — %s / %s\n\n", r.Provider, r.Model)
	fmt.Fprintf(&b, "Generated %s\n\n", render.Timestamp(r.GeneratedAt))

	for _, h := range r.harnesses() {
		fmt.Fprintf(&b, "## Harness: %s\n\n", h)
		fmt.Fprintln(&b, "| Task | Success | Cost | Tokens | TTFT | Median turn | Live ctx | Iters |")
		fmt.Fprintln(&b, "|---|---|---|---|---|---|---|---|")
		var completed, tokens int
		var usd float64
		for _, run := range r.Runs {
			if run.Harness != h {
				continue
			}
			m := run.Metrics
			if m.Success {
				completed++
			}
			tokens += m.Tokens
			usd += m.TotalUSD
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %.0f%% | %d |\n",
				run.Task, yesNo(m.Success), money(m), render.Tokens(m.Tokens),
				dur(m.TTFT), dur(m.MedianTurnLatency), m.LiveContextPct, m.Iterations)
		}
		fmt.Fprintf(&b, "\n**Totals:** %s completed · %s · %s\n\n",
			render.Count(completed, "task"), render.Money("$", usd), render.Tokens(tokens))
	}
	return b.String()
}

// harnesses returns the distinct harness names in first-seen order.
func (r Report) harnesses() []string {
	var out []string
	seen := map[string]bool{}
	for _, run := range r.Runs {
		if !seen[run.Harness] {
			seen[run.Harness] = true
			out = append(out, run.Harness)
		}
	}
	return out
}

// Regression is one flagged directional change between a baseline run and the
// current run of the same (task, harness).
type Regression struct {
	Task    string
	Harness string
	Kind    string // "success", "cost", or "context"
	Detail  string
}

// Compare flags directional regressions of current against baseline: a task that
// stopped succeeding, or whose cost or context tokens grew past RegressionThreshold.
// It is how the suite makes a deliberate context-bloat regression visible without
// failing on stochastic noise — the caller decides what to do with the flags.
func Compare(baseline, current Report) []Regression {
	base := index(baseline)
	var regs []Regression
	for _, run := range current.Runs {
		b, ok := base[key{run.Task, run.Harness}]
		if !ok {
			continue
		}
		cur := run.Metrics
		if b.Success && !cur.Success {
			regs = append(regs, Regression{run.Task, run.Harness, "success",
				"task no longer completes"})
		}
		if grew(b.TotalUSD, cur.TotalUSD) {
			regs = append(regs, Regression{run.Task, run.Harness, "cost",
				fmt.Sprintf("cost %s → %s", render.Money("$", b.TotalUSD), render.Money("$", cur.TotalUSD))})
		}
		if grewInt(b.Tokens, cur.Tokens) {
			regs = append(regs, Regression{run.Task, run.Harness, "context",
				fmt.Sprintf("tokens %s → %s", render.Tokens(b.Tokens), render.Tokens(cur.Tokens))})
		}
	}
	sort.Slice(regs, func(i, j int) bool {
		if regs[i].Task != regs[j].Task {
			return regs[i].Task < regs[j].Task
		}
		return regs[i].Kind < regs[j].Kind
	})
	return regs
}

type key struct{ task, harness string }

func index(r Report) map[key]Metrics {
	m := map[key]Metrics{}
	for _, run := range r.Runs {
		m[key{run.Task, run.Harness}] = run.Metrics
	}
	return m
}

func grew(base, cur float64) bool {
	if base <= 0 {
		return cur > 0
	}
	return (cur-base)/base > RegressionThreshold
}

func grewInt(base, cur int) bool { return grew(float64(base), float64(cur)) }

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func money(m Metrics) string {
	if !m.Priced {
		return "n/a"
	}
	return render.Money("$", m.TotalUSD)
}

func dur(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	return d.Round(time.Millisecond).String()
}
