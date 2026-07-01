# ADR-0004 — Secret management and redaction contract for the orchestrator (AS-154)

> Status: **accepted** · Scope: how orchestrated jobs declare, receive, audit, redact, and revoke secrets (PRD §4.6, §5, Q7; AS-154) · Date: 2026-07-01

## Context

The dogfood orchestrator ([orchestrator-architecture.md](../architecture/orchestrator-architecture.md))
runs jobs that need credentials: model-provider API keys for the agent phase,
the GitHub maintainer token of [ADR-0003](adr-0003-github-auth-strategy.md) for
deterministic GitHub actions, optional user/team secrets a job's own tools need,
and Smith's own service credentials (run DB, telemetry). Q7 (deferred from
AS-159) asked for the secret contract before those consumers hard-wire anything.

Input: the AS-158 competitive spike
([orchestrator-competitive-research.md §3 AS-154](../research/orchestrator-competitive-research.md#as-154--secret-management-and-redaction-contract))
surveyed Claude Code, Codex, Cursor, Copilot, Devin, Jules, Coder, and Ona. The
convergent pattern: **declared named scopes per job** (Copilot's isolated bucket
→ only declared scopes are visible); a **credential proxy** so values never enter
the runner, spec, run DB, or log (Ona, Claude web) as the *primary* control;
**setup-phase-only** secrets removed before the agent phase (Codex);
**redaction-at-capture** as the secondary control (Cursor Runtime Secrets); a
preference for **file-type over env-var** secrets (Ona); and an **injection audit
record of name/scope/expiry/recipient/run-IDs, never values**.

This is a **private single-tenant dogfood**, not a hosted multi-tenant product
(PRD §3 non-goals; D9 "not a sandbox"). That scoping is what makes a maintainer-
supplied resolver acceptable for MVP 0, exactly as ADR-0003 scopes the GitHub
credential.

## Decision

A secret is **declared by scope name, resolved through a proxy seam into a value
that refuses to render itself, injected as late as possible with an audit record
that carries no value, and redacted by value before any output leaves the
runner.** The contract has a load-time half and a run-time half.

### Secret classes (AC-1)

Every secret classifies into one of four classes; the same scope + proxy + audit
discipline applies to all of them. Classes are additive (D2) — a new class is a
new constant, never a breaking change.

| Class | Scope names | Source |
| --- | --- | --- |
| `provider` | `anthropic-api-key`, `openai-api-key` | model-provider credentials (AS-017 `internal/credential`) |
| `github` | `github-token` | GitHub credential (ADR-0003 maintainer PAT → future App token) |
| `user` | `user.*` (reserved prefix) | optional user/team secret a job declares for its own tools |
| `service` | `smith-service` | Smith service credentials (run DB, telemetry) |

### Load-time half: declared scopes, fail-closed (AC-2)

A job spec declares **scope names only**, under `secrets:` — never values. This
is already enforced in `internal/orchestrator/spec` and stays there:

- **Rule 9** — a `github.*` action needs `github-token`; an agent step routed to
  a provider needs that provider's api-key scope; a scope that is needed but not
  listed is a load error.
- **Rule 14** — a `${secrets.X}` reference to a scope not listed under `secrets`
  is a load error, and a literal that *looks like* a credential (the
  `secrets.go` pattern set) is rejected so a value can never be pasted in.

This ADR adds the **class-level** check (`secret.ValidateScopes`): every declared
scope must classify into a known class, so a run never injects a credential of
unknown provenance. All checks are **fail-closed** (D-ORCH-1/6): unknown or
underspecified ⇒ refuse to schedule.

### Run-time half: proxy, value, audit, redaction

Realised as the stdlib-only leaf `internal/orchestrator/secret`:

- **Credential proxy (AC-3, primary control).** `Resolver.Resolve(scope) →
  Value` is the seam that holds the real bytes outside the runner until
  injection. The daemon and the sandbox backend (AS-153/AS-156) inject the
  concrete resolver (env var, OS keychain via `internal/credential`, or a remote
  proxy); the leaf depends on no backend. Because resolution is by scope name,
  the value never enters the spec, the run DB, or the event log.
- **Non-rendering `Value` (AC-3, defence in depth).** A resolved `Value`'s
  `String`, `GoString`, and `MarshalJSON` all return `[REDACTED]`, so a value
  that is accidentally logged, `%v`-formatted, or JSON-encoded into a run-DB row
  or event-log block leaks nothing. The raw bytes are reachable only through the
  explicit, greppable `Value.Reveal()`, used as late as possible and never
  stored.
- **Injection audit record (AC-4).** `secret.Audit(value, recipient, runID,
  expiry, at)` builds an `AuditRecord{Name, Scope, Class, Recipient, RunID,
  Expiry, At}`. It is built from the value's *scope*, never its bytes: there is
  no value field to populate. This is the record of *who received what kind of
  credential for which run and until when* — never the credential.
- **Redaction-at-capture (AC-5, secondary control).** `secret.NewRedactor(values
  …).Redact(...)` replaces the exact injected secret strings with `[REDACTED]`
  before any log line or artifact leaves the runner (longest-match-first so a
  substring secret is not partially revealed). It complements the pattern-based
  capture-time scrub of `internal/redaction` (AS-115): patterns catch secrets
  Smith never saw the value of; the value redactor catches the ones it did.

### Injection discipline

- **Late injection.** `Value.Reveal()` is called at the point of use only
  (setting an HTTP header, writing a credential file), never earlier and never
  into a stored structure.
- **Setup-phase-only secrets.** A credential a job needs only before the agent
  phase (e.g. a checkout token) is resolved for that phase and dropped before the
  model runs, matching Codex; the agent phase resolves only the scopes it needs.
- **File over env where possible.** Following Ona, a resolver should prefer a
  short-lived credential *file* (removed on teardown) over an environment
  variable, to dodge process-list and crash-dump leaks. MVP 0's env/keychain
  resolver documents this as the migration target for the sandbox backend.

### Revocation & expiry

`AuditRecord.Expiry` records when an injected credential stops being valid. The
GitHub App migration (ADR-0003) mints short-lived per-operation tokens whose
expiry is minutes; the MVP 0 maintainer PAT is long-lived and **rotatable** —
revocation is "rotate the PAT at the source, and the next resolve returns the new
value." Nothing Smith stores needs revoking because Smith stores no value.

## Consequences

- Downstream tickets consume the seam, not a re-implementation: the sandbox
  interface (AS-153) supplies the resolver and applies the redactor at the
  runner boundary; private-VPC deployment (AS-156) hosts the proxy; shareable
  bundles (AS-166) run over already-redacted logs.
- The run DB (AS-161) and session/event log (AS-151) gain no secret fields: the
  contract is that a value has nowhere to land there, enforced by the
  non-rendering `Value` and the class check rather than by reviewer vigilance.
- MVP 0's resolver is env/keychain-backed and single-tenant; the proxy-outside-
  the-runner and file-type-secret properties are documented migration targets,
  not MVP 0 blockers — the same token-first-App-later shape as ADR-0003.

## Follow-ups

- **Wiring** the resolver + redactor into the live runner boundary is AS-153
  (sandbox seam) / AS-156 (private VPC); this ticket fixes the contract and the
  leaf they consume.
- Persisting `AuditRecord`s into the run-DB audit trail is left to the AS-153/156
  wiring so the record shape can settle against a real injection path first.
