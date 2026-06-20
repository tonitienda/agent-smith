package skillcontract

import (
	"testing"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

func costPtr(f float64) *float64 { return &f }

// skillBlock is a block attributed to skill, of the given kind.
func skillBlock(skill string, kind schema.Kind) schema.Block {
	return schema.Block{Kind: kind, Attribution: &schema.Attribution{Skill: skill}}
}

// turnEnd is an untagged block that closes a turn (carries a StopReason).
func turnEnd() schema.Block {
	return schema.Block{Kind: schema.KindText, StopReason: "end_turn"}
}

func TestTrackerOpensOnActivationNotSkillLoad(t *testing.T) {
	tr := NewTracker()
	// A skill-load availability marker must not open a span.
	tr.Observe(eventlog.NewSkillLoad("skill-loader", "deploy"))
	if got := len(tr.Open()); got != 0 {
		t.Fatalf("skill_load opened %d spans, want 0", got)
	}
	// The first real attributed block (the skill invocation) opens the span.
	tr.Observe(skillBlock("deploy", schema.KindToolCall))
	if got := len(tr.Open()); got != 1 {
		t.Fatalf("activation opened %d spans, want 1", got)
	}
}

func TestTrackerSignalTeardown(t *testing.T) {
	tr := NewTracker()
	tr.Declare("deploy", Contract{Completion: Completion{Signal: "`make ship` exited 0", IdleTurns: 3}})

	tr.Observe(skillBlock("deploy", schema.KindToolCall))
	// A tool result whose stdout carries the declared signal fires teardown,
	// preferred over the idle heuristic.
	res := skillBlock("deploy", schema.KindToolResult)
	res.ToolResult = &schema.ToolResultBody{Stdout: "running make ship\n`make ship` exited 0\n"}
	tr.Observe(res)

	if got := len(tr.Open()); got != 0 {
		t.Fatalf("span still open after signal, Open() = %d", got)
	}
	closed := tr.Closed()
	if len(closed) != 1 {
		t.Fatalf("Closed() = %d spans, want 1", len(closed))
	}
	if closed[0].TornDownBy != TeardownSignal {
		t.Errorf("TornDownBy = %q, want %q", closed[0].TornDownBy, TeardownSignal)
	}
}

func TestTrackerIdleTeardown(t *testing.T) {
	tr := NewTracker()
	tr.Declare("deploy", Contract{Completion: Completion{IdleTurns: 2}})

	tr.Observe(skillBlock("deploy", schema.KindToolCall)) // activate, used this turn
	tr.Observe(turnEnd())                                 // turn 1: used -> idle resets to 0
	tr.Observe(turnEnd())                                 // turn 2: idle 1
	if got := len(tr.Open()); got != 1 {
		t.Fatalf("span closed too early, Open() = %d", got)
	}
	tr.Observe(turnEnd()) // turn 3: idle 2 >= 2 -> teardown

	closed := tr.Closed()
	if len(closed) != 1 {
		t.Fatalf("Closed() = %d, want 1", len(closed))
	}
	if closed[0].TornDownBy != TeardownIdle {
		t.Errorf("TornDownBy = %q, want %q", closed[0].TornDownBy, TeardownIdle)
	}
	if closed[0].Actuals.Turns != 3 {
		t.Errorf("Turns = %d, want 3", closed[0].Actuals.Turns)
	}
}

func TestTrackerActualsTrace(t *testing.T) {
	tr := NewTracker()
	tr.Declare("deploy", Contract{ExpectedOutcome: ExpectedOutcome{
		EffortBudget: EffortBudget{ToolCalls: 3, Turns: 2, MaxCostUSD: 0.15},
	}})

	tr.Observe(skillBlock("deploy", schema.KindToolCall)) // tool_calls: 1

	untagged := schema.Block{Kind: schema.KindToolCall, CostUSD: costPtr(0.05)}
	tr.Observe(untagged) // innermost(deploy): tool_calls 2, cost 0.05

	tagged := skillBlock("deploy", schema.KindText)
	tagged.CostUSD = costPtr(0.10)
	tr.Observe(tagged) // deploy: cost 0.15 (text, not a tool call)

	tr.Observe(turnEnd()) // turns: 1

	spans := tr.Finish()
	if len(spans) != 1 {
		t.Fatalf("Finish() = %d spans, want 1", len(spans))
	}
	got := spans[0].Actuals
	if got.ToolCalls != 2 {
		t.Errorf("ToolCalls = %d, want 2", got.ToolCalls)
	}
	if got.Turns != 1 {
		t.Errorf("Turns = %d, want 1", got.Turns)
	}
	if got.CostUSD < 0.149 || got.CostUSD > 0.151 {
		t.Errorf("CostUSD = %v, want ~0.15", got.CostUSD)
	}
	if spans[0].TornDownBy != TeardownSessionEnd {
		t.Errorf("TornDownBy = %q, want %q", spans[0].TornDownBy, TeardownSessionEnd)
	}
}

func TestTrackerOverlapAttribution(t *testing.T) {
	tr := NewTracker()
	tr.Observe(skillBlock("outer", schema.KindToolCall)) // outer tool_calls 1
	tr.Observe(skillBlock("inner", schema.KindToolCall)) // inner tool_calls 1 (innermost)

	tr.Observe(schema.Block{Kind: schema.KindToolCall})  // untagged -> innermost (inner) -> 2
	tr.Observe(skillBlock("outer", schema.KindToolCall)) // tagged outer -> outer 2
	tr.Observe(turnEnd())                                // turn -> innermost (inner) Turns 1

	spans := tr.Finish()
	byName := map[string]Span{}
	for _, s := range spans {
		byName[s.Skill] = s
	}
	if o := byName["outer"].Actuals; o.ToolCalls != 2 || o.Turns != 0 {
		t.Errorf("outer actuals = %+v, want {ToolCalls:2 Turns:0}", o)
	}
	if i := byName["inner"].Actuals; i.ToolCalls != 2 || i.Turns != 1 {
		t.Errorf("inner actuals = %+v, want {ToolCalls:2 Turns:1}", i)
	}
}

func TestTrackerReactivationOpensNewSpan(t *testing.T) {
	tr := NewTracker()
	tr.Declare("deploy", Contract{Completion: Completion{IdleTurns: 1}})

	tr.Observe(skillBlock("deploy", schema.KindToolCall))
	tr.Observe(turnEnd()) // used -> idle 0
	tr.Observe(turnEnd()) // idle 1 -> close idle
	if got := len(tr.Closed()); got != 1 {
		t.Fatalf("after idle, Closed() = %d, want 1", got)
	}

	// A second activation of the same skill opens a fresh span.
	tr.Observe(skillBlock("deploy", schema.KindToolCall))
	if got := len(tr.Open()); got != 1 {
		t.Fatalf("re-activation Open() = %d, want 1", got)
	}
	tr.Finish()
	if got := len(tr.Closed()); got != 2 {
		t.Fatalf("Closed() = %d after Finish, want 2", got)
	}
}

func TestTrackerUndeclaredSkillTracksWithZeroContract(t *testing.T) {
	tr := NewTracker()
	// No Declare: a skill with no contract still gets a span and accumulates.
	tr.Observe(skillBlock("ad-hoc", schema.KindToolCall))
	spans := tr.Finish()
	if len(spans) != 1 || spans[0].Skill != "ad-hoc" {
		t.Fatalf("spans = %+v, want one ad-hoc span", spans)
	}
	if spans[0].Contract.Declared {
		t.Errorf("undeclared skill Contract.Declared = true, want false")
	}
	if spans[0].Actuals.ToolCalls != 1 {
		t.Errorf("ToolCalls = %d, want 1", spans[0].Actuals.ToolCalls)
	}
}
