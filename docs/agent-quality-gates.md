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

## Agent integration

- **Claude**: configure a stop/pre-submit hook, or the nearest available project hook, to run `./scripts/agent-quality-gate.sh` before final response or commit.
- **Codex**: use the repository instruction files and the final pre-commit step to run `./scripts/agent-quality-gate.sh`; this is the Codex equivalent of a project hook in this repo.
- **GrokBuild**: configure the project check/hook command to run `./scripts/agent-quality-gate.sh` before submitting changes.

If an environment cannot execute one of the commands, report it as an environment warning and include the command output. Do not silently skip the gate.
