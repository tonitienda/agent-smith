---
id: AS-087
title: /init model-assisted draft enrichment
status: ready-to-implement
github_issue: null
depends_on: [AS-039]
area: commands
priority: P2
source: PRD.md §7.16, AS-039
---

# AS-087 · /init model-assisted draft enrichment

**Status: ready to implement**

## Description

AS-039 shipped `/init` with a **deterministic** repo scan: build/test/lint
commands come from Makefile targets and package.json scripts, and the layout
section from conventional source directories. That nails the commands exactly and
costs zero tokens, but the generated AGENT.md is otherwise terse — it does not
describe what the project *is*, its conventions, or anything that requires
reading the code.

This ticket adds an optional model-assisted enrichment pass on top of the
deterministic scaffold: feed the scan facts plus a small sample of the repo
(README, top-level package docs) to a cheap-tier model and have it draft prose
sections (project summary, conventions, gotchas) that the user reviews through
the same `/init` preview → `--apply` flow.

## Acceptance criteria

- [ ] Enrichment is opt-in (a flag, e.g. `/init --describe`) and off by default,
      so the base `/init` stays deterministic and free.
- [ ] The model call uses the cheap/summarizer tier and is budget-capped.
- [ ] The deterministic build/test/lint section is never replaced by model
      output — enrichment only adds prose sections.
- [ ] Generated prose still flows through the diff preview + confirm; nothing is
      written without review.

## Dependencies

- AS-039 (the `/init` scaffold + `internal/initscaffold` this builds on)
