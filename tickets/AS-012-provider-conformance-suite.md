---
id: AS-012
title: Provider conformance test suite (recorded fixtures, no live calls in CI)
status: ready-to-implement
github_issue: null
depends_on: [AS-009, AS-010]
area: provider
priority: P0
source: PRD.md §9 (provider API drift risk)
---

# AS-012 · Provider conformance test suite

**Status: ready to implement**

## Description

Provider API drift is the top risk in the PRD (§9). Mitigation: a shared conformance suite every provider implementation must pass, running on recorded fixtures so CI needs no API keys.

- One suite of behavioral tests written against the AS-008 interface: streaming order, tool-call normalization, multi-tool turns, usage accounting, error mapping, context-too-long handling, unicode/edge-case content.
- Fixture recorder: capture real API request/response pairs (sanitized) into replayable fixtures; a refresh workflow (manual, with keys) to re-record when providers change.
- Both providers run the identical suite; provider-specific quirks must be absorbed inside the provider package, not leak into expected outputs.

## Acceptance criteria

- [ ] The same test suite passes for Anthropic and OpenAI providers from fixtures alone.
- [ ] CI runs the suite with zero network access.
- [ ] A documented `make record-fixtures` flow regenerates fixtures with live keys.
- [ ] A normalization bug (e.g., tool-call args differing across providers) is catchable by the suite — include at least one regression test proving it.

## Dependencies

- AS-009, AS-010 (implementations under test)
