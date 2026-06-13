---
id: AS-009
title: Anthropic provider implementation
status: done
github_issue: 9
depends_on: [AS-008]
area: provider
priority: P0
source: PRD.md §7.1, D6
---

# AS-009 · Anthropic provider implementation

**Status: done** — implemented in `internal/provider/anthropic`.

## Description

Implement the provider interface against the Anthropic Messages API.

- Streaming (SSE) with all event types normalized to the AS-008 stream events.
- Tool use: tool definitions out, `tool_use` blocks in, `tool_result` blocks back — round-tripped through our block schema.
- Reasoning/thinking block support where the model emits it.
- Usage capture per turn: input, output, cache-read, cache-write tokens.
- Auth via API key (sourced through the key-storage layer, AS-017; env var works until then).
- Honest error mapping into the AS-008 taxonomy, with retry/backoff on rate limits.

## Acceptance criteria

- [x] A full agentic turn (user → assistant with tool calls → tool results → assistant) works end-to-end against the real API (smoke test behind an env flag; not in CI). — `TestLiveAgenticTurn`, gated by `SMITH_LIVE_ANTHROPIC=1` + `ANTHROPIC_API_KEY`.
- [ ] All conformance fixtures (AS-012) pass. — AS-012 is not yet built; revisit when the conformance suite lands.
- [x] Usage numbers match the API's reported usage exactly. — input/cache reported at `message_start`, output at `message_delta`, each only where Anthropic reports it (no double counting); all counts are pointers so unreported stays nil.
- [x] Streaming deltas render incrementally (no buffering of whole responses). — the SSE stream translates one frame at a time on `Next`.

## Implementation notes

- Package `internal/provider/anthropic`: `anthropic.go` (Provider + `Stream`), `request.go` (block→Messages wire mapping), `stream.go` (SSE reader + event normalization), `errors.go` (taxonomy mapping + Retry-After).
- Retry/backoff is intentionally left to the loop (AS-018): the adapter classifies failures into `*provider.Error` with `Retryable`/`RetryAfter` set so one retry policy spans all providers.
- Added one additive field, `provider.Event.EncryptedDelta`, so redacted/encrypted reasoning (`redacted_thinking`) round-trips losslessly through the normalized stream (PRD D2).
- `file_read` blocks (harness-native, no Anthropic analogue) currently render as a text content block; proper back-projection onto a read-tool `tool_result` is a projection concern (AS-006), tracked separately if it proves necessary.

## Dependencies

- AS-008 (interface)
