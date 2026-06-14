# ADR-0001 — TUI framework: Bubble Tea + Lipgloss (AS-021)

> Status: **accepted** · Scope: the flagship interactive face (PRD §7.8, D6) · Date: 2026-06-14

## Context

AS-021 builds the first interactive face for Agent Smith: a streaming chat over
the agentic loop (AS-018). It needs incremental output rendering, a multi-line
input editor with history, a status line, scrollback, and clean cancellation —
all in a terminal, all reacting to the loop's face-agnostic `UIEvent` stream.

The face must stay a thin renderer: it consumes only `loop.UIEvent` and drives
turns through a narrow `Runner` seam, importing no provider or tool package (the
normalization IP lives behind the provider interface, PRD §9). The framework
choice should not leak business concepts into the UI or vice versa.

## Decision

Use **[Bubble Tea](https://github.com/charmbracelet/bubbletea)** (the Elm-style
Model/Update/View runtime) with **[Lipgloss](https://github.com/charmbracelet/lipgloss)**
for styling/layout, **[Bubbles](https://github.com/charmbracelet/bubbles)** for
the stock `textarea` (multi-line input), `viewport` (scrollback), and `spinner`
components, and **[Glamour](https://github.com/charmbracelet/glamour)** to render
final assistant messages as markdown.

This is the de-facto Go TUI stack and the one the ticket recommends. The
alternatives considered:

- **[tview](https://github.com/rivo/tview)** / **[tcell](https://github.com/gdamore/tcell)** —
  mature and widget-rich, but an imperative, callback-driven model. Bubble Tea's
  message-passing `Update` loop maps directly onto our external `UIEvent`
  stream: each loop event becomes a `tea.Msg`, so the face has one funnel for
  both user input and loop progress.
- **Hand-rolled ANSI over `golang.org/x/term`** — keeps the dependency surface at
  zero, but re-implements resize handling, wrapping, scrollback, and input
  editing that Bubbles already solves. The repo's "stdlib-only" rule is scoped to
  **repo tooling**; the user-facing binary may take dependencies, and this ticket
  explicitly introduces them.

## How `UIEvent`s reach the UI

The loop calls its `Observer` inline on the goroutine driving a turn. The TUI
exposes `App.Observer()`, a `loop.Observer` that forwards every event onto a
buffered channel. A long-lived `tea.Cmd` blocks on that channel and re-arms
itself after each event, turning loop progress into `tea.Msg`s the `Update` loop
folds into the transcript. A turn runs in its own `tea.Cmd`; its `context` is
cancelled when the user presses Esc, so cancellation is cooperative and the
session stays usable.

This keeps the seam one-directional and face-agnostic: `cmd/smith` wires the
observer into the engine, the engine knows nothing about Bubble Tea, and the
`internal/tui` package imports neither `internal/provider` nor `internal/tool`
(enforced by a test).

## Consequences

- The binary gains the Charm dependency tree (Bubble Tea, Bubbles, Lipgloss,
  Glamour and their transitive deps). This is the first non-stdlib dependency in
  a user-facing command; `go.mod`/`go.sum` grow accordingly.
- Interactive launch requires a real terminal; `smith` falls back to printing
  usage when stdin/stdout is not a TTY, so non-interactive invocations (scripts,
  CI, `make test`) are unaffected.
- Permission prompts, diff review, and tool transparency are **out of scope** for
  this skeleton — they land in AS-024 and build on the same event stream. The
  Matrix personality layer (status-line styling/voice) is fast-follow (D6,
  AS-053); the status line here is plain text.
