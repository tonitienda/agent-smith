---
id: AS-027
title: Segment topic labeling engine
status: ready-to-implement
github_issue: 27
depends_on: [AS-006]
area: context-wedge
priority: P1
source: PRD.md §5 (segmented store), §7.11, §10 Q4 (adjacent)
---

# AS-027 · Segment topic labeling engine

**Status: ready to implement**

## Description

§5 says every segment knows "its topic/tags" — that powers the topic dimension of `/context` and is the substrate semantic `/clean` (AS-029) matches against. But the PRD never specifies **how topics are derived**, and the answer has real cost and architecture consequences.

## Clarified implementation decisions

- **Mechanism:** V1 topic labels are deterministic heuristics only: file paths/modules, tool names, command names, explicit goal text, and turn boundaries. No embeddings and no model calls in this ticket.
- **When labeling runs:** labels are computed lazily during projection and cached as derived metadata/events only when a command needs stable handles. Labeling never runs on the hot provider path.
- **Cost budget:** zero provider-token cost. Later semantic/model labeling can add optional enrichments, but the V1 contract must work offline.
- **Granularity:** every projected segment gets multiple tags: at least one coarse type tag (`tool:shell`, `file`, `conversation`, etc.) and any discovered file/module/topic tags. Labels are user-visible in `/context`; manual editing is out of scope.

## Acceptance criteria

- [ ] Every projected segment carries at least one deterministic topic/tag.
- [ ] Labels are derived additively and never mutate original blocks (D3).
- [ ] `/context` gains a "by topic" grouping based on these labels.
- [ ] Labeling has zero provider-token cost and never blocks an interactive turn on a model call.
- [ ] Tags are stable enough to be used by AS-029 as the first-pass candidate set.

## Dependencies

- AS-006 (segments to label)
- Decision feeds AS-029 (semantic /clean)
