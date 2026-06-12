---
id: AS-046
title: User-delegated subagents (scoped child agents with own context)
status: ready-to-implement
github_issue: null
depends_on: [AS-013, AS-018]
area: subagents
priority: P1
source: PRD.md §7.17
---

# AS-046 · User-delegated subagents

**Status: ready to implement**

## Description

§7.17: delegate scoped tasks to child agents with their own context window; results summarized back. Distinct from the harness's system sub-agents (AS-044) — these are task workers the user/model spawns.

- A `task`/`agent` tool: prompt + optional agent type; the child runs its own loop with its **own event log** (a full session artifact — resumable, inspectable, costed like any session) linked to the parent by provenance.
- Result summarized back into the parent as an attributed sub-thread segment (visible in `/context`, cleanable via `/clean`).
- Cheap model by default for fan-out (tier resolution via AS-042 when it lands; configured default until then).
- Parallel child execution reusing the AS-019 machinery; child cost rolls up into parent `/cost` totals, itemized.
- Children inherit the parent's permission mode; `ask` prompts in a child surface in the parent TUI clearly attributed.

## Acceptance criteria

- [ ] A delegated task runs in isolation (parent context not consumed by child work) and returns a summary block.
- [ ] Child sessions are persisted, linked to the parent, and individually inspectable/resumable.
- [ ] Parallel fan-out of N children works with bounded concurrency; costs itemize per child.
- [ ] Child permission prompts are attributed and functional in the parent UI.

## Dependencies

- AS-013 (tool to expose), AS-018 (loop reuse); AS-019 (parallelism) and AS-042 (tiering) soft
