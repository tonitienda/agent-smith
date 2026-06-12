---
id: AS-004
title: Additive-only schema guard (compatibility tests + CI enforcement)
status: ready-to-implement
github_issue: null
depends_on: [AS-003]
area: schema
priority: P0
source: PRD.md D2
---

# AS-004 · Additive-only schema guard

**Status: ready to implement**

## Description

D2 commits to **additive-only from V1, forever**: no removals, no repurposing, no breaking changes. That promise needs mechanical enforcement, not discipline. Build the guardrail that makes a breaking schema change fail CI.

- Golden-file corpus: serialized sessions from schema v1 checked into the repo.
- Compatibility test: current code must parse every golden file, byte-for-byte semantics preserved.
- A schema-diff check in CI that fails if a field is removed or its type/meaning changes (e.g., compare generated JSON Schema against the committed baseline).
- Contributor docs: how to add fields (allowed) and what is forbidden.

## Acceptance criteria

- [ ] Removing or renaming any schema field causes a CI failure with a clear message.
- [ ] Adding a new optional field passes CI without touching old golden files.
- [ ] Golden sessions from v1 parse correctly and are kept permanently.
- [ ] `docs/schema/EVOLUTION.md` states the rules and the process for additions.

## Dependencies

- AS-003 (schema must exist to guard)
