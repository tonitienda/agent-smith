# Agent Smith — Ticket Backlog

The full backlog derived from [PRD.md](../PRD.md), in three waves:

- **AS-001 … AS-030 — V1** (Decision Log D6 ship set): substrate, providers, loop, tools, permissions, TUI, persistence, cost meter, `/context` + `/clean`. (Plus **AS-060** and **AS-061**, V1-freeze-window schema-hardening passes appended after the spike work, **AS-062**, a tools follow-on spun out of AS-013, **AS-063**, a cost follow-on spun out of AS-020, **AS-064**, a `/resume` UX follow-on spun out of AS-023, **AS-065**, the CLI subcommand router/contract from the [CLI-UX.md](../CLI-UX.md) grilling, and **AS-067**, the TUI inspect-mode panel framework from the [TUI-UX.md](../TUI-UX.md) grilling.)
- **AS-031 … AS-059 — fast-follow & P2** (everything D6 defers): capability layer (memory files, skills, hooks, MCP, custom commands), remaining power commands, the `/tidy`–`/insights`–routing–budgets wedges, system sub-agents + living skills, headless/ACP faces, Matrix layer, async runner, observability/compliance, and two design spikes. (Plus **AS-066**, the shared slash↔subcommand command-registry follow-on from the [CLI-UX.md](../CLI-UX.md) grilling, **AS-068**, the interactive in-panel `/clean` selection spun out of AS-028, and **AS-072 … AS-076**, the Coding Mode orchestration layer from the [coding-mode.prd.md](../coding-mode.prd.md) grilling.)
- **AS-077 … AS-081 — GUI wave** (graphical faces over the face-agnostic core, post-V1): the `smith serve` JSON-RPC/WebSocket spine, a thin-client web GUI and a Viscose (VS Code) extension on top of it, a WASM read-only session inspector (the one place WASM genuinely pays off), and a flagged spike for the D9 collision around hosting a live agent for strangers. See those tickets for the full reasoning (browser can't run the live agent — no fs/shell/exec, no keychain, CORS; the GUI is a new *face* over `smith serve`, not a WASM rewrite).

Not ticketed (intentionally): §7.26 plugin marketplace / team config — PRD marks it "later" and it's too far out to spec honestly; AS-059 (plugin trust) is a prerequisite. (The Desktop/editor UI itself is now ticketed as the GUI wave AS-077…AS-081, shipping on the AS-077 JSON-RPC fallback per §10 Q5 rather than waiting on AS-052/ACP.)

## Conventions

- One file per ticket: `AS-NNN-slug.md`. Frontmatter fields:
  - `id` — stable ticket ID (`AS-NNN`), used in `depends_on` references.
  - `status` — `ready-to-implement` | `needs-clarification` | `done` (later: `in-progress`).
  - `github_issue` — `null` until the GitHub issue is created; then the issue number. Keep it in sync.
  - `depends_on` — ticket IDs that should land (or at least be designed) first.
  - `area`, `priority`, `source` — grouping, PRD tier, and the PRD sections the ticket comes from.
- To find tickets: `grep -l "status: needs-clarification" tickets/` etc.

## Index — V1 (AS-001 … AS-030)

| ID | Title | Area | Status | Depends on |
|---|---|---|---|---|
| [AS-001](AS-001-project-scaffolding.md) | Project scaffolding, CI, Apache-2.0 | foundation | done | — |
| [AS-002](AS-002-wire-format-spike.md) | Spike: mainstream agent wire-format union (D4) | schema | done | 001 |
| [AS-003](AS-003-block-schema-v1.md) | Immutable content-block schema v1 | schema | done | 002 |
| [AS-004](AS-004-additive-only-guard.md) | Additive-only schema guard (CI) | schema | done | 003 |
| [AS-005](AS-005-event-log-store.md) | Append-only event log store | core-log | done | 003 |
| [AS-006](AS-006-context-projection-engine.md) | Context projection engine | core-log | done | 005 |
| [AS-007](AS-007-session-persistence.md) | Session persistence (save/list/load) | core-log | done | 005 |
| [AS-008](AS-008-provider-interface.md) | Provider abstraction interface | provider | done | 002, 003 |
| [AS-009](AS-009-anthropic-provider.md) | Anthropic provider | provider | done | 008 |
| [AS-010](AS-010-openai-provider.md) | OpenAI provider (+ compatible endpoints) | provider | done | 008 |
| [AS-011](AS-011-prompt-caching.md) | Prompt caching + cache-aware assembly | provider | done | 009, 010, 006 |
| [AS-012](AS-012-provider-conformance-suite.md) | Provider conformance test suite | provider | done | 009, 010 |
| [AS-013](AS-013-tool-runtime.md) | Tool runtime framework | tools | done | 005 |
| [AS-014](AS-014-file-and-search-tools.md) | File & search tools (read/write/edit/glob/grep) | tools | done | 013 |
| [AS-015](AS-015-shell-tool.md) | Shell tool | tools | done | 013, 016 |
| [AS-016](AS-016-permission-model.md) | Permission model + security posture doc | security | done | 013 |
| [AS-017](AS-017-keychain-key-storage.md) | OS-keychain API key storage | security | **needs clarification** | 001 |
| [AS-018](AS-018-agentic-loop.md) | Agentic loop orchestrator | loop | done | 006, 008, 013 |
| [AS-019](AS-019-parallel-tool-execution.md) | Parallel tool execution | loop | done | 018 |
| [AS-020](AS-020-cost-accounting.md) | Token & cost accounting + `/cost` | cost | done | 009, 010, 022 |
| [AS-021](AS-021-tui-skeleton.md) | TUI skeleton (streaming chat) | tui | done | 018 |
| [AS-022](AS-022-slash-command-framework.md) | Slash-command framework + palette | commands | done | 021 |
| [AS-023](AS-023-parity-commands.md) | Parity commands: `/clear` `/model` `/resume` | commands | done | 007, 008, 022 |
| [AS-024](AS-024-tui-tool-transparency.md) | Tool transparency, diff review, permission prompts | tui | done | 021, 016, 067 |
| [AS-025](AS-025-context-meter.md) | Always-visible context meter | tui | done | 006, 020, 021 |
| [AS-026](AS-026-context-composition-view.md) | `/context` composition view (wedge) | context-wedge | done | 006, 020, 022 |
| [AS-027](AS-027-segment-topic-labeling.md) | Segment topic labeling engine | context-wedge | **needs clarification** | 006 |
| [AS-028](AS-028-clean-manual.md) | `/clean` manual removal + preview/undo (wedge) | context-wedge | done | 006, 026 |
| [AS-029](AS-029-clean-semantic.md) | `/clean "<topic>"` semantic matching (wedge) | context-wedge | **needs clarification** | 028, 027 |
| [AS-030](AS-030-benchmark-guardrail-suite.md) | Cost/speed benchmark suite (D5 guardrail) | quality | **needs clarification** | 018, 020 |
| [AS-060](AS-060-session-capture-corpus.md) | Capture & compare real vendor session files before V1 schema freeze | schema | ready | 002, 003 |
| [AS-061](AS-061-json-schema-publication.md) | Publish the block schema as JSON Schema (+ Go↔schema divergence guard) | schema | ready | 003, 004, 060 |
| [AS-062](AS-062-tool-arg-schema-validation.md) | Fuller JSON-Schema validation for tool arguments | tools | done | 013 |
| [AS-063](AS-063-per-block-token-estimates.md) | Per-block token estimates for window composition pricing | cost | done | 020, 006 |
| [AS-064](AS-064-resume-picker-rehydration.md) | `/resume` interactive picker + transcript rehydration | tui | done | 023, 024 |
| [AS-065](AS-065-cli-subcommand-router.md) | CLI subcommand router + arg/output/exit-code contract | faces | done | 018, 021, 022 |
| [AS-067](AS-067-tui-panel-framework.md) | TUI inspect-mode panel framework + focus/hotkey routing | tui | done | 021, 022 |

## Index — Fast-follow & P2 (AS-031 … AS-059)

| ID | Title | Area | Status | Depends on |
|---|---|---|---|---|
| [AS-031](AS-031-config-system.md) | Layered configuration system | foundation | done | 001 |
| [AS-032](AS-032-memory-files.md) | Memory files (AGENT/CLAUDE/AGENTS.md merge) | capability | done | 006, 018, 031 |
| [AS-033](AS-033-custom-slash-commands.md) | Custom slash commands | commands | done | 022, 031 |
| [AS-034](AS-034-skills-loading.md) | Portable skills loading | capability | done | 018, 031 |
| [AS-035](AS-035-hooks.md) | Lifecycle hooks | capability | done | 013, 018, 031 |
| [AS-036](AS-036-mcp-client.md) | MCP client (stdio + HTTP/SSE) | capability | done | 013, 016, 031 |
| [AS-037](AS-037-rewind.md) | `/rewind` checkpoint/restore | commands | done | 006, 022 |
| [AS-038](AS-038-compact.md) | `/compact` (reversible, for once) | commands | done | 006, 022 |
| [AS-039](AS-039-init.md) | `/init` project scaffold | commands | ready | 014, 022, 032 |
| [AS-040](AS-040-goal.md) | `/goal` session objective | commands | done | 006, 022 |
| [AS-041](AS-041-budget-guardrails.md) | Budget guardrails + `/budget` | cost | ready | 020, 022, 031 |
| [AS-042](AS-042-model-routing.md) | Model routing/tiering + `/route` | cost | **needs clarification** | 008, 031, 044 |
| [AS-043](AS-043-tidy.md) | `/tidy` context reorganization (wedge) | context-wedge | **needs clarification** | 006, 027, 028 |
| [AS-044](AS-044-system-subagent-framework.md) | System sub-agent framework + plugin registry | subagents | ready | 006, 018, 020, 031 |
| [AS-045](AS-045-insights-dashboard.md) | `/insights` session dashboard (wedge) | insights-wedge | ready | 020, 022, 044 |
| [AS-046](AS-046-user-subagents.md) | User-delegated subagents | subagents | ready | 013, 018 |
| [AS-047](AS-047-skill-contracts.md) | Skill expectation contracts (C.1) | living-skills | ready | 034 |
| [AS-048](AS-048-rediscovered-fact-detector.md) | Rediscovered-fact detector (D7 first form) | living-skills | **needs clarification** | 032, 044, 047 |
| [AS-049](AS-049-skill-expectation-analyzer.md) | skill-expectation-analyzer (experimental) | living-skills | ready | 044, 047, 048 |
| [AS-050](AS-050-skills-report.md) | `/skills` report + cross-session rollup | living-skills | ready | 007, 022, 049 |
| [AS-051](AS-051-headless-cli.md) | Headless CLI mode | faces | ready | 018, 031 |
| [AS-052](AS-052-acp-server.md) | ACP server | faces | **needs clarification** | 018, 051 |
| [AS-053](AS-053-matrix-personality-layer.md) | Matrix personality layer + `/serious` | polish | ready | 021, 022, 031 |
| [AS-054](AS-054-async-runner.md) | Background/async runner | async | **needs clarification** | 007, 041, 051 |
| [AS-055](AS-055-replayable-runs-otel.md) | Replayable runs + OpenTelemetry export | observability | ready | 005, 007, 020 |
| [AS-056](AS-056-compliance-archiving-spike.md) | Spike: compliance archiving vs erasure (Q13) | compliance | ready (spike) | 005 |
| [AS-057](AS-057-cross-session-analytics.md) | Cross-session analytics | insights-wedge | **needs clarification** | 007, 020, 045 |
| [AS-058](AS-058-self-improving-config.md) | Self-improving config | insights-wedge | **needs clarification** | 032, 045, 050 |
| [AS-059](AS-059-plugin-trust-spike.md) | Spike: plugin trust & sandboxing (Q12) | security | ready (spike) | 044 |
| [AS-066](AS-066-command-registry-parity.md) | Shared command registry — slash ↔ subcommand parity | commands | done | 022, 065 |
| [AS-068](AS-068-clean-interactive-selection.md) | `/clean` interactive multi-select + per-item archive restore | context-wedge | done | 028, 067 |
| [AS-069](AS-069-headless-prompt-file-precedence.md) | `smith run -f <file>` shadowed by ambient stdin on a non-TTY | faces | done | 065 |
| [AS-070](AS-070-leaf-help-command-flags.md) | `smith <cmd> --help` omits command-specific flags | faces | done | 065 |
| [AS-071](AS-071-config-consumer-migration.md) | Migrate config consumers onto the layered config substrate | foundation | done | 031 |
| [AS-072](AS-072-coding-mode-shell.md) | Coding Mode shell — `/feature` & `/mode` entry/exit + phase-as-block | coding-mode | **needs clarification** | 006, 033, 040 |
| [AS-073](AS-073-coding-mode-phase-tracker.md) | Coding Mode phase tracker panel + presentation | coding-mode | **needs clarification** | 067, 072 |
| [AS-074](AS-074-coding-mode-process-skill-pack.md) | Coding Mode process skill pack (bundled, auto-enabled) | coding-mode | ready | 034, 072 |
| [AS-075](AS-075-coding-mode-method-override.md) | Coding Mode project-level method override (memory files) | coding-mode | ready | 032, 072 |
| [AS-076](AS-076-coding-mode-reflect-artifacts.md) | Coding Mode reflect-phase artifacts | coding-mode | **needs clarification** | 045, 048, 072 |
| [AS-077](AS-077-serve-jsonrpc-session-server.md) | `smith serve` — local JSON-RPC/WebSocket session server | faces | ready | 018, 051, 066 |
| [AS-078](AS-078-web-gui-thin-client.md) | Web GUI — thin client over `smith serve` | faces | ready | 077 |
| [AS-079](AS-079-wasm-session-inspector.md) | WASM observability core + static session inspector | observability | ready | 005, 006, 020, 061, 038 |
| [AS-080](AS-080-hosted-agent-sandboxing-spike.md) | Spike: hosted multi-tenant live-agent sandboxing | security | **needs clarification** (spike) | 077, 059 |
| [AS-081](AS-081-viscose-vscode-extension.md) | Viscose (VS Code) extension over `smith serve` | faces | **needs clarification** | 077, 078 |
| [AS-082](AS-082-memory-file-imports.md) | Memory file `@import`-style includes | capability | done | 032 |
| [AS-083](AS-083-mcp-resources-prompts-reconnect.md) | MCP resources, prompts, reconnect & pagination | capability | ready | 036 |
| [AS-084](AS-084-rewind-file-snapshot.md) | Rewind file-system snapshot & restore | commands | **needs clarification** | 037 |
| [AS-085](AS-085-auto-compact.md) | Auto-compact on approaching the window limit (config-flagged, default off) | commands | ready | 038, 025, 031 |

## Suggested build order

1. **Substrate first** (the moat): 001 → 002 → 003 → 004, then 005–007 in parallel with 008. Run **060** (capture real vendor sessions, refine the schema) before the V1 freeze of 003 — D2 makes the schema additive-only only *from* V1.
2. **Providers + tools**: 009/010 → 011/012 · 013 → 014/016 → 015.
3. **Loop + faces**: 018 → 019/021 → 022 → 020/023/024/025; 065 (CLI router) once 022 exists, so commands grow on the subcommand-first shape.
4. **The V1 wedges** (the demo): 026 → 028, while 027/029 get their open questions answered.
5. **Guardrail**: 030 as soon as 018+020 exist — D5 needs measurements before habits form. AS-056 (spike) can run any time; AS-059 after 044.
6. **Fast-follow, capability wave**: 031 → 032/033/034/035/036 (mostly parallel) + the cheap commands 037–041.
7. **Fast-follow, wedge wave**: 044 → 045/046 → 047 → 048/049/050; 051 → 052/053; 066 (registry parity) alongside 051; 042/043 once clarified.
8. **P2**: 054/055/057/058 as the async + analytics story matures.
9. **Coding Mode** (orchestration layer, after the capability + wedge waves): 072 (shell) → 073 (phase tracker) / 074 (process skills) / 075 (method override) → 076 (reflect artifacts). 072/073/076 need their open questions answered first (see below); 074/075 are ready once 034/032 land.
10. **GUI wave** (graphical faces, after 051): 077 (`smith serve`, the JSON-RPC/WS spine — the Q5 JSON-RPC-first fallback that ACP later re-skins) → 078 (web thin client) and 081 (Viscose extension, once its wiring question is answered). 079 (WASM read-only session inspector) is largely independent — it needs the substrate (005/006/020/061) plus 038 for the `/compact` preview view, but none of the GUI-wave serve/client work — and is the genuine WASM payoff plus the safe public demo. 080 (hosted multi-tenant sandboxing) is a flagged spike that must clear D9 before any stranger-facing live hosting; it likely closes in favour of 079.

## Needs clarification — decisions to make

| Ticket | The decision |
|---|---|
| [AS-017](AS-017-keychain-key-storage.md) | V1 platform scope (Windows?), no-keychain fallback, multi-profile keys |
| [AS-027](AS-027-segment-topic-labeling.md) | How topics are derived (heuristics / embeddings / cheap model), when labeling runs, cost budget |
| [AS-029](AS-029-clean-semantic.md) | PRD §10 Q4: `/clean` matching engine + latency/cost budget |
| [AS-030](AS-030-benchmark-guardrail-suite.md) | Benchmark task suite, the "naive baseline harness", run budget/cadence, variance |
| [AS-042](AS-042-model-routing.md) | Does routing touch the main loop in v1; escalation semantics; cross-provider policy |
| [AS-043](AS-043-tidy.md) | Dead-end detection, fidelity-diff definition, model budget, topic-label dependency |
| [AS-048](AS-048-rediscovered-fact-detector.md) | Detection mechanism + precision bar, durable-fact definition, save-target rule, offer UX |
| [AS-052](AS-052-acp-server.md) | PRD §10 Q5: ACP now vs minimal JSON-RPC first; spec pinning; first editor |
| [AS-054](AS-054-async-runner.md) | Process model (daemon?), queue semantics, scheduling ownership, completion surface |
| [AS-057](AS-057-cross-session-analytics.md) | Surface (TUI/HTML), aggregation index vs on-demand scan, friction scope |
| [AS-058](AS-058-self-improving-config.md) | Trigger/cadence, edit-target scope, approval UX, conflict handling |
| [AS-072](AS-072-coding-mode-shell.md) | coding-mode.prd.md Q1 (naming `/feature` vs `/mode`, mode primitive?), Q2 (phase-advancement trigger), Q4 (multi-feature interleaving) |
| [AS-073](AS-073-coding-mode-phase-tracker.md) | coding-mode.prd.md Q5 (does Coding Mode mean anything headless/ACP, or TUI-only) |
| [AS-076](AS-076-coding-mode-reflect-artifacts.md) | Blocked on AS-048 (needs-clarification); reflect-artifact depth (scratch note vs synced check-back tickets) |
| [AS-080](AS-080-hosted-agent-sandboxing-spike.md) | Is a live hosted demo in scope at all (vs AS-079 inspector)? If so: isolation model, key/cost model, tool subset (D9 collision) |
| [AS-084](AS-084-rewind-file-snapshot.md) | Snapshot mechanism (content/git/log-replay), shell-change capture, snapshot cost/pruning, working-tree conflict handling, opt-in vs default |
| [AS-081](AS-081-viscose-vscode-extension.md) | How the extension reaches the core (bundle native binary / spawn user's `smith` / WASM-in-host); workspace integration depth; lifecycle; distribution |

The spikes (AS-056, AS-059, AS-080) are themselves the clarification work for PRD open questions (Q13, Q12, and the D9 hosted-execution collision) — AS-056/AS-059 are ready to start; AS-080's first question is whether it should exist.

## Syncing to GitHub issues

Each file maps 1:1 to an issue via [`cmd/ticket-sync`](../../../cmd/ticket-sync/main.go). **The files are the source of truth** — the tool never merges; it overwrites the issue (title, body, labels) from the file.

```sh
go run ./cmd/ticket-sync                 # sync tickets added/edited but not yet pushed
go run ./cmd/ticket-sync -all            # sync every ticket (first run: creates all issues)
go run ./cmd/ticket-sync docs/project/tickets/AS-017-keychain-key-storage.md   # sync specific files
go run ./cmd/ticket-sync -dry-run        # show what would happen
```

- `github_issue: null` → an issue is created and its number is written back into the frontmatter (commit that change).
- `github_issue: <n>` → issue `#n` is updated from the file.
- `status: done` → the synced GitHub issue is closed after its title/body/labels are updated.
- Labels applied: `status`, `area:<area>`, `priority` (created on the repo if missing).
- Auth via the `gh` CLI (`gh auth login`). Repo resolution: `-repo owner/name` flag → `TICKET_SYNC_REPO` env var → the current git remote.
- After a pull request is merged, the **Sync merged tickets** GitHub Actions workflow finds ticket files changed by that PR and runs `go run ./cmd/ticket-sync -require-existing` against them. This keeps related issues current and closes `done` tickets, while failing on `github_issue: null` so new tickets are linked before merge instead of creating uncommitted issue-number changes in CI.
