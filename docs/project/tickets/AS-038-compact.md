---
id: AS-038
title: /compact — lossy summarization fallback (but reversible here)
status: done
github_issue: 38
depends_on: [AS-006, AS-022]
area: commands
priority: P1
source: PRD.md §7.16, Appendix A, D3
---

# AS-038 · /compact

**Status: done**

## Description

Parity command, positioned in Appendix A as the *fallback when `/tidy` isn't enough*. Incumbents' `/compact` is destructive — ours is not, because of D3: the summary is a **derived block** whose source blocks are excluded but archived, with provenance linking summary → sources.

- Summarize the compactable span (everything except pinned/system/memory/recent blocks) with a cheap-tier model.
- Result: one `derived_block` (summary) + exclusion of its sources, all appended events. The projection shrinks; the log keeps everything.
- Preview before apply: tokens before/after, what's being summarized. Undo restores the exact pre-compact projection.
- Fires the `pre-compact` hook (AS-035) when hooks are configured.
- Auto-compact on approaching the window limit: include behind a config flag, default off (the product thesis prefers `/clean`/`/tidy`; compact is the blunt instrument).

## Acceptance criteria

- [x] Compacting reduces projected tokens by a visible amount and the session continues coherently on both providers.
- [x] Undo restores the exact prior projection (§6 guardrail — this alone differentiates from every incumbent).
- [x] The summary block's provenance lists every source block ID.
- [x] Summarization runs on the cheap tier and its cost is itemized in `/cost`.

## Dependencies

- AS-006 (derived blocks + exclusions), AS-022 (command framework); AS-035 (pre-compact hook) soft

## Implementation notes

- `internal/compact` is the pure engine (mirrors `internal/clean`/`internal/rewind`): `Preview` selects the compactable span — every live content block except the system/memory prefix and the most recent turn (its last live user-text block onward) — and `Build` stamps the cheap-tier summary as a `schema.KindCompaction` derived block via `eventlog.Derive`, so the one appended event both renders the summary and excludes its sources. `Undo` is a counter-exclusion targeting that block.
- The summarization model call is I/O the controller (`cmd/smith/controller.go` `compactApply`) performs: it fires the `pre-compact` hook, renders the sources to a plain-text transcript (`compact.Transcript`, avoiding tool-call/result pairing constraints), summarizes on the active vendor's cheap model (`cheapModel`: Haiku for Anthropic, gpt-4o-mini for OpenAI), records a `/compact`-attributed usage event so `/cost` itemizes it, then appends the compaction block.
- Both provider adapters now render a `KindCompaction` block as a user message (previously skipped), so the summary reaches the model on Anthropic Messages, OpenAI Chat Completions, and OpenAI Responses.
- **Not done (deferred):** auto-compact on approaching the window limit (the optional config-flagged behaviour, default off) — see AS-085.
