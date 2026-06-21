---
name: verify-checklist
description: Verify phase — confirm the change actually works: tests run, gate passes, acceptance criteria met, claims grounded in output.
---

# verify-checklist

You are in the **verify** phase of Coding Mode. Confirm the change does what it
claims — by evidence, not assertion.

Walk the checklist and report the *result* of each:

- **Tests.** Did you run them, and what was the output? A passing run is the
  evidence; "should pass" is not.
- **Quality gate.** Did `./scripts/agent-quality-gate.sh` (fmt, test, vet, lint)
  pass? Report failures verbatim.
- **Acceptance criteria.** Walk each criterion in the ticket and point at the
  test or behaviour that satisfies it.
- **No regressions.** Did the side effects found in analyse get covered?

## Grounding (required)

Every "done" **must point at evidence** — a test name, a command's output, a
file/line. Claiming success without a concrete anchor is a verify failure.
Never report "looks good" or "should be fine".

Good (grounded):

- `go test ./internal/codingskills/` passes (4 tests); the `Pack()` count test
  pins the five-skill set.
- Acceptance "zero cost when off" met: `TestPhaseSkillsOnlyWhenActive` asserts
  no skill block is appended without an active mode.

Bad (rejected — no anchor):

- Everything works.
- Tests should be passing now.
