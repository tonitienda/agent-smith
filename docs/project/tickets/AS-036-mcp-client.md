---
id: AS-036
title: MCP client (stdio + HTTP/SSE servers)
status: done
github_issue: 36
depends_on: [AS-013, AS-016, AS-031]
area: capability
priority: P0
source: PRD.md §7.4
---

# AS-036 · MCP client

**Status: done** — `internal/mcp` connects stdio and HTTP/SSE servers, runs the
MCP handshake, lists tools, and calls them; `cmd/smith` registers each tool under
`mcp__<server>__<tool>` so calls flow through permissions, logging, and
`/context` attribution like native tools. Resources/prompts, on-demand reconnect,
and tools/list pagination are tracked as the AS-083 follow-on.

## Description

§7.4: connect to stdio and HTTP/SSE MCP servers and expose their tools/resources/prompts to the loop.

- Server config in AS-031 (`mcp_servers:` — command/args for stdio, URL + headers for HTTP/SSE), project- and user-level.
- MCP tools registered into the tool runtime (AS-013) with namespaced names (`mcp__<server>__<tool>`), so they flow through permissions (AS-016), logging, and `/context` attribution like native tools.
- Resources and prompts exposed; prompts surfaced via the command palette.
- Isolation: a crashing or hanging server degrades only its own tools (timeout + circuit-break), never the session. Reconnect on demand.
- Large MCP results obey the same truncation rules as native tools — oversized tool dumps are exactly what `/context` and `/insights` later call out (§7.14 example).

## Acceptance criteria

- [ ] A stdio server and an HTTP/SSE server both connect, list tools, and execute calls end-to-end into the event log.
- [ ] MCP tool calls respect permission modes (ask/allowlist/auto) like native tools.
- [ ] Killing a connected server mid-session leaves the session healthy; its tools report unavailable.
- [ ] MCP results are attributed per server in `/context` with token costs.

## Dependencies

- AS-013 (tool registry), AS-016 (permissions), AS-031 (config)
