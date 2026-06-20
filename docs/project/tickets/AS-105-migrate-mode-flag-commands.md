---
id: AS-105
title: Migrate the remaining mode-flag commands onto the shared flag contract
status: ready-to-implement
github_issue:
depends_on: [AS-104]
area: commands
priority: P2
source: AS-104
---

# AS-105 · Migrate the remaining mode-flag commands onto the shared flag contract

**Status: ready to implement**

## Description

AS-104 introduced the shared flag contract (`command.Command.Flags` +
`Command.ParseFlags`, parsed values carried on the handler context via
`command.FlagsFrom`) and migrated `/clean` off ad-hoc `args[0]` flag matching.
Three sibling commands still hand-match `--flag` on the raw positional slice in
`cmd/smith/controller.go`:

- `/init` — `--apply`, `--cancel` (`cmdInit`)
- `/rewind` — `--mark "<label>"` (string-valued), `--apply`, `--undo`, `--cancel` (`cmdRewind`)
- `/compact` — `--apply`, `--undo`, `--cancel` (`cmdCompact`)

Migrate each to declare its flags once on the descriptor (`chatCommands` in
`cmd/smith/chat.go`) and read them via `command.FlagsFrom(ctx)`, exactly as
`/clean` now does. `/rewind --mark "<label>"` exercises the string-valued flag
path (the value travels with the flag through `command.PermuteFlags`), so it is
the most valuable of the three to get right.

This is mechanical follow-through, not new design: the contract already exists.

## Acceptance criteria

- [ ] `/init`, `/rewind`, and `/compact` declare their flags via `Command.Flags`
      and read them through `command.FlagsFrom(ctx)`; none hand-matches `--flag`
      on `args[0]`.
- [ ] `/rewind --mark "<label>"` parses the label through the shared string-flag
      path (declared as a `flag.String`), not by reading `args` past the flag.
- [ ] Existing `/init`, `/rewind`, `/compact` tests pass against the flag path
      (dispatch through `ParseFlags`, mirroring the `runClean` test helper), and
      an undeclared flag is a usage error.
- [ ] No new external dependencies.

## Dependencies

- AS-104 (the shared flag contract this migrates the remaining commands onto)
