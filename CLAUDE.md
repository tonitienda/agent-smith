# Agent Smith — working notes for Claude

Provider-agnostic coding agent in Go. Product truth lives in [docs/project/PRD.md](docs/project/PRD.md) — read the **Decision Log (D0–D9) first**; it overrides the rest of the document where they conflict.

## Tickets

- Backlog: one file per ticket in `docs/project/tickets/` (`AS-NNN-slug.md`), indexed in its [README](docs/project/tickets/README.md).
- Frontmatter is machine-read by `cmd/ticket-sync` — keep `id`, `title`, `status`, `github_issue`, `depends_on`, `area`, `priority` intact. Ticket IDs are stable: never renumber or reuse.
- `status` is `ready-to-implement` or `needs-clarification`; the latter must contain an "Open questions" section. New tickets continue the AS-NNN sequence.
- When adding or changing tickets, update the index table in the tickets README.
- **Files are the source of truth over GitHub issues** — edit the file, then sync (`go run ./cmd/ticket-sync`, `-dry-run` to preview, `-all` for everything). The tool writes issue numbers back into frontmatter; never invent a `github_issue` value by hand.

## Conventions

- Go: stdlib-only for repo tooling; `gofmt`, `go vet`, `go build ./...` must pass before committing.
- Scope discipline (PRD D6): V1 = AS-001…AS-030. Don't pull deferred features into V1 tickets; punted/hard problems are documented explicitly, never silently (D0).
- Schema/data design follows additive-only thinking (D2) — applies to our own file formats (tickets, rollups) too.
