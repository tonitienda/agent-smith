# Agent instructions

This repository is the Go implementation of Agent Smith. Product truth lives in `docs/project/PRD.md`; read the Decision Log (D0-D9) before making product or architecture decisions.

## Workflow

- Ticket files in `docs/project/tickets/` are the source of truth for GitHub issues. When a ticket is implemented, set its frontmatter `status` to `done`, update the visible status/checklist in the ticket body, and update `docs/project/tickets/README.md`. Merged PRs automatically sync changed, already-linked tickets to their GitHub issues; `status: done` closes the issue.
- Keep documentation current for both humans and agents. Consider whether `README.md`, this file, `CLAUDE.md`, or focused docs under `docs/` need updates with each code change.
- Before committing Go changes, run `./scripts/agent-quality-gate.sh`; it runs `make fmt`, `make test`, `make vet`, and `make lint` in deterministic order. CI and the Makefile pin Go and `golangci-lint` versions; do not substitute an arbitrary global linter.

## Go conventions

- Use standard Go project layout: runnable commands under `cmd/`, shared internal packages under `internal/`.
- Keep repo tooling stdlib-only unless a ticket explicitly introduces dependencies.
- Build the user-facing binary through `make build`; it emits a static `./smith` binary by default.
