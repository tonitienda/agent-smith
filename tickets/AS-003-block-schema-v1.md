---
id: AS-003
title: Immutable content-block schema v1 (the frozen substrate)
status: ready-to-implement
github_issue: null
depends_on: [AS-002]
area: schema
priority: P0
source: PRD.md D1, D2, D3, D4
---

# AS-003 · Immutable content-block schema v1

**Status: ready to implement**

## Description

Define and implement the core data types of the open substrate: the content blocks that make up the append-only session log (D3). This schema is the product's moat (D1) and becomes **additive-only forever** once shipped (D2), so it must incorporate the union design from the AS-002 spike.

- Go types + JSON serialization for block kinds: `text`, `tool_call`, `tool_result`, `file_read`, `reasoning` — plus an event envelope (stable block ID, timestamp, role/origin, provenance).
- Token-count and cost fields on blocks (optional, fillable later by accounting).
- Unknown-field tolerance on deserialization (consumers tolerate missing/unknown — D2).
- Schema documented in `docs/schema/` as the public, versioned contract.

## Acceptance criteria

- [ ] All five block kinds round-trip through JSON losslessly.
- [ ] Every block has a stable, unique ID and provenance metadata.
- [ ] Deserializing a document with unknown extra fields succeeds (forward compatibility).
- [ ] Public schema doc published in-repo, marked v1, with the additive-only rules stated.
- [ ] Schema accounts for every mapping in the AS-002 union doc (no known provider concept is unrepresentable).

## Dependencies

- AS-002 (union design must exist before freezing)
