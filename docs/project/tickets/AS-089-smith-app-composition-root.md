---
id: AS-089
title: Shrink cmd/smith into a thin composition root
status: ready-to-implement
github_issue: null
depends_on: [AS-065, AS-066]
area: architecture
priority: P2
source: code-improvements.md
---

# AS-089 · Shrink cmd/smith into a thin composition root

**Status: ready to implement**

## Description

`cmd/smith` has become the largest production package and now mixes process
entry, CLI routing, dependency construction, provider/session setup, command
registration, TUI/headless startup, MCP, hooks, permissions, and parity glue.
That is workable but makes every new face or command add more executable-package
branching.

Introduce a focused application package, for example `internal/app` or
`internal/smithapp`, that owns reusable Smith application wiring. Keep
`cmd/smith` as the thin composition root: parse process-level flags, load config,
construct the app, and call the requested mode.

Prefer concrete structs and option/config values over a large `App` interface.
The executable may receive small consumer-side interfaces where it needs seams,
but constructors should return structs.

## Acceptance criteria

- [ ] Reusable session/provider/tool/command wiring moves out of `cmd/smith` into
      a focused internal package.
- [ ] `cmd/smith` remains responsible for process entry and flag/subcommand
      dispatch only.
- [ ] Existing TUI and headless behavior is unchanged.
- [ ] Tests cover the new application wiring without requiring terminal UI
      startup.
- [ ] Documentation or agent guidance is updated if the package layout changes
      how future features should be wired.

## Dependencies

- AS-065 (CLI router), AS-066 (shared command registry parity)
