---
id: AS-083
title: MCP resources, prompts, reconnect, and tools/list pagination
status: ready-to-implement
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

- [ ] MCP resources can be listed and read into context, attributed per server.
- [ ] MCP prompts appear in the command palette and expand into the conversation.
- [ ] A crashed-then-restarted server's tools recover via reconnect, no session restart.
- [ ] A server that pages `tools/list` exposes all of its tools.

## Dependencies

- AS-036 (MCP client tools path)
