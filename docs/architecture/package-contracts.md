# Package architecture contracts

This note records the **dependency direction** and **ownership** between Agent
Smith's core packages, for humans and agents deciding where new code belongs. It
complements [dependency boundaries](dependency-boundaries.md), which governs
third-party imports, and [interface conventions](interface-conventions.md), which
governs when a seam should be an interface and where it lives; this page governs
how core packages may depend on *each other*.

The most load-bearing rules are enforced by a guard test
(`internal/archtest/layering_test.go`), not by review, so the documentation and
the code cannot silently drift.

## Dependency direction

Dependencies point **inward**, toward stable contracts and leaf packages.
Orchestration, faces, and composition roots depend on the layers below them;
those lower layers never depend back up.

| Layer | Packages | Depends on | Must not depend on |
|---|---|---|---|
| **Schema** | `schema` | (stdlib only) | anything in this module |
| **Render primitives** | `internal/render` | (stdlib only) | anything in this module |
| **Stream I/O primitives** | `internal/streamio` | (stdlib only) | anything in this module |
| **Event log** | `internal/eventlog` | `schema` | projection, provider, loop, faces |
| **Projection** | `internal/projection` | `schema`, `internal/eventlog` | provider, loop, faces |
| **Provider contracts** | `internal/provider` | `schema` | concrete providers, loop, faces |
| **Concrete providers** | `internal/provider/anthropic`, `internal/provider/openai` | `internal/provider`, `schema` | loop, faces, `cmd/*` |
| **Tools** | `internal/tool` (+ `builtin`) | `schema` | loop, faces |
| **Loop** | `internal/loop` | provider contracts, tools, eventlog, projection, budget | faces (`internal/tui`), `cmd/*` |
| **Faces** | `internal/tui` | core packages below | other faces, `cmd/*` |
| **Composition roots** | `cmd/*`, `internal/smithapp` | everything | — |

The enforced contracts (guard test) are the corners most prone to drift:

- provider contracts must not import the concrete providers;
- concrete providers must not import the loop, faces, or `cmd/*`;
- the loop must not import face packages;
- leaf primitives (`internal/render`, `internal/streamio`) must not import any other package in this module.

## Where new code goes

- **A new command** (slash or subcommand): the command's semantics live in
  `internal/command` / `internal/cli`; wiring it into a process belongs in the
  composition root (`cmd/smith`, `internal/smithapp`). Faces only render and
  dispatch. Declare the command once as a `command.Command` descriptor in the
  shared registry — name, summary, examples, scriptability, and its argument
  contract — so the slash command and its `smith <verb>` subcommand can't drift
  (AS-066, AS-090). State arity on the descriptor's `ArgSpec` (`Min`/`Max`, a
  negative `Max` meaning unbounded; a nil `ArgSpec` leaves arity unchecked) and
  let `CheckArity` reject out-of-range argument counts: both faces call it before
  the handler runs, so a usage error reads the same whichever face surfaced it.
  Keep face-specific lexing where it belongs — the TUI lexes a slash line
  (`command.Parse`), the CLI permutes flags ahead of positionals (`flag.FlagSet`)
  — then hand the resulting positionals to the one descriptor. (Threading parsed
  *flags*, not just positional arity, through the shared handler is tracked as
  follow-on work, AS-104.)
- **A new face** (alternate UI): a new `internal/<face>` package that depends on
  the loop and core packages. It must not import another face or `cmd/*`, and the
  loop must not learn about it.
- **A new provider**: a concrete adapter under `internal/provider/<vendor>` that
  implements the `internal/provider` contracts using the standard library. It
  must not import the loop, faces, or `cmd/*`.
- **A new tool**: under `internal/tool` (or `internal/tool/builtin` for the
  shipped set), depending only on `schema` and stdlib. The loop and faces wire
  tools in; a tool never reaches back into them.
- **Application wiring**: shared, face-neutral construction belongs in
  `internal/smithapp`; process-specific entry/composition belongs in `cmd/*`.
- **A new orchestration/dev tool** (e.g. the benchmark suite, AS-030): a
  consumer package like `internal/benchmark` may depend on the loop, providers,
  cost, projection, and tools — it sits at the same layer as a face/composition
  root, so nothing in the inward core may import it. It drives the real loop
  through the public `loop.WithProjector` seam (the naive baseline swaps the
  context policy without forking the loop). Its CLI entry is a thin `cmd/bench`
  composition root. It is not a quality gate (see
  [agent-quality-gates.md](../agent-quality-gates.md)).
- **Shared stream I/O mechanics** (SSE framing, bounded best-effort reads, or
  drain-then-close helpers): the generic primitive goes in `internal/streamio`
  (stdlib-only leaf). Provider-, MCP-, or feature-specific parsing and
  correlation stays package-local and calls the primitive.
- **A shared format helper** (token/count/dollar/timestamp/table formatting for
  textual reports): the generic primitive goes in `internal/render` (stdlib-only
  leaf); feature-specific `Render` logic stays in each feature package and calls
  the primitive.
## Interface convention (AS-091)

Go interfaces here follow **accept interfaces, return concrete structs**:
constructors return concrete types (`*loop.Engine`, `*tool.Runtime`,
`*eventlog.Log`), and a package that *consumes* a capability declares the small
interface it needs **at the consumer**, not at the producer. Classify every
interface as one of three kinds:

- **Product boundary** — a deliberate, central seam that is part of the product's
  shape and has (or will have) several implementations. Keep these where they
  are: `provider.Provider`, `provider.Stream`, `tool.Tool`, `permission.Asker`,
  `loop.Observer`, `subagent.SubAgent`, `subagent.Store`. They are justified by
  vendor normalization, pluggable tools/faces/analyzers, or documented future
  backends — not by a single caller's convenience.
- **Consumer seam** — a tiny interface defined next to the code that uses it,
  naming just the method(s) that code needs, satisfied by a concrete type from
  another package. Prefer this over importing a whole subsystem. Examples: the
  `configReader`/`configDecoder` views over `*config.Config` (see below), and the
  loop's `EventLog` (`Append`/`Events`), `ToolExecutor` (`ExecuteBatch`), and
  `ToolDefs` (`ProviderDefs`) — each names the one or two methods the loop uses
  instead of taking the whole `*eventlog.Log`, `*tool.Runtime`, or
  `*tool.Registry`.
- **Unnecessary abstraction** — a single-implementation interface that exists
  only as indirection. Replace it with the concrete struct or a plain function.

The test pay-off is the point: a consumer seam is a one- or two-method fake, so
tests fake only what they exercise. This does not override the Classical testing
strategy — prefer the real collaborator when it is cheap and deterministic
(the loop's tests still drive a real `*eventlog.Log` and `*tool.Registry`); reach
for a tiny fake only where the real one is awkward at the boundary.

- **Reading config for a feature** (AS-093): `internal/config` stays the generic
  layered substrate (dotted-path getters + `Decode`). A feature that consumes
  config owns a small typed view — a `ConfigFrom` constructor that takes a tiny
  consumer-side reader interface (`interface{ Decode(path string, v any) (bool,
  error) }`, satisfied by `*config.Config`) and returns a concrete validated
  struct. The dotted path strings, type validation, defaulting, and
  tolerate-but-warn (D2) all live with the feature, so dotted keys never spread
  through the composition root and the feature does not import `internal/config`.
  See `budget.ConfigFrom`, `compact.ConfigFrom`, `permission.ConfigFrom`,
  `mcp.ConfigFrom`.
