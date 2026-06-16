---
id: AS-070
title: "smith <cmd> --help omits command-specific flags"
status: done
github_issue: 115
depends_on: [AS-065]
area: faces
priority: P2
source: AS-065 follow-on; Copilot review on PR #111
---

# AS-070 · `smith <cmd> --help` omits command-specific flags

**Status: done**

## Description

Spun out of a Copilot review on PR #111. `commandHelp` in `internal/cli/help.go`
renders a leaf command's usage, summary, examples, and the **global** flags block,
but never its **command-specific** flags. So `smith run --help` hides `-f`,
`smith tui --help` hides `--resume`/`--no-splash`, and `smith config set --help`
hides `--user`. The flags work; they're just undiscoverable from help.

The command-specific flags are registered through `Command.Flags(*flag.FlagSet)`,
so `commandHelp` can build a throwaway `FlagSet`, invoke `cmd.Flags` on it, and
render the registered flags — excluding the globals (which `registerGlobals`
already covers) so they aren't listed twice. The same data should feed the
`--help --output json` entry (a `flags` array) for parity with D-CLI-10.

## Acceptance criteria

- [x] `smith run --help` lists `-f`; `smith tui --help` lists `--resume` and
  `--no-splash`; `smith config set --help` lists `--user`.
- [x] Command-specific flags render in their own block, distinct from the global
  flags, with no duplication of the globals.
- [x] `--help --output json` includes the command-specific flags.
- [x] A test asserts a leaf with custom flags shows them in both text and JSON help.

## Dependencies

- AS-065 (the CLI router and `commandHelp` this extends).
