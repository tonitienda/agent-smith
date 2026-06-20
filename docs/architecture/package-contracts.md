# Package architecture contracts

This note records the **dependency direction** and **ownership** between Agent
Smith's core packages, for humans and agents deciding where new code belongs. It
complements [dependency boundaries](dependency-boundaries.md), which governs
third-party imports; this page governs how core packages may depend on *each
other*.

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
  dispatch.
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
- **Shared stream I/O mechanics** (SSE framing, bounded best-effort reads, or
  drain-then-close helpers): the generic primitive goes in `internal/streamio`
  (stdlib-only leaf). Provider-, MCP-, or feature-specific parsing and
  correlation stays package-local and calls the primitive.
- **A shared format helper** (token/count/dollar/timestamp/table formatting for
  textual reports): the generic primitive goes in `internal/render` (stdlib-only
  leaf); feature-specific `Render` logic stays in each feature package and calls
  the primitive.
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
