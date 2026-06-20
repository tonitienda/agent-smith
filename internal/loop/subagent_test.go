package loop_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/subagent"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/schema"
)

// spyRec is the shared, concurrency-safe ledger a spyAgent records its lifecycle
// calls into. Shared by every instance the Runner's factory builds for one
// sub-agent so the test can assert against it after the run.
type spyRec struct {
	mu        sync.Mutex
	inits     []subagent.Scope
	observed  []schema.Kind
	teardowns []subagent.Scope
	sliceLens []int
}

// spyAgent is a passive sub-agent that records each lifecycle call and never
// calls a model (Teardown returns an empty Result: no findings, no spend).
type spyAgent struct {
	manifest subagent.Manifest
	rec      *spyRec
}

func (s *spyAgent) Manifest() subagent.Manifest { return s.manifest }

func (s *spyAgent) Init(sc subagent.Scope) {
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	s.rec.inits = append(s.rec.inits, sc)
}

func (s *spyAgent) Observe(b schema.Block) {
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	s.rec.observed = append(s.rec.observed, b.Kind)
}

func (s *spyAgent) Teardown(sc subagent.Scope, slice []schema.Block) subagent.Result {
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	s.rec.teardowns = append(s.rec.teardowns, sc)
	s.rec.sliceLens = append(s.rec.sliceLens, len(slice))
	return subagent.Result{} // no model call: no findings, no spend
}

func spyFactory(m subagent.Manifest, rec *spyRec) subagent.Factory {
	return func() subagent.SubAgent { return &spyAgent{manifest: m, rec: rec} }
}

// TestSubAgentLifecycleWiring is the AS-088 acceptance check: with a Runner
// installed the loop Begins/Observes/Ends sub-agents at the right lifecycle
// points — a span scope per turn and one session scope for the whole Run — fanning
// every appended block out to Observe, and tearing each scope down without a model
// call. A span-scoped and a session-scoped spy together exercise both windows.
func TestSubAgentLifecycleWiring(t *testing.T) {
	calls := 0
	p := &provider.Mock{ScriptFn: func(_ context.Context, _ provider.Request) ([]provider.Event, error) {
		calls++
		if calls == 1 {
			return provider.ToolCallTurn("toolu_1", "echo", json.RawMessage(`{"msg":"hi"}`)), nil
		}
		return provider.TextTurn("all done", ""), nil
	}}

	spanRec, sessRec := &spyRec{}, &spyRec{}
	reg := subagent.NewRegistry()
	if err := reg.Register(spyFactory(subagent.Manifest{
		Name: "span-spy", Kind: subagent.KindAnalyzer, EnabledByDefault: true,
	}, spanRec)); err != nil { // defaults: AtTeardown schedule + span scope
		t.Fatalf("register span spy: %v", err)
	}
	if err := reg.Register(spyFactory(subagent.Manifest{
		Name: "sess-spy", Kind: subagent.KindAnalyzer, EnabledByDefault: true,
		Schedule: subagent.AtSessionEnd, Scope: subagent.SessionScope,
	}, sessRec)); err != nil {
		t.Fatalf("register session spy: %v", err)
	}
	runner := subagent.NewRunner(reg, nil, "sess-1")

	h := newHarness(t, p, []tool.Tool{echoTool()}, loop.WithSubAgents(runner))
	if _, err := h.engine.Run(context.Background(), "please echo hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// One span scope per turn (two turns), each inited and torn down, in order.
	wantSpans := []string{"turn-0", "turn-1"}
	assertScopeIDs(t, "span init", spanRec.inits, subagent.SpanScope, wantSpans)
	assertScopeIDs(t, "span teardown", spanRec.teardowns, subagent.SpanScope, wantSpans)

	// Exactly one session scope, inited at session start and torn down at the end.
	if len(sessRec.inits) != 1 || sessRec.inits[0].Kind != subagent.SessionScope {
		t.Errorf("session inits = %+v, want one session scope", sessRec.inits)
	}
	if len(sessRec.teardowns) != 1 || sessRec.teardowns[0].Kind != subagent.SessionScope {
		t.Errorf("session teardowns = %+v, want one session scope", sessRec.teardowns)
	}

	// Every block on the log was fanned out to Observe, in append order, for both
	// sub-agents — the log is the single record, so this holds regardless of which
	// layer appended each block (tool results included).
	wantKinds := logKinds(h.log.Events())
	if len(wantKinds) == 0 {
		t.Fatal("expected the run to append blocks")
	}
	assertObserved(t, "span", spanRec.observed, wantKinds)
	assertObserved(t, "sess", sessRec.observed, wantKinds)

	// The session teardown saw the whole session slice; each span teardown saw a
	// non-empty slice of its turn's blocks.
	if got := sessRec.sliceLens; len(got) != 1 || got[0] != len(wantKinds) {
		t.Errorf("session teardown slice lens = %v, want [%d]", got, len(wantKinds))
	}
	for i, n := range spanRec.sliceLens {
		if n == 0 {
			t.Errorf("span teardown %d saw an empty slice", i)
		}
	}

	// Passive sub-agents never spend: no model call happened in any teardown.
	if got := runner.SpentUSD("span-spy") + runner.SpentUSD("sess-spy"); got != 0 {
		t.Errorf("sub-agent spend = %v, want 0 (passive, no model call)", got)
	}
}

// TestSubAgentWiringNilIsNoOp verifies the wiring is inert when no Runner is
// installed: a nil option is ignored and the loop runs exactly as before.
func TestSubAgentWiringNilIsNoOp(t *testing.T) {
	p := &provider.Mock{ScriptFn: func(_ context.Context, _ provider.Request) ([]provider.Event, error) {
		return provider.TextTurn("hi", ""), nil
	}}
	h := newHarness(t, p, nil, loop.WithSubAgents(nil))
	res, err := h.engine.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.FinalText != "hi" {
		t.Errorf("FinalText = %q, want %q", res.FinalText, "hi")
	}
}

func logKinds(blocks []schema.Block) []schema.Kind {
	out := make([]schema.Kind, len(blocks))
	for i, b := range blocks {
		out[i] = b.Kind
	}
	return out
}

func assertObserved(t *testing.T, who string, got, want []schema.Kind) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s observed %d blocks, want %d (%v vs %v)", who, len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s observed kinds = %v, want %v", who, got, want)
		}
	}
}

func assertScopeIDs(t *testing.T, what string, scopes []subagent.Scope, kind subagent.ScopeKind, wantSpans []string) {
	t.Helper()
	if len(scopes) != len(wantSpans) {
		t.Fatalf("%s: got %d scopes, want %d (%+v)", what, len(scopes), len(wantSpans), scopes)
	}
	for i, sc := range scopes {
		if sc.Kind != kind || sc.Span != wantSpans[i] || sc.Session != "sess-1" {
			t.Errorf("%s[%d] = %+v, want kind=%s span=%s session=sess-1", what, i, sc, kind, wantSpans[i])
		}
	}
}
