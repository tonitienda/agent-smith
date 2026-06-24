# Agent Smith manual test campaign

_Last updated: 2026-06-24._

This campaign is the human smoke/regression pass to run after a burst of ticket work when nobody has recently driven the application end-to-end. It covers every ticket in the backlog: completed tickets have concrete actions and expected results; not-yet-implemented tickets are explicitly marked so testers do not mistake absence for a regression.

## How to use this document

1. Build once from the repository root: `make build`.
2. Use a disposable project directory unless a scenario says otherwise.
3. Prefer a test user config directory so manual state does not leak into your real Smith setup, for example: `export XDG_CONFIG_HOME=$(mktemp -d)` and `export HOME=$(mktemp -d)`.
4. Record the result for each scenario as **Pass**, **Fail**, **Blocked**, or **Not implemented**.
5. When a completed-ticket scenario fails, create a new `docs/project/tickets/AS-NNN-*.md` ticket with `status: ready-to-implement`, `type: bug`, and `area` matching the failing feature. Update `docs/project/tickets/README.md` and this campaign in the same change.
6. Before committing or handing off campaign updates, run `./scripts/agent-quality-gate.sh` as required by the repository harness contract.

## Status legend

- **Implemented**: ticket frontmatter currently says `status: done`; run the listed action and verify the expectation.
- **Not implemented**: ticket is `ready-to-implement`; only verify the product does not claim the feature is available unless a partial scenario is listed.
- **Needs clarification**: ticket is intentionally blocked; verify documentation still points to the open questions.

## Quick campaign checklist

Use this shortened pass when time is limited; the detailed sections below explain each expectation.

| ID(s) | Status | Action | Expected result |
| --- | --- | --- | --- |
| AS-001, AS-065, AS-089 | Implemented | `make build`; run `./smith --help`, `./smith <cmd> --help`, and a command with bad arguments. | Static binary builds; help is readable; invalid usage exits `2`; command dispatch stays in `cmd/smith` composition root. |
| AS-003–AS-007 | Implemented | Start a disposable session, send a small prompt if credentials exist, then inspect sessions with `smith session list` / `resume`. | Event log is append-only, projection reloads, sessions are project-scoped and rehydratable. |
| AS-008–AS-012 | Implemented | Run a scripted Anthropic and OpenAI-compatible smoke with test credentials, then provider fixture tests. | Provider stream events normalize consistently; conformance tests pass without live network. |
| AS-013–AS-016, AS-024, AS-062 | Implemented | Ask the agent to read, edit, search, and run a shell command in a disposable repo. | Tools validate arguments, request/obey permissions, show transparent calls/diffs, and log results. |
| AS-020, AS-025, AS-026, AS-063, AS-086 | Implemented | Use `/cost`, `/context`, and budget settings after a few turns. | Token/cost totals, per-block estimates, context meter, and conservative budget enforcement are visible and coherent. |
| AS-021–AS-023, AS-033, AS-037–AS-041, AS-053, AS-064, AS-066–AS-068, AS-072–AS-076 | Implemented | Drive the TUI command palette and slash commands. | Built-ins and custom commands appear, panels work, command metadata matches CLI help, and coding-mode UI state is visible. |
| AS-031, AS-071, AS-093 | Implemented | Set config through env, user config, project config, and flags. | Precedence is flag > project > user > env > default; typed consumers agree. |
| AS-032, AS-034, AS-035, AS-036, AS-047, AS-048, AS-049, AS-082, AS-083, AS-106–AS-108, AS-114 | Implemented | Add memory files, skills, hooks, MCP test server, and subagent/living-skill fixtures. | Capabilities load in the right scope, are attributed in context, and failures degrade visibly. AS-049 (skill-expectation analyzer) is opt-in via `subagents.skill-expectation-analyzer.enabled`; its grades land as findings surfaced by `/skills` (AS-050). |
| AS-030, AS-095–AS-103, AS-112 | Implemented | Run harness commands and benchmark smoke. | Quality gates and architecture/parity guards pass; benchmark report writes under `.cache/bench/`. |
| AS-084 | Implemented | In a disposable repo: have the agent write/edit a file, drop a `/rewind --mark`, change it again, then `/rewind <handle> --restore-files`. Also hand-edit a file after Smith wrote it, and try restoring it. | Files modified after the checkpoint are restored to their checkpoint state (new files deleted); a file changed outside Smith is flagged as a conflict and left untouched; oversized files are skipped with a note. |
| AS-017, AS-050, AS-052, AS-054–AS-055, AS-057–AS-058, AS-060–AS-061, AS-077–AS-081, AS-087, AS-109, AS-111, AS-119–AS-120, AS-133–AS-135 | Not implemented | Check README/help/tickets only. | Feature is ticketed but not advertised as complete; no manual pass/fail expected. |
| AS-113 | Needs clarification | Read its ticket. | Open questions remain clear until a plugin install/marketplace path exists. |

## Detailed manual scenarios

### 1. Build, command router, help, and exit-code contract

Covers AS-001, AS-065, AS-066, AS-069, AS-070, AS-089, AS-090, AS-104, AS-105.

| Step | Action | Expected result |
| --- | --- | --- |
| 1.1 | Run `make build`. | `./smith` is produced without requiring cgo. |
| 1.2 | Run `./smith --help`, `./smith run --help`, `./smith session --help`, `./smith context --help`, `./smith config --help`, `./smith serve --help`, and one leaf help with `--output json`. | Help lists command-specific flags, output modes, and noun-grouped commands. JSON help is parseable. |
| 1.3 | Run `./smith does-not-exist`. | Command fails with invalid-usage exit code `2` and a concise diagnostic on stderr. |
| 1.4 | Create a prompt file and run `printf 'stdin prompt' | ./smith run -f prompt.txt --dry-run` if dry-run is available, otherwise run without credentials and inspect the selected prompt in the diagnostic/log. | `-f` takes precedence over ambient non-TTY stdin; AS-069 does not regress. |
| 1.5 | Compare TUI slash-command names with CLI leaf help for shared commands. | Shared registry metadata is consistent; mode flags appear where migrated. |

### 2. Schema, event log, projection, persistence, and replay substrate

Covers AS-002 through AS-007, AS-027, AS-037, AS-038, AS-056, AS-084, AS-115.

| Step | Action | Expected result |
| --- | --- | --- |
| 2.1 | In a disposable project, start `./smith tui` or `./smith`, send a simple prompt, and exit. If no provider credentials are available, create a session through the smallest offline command that writes a log. | A project-scoped session directory appears with an event log and debug log. |
| 2.2 | Inspect the event log file before and after `/clear`, `/rewind`, `/compact`, or `/clean --apply`. | History is append-only; edits append exclusion, checkpoint, restore, or derived events instead of mutating earlier blocks. |
| 2.3 | Run `./smith session list` and resume the session. | The session appears for the current project and transcript/context are rehydrated. |
| 2.4 | Add prompts containing fake secrets/PII such as `sk-test-secret` or an email address and inspect captured log text. | Best-effort redaction occurs before capture where AS-115 applies; debug logs do not reveal more than intended. |
| 2.5 | Exercise `/compact` preview/apply/undo and `/rewind` checkpoint/restore. | Derived compact blocks and restore events are visible and reversible. |
| 2.6 | After file edits past a checkpoint, run `/rewind <handle> --restore-files` (AS-084). Repeat after hand-editing a Smith-written file outside the session. | Restored/deleted files match the checkpoint; externally-changed files are flagged as conflicts and never overwritten; the log still only grows. |

### 3. Providers, prompt caching, and provider conformance

Covers AS-008 through AS-012 and AS-092.

| Step | Action | Expected result |
| --- | --- | --- |
| 3.1 | With `ANTHROPIC_API_KEY`, run a one-turn `smith run` against an Anthropic model. | Streaming text arrives; usage metadata and provider errors are normalized. |
| 3.2 | With `OPENAI_API_KEY` or OpenAI-compatible endpoint config, run the same one-turn task. | The same face-level events appear despite provider differences. |
| 3.3 | Repeat a prompt with cache-eligible context. | Prompt-cache usage/savings appear in `/cost` when the provider reports them. |
| 3.4 | Run provider conformance tests from the quality gate. | Recorded fixtures pass offline; no live network is needed for default tests. |
| 3.4a | Once AS-133–AS-135 land, run the recorded-provider simulator and offline E2E suite against redacted fixtures. | Fake Anthropic/OpenAI-compatible servers replay captures deterministically; TUI-facing state and append-only JSONL assertions pass without burning tokens. Until then, these tickets remain **Not implemented**. |
| 3.5 | `smith auth set anthropic` (paste a key at the hidden prompt), then `smith auth status`. | Key stored in the OS keychain, never a plaintext file; status shows `set (keychain)` without revealing the value. (AS-017) |
| 3.6 | Export `ANTHROPIC_API_KEY` and re-run `smith auth status anthropic`. | Reports `set (env ANTHROPIC_API_KEY)` — the env var overrides the stored key. (AS-017) |
| 3.7 | On a host with no Secret Service running, `smith auth set openai`. | Fails with an actionable error pointing at `OPENAI_API_KEY`; no plaintext file is written. (AS-017) |

### 4. Tools, permissions, and transparent execution

Covers AS-013 through AS-016, AS-019, AS-024, AS-062, AS-094.

| Step | Action | Expected result |
| --- | --- | --- |
| 4.1 | Ask the agent to read an existing file, glob files, grep for a symbol, write a new scratch file, edit it, and run a harmless shell command. | Tool calls are registered, JSON arguments are validated, and results are logged as content blocks. |
| 4.2 | Attempt a malformed tool request through a custom command, MCP fixture, or provider fixture if available. | Fuller schema validation rejects bad arguments with actionable errors before execution. |
| 4.3 | Try an action that requires approval, then deny it. | Permission prompt is visible; denial is respected and the model receives a clear tool error. |
| 4.4 | Approve an edit. | The TUI shows transparent tool-call state and diff review before/while applying changes. |
| 4.5 | Trigger two independent tool calls in one model turn if provider behavior allows it. | Independent calls run in parallel without interleaving corrupting the log or UI. |

### 5. Cost, context, cleaning, budgets, and routing

Covers AS-020, AS-025 through AS-028, AS-041, AS-042, AS-063, AS-068, AS-085, AS-086, AS-110.

| Step | Action | Expected result |
| --- | --- | --- |
| 5.1 | After several turns and at least one tool result, run `/cost` and `./smith cost`. | Per-turn input/output/cache token counts and dollar totals appear; unknown model pricing is marked unknown rather than guessed. |
| 5.2 | Open `/context` and sort by size, age, and type. | The composition view shows segment handles, token shares, dollar estimates, type rollups, duplicate reads, stale candidates, and excluded blocks. |
| 5.3 | Run `/clean <handle>` and inspect the preview; then `/clean --apply`, `/context`, and `/clean --undo`. | Preview is non-mutating; apply excludes the selected segment/tool pair; undo restores it by appending a reversal. |
| 5.4 | Use interactive `/clean` multi-select from the context panel. | Multi-select removal and per-item archive restore work from the panel. |
| 5.5 | Configure a very small budget and run a prompt. | Conservative pre-turn budget enforcement blocks or warns before an unpriced/over-budget turn. |
| 5.6 | Configure auto-compact off and near-limit context. | Auto-compact does not surprise the user unless explicitly enabled. |
| 5.7 | Use `/route` and configured model tiers. | Current implemented routing/tiering is visible; per-session escalation overrides remain **Not implemented** until AS-110 lands. |

### 6. TUI, commands, custom commands, Matrix layer, and Coding Mode

Covers AS-021 through AS-023, AS-033, AS-039, AS-040, AS-053, AS-064, AS-067, AS-072 through AS-076, AS-114, AS-121, AS-122, AS-126.

| Step | Action | Expected result |
| --- | --- | --- |
| 6.1 | Launch `./smith tui` in a real terminal and type `/`. | Command palette opens; focus/hotkeys route among input, transcript, and inspect panels. |
| 6.2 | Run `/clear`, `/model`, `/resume`, `/goal`, `/init`, `/serious`, `/feature`, and `/mode` where applicable. | Each command has visible feedback and updates only its intended session state. |
| 6.3 | Add `.agent-smith/commands/review.md` with frontmatter and `$ARGUMENTS`; reopen the palette. | Custom command appears with description/argument hint; built-ins still win name conflicts. |
| 6.4 | Enter Coding Mode with `/feature`; advance through phases. | Phase tracker panel and phase-as-block state are visible; process skill blocks are scoped to the active phase. |
| 6.5 | Add a ```` ```smith-method ```` block (`phases: think, plan, implement, verify`) to the project `CLAUDE.md`/`AGENTS.md`, start a session, and `/feature`. | The phase tracker reflects the overridden phase sequence (reordered/skipped); a malformed block degrades to the default (AS-075). |
| 6.6 | In Coding Mode, `/phase reflect`, then ask Smith to wrap up the feature (AS-076). | The reflect-artifacts skill drives three artifacts: a measurable success metric (citing the signal to read), an instrumentation proposal as a diff, and a check-back ticket draft (house `AS-NNN` format in this repo, markdown elsewhere — a draft only, no `cmd/ticket-sync`/remote issue). Smith never claims to read shipped-app runtime data. |
| 6.7 | Resume a prior session through the picker. | Picker lists sessions and restored transcript is readable. |
| 6.8 | Launch `./smith tui` and look at the empty startup screen, then type a character and clear it (AS-122). | Logo `▞▞ AGENT SMITH`, a full-width underrule, and the `path · model · mode` context line render; the invite headline and command-hint row show below; at default (medium) Matrix intensity the rain renders behind the copy and the idle phrase replaces the hint after ~3s; the `┃` gutter caret blinks while empty and goes solid the instant you type. `--no-splash` shows nothing above the input bar. |

### 7. Configuration, memory, skills, hooks, MCP, and living-skills substrate

Covers AS-031, AS-032, AS-034 through AS-036, AS-047, AS-048, AS-071, AS-082, AS-083, AS-093, AS-106 through AS-108.

| Step | Action | Expected result |
| --- | --- | --- |
| 7.1 | Set the same config key via environment, user config, project config, and command flag. | Effective value follows flag > project > user > env > default. |
| 7.2 | Add nested `AGENTS.md`, `CLAUDE.md`, and memory imports. | Memory files merge hierarchically; imports resolve in-scope and cycles/errors are visible. |
| 7.3 | Add project and user skills with the same name. | Project skill shadows user skill; skill contract frontmatter parses; loaded skill content is attributed in context. |
| 7.4 | Configure lifecycle hooks for `session-start`, `user-prompt-submit`, `pre-tool-use`, `post-tool-use`, and `session-stop`. | Hooks receive JSON, can annotate/rewrite/block as documented, time out safely, and do not bypass permissions. |
| 7.5 | Configure a tiny stdio MCP test server exposing a tool, resource, and prompt; then kill/restart it. | Namespaced MCP tools/resources/prompts are visible; reconnect/pagination works; server failure degrades without killing the session. |
| 7.6 | Trigger repeated rediscovery of a path/config fact. | Rediscovered-fact detector records ledger entries and resolves memory/skill save targets. |

### 8. Subagents, insights, and plugin trust boundaries

Covers AS-044, AS-045, AS-046, AS-050, AS-056, AS-059, AS-088, AS-107, AS-108, AS-112, AS-113.

| Step | Action | Expected result |
| --- | --- | --- |
| 8.0 | In an interactive session, prompt the model to use the `task` tool to delegate a self-contained subtask (or two in parallel). | The child runs in its own session (visible via `/resume` as a `task: …` session linked to the parent); its summary returns into the parent context attributed to `task`; the child's spend is included in `/cost`. Child tool calls prompt through the same permission gate. Headless/serve delegation and per-child cost itemization are **Not implemented** until AS-119/AS-120. |
| 8.1 | Run a session that should invoke built-in subagent/living-skill lifecycle hooks. | Built-in system subagents are registered by the composition root and run through the turn lifecycle. |
| 8.2 | Run `/insights` after a session with a goal, tool use, and context churn. | Implemented dashboard summarizes cost/context/session findings; model-assisted goal anchoring is **Not implemented** until AS-109 lands. |
| 8.2a | Across several sessions in the same project, rediscover the same fact (and/or enable the skill-expectation analyzer), then run `/skills`. | The per-session findings list plus a cross-session rollup render; a fact seen in 3+ sessions is flagged escalated; `/skills apply <n>` lands the remedy's diff into its target file and marks the finding resolved (it stops pending and survives a restart). |
| 8.3 | Inspect plugin/subagent registry docs and tests. | Third-party plugin boundary remains declarative-only and guarded. |
| 8.4 | Read AS-113. | Plugin consent screen remains **Needs clarification** because plugin install/marketplace flow is not ticketed yet. |

### 9. Headless, serve, GUI-adjacent faces, and external integrations

Covers AS-051, AS-077, and not-yet-implemented AS-052, AS-078 through AS-081.

| Step | Action | Expected result |
| --- | --- | --- |
| 9.1 | Run `./smith run "say hello"` with provider credentials and again without credentials. | With credentials, a non-interactive run streams/completes; without credentials, failure is concise and non-zero. |
| 9.2 | Run `./smith run --output json` and `--output stream-json` for a small prompt. | Output is machine-readable and diagnostics stay on stderr. |
| 9.3 | Start `./smith serve` on the default bind address and inspect startup output. | JSON-RPC/WebSocket server binds to loopback by default and documents `--unsafe-bind` for non-loopback. |
| 9.4 | Try ACP, web GUI, WASM inspector, hosted sandboxing, and Viscose extension entry points. | These remain **Not implemented** unless their tickets have landed; help/README should not advertise them as complete. |

### 10. Quality harness, architecture guards, and benchmark guardrails

Covers AS-030 and AS-095 through AS-103.

| Step | Action | Expected result |
| --- | --- | --- |
| 10.1 | Run `scripts/harness/quick.sh ./...`. | Formatting and Go tests run through the quick gate. |
| 10.2 | Run `scripts/harness/arch.sh`. | Architecture contract tests pass. |
| 10.3 | Run `./scripts/agent-quality-gate.sh` or `scripts/harness/full.sh`. | `make fmt`, `make test`, `make vet`, and pinned `make lint` pass. |
| 10.4 | Run `scripts/harness/ci-local.sh`. | Local command sequence mirrors CI. |
| 10.5 | Run `scripts/harness/benchmark.sh`. | Deterministic benchmark report appears under `.cache/bench/`; it is advisory, not a required gate. |

## Ticket coverage matrix

| Ticket | Title | Campaign status |
| --- | --- | --- |
| AS-001 | Project scaffolding, CI pipeline, and Apache-2.0 license | Implemented (`done`) |
| AS-002 | Spike: mainstream agent wire-format union (polyglot schema groundwork) | Implemented (`done`) |
| AS-003 | Immutable content-block schema v1 (the frozen substrate) | Implemented (`done`) |
| AS-004 | Additive-only schema guard (compatibility tests + CI enforcement) | Implemented (`done`) |
| AS-005 | Append-only event log store | Implemented (`done`) |
| AS-006 | Context projection engine (model-facing context as a pure projection over the log) | Implemented (`done`) |
| AS-007 | Session persistence — save, list, and load sessions on disk | Implemented (`done`) |
| AS-008 | Provider abstraction interface (pluggable, normalized) | Implemented (`done`) |
| AS-009 | Anthropic provider implementation | Implemented (`done`) |
| AS-010 | OpenAI provider implementation (+ OpenAI-compatible endpoint support) | Implemented (`done`) |
| AS-011 | Prompt caching support and cache-aware prompt assembly | Implemented (`done`) |
| AS-012 | Provider conformance test suite (recorded fixtures, no live calls in CI) | Implemented (`done`) |
| AS-013 | Tool runtime framework (registry, validation, execution, logging) | Implemented (`done`) |
| AS-014 | Core tools: file read / write / edit, glob, grep | Implemented (`done`) |
| AS-015 | Shell tool (command execution, gated by permissions) | Implemented (`done`) |
| AS-016 | Permission model (ask / allowlist / auto) + documented security posture | Implemented (`done`) |
| AS-017 | OS-keychain API key storage | Implemented (`done`) |
| AS-018 | Agentic loop orchestrator | Implemented (`done`) |
| AS-019 | Parallel tool execution for independent calls | Implemented (`done`) |
| AS-020 | Token & cost accounting engine + /cost command | Implemented (`done`) |
| AS-021 | TUI skeleton — streaming chat, input, status line | Implemented (`done`) |
| AS-022 | Slash-command framework + command palette | Implemented (`done`) |
| AS-023 | Parity commands: /clear, /model, /resume | Implemented (`done`) |
| AS-024 | TUI tool-call transparency, diff review, and permission prompts | Implemented (`done`) |
| AS-025 | Always-visible context meter in the TUI | Implemented (`done`) |
| AS-026 | /context — context composition view (flagship wedge, v1 scope) | Implemented (`done`) |
| AS-027 | Segment topic labeling engine | Implemented (`done`) |
| AS-028 | /clean — manual segment removal with preview, archive, and undo | Implemented (`done`) |
| AS-029 | /clean "<topic>" — natural-language semantic matching | Not implemented (`ready-to-implement`) |
| AS-030 | Cost/speed benchmark suite (the D5 internal guardrail) | Implemented (`done`) |
| AS-031 | Layered configuration system | Implemented (`done`) |
| AS-032 | Memory files — AGENT.md, CLAUDE.md, AGENTS.md loaded and merged hierarchically | Implemented (`done`) |
| AS-033 | Custom slash commands from project and user directories | Implemented (`done`) |
| AS-034 | Portable skills — discovery, matching, and loading | Implemented (`done`) |
| AS-035 | Lifecycle hooks (session, tool use, compact, prompt-submit) | Implemented (`done`) |
| AS-036 | MCP client (stdio + HTTP/SSE servers) | Implemented (`done`) |
| AS-037 | /rewind — checkpoint and restore | Implemented (`done`) |
| AS-038 | /compact — lossy summarization fallback (but reversible here) | Implemented (`done`) |
| AS-039 | /init — scaffold project config and memory file | Implemented (`done`) |
| AS-040 | /goal — explicit session objective that anchors insights | Implemented (`done`) |
| AS-041 | Budget guardrails + /budget command | Implemented (`done`) |
| AS-042 | Model routing/tiering + /route command | Implemented (`done`) |
| AS-043 | /tidy — context reorganization without lossy summarization (flagship wedge) | Not implemented (`ready-to-implement`) |
| AS-044 | System sub-agent lifecycle framework + plugin registry | Implemented (`done`) |
| AS-045 | /insights — model-assisted session retrospective dashboard (flagship wedge) | Implemented (`done`) |
| AS-046 | User-delegated subagents (scoped child agents with own context) | Implemented (`done`) — interactive face; headless/serve + per-child cost itemization spun out to AS-119/AS-120 |
| AS-047 | Skill expectation contracts (frontmatter schema, parsing, span boundaries) | Implemented (`done`) |
| AS-048 | Rediscovered-fact detector (living skills, first form) | Implemented (`done`) |
| AS-049 | skill-expectation-analyzer — predict-then-measure skill grading (experimental) | Implemented (`done`) |
| AS-050 | /skills — per-session findings + cross-session rollup | Implemented (`done`) |
| AS-051 | Headless CLI mode (scripting / CI) | Implemented (`done`) |
| AS-052 | ACP server (editor / programmatic protocol face) | Not implemented (`ready-to-implement`) |
| AS-053 | The Matrix layer — personality theme + /serious kill switch | Implemented (`done`) |
| AS-054 | Background/async runner (queue, scheduled, resumable, budget-capped) | Not implemented (`ready-to-implement`) |
| AS-055 | Replayable run logs + OpenTelemetry export | Not implemented (`ready-to-implement`) |
| AS-056 | Design spike: compliance archiving — immutability vs right-to-erasure | Implemented (`done`) |
| AS-057 | Cross-session analytics (portfolio dashboard) | Not implemented (`ready-to-implement`) |
| AS-058 | Self-improving config (aggregated insights propose memory/skill/command edits) | Not implemented (`ready-to-implement`) |
| AS-059 | Design spike: third-party sub-agent plugin trust, permissions, and sandboxing | Implemented (`done`) |
| AS-060 | Capture & compare real vendor session files to refine the block schema before V1 freeze | Not implemented (`ready-to-implement`) |
| AS-061 | Publish the block schema as JSON Schema (language-neutral contract + Go↔schema divergence guard) | Not implemented (`ready-to-implement`) |
| AS-062 | Fuller JSON-Schema validation for tool arguments | Implemented (`done`) |
| AS-063 | Per-block token estimates for window composition pricing | Implemented (`done`) |
| AS-064 | /resume interactive picker + transcript rehydration | Implemented (`done`) |
| AS-065 | CLI subcommand router + arg/output/exit-code contract | Implemented (`done`) |
| AS-066 | Shared command registry — slash ↔ subcommand parity metadata | Implemented (`done`) |
| AS-067 | TUI inspect-mode panel framework + focus/hotkey routing | Implemented (`done`) |
| AS-068 | /clean interactive multi-select from the /context panel + per-item archive restore | Implemented (`done`) |
| AS-069 | smith run -f <file> is shadowed by ambient stdin on a non-TTY | Implemented (`done`) |
| AS-070 | smith <cmd> --help omits command-specific flags | Implemented (`done`) |
| AS-071 | Migrate config consumers (flat key=value, permissions, pricing) onto the layered config substrate | Implemented (`done`) |
| AS-072 | Coding Mode shell — /feature & /mode entry/exit + phase-as-block | Implemented (`done`) |
| AS-073 | Coding Mode phase tracker panel + mode presentation | Implemented (`done`) |
| AS-074 | Coding Mode process skill pack (bundled, auto-enabled per phase) | Implemented (`done`) |
| AS-075 | Coding Mode project-level method override (via memory files) | Implemented (`done`) |
| AS-076 | Coding Mode reflect-phase artifacts (success metric, instrumentation, check-back ticket) | Implemented (`done`) |
| AS-077 | `smith serve` — local JSON-RPC/WebSocket session server | Implemented (`done`) |
| AS-078 | Web GUI — thin client over `smith serve` | Not implemented (`ready-to-implement`) |
| AS-079 | WASM observability core + static session inspector | Not implemented (`ready-to-implement`) |
| AS-080 | Spike: hosted multi-tenant live-agent sandboxing | Not implemented (`ready-to-implement`) |
| AS-081 | Viscose (VS Code) extension over `smith serve` | Not implemented (`ready-to-implement`) |
| AS-082 | Memory file @import-style includes | Implemented (`done`) |
| AS-083 | MCP resources, prompts, reconnect, and tools/list pagination | Implemented (`done`) |
| AS-084 | Rewind file-system snapshot & restore | Implemented (`done`) |
| AS-085 | Auto-compact on approaching the window limit (config-flagged, default off) | Implemented (`done`) |
| AS-086 | Conservative budget enforcement — pre-turn estimate + unpriced-turn handling | Implemented (`done`) |
| AS-087 | /init model-assisted draft enrichment | Not implemented (`ready-to-implement`) |
| AS-088 | Wire the sub-agent Runner into the turn loop lifecycle | Implemented (`done`) |
| AS-089 | Shrink cmd/smith into a thin composition root | Implemented (`done`) |
| AS-090 | Consolidate slash and subcommand semantics in one command catalog | Implemented (`done`) |
| AS-091 | Audit interfaces and move small seams to consumer packages | Implemented (`done`) |
| AS-092 | Extract shared stream I/O mechanics for providers and MCP | Implemented (`done`) |
| AS-093 | Add typed consumer config views over layered config | Implemented (`done`) |
| AS-094 | Standardize filesystem traversal on fs.FS and WalkDir | Implemented (`done`) |
| AS-095 | Enforce stdlib-first dependency boundaries for core packages | Implemented (`done`) |
| AS-096 | Add tiny shared render primitives for textual reports | Implemented (`done`) |
| AS-097 | Modernize tests with Go 1.26 stdlib idioms | Implemented (`done`) |
| AS-098 | Document and test package architecture contracts | Implemented (`done`) |
| AS-099 | Harness quality system design and command contract | Implemented (`done`) |
| AS-100 | Add quick, full, architecture, and CI-local harness scripts | Implemented (`done`) |
| AS-101 | Agent and local hook integration for the harness | Implemented (`done`) |
| AS-102 | Repository skills for quality gates and CI triage | Implemented (`done`) |
| AS-103 | Guard CI and local harness parity | Implemented (`done`) |
| AS-104 | Thread a shared flag contract through the command catalog | Implemented (`done`) |
| AS-105 | Migrate the remaining mode-flag commands onto the shared flag contract | Implemented (`done`) |
| AS-106 | Rediscovered-fact detector: path-convergence + config-key signals | Implemented (`done`) |
| AS-107 | Wire a sub-agent Runner into the composition root (register built-ins + install WithSubAgents) | Implemented (`done`) |
| AS-108 | Persist the rediscovered-fact ledger and wire a memory/skill-aware save-target resolver | Implemented (`done`) |
| AS-109 | /insights model-assisted layer + goal anchoring (spun out of AS-045) | Not implemented (`ready-to-implement`) |
| AS-110 | Model routing escalation + per-session /route overrides | Not implemented (`ready-to-implement`) |
| AS-111 | Scope-gated context slices for third-party sub-agents | Not implemented (`ready-to-implement`) |
| AS-112 | Guard the declarative-only plugin boundary with a test + archtest | Implemented (`done`) |
| AS-113 | Plugin consent screen + scope→sentence table | Needs clarification (`needs-clarification`) |
| AS-114 | Scope Coding Mode process-skill blocks to the active phase | Implemented (`done`) |
| AS-115 | Redaction-at-capture — best-effort secret/PII scrub before the log (spun out of AS-056) | Implemented (`done`) |
| AS-116 | Surface auto-escalation in `/route` and `/cost` + wire the first producer | Implemented (`done`) |
| AS-117 | `/tidy` dead-end collapse + working-memory promotion (spun out of AS-043) | Needs clarification (`needs-clarification`) |
| AS-118 | Root help ignores `--output json` | Implemented (`done`) |
| AS-132 | Background runner daemon (`runs work --watch`) + worker concurrency (`--concurrency N`) | Implemented (`done`) — enqueue runs (`smith run "…" --queue`), start `smith runs work --watch --concurrency 2`; confirm runs enqueued after start are picked up, two workers never double-run a record, Ctrl+C drains cleanly, and plain `smith runs work` still drains-and-exits |
| AS-133 | Recorded vendor simulators for Anthropic, OpenAI, and compatible providers | Not implemented (`ready-to-implement`) |
| AS-134 | Offline E2E regression suite over recorded providers, TUI, and append-only logs | Not implemented (`ready-to-implement`) |
| AS-135 | Capture-to-fixture workflow for redacted vendor sessions and CI-safe regressions | Not implemented (`ready-to-implement`) |

## Current local smoke pass (2026-06-22)

The following lightweight checks were attempted while creating this campaign:

| Check | Result | Notes |
| --- | --- | --- |
| `timeout 60 make build` | Pass | The binary built successfully after the initial long-running build was allowed to complete. |
| Ticket status extraction from `docs/project/tickets/AS-*.md` | Pass | Used to generate the coverage matrix above. |
| `./smith --help --output json` plus `python3 -m json.tool` | Pass | Root help now emits valid JSON (root summary, global flags, command tree). Fixed in AS-118. |


AS-118 (originally filed as a duplicate AS-116 id, then renumbered past a colliding AS-117) was created from this smoke pass because root JSON help did not behave as documented; it is now fixed and the JSON help is parseable. No other completed feature was proven to fail during the limited local pass.
