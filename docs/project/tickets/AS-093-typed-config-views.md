---
id: AS-093
title: Add typed consumer config views over layered config
status: ready-to-implement
github_issue: 163
depends_on: [AS-031, AS-071]
area: foundation
priority: P2
source: code-improvements.md
---

# AS-093 · Add typed consumer config views over layered config

**Status: ready to implement**

## Description

`internal/config` is the generic layered configuration substrate. As more
features consume config, dotted string paths should not spread through the
codebase. Each feature package should own a small typed view that validates and
normalizes its settings.

Keep `internal/config` generic. In consumer packages, add constructors such as
`permission.ConfigFrom`, `hook.ConfigFrom`, `mcp.ConfigFrom`, or
`budget.ConfigFrom` that accept a tiny consumer-side reader interface and return
concrete validated structs.

## Acceptance criteria

- [ ] At least three config consumers expose typed config structs built from the
      layered config substrate.
- [ ] Dotted path strings for those consumers are localized to their package.
- [ ] Validation/defaulting/provenance warnings live with the consuming feature.
- [ ] Tests cover defaults, overrides, bad types, and provenance.
- [ ] Test updates also restructure affected tests to follow the Classical
      testing strategy for the touched area, so the refactor improves both
      production code and test structure.

## Dependencies

- AS-031 (layered configuration), AS-071 (config consumer migration)
