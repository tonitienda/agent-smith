# Agent Smith content-block schema — **v1** (the frozen substrate)

> Status: **v1** · Reference implementation: [`schema`](../../schema) (Go) · Design input: [block-schema-union (AS-002)](../design/block-schema-union.md) · Tickets: [AS-003](../project/tickets/AS-003-block-schema-v1.md)

This is the public, versioned contract for the **open, stable data substrate** that is Agent Smith's moat (PRD **D1**). A session is an **append-only, immutable event log of content blocks** (PRD **D3**); the model-facing context is a *projection* over that log, never stored state. This document specifies the block shape; the [AS-002 union spike](../design/block-schema-union.md) is the field-by-field rationale, and the Go package [`schema`](../../schema) is the reference implementation.

`schema_version` is **`1`** and `schema` is **`agent-smith.blocks.v1`**.

## Additive-only rules (PRD D2) — binding from V1, forever

From this V1 freeze onward the schema is **additive-only, forever**:

- **No field is ever removed, renamed, or repurposed.** A field's name, type, and meaning are permanent.
- **New concepts arrive only additively** — as a new optional field, or a new block `kind`. Never as a breaking change to an existing one.
- **Consumers MUST tolerate missing and unknown data.** An absent optional field is normal (it means "not reported"/"not applicable", *never* an implied zero). Unknown fields and unknown block kinds MUST deserialize without error and be preserved or passed through, not rejected.
- **There are no deprecation windows.** Nothing is ever scheduled for removal, because nothing is ever removed.

Pre-V1 the draft was malleable (union spike §15); at V1 it locks. Mechanical enforcement of these rules (golden-file corpus + schema-diff CI) is **AS-004**, which also publishes the contributor process in `docs/schema/EVOLUTION.md`. A language-neutral **JSON Schema** for non-Go clients — plus a Go↔schema divergence guard — is **AS-061** (see below).

## JSON Schema (language-neutral contract) — [`block.schema.json`](block.schema.json)

Non-Go clients (TypeScript, Python, Rust, downstream CI validators) validate against the published **JSON Schema** (draft 2020-12): [`docs/schema/block.schema.json`](block.schema.json). It mirrors the Go reference types above — the envelope plus the five content kinds — and is versioned to schema v1.

### How enums and `additionalProperties` reconcile with additive-only

A naive JSON Schema (`additionalProperties: false` + closed `enum`s) would **reject** any document written by a newer, additively-evolved version — the opposite of "consumers tolerate missing and unknown" (D2). The published schema therefore deliberately:

- **Leaves `additionalProperties` open everywhere** (the JSON Schema default). Unknown envelope, body, and sub-object fields — the whole point of the `ext` escape hatch and forward-compat — validate, not fail.
- **Treats `kind`, `role`, `stop_reason`, and the other vocabularies as non-exhaustive.** They are typed `string`, not closed `enum`s; the known values are documented in each field's `description`. Adding a new `kind` or `stop_reason` is additive, so an unknown value validates. An unknown `kind` carries **no body constraint** (it may legitimately use a body shape v1 does not model) — mirroring the Go `Validate`, which imposes no body rule on non-content kinds.

It still enforces the constraints that are **invariant forever**: the five required envelope keys (`id`, `kind`, `seq`, `ts`, `role`); the required body keys on a **known** `kind` (`tool_call` → `tool_use_id` + `name`; `tool_result` → `tool_use_id`; `file_read` → `path`); that the body present matches the discriminator `kind` (an `if`/`then` per known kind); and the types of known fields (e.g. token counts are non-negative integers).

### Divergence guard (Go ↔ JSON Schema)

[`internal/schemajson`](../../internal/schemajson) is a small stdlib-only validator for the JSON Schema subset the contract uses; its guard test (`go test ./...`, so CI) keeps the two artifacts honest:

- every document the Go types marshal — including the **maximal full-coverage session** and the permanently-kept golden corpus (AS-004) — **validates** against the published schema (a Go change that emits a shape the schema rejects fails CI);
- a document with **unknown fields and an unknown block `kind`** still validates (forward-compat);
- a curated **invalid** corpus (mismatched body/`kind`, missing `tool_use_id`/`name`/`path`, wrong field types, negative token counts) is **rejected**, proving the schema actually constrains.

The schema is **hand-authored** and kept honest by the round-trip guard above (rather than generated), since the Go-derived descriptor that the AS-004 baseline already maintains makes a second generator redundant.

## The event envelope

Every block shares one envelope. All fields except `id`, `kind`, `seq`, `ts`, `role` are optional.

| Field | Type | Meaning |
|---|---|---|
| `id` | string | Stable, globally unique block ID. **Never reused, never changed.** Ours, not the provider's. |
| `kind` | enum | Discriminates the body: `text` · `tool_call` · `tool_result` · `file_read` · `reasoning` (+ additive future kinds, e.g. `compaction`, `fallback`). |
| `seq` | int | Monotonic append order within a session. |
| `ts` | RFC3339 | Append time (harness clock). |
| `role` | enum | `user` · `assistant` · `system` · `tool` · `harness`. |
| `stop_reason` | string? | Turn stop reason (`end_turn`/`stop`, `tool_use`/`tool_calls`, `max_tokens`/`length`, `refusal`, `content_filter`, `model_context_window_exceeded`, `pause_turn`). |
| `provenance` | object? | `{producer, request_id, response_id, turn_id, derived_from[]}` — links derived blocks (`/clean`, `/tidy`, `/compact`, compaction) back to their sources, so reversibility and audit are structural. |
| `provider` | object? | `{vendor, surface, model, native_type, native_id}` — the source surface's own type/IDs, preserved verbatim for lossless re-emission. |
| `thread` | object? | `{thread_id, parent_block_id, parent_thread_id, agent_id, is_sidechain}` — sub-agent / multi-agent tree. The main thread has `parent_thread_id` empty. |
| `attribution` | object? | `{skill, mcp_server, mcp_tool, tool, hook}` — what produced the block (feeds living-skills and `/insights`). |
| `tokens` | object? | Usage breakdown (see below). Filled later by accounting. |
| `cost_usd` | number? | Filled later by accounting. |
| `usage_meta` | object? | `{service_tier, speed, server_tool_use}` — price-affecting metadata. |
| `cache` | object? | `{mode: explicit\|automatic, breakpoints[], ttl}` — prompt-caching semantics. |
| `excluded_by` | string[]? | IDs of exclusion/derivation events that drop this block from the projection. History is never mutated. |
| `ext` | object? | Open map for not-yet-modeled fields. Forward-compat escape hatch. |

### The two escape hatches → lossless re-emission

`provider.native_type` + `provider.native_id` + `ext` (present on the envelope **and every sub-object/body**) together guarantee that *any* concept the union does not model first-class still survives a read → store → write cycle. An adapter that meets an unmodeled concept records it explicitly in `ext`; it round-trips opaquely today and can be promoted to a first-class optional field later with **zero** breaking change.

## The block kinds

The bodies live under a key named for the kind (`text`, `tool_call`, …); exactly one is set per block.

- **`text`** — `{text?, subtype?, parts[]?, citations[]?, annotations[]?}`. `text` is optional so a purely multimodal turn (only `parts[]`) is representable. A refusal is a `text` block with `subtype: "refusal"` and `stop_reason: "refusal"`.
- **`tool_call`** — `{tool_use_id, name, arguments, arguments_raw?, tool_kind, tool_subtype?, parallel_group?, mcp_server?}`. `arguments` is canonical structured JSON; `arguments_raw` keeps the verbatim string when a surface sent one (signatures/caching depend on exact bytes). `tool_use_id` links a call to its result.
- **`tool_result`** — `{tool_use_id, content[], is_error, citations[]?, exit_code?, stdout?, stderr?, structured_content?, truncated?, offload_ref?}`. One per `tool_use_id`. Results a surface fuses onto the call are split into a paired call + result.
- **`file_read`** — `{path, range?, content?, content_hash?, offload_ref?, error?, produced_by?, media_type, source}`. Agent Smith-native (no provider exposes it); back-projects onto a read-tool `tool_result`. `content`/`content_hash` are optional to cover large/binary/offloaded content and failed reads.
- **`reasoning`** — `{text?, summary[]?, encrypted?, signature?, redacted?, replay_scope?}`. `encrypted` is opaque passthrough — **never inspected**, stored verbatim. `replay_scope` (`same_model_only`\|`portable`, default `portable`) lets the projection engine honor each provider's replay contract.

### Token usage — "missing means unreported, never zero"

`tokens` is the union of every surveyed provider's breakdown: `{input, output, cache_read, cache_write, reasoning, cache_write_5m, cache_write_1h, iterations[]}`. Each is optional; an absent field means **not reported by this surface**, never zero. (In the Go reference implementation these are pointers so a reported `0` is distinguishable from an unreported field.)

## What is *not* a stored block

Session **rollups** (`total_cost_usd`, `num_turns`, `duration_ms`) are *projections* over the log (derived), not blocks — though they are importable as a session-level record when only a foreign agent's headless result is available. Harness-lifecycle events (hooks, permissions, attachments) are modeled as additive **non-block event kinds** on the same event log, excluded from the model-facing projection by default. See union spike §5A.

## Coverage

Every mapping in the [AS-002 union doc](../design/block-schema-union.md) — across the wire, headless-result, and persisted-session representation layers, for Anthropic, OpenAI (Responses + Chat Completions), xAI/Grok, Codex CLI, and Gemini CLI — is representable here. No known public provider/agent concept is unrepresentable; the rest round-trips through the escape hatches.
