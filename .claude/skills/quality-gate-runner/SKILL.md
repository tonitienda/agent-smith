---
name: quality-gate-runner
description: >
  Choose, run, and interpret the Agent Smith harness quality gates when
  finishing or handing off code changes. Use when you have edited Go code and
  need to validate it, when deciding between the quick/full/arch checks, when a
  gate fails and you must classify the failure, or when a command can't run in
  the current environment. Triggers: "run the gate", "quality gate", "before
  commit", "is this ready to push", "harness check", "make test/vet/lint".
license: MIT
---

# Quality gate runner

Canonical contract: [`docs/agent-quality-gates.md`](../../../docs/agent-quality-gates.md).
Design: [`docs/projects/harness-quality-system.md`](../../../docs/projects/harness-quality-system.md).
Run everything from the repository root. Do not re-implement the command lists —
call the scripts; they print each command and preserve exit codes.

## Pick the smallest gate that covers your change

- **Editing, inner loop** → `scripts/harness/quick.sh [packages...]`
  (`make fmt` + `go test` on the given packages, default `./...`).
- **Moved packages / added interfaces / changed dependency direction** →
  `scripts/harness/arch.sh` (`go test ./internal/archtest/...`).
- **Before every commit or handoff (required)** → `scripts/harness/full.sh`
  (= `./scripts/agent-quality-gate.sh` = `make fmt test vet lint`).
- **Before pushing a larger branch** → `scripts/harness/ci-local.sh`
  (`make build && make test && make vet && make lint`, in CI job order).

`full` is a superset of `quick` and `arch`. Use `quick`/`arch` only to shorten
the loop, never instead of `full` before handoff.

## Interpret failures

- `make fmt` diff → run it, commit the formatting change.
- `make test` → read the failing test name; reproduce with
  `go test ./path/to/pkg -run TestName`. Architecture contracts
  (`internal/archtest`), schema guard (`cmd/schema-guard`), and CI/local parity
  (`internal/harnessparity`) all run inside `make test`.
- `make vet` → fix the reported construct.
- `make lint` → pinned `golangci-lint` (under `.cache/tools/`); fix or justify.

Each run writes a concise summary under `.cache/harness/` (git-ignored) you can
quote.

## Reporting

Report in the repo's testing-summary format: the command run, its exit status, a
concise failure summary, and the next suggested command. If the environment
cannot execute a command, report it as an **environment warning** with the
command output — never silently skip the gate.
