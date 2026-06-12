---
id: AS-029
title: /clean "<topic>" — natural-language semantic matching
status: needs-clarification
github_issue: null
depends_on: [AS-028, AS-027]
area: context-wedge
priority: P0
source: PRD.md §7.12, §10 Q4, D6
---

# AS-029 · /clean natural-language matching

**Status: needs clarification**

## Description

The headline demo (§7.12, D6 ships it in V1): `/clean "the content related to the bug we fixed"` → the engine selects matching segments, previews, and removes on confirm. All removal mechanics (preview, exclusion events, archive, undo) already exist from AS-028 — this ticket is **only the matcher**.

The matching engine is **explicitly an open question in the PRD (§10 Q4)** and must be decided before implementation.

## Open questions (why this needs clarification)

1. **Engine choice (§10 Q4)** — embeddings/semantic search over segments, a cheap-model classifier ("does this block relate to X? y/n over candidates"), or a hybrid (embedding recall → cheap-model precision pass)? Each differs in cost, latency, offline behavior, and binary size (local embedding model vs API calls).
2. **Cost/latency budget** — `/clean` is interactive; how long may matching take, and is a paid model call per `/clean` acceptable for a cost-focused product?
3. **Precision/recall posture** — favor over-selection (user deselects in preview) or under-selection (user adds)? The preview makes mistakes recoverable, but defaults shape trust.
4. **Relationship to AS-027** — if topic labels exist, is matching just label lookup, or does it run fresh per query? (One decision should cover both — flagged in AS-027 too.)

## Acceptance criteria (draft, to confirm after clarification)

- [ ] A natural-language phrase selects a coherent, explainable set of segments shown in the AS-028 preview (nothing auto-removes).
- [ ] PRD §6 guardrail holds: nothing is lost; undo is exact (inherited from AS-028).
- [ ] Matching meets the agreed latency/cost budget.
- [ ] Demo scenario passes: fix bug A, move to task B, `/clean "the bug we fixed"` reclaims A's segments and the session continues correctly.

## Dependencies

- AS-028 (removal mechanics) — hard.
- AS-027 (topic labels) — soft; depends on the engine decision.
