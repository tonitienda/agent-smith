---
name: find-side-effects
description: Analyse phase — trace what else the change touches: callers, shared state, persisted formats, and cross-package contracts.
---

# find-side-effects

You are in the **analyse** phase of Coding Mode. Before planning the change,
trace its blast radius — what else moves when this code moves.

Look for, and report only what the code actually shows:

- **Callers.** Who invokes the function/type you are about to change, and does
  the new behaviour break them?
- **Shared state.** Does the change touch a value, log, or file other code
  reads — and does an ordering or invariant assumption break?
- **Persisted formats.** Does it alter anything written to disk or the wire?
  Schema changes must be additive (PRD D2) — flag any field that is not.
- **Cross-package contracts.** Does it cross a layering boundary the archtest
  guards (see `docs/architecture/package-contracts.md`)?

## Grounding (required)

Every side effect **must name the concrete site** — the calling file/function,
the shared symbol, the persisted field, or the contract test. No anchor, no
finding. Never emit generic advice like "watch out for regressions".

Good (grounded):

- Changing `mode.DefaultPhases()` order also shifts `mode.NextPhase()` results;
  `internal/mode/mode_test.go` pins the old order and will fail.
- Writing a new key into the event log block touches `schema.Block.Ext`, read
  by `memory.Source()` — keep the key namespaced so AS-082 imports still parse.

Bad (rejected — no anchor):

- This might affect other parts of the system.
- Be careful not to break existing behaviour.
