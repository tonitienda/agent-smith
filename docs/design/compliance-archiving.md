# Compliance Archiving — immutability vs. right-to-erasure — design spike (AS-056)

> Status: **accepted as design input** · Owner: Agent Smith · Spike for PRD **§10 Q13**
> (immutability vs. erasure), grounded in **D2**, **D3**, **D8**, **D9**, **§7.23**.
> Retrieval date for external references: **2026-06-21** (US). Not legal advice — the
> named open questions in §7 are for counsel.

This document answers **Q13**: the append-only block log (D3) is the audit artifact that
makes compliance archiving (§7.23) a near-free monetization candidate (D8) — but the log
holds PII/PHI/secrets, and "never break the log" fights GDPR Art. 17 / HIPAA "erase a
subject on request." It evaluates crypto-shredding, redaction-at-capture, and legal-hold
semantics, draws the open-core line, and — the decision D2 forces **now** — states whether
the V1 block schema needs erasure/encryption fields **before** the V1 freeze.

**Bottom line up front.**
1. The tension only bites where Agent Smith (or a customer's retained archive) becomes a
   **data controller/processor of a retained copy**. On the local-first OSS tool the user
   owns their disk (D9: your machine, your privileges); erasure there is `rm` of a session
   file, already supported. So the hard problem lives entirely in the **paid compliance-
   archive tier**, not in the OSS substrate.
2. **No block-schema changes are required before the V1 freeze.** Crypto-shredding operates
   at the **archive/export envelope**, *outside* `/schema`; redaction-at-capture, if it
   lands, reuses the existing derived-block + `ExcludedBy` machinery plus `ext` — both are
   additive (D2-safe) and neither needs a new top-level `Block` field now (YAGNI).
3. Recommended layering: **redaction-at-capture** (OSS, best-effort data minimization) +
   **crypto-shredding at the archive layer** (paid, the authoritative erasure mechanism) +
   **legal-hold** flags that compose with erasure at that same layer.

---

## 1. Framing — where does the tension actually live?

"Immutability vs. erasure" is only a contradiction once a party has a **legal duty to
both retain and to erase the same bytes**. That duty attaches to a *data controller*, not
to a file format. Three deployment shapes, only one of which has the conflict:

| Shape | Who holds the log | Controller? | Erasure mechanism | Conflict? |
|---|---|---|---|---|
| **A. Local OSS tool** (today) | The user, on their own disk | The user is their own controller | Delete the `<session>.jsonl` (AS-007 already lists/loads/saves; delete is `rm`) | **No.** Append-only is a *within-session* invariant (D3), not a "user may never delete their own files" promise. |
| **B. BYO-bucket archive** (§7.23 paid) | The **customer**, in *their* storage/KMS/region | The customer | The customer applies erasure to their own retained copy; we ship the mechanism | **Yes, but it is the customer's to resolve** — we provide crypto-shredding + legal-hold primitives, they own the policy. |
| **C. Hosted/SaaS archive** (explicitly out of scope, D8) | Agent Smith | Agent Smith | Would require us to operate erasure ourselves | **Yes — and we don't build it now.** D8 defers any cloud tier until the OSS tool has users; §10 Q12/Q6 already park hosting. |

**Consequence.** The append-only invariant (D3) is about *not mutating history mid-session*
so projection, `/rewind`, `/clean`, and replay stay sound — exclusions are appended, never
in-place edits. It was never a promise that a *subject* can't be erased from a *retained
archive*. The two operate at different layers: D3 is the **session log**; erasure is an
**archive-lifecycle** operation. Keeping them at different layers is the whole resolution.

---

## 2. The three candidate mechanisms

### 2.1 Crypto-shredding (the authoritative erasure mechanism — paid layer)

Encrypt log content at rest under keys scoped to an erasure unit; **destroy the key to
erase**. The ciphertext may remain in WORM/Object-Lock storage forever (satisfying
tamper-evident retention) while being permanently unreadable (satisfying erasure). This is
the standard reconciliation for "immutable storage + GDPR" and is what makes WORM and
Art. 17 co-exist at all.

**What is the erasure unit ("subject")?** In a coding session the natural, defensible
granularity is **per-session, with optional per-subject sub-keys**:

- **Per-session key (default).** Each archived session encrypted under its own data key.
  Erasure granularity = one session. Cheap, obvious, matches how users already think
  ("delete that session"). Sufficient for the common request "erase everything from my
  engagement with client X" when sessions map to engagements.
- **Per-subject sub-key (optional, finer).** When a session mixes subjects (e.g. PHI for
  several patients in one debugging session), derive content-encryption keys per declared
  subject tag and envelope-encrypt blocks under the relevant subject key. Erasure = destroy
  that subject's key; the rest of the session stays readable. Higher key-management burden;
  only offered to customers who need it.

**Key hierarchy.** Per-archive KMS root (customer's KMS in BYO-bucket — *no key material we
hold*, which also keeps us out of the controller seat) → per-session/per-subject data keys
wrapped by the root → key destruction = delete the wrapped data key from the manifest. This
is BYO-KMS envelope encryption; nothing exotic.

**Where it lives:** the **archive/export format**, not the block. See §4 — this is the
decision that keeps the V1 schema clean.

### 2.2 Redaction-at-capture (data minimization — OSS, best-effort)

Classify and redact obvious secrets/PII **before** they enter the log: API keys, tokens,
`Authorization` headers, common credential patterns, optionally configurable PII matchers.
Reduces how much sensitive data exists to erase in the first place and is good hygiene even
for purely local use.

**Honest limits (D0 — punts documented, not buried):**
- **False negatives are inherent.** Regex/classifier redaction will miss novel formats; it
  is *defense in depth and data minimization*, **never** the erasure guarantee. Crypto-
  shredding (§2.1) is the authoritative mechanism precisely because redaction can miss.
- **Replay/`/insights` fidelity.** Redacting content changes what replay (§7.23) and
  `/insights` see. Redaction must be **append-time and structural**: capture a redacted
  body and record the redaction as provenance, so the projection is self-describing rather
  than silently lossy. It composes with the AS-059 plugin scope layer, which evaluates over
  **already-redacted** blocks (plugin-trust.md §2.3) — access control and data minimization
  stay orthogonal.
- **Secrets are a capture-layer bug, not a slice-filter job.** Credentials should never
  reach the persisted log; AS-017 already keeps raw keys out of blocks, and the plugin
  slice floor (plugin-trust.md §3 item 4) is backstop, not primary control.

### 2.3 Legal-hold semantics (lifecycle — paid layer)

Retention windows + hold flags that **compose with** erasure: a block/session under legal
hold is exempt from an otherwise-due erasure until the hold lifts; an expired retention
window without a hold makes a session eligible for crypto-shred. This is archive-manifest
metadata (hold flag, retention-until timestamp, hold reason), evaluated by the archive
lifecycle job — again **outside** the per-block schema.

---

## 3. Erasure walked against the regulations

| Scenario | GDPR Art. 17 / HIPAA expectation | Mechanism here |
|---|---|---|
| User deletes their own local session | Subject = controller; their data, their disk | `rm <session>.jsonl` (AS-007). No conflict; D3 is intra-session, not anti-delete. |
| Customer must erase subject X from a retained compliance archive | Erase X's personal data while retaining the rest of the immutable audit trail | Destroy X's per-subject (or per-session) data key → ciphertext is permanently unreadable; WORM retention + tamper-evident manifest stay intact. |
| Regulator/litigation hold blocks an erasure that is otherwise due | Retention duty overrides erasure (Art. 17(3)(b/e)) | Legal-hold flag on the session suspends key destruction until the hold lifts. |
| "Right to erasure" but the data is in an Object-Lock/WORM bucket the customer can't delete from | Immutable storage cannot physically delete | Crypto-shredding is *the* answer: you never delete the object, you destroy the key. The WORM object becomes meaningless ciphertext. |
| Secret accidentally captured into a local log | Minimize blast radius | Redaction-at-capture catches the common cases at append time; what it misses is a capture-layer bug to fix at capture, and is crypto-shredded along with its session in the archive. |

The combination — **redaction reduces the surface, crypto-shredding guarantees erasure,
legal-hold gates it** — covers each scenario without ever mutating an existing block.

---

## 4. The D2 decision: does the V1 block schema need erasure/encryption fields now?

**No.** This is the decision the spike exists to make before the freeze, and the answer is
"keep the substrate clean." Reasoning:

1. **Crypto-shredding is an at-rest property of the *archive*, not a property of a *block*.**
   The natural place for encryption envelopes, key-wrapping, and erasure tombstones is the
   **export/archive container** (the gzipped, hash-chained, signed bundle pushed to the
   customer's bucket per §7.23), which is a *new paid-layer format* with its own schema —
   not `schema.Block`. Putting `encryption:`/`key_id:` fields on every `Block` in the OSS
   substrate would tax every consumer (D1: anyone can build on the raw shape) for a feature
   only the paid archive uses. Wrong layer.

2. **Redaction provenance needs no new top-level field either.** A redaction is naturally a
   **derived block** (the existing `KindCompaction`/derived-block pattern) or an append-time
   transform recorded via the existing `Provenance` (`DerivedFrom`) + `ExcludedBy` +
   `Attribution` machinery, with any extra marker riding in the `ext` escape hatch. The
   substrate already has the shape; redaction reuses it.

3. **D2 makes future additions safe anyway.** The schema is additive-only *from* V1: if a
   redaction marker or an archive-side field genuinely earns first-class status later
   (validated by a real implementation ticket, not speculation), it is added as a new
   **optional** field/kind that older consumers tolerate. Adding it now — unused, unproven —
   maximizes churn for zero payoff and contradicts YAGNI. The right-to-add is preserved by
   D2; the obligation-to-add now is not real.

**Draft of what *would* be added, if and when redaction-at-capture lands (not now):**

```
// Additive, optional — illustrative only; do NOT add to /schema in this ticket.
// A redaction is recorded as a derived block; no new Block envelope field needed.
//   Kind:        "redaction"            // new additive derived kind
//   Provenance.DerivedFrom: [<original block id>]
//   ExcludedBy:  set on the original block id (the raw body leaves the projection)
//   ext["redaction"]: { "rule": "api_key", "ranges": [...], "method": "tokenized" }
```

And, at the **archive layer only** (a separate paid-tier format, not `/schema`):

```
ArchiveManifest {
  KeyWrapping: per-session/per-subject wrapped data keys (BYO-KMS root)
  HashChain:   prev-hash per event (tamper-evidence)
  Signature:   signed manifest
  Holds:       [{ subjectOrSession, retainUntil, reason }]
  Tombstones:  [{ keyId, destroyedAt }]   // erasure = key destroyed, ciphertext kept
}
```

So the only V1-freeze action is the **decision itself**: *no schema change required now.*

---

## 5. Open-core boundary (OSS vs paid)

Drawn concretely, consistent with D8:

| Capability | Tier | Rationale |
|---|---|---|
| Append-only block log (D3) | **OSS** | The substrate/moat (D1). |
| Local session save/list/load + **delete** (`rm`) | **OSS** | User owns their disk (D9); local erasure is trivial. |
| Local viewing / replay / `/insights` over the log | **OSS** | Already built; the audit artifact is open. |
| Redaction-at-capture (best-effort secret/PII scrub) | **OSS** | Hygiene benefits every local user; should not be a paywalled safety feature. |
| Plain export (gzip the session, no crypto) | **OSS** | "Take your data" is table stakes. |
| **Tamper-evidence** (hash-chained events + signed manifest) | **Paid** | Compliance-grade attestation. |
| **WORM retention** (S3 Object Lock orchestration) + **BYO-bucket/KMS/residency** | **Paid** | Enterprise data-governance; no infra for us to run. |
| **Crypto-shredding** (per-session/per-subject keys + key-destruction erasure) | **Paid** | The authoritative erasure mechanism for retained archives. |
| **Legal-hold** (retention windows, hold flags) | **Paid** | Enterprise lifecycle policy. |
| Configurable retention + attestation/export format | **Paid** | Enterprise compliance layer. |

**The line:** *the log and local viewing/redaction/export are OSS; the retained, tamper-
evident, erasable compliance archive is paid.* This matches §7.23's stated open-core line
and keeps the erasure-vs-immutability machinery (the genuinely hard part) in the tier where
a controller relationship actually exists (§1 shape B).

---

## 6. Recommendation

1. **Resolve the contradiction by layering, not by weakening D3.** Append-only stays an
   intra-session invariant (projection/replay soundness). Erasure is an archive-lifecycle
   operation one layer up. They never touch the same bytes at the same layer.
2. **Authoritative erasure = crypto-shredding at the archive layer** (per-session default,
   per-subject optional), **BYO-KMS** so we hold no key material and stay out of the
   controller seat for BYO-bucket customers.
3. **Redaction-at-capture as OSS data minimization**, explicitly documented as best-effort
   defense-in-depth, never the erasure guarantee.
4. **Legal-hold composes with erasure** at the archive layer.
5. **No V1 block-schema change.** Substrate stays clean; D2 preserves the right to add an
   additive redaction marker / archive fields when a real implementation ticket proves the
   need. (See follow-on **AS-115**.)
6. **Do not build the hosted/SaaS archive (shape C) now** — D8 defers it; the controller
   burden it creates is exactly what BYO-bucket avoids.

---

## 7. Q13 resolution — narrowed to named questions for counsel

Q13's *architecture* question is **resolved**: immutability and erasure co-exist via
crypto-shredding at the archive layer + redaction-at-capture + legal-hold, with **no V1
schema change required**. What remains is genuinely legal, not architectural, and is
narrowed to these named items for counsel **before selling the paid archive** (not before
V1, not blocking any current backlog):

1. **Controller/processor determination** for the BYO-bucket model — does shipping the
   crypto-shred mechanism while the customer holds the bucket/KMS keep us a processor (or
   neither)? The architecture is designed to make the customer the controller; confirm.
2. **Crypto-shredding as "erasure" sufficiency** under GDPR Art. 17 and HIPAA for the
   target jurisdictions — key destruction with retained ciphertext is widely accepted, but
   confirm per regulator/sector.
3. **Retention-window law** per regulated sector (HIPAA 6-year, financial-records statutes)
   that drives default legal-hold/retention configuration.
4. **Backup/replica propagation** of key destruction — ensuring shredded keys are also
   destroyed in the customer's backups (a documentation/runbook obligation we hand the
   customer, not a code path).

PRD §10 Q13 should be updated from "open" to **"architecture resolved (this doc); narrowed
to named legal questions for counsel before the paid archive ships."**

---

## 8. Follow-up implementation tickets (drafted, not built here — YAGNI)

These are *future* and **block nothing in the current backlog** (D8 defers monetization
until the OSS tool has users). Filed so the work is tracked, not lost in this doc:

- **AS-115 · Redaction-at-capture (OSS, best-effort secret/PII scrub).** Append-time
  classifier + the additive redaction derived-block shape sketched in §4; the one piece of
  this spike that is OSS and could land independently of any paid tier. Spun out below.
- *(Paid tier, deferred under D8 — not ticketed now, intentionally, per §7.26-style "too far
  out to spec honestly"):* compliance-archive format (hash-chain + signed manifest), BYO-
  KMS crypto-shredding, WORM/Object-Lock orchestration, legal-hold lifecycle. These get
  tickets when the OSS tool has users and the paid tier is greenlit (D8), so they are spec'd
  against a real monetization decision rather than speculatively.
