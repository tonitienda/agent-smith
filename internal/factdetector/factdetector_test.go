package factdetector

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tonitienda/agent-smith/internal/subagent"
	"github.com/tonitienda/agent-smith/schema"
)

// shellCall builds a shell tool_call block carrying command, keyed by id.
func shellCall(id, command, skill string) schema.Block {
	args, _ := json.Marshal(map[string]string{"command": command})
	b := schema.Block{
		Kind:     schema.KindToolCall,
		Role:     schema.RoleAssistant,
		ToolCall: &schema.ToolCallBody{ToolUseID: id, Name: shellTool, Arguments: args},
	}
	if skill != "" {
		b.Attribution = &schema.Attribution{Skill: skill}
	}
	return b
}

// shellResult builds a shell tool_result for the call id, failing or not.
func shellResult(id string, failed bool) schema.Block {
	exit := 0
	if failed {
		exit = 1
	}
	return schema.Block{
		Kind:       schema.KindToolResult,
		Role:       schema.RoleTool,
		ToolResult: &schema.ToolResultBody{ToolUseID: id, IsError: failed, ExitCode: &exit},
	}
}

// flailToFindTest is the canonical trace: the agent flails to find the test
// command, failing twice before `go test ./...` works.
func flailToFindTest() []schema.Block {
	return []schema.Block{
		shellCall("c1", "npm test", ""),
		shellResult("c1", true),
		shellCall("c2", "make tests", ""),
		shellResult("c2", true),
		shellCall("c3", "go test ./...", ""),
		shellResult("c3", false),
	}
}

// AC: on a session that rediscovers a known fact (flailing to find the test
// command), the detector proposes exactly that fact with the trace as evidence.
func TestDetectsRediscoveredCommand(t *testing.T) {
	d := New(nil, NewMemLedger())
	res := d.Teardown(subagent.Scope{Kind: subagent.SessionScope}, flailToFindTest())

	if len(res.Findings) != 1 {
		t.Fatalf("want exactly 1 finding, got %d: %+v", len(res.Findings), res.Findings)
	}
	if res.SpentUSD != 0 {
		t.Fatalf("detector must not spend (no model calls), got %v", res.SpentUSD)
	}
	f := res.Findings[0]
	if f.Kind != FindingKind {
		t.Fatalf("wrong finding kind %q", f.Kind)
	}
	if f.Proposal == nil {
		t.Fatal("finding carries no proposal")
	}
	if got, want := f.Proposal.Target, DefaultTarget; got != want {
		t.Fatalf("target = %q, want fallback %q", got, want)
	}
	// The proposed fact is the command that worked, and the evidence cites the
	// failed attempts that share a meaningful token with it.
	if !strings.Contains(f.Proposal.Description, "go test ./...") {
		t.Fatalf("diff does not propose the working command: %q", f.Proposal.Description)
	}
	if !strings.Contains(f.Detail, "npm test") || !strings.Contains(f.Detail, "make tests") {
		t.Fatalf("evidence does not cite the failed attempts: %q", f.Detail)
	}
}

// A success with no related flailing is not a rediscovered fact (precision: a
// command that just worked is not evidence the skill/memory has a gap).
func TestNoFlailNoFact(t *testing.T) {
	blocks := []schema.Block{
		shellCall("c1", "go build ./...", ""),
		shellResult("c1", false),
		shellCall("c2", "go test ./...", ""),
		shellResult("c2", false),
	}
	d := New(nil, NewMemLedger())
	if got := d.Teardown(subagent.Scope{}, blocks).Findings; len(got) != 0 {
		t.Fatalf("want no findings for clean run, got %+v", got)
	}
}

// A failure followed by an unrelated success (no shared significant token) is
// not linked — guards against false positives from coincidental sequencing.
func TestUnrelatedSuccessNotLinked(t *testing.T) {
	blocks := []schema.Block{
		shellCall("c1", "npm test", ""),
		shellResult("c1", true),
		shellCall("c2", "git status", ""),
		shellResult("c2", false),
	}
	d := New(nil, NewMemLedger())
	if got := d.Teardown(subagent.Scope{}, blocks).Findings; len(got) != 0 {
		t.Fatalf("want no findings for unrelated success, got %+v", got)
	}
}

// An unrelated success mid-flail (an informational `git status`) must not clear
// the failure history: the later related success still links to the failures.
func TestUnrelatedSuccessDoesNotOrphanFlail(t *testing.T) {
	blocks := []schema.Block{
		shellCall("c1", "npm test", ""),
		shellResult("c1", true),
		shellCall("c2", "git status", ""), // unrelated success in the middle
		shellResult("c2", false),
		shellCall("c3", "go test ./...", ""),
		shellResult("c3", false),
	}
	d := New(nil, NewMemLedger())
	got := d.Teardown(subagent.Scope{}, blocks).Findings
	if len(got) != 1 {
		t.Fatalf("want 1 finding (flail survives unrelated success), got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Detail, "npm test") {
		t.Fatalf("evidence lost the original failure: %q", got[0].Detail)
	}
}

// A zero-value MemLedger is safe to use directly (no nil-map panic on Record).
func TestZeroValueLedgerSafe(t *testing.T) {
	var led MemLedger
	led.Record("command:x", Dismissed)
	if !led.Dismissed("command:x") {
		t.Fatal("zero-value ledger did not record dismissal")
	}
}

// A flailed local script is tokenized on its name, so it can be detected.
func TestLocalScriptTokenized(t *testing.T) {
	blocks := []schema.Block{
		shellCall("c1", "./test.sh --all", ""),
		shellResult("c1", true),
		shellCall("c2", "bash test.sh", ""),
		shellResult("c2", false),
	}
	d := New(nil, NewMemLedger())
	if got := d.Teardown(subagent.Scope{}, blocks).Findings; len(got) != 1 {
		t.Fatalf("want 1 finding for local-script flail, got %d: %+v", len(got), got)
	}
}

// searchCallBlock builds a grep/glob tool_call carrying a pattern, keyed by id.
func searchCallBlock(id, toolName, pattern string) schema.Block {
	args, _ := json.Marshal(map[string]string{"pattern": pattern})
	return schema.Block{
		Kind:     schema.KindToolCall,
		Role:     schema.RoleAssistant,
		ToolCall: &schema.ToolCallBody{ToolUseID: id, Name: toolName, Arguments: args},
	}
}

// readCall builds a read tool_call carrying a path, keyed by id.
func readCall(id, path, skill string) schema.Block {
	args, _ := json.Marshal(map[string]string{"path": path})
	b := schema.Block{
		Kind:     schema.KindToolCall,
		Role:     schema.RoleAssistant,
		ToolCall: &schema.ToolCallBody{ToolUseID: id, Name: "read", Arguments: args},
	}
	if skill != "" {
		b.Attribution = &schema.Attribution{Skill: skill}
	}
	return b
}

// shellResultText is a shell tool_result whose combined output (content text)
// carries the given text — how real shell results surface stderr signatures.
func shellResultText(id, text string, failed bool) schema.Block {
	b := shellResult(id, failed)
	b.ToolResult.Content = []schema.Part{{Type: "text", Text: text}}
	return b
}

// AC: a session that flails across searches before reading one file proposes that
// path as a fact, with the search trace as evidence.
func TestDetectsRediscoveredPath(t *testing.T) {
	blocks := []schema.Block{
		searchCallBlock("s1", "grep", "func detectConfigKeys"),
		shellResult("s1", false),
		searchCallBlock("s2", "glob", "**/factdetector*.go"),
		shellResult("s2", false),
		readCall("r1", "internal/factdetector/factdetector.go", ""),
		shellResult("r1", false),
	}
	d := New(nil, NewMemLedger())
	got := d.Teardown(subagent.Scope{}, blocks).Findings
	if len(got) != 1 {
		t.Fatalf("want 1 path finding, got %d: %+v", len(got), got)
	}
	f := got[0]
	if !strings.Contains(f.Summary, "file location") {
		t.Fatalf("summary not a path fact: %q", f.Summary)
	}
	if !strings.Contains(f.Proposal.Description, "internal/factdetector/factdetector.go") {
		t.Fatalf("diff does not propose the path: %q", f.Proposal.Description)
	}
	if !strings.Contains(f.Detail, "factdetector") {
		t.Fatalf("evidence does not cite the searches: %q", f.Detail)
	}
}

// Precision: a single direct read with no preceding flailing is not a fact.
func TestSingleDirectReadNoPathFact(t *testing.T) {
	blocks := []schema.Block{
		readCall("r1", "internal/factdetector/factdetector.go", ""),
		shellResult("r1", false),
	}
	d := New(nil, NewMemLedger())
	if got := d.Teardown(subagent.Scope{}, blocks).Findings; len(got) != 0 {
		t.Fatalf("want no finding for a direct read, got %+v", got)
	}
}

// Precision: searches that do not name the read path are not linked to it.
func TestUnrelatedSearchesNoPathFact(t *testing.T) {
	blocks := []schema.Block{
		searchCallBlock("s1", "grep", "TODO"),
		shellResult("s1", false),
		searchCallBlock("s2", "glob", "**/*.json"),
		shellResult("s2", false),
		readCall("r1", "internal/factdetector/factdetector.go", ""),
		shellResult("r1", false),
	}
	d := New(nil, NewMemLedger())
	if got := d.Teardown(subagent.Scope{}, blocks).Findings; len(got) != 0 {
		t.Fatalf("want no finding for unrelated searches, got %+v", got)
	}
}

// AC: a config key discovered through a failed-then-fixed run is proposed with
// its trace.
func TestDetectsRediscoveredConfigKey(t *testing.T) {
	blocks := []schema.Block{
		shellCall("c1", "go run ./cmd/server", ""),
		shellResultText("c1", "[exit code 1]\nfatal: DATABASE_URL is not set", true),
		shellCall("c2", "go run ./cmd/server", ""),
		shellResult("c2", false),
	}
	d := New(nil, NewMemLedger())
	got := d.Teardown(subagent.Scope{}, blocks).Findings
	// The failed-then-successful command is also a command fact; assert the config
	// fact is present among the findings.
	var cfg *subagent.Finding
	for i := range got {
		if strings.Contains(got[i].Summary, "required config") {
			cfg = &got[i]
		}
	}
	if cfg == nil {
		t.Fatalf("no config finding among %+v", got)
	}
	if !strings.Contains(cfg.Proposal.Description, "DATABASE_URL") {
		t.Fatalf("config diff lost the var: %q", cfg.Proposal.Description)
	}
	if !strings.Contains(cfg.Detail, "go run ./cmd/server") {
		t.Fatalf("config evidence lost the failing command: %q", cfg.Detail)
	}
}

// Precision: a failed-then-fixed run whose output names no env var is not a
// config fact, and an ordinary config read is never flagged.
func TestOrdinaryConfigNotFlagged(t *testing.T) {
	blocks := []schema.Block{
		shellCall("c1", "go build ./...", ""),
		shellResultText("c1", "undefined: Foo", true),
		shellCall("c2", "cat README.md", ""),
		shellResult("c2", false),
	}
	d := New(nil, NewMemLedger())
	for _, f := range d.Teardown(subagent.Scope{}, blocks).Findings {
		if strings.Contains(f.Summary, "required config") {
			t.Fatalf("ordinary run flagged as config: %+v", f)
		}
	}
}

// Precision: an unrelated successful command (an `ls` mid-flail) must not resolve
// a pending config key; only a related success (the failing command re-run after
// the var is set) confirms the fix.
func TestUnrelatedSuccessDoesNotResolveConfig(t *testing.T) {
	blocks := []schema.Block{
		shellCall("c1", "go run ./cmd/server", ""),
		shellResultText("c1", "[exit code 1]\nfatal: DATABASE_URL is not set", true),
		shellCall("c2", "ls -la", ""),
		shellResult("c2", false),
	}
	d := New(nil, NewMemLedger())
	for _, f := range d.Teardown(subagent.Scope{}, blocks).Findings {
		if strings.Contains(f.Summary, "required config") {
			t.Fatalf("unrelated success resolved config: %+v", f)
		}
	}
}

// AC: declining records the dismissal and the same fact is not re-suggested.
func TestDismissedFactNotResuggested(t *testing.T) {
	led := NewMemLedger()
	d := New(nil, led)

	first := d.Teardown(subagent.Scope{}, flailToFindTest()).Findings
	if len(first) != 1 {
		t.Fatalf("want 1 finding first time, got %d", len(first))
	}
	led.Record(commandFingerprint("go test ./..."), Dismissed)

	second := d.Teardown(subagent.Scope{}, flailToFindTest()).Findings
	if len(second) != 0 {
		t.Fatalf("dismissed fact was re-suggested: %+v", second)
	}
}

// AC: the precision bar is tracked — accepted vs dismissed.
func TestPrecisionTracking(t *testing.T) {
	led := NewMemLedger()
	led.Record("command:a", Accepted)
	led.Record("command:b", Accepted)
	led.Record("command:c", Dismissed)

	s := led.Stats()
	if s.Accepted != 2 || s.Dismissed != 1 || s.Total() != 3 {
		t.Fatalf("unexpected stats: %+v", s)
	}
	if got := s.Precision(); got < 0.66 || got > 0.67 {
		t.Fatalf("precision = %v, want ~0.667", got)
	}
	if (Stats{}).Precision() != 0 {
		t.Fatal("empty precision must be 0, not NaN")
	}
}

// The save target resolves to the active skill when the trace is skill-scoped,
// and to whatever the resolver returns otherwise (C.1 save-target rule).
func TestTargetResolution(t *testing.T) {
	resolve := func(skill string, _ []string) string {
		if skill != "" {
			return "skills/" + skill + "/SKILL.md"
		}
		return "deep/AGENT.md"
	}
	d := New(resolve, NewMemLedger())

	// Skill-scoped trace → the skill's file.
	scoped := []schema.Block{
		shellCall("c1", "npm test", "deploy-service"),
		shellResult("c1", true),
		shellCall("c2", "go test ./...", "deploy-service"),
		shellResult("c2", false),
	}
	f := d.Teardown(subagent.Scope{}, scoped).Findings
	if len(f) != 1 || f[0].Proposal.Target != "skills/deploy-service/SKILL.md" {
		t.Fatalf("skill-scoped target wrong: %+v", f)
	}

	// Unscoped trace → resolver's memory-file choice.
	g := d.Teardown(subagent.Scope{}, flailToFindTest()).Findings
	if len(g) != 1 || g[0].Proposal.Target != "deep/AGENT.md" {
		t.Fatalf("unscoped target wrong: %+v", g)
	}
}

// AC: zero cost when disabled; within budget when enabled. Driven through the
// real subagent Runner to prove the framework never inits/observes/tears down a
// disabled detector, and runs the enabled one at session end for $0.
func TestRunnerIntegration(t *testing.T) {
	led := NewMemLedger()

	// Enabled (default): the Runner tears it down at session end and records a
	// finding, spending nothing.
	reg := subagent.NewRegistry()
	if err := reg.Register(Factory(nil, led)); err != nil {
		t.Fatal(err)
	}
	rn := subagent.NewRunner(reg, nil, "s1")
	rn.End(subagent.Scope{Kind: subagent.SessionScope}, flailToFindTest())
	if got := rn.Store().Findings("s1"); len(got) != 1 || got[0].SubAgent != Name {
		t.Fatalf("enabled detector did not record a finding: %+v", got)
	}
	if got := rn.SpentUSD(Name); got != 0 {
		t.Fatalf("detector spent %v, want 0", got)
	}

	// Disabled by one config line: never driven, zero findings.
	off := false
	reg.Configure(map[string]subagent.Config{Name: {Enabled: &off}})
	rn2 := subagent.NewRunner(reg, nil, "s2")
	rn2.End(subagent.Scope{Kind: subagent.SessionScope}, flailToFindTest())
	if got := rn2.Store().Findings("s2"); len(got) != 0 {
		t.Fatalf("disabled detector produced findings: %+v", got)
	}
}

// The detector's manifest passes the framework's validation and declares the
// passive, propose-only, zero-cost shape AS-044 requires.
func TestManifestValid(t *testing.T) {
	m := (&Detector{}).Manifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest invalid: %v", err)
	}
	if m.ModelTier != "" || m.BudgetUSD != 0 {
		t.Fatalf("detector should declare no model use: %+v", m)
	}
	if !m.Allows(subagent.ProposeEdit) || !m.Allows(subagent.ReadTranscript) {
		t.Fatalf("detector must claim propose_edit + read_transcript: %+v", m)
	}
}
