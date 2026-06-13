# Agent Smith — working notes for Claude

Provider-agnostic coding agent in Go. Product truth lives in [docs/project/PRD.md](docs/project/PRD.md) — read the **Decision Log (D0–D9) first**; it overrides the rest of the document where they conflict.

## Tickets

- Backlog: one file per ticket in `docs/project/tickets/` (`AS-NNN-slug.md`), indexed in its [README](docs/project/tickets/README.md).
- Frontmatter is machine-read by `cmd/ticket-sync` — keep `id`, `title`, `status`, `github_issue`, `depends_on`, `area`, `priority` intact. Ticket IDs are stable: never renumber or reuse.
- `status` is `ready-to-implement`, `needs-clarification`, or `done`; `needs-clarification` tickets must contain an "Open questions" section. New tickets continue the AS-NNN sequence.
- **Ticket numbers do not mark implementation order necessarily. Dependencies and judgement matter more.** Pick the next `ready-to-implement` ticket that makes the most sense to work on now (a `depends_on` chain that is satisfied, a foundational piece that unblocks others) even if a lower-numbered ticket exists. The `depends_on` graph and the "Suggested build order" in the tickets README are the real ordering; the AS-NNN sequence is just stable identity.
- **Surface follow-on work as a ticket, not a TODO.** When a task reveals additional work — a refinement, a validation pass, a discovered gap, a punted decision — create a new `AS-NNN` ticket for it (continue the sequence, file it in `docs/project/tickets/AS-NNN-slug.md`, add it to the README index) so it's tracked properly instead of being lost in a comment or PR description. (Example: AS-060 was spun out of the AS-002 spike.)
- When adding or changing tickets, update the index table in the tickets README.
- **Files are the source of truth over GitHub issues** — edit the file, then sync (`go run ./cmd/ticket-sync`, `-dry-run` to preview, `-all` for everything). The tool writes issue numbers back into frontmatter; never invent a `github_issue` value by hand. Merged PRs automatically sync changed, already-linked tickets to their GitHub issues and close issues whose ticket frontmatter says `status: done`.

## Conventions

- Go: stdlib-only for repo tooling; `gofmt`, `go vet`, `go test ./...`, and relevant `make` targets must pass before committing.
- Scope discipline (PRD D6): V1 = AS-001…AS-030. Don't pull deferred features into V1 tickets; punted/hard problems are documented explicitly, never silently (D0).
- Schema/data design follows additive-only thinking (D2) — applies to our own file formats (tickets, rollups) too.

## Pull requests

- **Always subscribe to PR activity.** As soon as a PR exists for the branch you're working on (whether you opened it or it was created from the Claude Code UI), call `subscribe_pr_activity` for it so you receive CI results and review comments — then follow through: investigate each event, push fixes for failing CI and actionable review feedback, and ask via `AskUserQuestion` when a fix is ambiguous. Keep watching until the PR is merged or closed, or the user tells you to stop (`unsubscribe_pr_activity`).

