# Agent Smith manual test campaign

_Last updated: 2026-06-26 (AS-140: added dedicated detailed scenario steps for AS-029, AS-043, AS-054/AS-132, AS-057/AS-136, AS-110, AS-119/AS-120, AS-133/AS-135, AS-138, and AS-121 that previously only appeared in the coverage matrix or as composite footnotes)._

_Earlier: 2026-06-25 (QA pass: corrected stale not-implemented entries, added coverage for AS-029, AS-043, AS-054, AS-057, AS-061, AS-110, AS-119, AS-120, AS-133, AS-135, AS-136, AS-138; added TUI visual polish wave AS-121–AS-131 and AS-139 to coverage matrix)._

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
| AS-060 | Manual capture | Follow section "AS-060 vendor session captures": grab a handful of redacted session artifacts (offline ones first, then one cheap live turn per vendor), run each through the `schema` types, record dispositions. | Each surface has at least one redacted capture round-tripped through AS-003; fields-without-a-home are classified promote / `ext` / out-of-scope; proposed additive deltas linked to AS-003. No CI keys needed. |
| AS-017, AS-050, AS-054, AS-057–AS-058, AS-061, AS-077, AS-110, AS-119–AS-120, AS-132–AS-133, AS-135–AS-136, AS-138 | Implemented | See detailed sections 3.4b, 3.5–3.7, 5.7/5.7a, 8.0/8.0a, 8.2a–8.2d, 8.5, 9.3; run the CLI subcommands (`smith auth`, `smith stats`, `smith stats all`, `smith stats rebuild`, `smith improve`, `smith runs`). | Auth, stats, background runner, self-improving config, block-schema JSON, task delegation, vendor simulators, and route escalation are all implemented; see per-feature sections for details. |
| AS-029, AS-043, AS-121 | Implemented | See detailed steps 5.3a (`/clean "<topic>"`), 5.8 (`/tidy`), and 6.9 (phosphor palette). | Semantic clean previews/excludes only matching segments; `/tidy` dedups reversibly; all TUI surfaces share the phosphor palette. |
| AS-052, AS-078–AS-079, AS-081, AS-111, AS-123, AS-127–AS-131 | Not implemented | Check README/help/tickets only. | Feature is ticketed but not yet implemented; no manual pass/fail expected. |
| AS-087 | Implemented | In a repo with a README, run `/init --describe` (or `smith init --describe`) with a provider configured, review the preview, then `/init --apply`. Compare against plain `/init`. | `--describe` adds model-authored prose sections (e.g. `## Overview`) after the deterministic Build & test / Layout sections; the commands are never restated or replaced; plain `/init` stays deterministic and prose-free; nothing is written until `--apply`. |
| AS-137 | Implemented | With `subagents.insights_writer.model` **unset**, run a session with tool use, then `/insights` (and `smith insights describe`). The dashboard offers the on-demand retro; running `describe` adds grounded `(model)` suggestions and the spend shows in `/cost`. Set a tiny `/budget` first to see it skip with "Budget reached". | Base `/insights` stays free and offers the retro only when the model layer is off; `describe` merges only evidence-citing suggestions, charges the session budget, and is skipped (no model call) with no budget room. |
| AS-080, AS-124, AS-139 | Implemented | Spike doc (AS-080); TUI tool-card polish under §6 (AS-124); `/improve` efficacy via `smith stats` per step 8.2c (AS-139). | Spike shipped as a design doc; tool cards show borders/left rule/truncation/elapsed time; applied-remedy before/after friction delta is computed in `internal/skillrollup` and surfaced in `smith stats`. |
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

Covers AS-002 through AS-007, AS-027, AS-037, AS-038, AS-055, AS-056, AS-084, AS-115.

| Step | Action | Expected result |
| --- | --- | --- |
| 2.1 | In a disposable project, start `./smith tui` or `./smith`, send a simple prompt, and exit. If no provider credentials are available, create a session through the smallest offline command that writes a log. | A project-scoped session directory appears with an event log and debug log. |
| 2.2 | Inspect the event log file before and after `/clear`, `/rewind`, `/compact`, or `/clean --apply`. | History is append-only; edits append exclusion, checkpoint, restore, or derived events instead of mutating earlier blocks. |
| 2.3 | Run `./smith session list` and resume the session. | The session appears for the current project and transcript/context are rehydrated. |
| 2.4 | Add prompts containing fake secrets/PII such as `sk-test-secret` or an email address and inspect captured log text. | Best-effort redaction occurs before capture where AS-115 applies; debug logs do not reveal more than intended. |
| 2.5 | Exercise `/compact` preview/apply/undo and `/rewind` checkpoint/restore. | Derived compact blocks and restore events are visible and reversible. |
| 2.6 | After file edits past a checkpoint, run `/rewind <handle> --restore-files` (AS-084). Repeat after hand-editing a Smith-written file outside the session. | Restored/deleted files match the checkpoint; externally-changed files are flagged as conflicts and never overwritten; the log still only grows. |
| 2.7 | Run `./smith replay <session>` for a stored session (AS-055), then `./smith replay <session> --output json`. | A manifest header (models, tools, token/cost totals) plus the transcript renders offline with no API keys; JSON mode emits `{manifest, blocks}`. No provider/tool runs (re-display, not re-execution). A `manifest.json` appears next to the log. |
| 2.8 | Set `telemetry.otel_endpoint` to a local OTLP/HTTP collector and run `./smith replay <session> --otel` (or a headless `smith run`). | `session → turn → model.call/tool.call` spans with token/cost attributes arrive at the collector. With no endpoint configured (default), nothing is exported and no network call is made. |

### 2A. AS-060 vendor session captures (manual, one-time, pre-V1 schema validation)

Covers AS-060. This is a **one-time capture pass**, not a per-build CI test — it needs no keys in CI. Do the offline rows first; they cover most surfaces with zero API spend. Only steps marked _(live key)_ need a vendor credential, and each is a single cheap turn done by hand. Redact secrets/PII before saving anything (the AS-115 scrub helps; review by eye too). Drop captures under `docs/design/captures/` (or reference them if too large/sensitive to commit).

For every capture: parse it into the `schema` block types, re-emit, and record whether the round-trip is lossless (block-schema-union §14 checklist). Log each field with no home except `ext`, and mark it **promote to first-class optional**, **leave in `ext`**, or **out-of-scope** (with reason).

| Step | Action | Expected result |
| --- | --- | --- |
| A.1 | **Claude Code L3 (offline).** Copy a real `~/.claude/projects/<proj>/<sid>.jsonl`, ideally one that spawned a sub-agent (`isSidechain`) and loaded a skill/MCP (`attribution*`). Redact paths/prompts. Run through the `schema` types. | Round-trip lossless or gaps logged; sub-agent/thread links and per-iteration usage are representable or flagged. |
| A.2 | **Codex CLI L3 (offline).** Copy a `$CODEX_HOME/sessions/.../rollout-*.jsonl`. Redact, round-trip. | Disposition recorded for every field; no Codex-specific field silently dropped. |
| A.3 | **Gemini CLI L2+L3 (offline).** Capture a `gemini --output-format json` result (with `stats`) and a `~/.gemini/history/<proj>/` log. Redact, round-trip. | `stats`/usage fields mapped or classified. |
| A.4 | **Anthropic L1** _(live key)_. One `smith run` (or raw `curl`) against an Anthropic model that emits thinking + a tool call/result; capture the Messages turn incl. cache fields. Redact, round-trip. | thinking/tool/cache fields map to `reasoning`/`tool_call`/`tool_result`/usage or are flagged. |
| A.5 | **OpenAI L1** _(live key)_. One Responses API turn with a typed `output[]` incl. a `reasoning` item, plus one Chat Completions turn (the compat projection). Redact, round-trip. | Both representation layers round-trip or log gaps. |
| A.6 | **xAI/Grok L1** _(live key)_. One OpenAI-compatible turn; capture `reasoning_content` and any Live Search citations. Redact, round-trip. | `reasoning_content`/citation fields classified. |
| A.7 | **Roll up.** Annex a comparison report (extend `docs/design/block-schema-union.md` or a sibling `*-validation.md`) and list proposed **additive** deltas; link them from AS-003 so they land before the V1 freeze. Any change that cannot be expressed additively is flagged loudly as a pre-V1 breaking change to make now. | Report + delta list exist and are linked from AS-003; redaction confirmed on every committed/referenced artifact. |

### 3. Providers, prompt caching, and provider conformance

Covers AS-008 through AS-012, AS-092, AS-133, AS-135.

| Step | Action | Expected result |
| --- | --- | --- |
| 3.1 | With `ANTHROPIC_API_KEY`, run a one-turn `smith run` against an Anthropic model. | Streaming text arrives; usage metadata and provider errors are normalized. |
| 3.2 | With `OPENAI_API_KEY` or OpenAI-compatible endpoint config, run the same one-turn task. | The same face-level events appear despite provider differences. |
| 3.3 | Repeat a prompt with cache-eligible context. | Prompt-cache usage/savings appear in `/cost` when the provider reports them. |
| 3.4 | Run provider conformance tests from the quality gate. | Recorded fixtures pass offline; no live network is needed for default tests. |
| 3.4a | Run the offline E2E suite: `go test ./internal/e2e/...` (also covered by `make test`). | Scripted whole-session scenarios (large tool payloads, parallel calls, denied-permission recovery, two-child delegation, resume) drive the recorded vendor simulators and assert transcript, UIEvent stream, cost/ledger, and append-only JSONL — deterministic, no keys, no network. (AS-133/AS-134; see [offline-e2e-suite.md](../testing/offline-e2e-suite.md)) |
| 3.4b | Read the capture-to-fixture workflow ([offline-e2e-suite.md](../testing/offline-e2e-suite.md) and AS-135), then run `go test ./internal/e2e/...`. | The redacted capture-to-fixture workflow (AS-135) produces the recorded vendor simulators (AS-133) that drive the offline E2E suite deterministically — no API keys, no live network. |
| 3.5 | `smith auth set anthropic` (paste a key at the hidden prompt), then `smith auth status`. | Key stored in the OS keychain, never a plaintext file; status shows `set (keychain)` without revealing the value. (AS-017) |
| 3.6 | Export `ANTHROPIC_API_KEY` and re-run `smith auth status anthropic`. | Reports `set (env ANTHROPIC_API_KEY)` — the env var overrides the stored key. (AS-017) |
| 3.7 | On a host with no Secret Service running, `smith auth set openai`. | Fails with an actionable error pointing at `OPENAI_API_KEY`; no plaintext file is written. (AS-017) — **Fixed (AS-144):** the credential store now classifies Secret-Service-unreachable failures (missing `dbus-launch`, D-Bus refused, `org.freedesktop.secrets` not provided) as `ErrUnavailable`, so `auth set` shows the `OPENAI_API_KEY` hint and `auth status` shows the `no keychain available` line instead of a raw dbus error. |

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

Covers AS-020, AS-025 through AS-029, AS-041, AS-042, AS-043, AS-063, AS-068, AS-085, AS-086, AS-110.

| Step | Action | Expected result |
| --- | --- | --- |
| 5.1 | After several turns and at least one tool result, run `/cost` and `./smith cost`. | Per-turn input/output/cache token counts and dollar totals appear; unknown model pricing is marked unknown rather than guessed. |
| 5.2 | Open `/context` and sort by size, age, and type. | The composition view shows segment handles, token shares, dollar estimates, type rollups, duplicate reads, stale candidates, and excluded blocks. |
| 5.3 | Run `/clean <handle>` and inspect the preview; then `/clean --apply`, `/context`, and `/clean --undo`. | Preview is non-mutating; apply excludes the selected segment/tool pair; undo restores it by appending a reversal. |
| 5.3a | After several distinct topics have accumulated, run `/clean "<topic>"` with a natural-language topic (e.g. `/clean "the dependency upgrade discussion"`); inspect the preview, then `/clean --apply` and `/context`. | Semantic matching previews only the segments whose topic labels match the phrase; non-matching segments are untouched; apply excludes only the matched segments and `/context` reflects the removal. (AS-029) |
| 5.4 | Use interactive `/clean` multi-select from the context panel. | Multi-select removal and per-item archive restore work from the panel. |
| 5.5 | Configure a very small budget and run a prompt. | Conservative pre-turn budget enforcement blocks or warns before an unpriced/over-budget turn. |
| 5.6 | Configure auto-compact off and near-limit context. | Auto-compact does not surprise the user unless explicitly enabled. |
| 5.7 | Use `/route` and configured model tiers; then run `/route <feature> <tier>` or `/route <tier> <vendor> <model>` to set a per-session override, and run `/clear` to confirm it resets. | Current routing policy is visible. Per-session overrides layer over the config policy for the rest of the session and reset on `/clear`; auto-escalation on structured low-confidence results is logged with a reason and visible in `/route` and `/cost`. (AS-110) |
| 5.7a | Run the concrete per-session override `/route cheap anthropic claude-haiku-4-5`, open `/route` to confirm it is active, then `/clear` and reopen `/route`. | The override shows as the active `cheap`-tier mapping for the rest of the session (config file unchanged) and reverts to the config policy after `/clear`. (AS-110) |
| 5.8 | After the session has read the same file two or more times, run `/tidy`; inspect the preview, `/tidy --apply`, then `/tidy --undo`. | `/tidy` dedups the repeated file reads; the preview shows the token delta without mutating; apply collapses the duplicates and undo reverses it by appending (lossless, no summarization). (AS-043) |

### 6. TUI, commands, custom commands, Matrix layer, and Coding Mode

Covers AS-021 through AS-023, AS-033, AS-039, AS-040, AS-053, AS-064, AS-067, AS-072 through AS-076, AS-114, AS-121, AS-122, AS-123, AS-126.

| Step | Action | Expected result |
| --- | --- | --- |
| 6.1 | Launch `./smith tui` in a real terminal and type `/`. | Command palette opens; focus/hotkeys route among input, transcript, and inspect panels. |
| 6.2 | Run `/clear`, `/model`, `/resume`, `/goal`, `/init`, `/serious`, `/feature`, and `/mode` where applicable. | Each command has visible feedback and updates only its intended session state. |
| 6.3 | Add `.agent-smith/commands/review.md` with frontmatter and `$ARGUMENTS`; reopen the palette. | Custom command appears with description/argument hint; built-ins still win name conflicts. |
| 6.4 | Enter Coding Mode with `/feature`; advance through phases. | Phase tracker panel and phase-as-block state are visible; process skill blocks are scoped to the active phase. |
| 6.5 | Add a ```` ```smith-method ```` block (`phases: think, plan, implement, verify`) to the project `CLAUDE.md`/`AGENTS.md`, start a session, and `/feature`. | The phase tracker reflects the overridden phase sequence (reordered/skipped); a malformed block degrades to the default (AS-075). |
| 6.6 | In Coding Mode, `/phase reflect`, then ask Smith to wrap up the feature (AS-076). | The reflect-artifacts skill drives three artifacts: a measurable success metric (citing the signal to read), an instrumentation proposal as a diff, and a check-back ticket draft (house `AS-NNN` format in this repo, markdown elsewhere — a draft only, no `cmd/ticket-sync`/remote issue). Smith never claims to read shipped-app runtime data. |
| 6.7 | Resume a prior session through the picker. | Picker lists sessions and restored transcript is readable. |
| 6.9 | Launch `./smith tui`, send a message, and watch the assistant reply land (AS-123). | The reply types in letter-by-letter (~40 ms/char) with a green `█` block cursor trailing the last character; a large token burst still reveals at the steady cadence rather than flashing all at once; the cursor disappears the instant the turn ends and finished text stays static. `--no-splash`/headless modes are unaffected. |
| 6.8 | Launch `./smith tui` and look at the empty startup screen, then type a character and clear it (AS-122). | Logo `▞▞ AGENT SMITH`, a full-width underrule, and the `path · model · mode` context line render; the invite headline and command-hint row show below; at default (medium) Matrix intensity the rain renders behind the copy and the idle phrase replaces the hint after ~3s; the `┃` gutter caret blinks while empty and goes solid the instant you type. `--no-splash` shows nothing above the input bar. |
| 6.9 | Launch `./smith tui` and compare colours across surfaces: input bar, transcript, status line, inspect panels, and the command palette. | Every surface draws from the centralized phosphor colour tokens — a consistent green-on-black phosphor palette with no ad-hoc per-surface colours. (AS-121) |

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

Covers AS-044, AS-045, AS-046, AS-050, AS-054, AS-056, AS-057, AS-059, AS-088, AS-107, AS-108, AS-112, AS-113, AS-119, AS-120, AS-132, AS-136, AS-138, AS-139.

| Step | Action | Expected result |
| --- | --- | --- |
| 8.0 | In an interactive session, prompt the model to use the `task` tool to delegate a self-contained subtask (or two in parallel); also run `smith run "…" --queue` to enqueue a headless task and `smith runs work` to drain it. | The child runs in its own session (visible via `/resume` as a `task: …` session linked to the parent); its summary returns into the parent context attributed to `task`; the child's spend is included in `/cost`. Child tool calls prompt through the same permission gate. Headless/serve task delegation (AS-119) and per-child cost itemization (AS-120) are implemented. |
| 8.0a | After draining a delegated/headless task (`smith run "…" --queue` + `smith runs work`, or an in-session `task` delegation), run `smith cost` / `/cost`. | Each child run is itemized as its own line with prompt attribution and per-child token/dollar spend, and the parent totals include the children (AS-119/AS-120). |
| 8.1 | Run a session that should invoke built-in subagent/living-skill lifecycle hooks. | Built-in system subagents are registered by the composition root and run through the turn lifecycle. |
| 8.2 | Run `/insights` after a session with a goal, tool use, and context churn. | Dashboard summarizes cost/context/session findings and reports whether the `/goal` objective was met (AS-109 goal anchoring: in-progress while live, met once `/goal done` retires it), grounded in measured signals. The model-assisted suggestion layer is opt-in via `subagents.insights_writer.model` (cheap-tier, budget-capped, session-end); when that layer is off, `/insights` offers a one-time on-demand model retro via `/insights describe` (AS-137), which adds grounded `(model)` suggestions and charges the session budget. |
| 8.2a | Across several sessions in the same project, rediscover the same fact (and/or enable the skill-expectation analyzer), then run `/skills`. | The per-session findings list plus a cross-session rollup render; a fact seen in 3+ sessions is flagged escalated; `/skills apply <n>` lands the remedy's diff into its target file and marks the finding resolved (it stops pending and survives a restart). |
| 8.2b | After a few sessions with cost/tool activity in the project, run `/stats` (and `smith stats`); then `smith stats all` across more than one project; then `smith stats rebuild` to verify the index refreshes. | Cross-session analytics render offline: spend total, per-model and (with `all`) per-project breakdowns, a per-day spend trend, the top-3 grounded "ways to save" (each citing a measured number), and recurring friction linked to example session ids. `smith stats all` widens the spend view to every project. `smith stats rebuild` forces an index refresh. Persisted index/rebuild (AS-136) is implemented. With sessions in more than one project, the cross-project friction merge renders recurring friction lines grouped per project after `smith stats rebuild` (AS-057/AS-136). |
| 8.2c | Across ≥2 sessions in the same project, rediscover the same fact carrying a remedy, then run `/improve` (and `smith improve`). | The pending self-improving-config queue renders: one consolidated proposal per gap seen in ≥2 distinct sessions, each with target file, proposed edit, and cross-session evidence. `/improve apply <n>` lands the edit through a shown diff and marks it resolved; `/improve dismiss <n>` / `/improve snooze <n>` suppress it (the decision survives a restart). A finding seen in only one session is not yet proposed. High-confidence single-fact promotion is implemented (AS-138): a fact with a pinned remedy seen in 3+ sessions is auto-promoted without waiting for the second-session dedup threshold. Efficacy measurement (AS-139) is implemented: applied-remedy before/after friction deltas are computed in `internal/skillrollup` and surfaced via `smith stats` (improvements). |
| 8.2d | Rediscover the same fact carrying a pinned remedy across 3+ distinct sessions in a project, then run `smith improve` / `/improve`. | The high-confidence fact is auto-promoted into the proposal queue on its own — without relying on the normal 2-session dedup threshold; a fact seen in only one session still does not appear. (AS-138) |
| 8.5 | Enqueue two tasks (`smith run "task one" --queue`, `smith run "task two" --queue`), list them with `smith runs list`, then start `smith runs work --watch --concurrency 2`; while it watches, enqueue a third task; Ctrl+C to stop. Separately, run plain `smith runs work`. | Two workers pick up the two queued tasks without double-running a record; the third task enqueued after start is also drained (`--watch`); Ctrl+C drains in-flight work cleanly; plain `smith runs work` drains-and-exits. (AS-054/AS-132) |
| 8.3 | Inspect plugin/subagent registry docs and tests. | Third-party plugin boundary remains declarative-only and guarded. |
| 8.4 | Read AS-113. | Plugin consent screen remains **Needs clarification** because plugin install/marketplace flow is not ticketed yet. |

### 9. Headless, serve, GUI-adjacent faces, and external integrations

Covers AS-051, AS-077, AS-080, AS-110; not-yet-implemented: AS-052, AS-078, AS-079, AS-081.

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
| AS-029 | /clean "<topic>" — natural-language semantic matching | Implemented (`done`) — see step 5.3a |
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
| AS-043 | /tidy — context reorganization without lossy summarization (flagship wedge) | Implemented (`done`) — mechanical dedup core shipped as TUI `/tidy` slash command (see step 5.8); dead-end collapse + working-memory promotion spun out to AS-117 (needs-clarification) |
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
| AS-054 | Background/async runner (queue, scheduled, resumable, budget-capped) | Implemented (`done`) — `smith run --queue`, `smith runs list/status/work/resume`; daemon + concurrency spun out to AS-132 (also done); see steps 8.0 and 8.5 |
| AS-055 | Replayable run logs + OpenTelemetry export | Implemented (`done`) |
| AS-056 | Design spike: compliance archiving — immutability vs right-to-erasure | Implemented (`done`) |
| AS-057 | Cross-session analytics (portfolio dashboard) | Implemented (`done`) — `smith stats` and `smith stats all`; per-project friction merge in step 8.2b |
| AS-058 | Self-improving config (aggregated insights propose memory/skill/command edits) | Implemented (`done`) — see step 8.2c |
| AS-059 | Design spike: third-party sub-agent plugin trust, permissions, and sandboxing | Implemented (`done`) |
| AS-060 | Capture & compare real vendor session files to refine the block schema before V1 freeze | Manual capture (`ready-to-implement`) — see section 2A; deps AS-002/AS-003 done, no CI keys needed |
| AS-061 | Publish the block schema as JSON Schema (language-neutral contract + Go↔schema divergence guard) | Implemented (`done`) — `internal/schemajson` package; guarded by `make test` |
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
| AS-080 | Spike: hosted multi-tenant live-agent sandboxing | Implemented (`done`) — design spike; no runtime feature, see ticket |
| AS-081 | Viscose (VS Code) extension over `smith serve` | Not implemented (`ready-to-implement`) |
| AS-082 | Memory file @import-style includes | Implemented (`done`) |
| AS-083 | MCP resources, prompts, reconnect, and tools/list pagination | Implemented (`done`) |
| AS-084 | Rewind file-system snapshot & restore | Implemented (`done`) |
| AS-085 | Auto-compact on approaching the window limit (config-flagged, default off) | Implemented (`done`) |
| AS-086 | Conservative budget enforcement — pre-turn estimate + unpriced-turn handling | Implemented (`done`) |
| AS-087 | /init model-assisted draft enrichment | Implemented (`done`) |
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
| AS-109 | /insights model-assisted layer + goal anchoring (spun out of AS-045) | Implemented (`done`) |
| AS-110 | Model routing escalation + per-session /route overrides | Implemented (`done`) — see steps 5.7 and 5.7a |
| AS-111 | Scope-gated context slices for third-party sub-agents | Not implemented (`ready-to-implement`) |
| AS-112 | Guard the declarative-only plugin boundary with a test + archtest | Implemented (`done`) |
| AS-113 | Plugin consent screen + scope→sentence table | Needs clarification (`needs-clarification`) |
| AS-114 | Scope Coding Mode process-skill blocks to the active phase | Implemented (`done`) |
| AS-115 | Redaction-at-capture — best-effort secret/PII scrub before the log (spun out of AS-056) | Implemented (`done`) |
| AS-116 | Surface auto-escalation in `/route` and `/cost` + wire the first producer | Implemented (`done`) |
| AS-117 | `/tidy` dead-end collapse + working-memory promotion (spun out of AS-043) | Needs clarification (`needs-clarification`) |
| AS-118 | Root help ignores `--output json` | Implemented (`done`) |
| AS-132 | Background runner daemon (`runs work --watch`) + worker concurrency (`--concurrency N`) | Implemented (`done`) — see step 8.5; enqueue runs (`smith run "…" --queue`), start `smith runs work --watch --concurrency 2`; confirm runs enqueued after start are picked up, two workers never double-run a record, Ctrl+C drains cleanly, and plain `smith runs work` still drains-and-exits |
| AS-133 | Recorded vendor simulators for Anthropic, OpenAI, and compatible providers | Implemented (`done`) — used by `internal/e2e`; see steps 3.4a and 3.4b |
| AS-134 | Offline E2E regression suite over recorded providers, TUI, and append-only logs | Implemented (`done`) — `internal/e2e`, runs in `make test`; see row 3.4a |
| AS-135 | Capture-to-fixture workflow for redacted vendor sessions and CI-safe regressions | Implemented (`done`) — supports the AS-133 recorded simulators; see step 3.4b |
| AS-119 | `task` delegation across faces + child tool inheritance | Implemented (`done`) — see steps 8.0 and 8.0a; `smith run --queue` + headless delegation; child tool calls through the same permission gate |
| AS-120 | `task` per-child cost itemization, prompt attribution, budget | Implemented (`done`) — see steps 8.0 and 8.0a; child spend included in `/cost` |
| AS-121 | TUI phosphor palette — centralize colour tokens and apply to all surfaces | Implemented (`done`) — visual pass; see step 6.9 |
| AS-122 | TUI splash screen — logo, divider rule, invite text, blinking caret | Implemented (`done`) — see step 6.8 |
| AS-123 | TUI typewriter streaming — char-by-char reveal with trailing block cursor | Not implemented (`ready-to-implement`) |
| AS-124 | TUI tool card visual polish — bordered cards, left rule, truncation, elapsed time | Implemented (`done`) — bordered tool cards, left rule, truncation, elapsed time; visual pass (see §6) |
| AS-125 | TUI status line + mode bar visual polish — spec-compliant layout and colours | Implemented (`done`) — phosphor status-line segments, goal/cost/running colours, alive-pulse, coloured mode-bar phase track; visual pass (see §6) |
| AS-126 | TUI Matrix rain — medium intensity default, animated falling chars, /serious disables | Implemented (`done`) — see step 6.8 |
| AS-127 | TUI command palette visual redesign — search border, per-command styling, footer hints | Not implemented (`ready-to-implement`) |
| AS-128 | TUI /context panel visual redesign — segmented bar, amber auto-compact marker, stats rail | Not implemented (`ready-to-implement`) |
| AS-129 | TUI permission gate visual redesign — diff colours, dimmed context, option list | Not implemented (`ready-to-implement`) |
| AS-130 | TUI /agents orchestrator panel — tree view, state dots, pulsing animation | Not implemented (`ready-to-implement`) |
| AS-131 | TUI /insights panel visual redesign — stat cards, timeline, tool histogram | Not implemented (`ready-to-implement`) |
| AS-136 | Persisted cross-session stats index + cross-project friction merge | Implemented (`done`) — `smith stats rebuild`; see step 8.2b |
| AS-137 | `/insights describe` on-demand model retro when the session-end model layer is off | Implemented (`done`) — see row 8.2 |
| AS-138 | `/improve` high-confidence single-fact threshold | Implemented (`done`) — see steps 8.2c and 8.2d; facts with remedy in 3+ sessions auto-promoted |
| AS-139 | `/improve` proposal efficacy measurement (before/after friction delta) | Implemented (`done`) — before/after remedy efficacy in `internal/skillrollup`, surfaced via `smith stats`; see step 8.2c |

## Current local smoke pass (2026-06-22)

The following lightweight checks were attempted while creating this campaign:

| Check | Result | Notes |
| --- | --- | --- |
| `timeout 60 make build` | Pass | The binary built successfully after the initial long-running build was allowed to complete. |
| Ticket status extraction from `docs/project/tickets/AS-*.md` | Pass | Used to generate the coverage matrix above. |
| `./smith --help --output json` plus `python3 -m json.tool` | Pass | Root help now emits valid JSON (root summary, global flags, command tree). Fixed in AS-118. |

AS-118 (originally filed as a duplicate AS-116 id, then renumbered past a colliding AS-117) was created from this smoke pass because root JSON help did not behave as documented; it is now fixed and the JSON help is parseable. No other completed feature was proven to fail during the limited local pass.

## QA campaign pass (2026-06-25)

Full campaign run against `make build` binary (commit 7bbf891). All automated tests (`make test`, arch harness) pass. CLI checks run for all implemented features.

| Check | Result | Notes |
| --- | --- | --- |
| `make build` | Pass | Binary builds; `./smith --version` reports `smith dev (7bbf891)`. |
| `make test` | Pass | All 50 packages pass; includes `internal/e2e` offline E2E suite (AS-134). |
| `scripts/harness/quick.sh ./...` | Pass | Formatting and tests pass. |
| `scripts/harness/arch.sh` | Pass | Architecture contract tests pass. |
| `./smith --help --output json \| python3 -m json.tool` | Pass | JSON parseable (AS-118 regression holds). |
| `./smith does-not-exist` exit code | Pass | Exits 2 (invalid usage). |
| `./smith run "say hello"` without credentials | Pass | Exits 6 (non-zero); concise error on stderr. |
| `./smith run --help` shows `-f` flag | Pass | AS-069 regression holds. |
| `./smith serve --help` | Pass | Loopback default and `--unsafe-bind` documented. |
| `./smith stats` / `./smith stats all` / `./smith stats rebuild` | Pass | Cross-session analytics work; `smith stats all` shows per-project; rebuild completes. |
| `./smith improve --help` | Pass | `apply`/`dismiss`/`snooze` subcommands visible. |
| `./smith route cheap anthropic claude-haiku-4-5` | Pass | Per-session override applied without config mutation. |
| `./smith runs work --help` | Pass | `--watch` and `--concurrency` flags documented (AS-132). |
| `./smith run --help` shows `--queue` | Pass | Background enqueue flag documented (AS-054). |
| Campaign stale entries | Fixed | 10 tickets corrected from "Not implemented" → "Implemented"; AS-119–AS-139 coverage matrix rows added; ticket AS-140 filed for missing detailed scenarios. |

## QA campaign pass (2026-06-26)

Campaign re-run against `make build` binary (commit 3b1222a). All automated
suites pass; CLI scenarios re-checked on a headless Linux host (no D-Bus /
Secret Service). One bug found at step 3.7 and filed as AS-144.

| Check | Result | Notes |
| --- | --- | --- |
| `make build` | Pass | Static binary builds; `./smith --version` reports `smith dev (3b1222a)`. |
| `make test` | Pass | All packages pass, including `internal/e2e` offline suite (AS-134). |
| `scripts/harness/arch.sh` | Pass | Architecture contract tests pass. |
| `./smith --help --output json` + leaf `run --help --output json` | Pass | Both parse as JSON (AS-118 holds). |
| `./smith does-not-exist` | Pass | Exits 2 (invalid usage). |
| `./smith run "say hello"` without credentials | Pass | Exits 6; concise stderr error. |
| `run -f` / `serve --unsafe-bind` / `runs work --watch/--concurrency` / `run --queue` help | Pass | AS-069, AS-077, AS-132, AS-054 surfaces documented. |
| `./smith stats` / `stats all` / `stats rebuild` | Pass | Cross-session analytics; rebuild refreshes index. |
| `./smith improve --help` / `route cheap anthropic …` / `insights --help` | Pass | `apply/dismiss/snooze`, per-session route override, `insights describe` present. |
| `ANTHROPIC_API_KEY=… smith auth status anthropic` | Pass | Reports `set (env ANTHROPIC_API_KEY)` — env overrides keychain (AS-017 step 3.6). |
| `smith auth set openai` / `auth status` with no Secret Service | **Fail → AS-144** | Leaks raw `keychain … exec: "dbus-launch": …` error instead of the actionable `OPENAI_API_KEY` hint promised by AS-017 / step 3.7. Bug ticket AS-144 filed. |

## QA campaign pass (2026-06-30)

Campaign re-run against `make build` binary (commit `1787378`). All automated
suites pass; CLI scenarios re-checked on a headless Linux host (no D-Bus /
Secret Service). No product bugs found; the AS-144 fix from the previous pass
now holds. Three coverage-matrix rows had gone stale (the ticket was marked
`done` but the matrix still said "Not implemented") and were corrected in this
pass — no bug tickets were filed because the product behaves as the tickets
describe.

| Check | Result | Notes |
| --- | --- | --- |
| `make build` | Pass | Static binary builds; `./smith --version` reports `smith dev (1787378)`. |
| `make test` | Pass | All packages pass, including `internal/e2e` offline suite (AS-134). |
| `scripts/harness/arch.sh` | Pass | Architecture contract tests pass. |
| `go test ./internal/e2e/...` | Pass | Offline E2E suite passes (AS-133/AS-134/AS-135). |
| `./smith --help --output json` + leaf `run --help --output json` | Pass | Both parse as JSON (AS-118 holds). |
| `./smith does-not-exist` | Pass | Exits 2 (invalid usage). |
| `./smith run "say hello"` without credentials | Pass | Exits 6; concise error. `--output json` emits a machine-readable `{…,"error":…}` and still exits 6. |
| `run -f` / `run --queue` / `serve --unsafe-bind` / `runs work --watch/--concurrency` help | Pass | AS-069, AS-054, AS-077, AS-132 surfaces documented. |
| `./smith serve` startup | Pass | Binds `ws://127.0.0.1:8765` (loopback only) and notes Ctrl+C to stop. |
| `./smith stats` / `stats all` / `stats rebuild` | Pass | Cross-session analytics; rebuild refreshes index. |
| `./smith improve --help` / `route cheap anthropic …` / `insights --help` | Pass | `apply/dismiss/snooze`, per-session route override, `insights describe` present. |
| `ANTHROPIC_API_KEY=… smith auth status anthropic` | Pass | Reports `set (env ANTHROPIC_API_KEY)` — env overrides keychain (step 3.6). |
| `smith auth set openai` / `auth status` with no Secret Service | Pass (AS-144 holds) | `auth set` shows the actionable `no OS keychain available … set OPENAI_API_KEY` hint and never writes a plaintext key; `auth status` shows `no keychain available (set OPENAI_API_KEY …)`. |
| Campaign stale entries | Fixed | AS-080 (spike), AS-124, AS-139 moved from "Not implemented" → "Implemented (`done`)": removed from the not-implemented rows and added to the active lists — coverage matrix, a new Implemented quick-checklist row, the §6 and §8 `Covers` lists, the §9 header `Covers` list, and step 8.2c. All three confirmed `done` in their ticket frontmatter (AS-139 efficacy code in `internal/skillrollup`/`internal/stats`, AS-124 merged in #467). |
