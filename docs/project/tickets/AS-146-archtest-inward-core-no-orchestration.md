---
id: AS-146
title: "Archtest: guard that inward-core packages do not import orchestration packages"
status: needs-clarification
area: quality
priority: low
depends_on: [AS-098]
---

# AS-146 — Archtest: guard inward-core ↛ orchestration imports

## Problem

`docs/architecture/package-contracts.md` states the load-bearing invariant that
"nothing in the inward core may import" an orchestration / consumer / face
package (the layer of `benchmark`, `delegate`, `insights`, `insightsmodel`,
`stats`, `statsindex`, `improve`, `skillrollup`, and the faces). A QA audit
(2026-06-26) verified this holds today across ~32 inward-core packages, but it is
**convention only** — `internal/archtest/layering_test.go` enforces the inverse
direction per-orchestration-package (e.g. `delegate ↛ faces`) and individual
inward leaves, never the blanket "no inward package reaches an orchestration
package."

A guard would make the most architecturally important rule in the doc
("dependencies point inward") actually unbreakable, not just reviewed.

## Why needs-clarification

Unlike AS-145 (two narrow, obviously-correct `forbidden` lists), this rule needs
a design decision before implementation, because both sets are open-ended and
must be maintained as packages are added.

## Open questions

1. **How to enumerate the two sets?** Options:
   - (a) An explicit `orchestrationPackages` allow-list + an explicit
     `inwardCorePackages` list, both hand-maintained (clear, but two lists to keep
     current — the maintenance cost AS-141/AS-145 avoided by being per-package).
   - (b) Derive "inward" as *everything except* a small allow-list of
     orchestration/face/cmd packages, and assert none of them import anything in
     that allow-list. Lower maintenance (new core packages are covered
     automatically) but a new orchestration package must be added to the allow-list
     or it gets falsely treated as inward.
   - (c) Walk `go list -deps` per orchestration package and assert none of its
     *importers* are inward — inverts the check but needs the importer graph.

   Which trade-off does the maintainer want — explicit lists (b's allow-list is
   the smallest) or automatic coverage?

2. **Is the rule worth a guard at all, or is per-package enough?** The existing
   per-package face guards already catch the most likely real regression
   (something inward importing a face). The pure "inward ↛ orchestration" case
   (e.g. `projection` importing `internal/stats`) is less likely. Is the blanket
   guard worth the list maintenance, or should this ticket be closed as
   wont-fix in favour of the per-package guards plus review?

3. **Boundary cases:** `internal/e2e` and `cmd/*` legitimately import
   orchestration packages and must be excluded; `insightsmodel→insights`,
   `statsindex→stats`, `improve→skillrollup`, `stats→skillrollup` are
   intra-orchestration and allowed. Any guard must encode these exceptions.

## Suggested resolution

Lean toward option (b): one hand-maintained `orchestrationAndFacePackages`
allow-list, asserting every *other* first-party package imports nothing in it.
This is the lowest-maintenance form and auto-covers new core packages. Confirm
before implementing.
