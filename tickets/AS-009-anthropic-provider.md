---
id: AS-009
title: Anthropic provider implementation
status: ready-to-implement
github_issue: null
depends_on: [AS-008]
area: provider
priority: P0
source: PRD.md §7.1, D6
---

# AS-009 · Anthropic provider implementation

**Status: ready to implement**

## Description

Implement the provider interface against the Anthropic Messages API.

- Streaming (SSE) with all event types normalized to the AS-008 stream events.
- Tool use: tool definitions out, `tool_use` blocks in, `tool_result` blocks back — round-tripped through our block schema.
- Reasoning/thinking block support where the model emits it.
- Usage capture per turn: input, output, cache-read, cache-write tokens.
- Auth via API key (sourced through the key-storage layer, AS-017; env var works until then).
- Honest error mapping into the AS-008 taxonomy, with retry/backoff on rate limits.

## Acceptance criteria

- [ ] A full agentic turn (user → assistant with tool calls → tool results → assistant) works end-to-end against the real API (smoke test behind an env flag; not in CI).
- [ ] All conformance fixtures (AS-012) pass.
- [ ] Usage numbers match the API's reported usage exactly.
- [ ] Streaming deltas render incrementally (no buffering of whole responses).

## Dependencies

- AS-008 (interface)
