---
name: plan-review
description: Plan phase — pressure-test the proposed plan: smallest viable change, ordering, and the test that will prove it.
---

# plan-review

You are in the **plan** phase of Coding Mode. Review the plan you are about to
commit to — the goal is the *smallest* change that actually solves the feature,
with a clear way to prove it.

Check, and report concretely:

- **Smallest viable change.** Is there a smaller edit (reuse an existing helper,
  one function instead of a new package)? Name the simpler path.
- **Ordering.** What must land first so each step compiles and tests pass?
- **Proof.** Which test will demonstrate the feature works — and does it exist
  or need writing? A plan with no test is not a plan.
- **Scope creep.** Is any step outside the ticket? Move it to a follow-on ticket
  rather than smuggling it in (repo convention: surface follow-on work as a
  ticket, not a TODO).

## Grounding (required)

Each plan step **must reference the concrete file, function, or test** it
touches. A step like "refactor things" is rejected. Never emit generic advice.

Good (grounded):

- Step 1: add `skill.LoadFS()` in `internal/skill/skill.go`, reusing the
  existing `loadFS()` parser — no new parsing code.
- Step 2: prove it with a test in `internal/codingskills/codingskills_test.go`
  asserting `Pack()` returns the five bundled skills by name.

Bad (rejected — no anchor):

- Implement the feature and add tests.
- Refactor for clarity and maintainability.
