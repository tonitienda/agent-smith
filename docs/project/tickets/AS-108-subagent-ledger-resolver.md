---
id: AS-108
title: "Persist the rediscovered-fact ledger and wire a memory/skill-aware save-target resolver"
status: done
github_issue: 374
depends_on: [AS-107, AS-032, AS-034]
area: subagents
priority: P2
source: AS-107 open questions; PRD §7.20, Appendix C.1
---

# AS-108 · Persist the fact ledger + memory/skill-aware save-target resolver

**Status: ready to implement**

## Description

AS-107 wired the rediscovered-fact detector (AS-048) into the composition root
with the two cheapest defaults its constructor allows:

- a **nil `Resolve`**, so every proposed fact falls back to the project-root
  memory file (`factdetector.DefaultTarget` = `AGENT.md`); and
- an **in-memory `Ledger`** (`factdetector.NewMemLedger`), so dismissals and the
  precision tally live only for the process's lifetime.

Both are placeholders the detector was explicitly designed to inject from the
consumer (see `factdetector.Resolve` / `Ledger` and the AS-107 open questions).
This ticket replaces them with the real implementations:

- **Save-target resolver (Appendix C.1):** wire a `Resolve` that prefers the
  active skill's memory/contract when a fact is discovered inside a skill scope,
  otherwise the deepest applicable memory file for the files involved
  (`internal/memory`, AS-032 / AS-034), falling back to the project root. This is
  the consumer step that keeps `internal/factdetector` free of `memory`/`skill`
  imports.
- **Persisted dismissal ledger:** back the `Ledger` with a durable store so a
  fact the user declined is not re-offered in a later session, and the precision
  tally (the D7 quality bar) survives a restart. The natural home is the
  cross-session rollup (AS-050); until that lands a small on-disk file under the
  session store is acceptable.

## Resolved decisions

- **Ledger location:** a dedicated `fact-ledger.json` under the project's session
  area (`<store root>/sessions/<project-hash>/fact-ledger.json`), so dismissals
  and the precision tally are shared across every session of that project
  (interactive and headless). Folding it into the insights `Store` waits on that
  store gaining a durable backing (AS-045/AS-057); the file shape is small and
  additive so the migration is a reader change, not a break.
- **Resolver inputs:** the composition root already loads the skill snapshot and
  knows `wd`, so `saveTargetResolver(wd, skills)` builds a name→`SKILL.md` map once
  and runs `memory.Discover` per finding (cheap, stat-only). No per-finding
  re-scan of the skill tree; the active skill comes from `Attribution.Skill` via
  the detector's `Resolve(skill, files)` call. Headless runs pass no skills (no
  skill tool is loaded there) so facts there resolve on the memory tree alone.

## Acceptance criteria

- [x] A discovered fact inside a skill scope proposes saving to that skill's
      memory/contract; outside one, to the deepest applicable memory file; with
      the project-root fallback preserved. (`saveTargetResolver`,
      `TestSaveTargetResolver` / `TestSaveTargetResolverFallback`.)
- [x] A dismissed fact is not re-offered in a fresh session (ledger survives a
      process restart), with a test over two scripted sessions.
      (`factdetector.FileLedger`, `TestFileLedgerSurvivesRestart`.)
- [x] `internal/factdetector` still imports neither `memory` nor `skill` — the
      resolver and ledger are injected from the composition root.

## Dependencies

- AS-107 (the composition-root wiring this enriches), AS-032 (memory files),
  AS-034 (skills) for the resolver.
