# Evolving the content-block schema — the additive-only rules

> Status: **binding from V1, forever** · Enforced by: [`schema-guard`](../../cmd/schema-guard) + [`internal/schemaguard`](../../internal/schemaguard) · Schema: [`schema`](../../schema) (Go), [README](README.md) · Tickets: [AS-004](../project/tickets/AS-004-additive-only-guard.md) (this guard), [AS-061](../project/tickets/AS-061-json-schema-publication.md) (JSON Schema publication)

The Agent Smith content-block schema is the **open, stable data substrate** that is the project's moat (PRD **D1**). From the V1 freeze it is **additive-only, forever** (PRD **D2**). This document states the rules, the process for making a change, and how the rules are mechanically enforced so that *discipline is never the only thing standing between the schema and a breaking edit*.

## The rules

From V1 onward:

1. **Nothing is ever removed.** No field, struct type, or enum value is ever deleted.
2. **Nothing is ever renamed.** A field's Go name, its JSON wire key, a type's name, and an enum value's string are permanent.
3. **Nothing is ever retyped or repurposed.** A field's type and meaning are fixed for all time. `int` does not become `string`; `cache_read` does not start meaning something else.
4. **Wire presence does not change.** A field does not gain or lose `omitempty`; a required field stays required and an optional field stays optional.
5. **New concepts arrive only additively** — as a **new optional field**, a **new struct type**, or a **new enum value** (e.g. a new block `kind`).
6. **Consumers MUST tolerate the unknown.** An absent optional field means "not reported / not applicable", **never** an implied zero. Unknown fields and unknown block kinds MUST deserialize without error and be preserved or passed through, not rejected.
7. **There are no deprecation windows.** Nothing is ever scheduled for removal, because nothing is ever removed. A field that turns out to be a mistake is simply left in place (and a better one is added beside it).

If a concept genuinely cannot be modeled additively, it does **not** go in. Use the escape hatches instead (below), and — if it deserves promotion — add a new first-class optional field later, which *is* additive.

### The escape hatches

Every object carries an `ext` open map, and `provider.native_type` / `provider.native_id` preserve a source surface's own type and IDs verbatim. Together they guarantee that any concept the union does not yet model first-class still survives a read → store → write cycle. An adapter that meets an unmodeled concept records it in `ext`; it round-trips opaquely today and can be **promoted to a first-class optional field later with zero breaking change**. Reach for `ext` before you reach for a schema change.

## How to make an additive change

1. **Add** the new optional field (with `omitempty` and a doc comment), new struct type, or new enum constant to the [`schema`](../../schema) package. Follow the existing conventions: "missing means unreported, never zero" (use pointers where a zero value is meaningful), an `ext` map on every new sub-object, order-preserving slices.
2. If the addition is a new enum value on a guarded enum, add it to the registry in [`internal/schemaguard/descriptor.go`](../../internal/schemaguard/descriptor.go) (`enumRegistry`) so it is recorded and future removals of it are caught.
3. **Run the guard** to confirm the change is additive:

   ```sh
   make schema-guard          # or: go run ./cmd/schema-guard
   ```

   It compares the live schema against the committed baseline and fails on any removal, rename, type change, wire-presence change, or dropped enum value. Pure additions pass.
4. **Record the addition under the guard** so it is protected from future removal, and refresh the golden corpus:

   ```sh
   make schema-baseline       # or: go run ./cmd/schema-guard -update
   ```

   This rewrites [`internal/schemaguard/testdata/schema_baseline.json`](../../internal/schemaguard/testdata) and the generated golden sessions. `-update` **refuses** to run if the change is not additive — it will not let you launder a breaking change into the baseline.
5. **Commit** the schema change, the regenerated baseline, and the regenerated goldens together.
6. Update the human-facing [schema README](README.md) and (once it exists, AS-061) the published JSON Schema.

## How the guard works (and what runs in CI)

The guard lives in [`internal/schemaguard`](../../internal/schemaguard) and has three parts:

- **Reflective descriptor** — `Generate()` reflects the Go schema types reachable from `Document`/`Block` into a flat, serializable `Descriptor` of every type, field (Go name, JSON key, canonical type string, `omitempty`), and named enum. A frozen copy is committed at `testdata/schema_baseline.json`.
- **Diff** — `Compare(baseline, current)` reports a message for every **non-additive** delta and says nothing about additions. Field identity is the Go field name, so a rename reads correctly as a removal of the old contract.
- **Golden corpus** — `testdata/golden/*.json` holds serialized **v1 sessions, kept permanently**. They must keep parsing, validating, and surviving a re-emit → re-parse cycle with identical semantics. `session-forward-compat.json` carries unknown fields and an unknown block `kind` to pin the tolerate-unknown guarantee.

All of this runs in CI through the ordinary `go test ./...` (`make test`) path:

- `TestSchemaIsAdditiveOnly` fails the build on any breaking change, with a message naming each offending field/type/enum.
- `TestGoldenSessionsParse` / `TestGoldenCorpusCoversEveryContentKind` keep the permanent v1 corpus parseable and representative.
- `TestCompareDetectsBreakingChanges` proves the diff catches every breaking-change category (removed/renamed/retyped field, omitempty flip, removed type, dropped/removed enum).

The golden corpus is the floor for forward compatibility; the baseline is the floor for additive-only. Raise the floors with `-update` as new fields land — never lower them.

## Scope notes

- This guard covers the Go reference implementation's **structural** contract. A language-neutral **JSON Schema** for non-Go clients, plus a Go↔schema divergence guard with enums and `additionalProperties` reconciled against the tolerate-unknown rule, is **[AS-061](../project/tickets/AS-061-json-schema-publication.md)**, scheduled for the V1-freeze window.
- Pre-V1, the schema is still malleable (the union spike's design window): breaking changes surfaced by the real-capture pass **[AS-060](../project/tickets/AS-060-session-capture-corpus.md)** must be made **now**, before the freeze. After V1, only the additive path above remains.
