---
id: AS-162
title: Guard that every internal package is accounted for in package-contracts.md
status: needs-clarification
github_issue: null
depends_on: [AS-098, AS-146]
area: quality
priority: P3
source: docs/architecture/package-contracts.md; QA pass 2026-06-30
---

# AS-162 · Guard that every internal package is accounted for in package-contracts.md

**Status: needs clarification** *(raised during a QA pass comparing the architecture docs, arch tests, and code; the doc claim that caused the drift is unenforced)*

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

## Open questions

1. **Is a completeness guard worth the maintenance cost?** It would require every
   new package to be either named in the doc or added to an allowlist, on pain of
   a failing test. That is the same trade the `orchestrationAndFacePackages`
   allowlist already makes (a new orchestration package must be appended). Is the
   doc-completeness payoff worth a second list to keep in sync, or is the
   "map is complete" claim better *softened* (drop the completeness promise) so
   the doc stays a curated guide rather than an exhaustive registry?
2. **What counts as "accounted for"?** A crude check (package basename appears as
   a backticked token somewhere in the file) is cheap but easy to satisfy
   trivially and prone to false matches (e.g. `composition` the package vs.
   "composition root"). A stricter check (an explicit per-package registry table)
   is more honest but more boilerplate. Which granularity?
3. **Where should the guard live and what is its allowlist seed?** Presumably a
   new test in `internal/archtest` walking `internal/*` and `cmd/*`, seeded with
   the packages intentionally not in the narrative map (pure leaves already
   covered by `dependency-boundaries.md`, test tooling, etc.). Confirm the seam
   and the initial allowlist before implementing.
4. **Does the same drift risk apply to `dependency-boundaries.md`?** Its core
   list is illustrative (ends in `…`), so it does not claim completeness today.
   Should it, or is one enforced map enough?

## Notes

- No code-vs-doc *contract* violation was found in the QA pass — the enforced
  layering and stdlib-first guards all pass; this is purely about keeping the
  human-facing narrative map honest as packages are added.
- Depends on AS-098 (the layering guard framework) and AS-146 (the blanket
  inward-core guard) as the patterns any new guard would follow.
