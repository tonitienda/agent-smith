---
id: AS-142
title: "Archtest: add layering guard for internal/provider/conformance and schema"
status: done
area: quality
priority: medium
depends_on: [AS-098, AS-141]
---

# AS-142 — Archtest: add layering guard for `internal/provider/conformance` and `schema`

## Problem

Two packages documented as architectural leaves have no guard test in
`internal/archtest/layering_test.go`:

### `internal/provider/conformance` (AS-012)

The shared provider conformance suite lives under the provider layer and depends
only on `internal/provider`, `schema`, and stdlib. It must not import the loop,
faces, or composition roots — the same constraint as concrete provider adapters.
Without a test case, a refactor could accidentally pull in a higher-layer package
(e.g. a face helper) and CI would not object.

### `schema` (module root)

`package-contracts.md` states: `schema` must not import anything in this module
(stdlib only). The boundaries guard (`TestCoreStaysStdlibFirst`) enforces no
third-party imports, but there is no `forbidModule: true` case for `schema`
itself. An import of a first-party package from `schema` would be caught by the
Go compiler as a cycle (since everything imports schema), but the architectural
intent is not machine-documented. Adding the case makes the contract explicit and
parallel to `internal/render` and `internal/streamio`, which already have
`forbidModule: true` cases.

## Acceptance criteria

1. `layering_test.go` has a new test case:
   - `internal/provider/conformance` must not import `internal/loop`,
     `internal/tui`, `internal/serve`, `internal/smithapp`, or `cmd`.
2. `layering_test.go` has a new test case:
   - `schema` has `forbidModule: true` (must not import any module package).
3. `make test` passes.

## Implementation notes

File: `internal/archtest/layering_test.go`

Both new cases follow the existing pattern. For `schema`, set `forbidModule: true`
(same as `internal/render` and `internal/streamio`). For
`internal/provider/conformance`, add a `forbidden` list matching the concrete
provider adapter cases plus `"internal/serve"` (per AS-141) and
`"internal/smithapp"`. The `internal/smithapp` entry is critical: unlike
`cmd/*`, `smithapp` lives under `internal/` so the `"cmd"` prefix check does not
cover it, and unlike concrete adapters (which `smithapp` imports, making a cycle
impossible), `smithapp` does not import `conformance`, so the Go compiler would
not catch a layering violation there.
