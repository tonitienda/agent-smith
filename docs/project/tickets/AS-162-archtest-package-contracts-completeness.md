---
id: AS-162
title: Guard that every internal package is accounted for in package-contracts.md
status: ready-to-implement
github_issue: null
depends_on: [AS-098, AS-146]
area: quality
priority: P3
source: docs/architecture/package-contracts.md; QA pass 2026-06-30
---

# AS-162 · Guard that every internal package is accounted for in package-contracts.md

**Status: ready to implement** *(raised during a QA pass comparing the architecture docs, arch tests, and code; clarified against the existing `orchestrationAndFacePackages` guard pattern)*

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

## Clarification (resolved 2026-06-30)

1. **Worth the maintenance cost? Yes.** `internal/archtest/inward_core_test.go`
   already runs exactly this trade for `orchestrationAndFacePackages` and
   documents why it's the right shape: "the lowest-maintenance form of the
   guard... a new inward-core package is covered automatically... the cost is
   that a *new* orchestration/face package must be appended below, or the guard
   will treat it as inward and fail... that failure is the reminder to update
   the list." A completeness guard for `package-contracts.md` is the same
   precedent applied to the doc instead of the layering rule — proven cheap in
   this codebase, not a new kind of cost. Softening the "map is complete" claim
   instead would just relocate today's silent-drift failure mode rather than
   close it, so keep the completeness promise and enforce it.
2. **Granularity: start with the cheap backtick-token check, plus an
   allowlist for the false-match it names.** A per-package registry table is
   more boilerplate than the existing `package-contracts.md` narrative style
   warrants and would itself drift from the prose. The `composition`-the-package
   vs. "composition root" false-positive AS-162 itself names is a one-line fix:
   require the token to appear as an inline-code span (`` `composition` ``)
   immediately preceding or following a path-like context (e.g. `internal/...`)
   — and where that's still ambiguous, add the package to the same allowlist
   used for intentional omissions (Q3). This mirrors how
   `orchestrationAndFacePackages` resolves its own edge cases: a short
   maintained list beats a fully formal registry.
3. **Guard location and allowlist seed.** A new test in `internal/archtest`
   (sibling to `inward_core_test.go` and `layering_test.go`), walking
   `internal/*` and `cmd/*` directories and asserting each is either named in
   `package-contracts.md` or present in a small `docCompletenessAllowlist`
   slice in the test file — same pattern as `orchestrationAndFacePackages`.
   Seed the allowlist with packages already intentionally excluded from the
   narrative map: pure test-tooling packages (`internal/archtest` itself,
   `internal/e2e` fixtures) and any package whose only purpose is internal to
   another package's tests. Since the QA pass already closed the specific gap
   (`composition`, `credential`, `customcmd`, `hook`, `rewind`, `run`,
   `snapshot`, `goal`, `version`, `schemaguard` are all now in the doc), the
   seed allowlist should be small or empty at implementation time — confirm by
   running the walk against current `package-contracts.md` content.
4. **`dependency-boundaries.md` does not need the same guard.** Its "Core" row
   already ends in `…` and is explicitly illustrative, not a completeness
   claim — there is nothing for a guard to enforce there. One enforced map
   (`package-contracts.md`) is enough; `dependency-boundaries.md` stays a
   curated illustrative list as designed.

## Notes

- No code-vs-doc *contract* violation was found in the QA pass — the enforced
  layering and stdlib-first guards all pass; this is purely about keeping the
  human-facing narrative map honest as packages are added.
- Depends on AS-098 (the layering guard framework) and AS-146 (the blanket
  inward-core guard) as the patterns any new guard would follow.
