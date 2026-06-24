package stats_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/skillrollup"
	"github.com/tonitienda/agent-smith/internal/stats"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// corpus builds two priced sessions across two days and two models so the
// per-model, per-day, and savings aggregations all have something to fold.
func corpus() []stats.Session {
	return []stats.Session{
		{
			ID:        "sess_a",
			Project:   "/proj/one",
			UpdatedAt: day("2026-06-20"),
			Cost: cost.Summary{
				TotalUSD:  3.0,
				AllPriced: true,
				Currency:  "USD",
				Turns: []cost.TurnCost{
					{Model: "opus", Priced: true, TotalUSD: 2.5, Tokens: cost.Tokens{Input: 1000, Output: 500}},
					{Model: "haiku", Priced: true, TotalUSD: 0.5, Tokens: cost.Tokens{Input: 200, Output: 100}},
				},
			},
		},
		{
			ID:        "sess_b",
			Project:   "/proj/one",
			UpdatedAt: day("2026-06-21"),
			Cost: cost.Summary{
				TotalUSD:        1.0,
				AllPriced:       true,
				Currency:        "USD",
				CacheReadTokens: 0, // read nothing from cache -> a "missed" caching opportunity
				Turns: []cost.TurnCost{
					{Model: "haiku", Priced: true, TotalUSD: 0.5, Tokens: cost.Tokens{Input: 100, Output: 50}},
					{Model: "haiku", Priced: true, TotalUSD: 0.5, Tokens: cost.Tokens{Input: 100, Output: 50}},
				},
			},
		},
	}
}

func TestBuildAggregates(t *testing.T) {
	r := stats.Build(corpus(), skillrollup.Report{}, "this project")

	if r.Sessions != 2 {
		t.Fatalf("Sessions = %d, want 2", r.Sessions)
	}
	if r.TotalUSD != 4.0 {
		t.Fatalf("TotalUSD = %v, want 4.0", r.TotalUSD)
	}
	if !r.AllPriced {
		t.Fatalf("AllPriced = false, want true")
	}

	// Models sorted by spend desc: opus ($2.5) before haiku ($1.5 across 3 turns).
	if len(r.Models) != 2 || r.Models[0].Model != "opus" || r.Models[1].Model != "haiku" {
		t.Fatalf("Models = %+v, want opus then haiku", r.Models)
	}
	if r.Models[1].Turns != 3 || r.Models[1].USD != 1.5 {
		t.Fatalf("haiku = %+v, want 3 turns $1.5", r.Models[1])
	}

	// Days are chronological.
	if len(r.Days) != 2 || r.Days[0].Date != "2026-06-20" || r.Days[1].Date != "2026-06-21" {
		t.Fatalf("Days = %+v, want 06-20 then 06-21", r.Days)
	}
}

func TestSavingsAreGroundedAndCapped(t *testing.T) {
	r := stats.Build(corpus(), skillrollup.Report{}, "this project")

	if len(r.Savings) == 0 || len(r.Savings) > 3 {
		t.Fatalf("Savings count = %d, want 1..3", len(r.Savings))
	}
	// The dominant-model recommendation must carry the concrete number it was
	// derived from (anti-generic, §9): opus's $2.5 spend.
	top := r.Savings[0]
	if !strings.Contains(top.Detail, "opus") || !strings.Contains(top.Detail, "2.5000") {
		t.Fatalf("top saving not grounded in opus spend: %q", top.Detail)
	}
	// Savings are sorted by measured lever (USD) descending.
	for i := 1; i < len(r.Savings); i++ {
		if r.Savings[i-1].USD < r.Savings[i].USD {
			t.Fatalf("savings not sorted by USD desc: %+v", r.Savings)
		}
	}
}

func TestFrictionLinksRecurringSessions(t *testing.T) {
	friction := skillrollup.Report{
		Groups: []skillrollup.Group{
			{Summary: "re-read config.go", Sessions: 3, Count: 5, Examples: []string{"sess_a", "sess_b"}},
			{Summary: "one-off blip", Sessions: 1, Count: 1, Examples: []string{"sess_c"}},
			{Summary: "fixed already", Sessions: 4, Count: 9, Resolved: true},
		},
	}
	r := stats.Build(corpus(), friction, "this project")

	if len(r.Friction) != 1 {
		t.Fatalf("Friction = %+v, want only the recurring unresolved item", r.Friction)
	}
	f := r.Friction[0]
	if f.Summary != "re-read config.go" || f.Sessions != 3 {
		t.Fatalf("friction = %+v, want re-read config.go x3", f)
	}
	if len(f.Examples) != 2 {
		t.Fatalf("friction examples = %v, want 2 linked sessions", f.Examples)
	}
}

func TestRenderEmpty(t *testing.T) {
	out := stats.Render(stats.Build(nil, skillrollup.Report{}, "this project"))
	if !strings.Contains(out, "No sessions recorded yet.") {
		t.Fatalf("empty render = %q", out)
	}
}

func TestRenderContents(t *testing.T) {
	friction := skillrollup.Report{Groups: []skillrollup.Group{
		{Summary: "re-read config.go", Sessions: 2, Count: 4, Examples: []string{"sess_a", "sess_b"}},
	}}
	out := stats.Render(stats.Build(corpus(), friction, "this project"))

	for _, want := range []string{
		"Cross-session analytics — this project",
		"By model",
		"opus",
		"Spend trend",
		"2026-06-20",
		"Top ways to save",
		"Recurring friction",
		"re-read config.go",
		"sess_a", // example session linked
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q in:\n%s", want, out)
		}
	}
}
