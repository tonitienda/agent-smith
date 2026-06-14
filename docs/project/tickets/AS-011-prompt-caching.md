---
id: AS-011
title: Prompt caching support and cache-aware prompt assembly
status: done
github_issue: 11
depends_on: [AS-009, AS-010, AS-006]
area: provider
priority: P0
source: PRD.md §7.1, §7.15 (cache transparency portion), D5
---

# AS-011 · Prompt caching support

**Status: done**

## Description

Use prompt caching wherever the provider supports it (§7.1) — this is foundational to the cost/speed design criterion (D5), and the cache hit data feeds the cost meter.

- Anthropic: place `cache_control` breakpoints sensibly (system prompt / stable prefix / tools).
- OpenAI: automatic caching — capture `cached_tokens` from usage.
- Projection-to-request assembly (AS-006 output → provider request) must be **prefix-stable**: stable ordering and serialization so unchanged context prefixes stay byte-identical across turns and keep hitting cache. Document this as a projection invariant.
- Cache read/write tokens flow into per-turn usage records.

## Acceptance criteria

- [x] Repeated turns in an unchanged session show cache hits on both providers (verified in smoke tests). — Anthropic auto-places `cache_control` breakpoints on the stable system/tools prefix and the conversation prefix (`autoBreakpoints` in `internal/provider/anthropic/request.go`); OpenAI caches automatically. The gated live smoke tests `TestLiveCacheHits` (Anthropic) and the existing OpenAI live turn exercise real cache reads; CI-level tests assert breakpoint placement and prefix-byte stability.
- [x] Cache savings ($ and tokens) are recorded per turn and available to the cost meter (AS-020). — Both adapters normalize `cache_read`/`cache_write` (incl. Anthropic ephemeral 5m/1h) into `schema.Tokens`; AS-020 reads them off the usage records.
- [x] An exclusion event mid-session only invalidates cache from the first changed block onward, not the whole prefix. — Guaranteed by the projection prefix-stability invariant (documented on `projection.Live`, tested by `TestLivePrefixStableBeforeExclusion`): blocks before the first change stay byte-identical, so only the tail re-caches.

## Implementation notes

- **Cache-aware assembly lives in the vendor adapter.** Per the `provider.CacheHints` contract, the zero value defers to the adapter's default placement. The Anthropic adapter auto-places breakpoints (last system block → caches tools+system; last context block → caches the conversation prefix) so caching is on by default without the caller computing breakpoints each turn. Callers can still pass explicit `Breakpoints`, or set the new `CacheHints.Disabled` to opt out.
- **Prefix stability is the projection's job.** `projection.Live` emits live blocks in append order over an immutable, append-only log, so an unchanged prefix serializes byte-identically turn to turn — the precondition for cache hits. Documented as an invariant on `Live` and covered by `TestLivePrefixStableAcrossAppend`.

## Dependencies

- AS-009, AS-010 (providers)
- AS-006 (projection must guarantee prefix stability)
