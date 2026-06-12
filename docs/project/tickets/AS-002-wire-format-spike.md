---
id: AS-002
title: "Spike: mainstream agent wire-format union (polyglot schema groundwork)"
status: ready-to-implement
github_issue: 2
depends_on: [AS-001]
area: schema
priority: P0
source: PRD.md D4
---

# AS-002 · Spike: mainstream agent wire-format union

**Status: ready to implement**

## Description

Decision D4 requires the immutable block schema to be designed as the **union/superset of mainstream agent/provider wire formats up front**, before it is frozen — so provider #2, a compatible endpoint, or a mainstream agent transcript/stream never forces a breaking change. This spike compares the first-party provider APIs we plan to implement (Anthropic and OpenAI) plus public, mainstream agent/API surfaces such as xAI/Grok Build, and produces the design input for AS-003.

Compare, for each included provider/agent surface:
- Message roles and content-block structures (text, tool use/call, tool result, reasoning/thinking blocks, images).
- Streaming event shapes and ordering.
- Tool/function-calling request + response formats.
- Token usage reporting, including cache-related fields.
- Prompt-caching semantics (explicit `cache_control` vs automatic).
- For OpenAI: decide which API surface we target (Chat Completions vs Responses API) and document why.
- For xAI/Grok Build: map both the OpenAI-compatible API shape used by grok-build-* models and the headless/agent surfaces that are public enough to preserve (--output-format streaming-json, MCP-facing events, server-side tools, citations, and code-execution outputs). Treat undocumented/private transcript internals as non-normative observations only.
- Include a short survey of other mainstream coding agents with public formats (for example Codex CLI/headless streams, Gemini CLI, Cursor/Cline/Aider where available) and explicitly classify each as either **schema input** (public/stable enough to model), **compatibility note** (covered by OpenAI-compatible/API projection), or **out of scope** (private/unstable).

Deliverable: `docs/design/block-schema-union.md` — a field-by-field mapping into a proposed superset schema, with normalization rules and a list of provider/agent-exclusive concepts and how the schema represents them as optional fields. The doc must include source links and retrieval dates for every external format it treats as schema input, because these agent formats change quickly.

## Acceptance criteria

- [ ] Design doc covers every block type named in D3 (text / tool-call / tool-result / file-read / reasoning).
- [ ] Every provider/agent-exclusive field that is public enough to model is identified with its representation in the union schema; private or unstable formats are explicitly marked non-normative.
- [ ] The OpenAI API surface choice (Chat Completions vs Responses) is made and justified.
- [ ] xAI/Grok Build is covered explicitly, including whether its model API is represented as an OpenAI-compatible projection, whether any Responses/API extensions need first-class optional fields, and how headless streaming-json/MCP events map into or stay outside the block schema.
- [ ] At least two additional mainstream coding-agent public formats are surveyed, with a clear include/compatibility/out-of-scope decision for each.
- [ ] Doc reviewed and accepted as the basis for the AS-003 schema freeze.

## Dependencies

- AS-001 (repo to hold the doc)
