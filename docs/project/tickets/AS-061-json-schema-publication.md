---
id: AS-061
title: "Publish the block schema as JSON Schema (language-neutral contract + Go↔schema divergence guard)"
status: ready-to-implement
github_issue: null
depends_on: [AS-003, AS-004, AS-060]
area: schema
priority: P1
source: PRD.md D1, D2, D4; docs/design/block-schema-union.md §15
---

# AS-061 · Publish the block schema as JSON Schema

**Status: ready to implement** — but deliberately scheduled for the **V1-freeze window**, not now (see Timing).

## Description

The block schema is currently expressed only as Go types ([`/schema`](../../schema)) plus a prose contract ([`docs/schema/README.md`](../../schema/README.md)). The moat (PRD D1) is an **open, stable substrate that anyone can build on** — but non-Go clients (TypeScript, Python, Rust importers/exporters, validators in CI of downstream projects) have no machine-readable contract to validate against, and several constraints are awkward or impossible to express in Go structs:

- **enums** — `kind`, `role`, `tool_kind`, `cache.mode`, `reasoning.replay_scope`, `stop_reason` are open string fields in Go.
- **cardinality / required** — e.g. `tool_call` requires `tool_use_id` + `name`; `file_read` requires `path`; exactly one body must match `kind`.
- **conditional shape** — the body present must match the discriminator `kind` (a `oneOf`/`if-then` in JSON Schema).
- **value constraints** — token counts ≥ 0, RFC3339 `ts`, etc.

This ticket publishes a **JSON Schema** (draft 2020-12) for the block schema as the canonical language-neutral contract, and — critically — adds a **divergence guard** so the JSON Schema and the Go reference implementation cannot silently drift apart.

### The additive-only / forward-compat subtlety (must get right)

A naive JSON Schema would set `additionalProperties: false` and closed `enum`s — which would **reject any document written by a newer, additively-evolved schema version**, directly violating PRD D2 ("consumers tolerate missing and unknown"). The published schema must instead:

- **Allow unknown fields** (`additionalProperties` left open, or explicitly `true`) everywhere the Go types tolerate them — the whole point of the `ext` escape hatch and forward-compat.
- Treat **enums as non-exhaustive**: adding a new `kind` or `stop_reason` is an *additive* change, so a validator must not fail on an unknown value. Either document the enums as advisory (validate known values, pass unknown), or model them so unknown values validate. The ticket must decide and document which.
- Still enforce the constraints that are **invariant forever** (required keys on a known body, types of known fields, call↔result pairing key presence).

This tension — strict enough to be useful, open enough to honor additive-only — is the core design work of the ticket.

## Timing — why this is not done now

Per the AS-002 spike §15 and PRD D2, the schema is **malleable until it ships in V1** and additive-only only *from* V1. Generating and maintaining a second artifact (JSON Schema) while the Go shape is still changing — through AS-060's real-capture validation pass — would mean keeping two churning sources in sync for no payoff, maximizing exactly the divergence risk this ticket exists to prevent. So:

- **Land this in the V1-freeze window**, after AS-060 has fed its deltas back into AS-003 and the shape has stopped moving.
- It pairs with **AS-004**: that ticket's CI guard already plans to "compare generated JSON Schema against the committed baseline" to detect breaking changes. The JSON Schema artifact this ticket produces becomes the input AS-004's diff guard consumes — coordinate so there is one generator, not two.

## Deliverables

- A published JSON Schema (draft 2020-12) for the block envelope and the five content kinds, committed under `docs/schema/` (e.g. `docs/schema/block.schema.json`), versioned and marked v1, linked from `docs/schema/README.md`.
- A decision, documented in the schema and in `docs/schema/README.md`, on how enums and `additionalProperties` reconcile with additive-only / tolerate-unknown (see subtlety above).
- A **divergence guard** in tests/CI:
  - every block the Go reference implementation marshals (incl. the round-trip corpus from AS-003/AS-004) **validates** against the published JSON Schema; and
  - a curated corpus of **invalid** instances (wrong body for `kind`, missing required field, wrong type) is **rejected** by the schema — so the schema actually constrains, and a Go change that breaks an invariant fails CI.
- Decide and document whether the JSON Schema is **hand-authored** (Go round-trip test keeps it honest) or **generated from the Go types** (generator keeps it honest); either is acceptable if the guard above holds.

## Acceptance criteria

- [ ] A JSON Schema (draft 2020-12) for the envelope + five content kinds is committed under `docs/schema/`, versioned, and linked from `docs/schema/README.md`.
- [ ] A document written by the current Go types validates against the schema; the AS-003 round-trip corpus all validates.
- [ ] A document containing **unknown fields and an unknown block `kind`** still validates (forward-compat / D2) — the schema does not reject additive evolution.
- [ ] A curated invalid corpus (mismatched body/`kind`, missing `tool_use_id`/`name`/`path`, wrong field types) is rejected, proving the schema constrains.
- [ ] CI fails if the Go types and the JSON Schema diverge (a Go-marshaled instance that no longer validates, or a constraint the schema claims that the Go types violate).
- [ ] The enum / `additionalProperties` reconciliation with additive-only is explicitly documented.

## Dependencies

- AS-003 (the Go schema this mirrors) — done.
- AS-060 (real-capture validation must settle the shape before a second artifact is worth maintaining). **This ticket should land after AS-060, in the V1-freeze window.**
- AS-004 (additive-only guard) — shares the "generated JSON Schema vs committed baseline" mechanism; coordinate so there is a single generator/source of truth.
