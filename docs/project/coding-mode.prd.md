# Agent Smith — Coding Mode (SDLC mode)

> Status: **conclusions from a design grilling (2026-06-17)**
> Scope: an opinionated, process-driven *working mode* for building a feature —
> think → analyse → plan → implement → verify → refactor → reflect → loop —
> read against [PRD.md](PRD.md), [TUI-UX.md](TUI-UX.md), and [CLI-UX.md](CLI-UX.md).
> Owner: Toni

This document records the decisions for **Coding Mode** (working title; also
"SDLC mode" / "feature mode"): a structured alternative to free-form vibe-coding
chat, where Smith acts as an opinionated advisor that guides the user through a
repeatable feature-building process and produces real artifacts (gap analysis,
PRD/ticket drafts, plan, verification) along the way.

It complements the PRD Decision Log (D0–D9) and does **not** override it. Coding
Mode is **fast-follow at the earliest** (it composes capability-layer features
that D6 defers — see D-CODE-7). Additive-only discipline (D2) applies to every
new event/block type, config key, and command this introduces.

---

## Why this exists (the problem, stated honestly)

Free-form chat with a coding agent collapses into vibe-coding: jump straight to
implementing, skip framing, skip verification, and discover the gaps after the
code is written. The user (and most users) get a worse outcome not because the
model is weak but because **the process is unstructured**. Coding Mode bets that
a *soft, opinionated process* — surfaced by the harness, not enforced by it —
produces better features without taking the wheel away from a senior developer.

This is **not** one of the PRD's five wedges (D1: the moat is provider-neutrality
+ the open data substrate). Coding Mode is a **UX/orchestration layer on top of
existing primitives** — it earns its place by making the substrate *useful*, not
by being the moat. We say this out loud so it is never silently treated as a
core differentiator (D0).

---

## Decision Log — Coding Mode (D-CODE-N)

Locked from the design grilling. Where this conflicts with the loose narrative
below, this wins.

**D-CODE-1 · Thin core, real mode *feel*.** Coding Mode is **not** a new engine.
Mechanically it is: a custom command that *enters* a mode, a process definition
(the phase sequence), a bundled skill pack, and a distinct TUI presentation. It
reuses `/goal` (AS-040), skills (AS-034), custom commands (AS-033), system
sub-agents (AS-044), and `/insights` (AS-045) rather than reimplementing them.
But entering it must **feel like crossing a threshold** — a different layout, a
visible phase tracker, a changed status line — not just a chattier prompt
(D-CODE-4).

**D-CODE-2 · Soft advisor, never a gate.** Smith *suggests* the next phase,
grills, surfaces gaps and side effects, and nudges — but **never blocks**. A
senior dev can skip a phase, jump back, or ignore the process entirely with one
command. No phase is a hard precondition for another. This is the make-or-break
UX decision: gates that refuse to implement until a plan is "approved" make the
tool rage-quit material. Opinionated ≠ obstructive.

**D-CODE-3 · Phases are derived blocks over the append-only log (D3).** Mode
state — current phase, phase transitions, the artifacts produced — is recorded
as **new optional block/event types appended to the session log**, never as
mutable side-state. A phase transition is an event; the "current phase" is a
projection over those events. This makes mode history auditable and reversible
for free (consistent with D3) and keeps the addition additive-only (D2).

**D-CODE-4 · A distinct presentation, reusing the panel framework.** Coding Mode
renders as an inspect-mode-style shell (AS-067 panel framework, D-TUI-3): a
pinned **phase tracker** (think · analyse · plan · implement · verify · refactor
· reflect, current one highlighted), the active goal, and the artifacts produced
so far. Work still happens in the transcript; the chrome is what changes. For
ACP/headless faces, the mode degrades to plain phase-tagged events — flavor and
layout live only in the TUI (consistent with the PRD's "confined to chrome").

**D-CODE-5 · Opinions are layered, default → overridable.** The opinionated
method has three layers, most-specific wins:
1. **Baked-in house method** — Smith ships a default opinionated SDLC process
   (the seven phases, the grilling stance, the "no implement before a sketch"
   nudge). This is the out-of-box opinion.
2. **A shipped process skill pack** — the phase behaviors are *skills*
   (`grill-gaps`, `find-side-effects`, `plan-review`, `verify-checklist`, …)
   that Smith auto-invokes per phase. Swappable and individually improvable.
3. **Project memory** — `CLAUDE.md` / `AGENTS.md` / `AGENT.md` (AS-032) can
   override or extend the method per project (e.g. "this repo requires a ticket
   before any code", "skip refactor phase", custom phase order).

   So the advisor is opinionated by default, but the opinions are *somebody's*
   (Smith's, then the project's) and can be customised — not vibes (D-CODE-8).

**D-CODE-6 · Skills auto-enable; the user never pulls them.** Per phase, Smith
loads the relevant bundled skills itself (this is just the existing skills model
— AS-034 invokes on match; the process simply declares which skills belong to
which phase). The user does not manage a skill list to use Coding Mode. Bundled
process skills ship *with* Smith so there is nothing to install.

**D-CODE-7 · Success-analysis is cut from this PRD.** "Analyse user behaviour to
judge whether a shipped feature succeeded" is a **different product**: it needs
the user's deployed app's runtime telemetry, which a coding harness cannot see
(it would require an instrumentation SDK, an event pipeline, dashboards — i.e.
Amplitude/PostHog, not Smith). What Coding Mode *can* honestly do lives in the
**reflect** phase: help the user *write* a success metric, scaffold the
instrumentation code, and file a check-back ticket — Smith produces **artifacts**,
never reads runtime data. The grander "watch users, judge success" vision is
out of scope and documented as such (D0), to be revisited as its own exploration
if ever.

**D-CODE-8 · Grounded, not preachy.** When Smith grills or flags a gap/side
effect, it cites the concrete thing — the file, the function, the missing test,
the ticket — never "consider best practices." Same discipline the PRD demands of
`/insights` (§9): claims point at evidence. An advisor that can't point at the
code loses trust fast.

---

## The process (default house method)

Seven phases, each with a stance and the artifact it tends to produce. The order
is the default; projects can reorder/skip (D-CODE-5.3), and the user can jump
freely (D-CODE-2).

| Phase | Smith's stance | Typical artifact |
|---|---|---|
| **think** | Clarify the actual goal and constraints; set `/goal`. Ask what success looks like. | a stated goal + open questions |
| **analyse** | Read the relevant code; grill the idea; surface gaps, prior art, and side effects *before* planning. | a gap/impact note |
| **plan** | Propose an opinionated approach; name the refactors that would make it cleaner; call out risk. | a plan (and PRD/ticket drafts where warranted) |
| **implement** | Do the work in scoped steps, staying anchored to the plan and goal. | the diff |
| **verify** | Run tests/build/quality gate; confirm the goal is met; show evidence. | passing checks / a verification note |
| **refactor** | Offer the cleanups the implementation revealed (opt-in, never forced). | a follow-up diff or a ticket |
| **reflect** | What was learned; durable facts worth saving (→ living skills, AS-048); how we'd *measure* success and a check-back ticket (D-CODE-7). | an insights note + saved facts + a check-back ticket |
| → **loop** | Reflection feeds the next think. | — |

Entry: `/feature "<prompt>"` (or `/mode coding`) sets the goal, enters the mode,
and starts at **think**. Exit: `/mode off` (or Esc out) leaves the mode; the
session and its phase history remain in the log.

---

## What we are NOT building (scope discipline)

- **No hard gates / approval walls** (D-CODE-2).
- **No runtime product analytics / success measurement of shipped features**
  (D-CODE-7) — artifacts only.
- **No new agent engine, planner, or state machine subsystem** — phases are
  derived blocks over the existing log (D-CODE-3); behaviors are existing skills.
- **No skill marketplace / install step for the process skills** — they ship
  in-tree (D-CODE-6).
- **Not a V1 feature** — composes capability-layer pieces that D6 defers.

---

## Dependencies & build order

Coding Mode is an orchestration layer; it needs its parts to exist first.

| Needs | Ticket | State |
|---|---|---|
| Custom slash commands | AS-033 | ready |
| Skills loading (auto-invoke) | AS-034 | ready |
| Memory files (opinion overrides) | AS-032 | ready |
| `/goal` (session objective) | AS-040 | done |
| TUI panel framework (phase tracker) | AS-067 | done |
| System sub-agents (phase analyzers) | AS-044 | ready |
| `/insights` (reflect phase) | AS-045 | ready |
| Living-skills / rediscovered-fact detector (reflect) | AS-048 | needs clarification |

Earliest sensible slot: after the capability wave (tickets README build order
step 6–7). It is fast-follow, and the thin version (command + process skills +
phase tracker, soft) can ship before the heavier analyzers (AS-044/045/048),
which only enrich the **analyse** and **reflect** phases.

---

## Proposed tickets (to be filed if this PRD is accepted)

Continuing the AS-NNN sequence; final numbers assigned at filing. These are the
honest decomposition of D-CODE-1…8 — file them rather than carrying them as
TODOs (CLAUDE.md ticket discipline).

1. **Coding-mode shell + `/feature` / `/mode` entry/exit** — the mode lifecycle,
   phase-as-derived-block schema (D-CODE-3), soft transitions (D-CODE-2).
   *depends_on:* 033, 040, 005/006.
2. **Phase tracker panel + mode presentation** — the distinct TUI feel
   (D-CODE-4). *depends_on:* 067, ticket 1.
3. **Process skill pack (bundled)** — `grill-gaps`, `find-side-effects`,
   `plan-review`, `verify-checklist`, etc., wired to phases (D-CODE-5.2, D-CODE-6).
   *depends_on:* 034, ticket 1.
4. **Project-level method override** — read process customisation from memory
   files (D-CODE-5.3). *depends_on:* 032, ticket 1. **Shipped (AS-075):** a
   project reorders/skips/extends the phase sequence by embedding a fenced
   ```` ```smith-method ```` block carrying a `phases:` list in any memory file
   (`CLAUDE.md`/`AGENTS.md`/`AGENT.md`); resolution is declarative (no code runs),
   most-specific memory wins, and a malformed directive degrades to the house
   default.
5. **Reflect-phase artifacts** — success-metric/instrumentation scaffolding +
   check-back ticket generation (D-CODE-7); hook into `/insights` and the
   rediscovered-fact detector. *depends_on:* 045, 048, ticket 1.

---

## Open questions

1. **Mode vs. command naming** — `/feature` (task-shaped) vs. `/mode coding`
   (mode-shaped) vs. both? Does "mode" generalise (a future "review mode",
   "debug mode") or is this the only one? If it generalises, the entry/exit and
   phase-tracker mechanics should be built as a reusable *mode* primitive.
2. **Phase advancement trigger** — what nudges think→analyse→…? Pure user
   command, a model judgement ("looks like we're ready to plan — proceed?"), or
   a signal (goal set / tests green)? Soft means *never auto-advance without a
   yes*, but the prompt for that yes still needs a trigger.
3. **PRD/ticket drafting depth** — in the plan phase, how far does Smith go in
   producing PRD/ticket drafts? A scratch note, or actual `AS-NNN` files synced
   via `cmd/ticket-sync`? The latter is powerful but couples Coding Mode to this
   repo's specific ticket workflow.
4. **Multi-feature / interleaving** — can two features be in-flight in one
   session, each with its own phase state? Or is the mode single-goal at a time?
5. **Headless behavior** — does Coding Mode mean anything in `smith run` / ACP,
   or is it strictly a TUI experience? (D-CODE-4 leans TUI-only; confirm.)
