---
id: AS-006
title: Context projection engine (model-facing context as a pure projection over the log)
status: done
github_issue: 6
depends_on: [AS-005]
area: core-log
priority: P0
source: PRD.md D3, §5
---

# AS-006 · Context projection engine

**Status: done** — implemented in `internal/projection` (pure function over `[]schema.Block`, point-in-time `ProjectAt`, exclusion + derived-block folding, deterministic single reverse pass, golden test).

## Description

The model-facing context is a **projection over the event log, not stored state** (D3). Build the engine that computes "what the model sees" from the log, honoring exclusion and derived-block events.

- Pure function: `(event log, projection options) → ordered block list` ready for provider request assembly.
- Honors `exclusion` events (excluded blocks leave the projection but remain in the log/archive).
- Honors `derived_block` events (replacement blocks appear; their sources are excluded, provenance preserved).
- Deterministic and reproducible: same log → same projection, always.
- Point-in-time projection (project the log as of event N) — the structural basis for `/rewind` later.
- Per-block metadata exposed for downstream features: type, origin, token count, recency, live/excluded status. This is what `/context`, `/clean`, and the cost meter read.

## Acceptance criteria

- [x] Projection of a log with no edit events equals the raw conversation.
- [x] Excluding a block removes it from the projection without touching the log.
- [x] Undoing an exclusion (appending a counter-event) restores the projection exactly.
- [x] Point-in-time projection at any event index is correct and covered by tests.
- [x] Projection is deterministic (golden tests).

## Dependencies

- AS-005 (event log store)
