---
id: AS-104
title: Thread a shared flag contract through the command catalog
status: ready-to-implement
github_issue:
depends_on: [AS-090]
area: commands
priority: P2
source: AS-090
---

# AS-104 · Thread a shared flag contract through the command catalog

**Status: ready to implement**

## Description

Spun out of AS-090, which unified the **positional argument arity** contract in
the shared command catalog (`command.Command.ArgSpec` + `CheckArity`, enforced by
both faces). This ticket extends the catalog to **flags**.

Today several slash handlers hand-roll flag parsing on the raw positional slice —
`/clean`, `/init`, `/rewind`, and `/compact` string-match `--apply`, `--undo`,
and `--cancel` against `args[0]` in `cmd/smith/controller.go` — while the CLI face
parses the same surface with stdlib `flag.FlagSet` (`internal/cli` `runLeaf`,
`reorder`, `takesValue`). That is exactly the drift AS-090 set out to remove: a
flag added to a command's slash form can silently disagree with its subcommand.

Let a command declare its flags once on the descriptor (a `flag.FlagSet` binder,
mirroring `internal/cli.Command.Flags`) and have both faces parse them through one
path before the handler runs: the CLI already permutes flags ahead of positionals;
the TUI lexes the slash line (`command.Parse`) and would feed the tokens through
the same binder. Keep face-specific *lexing* isolated and feed parsed values into
the one spec.

The friction to design through: the shared `command.Handler` signature is
`func(ctx, args []string)`, so threading parsed flag *values* into the handler
either extends that signature (touches every handler and both faces) or passes a
parsed-args carrier. Pick the smallest contract that lets a flag be declared once
and honored identically by both faces.

## Acceptance criteria

- [ ] A command can declare command-specific flags once on the shared descriptor,
      parsed with stdlib `flag.FlagSet`.
- [ ] Both the TUI slash form and the `smith <verb>` subcommand parse those flags
      through one path; neither hand-matches `--flag` on `args[0]`.
- [ ] At least one mode-flag command (`/clean`, `/init`, `/rewind`, or
      `/compact`) is migrated off ad-hoc `args[0]` flag matching.
- [ ] TUI and CLI tests assert equivalent flag behavior (including an unknown
      flag and a flag written after a positional) for a migrated command.
- [ ] No new external dependencies; slash lexing stays isolated from flag parsing.
- [ ] Test updates follow the Classical testing strategy for the touched area.

## Dependencies

- AS-090 (positional argument contract in the shared catalog)
