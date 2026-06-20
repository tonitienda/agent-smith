---
id: AS-106
title: "Rediscovered-fact detector: path-convergence + config-key signals"
status: ready-to-implement
github_issue: null
depends_on: [AS-048]
area: living-skills
priority: P2
source: PRD.md D7, §7.20; spun out of AS-048
---

# AS-106 · Fact detector — path & config-key signals

**Status: ready to implement**

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

## Open questions

- What is the precision-preserving relation for path convergence? Candidate:
  the settled `read` path's basename (or a path segment) must appear as a token
  in a preceding `grep`/`glob` pattern, and there must be ≥2 prior searches with
  no successful read in between.
- How is a config key/value recognized mechanically without a model call? It may
  need a small allow-list of signals (env-var-not-set stderr, a `*.env`/config
  file edit following a failed run) rather than free-text parsing.

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
