package subagent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonitienda/agent-smith/internal/config"
	"github.com/tonitienda/agent-smith/schema"
)

// spyAgent is a built-in test sub-agent that records its lifecycle calls and
// emits a configurable result from teardown, so the tests can assert exactly when
// (and whether) the framework drives it and what it spends.
type spyAgent struct {
	manifest  Manifest
	inits     int
	observes  int
	teardowns int
	result    Result
}

func (a *spyAgent) Manifest() Manifest { return a.manifest }
func (a *spyAgent) Init(Scope)         { a.inits++ }
func (a *spyAgent) Observe(schema.Block) {
	a.observes++
}
func (a *spyAgent) Teardown(scope Scope, slice []schema.Block) Result {
	a.teardowns++
	return a.result
}

func boolp(b bool) *bool { return &b }

func block(text string) schema.Block {
	return schema.Block{Kind: schema.KindText, Role: schema.RoleAssistant, Text: &schema.TextBody{Text: text}}
}

func TestManifestValidate(t *testing.T) {
	cases := []struct {
		name string
		m    Manifest
		ok   bool
	}{
		{"ok", Manifest{Name: "a", Kind: KindAnalyzer}, true},
		{"defaults filled", Manifest{Name: "a", Kind: KindAnalyzer, Schedule: "", Scope: ""}, true},
		{"no name", Manifest{Kind: KindAnalyzer}, false},
		{"bad kind", Manifest{Name: "a", Kind: "watcher"}, false},
		{"bad schedule", Manifest{Name: "a", Kind: KindAnalyzer, Schedule: "whenever"}, false},
		{"bad scope", Manifest{Name: "a", Kind: KindAnalyzer, Scope: "galaxy"}, false},
		{"bad perm", Manifest{Name: "a", Kind: KindAnalyzer, Permissions: []Permission{"rm -rf"}}, false},
		{"negative budget", Manifest{Name: "a", Kind: KindAnalyzer, BudgetUSD: -1}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.m.Validate()
			if c.ok && err != nil {
				t.Fatalf("want valid, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatalf("want invalid, got nil")
			}
		})
	}
}

// AC: a third-party declarative manifest loads through the same registry as
// built-ins — and only as data (no code path).
func TestLoadManifestDeclarative(t *testing.T) {
	reg := NewRegistry()
	data := []byte(`{"name":"vendor-x","kind":"analyzer","schedule":"session_end","enabledByDefault":true,"emits":["note"]}`)
	if err := reg.LoadManifest(data); err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	m, ok := reg.Effective("vendor-x")
	if !ok {
		t.Fatal("declarative sub-agent not registered")
	}
	if m.Schedule != AtSessionEnd || !m.EnabledByDefault {
		t.Fatalf("unexpected effective manifest: %+v", m)
	}

	// A manifest with an unknown field is rejected: declarative input is strict.
	if err := reg.LoadManifest([]byte(`{"name":"y","kind":"analyzer","exec":"./evil"}`)); err == nil {
		t.Fatal("want error for unknown manifest field, got nil")
	}
}

// TestDeclarativeBoundaryNoOp is the behavioral half of the D9 declarative-only
// boundary guard (AS-112, plugin-trust.md §4.3): a third-party manifest is data,
// never code, so a LoadManifest'd sub-agent must do *nothing* on every lifecycle
// call — no findings, no spend — no matter what the manifest declares.
//
// IMPORTANT: this assertion ("zero spend, zero findings") is valid only while
// declarative plugins are entirely non-functional (the v1 line). When a
// framework-side model-execution path for declarative plugins lands (running a
// plugin's prompt on the user's behalf), these sub-agents *will* emit findings
// and incur spend — at that point re-parameterize this guard to "no arbitrary
// third-party code runs; spend is bounded by the budget cap" rather than deleting
// it. Update it deliberately, do not drop it in confusion.
func TestDeclarativeBoundaryNoOp(t *testing.T) {
	manifests := []string{
		`{"name":"minimal","kind":"analyzer"}`,
		`{"name":"session","kind":"analyzer","schedule":"session_end","scope":"session","enabledByDefault":true,"emits":["note"]}`,
		`{"name":"budgeted","kind":"analyzer","modelTier":"cheap","budgetUSD":5,"emits":["a","b"]}`,
		`{"name":"permissive","kind":"analyzer","permissions":["read_transcript","propose_edit"]}`,
	}
	slice := []schema.Block{block("one"), block("two")}
	for _, data := range manifests {
		reg := NewRegistry()
		if err := reg.LoadManifest([]byte(data)); err != nil {
			t.Fatalf("load %s: %v", data, err)
		}
		m, _ := ParseManifest([]byte(data))
		// Reach through the registry factory to the actual instance the Runner would
		// drive — that is the value whose lifecycle must be inert.
		sa := reg.entries[m.Name].factory()
		if _, isDecl := sa.(declarative); !isDecl {
			t.Fatalf("%s: loaded sub-agent is %T, not the declarative wrapper", m.Name, sa)
		}
		scope := Scope{Kind: m.effectiveScope(), Session: "s1", Span: "1"}
		sa.Init(scope)
		for _, b := range slice {
			sa.Observe(b)
		}
		got := sa.Teardown(scope, slice)
		if len(got.Findings) != 0 || got.SpentUSD != 0 {
			t.Fatalf("%s: declarative sub-agent was not inert: findings=%d spent=%.4f", m.Name, len(got.Findings), got.SpentUSD)
		}
	}
}

// AC: enabling/disabling is one config line; a disabled analyzer is never driven.
func TestConfigEnableDisable(t *testing.T) {
	reg := NewRegistry()
	a := &spyAgent{manifest: Manifest{Name: "labeler", Kind: KindAnalyzer, Schedule: AtTeardown, EnabledByDefault: true}}
	if err := reg.Register(func() SubAgent { return a }); err != nil {
		t.Fatal(err)
	}

	// Default-on: the registry reports it enabled.
	if m, _ := reg.Effective("labeler"); !m.EnabledByDefault {
		t.Fatal("want enabled by default")
	}

	// One config line disables it.
	warns := reg.Configure(map[string]Config{"labeler": {Enabled: boolp(false)}})
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if m, _ := reg.Effective("labeler"); m.EnabledByDefault {
		t.Fatal("config did not disable")
	}

	// A disabled analyzer is never inited, observed, or torn down → zero cost.
	rn := NewRunner(reg, nil, "s1")
	scope := Scope{Kind: SpanScope, Span: "span-1"}
	rn.Begin(scope)
	rn.Observe(block("hello"))
	rn.End(scope, []schema.Block{block("hello")})
	if a.inits != 0 || a.observes != 0 || a.teardowns != 0 {
		t.Fatalf("disabled agent was driven: %+v", a)
	}
}

// AC: an enabled analyzer runs without slowing the interactive turn — observe is
// the only per-block work and teardown happens at the scope boundary, not inline.
func TestLifecycleOrder(t *testing.T) {
	reg := NewRegistry()
	a := &spyAgent{
		manifest: Manifest{Name: "labeler", Kind: KindAnalyzer, Schedule: AtTeardown, EnabledByDefault: true, Emits: []string{"topic"}},
		result:   Result{Findings: []Finding{{Kind: "topic", Summary: "auth"}}},
	}
	if err := reg.Register(func() SubAgent { return a }); err != nil {
		t.Fatal(err)
	}
	rn := NewRunner(reg, nil, "s1")
	scope := Scope{Kind: SpanScope, Span: "span-1"}

	rn.Begin(scope)
	if a.inits != 1 {
		t.Fatalf("want 1 init, got %d", a.inits)
	}
	// Observe is cheap and does not analyze: no teardown, no findings yet.
	rn.Observe(block("a"))
	rn.Observe(block("b"))
	if a.observes != 2 || a.teardowns != 0 {
		t.Fatalf("observe phase wrong: observes=%d teardowns=%d", a.observes, a.teardowns)
	}
	if got := rn.Store().Findings("s1"); len(got) != 0 {
		t.Fatalf("findings recorded before teardown: %v", got)
	}

	// Teardown at the boundary records the finding, attributed to the scope.
	rn.End(scope, []schema.Block{block("a"), block("b")})
	if a.teardowns != 1 {
		t.Fatalf("want 1 teardown, got %d", a.teardowns)
	}
	got := rn.Store().Findings("s1")
	if len(got) != 1 || got[0].SubAgent != "labeler" || got[0].Span != "span-1" || got[0].Session != "s1" {
		t.Fatalf("finding not attributed: %+v", got)
	}
}

// AC: budget caps are enforced per sub-agent per session.
func TestBudgetCapEnforced(t *testing.T) {
	reg := NewRegistry()
	a := &spyAgent{
		manifest: Manifest{Name: "pricey", Kind: KindAnalyzer, Schedule: AtTeardown, EnabledByDefault: true, BudgetUSD: 0.10, ModelTier: "cheap"},
		result:   Result{SpentUSD: 0.06, Findings: []Finding{{Kind: "x", Summary: "run"}}},
	}
	if err := reg.Register(func() SubAgent { return a }); err != nil {
		t.Fatal(err)
	}
	rn := NewRunner(reg, nil, "s1")

	// Span 1: spent 0 < cap → runs, charges 0.06.
	rn.End(Scope{Kind: SpanScope, Span: "1"}, nil)
	// Span 2: spent 0.06 < 0.10 → runs, charges to 0.12.
	rn.End(Scope{Kind: SpanScope, Span: "2"}, nil)
	// Span 3: spent 0.12 >= cap → skipped.
	rn.End(Scope{Kind: SpanScope, Span: "3"}, nil)

	if a.teardowns != 2 {
		t.Fatalf("want 2 teardowns before cap, got %d", a.teardowns)
	}
	if got := rn.SpentUSD("pricey"); got != 0.12 {
		t.Fatalf("want spend 0.12, got %v", got)
	}
	if got := len(rn.Store().Findings("s1")); got != 2 {
		t.Fatalf("want 2 findings (capped run produced none), got %d", got)
	}
}

// Schedule decides which scope boundary drives a sub-agent: a session_end
// analyzer does not run at a span teardown, and vice versa.
func TestScheduleScoping(t *testing.T) {
	reg := NewRegistry()
	span := &spyAgent{manifest: Manifest{Name: "span", Kind: KindAnalyzer, Schedule: AtTeardown, Scope: SpanScope, EnabledByDefault: true}}
	sess := &spyAgent{manifest: Manifest{Name: "sess", Kind: KindAnalyzer, Schedule: AtSessionEnd, Scope: SessionScope, EnabledByDefault: true}}
	roll := &spyAgent{manifest: Manifest{Name: "roll", Kind: KindAnalyzer, Schedule: AtRollup, EnabledByDefault: true}}
	for _, a := range []*spyAgent{span, sess, roll} {
		if err := reg.Register(func() SubAgent { return a }); err != nil {
			t.Fatal(err)
		}
	}
	rn := NewRunner(reg, nil, "s1")

	rn.End(Scope{Kind: SpanScope, Span: "1"}, nil)
	if span.teardowns != 1 || sess.teardowns != 0 || roll.teardowns != 0 {
		t.Fatalf("span boundary drove wrong agents: span=%d sess=%d roll=%d", span.teardowns, sess.teardowns, roll.teardowns)
	}
	rn.End(Scope{Kind: SessionScope}, nil)
	if sess.teardowns != 1 || roll.teardowns != 0 {
		t.Fatalf("session boundary wrong: sess=%d roll=%d", sess.teardowns, roll.teardowns)
	}
}

// Each Runner instantiates its own sub-agents from the registry factory, so two
// concurrent sessions never share mutable state (the Gemini concurrency fix).
func TestPerSessionInstancesAreIsolated(t *testing.T) {
	reg := NewRegistry()
	var created []*spyAgent
	if err := reg.Register(func() SubAgent {
		a := &spyAgent{manifest: Manifest{Name: "iso", Kind: KindAnalyzer, Schedule: AtTeardown, EnabledByDefault: true}}
		created = append(created, a)
		return a
	}); err != nil {
		t.Fatal(err)
	}
	// created[0] is the validation instance built at Register; each NewRunner adds
	// one more, distinct, instance.
	r1 := NewRunner(reg, nil, "s1")
	r2 := NewRunner(reg, nil, "s2")
	r1.Observe(block("x"))
	r1.Observe(block("y"))
	r2.Observe(block("z"))

	if len(created) != 3 {
		t.Fatalf("want 3 instances (validation + one per runner), got %d", len(created))
	}
	if created[0].observes != 0 {
		t.Fatalf("validation instance was driven: %d", created[0].observes)
	}
	if created[1].observes != 2 || created[2].observes != 1 {
		t.Fatalf("instances not isolated: r1=%d r2=%d", created[1].observes, created[2].observes)
	}
}

func TestConfigWarnsUnknownAgent(t *testing.T) {
	reg := NewRegistry()
	warns := reg.Configure(map[string]Config{"ghost": {Enabled: boolp(true)}})
	if len(warns) != 1 {
		t.Fatalf("want 1 warning for unknown sub-agent, got %v", warns)
	}
}

func TestRegisterDuplicate(t *testing.T) {
	reg := NewRegistry()
	a := &spyAgent{manifest: Manifest{Name: "dup", Kind: KindAnalyzer}}
	if err := reg.Register(func() SubAgent { return a }); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(func() SubAgent { return a }); err == nil {
		t.Fatal("want duplicate-name error, got nil")
	}
}

// loadConfig builds a real *config.Config from a JSON document written to a temp
// file — the production read path — so Load is exercised against the genuine
// Decode collaborator rather than a hand-written double. body is the full config
// object (e.g. `{"subagents":{...}}`); an empty body yields a config with no
// `subagents` key.
func loadConfig(t *testing.T, body string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if body == "" {
		body = "{}"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	layer, err := config.FileLayer("project", path)
	if err != nil {
		t.Fatalf("FileLayer: %v", err)
	}
	return config.New(layer)
}

func TestLoadFromConfig(t *testing.T) {
	reg := NewRegistry()
	a := &spyAgent{manifest: Manifest{Name: "labeler", Kind: KindAnalyzer, EnabledByDefault: false}}
	if err := reg.Register(func() SubAgent { return a }); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, `{"subagents":{"labeler":{"enabled":true,"budgetUSD":0.5}}}`)
	warns, err := reg.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	m, _ := reg.Effective("labeler")
	if !m.EnabledByDefault || m.BudgetUSD != 0.5 {
		t.Fatalf("config overlay not applied: %+v", m)
	}

	// A missing key is not an error and changes nothing.
	if _, err := reg.Load(loadConfig(t, "")); err != nil {
		t.Fatalf("missing key should be no-op, got %v", err)
	}

	// A nil config is also a no-op, leaving manifest defaults in place.
	if warns, err := reg.Load(nil); err != nil || len(warns) != 0 {
		t.Fatalf("nil config should be no-op: warns=%v err=%v", warns, err)
	}
}
