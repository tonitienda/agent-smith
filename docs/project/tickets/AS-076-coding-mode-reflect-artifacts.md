---
id: AS-076
title: Coding Mode reflect-phase artifacts (success metric, instrumentation, check-back ticket)
status: needs-clarification
github_issue: 126
depends_on: [AS-045, AS-048, AS-072]
area: coding-mode
priority: P1
source: coding-mode.prd.md (D-CODE-7)
---

# AS-076 · Coding Mode reflect-phase artifacts

**Status: needs-clarification**

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

## Open questions

- **Prerequisite not yet clarified:** depends on AS-048 (rediscovered-fact
  detector), which is itself `needs-clarification` (detection mechanism, precision
  bar, durable-fact definition). This ticket can't be finalised until AS-048 is.
- **Reflect-artifact depth (PRD Q3-adjacent):** how far does the reflect phase go
  in producing artifacts — a scratch success-metric note, or actual synced
  `AS-NNN` check-back tickets via `cmd/ticket-sync`? The latter couples Coding
  Mode to this repo's ticket workflow.

## Dependencies

- AS-045 (`/insights` retro surface), AS-048 (rediscovered-fact detector for
  durable facts), AS-072 (mode shell — reflect is a phase within it).
