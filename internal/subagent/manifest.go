// Package subagent implements the system sub-agent framework (AS-044, PRD
// §7.19, Appendix C.3–C.5): built-in specialized analyzers driven at session
// lifecycle points under the contract init(scope) → observe (passive, no model
// calls) → teardown(scope, state), with a registry + manifest so built-ins are
// just first-party plugins on a public interface and third-party sub-agents load
// the same way — but declaratively only (D9: a manifest is data, never code).
//
// The framework is the substrate the wedges build on (AS-045 /insights, AS-046
// user sub-agents, AS-049 skill analyzer). It is pure and face-agnostic: observe
// is passive by construction (it reads the log, never calls a model), teardown
// hands a context slice to the analyzer off the interactive hot path, and
// findings land in an in-memory insights Store — never on the event log, so a
// disabled analyzer adds zero token cost and zero extra blocks (§7.19 AC).
// Per-sub-agent dollar caps reuse the AS-041 budget Guard, so the whole system
// enforces spend with one rule.
//
// Layering: this package depends only on the schema, the budget Guard, and the
// stdlib. It does not import the loop or any face; the loop opts the framework in
// (the same way it opts in budget enforcement), so the substrate lands without
// touching the hot path.
package subagent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Kind classifies a sub-agent. V1 ships one kind — an analyzer that observes a
// trace and reports findings — but the field is explicit in the manifest so the
// registry can reject an unknown kind rather than silently mis-driving it.
type Kind string

// KindAnalyzer is the only supported kind: a passive analyzer (no model calls in
// observe; teardown may analyze a context slice).
const KindAnalyzer Kind = "analyzer"

// Schedule names when a sub-agent's teardown analysis runs. It is the manifest's
// declared default; per-sub-agent config (C.3) may override it. Scheduling is
// always off the interactive hot path — at a span teardown, at session end, or in
// a cross-session rollup — never inline with a turn (§7.19 AC).
type Schedule string

const (
	// AtTeardown runs the analyzer when each observed span tears down.
	AtTeardown Schedule = "teardown"
	// AtSessionEnd runs the analyzer once, when the session ends.
	AtSessionEnd Schedule = "session_end"
	// AtRollup runs the analyzer in a cross-session rollup (AS-050), not within a
	// live session; the live Runner skips it.
	AtRollup Schedule = "rollup"
)

// validSchedules is the recognized set, so a misspelled schedule surfaces as a
// warning rather than a sub-agent that never fires.
var validSchedules = map[Schedule]bool{AtTeardown: true, AtSessionEnd: true, AtRollup: true}

// ScopeKind is the lifecycle window a sub-agent observes: a single span or the
// whole session. Span-scoped analyzers tear down per span; session-scoped ones
// observe across the session and tear down once at the end.
type ScopeKind string

const (
	// SpanScope observes one span (e.g. a tool batch or a sub-task).
	SpanScope ScopeKind = "span"
	// SessionScope observes the whole session.
	SessionScope ScopeKind = "session"
)

var validScopeKinds = map[ScopeKind]bool{SpanScope: true, SessionScope: true}

// Permission is a capability a sub-agent declares in its manifest. The set is
// deliberately tiny and propose-only: a sub-agent may read the transcript and
// propose an edit, but the framework never lets it write — a proposal is surfaced
// for the user to confirm (D9, C.5).
type Permission string

const (
	// ReadTranscript lets the sub-agent see the context slice handed to teardown.
	ReadTranscript Permission = "read_transcript"
	// ProposeEdit lets the sub-agent attach a proposed edit to a finding. It is
	// propose-only: the framework records the proposal, it never applies it.
	ProposeEdit Permission = "propose_edit"
)

var validPermissions = map[Permission]bool{ReadTranscript: true, ProposeEdit: true}

// Manifest is a sub-agent's declaration (Appendix C.5): identity, when and over
// what scope it runs, its model tier and default-on state, its per-session dollar
// cap, what finding kinds it emits, and the permissions it claims. It is the
// JSON-decoded form a third-party sub-agent ships and the value a built-in
// returns from Manifest(); the registry validates it the same way for both.
type Manifest struct {
	// Name uniquely identifies the sub-agent within a registry. Required.
	Name string `json:"name"`
	// Kind is the sub-agent kind; only KindAnalyzer is supported.
	Kind Kind `json:"kind"`
	// Schedule is the default lifecycle point at which teardown runs; empty defaults
	// to AtTeardown. Per-sub-agent config (C.3) overrides it.
	Schedule Schedule `json:"schedule,omitempty"`
	// Scope is the lifecycle window observed; empty defaults to SpanScope.
	Scope ScopeKind `json:"scope,omitempty"`
	// ModelTier names the model tier teardown analysis would use ("cheap",
	// "summarizer", …). Observe never calls a model; this is advisory for teardown
	// and is the tier the budget cap meters against. Empty means no model use.
	ModelTier string `json:"modelTier,omitempty"`
	// EnabledByDefault is whether the sub-agent runs without an explicit config
	// opt-in. Built-ins that cost nothing when idle may default on; config can flip
	// either way (C.3).
	EnabledByDefault bool `json:"enabledByDefault,omitempty"`
	// BudgetUSD is the default per-session dollar cap for this sub-agent; 0 means
	// uncapped. Config may override it.
	BudgetUSD float64 `json:"budgetUSD,omitempty"`
	// Emits lists the finding kinds the sub-agent reports, so a consumer (AS-045
	// /insights) knows what to expect without running it.
	Emits []string `json:"emits,omitempty"`
	// Permissions are the capabilities claimed (read_transcript, propose_edit).
	Permissions []Permission `json:"permissions,omitempty"`
}

// effectiveSchedule returns the manifest schedule, defaulting empty to AtTeardown.
func (m Manifest) effectiveSchedule() Schedule {
	if m.Schedule == "" {
		return AtTeardown
	}
	return m.Schedule
}

// effectiveScope returns the manifest scope, defaulting empty to SpanScope.
func (m Manifest) effectiveScope() ScopeKind {
	if m.Scope == "" {
		return SpanScope
	}
	return m.Scope
}

// Allows reports whether the manifest claims permission p.
func (m Manifest) Allows(p Permission) bool {
	for _, have := range m.Permissions {
		if have == p {
			return true
		}
	}
	return false
}

// Validate checks a manifest is well formed: a name, a supported kind, a known
// schedule and scope (defaults are filled, not rejected), and only recognized
// permissions. It is applied identically to built-in and third-party manifests so
// the registry enforces one contract for both. A declarative third-party manifest
// that fails here is rejected at load rather than mis-driven at run time.
func (m Manifest) Validate() error {
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("subagent: manifest has no name")
	}
	if m.Kind != KindAnalyzer {
		return fmt.Errorf("subagent %q: unsupported kind %q (only %q)", m.Name, m.Kind, KindAnalyzer)
	}
	if !validSchedules[m.effectiveSchedule()] {
		return fmt.Errorf("subagent %q: unknown schedule %q", m.Name, m.Schedule)
	}
	if !validScopeKinds[m.effectiveScope()] {
		return fmt.Errorf("subagent %q: unknown scope %q", m.Name, m.Scope)
	}
	if m.BudgetUSD < 0 {
		return fmt.Errorf("subagent %q: negative budget %.4f", m.Name, m.BudgetUSD)
	}
	for _, p := range m.Permissions {
		if !validPermissions[p] {
			return fmt.Errorf("subagent %q: unknown permission %q", m.Name, p)
		}
	}
	return nil
}

// ParseManifest decodes and validates a third-party declarative manifest (C.5,
// D9). It accepts data only — no code — so loading a third-party sub-agent can
// never run arbitrary logic: the parsed manifest drives a passive declarative
// sub-agent through the same registry and lifecycle as a built-in.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("subagent: parse manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}
