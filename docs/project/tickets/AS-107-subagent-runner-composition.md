---
id: AS-107
title: "Wire a sub-agent Runner into the composition root (register built-ins + install WithSubAgents)"
status: done
github_issue: 373
depends_on: [AS-088, AS-048]
area: subagents
priority: P2
source: PRD.md §7.19, AS-044, AS-088; spun out of AS-088
---

# AS-107 · Wire a sub-agent Runner into the composition root

**Status: ready to implement**

## Description

AS-044 shipped the sub-agent framework, AS-048 shipped the first built-in
(`internal/factdetector`, which already exposes a `subagent.Factory`), and
AS-088 taught the turn loop to drive an optional `*subagent.Runner`
(`loop.WithSubAgents`). What is still missing is the composition root actually
building a Runner and installing it, so nothing runs sub-agents in a real
session yet.

This ticket closes that gap in `cmd/smith` (and `internal/smithapp` as needed):

- Build a `subagent.Registry`, register the built-in sub-agents (starting with
  `factdetector.Factory(...)`), and apply config overlay via `Registry.Load` off
  the layered config (`subagents.<name>`, C.3).
- Construct a per-session `subagent.Runner` over the registry and an insights
  `Store`, and pass `loop.WithSubAgents(runner)` into the engine options
  alongside the existing `WithBudget`/`WithObserver` wiring (controller.go and
  cli.go).
- Decide the `Store` lifetime/ownership (per session) and where findings surface
  — this is the seam `/insights` (AS-045) consumes, so keep the Store reachable.

## Open questions

- `factdetector.New` needs a `Resolve` and a `Ledger`; what supplies them in the
  composition root (a default mechanical resolve + an in-memory or persisted
  ledger)? Persisting the dismissal ledger across sessions is likely its own
  follow-on.
- Should the Runner be installed for every face (TUI, headless CLI, ACP) or
  gated by config/face initially? Default-on costs nothing when idle (passive
  analyzers), but teardown timing differs for a one-shot headless run.

## Acceptance criteria

- [ ] A real session (TUI and headless) constructs a Runner with the built-in
      sub-agents registered and installs it via `loop.WithSubAgents`.
- [ ] Config under `subagents.<name>` enables/disables and tunes a sub-agent
      end-to-end (the §7.19 one-config-line property), surfaced through the
      composition root.
- [ ] Findings recorded during a session are reachable from the Store the
      composition root owns (the seam AS-045 will read), with a test covering at
      least one built-in producing a finding over a scripted session.

## Dependencies

- AS-088 (the loop now drives a Runner), AS-048 (the first built-in sub-agent).
