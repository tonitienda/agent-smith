// Package secret is the orchestrator's secret-handling contract (AS-154): how an
// orchestrated run declares, classifies, receives, audits, and redacts secrets
// without ever letting a value reach a job spec, the run DB, or the Smith event
// log. The design is recorded in docs/design/adr-0004-secret-management-redaction.md
// and informed by the AS-158 competitive spike.
//
// It is a stdlib-only leaf (like internal/orchestrator/spec and .../store): it
// owns the *types and rules* of the contract, not the wiring. The daemon and the
// sandbox seam (AS-153/AS-156) inject a concrete [Resolver] — the credential
// proxy that holds the real value outside the runner — and consume the [Value],
// [AuditRecord], and [Redactor] this package defines. Nothing here reaches back
// into the daemon, the loop, or a face.
//
// The load-time half of the contract (a spec declares scope *names*, never
// values; an undeclared ${secrets.*} reference or a plaintext-looking literal is
// rejected fail-closed) already lives in internal/orchestrator/spec (rules 9 and
// 14). This package is the run-time half: turning a declared scope into a value
// that cannot leak.
package secret

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Class is a secret's category (AS-154 AC-1). The same scope+proxy+audit
// discipline applies to every class; the class exists so audit records and
// operator tooling can reason about what kind of credential a run touched
// without ever seeing its value. Classes are additive (PRD D2): an unknown class
// is treated as opaque, never rejected.
type Class string

const (
	// ClassProvider is a model-provider credential (Anthropic, OpenAI, …).
	ClassProvider Class = "provider"
	// ClassGitHub is a GitHub credential (the maintainer PAT of ADR-0003, or a
	// future App installation token).
	ClassGitHub Class = "github"
	// ClassUser is an optional user- or team-supplied secret a job declares for
	// its own tools (e.g. a deploy token).
	ClassUser Class = "user"
	// ClassService is a Smith service credential (the run DB, telemetry sink, …).
	ClassService Class = "service"
)

// UserScopePrefix marks a scope as a user/team secret: any declared scope that
// begins with "user." classifies as [ClassUser]. This keeps user secrets in a
// reserved namespace that can never collide with a built-in provider/github/
// service scope.
const UserScopePrefix = "user."

// builtinScopes maps the fixed, non-user scope names to their class. It mirrors
// the providerScope binding in internal/orchestrator/spec so the two halves of
// the contract agree on what a scope name means.
var builtinScopes = map[string]Class{
	"anthropic-api-key": ClassProvider,
	"openai-api-key":    ClassProvider,
	"github-token":      ClassGitHub,
	"smith-service":     ClassService,
}

// Classify returns the [Class] of a declared scope name and whether the scope is
// known. A "user."-prefixed scope is always [ClassUser]; every other scope must
// be a built-in. An unknown scope returns ("", false) so callers fail closed.
func Classify(scope string) (Class, bool) {
	if strings.HasPrefix(scope, UserScopePrefix) && len(scope) > len(UserScopePrefix) {
		return ClassUser, true
	}
	c, ok := builtinScopes[scope]
	return c, ok
}

// ValidateScopes reports any declared scope that names no known class. It is the
// class-level companion to the spec loader's rule-9/14 checks: the loader ensures
// every referenced scope is *declared*; this ensures every declared scope is
// *classifiable*, so a run never injects a credential of unknown provenance.
// Returned scopes are sorted for deterministic messages.
func ValidateScopes(scopes []string) []string {
	var unknown []string
	for _, s := range scopes {
		if _, ok := Classify(s); !ok {
			unknown = append(unknown, s)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// redactedPlaceholder is the fixed substitution used everywhere a secret value
// would otherwise be rendered.
const redactedPlaceholder = "[REDACTED]"

// Value is a resolved secret value that refuses to render itself. Its String,
// GoString, and MarshalJSON all return the redaction placeholder, so a Value that
// is accidentally logged, %v-formatted, or JSON-encoded into a run-DB row or an
// event-log block leaks nothing (AS-154 AC-3). The real bytes are reachable only
// through the explicit [Value.Reveal], which every call site must name.
type Value struct {
	scope string
	raw   string
}

// NewValue wraps a resolved secret. It is called only inside a [Resolver] (the
// credential proxy), never from spec or store code.
func NewValue(scope, raw string) Value { return Value{scope: scope, raw: raw} }

// Scope reports which declared scope this value resolved. The scope name is not
// itself a secret (it appears in the spec and in audit records).
func (v Value) Scope() string { return v.scope }

// Reveal returns the raw secret bytes. Naming this method at a call site is the
// deliberate, greppable act of taking a value out of its safe wrapper — do it as
// late as possible (at the point of injection) and never store the result.
func (v Value) Reveal() string { return v.raw }

// Empty reports whether no value was resolved.
func (v Value) Empty() bool { return v.raw == "" }

// String implements fmt.Stringer with the redaction placeholder.
func (v Value) String() string { return redactedPlaceholder }

// GoString implements fmt.GoStringer so even %#v does not print the value.
func (v Value) GoString() string { return redactedPlaceholder }

// MarshalJSON renders the placeholder so a Value serialised into a run-DB row or
// event-log block never carries plaintext.
func (v Value) MarshalJSON() ([]byte, error) {
	return []byte(`"` + redactedPlaceholder + `"`), nil
}

// Resolver is the credential-proxy seam: it turns a declared scope name into a
// [Value] whose bytes live outside the runner until the moment of injection. The
// daemon and the sandbox backend (AS-153/AS-156) supply the concrete resolver
// (env var, OS keychain via internal/credential, or a remote proxy); this package
// defines only the boundary so no leaf here depends on a credential backend.
type Resolver interface {
	// Resolve returns the value for scope, or an error the caller maps to
	// store.FailureMissingSecret. It must never return the value by any channel
	// other than the returned [Value].
	Resolve(scope string) (Value, error)
}

// MapResolver is an in-memory [Resolver] for tests and offline runs: scope →
// raw value. It never touches a host keychain or network.
type MapResolver map[string]string

// Resolve implements [Resolver].
func (m MapResolver) Resolve(scope string) (Value, error) {
	raw, ok := m[scope]
	if !ok || raw == "" {
		return Value{}, fmt.Errorf("secret: no value for scope %q", scope)
	}
	return NewValue(scope, raw), nil
}

// AuditRecord is the immutable record written when a secret is injected into a
// run (AS-154 AC-4). It carries identity, class, expiry, recipient, and the run
// it served — and, by construction, no value: there is no field for one and the
// struct is built from a [Value]'s scope, never its bytes.
type AuditRecord struct {
	// Name is the operator-facing secret name (the declared scope).
	Name string `json:"name"`
	// Scope is the declared scope this injection resolved.
	Scope string `json:"scope"`
	// Class is the resolved [Class].
	Class Class `json:"class"`
	// Recipient is the run component the value was handed to (e.g. "agent",
	// "github", a step id) — who received it, never what.
	Recipient string `json:"recipient"`
	// RunID ties the injection to a run-DB row.
	RunID string `json:"run_id"`
	// Expiry is when the injected credential stops being valid; the zero time
	// means "no declared expiry" (a rotatable long-lived credential).
	Expiry time.Time `json:"expiry,omitempty"`
	// At is when the injection happened.
	At time.Time `json:"at"`
}

// Audit builds the [AuditRecord] for injecting v into recipient during runID at
// time at, with the given expiry (pass the zero time for none). The value's
// scope names the record; its bytes are never read.
func Audit(v Value, recipient, runID string, expiry, at time.Time) AuditRecord {
	class, _ := Classify(v.Scope())
	return AuditRecord{
		Name:      v.Scope(),
		Scope:     v.Scope(),
		Class:     class,
		Recipient: recipient,
		RunID:     runID,
		Expiry:    expiry,
		At:        at,
	}
}

// Redactor performs value-based redaction-at-capture (AS-154 AC-5): it knows the
// exact secret strings a run was injected with and replaces every occurrence
// with the placeholder before any log line or artifact leaves the runner. It is
// the complement of the pattern-based capture-time scrub in internal/redaction
// (AS-115): patterns catch secrets Smith never saw the value of; this catches the
// ones it did. The zero value redacts nothing; use [NewRedactor].
type Redactor struct {
	values []string
}

// NewRedactor builds a redactor over the raw bytes of the given values. Empty
// values are skipped so an unresolved scope never turns into a placeholder that
// matches the empty string. This is the one place outside a [Resolver] that
// reads a [Value]'s bytes, and it keeps them only in memory to match-and-replace,
// never persisting them.
func NewRedactor(values ...Value) *Redactor {
	r := &Redactor{}
	for _, v := range values {
		if raw := v.Reveal(); raw != "" {
			r.values = append(r.values, raw)
		}
	}
	// Longest first so a secret that is a substring of another is not partially
	// revealed by redacting the shorter one first.
	sort.SliceStable(r.values, func(i, j int) bool { return len(r.values[i]) > len(r.values[j]) })
	return r
}

// Redact returns s with every injected secret value replaced by the placeholder.
func (r *Redactor) Redact(s string) string {
	if r == nil {
		return s
	}
	for _, v := range r.values {
		s = strings.ReplaceAll(s, v, redactedPlaceholder)
	}
	return s
}

// RedactBytes is the []byte form of [Redactor.Redact] for streaming log/artifact
// capture.
func (r *Redactor) RedactBytes(b []byte) []byte {
	if r == nil || len(r.values) == 0 {
		return b
	}
	return []byte(r.Redact(string(b)))
}
