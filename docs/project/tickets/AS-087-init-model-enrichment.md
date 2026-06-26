---
id: AS-087
title: /init model-assisted draft enrichment
status: done
github_issue: 157
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

- [x] Enrichment is opt-in (`/init --describe`) and off by default, so the base
      `/init` stays deterministic and free.
- [x] The model call uses the cheap routing tier (`routing.Cheap`) and is
      budget-capped (`maxOutputTokens` bounds the reply, README sample bounded to
      `maxReadmeBytes`).
- [x] The deterministic build/test/lint section is never replaced by model
      output — enrichment only appends prose sections after the deterministic ones.
- [x] Generated prose still flows through the diff preview + confirm; `--describe`
      stages it into the same `/init` preview → `--apply` flow, nothing is written
      without review.

## Implementation notes (done)

- Seam `initscaffold.Enricher` + `ScanWithEnrichment` keep `internal/initscaffold`
  deterministic and provider-free; the provider-backed enricher lives in
  `internal/initenrich` (mirrors `internal/insightsmodel`) and is wired in
  `cmd/smith` (`setInitEnricher`) for both the TUI and the headless CLI.
- The model is handed the deterministic facts (commands, layout, README sample)
  and told the commands/layout are already documented, so prose adds rather than
  repeats; it returns level-2 Markdown sections, capped at `maxSections`.

## Dependencies

- AS-039 (the `/init` scaffold + `internal/initscaffold` this builds on)
