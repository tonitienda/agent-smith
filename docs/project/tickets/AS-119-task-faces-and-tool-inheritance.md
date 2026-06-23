---
id: AS-119
title: User-delegated subagents — headless/serve wiring + child tool inheritance
status: done
github_issue: 384
depends_on: [AS-046, AS-051, AS-077]
area: subagents
priority: P2
source: spun out of AS-046
---

# AS-119 · `task` delegation across faces + child skill/MCP tool inheritance

**Status: ready to implement** *(spun out of AS-046)*

## Description

AS-046 landed user-delegated subagents (the `task` tool + `internal/delegate`
spawner) wired into the **interactive** face only, and a child runs with the
builtin file/search/shell tool set only. Two gaps remain:

1. **Faces.** The `task` tool is registered in `cmd/smith/chat.go`. Headless
   (`smith run`, AS-051) and the `serve` JSON-RPC face (AS-077) build their own
   engines and do not register it, so delegation is unavailable there. The
   spawner is face-neutral already (it takes a `parent func() delegate.Parent`
   closure); wire the same registration into the headless and serve composition
   paths. Decide how a child's permission prompt behaves on a non-interactive
   face (headless: fail-fast to denial like D-CLI-9; serve: forward as a
   server-initiated request, mirroring the parent's handling).

2. **Child tool inheritance.** A delegated child should be able to use the
   parent's skills (AS-034) and connected MCP tools (AS-036), not just the
   builtin set — otherwise a task that needs a skill or an MCP server silently
   can't. Let the `childTools` builder include the skill tool and MCP tools while
   still excluding `task` (no recursion). Mind lifecycle: MCP clients are owned by
   the parent session; a child must borrow, not re-connect or close them.

## Acceptance criteria

- [x] `smith run` and `serve` sessions expose the `task` tool with the same
      isolation, linking, cheap-tier default, and cost rollup as the TUI.
- [x] A child's permission prompt on a non-interactive face is handled per that
      face's documented policy (no hang): the child inherits the run/session gate,
      so headless denies (allowlist-then-deny) and serve forwards to the client.
- [x] A child can invoke the parent's skills and MCP tools; the `task` tool is
      still absent from the child registry (no recursion). The interactive face
      passes its skills + live MCP clients to `childTools`; headless/serve load
      neither, so their children get the builtin set.
- [x] MCP client lifecycle is unaffected by delegation (no double-close / leak):
      `childTools` registers fresh tool wrappers over the parent's live clients
      (borrow), and never dials or closes them.

## Dependencies

- AS-046 (the spawner and tool), AS-051 (headless), AS-077 (serve).
