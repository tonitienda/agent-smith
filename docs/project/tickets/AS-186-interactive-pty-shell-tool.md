---
id: AS-186
title: Interactive PTY-driven shell tool ("full terminal use")
status: Pending Debrief
github_issue: null
area: tools
priority: P2
depends_on: [AS-015, AS-016]
source: docs/project/competitors.md
---

# AS-186 · Interactive PTY-driven shell tool ("full terminal use")

## Description

Smith's shell tool (AS-015) spawns a command, captures stdout/stderr, and
returns when it exits. That covers the overwhelming majority of tool calls,
but it cannot drive anything that expects **interactive stdin mid-session** —
a database shell (`psql`, `mongosh`), a debugger (`dlv`, `pdb`), an
interactive migration or scaffolding prompt, or any REPL. Today the agent
either can't use these tools at all, or has to fall back on brittle
one-shot `-e`/`-c` flag equivalents when the tool even offers one.

Warp open-sourced "Full Terminal Use" in April 2026: the agent reads the
live terminal buffer at the PTY level and can respond to interactive
prompts as they appear, rather than only spawn-and-capture. This ticket
scopes an equivalent capability for Smith: a PTY-backed shell tool mode
that lets the agentic loop see incremental output and send follow-up input
to a still-running process, gated by the existing permission model (D9)
rather than a new trust tier.

This is additive (D2) — the existing spawn-and-capture shell tool stays the
default; a PTY session is a distinct, explicitly-invoked tool affordance
the model reaches for when a command needs interaction.

## Acceptance criteria

- [ ] A PTY-backed session tool exists alongside the existing shell tool:
      the model can start an interactive session, read incremental output,
      and send input to the still-running process across multiple tool
      calls within a turn.
- [ ] Permission gating (AS-016) applies to starting a PTY session the same
      way it applies to the existing shell tool; the session is visible in
      tool-transparency UI (AS-024) as a distinct, long-lived tool call
      rather than a single request/response.
- [ ] Session output and sent input are captured as ordinary event-log
      blocks (D3) — no parallel state store, fully auditable/replayable.
- [ ] A hard idle/wall-clock timeout and an explicit close/kill path exist
      so an interactive session cannot hang a turn indefinitely.
- [ ] Works for at least one interactive REPL and one interactive prompt
      (e.g. a DB shell and a scaffolding tool's y/n/text prompts) in the
      offline E2E regression suite (AS-134).

## Debrief questions

- Is this a new first-class tool (`pty_session` or similar) or a mode flag
  on the existing shell tool? A distinct tool keeps the common case
  (spawn-and-capture) simple and lets permission/UI treat interactive
  sessions differently.
- How does the model decide when to reach for interactive mode vs. the
  existing shell tool — a description-only signal, or should common
  interactive binaries (`psql`, `mongosh`, `dlv`, `python -i`) be
  recognized and default to PTY mode?
- Does this belong in V1's tool set at all, or is it P2 polish given AS-015
  already ships and most agentic workflows avoid interactive tools by
  design? Warp's validation is real but recent (open-sourced Apr 2026) —
  worth confirming user demand before committing scope.
- Sandboxing implications: an interactive session that can accept arbitrary
  follow-up input from the model is a larger blast-radius surface than a
  single gated command. Does this change D9's "not a sandbox" posture, or
  does the existing permission/approval model already cover it?

## Dependencies

[AS-015, AS-016]
