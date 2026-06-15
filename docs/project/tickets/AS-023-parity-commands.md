---
id: AS-023
title: "Parity commands: /clear, /model, /resume"
status: done
github_issue: 23
depends_on: [AS-007, AS-008, AS-022]
area: commands
priority: P0
source: PRD.md §7.16, Appendix A, D6
---

# AS-023 · Parity commands: /clear, /model, /resume

**Status: ready to implement**

## Description

The V1 subset of parity power commands (D6: "a subset of 7.16"). `/cost` ships with AS-020; `/compact`, `/rewind`, `/init`, `/goal`, `/budget`, `/route` are fast-follow.

- **/clear** — end the current session and start a fresh one (new log). The old session remains on disk and resumable; nothing is deleted (append-only ethos).
- **/model** — list configured providers/models; switch mid-session. The switch is recorded as an event in the log so cost attribution and the transcript stay accurate. Works across providers (Anthropic ↔ OpenAI) thanks to the normalized block schema.
- **/resume** — list recent sessions for this project (title, age, cost, size) and load one; also `smith --resume <id>` from the CLI.

## Acceptance criteria

- [x] `/clear` starts a clean context; the previous session appears in `/resume`.
- [x] `/model` switches provider mid-session and the next turn uses the new model; the event log records the switch.
- [x] A session started on Anthropic resumes and continues on OpenAI without transcript corruption (the polyglot-schema payoff, D4 — test this explicitly).
- [x] `/resume` restores a session whose projection is identical to its last live state.

## Dependencies

- AS-007 (persistence), AS-008 (per-request model selection), AS-022 (command framework)

## Implementation notes

- `cmd/smith/controller.go` holds a `chatSession` controller that owns the mutable
  per-session state (active provider/model, current log) and presents it to the TUI
  through three stable seams — `Run` (turn driver), `Meta` (status-line identity),
  `Meter` (context gauge) — so the parity commands can swap provider/model or the
  whole session without `internal/tui` learning about the provider/tool/session
  wiring (the AS-021 boundary). A switch rebuilds the engine over the (possibly new)
  log, re-using the same UIEvent observer.
- `/model` resolves the provider for a target model from the pricing table's
  `Vendor`, so it switches across providers (Anthropic ↔ OpenAI) and records an
  `eventlog.KindModelSwitch` control event (new, harness-only, never projected) on
  the log. `/clear` and `/resume` swap the active log; `command.Output.ResetView`
  asks the face to clear the transcript. `tui.MetaFunc` (parallel to `MeterFunc`)
  re-reads the status-line identity each refresh.
- `smith --resume <id>` resumes from the CLI; a resumed session restores the model
  it last used so its window/cost meter matches.
- Punted to AS-064: an interactive `/resume` picker (arrow-key select) and
  transcript rehydration on resume. Today `/resume` lists sessions and loads by ID,
  and the restored transcript starts fresh (the engine's projection is exact; the
  on-screen scrollback is not replayed).
