---
id: AS-068
title: /clean interactive multi-select from the /context panel + per-item archive restore
status: ready-to-implement
github_issue: 107
depends_on: [AS-028, AS-067]
area: context-wedge
priority: P2
source: PRD.md §7.12, AS-028 follow-on
---

# AS-068 · /clean interactive selection (the in-panel affordance)

**Status: ready to implement**

## Description

AS-028 shipped the manual-`/clean` engine (`internal/clean`) and a handle-based
command surface (`/clean <handle>… | --apply | --undo | --cancel`): the user
reads handles from the `/context` panel and types them. This ticket adds the
slick affordance the wedge demo wants — **select segments directly in the
composition view** rather than copying handles by hand:

- In the `/context` panel (built on the inspect-mode panel framework, AS-067),
  let the user move a cursor over segments and toggle a selection (e.g.
  space/checkbox), with a live preview pane showing the running tokens/$
  reclaimed (`clean.Preview` already computes this).
- Confirm applies the staged plan (`clean.Apply`); the engine, atomic pairing,
  exclusion events, and undo are all reused unchanged.
- **Per-item restore from the archive**: from the "Excluded from the window"
  section, restore a single block (not just the whole last removal `/clean
  --undo` reverses) by appending a targeted counter-exclusion.

This is purely the interactive front-end and a finer-grained undo target; the
removal semantics, provenance, and reversibility are AS-028's and do not change.

## Acceptance criteria

- [ ] Segments are selectable in the `/context` panel without typing handles;
      the selection drives `clean.Preview` and shows reclaimed tokens/$ live.
- [ ] Confirm applies via the existing `clean.Apply` path (one exclusion event).
- [ ] A single excluded block can be restored from the archive view, leaving any
      other removals in place.
- [ ] No change to the log/exclusion semantics or the engine's public API beyond
      what a per-item restore needs.

## Dependencies

- AS-028 (clean engine + command), AS-067 (TUI inspect-mode panel framework)
