---
id: AS-028
title: /clean — manual segment removal with preview, archive, and undo
status: done
github_issue: 28
depends_on: [AS-006, AS-026]
area: context-wedge
priority: P0
source: PRD.md §7.12, D3, D6
---

# AS-028 · /clean (manual selection) with preview and undo

**Status: ready to implement**

## Description

The second flagship wedge (§7.12), v1 half: **manual** semantic editing — select segments in the composition view and drop them. The natural-language matcher is AS-029 (needs clarification); everything it will need (preview, exclusion events, archive, undo) is built here.

- From `/context` (AS-026): select segments → preview pane shows exactly what's removed and tokens/$ reclaimed → confirm → an `exclusion` event (with provenance: user-manual, timestamp, block IDs) is appended. **History is never mutated** (D3).
- Excluded segments move to an off-window **archive view** (browsable from `/context`), not oblivion.
- `/clean --undo` (and per-item restore from the archive) appends a counter-event; restoration is exact.
- Guardrails: refuse to orphan structure (e.g., excluding a tool call but not its result — exclude pairs atomically); warn when removing very recent blocks.

## Acceptance criteria

- [ ] PRD AC: removing a topic's segments reclaims tokens without breaking the live thread — next turn works against both providers.
- [ ] PRD AC + §6 guardrail: undo restores the projection **exactly**; no data loss is possible (log untouched, verified by test).
- [ ] Preview shows token + $ reclaimed before any change is applied.
- [ ] Tool-call/result pairs are excluded atomically.
- [ ] Cache impact is bounded: prefix before the first excluded block still cache-hits (AS-011 invariant).

## Dependencies

- AS-006 (exclusion events + projection), AS-026 (selection UI)

## Implementation notes

Shipped as the `internal/clean` engine plus the `/clean` command (`cmd/smith`):

- **Engine (`internal/clean`)** — pure, log-free preview/apply/undo over the
  projection. `Preview` resolves block handles (the ID prefixes `/context` now
  surfaces) against the live window, expands tool-call/result pairs atomically,
  and reports tokens/$ reclaimed and recency warnings without mutating anything.
  `Apply` emits an `eventlog.KindExclusion`; `Undo` finds the most recent active
  `/clean` removal and appends the counter-exclusion that the projection engine
  (AS-006) reverses exactly. Undo is a stack (repeated removals reverse newest
  first). Token/$ figures reuse `internal/composition` so the preview matches
  `/context` exactly.
- **Command (`/clean`)** — `<handle>…` previews and stages a removal,
  `--apply` confirms, `--undo` restores, `--cancel` discards. The staged plan is
  keyed to its session, so a `/clear`/`/resume` between preview and apply
  invalidates it rather than editing the wrong log.
- **Archive view** — excluded blocks remain browsable in `/context`'s
  "Excluded from the window" section (already present from AS-026), with handles
  and a `/clean --undo` hint.

ACs met and tested (`internal/clean`, `cmd/smith` controller tests): reclaim
without breaking the live thread, exact undo (log untouched), preview before any
change, atomic tool-call/result pairing, and a stable cache prefix ahead of the
first excluded block (AS-011 invariant).

**Deferred to AS-068** (follow-on): the in-panel interactive multi-select from
the `/context` view (checkbox/keyboard selection + live preview pane) and
per-item restore from the archive. AS-028 ships the engine and a handle-based
command surface those will drive; the interactive TUI affordance is additive and
tracked separately so it isn't smuggled in silently (D0).
