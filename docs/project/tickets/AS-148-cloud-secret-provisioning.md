---
id: AS-148
title: Secret provisioning and redaction contract for cloud sandboxes
status: ready-to-implement
area: security
priority: P2
depends_on: [AS-144, AS-147]
source: docs/projects/smith-cloud-prd.md
---

# AS-148 · Secret provisioning and redaction contract for cloud sandboxes

## Description

Define and implement the secret metadata, storage, injection, expiry, audit, and redaction contract for cloud jobs.

## Acceptance criteria

- [ ] Secret values are stored only in an encrypted backing store or delegated secret manager, never in job specs or event logs.
- [ ] Jobs declare named secret scopes and workers receive only short-lived material needed for the claimed sandbox.
- [ ] Injection audit records include secret name/scope/expiry/sandbox/run IDs but never values.
- [ ] Redaction-at-capture remains active for cloud logs before upload.

## Dependencies

[AS-144, AS-147]
