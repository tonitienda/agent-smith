---
id: AS-026
title: /context — context composition view (flagship wedge, v1 scope)
status: done
github_issue: 26
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

- [x] PRD AC: a user can identify the top 3 things eating their context in under 5 seconds (top consumers are the first thing visible).
- [x] Token sum across segments equals the meter/projection total exactly.
- [x] Duplicate reads of the same file are visibly flagged with combined cost.
- [x] Selection state is exposed for AS-028 to consume.
- [x] Panel opens instantly (no model calls; pure projection data).

## Implementation notes

- `internal/composition` is the pure data model: `Build(proj, table, model, now, sort) Composition`
  turns the projection's live blocks into ranked, grouped, flagged `Segment`s, and
  `Render(Composition)` formats the plain-text full-screen panel. Face-agnostic, mirroring
  the `internal/cost` package shape, so a headless face (AS-051) renders the same view.
- Wired as the `/context [size|age|type]` full-screen command (`cmd/smith/controller.go`
  `cmdContext`, registered in `chat.go`). The pre-existing `Ctrl+G c` panel hotkey now
  resolves to it.
- **Live vs excluded:** live blocks drive the window total and the consumer
  rankings; blocks dropped from the window (`projection.Block.Live == false`) are
  itemized read-only in a dedicated "Excluded from the window" section with their
  reason, and kept out of the total — the restore candidates a later `/clean` undo
  (AS-028) acts on.
- **Token total is the per-block estimate sum** (`cost.EstimateContextTokens` over the live
  blocks, AS-063) — self-consistent by construction. This is the projection total; the
  always-visible meter (AS-025) uses provider-reported per-turn counts, which are a different
  (billing-exact) source. Per-block reported counts don't exist, so the panel is estimate-based
  as AS-063 intends.
- **Per-segment $** prices each block's estimated tokens at the active model's *input* rate
  (every live block is input on the next request). Unpriced model degrades to blank `—`.
- **Selection** is exposed as `Composition.Segments`, each carrying the stable `schema.Block`
  ID — the key the manual `/clean` view (AS-028) removes by. The *interactive* multi-select UI
  itself ships with AS-028, the ticket that consumes the selection to drive removal; AS-026
  ships the read-only composition the selection runs on (the panel framework renders static
  scrollable text today).

## Dependencies

- AS-006 (projection metadata), AS-020 (per-block tokens/$), AS-022 (full-screen command mode)
- AS-027 enriches this view with topics later — not a blocker.
