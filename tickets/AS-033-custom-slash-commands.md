---
id: AS-033
title: Custom slash commands from project and user directories
status: ready-to-implement
github_issue: null
depends_on: [AS-022, AS-031]
area: commands
priority: P0
source: PRD.md §7.6
---

# AS-033 · Custom slash commands

**Status: ready to implement**

## Description

§7.6: custom commands loadable from project/user dirs (Markdown/templated), completing the slash-command story started by the AS-022 framework.

- Discovery: `.agent-smith/commands/*.md` (project) and the user-level equivalent; project wins on name collision.
- File format: Markdown body = prompt template; optional frontmatter for `description` and argument hints. Support `$ARGUMENTS` (full string) and positional `$1..$n` substitution.
- Compatibility goal: a Claude-Code-style command file works unmodified where features overlap (portability thesis, §4).
- Custom commands appear in the AS-022 palette and `/help`, marked as custom with their source path.

## Acceptance criteria

- [ ] Dropping a `.md` file into the commands dir makes it invocable without restart (rescan on palette open is acceptable).
- [ ] Argument substitution works for quoted and positional args.
- [ ] Name collisions resolve project-over-user with a visible note in `/help`.
- [ ] A representative Claude Code command file runs unmodified.

## Dependencies

- AS-022 (command framework), AS-031 (directory conventions)
