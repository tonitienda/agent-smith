---
id: AS-162
title: Guard that every internal package is accounted for in package-contracts.md
status: done
github_issue: null
depends_on: [AS-098, AS-146]
area: quality
priority: P3
source: docs/architecture/package-contracts.md; QA pass 2026-06-30
---

# AS-162 · Guard that every internal package is accounted for in package-contracts.md

**Status: done** *(raised during a QA pass comparing the architecture docs, arch tests, and code; the doc claim that caused the drift is now enforced)*

## Description

`docs/architecture/package-contracts.md` states its "Feature-leaf peers"
paragraph is "called out so the map is complete." During a QA pass (2026-06-30)
the map was found to have silently drifted: several real packages were absent
from it — `composition`, `credential`, `customcmd`, `hook`, `rewind`, `run`,
`snapshot`, `goal`, `version`, and the `schemaguard` guard. The same QA pass
filled the gap directly (see the `[QA]` PR that introduced this ticket), but the
underlying cause is structural: **nothing enforces that the documented map stays
complete.**

The directional contracts in `package-contracts.md` *are* guarded
(`internal/archtest/layering_test.go`, `inward_core_test.go`) and the third-party
boundary is guarded (`boundaries_test.go`), so those cannot drift. The
*completeness* of the prose map is not — it relies on review, which is exactly
the failure mode the existing guards were written to remove. A new package can be
added with a correct layering and still never appear in the doc, and no test will
notice.

This ticket asks whether to add a guard test (analogous to the existing
`archtest` guards) that fails when a first-party package directory is neither
mentioned in `package-contracts.md` nor on an explicit allowlist, so the doc and
the code cannot silently diverge.

## Resolved decisions

1. **Worth the guard, not a softened claim.** The doc keeps its completeness
   promise and a test enforces it — the same trade `orchestrationAndFacePackages`
   and `thirdPartyAllowed` already make. The QA pass that filed this ticket is the
   proof the review-only path drifts; a ~110-line stdlib guard is cheaper than the
   recurring manual audit.
2. **"Accounted for" = an exact backticked token.** The guard collects every
   ``-quoted span in `package-contracts.md` and requires each package directory to
   appear as either its basename (`goal`) or its full module-relative path
   (`internal/loop`). Exact-token matching avoids the false-positive the question
   flagged (`composition` the package vs. the prose "composition root", which is
   unbackticked). No separate per-package registry table — the prose mentions the
   doc already carries are the registry.
3. **Lives in `internal/archtest`**, walking the immediate children of `internal/`
   and `cmd/` that ship production (non-test) Go. Test-only dirs (e.g. `archtest`
   itself) are skipped. Allowlist seed = repo tooling outside the architecture map:
   `cmd/capture-fixture`, `cmd/schema-guard`, `cmd/ticket-sync`. The three core
   seams the prose never named explicitly — `permission`, `session`, `budget` —
   were given a backticked callout in the doc rather than allowlisted, since they
   are real architecture, not tooling.
4. **One enforced map is enough.** `dependency-boundaries.md` stays illustrative
   (its core list ends in `…` and claims no completeness), so it needs no guard.

## Implementation

- `internal/archtest/package_contracts_completeness_test.go` —
  `TestPackageContractsCompleteness` walks `internal/*` and `cmd/*` and fails when
  a production package is neither named in `package-contracts.md` nor on
  `docCompletenessAllowlist`.
- `docs/architecture/package-contracts.md` — added the cross-cutting-seam callout
  (`permission`/`session`/`budget`) and a note that the map's completeness is now
  guarded by the test.

## Notes

- No code-vs-doc *contract* violation was found in the QA pass — the enforced
  layering and stdlib-first guards all pass; this is purely about keeping the
  human-facing narrative map honest as packages are added.
- Depends on AS-098 (the layering guard framework) and AS-146 (the blanket
  inward-core guard) as the patterns any new guard would follow.
