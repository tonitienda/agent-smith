# Agent quality gates

See also the broader harness design in [`docs/projects/harness-quality-system.md`](projects/harness-quality-system.md). That design defines the planned quick/full/architecture/CI-local command layers, agent hook integrations, skills, and CI-local parity work that should keep local, agent, and CI validation aligned.

All coding agents and humans should use the same repository-owned quality gate before handing off changes:

```sh
./scripts/agent-quality-gate.sh
```

The script intentionally delegates to Make targets so CI, Claude, Codex, GrokBuild, and local development share one deterministic implementation:

1. `make fmt`
2. `make test`
3. `make vet`
4. `make lint`

`make lint` installs and runs the exact `golangci-lint` version pinned in the Makefile under `.cache/tools/` (currently `v2.12.2`). It does not fall back to an arbitrary globally installed binary, so local runs and CI use the same linter version and the same Go toolchain selected for the repository.

## Harness command contract

The harness defines four named entry points, each a thin script under `scripts/harness/`. Agents pick the smallest one that covers what they changed. Every script prints each command before running it, preserves the underlying exit code, and writes a concise (git-ignored) summary under `.cache/harness/`. Run them from the repository root.

| Entry point | When to use | Script | Runs |
| --- | --- | --- | --- |
| **quick** | Inner loop while editing: fast format + affected-package tests for quick feedback. | `scripts/harness/quick.sh [packages...]` | `make fmt`, then `go test` on the given packages (default `./...`) |
| **full** | Before every commit or handoff. The canonical gate; required to pass before pushing. | `scripts/harness/full.sh` | `./scripts/agent-quality-gate.sh` (`make fmt test vet lint`) |
| **arch** | After moving packages, adding interfaces, or changing dependency direction. | `scripts/harness/arch.sh` | `go test ./internal/archtest/...` |
| **ci-local** | Before pushing a larger branch: approximate every CI job locally, in job order. | `scripts/harness/ci-local.sh` | `make build && make test && make vet && make lint` |

`full` is a superset of `quick` and `arch`; running `full` satisfies them. Use `quick`/`arch` only to shorten the inner loop, never as a substitute for `full` before handoff.

### CI/local parity

Each CI job maps to a local command (`scripts/harness/ci-local.sh` runs them in this order). If CI gains or changes a check, update this table and the harness scripts in the same change.

| CI job (`.github/workflows/ci.yml`) | Step | Local command |
| --- | --- | --- |
| `test` (ubuntu + macos) | Build smith | `make build` |
| `test` (ubuntu + macos) | Run unit tests | `make test` |
| `test` (ubuntu + macos) | Run go vet | `make vet` |
| `lint` | Run golangci-lint | `make lint` |

`make fmt` has no separate CI job; formatting drift surfaces as a `make lint`/diff failure, so the full gate runs it first. The architecture contracts (`internal/archtest`) and schema guard (`cmd/schema-guard`) run inside `make test`, so CI's `test` job covers them; the `arch` entry point is a faster subset for after package moves.

### Failure reporting

When a harness command fails, report it in the format the rest of the repository's testing summaries use: the command run, its exit status, a concise failure summary, and the next suggested command. If an environment cannot execute a command, report it as an environment warning with the command output — do not silently skip it. This keeps agent final responses compatible with the testing-summary convention humans and CI already read.

## Agent integration

- **Claude**: configure a stop/pre-submit hook, or the nearest available project hook, to run `./scripts/agent-quality-gate.sh` before final response or commit.
- **Codex**: use the repository instruction files and the final pre-commit step to run `./scripts/agent-quality-gate.sh`; this is the Codex equivalent of a project hook in this repo.
- **GrokBuild**: configure the project check/hook command to run `./scripts/agent-quality-gate.sh` before submitting changes.

If an environment cannot execute one of the commands, report it as an environment warning and include the command output. Do not silently skip the gate.
