---
id: AS-005
title: Append-only event log store
status: ready-to-implement
github_issue: null
depends_on: [AS-003]
area: core-log
priority: P0
source: PRD.md D3
---

# AS-005 · Append-only event log store

**Status: ready to implement**

## Description

Implement the session as an **append-only, immutable event log of content blocks** (D3). This is the core data structure everything else projects from. History is never mutated; edits (`/clean`, `/rewind`, derived blocks) are themselves appended events.

- Append API only — no update or delete operations exist in the type's public surface.
- In-memory log with a disk-backed write-ahead persistence format (JSONL recommended: one event per line, append = O(1), crash-safe).
- Ordered iteration; lookup by block ID.
- Event types beyond content blocks: `exclusion` (marks blocks out of the projection) and `derived_block` (a new block computed from others), both carrying provenance (which command/agent created them, from which source blocks).

## Acceptance criteria

- [ ] The public API offers append and read only; mutation is impossible by construction.
- [ ] A log written to disk and re-read yields identical events in identical order.
- [ ] A process kill mid-append never corrupts previously written events.
- [ ] Exclusion and derived-block events serialize with full provenance.
- [ ] Property/fuzz tests cover append + reload round-trips.

## Dependencies

- AS-003 (block schema)
