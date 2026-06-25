# Offline end-to-end regression suite (AS-134)

`internal/e2e` drives a **full Smith session** — the same loop, tool runtime,
append-only log, cost accounting, and sub-agent delegation the executable faces
wire — against the recorded vendor simulators from AS-133. It catches expensive
whole-session regressions (large tool payloads, parallel tool calls, denied
permissions, nested delegation, resume) **offline**: no vendor API keys, no live
network, deterministic.

## How it runs

The suite is plain Go tests, so it runs inside `make test` with the rest of the
quality gate and inside CI's existing `test` job — no separate workflow, build
tag, or env var. There is nothing extra to invoke:

```sh
go test ./internal/e2e/...      # or: make test
```

Each scenario:

1. Scripts an ordered list of provider `turn`s (built with `textTurn` /
   `toolTurn` in `sse.go`, which emit Anthropic Messages SSE matching the
   conformance fixtures).
2. Seeds a temp working directory and a disk-backed session.
3. Runs scripted prompts through the real `loop.Engine`.
4. Asserts on the transcript, the face-agnostic `loop.UIEvent` stream the TUI
   renders (tool cards, turn lifecycle — verified without a terminal), token/cost
   accounting, the per-child delegation ledger, and the on-disk JSONL — including
   that reopening the session reprojects identically and mutates no prior event.

The simulator validates each request in order and `AssertSimulatorDrained` fails
with an actionable diff on any divergence (an extra turn, a dropped tool result,
a mismatched body substring).

## Adding or changing a scenario

Add a `Test…` function in `e2e_test.go`. Use `New(t, []turn{…}, opts…)`:

- `tools(id, calls…)` — a `tool_use` turn requesting client tool calls.
- `answer(id, text)` — a final `end_turn` text turn.
- a raw `turn{body: …, bodyContains: […]}` when you need to assert the request
  the adapter serialized for that turn (e.g. a tool result fed back).
- options: `WithFile(path, content)`, `WithPermission(fn)`, `WithDelegation()`,
  `WithEngineOption(loop.Option)`.

Tool call ids and tool names in a scripted turn must match a registered built-in
tool (`read`, `write`, `edit`, `glob`, `grep`, `shell`, and `task` under
`WithDelegation`). The simulator serves turns in script order across both parent
and child engines, so a delegation scenario lists the parent's tool turn, then
the child's turns, then the parent's final turn.

## Intended schema evolution vs a regression

These scenarios assert on the **on-disk JSONL** and its projection. A change to
the block schema, the loop's block output, or the projection will make a scenario
fail. Tell the two apart:

- **Regression** — the diff shows lost/mutated content, a dropped or reordered
  event, a missing tool result, a changed tool-call id, or wrong cost
  attribution. The session behaviour changed; fix the code.
- **Intended schema evolution** — the diff is a *new additive field* or a
  deliberate, documented change consistent with PRD **D2 (additive-only)** and
  the block schema guard (`cmd/schema-guard`, `internal/schemaguard`). Update the
  affected assertion in the same change, and make sure the schema guard and its
  goldens were updated too. If a field was removed or repurposed (not additive),
  that is a D2 violation, not a refresh — stop and reconsider.

Unlike the provider conformance fixtures, this suite has **no golden files to
re-record**: the provider turns are authored inline as SSE, so "refreshing a
golden" means editing the scripted `turn`s and the assertions directly, in the
test. Keep payloads small except where a scenario's point is a large
request/response pair.
