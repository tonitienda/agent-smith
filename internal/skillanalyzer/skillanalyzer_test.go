package skillanalyzer

import (
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/subagent"
	"github.com/tonitienda/agent-smith/schema"
)

// skillCall is a tool-call block attributed to a skill (it does measurable work,
// so the activation is not a no_op).
func skillCall(skill string, seq int) schema.Block {
	return schema.Block{Kind: schema.KindToolCall, Seq: seq, Attribution: &schema.Attribution{Skill: skill}}
}

// skillResult is a tool-result block attributed to a skill, carrying stdout the
// rediscovered-fact scan reads.
func skillResult(skill, stdout string, seq int) schema.Block {
	return schema.Block{
		Kind:        schema.KindToolResult,
		Seq:         seq,
		Attribution: &schema.Attribution{Skill: skill},
		ToolResult:  &schema.ToolResultBody{Stdout: stdout},
	}
}

// userText is an unattributed text block — session content the should_have_loaded
// match scans.
func userText(s string, seq int) schema.Block {
	return schema.Block{Kind: schema.KindText, Seq: seq, Text: &schema.TextBody{Text: s}}
}

// gradeFor finds the grade for a skill, or fails.
func gradeFor(t *testing.T, grades []Grade, skill string) Grade {
	t.Helper()
	for _, g := range grades {
		if g.Skill == skill {
			return g
		}
	}
	t.Fatalf("no grade for skill %q in %+v", skill, grades)
	return Grade{}
}

// AC1: for a session where a loaded skill underperformed, the analyzer produces a
// specific, grounded, applicable suggestion and a concrete skill diff.
func TestUnderperformedContentGapProducesDiff(t *testing.T) {
	frontmatter := "expected_outcome:\n" +
		"  should_not_rediscover:\n" +
		"    - the deploy command is make ship\n"
	a := New([]Skill{{Name: "deploy", Source: "skills/deploy/SKILL.md", Frontmatter: frontmatter}})

	slice := []schema.Block{
		skillCall("deploy", 1),
		skillResult("deploy", "tried several things, ran make ship to deploy", 2),
	}
	grades := a.Evaluate(slice, "sess-1")

	g := gradeFor(t, grades, "deploy")
	if g.Verdict != Underperformed {
		t.Fatalf("verdict = %q, want %q", g.Verdict, Underperformed)
	}
	if g.Classification != ContentGap {
		t.Errorf("classification = %q, want %q", g.Classification, ContentGap)
	}
	if g.Remedy != PatchSkill {
		t.Errorf("remedy = %q, want %q", g.Remedy, PatchSkill)
	}
	if len(g.Evidence.Rediscovered) == 0 {
		t.Error("expected rediscovered evidence")
	}
	if g.Diff == "" {
		t.Error("expected a concrete skill diff")
	}

	// The finding carries the diff as a propose-only edit targeting the SKILL.md.
	res := a.Teardown(subagent.Scope{Session: "sess-1"}, slice)
	var f subagent.Finding
	for _, x := range res.Findings {
		if strings.Contains(x.Summary, "deploy") {
			f = x
		}
	}
	if f.Proposal == nil {
		t.Fatal("underperformed finding has no proposal")
	}
	if f.Proposal.Target != "skills/deploy/SKILL.md" {
		t.Errorf("proposal target = %q, want the SKILL.md path", f.Proposal.Target)
	}
}

// AC2: inferred contracts are recorded at load time and immutable thereafter.
func TestInferredContractRecordedAndImmutable(t *testing.T) {
	a := New([]Skill{
		{Name: "research", Description: "deep multi-source research report"},
		{Name: "deploy", Frontmatter: "expected_outcome:\n  summary: ship it\n"},
	})

	if !a.Inferred("research") {
		t.Error("research declared no contract; want Inferred == true")
	}
	if a.Inferred("deploy") {
		t.Error("deploy declared a contract; want Inferred == false")
	}

	before := a.Contracts()["research"]
	if before.Declared {
		t.Error("inferred contract must report Declared == false")
	}
	if before.ExpectedOutcome.Summary != "deep multi-source research report" {
		t.Errorf("inferred summary = %q, want the description", before.ExpectedOutcome.Summary)
	}
	if len(before.ExpectedOutcome.ShouldNotRediscover) == 0 {
		t.Error("inferred contract should seed should_not_rediscover from the description")
	}

	// Running an evaluation must not mutate the frozen contract (fixed at load
	// time, not hindsight).
	a.Evaluate([]schema.Block{skillCall("research", 1)}, "sess-2")
	after := a.Contracts()["research"]
	if before.ExpectedOutcome.Summary != after.ExpectedOutcome.Summary ||
		len(before.ExpectedOutcome.ShouldNotRediscover) != len(after.ExpectedOutcome.ShouldNotRediscover) {
		t.Error("contract changed after evaluation; must be immutable")
	}
}

// AC3: findings conform to the C.2 schema and carry working jump-to links.
func TestFindingCarriesC2FieldsAndJumpLink(t *testing.T) {
	a := New([]Skill{{Name: "deploy"}})
	slice := []schema.Block{skillCall("deploy", 7)}

	res := a.Teardown(subagent.Scope{Session: "sess-3"}, slice)
	if len(res.Findings) == 0 {
		t.Fatal("expected at least one finding")
	}
	f := res.Findings[0]
	if f.Kind != FindingKind {
		t.Errorf("finding kind = %q, want %q", f.Kind, FindingKind)
	}
	if !strings.Contains(f.Detail, "verdict ") {
		t.Errorf("detail missing verdict: %q", f.Detail)
	}
	if !strings.Contains(f.Detail, "eval_seed: session://sess-3") {
		t.Errorf("detail missing eval_seed jump link: %q", f.Detail)
	}
	if !strings.Contains(f.Detail, "#7") {
		t.Errorf("detail missing block anchor #7: %q", f.Detail)
	}
}

// AC4: should_have_loaded fires for a session where a matching skill existed but
// never triggered (trigger-failure path).
func TestShouldHaveLoadedTriggerFailure(t *testing.T) {
	a := New([]Skill{
		{Name: "migrate", Description: "run database migrations", Source: "skills/migrate/SKILL.md"},
		{Name: "deploy", Description: "ship the build"},
	})

	// The session works on migrations but never loads the migrate skill, and never
	// touches anything deploy-shaped.
	slice := []schema.Block{userText("we need to migrate the database schema", 4)}
	grades := a.Evaluate(slice, "sess-4")

	g := gradeFor(t, grades, "migrate")
	if g.Verdict != ShouldHaveLoaded {
		t.Fatalf("verdict = %q, want %q", g.Verdict, ShouldHaveLoaded)
	}
	if g.Classification != TriggerFailure {
		t.Errorf("classification = %q, want %q", g.Classification, TriggerFailure)
	}
	if len(g.Evidence.Seqs) == 0 || g.Evidence.Seqs[0] != 4 {
		t.Errorf("expected jump-to anchor at block 4, got %v", g.Evidence.Seqs)
	}
	// The irrelevant, never-loaded deploy skill must not be flagged (precision).
	for _, x := range grades {
		if x.Skill == "deploy" {
			t.Errorf("deploy was never relevant; should not be graded: %+v", x)
		}
	}
}

// A loaded skill that does real work within its contract grades as helped, with no
// remedy diff.
func TestHelpedCleanActivation(t *testing.T) {
	a := New([]Skill{{Name: "deploy"}})
	grades := a.Evaluate([]schema.Block{skillCall("deploy", 1), turnEnd(2)}, "sess-5")
	g := gradeFor(t, grades, "deploy")
	if g.Verdict != Helped {
		t.Fatalf("verdict = %q, want %q", g.Verdict, Helped)
	}
	if g.Diff != "" {
		t.Errorf("helped grade should carry no diff, got %q", g.Diff)
	}
	if g.Score != 1.0 {
		t.Errorf("helped score = %v, want 1.0", g.Score)
	}
}

// A skill loaded but doing no measurable work grades as a no_op friction case.
func TestNoOpFriction(t *testing.T) {
	a := New([]Skill{{Name: "deploy"}})
	// An attributed text block with no tool call and no turn boundary: loaded, idle.
	slice := []schema.Block{{Kind: schema.KindText, Seq: 1, Attribution: &schema.Attribution{Skill: "deploy"}, Text: &schema.TextBody{Text: "noted"}}}
	g := gradeFor(t, a.Evaluate(slice, "sess-6"), "deploy")
	if g.Verdict != NoOp {
		t.Fatalf("verdict = %q, want %q", g.Verdict, NoOp)
	}
	if g.Remedy != Prune {
		t.Errorf("remedy = %q, want %q", g.Remedy, Prune)
	}
}

// turnEnd is an unattributed turn boundary, so an activated skill accrues a turn.
func turnEnd(seq int) schema.Block {
	return schema.Block{Kind: schema.KindText, Seq: seq, StopReason: "end_turn"}
}
