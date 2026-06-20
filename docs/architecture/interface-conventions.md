# Interface conventions

This note records when Agent Smith introduces an interface and where it lives, so
future code follows one rule instead of growing speculative abstractions. It
complements [package contracts](package-contracts.md) (dependency direction) and
the [testing strategy](../testing-strategy.md) (mock only real boundaries).

## The rule

**Accept interfaces, return concrete structs.** A constructor returns a concrete
`*T`; a consumer that needs to vary a collaborator declares the smallest
interface it actually uses, *in the consuming package*. We do not add an
interface for a single implementation on the chance a second one might appear
later (YAGNI) — introduce it when the second implementation actually lands, at
the consumer that needs the choice.

Three classes of interface, and how to treat each:

- **Product boundary** — a contract central to the product thesis: provider
  neutrality, the client-side tool protocol, the safety-prompt seam, the
  sub-agent extension points. These are deliberately stable and may precede a
  second implementation. Keep them.
- **Consumer seam** — a small interface a package declares to decouple itself
  from a concrete collaborator it must not import (a layering rule) or to swap a
  real boundary in tests. It lives with the consumer and names only the methods
  that consumer calls.
- **Unnecessary abstraction** — a single-implementation interface with no
  consumer-side need (no layering wall, no test boundary, no second impl in
  sight). Replace it with the concrete struct or a function value.

## Current classification (audit AS-091)

These are every production interface in the module at the time of the audit.

| Interface | Class | Why |
|---|---|---|
| `provider.Provider`, `provider.Stream` | Product boundary | Provider neutrality (PRD): adapters implement, the loop consumes. |
| `tool.Tool` | Product boundary | The client-side tool protocol every built-in and MCP tool satisfies. |
| `permission.Asker` | Product boundary (consumer seam) | The interactive-approval seam; the active face implements it, the policy consumes it. Safe to swap in tests. |
| `subagent.SubAgent` | Product boundary | The §7.19 sub-agent extension contract; multiple built-ins implement it. |
| `subagent.Store` | Product boundary | The insights-store seam; in-memory today, durable later (AS-045/AS-057). Deliberately ahead of a second impl. |
| `tui.Runner` | Consumer seam | The face declares it so it stays decoupled from `*loop.Engine` (and the provider/tool packages the engine wires). Exemplary. |
| `mcp.transport` | Consumer seam | Two real impls (stdio, HTTP/SSE); the `Client` consumes the smaller interface. |
| `tui.markdownRenderer` | Consumer seam | Lets the model fall back to raw text (nil renderer) and lets tests build a model without a terminal; the glamour impl stays in `transcript.go`. |
| `budget`/`compact`/`mcp`/`permission`/`hook`/`subagent` `configReader` | Consumer seam | The AS-093 typed-config pattern: each feature owns a tiny `interface{ Decode(path, v) (bool, error) }` so it reads its section without importing `internal/config` (consumers depend on config, not the reverse). |

No constructor in the module returns an interface; all return concrete `*T`.
Loop budget seams are function values (`func() float64`, `BudgetReservation`)
rather than interfaces, which is the narrowest form of the same rule.

## Config seam naming

The AS-093 config-reader interface is named **`configReader`** in every feature
package (`budget`, `compact`, `mcp`, `permission`, `hook`, `subagent`). It is the
same one-method shape everywhere; keep the name consistent so the pattern is
greppable. It is intentionally *not* a single shared type — sharing it would
couple the features to a common package and defeat the consumer-side rule.

## Tests

A consumer seam exists partly so tests can substitute a boundary. Prefer the
**real in-process collaborator** when the dependency is not itself a boundary:
the config-reader seam keeps `internal/config` out of feature packages, but tests
of those features build a real `*config.Config` (via `config.FileLayer` /
`config.New`) instead of a hand-written `Decode` fake — the genuine parse →
merge → decode path, deterministic and offline. Reserve hand-written doubles for
real boundaries (provider APIs, the permission prompt, MCP servers, hooks, the
clock, the terminal).
