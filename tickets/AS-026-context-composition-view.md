---
id: AS-026
title: /context — context composition view (flagship wedge, v1 scope)
status: ready-to-implement
github_issue: null
depends_on: [AS-006, AS-020, AS-022]
area: context-wedge
priority: P0
source: PRD.md §7.11, D6
---

# AS-026 · /context composition view

**Status: ready to implement**

## Description

The flagship differentiator (§7.11): a full-screen panel showing what is actually in the window. **V1 scope is the mechanically derivable dimensions** — segment type, file, recency, size. The *topic* dimension depends on the labeling engine (AS-027, needs clarification) and is added when that lands; the panel ships without it.

- Full-screen `/context` panel: every projected segment with type, origin (file path / tool / role), token count, $ cost, age, and live/excluded status.
- Groupings: by type (user / assistant / tool result / file read / reasoning / system+memory), by file, by recency bucket.
- Highlights (§7.11): top consumers ranked; **duplicated file reads** flagged; stale candidates (old, large, untouched-since blocks) marked.
- Keyboard navigation with multi-select — selection is the input to manual `/clean` (AS-028).
- Sort by size / age / type.

## Acceptance criteria

- [ ] PRD AC: a user can identify the top 3 things eating their context in under 5 seconds (top consumers are the first thing visible).
- [ ] Token sum across segments equals the meter/projection total exactly.
- [ ] Duplicate reads of the same file are visibly flagged with combined cost.
- [ ] Selection state is exposed for AS-028 to consume.
- [ ] Panel opens instantly (no model calls; pure projection data).

## Dependencies

- AS-006 (projection metadata), AS-020 (per-block tokens/$), AS-022 (full-screen command mode)
- AS-027 enriches this view with topics later — not a blocker.
