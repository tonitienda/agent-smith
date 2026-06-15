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

```
docs/project/PRD.md        product spec — read the Decision Log (D0–D9) first
docs/project/tickets/      one file per ticket (AS-NNN-slug.md) + index README
cmd/smith/                 Agent Smith CLI entrypoint (single-binary target)
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

Run `./smith` in a terminal to start an interactive chat session (the flagship TUI face, AS-021); set `ANTHROPIC_API_KEY` to talk to the Anthropic provider and `SMITH_MODEL` to override the default model. Off a TTY (scripts, CI) the binary prints usage instead.

Inside the chat, type `/` to open the command palette. `/cost` (AS-020) shows the session's token & dollar accounting — a per-turn breakdown by input/output/cache plus how much prompt caching saved. Pricing ships as data; point `SMITH_PRICING` at a JSON file (same shape as [`internal/cost/data/pricing.json`](internal/cost/data/pricing.json)) to override or add model rates without recompiling. Models with no rate still show exact token counts, with the dollar figure marked unknown.

## License

Apache-2.0 (Decision Log D8 — OSS-first). See [LICENSE](LICENSE).

---

<p align="center"><sub>"Never send a human to do a machine's job." — run <code>/serious</code> if you disagree.</sub></p>
