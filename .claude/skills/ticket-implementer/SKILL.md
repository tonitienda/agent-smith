---
name: ticket-implementer
description: >
  Start an Agent Smith ticket the right way — read the ticket, its dependencies,
  the PRD Decision Log, and the affected architecture/docs before editing. Use
  when picking up an AS-NNN ticket, deciding what to work on next, or before
  touching code for a backlog item. Triggers: "work on AS-", "start a ticket",
  "implement the ticket", "what should I work on next", "pick a ticket".
license: MIT
---

# Ticket implementer

Read before editing — context first, code second.

## Before editing

1. **The ticket**: `docs/project/tickets/AS-NNN-slug.md`. Confirm
   `status: ready-to-implement`; honour its acceptance criteria and Open
   questions. Files are the source of truth over GitHub issues.
2. **Dependencies**: every `depends_on` ticket should be `done` (or at least
   designed). The index in
   [`docs/project/tickets/README.md`](../../../docs/project/tickets/README.md)
   and its "Suggested build order" define real ordering — the AS-NNN sequence is
   just stable identity. Prefer the ticket that unblocks the most or resolves the
   most uncertainty.
3. **Product truth**: the Decision Log
   [`docs/project/decisions.md`](../../../docs/project/decisions.md) — read
   **D0–D9 first**; it overrides the rest of
   [`PRD.md`](../../../docs/project/PRD.md). Stay inside scope (D6: V1 =
   AS-001…AS-030); never pull deferred features in silently (D0).
4. **Architecture**: if the change touches runtime seams, data flow, storage, or
   provider/tool boundaries, read and plan to update the C4 docs under
   [`docs/architecture/`](../../../docs/architecture/) — contracts are guarded by
   `internal/archtest`. New code follows
   [`package-contracts.md`](../../../docs/architecture/package-contracts.md).

## While implementing

- Run `scripts/harness/quick.sh` in the inner loop; see the
  **quality-gate-runner** skill.
- Surface follow-on work as a **new** `AS-NNN` ticket (continue the sequence,
  file it, add it to the README index) — never as a silent TODO.
- Keep docs current in the same change (`README.md`, `CLAUDE.md`, focused docs).

## Before handoff

- Run the full gate `scripts/harness/full.sh` (see **ci-failure-triage** if CI
  is red).
- Set `status: done` in the ticket frontmatter and update the README index. Do
  **not** run `ticket-sync` or close the issue yourself — CI does that on merge.

## Ticket file conventions (when adding/changing a ticket)

- One file per ticket: `docs/project/tickets/AS-NNN-slug.md`, indexed in the
  [README](../../../docs/project/tickets/README.md). Update the index table in
  the same change. **Ticket IDs are stable — never renumber or reuse.**
- Frontmatter is machine-read by `cmd/ticket-sync`; keep `id`, `title`,
  `status`, optional `type`, `github_issue`, `depends_on`, `area`, `priority`
  intact. Never invent a `github_issue` value — the tool writes it back.
- `status` ∈ `ready-to-implement` | `needs-clarification` | `done`. A
  `needs-clarification` ticket must carry an **"Open questions"** section. Use
  `type: bug` for defects.
- Numbering: implementation tickets continue `AS-NNN`; QA/verification/manual-test
  follow-ups use the separate `AS-Q-NNN` sequence (fewer parallel-PR collisions).
  PRs that reuse an existing ID fail CI; if two parallel PRs still collide, the
  merge ticket-sync workflow renumbers and rewrites `depends_on` references.
- **Files are the source of truth over GitHub issues** — land the file change
  and let CI sync/close the issue. `go run ./cmd/ticket-sync -dry-run` is fine
  to *preview* locally; never push issue-state changes from a session.
- When a ticket is created/implemented/clarified/found-buggy during a smoke pass,
  update [`docs/projects/manual-test-campaign.md`](../../../docs/projects/manual-test-campaign.md)
  alongside the ticket file and README index.
