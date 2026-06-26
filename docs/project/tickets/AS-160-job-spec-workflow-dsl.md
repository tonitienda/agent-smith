---
id: AS-160
title: Job specification and workflow DSL
status: needs-clarification
area: orchestrator
priority: P2
depends_on: [AS-159]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-160 · Job specification and workflow DSL

## Description

Design the versioned `.agent-smith/jobs/*.yaml` format used by the orchestrator to define schedules, GitHub triggers, deterministic steps, model roles, permissions, budgets, and merge policy.

## Acceptance criteria

- [ ] Spec covers stable job ID, owner, repo/org scope, triggers, concurrency, timeout, retries, budgets, steps, hooks, provider routing, GitHub permissions, secret scopes, PR policy, and retention.
- [ ] Examples cover cron, manual dispatch, issue labeled, PR labeled, PR merged, and bounded follow-up runs.
- [ ] Deterministic actions such as `github.add_label`, `github.create_or_update_pr`, and `github.enable_auto_merge` are modeled as workflow steps/hooks rather than prompt text.
- [ ] Validation rejects ambiguous timezones, unbounded concurrency, missing budgets, unknown labels/actions, and undeclared secret or permission scopes.
- [ ] Job specs are repo-reviewable and can be loaded by the daemon without a cloud UI.

## Dependencies

[AS-159]
