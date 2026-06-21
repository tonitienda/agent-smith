---
id: AS-072
title: Coding Mode shell — /feature & /mode entry/exit + phase-as-block
status: done
github_issue: 122
depends_on: [AS-006, AS-033, AS-040]
area: coding-mode
priority: P1
source: coding-mode.prd.md (D-CODE-1, -2, -3)
---

# AS-072 · Coding Mode shell — entry/exit + phase state

**Status: ready to implement**

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

## Clarified implementation decisions

- **Naming/shape:** support both `/feature "<prompt>"` and `/mode coding`; implement the underlying state as a generic mode primitive so future modes can reuse it. `/feature` is a convenience that sets the goal and enters `coding`.
- **Phase advancement:** advancement is user-commanded (`/phase next|back|<name>`). Smith may suggest the next phase after clear signals such as a saved plan or passing verification, but it never auto-advances.
- **Multi-feature interleaving:** V1 supports one active feature/mode instance per session. Starting a second feature prompts the user to exit or replace the current one. Phase events include a feature ID so future interleaving is additive.

## Dependencies

- AS-006 (projection — phase derived from the log), AS-033 (custom commands —
  `/feature`, `/mode`, `/phase`), AS-040 (`/goal` — entry sets the objective).

## Implementation notes

- Lifecycle core lives in `internal/mode` (mirrors `internal/goal`): `Enter`,
  `SetPhase`, `Exit`, `Current`, `History`, phase navigation, and a plain-text
  `Tracker`. Mode state is **derived from the log alone** — `Current` scans for
  the latest non-exited `mode_enter` and projects its current phase from the
  latest `phase_change`. No stored field.
- Three additive control kinds in `internal/eventlog`: `mode_enter`,
  `phase_change`, `mode_exit`. The `mode_enter` block's own ID is the mode
  instance ID; phase-change and exit events reference it via
  `Provenance.DerivedFrom`. Each entry appends a `mode_enter` plus an initial
  `phase_change`, so every instance has at least one phase event and derivation
  is uniform.
- `DefaultPhases` (think → analyse → plan → implement → verify → refactor →
  reflect) is package **data**, not control flow, so AS-074/AS-075 can override
  it. The shell never gates: `/phase next|back|<name>` always succeeds (D-CODE-2).
- Commands `/feature`, `/mode`, `/phase` registered in `cmd/smith/chat.go`;
  handlers in `cmd/smith/controller.go`. `/feature` sets the goal then enters;
  one active instance per session — a second `/feature` asks the user to exit
  first. Subcommand/slash parity flows through the shared registry (AS-066);
  `docs/project/command-parity.md` regenerated.
- The pinned phase-tracker panel and distinct TUI presentation (D-CODE-4) are
  AS-073; bundled process skills are AS-074; method override is AS-075.
