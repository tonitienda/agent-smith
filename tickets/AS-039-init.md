---
id: AS-039
title: /init — scaffold project config and memory file
status: ready-to-implement
github_issue: null
depends_on: [AS-014, AS-022, AS-032]
area: commands
priority: P1
source: PRD.md §7.16, Appendix A
---

# AS-039 · /init

**Status: ready to implement**

## Description

Parity command (§7.16): bootstrap a project for Agent Smith.

- Model-assisted scan of the repo (build system, test command, language, layout, existing CLAUDE.md/AGENTS.md) producing a draft **AGENT.md** (our canonical name; the loader treats all three as equivalent per AS-032).
- If a CLAUDE.md or AGENTS.md already exists, `/init` proposes additions to it rather than creating a competing file.
- Also scaffolds `.agent-smith/` (config stub, commands dir) with comments.
- Never clobbers: all writes go through diff preview + confirm (reuses AS-024 diff review).

## Acceptance criteria

- [ ] On a representative Go/JS repo, the generated AGENT.md correctly names build/test/lint commands.
- [ ] Existing memory files are amended via proposed diff, never overwritten.
- [ ] Re-running `/init` on an initialized project proposes only deltas.
- [ ] Generated files are immediately picked up next session (AS-032 loader).

## Dependencies

- AS-014 (repo scanning tools), AS-022 (command), AS-032 (memory conventions)
