---
id: AS-141
title: "Archtest: add internal/serve to faces forbidden list in layering contracts"
status: ready-to-implement
area: quality
priority: high
depends_on: [AS-098]
---

# AS-141 — Archtest: add `internal/serve` to faces forbidden list in layering contracts

## Problem

`internal/serve` is the second face (`internal/tui` is the first), but six test
cases in `internal/archtest/layering_test.go` guard against `internal/tui` only
when checking that a core package must not depend on a face. If any of these core
packages accidentally imported `internal/serve`, CI would not catch it.

The affected test cases and their `forbidden` lists:

| Test case | Current forbidden | Missing |
|---|---|---|
| `loop does not import face packages` | `["internal/tui"]` | `"internal/serve"` |
| `event log does not import projection, provider, loop, or faces` | `[..., "internal/tui"]` | `"internal/serve"` |
| `projection does not import provider, loop, or faces` | `[..., "internal/tui"]` | `"internal/serve"` |
| `tools do not import loop or faces` | `["internal/loop", "internal/tui"]` | `"internal/serve"` |
| `anthropic adapter does not import loop, faces, or composition roots` | `["internal/loop", "internal/tui", "cmd"]` | `"internal/serve"` |
| `openai adapter does not import loop, faces, or composition roots` | `["internal/loop", "internal/tui", "cmd"]` | `"internal/serve"` |

The newer test cases (`builtin tools`, `delegate`, `manifest`, `otelexport`)
already guard against both faces correctly. The older cases predate
`internal/serve` (landed in AS-077) and were never updated.

`docs/architecture/package-contracts.md` already describes faces as
`internal/tui, internal/serve`; the archtest enforcement lags the documentation.

## Acceptance criteria

1. All six test cases above have `"internal/serve"` added to their `forbidden`
   slice.
2. `make test` passes (no current violations exist; this purely closes the guard
   gap).
3. A comment or the test name makes clear that "faces" means both faces.

## Implementation notes

File: `internal/archtest/layering_test.go`

The change is purely additive — add `"internal/serve"` to the `forbidden` slice
of each affected test case. No production code changes needed.
