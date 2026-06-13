---
id: AS-010
title: OpenAI provider implementation (+ OpenAI-compatible endpoint support)
status: done
github_issue: 10
depends_on: [AS-008]
area: provider
priority: P0
source: PRD.md §7.1, D6
---

# AS-010 · OpenAI provider implementation

**Status: done** — implemented in `internal/provider/openai` (two surfaces:
Responses primary, Chat Completions for OpenAI-compatible endpoints).

## Description

Implement the provider interface against the OpenAI API surface chosen in the AS-002 spike. Configurable base URL from day one — that single knob is what covers xAI/Grok model APIs (including Grok Build models exposed through the xAI Responses/OpenAI-compatible surface), local models (Ollama), and OpenRouter as the "OpenAI-compatible" tier (§7.1 bonus, §10 Q1), without extra code for basic turns.

- Streaming, tool/function calling, and usage capture normalized to the AS-008 events.
- Reasoning-model support (reasoning effort params / reasoning summaries) where applicable.
- Configurable `base_url` + model string for compatible endpoints; degrade gracefully when a compatible endpoint omits optional fields (usage, reasoning).
- Preserve compatible-endpoint extensions surfaced by AS-002 when present (for example xAI/Grok citations, server-side tool usage, and code-execution outputs) as optional metadata rather than discarding them.
- Error mapping into the shared taxonomy.

## Acceptance criteria

- [x] Full agentic turn with tool calls works against the real OpenAI API (smoke test behind env flag). — `TestLiveAgenticTurn` (skipped unless `SMITH_LIVE_OPENAI=1`), Responses surface; a two-turn tool-call → tool-result → answer flow.
- [ ] All conformance fixtures (AS-012) pass. — deferred: AS-012 is not built yet. The adapter is exercised by httptest-replay fixtures for both surfaces here; the shared conformance suite will subsume them when AS-012 lands.
- [x] Pointing `base_url` at a non-OpenAI compatible endpoint completes a basic chat turn. — `WithSurface(SurfaceChatCompletions)` + `WithBaseURL`; `TestChatStreamCompatibleEndpointNoUsage` covers a minimal endpoint that omits usage and ends without a `[DONE]` sentinel.
- [x] xAI/Grok-compatible fixtures preserve optional extensions without affecting plain OpenAI behavior. — `TestChatStreamGrokReasoning` maps Grok `reasoning_content` to a reasoning block; plain text turns are unaffected.
- [x] Missing optional response fields never crash the loop. — pointer-typed usage (missing ≠ zero), graceful no-usage/no-`[DONE]` handling, and additive tolerance for unknown event/item types.

## Implementation notes

- **Two surfaces, one adapter** (AS-002 §4): `SurfaceResponses` (default) is the
  schema-input source of truth; `SurfaceChatCompletions` is the
  "OpenAI-compatible" wire shape for xAI/Grok, OpenRouter, and local servers.
  Select with `WithSurface`; point at any endpoint with `WithBaseURL`.
- **Caching** is automatic for both OpenAI surfaces (union §9), so `CacheHints`
  needs no request-side handling — the adapter just observes `cached_tokens` in
  usage (`schema.Tokens.CacheRead`). Explicit breakpoints are an Anthropic-only
  concern; cache-aware assembly is AS-011.
- **Reasoning reuse**: a reasoning block is only re-sent to the Responses API
  when it carries `encrypted_content` (the documented stateless-reuse path);
  visible-only summaries are dropped to avoid malformed-input errors. Chat
  Completions has no reasoning input field, so reasoning is dropped there.
- Follow-on work that surfaced and is tracked elsewhere: the shared conformance
  suite (AS-012) and prompt-caching assembly (AS-011).

## Dependencies

- AS-008 (interface)
