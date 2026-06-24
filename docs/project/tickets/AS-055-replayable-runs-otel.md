---
id: AS-055
title: Replayable run logs + OpenTelemetry export
status: done
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

## Implementation notes (done)

- **Run manifest** — `internal/manifest` derives a `manifest.json` (models, tools,
  token/cost totals, sanitized config snapshot, binary version) purely from the
  event log + cost summary, written next to the log. `Sanitize` is the single
  chokepoint that strips secret-looking config keys (the canary-secret guarantee).
  Headless/async runs write it at run end (`executeRun`); `smith replay` rebuilds
  and self-heals it for sessions that never wrote one (e.g. a TUI session).
- **`smith replay <session>`** — re-renders a persisted session from its log as a
  manifest header + transcript; `--output json` dumps `{manifest, blocks}`. It is
  re-display, not re-execution (no provider/tool call), so a session replays fully
  offline with no API keys. TUI *playback* (animated re-render) is out of scope
  here — dump mode satisfies the offline-replay acceptance; a follow-on can add the
  animated view if wanted.
- **OpenTelemetry export** — `internal/otelexport` projects the log + cost into an
  OTLP/HTTP-JSON trace (`session → turn → model.call / tool.call`, with token and
  cost attributes) and POSTs it to the configured collector. Stdlib-only (no OTel
  SDK dependency, AS-095); span/trace IDs are derived deterministically from block
  IDs so the trace is replayable and unit-testable offline. Off by default — config
  key `telemetry.otel_endpoint` (via AS-031); export runs at run end and on
  `smith replay <id> --otel`.
- **Secret hygiene** — token/cost figures come from the redacted log (AS-115); the
  config snapshot is sanitized in `manifest.Sanitize`. Covered by a canary-secret
  test in `internal/manifest`.
