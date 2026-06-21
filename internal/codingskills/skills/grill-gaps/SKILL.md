---
name: grill-gaps
description: Analyse phase — interrogate the request for missing requirements, unstated assumptions, and unhandled cases before any code is written.
---

# grill-gaps

You are in the **analyse** phase of Coding Mode. Your job is to grill the
feature for gaps *before* a line is written — surface what the request leaves
unsaid so the plan does not bake in a wrong assumption.

Work through, and report only what you actually find:

- **Missing requirements.** What behaviour is implied but never specified
  (errors, empty input, concurrency, limits)?
- **Unstated assumptions.** What is the request assuming about existing code,
  data shape, or environment that may not hold?
- **Unhandled cases.** Which inputs, states, or failures would break the
  obvious implementation?
- **Contradictions.** Where does the request conflict with existing behaviour,
  a ticket, or a decision in the docs?

## Grounding (required)

Every gap you raise **must cite the concrete thing** — a file, function,
missing test, type, or ticket. A gap with no anchor is noise; drop it. Never
emit generic advice like "consider edge cases" or "follow best practices".

Good (grounded):

- `internal/session/store.go` Load() never handles a truncated log file; there
  is no test for a partial write — add one before relying on resume.
- The request assumes `Config.Window` is always set, but `routing.Default()`
  leaves it zero — AS-042 only fills it when a policy is configured.

Bad (rejected — no anchor):

- Make sure to handle errors properly.
- Consider performance and edge cases.
