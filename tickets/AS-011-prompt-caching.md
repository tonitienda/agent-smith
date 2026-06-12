---
id: AS-011
title: Prompt caching support and cache-aware prompt assembly
status: ready-to-implement
github_issue: null
depends_on: [AS-009, AS-010, AS-006]
area: provider
priority: P0
source: PRD.md §7.1, §7.15 (cache transparency portion), D5
---

# AS-011 · Prompt caching support

**Status: ready to implement**

## Description

Use prompt caching wherever the provider supports it (§7.1) — this is foundational to the cost/speed design criterion (D5), and the cache hit data feeds the cost meter.

- Anthropic: place `cache_control` breakpoints sensibly (system prompt / stable prefix / tools).
- OpenAI: automatic caching — capture `cached_tokens` from usage.
- Projection-to-request assembly (AS-006 output → provider request) must be **prefix-stable**: stable ordering and serialization so unchanged context prefixes stay byte-identical across turns and keep hitting cache. Document this as a projection invariant.
- Cache read/write tokens flow into per-turn usage records.

## Acceptance criteria

- [ ] Repeated turns in an unchanged session show cache hits on both providers (verified in smoke tests).
- [ ] Cache savings ($ and tokens) are recorded per turn and available to the cost meter (AS-020).
- [ ] An exclusion event mid-session only invalidates cache from the first changed block onward, not the whole prefix.

## Dependencies

- AS-009, AS-010 (providers)
- AS-006 (projection must guarantee prefix stability)
