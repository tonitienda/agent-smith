<p align="center">
  <img src="docs/assets/logo.svg" width="170" alt="Agent Smith logo">
</p>

<h1 align="center">Agent Smith</h1>

<p align="center"><em>The models do the thinking; Agent Smith makes the thinking observable, controllable, portable, and reusable.</em></p>

---

**Agent Smith** is a fast, provider-agnostic coding agent written in Go — a single binary that works with **Anthropic and OpenAI** (plus OpenAI-compatible endpoints) and treats the LLM as a swappable reasoning engine. The bet: model intelligence is commoditizing, so the durable value of a third-party agent is the **harness** — context engineering, cost/speed control, observability, and portability. Agent Smith aims to be the best harness, not the best brain.

> Status: **core substrate in progress**. The product spec ([PRD](docs/project/PRD.md)) and fully ticketed backlog are done; AS-001 through AS-007 have landed the Go CLI scaffold, schema/event-log/projection substrate, and project-scoped disk session persistence. The agent runtime is still implemented ticket-by-ticket from the roadmap.

## The problem

Today's coding agents (Claude Code, Codex, Cursor, Aider, …) optimize for capability and leave four things underserved:

1. **Context is a black box.** You see a token count, not *composition* — and you can't say "remove everything about the bug we already fixed."
2. **Sessions leave no learning behind.** No retrospective, no "you re-read the same file 6 times," no feedback loop.
3. **Cost and speed are afterthoughts.** No budgets, no cheap-model routing, no cache transparency.
4. **Config is vendor-locked.** Memory files, skills, and hooks don't port between providers.

## How it's different

Two structural decisions (see the PRD's [Decision Log](docs/project/PRD.md#decision-log--v03-post-grilling-where-this-conflicts-with-the-sections-below-this-wins), D0–D9, which overrides the rest of the document):

- **An open, stable data substrate.** Every session is an **append-only, immutable event log of content blocks**; the model-facing context is a *projection* over that log. Editing commands append exclusion/derived events with provenance — they never mutate history, so every edit is reversible and auditable by construction (D3). The schema is the designed-up-front union of mainstream agent/provider wire formats (D4) and is **additive-only forever** — no breaking changes, ever (D2). Incumbents keep the transcript as a private, churning internal artifact; Agent Smith publishes it as a stable API others can build on. That, plus provider neutrality, is the moat (D1).
- **Cost/speed as a design criterion, not a marketing claim.** "Cheaper and faster than a naive harness on the same model" is an internal guardrail measured on a benchmark suite (D5). External positioning leads with control, observability, and neutrality.

On top of that substrate sit the **five wedges** — the features none of the incumbents ship:

| Wedge | What it does |
|---|---|
| **Context composition view** (`/context`) | See the window broken down by segment, topic, file, recency — with token + $ per segment |
| **Semantic context editing** (`/clean`) | `"/clean the bug we already fixed"` — preview, reclaim tokens, fully undoable |
| **Context tidy** (`/tidy`) | Restructure a messy session — dedupe, collapse dead ends — without lossy summarization |
| **Session insights** (`/insights`) | Model-assisted retro: what burned tokens, what to add to your memory files, what to improve |
| **Living skills** | Detect facts you keep rediscovering and offer to save them into the right skill/memory |

### Architecture at a glance

```
┌────────────────────────────────────────────────────────────┐
│  Faces:    TUI (flagship)  │  ACP server  │  headless CLI  │
├────────────────────────────────────────────────────────────┤
│                      Agent Core (Go)                       │
│   append-only event log  ·  context projection engine     │
│   tool runtime  ·  permissions  ·  cost/speed optimizer   │
│   insights engine  ·  system sub-agents (lifecycle)       │
├────────────────────────────────────────────────────────────┤
│  Capability layer: memory files (AGENT/CLAUDE/AGENTS.md)   │
│  skills · MCP client · hooks · subagents · slash commands  │
├────────────────────────────────────────────────────────────┤
│ Providers: Anthropic │ OpenAI │ xAI/Grok/OpenAI-compatible │
└────────────────────────────────────────────────────────────┘
```

## Roadmap

The backlog is one file per ticket in [`docs/project/tickets/`](docs/project/tickets/README.md), in two waves.

### V1 — the thinnest thesis slice ([AS-001…AS-030](docs/project/tickets/README.md#index--v1-as-001--as-030))

Per Decision Log D6: two providers, the event-log + projection core, a basic agentic loop with file/shell tools, a TUI, `/context` + `/clean`, and the permission model. Build order:

1. **Substrate first** (the moat) — scaffolding, wire-format spike, immutable block schema, additive-only CI guard, event log, projection engine, persistence (AS-001…007)
2. **Providers + tools** — provider interface, Anthropic + OpenAI, prompt caching, conformance suite; tool runtime, file/search/shell tools, permission model, keychain storage (AS-008…017)
3. **Loop + faces** — agentic loop, parallel tools, TUI, slash commands, cost accounting, parity commands (AS-018…025)
4. **The V1 wedges** (the demo) — `/context` composition view, `/clean` manual + semantic (AS-026…029)
5. **Guardrail** — the cost/speed benchmark suite that keeps D5 honest (AS-030)

### Fast-follow ([AS-031…AS-053](docs/project/tickets/README.md#index--fast-follow--p2-as-031--as-059))

The capability layer (memory files, skills, hooks, MCP, custom commands), remaining power commands (`/rewind`, `/compact`, `/goal`, `/budget`), model routing, `/tidy`, the system sub-agent framework, `/insights`, living skills, headless CLI + ACP server — and the optional Matrix personality layer (`/serious` turns it off).

### P2 — production & scale (AS-054…AS-059)

Background/async runner, replayable runs + OpenTelemetry, cross-session analytics, self-improving config, plus two design spikes (compliance archiving vs. erasure; plugin trust & sandboxing).

**Explicitly out of scope for v1:** training models, desktop GUI, team/enterprise features, OS-level sandboxing, and prompt-injection defense — the last two are documented known limits (D9): *Agent Smith runs with your privileges in your environment; you approve actions. It is not a sandbox.*

## Repository layout

Architecture docs live under [`docs/architecture/`](docs/architecture/README.md). Start with the C4 [system context](docs/architecture/system-context.md), then the [container](docs/architecture/containers.md) and [core component](docs/architecture/core-components.md) views when changing runtime seams.

```
docs/architecture/         C4 architecture docs and runtime flow diagrams
docs/project/PRD.md        product spec — read the Decision Log (D0–D9) first
docs/project/tickets/      one file per ticket (AS-NNN-slug.md) + index README
cmd/smith/                 Agent Smith CLI entrypoint and subcommand dispatch (single-binary target)
internal/smithapp/         reusable Smith app wiring for router, providers, and sessions
internal/                  internal Go packages shared by binaries
cmd/ticket-sync/           mirrors ticket files to GitHub issues (files are the source of truth)
internal/session/          project-scoped disk persistence for append-only session logs
internal/provider/         provider abstraction: normalized request/stream interface + taxonomy
internal/loop/             agentic turn loop: projection → stream → tool dispatch, face-agnostic UI events
.github/workflows/         CI for build, vet, lint, tests, and merged-ticket issue sync
scripts/agent-quality-gate.sh  shared deterministic pre-submit gate for humans and agents
```

## Development

The primary binary is `smith`. Before handing off changes, humans and agents should run `./scripts/agent-quality-gate.sh` (documented in [Agent quality gates](docs/agent-quality-gates.md)) so formatting, unit tests, vet, and lint match CI:

```sh
make build      # builds a static ./smith binary from ./cmd/smith
make test       # runs all Go tests
make vet        # runs go vet
make lint       # installs/runs the pinned golangci-lint version (v2.12.2)
make verify     # runs fmt, test, vet, and lint in the same order agents use
```

Run `./smith` in a terminal to start an interactive chat session (the flagship TUI face, AS-021); set `ANTHROPIC_API_KEY` to talk to the Anthropic provider and `SMITH_MODEL` to override the default model. Off a TTY (scripts, CI) bare `smith` prints usage and exits non-zero instead of hanging.

Each persisted interactive session now also writes a plain-text debug log beside its event log at `~/.agent-smith/sessions/<project-hash>/<session-id>/debug.log`, so provider failures and stream errors are inspectable even when the TUI only shows the summarized error.

`smith` is also a noun-grouped subcommand router (AS-065, [CLI-UX.md](docs/project/CLI-UX.md)): `smith run "<prompt>"` runs a single task non-interactively (the prompt also arrives via piped stdin or `-f <file>`), `smith session list|resume`, `smith context show`, and `smith cost` inspect a project's sessions, `smith config get|set` reads/writes layered config (flag > project file > user file > `SMITH_*` env > default), and `smith tui` launches the TUI explicitly. The inspection verbs share the exact handlers the TUI slash-commands use. Results go to stdout and diagnostics to stderr; `--output plain|json|stream-json`, `--color`, and `NO_COLOR` control rendering; exit codes are `0` success / `1` failure / `2` invalid usage. `smith --help`, `smith <cmd> --help`, and `smith <cmd> --help --output json` (machine-readable) document the tree. The headless scripting feature set (budgets, `--auto`, streaming) builds on this spine in AS-051.

Inside the chat, type `/` to open the command palette. `/cost` (AS-020) shows the session's token & dollar accounting — a per-turn breakdown by input/output/cache plus how much prompt caching saved. Pricing ships as data; point `SMITH_PRICING` at a JSON file (same shape as [`internal/cost/data/pricing.json`](internal/cost/data/pricing.json)) to override or add model rates without recompiling. Models with no rate still show exact token counts, with the dollar figure marked unknown.

`/context` (AS-026, also `Ctrl+G c`) opens the composition view — what is actually filling the window right now, broken down segment by segment: the top consumers first, a by-type rollup, duplicate file reads flagged with their combined cost, and stale reclaim candidates, each with its token share, dollar cost and age. It opens instantly from projection data (no model call); sort the full list with `/context size|age|type`. Each segment in the list carries a short **handle**.

`/clean` (AS-028) edits the window by removing segments you no longer need — pass the handles from `/context`: `/clean <handle>…` previews exactly what would leave the window and the tokens/$ it reclaims (tool-call/result pairs are removed together, and very recent blocks draw a warning) without changing anything; `/clean --apply` confirms, `/clean --undo` restores the most recent removal, `/clean --cancel` discards a preview. Nothing ever leaves the log — a removal is an appended exclusion event and an undo is its exact reversal (PRD D3) — and excluded blocks stay browsable in the "Excluded from the window" section of `/context`.

**Custom slash commands** (AS-033) let you define your own commands as Markdown files — a prompt template the model runs. Drop `name.md` into `.agent-smith/commands/` (per project) or `<user-config-dir>/smith/commands/` (everywhere); it becomes `/name` the next time you open the palette, no restart needed. The body is the prompt, with `$ARGUMENTS` (the whole argument string) and `$1`, `$2`, … (positional) substituted in; an optional `---`-fenced frontmatter sets a `description` and `argument-hint` for `/help`. The layout matches Claude Code's, so an existing command file works unmodified. A project command beats a user one of the same name (and `/help` says so); a built-in name like `/cost` always wins. Example — `.agent-smith/commands/review.md`:

```markdown
---
description: Review a file for bugs
argument-hint: "<path>"
---
Review $1 for correctness bugs and suggest fixes.
```

**Portable skills** (AS-034) are instruction bundles the model loads on demand. Each skill is a directory holding a `SKILL.md` — a `---`-fenced frontmatter with `name` and `description`, then a Markdown body of instructions. Drop them under `.agent-smith/skills/<name>/` (per project) or `<user-config-dir>/smith/skills/<name>/` (everywhere); a project skill shadows a user one of the same name. Discovered skills are offered to the model through a single `skill` tool: when a request matches a skill's description the model invokes it by name and the instructions enter the conversation, attributed to the skill so `/context` shows their token cost under a **skill** group. The layout matches Claude Code's, so an existing skill loads unmodified. Example — `.agent-smith/skills/changelog/SKILL.md`:

```markdown
---
name: changelog
description: Write a release changelog from the git history
---
Summarize the commits since the last tag into a Keep-a-Changelog entry.
```

**Lifecycle hooks** (AS-035) run your own shell commands at well-known points in a session — `session-start`, `session-stop`, `pre-tool-use`, `post-tool-use`, `pre-compact`, and `user-prompt-submit` — to observe, block, modify, or annotate what the agent does. Configure them as a `hooks` array in layered config (`.agent-smith/config.json` per project, or the user config file). Each hook names an `event`, an optional `matcher` (a glob on the tool name, for the tool events), the `command` to run (via `sh -c`), an optional `timeout` (e.g. `"5s"`), and a `failOpen` policy. The hook receives the event payload as JSON on stdin and steers the outcome with its exit code and an optional JSON object on stdout: exit `0` with empty output allows; `{"decision":"block","reason":"…"}` (or exit `2`) blocks and feeds the reason back to the model; `{"input":{…}}` rewrites a tool's arguments before it runs (recorded on the log with hook provenance — for `user-prompt-submit`, where there are no tool arguments, `input` is instead the replacement prompt as a JSON **string**, e.g. `{"input":"<rewritten prompt>"}`); `{"annotation":"…"}` appends an audit note. Hooks are automation layered on top of the permission gate, never a replacement for it — `pre-tool-use` runs *after* the permission check. A hook that hangs or crashes never wedges the loop: it runs under a timeout and its failure resolves through `failOpen` (continue) or fail-closed (block), with a visible warning. Example — block the shell tool from touching `/etc`:

```json
{
  "hooks": [
    {
      "event": "pre-tool-use",
      "matcher": "shell",
      "command": "grep -q '/etc' && echo '{\"decision\":\"block\",\"reason\":\"no /etc access\"}'; exit 0",
      "timeout": "5s"
    }
  ]
}
```

(The `pre-compact` event is defined and fires through the same machinery; it is wired to `/compact` when that lands — AS-038.)

**MCP servers** (AS-036) let the agent use tools hosted by external [Model Context Protocol](https://modelcontextprotocol.io) servers — a stdio subprocess or an HTTP/SSE endpoint. Declare them under `mcp.servers` in layered config, keyed by name; each server's tools are registered under a namespaced name `mcp__<server>__<tool>`, so they flow through the permission gate, the event log, and `/context` (where their cost is attributed per server) exactly like the built-in tools. A `command` (plus optional `args`/`env`) selects the stdio transport; a `url` (plus optional `headers`) selects HTTP/SSE; an optional `timeout` bounds each call. Isolation is the rule: a server that fails to connect is skipped with a warning, and one that crashes or hangs mid-session only makes *its* tools report unavailable — the session stays healthy. Keep secrets out of the (often checked-in) config file: a stdio server inherits the agent's environment, so export `GITHUB_TOKEN`/`API_KEY` there rather than hard-coding it under `env`. Example:

```json
{
  "mcp": {
    "servers": {
      "github": { "command": "github-mcp-server", "args": ["stdio"] },
      "search":  { "url": "https://mcp.example.com/sse", "headers": { "Authorization": "Bearer <token>" }, "timeout": "20s" }
    }
  }
}
```

(A stdio server inherits the agent's environment — the preferred home for secrets. HTTP `headers` are read verbatim from config and not expanded, so if one must carry a token, keep that config file out of version control and treat it as sensitive.)

(Resources, prompts, on-demand reconnect, and `tools/list` pagination are the AS-083 follow-on.)

## License

Apache-2.0 (Decision Log D8 — OSS-first). See [LICENSE](LICENSE).

---

<p align="center"><sub>"Never send a human to do a machine's job." — run <code>/serious</code> if you disagree.</sub></p>
