---
id: AS-147
title: GitHub event ingestion and deterministic hooks
status: ready-to-implement
area: integrations
priority: P2
depends_on: [AS-160, AS-161]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-147 · GitHub event ingestion and deterministic hooks

## Description

Add the GitHub event and deterministic hook layer needed for Smith to react to labels and PR lifecycle events without encoding workflow state changes inside prompts.

## Clarification (resolved 2026-06-30)

This ticket carried no ticket-local open question — it was held at
`needs-clarification` only because the [orchestrator ADR](../../architecture/orchestrator-architecture.md)
(AS-159) had not yet fixed the architecture it builds on. AS-159 is now
**Accepted** and both dependencies (AS-160 job-spec DSL, AS-161 daemon/scheduler/
run store) are **done**. The ADR's D-ORCH-4 boundary table already places this
work: "GitHub integration | `internal/orchestrator` (AS-147/149) | Normalize
webhooks → trigger events; deterministic action steps." The acceptance criteria
already fully specify the trigger-record shape (repository, issue/PR number,
labels, actor, event time, delivery ID, idempotency key) and the hook surface
(label add/remove, comment, status). No remaining product decision blocks
implementation.

## Acceptance criteria

- [ ] Webhook/event normalizer maps issue labeled, PR labeled, PR merged, and comment command events into stable Smith trigger records.
- [ ] Trigger records include repository, issue/PR number, labels, actor, event time, delivery ID, and idempotency key.
- [ ] Deterministic hooks can add/remove labels, comment summaries, and update statuses as explicit workflow steps.
- [ ] Hook execution records success/failure in the run DB and append-only Smith session.
- [ ] Duplicate webhook delivery does not enqueue duplicate effective work.
- [ ] Prompt content is not responsible for remembering labels or workflow state transitions.

## Dependencies

[AS-160, AS-161]
