---
id: AS-046
title: User-delegated subagents (scoped child agents with own context)
status: done
github_issue: 46
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

## Delivered

The core delegation mechanism (interactive face):

- A **`task` tool** (`internal/tool/builtin/task.go`) the model invokes with a
  `prompt` (+ optional `agent_type`, `model`). It stays a pure leaf, depending
  only on a small `builtin.Spawner` consumer seam (AS-091).
- The spawner (`internal/delegate`) builds a **child agent loop over its own
  isolated, persisted event log** — the parent context is never consumed by the
  child's work — runs it to completion, and returns the child's final text as the
  tool result, attributed to the child session.
- **Child sessions are persisted and linked** to the parent (`session.CreateChild`
  records `Metadata.Parent`); they are ordinary sessions, so `List`/`/resume`
  discover and rehydrate them, and they are individually inspectable.
- **Cheap model by default** for fan-out: the child resolves the `cheap` routing
  tier (AS-042) unless the call overrides `model`.
- **Parallel fan-out, bounded concurrency**: multiple `task` calls in one turn
  dispatch through the existing `Runtime.ExecuteBatch` worker pool (AS-019); each
  `Spawn` is isolated and the parent-log rollup append is mutex-guarded.
- **Cost rollup**: the child's usage events are copied onto the parent log as a
  sidechain (`Thread.IsSidechain`, `AgentID`), so the parent's `/cost` totals and
  budget guard account for the delegated spend.
- **Permission inheritance**: the child runtime reuses the parent's permission
  gate, so a child's tool call prompts through the same parent UI and is gated.
- Recursion is bounded: the child's tool registry omits the `task` tool.
- Wired in `cmd/smith/chat.go`; guarded by an `internal/archtest` layering case.

## Deferred (spun out, per D0 — no silent punts)

- **AS-119** — wire `task` into the headless (`smith run`) and `serve` faces, and
  let a child inherit the parent's skills/MCP tools (today the child gets the
  builtin file/search/shell set only).
- **AS-120** — surface per-child cost itemization in `/cost`, attribute a child's
  permission prompt to the delegating agent in the TUI, and add an explicit
  per-child budget ceiling (today the child is bounded only by max-iterations).

## Dependencies

- AS-013 (tool to expose), AS-018 (loop reuse); AS-019 (parallelism) and AS-042 (tiering) soft
