---
id: AS-043
title: /tidy — context reorganization without lossy summarization (flagship wedge)
status: needs-clarification
github_issue: null
depends_on: [AS-006, AS-027, AS-028]
area: context-wedge
priority: P1
source: PRD.md §7.13, §9, D6 (fast-follow)
---

# AS-043 · /tidy

**Status: needs clarification**

## Description

The third flagship wedge (§7.13): restructure a messy session into a clean, ordered context — dedupe file reads, collapse dead ends, group by topic, promote durable facts to a "working memory" block — **without** lossy summarization. Mechanically it composes primitives that already exist: derived blocks + exclusions (AS-006), preview/undo (AS-028 patterns). The §9 risk row demands a **fidelity diff** so tidy never becomes another lossy compact.

Clear parts: dedupe of identical file reads (keep latest, exclude older — pure mechanics); preview + reversibility; output labeled for easy later `/clean`.

## Open questions (why this needs clarification)

1. **Dead-end detection** — what marks a sub-thread as a dead end (abandoned approach, error loop)? Heuristics over the trace, a cheap-model pass, or user-assisted marking in the preview?
2. **"Preserves all live facts" verification** — what concretely is the *fidelity diff* (§9)? A model-generated claim list compared before/after? How do we test the §7.13 AC "preserves all live facts" mechanically?
3. **Working-memory block** — what qualifies a fact for promotion, and does this overlap with the rediscovered-fact detector (AS-048)? One mechanism or two?
4. **Model involvement & budget** — grouping/dead-end analysis presumably needs the cheap tier; what's the acceptable cost per `/tidy`, and is it run as a system sub-agent (AS-044)?
5. **Dependency on topic labels** — does grouping require AS-027 to be resolved first, or can v1 group by file/tool-span only?

## Acceptance criteria (draft, to confirm after clarification — PRD AC included)

- [ ] Tidied context is materially smaller, preserves all live facts, and is reversible (§7.13 AC verbatim).
- [ ] A fidelity diff is shown before apply; originals remain in the archive (§9 mitigation).
- [ ] Duplicate file reads are deduped to the latest version.
- [ ] Output segments are labeled well enough that a follow-up `/clean` can target them.

## Dependencies

- AS-006 (derived blocks/exclusions), AS-027 (topic labels — pending its resolution), AS-028 (preview/undo/archive mechanics)
