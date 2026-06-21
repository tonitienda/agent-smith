---
id: AS-115
title: "Redaction-at-capture — best-effort secret/PII scrub before the log (spun out of AS-056)"
status: ready-to-implement
github_issue: null
depends_on: [AS-005, AS-016]
area: compliance
priority: P3
source: AS-056 spike (docs/design/compliance-archiving.md §2.2, §4, §8)
---

# AS-115 · Redaction-at-capture

**Status: ready to implement**

## Description

The AS-056 compliance-archiving spike ([docs/design/compliance-archiving.md](../../design/compliance-archiving.md))
recommends redaction-at-capture as the one **OSS** piece of the compliance story:
classify and scrub obvious secrets/PII **before** they enter the append-only log
(D3), as best-effort data minimization that benefits every local user — not only
the future paid archive tier.

Per the spike (§4) this needs **no new top-level `Block` field**: a redaction is
recorded using the existing derived-block + provenance machinery — a new additive
`redaction` derived kind, `Provenance.DerivedFrom` linking the original block,
`ExcludedBy` set on the original so the raw body leaves the projection, and any
rule metadata in the `ext` escape hatch (all D2-safe, additive-only).

## Scope

- Append-time classifier over inbound block bodies for high-confidence secret
  patterns first (API keys, bearer/`Authorization` tokens, common credential
  formats), with an optional config-driven PII matcher set.
- Structural, self-describing redaction: the redacted body is captured and the
  redaction recorded as provenance, so replay (§7.23) and `/insights` see a
  *documented* redaction rather than a silent loss.
- Composes with the AS-059 plugin scope layer: scope evaluation runs over
  **already-redacted** blocks (plugin-trust.md §2.3) — redaction is data
  minimization, scope is access control; keep them orthogonal.
- Off or best-effort by default; explicitly documented (D0) as defense-in-depth,
  **never** the erasure guarantee (crypto-shredding at the paid archive layer is
  the authoritative mechanism).

## Acceptance criteria

- [ ] High-confidence secrets are redacted at capture before reaching the log.
- [ ] Redaction is recorded structurally (additive `redaction` derived kind +
      `DerivedFrom`/`ExcludedBy`/`ext`); no breaking schema change.
- [ ] Redacted blocks round-trip through projection; replay/`/insights` see the
      redaction marker, not raw secrets.
- [ ] Docs note redaction is best-effort minimization, not an erasure guarantee.

## Dependencies

- AS-005 (the log being written into), AS-016 (security posture). Blocks nothing
  in the current backlog; the paid compliance-archive layer (tamper-evidence,
  crypto-shredding, WORM, legal-hold) stays deferred under D8 and is not ticketed
  until the OSS tool has users.
