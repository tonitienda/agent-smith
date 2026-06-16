---
id: AS-040
title: /goal — explicit session objective that anchors insights
status: done
github_issue: 40
depends_on: [AS-006, AS-022]
area: commands
priority: P1
source: PRD.md §7.16, Appendix A
---

# AS-040 · /goal

**Status: done**

## Description

New power command (§7.16): set/track an explicit objective that anchors the session and the insights retro.

- `/goal "<objective>"` appends a goal event; the goal renders persistently in the status line and is pinned into the projection as a small, stable block (prefix-stable for caching, AS-011).
- `/goal` with no args shows the current goal and history; `/goal done` / re-setting records progression — all as events.
- Downstream value: `/insights` (AS-045) reads the goal to frame the retro ("did the session achieve its stated goal? where did it drift?"); drift detection itself belongs to the insights engine, not this ticket.

## Acceptance criteria

- [x] Goal is visible in the status line and to the model (verifiable: the model can state the goal when asked). The goal is a `system`-role text block that projects live into the window; `TestGoalBlockIsLiveAndModelFacing` asserts it, and `goalLabel` renders it persistently in the TUI status line.
- [x] Goal changes are events with timestamps — full history reconstructable. Setting appends a goal block; replacing/`done` append an exclusion that retires the prior goal. `goal.History` rebuilds the full ordered history from the log alone.
- [x] The pinned goal block does not break cache prefix stability when set mid-session (it appends; it doesn't reorder existing blocks). `TestSetAppendsWithoutReorder` asserts earlier live blocks are unmoved.
- [x] Goal data is exposed in the session metadata for AS-045 to consume — via `goal.Current` / `goal.History` over the event log, **not** a stored `Metadata` field. The session `Metadata` deliberately holds only fields that are *not* reconstructible from the event stream (see `internal/session`); the goal is reconstructable, so the event log is its source of truth and the `goal` package is the API AS-045 reads.

## Implementation notes

- New `internal/goal` package: `Set` (append a `Session goal: …` system text block), `Retire` (exclusion event), `Current`/`History`/`Render` (pure functions over the event log, deriving liveness from the projection).
- `/goal` registered in `cmd/smith` (`cmdGoal`); `"<objective>"` sets/replaces, no-arg shows current + history, `done` completes. Exactly one goal stays live at a time; D3 keeps all history on the log.
- Status line: `tui.Meta.Goal` + `goalLabel` (truncated) in `internal/tui`.

## Dependencies

- AS-006 (pinned block in projection), AS-022 (command framework)
