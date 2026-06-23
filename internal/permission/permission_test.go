package permission

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// recordingAsker is a test Asker that returns a canned Outcome and records the
// requests it received.
type recordingAsker struct {
	mu       sync.Mutex
	outcome  Outcome
	err      error
	requests []Request
}

func (a *recordingAsker) Ask(_ context.Context, req Request) (Outcome, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests = append(a.requests, req)
	return a.outcome, a.err
}

func (a *recordingAsker) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.requests)
}

func shellCall(cmd string) tool.Call {
	return tool.Call{ToolUseID: "t1", Name: "shell", Arguments: json.RawMessage(`{"command":` + quote(cmd) + `}`)}
}

func pathCall(name, path string) tool.Call {
	return tool.Call{ToolUseID: "t1", Name: name, Arguments: json.RawMessage(`{"path":` + quote(path) + `}`)}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestModeAutoAllowsWithoutAsker(t *testing.T) {
	p := New(Config{DefaultMode: ModeAuto})
	if d := p.decide(context.Background(), shellCall("rm -rf /")); !d.Allow {
		t.Fatalf("auto mode denied: %+v", d)
	}
}

func TestModeAskPromptsAndHonorsOutcome(t *testing.T) {
	asker := &recordingAsker{outcome: Outcome{Allow: true}}
	p := New(Config{DefaultMode: ModeAsk}, WithAsker(asker))

	d := p.decide(context.Background(), shellCall("git status"))
	if !d.Allow {
		t.Fatalf("ask+approve denied: %+v", d)
	}
	if asker.count() != 1 {
		t.Fatalf("asker consulted %d times, want 1", asker.count())
	}
	if got := asker.requests[0].Subject; got != "git status" {
		t.Fatalf("request subject = %q, want %q", got, "git status")
	}

	asker.outcome = Outcome{Allow: false}
	if d := p.decide(context.Background(), shellCall("git push")); d.Allow {
		t.Fatalf("ask+deny allowed: %+v", d)
	}
}

// TestPromptAttributesDelegatedAgent is the AS-120 attribution check: when a
// delegated child's call flows through the gate, its agent id (tagged on the
// context by the delegation layer) reaches the Asker's Request, so a face can
// name the child; a non-delegated call carries no agent id.
func TestPromptAttributesDelegatedAgent(t *testing.T) {
	asker := &recordingAsker{outcome: Outcome{Allow: true}}
	p := New(Config{DefaultMode: ModeAsk}, WithAsker(asker))

	p.decide(tool.WithAgent(context.Background(), "child-7"), shellCall("ls"))
	p.decide(context.Background(), shellCall("pwd"))

	if len(asker.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(asker.requests))
	}
	if got := asker.requests[0].AgentID; got != "child-7" {
		t.Errorf("delegated request AgentID = %q, want child-7", got)
	}
	if got := asker.requests[1].AgentID; got != "" {
		t.Errorf("non-delegated request AgentID = %q, want empty", got)
	}
}

func TestModeAskDeniesWhenNoAsker(t *testing.T) {
	p := New(Config{DefaultMode: ModeAsk})
	d := p.decide(context.Background(), shellCall("git status"))
	if d.Allow {
		t.Fatalf("ask mode without asker allowed")
	}
	if !strings.Contains(d.Reason, "no approver") {
		t.Fatalf("reason = %q, want it to mention the missing approver", d.Reason)
	}
}

func TestAskerErrorIsDenial(t *testing.T) {
	asker := &recordingAsker{err: context.Canceled}
	p := New(Config{DefaultMode: ModeAsk}, WithAsker(asker))
	d := p.decide(context.Background(), shellCall("git status"))
	if d.Allow {
		t.Fatalf("asker error allowed the call")
	}
	if !strings.Contains(d.Reason, "approval failed") {
		t.Fatalf("reason = %q, want approval-failed", d.Reason)
	}
}

func TestModeAllowlistMatchRunsRestPrompts(t *testing.T) {
	asker := &recordingAsker{outcome: Outcome{Allow: false}}
	p := New(Config{
		DefaultMode: ModeAllowlist,
		Allow:       []Rule{{Tool: "shell", Pattern: "git status*"}},
	}, WithAsker(asker))

	// Matching call runs without a prompt.
	if d := p.decide(context.Background(), shellCall("git status -s")); !d.Allow {
		t.Fatalf("allowlisted call denied: %+v", d)
	}
	if asker.count() != 0 {
		t.Fatalf("allowlisted call still prompted")
	}

	// Non-matching call falls through to the prompt (here denied).
	if d := p.decide(context.Background(), shellCall("git push")); d.Allow {
		t.Fatalf("non-allowlisted call allowed: %+v", d)
	}
	if asker.count() != 1 {
		t.Fatalf("non-allowlisted call did not prompt")
	}
}

func TestPerToolModeOverridesDefault(t *testing.T) {
	p := New(Config{
		DefaultMode: ModeAsk,
		Tools:       map[string]Mode{"read": ModeAuto},
	})
	// read is auto despite the ask default and no asker.
	if d := p.decide(context.Background(), pathCall("read", "main.go")); !d.Allow {
		t.Fatalf("per-tool auto override denied: %+v", d)
	}
	// shell still uses the ask default → denied (no asker).
	if d := p.decide(context.Background(), shellCall("ls")); d.Allow {
		t.Fatalf("default ask mode allowed shell without an asker")
	}
}

func TestUnknownModeFailsSafeToAsk(t *testing.T) {
	p := New(Config{DefaultMode: Mode("bogus")})
	if d := p.decide(context.Background(), shellCall("ls")); d.Allow {
		t.Fatalf("unknown default mode did not fail safe to ask/deny")
	}
}

func TestRememberPersistsAndTakesEffect(t *testing.T) {
	var persisted []Rule
	asker := &recordingAsker{outcome: Outcome{Allow: true, Remember: true}}
	p := New(Config{DefaultMode: ModeAllowlist},
		WithAsker(asker),
		WithPersister(func(r Rule) error { persisted = append(persisted, r); return nil }))

	// First call: not in the list, prompts, approved with remember.
	if d := p.decide(context.Background(), shellCall("npm test")); !d.Allow {
		t.Fatalf("first call denied: %+v", d)
	}
	if asker.count() != 1 {
		t.Fatalf("first call did not prompt")
	}
	if len(persisted) != 1 || persisted[0] != (Rule{Tool: "shell", Pattern: "npm test"}) {
		t.Fatalf("persisted = %+v, want one shell/npm test rule", persisted)
	}

	// Second identical call: now allow-listed, runs without prompting again.
	if d := p.decide(context.Background(), shellCall("npm test")); !d.Allow {
		t.Fatalf("remembered call denied: %+v", d)
	}
	if asker.count() != 1 {
		t.Fatalf("remembered call prompted again (count=%d)", asker.count())
	}
}

func TestRememberUsesExplicitRule(t *testing.T) {
	asker := &recordingAsker{outcome: Outcome{Allow: true, Remember: true, Rule: Rule{Tool: "shell", Pattern: "npm*"}}}
	p := New(Config{DefaultMode: ModeAllowlist}, WithAsker(asker))

	if d := p.decide(context.Background(), shellCall("npm test")); !d.Allow {
		t.Fatalf("first call denied: %+v", d)
	}
	// A different npm command is now covered by the broader remembered rule.
	if d := p.decide(context.Background(), shellCall("npm run build")); !d.Allow {
		t.Fatalf("broader remembered rule did not cover sibling command: %+v", d)
	}
	if asker.count() != 1 {
		t.Fatalf("broader rule did not suppress the second prompt (count=%d)", asker.count())
	}
}

// TestPolicyGatesRealRuntime wires the Policy through the real tool.Runtime to
// prove the gate is enforced end-to-end: a denied call never runs the tool and
// surfaces a model-readable error, while an allow-listed call runs.
func TestPolicyGatesRealRuntime(t *testing.T) {
	var ran int
	reg := tool.NewRegistry()
	shell := tool.Func{
		Spec: tool.Def{
			Name:        "shell",
			InputSchema: json.RawMessage(`{"type":"object","required":["command"],"properties":{"command":{"type":"string"}}}`),
		},
		Fn: func(context.Context, json.RawMessage) (tool.Output, error) {
			ran++
			return tool.Output{Text: "ok"}, nil
		},
	}
	if err := reg.Register(shell); err != nil {
		t.Fatalf("Register: %v", err)
	}

	p := New(Config{DefaultMode: ModeAllowlist, Allow: []Rule{{Tool: "shell", Pattern: "git status*"}}})
	rt := tool.NewRuntime(reg, eventlog.New(), tool.WithPermission(p.Func()))

	// Denied (no match, no asker): tool does not run, error result returned.
	res, err := rt.Execute(context.Background(), toolCallBlock("shell", `{"command":"rm -rf /"}`))
	if err != nil {
		t.Fatalf("Execute (denied): %v", err)
	}
	if ran != 0 {
		t.Fatalf("tool ran despite denial")
	}
	if !res.ToolResult.IsError || !strings.Contains(res.ToolResult.Content[0].Text, "permission denied") {
		t.Fatalf("denied result = %+v, want permission-denied error", res.ToolResult)
	}

	// Allowed (matches the allowlist): tool runs.
	if _, err := rt.Execute(context.Background(), toolCallBlock("shell", `{"command":"git status"}`)); err != nil {
		t.Fatalf("Execute (allowed): %v", err)
	}
	if ran != 1 {
		t.Fatalf("allow-listed tool did not run (ran=%d)", ran)
	}
}

func toolCallBlock(name, args string) schema.Block {
	return schema.Block{
		Kind: schema.KindToolCall,
		Role: schema.RoleAssistant,
		ToolCall: &schema.ToolCallBody{
			ToolUseID: "tu-" + name,
			Name:      name,
			Arguments: json.RawMessage(args),
		},
	}
}
