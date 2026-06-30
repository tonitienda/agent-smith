# Agent Smith — working notes for Claude

Provider-agnostic coding agent in Go. **Product truth: the Decision Log [docs/project/decisions.md](docs/project/decisions.md) (D0–D9) — read it first; it overrides the rest of [docs/project/PRD.md](docs/project/PRD.md) where they conflict.**

## Always (cheap invariants — keep these in head)

- **Go layout:** runnable commands under `cmd/`, shared code under `internal/`. `cmd/smith` is the process entry / subcommand composition root; reusable Smith wiring belongs in `internal/smithapp`.
- Repo tooling is **stdlib-only** unless a ticket explicitly introduces a dependency.
- **Before every commit/handoff the full gate must pass:** `./scripts/agent-quality-gate.sh` (`make fmt`, `make test`, `make vet`, `make lint`) — wire it into a pre-handoff hook. Build the user binary via `make build` (static `./smith`).
- **Scope (PRD D6):** V1 = AS-001…AS-030. Don't pull deferred features into V1 tickets; document punts explicitly, never silently (D0).
- **Schema/data design is additive-only (PRD D2)** — including our own formats (tickets, rollups).
- **Keep docs current in the same change:** `README.md`, `CLAUDE.md`, focused `docs/`, and the C4 docs under `docs/architecture/` whenever a change touches runtime seams, data flow, storage, provider/tool boundaries, or user-facing containers.

## Read on demand — don't load these up front

Prefer the **skill**: it pulls only the doc slice the task needs, keeping main context small. Reach for the raw doc only when no skill covers the task.

| When you're… | Use |
|---|---|
| Picking / starting / implementing a ticket, or adding/editing a ticket file | **skill: ticket-implementer** (ticket + deps + PRD Decision Log + arch; also ticket-file conventions) |
| Running or interpreting the quality gates | **skill: quality-gate-runner** |
| Reproducing a red CI job locally | **skill: ci-failure-triage** |
| Opening / reviewing / merging a PR | **skill: pr-workflow** |
| Writing tests | [docs/testing-strategy.md](docs/testing-strategy.md) — Classical: prefer feature/integration tests, mocks only at required boundaries, deterministic/offline by default, fuzz adversarial parsers |
| Placing new code / reasoning about package deps | [docs/architecture/package-contracts.md](docs/architecture/package-contracts.md) (enforced by `internal/archtest`) |
| Needing the full harness command / CI-parity contract | [docs/agent-quality-gates.md](docs/agent-quality-gates.md) |
| Touching the TUI / terminal face | [internal/tui/CLAUDE.md](internal/tui/CLAUDE.md) (visual rules); full spec [docs/design/tui-visual-design.md](docs/design/tui-visual-design.md) |
| Making a product / architecture decision | [docs/project/decisions.md](docs/project/decisions.md) (Decision Log D0–D9) first; wider context in [PRD.md](docs/project/PRD.md) |
