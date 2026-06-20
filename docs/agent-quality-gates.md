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

## Hook integration

Every hook surface delegates to the same `scripts/harness/*.sh` commands instead of embedding its own check list, so Claude, Codex, local Git, and future Smith hooks stay in sync with CI. Hooks are non-magical: the harness scripts print each command before running it, preserve the underlying exit code, and never hide failures from the transcript. Each surface documents a bypass for emergency local workflows.

### Local Git hooks

Install repo-owned Git hooks for this clone (idempotent; re-run any time):

```sh
scripts/harness/install-git-hooks.sh                 # pre-commit -> quick gate
scripts/harness/install-git-hooks.sh --with-pre-push # also full gate on push
scripts/harness/install-git-hooks.sh --uninstall     # restore default hooks
```

The installer points `core.hooksPath` at the tracked [`.githooks/`](../.githooks) directory. `pre-commit` runs [`scripts/harness/quick.sh`](../scripts/harness/quick.sh); `pre-push` is opt-in (gated on `git config harness.prePush`) and runs [`scripts/harness/full.sh`](../scripts/harness/full.sh). Bypass a single commit or push with `--no-verify`.

### Claude hooks

Claude runs project hooks directly. Merge the sample in [`docs/examples/claude-harness-hooks.json`](examples/claude-harness-hooks.json) into your `.claude/settings.json`: a `PostToolUse` hook runs `scripts/harness/quick.sh` after edits for fast feedback, and a `Stop` hook runs `scripts/harness/full.sh` before Claude ends a turn so nothing is handed off un-gated.

### Codex workflow

Codex does not use Claude-style hooks; its equivalent is the repository instruction files plus, optionally, the local Git hooks above:

1. Follow the repo instructions to run the smallest matching harness entry point while working (`quick`/`arch`).
2. Run the local Git hooks by installing them with `scripts/harness/install-git-hooks.sh`, so `git commit` runs the quick gate automatically.
3. Run the **mandatory final full gate** — `scripts/harness/full.sh` (or `./scripts/agent-quality-gate.sh`) — before the final commit/handoff, every time. GrokBuild and other agents do the same from their nearest check/hook feature.

### Future Smith lifecycle hooks

When Smith grows its own lifecycle hooks (e.g. `pre_tool_use`, `user_prompt_submit`), they must call the same `scripts/harness/*.sh` scripts rather than re-implementing the command list. The harness scripts are the single source of truth; Smith hooks only decide *when* to invoke them.

If an environment cannot execute one of the commands, report it as an environment warning and include the command output. Do not silently skip the gate.
