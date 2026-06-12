---
id: AS-008
title: Provider abstraction interface (pluggable, normalized)
status: ready-to-implement
github_issue: 8
depends_on: [AS-002, AS-003]
area: provider
priority: P0
source: PRD.md §7.1, D4
---

# AS-008 · Provider abstraction interface

**Status: ready to implement**

## Description

Define the Go interface every provider implements (§7.1). The agent core must depend only on this interface; normalization differences live inside provider packages. Treat normalization as core IP (§9 risk table).

- Request side: model ID, projected context (blocks from AS-006), tool definitions, sampling params, cache hints.
- Response side: a normalized stream of events — text delta, reasoning delta, tool-call (id, name, args), usage (input/output/cache tokens), stop reason, errors.
- Error taxonomy: auth, rate-limit, overloaded, context-too-long, invalid-request — mapped uniformly so the loop can react (retry/backoff policy hooks).
- A mock/fake provider for tests.

## Acceptance criteria

- [ ] Agent core packages import only the interface, never a concrete provider.
- [ ] Stream event types cover every concept in the AS-002 union doc.
- [ ] Mock provider exists and is used in loop tests.
- [ ] Per-request model selection is part of the interface (no global model state).

## Dependencies

- AS-002 (union design drives the normalized event set)
- AS-003 (blocks are the request payload)
