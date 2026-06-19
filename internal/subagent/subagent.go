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

// Registry holds the registered sub-agents (built-in and declarative third-party)
// keyed by name, plus the per-sub-agent config overlay. It validates every
// manifest the same way, so a third-party sub-agent loads through exactly the
// same path as a built-in (§7.19 AC). It is built once before a session and read
// concurrently during one, so registration happens up front and the live methods
// only read.
type Registry struct {
	mu     sync.RWMutex
	agents map[string]SubAgent
	cfg    map[string]Config
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{agents: map[string]SubAgent{}, cfg: map[string]Config{}}
}

// Register adds a built-in sub-agent. It validates the manifest and rejects a
// duplicate name, so a misdeclared built-in fails at startup rather than mid-
// session.
func (r *Registry) Register(a SubAgent) error {
	if a == nil {
		return fmt.Errorf("subagent: nil sub-agent")
	}
	m := a.Manifest()
	if err := m.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.agents[m.Name]; dup {
		return fmt.Errorf("subagent %q: already registered", m.Name)
	}
	r.agents[m.Name] = a
	return nil
}

// LoadManifest registers a third-party sub-agent from a declarative manifest
// (D9): the manifest is parsed and validated, then wrapped in a passive
// declarative sub-agent that runs through the same lifecycle as a built-in but
// executes no third-party code. This is the enforced declarative boundary — a
// third-party sub-agent is data, never logic.
func (r *Registry) LoadManifest(data []byte) error {
	m, err := ParseManifest(data)
	if err != nil {
		return err
	}
	return r.Register(declarative{manifest: m})
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
		if _, known := r.agents[name]; !known {
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
// registered. It is how a consumer reads the resolved truth for one sub-agent.
func (r *Registry) Effective(name string) (Manifest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[name]
	if !ok {
		return Manifest{}, false
	}
	return r.overlay(a.Manifest()), true
}

// overlay applies the per-sub-agent config to a manifest. The caller holds the
// lock (or is building from an immutable snapshot).
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

// enabledFor returns the registered sub-agents that are enabled (after the config
// overlay) and scheduled to tear down at scopeKind, in name order. A sub-agent
// scheduled AtSessionEnd runs only on a session scope; one scheduled AtTeardown
// runs on a span scope; AtRollup never runs in a live session.
func (r *Registry) enabledFor(kind ScopeKind) []SubAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.agents))
	for name := range r.agents {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []SubAgent
	for _, name := range names {
		a := r.agents[name]
		m := r.overlay(a.Manifest())
		if !m.EnabledByDefault {
			continue
		}
		if scheduleScope(m.effectiveSchedule()) == kind {
			out = append(out, a)
		}
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
// Budget caps are per sub-agent per session: the Runner tracks each sub-agent's
// cumulative spend across every teardown in the session and consults the AS-041
// budget Guard before running a teardown, skipping it once the cap is reached —
// so one decision rule (budget.Guard) governs sessions and sub-agents alike.
type Runner struct {
	reg     *Registry
	store   Store
	session string

	mu    sync.Mutex
	spent map[string]float64 // per-sub-agent cumulative spend this session
}

// NewRunner builds a Runner for one session over a registry and a findings Store.
// A nil store defaults to a fresh in-memory store.
func NewRunner(reg *Registry, store Store, session string) *Runner {
	if store == nil {
		store = NewMemStore()
	}
	return &Runner{reg: reg, store: store, session: session, spent: map[string]float64{}}
}

// Store returns the findings store the Runner records into.
func (r *Runner) Store() Store { return r.store }

// Begin opens a scope: it calls Init on every enabled sub-agent scheduled to tear
// down at this scope kind. Scope.Session is forced to the Runner's session so a
// caller cannot misattribute findings.
func (r *Runner) Begin(scope Scope) {
	scope.Session = r.session
	for _, a := range r.reg.enabledFor(scope.Kind) {
		a.Init(scope)
	}
}

// Observe fans one appended block out to every enabled sub-agent (across both
// scope kinds, since a session-scoped analyzer observes spans too). It is the
// only per-block work and is passive by contract, so it stays off the cost path
// and out of the interactive turn's critical section. A nil registry or no
// enabled sub-agents makes it a cheap no-op.
func (r *Runner) Observe(block schema.Block) {
	for _, a := range r.reg.enabledSubAgents() {
		a.Observe(block)
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
	for _, a := range r.reg.enabledFor(scope.Kind) {
		m := r.reg.overlay(a.Manifest())
		guard := budget.Guard{LimitUSD: m.BudgetUSD}

		r.mu.Lock()
		alreadySpent := r.spent[m.Name]
		r.mu.Unlock()

		// Enforce the cap before spending: a sub-agent at or past its ceiling does
		// not run another teardown this session.
		if guard.Check(alreadySpent) == budget.Halt {
			continue
		}

		res := a.Teardown(scope, slice)
		for _, f := range res.Findings {
			f.SubAgent = m.Name
			f.Session = scope.Session
			f.Span = scope.Span
			r.store.Record(f)
		}
		if res.SpentUSD != 0 {
			r.mu.Lock()
			r.spent[m.Name] += res.SpentUSD
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

// enabledSubAgents returns every enabled sub-agent regardless of schedule, for
// the per-block Observe fan-out, in name order.
func (r *Registry) enabledSubAgents() []SubAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.agents))
	for name := range r.agents {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []SubAgent
	for _, name := range names {
		a := r.agents[name]
		if r.overlay(a.Manifest()).EnabledByDefault {
			out = append(out, a)
		}
	}
	return out
}

// declarative is the passive sub-agent a third-party manifest is wrapped in: it
// carries the manifest and does nothing on the lifecycle calls. It exists to
// enforce the D9 declarative boundary — a third-party sub-agent contributes a
// declaration only, never executable behavior — while still loading, configuring,
// and scheduling through the same registry and Runner as a built-in.
type declarative struct{ manifest Manifest }

func (d declarative) Manifest() Manifest                    { return d.manifest }
func (d declarative) Init(Scope)                            {}
func (d declarative) Observe(schema.Block)                  {}
func (d declarative) Teardown(Scope, []schema.Block) Result { return Result{} }
