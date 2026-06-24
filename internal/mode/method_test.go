package mode

import (
	"slices"
	"testing"
)

func TestResolvePhasesNoOverride(t *testing.T) {
	// No memory texts, and texts without a directive, both keep the house default.
	for _, texts := range [][]string{nil, {"# Project notes\nNothing to see here."}} {
		if got := ResolvePhases(texts); !slices.Equal(got, DefaultPhases()) {
			t.Errorf("ResolvePhases(%q) = %v, want default %v", texts, got, DefaultPhases())
		}
	}
}

func TestResolvePhasesReorderAndSkip(t *testing.T) {
	mem := "Intro.\n\n```smith-method\nphases: think, plan, implement, verify\n```\n\nOutro."
	got := ResolvePhases([]string{mem})
	want := []string{"think", "plan", "implement", "verify"}
	if !slices.Equal(got, want) {
		t.Fatalf("ResolvePhases = %v, want %v (reorder + skip analyse/refactor/reflect)", got, want)
	}
}

func TestResolvePhasesExtendAndWhitespaceSeparators(t *testing.T) {
	// Whitespace separators and a project-specific extra phase are accepted.
	got := ResolvePhases([]string{"```smith-method\nphases: think implement verify ship\n```"})
	want := []string{"think", "implement", "verify", "ship"}
	if !slices.Equal(got, want) {
		t.Fatalf("ResolvePhases = %v, want %v", got, want)
	}
}

func TestResolvePhasesTolerantDegrade(t *testing.T) {
	cases := []string{
		"```smith-method\nphases:\n```",          // empty value
		"```smith-method\nstance: be terse\n```", // no phases key
		"phases: think, plan",                    // directive outside any fence
	}
	for _, c := range cases {
		if got := ResolvePhases([]string{c}); !slices.Equal(got, DefaultPhases()) {
			t.Errorf("ResolvePhases(%q) = %v, want default", c, got)
		}
	}
}

func TestResolvePhasesMostSpecificWins(t *testing.T) {
	// Memory texts arrive lowest-precedence first; the last valid override wins.
	low := "```smith-method\nphases: think, plan, implement\n```"
	high := "```smith-method\nphases: plan, build\n```"
	got := ResolvePhases([]string{low, high})
	want := []string{"plan", "build"}
	if !slices.Equal(got, want) {
		t.Fatalf("ResolvePhases = %v, want %v (most specific wins)", got, want)
	}
}

func TestResolvePhasesDedupAndCanonicalCase(t *testing.T) {
	got := ResolvePhases([]string{"```smith-method\nphases: Think, THINK, Plan\n```"})
	want := []string{"think", "plan"}
	if !slices.Equal(got, want) {
		t.Fatalf("ResolvePhases = %v, want %v (lowercased + deduped)", got, want)
	}
}

func TestResolvePhasesIsolatesDefault(t *testing.T) {
	// A returned override must not be the shared default slice, nor mutate it.
	got := ResolvePhases([]string{"```smith-method\nphases: think, ship\n```"})
	got[0] = "MUTATED"
	if DefaultPhases()[0] != "think" {
		t.Fatal("ResolvePhases leaked a mutable reference to the default phases")
	}
}
