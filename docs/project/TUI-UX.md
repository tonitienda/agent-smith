# Agent Smith — TUI UX

> Status: **conclusions from a design grilling (2026-06-15)**
> Scope: the interactive terminal face — Bubble Tea wiring, panels, focus,
> liveness, trust surfaces — read against [docs/UX.md](../UX.md), [PRD.md](PRD.md),
> [clig.dev](https://clig.dev/), and [Bubble Tea](https://github.com/charmbracelet/bubbletea).
> Owner: Toni

This document records the decisions for how the flagship TUI *behaves and is
wired*. [docs/UX.md](../UX.md) owns the product UX direction and the locked
decisions (§22); this pins the **interaction mechanics and Bubble Tea
architecture** UX.md leaves open, and resolves UX.md §23 Q2 and Q6.
[CLI-UX.md](CLI-UX.md) owns the command-line contract; the two faces share one
command registry (D-CLI-10).

It complements UX.md §22 and the PRD Decision Log (D0–D9); it does not override
them. Additive-only discipline (D2) applies to view models and event types too.

---

## Decision Log — TUI (D-TUI-N)

**D-TUI-1 · Async work reaches `Update` via a goroutine + event→Msg pump.** The
agent loop runs in a goroutine; the TUI subscribes to the face-neutral event
stream and a pump turns each event into a `tea.Msg`. `Update` only renders and
emits user *decisions* — it never calls a provider, tool, or cost code directly
(UX.md §2.6 / §18). Already realized by AS-018's `UIEvent` stream and AS-021.

**D-TUI-2 · Composed sub-models.** A root `Model` composes sub-models —
transcript, status line, input, and each panel — each with its own
`Update`/`View`, rendering the face-neutral view models (`TranscriptItemView`,
`ToolCallView`, `ContextDashboardView`, … UX.md §18.2). No god-object; this
scales to `/context`, `/agents`, diff review.

**D-TUI-3 · Two attention modes; inspect panels are full-screen swaps with a
pinned status line.** Work mode is the transcript shell. Inspect mode
(`/context`, `/diff`, `/agents`, `/resume`) takes the whole screen, with the
status line pinned so the user never loses what's running; Esc returns to work
mode. Optimized for the ≤100-col target (UX.md §4.1/§4.3); split/side panels stay
deferred to wide terminals (UX.md §4.2), but panels are modeled as reusable views
so a side-by-side host can render them later without a rewrite.

**D-TUI-4 · Panels open by slash command *and* hotkey.** `/context`, `/diff`,
etc. are the canonical, discoverable path (parity with the headless subcommands,
one registry). Common panels also get a modifier hotkey for daily speed. Because
the prompt is always-typing (D-TUI-7), those hotkeys are **modifiers**
(PgUp / ctrl- / alt- / a leader chord), never bare letters.

**D-TUI-5 · Streaming renders plain, styles on finalize.** Assistant tokens
render as raw/plain text as they arrive; markdown styling (glamour) is applied
only when the message finalizes. Cheap per token, no reflow jitter, and code
stays copyable mid-stream — at the cost of a brief "plain→styled" settle at the
end. (Bubble Tea redraws the whole view per `Msg`, so per-token re-styling is the
expensive path to avoid.) Already the behavior in AS-021.

**D-TUI-6 · Follow-tail, pause on scroll-up.** Auto-scroll while pinned to the
bottom; the moment the user scrolls up, following stops and a "new output ↓"
marker appears; a jump-to-bottom re-attaches. `bubbles/viewport` backs this;
AS-021 already sticks to the bottom only when already there.

**D-TUI-7 · Always-typing prompt focus.** Keystrokes always go to the input;
scrolling and panel hotkeys use modifiers (no vim-style insert/normal modes).
Familiar to Claude Code / chat users, no mode confusion, bare letters never
become commands. (Consequence: D-TUI-4 hotkeys are modifiers; in-TUI help is
`/help` or a modifier chord, since `?` is just typed text.)

**D-TUI-8 · Permissions: inline card, modal for destructive.** A normal
approval-needing action pauses the run and presents an **inline transcript card**
the user navigates to — so they can scroll context before deciding. A
**destructive or broad-scope** action escalates to a **blocking modal overlay**:
focus trapped until decided, severe styling, can't be misclicked past. Either
way the exact command/path/scope is shown verbatim — never a paraphrase (UX.md
§11). Refines AS-024 (which previously said "modal" for all prompts).

**D-TUI-9 · Subagents: collapsed line, expand on demand.** Each subagent shows as
one collapsed line (name · role · current action); expand to see its tool calls.
The orchestrator posts the final summary inline. Live nested streaming of every
worker is rejected as too noisy for multi-agent runs. V1 has one visible actor,
but the event/render model carries attribution from the start (UX.md §14).
Resolves UX.md §23 Q2.

**D-TUI-10 · Startup header: on by default, fast.** A small ASCII header
(project / model / mode, UX.md §5.2) renders on every launch — no model call, no
delay. `--no-splash` and serious-mode hide it. PRD 7.21: personality is on by
default in the TUI (and only the TUI). Resolves UX.md §23 Q6.

**D-TUI-11 · Degrade gracefully below layout size.** When the terminal is smaller
than the layout wants, best-effort render: drop the status line / truncate panels
rather than blocking on a "terminal too small" screen. Driven by
`WindowSizeMsg`; never glitch (AS-021 AC).

**D-TUI-12 · Liveness lives in chrome only.** Spinners and elapsed timers appear
in the status line and on the active tool card — never animating over transcript
text, code, or diffs, which stay still and copyable (UX.md §2.1). Mouse is off in
V1, so terminal-native selection keeps copy/paste working (UX.md §19); the
architecture doesn't preclude mouse later.

---

## 1. Modes & layout (V1, ≤100 col)

```text
work mode                          inspect mode (full-screen swap)
┌────────────────────────────┐     ┌────────────────────────────┐
│ transcript (follow-tail)   │     │ status line (pinned)        │
│  · streaming text          │     ├────────────────────────────┤
│  · tool cards              │     │ /context · /diff · /agents  │
│  · inline permission card  │     │  (full-screen panel body)   │
├────────────────────────────┤     │                            │
│ status line + spinner      │     │                            │
├────────────────────────────┤     ├────────────────────────────┤
│ prompt (always-typing)     │     │ panel keybar · Esc = back   │
└────────────────────────────┘     └────────────────────────────┘
            │  /context or hotkey  →
            ←  Esc                 │
```

A **blocking modal** overlays either mode for destructive permission prompts
(D-TUI-8).

## 2. Keymap (direction; exact bindings settled in AS-067)

| Key | Action |
|---|---|
| `Enter` | submit prompt (locked, UX.md §22.8) |
| `Alt+Enter` | newline in prompt (locked) |
| `Esc` | close panel/modal → cancel in-flight turn → clear input (locked, UX.md §9.2) |
| `PgUp`/`PgDn`, modifier+`j`/`k` | scroll transcript (detaches follow-tail) |
| modifier / leader + letter | open a panel (`/context`, `/diff`, …) |
| `/<name>` | open panel / run command via the palette |

Bare letters are always literal input (D-TUI-7). Mouse: not V1.

## 3. Bubble Tea architecture

```text
loop goroutine ──UIEvent──▶ event→Msg pump ──tea.Msg──▶ root Model.Update
                                                          │ composes:
                                                          ├─ transcript sub-model
                                                          ├─ status-line sub-model
                                                          ├─ input sub-model
                                                          └─ panel host (inspect mode)
Update renders view models + emits user decisions only — no provider/tool/cost calls.
```

This is the §18.1 layering: the TUI is a *renderer* of the face-neutral registry
and view models; the same registry/events feed the headless CLI (CLI-UX.md §4).

## 4. How this maps to tickets

- **AS-021 (done)** — already implements D-TUI-1, -2 (partial), -5, -6, -11.
- **AS-024 (ready)** — tool cards + diff review + permissions; updated for D-TUI-8
  (inline card, modal only for destructive).
- **AS-026 (ready)** — `/context` is the first inspect panel; renders inside the
  AS-067 panel host.
- **AS-067 (done)** — the generic inspect-mode panel framework: full-screen panel
  host with pinned status + Esc-to-return, `Ctrl+G` leader-hotkey routing,
  reusable modal-overlay infra, startup header (`--no-splash`), and status-line
  graceful degrade. D-TUI-3, -4, -7, -8 (modal), -10, -11.
- **AS-053** — Matrix layer / `/serious`; this doc only fixes the *default*
  (header on in TUI).

## 5. Open questions (small, non-blocking)

1. Exact hotkey/leader assignments per panel — settle in AS-067.
2. In-TUI help trigger (`/help` vs a fixed modifier chord) — lean `/help` plus a
   chord.
3. Minimum-size thresholds for D-TUI-11 — pick during AS-067 implementation.
4. Default-collapsed depth for nested subagents once orchestration is real
   (UX.md §23 Q3 territory) — revisit when `/agents` lands.
