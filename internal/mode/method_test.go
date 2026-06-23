package mode

import (
	"slices"
	"testing"
)

func TestResolveMethodNoOverrideKeepsDefault(t *testing.T) {
	got := ResolveMethod(DefaultPhases(), []string{"# A project\n\nJust prose, no method block.\n"})
	if !slices.Equal(got.Phases, DefaultPhases()) {
		t.Fatalf("phases = %v, want default %v", got.Phases, DefaultPhases())
	}
	if len(got.Rules) != 0 {
		t.Fatalf("rules = %v, want none", got.Rules)
	}
}

func TestResolveMethodReordersAndAddsRule(t *testing.T) {
	memo := "intro\n```smith-method\nphases: think, plan, implement, verify\nrule: require a ticket before any code\n```\noutro\n"
	got := ResolveMethod(DefaultPhases(), []string{memo})
	want := []string{"think", "plan", "implement", "verify"}
	if !slices.Equal(got.Phases, want) {
		t.Fatalf("phases = %v, want %v", got.Phases, want)
	}
	if !slices.Equal(got.Rules, []string{"require a ticket before any code"}) {
		t.Fatalf("rules = %v", got.Rules)
	}
}

func TestResolveMethodSkipsAPhase(t *testing.T) {
	memo := "```smith-method\nskip: refactor\n```\n"
	got := ResolveMethod(DefaultPhases(), []string{memo})
	if PhaseIndex(got.Phases, "refactor") >= 0 {
		t.Fatalf("refactor not skipped: %v", got.Phases)
	}
	// The other phases survive in order.
	if PhaseIndex(got.Phases, "implement") < 0 || PhaseIndex(got.Phases, "reflect") < 0 {
		t.Fatalf("unexpected phases after skip: %v", got.Phases)
	}
}

func TestResolveMethodMostSpecificWins(t *testing.T) {
	// Lowest precedence first: a broad override then a more specific one.
	broad := "```smith-method\nphases: think, plan, implement\n```\n"
	specific := "```smith-method\nphases: analyse, implement, verify\nrule: specific rule\n```\n"
	got := ResolveMethod(DefaultPhases(), []string{broad, specific})
	want := []string{"analyse", "implement", "verify"}
	if !slices.Equal(got.Phases, want) {
		t.Fatalf("phases = %v, want %v", got.Phases, want)
	}
}

func TestResolveMethodTolerantOfMalformed(t *testing.T) {
	// A block with no recognised directive degrades to the default, not an error.
	memo := "```smith-method\nphazes: oops typo\n: no key\nphases:\n```\n"
	got := ResolveMethod(DefaultPhases(), []string{memo})
	if !slices.Equal(got.Phases, DefaultPhases()) {
		t.Fatalf("phases = %v, want default %v", got.Phases, DefaultPhases())
	}
}

func TestParseOverrideAccumulatesSkipAndRules(t *testing.T) {
	memo := "```smith-method\nskip: refactor\nskip: analyse\nrule: first\nrules: second\n```\n"
	o := ParseOverride(memo)
	if !slices.Equal(o.Skip, []string{"refactor", "analyse"}) {
		t.Fatalf("skip = %v", o.Skip)
	}
	if !slices.Equal(o.Rules, []string{"first", "second"}) {
		t.Fatalf("rules = %v", o.Rules)
	}
}

func TestParseOverrideTildeFenceAndCaseInsensitiveTag(t *testing.T) {
	memo := "```SMITH-METHOD\nphases: Think, Plan\n```\n"
	o := ParseOverride(memo)
	if !slices.Equal(o.Phases, []string{"think", "plan"}) {
		t.Fatalf("phases = %v, want lowercased think,plan", o.Phases)
	}
}
