---
id: AS-055
title: Replayable run logs + OpenTelemetry export
status: ready-to-implement
github_issue: 55
depends_on: [AS-005, AS-007, AS-020]
area: observability
priority: P2
source: PRD.md §7.23 (OSS portion), §8
---

# AS-055 · Replayable run logs + OpenTelemetry export

**Status: ready to implement**

## Description

§7.23, the OSS half (compliance archiving is AS-056's spike): every run produces a structured, replayable record for debugging and reproducibility. The append-only log (D3) already *is* the record — this ticket adds the run manifest, the replay UX, and OTel export. Per §8, this is replayability, **not** bit-exact determinism.

- Run manifest per session: models used (with versions), config snapshot (sanitized — never keys), tool versions, totals — alongside the event log.
- `smith replay <session>`: re-render a session from its log (TUI playback / dump mode) — re-display, not re-execution; clearly labeled as such.
- OpenTelemetry export: spans for session → turn → model call / tool call, with token/cost attributes; OTLP endpoint config via AS-031; off by default.
- Secret hygiene: keys and env secrets never enter the log or manifest (test with a canary secret).

## Acceptance criteria

- [ ] Any persisted session replays fully offline with no API keys present.
- [ ] OTel spans arrive at a local collector with correct hierarchy and cost attributes.
- [ ] Manifest + log suffice to answer: which model, which config, what cost, which tools ran, in what order.
- [ ] Canary-secret test proves sanitization.

## Dependencies

- AS-005/AS-007 (the log + store), AS-020 (cost attributes), AS-031 (OTLP config)
