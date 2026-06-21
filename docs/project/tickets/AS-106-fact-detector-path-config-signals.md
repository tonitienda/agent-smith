---
id: AS-106
title: "Rediscovered-fact detector: path-convergence + config-key signals"
status: done
github_issue: 210
depends_on: [AS-048]
area: living-skills
priority: P2
source: PRD.md D7, §7.20; spun out of AS-048
---

# AS-106 · Fact detector — path & config-key signals

**Status: done**

## Description

AS-048 shipped the rediscovered-fact detector (`internal/factdetector`) with one
mechanical signal: the failed-then-successful **command** pattern. D7 lists two
more durable-fact kinds AS-048 deliberately deferred to keep the first form high
precision rather than noisy:

- **Repeated searches converging on one path** — several `grep`/`glob`/`read`
  calls flailing before a `read` settles on a concrete file path; the durable
  fact is "the thing lives at `<path>`".
- **Config keys/values** — a config key/value discovered the hard way (e.g.
  through a failed run that names the missing/var, then a success once it is set).

Add these as additional candidate kinds reusing the existing
`candidate`/`Finding`/`Ledger`/`Resolve` machinery. The bar stays D7's
high-precision-over-recall: each signal needs a clear trial-and-error link
(prior flailing + a meaningful relation to the resolved fact), never a single
lucky search/read.

## Resolution

- **Path convergence:** ≥2 `grep`/`glob` searches must precede the settling
  `read` (a successful read ends the flail run, so a direct read is never
  flagged), and a significant token of the read path (a path segment or the
  basename, alphanumeric, length ≥3) must appear in one of those search
  patterns. Implemented as `detectPaths` reusing `candidate`/`Ledger`/`Resolve`;
  the resolved path is passed to the save-target resolver so the deepest
  applicable memory file is chosen.
- **Config keys:** a small allow-list of stderr signatures
  (`envVarPatterns`: `unbound variable`, `… (is) not set/unset/required/missing`,
  `environment variable …`) mechanically names a missing `UPPER_SNAKE` env var
  on a failed shell run; a subsequent successful shell run confirms the fix and
  proposes the var. The uppercase convention is the precision filter, so ordinary
  config reads are never flagged. The `*.env`-edit sub-signal was dropped as
  lower-precision; the stderr allow-list covers the failed-then-fixed case
  cleanly. Implemented as `detectConfigKeys`.

## Acceptance criteria

- [ ] A scripted session that flails across searches before reading one file
      proposes that path as a fact, with the search trace as evidence, and no
      false positive on a single direct read.
- [ ] A config key/value discovered through a failed-then-fixed run is proposed
      with its trace; ordinary config reads are not flagged.
- [ ] Precision stays high: the new signals reuse the AS-048 `Ledger` dismissal
      suppression and precision tally, and the existing command tests still pass.

## Dependencies

- AS-048 (detector substrate: `internal/factdetector`, candidate/Finding/Ledger).
