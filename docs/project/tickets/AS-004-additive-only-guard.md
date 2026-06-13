---
id: AS-004
title: Additive-only schema guard (compatibility tests + CI enforcement)
status: done
github_issue: 4
depends_on: [AS-003]
area: schema
priority: P0
source: PRD.md D2
---

# AS-004 · Additive-only schema guard

**Status: done**

Implemented as the [`schema-guard`](../../../cmd/schema-guard) tool and the [`internal/schemaguard`](../../../internal/schemaguard) package: a reflective schema descriptor diffed against a committed baseline (`testdata/schema_baseline.json`), a permanently-kept golden v1 session corpus (`testdata/golden/`), and the contributor process in [`docs/schema/EVOLUTION.md`](../../schema/EVOLUTION.md). Enforcement runs in CI through the existing `go test ./...` path (`TestSchemaIsAdditiveOnly`, golden-parse tests, and `TestCompareDetectsBreakingChanges`). `make schema-guard` checks on demand; `make schema-baseline` records additive changes (and refuses breaking ones).

## Description

D2 commits to **additive-only from V1, forever**: no removals, no repurposing, no breaking changes. That promise needs mechanical enforcement, not discipline. Build the guardrail that makes a breaking schema change fail CI.

- Golden-file corpus: serialized sessions from schema v1 checked into the repo.
- Compatibility test: current code must parse every golden file, byte-for-byte semantics preserved.
- A schema-diff check in CI that fails if a field is removed or its type/meaning changes (e.g., compare generated JSON Schema against the committed baseline).
- Contributor docs: how to add fields (allowed) and what is forbidden.

## Acceptance criteria

- [x] Removing or renaming any schema field causes a CI failure with a clear message.
- [x] Adding a new optional field passes CI without touching old golden files.
- [x] Golden sessions from v1 parse correctly and are kept permanently.
- [x] `docs/schema/EVOLUTION.md` states the rules and the process for additions.

## Dependencies

- AS-003 (schema must exist to guard)
