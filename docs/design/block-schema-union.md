# Block-Schema Union — mainstream agent/provider wire-format spike (AS-002)

> Status: **accepted as input for AS-003** · Owner: Agent Smith · Spike for PRD Decision **D4**
> Retrieval date for every external format treated as schema input: **2026-06-13** (US).
> These formats change quickly. Every external source below carries a retrieval date; re-verify before the AS-003 freeze if more than a few weeks have passed.

This document is the design input required by **PRD D4**: the immutable content-block schema (AS-003) must be modeled as the **union/superset of mainstream agent/provider wire formats, designed up front**, so that provider #2, an OpenAI-compatible endpoint, or a mainstream agent transcript/stream never forces a breaking change to a schema that is **additive-only forever** (D2). It compares the first-party provider APIs we will implement (Anthropic, OpenAI) plus public agent/API surfaces (xAI/Grok, Codex CLI, Gemini CLI, and compatibility notes for Cline/Aider), maps each field into a proposed superset, and classifies every surveyed surface as **schema input**, **compatibility note**, or **out of scope**.

The core data model is fixed by **D3**: an append-only, immutable event log of **content blocks** — `text` / `tool_call` / `tool_result` / `file_read` / `reasoning`, each with a stable ID — over which the model-facing context is a *projection*. This doc maps every surveyed surface onto those five block kinds plus a shared event envelope, and identifies the optional fields the union needs so no known public concept is unrepresentable.

**Two things this revision adds (per review):**

1. **The union must superset across *representation layers*, not just providers.** An agent exposes the *same* session at several layers, each carrying *different* information: the provider wire/API layer (richest on block content + caching), the agent **headless-result** layer (`--output-format json`, `codex exec --json` — rich on cost/usage/turn-count *rollups*), and the agent **persisted-session** layer (Claude `.jsonl`, Codex `rollout-*.jsonl`, Gemini history — rich on *internal* structure: sub-agents, hooks, skill/MCP attribution, per-iteration usage). The earlier draft modeled only the wire layer. **§5A** adds the layer model; **§3** and **§8** are extended for the structural and usage fields it surfaces.
2. **The schema is not frozen until V1.** D2 is "additive-only **from V1**, forever" — so *pre*-V1 (now, through AS-003) we can still make breaking changes. The union is the up-front design (D4) that *minimizes* future breaks, but it is a **draft we should rest, validate against real captured sessions, and refine** before the V1 freeze locks it. See **§15**.

---

## 1. Surveyed surfaces and classification

| Surface | What it is | Classification | Why |
|---|---|---|---|
| **Anthropic Messages API** | First-party provider #1 | **Schema input** | We implement it (AS-009); richest block taxonomy (thinking, server tools, citations, compaction). |
| **OpenAI Responses API** | First-party provider #2 (primary surface) | **Schema input** | We implement it (AS-010); typed `output[]` item model is the closest external analogue to our block log. **Chosen target — see §4.** |
| **OpenAI Chat Completions API** | Legacy/ubiquitous OpenAI surface | **Compatibility note** | The de-facto "OpenAI-compatible" wire shape (Grok, OpenRouter, local). We must *read/emit* it, but it is a thinner projection of Responses. |
| **xAI / Grok API** | Mainstream coding-agent provider | **Schema input (projection)** | OpenAI-compatible projection; a few first-class optional fields (`reasoning_content`, Live Search citations) — see §5. |
| **Codex CLI headless** (`codex exec --json`) | OpenAI's coding agent, headless JSONL | **Schema input** | Public, documented JSONL event stream with stable `item.type` taxonomy — maps cleanly to blocks. |
| **Gemini CLI headless** (`--output-format stream-json`) | Google's coding agent, headless JSONL | **Schema input** | Public JSONL event stream (`init`/`message`/`tool_use`/`tool_result`/`result`); ACP mode is JSON-RPC. |
| **Cline** session files (`api_conversation_history.json`) | VS Code agent | **Compatibility note** | The conversation file *is* a raw Anthropic/OpenAI message array (a provider projection); its `ui_messages.json` is private UI state — **non-normative**. |
| **Aider** chat history (`.aider.chat.history.md`) | Pair-programming agent | **Out of scope** as schema input | Human-readable Markdown transcript, not a structured wire format. Importable by parsing only; never a normative schema source. |
| Grok Build / Codex private transcript internals | Vendor-internal session state | **Out of scope / non-normative** | Undocumented, unstable; D4 explicitly treats these as non-normative observations. |

**Rule applied throughout:** a concept becomes a *union field* only if it appears in a **public, reasonably stable** surface (schema input). Private/unstable internals are recorded as **non-normative observations** (§10) and are *not* given first-class fields — but because the schema is additive-only, any of them can be added later as a new optional field without a break.

---

## 2. Sources (with retrieval dates)

All retrieved **2026-06-13** unless noted. Doc-portal pages that blocked automated fetch are cited from their canonical URL; field-level claims for those were cross-checked against the Anthropic API reference bundled in-repo tooling and the secondary sources listed.

- Anthropic Messages API — content blocks, streaming, usage, caching, compaction, fallback, refusal. Canonical: `https://platform.claude.com/docs/en/build-with-claude/streaming`, `.../prompt-caching`, `.../compaction`, `.../context-editing`, `.../api/errors`. (Cross-checked against the in-repo `claude-api` reference.)
- OpenAI — Migrate to the Responses API: `https://developers.openai.com/api/docs/guides/migrate-to-responses`
- OpenAI — Chat Completions reference: `https://developers.openai.com/api/reference/chat-completions/overview`
- OpenAI — Text generation guide: `https://platform.openai.com/docs/guides/text`
- OpenAI — API changelog: `https://developers.openai.com/api/docs/changelog`
- xAI — Generate Text / Chat guide: `https://docs.x.ai/docs/guides/chat`
- xAI — Chat Completions (Deprecated): `https://docs.x.ai/docs/guides/chat-completions`
- xAI — Tools Overview: `https://docs.x.ai/docs/guides/tools/overview`
- xAI — API reference (Chat): `https://docs.x.ai/docs/api-reference`
- Codex CLI — Non-interactive mode: `https://developers.openai.com/codex/noninteractive`
- Codex CLI — `exec --json` schema discussion: `https://github.com/openai/codex/issues/4776`
- Codex CLI — `exec --json` cheatsheet (secondary): `https://takopi.dev/reference/runners/codex/exec-json-cheatsheet/`
- Gemini CLI — `stream-json` output (PR): `https://github.com/google-gemini/gemini-cli/pull/10883`; structured-output issues `#8203`, `#8022`; ACP mode: `https://geminicli.com/docs/cli/acp-mode/`
- Cline — session history shape (`api_conversation_history.json` / `ui_messages.json`): observed in the Cline repo task-storage; **compatibility/non-normative**.
- Aider — chat history file: `https://aider.chat/docs/config/options.html` (`--chat-history-file`, default `.aider.chat.history.md`).

**Representation-layer sources (§5A), retrieved 2026-06-13:**

- Claude Code — headless / programmatic output (`--output-format json`/`stream-json`, result fields incl. `total_cost_usd`, `num_turns`, `duration_ms`, `session_id`): `https://code.claude.com/docs/en/headless`
- Claude Code — **persisted session `.jsonl`**: **primary-source inspection of this very session's file** `~/.claude/projects/-home-user-agent-smith/<sid>.jsonl` (2026-06-13). Record types `user`/`assistant`/`system`/`attachment`/`queue-operation`; envelope `uuid`/`parentUuid`/`isSidechain`/`sessionId`/`cwd`/`gitBranch`/`version`/`requestId`; `attributionSkill`/`attributionMcpServer`/`attributionMcpTool`; `toolUseResult`; `sourceToolUseID`; `usage.{iterations[], cache_creation.ephemeral_1h_input_tokens/ephemeral_5m_input_tokens, service_tier, speed, server_tool_use}`. Field names are observed, not from published docs (the on-disk format is not a documented contract — treated here as a **stable-enough schema input with a non-normative caveat**).
- Codex CLI — session/rollout files vs `exec --json`: `https://github.com/openai/codex/discussions/3827`, `https://github.com/openai/codex/issues/2288`, `https://developers.openai.com/codex/noninteractive`
- Gemini CLI — `--output-format json` (response + `stats`) / headless reference: `https://github.com/google-gemini/gemini-cli/pull/8119`, `https://geminicli.com/docs/cli/headless/`
- Anthropic Managed-Agents **session threads** (sub-agent layer, `parent_thread_id`, cross-thread messages): `https://platform.claude.com/docs/en/managed-agents/multi-agent.md`

> **Format-churn note (observed 2026-06-13):** xAI now markets its OpenAI-style Chat Completions surface as **deprecated** in favor of a newer unified/Responses-style API, and routes legacy model slugs to `grok-4.3`. Codex `exec --json` changed `item_type → type` and `assistant_message → agent_message`. Gemini CLI's `stream-json` is recent and still stabilizing. These are exactly the kind of fast-moving changes D4 anticipates; the union below is designed so each is an additive field/value, not a break.

---

## 3. The event envelope (shared across all blocks)

Every block in the append-only log shares one envelope. Fields are additive-only (D2); consumers tolerate missing/unknown (D2, AS-003 forward-compat).

| Field | Type | Notes |
|---|---|---|
| `id` | string (stable, unique) | D3 stable block ID. Never reused. Our ID, not the provider's. |
| `kind` | enum | `text` \| `tool_call` \| `tool_result` \| `file_read` \| `reasoning` (+ future kinds, additive). |
| `seq` | int | Monotonic append order within a session. |
| `ts` | RFC3339 | Append time (harness clock). |
| `role` / `origin` | enum | `user` \| `assistant` \| `system` \| `tool` \| `harness`. Captures OpenAI's `developer`/`system` split and Anthropic mid-conversation `system` messages. |
| `provenance` | object | `{producer, request_id, response_id?, turn_id?, derived_from?[]}` — links derived blocks (`/clean`, `/tidy`, `/compact`, compaction) back to source blocks (D3 reversibility/auditability). |
| `provider` | object | `{vendor, surface, model, native_type, native_id}` — round-trip fidelity: the provider's own type string and ID are preserved verbatim so we can re-emit losslessly. |
| `thread` | object? | `{thread_id, parent_block_id?, parent_thread_id?, agent_id?, is_sidechain?}` — **sub-agent / multi-agent structure** (§5A). Locates a block in the sub-agent tree; the main thread has `parent_thread_id: null`. Captures Claude Code `isSidechain` + `sourceToolUseID`, Anthropic Managed-Agents session *threads*, and our own AS-044/AS-046 sub-agents. |
| `attribution` | object? | `{skill?, mcp_server?, mcp_tool?, tool?, hook?}` — **what produced this block** (persisted-session layer). From Claude Code `attributionSkill` / `attributionMcpServer` / `attributionMcpTool`. Feeds living-skills (AS-049) and `/insights` (AS-045). |
| `tokens` | object? | `{input, output, cache_read, cache_write, reasoning}` — optional, fillable later by AS-020 accounting. Union of all providers' usage breakdowns (§8). |
| `cost_usd` | number? | Optional; filled by accounting. |
| `excluded_by` | string[]? | IDs of exclusion/derivation events that drop this block from the projection (D3). History is never mutated. |
| `ext` | object? | Open map for provider/agent-exclusive fields not yet promoted to first-class. Round-trips unknown data; forward-compat escape hatch. |

`native_type` + `native_id` + `ext` together guarantee the **lossless re-emission** property: even a concept the union does not model first-class survives a read→store→write cycle.

---

## 4. OpenAI surface decision — Responses API (primary), Chat Completions (compatibility)

**Decision: target the Responses API as OpenAI's primary surface; support Chat Completions as a compatibility projection.**

Rationale:

1. **Structural fit with D3.** The Responses API models a turn as a typed `output[]` array of *items* (`message`, `reasoning`, `function_call`, `web_search_call`, `code_interpreter_call`, …). That is the same shape as our append-only block log — each item maps to one block — whereas Chat Completions flattens everything into a single `choices[0].message` with side-arrays (`tool_calls[]`). Mapping Responses → blocks is near-1:1; mapping Chat Completions → blocks requires de-interleaving.
2. **Reasoning is first-class in Responses.** `reasoning` items carry `summary[]` and (for stateless reuse) `encrypted_content`. Chat Completions hides reasoning entirely for OpenAI models. Our `reasoning` block (D3) needs the richer source.
3. **Forward direction.** OpenAI explicitly recommends Responses over Chat Completions for new text-generation apps, and server-side tools (web search, file search, code interpreter, computer use, MCP) are surfaced as typed items there. Betting the union on Responses keeps us aligned with where the surface is going.
4. **Compatibility is still mandatory.** Chat Completions is the de-facto "OpenAI-compatible" wire format for Grok, OpenRouter, local servers (llama.cpp, vLLM, Ollama) — PRD's cheapest/private tier (§10 Q1). So the union must also *read and emit* Chat Completions cleanly. We treat it as a **lossy-inbound / faithful-outbound projection**: every Chat Completions message maps onto union blocks, and the union can render back to Chat Completions for any endpoint that only speaks it.

Net: **Responses is the schema-input source of truth for OpenAI; Chat Completions is a required projection, not a second schema.** The union fields below are chosen so both round-trip.

---

## 5. xAI / Grok coverage (D4 explicit requirement)

- **Model API = OpenAI-compatible projection.** Grok's inference API has been OpenAI Chat Completions-compatible (`/v1/chat/completions`), and (as of 2026-06-13) xAI is moving to a newer unified/Responses-style surface while marking the old Chat Completions guide *deprecated*. Either way it projects onto the same union as OpenAI: roles, `content` parts, `tool_calls[]` (`{id, type:"function", function:{name, arguments}}`), `tool` messages keyed by `tool_call_id`.
- **First-class optional fields it needs:**
  - `reasoning_content` — Grok reasoning models expose reasoning text (and encrypted/“thinking” content) in a `reasoning_content` field on the assistant message; this maps to our **`reasoning` block** (text + opaque `encrypted` passthrough). *No break vs OpenAI* because the field is optional.
  - `max_completion_tokens` (vs `max_tokens`) — a request-side knob, not a block field; noted for the provider adapter (AS-010), not the schema.
  - **Live Search / server-side tools + citations** — Grok server-side search returns cited sources; this maps to a **`tool_result` block** of a server tool, with citations carried in the union's `citations[]` optional (§6.3), shared with Anthropic web-search/citations.
- **Headless / agent surfaces (Grok Build / `grok` CLI):**
  - Public, preserve-worthy: an OpenAI-compatible **streaming-JSON** event stream and **MCP-facing events**, **server-side tool** outputs, **citations**, and **code-execution** outputs. These map to the union's streaming model (§7) and to `tool_call`/`tool_result` blocks with server-tool subtypes.
  - **Non-normative:** undocumented/private transcript internals of Grok Build are explicitly out of scope per D4 — captured (if ever) only via `ext`, never as first-class fields.

**Conclusion for AS-003:** Grok is represented as an **OpenAI-compatible projection plus two optional fields already required by Anthropic** (`reasoning` passthrough, `citations`). No Grok-exclusive first-class field is needed today; if the new unified API adds Responses-style extensions, they land as additive optionals.

---

## 5A. Representation layers — wire vs headless-result vs persisted-session

The first draft compared **wire/API formats**. But an agent does not have *one* representation of a session — it has several, at different layers, and **they deliberately carry different information**. The reviewer's example is exact: Claude Code's headless JSON *output* is richer in cost/usage/result data, while its persisted session *file* is richer in sub-agent and internal structure. The union schema (and our own log, D3) must be a **superset across layers**, because we both *produce* our own session at all of them and *import* foreign sessions from any of them.

### The three layers

| Layer | What it is | Rich in | Thin on |
|---|---|---|---|
| **L1 — Provider/API wire** | One model turn: Messages / Responses / Chat Completions | Block content, tool calls, caching breakpoints, raw `usage` | Session-level rollups; cross-turn / sub-agent structure |
| **L2 — Agent headless result** | `claude -p --output-format json` / `stream-json`; `codex exec --json`; `gemini --output-format json` | **Rollups**: final result, `total_cost_usd` + per-model cost, `num_turns`, `duration_ms`, `session_id`, `is_error`; session `stats` | Internal structure (sub-agents, hooks, attribution); intermediate blocks (json mode) |
| **L3 — Agent persisted session** | Claude `~/.claude/projects/<proj>/<sid>.jsonl`; Codex `$CODEX_HOME/sessions/.../rollout-*.jsonl`; Gemini `~/.gemini/history/<proj>/` | **Internal structure**: sub-agent/sidechain tree, hooks, skill/MCP attribution, attachments, parent/child UUID DAG, per-iteration usage, permission/queue events | Nothing — this is the fullest layer; it is the closest external analogue to our own D3 log |

### Per-agent layer map

| Agent | L2 (headless result) | L3 (persisted session) | Layer-exclusive info the union must hold |
|---|---|---|---|
| **Claude Code** | `--output-format json`: `{type:"result", subtype, result, session_id, is_error, total_cost_usd, num_turns, duration_ms, usage, modelUsage{}}`; `stream-json` JSONL (`system` init → `assistant`/`user` → `result`) | `<sid>.jsonl`, one record per event: `type` ∈ `user`/`assistant`/`system`/`attachment`/`queue-operation`; envelope `uuid`/`parentUuid` (DAG), `isSidechain`, `sessionId`, `cwd`, `gitBranch`, `version`, `requestId`; `attributionSkill`/`attributionMcpServer`/`attributionMcpTool`; `toolUseResult`; `system` records carry `hookInfos`/`hookErrors`/`stopReason`; user records carry `sourceToolUseID`/`sourceToolAssistantUUID`; `usage` includes `iterations[]`, `cache_creation.{ephemeral_1h_input_tokens, ephemeral_5m_input_tokens}`, `service_tier`, `speed`, `server_tool_use` | sub-agent (`isSidechain`), skill/MCP attribution, hook lifecycle, attachments, per-iteration + TTL-split usage |
| **Codex CLI** | `exec --json`: JSONL state-change events (`thread.started`/`turn.*`/`item.*`); final `turn.completed` usage | `rollout-*.jsonl` under `$CODEX_HOME/sessions/YYYY/MM/DD/`: full conversation history, tool calls, token usage; resumable (`codex resume`) | full trajectory + resume state not present in the event stream |
| **Gemini CLI** | `--output-format json`: `{response, stats:{ session duration_ms, model turns, tool calls, user turns }}`; `--output-format stream-json` JSONL | `~/.gemini/history/<project>/` JSON session logs | session `stats` rollup (L2) vs full history (L3) |
| **OpenAI Responses** | n/a (provider, not an agent) — but **L1 itself is layered**: stored response objects (`store:true`, chained by `previous_response_id`) are server-side session state distinct from the streaming events | n/a | server-managed session state / reasoning reuse via `encrypted_content` |

### Consequences for the union (all additive — D2)

1. **Sub-agents / multi-agent are first-class, not an afterthought.** Added to the envelope as `thread{thread_id, parent_thread_id, parent_block_id, agent_id, is_sidechain}` (§3). This represents Claude Code sidechains, Anthropic Managed-Agents session *threads* (`parent_thread_id`, cross-thread messages), and our own AS-044/AS-046 sub-agents. A sub-agent is just a linked sub-stream of the one append-only log — it fits D3 with no new machinery, only envelope fields.
2. **Attribution is captured per block.** `attribution{skill, mcp_server, mcp_tool, tool, hook}` (§3) — directly enabling the living-skills wedge (which skill rediscovered which fact, AS-049) and `/insights` cost-by-skill (AS-045). This is information that *only* exists at L3 today; modeling it now means we don't break the schema to add it later.
3. **Rollups are projections, not stored blocks.** `total_cost_usd`, `num_turns`, `duration_ms`, session `stats` are **derived over the log** (D3: context/rollups are projections). The union does **not** store them as blocks — our cost meter (AS-020) and session rollups *compute* them. But the union must be able to **ingest** them when importing a foreign session that only exposes the L2 result (e.g. a CI run we only have the `--output-format json` for): those land on a session-level rollup record, with provenance noting they were imported, not derived.
4. **Harness-lifecycle events are non-block events on the same log.** Hooks, permission decisions, queue operations, attachments (Claude `system`/`queue-operation`/`attachment` records) are session events but not model content blocks. D3's log is already an *event* log, so these are additive **event kinds** (e.g. `hook`, `permission`, `attachment`) alongside the five content-block kinds — recorded for audit/replay, excluded from the model-facing projection by default.
5. **Usage is richer at L3 — extend §8.** Per-iteration usage, TTL-split cache-creation, `service_tier`, `speed`, and server-tool request counts appear in the persisted layer; §8 is extended accordingly.

---

## 6. Block-by-block union (the five D3 kinds)

For each kind: the per-surface mapping, the **normalization rule**, and the **provider-exclusive optionals** that make the field a true superset.

### 6.1 `text`

| Surface | Representation |
|---|---|
| Anthropic | `{"type":"text","text":...}` content block; optional `citations[]`. |
| OpenAI Responses | `message` item → `content[]` of `{type:"output_text", text, annotations[]}` / inbound `{type:"input_text"}`; `{type:"refusal", refusal}`. |
| OpenAI Chat Completions | `message.content` as string, or parts `[{type:"text"}, {type:"image_url"}, {type:"input_audio"}, {type:"file"}]`. |
| xAI/Grok | Same as Chat Completions; assistant text in `content`. |
| Codex CLI | `item.type:"agent_message"` → `text`. |
| Gemini CLI | `message` event with text payload. |

**Normalization rule:** one `text` block per contiguous assistant/user text span. Multimodal *inputs* (image/audio/file parts) attach to the same block as `parts[]` (see §6.5), not separate kinds, preserving order. A **refusal** is a `text` block with `stop_reason:"refusal"` on the turn and `text.subtype:"refusal"`; the original refusal payload is preserved in `ext`.

**Optionals:** `citations[]` (Anthropic, Grok Live Search), `annotations[]` (OpenAI Responses), `subtype` (`normal`|`refusal`), `parts[]` (multimodal).

### 6.2 `tool_call`

| Surface | Representation |
|---|---|
| Anthropic | `{"type":"tool_use","id":"toolu_…","name","input":{…}}` (input is **parsed JSON object**). Server tools: `server_tool_use` (web_search/web_fetch/code execution). |
| OpenAI Responses | `function_call` item `{id, call_id, name, arguments}` (**arguments is a JSON string**); server tools: `web_search_call`, `file_search_call`, `code_interpreter_call`, `computer_call`, `image_generation_call`, `mcp_call`. |
| OpenAI Chat Completions | `assistant.tool_calls[]: {id, type:"function", function:{name, arguments(string)}}`. |
| xAI/Grok | Same as Chat Completions `tool_calls[]`; server-side Live Search/code-exec as server tools. |
| Codex CLI | `item.type` ∈ `command_execution`, `file_change`, `mcp_tool_call`, `web_search`. |
| Gemini CLI | `tool_use` event `{name, input}`. |

**Normalization rule:** canonical `arguments` is stored as **structured JSON** (object). Because some surfaces send a JSON *string* and signatures/caching depend on exact bytes, we **also** keep `arguments_raw` (the verbatim string) when provided. `tool_use_id` is the union key linking call→result; the provider's native ID is preserved in `provider.native_id`. A `tool_kind` discriminates `client` (harness executes) vs `server` (provider executes, e.g. Anthropic `server_tool_use`, OpenAI `web_search_call`, Grok Live Search), and `tool_subtype` carries the specific server tool name.

**Optionals:** `tool_kind`, `tool_subtype`, `arguments_raw`, `parallel_group` (Anthropic/OpenAI parallel tool calls — AS-019), `mcp_server` (MCP-routed calls, Codex/Gemini/Anthropic/OpenAI MCP).

### 6.3 `tool_result`

| Surface | Representation |
|---|---|
| Anthropic | `{"type":"tool_result","tool_use_id","content":[…],"is_error"}`; server-tool results e.g. `web_search_tool_result` with `citations`. |
| OpenAI Responses | `function_call_output {call_id, output}`; server-tool calls carry their own result payloads inline on the call item. |
| OpenAI Chat Completions | `tool` role message `{tool_call_id, content}`. |
| xAI/Grok | `tool` message; Live Search results with citations. |
| Codex CLI | result fields on the same `command_execution`/`mcp_tool_call`/`web_search` item (`exit_code`, `output`, `aggregated_output`). |
| Gemini CLI | `tool_result` event `{output, is_error?}`. |

**Normalization rule:** one `tool_result` block per `tool_use_id`, with `content` as a list of typed parts (text, image, structured). `is_error` is a first-class boolean (Anthropic/Gemini have it explicitly; for surfaces that don't, infer from non-zero `exit_code` and record the raw signal in `ext`). Server-tool results that arrive *attached to the call* (OpenAI Responses, Codex single-item) are **split** into a paired `tool_call` + `tool_result` block so the log stays uniform; provenance links them.

**Optionals:** `citations[]` (shared with `text`), `exit_code`/`stdout`/`stderr` (shell/command execution — Codex, our AS-015 shell tool), `structured_content` (typed JSON results), `truncated` + `offload_ref` (Anthropic offloads >100K-token MCP results to a file; OpenAI/Codex similar — preserve the pointer).

### 6.4 `file_read` (Agent Smith-native; D3)

**No provider has a native `file_read` block.** It is a harness concept (D3) and a wedge enabler (composition view / `/clean` need to see and dedupe file reads). In the wild it appears as a **`tool_result` of a read/text-editor tool**: Anthropic text-editor / `read`, OpenAI `file_search_call` + code-interpreter file ops, Codex `file_change`/read commands, Gemini file tools, plus document/`input_file` *inputs*.

**Normalization rule:** `file_read` is a **first-class, additive block** that is *also* representable as a `tool_result` for providers that need it re-emitted. It carries:

| Field | Notes |
|---|---|
| `path` | absolute/normalized path |
| `range` | `{start_line, end_line}` or byte range (nullable = whole file) |
| `content` | the bytes/text read |
| `content_hash` | for dedupe (composition view "duplicated reads") |
| `produced_by` | `tool_use_id` of the read call, if any (provenance) |
| `media_type` | text/image/pdf/etc. (covers document inputs) |
| `source` | `tool` \| `attachment` \| `mcp_resource` |

This is the clearest case where the union is a **superset, not an intersection**: we model a block none of the providers expose, and define how it *projects back* onto each (as a read-tool `tool_result`, or a document/`input_file` content part).

### 6.5 `reasoning`

| Surface | Representation |
|---|---|
| Anthropic | `thinking` block `{thinking, signature}` (display `summarized`/`omitted`); `redacted_thinking` (encrypted). Replay: echo unchanged on same model; other models drop it. |
| OpenAI Responses | `reasoning` item `{id, summary:[{type:"summary_text"}], encrypted_content}`. |
| OpenAI Chat Completions | hidden for OpenAI models (no block). |
| xAI/Grok | `reasoning_content` on the assistant message; encrypted "thinking" content. |
| Codex CLI | `item.type:"reasoning"`. |
| Gemini CLI | "thought" parts (when surfaced). |

**Normalization rule:** one `reasoning` block per reasoning span, with:

| Field | Notes |
|---|---|
| `text` | visible/summarized reasoning (may be empty when display=omitted). |
| `summary` | list of summary parts (OpenAI `summary[]`, Anthropic summarized). |
| `encrypted` | opaque passthrough (Anthropic `redacted_thinking`, OpenAI `encrypted_content`, Grok encrypted thinking). **Never inspected**, stored verbatim. |
| `signature` | Anthropic thinking signature (replay integrity). |
| `redacted` | bool. |
| `replay_scope` | `same_model_only` (Anthropic/Fable) vs `portable` — drives whether the projection re-sends or drops the block across models. |

**Replay rules captured as data (critical for D2 round-trip):** Anthropic requires thinking blocks be echoed back *exactly as received* on the same model (including empty-text blocks) and dropped when the target model differs; OpenAI reasoning items reused statelessly need `encrypted_content`. Encoding `replay_scope` + `encrypted` + `signature` lets the projection engine (AS-006) honor each provider's replay contract without losing data.

---

## 7. Streaming event mapping

All four streaming surfaces are **incremental builders of the same blocks** — the union does not store stream events, it stores the assembled blocks plus enough metadata to *re-stream* them.

| Concept | Anthropic | OpenAI Responses | Chat Completions / Grok | Codex CLI | Gemini CLI |
|---|---|---|---|---|---|
| turn start | `message_start` | `response.created` | first chunk | `thread.started`/`turn.started` | `init` |
| block opens | `content_block_start` | `response.output_item.added` | (implicit) | `item.started` | (implicit) |
| text delta | `content_block_delta:text_delta` | `response.output_text.delta` | `choices[].delta.content` | `item.updated` | `message` delta |
| tool-args delta | `content_block_delta:input_json_delta` | `response.function_call_arguments.delta` | `delta.tool_calls[].function.arguments` | `item.updated` | `tool_use` |
| reasoning delta | `content_block_delta:thinking_delta`/`signature_delta` | `response.reasoning_summary_text.delta` | (Grok `reasoning_content`) | `reasoning` item | thought |
| block closes | `content_block_stop` | `response.output_item.done` | finish | `item.completed` | event end |
| turn end | `message_delta`+`message_stop` | `response.completed` | `finish_reason` | `turn.completed` | `result` |

**Normalization rule:** the provider adapters (AS-009/010) reduce streams to blocks; the schema records per-block `stream_order` and the turn's `stop_reason` (union of `end_turn`/`stop`, `tool_use`/`tool_calls`, `max_tokens`/`length`, `refusal`, `content_filter`, `model_context_window_exceeded`, `pause_turn`). Anthropic's `pause_turn` (server-tool continuation) and OpenAI's resumable items are recorded so a paused turn can be resumed without schema change.

---

## 8. Token usage & cache fields (union)

| Union field | Anthropic | OpenAI (both) | xAI/Grok | Codex / Gemini |
|---|---|---|---|---|
| `tokens.input` | `input_tokens` | `prompt_tokens` / `input_tokens` | `prompt_tokens` | `turn.completed` usage |
| `tokens.output` | `output_tokens` | `completion_tokens` / `output_tokens` | `completion_tokens` | usage |
| `tokens.cache_read` | `cache_read_input_tokens` | `prompt_tokens_details.cached_tokens` / `input_tokens_details.cached_tokens` | `cached_tokens` | (when present) |
| `tokens.cache_write` | `cache_creation_input_tokens` | — (auto-cached; no explicit write count) | — | — |
| `tokens.reasoning` | (part of output) | `completion_tokens_details.reasoning_tokens` / `output_tokens_details.reasoning_tokens` | reasoning tokens | — |

**Normalization rule:** `tokens` is optional per block and per turn; accounting (AS-020) fills it. The union keeps **all** breakdowns; a missing field means "not reported by this surface", never zero. Anthropic's explicit `cache_creation_input_tokens` has no OpenAI analogue (OpenAI auto-caches without a write count) — we keep the field and leave it null for OpenAI, which is exactly the additive-only posture (D2).

**Persisted-layer extensions (§5A).** The Claude Code `.jsonl` `usage` object is richer than the bare API `usage` and the union should carry the extras as optional fields:

| Union field | Source |
|---|---|
| `tokens.cache_write_5m` / `tokens.cache_write_1h` | Claude `cache_creation.{ephemeral_5m_input_tokens, ephemeral_1h_input_tokens}` (TTL-split write counts). |
| `tokens.iterations[]` | Claude `usage.iterations[]` — per-inference-iteration usage within one turn (server-tool loops, continuations). |
| `usage_meta.service_tier` / `usage_meta.speed` | Claude `service_tier` / `speed` — affect price; needed for accurate cost (AS-020). |
| `usage_meta.server_tool_use` | Claude `server_tool_use.{web_search_requests, web_fetch_requests}` — billable server-tool call counts. |
| Session rollup: `total_cost_usd`, `cost_by_model[]`, `num_turns`, `duration_ms` | L2 headless result (`--output-format json`, `gemini stats`) — a **projection/import**, not a block (§5A consequence 3). |

---

## 9. Prompt-caching semantics (explicit vs automatic)

| Surface | Model | What the schema records |
|---|---|---|
| Anthropic | **Explicit** `cache_control:{type:"ephemeral", ttl}` breakpoints (max 4); prefix-match. | `cache.breakpoints[]` (block-level marker + ttl) so we can reconstruct and re-emit breakpoints; `cache.read/write` token counts (§8). |
| OpenAI (Responses/Chat) | **Automatic** prefix caching; `cached_tokens` reported, no client breakpoints. | `cache.mode:"automatic"`; cached-token count only. |
| xAI/Grok | OpenAI-style automatic (plus any explicit knobs the new API adds). | `cache.mode` + counts; explicit knobs via `ext` until promoted. |

**Normalization rule:** a `cache` object on the envelope with `mode` (`explicit`|`automatic`), optional `breakpoints[]`, and `ttl`. This lets the cache-aware assembler (AS-011) place Anthropic breakpoints and simply observe OpenAI/Grok auto-cache — one schema, two strategies. The breakpoint marker is provenance, not content, so it never mutates a block.

---

## 10. Provider/agent-exclusive concepts → union representation

| Exclusive concept | Origin | Representation in the union |
|---|---|---|
| Thinking signature / replay-locked reasoning | Anthropic (Fable/Opus) | `reasoning.signature` + `replay_scope:"same_model_only"`. |
| Encrypted reasoning for stateless reuse | OpenAI Responses, Grok | `reasoning.encrypted` opaque passthrough. |
| Compaction block (server-side summary) | Anthropic (`compact-2026-01-12`) | Derived block kind `compaction` (additive) with `provenance.derived_from[]`; *fits D3's derived-block model natively.* |
| Refusal as a successful turn | Anthropic Fable, OpenAI | `stop_reason:"refusal"` + `text.subtype:"refusal"`; refused/partial content preserved. |
| Server-side fallback / fallback blocks | Anthropic (`fallbacks`) | `provider.native_type:"fallback"` audit marker block (additive); `provenance` records the model switch. |
| Server tools (web search, code exec, computer use, file search, MCP) | Anthropic, OpenAI, Grok | `tool_call`/`tool_result` with `tool_kind:"server"` + `tool_subtype`. |
| Citations / Live Search sources | Anthropic, Grok | `citations[]` on `text`/`tool_result`. |
| MCP resources / tool routing | All four agents | `tool_call.mcp_server`; MCP resource reads → `file_read.source:"mcp_resource"`. |
| Multimodal inputs (image/audio/pdf/file) | OpenAI, Anthropic, Gemini | `parts[]` on `text`/`file_read` with `media_type`. |
| Command-execution detail (exit code, stdout/stderr) | Codex, our shell tool | `tool_result.{exit_code, stdout, stderr}`. |
| TODO / plan items | Codex (`todo_list`) | Recorded via `ext` (non-normative today); promotable to a `plan` block later if it stabilizes — additive. |
| **Sub-agent / sidechain tree** (§5A) | Claude Code (`isSidechain`), Anthropic Managed-Agents threads, OpenAI, our AS-044/046 | Envelope `thread{thread_id, parent_thread_id, parent_block_id, agent_id, is_sidechain}`; a sub-agent is a linked sub-stream of the one log. |
| **Block attribution** (§5A) | Claude Code (`attributionSkill`/`attributionMcp*`) | Envelope `attribution{skill, mcp_server, mcp_tool, tool, hook}`; powers living-skills + `/insights`. |
| **Harness lifecycle events** (§5A) | Claude Code `system`/`queue-operation`/`attachment` records (hooks, permissions, queue, attachments) | Additive **non-block event kinds** (`hook`/`permission`/`attachment`/…) on the same event log; excluded from the model-facing projection by default. |
| **Session rollups** (§5A L2) | Claude `total_cost_usd`/`num_turns`/`duration_ms`, Gemini `stats` | A **projection** over the log (derived), or a session-level import record when only L2 is available — not a content block. |
| Cline `ui_messages.json` | Cline | **Non-normative** — private UI state; ignored or stashed in `ext` on import. |
| Aider Markdown transcript | Aider | **Out of scope** as schema; importable only by parsing into blocks. |

The two escape hatches — `ext` (open map) + `provider.native_type/native_id` — mean **any** unmodeled public concept round-trips today and can be **promoted to a first-class optional field tomorrow with zero breaking change** (D2). That is the whole point of designing the union up front (D4): the freeze in AS-003 is safe because the shape already anticipates provider #2 and mainstream agent imports.

---

## 11. Non-normative observations (explicitly not schema input)

Per D0 (no silent punts) and D4 (mark private/unstable as non-normative), these were looked at but are **deliberately excluded** from first-class modeling:

- **xAI new unified/Responses-style API specifics** — surfaced as deprecating Chat Completions on 2026-06-13 but the field-level shape was not retrievable from primary docs (portal blocked automated fetch). We treat Grok as an OpenAI-compatible projection + the two optionals in §5; revisit before the freeze if xAI publishes a stable item model.
- **Grok Build / Codex private transcript internals** — undocumented, unstable.
- **Cline `ui_messages.json`** — private UI render state.
- **Aider Markdown history** — presentation format, not a wire format.
- **Gemini CLI `stream-json`** — public but recent (PR-stage as of retrieval); modeled at the event-type level (`init`/`message`/`tool_use`/`tool_result`/`result`), not pinned to a frozen schema. Any field churn lands in `ext`.

None of these blocks AS-003: each is reachable additively later.

---

## 12. Proposed union schema (input for AS-003)

Concrete shape AS-003 should implement (Go types + JSON), expressed as the envelope (§3) plus the five block kinds (§6). Summary:

- **Envelope**: `id, kind, seq, ts, role, provenance, provider{vendor,surface,model,native_type,native_id}, tokens?, cost_usd?, cache?, excluded_by?, ext?`.
- **text**: `text, subtype?, parts?[], citations?[], annotations?[]`.
- **tool_call**: `tool_use_id, name, arguments(object), arguments_raw?, tool_kind, tool_subtype?, parallel_group?, mcp_server?`.
- **tool_result**: `tool_use_id, content[], is_error, citations?[], exit_code?, stdout?, stderr?, structured_content?, truncated?, offload_ref?`.
- **file_read**: `path, range?, content, content_hash, produced_by?, media_type, source`.
- **reasoning**: `text, summary?[], encrypted?, signature?, redacted?, replay_scope`.
- **Derived/additive block kinds already anticipated**: `compaction`, `fallback` (both via the same envelope + `provenance.derived_from[]`).

**Hard requirements AS-003 must honor (from this spike):**

1. **Unknown-field tolerance on deserialize** — `ext` + lenient decoding (D2 forward-compat).
2. **Lossless re-emission** — `provider.native_type/native_id` + `ext` preserve anything unmodeled.
3. **Parsed + raw tool arguments** — keep both (`arguments`, `arguments_raw`) for signature/cache fidelity.
4. **Opaque reasoning passthrough + replay scope** — never inspect `encrypted`; honor `replay_scope`.
5. **Call↔result pairing by `tool_use_id`** — even when a surface fuses them (Responses/Codex), split into paired blocks with provenance.
6. **`file_read` is first-class** but defined with a back-projection to a read-tool `tool_result`.
7. **Optional, never-zero token/cost fields** — missing means unreported.
8. **Sub-agent / thread structure on the envelope** — `thread{…}` so multi-agent and sidechain sessions round-trip (§5A).
9. **Block attribution + non-block event kinds** — `attribution{…}` and additive event kinds (`hook`/`permission`/`attachment`) so the persisted-session layer round-trips, not just the wire layer (§5A).
10. **Rollups are derivable, not stored** — but importable as a session-level record when only the L2 result is available (§5A).

---

## 13. Acceptance-criteria mapping

| AS-002 acceptance criterion | Where satisfied |
|---|---|
| Covers every D3 block type (text / tool-call / tool-result / file-read / reasoning) | §6.1–6.5 |
| Every public provider/agent-exclusive field identified with its union representation; private/unstable marked non-normative | §10, §11 |
| OpenAI surface choice (Chat Completions vs Responses) made and justified | §4 |
| xAI/Grok covered: OpenAI-compatible projection, optional first-class fields, headless streaming-json/MCP mapping | §5 |
| ≥2 additional mainstream coding-agent public formats surveyed with include/compat/out-of-scope decision | §1 (Codex CLI = input, Gemini CLI = input, Cline = compat, Aider = out of scope) |
| Source links + retrieval dates for every external schema-input format | §2 |
| Field-by-field mapping into a proposed superset with normalization rules + exclusive-concept handling | §3, §6–§10, §12 |
| Superset across representation layers (wire / headless-result / persisted-session), incl. sub-agent + cost/usage layering | §5A, §3, §8 |
| Doc accepted as the basis for AS-003 schema freeze | This doc (status: accepted input for AS-003) |

---

## 14. Representation-layer round-trip checklist

A quick self-test for AS-003: for each layer of each agent, can the union **ingest it and re-emit it without loss?**

- [ ] **L1 wire** (Messages / Responses / Chat Completions) → blocks → wire. Covered by §6–§9.
- [ ] **L2 headless result** (`--output-format json`, `codex exec --json` final, `gemini stats`) → session rollup record (import) + derivable from the log. Covered by §5A consequence 3, §8.
- [ ] **L3 persisted session** (Claude `.jsonl`, Codex `rollout-*.jsonl`, Gemini history) → blocks + `thread`/`attribution`/non-block events. Covered by §5A consequences 1/2/4, §3.
- [ ] **Sub-agent tree** survives a round-trip (parent/child links, sidechain flag).
- [ ] **Per-iteration + TTL-split usage** survives (no collapse to a single number).

If any box can't be checked at freeze time, it is a missing **optional** field — add it before V1 (§15), not after.

---

## 15. Schema freeze timing — pre-V1 is still malleable

PRD **D2** is precise: *"additive-only **from V1**, forever."* The irreversibility starts at the V1 freeze, **not now**. That gives us a deliberate window:

- **Until AS-003 ships in V1, breaking changes are allowed.** This union is the *up-front design* D4 asks for — its job is to make the eventual freeze safe by anticipating provider #2 and mainstream agent imports — but it is a **draft**, not the contract. We should *rest it, validate it against real captured sessions, and refine* before locking.
- **Recommended validation before the freeze (cheap, high-value):** capture a handful of real sessions per surface (a Claude Code `.jsonl`, a `codex exec --json` run + its `rollout-*.jsonl`, a Gemini `--output-format json` run, an Anthropic and an OpenAI raw turn, a Grok turn) and run the §14 round-trip checklist against the AS-003 types. Each gap found *now* is a free fix; the same gap found *after* V1 is a permanent optional-field workaround.
- **What stays additive even post-V1:** the two escape hatches (`ext`, `provider.native_type/native_id`) mean a *missed* concept is never fatal — it round-trips opaquely and can be promoted later. So the freeze is low-risk, and the pre-V1 window is about *reducing the number of opaque-only fields*, not about avoiding a catastrophe.

**Net:** treat this doc as the *baseline* for AS-003, implement against it, but schedule one explicit "validate the union against real captures and refine" pass before the V1 freeze. After V1, the additive-only discipline (D2) takes over.
