---
id: AS-068
title: /clean interactive multi-select from the /context panel + per-item archive restore
status: done
github_issue: 107
depends_on: [AS-028, AS-067]
area: context-wedge
priority: P2
source: PRD.md §7.12, AS-028 follow-on
---

# AS-068 · /clean interactive selection (the in-panel affordance)

**Status: done**

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

- [x] Segments are selectable in the panel without typing handles; the selection
      drives `clean.Preview` and shows reclaimed tokens/$ live.
- [x] Confirm applies via the existing `clean.Apply` path (one exclusion event).
- [x] A single excluded block can be restored from the archive view, leaving any
      other removals in place.
- [x] No change to the log/exclusion semantics or the engine's public API beyond
      what a per-item restore needs.

## Implementation notes

- **Hosting (deliberate, not a silent punt — D0).** The interactive multi-select
  is reached via the no-arg `/clean` in the TUI rather than embedded inside the
  `/context` analytical panel. Housing it on the command it drives keeps the
  read-only `/context` view (top-consumers / by-type / duplicate / stale
  highlights, AS-026) intact and avoids mixing a cursor-driven list into a
  scrollable analytics panel. The selector still shows the live composition
  (segments, largest first), so selection happens "in the composition view" in
  spirit. The CLI `smith clean` ignores the interactive surface and renders the
  usage text (advisory/additive, like the AS-064 picker).
- **Face boundary.** A new advisory `command.Selector` (items + archive +
  `Preview`/`Apply`/`Restore` closures) rides on `command.Output`, mirroring
  `command.Picker`. The controller closes the closures over the session, so
  `internal/tui` holds only data + functions and never imports the
  projection/clean packages (the AS-021 boundary, enforced by
  `no_business_imports_test.go`).
- **Per-item restore.** `clean.RestoreBlock` cancels each active exclusion that
  drops the chosen block and re-excludes that removal's other members as a fresh
  content-only exclusion, so only the chosen block returns. Using two events
  (cancel + content-only re-removal) rather than one mixed event is what keeps
  chained restores from reactivating the original removal. No projection/exclusion
  semantics changed — it composes from the existing `DerivedFrom` mechanism.

## Dependencies

- AS-028 (clean engine + command), AS-067 (TUI inspect-mode panel framework)
