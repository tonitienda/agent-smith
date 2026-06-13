# Agent Smith — working notes for Claude

Provider-agnostic coding agent in Go. Product truth lives in [docs/project/PRD.md](docs/project/PRD.md) — read the **Decision Log (D0–D9) first**; it overrides the rest of the document where they conflict.


## Repository instructions

- Product truth lives in `docs/project/PRD.md`; read the Decision Log (D0-D9) before making product or architecture decisions.
- Keep documentation current for both humans and agents. Consider whether `README.md`, `CLAUDE.md`, or focused docs under `docs/` need updates with each code change.
- Use standard Go project layout: runnable commands under `cmd/`, shared internal packages under `internal/`.
- Keep repo tooling stdlib-only unless a ticket explicitly introduces dependencies.
- Build the user-facing binary through `make build`; it emits a static `./smith` binary by default.
- All agents (Claude, Codex, GrokBuild) and humans must use `./scripts/agent-quality-gate.sh` before handoff/commit, via hooks or the closest agent-specific equivalent. The gate runs `make fmt`, `make test`, `make vet`, and `make lint`; `make lint` installs and runs the pinned repo-local `golangci-lint` instead of a global binary.

## Tickets

- Backlog: one file per ticket in `docs/project/tickets/` (`AS-NNN-slug.md`), indexed in its [README](docs/project/tickets/README.md).
- Frontmatter is machine-read by `cmd/ticket-sync` — keep `id`, `title`, `status`, `github_issue`, `depends_on`, `area`, `priority` intact. Ticket IDs are stable: never renumber or reuse.
- `status` is `ready-to-implement`, `needs-clarification`, or `done`; `needs-clarification` tickets must contain an "Open questions" section. New tickets continue the AS-NNN sequence.
- **Ticket numbers do not mark implementation order necessarily. Dependencies and judgement matter more.** Pick the next `ready-to-implement` ticket that makes the most sense to work on now (a `depends_on` chain that is satisfied, a foundational piece that unblocks others) even if a lower-numbered ticket exists. The `depends_on` graph and the "Suggested build order" in the tickets README are the real ordering; the AS-NNN sequence is just stable identity.
- **Surface follow-on work as a ticket, not a TODO.** When a task reveals additional work — a refinement, a validation pass, a discovered gap, a punted decision — create a new `AS-NNN` ticket for it (continue the sequence, file it in `docs/project/tickets/AS-NNN-slug.md`, add it to the README index) so it's tracked properly instead of being lost in a comment or PR description. (Example: AS-060 was spun out of the AS-002 spike.)
- When adding or changing tickets, update the index table in the tickets README.
- **Files are the source of truth over GitHub issues** — edit the file, then sync (`go run ./cmd/ticket-sync`, `-dry-run` to preview, `-all` for everything). The tool writes issue numbers back into frontmatter; never invent a `github_issue` value by hand. Merged PRs automatically sync changed, already-linked tickets to their GitHub issues and close issues whose ticket frontmatter says `status: done`.

## Conventions

- Go: stdlib-only for repo tooling; `./scripts/agent-quality-gate.sh` must pass before committing. Configure Claude project hooks (or the nearest equivalent) to run that script before final handoff so `make fmt`, `make test`, `make vet`, and `make lint` match CI. Codex and GrokBuild should use their equivalent pre-submit/check hooks for the same script.
- Scope discipline (PRD D6): V1 = AS-001…AS-030. Don't pull deferred features into V1 tickets; punted/hard problems are documented explicitly, never silently (D0).
- Schema/data design follows additive-only thinking (D2) — applies to our own file formats (tickets, rollups) too.

## Pull requests

- **Always open a PR automatically.** When you finish a unit of work on a feature branch — the quality gate (`./scripts/agent-quality-gate.sh`) passes and the work is committed and pushed — open a pull request for it without waiting to be asked. Give it a clear title and a body summarizing the change, the ticket(s) it closes, and how it was verified. The only exceptions are when a PR already exists for the branch (push to it instead) or the user has explicitly told you not to open one.
- **Always subscribe to PR activity.** As soon as a PR exists for the branch you're working on (whether you opened it or it was created from the Claude Code UI), call `subscribe_pr_activity` for it so you receive CI results and review comments — then follow through: investigate each event, push fixes for failing CI and actionable review feedback, and ask via `AskUserQuestion` when a fix is ambiguous. Keep watching until the PR is merged or closed, or the user tells you to stop (`unsubscribe_pr_activity`).
- **Always close the loop on every review comment.** For each review thread, once you've pushed a change that addresses it (or decided it isn't worth doing), post a short reply on that thread saying what you did (reference the commit) or why you're declining, then mark the thread resolved. Never leave an addressed comment silently — the reviewer must be able to see, thread by thread, what was tackled and what was intentionally skipped.

