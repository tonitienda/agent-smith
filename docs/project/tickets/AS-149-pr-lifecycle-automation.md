---
id: AS-149
title: PR lifecycle automation
status: ready-to-implement
area: integrations
priority: P2
depends_on: [AS-147, AS-148]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-149 · PR lifecycle automation

## Description

Add the deterministic PR lifecycle actions required for Smith to implement Smith: create a branch, open a PR, update a prior Smith-authored PR, comment run summaries, and hand off to merge policy.

## Acceptance criteria

- [ ] Workflow steps can create a Smith-owned branch and open a PR linked to the source issue/run.
- [ ] Reruns can update an existing Smith-authored PR while leaving unrelated branches unchanged.
- [ ] PR body/comment includes run summary, job ID, provider roles, budget/cost, artifacts, and session links.
- [ ] PR actions are recorded in the run DB and Smith append-only session.
- [ ] Safety checks confirm the target PR or branch is recognized as Smith-owned before update actions proceed.
- [ ] Merge and auto-merge are delegated to AS-157 policy rather than prompt instructions.

## Dependencies

[AS-147, AS-148]

## Clarification (resolved 2026-06-30)

The blocker named here was sequencing, not an open product question: AS-147 and
AS-148 are now both `ready-to-implement` with their design fixed, which is what
this ticket was waiting on.

1. **Idempotency keys.** AS-147's acceptance criteria already define the
   trigger-record shape this ticket consumes — "repository, issue/PR number,
   labels, actor, event time, delivery ID, and idempotency key" — so PR
   create/update/comment/status actions key off that same `(delivery ID,
   idempotency key)` pair rather than defining a second one; "duplicate webhook
   delivery does not enqueue duplicate effective work" (AS-147 AC) is the
   existing guarantee this ticket's actions build on.
2. **Auth.** AS-148's clarified strategy (scoped maintainer token now, GitHub
   App migration later, real credential kept in a proxy outside the runner,
   push restricted to the run's own branch) is the credential path PR actions
   use; no separate auth design is needed here.
