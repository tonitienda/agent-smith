package tool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tonitienda/agent-smith/schema"
)

// stubTool is a minimal Tool for registry tests.
func stubTool(name string) Tool {
	return Func{
		Spec: Def{Name: name, Description: name + " tool"},
		Fn: func(context.Context, json.RawMessage) (Output, error) {
			return Output{Text: "ok"}, nil
		},
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(stubTool("read")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got, ok := r.Get("read"); !ok || got.Def().Name != "read" {
		t.Fatalf("Get(read) = %v, %v; want the read tool", got, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatalf("Get(missing) reported found")
	}
	if r.Len() != 1 {
		t.Fatalf("Len = %d, want 1", r.Len())
	}
}

func TestRegistryRejectsDuplicateAndEmptyName(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(stubTool("read")); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.Register(stubTool("read")); err == nil {
		t.Fatalf("duplicate Register: want error, got nil")
	}
	if err := r.Register(stubTool("")); err == nil {
		t.Fatalf("empty-name Register: want error, got nil")
	}
	if err := r.Register(nil); err == nil {
		t.Fatalf("nil Register: want error, got nil")
	}
}

func TestRegistryDefsSortedAndProviderShape(t *testing.T) {
	r := NewRegistry()
	// Register out of order; Defs/ProviderDefs must come back name-sorted so the
	// request prefix is stable for caching (AS-011).
	for _, n := range []string{"write", "glob", "read"} {
		if err := r.Register(stubTool(n)); err != nil {
			t.Fatalf("Register %q: %v", n, err)
		}
	}
	defs := r.Defs()
	gotOrder := []string{defs[0].Name, defs[1].Name, defs[2].Name}
	want := []string{"glob", "read", "write"}
	for i := range want {
		if gotOrder[i] != want[i] {
			t.Fatalf("Defs order = %v, want %v", gotOrder, want)
		}
	}

	pdefs := r.ProviderDefs()
	if len(pdefs) != 3 {
		t.Fatalf("ProviderDefs len = %d, want 3", len(pdefs))
	}
	for _, pd := range pdefs {
		if pd.Kind != schema.ToolKindClient {
			t.Fatalf("ProviderDef %q kind = %q, want %q", pd.Name, pd.Kind, schema.ToolKindClient)
		}
	}
}

func TestMustRegisterPanicsOnDuplicate(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(stubTool("read"))
	defer func() {
		if recover() == nil {
			t.Fatalf("MustRegister duplicate: want panic")
		}
	}()
	r.MustRegister(stubTool("read"))
}
