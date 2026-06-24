---
id: AS-076
title: Coding Mode reflect-phase artifacts (success metric, instrumentation, check-back ticket)
status: done
github_issue: 126
depends_on: [AS-045, AS-048, AS-072]
area: coding-mode
priority: P1
source: coding-mode.prd.md (D-CODE-7)
---

# AS-076 · Coding Mode reflect-phase artifacts

**Status: done**

Implemented skill-driven (D-CODE-7): the bundled `reflect-artifacts` process skill (`internal/codingskills/skills/reflect-artifacts/`), mapped to the reflect phase in `internal/mode` `phaseSkills`, drives the three artifacts — a measurable success metric, an instrumentation diff/proposal, and a check-back ticket draft (house `AS-NNN` format in this repo, markdown elsewhere; draft only, never `cmd/ticket-sync` or remote issues). The skill forbids reading/claiming shipped-app runtime data.

AC2's "no telemetry-ingestion path" is guarded structurally by `archtest.TestCodingModeHasNoTelemetryIngestion` (the Coding Mode subsystem may import no network client or database) and the skill's content guard in `codingskills_test.go`. Durable facts (AS-048) and the `/insights` retro (AS-045) already run session-wide and so are available in the reflect phase.

## Description

The **reflect** phase, scoped honestly (D-CODE-7). Judging whether a *shipped*
feature succeeded needs the user's deployed app's runtime telemetry, which a
coding harness cannot see — that is a different product and is **out of scope**.
What Coding Mode *can* do is produce **artifacts**: help the user write the
success metric, scaffold the instrumentation code, and file a check-back ticket.
It never reads runtime data.

- Help the user articulate a measurable success metric for the feature.
- Scaffold the instrumentation code (an event/log/metric the user wires into
  their app) — output as a diff/proposal, like any other code work.
- File a **check-back ticket** so success is revisited later instead of forgotten.
- Hook into `/insights` (AS-045) for the per-session retro and the
  rediscovered-fact detector (AS-048) so durable facts learned in the feature
  get offered for saving (living skills).

## Acceptance criteria

- [ ] In the reflect phase, Smith can produce: (a) a stated success metric,
      (b) an instrumentation code proposal as a diff, (c) a check-back ticket.
- [ ] Smith never attempts to read or claim shipped-app runtime data; the phase
      deals only in artifacts (a test asserts no telemetry-ingestion path exists).
- [ ] Durable facts surfaced during the feature are offered for saving via the
      AS-048 detector; the session retro is available via `/insights` (AS-045).

## Clarified implementation decisions

- **AS-048 prerequisite:** use the clarified AS-048 durable-fact detector contract: commands, paths, config values, and repo conventions are offered through `/insights` with diff preview.
- **Reflect-artifact depth:** V1 creates local artifacts only: a success-metric note in the session, an instrumentation diff/proposal, and a check-back ticket draft in Smith's own ticket format when running inside this repo. It must not call `cmd/ticket-sync` or mutate remote issue state. In other repos, produce a markdown check-back note/diff appropriate to the detected project.

## Dependencies

- AS-045 (`/insights` retro surface), AS-048 (rediscovered-fact detector for
  durable facts), AS-072 (mode shell — reflect is a phase within it).
