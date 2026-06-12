---
id: AS-049
title: skill-expectation-analyzer — predict-then-measure skill grading (experimental)
status: ready-to-implement
github_issue: null
depends_on: [AS-044, AS-047, AS-048]
area: living-skills
priority: P2
source: PRD.md §7.20, D7, Appendix C.1–C.2
---

# AS-049 · skill-expectation-analyzer (experimental)

**Status: ready to implement** *(explicitly experimental — D7 demotes this until session volume exists)*

## Description

The full predict-then-measure analyzer (§7.20): at skill load, establish the contract (declared C.1 when present, **inferred** from the skill's stated purpose otherwise — Q8 resolution); at teardown, compare contract vs actual span and grade.

- Output per activation, per Appendix C.2: verdict (`helped | no_op | underperformed | should_have_loaded`), normalized score, classification (`content_gap | trigger_failure | friction`), evidence (turns, cost, rediscovered facts), and a remedy (`patch_skill | fix_description | prune | new_skill`) with a concrete diff.
- **Grounded, never vibes** (§7.20): every claim cites turns, cost, and a transcript span with a jump-to link; §9 mitigation for fairness — contract is fixed at load time, not hindsight, and the UI offers an optional "what did you expect?" branch instead of guessing intent.
- Closes the loop: a content gap patches the skill or seeds a new one, with the real transcript span attached as the first regression/eval case (`eval_seed: session://…`, C.2).
- Runs as an opt-in, cheap-tier, teardown-scheduled system sub-agent (C.3); findings flow into `/insights` and the `/skills` rollup (AS-050).

## Acceptance criteria (PRD §7.20 AC included)

- [ ] For a session where a loaded skill underperformed, the analyzer produces a specific, grounded, applicable suggestion and a concrete skill diff (PRD AC verbatim).
- [ ] Inferred contracts are recorded at load time and immutable thereafter (fairness property, testable).
- [ ] Findings conform to the C.2 schema and carry working jump-to links.
- [ ] `should_have_loaded` verdicts fire for sessions where a matching skill existed but never triggered (trigger-failure path).

## Dependencies

- AS-044 (lifecycle), AS-047 (contracts + spans), AS-048 (rediscovery evidence feeds `content_gap` findings)
