---
id: AS-003
title: Immutable content-block schema v1 (the frozen substrate)
status: done
github_issue: 3
depends_on: [AS-002]
area: schema
priority: P0
source: PRD.md D1, D2, D3, D4
---

# AS-003 · Immutable content-block schema v1

**Status: done** — implemented in the [`schema`](../../../schema) Go package; public contract published at [`docs/schema/README.md`](../../schema/README.md).

## Description

Define and implement the core data types of the open substrate: the content blocks that make up the append-only session log (D3). This schema is the product's moat (D1) and becomes **additive-only forever** once shipped (D2), so it must incorporate the union design from the AS-002 spike.

- Go types + JSON serialization for block kinds: `text`, `tool_call`, `tool_result`, `file_read`, `reasoning` — plus an event envelope (stable block ID, timestamp, role/origin, provenance).
- Token-count and cost fields on blocks (optional, fillable later by accounting).
- Unknown-field tolerance on deserialization (consumers tolerate missing/unknown — D2).
- Schema documented in `docs/schema/` as the public, versioned contract.

## Acceptance criteria

- [x] All five block kinds round-trip through JSON losslessly. (`TestBlockRoundTripLossless`)
- [x] Every block has a stable, unique ID and provenance metadata. (`Block.ID` + `NewID`, `Provenance`; `TestNewIDUniqueAndPrefixed`)
- [x] Deserializing a document with unknown extra fields succeeds (forward compatibility). (`TestUnknownFieldTolerance`)
- [x] Public schema doc published in-repo, marked v1, with the additive-only rules stated. (`docs/schema/README.md`)
- [x] Schema accounts for every mapping in the AS-002 union doc (no known provider concept is unrepresentable). (envelope + five bodies + `ext`/`native_*` escape hatches)

## Dependencies

- AS-002 (union design must exist before freezing)
