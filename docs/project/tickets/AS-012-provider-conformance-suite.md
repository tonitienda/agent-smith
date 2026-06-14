---
id: AS-012
title: Provider conformance test suite (recorded fixtures, no live calls in CI)
status: done
github_issue: 12
depends_on: [AS-009, AS-010]
area: provider
priority: P0
source: PRD.md §9 (provider API drift risk)
---

# AS-012 · Provider conformance test suite

**Status: done**

## Description

Provider API drift is the top risk in the PRD (§9). Mitigation: a shared conformance suite every provider implementation must pass, running on recorded fixtures so CI needs no API keys.

- One suite of behavioral tests written against the AS-008 interface: streaming order, tool-call normalization, multi-tool turns, usage accounting, error mapping, context-too-long handling, unicode/edge-case content.
- Fixture recorder: capture real API request/response pairs (sanitized) into replayable fixtures; a refresh workflow (manual, with keys) to re-record when providers change.
- Both providers run the identical suite; provider-specific quirks must be absorbed inside the provider package, not leak into expected outputs.

## Acceptance criteria

- [x] The same test suite passes for Anthropic and OpenAI providers from fixtures alone.
- [x] CI runs the suite with zero network access.
- [x] A documented `make record-fixtures` flow regenerates fixtures with live keys.
- [x] A normalization bug (e.g., tool-call args differing across providers) is catchable by the suite — include at least one regression test proving it.

## Dependencies

- AS-009, AS-010 (implementations under test)

## Implementation notes

- `internal/provider/conformance` holds the shared suite: `Cases()` defines the
  behavioral scenarios (streaming text, tool-call normalization, multi-tool
  turns, reasoning, unicode, usage accounting, and error mapping for rate-limit /
  context-too-long / auth); `Assemble` reduces an event stream to a comparable
  `Result`; `Compare` checks it against the case's `Want`. Each case declares the
  **normalized** turn, so the identical `Want` holds for both vendors — the suite
  enforces cross-provider equivalence, not per-vendor goldens.
- Fixtures live per vendor under `internal/provider/<vendor>/testdata/conformance/
  <case>.http` (a raw HTTP response). The adapter under test still builds its
  request; `conformance.FileTransport` answers it with the fixture, so CI runs
  with zero network access.
- `make record-fixtures` (→ `TestRecordConformance`, skipped without
  `SMITH_RECORD=1` + a key) regenerates the recordable fixtures live via
  `RecordingTransport`; error fixtures are curated by hand. See the package
  `README.md`.
- Regression coverage: `divergence_test.go` proves the suite catches a tool-call
  argument-reformatting bug (and the assembler rejects an unterminated block).
