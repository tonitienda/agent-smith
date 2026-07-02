---
id: AS-187
title: Repo-convention rules miner for /init and memory
status: Pending Debrief
github_issue: null
area: capability
priority: P2
depends_on: [AS-039, AS-087, AS-032, AS-034]
source: docs/project/competitors.md
---

# AS-187 · Repo-convention rules miner for `/init` and memory

## Description

Smith's living-skills loop (D7, AS-048 rediscovered-fact detector) learns
from a session's own trial-and-error: it notices when the agent stumbles
onto a durable fact and offers to save it. That's a high-precision signal,
but it only fires after friction has already happened at least once inside
a Smith session — it has nothing to say about an existing, mature repo's
conventions on day one.

Qodo's Custom Rules Miner (announced June 2026) mines a repo's PR/review
history into enforceable conventions instead of waiting for a live agent to
rediscover them. Smith already has a natural home for the mined output —
memory files (AS-032) and skills (AS-034) — and `/init` (AS-039) plus its
model-assisted enrichment pass (AS-087) is already the right entry point to
run this kind of one-time analysis.

This ticket scopes a **rules-miner pass**: read existing repo signal (git
log/blame patterns, PR/review comments if available via the GitHub
integration from the orchestrator wave, lint/formatter configs, existing
CI checks) and propose additions to the project's memory file and/or a
generated skill, each with a citation back to the commits/PRs/config that
justified it — consistent with D3's "every claim has provenance" pattern
already used by AS-048/AS-049.

## Acceptance criteria

- [ ] A rules-miner pass can run standalone or as part of `/init`
      enrichment (AS-087), reading git history and repo config without
      requiring network/GitHub access as a hard dependency (graceful
      degradation when PR/review data isn't available).
- [ ] Proposed conventions are surfaced as a reviewable diff against the
      memory file (or a new skill file) — never auto-applied — mirroring
      the rediscovered-fact detector's user-checkable, one-click-accept UX
      (AS-048).
- [ ] Every proposed convention cites the commits/PRs/config lines that
      justified it.
- [ ] Additive-only (D2): the pass proposes new memory/skill content, never
      rewrites or deletes existing project conventions without an explicit
      user action.
- [ ] Works offline against local git history alone as the baseline case;
      PR/review-comment mining is an enhancement gated on the orchestrator's
      existing GitHub auth (AS-148), not a new credential path.

## Debrief questions

- Is this a genuinely distinct capability from AS-087 (`/init` model-assisted
  enrichment), or should it just be a scope expansion of that ticket? The
  signal source (repo history vs. a one-time draft pass) and the output
  target (memory/skills vs. AGENT.md scaffold) both differ, which argues for
  a separate ticket, but they're close enough to reconsider at debrief.
- How much of "mining PR/review history" is realistic without deeper GitHub
  API scope than the orchestrator wave already needs for AS-147/148/149 —
  is local git-log-only mining (commit message patterns, consistently
  co-changed files, existing lint config) sufficient for a first version?
- Should this overlap with the future review-agent idea noted in
  `competitors.md` (Augment Code row) — i.e. do mined conventions feed a
  later PR-review surface, or stay scoped to onboarding/memory for now?

## Dependencies

[AS-039, AS-087, AS-032, AS-034]
