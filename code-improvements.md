# Code improvement review — Go 1.26 baseline

This review treats the current branch and its open PR work as the baseline. The codebase is already in good shape for a young Go agent: packages are mostly face-agnostic, the schema is explicit, the event log is append-only, and the provider boundary keeps vendor formats out of the loop. The main opportunity now is to make the architecture **smaller, more standard, and easier to evolve** as Go 1.26 and the standard library continue to improve.

The recommendations below favor:

- Go 1.26 and recent stdlib idioms.
- Standard library first, especially for command routing, HTTP/SSE, JSON, filesystem traversal, and test helpers.
- Focused packages with small interfaces.
- Consumer-owned interfaces: receive interfaces, return structs.
- Explicit seams where behavior varies, concrete types where it does not.
- Concise code that remains readable under pressure.

## Highest-leverage improvements

### 1. Split `cmd/smith` into a thin composition root

`cmd/smith` is currently the largest production package by a wide margin. It owns CLI wiring, provider construction, session setup, command registration, TUI/headless entry points, hooks, MCP, permissions, and parity glue. That makes it a practical integration point, but it also means new features tend to add more branching to the executable package instead of landing behind a narrow application seam.

**Recommendation**

Turn `cmd/smith` into a very small composition root:

- Parse process-level flags and subcommands.
- Load config.
- Construct dependencies.
- Call one or two exported application functions from a new focused package, for example `internal/app` or `internal/smithapp`.

Keep the package boundary concrete. Avoid a large `App` interface. A concrete `app.App` struct with small option structs is enough.

**Rationale**

- Smaller executable packages are easier to reason about and test.
- The same application wiring can serve future faces (`smith serve`, headless, ACP) without copying setup logic.
- It creates a natural home for dependency construction without contaminating domain packages.
- It follows “receive interfaces, return structs”: command handlers can receive the small capabilities they need, while constructors return concrete values.

**Potential ticket**: AS-089.

### 2. Move face-independent command semantics out of the TUI/CLI edge

The project already has `internal/command` and `internal/cli`, but command behavior is still spread across command registry/parity code, `cmd/smith`, and feature packages. As slash commands and subcommands grow, the risk is not runtime complexity; it is semantic drift.

**Recommendation**

Introduce a single command-application layer that describes commands in one place:

- Name, aliases, summary, detailed help.
- Argument parsing contract.
- Execution function over a small consumer-side interface.
- Rendered outputs for TUI/headless/CLI.

Use stdlib `flag.FlagSet` for command-specific flags when possible. Avoid inventing more parsing machinery unless the product needs shell-like syntax. Where slash command parsing needs different lexical rules, isolate that in a parser and feed the same command specs.

**Rationale**

- Keeps `/foo` and `smith foo` semantically identical.
- Reduces help-text duplication.
- Makes future faces consume a command catalog instead of reimplementing command behavior.
- Preserves focused packages: feature packages implement behavior; face packages render it.

**Potential ticket**: AS-090.

### 3. Replace package-owned broad interfaces with consumer-side interfaces where possible

The repository has some deliberately central interfaces (`provider.Provider`, `tool.Tool`, `provider.Stream`). Those are justified because they are product boundaries. Elsewhere, we should be stricter: interfaces should usually live with the consumer, not the producer.

**Recommendation**

Audit exported interfaces and callback types, then classify them:

- **Product boundaries**: keep central interfaces (`provider.Provider`, `tool.Tool`, `provider.Stream`).
- **Consumer needs**: move or shrink interfaces to the package that consumes them.
- **Single implementation seams**: prefer concrete structs and functions.

In particular, inspect observer, permission, budget, config, command, and hook seams. When a package only needs “append this block” or “read this value,” define that tiny interface locally instead of accepting a full concrete subsystem or broad interface from another package.

**Rationale**

- Smaller interfaces age better.
- Tests become simpler because fakes implement one or two methods.
- It reduces package coupling without over-abstracting.
- It makes architectural boundaries visible in the code, not just in docs.

**Potential ticket**: AS-091.

### 4. Create a small shared streaming/SSE substrate for providers and MCP

Provider adapters and MCP clients both deal with streaming protocols, line scanning, JSON messages, reconnect/error semantics, and context cancellation. Today those implementations are necessarily separate, but the low-level stream mechanics are likely to drift.

**Recommendation**

Extract only the boring, protocol-agnostic pieces into a focused internal package, for example `internal/streamio`:

- Context-aware line/event reading.
- Safe close/drain helpers.
- Byte-size limits.
- HTTP request helpers that use `http.NewRequestWithContext` and shared client defaults.
- Test fixtures for chunked streams and malformed frames.

Do **not** force providers and MCP behind one abstraction. Keep their domain event parsing in their own packages.

**Rationale**

- Removes duplication without merging unrelated domains.
- Centralizes cancellation and close behavior, which is easy to get subtly wrong.
- Makes future Go stdlib improvements to HTTP/SSE adoption local to one package.
- Keeps vendor normalization code clear and domain-specific.

**Potential ticket**: AS-092.

### 5. Introduce typed configuration views for consumers

`internal/config` is a solid layered substrate, but consumers should not need to know dotted string paths forever. Stringly typed config scales poorly as more features land.

**Recommendation**

Keep the generic layered config core, but add small typed view constructors near consumers:

- `permission.ConfigFrom(config.Reader)`
- `hook.ConfigFrom(config.Reader)`
- `mcp.ConfigFrom(config.Reader)`
- `budget.ConfigFrom(config.Reader)`

Define a tiny consumer-side `Reader` interface where needed:

```go
type configReader interface {
    String(path string) (string, config.Source, bool)
    Bool(path string) (bool, config.Source, bool)
}
```

Return concrete config structs from each consumer package. Those structs should hold validated values, defaults, and provenance needed for warnings.

**Rationale**

- Dotted paths stay localized.
- Feature packages own validation for their own settings.
- Tests can pass a tiny fake reader or a real `config.Config`.
- The core config package remains small and reusable.

**Potential ticket**: AS-093.

### 6. Standardize filesystem traversal on `fs.FS`, `filepath.WalkDir`, and `io/fs`

The repository performs repo scans for memory files, skills, custom commands, init scaffolding, sessions, and ticket sync. These are good candidates for standard-library unification.

**Recommendation**

Where paths are read-only, accept `fs.FS` and return concrete results. Where OS paths are required, keep `os.DirFS(root)` at the edge and normalize paths once. Prefer `filepath.WalkDir` for OS traversal and `fs.WalkDir` for abstract filesystems.

Also consider package-local helpers for:

- “find nearest file upward” for AGENTS/CLAUDE/memory discovery.
- “read bounded text file” with size limits.
- “safe relative path within root”.

**Rationale**

- Improves testability without mocks.
- Makes path handling more explicit and safer.
- Allows future virtual/session filesystems without redesign.
- Keeps stdlib as the default dependency.

**Potential ticket**: AS-094.

### 7. Trim external dependencies at face boundaries only

The current external dependency footprint is dominated by the terminal UI stack. That is reasonable for an interactive TUI, but the core should stay stdlib-only wherever possible.

**Recommendation**

Formalize the rule:

- Core packages (`schema`, event log, projection, provider contracts, loop, cost, config, permissions, tools) should remain stdlib-only unless a ticket explicitly justifies an exception.
- External UI libraries stay inside face packages (`internal/tui`) and executable wiring.
- Markdown rendering and styling dependencies should not leak into command or domain packages.

Add/extend import-boundary tests to enforce this across more packages, not just current personality/TUI boundaries.

**Rationale**

- Keeps the agent embeddable and easier to audit.
- Reduces transitive dependency surface for non-TUI faces.
- Aligns with the project preference for stdlib over dependencies.
- Makes future GUI/serve work less coupled to the terminal implementation.

**Potential ticket**: AS-095.

### 8. Unify renderers around small pure functions

Packages such as cost, composition, compact, clean, budget, and goal render textual views. Some duplication is acceptable, but rendering conventions can drift: table formatting, empty states, timestamps, currency, token counts, and block labels.

**Recommendation**

Create a very small `internal/render` package only for generic formatting primitives:

- Token/count formatting.
- Dollar formatting.
- Timestamp/age formatting.
- Stable table helpers over `text/tabwriter`.
- Plain Markdown-ish section helpers.

Do not put business-specific rendering there. Each feature should keep its own top-level `Render` function.

**Rationale**

- Avoids a “god renderer.”
- Removes copy/paste for tiny formatting details.
- Makes output more consistent across TUI/headless/CLI.
- Keeps tests golden and stable.

**Potential ticket**: AS-096.

### 9. Use Go 1.26 stdlib testing idioms consistently

The tests already look comprehensive, but the codebase can standardize on modern stdlib testing patterns.

**Recommendation**

Audit tests for consistent use of:

- `t.Context()` for cancellation-aware tests where subprocesses, HTTP servers, or streams are involved.
- `t.TempDir()` and `t.Setenv()` rather than manual cleanup.
- `testing/synctest` where concurrent stream/runtime tests need deterministic synchronization, if available and appropriate in Go 1.26.
- `cmp`/`slices`/`maps` stdlib helpers instead of custom loops where clarity improves.
- Table tests that keep setup local and name cases after behavior, not implementation.

**Rationale**

- Less flaky concurrency testing.
- Less cleanup boilerplate.
- Clearer tests around streams, MCP, runtime cancellation, and provider conformance.
- Better use of the Go 1.26 standard library without increasing dependencies.

**Potential ticket**: AS-097.

### 10. Define package-level architectural contracts in docs, then enforce them lightly

The project has good high-level docs and tickets. The next step is to document the package dependency rules that keep the architecture clean.

**Recommendation**

Add a short architecture note under `docs/`, then back the most important rules with tests:

- `schema` imports stdlib only.
- `internal/provider` contract imports `schema` but not concrete providers.
- Concrete providers do not import loop/TUI/cmd.
- `internal/loop` imports provider/tool/eventlog/projection but no face.
- `internal/tui` may import UI dependencies; core packages may not.
- `cmd/*` composes; it should not accumulate reusable domain logic.

**Rationale**

- Helps humans and agents make coherent changes.
- Turns architecture from taste into checkable constraints.
- Avoids slow drift as more tickets land.

**Potential ticket**: AS-098.

## Smaller opportunistic improvements

### Prefer concrete option structs when options grow

Functional options are pleasant for a few knobs, but large option sets can hide required fields and interactions. Where constructors already have many `With...` functions, consider a concrete `Config` struct plus validation.

This is most useful when:

- Options are mostly data.
- Defaults need to be inspected or documented.
- Multiple options interact.

Keep functional options where they protect call-site readability for optional, rare behavior.

### Keep comments high-signal

Package comments are thorough and helpful, but some implementation comments restate history or ticket context more than behavior. Prefer comments that explain invariants, ownership, and gotchas. Move long historical rationale into docs when it is not needed at the edit site.

This can reduce code size without making the project less understandable.

### Prefer `json.Decoder` with limits at trust boundaries

For config files, event logs, tool arguments, provider/MCP streams, and ticket sync inputs, make size limits and unknown-field policy explicit. Use `io.LimitReader` or bounded scanners where appropriate. Avoid `DisallowUnknownFields` for additive formats unless a specific file is intentionally closed.

### Keep provider adapters boring

Provider packages should stay repetitive if that keeps vendor differences obvious. Deduplicate only the mechanics: HTTP, streaming, error classification helpers, and test fixtures. Do not force Anthropic/OpenAI request assembly into a clever generic mapper.

### Add examples for core package APIs

Small `Example...` tests for event log append/read, projection, tool registration, and provider stream collection would double as API documentation. They can prevent future over-abstraction by showing the intended simple path.

## Suggested ticket sequence

The tickets added with this review are intentionally incremental:

1. AS-089: shrink the executable package by introducing an app composition package.
2. AS-090: consolidate command semantics across slash commands and subcommands.
3. AS-091: audit and shrink interfaces.
4. AS-092: extract shared stream I/O mechanics.
5. AS-093: add typed config views.
6. AS-094: standardize filesystem traversal.
7. AS-095: enforce stdlib-first core dependency boundaries.
8. AS-096: add tiny shared render primitives.
9. AS-097: modernize tests for Go 1.26 idioms.
10. AS-098: document and test package architecture contracts.

These should not block feature work. They are best done opportunistically when touching adjacent code, except AS-089 and AS-090, which will make later face/command work significantly cleaner.
