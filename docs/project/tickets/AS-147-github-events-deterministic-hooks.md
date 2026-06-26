---
id: AS-147
title: GitHub event ingestion and deterministic hooks
status: needs-clarification
area: integrations
priority: P2
depends_on: [AS-145, AS-146]
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

[AS-145, AS-146]
