---
id: AS-043
title: /tidy — context reorganization without lossy summarization (flagship wedge)
status: ready-to-implement
github_issue: 43
depends_on: [AS-006, AS-027, AS-028]
area: context-wedge
priority: P1
source: PRD.md §7.13, §9, D6 (fast-follow)
---

# AS-043 · /tidy

**Status: ready to implement**

## Description

The third flagship wedge (§7.13): restructure a messy session into a clean, ordered context — dedupe file reads, collapse dead ends, group by topic, promote durable facts to a "working memory" block — **without** lossy summarization. Mechanically it composes primitives that already exist: derived blocks + exclusions (AS-006), preview/undo (AS-028 patterns). The §9 risk row demands a **fidelity diff** so tidy never becomes another lossy compact.

Clear parts: dedupe of identical file reads (keep latest, exclude older — pure mechanics); preview + reversibility; output labeled for easy later `/clean`.

## Clarified implementation decisions

- **Dead-end detection:** V1 is user-assisted and heuristic: group by phase/turn/file/tool spans, detect repeated failing shell commands and abandoned file paths, then let the preview decide. No autonomous deletion.
- **Fidelity diff:** show before/after segment inventory, exact originals covered by each derived group, token deltas, and any heuristic "live fact" warnings. V1 does not use a model-generated claim list as the authority.
- **Working memory:** promote only concrete durable facts in the same inclusion set as AS-048 (commands, paths, config values, repo conventions). The promotion mechanism is shared with AS-048; `/tidy` may surface candidates but must not invent a second memory-writing path.
- **Model involvement:** zero provider-token default. A future cheap-tier reorganizer can be added behind config after AS-042, but V1 tidy must be useful mechanically.
- **Topic-label dependency:** AS-027 is required for labels, but grouping can fall back to file/tool spans when no semantic tag is present.

## Acceptance criteria (draft, to confirm after clarification — PRD AC included)

- [ ] Tidied context is materially smaller, preserves all live facts, and is reversible (§7.13 AC verbatim).
- [ ] A fidelity diff is shown before apply; originals remain in the archive (§9 mitigation).
- [ ] Duplicate file reads are deduped to the latest version.
- [ ] Output segments are labeled well enough that a follow-up `/clean` can target them.

## Dependencies

- AS-006 (derived blocks/exclusions), AS-027 (topic labels — pending its resolution), AS-028 (preview/undo/archive mechanics)
