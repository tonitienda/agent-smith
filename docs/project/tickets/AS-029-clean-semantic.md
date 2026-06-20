---
id: AS-029
title: /clean "<topic>" — natural-language semantic matching
status: ready-to-implement
github_issue: 29
depends_on: [AS-028, AS-027]
area: context-wedge
priority: P0
source: PRD.md §7.12, §10 Q4, D6
---

# AS-029 · /clean natural-language matching

**Status: ready to implement**

## Description

The headline demo (§7.12, D6 ships it in V1): `/clean "the content related to the bug we fixed"` → the engine selects matching segments, previews, and removes on confirm. All removal mechanics (preview, exclusion events, archive, undo) already exist from AS-028 — this ticket is **only the matcher**.

The matching engine is **explicitly an open question in the PRD (§10 Q4)** and must be decided before implementation.

## Clarified implementation decisions

- **Engine choice:** V1 uses a deterministic, explainable hybrid without embeddings: normalize the natural-language query, match it against AS-027 tags/file paths/tool spans/goal text, and rank candidates with lexical scoring. A cheap-model precision pass may be added later behind config, but is out of scope for this ticket.
- **Cost/latency budget:** zero provider-token cost; interactive matching should complete in-process over the current projection.
- **Precision posture:** prefer conservative under-selection. The preview lets users add/remove handles before apply, and explanations must show why each segment matched.
- **Relationship to AS-027:** AS-027 supplies the label index and candidate tags; AS-029 runs fresh per query over the current projection and does not persist semantic search state.

## Acceptance criteria

- [ ] A natural-language phrase selects a coherent, explainable set of segments shown in the AS-028 preview; nothing auto-removes.
- [ ] PRD §6 guardrail holds: nothing is lost; undo is exact (inherited from AS-028).
- [ ] Matching uses no provider/model calls and meets the interactive latency budget on the current projection.
- [ ] Demo scenario passes: fix bug A, move to task B, `/clean "the bug we fixed"` reclaims A's segments and the session continues correctly.
- [ ] The preview explains matches with tags/files/tools so users can trust or correct the selection.

## Dependencies

- AS-028 (removal mechanics) — hard.
- AS-027 (topic labels) — soft; depends on the engine decision.
