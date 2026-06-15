# Agent Smith — Ticket Backlog

The full backlog derived from [PRD.md](../PRD.md), in two waves:

- **AS-001 … AS-030 — V1** (Decision Log D6 ship set): substrate, providers, loop, tools, permissions, TUI, persistence, cost meter, `/context` + `/clean`. (Plus **AS-060** and **AS-061**, V1-freeze-window schema-hardening passes appended after the spike work, **AS-062**, a tools follow-on spun out of AS-013, **AS-063**, a cost follow-on spun out of AS-020, **AS-064**, a `/resume` UX follow-on spun out of AS-023, **AS-065**, the CLI subcommand router/contract from the [CLI-UX.md](../CLI-UX.md) grilling, and **AS-067**, the TUI inspect-mode panel framework from the [TUI-UX.md](../TUI-UX.md) grilling.)
- **AS-031 … AS-059 — fast-follow & P2** (everything D6 defers): capability layer (memory files, skills, hooks, MCP, custom commands), remaining power commands, the `/tidy`–`/insights`–routing–budgets wedges, system sub-agents + living skills, headless/ACP faces, Matrix layer, async runner, observability/compliance, and two design spikes. (Plus **AS-066**, the shared slash↔subcommand command-registry follow-on from the [CLI-UX.md](../CLI-UX.md) grilling.)

Not ticketed (intentionally): §7.26 plugin marketplace / Desktop UI / team config — PRD marks it "later" and it's too far out to spec honestly; AS-052 (ACP) and AS-059 (plugin trust) are its prerequisites anyway.

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
| [AS-024](AS-024-tui-tool-transparency.md) | Tool transparency, diff review, permission prompts | tui | ready | 021, 016, 067 |
| [AS-025](AS-025-context-meter.md) | Always-visible context meter | tui | done | 006, 020, 021 |
| [AS-026](AS-026-context-composition-view.md) | `/context` composition view (wedge) | context-wedge | ready | 006, 020, 022 |
| [AS-027](AS-027-segment-topic-labeling.md) | Segment topic labeling engine | context-wedge | **needs clarification** | 006 |
| [AS-028](AS-028-clean-manual.md) | `/clean` manual removal + preview/undo (wedge) | context-wedge | ready | 006, 026 |
| [AS-029](AS-029-clean-semantic.md) | `/clean "<topic>"` semantic matching (wedge) | context-wedge | **needs clarification** | 028, 027 |
| [AS-030](AS-030-benchmark-guardrail-suite.md) | Cost/speed benchmark suite (D5 guardrail) | quality | **needs clarification** | 018, 020 |
| [AS-060](AS-060-session-capture-corpus.md) | Capture & compare real vendor session files before V1 schema freeze | schema | ready | 002, 003 |
| [AS-061](AS-061-json-schema-publication.md) | Publish the block schema as JSON Schema (+ Go↔schema divergence guard) | schema | ready | 003, 004, 060 |
| [AS-062](AS-062-tool-arg-schema-validation.md) | Fuller JSON-Schema validation for tool arguments | tools | ready | 013 |
| [AS-063](AS-063-per-block-token-estimates.md) | Per-block token estimates for window composition pricing | cost | done | 020, 006 |
| [AS-064](AS-064-resume-picker-rehydration.md) | `/resume` interactive picker + transcript rehydration | tui | ready | 023, 024 |
| [AS-065](AS-065-cli-subcommand-router.md) | CLI subcommand router + arg/output/exit-code contract | faces | ready | 018, 021, 022 |
| [AS-067](AS-067-tui-panel-framework.md) | TUI inspect-mode panel framework + focus/hotkey routing | tui | ready | 021, 022 |

## Index — Fast-follow & P2 (AS-031 … AS-059)

| ID | Title | Area | Status | Depends on |
|---|---|---|---|---|
| [AS-031](AS-031-config-system.md) | Layered configuration system | foundation | ready | 001 |
| [AS-032](AS-032-memory-files.md) | Memory files (AGENT/CLAUDE/AGENTS.md merge) | capability | ready | 006, 018, 031 |
| [AS-033](AS-033-custom-slash-commands.md) | Custom slash commands | commands | ready | 022, 031 |
| [AS-034](AS-034-skills-loading.md) | Portable skills loading | capability | ready | 018, 031 |
| [AS-035](AS-035-hooks.md) | Lifecycle hooks | capability | ready | 013, 018, 031 |
| [AS-036](AS-036-mcp-client.md) | MCP client (stdio + HTTP/SSE) | capability | ready | 013, 016, 031 |
| [AS-037](AS-037-rewind.md) | `/rewind` checkpoint/restore | commands | ready | 006, 022 |
| [AS-038](AS-038-compact.md) | `/compact` (reversible, for once) | commands | ready | 006, 022 |
| [AS-039](AS-039-init.md) | `/init` project scaffold | commands | ready | 014, 022, 032 |
| [AS-040](AS-040-goal.md) | `/goal` session objective | commands | ready | 006, 022 |
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
| [AS-066](AS-066-command-registry-parity.md) | Shared command registry — slash ↔ subcommand parity | commands | ready | 022, 065 |

## Suggested build order

1. **Substrate first** (the moat): 001 → 002 → 003 → 004, then 005–007 in parallel with 008. Run **060** (capture real vendor sessions, refine the schema) before the V1 freeze of 003 — D2 makes the schema additive-only only *from* V1.
2. **Providers + tools**: 009/010 → 011/012 · 013 → 014/016 → 015.
3. **Loop + faces**: 018 → 019/021 → 022 → 020/023/024/025; 065 (CLI router) once 022 exists, so commands grow on the subcommand-first shape.
4. **The V1 wedges** (the demo): 026 → 028, while 027/029 get their open questions answered.
5. **Guardrail**: 030 as soon as 018+020 exist — D5 needs measurements before habits form. AS-056 (spike) can run any time; AS-059 after 044.
6. **Fast-follow, capability wave**: 031 → 032/033/034/035/036 (mostly parallel) + the cheap commands 037–041.
7. **Fast-follow, wedge wave**: 044 → 045/046 → 047 → 048/049/050; 051 → 052/053; 066 (registry parity) alongside 051; 042/043 once clarified.
8. **P2**: 054/055/057/058 as the async + analytics story matures.

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

The two spikes (AS-056, AS-059) are themselves the clarification work for PRD open questions Q13 and Q12 — they're ready to start.

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
