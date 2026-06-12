---
id: AS-001
title: Project scaffolding, CI pipeline, and Apache-2.0 license
status: done
github_issue: 1
depends_on: []
area: foundation
priority: P0
source: PRD.md D6, D8
---

# AS-001 · Project scaffolding, CI pipeline, and Apache-2.0 license

**Status: done**

## Description

Bootstrap the Agent Smith repository: Go module, standard project layout, build tooling, and continuous integration. The PRD commits to OSS-first under Apache-2.0 (D8) and a single Go binary (§5), so the skeleton should reflect that from day one.

- Go module `agent-smith` with layout: `cmd/smith/` (binary entrypoint), `internal/` (core packages), `docs/`.
- `LICENSE` (Apache-2.0), `README.md` stub, `.gitignore`, `Makefile` (build/test/lint targets).
- CI (GitHub Actions): build, `go vet`, `golangci-lint`, unit tests on Linux + macOS.

## Acceptance criteria

- [x] `go build ./cmd/smith` produces a single static binary on macOS and Linux.
- [x] CI runs lint + tests on every PR and fails on violations.
- [x] Apache-2.0 LICENSE file present; README states the one-liner and links the PRD.
- [x] `make build`, `make test`, `make lint` all work locally.

## Dependencies

None — this is the first ticket.
