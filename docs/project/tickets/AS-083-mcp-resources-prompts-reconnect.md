---
id: AS-083
title: MCP resources, prompts, reconnect, and tools/list pagination
status: done
github_issue: 140
depends_on: [AS-036]
area: capability
priority: P2
source: PRD.md §7.4
---

# AS-083 · MCP resources, prompts, reconnect & pagination

**Status: ready to implement**

## Description

Follow-on to AS-036, which shipped the MCP client's tools path (connect,
handshake, `tools/list`, `tools/call`, namespaced registration, permissions,
per-server `/context` attribution, timeout + circuit-break isolation). This
ticket covers the §7.4 surface AS-036 deliberately left out (PRD D0 — punted
explicitly, not silently):

- **Resources** — `resources/list` + `resources/read`, surfaced so the model can
  pull MCP resources into context; attribute reads to the server with
  `FileSourceMCPResource` (already in the schema) for `/context` cost.
- **Prompts** — `prompts/list` + `prompts/get`, surfaced via the command palette
  (an MCP prompt becomes an invocable command, AS-022).
- **Reconnect on demand** — an unhealthy server (crashed/hung, currently
  circuit-broken for the session) should be re-dialable, so a restarted server's
  tools recover without restarting the session.
- **`tools/list` pagination** — follow `nextCursor` so servers that page their
  catalog expose every tool (AS-036 takes the first page only).

## Acceptance criteria

- [x] MCP resources can be listed and read into context, attributed per server.
- [x] MCP prompts appear in the command palette and expand into the conversation.
- [x] A crashed-then-restarted server's tools recover via reconnect, no session restart.
- [x] A server that pages `tools/list` exposes all of its tools.

## Dependencies

- AS-036 (MCP client tools path)

## Implementation notes

- `internal/mcp`: `initialize` now records the server's `resources`/`prompts`
  capability flags (`HasResources`/`HasPrompts`); `tools/list`, `resources/list`,
  and `prompts/list` all follow `nextCursor` through a shared `pageThrough` helper
  (capped to guard a misbehaving server). `ListResources`/`ReadResource`,
  `ListPrompts`/`GetPrompt`, and a `Reconnect` that re-dials from the stored config
  and atomically swaps the transport in. A new `rpc` helper centralises the §7.4
  isolation contract (health check, timeout, circuit-break) across every method.
- `cmd/smith`: a resource-capable server gets synthetic `mcp__<server>__list_resources`
  and `mcp__<server>__read_resource` tools; the read returns the content in a
  `file_read` block sourced `mcp_resource` and attributed to the server.
  `recordFileRead` now merges the tool's `Output.Attribution`, so the resource
  read's cost is credited per server in `/context`. Each server prompt becomes an
  `mcp__<server>__<prompt>` command that expands into a fresh user turn via
  `command.Output.Prompt`. A `/mcp` command reports server health and re-dials a
  crashed server (`/mcp reconnect [server]`) — the on-demand reconnect trigger that
  keeps the deliberate circuit-break semantics for the automatic path.
