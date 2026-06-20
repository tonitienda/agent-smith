---
id: AS-048
title: Rediscovered-fact detector (living skills, first form)
status: done
github_issue: 48
depends_on: [AS-032, AS-044, AS-047]
area: living-skills
priority: P1
source: PRD.md D7, §7.20
---

# AS-048 · Rediscovered-fact detector

**Status: done** — shipped in `internal/factdetector` (`subagent.SubAgent`
built-in): a passive, zero-cost analyzer that at session end scans the block
slice for the failed-then-successful command pattern and proposes the working
command as a propose-only `Finding` (one-line diff + resolved save target). High
precision: a candidate needs prior flailing **and** a significant shared token
linking a failure to the success, so a command that merely worked is never
flagged. Dismissals are tracked in a `Ledger` (in-memory `MemLedger`; durable
backing is the AS-050 rollup seam) so a declined fact is not re-suggested, and the
same ledger tracks the precision tally (accepted vs dismissed). Save-target
resolution is injected as a `Resolve` func (skill scope → deepest memory file →
project root fallback), keeping the package free of the `memory`/`skill` imports.
Repeated-search→path convergence and config-key facts are the precision-tuned
follow-on **AS-106**; live wiring (Runner into the loop, `/insights` offer UX with
diff-preview apply) is the consumer step **AS-088**/**AS-045**, the same
substrate-first split AS-044 used.

## Description

D7 locks this as the **first form of living skills**: a scalpel, not a courtroom. Detect trial-and-error that lands on a concrete durable fact — a command, path, or config value — and offer to save it to the relevant skill or memory file. High precision, user-checkable; budget/contract grading is explicitly demoted (that's AS-049, later).

Shape: a system sub-agent (AS-044) running at session end on the cheap tier; candidate signals are mechanical (failed command → variant → success; repeated searches converging on one path), with confirmation producing a one-line diff to AGENT.md or the relevant skill, applied via diff preview.

## Clarified implementation decisions

- **Detection mechanism:** heuristics propose candidates from trace patterns; an optional cheap-model confirmer can be added later, but V1 must achieve useful results without provider calls.
- **Precision bar:** optimize for high precision over recall. Track accepted vs dismissed suggestions and treat repeated low acceptance as a detector-quality bug; do not chase an exact percentage gate until there is enough session volume.
- **Durable fact definition:** commands, paths, config keys/values, and explicit repo conventions discovered through failed-then-successful work. General advice, subjective preferences, and one-off debugging observations are out of scope.
- **Save-target resolution:** prefer the active skill's memory/contract when the trace is inside a skill scope; otherwise choose the deepest applicable memory file for the files involved, falling back to the project root memory file. Always show the target in the diff preview.
- **Offer UX:** V1 offers candidates at session end through `/insights`; no inline interruption. Declines are recorded so the same evidence is not re-suggested.

## Acceptance criteria

- [x] On a scripted session that rediscovers a known fact (e.g., flailing to find the test command), the detector proposes exactly that fact with the trace as evidence. (`TestDetectsRediscoveredCommand`)
- [x] Accepting writes a minimal diff to the chosen target via preview; declining records the dismissal (don't re-suggest the same fact). (`Finding.Proposal` carries the one-line diff + target; `Ledger.Record(…, Dismissed)` suppresses re-offering — `TestDismissedFactNotResuggested`. The interactive apply-via-preview surface is the `/insights` consumer step AS-045/AS-088.)
- [x] Zero cost when disabled; within budget when enabled (AS-044 guarantees). (`TestRunnerIntegration`: disabled → never driven; enabled → `SpentUSD == 0`, no model tier.)
- [x] Precision bar (once set) is tracked: suggestions accepted vs dismissed. (`Stats.Precision`, `TestPrecisionTracking`.)

## Dependencies

- AS-032 (memory targets), AS-044 (sub-agent framework), AS-047 (span data); relates to `should_not_rediscover` lists
