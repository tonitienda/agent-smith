---
id: AS-145
title: Scheduled job spec and trigger semantics
status: ready-to-implement
area: cloud
priority: P2
depends_on: [AS-144]
source: docs/projects/smith-cloud-prd.md
---

# AS-145 · Scheduled job spec and trigger semantics

## Description

Add a versioned Smith Cloud job-spec design for cron, GitHub events, manual dispatch, and chained follow-up subtasks.

## Acceptance criteria

- [ ] Spec documents stable job ID, owner, triggers, timezone, missed-run policy, concurrency, timeout, retries, budgets, sandbox profile, secret scopes, GitHub permissions, PR policy, and retention.
- [ ] Cron, PR-merged, manual dispatch, and chained run triggers have examples under a proposed .agent-smith/jobs/ layout.
- [ ] Validation rules reject ambiguous timezones, unbounded concurrency, missing budget ceilings, and secret scopes not declared in policy.

## Dependencies

[AS-144]
