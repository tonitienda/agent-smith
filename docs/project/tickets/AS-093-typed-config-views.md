---
id: AS-093
title: Add typed consumer config views over layered config
status: done
github_issue: 163
depends_on: [AS-031, AS-071]
area: foundation
priority: P2
source: code-improvements.md
---

# AS-093 · Add typed consumer config views over layered config

**Status: done**

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

- [x] At least three config consumers expose typed config structs built from the
      layered config substrate. (`budget.ConfigFrom`, `compact.ConfigFrom`,
      `permission.ConfigFrom`, `mcp.ConfigFrom`.)
- [x] Dotted path strings for those consumers are localized to their package.
      (`budget.*`, `compact.*`, `permissions`, `mcp.servers` no longer appear in
      `cmd/smith`.)
- [x] Validation/defaulting/provenance warnings live with the consuming feature.
      (Range checks and tolerate-but-warn in `budget`/`compact`; threshold
      defaulting moved from `cmd/smith` into `compact`.)
- [x] Tests cover defaults, overrides, bad types, and provenance. (Layered
      `config.New` fixtures in each `config_test.go`.)
- [x] Test updates also restructure affected tests to follow the Classical
      testing strategy for the touched area, so the refactor improves both
      production code and test structure. (New external-package tests drive each
      view through real `*config.Config` layers.)

## Convention

The typed config view convention is documented in
[docs/architecture/package-contracts.md](../../architecture/package-contracts.md)
("Where new code goes" → "Reading config for a feature").

## Dependencies

- AS-031 (layered configuration), AS-071 (config consumer migration)
