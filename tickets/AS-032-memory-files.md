---
id: AS-032
title: Memory files — AGENT.md, CLAUDE.md, AGENTS.md loaded and merged hierarchically
status: ready-to-implement
github_issue: null
depends_on: [AS-006, AS-018, AS-031]
area: capability
priority: P0
source: PRD.md §7.3, §4 (competitive matrix)
---

# AS-032 · Memory files (AGENT.md / CLAUDE.md / AGENTS.md)

**Status: ready to implement**

## Description

§7.3: load and merge **all three** memory-file conventions hierarchically (user → project → directory), treating them as equivalent — this is the portability wedge applied to config: a project set up for Claude Code or Codex works in Agent Smith unmodified.

- Discovery: user-level file, project root, and parent/subdirectory files along the working path.
- All three filenames honored at each level; when multiple coexist at one level, all load with a documented, deterministic order (and a dedupe note in `/context`).
- Loaded content enters the projection as attributed `memory` segments — visible in `/context` with file path and token cost, like any other segment (§5: memory is a segment type).
- `@import`-style includes: out of scope for this ticket; note as follow-up.

## Acceptance criteria

- [ ] A project with only CLAUDE.md behaves identically to the same project with only AGENT.md (equivalence test).
- [ ] Hierarchy precedence (user → project → dir) is deterministic and tested.
- [ ] Memory segments appear in `/context` attributed to their source file with token counts.
- [ ] Files are re-read at session start; a mid-session refresh command is not required for this ticket.

## Dependencies

- AS-006 (memory as projection segments), AS-018 (loop assembles system context), AS-031 (config)
