---
id: AS-135
title: Capture-to-fixture workflow for redacted vendor sessions and CI-safe regressions
status: done
github_issue: 415
depends_on: [AS-056, AS-060, AS-115]
area: schema
priority: P1
source: AS-060 regression-testing follow-on; docs/design/block-schema-union.md §14–§15
---

# AS-135 · Capture-to-fixture workflow for redacted vendor sessions and CI-safe regressions

## Problem

AS-060 asks for real vendor and CLI session captures, while AS-133/AS-134 turn selected
captures into deterministic offline regressions. Without a documented and tool-assisted
workflow, contributors may either avoid adding captures because redaction is scary or commit
fixtures that leak secrets, PII, account identifiers, or brittle vendor-specific noise.

Create a repeatable capture-to-fixture path: collect a live artifact once, scrub and review
it, classify which fields matter, then emit both human-readable validation notes and a
CI-safe recorded-server fixture.

## What to build

- A documented workflow for moving an artifact through these states:
  `raw local capture` → `redacted reviewed capture` → `schema validation report` →
  `recorded-server fixture` → `E2E scenario`.
- A small CLI or script that helps contributors:
  - normalize timestamps, IDs, account/project names, and request IDs;
  - apply the AS-115 redaction rules and flag values requiring manual review;
  - preserve semantically important large payload shape without preserving private content;
  - emit fixture metadata consumed by AS-133.
- A review checklist covering secrets, PII, licensing/sensitivity of prompts, vendor account
  metadata, and whether the fixture can be committed publicly.
- Guidance for when to keep an artifact out of git and only commit a synthetic derivative.
- Links from AS-060 and provider-conformance docs so future schema-validation work feeds the
  simulator/E2E suites instead of becoming a one-off report.

## Acceptance criteria

- [x] Contributors can produce a CI-safe fixture from a raw local capture without hand-editing
      every field. (`cmd/capture-fixture` normalizes + redacts in one pass.)
- [x] The workflow makes a clear distinction between raw private captures, redacted captures,
      synthetic derivatives, and public CI fixtures. (`redaction_status` enum + the data-class
      table in `docs/design/capture-to-fixture.md`.)
- [x] Redaction preserves the shape needed to validate schema fields, provider streaming, tool
      arguments/results, usage, cache data, and subagent/session links. (Normalize rewrites only
      identifying *values*; bodies are scrubbed via `internal/redaction` without changing kind or
      shape; cross-block references are remapped through a shared table.)
- [x] The generated metadata records source, redaction status, fixture intent, supported
      providers, and whether live-network reproduction is possible. (`Metadata` sidecar, validated.)
- [x] Docs point AS-060 implementers to this workflow and explain how AS-133/AS-134 consume the
      resulting fixtures. (Links from AS-060, `block-schema-union.md` §15, and `captures/README.md`.)

## Implementation notes

- `internal/capturefixture` — `Process` normalizes identifying envelope values
  (IDs, seq, timestamps, request/response/turn/native/thread/agent IDs, and the
  block-ID references in `derived_from`/`excluded_by`/thread parents) to stable
  deterministic placeholders, then scrubs bodies through the AS-115 redactor,
  validating every block. `Metadata` is the validated sidecar contract AS-133 reads.
- `cmd/capture-fixture` — thin stdlib CLI over the library (JSONL in/out + sidecar
  metadata); exits non-zero if any block fails schema validation.
- `docs/design/capture-to-fixture.md` — the state machine, redaction review
  checklist, when to keep an artifact out of git, and how AS-133/AS-134 consume the
  fixtures. A worked synthetic example lives under `docs/design/captures/examples/`.
- The auto-flag-for-manual-review wishlist item is handled by the documented review
  checklist rather than a heuristic flagger (a brittle classifier would give false
  confidence); the tool reports redaction-span counts and the human reviews the rest.

## Dependencies

- AS-056/AS-115 define compliance and redaction expectations.
- AS-060 supplies the initial real-world captures and schema-validation pressure.
- AS-133 consumes the resulting fixture format in recorded vendor simulators after this workflow is defined.
