---
id: AS-157
title: Auto-merge policies and safety gates
status: done
github_issue: 461
area: integrations
priority: P2
depends_on: [AS-147, AS-148, AS-149]
source: docs/project/smith-orchestrator-dogfood-prd.md
---

# AS-157 · Auto-merge policies and safety gates

## Description

Define the deterministic policy that allows Smith-authored PRs to be merged automatically during dogfood without letting prompts decide merge eligibility.

## Acceptance criteria

- [x] Policy inputs include PR author/branch ownership, labels, changed-file allow/deny lists, branch protection, required checks, review state, budget outcome, and repository settings. (`MergeFacts` in `internal/orchestrator/merge.go`; every field is snapshotted into the recorded `PolicyDecision.Inputs`.)
- [x] Auto-merge is disabled unless the job spec and repository policy both explicitly allow it. (`EvaluateMerge` requires `merge_policy.mode: auto` *and* `MergeFacts.RepoAutoMerge`; any other mode or a repo that forbids auto-merge blocks.)
- [x] Failed, pending, missing, or unknown checks block merge. (`checksNotGreen` — anything but `success`, including zero checks, is fail-closed.)
- [x] Workflow files, secret-related files, job specs, and high-risk paths can be denied or require stronger approval. (engine-owned `highRiskPrefixes`/`isHighRiskPath` deny list — not spec-permittable — covers `.github/workflows`, `.github/actions`, `.agent-smith/jobs`, and secret/credential/`.pem`/`.key` files.)
- [x] Every merge decision records all evaluated inputs and the final allow/deny reason in the run DB and Smith event log. (`runMergeStep` writes a `merge_policy` `PolicyDecision` with `Inputs` on every path; the block lands on the run's session, which the run store links — AS-151.)
- [x] Manual override path is explicit and audited. (`mode: manual` merges only via a `ManualOverride{Actor,Reason}` by a login other than the PR author; the actor and reason are recorded in the decision inputs.)

## Clarification (resolved 2026-06-30) — research input (AS-158)

See [orchestrator-competitive-research.md §3 AS-157](../../research/orchestrator-competitive-research.md#as-157--auto-merge-policies-and-safety-gates):
no surveyed vendor ships prompt-driven auto-merge — all defer to GitHub branch
protection + a human gate. Mirror Copilot's hard rule (agent PRs need an
independent human approval; the requester cannot self-approve); auto-merge off
unless both job spec and repo policy allow; failed/pending/missing/unknown checks
block merge; use GitHub native auto-merge; deny-list high-risk paths; record
every evaluated input + reason in run DB and session log.

## Implementation notes

- `internal/orchestrator/merge.go` — `EvaluateMerge(policy, facts)` is the pure,
  deterministic verdict (offline-testable, no live GitHub). Reasons are checked in
  a fixed audit order (auto-merge enabled? → repo allows? → budget → forbidden
  invariants → required predicates → independent approval), so the recorded reason
  is stable and states the strongest objection. The forbidden invariants
  (`unknown_checks`, `branch_protection_bypass`, `force_push`) and the high-risk
  path deny list are engine-owned and cannot be spelled away by a spec — the DSL
  already forbids listing them as *permitted* (`spec/validate.go` rule 12).
- Native-merge posture: `MergeActions.EnableAutoMerge` uses GitHub's own
  auto-merge, so GitHub still enforces branch protection at merge time — Smith
  never bypasses it.
- Independent-approval gate mirrors the AS-158 research (Copilot's hard rule): a
  Smith PR needs one approval from a login other than the author; the requester
  can never self-approve. Same rule gates a manual override.
- Wiring: `SessionExecutor.WithMergeActions` injects the port the same way as
  AS-149's `WithPRActions`; a `github.enable_auto_merge`/`github.merge` step routes
  through `runMergeStep` instead of the old AS-149 deferral. With no port wired (no
  credentials, the current composition root) it records an explicit deferral, so an
  orchestrator without GitHub still runs cleanly. A guarded merge step (`when:`) is
  skipped fail-closed — guard evaluation is AS-152.
- `PolicyDecision.Inputs` (additive, D2) carries every evaluated fact so the
  append-only session is a faithful audit record on both allow and deny.

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
