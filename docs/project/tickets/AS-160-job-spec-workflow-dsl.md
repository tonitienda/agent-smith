---
id: AS-160
title: Job specification and workflow DSL
status: done
area: orchestrator
priority: P2
depends_on: [AS-159]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-160 · Job specification and workflow DSL

## Description

Design the versioned `.agent-smith/jobs/*.yaml` format used by the orchestrator to define schedules, GitHub triggers, deterministic steps, model roles, permissions, budgets, and merge policy.

**Delivered:** the format is specified in [`docs/design/job-spec-dsl.md`](../../design/job-spec-dsl.md) and linked from the orchestrator ADR. The loader/validator that enforces §5 of that doc is AS-161's deliverable; this ticket fixes the format and validation contract only.

## Acceptance criteria

- [x] Spec covers stable job ID, owner, repo/org scope, triggers, concurrency, timeout, retries, budgets, steps, hooks, provider routing, GitHub permissions, secret scopes, PR/merge policy, and retention (job-spec-dsl.md §3–§4).
- [x] Examples cover cron, manual dispatch, issue labeled, PR labeled, PR merged, and bounded follow-up runs (§6, plus the canonical PRD §4.1 example for issue/PR labeled).
- [x] Deterministic actions such as `github.add_label`, `github.create_or_update_pr`, and `github.enable_auto_merge` are modeled as workflow steps/hooks rather than prompt text — load-time agent-vs-deterministic split (§4.7, §4.9).
- [x] Validation rejects ambiguous timezones, unbounded concurrency, missing budgets, unknown labels/actions, and undeclared secret or permission scopes (§5 normative rules).
- [x] Job specs are repo-reviewable and can be loaded by the daemon without a cloud UI (§2; validation reads like a review comment, §5).

## Dependencies

[AS-159]
