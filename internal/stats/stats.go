// Package stats builds the cross-session portfolio analytics surfaced by
// `smith stats` and the `/stats` slash command (AS-057, PRD §7.24).
//
// It is a pure aggregation layer: callers load the session corpus, price each
// session through internal/cost, and hand the priced sessions plus the durable
// findings rollup (AS-050) to Build, which folds them into a Report. Nothing
// here touches disk or the network, so the analytics are fully offline and the
// aggregation is deterministic and unit-testable without fixtures.
//
// The Report itself is pure derived state. A disposable on-disk index of the
// priced per-session rows (internal/statsindex, AS-136) lets callers skip
// re-pricing unchanged sessions, but it is never load-bearing: a missing or stale
// index degrades to pricing the whole corpus, exactly the behaviour here.
package stats

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/render"
	"github.com/tonitienda/agent-smith/internal/skillrollup"
)

// Session is one priced session in the corpus, the input to Build. It is the
// face-agnostic projection of a stored session: identity, the project it
// belongs to, when it was last active, and its cost summary (the priced view of
// its usage events). Callers build it from session.Summary + cost.Summarize.
type Session struct {
	ID        string
	Project   string
	UpdatedAt time.Time
	Cost      cost.Summary
}

// Report is the portfolio view: corpus totals, the spend trend over time, the
// per-project and per-model breakdowns, the top grounded savings
// recommendations, and the recurring friction linked back to example sessions.
type Report struct {
	Scope     string // human label for the aggregation scope
	Sessions  int
	TotalUSD  float64
	AllPriced bool
	Currency  string

	Projects []ProjectSpend // most expensive first
	Models   []ModelSpend   // most expensive first
	Days     []DaySpend     // chronological spend trend
	Savings  []Saving       // top recommendations, highest measured lever first
	Friction []FrictionItem // recurring findings across ≥2 sessions

	// Improvements is the before/after payoff of applied /improve remedies
	// (AS-139): whether the friction each one targeted dropped in later sessions.
	Improvements []Improvement
}

// ProjectSpend is one project's slice of the corpus.
type ProjectSpend struct {
	Project  string
	Sessions int
	USD      float64
}

// ModelSpend is one model's slice of the corpus.
type ModelSpend struct {
	Model  string
	Turns  int
	Tokens int
	USD    float64
}

// DaySpend is the spend on one calendar day (UTC, YYYY-MM-DD).
type DaySpend struct {
	Date     string
	Sessions int
	USD      float64
}

// Saving is one grounded money/time-saving recommendation. USD is the measured
// lever it is sized by (and sorted on), so the advice is never generic (§9): it
// always carries the number it was derived from.
type Saving struct {
	Title  string
	Detail string
	USD    float64
}

// FrictionItem is one recurring finding aggregated across sessions, with a small
// sample of session ids to inspect (Examples).
type FrictionItem struct {
	Summary  string
	Sessions int
	Count    int
	Examples []string
}

// Improvement is one applied /improve remedy's before/after payoff (AS-139): the
// friction it targeted, where the edit landed, and whether that friction stopped
// recurring once the proposal was applied. Worked is the deterministic proxy
// (After == 0): the targeted finding has not recurred since.
type Improvement struct {
	Summary       string
	Target        string
	Before        int
	After         int
	SessionsAfter int
	Worked        bool
}

// maxSavings and maxFriction bound the two ranked lists so the report stays a
// digest, not a dump. maxSavings honours the "top 3 ways to save" framing
// (§7.24); maxFriction keeps the recurring list scannable. maxImprovements keeps
// the applied-remedy payoff list scannable for the same reason.
const (
	maxSavings      = 3
	maxFriction     = 5
	maxImprovements = 5
)

// Build folds the priced corpus and the findings rollup into a Report under the
// given scope label and reference time. friction.Groups supplies the recurring
// items (already aggregated by AS-050); only groups seen in ≥2 distinct sessions
// are surfaced, so a one-off finding is not framed as a pattern.
func Build(sessions []Session, friction skillrollup.Report, scope string) Report {
	r := Report{Scope: scope, Sessions: len(sessions), AllPriced: true, Currency: "USD"}

	projects := map[string]*ProjectSpend{}
	models := map[string]*ModelSpend{}
	days := map[string]*DaySpend{}
	var biggestTurn cost.TurnCost
	var unpriced int

	for _, s := range sessions {
		sum := s.Cost
		r.TotalUSD += sum.TotalUSD
		if !sum.AllPriced {
			r.AllPriced = false
		}
		if sum.Currency != "" {
			r.Currency = sum.Currency
		}

		proj := s.Project
		if proj == "" {
			proj = "(unknown)"
		}
		p := projects[proj]
		if p == nil {
			p = &ProjectSpend{Project: proj}
			projects[proj] = p
		}
		p.Sessions++
		p.USD += sum.TotalUSD

		day := s.UpdatedAt.UTC().Format("2006-01-02")
		d := days[day]
		if d == nil {
			d = &DaySpend{Date: day}
			days[day] = d
		}
		d.Sessions++
		d.USD += sum.TotalUSD

		for _, t := range sum.Turns {
			m := models[t.Model]
			if m == nil {
				m = &ModelSpend{Model: t.Model}
				models[t.Model] = m
			}
			m.Turns++
			m.Tokens += t.Tokens.Input + t.Tokens.Output + t.Tokens.Reasoning
			m.USD += t.TotalUSD
			if !t.Priced {
				unpriced++
			}
			if t.TotalUSD > biggestTurn.TotalUSD {
				biggestTurn = t
			}
		}
	}

	r.Projects = sortedProjects(projects)
	r.Models = sortedModels(models)
	r.Days = sortedDays(days)
	r.Savings = savings(sessions, r, biggestTurn, unpriced)
	r.Friction = frictionItems(friction)
	r.Improvements = improvements(friction)
	return r
}

// improvements projects the applied-remedy efficacy (AS-139) into the report,
// capping the list so it stays a digest. Each item reports whether the targeted
// friction stopped recurring after the proposal was applied (the deterministic
// before/after proxy).
func improvements(rep skillrollup.Report) []Improvement {
	var out []Improvement
	for _, e := range rep.Efficacy {
		out = append(out, Improvement{
			Summary:       e.Summary,
			Target:        e.Target,
			Before:        e.Before,
			After:         e.After,
			SessionsAfter: e.SessionsAfter,
			Worked:        e.After == 0,
		})
		if len(out) == maxImprovements {
			break
		}
	}
	return out
}

func sortedProjects(m map[string]*ProjectSpend) []ProjectSpend {
	out := make([]ProjectSpend, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].USD != out[j].USD {
			return out[i].USD > out[j].USD
		}
		return out[i].Project < out[j].Project
	})
	return out
}

func sortedModels(m map[string]*ModelSpend) []ModelSpend {
	out := make([]ModelSpend, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].USD != out[j].USD {
			return out[i].USD > out[j].USD
		}
		return out[i].Model < out[j].Model
	})
	return out
}

func sortedDays(m map[string]*DaySpend) []DaySpend {
	out := make([]DaySpend, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// savings derives the grounded recommendations and returns the top maxSavings by
// measured lever. Every candidate carries a concrete number so none is generic
// (§9): the dominant model's spend, realized cache savings, the single costliest
// turn, and a data-quality flag when unpriced turns undercount the total.
func savings(sessions []Session, r Report, biggest cost.TurnCost, unpriced int) []Saving {
	var out []Saving

	// 1. Concentration: the dominant model and its share of total spend. Only
	// actionable when more than one model ran (else there is nothing to route to).
	if len(r.Models) > 1 && r.TotalUSD > 0 {
		top := r.Models[0]
		share := 100 * top.USD / r.TotalUSD
		out = append(out, Saving{
			Title:  fmt.Sprintf("Route routine turns off %s", top.Model),
			Detail: fmt.Sprintf("%s drove %s (%.0f%% of spend) over %s; sending routine turns to a cheaper tier is the biggest lever.", top.Model, money(top.USD, r.Currency), share, render.Count(top.Turns, "turn")),
			USD:    top.USD,
		})
	}

	// 2. Caching: realized savings plus sessions that read nothing from cache.
	var realized float64
	var missed int
	for _, s := range sessions {
		realized += s.Cost.CacheSavingsUSD
		if len(s.Cost.Turns) > 1 && s.Cost.CacheReadTokens == 0 {
			missed++
		}
	}
	if realized > 0 || missed > 0 {
		detail := fmt.Sprintf("Prompt caching saved %s so far", money(realized, r.Currency))
		if missed > 0 {
			detail += fmt.Sprintf("; %s read nothing from cache — caching their repeated context could save more", render.Count(missed, "session"))
		}
		out = append(out, Saving{Title: "Lean harder on prompt caching", Detail: detail + ".", USD: realized})
	}

	// 3. The single costliest turn — the sharpest one-shot lever.
	if biggest.TotalUSD > 0 {
		out = append(out, Saving{
			Title:  "Trim the costliest single turn",
			Detail: fmt.Sprintf("One turn on %s cost %s; splitting or trimming that prompt removes it in one move.", biggest.Model, money(biggest.TotalUSD, r.Currency)),
			USD:    biggest.TotalUSD,
		})
	}

	// 4. Data quality: unpriced turns mean the total is a lower bound. Sorted last
	// (no dollar lever) but worth surfacing so the numbers above are trusted.
	if unpriced > 0 {
		out = append(out, Saving{
			Title:  "Price the unpriced models",
			Detail: fmt.Sprintf("%s ran on models with no pricing entry, so spend exceeds the %s shown — add their rates to measure them.", render.Count(unpriced, "turn"), money(r.TotalUSD, r.Currency)),
		})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].USD > out[j].USD })
	if len(out) > maxSavings {
		out = out[:maxSavings]
	}
	return out
}

// frictionItems projects the recurring findings (≥2 distinct sessions) into the
// portfolio's friction list, carrying the example session ids so each item links
// back to concrete sessions.
func frictionItems(rep skillrollup.Report) []FrictionItem {
	var out []FrictionItem
	for _, g := range rep.Groups {
		if g.Resolved || g.Sessions < 2 {
			continue
		}
		out = append(out, FrictionItem{
			Summary:  g.Summary,
			Sessions: g.Sessions,
			Count:    g.Count,
			Examples: g.Examples,
		})
		if len(out) == maxFriction {
			break
		}
	}
	return out
}

func money(v float64, currency string) string {
	return render.Money(cost.Symbol(currency), v)
}

// Render formats a Report as a face-agnostic textual dashboard for the TUI panel
// and the headless `smith stats` verb.
func Render(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Cross-session analytics — %s\n\n", r.Scope)

	if r.Sessions == 0 {
		b.WriteString("No sessions recorded yet.")
		return b.String()
	}

	total := money(r.TotalUSD, r.Currency)
	if !r.AllPriced {
		total += "+ (some turns unpriced)"
	}
	fmt.Fprintf(&b, "%s · %s total spend\n", render.Count(r.Sessions, "session"), total)

	if len(r.Projects) > 1 {
		b.WriteString("\nBy project\n")
		for _, p := range r.Projects {
			fmt.Fprintf(&b, "  %s — %s across %s\n", p.Project, money(p.USD, r.Currency), render.Count(p.Sessions, "session"))
		}
	}

	if len(r.Models) > 0 {
		b.WriteString("\nBy model\n")
		for _, m := range r.Models {
			fmt.Fprintf(&b, "  %s — %s across %s\n", m.Model, money(m.USD, r.Currency), render.Count(m.Turns, "turn"))
		}
	}

	if len(r.Days) > 0 {
		b.WriteString("\nSpend trend\n")
		for _, d := range r.Days {
			fmt.Fprintf(&b, "  %s  %s  (%s)\n", d.Date, money(d.USD, r.Currency), render.Count(d.Sessions, "session"))
		}
	}

	b.WriteString("\nTop ways to save\n")
	if len(r.Savings) == 0 {
		b.WriteString("  Not enough priced data yet.\n")
	}
	for i, s := range r.Savings {
		fmt.Fprintf(&b, "  %d. %s\n     %s\n", i+1, s.Title, s.Detail)
	}

	if len(r.Friction) > 0 {
		b.WriteString("\nRecurring friction\n")
		for _, f := range r.Friction {
			line := fmt.Sprintf("  %s — %s in %s", f.Summary, render.Count(f.Count, "time"), render.Count(f.Sessions, "session"))
			if len(f.Examples) > 0 {
				line += fmt.Sprintf(" (e.g. %s)", strings.Join(f.Examples, ", "))
			}
			b.WriteString(line + "\n")
		}
	}

	if len(r.Improvements) > 0 {
		b.WriteString("\nApplied improvements\n")
		for _, im := range r.Improvements {
			line := fmt.Sprintf("  %s", im.Summary)
			if im.Target != "" {
				line += fmt.Sprintf(" (%s)", im.Target)
			}
			if im.Worked {
				line += " — ✓ no recurrence since applied"
			} else {
				line += fmt.Sprintf(" — still recurring: %s in %s since applied",
					render.Count(im.After, "time"), render.Count(im.SessionsAfter, "session"))
			}
			b.WriteString(line + "\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}
