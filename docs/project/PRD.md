# Agent Smith — Product Requirements Document (Initial Draft)

> Status: **Draft v0.3** (post stress-test) · Owner: Toni · Date: 2026-06-12
> A provider-agnostic coding agent built on an **open, stable data substrate** for agent sessions, with first-class **context observability and control** across Anthropic, OpenAI, and mainstream compatible agent/API surfaces. Efficiency is a design criterion, not a headline claim (see Decision Log, D5).

---

## 1. Executive Summary

We're building **Agent Smith**, a fast, low-cost, provider-agnostic coding agent (core in Go) for developers who already live in Claude Code / Codex / Grok but are frustrated by **opaque context, runaway cost, and zero feedback on how their sessions actually went**. Agent Smith treats the LLM as a swappable reasoning engine and concentrates its own value on the **harness layer**: visualizing and surgically editing context, tidying messy conversations into clean reusable context, routing work to the cheapest capable model, and producing a **model-assisted session dashboard** ("what went well, what burned tokens, what to improve"). The result is an agent that feels powerful and delightful while being measurably cheaper and faster — and a natural fit as an **optimized engine for async/background tasks**.

**One-liner:** *The models do the thinking; Agent Smith makes the thinking observable, controllable, portable, and reusable.*

---

## Decision Log — v0.3 (post-grilling; where this conflicts with the sections below, this wins)

Locked commitments from a design stress-test. Sections 1–10 remain the original exploration; the items below are the decisions.

**D0 · Intent.** Built to learn, but treated as a *shippable product*. Genuinely hard problems may be punted — but only as **explicitly documented known compromises**, never silently.

**D1 · Moat.** Not the features — **provider-neutrality + an open, stable data substrate**. Incumbents keep the session/transcript as a private, churning internal artifact so nothing can be built on it; Agent Smith publishes *all* session data behind an **additive-only API/schema that never breaks**. Context features (`/context`, `/clean`, …) are *acquisition* (they win the demo); neutrality + open data are *retention* (incumbents structurally can't match them). OSS transparency reinforces both.

**D2 · Schema discipline.** **Additive-only from V1, forever** — no removals, no repurposing, no deprecation windows, no breaking changes. New concepts = new optional fields/records; consumers tolerate missing/unknown.

**D3 · Core data model.** The session is an **append-only, immutable event log of content blocks** (text / tool-call / tool-result / file-read / reasoning; stable ID). The model-facing **context is a *projection* over the log**, not stored state. `/clean`, `/tidy`, `/compact`, `/rewind` append exclusion or derived-block events (with provenance); they never mutate history. Reversibility and auditability become structural, and additive-only becomes natural. (Resolves §10 Q3.)

**D4 · Polyglot schema.** The immutable block schema is modeled as the **union/superset of mainstream agent/provider wire formats, designed up front** — starting with Anthropic and OpenAI, and explicitly surveying public surfaces from mainstream coding agents such as xAI/Grok Build (OpenAI-compatible Responses API, headless streaming JSON, MCP-facing events), Codex/Gemini/Cursor/Cline/Aider where stable formats exist. The spike classifies each surface as schema input, compatibility note, or out of scope before the schema is frozen — so provider #2 or a mainstream agent import/export never forces a breaking change.

**D5 · Cost/speed = criterion, not claim.** "Cheaper/faster than a *naive* harness on the same model" is an **internal design criterion + guardrail metric**, measured on a benchmark suite — *not* a marketing promise. External positioning leads with control, observability, and neutrality. (Supersedes the "30–50% cheaper" headline in §6.)

**D6 · V1 = thinnest thesis slice.** Ships: 2 providers (Anthropic + OpenAI) · event-log + projection core · basic agentic loop with file/shell tools · TUI · `/context` + `/clean` · the permission model (D9). **Deferred to fast-follow:** `/tidy`, `/insights`, living-skills, model routing, budgets, MCP, hooks, skills, subagents, ACP, async runner, personality/Matrix layer.

**D7 · Living-skills = scalpel, not courtroom.** The first form (post-core) is the **rediscovered-fact detector**: spot trial-and-error → a concrete durable fact (command/path/config) → offer to save it to the relevant skill/memory. Budget/contract scoring is demoted to a later *experimental* rollup signal once session volume exists.

**D8 · Business & license.** **OSS-first, Apache-2.0.** Monetization deferred (no cloud infra now); leading future candidate is **premium sub-agents** on the public plugin interface. A second candidate falls out of the architecture for free: **compliance-grade session archiving** for regulated orgs (healthcare/banking) — tamper-evident, retained, exportable to *their* storage — because the append-only log (D3) is already the audit artifact (see 7.23). Any cloud/async/team tier waits until the OSS tool has users. (Resolves §10 Q6.)

**D9 · Security posture.** Build now: permission/approval model (ask / allowlist / auto) + OS-keychain key storage. **Punted as documented known limits:** OS-level sandboxing, prompt-injection defense, plugin code-sandboxing (V1 sub-agents are first-party; third-party plugins, when they arrive, are *declarative-only* — manifest + prompt, no arbitrary code). Stated posture: *"Agent Smith runs with your privileges in your environment; you approve actions. It is not a sandbox."*

---

## 2. Problem Statement

### Who has this problem?
Power users of agentic coding tools — individual developers and small teams who run dozens of sessions a week across Claude Code, OpenAI Codex, Grok, Cursor, Aider, Cline, and Gemini CLI.

### What is the problem?
Today's agents optimize almost entirely for **capability**, and leave four things underserved:

1. **Context is a black box.** You can see a rough token count, but not *composition* — which files, tool outputs, sub-threads, and dead ends are eating the window. You can `/compact` (lossy) or `/clear` (nuclear) or `/rewind` (linear, time-based), but you can't say *"remove everything about the bug we already fixed."*
2. **A session leaves no learning behind.** When a session ends, there's no retrospective — no "you re-read the same file 6 times," "this 40k-token tool dump added nothing," "your CLAUDE.md is missing the test command you typed 4 times." The user repeats the same friction forever.
3. **Cost and speed are afterthoughts.** Most agents don't expose a budget, don't auto-route cheap subtasks to cheap models, don't surface cache savings, and don't tell you *how to make the next turn cheaper*.
4. **Config is vendor-locked.** CLAUDE.md, skills, MCP wiring, and hooks don't port cleanly between Anthropic and OpenAI, so switching providers (for cost, speed, or availability) is painful.

### Why is it painful?
- **Money:** Long, messy contexts are re-sent every turn. A bloated window is a recurring tax, paid silently.
- **Speed:** Bigger context = slower time-to-first-token and slower turns.
- **Quality:** Noisy context degrades model output ("context rot"); the user often can't tell *why* the agent got worse.
- **No compounding:** Without insights, users never get better at driving the agent, and never improve their reusable assets (memory, skills).

### Evidence (to validate — see Open Questions)
- First-party: the builder's own daily friction across the tools above.
- Widespread community signal: "/compact ruined my context," "why is this so expensive," "I wish I could edit the context" are recurring themes in agent-tool communities.
- Structural: none of the four major actors ships a session-retrospective or a context-composition editor today (see §4 competitive matrix).

---

## 3. Target Users & Personas

### Primary — "Power-User Pat" (the optimizer)
- Senior/staff engineer or indie hacker; runs many agent sessions daily.
- Tech-savvy, cost-aware, opinionated about tooling; already uses CLAUDE.md/AGENTS.md, MCPs, custom commands.
- **Jobs:** ship faster, keep spend predictable, switch models freely, stop repeating the same setup mistakes.
- **Pains today:** opaque context, surprise bills, vendor lock-in, no feedback loop.

### Secondary — "Async Ana" (the automator)
- Builds background/batch pipelines: triage issues, draft PRs, refactor at scale, scheduled jobs.
- **Jobs:** run thousands of cheap, reliable, auditable agent runs unattended.
- **Needs:** headless/programmatic API, deterministic-ish replayable runs, hard cost ceilings, cheap model routing.

### Tertiary — "Team-Lead Tess" (future)
- Wants shared, portable config and cross-session analytics for a small team; cares about spend visibility and consistency.

---

## 4. Strategic Context

### Thesis
The frontier of *intelligence* belongs to the model labs and is commoditizing fast (multiple comparable Anthropic/OpenAI/xAI models). The durable, defensible value for a third-party agent is the **harness**: context engineering, cost/speed optimization, observability, and portability. Agent Smith bets on **being the best harness, not the best brain** — which is exactly what makes it provider-agnostic and a good async/background engine.

### Competitive landscape (approximate, mid-2026 — verify before relying)

> Note on "PI": I interpreted this as the broad field of "other powerful agents." If you meant a specific tool (Inflection Pi is a consumer chat assistant, not a coding agent), tell me and I'll slot it in precisely. I've covered the actors that actually compete in agentic coding.

| Capability | Claude Code | OpenAI Codex | Grok (Build/CLI) | Cursor / Cline / Aider | **Agent Smith (target)** |
|---|---|---|---|---|---|
| Memory file | CLAUDE.md | AGENTS.md | AGENTS.md-ish | varies | **AGENT.md + CLAUDE.md + AGENTS.md (all)** |
| Skills | ✅ | partial | ❌ | ❌ | **✅ portable** |
| MCP | ✅ | ✅ | partial | ✅ (Cursor/Cline) | **✅** |
| Hooks | ✅ | partial | ❌ | ❌ | **✅** |
| Subagents | ✅ | partial | ❌ | partial | **✅** |
| Slash/power commands | ✅ rich | ✅ | basic | varies | **✅ rich + new** |
| Sandbox/permissions | ✅ | ✅ strong | ✅ | varies | **✅** |
| Multi-provider | ❌ (Anthropic) | ❌ (OpenAI) | ❌ (xAI) | ✅ (some) | **✅ Anthropic + OpenAI + compatible** |
| TUI | ✅ | ✅ | ✅ | IDE-first | **✅ flagship** |
| Programmatic / SDK | ✅ Agent SDK | ✅ | partial | partial | **✅ ACP + Go SDK + headless** |
| Cloud/async runner | ✅ | ✅ Codex cloud | partial | ❌ | **✅ (cheap-optimized)** |
| Cost/token meter | basic `/cost` | basic | basic | basic | **✅ deep + projections** |
| Budget guardrails | ❌ | ❌ | ❌ | ❌ | **✅** |
| Model routing/tiering | manual | manual | n/a | manual | **✅ automatic** |
| **Context composition view** | ❌ | ❌ | ❌ | ❌ | **✅ — wedge** |
| **Semantic context edit (`/clean`)** | ❌ | ❌ | ❌ | ❌ | **✅ — wedge** |
| **Context tidy/reorganize** | compaction only | compaction only | compaction only | ❌ | **✅ — wedge** |
| **Session insights / retro** | ❌ | ❌ | ❌ | ❌ | **✅ — wedge** |
| **Self-improving config** | ❌ | ❌ | ❌ | ❌ | **✅** |
| **Skill performance from real sessions** | ❌ | ❌ | ❌ | ❌ | **✅ — wedge** |
| **System sub-agents (lifecycle, opt-in, pluggable)** | ❌ | ❌ | ❌ | ❌ | **✅** |

### The gaps = our five wedges
1. **Context observability & surgical editing** (composition view + `/clean` + `/tidy`).
2. **Session insights** (model-assisted retrospective dashboard).
3. **Cost/speed as a product** (budgets, routing, cache transparency).
4. **Provider portability** (one config, any model).
5. **Living skills** (skill performance graded from real sessions; company/project skills get sharper over time).

### Why now?
- Models are good enough and cheap enough that the bottleneck has shifted from *intelligence* to *operating the agent well*.
- Cross-provider parity makes portability valuable for the first time.
- Go gives us a fast, single-binary, low-overhead harness — the right tool for a "speed and cost" thesis.

---

## 5. Solution Overview

Agent Smith is a single Go binary that exposes the same agent core through three faces: a **TUI** (flagship), an **ACP server** (programmatic/editor), and a **headless CLI** (scripting/CI). A future **desktop UI** reuses the same core over ACP.

```
┌──────────────────────────────────────────────────────────┐
│  Faces:   TUI   │   ACP server   │   Headless CLI   │ (Desktop, later) │
├──────────────────────────────────────────────────────────┤
│                     Agent Core (Go)                        │
│  Conversation/Context Engine  ·  Tool runtime  ·  Permissions/Sandbox │
│  Context store (composition-aware, segmented, editable)    │
│  Cost/Speed optimizer:  model router · cache mgr · budgets │
│  Insights engine (model-assisted retro)                    │
│  System sub-agents:  init → observe → teardown (opt-in)    │
├──────────────────────────────────────────────────────────┤
│  Capability layer:  Memory (AGENT/CLAUDE/AGENTS.md) ·       │
│  Skills · MCP client · Hooks · Subagents · Slash commands  │
├──────────────────────────────────────────────────────────┤
│  Provider abstraction:  Anthropic │ OpenAI │ OpenAI-compatible (Grok, local, OpenRouter) │
└──────────────────────────────────────────────────────────┘
```

### The headline experience: **context as a first-class, editable object**
Instead of an opaque transcript, the context is a **segmented, labeled store**. Every segment knows: its type (user msg, assistant msg, tool call/result, file read, sub-thread, memory, system), its token cost, its topic/tags, its recency, and whether it's still "live" or stale. This single design unlocks all four wedges:
- **See it** — composition meter + breakdown.
- **Clean it** — semantic removal (`/clean "the bug we fixed"`).
- **Tidy it** — reorganize messy → structured without lossy summarization.
- **Learn from it** — the insights engine has structured data to analyze.

### How the differentiators are built: **system sub-agents with a lifecycle**
Most wedges aren't monolithic code — they're small, specialized, cheap-model **system sub-agents** the main agent drives at lifecycle hooks (distinct from user-delegated task subagents in §7.17). Each follows the same contract:

```
skill loads ─▶ init()                          teardown() ─▶ report
               │ write/load expectation          ▲  analyze: contract vs. actual
               ▼ contract                         │  classify: gap | trigger | friction
           ┌─────────────────────────────────────┴──┐
           │       main agent does the work          │ ◀─ observe (passive: turns,
           └────────────────────────────────────────-┘     tools, corrections, cost)
```

- **init(scope)** — set up before the watched span (e.g., establish a skill's expectation contract).
- **observe** — passively accumulate trace signals from the segmented context store; no extra model calls.
- **teardown(scope, state)** — the main agent hands over the relevant context slice; the sub-agent analyzes and reports findings into `/insights`.

They are **opt-in and cost-bounded**: off by default where they add cost, run on the cheap routing tier, and execute async/batched at teardown so they never slow an interactive turn. The same primitive powers `skill-expectation-analyzer`, `insights-writer`, the `/clean` matcher, the `/tidy` reorganizer, and the router — and they're **first-party plugins on a public sub-agent interface**, so third parties (or a team) can add their own (security review, test coverage, compliance) against the same API we build on. Schemas in Appendix C.

---

## 6. Success Metrics

### Primary
- **Cost per completed task vs. a naive baseline harness using the same model**, on a fixed benchmark task suite with per-block token/cost accounting. This is an **internal design criterion + guardrail** (Decision Log D5), *not* a public claim — it guides feature decisions; it is never a marketing number.

### Secondary
- **Time-to-first-token and median turn latency** vs. baseline (target: faster, driven by smaller live context + caching + routing).
- **Context efficiency:** median % of window that is "live/relevant" at end of session (target: ↑ via `/clean` + `/tidy`).
- **Insights adoption:** % of sessions where the user opens `/insights`; % who apply at least one suggestion.
- **Skill health:** % of flagged skill-gaps applied; trend of over-budget skill runs (should ↓ as skills absorb project knowledge).
- **Portability:** % of users running >1 provider with the same config.
- **Delight:** retention (weekly active), NPS-style "feels powerful" rating.

### Guardrails (must not regress)
- **Task success rate** vs. baseline (cheaper must not mean worse).
- **No data loss** from `/clean`/`/tidy` (every edit is reversible).

---

## 7. Requirements (Feature Catalog)

Organized as **P0 (MVP)**, **P1 (differentiators)**, **P2 (scale/production)**. Each is phrased as a capability with acceptance criteria.

> **V1 ship set (Decision Log D6):** 7.1 *(both providers)* · the append-only block-log + projection core *(D3)* · 7.2 loop & tools + permission/key-storage *(D9)* · 7.8 TUI · 7.9 persistence · 7.10 cost meter · 7.11 `/context` · 7.12 `/clean` · a subset of 7.16 power commands. **Everything else below is fast-follow** — including MCP, hooks, skills, subagents, `/tidy`, `/insights`, living-skills, routing, budgets, ACP, async, and the personality layer. The P0/P1/P2 tiers below describe *eventual* priority, not V1.

### P0 — Credible agent core (table stakes)

**7.1 Provider abstraction (Anthropic + OpenAI)**
- [ ] Pluggable provider interface; Anthropic + OpenAI implemented; OpenAI-compatible endpoint support (covers Grok/local/OpenRouter) as a bonus.
- [ ] Per-request model selection; streaming; tool/function calling normalized across providers.
- [ ] Prompt caching used where the provider supports it.

**7.2 Agentic loop + tool runtime**
- [ ] Core tools: file read/edit/write, glob/grep, shell (sandboxed), web fetch/search.
- [ ] Parallel tool execution when calls are independent.
- [ ] Permission modes (ask / allowlist / auto) + sandbox boundaries.

**7.3 Memory files**
- [ ] Load + merge **AGENT.md, CLAUDE.md, AGENTS.md** hierarchically (user → project → dir). Treat them as equivalent so config ports across ecosystems.

**7.4 MCP client**
- [ ] Connect to stdio + HTTP/SSE MCP servers; expose their tools/resources/prompts to the loop.

**7.5 Hooks**
- [ ] Lifecycle events: session start/stop, pre/post tool use, pre-compact, user-prompt-submit. Hooks can block, modify, or annotate.

**7.6 Slash commands (built-in + custom)**
- [ ] Built-ins below; custom commands loadable from project/user dirs (Markdown/templated).

**7.7 Skills**
- [ ] Load portable skills (instructions + optional tools); model invokes them on match.

**7.8 TUI (flagship face)**
- [ ] Streaming output, diff review, tool-call transparency, command palette, context meter always visible.

**7.9 Session persistence**
- [ ] Save/resume sessions; full transcript + segmented context stored on disk.

**7.10 Cost meter**
- [ ] Live per-turn and per-session token + $ accounting, broken down by input/output/cache.

### P1 — The wedges (why anyone switches)

**7.11 Context composition view** *(flagship differentiator)*
- [ ] A live panel/`/context` command showing the window broken down by segment type, topic, file, and recency, with token + $ per segment.
- [ ] Highlights: largest consumers, stale segments, duplicated reads.
- *AC:* user can identify, in <5s, the top 3 things eating their context.

**7.12 `/clean` — semantic context editing** *(flagship differentiator)*
- [ ] Natural-language removal: `/clean "the content related to the bug we fixed"`. The engine selects matching segments, shows a preview (what's removed, tokens reclaimed), and removes on confirm.
- [ ] Also manual: select segments in the composition view and drop them.
- [ ] Fully reversible (kept in an off-window archive).
- *AC:* removing a topic reclaims tokens without breaking the live thread; undo restores exactly.

**7.13 `/tidy` — context reorganization** *(flagship differentiator)*
- [ ] Restructure a messy session into a clean, ordered context: dedupe file reads, collapse dead ends, group by topic, promote durable facts to a "working memory" block — **without** the lossy summarization of `/compact`.
- [ ] Output is a new, smaller, well-labeled context that's easy to `/clean` later.
- *AC:* tidied context is materially smaller, preserves all live facts, and is reversible.

**7.14 `/insights` — session dashboard** *(flagship differentiator)*
- [ ] Model-assisted retrospective: cost breakdown, slowest/most expensive turns, repeated/wasted work (re-reads, loops), context-health timeline, and **concrete suggestions** ("add `make test` to CLAUDE.md," "this MCP returned 40k unused tokens — scope it," "split this skill").
- [ ] Suggestions are actionable: one-click to update CLAUDE.md/skills where safe.
- *AC:* every session can produce a dashboard; ≥1 suggestion is specific and applicable.

**7.15 Cost/speed optimizer**
- [ ] **Model routing/tiering:** cheap/fast model for mechanical subtasks (search, summarize, classify), strong model for reasoning; configurable policy; auto-escalate on failure.
- [ ] **Budget guardrails:** per-session/per-task $ ceiling with warn + hard stop; "budget mode" that trims context aggressively.
- [ ] **Cache transparency:** show cache hit rate and $ saved; structure prompts to maximize cache reuse.

**7.16 Power commands (parity + new)**
- [ ] Parity: `/compact`, `/rewind` (checkpoint/restore), `/clear`, `/cost`, `/init`, `/model`, `/resume`.
- [ ] New: `/goal` (set/track an explicit objective that anchors the session and the insights retro), `/context`, `/clean`, `/tidy`, `/budget`, `/insights`, `/route`.

**7.17 Subagents (user-delegated)**
- [ ] Delegate scoped tasks to child agents (own context window); results summarized back. Cheap model by default for fan-out. Distinct from the harness's own *system sub-agents* (7.19).

**7.18 ACP server (programmatic face)**
- [ ] Speak Agent Client Protocol (or compatible) so editors/external clients can drive the same core. Headless CLI mode for CI/scripting.

**7.19 System sub-agents (lifecycle framework)** *(new primitive — powers 7.20 and other wedges)*
- [ ] A framework for **built-in, specialized sub-agents** the harness runs at lifecycle hooks (distinct from user-delegated subagents in 7.17). Examples: `skill-expectation-analyzer`, `insights-writer`, `clean-matcher`, `tidy-reorganizer`, `router`.
- [ ] Common lifecycle the main agent drives: **init(scope)** (set up / establish contract) → **observe** (passive trace capture, no model calls) → **teardown(scope, state)** (main agent hands over the context slice; sub-agent analyzes and reports into `/insights`).
- [ ] **Opt-in & cost-bounded:** each is individually toggleable, off by default where it adds cost; runs on the cheap routing tier; can run async/batched (teardown / session-end / rollup) so it never blocks the interactive turn; respects budget guardrails.
- [ ] **Plugin architecture from day one:** the built-ins are *first-party plugins* on a public sub-agent interface (manifest + lifecycle hooks + declared scope/model/budget/permissions). Third parties can ship their own analyzers against the same API; we dogfood it so it stays capable. Interface lands in P1; marketplace distribution in 7.25. Manifest in Appendix C.5.
- *AC:* enabling/disabling a system sub-agent is one config line; a disabled analyzer adds zero token cost; an enabled one runs without slowing the interactive turn; a third-party plugin loads through the same registry as the built-ins.

**7.20 Living skills (skill performance from real sessions)** *(wedge #5)*
- [ ] **First form (Decision Log D7): the rediscovered-fact detector** — detect trial-and-error that lands on a concrete durable fact (command/path/config) and offer to save it to the relevant skill/memory. High precision, user-checkable; this is the part that ships first. The contract/budget grading below is demoted to a *later experimental rollup* once session volume exists.
- [ ] The `skill-expectation-analyzer` applies **predict-then-measure**: at skill load it establishes a contract (declared `expected_outcome` if present, inferred otherwise); at teardown it compares contract vs. actual trace and grades the result.
- [ ] Classifies each finding as **content gap**, **trigger failure**, or **friction**, each with a one-click remedy (patch skill body / fix skill `description` / prune-correct).
- [ ] Grounded: every claim cites turns, cost, and a transcript span with a jump-to link — never vibes.
- [ ] Closes the loop to skill authoring: a gap patches the existing skill or seeds a new one via skill-creator, with the real transcript attached as the first regression/eval case (real sessions become evals).
- [ ] Surfaced **per-session** (in `/insights`) and as a **cross-session rollup** ("skill X ran 4× over budget across 9 sessions; 3 facts it keeps rediscovering").
- [ ] **Teardown boundary:** target is a skill-declared `completion_signal` (frontmatter); v1 fallback is a heuristic (no skill tools called for N idle turns). This decides when the analyzer fires and how cleanly it attributes cost to the right skill.
- *AC:* for a session where a loaded skill underperformed, the analyzer produces a specific, grounded, applicable suggestion and a concrete skill diff.

**7.21 Brand & personality — "the Matrix layer"** *(P1 polish · on by default, one switch off)*
- [ ] Optional cosmetic theme giving Agent Smith a Matrix-franchise personality: themed thinking/loading lines ("entering the matrix…", "dodging bullets…", "following the white rabbit…", "there is no spoon…") and Matrix role-names for entities — the user as `Mr. Anderson`, the router as `The Keymaker`, the skill analyzer as `The Oracle`, insights as `The Architect`, background runners as `Sentinels`, system sub-agents collectively as `Agents`.
- [ ] **Confined to chrome.** Flavor lives only in spinners, the status line, entity display-names, empty states, and easter eggs — **never** in generated code, diffs, commit messages, file writes, error payloads, or ACP/headless output. Substance is always plain.
- [ ] **One switch off.** `serious_mode: true` mutes every pun/reference globally; `/serious` toggles it at runtime. Default: theme **on** in the interactive TUI (`serious_mode: false`); **off automatically** for ACP/headless/CI/async runs so programmatic output stays clean.
- [ ] Names and intensity are overridable (`intensity: full | subtle`; custom name map). Logo: a minimalist Agent mark (sunglasses, white shirt, black tie, earpiece) — Appendix B.
- *AC:* toggling serious mode removes all references with zero effect on behavior or output; no flavor text ever appears in code, commits, or programmatic responses; non-interactive faces default to clean.

### P2 — Production / async engine & scale

**7.22 Background/async runner**
- [ ] Fire-and-forget and scheduled runs; queue; resumable; hard budget ceilings; the "cheap optimized Claude/Codex for background tasks" use case.

**7.23 Auditable, replayable runs**
- [ ] Every run produces a structured, replayable log (inputs, tool calls, model, cost) for debugging and reproducibility; OpenTelemetry export.
- [ ] **Compliance archiving (premium · future).** Regulated orgs (healthcare, banking) must retain an immutable audit trail of AI-assisted work — the append-only block log (D3) already *is* that artifact. Premium layer adds: **tamper-evidence** (hash-chained events + signed manifests), **WORM retention** (e.g., S3 Object Lock), **BYO-bucket / data residency** (gzip + push to the customer's own storage/region/KMS — no infra for us to run), configurable retention, and an attestation/export format. Open-core line: the log + local viewing are OSS; the enterprise compliance layer is paid.
- [ ] **Hard tension to resolve before selling this:** immutability vs. right-to-erasure (GDPR/HIPAA). Logs contain PII/PHI/secrets, so "never break the log" fights "must erase a subject on request." Candidate answers: crypto-shredding (per-subject keys, destroy key to erase) and/or redaction-at-capture, plus legal-hold semantics. See §10 Q13.

**7.24 Cross-session analytics**
- [ ] Portfolio dashboard across sessions/projects: spend trends, recurring friction, "your top 3 ways to save money/time this week."

**7.25 Self-improving config**
- [ ] Aggregate insights into proposed edits to memory/skills/commands; the agent gets better at *your* workflow over time. (Living skills (7.20) is the first, highest-value instance.)

**7.26 Plugin marketplace, Desktop UI, team config** *(later)*
- [ ] Distribution + discovery for third-party sub-agent plugins (the *interface* itself ships in 7.19), commands, and skills; Desktop UI over ACP; shared/portable team config.

---

## 8. Out of Scope (for v1)

- **Training/fine-tuning models** — we are a harness, not a lab.
- **Desktop GUI** — deferred; ACP makes it additive later.
- **Team/enterprise** (SSO, RBAC, shared billing) — single-user first.
- **Non-coding verticals** — coding-focused to start.
- **Our own model** — always BYO provider key.
- **Full determinism** — we offer replayable/auditable runs, not bit-exact reproducibility (models are stochastic).
- **OS-level sandboxing & prompt-injection defense** (V1) — documented known limits; posture is "runs with your privileges; you approve actions" (Decision Log D9).
- **Cloud / hosted / team tier** (for now) — OSS local-first first; revisit monetization after adoption (Decision Log D8).

---

## 9. Dependencies & Risks

| Risk | Impact | Mitigation |
|---|---|---|
| **Provider API drift** (tool-call formats, caching semantics differ Anthropic↔OpenAI) | High | Strong provider abstraction; conformance tests per provider; treat normalization as core IP. |
| **`/clean` removes something still needed** | High | Preview before apply; everything reversible via off-window archive; never destructive. |
| **`/tidy` loses fidelity** (becomes another lossy compaction) | High | Tidy = restructure + dedupe, not summarize; keep originals; show a fidelity diff. |
| **Insights feel generic** ("be more specific") | Med | Ground suggestions in measured signals (token counts, re-reads, loops), not vibes; require ≥1 concrete, applicable item. |
| **ACP spec immaturity/instability** | Med | Abstract the protocol layer; ship headless JSON-RPC as fallback; track spec. |
| **Cost of the insights model call** undermines the cost story | Med | Run retros on a cheap model; make `/insights` opt-in / async / batched. |
| **Analysis sub-agents add cost/latency** | Med | Opt-in, off by default; cheap tier; async/batched at teardown, never inline; budget-capped per session. |
| **"Skill underperformed" judgments feel unfair** (contract ≠ user intent) | Med | Predict the contract at load-time (not hindsight); cite turns/cost/spans; offer an optional "what did you expect?" branch rather than guessing intent. |
| **"Just a thinner Claude Code"** perception | Med | Lead with the five wedges nobody else has; demo composition view + `/clean` first. |
| **Scope creep** (matching every feature of every agent) | Med | P0 = credible core; win on wedges, not feature parity. |

---

## 10. Open Questions

> Several are now resolved in the Decision Log (D-series) above — kept here for the trail. Still genuinely open: Q4 (`/clean` matching engine), Q5 (ACP vs. custom protocol), Q7 (discovery/evidence), Q12 (plugin trust depth), Q13 (immutability vs. erasure).

1. **Provider scope** — *resolved:* drop PI as a competitor; Ollama and other local models are covered by the OpenAI-compatible provider layer (cheapest/private tier).
2. **Wedge sequencing** — ship all five wedges together, or lead with one (I'd lead with **context composition + `/clean`** — most visceral demo, hardest for incumbents to copy quickly)?
3. **Context store design** — segment at the message level, or finer (sub-message spans)? Finer = better `/clean`, more complexity.
4. **`/clean` matching engine** — embeddings/semantic search over segments, a cheap-model classifier, or both?
5. **ACP vs. custom protocol** — commit to Agent Client Protocol, or ship a minimal JSON-RPC now and adopt ACP once stable?
6. **Pricing/positioning** — pure OSS tool, OSS + paid cloud async runner, or paid from day one?
7. **Evidence** — do we run a quick discovery pass (a handful of power-user interviews + our own session telemetry) before committing the roadmap?
8. **Skill contract source** — *resolved:* support both — declared `expected_outcome` in skill frontmatter when present, inferred at load-time as fallback (Appendix C).
9. **Insights cadence** — *resolved:* both per-session (`/insights`) and a cross-session rollup; the rollup is where project skills compound.
10. **Teardown trigger** — *resolved:* target a skill-declared `completion_signal` (Appendix C.1); ship a heuristic idle-turns fallback in v1.
11. **System sub-agent extensibility** — *resolved:* public plugin interface from day one (built-ins are first-party plugins on it); marketplace distribution later (7.25).
12. **Plugin trust/sandboxing** — third-party sub-agents see transcript context and can propose edits; what permission scopes + sandboxing do they need, and do they ever run untrusted code? (new — opened by the plugin decision.)
13. **Immutability vs. erasure** — the append-only log (D3) will hold PII/PHI/secrets, but compliance archiving (7.23) and GDPR/HIPAA demand erasure paths. How do we reconcile "never break the log" with "must be able to delete a subject"? (crypto-shredding via per-subject keys? redaction-at-capture? legal-hold semantics?) (new — opened by the compliance-archiving idea.)

---

## Appendix A — Command catalog (proposed)

| Command | Purpose | Status |
|---|---|---|
| `/goal` | Set/track the session objective; anchors insights | New |
| `/context` | Open the composition view | New (wedge) |
| `/clean "<topic>"` | Semantically remove matching context | New (wedge) |
| `/tidy` | Reorganize messy context into clean, labeled context | New (wedge) |
| `/insights` | Model-assisted session retrospective dashboard | New (wedge) |
| `/skills` | Skill performance report — per-session findings + cross-session rollup | New (wedge) |
| `/budget <$>` | Set cost ceiling / budget mode | New |
| `/route` | Inspect/override model routing policy | New |
| `/serious` | Toggle serious mode (mute the Matrix theme) | New |
| `/compact` | Lossy summarize (fallback when tidy isn't enough) | Parity |
| `/rewind` | Checkpoint / restore to an earlier state | Parity |
| `/cost` | Token + $ accounting | Parity |
| `/model` | Switch provider/model | Parity |
| `/resume` | Resume a saved session | Parity |
| `/clear`, `/init` | Reset / scaffold project config | Parity |

## Appendix B — Naming & logo
"Agent Smith" (working title) fits the Matrix theme and the "replicating, everywhere, efficient" vibe. Consider trademark/SEO before committing publicly.

Logo direction: a minimalist **Agent** mark — sunglasses, white shirt, black tie, coiled earpiece — that works as a square app icon and a horizontal wordmark. Evoke the *archetype*, not the actor (no character likeness — a trademark-safer path). Personality/theme spec in Appendix D.

## Appendix C — Living-skills contract & system sub-agent schemas

### C.1 Skill expectation contract (declared in skill frontmatter)
Skills may declare what they should accomplish and a rough budget. The `skill-expectation-analyzer` reads this at load-time; if it's absent, the analyzer infers an equivalent contract from the skill's stated purpose (declared-when-present, inferred-as-fallback).

```yaml
---
name: deploy-service
description: Ship a service to production at Acme (triggers on "deploy", "ship", "release").
expected_outcome:
  summary: Deploy a service to prod in one pass without rediscovering our pipeline.
  effort_budget:
    tool_calls: 3            # soft target for the in-scope span
    turns: 2
    max_cost_usd: 0.15       # optional soft ceiling
  should_not_rediscover:     # facts the skill already encodes; rediscovery ⇒ content gap
    - deploy command (make ship)
    - staging approval gate
    - rollback procedure
  success_signals:           # optional; how we know it worked
    - "`make ship` exited 0"
    - no user correction about the deploy steps
completion:                  # when does this skill's span end? (drives analyzer teardown)
  signal: "`make ship` exited 0"   # (c) declared — preferred
  idle_turns: 3                    # (b) heuristic fallback if no signal is declared/fires
---
```

### C.2 Analyzer output (one finding per skill activation)

```yaml
skill: deploy-service
verdict: underperformed       # helped | no_op | underperformed | should_have_loaded
score: 0.42                   # predicted budget ÷ actual cost, normalized (1.0 = on budget)
classification: content_gap   # content_gap | trigger_failure | friction
evidence:
  turns: [12, 17]
  cost_usd: 0.34
  rediscovered: migration command (make db-migrate)   # found the hard way
remedy:
  type: patch_skill           # patch_skill | fix_description | prune | new_skill
  diff: "+ Migrations run via `make db-migrate` (never edit schema.sql directly)."
  eval_seed: session://refactor-auth#turns=12-17       # real transcript attached as a regression case
```

### C.3 System sub-agent configuration (opt-in, cost-bounded)

```yaml
subagents:
  skill_expectation_analyzer:
    enabled: false            # opt-in — adds cost
    model: cheap              # routing tier
    schedule: teardown        # teardown | session_end | rollup
    mode: async               # never blocks the interactive turn
    budget:
      max_cost_usd_per_session: 0.05
  insights_writer:
    enabled: true
    model: cheap
    schedule: session_end
```

### C.4 The lifecycle the main agent drives
`init(scope)` → establish or load the contract · `observe()` → passive trace capture from the segmented context store (no model calls) · `teardown(scope, state)` → analyze contract-vs-actual, classify, and report findings into `/insights`. Opt-in per sub-agent; async/batched so the interactive loop stays fast. Teardown fires on the watched skill's `completion.signal` (preferred) or the `idle_turns` heuristic (v1 fallback).

### C.5 Sub-agent plugin manifest (built-ins are first-party plugins)
Every system sub-agent — first-party or third-party — declares itself the same way. The built-ins ship in-tree but load through this registry, so the public interface stays capable (we build on it ourselves).

```yaml
name: skill-expectation-analyzer
kind: system-subagent
hooks: [on_skill_load, on_skill_complete]      # lifecycle points it binds to
scope: skill_span                               # context slice it receives at teardown
model: cheap
enabled_by_default: false                       # opt-in
budget:
  max_cost_usd_per_session: 0.05
emits: [insights_finding, proposed_skill_diff]
permissions: [read_transcript, propose_edit]    # never writes without confirm
```

Third-party examples on the same interface: a `security-reviewer` that runs at teardown over changed files, a `test-coverage` analyzer, or a company `compliance-checker`. Permission scopes + sandboxing for untrusted plugins are an open question (§10.12).

## Appendix D — Personality config & naming map ("the Matrix layer")

On by default in the TUI; auto-disabled for programmatic faces (ACP/headless/CI/async). Flavor is confined to chrome — never code, commits, or machine-readable output.

```yaml
personality:
  theme: matrix          # matrix | none
  serious_mode: false    # true => no puns/references anywhere (the kill switch)
                         #   default: false in TUI, true for ACP/headless/CI/async
  intensity: full        # full (renaming + flavor lines) | subtle (status/loading only)
  names:                 # override any role label
    user: Mr. Anderson
    router: The Keymaker
    skill_expectation_analyzer: The Oracle
    insights_writer: The Architect
    background_runner: Sentinels
    system_subagents: Agents
```

Sample themed status lines (rotate while working): "entering the matrix…", "dodging bullets…", "following the white rabbit…", "there is no spoon…", "asking the Oracle…", "bending the spoon…". With `serious_mode: true`: plain "Thinking…", "Analyzing…", "Running…".

Runtime: `/serious` flips it mid-session — the kill switch is total, one line, no residual flavor.

> Note: "The Matrix", its characters, and likenesses are Warner Bros. IP. Evoking the *archetype* (suited agent, sunglasses) for a personal/fun project is low-risk; revisit names and the mark before any public or commercial release.
