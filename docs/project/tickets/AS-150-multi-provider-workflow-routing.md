---
id: AS-150
title: Multi-provider workflow routing
status: ready-to-implement
area: provider
priority: P2
depends_on: [AS-160]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-150 · Multi-provider workflow routing

## Description

Design how orchestrated jobs bind workflow roles to provider/model routing policies so schedules and GitHub triggers are owned by Smith rather than vendor-specific schedulers.

## Acceptance criteria

- [ ] Workflow steps can declare roles such as implementation, review, architecture-check, manual-test, documentation-drift, and image-generation.
- [ ] Roles can bind to explicit providers/models or named routing policies.
- [ ] Provider selection is recorded in the run DB and Smith event log per step.
- [ ] Budgets can be configured per job and per step.
- [ ] The design explains how skills/subagents may influence routing without making the workflow nondeterministic.
- [ ] Examples show Anthropic implementation plus GPT review, alternating scheduled architecture/manual-test checks, and specialist provider selection for non-code tasks.

## Clarification (resolved 2026-06-30)

This ticket carried no ticket-local open question; it was held at
`needs-clarification` pending the [orchestrator ADR](../../architecture/orchestrator-architecture.md)
(AS-159), which is now **Accepted**, and AS-160 (job-spec DSL), which is now
**done**. The ADR resolves PRD Open Q6 directly: "**Decided:** both supported —
explicit per-step provider policy or a named routing policy/skill (D-ORCH-4).
Mechanism → AS-150." Its D-ORCH-4 boundary table assigns the implementation
seam: "Provider routing | core routing (AS-042/110) via per-step policy
(AS-150) | Role↔provider separation; explicit per step or a named routing
policy." So the design direction is fixed — bind workflow roles to providers by
reusing the existing AS-042/AS-110 routing/escalation substrate rather than
building a second routing path, with per-step explicit overrides as the
escape hatch named in the acceptance criteria.

## Dependencies

[AS-160]
