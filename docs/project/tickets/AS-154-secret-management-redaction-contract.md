---
id: AS-154
title: Secret management and redaction contract
status: ready-to-implement
area: security
priority: P2
depends_on: [AS-159, AS-148, AS-158]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-154 · Secret management and redaction contract

## Description

Define how orchestrated jobs declare, receive, audit, redact, and revoke secrets without leaking values into prompts, job specs, run DB rows, or Smith event logs.

## Acceptance criteria

- [ ] Secret classes cover model-provider credentials, GitHub credentials, optional user/team secrets, and Smith service credentials.
- [ ] Job specs declare named secret scopes and validation fails when a job references undeclared scopes.
- [ ] Secret values are never stored in plaintext job specs, run DB audit entries, or event-log blocks.
- [ ] Injection audit records include name/scope/expiry/recipient/run IDs but never values.
- [ ] Redaction-at-capture is applied before logs or artifacts leave the runner where possible.
- [ ] The implementation plan reflects findings from AS-158.

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
