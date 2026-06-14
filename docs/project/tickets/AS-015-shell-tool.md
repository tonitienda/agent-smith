---
id: AS-015
title: Shell tool (command execution, gated by permissions)
status: done
github_issue: 15
depends_on: [AS-013, AS-016]
area: tools
priority: P0
source: PRD.md §7.2, D9
---

# AS-015 · Shell tool

**Status: done**

## Description

Shell execution for the agentic loop (§7.2). Per D9, V1 ships **no OS-level sandbox** — the security boundary is the permission model (AS-016) plus the documented posture: *"Agent Smith runs with your privileges in your environment; you approve actions."*

- Execute commands via the user's shell; capture stdout/stderr (interleaved), exit code, duration.
- Configurable timeout with hard kill; output size cap with truncation marker.
- Working directory = session project root; persistent cwd across calls is **not** required for V1 (document the choice).
- Every invocation goes through the AS-016 permission check (ask / allowlist / auto) before running — no bypass path.
- macOS + Linux support.

## Acceptance criteria

- [x] Commands run, stream/capture output, and append `tool_result` blocks with exit codes.
- [x] A command exceeding the timeout is killed and reported as such to the model.
- [x] In `ask` mode, nothing executes before user approval; denial is reported to the model as feedback, not an error crash.
- [x] Output beyond the cap is truncated with an explicit marker.

## Dependencies

- AS-013 (tool runtime)
- AS-016 (permission model — hard requirement, not optional)
