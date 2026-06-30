# Agent Smith — Decision Log (v0.3)

> **This is product truth. Read it first.** Where it conflicts with the rest of
> [PRD.md](PRD.md), **this wins.** The PRD's sections 1–10 are the original
> exploration; the items below are the locked decisions from a design
> stress-test.

**D0 · Intent.** Built to learn, but treated as a *shippable product*. Genuinely hard problems may be punted — but only as **explicitly documented known compromises**, never silently.

**D1 · Moat.** Not the features — **provider-neutrality + an open, stable data substrate**. Incumbents keep the session/transcript as a private, churning internal artifact so nothing can be built on it; Agent Smith publishes *all* session data behind an **additive-only API/schema that never breaks**. Context features (`/context`, `/clean`, …) are *acquisition* (they win the demo); neutrality + open data are *retention* (incumbents structurally can't match them). OSS transparency reinforces both.

**D2 · Schema discipline.** **Additive-only from V1, forever** — no removals, no repurposing, no deprecation windows, no breaking changes. New concepts = new optional fields/records; consumers tolerate missing/unknown.

**D3 · Core data model.** The session is an **append-only, immutable event log of content blocks** (text / tool-call / tool-result / file-read / reasoning; stable ID). The model-facing **context is a *projection* over the log**, not stored state. `/clean`, `/tidy`, `/compact`, `/rewind` append exclusion or derived-block events (with provenance); they never mutate history. Reversibility and auditability become structural, and additive-only becomes natural. (Resolves §10 Q3.)

**D4 · Polyglot schema.** The immutable block schema is modeled as the **union/superset of mainstream agent/provider wire formats, designed up front** — starting with Anthropic and OpenAI, and explicitly surveying public surfaces from mainstream coding agents such as xAI/Grok Build (OpenAI-compatible Responses API, headless streaming JSON, MCP-facing events), Codex/Gemini/Cursor/Cline/Aider where stable formats exist. The spike classifies each surface as schema input, compatibility note, or out of scope before the schema is frozen — so provider #2 or a mainstream agent import/export never forces a breaking change.

**D5 · Cost/speed = criterion, not claim.** "Cheaper/faster than a *naive* harness on the same model" is an **internal design criterion + guardrail metric**, measured on a benchmark suite — *not* a marketing promise. External positioning leads with control, observability, and neutrality. (Supersedes the "30–50% cheaper" headline in §6.)

**D6 · V1 = thinnest thesis slice.** Ships: 2 providers (Anthropic + OpenAI) · event-log + projection core · basic agentic loop with file/shell tools · TUI · `/context` + `/clean` · the permission model (D9). **Deferred to fast-follow:** `/tidy`, `/insights`, living-skills, model routing, budgets, MCP, hooks, skills, subagents, ACP, async runner, personality/Matrix layer.

**D7 · Living-skills = scalpel, not courtroom.** The first form (post-core) is the **rediscovered-fact detector**: spot trial-and-error → a concrete durable fact (command/path/config) → offer to save it to the relevant skill/memory. Budget/contract scoring is demoted to a later *experimental* rollup signal once session volume exists.

**D8 · Business & license.** **OSS-first, Apache-2.0.** Monetization deferred (no cloud infra now); leading future candidate is **premium sub-agents** on the public plugin interface. A second candidate falls out of the architecture for free: **compliance-grade session archiving** for regulated orgs (healthcare/banking) — tamper-evident, retained, exportable to *their* storage — because the append-only log (D3) is already the audit artifact (see 7.23). Any cloud/async/team tier waits until the OSS tool has users. (Resolves §10 Q6.)

**D9 · Security posture.** Build now: permission/approval model (ask / allowlist / auto) + OS-keychain key storage. **Punted as documented known limits:** OS-level sandboxing, prompt-injection defense, plugin code-sandboxing (V1 sub-agents are first-party; third-party plugins, when they arrive, are *declarative-only* — manifest + prompt, no arbitrary code). Stated posture: *"Agent Smith runs with your privileges in your environment; you approve actions. It is not a sandbox."* This posture also settles the hosted-agent question (AS-080 spike, [docs/design/hosted-agent-sandboxing.md](../design/hosted-agent-sandboxing.md)): `smith serve` is supported for **local** use only; **hosting a live, stranger-driven agent is out of scope** because it would require *being* a sandbox, which D9 declines. The public web demo is the read-only inspector (AS-079), not a hosted live agent.
