---
id: AS-140
title: Manual test campaign — add detailed scenarios for newly-completed tickets
status: ready-to-implement
github_issue: null
type: bug
depends_on: [AS-029, AS-043, AS-054, AS-057, AS-110, AS-119, AS-120, AS-132, AS-133, AS-135, AS-136, AS-138]
area: quality
priority: P2
source: QA pass 2026-06-25
---

# AS-140 · Manual test campaign: missing detailed scenarios for completed tickets

## Description

The 2026-06-25 QA campaign pass found that the manual test campaign
(`docs/projects/manual-test-campaign.md`) had several tickets listed as "Not
implemented" when the corresponding tickets are `status: done`. The stale entries
were corrected in the same PR. However, the following completed tickets still lack
**detailed test scenarios** in the campaign's numbered sections; they are mentioned
only in the ticket coverage matrix or as footnotes.

Tickets needing scenario rows added to the appropriate section:

| Ticket | Area | Missing scenario |
|---|---|---|
| AS-029 | context-wedge | Step in section 5 for `/clean "<topic>"` semantic matching: type a topic, confirm matching segments are previewed, apply, verify the right segments are excluded |
| AS-043 | context-wedge | Step in section 5 for `/tidy`: trigger dedup of repeated file reads, preview shows delta, apply is reversible |
| AS-054/AS-132 | async | Full step in section 8 or a new section 8.x for the background runner: `smith run "…" --queue`, `smith runs list`, `smith runs work --watch --concurrency 2`, confirm two workers pick up distinct queued tasks without double-running, Ctrl+C drains cleanly |
| AS-057/AS-136 | insights-wedge | Expand step 8.2b with `smith stats rebuild` and confirm per-project friction lines appear when >1 project has sessions |
| AS-110 | cost | Expand step 5.7 with concrete per-session override test: `/route cheap anthropic claude-haiku-4-5`, verify override is active in `/route`, run `/clear`, verify it resets |
| AS-119/AS-120 | subagents | Expand step 8.0 with headless delegation (`smith run --queue` + `smith runs work`); confirm per-child cost appears in `smith cost` |
| AS-133/AS-135 | provider | Add a step referencing the capture-to-fixture workflow and confirming the recorded providers are used by `go test ./internal/e2e/...` (row 3.4a already exists but doesn't reference AS-133/AS-135 explicitly) |
| AS-138 | insights-wedge | Add a step for the high-confidence auto-promotion path: set up a fact seen in 3+ sessions and verify it appears in `smith improve` without needing the second-session dedup threshold |

Additionally, the TUI visual polish wave (AS-121–AS-126, which are done) has only
one composite step (6.8) covering the splash and Matrix rain. Steps should be added
for the phosphor palette (AS-121) and any other verifiable aspects of the done
tickets.

## Acceptance criteria

- [ ] Each ticket in the table above has at least one numbered step in the
      appropriate campaign section with a concrete action and expected result.
- [ ] The quick campaign checklist table is updated to reference the new steps.
- [ ] The ticket coverage matrix links to the steps.
- [ ] Campaign still passes `./scripts/agent-quality-gate.sh` after edits.
