---
id: AS-145
title: "Archtest: guard loop↛cmd and face↛face/cmd, the documented-but-unenforced layering rules"
status: done
area: quality
priority: medium
depends_on: [AS-098, AS-141]
---

# AS-145 — Archtest: guard loop↛cmd and face↛face/cmd layering rules

## Problem

`docs/architecture/package-contracts.md` documents three dependency-direction
rules that the guard test in `internal/archtest/layering_test.go` does **not**
currently enforce. They hold by convention today (verified — no current
violations), but nothing stops them drifting silently, exactly the failure mode
AS-141 closed for the older face-import cases.

The documented-but-unenforced rules:

| Documented rule (package-contracts.md) | Currently guarded? |
|---|---|
| **Loop** — "Must not depend on: faces (`internal/tui`, `internal/serve`), `cmd/*`" | Faces guarded; **`cmd/*` is not** — the `loop does not import face packages` case forbids `internal/tui`/`internal/serve` only. |
| **Faces** — "Must not depend on: other faces, `cmd/*`" | **Not guarded at all** — there is no test case with `pkgDir: internal/tui` or `internal/serve`. |

So `internal/loop` could import `cmd/...`, `internal/tui` could import
`internal/serve` (or `cmd/...`), or `internal/serve` could import `internal/tui`
(or `cmd/...`), and CI would stay green.

## Acceptance criteria

1. The `loop does not import face packages` case (or a renamed equivalent) adds
   `"cmd"` to its `forbidden` slice so the loop cannot import a composition root.
2. Two new test cases are added:
   - `pkgDir: "internal/tui"`, `forbidden: ["internal/serve", "cmd"]`.
   - `pkgDir: "internal/serve"`, `forbidden: ["internal/tui", "cmd"]`.
3. `make test` passes — these purely close guard gaps; no production code change
   is expected (the rules already hold).
4. The reasons reference `package-contracts.md` and AS-098, matching the style of
   the existing cases.

## Implementation notes

File: `internal/archtest/layering_test.go`. Purely additive, mirroring AS-141.
Note `packageImports` is non-recursive, so the new face cases inspect only the
package's own `.go` files — sufficient, since a face's subpackages are covered by
their own layer rules.

`internal/tui` legitimately imports the Charmbracelet stack (third-party); that is
governed by `boundaries_test.go`, not this test, so adding a `forbidden` row for
`internal/tui` here does not conflict with the stdlib-first allow-list.
