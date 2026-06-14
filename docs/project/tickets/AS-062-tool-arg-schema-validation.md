---
id: AS-062
title: Fuller JSON-Schema validation for tool arguments
status: ready-to-implement
github_issue: 84
depends_on: [AS-013]
area: tools
priority: P2
source: AS-013 follow-on
---

# AS-062 · Fuller JSON-Schema validation for tool arguments

**Status: ready to implement**

## Description

The tool runtime (AS-013) validates a model's tool-call arguments against a
**deliberately minimal** subset of JSON Schema before execution: top-level
`type: object`, `required`, top-level property scalar/array/object `type`, and
`additionalProperties: false`. Everything else (nested object/array schemas,
`enum`, `format`, numeric bounds like `minimum`/`maximum`, string
`minLength`/`pattern`, and composition keywords `oneOf`/`anyOf`/`allOf`) is
ignored, so the validator never rejects a call it cannot fully model. That keeps
V1 honest and crash-free but lets some clearly-invalid calls through to the tool.

This ticket widens coverage so more bad calls are caught *before* execution and
turned into model-readable errors, reducing wasted tool runs and giving the model
sharper feedback.

- Validate nested schemas (`properties` of object properties, `items` of arrays).
- Support `enum`, numeric bounds, string `minLength`/`maxLength`/`pattern`,
  and array `minItems`/`maxItems`.
- Support `type` as an array (union types) instead of skipping the check.
- Keep the "lenient on the unmodeled" stance: unknown keywords are still ignored,
  never a false rejection.
- Stdlib-only unless this ticket explicitly introduces a vetted JSON-Schema
  dependency; if a dependency is proposed, justify it against the repo's
  stdlib-only default and the additive-schema discipline.

## Acceptance criteria

- [ ] A call violating a nested/enum/bounds constraint produces a model-readable
      error before the tool runs.
- [ ] Unknown/unsupported keywords still never cause a false rejection.
- [ ] The error message names the offending property and the constraint.
- [ ] Existing AS-013 validation behavior (object/required/scalar/`additionalProperties`)
      is preserved.

## Dependencies

- AS-013 (the runtime and the `validateArgs` seam this extends)
