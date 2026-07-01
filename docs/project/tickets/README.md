# Agent Smith — Ticket Backlog

The full backlog derived from [PRD.md](../PRD.md), in three waves:

- **AS-001 … AS-030 — V1** (Decision Log D6 ship set): substrate, providers, loop, tools, permissions, TUI, persistence, cost meter, `/context` + `/clean`. (Plus **AS-060** and **AS-061**, V1-freeze-window schema-hardening passes appended after the spike work, **AS-062**, a tools follow-on spun out of AS-013, **AS-063**, a cost follow-on spun out of AS-020, **AS-064**, a `/resume` UX follow-on spun out of AS-023, **AS-065**, the CLI subcommand router/contract from the [CLI-UX.md](../CLI-UX.md) grilling, and **AS-067**, the TUI inspect-mode panel framework from the [TUI-UX.md](../TUI-UX.md) grilling.)
- **AS-031 … AS-059 — fast-follow & P2** (everything D6 defers): capability layer (memory files, skills, hooks, MCP, custom commands), remaining power commands, the `/tidy`–`/insights`–routing–budgets wedges, system sub-agents + living skills, headless/ACP faces, Matrix layer, async runner, observability/compliance, and two design spikes. (Plus **AS-066**, the shared slash↔subcommand command-registry follow-on from the [CLI-UX.md](../CLI-UX.md) grilling, **AS-068**, the interactive in-panel `/clean` selection spun out of AS-028, and **AS-072 … AS-076**, the Coding Mode orchestration layer from the [coding-mode.prd.md](../coding-mode.prd.md) grilling.)
- **AS-077 … AS-081 — GUI wave** (graphical faces over the face-agnostic core, post-V1): the `smith serve` JSON-RPC/WebSocket spine, a thin-client web GUI and a Viscose (VS Code) extension on top of it, a WASM read-only session inspector (the one place WASM genuinely pays off), and a flagged spike for the D9 collision around hosting a live agent for strangers. See those tickets for the full reasoning (browser can't run the live agent — no fs/shell/exec, no keychain, CORS; the GUI is a new *face* over `smith serve`, not a WASM rewrite).

Not ticketed (intentionally): §7.26 plugin marketplace / team config — PRD marks it "later" and it's too far out to spec honestly; AS-059 (plugin trust) is a prerequisite. (The Desktop/editor UI itself is now ticketed as the GUI wave AS-077…AS-081, shipping on the AS-077 JSON-RPC fallback per §10 Q5 rather than waiting on AS-052/ACP.)

## Conventions

- One file per ticket: `AS-NNN-slug.md`. Frontmatter fields:
  - `id` — stable ticket ID (`AS-NNN`), used in `depends_on` references.
  - `status` — `ready-to-implement` | `needs-clarification` | `done` | `Pending Debrief` (later: `in-progress`). Use `Pending Debrief` for product-management discovery items that need human debrief before normal backlog triage.
  - `github_issue` — `null` until the GitHub issue is created; then the issue number. Keep it in sync.
  - `type` — optional ticket kind such as `bug`; when present, `ticket-sync` applies it as a `type:<value>` GitHub label. Use `type: bug` for defects found during manual or automated test passes.
  - `depends_on` — ticket IDs that should land (or at least be designed) first.
  - `area`, `priority`, `source` — grouping, PRD tier, and the PRD sections the ticket comes from.
- To find tickets: `grep -l "status: needs-clarification" tickets/` etc.
- GitHub issues are mirrors, not the source of truth. `cmd/ticket-sync` updates
  already-linked issues after merge, and the scheduled sync creates or links
  still-unlinked tickets. Before creating a new GitHub issue it searches for an
  existing issue whose title starts with the ticket ID (for example
  `[AS-123] ...`) so a failed write-back/push does not duplicate the ticket on
  the next scheduled run. Tickets with `status: done` are closed with GitHub's
  `completed` state reason when synced.

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
| [AS-017](AS-017-keychain-key-storage.md) | OS-keychain API key storage | security | done | 001 |
| [AS-018](AS-018-agentic-loop.md) | Agentic loop orchestrator | loop | done | 006, 008, 013 |
| [AS-019](AS-019-parallel-tool-execution.md) | Parallel tool execution | loop | done | 018 |
| [AS-020](AS-020-cost-accounting.md) | Token & cost accounting + `/cost` | cost | done | 009, 010, 022 |
| [AS-021](AS-021-tui-skeleton.md) | TUI skeleton (streaming chat) | tui | done | 018 |
| [AS-022](AS-022-slash-command-framework.md) | Slash-command framework + palette | commands | done | 021 |
| [AS-023](AS-023-parity-commands.md) | Parity commands: `/clear` `/model` `/resume` | commands | done | 007, 008, 022 |
| [AS-024](AS-024-tui-tool-transparency.md) | Tool transparency, diff review, permission prompts | tui | done | 021, 016, 067 |
| [AS-025](AS-025-context-meter.md) | Always-visible context meter | tui | done | 006, 020, 021 |
| [AS-026](AS-026-context-composition-view.md) | `/context` composition view (wedge) | context-wedge | done | 006, 020, 022 |
| [AS-027](AS-027-segment-topic-labeling.md) | Segment topic labeling engine | context-wedge | done | 006 |
| [AS-028](AS-028-clean-manual.md) | `/clean` manual removal + preview/undo (wedge) | context-wedge | done | 006, 026 |
| [AS-029](AS-029-clean-semantic.md) | `/clean "<topic>"` semantic matching (wedge) | context-wedge | done | 028, 027 |
| [AS-030](AS-030-benchmark-guardrail-suite.md) | Cost/speed benchmark suite (D5 guardrail) | quality | done | 018, 020 |
| [AS-060](AS-060-session-capture-corpus.md) | Capture & compare real vendor session files before V1 schema freeze | schema | ready | 002, 003 |
| [AS-061](AS-061-json-schema-publication.md) | Publish the block schema as JSON Schema (+ Go↔schema divergence guard) | schema | done | 003, 004, 060 |
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
| [AS-039](AS-039-init.md) | `/init` project scaffold | commands | done | 014, 022, 032 |
| [AS-040](AS-040-goal.md) | `/goal` session objective | commands | done | 006, 022 |
| [AS-041](AS-041-budget-guardrails.md) | Budget guardrails + `/budget` | cost | done | 020, 022, 031 |
| [AS-042](AS-042-model-routing.md) | Model routing/tiering + `/route` | cost | done | 008, 031, 044 |
| [AS-043](AS-043-tidy.md) | `/tidy` context reorganization (wedge) | context-wedge | done | 006, 027, 028 |
| [AS-044](AS-044-system-subagent-framework.md) | System sub-agent framework + plugin registry | subagents | done | 006, 018, 020, 031 |
| [AS-045](AS-045-insights-dashboard.md) | `/insights` session dashboard (wedge) | insights-wedge | done | 020, 022, 044 |
| [AS-046](AS-046-user-subagents.md) | User-delegated subagents | subagents | done | 013, 018 |
| [AS-047](AS-047-skill-contracts.md) | Skill expectation contracts (C.1) | living-skills | done | 034 |
| [AS-048](AS-048-rediscovered-fact-detector.md) | Rediscovered-fact detector (D7 first form) | living-skills | done | 032, 044, 047 |
| [AS-049](AS-049-skill-expectation-analyzer.md) | skill-expectation-analyzer (experimental) | living-skills | done | 044, 047, 048 |
| [AS-050](AS-050-skills-report.md) | `/skills` report + cross-session rollup | living-skills | done | 007, 022, 049 |
| [AS-051](AS-051-headless-cli.md) | Headless CLI mode | faces | done | 018, 031 |
| [AS-052](AS-052-acp-server.md) | ACP server | faces | ready | 018, 051 |
| [AS-053](AS-053-matrix-personality-layer.md) | Matrix personality layer + `/serious` | polish | done | 021, 022, 031 |
| [AS-054](AS-054-async-runner.md) | Background/async runner | async | done | 007, 041, 051 |
| [AS-055](AS-055-replayable-runs-otel.md) | Replayable runs + OpenTelemetry export | observability | done | 005, 007, 020 |
| [AS-056](AS-056-compliance-archiving-spike.md) | Spike: compliance archiving vs erasure (Q13) | compliance | done | 005 |
| [AS-057](AS-057-cross-session-analytics.md) | Cross-session analytics | insights-wedge | done | 007, 020, 045 |
| [AS-058](AS-058-self-improving-config.md) | Self-improving config | insights-wedge | done | 032, 045, 050 |
| [AS-059](AS-059-plugin-trust-spike.md) | Spike: plugin trust & sandboxing (Q12) | security | done | 044 |
| [AS-066](AS-066-command-registry-parity.md) | Shared command registry — slash ↔ subcommand parity | commands | done | 022, 065 |
| [AS-068](AS-068-clean-interactive-selection.md) | `/clean` interactive multi-select + per-item archive restore | context-wedge | done | 028, 067 |
| [AS-069](AS-069-headless-prompt-file-precedence.md) | `smith run -f <file>` shadowed by ambient stdin on a non-TTY | faces | done | 065 |
| [AS-070](AS-070-leaf-help-command-flags.md) | `smith <cmd> --help` omits command-specific flags | faces | done | 065 |
| [AS-071](AS-071-config-consumer-migration.md) | Migrate config consumers onto the layered config substrate | foundation | done | 031 |
| [AS-072](AS-072-coding-mode-shell.md) | Coding Mode shell — `/feature` & `/mode` entry/exit + phase-as-block | coding-mode | done | 006, 033, 040 |
| [AS-073](AS-073-coding-mode-phase-tracker.md) | Coding Mode phase tracker panel + presentation | coding-mode | done | 067, 072 |
| [AS-074](AS-074-coding-mode-process-skill-pack.md) | Coding Mode process skill pack (bundled, auto-enabled) | coding-mode | done | 034, 072 |
| [AS-075](AS-075-coding-mode-method-override.md) | Coding Mode project-level method override (memory files) | coding-mode | done | 032, 072 |
| [AS-076](AS-076-coding-mode-reflect-artifacts.md) | Coding Mode reflect-phase artifacts | coding-mode | done | 045, 048, 072 |
| [AS-077](AS-077-serve-jsonrpc-session-server.md) | `smith serve` — local JSON-RPC/WebSocket session server | faces | done | 018, 051, 066 |
| [AS-078](AS-078-web-gui-thin-client.md) | Web GUI — thin client over `smith serve` | faces | ready | 077 |
| [AS-079](AS-079-wasm-session-inspector.md) | WASM observability core + static session inspector | observability | ready | 005, 006, 020, 061, 038 |
| [AS-080](AS-080-hosted-agent-sandboxing-spike.md) | Spike: hosted multi-tenant live-agent sandboxing | security | done | 077, 059 |
| [AS-081](AS-081-viscose-vscode-extension.md) | Viscose (VS Code) extension over `smith serve` | faces | ready | 077, 078 |
| [AS-082](AS-082-memory-file-imports.md) | Memory file `@import`-style includes | capability | done | 032 |
| [AS-083](AS-083-mcp-resources-prompts-reconnect.md) | MCP resources, prompts, reconnect & pagination | capability | done | 036 |
| [AS-084](AS-084-rewind-file-snapshot.md) | Rewind file-system snapshot & restore | commands | done | 037 |
| [AS-085](AS-085-auto-compact.md) | Auto-compact on approaching the window limit (config-flagged, default off) | commands | done | 038, 025, 031 |
| [AS-086](AS-086-conservative-budget-enforcement.md) | Conservative budget enforcement — pre-turn estimate + unpriced-turn handling | cost | done | 041, 063 |
| [AS-087](AS-087-init-model-enrichment.md) | `/init` model-assisted draft enrichment (spun out of AS-039) | commands | done | 039 |
| [AS-088](AS-088-subagent-loop-wiring.md) | Wire the sub-agent Runner into the turn loop lifecycle (spun out of AS-044) | subagents | done | 044 |
| [AS-089](AS-089-smith-app-composition-root.md) | Shrink cmd/smith into a thin composition root | architecture | done | 065, 066 |
| [AS-090](AS-090-command-semantics-catalog.md) | Consolidate slash and subcommand semantics in one command catalog | commands | done | 066 |
| [AS-091](AS-091-interface-boundary-audit.md) | Audit interfaces and move small seams to consumer packages | architecture | done | — |
| [AS-092](AS-092-shared-streamio.md) | Extract shared stream I/O mechanics for providers and MCP | architecture | done | 010, 036, 083 |
| [AS-093](AS-093-typed-config-views.md) | Add typed consumer config views over layered config | foundation | done | 031, 071 |
| [AS-094](AS-094-filesystem-stdlib-audit.md) | Standardize filesystem traversal on fs.FS and WalkDir | architecture | done | — |
| [AS-095](AS-095-core-dependency-boundaries.md) | Enforce stdlib-first dependency boundaries for core packages | quality | done | — |
| [AS-096](AS-096-render-primitives.md) | Add tiny shared render primitives for textual reports | polish | done | — |
| [AS-097](AS-097-go126-test-idioms.md) | Modernize tests with Go 1.26 stdlib idioms | quality | done | — |
| [AS-098](AS-098-architecture-contracts.md) | Document and test package architecture contracts | architecture | done | 095 |
| [AS-099](AS-099-harness-quality-system.md) | Harness quality system design and command contract | quality | done | 095, 098 |
| [AS-100](AS-100-harness-scripts.md) | Add quick, full, architecture, and CI-local harness scripts | quality | done | 099 |
| [AS-101](AS-101-agent-hook-integration.md) | Agent and local hook integration for the harness | quality | done | 100 |
| [AS-102](AS-102-quality-skills.md) | Repository skills for quality gates and CI triage | capability | done | 099, 100 |
| [AS-103](AS-103-harness-ci-parity-guard.md) | Guard CI and local harness parity | quality | done | 100 |
| [AS-104](AS-104-shared-command-flag-contract.md) | Thread a shared flag contract through the command catalog | commands | done | 090 |
| [AS-105](AS-105-migrate-mode-flag-commands.md) | Migrate the remaining mode-flag commands onto the shared flag contract | commands | done | 104 |
| [AS-106](AS-106-fact-detector-path-config-signals.md) | Rediscovered-fact detector: path-convergence + config-key signals | living-skills | done | 048 |
| [AS-107](AS-107-subagent-runner-composition.md) | Wire a sub-agent Runner into the composition root (register built-ins + install WithSubAgents) | subagents | done | 088, 048 |
| [AS-108](AS-108-subagent-ledger-resolver.md) | Persist the rediscovered-fact ledger + memory/skill-aware save-target resolver | subagents | done | 107, 032, 034 |
| [AS-109](AS-109-insights-model-layer.md) | `/insights` model-assisted layer + goal anchoring (spun out of AS-045) | insights-wedge | done | 045, 040, 042 |
| [AS-110](AS-110-route-escalation-overrides.md) | Model routing escalation + per-session `/route` overrides (spun out of AS-042) | cost | done | 042 |
| [AS-111](AS-111-scoped-plugin-context-slices.md) | Scope-gated context slices for third-party sub-agents (spun out of AS-059) | security | ready | 044 |
| [AS-112](AS-112-declarative-boundary-guard.md) | Guard the declarative-only plugin boundary with a test + archtest (spun out of AS-059) | quality | done | 044, 098 |
| [AS-113](AS-113-plugin-consent-screen.md) | Plugin consent screen + scope→sentence table (spun out of AS-059) | security | needs-clarification | 044 |
| [AS-114](AS-114-phase-skill-projection-scope.md) | Scope Coding Mode process-skill blocks to the active phase (spun out of AS-074) | coding-mode | done | 074, 006 |
| [AS-115](AS-115-redaction-at-capture.md) | Redaction-at-capture — best-effort secret/PII scrub before the log (spun out of AS-056) | compliance | done | 005, 016 |
| [AS-116](AS-116-escalation-visibility-wiring.md) | Surface auto-escalation in `/route` and `/cost` + wire the first producer (spun out of AS-110) | cost | done | 110 |
| [AS-117](AS-117-tidy-dead-end-and-working-memory.md) | `/tidy` dead-end collapse + working-memory promotion (spun out of AS-043) | context-wedge | ready | 043, 048 |
| [AS-118](AS-118-json-root-help-output.md) | Root help ignores `--output json` | faces | done | 065, 070 |
| [AS-119](AS-119-task-faces-and-tool-inheritance.md) | `task` delegation across faces + child tool inheritance (spun out of AS-046) | subagents | done | 046, 051, 077 |
| [AS-120](AS-120-task-cost-itemization-and-budget.md) | `task` per-child cost itemization, prompt attribution, budget (spun out of AS-046) | cost | done | 046, 020, 041 |
| [AS-132](AS-132-async-runner-daemon-concurrency.md) | Background runner daemon + worker concurrency (spun out of AS-054) | async | done | 054 |
| [AS-136](AS-136-stats-index-and-cross-project-friction.md) | Persisted cross-session stats index + cross-project friction merge (spun out of AS-057) | insights-wedge | done | 050, 057 |
| [AS-137](AS-137-insights-on-demand-model-retro.md) | `/insights` on-demand model retro when the writer is disabled (spun out of AS-109) | insights-wedge | done | 109 |
| [AS-138](AS-138-improve-confidence-and-efficacy.md) | `/improve` high-confidence single-fact threshold (spun out of AS-058) | insights-wedge | done | 058, 048, 057 |
| [AS-139](AS-139-improve-efficacy-measurement.md) | `/improve` proposal efficacy measurement (before/after friction delta) (spun out of AS-138) | insights-wedge | done | 058, 057, 136 |

## Index — Recorded-provider regression harness (AS-133 … AS-135)

| ID | Title | Area | Status | Depends on |
|---|---|---|---|---|
| [AS-133](AS-133-recorded-vendor-simulators.md) | Recorded vendor simulators for Anthropic, OpenAI, and compatible providers | provider | done | 008, 009, 010, 012, 060, 135 |
| [AS-134](AS-134-offline-e2e-regression-suite.md) | Offline E2E regression suite over recorded providers, TUI, and append-only logs | quality | done | 005, 018, 021, 024, 046, 119, 120, 133 |
| [AS-135](AS-135-capture-to-fixture-workflow.md) | Capture-to-fixture workflow for redacted vendor sessions and CI-safe regressions | schema | done | 056, 060, 115 |

## Index — TUI visual polish (AS-121 … AS-131) — demo-priority

| ID | Title | Area | Status | Depends on |
|---|---|---|---|---|
| [AS-121](AS-121-tui-phosphor-palette.md) | TUI phosphor palette — centralize colour tokens and apply to all surfaces | tui | done | 021, 053 |
| [AS-122](AS-122-tui-splash-screen.md) | TUI splash screen — logo, divider rule, invite text, blinking caret | tui | done | 121, 126 |
| [AS-123](AS-123-tui-typewriter-streaming.md) | TUI typewriter streaming — char-by-char reveal with trailing block cursor | tui | done | 121, 021 |
| [AS-124](AS-124-tui-tool-card-polish.md) | TUI tool card visual polish — bordered cards, left rule, truncation, elapsed time | tui | ready | 121, 024 |
| [AS-125](AS-125-tui-status-line-polish.md) | TUI status line + mode bar visual polish — spec-compliant layout and colours | tui | done | 121, 025, 073 |
| [AS-126](AS-126-tui-matrix-rain-medium-default.md) | TUI Matrix rain — medium intensity default, animated falling chars, /serious disables | tui | done | 121, 053 |
| [AS-127](AS-127-tui-command-palette-visual.md) | TUI command palette visual redesign — search border, per-command styling, footer hints | tui | done | 121, 022 |
| [AS-128](AS-128-tui-context-panel-visual.md) | TUI /context panel visual redesign — segmented bar, amber auto-compact marker, stats rail | tui | ready | 121, 026 |
| [AS-129](AS-129-tui-permission-diff-visual.md) | TUI permission gate visual redesign — diff colours, dimmed context, option list | tui | ready | 121, 024 |
| [AS-130](AS-130-tui-agents-panel.md) | TUI /agents orchestrator panel — tree view, state dots, pulsing animation | tui | ready | 121, 044, 067 |
| [AS-131](AS-131-tui-insights-panel-visual.md) | TUI /insights panel visual redesign — stat cards, timeline, tool histogram | tui | ready | 121, 045 |

## Index — QA / test infrastructure follow-ons

| ID | Title | Area | Status | Depends on |
|---|---|---|---|---|
| [AS-140](AS-140-campaign-missing-scenarios.md) | Manual test campaign — add detailed scenarios for newly-completed tickets | quality | done | AS-029, AS-043, AS-054, AS-057, AS-110, AS-119, AS-120, AS-132, AS-133, AS-135, AS-136, AS-138 |
| [AS-141](AS-141-archtest-serve-face-layering.md) | Archtest: add `internal/serve` to faces forbidden list in layering contracts | quality | done | AS-098 |
| [AS-142](AS-142-archtest-conformance-schema-guard.md) | Archtest: add layering guard for `internal/provider/conformance` and `schema` | quality | done | AS-098, AS-141 |
| [AS-144](AS-144-keychain-unreachable-error-classification.md) | `auth set/status` leaks a raw dbus error instead of the actionable env-var hint when no Secret Service is reachable | faces | done | AS-017 |
| [AS-145](AS-145-archtest-loop-cmd-and-face-cross-imports.md) | Archtest: guard loop↛cmd and face↛face/cmd, the documented-but-unenforced layering rules | quality | done | AS-098, AS-141 |
| [AS-146](AS-146-archtest-inward-core-no-orchestration.md) | Archtest: guard that inward-core packages do not import orchestration packages | quality | done | AS-098 |

## Index — Architecture documentation follow-ons

| ID | Title | Area | Status | Depends on |
|---|---|---|---|---|
| [AS-143](AS-143-serve-runtime-flow-diagram.md) | Add `smith serve` JSON-RPC/WebSocket runtime flow diagram to runtime-flows.md | architecture | done | AS-077 |
| [AS-162](AS-162-archtest-package-contracts-completeness.md) | Guard that every internal package is accounted for in package-contracts.md | quality | done | AS-098, AS-146 |

## Index — Orchestrator dogfood wave (AS-159 … AS-161, AS-147 … AS-158)

The always-on deterministic workflow engine from
[smith-orchestrator-dogfood-prd.md](../smith-orchestrator-dogfood-prd.md);
architecture fixed by the [orchestrator ADR](../../architecture/orchestrator-architecture.md).
(Renumbered: the first three items are AS-159/160/161 because AS-144/145/146 were already taken.)
AS-163 carves the job-spec model + validator out of AS-161 so the daemon builds on a stable, tested target.

| ID | Title | Area | Status | Depends on |
|---|---|---|---|---|
| [AS-159](AS-159-orchestrator-architecture-boundaries.md) | Orchestrator architecture and product boundaries (ADR) | orchestrator | done | — |
| [AS-160](AS-160-job-spec-workflow-dsl.md) | Job specification and workflow DSL | orchestrator | done | AS-159 |
| [AS-161](AS-161-daemon-scheduler-sqlite-run-store.md) | Daemon, scheduler, and SQLite run store | orchestrator | done | AS-159, AS-160, AS-163 |
| [AS-163](AS-163-job-spec-model-validator.md) | Orchestrator job-spec model + validator | orchestrator | done | AS-159, AS-160 |
| [AS-147](AS-147-github-events-deterministic-hooks.md) | GitHub event ingestion and deterministic hooks | orchestrator | ready | AS-160, AS-161 |

| [AS-148](AS-148-github-authentication-strategy.md) | GitHub authentication strategy | orchestrator | ready | AS-159, AS-147 |
| [AS-149](AS-149-pr-lifecycle-automation.md) | PR lifecycle automation | orchestrator | ready | AS-147, AS-148 |
| [AS-150](AS-150-multi-provider-workflow-routing.md) | Multi-provider workflow routing | orchestrator | ready | AS-160 |
| [AS-151](AS-151-orchestrated-run-event-log-integration.md) | Smith event-log integration for orchestrated runs | orchestrator | ready | AS-161 |
| [AS-152](AS-152-smith-implements-smith-workflows.md) | Smith implements Smith dogfood workflow pack | orchestrator | ready | AS-160, AS-161, AS-147, AS-149, AS-150, AS-151, AS-157 |
| [AS-153](AS-153-sandbox-abstraction-execution-environments.md) | Sandbox abstraction and execution environments | orchestrator | ready | AS-159, AS-161, AS-158 |
| [AS-154](AS-154-secret-management-redaction-contract.md) | Secret management and redaction contract | orchestrator | ready | AS-159, AS-148, AS-158 |
| [AS-155](AS-155-operator-api-ui.md) | Operator API/UI | orchestrator | ready | AS-161, AS-151 |
| [AS-156](AS-156-private-vpc-deployment.md) | Private VPC deployment | orchestrator | ready | AS-161, AS-148, AS-154 |
| [AS-157](AS-157-auto-merge-policies-safety-gates.md) | Auto-merge policies and safety gates | orchestrator | ready | AS-147, AS-148, AS-149 |
| [AS-158](AS-158-agent-workflow-sandbox-secrets-research.md) | Competitive agent workflow, sandbox, and secrets research spike | orchestrator | done | AS-159 |

## Index — Orchestrator dogfood wave (`smith-orchestrator-dogfood-prd.md`)

> The architecture/DSL/daemon tickets were renumbered from the colliding AS-144–AS-146 (already owned by the merged keychain/archtest tickets above) to AS-159–AS-161. The AS-159 architecture ADR has landed (Accepted), so the wave below is clarified; see the index above for current status (most of AS-147…AS-162 are now `ready`, gated only on build order, not open design questions; AS-113 remains the sole product-decision blocker).

| ID | Title | Area | Status | Depends on |
|---|---|---|---|---|
| [AS-159](AS-159-orchestrator-architecture-boundaries.md) | Orchestrator architecture and product boundaries | orchestrator | done | — |
| [AS-160](AS-160-job-spec-workflow-dsl.md) | Job specification and workflow DSL | orchestrator | done | AS-159 |
| [AS-161](AS-161-daemon-scheduler-sqlite-run-store.md) | Daemon, scheduler, and SQLite run store | orchestrator | done | AS-159, AS-160, AS-163 |
| [AS-163](AS-163-job-spec-model-validator.md) | Orchestrator job-spec model + validator | orchestrator | done | AS-159, AS-160 |

## Index — Desktop app wave (AS-168 … AS-175)

Single packaged desktop app on Wails, with a strict internal adapter boundary
over the Smith core, per [smith-desktop-wails-prd.md](../smith-desktop-wails-prd.md).
This wave aims for a simple, local-first interactive desktop product before any
broader workboard or IDE-style expansion.

| ID | Title | Area | Status | Depends on |
|---|---|---|---|---|
| [AS-168](AS-168-wails-desktop-shell-bootstrap.md) | Wails desktop shell bootstrap over Smith core adapter | faces | ready-to-implement | AS-077 |
| [AS-169](AS-169-desktop-managed-runtime-lifecycle.md) | Desktop embedded runtime lifecycle and app state | faces | ready-to-implement | AS-168, AS-077 |
| [AS-170](AS-170-desktop-interactive-transcript-and-composer.md) | Desktop interactive transcript and composer | faces | ready-to-implement | AS-168, AS-169 |
| [AS-171](AS-171-desktop-tool-activity-and-permission-rail.md) | Desktop tool activity and permission rail | faces | ready-to-implement | AS-170, AS-024, AS-016 |
| [AS-172](AS-172-desktop-home-workspaces-and-session-resume.md) | Desktop home, recent workspaces, and session resume | faces | ready-to-implement | AS-170, AS-007, AS-064 |
| [AS-173](AS-173-desktop-context-and-cost-rail.md) | Desktop context and cost rail | faces | ready-to-implement | AS-170, AS-025, AS-020, AS-063 |
| [AS-174](AS-174-desktop-settings-runtime-status-and-auth-guidance.md) | Desktop settings, runtime status, and auth guidance | faces | ready-to-implement | AS-169, AS-170, AS-017 |
| [AS-175](AS-175-desktop-packaging-signing-updates-and-smoke-tests.md) | Wails desktop packaging, signing, updates, and smoke tests | quality | ready-to-implement | AS-168, AS-169, AS-170 |

## Index — PM discovery backlog (Pending Debrief)

These items come from competitor/product research and are intentionally marked `Pending Debrief` until the team reviews scope, priority, and sequencing.

| ID | Title | Area | Status | Depends on |
|---|---|---|---|---|
| [AS-164](AS-164-run-verify-skill-generator.md) | Run/verify skill generator | skills | ready-to-implement | AS-034, AS-035, AS-058, AS-099 |
| [AS-165](AS-165-background-cost-ledger.md) | Background cost ledger and autonomous activity attribution | cost | ready-to-implement | AS-020, AS-041, AS-054, AS-120, AS-132 |
| [AS-166](AS-166-shareable-session-bundles.md) | Shareable redacted session bundles | collaboration | ready-to-implement | AS-005, AS-055, AS-079, AS-115, AS-154 |
| [AS-167](AS-167-command-surface-simplification.md) | Command surface simplification and progressive disclosure audit | commands | Pending Debrief | AS-022, AS-066, AS-090, AS-104, AS-105 |

## PM discovery PRDs (Pending Debrief)

| PRD | Status | Primary opportunity |
|---|---|---|
| [Project Intelligence Map](../project-intelligence-map-prd.md) | Pending Debrief | Inspectable repo map with citations, freshness, and cheap context slices |
| [Local Agent Workboard](../agent-workboard-prd.md) | Pending Debrief | Local-first board for parallel/background Smith tasks across worktrees and daemon jobs |

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
10. **GUI wave** (graphical faces, after 051): 077 (`smith serve`, the JSON-RPC/WS spine — the Q5 JSON-RPC-first fallback that ACP later re-skins) → 078 (web thin client) and 081 (Viscose extension, once its wiring question is answered). 079 (WASM read-only session inspector) is largely independent — it needs the substrate (005/006/020/061) plus 038 for the `/compact` preview view, but none of the GUI-wave serve/client work — and is the genuine WASM payoff plus the safe public demo. 080 (hosted multi-tenant sandboxing) is **resolved** ([docs/design/hosted-agent-sandboxing.md](../../design/hosted-agent-sandboxing.md)): closed in favour of 079 — `smith serve` ships for local use only and the public demo is the read-only inspector, since hosting strangers collides with D9 ("not a sandbox").
11. **Harness quality system**: 099 documents the shared contract, then 100 adds scripts, 101 wires agent/local hooks, and 102/103 add skills and CI-local parity guards. This sequence can run alongside feature work because it reduces round trips for all later tickets.
12. **Recorded-provider regression harness**: 135 defines the safe capture-to-fixture workflow, 133 builds the fake vendor servers over AS-060 captures, and 134 promotes those fixtures into offline E2E coverage for the loop, TUI, subagents, cost, and append-only JSONL.
13. **Orchestrator dogfood wave** (always-on deterministic workflow engine; ADR AS-159): 159 (architecture/boundaries, done) → 160 (job-spec DSL, done) / 161 (daemon + SQLite run store, done), with 158 (research spike, done — [research notes](../../research/orchestrator-competitive-research.md)) feeding 148/153/154/156/157. The GitHub, routing, event-log, sandbox, secrets, operator, deployment, and auto-merge tickets (147–157, plus 162) are now `ready`, clarified against the landed ADR and research spike; build them in roughly their dependency order (147/150/151 first, then 148/149, then 153/154/156/157, then 155, with 152 last as it composes the rest).
14. **PM discovery backlog**: debrief the competitor-driven PRDs and tickets before implementation. AS-164…AS-166 have been accepted into `ready-to-implement`; the remaining open PM work is to split the Project Intelligence Map and Local Agent Workboard PRDs into numbered implementation tickets and debrief AS-167 before deciding which command families to collapse.

## Needs clarification — decisions to make

Previously listed `needs-clarification` tickets were triaged against the current
repo documentation and clarified where the documented direction was sufficient.
A 2026-06-30 QA pass re-triaged the remaining set, including the orchestrator
dogfood wave (AS-147…AS-157, AS-162) and AS-117, against the now-landed AS-159
ADR, the AS-158 research spike, and the AS-043/AS-048 implementations — all of
those moved to `ready-to-implement`, with the resolution recorded in each
ticket's "Clarification" section.

One open item remains: **AS-113** (plugin consent screen), which has nothing to
hang a consent flow on until a plugin-install/marketplace path exists (§7.26,
not yet ticketed) — confirmed still blocked against `docs/design/plugin-trust.md`
§8, which explicitly defers AS-113 until that path is filed. New bug follow-ons
should stay `ready-to-implement` unless they truly need a product decision. New
ambiguous follow-on work should be captured by adding a fresh ticket with a
focused Open questions section.

The clarified decisions are recorded in the individual ticket files rather than
repeated here, so the ticket remains the source of truth for implementation.

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
- Labels applied: `status`, `area:<area>`, optional `type:<type>`, and `priority` (created on the repo if missing).
- Auth via the `gh` CLI (`gh auth login`). Repo resolution: `-repo owner/name` flag → `TICKET_SYNC_REPO` env var → the current git remote.
- After a pull request is merged, the **Sync merged tickets** GitHub Actions workflow finds ticket files changed by that PR and runs `go run ./cmd/ticket-sync -require-existing` against them. This keeps related issues current and closes `done` tickets, while failing on `github_issue: null` so new tickets are linked before merge instead of creating uncommitted issue-number changes in CI.
