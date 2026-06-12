# Agent instructions

This repository is the Go implementation of Agent Smith. Product truth lives in `docs/project/PRD.md`; read the Decision Log (D0-D9) before making product or architecture decisions.

## Workflow

- Ticket files in `docs/project/tickets/` are the source of truth for GitHub issues. When a ticket is implemented, set its frontmatter `status` to `done`, update the visible status/checklist in the ticket body, and update `docs/project/tickets/README.md`.
- Keep documentation current for both humans and agents. Consider whether `README.md`, this file, `CLAUDE.md`, or focused docs under `docs/` need updates with each code change.
- Before committing Go changes, run `gofmt`, `go test ./...`, `go vet ./...`, and the relevant `make` target(s).

## Go conventions

- Use standard Go project layout: runnable commands under `cmd/`, shared internal packages under `internal/`.
- Keep repo tooling stdlib-only unless a ticket explicitly introduces dependencies.
- Build the user-facing binary through `make build`; it emits a static `./smith` binary by default.
