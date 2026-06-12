---
id: AS-038
title: /compact — lossy summarization fallback (but reversible here)
status: ready-to-implement
github_issue: 38
depends_on: [AS-006, AS-022]
area: commands
priority: P1
source: PRD.md §7.16, Appendix A, D3
---

# AS-038 · /compact

**Status: ready to implement**

## Description

Parity command, positioned in Appendix A as the *fallback when `/tidy` isn't enough*. Incumbents' `/compact` is destructive — ours is not, because of D3: the summary is a **derived block** whose source blocks are excluded but archived, with provenance linking summary → sources.

- Summarize the compactable span (everything except pinned/system/memory/recent blocks) with a cheap-tier model.
- Result: one `derived_block` (summary) + exclusion of its sources, all appended events. The projection shrinks; the log keeps everything.
- Preview before apply: tokens before/after, what's being summarized. Undo restores the exact pre-compact projection.
- Fires the `pre-compact` hook (AS-035) when hooks are configured.
- Auto-compact on approaching the window limit: include behind a config flag, default off (the product thesis prefers `/clean`/`/tidy`; compact is the blunt instrument).

## Acceptance criteria

- [ ] Compacting reduces projected tokens by a visible amount and the session continues coherently on both providers.
- [ ] Undo restores the exact prior projection (§6 guardrail — this alone differentiates from every incumbent).
- [ ] The summary block's provenance lists every source block ID.
- [ ] Summarization runs on the cheap tier and its cost is itemized in `/cost`.

## Dependencies

- AS-006 (derived blocks + exclusions), AS-022 (command framework); AS-035 (pre-compact hook) soft
