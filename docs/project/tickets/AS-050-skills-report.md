---
id: AS-050
title: /skills — per-session findings + cross-session rollup
status: ready-to-implement
github_issue: 50
depends_on: [AS-007, AS-022, AS-049]
area: living-skills
priority: P2
source: PRD.md §7.20, §10 Q9 (resolved), Appendix A
---

# AS-050 · /skills report and cross-session rollup

**Status: ready to implement**

## Description

The surfacing layer for living skills (§7.20, Q9 resolution: both per-session and rollup). The rollup is where project skills compound: *"skill X ran 4× over budget across 9 sessions; 3 facts it keeps rediscovering."*

- `/skills` panel: per-skill activation history, verdicts, scores, trend; rediscovered-fact tallies; pending remedies with one-click apply (diff preview).
- Rollup store: findings (C.2 records) indexed across sessions per project — local files alongside the session store (AS-007), additive-only like everything else.
- Aggregations: over-budget frequency, repeat findings (same fact rediscovered in N sessions ⇒ promote urgency), trigger-failure rates per skill.
- Every aggregate drills down to the underlying session spans (jump-to links).

## Acceptance criteria

- [ ] `/skills` renders per-session findings and the cross-session rollup for the current project.
- [ ] A fact rediscovered in 3+ sessions is visibly escalated.
- [ ] Applying a remedy from the rollup patches the skill via diff preview and marks the finding resolved.
- [ ] Rollup data survives schema additions (unknown-field tolerance, D2 discipline).

## Dependencies

- AS-007 (session store), AS-022 (panel), AS-049 (findings; AS-048 tallies feed it too)
