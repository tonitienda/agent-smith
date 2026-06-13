---
id: AS-060
title: "Capture & compare real vendor session files to refine the block schema before V1 freeze"
status: ready-to-implement
github_issue: null
depends_on: [AS-002, AS-003]
area: schema
priority: P0
source: PRD.md D2, D4; docs/design/block-schema-union.md §14–§15
---

# AS-060 · Capture & compare real vendor session files before the V1 schema freeze

**Status: ready to implement**

## Description

The block-schema union ([docs/design/block-schema-union.md](../../design/block-schema-union.md)) is the up-front design (D4) for the immutable substrate, but it is a **draft until AS-003 ships in V1**. Per D2, the schema becomes additive-only *from V1* — so the cheap, high-value window to find gaps is **now, before the freeze**. The union was derived largely from documentation and a single primary-source capture (this repo's own Claude Code `.jsonl`); §15 of the design doc commits us to validating it against **real captured sessions across vendors and representation layers** before V1.

This ticket is that validation pass: capture a small corpus of real session artifacts, run each through the AS-003 types, and feed concrete schema deltas back into AS-003 while breaking changes are still free.

Capture (a handful each is enough — breadth over volume), covering all three representation layers from design-doc §5A:

- **Anthropic** — a raw Messages API turn (incl. thinking, a tool call/result, cache fields).
- **OpenAI** — a Responses API turn (typed `output[]` incl. a `reasoning` item) and a Chat Completions turn (the compatibility projection).
- **xAI/Grok** — an API turn (OpenAI-compatible projection; capture `reasoning_content` and any Live Search citations).
- **Claude Code** — a headless `--output-format json` result (L2) **and** the persisted `~/.claude/projects/<proj>/<sid>.jsonl` (L3), ideally a session that spawns a sub-agent (`isSidechain`) and loads a skill/MCP (`attribution*`).
- **Codex CLI** — a `codex exec --json` event stream (L2) **and** the `$CODEX_HOME/sessions/.../rollout-*.jsonl` (L3).
- **Gemini CLI** — a `--output-format json` result with `stats` (L2) **and** a `~/.gemini/history/<proj>/` log (L3).

For each captured artifact: parse → into union blocks → re-emit, and record whether the round-trip is lossless (the §14 checklist). Log every field that has **no home** in the AS-003 types except the `ext` escape hatch, and decide per field: **promote to a first-class optional**, **leave in `ext`**, or **out of scope** (with rationale, per D0 — no silent punts).

## Deliverables

- A small, redacted capture corpus checked in under `docs/design/captures/` (or referenced if artifacts can't be committed for size/privacy — strip secrets and PII either way).
- A comparison report (extend or annex `docs/design/block-schema-union.md`, or a sibling `block-schema-union-validation.md`) listing, per surface/layer: round-trip result, fields-without-a-home, and the promote / `ext` / out-of-scope decision for each.
- A concrete list of proposed **additive** schema deltas for AS-003 (new optional fields / event kinds), each justified by a captured artifact.

## Acceptance criteria

- [ ] At least one real capture per surface in the list above, covering L1 (wire), L2 (headless result), and L3 (persisted session) where the surface exposes them.
- [ ] Each capture is run through the AS-003 types and scored against the §14 round-trip checklist (lossless ingest → re-emit, including sub-agent/thread links and per-iteration/TTL-split usage).
- [ ] Every field observed in a capture has an explicit disposition: first-class optional, `ext`, or out-of-scope (with reason).
- [ ] Captures are redacted (no secrets/PII) before being committed or referenced.
- [ ] Proposed additive schema deltas are recorded and linked from the AS-003 ticket so they land **before the V1 freeze**.
- [ ] If a capture reveals a needed change that cannot be expressed additively, it is flagged loudly as a **pre-V1 breaking change** to make now (D2 allows breaks until V1; not after).

## Dependencies

- AS-002 (the union design this validates) — done.
- AS-003 (the schema types to validate against). This ticket **gates the V1 freeze of AS-003**: run it before AS-003 is locked.
