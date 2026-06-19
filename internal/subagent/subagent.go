package subagent

import (
	"fmt"
	"sort"
	"sync"

	"github.com/tonitienda/agent-smith/internal/budget"
	"github.com/tonitienda/agent-smith/schema"
)

// SubAgent is the public interface every built-in implements and the contract a
// third-party manifest is driven against (Appendix C.4). The lifecycle is
// init(scope) → observe (passive) → teardown(scope, state):
//
//   - Init is called when the Runner begins a scope the sub-agent is scheduled
//     for, to set up per-scope state.
//   - Observe is called for each block appended while the scope is open. It is
//     passive by construction: it MUST NOT call a model or perform I/O — it only
//     accumulates trace signals — so an enabled analyzer never slows the
//     interactive turn and a disabled one costs literally nothing.
//   - Teardown is called when the scope ends, off the hot path, with the context
//     slice to analyze; it returns its findings and the dollars it spent (0 for a
//     purely passive analyzer). This is the only place a sub-agent may use its
//     model tier, and the only place that spends against its budget cap.
//
// A SubAgent is stateful (Init/Observe accumulate per-scope state), so instances
// are never shared across sessions: the Registry stores a Factory and each
// session's Runner builds its own instances (see Factory, NewRunner). One Runner
// serves one session, so a sub-agent's methods are never called concurrently for
// different scopes by the framework.
type SubAgent interface {
	// Manifest returns the sub-agent's declaration (validated at registration).
	Manifest() Manifest
	// Init prepares per-scope state.
	Init(scope Scope)
	// Observe accumulates a trace signal from one block. No model calls, no I/O.
	Observe(block schema.Block)
	// Teardown analyzes the scope's context slice and returns its result.
	Teardown(scope Scope, slice []schema.Block) Result
}

// Factory builds a fresh SubAgent instance. The Registry stores factories, not
// instances, so every session's Runner instantiates its own sub-agents and a
// stateful sub-agent never shares mutable state across concurrent sessions. A
// factory must return instances with the same (stable) manifest each call.
type Factory func() SubAgent

// Result is what a teardown produces: the findings to record and the dollars the
// analysis spent, which the Runner charges against the sub-agent's per-session
// budget cap. A passive analyzer returns SpentUSD == 0 and so is never capped.
type Result struct {
	Findings []Finding
	SpentUSD float64
}

// Config is the per-sub-agent configuration block (Appendix C.3), read from the
// `subagents.<name>` config subtree. Every field is optional and overrides the
// manifest default when set, so enabling or disabling a sub-agent — or changing
// its model, schedule, or cap — is the one config line the §7.19 AC calls for.
type Config struct {
	// Enabled overrides the manifest's EnabledByDefault. A nil pointer means "not
	// configured" (keep the manifest default); a non-nil value forces on or off.
	Enabled *bool `json:"enabled,omitempty"`
	// Model overrides the model tier teardown analysis uses.
	Model string `json:"model,omitempty"`
	// Schedule overrides when teardown runs (teardown | session_end | rollup).
	Schedule Schedule `json:"schedule,omitempty"`
	// Mode is an opaque per-sub-agent mode string, passed through for the sub-agent
	// to interpret (e.g. "summary" vs "verbose").
	Mode string `json:"mode,omitempty"`
	// BudgetUSD overrides the manifest's per-session cap when > 0.
	BudgetUSD float64 `json:"budgetUSD,omitempty"`
}

// Warning is a non-fatal configuration problem — a config entry for an unknown
// sub-agent, or an unparsable schedule — surfaced so a misconfiguration is
// visible rather than silently dropped (the same tolerate-but-warn ethos as the
// hook loader).
type Warning struct{ Message string }

func (w Warning) String() string { return "subagents: " + w.Message }

// entry is a registered sub-agent: its validated manifest (cached so config and
// scheduling can be resolved without instantiating) and the factory that builds
// per-session instances.
type entry struct {
	manifest Manifest
	factory  Factory
}

// Registry holds the registered sub-agent factories (built-in and declarative
// third-party) keyed by name, plus the per-sub-agent config overlay. It validates
// every manifest the same way, so a third-party sub-agent loads through exactly
// the same path as a built-in (§7.19 AC). It is built once before any session and
// then read concurrently: registration happens up front and the live snapshot a
// Runner takes only reads.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]entry
	cfg     map[string]Config
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{entries: map[string]entry{}, cfg: map[string]Config{}}
}

// Register adds a built-in sub-agent by its factory. It builds one instance to
// validate the manifest and rejects a duplicate name, so a misdeclared built-in
// fails at startup rather than mid-session. The factory is what a Runner calls
// per session to get its own instance.
func (r *Registry) Register(f Factory) error {
	if f == nil {
		return fmt.Errorf("subagent: nil factory")
	}
	a := f()
	if a == nil {
		return fmt.Errorf("subagent: factory returned nil")
	}
	m := a.Manifest()
	if err := m.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.entries[m.Name]; dup {
		return fmt.Errorf("subagent %q: already registered", m.Name)
	}
	r.entries[m.Name] = entry{manifest: m, factory: f}
	return nil
}

// LoadManifest registers a third-party sub-agent from a declarative manifest
// (D9): the manifest is parsed and validated, then a factory yielding a passive
// declarative sub-agent is registered through the same path as a built-in. The
// declarative sub-agent executes no third-party code — this is the enforced
// declarative boundary: a third-party sub-agent is data, never logic.
func (r *Registry) LoadManifest(data []byte) error {
	m, err := ParseManifest(data)
	if err != nil {
		return err
	}
	return r.Register(func() SubAgent { return declarative{manifest: m} })
}

// Configure installs the per-sub-agent config overlay (typically from Load),
// replacing any prior overlay. Entries naming an unregistered sub-agent are
// returned as warnings rather than rejected, so a config that mentions a sub-
// agent from a plugin that did not load is tolerated.
func (r *Registry) Configure(cfg map[string]Config) []Warning {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg = map[string]Config{}
	var warnings []Warning
	names := make([]string, 0, len(cfg))
	for name := range cfg {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic warning order
	for _, name := range names {
		c := cfg[name]
		if _, known := r.entries[name]; !known {
			warnings = append(warnings, Warning{fmt.Sprintf("config for unknown sub-agent %q ignored", name)})
			continue
		}
		if c.Schedule != "" && !validSchedules[c.Schedule] {
			warnings = append(warnings, Warning{fmt.Sprintf("sub-agent %q: unknown schedule %q; using manifest default", name, c.Schedule)})
			c.Schedule = ""
		}
		r.cfg[name] = c
	}
	return warnings
}

// configDecoder is the slice of config the loader reads from; config's *Config
// satisfies it via Decode. Kept as an interface so this package does not import
// config (config consumers depend on config, not the reverse) — the same pattern
// as the hook loader.
type configDecoder interface {
	Decode(path string, v any) (bool, error)
}

// Load reads the `subagents` map out of cfg and applies it as the overlay,
// returning any warnings. A missing key is not an error: sub-agents then run on
// their manifest defaults.
func (r *Registry) Load(cfg configDecoder) ([]Warning, error) {
	var m map[string]Config
	ok, err := cfg.Decode("subagents", &m)
	if err != nil {
		return nil, fmt.Errorf("subagent: load config: %w", err)
	}
	if !ok {
		return nil, nil
	}
	return r.Configure(m), nil
}

// Effective returns the manifest for name with the config overlay applied
// (enabled state, model, schedule, budget), and whether the sub-agent is
// registered. It is how a consumer reads the resolved truth for one sub-agent
// without instantiating it.
func (r *Registry) Effective(name string) (Manifest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[name]
	if !ok {
		return Manifest{}, false
	}
	return r.overlay(e.manifest), true
}

// overlay applies the per-sub-agent config to a manifest. The caller holds the
// lock.
func (r *Registry) overlay(m Manifest) Manifest {
	c, ok := r.cfg[m.Name]
	if !ok {
		return m
	}
	if c.Enabled != nil {
		m.EnabledByDefault = *c.Enabled
	}
	if c.Model != "" {
		m.ModelTier = c.Model
	}
	if c.Schedule != "" {
		m.Schedule = c.Schedule
	}
	if c.BudgetUSD > 0 {
		m.BudgetUSD = c.BudgetUSD
	}
	return m
}

// active is one enabled sub-agent resolved for a session: a fresh instance and
// its effective manifest, snapshotted once at Runner construction so the live
// path needs no registry lock, sort, or allocation.
type active struct {
	agent    SubAgent
	manifest Manifest
}

// snapshot instantiates every enabled sub-agent (after the config overlay) for a
// session, returning a fresh instance and its effective manifest per sub-agent,
// in name order. A sub-agent scheduled AtRollup is excluded: rollup is a cross-
// session concern (AS-050), not a live-session one. This is the single point
// where factories are called for a session, so a Runner's instances are its own.
func (r *Registry) snapshot() []active {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []active
	for _, name := range names {
		e := r.entries[name]
		m := r.overlay(e.manifest)
		if !m.EnabledByDefault {
			continue
		}
		if scheduleScope(m.effectiveSchedule()) == ScopeKind("") {
			continue // AtRollup / unrecognized: never runs in a live session.
		}
		out = append(out, active{agent: e.factory(), manifest: m})
	}
	return out
}

// scheduleScope maps a schedule to the scope kind whose teardown drives it. A
// span-teardown schedule runs at a span scope; session_end at a session scope;
// rollup runs in neither (it is a cross-session concern), so it maps to a value
// no live scope uses.
func scheduleScope(s Schedule) ScopeKind {
	switch s {
	case AtSessionEnd:
		return SessionScope
	case AtTeardown:
		return SpanScope
	default: // AtRollup and anything unrecognized never fire in a live session.
		return ScopeKind("")
	}
}

// Runner drives the lifecycle (§7.19): Begin inits the enabled sub-agents for a
// scope, Observe fans each appended block out to them passively, and End tears
// them down off the hot path, enforcing each sub-agent's per-session budget cap
// and recording findings into the Store. One Runner serves one session.
//
// The enabled sub-agents (their own instances) and their schedule buckets are
// resolved once, at construction, from a registry snapshot: the registry's config
// is static for a session, so the live path holds no registry lock and does no
// sorting or allocation. Budget caps are per sub-agent per session: the Runner
// tracks each sub-agent's cumulative spend across every teardown and consults the
// AS-041 budget Guard before running a teardown, skipping it once the cap is
// reached — one decision rule (budget.Guard) for sessions and sub-agents alike.
type Runner struct {
	store   Store
	session string

	observe  []active               // every enabled sub-agent (for the per-block fan-out)
	teardown map[ScopeKind][]active // enabled sub-agents bucketed by the scope that tears them down

	mu    sync.Mutex
	spent map[string]float64 // per-sub-agent cumulative spend this session
}

// NewRunner builds a Runner for one session over a registry and a findings Store,
// instantiating its own sub-agent instances from the registry's factories. A nil
// store defaults to a fresh in-memory store.
func NewRunner(reg *Registry, store Store, session string) *Runner {
	if store == nil {
		store = NewMemStore()
	}
	rn := &Runner{
		store:    store,
		session:  session,
		teardown: map[ScopeKind][]active{},
		spent:    map[string]float64{},
	}
	if reg != nil {
		for _, a := range reg.snapshot() {
			rn.observe = append(rn.observe, a)
			k := scheduleScope(a.manifest.effectiveSchedule())
			rn.teardown[k] = append(rn.teardown[k], a)
		}
	}
	return rn
}

// Store returns the findings store the Runner records into.
func (r *Runner) Store() Store { return r.store }

// Begin opens a scope: it calls Init on every enabled sub-agent scheduled to tear
// down at this scope kind. Scope.Session is forced to the Runner's session so a
// caller cannot misattribute findings.
func (r *Runner) Begin(scope Scope) {
	scope.Session = r.session
	for _, a := range r.teardown[scope.Kind] {
		a.agent.Init(scope)
	}
}

// Observe fans one appended block out to every enabled sub-agent (across both
// scope kinds, since a session-scoped analyzer observes spans too). It is the
// only per-block work and is passive by contract; the enabled set is cached, so
// this is a lock-free, allocation-free walk that stays out of the interactive
// turn's critical section. No enabled sub-agents makes it a cheap no-op.
func (r *Runner) Observe(block schema.Block) {
	for _, a := range r.observe {
		a.agent.Observe(block)
	}
}

// End closes a scope: for each enabled sub-agent scheduled at this scope kind it
// checks the per-session budget cap, and if there is room runs Teardown over the
// slice, records the findings, and charges the spend. A sub-agent already at or
// over its cap is skipped (its analysis does not run), which is how the per-sub-
// agent cap is enforced. This is the batched, off-hot-path step the §7.19 AC
// requires; a caller may invoke it from a goroutine — the Store and the spend
// ledger are concurrency-safe.
func (r *Runner) End(scope Scope, slice []schema.Block) {
	scope.Session = r.session
	for _, a := range r.teardown[scope.Kind] {
		guard := budget.Guard{LimitUSD: a.manifest.BudgetUSD}

		r.mu.Lock()
		alreadySpent := r.spent[a.manifest.Name]
		r.mu.Unlock()

		// Enforce the cap before spending: a sub-agent at or past its ceiling does
		// not run another teardown this session.
		if guard.Check(alreadySpent) == budget.Halt {
			continue
		}

		res := a.agent.Teardown(scope, slice)
		for _, f := range res.Findings {
			f.SubAgent = a.manifest.Name
			f.Session = scope.Session
			f.Span = scope.Span
			r.store.Record(f)
		}
		if res.SpentUSD != 0 {
			r.mu.Lock()
			r.spent[a.manifest.Name] += res.SpentUSD
			r.mu.Unlock()
		}
	}
}

// SpentUSD reports a sub-agent's cumulative teardown spend this session, for a
// cost view (AS-020/AS-045).
func (r *Runner) SpentUSD(name string) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.spent[name]
}

// declarative is the passive sub-agent a third-party manifest is wrapped in: it
// carries the manifest and does nothing on the lifecycle calls. It exists to
// enforce the D9 declarative boundary — a third-party sub-agent contributes a
// declaration only, never executable behavior — while still loading, configuring,
// and scheduling through the same registry and Runner as a built-in. Being
// stateless, it is safe to reuse, but the factory yields a fresh value anyway.
type declarative struct{ manifest Manifest }

func (d declarative) Manifest() Manifest                    { return d.manifest }
func (d declarative) Init(Scope)                            {}
func (d declarative) Observe(schema.Block)                  {}
func (d declarative) Teardown(Scope, []schema.Block) Result { return Result{} }
