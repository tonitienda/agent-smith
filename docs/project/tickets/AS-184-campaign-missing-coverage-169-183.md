---
id: AS-184
title: Manual test campaign — missing coverage for AS-169…AS-183
status: ready-to-implement
github_issue: null
type: bug
depends_on: [AS-168, AS-169, AS-170, AS-171, AS-172, AS-176, AS-177, AS-178, AS-179, AS-180, AS-181, AS-182, AS-183]
area: quality
priority: P2
source: QA manual-test-campaign pass 2026-07-02
---

# AS-184 · Manual test campaign: no coverage for AS-169…AS-183

## Description

The 2026-07-02 QA campaign pass (`docs/projects/manual-test-campaign.md`) ran the
full campaign against the `make build` binary (commit `5f18d5b`). All automated
suites pass (`make test`, `scripts/harness/arch.sh`, `go test ./internal/e2e/...`,
`go test ./internal/orchestrator/...`) and every documented CLI scenario matched
the product's behaviour — no product defect was found this pass.

One documentation gap was found, of the same class AS-168 was filed for. The
campaign states in its intro that it "covers every ticket in the backlog", but
the ticket coverage matrix stops at **AS-168** (plus the AS-161 side section),
while the backlog now runs to **AS-183**. Tickets AS-169…AS-183 exist and have
no coverage-matrix row, so a tester following the campaign cannot tell whether
they are shipped, deferred, or blocked.

| Ticket | Title | Frontmatter status | Campaign coverage |
|---|---|---|---|
| AS-169 | tool.Runtime couples to concrete `*eventlog.Log` instead of a consumer seam | needs-clarification | missing |
| AS-170 | Orchestrator daemon vs ADR D-ORCH-3 "reuses the async runner" reconciliation | needs-clarification | missing |
| AS-171 | Outbound completion notifications for background & orchestrator runs | Pending Debrief | missing |
| AS-172 | Team credential gateway and cross-session prompt/tool audit trail | Pending Debrief | missing |
| AS-176 | Wails desktop shell bootstrap over Smith core adapter | ready-to-implement | missing |
| AS-177 | Desktop embedded runtime lifecycle and app state | ready-to-implement | missing |
| AS-178 | Desktop interactive transcript and composer | ready-to-implement | missing |
| AS-179 | Desktop tool activity and permission rail | ready-to-implement | missing |
| AS-180 | Desktop home, recent workspaces, and session resume | ready-to-implement | missing |
| AS-181 | Desktop context and cost rail | ready-to-implement | missing |
| AS-182 | Desktop settings, runtime status, and auth guidance | ready-to-implement | missing |
| AS-183 | Wails desktop packaging, signing, updates, and smoke tests | ready-to-implement | missing |

(AS-173, AS-174, AS-175 were never allocated.)

None of these tickets are `done`, so none is a shipped-but-untested feature; the
required fix is documentation-only — every one needs a coverage-matrix row with a
campaign status matching its frontmatter, and a "Not implemented / Needs
clarification / Pending Debrief" note so testers do not mistake absence for a
regression. Coverage-matrix rows for AS-169…AS-183 were added in the 2026-07-02
pass; this ticket tracks folding the desktop wave (AS-176…AS-183) into a proper
numbered section once any of it ships.

## Acceptance criteria

- [ ] The coverage matrix has a row for every ticket AS-169…AS-183 with a
      campaign status matching its frontmatter.
- [ ] The `Not implemented` / `Needs clarification` / `Pending Debrief` tickets in
      this range carry a note so testers do not mistake absence for a regression.
- [ ] When any desktop-wave ticket (AS-176…AS-183) reaches `done`, it gains a
      detailed numbered scenario with a concrete action and expected result.
- [ ] Campaign still passes `./scripts/agent-quality-gate.sh` after edits.
