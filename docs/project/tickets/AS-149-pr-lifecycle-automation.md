---
id: AS-149
title: PR lifecycle automation
status: needs-clarification
area: integrations
priority: P2
depends_on: [AS-147, AS-148]
source: docs/projects/smith-orchestrator-dogfood-prd.md
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
