---
id: AS-168
title: Manual test campaign — stale not-implemented rows and missing coverage for AS-140…AS-167
status: ready-to-implement
github_issue: null
type: bug
depends_on: [AS-140, AS-123, AS-127, AS-143, AS-158, AS-159, AS-160, AS-161, AS-162, AS-163]
area: quality
priority: P2
source: QA manual-test-campaign pass 2026-07-01
---

# AS-168 · Manual test campaign: stale rows for AS-123/AS-127 and no coverage for AS-140…AS-167

## Description

The 2026-07-01 QA campaign pass (`docs/projects/manual-test-campaign.md`) found
two classes of gap between what the campaign documents and the actual backlog /
product behaviour.

### 1. Stale "Not implemented" rows (fixed in this pass)

Two tickets whose frontmatter is `status: done` were still listed as **Not
implemented** in the campaign, so a tester following the campaign would mark a
shipped feature as absent:

| Ticket | Title | Campaign said | Ticket status |
|---|---|---|---|
| AS-123 | TUI typewriter streaming — char-by-char reveal with trailing block cursor | Not implemented (quick checklist + coverage matrix) | `done` |
| AS-127 | TUI command palette visual redesign — search field, per-command styling, footer hints | Not implemented (quick checklist + coverage matrix) | `done` |

These were corrected in the same PR: moved out of the "Not implemented" quick
checklist row, added to the Implemented lists / coverage matrix, and step 6.9's
typewriter scenario (already present in the detailed sections) now matches the
matrix.

### 2. Coverage gap — AS-140…AS-167 absent from the campaign

The campaign states it "covers every ticket in the backlog", but the ticket
coverage matrix stops at AS-139 (plus AS-161 in a side section). Tickets
AS-140…AS-167 exist and several are `done` and offline-testable, yet have no
coverage-matrix row and, in most cases, no detailed scenario. Coverage-matrix
rows for AS-140…AS-167 were added in this pass; the following `done` tickets
still need **detailed test scenarios** in the numbered sections:

| Ticket | Area | Missing scenario |
|---|---|---|
| AS-143 | docs | Confirm the `smith serve` JSON-RPC/WebSocket runtime-flow diagram exists in `docs/architecture/runtime-flows.md`. |
| AS-158 | research | Verify the competitive agent-workflow/sandbox/secrets research spike shipped as a design doc. |
| AS-159 | orchestrator | Verify the orchestrator architecture ADR (boundaries) is Accepted and linked. |
| AS-160 | orchestrator | `smith runs daemon` job-spec/DSL: load `.agent-smith/jobs/*.yaml`, validate fail-closed. |
| AS-161 | orchestrator | Daemon/scheduler/SQLite run store — already sketched in the "AS-161" side section; fold it into the numbered sections + coverage matrix. |
| AS-162 | quality | `go test ./internal/archtest/...` guards that every internal package is accounted for in `package-contracts.md`. |
| AS-163 | orchestrator | Job-spec model + validator unit tests (`go test ./internal/orchestrator/...`). |

The `ready-to-implement` tickets in this range (AS-147…AS-157, AS-164…AS-166)
and the `Pending Debrief` AS-167 only need a "Not implemented / Pending" note so
testers do not mistake absence for a regression.

## Acceptance criteria

- [ ] AS-123 and AS-127 no longer appear as "Not implemented" anywhere in the
      campaign (quick checklist + coverage matrix).
- [ ] The coverage matrix has a row for every ticket AS-140…AS-167 with a
      campaign status matching its frontmatter.
- [ ] Each `done` ticket in the table above has at least one numbered step with a
      concrete action and expected result.
- [ ] Campaign still passes `./scripts/agent-quality-gate.sh` after edits.
