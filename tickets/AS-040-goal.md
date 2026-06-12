---
id: AS-040
title: /goal — explicit session objective that anchors insights
status: ready-to-implement
github_issue: null
depends_on: [AS-006, AS-022]
area: commands
priority: P1
source: PRD.md §7.16, Appendix A
---

# AS-040 · /goal

**Status: ready to implement**

## Description

New power command (§7.16): set/track an explicit objective that anchors the session and the insights retro.

- `/goal "<objective>"` appends a goal event; the goal renders persistently in the status line and is pinned into the projection as a small, stable block (prefix-stable for caching, AS-011).
- `/goal` with no args shows the current goal and history; `/goal done` / re-setting records progression — all as events.
- Downstream value: `/insights` (AS-045) reads the goal to frame the retro ("did the session achieve its stated goal? where did it drift?"); drift detection itself belongs to the insights engine, not this ticket.

## Acceptance criteria

- [ ] Goal is visible in the status line and to the model (verifiable: the model can state the goal when asked).
- [ ] Goal changes are events with timestamps — full history reconstructable.
- [ ] The pinned goal block does not break cache prefix stability when set mid-session (it appends; it doesn't reorder existing blocks).
- [ ] Goal data is exposed in the session metadata for AS-045 to consume.

## Dependencies

- AS-006 (pinned block in projection), AS-022 (command framework)
