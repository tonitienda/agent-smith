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
| **Loop** | `internal/loop` | provider contracts, tools, eventlog, projection, budget, subagent | faces (`internal/tui`), `cmd/*` |
| **Faces** | `internal/tui`, `internal/serve` | core packages below | other faces, `cmd/*` |
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
  loop must not learn about it. The terminal face (`internal/tui`) may import the
  Charmbracelet stack; every other face stays stdlib-first like the rest of the
  core. The `internal/serve` JSON-RPC/WebSocket face (AS-077) is the worked
  example: it owns the transport (a hand-rolled minimal RFC 6455 codec) and the
  JSON-RPC dispatch but no business logic — a `serve.Backend` consumer seam
  (implemented by `cmd/smith`) builds the loop/tools/permission gate, so the
  protocol adapter stays a pure translation layer and the loop stays
  face-agnostic. Server→client notifications carry the loop's `UIEvent` stream
  (mapped to `serve.Event` in the composition root, so `serve` never imports the
  loop); an ask-mode permission prompt is forwarded as a server-initiated request,
  failing fast to a denial when the client cannot answer (D-CLI-9 parity).
- **A new provider**: a concrete adapter under `internal/provider/<vendor>` that
  implements the `internal/provider` contracts using the standard library. It
  must not import the loop, faces, or `cmd/*`.
- **A new tool**: under `internal/tool` (or `internal/tool/builtin` for the
  shipped set), depending only on `schema` and stdlib. The loop and faces wire
  tools in; a tool never reaches back into them.
- **Application wiring**: shared, face-neutral construction belongs in
  `internal/smithapp`; process-specific entry/composition belongs in `cmd/*`.
- **User-delegated subagents** (AS-046, the `task` tool): the tool itself
  (`internal/tool/builtin/task.go`) stays a leaf — it depends only on the small
  `builtin.Spawner` consumer seam, never on the loop. The concrete spawner lives
  in `internal/delegate`, an orchestration package (same layer as `benchmark`)
  that may depend on the loop, providers, tools, session store, and cost but must
  not import a face or composition root. It builds a child agent loop over its own
  isolated, persisted `session.CreateChild` log (linked to the parent), runs it,
  and rolls the child's usage onto the parent log as a sidechain so `/cost` and
  the budget guard see the spend. The composition root (`cmd/smith`) wires it on
  every face — interactive, headless (`smith run`), and `serve` (AS-119) — through
  the shared `taskSpawner`/`childTools` helpers (`cmd/smith/delegate.go`): it builds
  the spawner with a `parent func() delegate.Parent` closure (read under the
  controller lock so a mid-session model/session swap is reflected) and registers
  `builtin.NewTask` on the parent registry. The child inherits the parent's
  permission gate per that face's policy (forwarded on the interactive/serve faces,
  fail-fast denied under the headless allowlist-then-deny posture) and its tool
  registry includes the parent's skills (AS-034) and live MCP tools (AS-036,
  borrowed not re-dialled) but deliberately omits `task`, so delegation does not
  recurse. Both contracts are guarded by `internal/archtest` (builtin tools and
  `delegate` must not import a face).
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
- **Living-skills analysis** (the declarative half of skills): contract parsing
  and span tracking live in `internal/skillcontract` (AS-047), a stdlib +
  `schema` + `eventlog` leaf consumed by the rediscovered-fact detector (AS-048)
  and the skill-expectation analyzer (AS-049). It reads skill frontmatter
  (`skill.Skill.Frontmatter`) and the log blocks; it never imports `skill`
  (AS-034), the loop, or a face — the dependency points from the analyzers inward
  to it, the same way `subagent` sits below the loop. The **rediscovered-fact
  detector** (AS-048) lives in `internal/factdetector` as a `subagent.SubAgent`
  built-in: it consumes `subagent` + `schema` only and stays free of `memory`/
  `skill` by injecting its save-target `Resolve` func and its dismissal `Ledger`
  from the consumer. The **skill-expectation analyzer** (AS-049) lives in
  `internal/skillanalyzer` as a `subagent.SubAgent` built-in: it consumes
  `subagent` + `skillcontract` + `schema` (+ `eventlog` for the skill-load marker)
  and stays free of `skill` by taking its catalog as plain `skillanalyzer.Skill`
  values the composition root adapts. It freezes each skill's contract at load
  (declared via `skillcontract.ParseContract`, else inferred from the description),
  then at session end grades each activation against it (verdict / score /
  classification / remedy + concrete diff, Appendix C.2) with no model calls —
  deterministic, opt-in (`EnabledByDefault` false, D7 demotes it until session
  volume exists). The **composition root** owns that consumer wiring (AS-107):
  `cmd/smith`'s `buildSubAgents` registers the built-in factories on a
  `subagent.Registry`, applies the `subagents.<name>` config overlay (C.3) via
  `Registry.Load`, and hands the chat controller / headless run the registry plus
  an insights `subagent.Store`; `buildEngine` then constructs a per-session
  `subagent.Runner` and installs it with `loop.WithSubAgents` (AS-088 gave the loop
  the capability; AS-107 builds and installs the Runner). The store is reachable
  for the `/insights` seam (AS-045). Like `subagent`/`skillcontract` the analyzer
  packages ship substrate-first — registration and the offer UX are consumer
  steps, not their concern. The store the composition root hands in is the
  **durable cross-session rollup** (AS-050) when a session store is present:
  `internal/skillrollup` implements `subagent.Store`, mirroring every recorded
  finding to a per-project JSONL log alongside the session store (next to the fact
  ledger) and reading it back as an aggregated `Rollup` — a fact rediscovered in
  `EscalateSessions`+ distinct sessions is escalated, and a `/skills apply`'d
  remedy is resolved by an appended tombstone. The log is additive-only (D2):
  `Record` carries optional json-tagged fields and unknown fields are ignored on
  load. It consumes `subagent` + `render` only and points inward; the in-memory
  `subagent.MemStore` remains the fallback when no session store is wired.
- **Coding Mode process skills** (AS-074): the bundled, per-phase skill pack lives
  in `internal/codingskills` — an `//go:embed`-ed set of `SKILL.md` files parsed
  through `skill.LoadFS` into ordinary `skill.Skill` values (it depends only on
  `skill` + stdlib). The phase→skill-name mapping is data on the phase definitions
  (`mode.PhaseSkills`), so the lifecycle core (`internal/mode`) stays string-only
  and never imports skill content. The composition root (`cmd/smith`) does the
  wiring: on each Coding Mode phase entry it auto-loads the phase's skill bodies as
  system text blocks (producer `coding-mode/skills`, attributed to the skill),
  deduped per `(instance, phase)` and skipped entirely when the mode is off. A
  user/project skill of the same name shadows the bundled one; the grounding
  discipline (D-CODE-8) is the `codingskills.IsGrounded` predicate.
- **Coding Mode project method override** (AS-075): the third opinion layer
  (D-CODE-5.3). A project's memory files (AS-032) may carry a fenced
  ` ```smith-method ` block that reorders phases (`phases:`), drops one (`skip:`),
  or adds a project rule (`rule:`). `mode.ResolveMethod` folds these over
  `mode.DefaultPhases` — string-only, so the lifecycle core still never imports
  `memory` or skill content; the composition root passes the memory block texts
  in. Overrides ride on the log as ordinary memory blocks, so resolution stays
  log-derived (D3) and precedence-ordered (most-specific memory wins). Parsing is
  tolerant and additive (D2): an unrecognised directive is ignored and the
  unspecified parts fall back to the default, so a partial override never fails the
  mode. The resolved phase order drives entry, `/phase`, and the tracker; rules
  surface in the `mode.Panel`.
- **Session retrospective** (`/insights`, AS-045): `internal/insights` analyzes a
  session's blocks into measured signals (cost, costliest turns, repeated reads /
  commands, oversized tool outputs, error loops, live-vs-stale context health) and
  grounded suggestions, with a face-agnostic `Render`. It consumes `cost`,
  `projection`, `render`, and `schema` — pointing inward — and houses the
  **insights-writer** `subagent.SubAgent` built-in (the C.3 `insights_writer`),
  which records the suggestions as findings at session end with no model calls
  (measured-first; the model-assisted rewrite layer is deferred to AS-109). The
  same `Analyze` drives the `/insights` panel, which prices turns and lands a
  suggestion's propose-only memory edit through a shown diff (`/insights apply`).
- **Living-skills report** (`/skills`, AS-050): `internal/skillrollup` is the
  surfacing layer for the living-skills findings — the rediscovered facts (AS-048)
  and skill grades (AS-049) the analyzers report. It renders the current session's
  findings (the per-session view) plus the cross-session rollup, escalating a fact
  that recurs across `EscalateSessions`+ sessions, and lands a pending remedy's
  propose-only diff through `/skills apply <n>`, marking it resolved. Like
  `/insights` it is deterministic and face-agnostic (one `Render` for the TUI panel
  and headless `smith skills`); the confirmed write happens only at the command, never
  from a sub-agent (D9, C.5).
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
