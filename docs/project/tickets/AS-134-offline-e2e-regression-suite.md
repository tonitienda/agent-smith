---
id: AS-134
title: Offline E2E regression suite over recorded providers, TUI, and append-only logs
status: ready-to-implement
github_issue: null
depends_on: [AS-005, AS-018, AS-021, AS-024, AS-046, AS-119, AS-120, AS-133]
area: quality
priority: P0
source: AS-060 regression-testing follow-on; docs/project/PRD.md D5
---

# AS-134 · Offline E2E regression suite over recorded providers, TUI, and append-only logs

## Problem

Provider conformance tests prove adapters normalize individual vendor streams, but they do
not prove that a full Smith session behaves correctly when those streams contain large tool
requests, multiple parallel tool calls, nested subagents, long contexts, or TUI-facing state
transitions. Those are exactly the expensive regressions we want to catch without burning
vendor tokens.

Create an offline end-to-end suite that drives Smith through realistic recorded-provider
scenarios and verifies the user-facing transcript, TUI model, cost accounting, subagent
ledger, and append-only JSONL session artifacts.

## What to build

- A scenario runner that starts the recorded vendor simulators from AS-133, configures Smith
  to use their loopback endpoints, and runs scripted sessions through the same composition
  root used by CLI/TUI faces.
- Golden scenarios for:
  - a single-agent tool loop with a large JSON tool argument and a large tool result;
  - parallel independent tool calls in one model turn;
  - an interrupted/denied permission flow followed by model recovery;
  - parent agent delegating to multiple child agents with large inherited context;
  - budget/context pressure that exercises `/context`, `/cost`, and append-only log replay.
- Assertions that inspect the resulting session JSONL files and require:
  - no mutation of previously written events;
  - stable parent/child session linkage;
  - preserved tool-call IDs, arguments, results, usage, and cost attribution;
  - deterministic projection after `session resume`/rehydration.
- TUI-facing assertions at the model/rendering layer so CI can verify tool cards, permission
  state, subagent state, and context/cost panels without requiring a real terminal screenshot.
- A harness entry point suitable for CI and local reproduction, documented alongside the
  existing quality-gate contract.

## Acceptance criteria

- [ ] The full E2E suite runs offline in CI with no vendor API keys and no live network calls.
- [ ] Scenarios fail with actionable diffs for transcript, event-log, or TUI-model drift.
- [ ] At least one scenario includes multiple subagents and verifies per-child cost/itemized
      attribution when AS-119/AS-120 are present.
- [ ] At least one scenario includes a large tool request/response pair that would be
      impractical to exercise repeatedly against live APIs.
- [ ] The suite documents how to refresh goldens intentionally and how to distinguish an
      intended schema evolution from a regression.
- [ ] The new command is wired into an appropriate CI job or documented as a required local
      pre-release gate if it is too slow for every pull request.

## Dependencies

- AS-133 supplies deterministic recorded providers.
- AS-005/AS-018/AS-021/AS-024 provide the log, loop, TUI, and tool transparency surfaces.
- AS-046/AS-119/AS-120 provide subagent delegation and child cost surfaces for the nested
  scenarios.
