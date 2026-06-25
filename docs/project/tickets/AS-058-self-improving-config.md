---
id: AS-058
title: Self-improving config (aggregated insights propose memory/skill/command edits)
status: done
github_issue: 58
depends_on: [AS-032, AS-045, AS-050]
area: insights-wedge
priority: P2
source: PRD.md §7.25
---

# AS-058 · Self-improving config

**Status: ready to implement**

## Description

§7.25: aggregate insights into proposed edits to memory, skills, and commands so the agent gets better at *your* workflow over time. Living skills (AS-048/049/050) is the first instance; this generalizes the pattern to all config surfaces. The PRD is intentionally thin here — it's the capstone, and most open questions resolve only after the upstream features generate real data.

## Clarified implementation decisions

- **Trigger/cadence:** proposals are generated on demand (`/improve` or `smith improve`) from rollup evidence; session-end jobs may record evidence but do not interrupt with proposals.
- **Threshold:** V1 requires the same actionable suggestion to recur in at least two sessions, or one high-confidence AS-048 durable fact, before a proposal is shown.
- **Edit targets:** V1 targets memory files and skills only. Custom commands and general config edits are deferred until enough examples exist.
- **Approval UX:** one pending-proposal queue rendered through `/insights`/`smith improve`; every proposal has evidence links, diff preview, accept, dismiss, and snooze.
- **Conflict handling:** proposals are keyed by target+normalized edit, deduped across sessions, and superseded when the target file changes.

## Acceptance criteria

- [ ] A friction pattern recurring across ≥2 sessions yields exactly one consolidated proposal with cross-session evidence.
- [ ] Every applied edit goes through diff preview; every proposal is dismissible with memory of the dismissal.
- [ ] Proposals never auto-apply (propose_edit permission semantics, Appendix C.5).
- [ ] Measurable claim: applying proposals reduces the measured friction in subsequent sessions (track via AS-030/AS-057 metrics).

## Dependencies

- AS-032 (memory targets), AS-045 (suggestion machinery), AS-050 (rollup evidence)

## Implementation notes (done)

Shipped as `internal/improve` + the `/improve` slash command and `smith improve`
subcommand, generalizing the living-skills `/skills` pattern:

- `improve.Build` consolidates the cross-session `skillrollup.Report` into one
  proposal per recurring finding signature that carries a target + edit, gated on
  `improve.MinSessions` (=2) distinct sessions — exactly one consolidated proposal
  per pattern (AC 1).
- `improve.Ledger` is a durable JSON file (`improve-ledger.json`, alongside the
  findings rollup and fact ledger) remembering dismiss/snooze decisions across
  sessions; keyed by target + normalized edit so a *superseding* edit is offered
  afresh (AC 2, conflict handling).
- `/improve apply <n>` lands the edit through the same shown-diff + `appendMemoryLine`
  path `/skills apply` uses and marks the finding resolved; the propose-only edit
  becomes a confirmed write only here, never from a sub-agent (AC 3, D9/C.5).

Deferred (follow-on **AS-138**): the alternate "one high-confidence AS-048 durable
fact" threshold — the rollup `Record` carries no confidence score yet, so V1 uses
the recurrence threshold only. AC 4 (measurable friction reduction via AS-030/AS-057)
is tracked there too, since it needs longitudinal data the harness gathers over time.
