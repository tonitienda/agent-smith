# Testing strategy

Agent Smith follows the Classical (Detroit/Chicago) school of testing: exercise observable behavior through realistic collaborations, prefer the real implementation of neighboring components, and use test doubles only at architectural or environmental boundaries where the real dependency would make the test slow, flaky, nondeterministic, expensive, or unsafe.

This document is normative for future tickets. When adding or changing behavior, update tests at the highest useful level first, then add narrower tests only where they improve diagnosis or cover hard-to-reach edge cases.

## Goals

- Protect the product promises in the PRD: provider neutrality, append-only event history, additive-only schema evolution, deterministic CLI behavior, and agent-loop safety.
- Keep the default test suite fast, deterministic, offline, and runnable with `go test ./...`.
- Make tests describe user-visible or integration-visible behavior rather than private implementation details.
- Lean on Go's standard library (`testing`, `httptest`, `fstest`, `testing/fstest`, `os`, `io`, `context`, `net/http`, `go/parser`, `encoding/json`, fuzzing, benchmarks) before introducing dependencies.
- Treat coverage as a feedback signal for risk and blind spots, not as a vanity metric that encourages low-value assertions.

## Testing principles

### 1. Test behavior through real collaborations

Prefer tests that drive a command, package boundary, workflow, or adapter as production code would. Assertions should be about emitted events, files written, command output, normalized provider semantics, or user-facing errors.

Good examples in this codebase:

- `cmd/smith/main_test.go` builds the production command tree with in-memory stdin/stdout/stderr and verifies CLI behavior through the real router wiring instead of calling tiny parser helpers directly.
- `internal/provider/conformance` replays recorded provider HTTP fixtures through real provider adapters and compares normalized semantics shared by all providers.
- `internal/eventlog/fuzz_test.go` writes blocks to a real JSONL log, closes it, reopens it, and checks the append/reload durability contract.
- `internal/schemaguard` checks committed schema descriptors and golden session documents, which protects the real on-disk compatibility contract rather than a single helper function.

### 2. Mock only required boundaries

Mocks and fakes are acceptable when the real dependency is a process, network API, model, clock, terminal, filesystem location outside a temp dir, or other nondeterministic boundary. They should represent a contract, not a guessed internal call sequence.

Use test doubles for:

- LLM/provider APIs in default CI. Use recorded HTTP fixtures or `internal/provider.Mock`; do not call live APIs in `go test ./...`.
- Timeouts, cancellation, and stream errors that are impossible or slow to trigger with the real dependency.
- TTY, stdin/stdout/stderr, environment variables, and config paths that must be controlled by the test.
- Permission prompts, shell commands, MCP servers, or hooks when executing the real thing would be unsafe or flaky.

Avoid:

- Verifying every helper call or exact internal call order unless order is part of the public contract.
- Hand-written mocks for pure in-process collaborators that can be used directly.
- Over-mocking the loop so that provider normalization, projection, tool dispatch, permission decisions, or log appends are never exercised together.

Current acceptable examples:

- `internal/provider/mock.go` is an architectural test double for the provider interface. Loop tests can script provider streams while still exercising the real loop, projection, and tool plumbing.
- Provider conformance tests use `http.Client` plus a file-backed transport, keeping adapter code real while replacing only network I/O.

Counterexamples to avoid in future code:

- A CLI test that calls only `resolvePromptSources` for a new command and never verifies `smith <command>` through `cli.App.Run`.
- A loop test that mocks projection, provider, tool runtime, permissions, event log, and UI sink at once; such a test would mostly prove the mock script, not the product workflow.
- A provider test that asserts private JSON-building helper calls instead of asserting the HTTP request shape and normalized event stream.

### 3. Use the standard library first

Default choices:

- `testing.T`, table tests, subtests, and `t.Helper()` for ordinary tests.
- `t.TempDir()` for isolated files and project config.
- `httptest.Server`, custom `http.RoundTripper`, or raw response fixtures for HTTP boundaries.
- `context.WithTimeout` for cancellation behavior.
- `io.Reader`/`io.Writer` buffers for CLI and TUI-adjacent tests.
- `testing/fstest` where an `fs.FS` boundary exists.
- `cmp`-style third-party assertion libraries should not be added unless a ticket explicitly justifies them; clear `if got != want { t.Fatalf(...) }` assertions are preferred.

### 4. Prefer table tests for rules, scenario tests for workflows

Use table tests for compact domain rules such as budget thresholds, parser cases, permission matching, or config precedence. Use scenario-style tests for workflows spanning several components: CLI command execution, event-log persistence, provider conformance, tool runtime behavior, projection, or agent-loop turns.

### 5. Keep tests deterministic by default

`make test` and CI must not require network, credentials, local global config, wall-clock timing, or the current user's shell state. Use temp dirs, fixed timestamps, committed fixtures, and explicit environment overrides. Live tests may exist, but they must be skipped unless an opt-in environment variable is set and must never run in required CI.

### 6. Test errors and recovery paths as first-class behavior

Agent Smith is a harness: failures are product behavior. Tests should cover malformed config, invalid provider responses, stream termination errors, context cancellation, denied permissions, failing hooks, missing files, and partial writes where the package owns recovery semantics.

### 7. Fuzz where inputs are grammar-like, persisted, or adversarial

Use Go fuzz tests when behavior must hold for many input shapes, especially:

- Append/reload and JSON/JSONL durability.
- CLI parsing, slash-command parsing, custom command template substitution, hook JSON payloads, and config parsing.
- Provider stream parsing (SSE chunks, unicode, partial frames) when the invariant can be checked offline.
- Permission matchers and glob-like syntax.

Fuzz targets must be deterministic, fast per input, and must not use network or credentials. Seed corpora should include realistic examples plus edge cases (empty input, unicode, quotes, newlines, control bytes, malformed JSON where relevant). Keep fuzz assertions focused on invariants: no panic, round-trip stability, parser/renderer consistency, or clear error classification.

Run fuzzing intentionally, for example:

```sh
go test ./internal/eventlog -run=^$ -fuzz=FuzzAppendReload -fuzztime=30s
```

Long fuzz campaigns belong in scheduled CI or local pre-release checks, not every PR.

### 8. Preserve architectural boundaries with tests when useful

Boundary tests are welcome when they encode a product or architecture rule. For example, the TUI package parses imports to ensure the face does not import provider or tool packages. These tests should explain the boundary they protect and should inspect source or behavior through stable rules rather than fragile formatting.

### 9. Golden files and fixtures are contracts

Golden files are appropriate for stable external formats: schemas, serialized sessions, provider fixture replay, rendered help text when user-facing formatting matters, and projection output. They should be deterministic, reviewed like code, and regenerated only through documented commands such as `make schema-baseline` or `make record-fixtures`.

Do not use golden files to freeze incidental whitespace or private helper output that should be free to evolve.

## Coverage policy

Coverage is a risk-management tool. We do not require a single global percentage gate for every PR because this can reward shallow tests and punish low-risk refactors. Instead:

- Every behavior change should include tests that would fail without the change, unless the PR is documentation-only or the rationale for no test is explicit.
- Coverage should be inspected for packages touched by non-trivial code changes with `go test -cover ./...` or a focused package command.
- High-risk packages should trend toward strong branch and error-path coverage: `schema`, `internal/eventlog`, `internal/projection`, `internal/provider`, `internal/loop`, `internal/tool`, `internal/permission`, `internal/config`, and command routing.
- New public package behavior should have positive, negative, and boundary cases.
- Generated code, fixture data, and explicitly live-only tests should not drive coverage decisions.
- Coverage decreases in critical packages should be discussed in the PR body, including whether the uncovered paths are intentionally impossible, covered by integration/golden/fuzz tests, or deferred to a follow-up ticket.

Recommended local commands:

```sh
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go tool cover -html=coverage.out
```

CI may publish coverage artifacts or comments, but coverage should initially be non-blocking except for severe drops in packages that own core contracts. If a numeric gate is later introduced, prefer package-aware thresholds for critical packages over a repository-wide percentage.

## CI behavior by event

### Pull request opened or updated

Required checks should run on every PR update:

- Build the `smith` binary with `make build`.
- Run the deterministic offline suite with `make test` on Linux and macOS.
- Run `make vet`.
- Run `make lint` with the pinned repo-local `golangci-lint` version.
- Optionally produce non-blocking coverage output for reviewer visibility.

PR CI must not require API keys, live provider calls, or network beyond dependency/tool installation. Tests that need secrets must skip by default.

### Pull request merged to the default branch

The default branch should run the same required quality checks as PRs. Merge-only automation may also run repository maintenance jobs, such as ticket-to-issue sync, after the code is checked out at the merge commit.

### Scheduled runs

Scheduled CI should run slower safety nets that are too expensive or noisy for every PR:

- Longer fuzz campaigns for selected fuzz targets.
- Coverage trend collection and reporting.
- Fixture drift checks or live provider smoke tests only when secrets are configured and the workflow is explicitly allowed to use them.
- Ticket sync safety nets and other repository maintenance.

Scheduled live tests should report failures clearly but should not be confused with deterministic PR failures; provider outages or API drift need triage, not blind retries in normal PR CI.

### Checks before a release

Before tagging a release candidate:

- Run `./scripts/agent-quality-gate.sh` locally or in a release workflow.
- Run the full required CI matrix on the release commit.
- Run focused coverage inspection for critical packages and document notable gaps.
- Run relevant fuzz targets for a longer bounded time.
- Run provider conformance replay from committed fixtures.
- If credentials are available, run live provider smoke tests for supported providers and surfaces.
- Rebuild the release binary with the intended version metadata and smoke-test `smith --help`, `smith --version`, and at least one non-interactive command.

### Manual or workflow-dispatch runs

Manual runs may expose opt-in jobs such as live provider smoke tests, fixture recording, schema baseline regeneration validation, or long fuzzing. They must clearly state whether they mutate repository files and should never silently update committed fixtures or schema baselines without a reviewed PR.

## Adding tests checklist

- Does the test exercise the highest useful behavior boundary?
- Are real in-process collaborators used unless a boundary genuinely requires a double?
- Is the test deterministic, offline, and isolated with temp dirs or in-memory I/O?
- Does it cover success, error, and edge cases relevant to the changed behavior?
- Would the test fail for a realistic regression?
- If using a golden or fixture, is the regeneration command documented?
- If adding fuzzing, are seeds realistic and is the invariant stable?
- If coverage drops in a critical package, is the reason documented?
