---
id: AS-065
title: CLI subcommand router + arg/output/exit-code contract
status: ready-to-implement
github_issue: null
depends_on: [AS-018, AS-021, AS-022]
area: faces
priority: P1
source: CLI-UX.md (D-CLI-1..10), UX.md §3.2/§9.3/§17, PRD.md §7.18
---

# AS-065 · CLI subcommand router + arg/output/exit-code contract

**Status: ready to implement**

## Description

Establishes the command-line *shape* both faces share, per
[CLI-UX.md](../CLI-UX.md). UX.md §3.2 sketched a flag-driven `smith -p "…"`; the
grilling settled on a **subcommand-first, noun-grouped** tree instead (D-CLI-1),
so the CLI needs a real router before the headless face (AS-051) or more commands
land on the wrong shape.

This ticket is the spine, not the headless feature set — AS-051 builds the
scripting/CI behaviour (output modes, budgets, permission posture) on top of it.

Scope:

- **Router (D-CLI-1).** `cmd/smith` parses argv into `command + args`, resolving
  through the existing face-neutral command registry (`internal/command`, AS-022)
  so TUI slash-commands and CLI subcommands share one handler set (D-CLI-10).
  Noun-grouped verbs: `run`, `session list|resume`, `context show|clean`,
  `cost`, `config get|set`, `tui`.
- **Bare invocation (D-CLI-2).** No args + TTY → launch the TUI (AS-021); no args
  + non-TTY → help on stderr, exit 2. `smith tui` is the explicit launch.
- **Prompt input (D-CLI-3).** `smith run` takes the prompt as a positional arg,
  from stdin when piped (or `-`), or from `-f <file>`. **No `-p` flag.**
- **Output selection (D-CLI-4).** Auto-detect TTY → human/plain vs bare plain;
  `--output plain|json|stream-json` forces; `--color auto|always|never`, honor
  `NO_COLOR`. Personality stays off on non-interactive paths.
- **Streams (D-CLI-5).** Results → stdout; diagnostics/errors → stderr.
  `--quiet/-q`, `--verbose/-v` tune stderr.
- **Config precedence (D-CLI-6).** flag > project file > user file > `SMITH_*`
  env > built-in default. (Secrets are out of this chain — keychain/env per
  AS-017.)
- **Exit codes (D-CLI-7).** `0` success, `1` runtime/task failure, `2` invalid
  usage. Richer classes reserved for AS-051 (additive).
- **Discoverability (D-CLI-10).** `--help`/`-h` + runnable examples on root and
  every subcommand; `--version`; "did you mean…?" on unknown commands;
  machine-readable help (`smith <cmd> --help --output json` dumps the registry
  entry). Shell completion deferred.
- **Accessibility (D-CLI-4).** Color is never the only signal — status/severity
  use symbols (`✓`/`✗`/`◐`) + exit code + stderr (UX.md §19).

Stdlib-only (`flag` or a hand-rolled dispatcher) unless a ticket explicitly
introduces a dependency.

## Acceptance criteria

- [ ] `smith` with no args on a TTY launches the TUI; with stdin/stdout not a TTY
  it prints usage to stderr and exits 2.
- [ ] `smith run` accepts the prompt as a positional arg, via piped stdin, and via
  `-f`; there is no `-p` flag.
- [ ] Output is plain on a non-TTY and respects `--output`/`--color`/`NO_COLOR`;
  data lands on stdout and diagnostics on stderr (verified by piping).
- [ ] Config resolves in the documented precedence order, covered by a test.
- [ ] Unknown subcommand exits 2 with a "did you mean…?" suggestion; `--version`
  and per-command `--help` with examples work; `--help --output json` emits the
  registry entry.
- [ ] A CLI subcommand and its TUI slash-command equivalent dispatch to the same
  registry handler (no duplicated command logic).

## Dependencies

- AS-018 (face-agnostic loop), AS-022 (slash-command framework + registry to
  share). AS-021 (TUI) for the bare-invocation launch path.
