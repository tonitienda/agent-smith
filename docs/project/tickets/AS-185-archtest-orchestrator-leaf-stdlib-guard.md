---
id: AS-185
title: "Archtest: the orchestrator stdlib-only leaves (spec, secret) have no third-party guard"
status: done
github_issue: null
depends_on: [AS-095, AS-098, AS-154, AS-163]
area: quality
priority: low
source: docs/architecture/dependency-boundaries.md; docs/architecture/package-contracts.md; internal/archtest; QA pass 2026-07-02
---

# AS-185 — Archtest: guard the orchestrator stdlib-only leaves against third-party imports

## Problem

Two orchestrator sub-packages are documented as **stdlib-only leaves**, but the
"no third-party imports" half of that promise is currently unenforced.

- `docs/architecture/package-contracts.md` calls `internal/orchestrator/spec` a
  "stdlib-only leaf" and `internal/orchestrator/secret` a "stdlib-only leaf".
- `docs/architecture/dependency-boundaries.md` states outright: "The stdlib-only
  job-spec *model* (`internal/orchestrator/spec`) stays third-party-free," and
  lists `secret` as depending on "stdlib only".

Both invariants hold **today** (`go list -f '{{.Imports}}' ./internal/orchestrator/spec`
and `.../secret` return only stdlib + first-party edges, and `forbidModule`
blocks the first-party edges), but a regression that adds a third-party import to
either leaf would pass CI silently:

1. `internal/archtest/boundaries_test.go` (`TestCoreStaysStdlibFirst`) is the only
   guard that rejects third-party imports. It `fs.SkipDir`s the **entire**
   `internal/orchestrator` subtree, because `thirdPartyAllowed` lists
   `internal/orchestrator` as a prefix — a blanket exemption the daemon
   (`gopkg.in/yaml.v3`) and run store (`modernc.org/sqlite`) genuinely need
   (AS-161, ADR D-ORCH-4). That blanket skip also swallows `spec` and `secret`.

2. `internal/archtest/layering_test.go` guards `spec` and `secret` with
   `forbidModule: true`, but `forbidModule` only fails on **first-party** imports
   (`isModuleImport`, layering_test.go). It never inspects third-party paths.

Net effect: `internal/orchestrator/spec` could import `gopkg.in/yaml.v3` — the
exact thing dependency-boundaries.md says it must not — and every arch test would
still pass. The documented invariant has no enforcement, which is precisely the
doc-vs-guard drift AS-095/AS-141/AS-145/AS-146 were created to close.

## Scope

- Add enforcement so `internal/orchestrator/spec` and `internal/orchestrator/secret`
  are asserted third-party-free, restoring the guarantee the two docs make.
- Keep the daemon (`internal/orchestrator`) and run store
  (`internal/orchestrator/store`) exempt — their SQLite/YAML deps are the
  justified, documented exceptions and must not start failing.
- No production-code change is expected; the leaves are already clean. This is a
  test-hardening / drift-guard ticket.

## Suggested implementation

Lowest-maintenance option: in `boundaries_test.go`, narrow the exemption. Instead
of skipping all of `internal/orchestrator`, exempt only the two packages that
actually need third-party deps (`internal/orchestrator` root and
`internal/orchestrator/store`), so the walker still parses `spec` and `secret`
and applies the existing stdlib-first check to them. Because `isThirdPartyAllowed`
matches on prefix and the walker `fs.SkipDir`s an allowed directory, this needs
the exemption to be expressed per-package rather than as a subtree prefix (e.g.
match exact dirs for the orchestrator entries, or list `spec`/`secret` on an
explicit "still-core" set that overrides the subtree skip). Confirm the daemon and
store stay skipped and `spec`/`secret` get scanned.

Alternative: give `layering_test.go`'s stdlib-only leaf cases (`forbidModule`) a
companion assertion that also rejects third-party imports for those `pkgDir`s, so
"stdlib-only leaf" means what it says in one place. Either approach is acceptable;
prefer whichever keeps the exemption list smallest and the intent clearest.

## Acceptance

- A deliberately-added third-party import in `internal/orchestrator/spec` or
  `internal/orchestrator/secret` fails `make test`.
- `internal/orchestrator` (yaml) and `internal/orchestrator/store` (sqlite) still
  pass.
- `docs/architecture/dependency-boundaries.md` and the boundaries test stay in
  lockstep, as that doc's "enforced by a guard test, not by review" note promises.
