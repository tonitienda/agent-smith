---
id: AS-033
title: Custom slash commands from project and user directories
status: done
github_issue: 33
depends_on: [AS-022, AS-031]
area: commands
priority: P0
source: PRD.md §7.6
---

# AS-033 · Custom slash commands

**Status: done** — implemented in `internal/customcmd` (discovery + frontmatter
parse + `$ARGUMENTS`/`$1..$n` expansion) and wired into the chat face: custom
commands are layered over the built-ins in `cmd/smith/chat.go`, re-scanned as the
palette opens (`tui.WithCommandRefresh`), and run by submitting their expanded
template as a model turn via the new advisory `command.Output.Prompt` field. A
name colliding with a built-in is skipped; project beats user on a custom↔custom
collision (the winner's `/help` summary notes the override and its source path).

## Description

§7.6: custom commands loadable from project/user dirs (Markdown/templated), completing the slash-command story started by the AS-022 framework.

- Discovery: `.agent-smith/commands/*.md` (project) and the user-level equivalent; project wins on name collision.
- File format: Markdown body = prompt template; optional frontmatter for `description` and argument hints. Support `$ARGUMENTS` (full string) and positional `$1..$n` substitution.
- Compatibility goal: a Claude-Code-style command file works unmodified where features overlap (portability thesis, §4).
- Custom commands appear in the AS-022 palette and `/help`, marked as custom with their source path.

## Acceptance criteria

- [x] Dropping a `.md` file into the commands dir makes it invocable without restart (the palette re-scans the dirs on open via `tui.WithCommandRefresh`).
- [x] Argument substitution works for quoted and positional args (`$ARGUMENTS` and `$1..$n`; quoted spans are kept intact by the existing `command.Parse` tokenizer, then joined for `$ARGUMENTS`).
- [x] Name collisions resolve project-over-user with a visible note in `/help` (the winner's summary carries `(custom: <path>; overrides user command)`).
- [x] A representative Claude Code command file runs unmodified (same `.agent-smith/commands/*.md` layout, `description`/`argument-hint` frontmatter, and `$ARGUMENTS`/`$1` placeholders).

## Out of scope / follow-ons

- Headless/CLI invocation of custom commands: the expansion is submitted as a turn only by the interactive face, so a custom command is registered `interactive-only` for now. A headless submission seam can adopt the same `customcmd` package later.
- Live removal: a file deleted while the session runs stays registered until restart (only additions are picked up on rescan). Not required by the acceptance criteria.

## Dependencies

- AS-022 (command framework), AS-031 (directory conventions)
