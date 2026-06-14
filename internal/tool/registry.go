package tool

import (
	"fmt"
	"sort"
	"sync"

	"github.com/tonitienda/agent-smith/internal/provider"
)

// Registry holds the tools available to the model and renders their definitions
// for a provider request. It is safe for concurrent use: registration takes a
// write lock, lookups and listing take a read lock, so the loop may register
// tools at startup and read definitions per turn without coordination.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds t to the registry, keyed by its Def().Name. It rejects a tool
// with an empty name or a name already registered: names are the model-facing
// identity and must be unique, so a silent overwrite would let one tool shadow
// another. The error names the conflict so misconfiguration is obvious at
// startup.
func (r *Registry) Register(t Tool) error {
	if t == nil {
		return fmt.Errorf("tool: cannot register a nil tool")
	}
	name := t.Def().Name
	if name == "" {
		return fmt.Errorf("tool: cannot register a tool with an empty name")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.tools[name]; dup {
		return fmt.Errorf("tool: %q is already registered", name)
	}
	r.tools[name] = t
	return nil
}

// MustRegister is Register that panics on error. It suits package-init or
// startup wiring where a duplicate or unnamed tool is a programming bug.
func (r *Registry) MustRegister(t Tool) {
	if err := r.Register(t); err != nil {
		panic(err)
	}
}

// Get returns the tool registered under name, and whether one was found.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Len reports how many tools are registered.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// Defs returns every registered tool's definition, sorted by name. The stable
// order matters for prefix-stable prompt assembly and caching (AS-011): the tool
// list is part of the request prefix, so a deterministic order keeps unchanged
// turns byte-identical.
func (r *Registry) Defs() []Def {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]Def, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Def())
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs
}

// ProviderDefs returns the registered tools as provider.ToolDef values for a
// turn's request, in the same stable name order as Defs.
func (r *Registry) ProviderDefs() []provider.ToolDef {
	defs := r.Defs()
	out := make([]provider.ToolDef, len(defs))
	for i, d := range defs {
		out[i] = d.ProviderDef()
	}
	return out
}
