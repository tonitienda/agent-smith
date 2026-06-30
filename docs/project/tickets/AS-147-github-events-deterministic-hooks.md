---
id: AS-147
title: GitHub event ingestion and deterministic hooks
status: needs-clarification
area: integrations
priority: P2
depends_on: [AS-160, AS-161]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-147 · GitHub event ingestion and deterministic hooks

## Description

Add the GitHub event and deterministic hook layer needed for Smith to react to labels and PR lifecycle events without encoding workflow state changes inside prompts.

## Acceptance criteria

- [ ] Webhook/event normalizer maps issue labeled, PR labeled, PR merged, and comment command events into stable Smith trigger records.
- [ ] Trigger records include repository, issue/PR number, labels, actor, event time, delivery ID, and idempotency key.
- [ ] Deterministic hooks can add/remove labels, comment summaries, and update statuses as explicit workflow steps.
- [ ] Hook execution records success/failure in the run DB and append-only Smith session.
- [ ] Duplicate webhook delivery does not enqueue duplicate effective work.
- [ ] Prompt content is not responsible for remembering labels or workflow state transitions.

## Dependencies

[AS-160, AS-161]

## Open questions

1. Webhook delivery for the local/VPC daemon (AS-148 auth + AS-161 daemon) — polling vs a public webhook endpoint vs a relay — is unresolved pending AS-148 and the AS-158 spike.
2. Which GitHub events are normalized in MVP 0 beyond issue/PR labeled and PR merged?
