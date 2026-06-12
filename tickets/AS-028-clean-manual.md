---
id: AS-028
title: /clean — manual segment removal with preview, archive, and undo
status: ready-to-implement
github_issue: null
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
