---
id: AS-154
title: Secret management and redaction contract
status: done
area: security
priority: P2
depends_on: [AS-159, AS-148, AS-158]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-154 · Secret management and redaction contract

## Description

Define how orchestrated jobs declare, receive, audit, redact, and revoke secrets without leaking values into prompts, job specs, run DB rows, or Smith event logs.

## Acceptance criteria

- [x] Secret classes cover model-provider credentials, GitHub credentials, optional user/team secrets, and Smith service credentials.
- [x] Job specs declare named secret scopes and validation fails when a job references undeclared scopes.
- [x] Secret values are never stored in plaintext job specs, run DB audit entries, or event-log blocks.
- [x] Injection audit records include name/scope/expiry/recipient/run IDs but never values.
- [x] Redaction-at-capture is applied before logs or artifacts leave the runner where possible.
- [x] The implementation plan reflects findings from AS-158.

## Resolution (2026-07-01)

Contract recorded in [ADR-0004 — Secret management and redaction contract](../../design/adr-0004-secret-management-redaction.md)
and realised as the stdlib-only leaf `internal/orchestrator/secret`:

- **Classes (AC-1)** — `provider`, `github`, `user` (reserved `user.*` prefix),
  `service`; `Classify`/`ValidateScopes` fail closed on an unknown class.
- **Declared scopes, fail-closed (AC-2)** — enforced in `internal/orchestrator/spec`
  (rules 9 + 14: needed-but-unlisted scope and undeclared `${secrets.*}`
  reference are load errors; plaintext-looking literals rejected), plus the new
  class-level `ValidateScopes` check.
- **No value in spec/DB/log (AC-3)** — resolution is by scope name through the
  `Resolver` proxy seam; the resolved `Value`'s `String`/`GoString`/`MarshalJSON`
  all render `[REDACTED]`, so a value encoded into a run-DB row or event-log block
  leaks nothing. Raw bytes only via the explicit `Value.Reveal()`.
- **Audit without values (AC-4)** — `secret.Audit` builds `AuditRecord{Name,
  Scope, Class, Recipient, RunID, Expiry, At}` from the value's scope, never its
  bytes; there is no value field.
- **Redaction-at-capture (AC-5)** — `secret.Redactor` value-redacts injected
  secrets (longest-match-first) before output leaves the runner, complementing
  the pattern-based `internal/redaction` scrub (AS-115).
- **AS-158 (AC-6)** — the ADR's decision maps each finding (credential proxy as
  primary control, setup-phase-only secrets, file-over-env, audit without
  values) to the contract.

Live wiring of the resolver + redactor at the runner boundary is deferred to the
consumers AS-153 (sandbox seam) / AS-156 (private VPC), which the ADR names.

## Clarification (resolved 2026-06-30) — research input (AS-158)

See [orchestrator-competitive-research.md §3 AS-154](../../research/orchestrator-competitive-research.md#as-154--secret-management-and-redaction-contract):
declared named scopes per job with validation failing on undeclared reference
(Copilot isolated bucket); a credential proxy so values never enter the
runner/spec/DB/log (Ona, Claude web) as the primary control; setup-phase-only
secrets removed before the agent phase (Codex); redaction-at-capture (`[REDACTED]`)
and prefer file-type over env-var secrets (Ona); audit name/scope/expiry/recipient/
run-IDs, never values.

## Dependencies

[AS-159, AS-148, AS-158]
