---
id: AS-059
title: "Design spike: third-party sub-agent plugin trust, permissions, and sandboxing"
status: ready-to-implement
github_issue: null
depends_on: [AS-044]
area: security
priority: P1
source: PRD.md §10 Q12, §7.19, D9, Appendix C.5
---

# AS-059 · Design spike: plugin trust & sandboxing (Q12)

**Status: ready to implement** *(a research/design spike — the deliverable is a decision document, not code)*

## Description

Q12 (opened by the plugin decision): third-party sub-agents see transcript context and can propose edits — what permission scopes and sandboxing do they need, and do they ever run untrusted code? D9 sets the v1 line (third-party plugins are **declarative-only**: manifest + prompt, no arbitrary code) — this spike designs what enforcing and eventually relaxing that line looks like.

Spike scope:
- **Permission scope granularity** (C.5 `permissions:`): is `read_transcript` all-or-nothing, or sliceable (own skill span only; redacted transcript; no file contents)? Define the scope vocabulary.
- **Data exposure** — transcripts contain code, paths, secrets-adjacent strings. What does a third-party plugin's context slice exclude by default? Interaction with AS-056's redaction thinking.
- **Declarative-only enforcement** — what technically stops a manifest+prompt plugin from exfiltrating via its model calls or proposed edits? (Prompt-injection-via-plugin is in scope to *describe* even though defense is a documented V1 punt, D9 — punts must be documented, never silent, D0.)
- **The future code question** — if plugins ever run code: WASM, subprocess + OS sandbox, or never? Recommendation with criteria for revisiting.
- Trust UX: install-time consent screen contents, scope display, update semantics.

## Acceptance criteria

- [ ] Design doc (`docs/design/plugin-trust.md`) with a defined permission-scope vocabulary for C.5.
- [ ] Default context-slice exclusions for third-party plugins specified.
- [ ] The declarative-only boundary's enforcement mechanism and its documented residual risks written down (D0 discipline).
- [ ] Q12 marked resolved or narrowed; concrete follow-up implementation tickets drafted.

## Dependencies

- AS-044 (the registry/manifest this governs); informs marketplace work (§7.26, not yet ticketed)
