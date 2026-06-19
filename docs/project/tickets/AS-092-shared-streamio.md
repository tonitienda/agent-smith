---
id: AS-092
title: Extract shared stream I/O mechanics for providers and MCP
status: ready-to-implement
github_issue: 162
depends_on: [AS-010, AS-036, AS-083]
area: architecture
priority: P2
source: code-improvements.md
---

# AS-092 · Extract shared stream I/O mechanics for providers and MCP

**Status: ready to implement**

## Description

Provider adapters and MCP clients both handle context-aware HTTP/process streams,
line/event framing, close/drain behavior, byte limits, malformed frames, and
stream tests. Their domain parsing should remain separate, but the low-level I/O
mechanics can be shared.

Create a small internal package, for example `internal/streamio`, for protocol-
agnostic helpers only. Do not introduce a provider/MCP mega-abstraction. Keep
Anthropic/OpenAI/MCP event normalization in their existing packages.

## Acceptance criteria

- [ ] Shared helpers cover context-aware line/event reading, close/drain, and
      bounded reads used by at least two existing stream consumers.
- [ ] Provider and MCP domain parsing remains package-local.
- [ ] Stream tests include chunked input, malformed frames, cancellation, and
      close behavior.
- [ ] No new external dependencies are introduced.
- [ ] Test updates also restructure affected tests to follow the Classical
      testing strategy for the touched area, so the refactor improves both
      production code and test structure.

## Dependencies

- AS-010 (OpenAI provider), AS-036 (MCP client), AS-083 (MCP resources/prompts/reconnect)
