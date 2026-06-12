---
id: AS-002
title: "Spike: Anthropic vs OpenAI wire-format union (bilingual schema groundwork)"
status: ready-to-implement
github_issue: 2
depends_on: [AS-001]
area: schema
priority: P0
source: PRD.md D4
---

# AS-002 · Spike: Anthropic vs OpenAI wire-format union

**Status: ready to implement**

## Description

Decision D4 requires the immutable block schema to be designed as the **union/superset of the Anthropic and OpenAI wire formats up front**, before it is frozen — so provider #2 never forces a breaking change. This spike does that comparison and produces the design input for AS-003.

Compare, for both providers:
- Message roles and content-block structures (text, tool use/call, tool result, reasoning/thinking blocks, images).
- Streaming event shapes and ordering.
- Tool/function-calling request + response formats.
- Token usage reporting, including cache-related fields.
- Prompt-caching semantics (explicit `cache_control` vs automatic).
- For OpenAI: decide which API surface we target (Chat Completions vs Responses API) and document why.

Deliverable: `docs/design/block-schema-union.md` — a field-by-field mapping into a proposed superset schema, with normalization rules and a list of provider-exclusive concepts and how the schema represents them as optional fields.

## Acceptance criteria

- [ ] Design doc covers every block type named in D3 (text / tool-call / tool-result / file-read / reasoning).
- [ ] Every provider-exclusive field is identified with its representation in the union schema.
- [ ] The OpenAI API surface choice (Chat Completions vs Responses) is made and justified.
- [ ] Doc reviewed and accepted as the basis for the AS-003 schema freeze.

## Dependencies

- AS-001 (repo to hold the doc)
