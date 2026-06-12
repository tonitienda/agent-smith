---
id: AS-027
title: Segment topic labeling engine
status: needs-clarification
github_issue: 27
depends_on: [AS-006]
area: context-wedge
priority: P1
source: PRD.md §5 (segmented store), §7.11, §10 Q4 (adjacent)
---

# AS-027 · Segment topic labeling engine

**Status: needs clarification**

## Description

§5 says every segment knows "its topic/tags" — that powers the topic dimension of `/context` and is the substrate semantic `/clean` (AS-029) matches against. But the PRD never specifies **how topics are derived**, and the answer has real cost and architecture consequences.

## Open questions (why this needs clarification)

1. **Mechanism** — heuristics only (file paths, tool names, turn boundaries)? Embeddings? A cheap-model classifier run over segments? Some hybrid? This overlaps with open question §10 Q4 for `/clean` matching — ideally one decision covers both.
2. **When does labeling run?** At ingest (every block, every turn — adds latency/cost to the hot path) or lazily on demand (when `/context` or `/clean` is invoked)? The system sub-agent principle says observe is "passive, no model calls" — does labeling get the same constraint?
3. **Cost budget** — if a model is involved, it conflicts with the cost thesis unless batched/cheap-tier. What's the acceptable per-session labeling spend?
4. **Granularity** — one topic per block, or multiple tags? User-visible/editable labels?

## Acceptance criteria (draft, to confirm after clarification)

- [ ] Every projected segment carries at least one topic/tag.
- [ ] Labels stored as additive events/metadata — never mutating original blocks (D3).
- [ ] `/context` gains a "by topic" grouping.
- [ ] Labeling cost stays within the agreed budget and never blocks an interactive turn.

## Dependencies

- AS-006 (segments to label)
- Decision feeds AS-029 (semantic /clean)
