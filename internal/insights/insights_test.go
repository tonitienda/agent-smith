package insights

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/schema"
)

func iptr(n int) *int { return &n }

func usage(in, out int) schema.Block {
	return eventlog.NewUsage("provider", "anthropic", "claude-opus-4-8", "end_turn",
		&schema.Tokens{Input: iptr(in), Output: iptr(out)}, nil)
}

func call(seq int, id, command string) schema.Block {
	args, _ := json.Marshal(map[string]string{"command": command})
	return schema.Block{
		Seq:      seq,
		Kind:     schema.KindToolCall,
		Role:     schema.RoleAssistant,
		ToolCall: &schema.ToolCallBody{ToolUseID: id, Name: shellTool, Arguments: args},
	}
}

func result(seq int, id string, failed bool, stdout string) schema.Block {
	exit := 0
	if failed {
		exit = 1
	}
	return schema.Block{
		Seq:        seq,
		Kind:       schema.KindToolResult,
		Role:       schema.RoleTool,
		ToolResult: &schema.ToolResultBody{ToolUseID: id, IsError: failed, ExitCode: &exit, Stdout: stdout},
	}
}

func read(seq int, path string) schema.Block {
	return schema.Block{
		Seq:      seq,
		Kind:     schema.KindFileRead,
		Role:     schema.RoleTool,
		FileRead: &schema.FileReadBody{Path: path, Content: "x"},
	}
}

// TestAnalyzeMeasuredSignals drives a session exercising every measured signal —
// a repeated command, a re-read file, an oversized tool output, an error loop —
// and asserts the report captures each and produces an applicable suggestion.
func TestAnalyzeMeasuredSignals(t *testing.T) {
	big := strings.Repeat("a", bigOutputTokens*charsPerTokenForTest+8)
	events := []schema.Block{
		usage(1000, 500),
		call(1, "c1", "make test"), result(2, "c1", false, ""),
		call(3, "c2", "make test"), result(4, "c2", false, ""),
		call(5, "c3", "make test"), result(6, "c3", false, ""),
		call(7, "c4", "grep -r foo ."), result(8, "c4", false, big),
		read(9, "main.go"), read(10, "main.go"),
		call(11, "c5", "ls"), result(12, "c5", false, ""),
		result(13, "e1", true, ""), result(14, "e2", true, ""), result(15, "e3", true, ""),
	}

	r := Analyze(events, cost.Embedded(), "claude-opus-4-8")

	if r.Turns != 1 {
		t.Errorf("Turns = %d, want 1", r.Turns)
	}
	if len(r.RepeatedCmds) != 1 || r.RepeatedCmds[0].Value != "make test" || r.RepeatedCmds[0].Count != 3 {
		t.Errorf("RepeatedCmds = %+v, want one 'make test' ×3", r.RepeatedCmds)
	}
	// `ls` ran once and is trivial anyway; it must never surface.
	for _, c := range r.RepeatedCmds {
		if c.Value == "ls" {
			t.Error("trivial command ls should not be a repeated-command signal")
		}
	}
	if len(r.RepeatedReads) != 1 || r.RepeatedReads[0].Value != "main.go" {
		t.Errorf("RepeatedReads = %+v, want main.go ×2", r.RepeatedReads)
	}
	if len(r.BigOutputs) != 1 || r.BigOutputs[0].Tool != shellTool {
		t.Errorf("BigOutputs = %+v, want one shell output", r.BigOutputs)
	}
	if r.Errors != 3 {
		t.Errorf("Errors = %d, want 3", r.Errors)
	}

	// AC: at least one specific, applicable suggestion (a memory-file edit) that
	// cites measured evidence with a jump-to anchor.
	applicable := 0
	for _, s := range r.Suggestions {
		if s.Edit == nil {
			continue
		}
		applicable++
		if !strings.Contains(s.Edit.Line, "make test") {
			t.Errorf("applicable suggestion edit = %q, want it to mention 'make test'", s.Edit.Line)
		}
		if !strings.Contains(s.Evidence, "#") {
			t.Errorf("suggestion evidence %q lacks a #seq jump-to anchor", s.Evidence)
		}
	}
	if applicable == 0 {
		t.Fatal("want at least one applicable (memory-edit) suggestion")
	}
}

// charsPerTokenForTest mirrors the cost package's chars-per-token heuristic so the
// test sizes a "big" output above the threshold without coupling to the constant.
const charsPerTokenForTest = 4

// TestAnalyzeNilTableZeroCost asserts the dashboard renders with no pricing (the
// zero-cost / model-disabled mode): tokens are exact, dollars unknown, and the
// non-cost suggestions still fire — the posture the insights-writer runs in.
func TestAnalyzeNilTableZeroCost(t *testing.T) {
	events := []schema.Block{
		usage(1000, 500),
		call(1, "c1", "go build ./..."), result(2, "c1", false, ""),
		call(3, "c2", "go build ./..."), result(4, "c2", false, ""),
		call(5, "c3", "go build ./..."), result(6, "c3", false, ""),
	}
	r := Analyze(events, nil, "")
	if r.AllPriced {
		t.Error("want AllPriced=false with a nil table")
	}
	if r.TotalTokens != 1500 {
		t.Errorf("TotalTokens = %d, want 1500 (exact even unpriced)", r.TotalTokens)
	}
	if len(r.Suggestions) == 0 || r.Suggestions[0].Edit == nil {
		t.Fatalf("want an applicable suggestion even unpriced, got %+v", r.Suggestions)
	}
	out := Render(r)
	if !strings.Contains(out, "go build") || !strings.Contains(out, "apply: /insights apply 1") {
		t.Errorf("render missing suggestion or apply hint:\n%s", out)
	}
}

// goalBlock builds a /goal-producer block the way internal/goal.Set does, so the
// retro recognizes it by value (goalProducer + goalPrefix) without importing goal.
func goalBlock(seq int, id, objective string) schema.Block {
	return schema.Block{
		Seq:        seq,
		ID:         id,
		Kind:       schema.KindText,
		Role:       schema.RoleSystem,
		Text:       &schema.TextBody{Text: goalPrefix + objective},
		Provenance: &schema.Provenance{Producer: goalProducer},
	}
}

// TestGoalAnchoringInProgress asserts a live /goal is surfaced as in-progress,
// grounded in measured signals (a #seq anchor), and never reads as met.
func TestGoalAnchoringInProgress(t *testing.T) {
	events := []schema.Block{
		goalBlock(1, "g1", "ship AS-109"),
		usage(1000, 500),
	}
	r := Analyze(events, nil, "")
	if r.Goal == nil {
		t.Fatal("want a goal assessment")
	}
	if r.Goal.Met || r.Goal.Status != "in-progress" {
		t.Errorf("goal = %+v, want in-progress, not met", r.Goal)
	}
	if r.Goal.Objective != "ship AS-109" {
		t.Errorf("objective = %q", r.Goal.Objective)
	}
	if !strings.Contains(r.Goal.Evidence, "#1") {
		t.Errorf("evidence %q lacks the goal #seq anchor", r.Goal.Evidence)
	}
	if out := Render(r); !strings.Contains(out, "Goal: ship AS-109") || !strings.Contains(out, "in progress") {
		t.Errorf("render missing in-progress goal:\n%s", out)
	}
}

// TestGoalAnchoringCompleted asserts a goal retired via /goal done (an exclusion
// of the goal block, with no live successor) reads as met — the measured
// completion signal.
func TestGoalAnchoringCompleted(t *testing.T) {
	events := []schema.Block{
		goalBlock(1, "g1", "land the spike"),
		usage(1000, 500),
		eventlog.NewExclusion(goalProducer, "g1"),
	}
	r := Analyze(events, nil, "")
	if r.Goal == nil || !r.Goal.Met || r.Goal.Status != "completed" {
		t.Fatalf("goal = %+v, want completed/met", r.Goal)
	}
	if out := Render(r); !strings.Contains(out, "met ✓") {
		t.Errorf("render missing met marker:\n%s", out)
	}
}

// TestGoalAnchoringNone asserts no goal yields no assessment (and no goal line),
// so the dashboard is unchanged for the common no-goal session.
func TestGoalAnchoringNone(t *testing.T) {
	r := Analyze([]schema.Block{usage(10, 10)}, nil, "")
	if r.Goal != nil {
		t.Errorf("want nil goal with no /goal set, got %+v", r.Goal)
	}
	if strings.Contains(Render(r), "Goal:") {
		t.Error("render should omit the goal line when no goal is set")
	}
}

// TestRenderEmpty asserts the dashboard degrades to a clear message rather than an
// empty panel when there is no activity yet.
func TestRenderEmpty(t *testing.T) {
	if got := Render(Analyze(nil, nil, "")); !strings.Contains(got, "No session activity") {
		t.Errorf("empty render = %q", got)
	}
}
