// Package permission implements Agent Smith's approval model (AS-016, PRD
// Decision Log D9): the policy that decides, before any tool call runs, whether
// it may proceed. It is the substance behind the tool runtime's permission hook
// (AS-013) — a Policy here produces the tool.PermissionFunc the Runtime invokes
// for every execution, so no tool runs without passing this gate.
//
// Three modes, applied per tool (D9):
//
//   - auto      — every call runs; no prompt.
//   - allowlist — a call matching an allow-rule runs; everything else prompts.
//   - ask       — every call prompts (allow-rules are not auto-applied).
//
// The mode for a call is its per-tool override (Config.Tools) when set, else the
// session default (Config.DefaultMode), else ModeAsk — the safe default. The
// prompt path is delegated to the active face through an Asker (the TUI prompt,
// AS-024); when no Asker is wired the prompt resolves to a denial with a
// model-readable reason, so a non-interactive run never silently blocks waiting
// for input. A user's "always allow this" at prompt time appends a rule to the
// allowlist (in memory, and to the project config when a Persister is set), so
// the approval sticks for the rest of the session and future ones.
//
// Denials return a structured Reason that the Runtime surfaces to the model as
// the tool_result error, so the model learns why a call was refused and can
// adjust rather than retry blindly.
//
// The security posture this model enforces — and its explicitly documented V1
// limits (no OS sandbox, no prompt-injection defense) — is stated in
// docs/SECURITY.md, per D0 (punts are documented, never silent).
package permission

import (
	"context"
	"sync"

	"github.com/tonitienda/agent-smith/internal/tool"
)

// Mode is a per-tool (or default) approval policy. See the package doc for the
// three modes' behavior.
type Mode string

const (
	// ModeAsk prompts for every call. Allow-rules are not auto-applied; this is
	// the conservative default when no mode is configured.
	ModeAsk Mode = "ask"
	// ModeAllowlist runs calls matching an allow-rule and prompts for the rest.
	ModeAllowlist Mode = "allowlist"
	// ModeAuto runs every call without prompting.
	ModeAuto Mode = "auto"
)

// valid reports whether m names a known mode, tolerating case and surrounding
// whitespace ("Auto", " auto "). An unknown or empty mode falls back to ModeAsk
// wherever a mode is resolved, so misconfiguration fails safe.
func (m Mode) valid() bool {
	switch normalizeMode(m) {
	case ModeAsk, ModeAllowlist, ModeAuto:
		return true
	default:
		return false
	}
}

// Request describes a call awaiting interactive approval, handed to the Asker.
// Subject is the call's matchable target (a shell command, a file path) already
// extracted for display; it is empty for a tool with no known subject. Arguments
// is the model's full, validated arguments object, so a face can render the whole
// call if it wants.
type Request struct {
	Tool      string
	Subject   string
	Arguments []byte
	// AgentID names the delegated sub-agent whose call this is (AS-120), so a
	// face can attribute the prompt ("delegated agent <id> wants to …"). It is
	// empty for the main agent's own calls.
	AgentID string
}

// Outcome is the Asker's answer. Allow gates this one call. Remember asks the
// Policy to persist an allow-rule so future matching calls run without a prompt;
// Rule is the rule to persist when Remember is set — a zero Rule is derived from
// the call (its tool and exact subject, or the whole tool when it has none).
type Outcome struct {
	Allow    bool
	Remember bool
	Rule     Rule
}

// Asker is the interactive approval path, implemented by the active face (the
// TUI, AS-024). Ask is called only when a call needs a prompt (ask mode, or a
// non-matching call under allowlist mode). Returning an error is treated as a
// denial carrying the error text, so a broken or cancelled prompt fails safe.
// Implementations must be safe for concurrent use: parallel tool calls (AS-019)
// may prompt concurrently.
type Asker interface {
	Ask(ctx context.Context, req Request) (Outcome, error)
}

// Persister durably records an allow-rule the user chose to remember — typically
// appending it to the project config file (see FilePersister). It is called
// while the Policy holds no lock, so it may do I/O. A nil Persister keeps
// remembered rules in memory only (still honored for the rest of the session).
type Persister func(Rule) error

// Policy is the approval engine. It resolves each call's mode, consults the
// allowlist, and routes prompts to the Asker, producing the Decision the tool
// Runtime enforces. It is safe for concurrent use.
type Policy struct {
	mu       sync.RWMutex
	cfg      Config
	asker    Asker
	persist  Persister
	subjects map[string]subjecter
}

// Option configures a Policy.
type Option func(*Policy)

// WithAsker sets the interactive approval path used for prompts. Without one,
// every call that would prompt is denied with a model-readable reason.
func WithAsker(a Asker) Option {
	return func(p *Policy) { p.asker = a }
}

// WithPersister sets where remembered ("always allow") rules are durably stored.
func WithPersister(fn Persister) Option {
	return func(p *Policy) { p.persist = fn }
}

// WithSubjecter teaches the Policy how to extract a rule-matchable subject from a
// tool's arguments: field is the JSON property to read, style how patterns match
// it. It registers (or overrides) the entry for tool, so a project can permission
// a custom tool by pattern. Built-in tools are registered by default.
func WithSubjecter(toolName, field string, style MatchStyle) Option {
	return func(p *Policy) {
		p.subjects[toolName] = subjecter{field: field, style: style}
	}
}

// New builds a Policy over cfg. cfg is copied defensively (including its Allow
// slice) so later remembered rules do not mutate the caller's value.
func New(cfg Config, opts ...Option) *Policy {
	p := &Policy{
		cfg:      cfg.clone(),
		subjects: defaultSubjecters(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Func returns the tool.PermissionFunc the Runtime invokes before every
// execution. Wiring it via tool.WithPermission is what makes this policy the
// single gate every tool call passes through.
func (p *Policy) Func() tool.PermissionFunc {
	return p.decide
}

// decide is the per-call decision. It resolves the call's mode, then:
//   - auto: allow.
//   - allowlist: allow when an allow-rule matches; otherwise prompt.
//   - ask: prompt.
func (p *Policy) decide(ctx context.Context, call tool.Call) tool.Decision {
	switch p.modeFor(call.Name) {
	case ModeAuto:
		return tool.Allowed()
	case ModeAllowlist:
		if p.allowed(call) {
			return tool.Allowed()
		}
		return p.prompt(ctx, call)
	default: // ModeAsk and any unknown mode fail safe to prompting.
		return p.prompt(ctx, call)
	}
}

// modeFor returns the effective mode for a tool: its per-tool override when set
// and valid, else the session default when valid, else ModeAsk.
func (p *Policy) modeFor(toolName string) Mode {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if m, ok := p.cfg.Tools[toolName]; ok && m.valid() {
		return normalizeMode(m)
	}
	if p.cfg.DefaultMode.valid() {
		return normalizeMode(p.cfg.DefaultMode)
	}
	return ModeAsk
}

// allowed reports whether any current allow-rule matches the call.
func (p *Policy) allowed(call tool.Call) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, r := range p.cfg.Allow {
		if p.matches(r, call) {
			return true
		}
	}
	return false
}

// prompt runs the interactive approval path. With no Asker it denies, naming the
// resolution (allowlist/auto or an approver) so the message guides a fix. A
// remembered approval is recorded before the allow is returned, so it is honored
// immediately for subsequent calls this session.
func (p *Policy) prompt(ctx context.Context, call tool.Call) tool.Decision {
	if p.asker == nil {
		return tool.Denied("interactive approval required but no approver is configured; " +
			"add an allowlist rule or set this tool's mode to auto")
	}

	subject, _ := p.subjectValue(call)
	out, err := p.asker.Ask(ctx, Request{
		Tool:      call.Name,
		Subject:   subject,
		Arguments: call.Arguments,
		AgentID:   tool.AgentFromContext(ctx),
	})
	if err != nil {
		return tool.Denied("approval failed: " + err.Error())
	}
	if !out.Allow {
		return tool.Denied("denied by user")
	}
	if out.Remember {
		p.remember(out.Rule, call)
	}
	return tool.Allowed()
}

// remember records an allow-rule the user approved with "always allow". A zero
// rule is derived from the call. The rule is added to the in-memory allowlist
// (so it takes effect at once) and handed to the Persister, if any, for durable
// storage. Persistence runs outside the lock since it may do I/O.
func (p *Policy) remember(r Rule, call tool.Call) {
	if r.isZero() {
		r = p.deriveRule(call)
	}

	p.mu.Lock()
	if !containsRule(p.cfg.Allow, r) {
		p.cfg.Allow = append(p.cfg.Allow, r)
	}
	persist := p.persist
	p.mu.Unlock()

	if persist != nil {
		// Best-effort: a failed persist still leaves the rule active in memory.
		_ = persist(r)
	}
}

// deriveRule builds the rule to remember for a call with no explicit Rule from
// the Asker: tool plus the call's exact subject, or a whole-tool rule when the
// tool exposes no subject.
func (p *Policy) deriveRule(call tool.Call) Rule {
	subject, ok := p.subjectValue(call)
	if !ok || subject == "" {
		return Rule{Tool: call.Name}
	}
	return Rule{Tool: call.Name, Pattern: subject}
}
