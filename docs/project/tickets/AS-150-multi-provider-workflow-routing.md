---
id: AS-150
title: Multi-provider workflow routing
status: needs-clarification
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

## Dependencies

[AS-160]

## Open questions

1. Whether per-step routing reuses the existing routing policy surface (AS-042/AS-110) verbatim or needs an orchestrator-specific policy name space.
