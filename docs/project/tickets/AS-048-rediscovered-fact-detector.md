---
id: AS-048
title: Rediscovered-fact detector (living skills, first form)
status: needs-clarification
github_issue: 48
depends_on: [AS-032, AS-044, AS-047]
area: living-skills
priority: P1
source: PRD.md D7, §7.20
---

# AS-048 · Rediscovered-fact detector

**Status: needs clarification**

## Description

D7 locks this as the **first form of living skills**: a scalpel, not a courtroom. Detect trial-and-error that lands on a concrete durable fact — a command, path, or config value — and offer to save it to the relevant skill or memory file. High precision, user-checkable; budget/contract grading is explicitly demoted (that's AS-049, later).

Shape: a system sub-agent (AS-044) running at session end on the cheap tier; candidate signals are mechanical (failed command → variant → success; repeated searches converging on one path), with confirmation producing a one-line diff to AGENT.md or the relevant skill, applied via diff preview.

## Open questions (why this needs clarification)

1. **Detection mechanism** — pure trace heuristics (exit-code sequences, command edit-distance, search-then-read patterns), a cheap-model pass over candidate spans, or heuristics-propose / model-confirm? D7 demands high precision — where's the bar (e.g., <1 in 10 suggestions rejected)?
2. **What counts as a durable fact** — commands/paths/config only (D7's list), or also conventions ("tests live in /spec")? A crisp inclusion list keeps the scalpel sharp.
3. **Save-target resolution** — when does a fact belong to a *skill* vs *AGENT.md* vs a *directory-level* memory file? Rule needed for the ambiguous case (no skill was active when the fact was discovered).
4. **Offer UX** — at session end via `/insights`, or a low-key inline prompt the moment the fact is confirmed? (Inline is more visceral; session-end is less interruptive — pick one for v1.)

## Acceptance criteria (draft, to confirm after clarification)

- [ ] On a scripted session that rediscovers a known fact (e.g., flailing to find the test command), the detector proposes exactly that fact with the trace as evidence.
- [ ] Accepting writes a minimal diff to the chosen target via preview; declining records the dismissal (don't re-suggest the same fact).
- [ ] Zero cost when disabled; within budget when enabled (AS-044 guarantees).
- [ ] Precision bar (once set) is tracked: suggestions accepted vs dismissed.

## Dependencies

- AS-032 (memory targets), AS-044 (sub-agent framework), AS-047 (span data); relates to `should_not_rediscover` lists
