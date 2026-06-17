---
id: AS-072
title: Coding Mode shell — /feature & /mode entry/exit + phase-as-block
status: needs-clarification
github_issue: null
depends_on: [AS-006, AS-033, AS-040]
area: coding-mode
priority: P1
source: coding-mode.prd.md (D-CODE-1, -2, -3)
---

# AS-072 · Coding Mode shell — entry/exit + phase state

**Status: needs-clarification**

## Description

The lifecycle core of Coding Mode (see [coding-mode.prd.md](../coding-mode.prd.md)):
an opinionated, soft, process-driven working mode that guides a feature through
think → analyse → plan → implement → verify → refactor → reflect → loop. This
ticket is the **thin core** (D-CODE-1) — no new engine; it composes existing
primitives and records mode state on the append-only log.

- `/feature "<prompt>"` (and `/mode coding`) **enters** the mode: sets the goal
  via `/goal` (AS-040), appends a `mode-enter` event, and starts at the **think**
  phase. `/mode off` (or Esc out of the mode shell) **exits**; phase history
  stays on the log.
- **Phases are derived blocks over the event log (D-CODE-3 / D3):** a phase
  transition is an appended event; "current phase" is a projection over those
  events. No mutable side-state. New event/block types are additive (D2).
- **Soft advisor, never gates (D-CODE-2):** the user can advance, skip, or jump
  back to any phase with one command; no phase is a hard precondition for
  another. Smith *suggests* the next phase but never blocks work.
- Phase definitions (order, stance, which skills belong to each) come from a
  default house method (D-CODE-5.1); override hooks for the skill pack (AS-074)
  and project memory (AS-075) are out of scope here but the phase list must be
  data, not hardcoded control flow, so those can extend it.

## Acceptance criteria

- [ ] `/feature "<prompt>"` enters the mode, sets a goal, and lands in **think**;
      `/mode coding` does the same without a prompt; `/mode off` exits.
- [ ] Entering, exiting, and every phase transition are **events on the log**;
      the current phase is reconstructable from the log alone (no stored field
      that isn't derivable).
- [ ] The user can move to any phase (`/phase <name>`, or next/back) at any time;
      nothing refuses to run because of the current phase.
- [ ] New event types are additive-only and pass the AS-004 schema guard.
- [ ] With the mode off, behavior is unchanged from today (zero cost when unused).

## Open questions

Blocked on design decisions still open in [coding-mode.prd.md](../coding-mode.prd.md):

- **Q1 — naming/shape:** `/feature` (task-shaped) vs `/mode coding` (mode-shaped)
  vs both; whether "mode" generalises to a reusable primitive (future review/debug
  modes) — which would change how entry/exit and phase state are built.
- **Q2 — phase-advancement trigger:** what prompts think→analyse→…? User command,
  model judgement, or a signal (goal set / tests green)? Soft never auto-advances,
  but the prompt for the "yes" still needs a defined trigger.
- **Q4 — multi-feature interleaving:** single-goal at a time, or multiple features
  in-flight in one session each with its own phase state? Changes the phase-state
  model.

## Dependencies

- AS-006 (projection — phase derived from the log), AS-033 (custom commands —
  `/feature`, `/mode`, `/phase`), AS-040 (`/goal` — entry sets the objective).
