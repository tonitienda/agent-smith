package delegate_test

import (
	"context"
	"testing"

	"github.com/tonitienda/agent-smith/internal/delegate"
	"github.com/tonitienda/agent-smith/internal/eventlog"
	"github.com/tonitienda/agent-smith/internal/provider"
	"github.com/tonitienda/agent-smith/internal/routing"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/internal/tool/builtin"
	"github.com/tonitienda/agent-smith/schema"
)

// scriptTextWithUsage scripts a single text-only turn that also reports usage, so
// a delegation produces both a final summary and a usage event to roll up.
func scriptTextWithUsage(text string, in, out int) []provider.Event {
	ev := provider.TextTurn(text, "")
	usage := provider.Event{Type: provider.EventUsage, Usage: &schema.Tokens{Input: &in, Output: &out}}
	res := make([]provider.Event, 0, len(ev)+1)
	res = append(res, ev[:len(ev)-1]...) // everything before EventTurnStop
	res = append(res, usage, ev[len(ev)-1])
	return res
}

func newStore(t *testing.T) *session.Store {
	t.Helper()
	store, err := session.NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

// TestSpawnRunsLinkedChildAndRollsUpCost covers the AS-046 core: a delegation
// runs a child agent in its own persisted session linked to the parent, returns
// the child's summary, and rolls the child's token usage onto the parent log as a
// sidechain so the parent's /cost accounting includes it.
func TestSpawnRunsLinkedChildAndRollsUpCost(t *testing.T) {
	store := newStore(t)
	parent, err := store.Create("parent")
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	defer func() { _ = parent.Log.Close() }()

	mock := &provider.Mock{Events: scriptTextWithUsage("did the work", 10, 5)}
	providers := map[string]provider.Provider{"mock": mock}

	sp := delegate.New(store, providers,
		func() (*tool.Registry, error) { return tool.NewRegistry(), nil },
		func() delegate.Parent {
			return delegate.Parent{
				Log:       parent.Log,
				SessionID: parent.ID,
				ProvName:  "mock",
				Model:     "fallback-model",
				Router:    routing.Default(),
			}
		})

	res, err := sp.Spawn(context.Background(), builtin.TaskRequest{Prompt: "do the work"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if res.Summary != "did the work" {
		t.Errorf("summary = %q, want %q", res.Summary, "did the work")
	}
	if res.SessionID == "" {
		t.Fatal("Spawn returned no child session id")
	}

	// The child is a real, persisted session linked back to the parent.
	child, err := store.Open(res.SessionID)
	if err != nil {
		t.Fatalf("open child session: %v", err)
	}
	defer func() { _ = child.Log.Close() }()
	if child.Metadata.Parent != parent.ID {
		t.Errorf("child.Parent = %q, want parent %q", child.Metadata.Parent, parent.ID)
	}

	// The child's usage was rolled up onto the parent log as a sidechain.
	var rolled int
	for _, b := range parent.Log.Events() {
		if b.Kind != eventlog.KindUsage {
			continue
		}
		rolled++
		if b.Thread == nil || !b.Thread.IsSidechain || b.Thread.AgentID != res.SessionID {
			t.Errorf("rolled usage thread = %+v, want sidechain agent %q", b.Thread, res.SessionID)
		}
		if b.Tokens == nil || b.Tokens.Input == nil || *b.Tokens.Input != 10 {
			t.Errorf("rolled usage tokens = %+v, want input 10", b.Tokens)
		}
	}
	if rolled != 1 {
		t.Errorf("rolled-up usage events = %d, want 1", rolled)
	}
}

// TestSpawnResolvesCheapTier covers the fan-out default (PRD §7.17): with no
// explicit model the child runs on the provider's cheap routing tier.
func TestSpawnResolvesCheapTier(t *testing.T) {
	store := newStore(t)
	parent, err := store.Create("parent")
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	defer func() { _ = parent.Log.Close() }()

	mock := &provider.Mock{Events: provider.TextTurn("ok", "")}
	router := routing.Default().WithVendorModel(routing.Cheap, "mock", "cheap-model")
	sp := delegate.New(store, map[string]provider.Provider{"mock": mock},
		func() (*tool.Registry, error) { return tool.NewRegistry(), nil },
		func() delegate.Parent {
			return delegate.Parent{Log: parent.Log, SessionID: parent.ID, ProvName: "mock", Model: "fallback-model", Router: router}
		})

	if _, err := sp.Spawn(context.Background(), builtin.TaskRequest{Prompt: "go"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("child never issued a request")
	}
	if reqs[0].Model != "cheap-model" {
		t.Errorf("child model = %q, want cheap-model", reqs[0].Model)
	}
}

// TestSpawnExplicitModelOverride covers the per-call override winning over the
// cheap tier.
func TestSpawnExplicitModelOverride(t *testing.T) {
	store := newStore(t)
	parent, err := store.Create("parent")
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	defer func() { _ = parent.Log.Close() }()

	mock := &provider.Mock{Events: provider.TextTurn("ok", "")}
	router := routing.Default().WithVendorModel(routing.Cheap, "mock", "cheap-model")
	sp := delegate.New(store, map[string]provider.Provider{"mock": mock},
		func() (*tool.Registry, error) { return tool.NewRegistry(), nil },
		func() delegate.Parent {
			return delegate.Parent{Log: parent.Log, SessionID: parent.ID, ProvName: "mock", Model: "fallback-model", Router: router}
		})

	if _, err := sp.Spawn(context.Background(), builtin.TaskRequest{Prompt: "go", Model: "explicit-model"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if reqs := mock.Requests(); len(reqs) == 0 || reqs[0].Model != "explicit-model" {
		t.Errorf("child model = %v, want explicit-model", mock.Requests())
	}
}

// TestSpawnUnknownProvider covers the error path: a missing provider is a real
// error the caller (the task tool) turns into a model-readable result.
func TestSpawnUnknownProvider(t *testing.T) {
	store := newStore(t)
	parent, err := store.Create("parent")
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	defer func() { _ = parent.Log.Close() }()

	sp := delegate.New(store, map[string]provider.Provider{},
		func() (*tool.Registry, error) { return tool.NewRegistry(), nil },
		func() delegate.Parent {
			return delegate.Parent{Log: parent.Log, SessionID: parent.ID, ProvName: "missing", Router: routing.Default()}
		})
	if _, err := sp.Spawn(context.Background(), builtin.TaskRequest{Prompt: "go"}); err == nil {
		t.Error("Spawn with unknown provider should error")
	}
}
