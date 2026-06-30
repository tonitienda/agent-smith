---
id: AS-157
title: Auto-merge policies and safety gates
status: ready-to-implement
area: integrations
priority: P2
depends_on: [AS-147, AS-148, AS-149]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-157 · Auto-merge policies and safety gates

## Description

Define the deterministic policy that allows Smith-authored PRs to be merged automatically during dogfood without letting prompts decide merge eligibility.

## Acceptance criteria

- [ ] Policy inputs include PR author/branch ownership, labels, changed-file allow/deny lists, branch protection, required checks, review state, budget outcome, and repository settings.
- [ ] Auto-merge is disabled unless the job spec and repository policy both explicitly allow it.
- [ ] Failed, pending, missing, or unknown checks block merge.
- [ ] Workflow files, secret-related files, job specs, and high-risk paths can be denied or require stronger approval.
- [ ] Every merge decision records all evaluated inputs and the final allow/deny reason in the run DB and Smith event log.
- [ ] Manual override path is explicit and audited.

## Clarification (resolved 2026-06-30) — research input (AS-158)

See [orchestrator-competitive-research.md §3 AS-157](../../research/orchestrator-competitive-research.md#as-157--auto-merge-policies-and-safety-gates):
no surveyed vendor ships prompt-driven auto-merge — all defer to GitHub branch
protection + a human gate. Mirror Copilot's hard rule (agent PRs need an
independent human approval; the requester cannot self-approve); auto-merge off
unless both job spec and repo policy allow; failed/pending/missing/unknown checks
block merge; use GitHub native auto-merge; deny-list high-risk paths; record
every evaluated input + reason in run DB and session log.

## Dependencies

[AS-147, AS-148, AS-149]

## Open questions (resolved)

1. ~~Exact acceptable auto-merge policy for Smith-authored PRs (PRD Q5) is a
   product decision pending AS-149 PR automation; the ADR fixes only the
   fail-closed constraints.~~ Resolved by the AS-158 research input above:
   mirror Copilot's hard gate (independent human approval; the requester
   cannot self-approve), auto-merge off unless both job spec and repo policy
   explicitly allow it, and failed/pending/missing/unknown checks always block
   — this is the concrete policy PRD Q5 asked for, validated against every
   surveyed vendor ("no surveyed system ships prompt-driven auto-merge").
   AS-149 (PR lifecycle automation) is now itself `ready-to-implement`, so the
   sequencing blocker is also cleared.
