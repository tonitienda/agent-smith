---
id: AS-005
title: Append-only event log store
status: done
github_issue: 5
depends_on: [AS-003]
area: core-log
priority: P0
source: PRD.md D3
---

# AS-005 · Append-only event log store

**Status: done** — implemented in `internal/eventlog` (`Log` type: append + read only, in-memory or disk-backed JSONL).

## Description

Implement the session as an **append-only, immutable event log of content blocks** (D3). This is the core data structure everything else projects from. History is never mutated; edits (`/clean`, `/rewind`, derived blocks) are themselves appended events.

- Append API only — no update or delete operations exist in the type's public surface.
- In-memory log with a disk-backed write-ahead persistence format (JSONL recommended: one event per line, append = O(1), crash-safe).
- Ordered iteration; lookup by block ID.
- Event types beyond content blocks: `exclusion` (marks blocks out of the projection) and `derived_block` (a new block computed from others), both carrying provenance (which command/agent created them, from which source blocks).

## Acceptance criteria

- [x] The public API offers append and read only; mutation is impossible by construction. (`Append` + `At`/`ByID`/`Events`/`Len`; no update or delete exists.)
- [x] A log written to disk and re-read yields identical events in identical order. (`TestDiskRoundTripIsIdentical`)
- [x] A process kill mid-append never corrupts previously written events. (JSONL append + fsync; a torn trailing line is truncated on reload — `TestTornTrailingLineIsDiscarded`.)
- [x] Exclusion and derived-block events serialize with full provenance. (`NewExclusion`/`Derive`; `TestExclusionAndDerivedCarryProvenance`.)
- [x] Property/fuzz tests cover append + reload round-trips. (`FuzzAppendReload`)

## Implementation notes

- `internal/eventlog/log.go` — `Log` is in-memory by default (`New`) or disk-backed (`Open`). `Append` validates each block (`schema.Block.Validate`), assigns the monotonic `Seq` and an append `TS`, rejects duplicate IDs, and writes one JSONL line per event (flush + `fsync`) before touching in-memory state, so disk and memory never diverge. Reads (`At`, `ByID`, `Events`, `Len`) are snapshot/lookup only. Safe for concurrent use.
- **Exclusion / derived-block events** (`internal/eventlog/events.go`) reuse the frozen schema rather than extending it (AS-003 stays untouched). Both name the blocks they remove from the projection in `provenance.derived_from`; a derived block additionally carries replacement content and a derived kind (e.g. `schema.KindCompaction`). The projection engine (AS-006) folds `derived_from` into the excluded set — one mechanism for both event types. `KindExclusion` is a non-content kind (no body), which `Block.Validate` tolerates by design.
- **Crash-safety**: a torn final line (no terminating newline) is treated as a partial write, discarded, and the file truncated to the last complete line so a later append can't concatenate onto it. A *complete* line that fails to parse is genuine corruption and is reported.

## Dependencies

- AS-003 (block schema)
