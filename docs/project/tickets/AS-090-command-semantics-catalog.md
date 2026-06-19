---
id: AS-090
title: Consolidate slash and subcommand semantics in one command catalog
status: ready-to-implement
github_issue: 160
depends_on: [AS-066]
area: commands
priority: P2
source: code-improvements.md
---

# AS-090 · Consolidate slash and subcommand semantics in one command catalog

**Status: ready to implement**

## Description

Slash commands and CLI subcommands should remain semantically identical as more
features land. Today the project has shared command infrastructure, but behavior,
help text, argument parsing, and face-specific output still risk drifting across
`internal/command`, `internal/cli`, feature packages, and `cmd/smith` wiring.

Introduce one command catalog that records command names, aliases, summaries,
help text, argument contracts, and execution functions over small consumer-side
capabilities. TUI slash commands and `smith <cmd>` subcommands should adapt this
catalog rather than maintain separate semantics.

Use stdlib `flag.FlagSet` for command-specific flags where possible. Keep any
slash-specific lexical parsing isolated and feed parsed arguments into the same
command specs.

## Acceptance criteria

- [ ] A single catalog is the source of truth for shared slash/subcommand help
      and execution semantics.
- [ ] At least two existing commands are migrated as examples, including one
      with flags or arguments.
- [ ] TUI and CLI tests assert equivalent behavior for migrated commands.
- [ ] New command authoring guidance is documented for humans and agents.

## Dependencies

- AS-066 (shared slash ↔ subcommand command registry)
