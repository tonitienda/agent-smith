---
id: AS-090
title: Consolidate slash and subcommand semantics in one command catalog
status: done
github_issue: 160
depends_on: [AS-066]
area: commands
priority: P2
source: code-improvements.md
---

# AS-090 · Consolidate slash and subcommand semantics in one command catalog

**Status: done**

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

- [x] A single catalog is the source of truth for shared slash/subcommand help
      and execution semantics. (AS-066 already routes shared verbs through the
      registry handler and sources help from the descriptor; AS-090 adds the
      *argument contract* — `command.Command.ArgSpec` + `CheckArity` — so the
      descriptor also owns how many positionals are valid.)
- [x] At least two existing commands are migrated as examples, including one
      with arguments. (`cost` declares `ArgSpec{0,0}`; `resume`/`session resume`
      declares `{0,1}`, split CLI-side into `session list` `{0,0}` and
      `session resume` `{1,1}`.)
- [x] TUI and CLI tests assert equivalent behavior for migrated commands.
      (`internal/tui` `TestCommandArityRejectedBeforeRun` and `cmd/smith`
      `TestArityParityAcrossFaces` exercise the same descriptors.)
- [x] New command authoring guidance is documented for humans and agents.
      (`docs/architecture/package-contracts.md`, "A new command".)
- [x] Test updates also restructure affected tests to follow the Classical
      testing strategy for the touched area, so the refactor improves both
      production code and test structure. (Both new tests dispatch through the
      real face — model/`cli.App` — rather than poking internals; table-driven.)

## Implementation notes

This ticket landed the **positional argument contract** half of the catalog.
Threading parsed **flags** through the shared handler — so slash commands like
`/clean`, `/init`, `/rewind`, and `/compact` stop hand-matching
`--apply`/`--undo`/`--cancel` on `args[0]` and instead declare a `flag.FlagSet`
once that both faces parse — is a larger change (it extends the `Handler`
execution contract) and is spun out to **AS-104** so it can be designed on its
own.

## Dependencies

- AS-066 (shared slash ↔ subcommand command registry)
