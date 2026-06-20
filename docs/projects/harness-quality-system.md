# Agent Smith harness quality system

## Purpose

Agent Smith already has unit tests, integration tests, linters, architecture tests, and CI. The harness should make those checks feel like one product-quality system instead of a set of commands an agent may or may not remember to run. The goal is to reduce round trips:

- between local development and CI by running the same commands, with the same tool versions, before handoff;
- between an agent and the model by giving the agent enough project-local context to choose the right checks and fix failures without asking for a new prompt;
- between Claude, Codex, GrokBuild, and humans by expressing the harness in repository-owned scripts, config, skills, and memory files instead of provider-specific tribal knowledge.

## Design principles

1. **Repo-owned truth beats agent-specific behavior.** Claude hooks, Codex instructions, and future Smith hooks should call the same repository scripts rather than each embedding their own check list.
2. **Fast before full.** Agents should run targeted checks while editing, then the complete gate before commit or handoff.
3. **Deterministic and offline by default.** The default harness must not depend on network services, mutable global tool versions, credentials, or provider availability.
4. **One failure report format.** Humans and agents should see the command, exit status, concise failure summary, and next suggested command.
5. **Documented escape hatches.** If a command cannot run in an environment, the agent must report it as an environment warning with output, not silently skip it.
6. **Extensible lifecycle.** The same harness should support Claude hooks today, Codex pre-submit instructions today, and native Smith lifecycle hooks as this project grows.

## Harness layers

### 1. Canonical command layer

Keep `./scripts/agent-quality-gate.sh` as the canonical full gate. It delegates to Make targets so CI, local development, and agents share implementation:

- `make fmt`
- `make test`
- `make vet`
- `make lint`

Add a small set of named harness entry points around it:

- `./scripts/harness/quick.sh` — formatting plus the fastest affected package tests, for inner-loop agent use.
- `./scripts/harness/full.sh` — wrapper around `./scripts/agent-quality-gate.sh`, with structured logging.
- `./scripts/harness/arch.sh` — architecture and package-boundary checks, so agents can run the contract suite directly after moving code.
- `./scripts/harness/ci-local.sh` — a local approximation of CI jobs in job order, used before pushing larger branches.

These scripts should stay thin and stdlib/shell based. The Makefile remains the tool-version pinning surface.

### 2. Agent integration layer

#### Claude

Claude can use project hooks directly. The repository should include a sample Claude hook configuration that calls `./scripts/harness/quick.sh` for edit/stop-time feedback and `./scripts/harness/full.sh` before final handoff or commit.

#### Codex

Codex does not need Claude-style hooks to get most of the value. The Codex equivalent is:

- repository instructions in `AGENTS.md`/`CLAUDE.md` that require the same scripts;
- a documented pre-commit checklist that the agent follows before committing;
- optional local Git hooks installed by a repo script, so Codex benefits from the same guard when it runs `git commit`;
- future Smith lifecycle hooks that can call the same harness scripts when Smith itself is the agent runtime.

Because Codex behavior is instruction-driven, the harness should make the right path easy and explicit: discover changed packages, run quick checks during implementation, run full gate before commit, then summarize every command in the final response.

#### Humans and other agents

Provide one `docs/agent-quality-gates.md` update that explains how to install local hooks and how to run quick/full/arch gates manually. GrokBuild and other agents should be told to call the same scripts from their nearest hook/check feature.

### 3. Skills layer

Add repository skills that encode repeatable project workflows as concise, versioned instructions:

- **quality-gate-runner** — when finishing code changes, choose quick/full/arch checks, run them, interpret common failures, and report skipped commands as warnings.
- **ticket-implementer** — when starting a ticket, read the ticket, dependencies, PRD Decision Log, architecture contracts, and affected docs before editing.
- **arch-boundary-checker** — when moving packages or adding interfaces, run and interpret architecture tests and update architecture docs.
- **ci-failure-triage** — when CI fails, map CI job names to local harness commands and produce the shortest reproduction path.

Skills should not duplicate long project docs. They should point to the canonical docs and scripts and focus on agent decision flow.

### 4. Hook layer

Use multiple hook surfaces, but keep their behavior identical by delegating into the command layer:

- **Claude hooks:** stop/pre-submit hook runs `./scripts/harness/full.sh`; optional post-edit hook runs `./scripts/harness/quick.sh` when cheap.
- **Git hooks:** repo-managed `pre-commit` runs formatting and quick checks; `pre-push` can run full checks when explicitly installed.
- **Smith lifecycle hooks:** once Smith hooks are used for project automation, configure `pre_tool_use` and `user_prompt_submit` examples that remind or run the harness for write-heavy sessions.
- **CI hooks:** CI remains authoritative, but should call the same Make targets and publish the same artifacts/log format.

Hooks must be non-magical: they should print the command they are running, support an documented bypass only for emergency local workflows, and never hide failures from the agent transcript.

### 5. CI/local parity layer

CI should be expressible as a local command sequence. The harness should document which local command maps to each CI job and keep the mapping up to date. If CI adds a new check, a ticket or PR should update the harness docs and local scripts in the same change.

### 6. Feedback artifacts

The harness should generate lightweight artifacts under an ignored directory such as `.cache/harness/`:

- latest command log;
- JSON summary with command, status, duration, and failure classifier;
- optional JUnit or test output when available.

These artifacts let agents quote exact failures, compare repeated runs, and avoid re-asking the model what happened.

## Proposed implementation sequence

1. Document the harness contract and create tickets for scripts, hooks, skills, and CI parity.
2. Add harness scripts and structured output while preserving `./scripts/agent-quality-gate.sh` behavior.
3. Add local Git hook installer and sample Claude/Codex integration docs.
4. Add repository skills for quality-gate running and CI failure triage.
5. Add CI/local parity checks so CI fails if a required local harness mapping is missing.
6. Iterate based on real failure transcripts and keep the harness small enough that agents actually run it.

## Non-goals

- Replacing CI. The harness should reduce CI surprises, not become a second CI system.
- Provider-specific lock-in. Claude hooks are useful, but the core harness must work for Codex and humans.
- Networked or model-backed default checks. Model-assisted review can be optional, but the default gate must be deterministic.
