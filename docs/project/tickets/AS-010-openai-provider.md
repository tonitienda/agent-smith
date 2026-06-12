---
id: AS-010
title: OpenAI provider implementation (+ OpenAI-compatible endpoint support)
status: ready-to-implement
github_issue: 10
depends_on: [AS-008]
area: provider
priority: P0
source: PRD.md §7.1, D6
---

# AS-010 · OpenAI provider implementation

**Status: ready to implement**

## Description

Implement the provider interface against the OpenAI API surface chosen in the AS-002 spike. Configurable base URL from day one — that single knob is what covers Grok, local models (Ollama), and OpenRouter as the "OpenAI-compatible" tier (§7.1 bonus, §10 Q1), without extra code.

- Streaming, tool/function calling, and usage capture normalized to the AS-008 events.
- Reasoning-model support (reasoning effort params / reasoning summaries) where applicable.
- Configurable `base_url` + model string for compatible endpoints; degrade gracefully when a compatible endpoint omits optional fields (usage, reasoning).
- Error mapping into the shared taxonomy.

## Acceptance criteria

- [ ] Full agentic turn with tool calls works against the real OpenAI API (smoke test behind env flag).
- [ ] All conformance fixtures (AS-012) pass.
- [ ] Pointing `base_url` at a non-OpenAI compatible endpoint completes a basic chat turn.
- [ ] Missing optional response fields never crash the loop.

## Dependencies

- AS-008 (interface)
