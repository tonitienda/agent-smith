package skillcontract

import (
	"reflect"
	"testing"
)

// the Appendix C.1 example contract, verbatim (frontmatter body only).
const c1Example = `name: deploy-service
description: Ship a service to production at Acme (triggers on "deploy", "ship", "release").
expected_outcome:
  summary: Deploy a service to prod in one pass without rediscovering our pipeline.
  effort_budget:
    tool_calls: 3            # soft target for the in-scope span
    turns: 2
    max_cost_usd: 0.15       # optional soft ceiling
  should_not_rediscover:     # facts the skill already encodes; rediscovery ⇒ content gap
    - deploy command (make ship)
    - staging approval gate
    - rollback procedure
  success_signals:           # optional; how we know it worked
    - "` + "`make ship`" + ` exited 0"
    - no user correction about the deploy steps
completion:                  # when does this skill's span end? (drives analyzer teardown)
  signal: "` + "`make ship`" + ` exited 0"   # (c) declared — preferred
  idle_turns: 3                    # (b) heuristic fallback if no signal is declared/fires`

func TestParseContractC1Example(t *testing.T) {
	c := ParseContract(c1Example)
	if !c.Declared {
		t.Fatal("Declared = false, want true for a contract-bearing skill")
	}
	want := Contract{
		Declared: true,
		ExpectedOutcome: ExpectedOutcome{
			Summary:      "Deploy a service to prod in one pass without rediscovering our pipeline.",
			EffortBudget: EffortBudget{ToolCalls: 3, Turns: 2, MaxCostUSD: 0.15},
			ShouldNotRediscover: []string{
				"deploy command (make ship)",
				"staging approval gate",
				"rollback procedure",
			},
			SuccessSignals: []string{
				"`make ship` exited 0",
				"no user correction about the deploy steps",
			},
		},
		Completion: Completion{Signal: "`make ship` exited 0", IdleTurns: 3},
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("contract mismatch:\n got %+v\nwant %+v", c, want)
	}
}

func TestParseContractNoContractFields(t *testing.T) {
	// A skill with only name/description carries no contract — it must parse
	// cleanly to a zero Contract rather than complaining (AS-049 infers later).
	c := ParseContract("name: deep-research\ndescription: Research a topic")
	if c.Declared {
		t.Errorf("Declared = true, want false for a skill with no contract fields")
	}
	if !reflect.DeepEqual(c, Contract{}) {
		t.Errorf("contract = %+v, want zero value", c)
	}
}

func TestParseContractEmpty(t *testing.T) {
	if c := ParseContract(""); c.Declared {
		t.Errorf("empty frontmatter Declared = true, want false")
	}
}

func TestParseContractPartial(t *testing.T) {
	// Only a completion section, no expected_outcome — every field is optional.
	c := ParseContract("completion:\n  idle_turns: 5")
	if !c.Declared {
		t.Fatal("Declared = false, want true")
	}
	if c.Completion.IdleTurns != 5 || c.Completion.Signal != "" {
		t.Errorf("completion = %+v, want {Signal: \"\", IdleTurns: 5}", c.Completion)
	}
	if !reflect.DeepEqual(c.ExpectedOutcome, ExpectedOutcome{}) {
		t.Errorf("expected_outcome = %+v, want zero", c.ExpectedOutcome)
	}
}

func TestParseContractMalformedNumbersDegrade(t *testing.T) {
	// A garbage budget value degrades to zero rather than failing the load.
	c := ParseContract("expected_outcome:\n  effort_budget:\n    tool_calls: lots\n    max_cost_usd: free")
	if c.ExpectedOutcome.EffortBudget.ToolCalls != 0 || c.ExpectedOutcome.EffortBudget.MaxCostUSD != 0 {
		t.Errorf("budget = %+v, want zeros for unparsable numbers", c.ExpectedOutcome.EffortBudget)
	}
	if !c.Declared {
		t.Error("Declared = false, want true (the section was present)")
	}
}

func TestParseContractSingleQuoted(t *testing.T) {
	// Single quotes are valid YAML and avoid escaping backticks; the signal must
	// parse to its contents verbatim so it matches later.
	c := ParseContract("completion:\n  signal: '`make ship` exited 0'")
	if c.Completion.Signal != "`make ship` exited 0" {
		t.Errorf("signal = %q, want backtick-bearing contents", c.Completion.Signal)
	}
}

func TestParseContractCRLF(t *testing.T) {
	c := ParseContract("completion:\r\n  signal: done\r\n  idle_turns: 2")
	if c.Completion.Signal != "done" || c.Completion.IdleTurns != 2 {
		t.Errorf("completion = %+v, want CRLF normalized", c.Completion)
	}
}

// FuzzParseContract asserts the contract parser never panics on arbitrary
// frontmatter — it is an adversarial parser over a persisted, user-authored
// format (testing strategy: fuzz parsers of persisted formats).
func FuzzParseContract(f *testing.F) {
	f.Add(c1Example)
	f.Add("")
	f.Add("expected_outcome:\n  effort_budget:\n    tool_calls: 9")
	f.Add("completion:\n  signal: \"unterminated")
	f.Add(":::\n- \n  -  - nested")
	f.Fuzz(func(t *testing.T, s string) {
		_ = ParseContract(s)
	})
}
