---
id: AS-047
title: Skill expectation contracts (frontmatter schema, parsing, span boundaries)
status: ready-to-implement
github_issue: null
depends_on: [AS-034]
area: living-skills
priority: P1
source: PRD.md §7.20, Appendix C.1, §10 Q8/Q10 (resolved)
---

# AS-047 · Skill expectation contracts

**Status: ready to implement**

## Description

The declarative half of living skills: parse and track the Appendix C.1 contract from skill frontmatter, and delimit the skill's span in the event log. This is pure plumbing — no judgment, no model calls — and a prerequisite for both the fact detector (AS-048) and the analyzer (AS-049).

- Parse `expected_outcome` (summary, effort_budget: tool_calls/turns/max_cost_usd, should_not_rediscover, success_signals) and `completion` (signal, idle_turns) per C.1; tolerate absence of any/all fields (inferred contracts are AS-049's job).
- Span tracking: from skill activation (AS-034 events) to teardown — fired by the declared `completion.signal` when present, else the `idle_turns` heuristic (Q10 resolution: declared preferred, heuristic v1 fallback).
- Per-span actuals accumulated from the log: tool calls, turns, cost — attributed to the right skill even with overlapping skill activations (document the attribution rule for overlaps).
- Contract + actuals handed to teardown consumers via the AS-044 scope mechanism.

## Acceptance criteria

- [ ] A C.1-conformant skill parses fully; a skill with no contract fields loads without complaint.
- [ ] Declared completion signal fires teardown at the right moment (test: `make ship` exit 0 example).
- [ ] Idle-turns fallback fires after N turns without skill tool use.
- [ ] Span actuals (tool calls / turns / $) match a hand-computed trace in tests.

## Dependencies

- AS-034 (skill activation events)
