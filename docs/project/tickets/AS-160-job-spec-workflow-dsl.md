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

## Acceptance criteria

- [x] Spec covers stable job ID, owner, repo/org scope, triggers, concurrency, timeout, retries, budgets, steps, hooks, provider routing, GitHub permissions, secret scopes, PR policy, and retention.
- [x] Examples cover cron, manual dispatch, issue labeled, PR labeled, PR merged, and bounded follow-up runs.
- [x] Deterministic actions such as `github.add_label`, `github.create_or_update_pr`, and `github.enable_auto_merge` are modeled as workflow steps/hooks rather than prompt text.
- [x] Validation rejects ambiguous timezones, unbounded concurrency, missing budgets, unknown labels/actions, and undeclared secret or permission scopes — specified as the normative validation contract a conformant loader MUST enforce (§12).
- [x] Job specs are repo-reviewable and can be loaded by the daemon without a cloud UI.

## Dependencies

[AS-159]

## Outcome

Design deliverable: [docs/design/job-spec-dsl.md](../../design/job-spec-dsl.md) — the
versioned `.agent-smith/jobs/*.yaml` format (every top-level field, the six trigger
shapes, the deterministic-action catalog, provider routing, permission/secret-scope
declarations, PR/merge policy, retention) plus the normative validation contract
(§12) and six worked conformance examples (§13). Directory convention established at
[`.agent-smith/jobs/README.md`](../../../.agent-smith/jobs/README.md); linked from the
architecture README.

Per the AS-159 ADR (D-ORCH-4), this ticket fixes the **schema and validation
contract**; runtime loading/validation, the YAML-parser dependency, and execution are
**AS-161**. The deliberate YAML-over-JSON format choice (vs ADR-0002 for config) and
the AS-161-owned parser dependency are recorded explicitly in §3 rather than punted
silently (PRD D0). Action implementations (AS-147/AS-149), routing mechanism (AS-150),
secret contract (AS-154), and merge-gate semantics (AS-157) bind to this surface.
