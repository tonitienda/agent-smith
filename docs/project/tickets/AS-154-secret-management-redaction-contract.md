---
id: AS-154
title: Secret management and redaction contract
status: needs-clarification
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

## Dependencies

[AS-159, AS-148, AS-158]

## Open questions

1. Concrete secret-store backend (PRD Q7) is gated on the AS-158 research spike; this ticket fixes only the contract (declared scopes, no plaintext, redaction-at-capture, fail-closed).
