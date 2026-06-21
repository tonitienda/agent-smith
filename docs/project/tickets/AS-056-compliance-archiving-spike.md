---
id: AS-056
title: "Design spike: compliance archiving — immutability vs right-to-erasure"
status: done
github_issue: 56
depends_on: [AS-005]
area: compliance
priority: P2
source: PRD.md §7.23, D8, §10 Q13
---

# AS-056 · Design spike: compliance archiving (Q13)

**Status: done** *(a research/design spike — the deliverable is a decision document, not code)*

**Outcome:** [docs/design/compliance-archiving.md](../../design/compliance-archiving.md). The architecture tension is resolved by **layering** (erasure is an archive-lifecycle operation above the intra-session append-only invariant), with **no V1 block-schema change required** (the D2 decision). Recommended: redaction-at-capture (OSS, best-effort) + crypto-shredding at the archive layer (paid, authoritative erasure) + legal-hold. Q13 is narrowed to named legal questions for counsel before the paid archive ships. OSS piece spun out as **AS-115** (redaction-at-capture).

## Description

D8 identifies compliance-grade session archiving as a monetization candidate that falls out of the architecture for free — *except* for one hard tension the PRD says must be resolved **before selling this** (§7.23): the append-only log will hold PII/PHI/secrets, and "never break the log" fights GDPR/HIPAA "must erase a subject on request" (§10 Q13).

Spike scope — evaluate and recommend among (combinations of):
- **Crypto-shredding:** per-subject (or per-session/per-block-class) encryption keys; erasure = key destruction. Key-management burden, granularity, and what "subject" means in a coding-session log.
- **Redaction-at-capture:** classify/redact PII/secrets before they enter the log. False-negative risk; interaction with replay fidelity and `/insights`.
- **Legal-hold semantics:** retention windows, hold flags, and how they compose with erasure requests.
- Tamper-evidence layer sketch: hash-chained events, signed manifests, WORM/BYO-bucket export (S3 Object Lock) — enough design to confirm the open-core boundary (log + local viewing OSS; compliance layer paid).
- **Decision needed early because of D2:** if crypto-shredding requires encryption envelopes *in the block schema*, those fields must be added additively before external consumers depend on raw shapes.

## Acceptance criteria

- [x] A design doc (`docs/design/compliance-archiving.md`) comparing the approaches against GDPR/HIPAA erasure scenarios, with a recommendation.
- [x] Explicit answer to whether schema additions are needed now (and a draft of them if so). — **No V1 schema change required** (doc §4).
- [x] The open-core boundary (OSS vs paid) is drawn concretely (doc §5).
- [x] Q13 in the PRD can be marked resolved, or narrowed to named legal questions for counsel — narrowed (doc §7; PRD §10 Q13 updated).

## Dependencies

- AS-005 (the log being protected); informs future implementation tickets, blocks none of the current backlog
