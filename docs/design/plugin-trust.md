# Plugin Trust, Permissions & Sandboxing — design spike (AS-059)

> Status: **accepted as design input** · Owner: Agent Smith · Spike for PRD **§10 Q12** (plugin trust depth), grounded in **D9**, **§7.19**, **Appendix C.5**.
> Retrieval date for external references: **2026-06-21** (US).

This document answers **Q12**: third-party sub-agents see transcript context and can
propose edits — what permission scopes and sandboxing do they need, and do they ever
run untrusted code? It designs what *enforcing* the v1 line costs and what *relaxing*
it later would require.

The v1 line is already set by **D9**: third-party plugins are **declarative-only** — a
manifest (data) plus a prompt, **no arbitrary code**. AS-044 already enforces this in
`internal/subagent`: `LoadManifest` parses a manifest with `DisallowUnknownFields`,
validates it, and wraps it in a `declarative` sub-agent whose lifecycle methods are
**no-ops**. A third-party sub-agent contributes a declaration only; it never executes
third-party logic. This spike does not undo that — it makes the *trust model around it*
explicit, defines the permission vocabulary the manifest's `permissions:` field needs,
and writes down the residual risks the declarative line does **not** close (D0: punts
documented, never silent).

---

## 1. What exists today (the trust surface)

`internal/subagent` (AS-044, AS-088, AS-107, AS-108) is the registry every sub-agent —
first-party and third-party — loads through. The current trust-relevant facts:

| Property | Current state | Trust consequence |
|---|---|---|
| Third-party code execution | **None.** `declarative.Init/Observe/Teardown` are no-ops. | No arbitrary-code RCE surface from a plugin. The biggest threat class is closed by construction. |
| Manifest parsing | `ParseManifest` is strict JSON (`DisallowUnknownFields`) + `Validate`. | A malformed/over-claiming manifest is rejected at load, not mis-driven at run. |
| Permission vocabulary | Two values: `read_transcript`, `propose_edit`. `Allows(p)` reports a claim. | Coarse. `read_transcript` is all-or-nothing; nothing yet *consumes* the claim to gate data. |
| Context slice | `Teardown(scope, slice []schema.Block)` receives the raw block slice. | A declarative sub-agent gets the **whole** slice today (though it does nothing with it). Once a *model-using* third-party kind exists, this is the exfiltration channel. |
| Edits | `propose_edit` is propose-only by design; the framework "never lets it write" (comment on `Permission`). | Correct, but no proposal *type* is plumbed yet — `Result.Findings` carry no edit payload. |
| Budget | Per-sub-agent `BudgetUSD` cap via the AS-041 `budget.Guard`. | Spend is bounded. Relevant once a plugin makes model calls. |

**Key observation.** The declarative wrapper means the permission vocabulary is, today,
*advisory metadata with no enforcement consumer* — a declarative sub-agent can claim
`read_transcript` and `propose_edit` and do nothing with either. The vocabulary becomes
load-bearing only when one of two things lands: (a) a model-using third-party sub-agent
kind (D9 punt; not v1), or (b) `propose_edit` proposals actually flow back as applyable
diffs (AS-046 / future). The design below is written so the vocabulary is **correct now
and enforceable when those consumers arrive**, without a breaking change (D2 additive).

---

## 2. Permission-scope vocabulary (Appendix C.5 `permissions:`)

The current `read_transcript` / `propose_edit` pair is too coarse: a `compliance-checker`
that only needs to see *which files changed* should not receive secrets-adjacent text from
unrelated turns. We define a small, additive scope vocabulary. Scopes are **subtractive by
default**: a plugin sees the least, and each scope *widens* what its context slice
contains. Unknown scopes are rejected at load (existing `validPermissions` gate).

### 2.1 Read scopes (what the teardown slice contains)

| Scope | Grants | Default for 3rd-party |
|---|---|---|
| `read_metadata` | Block envelopes only — kinds, IDs, timestamps, token counts, tool *names*. No content bodies. | **granted** (cheapest useful slice) |
| `read_own_span` | Full content of blocks **inside the plugin's own declared scope/span**, nothing outside it. | opt-in (consent) |
| `read_transcript` | Full content of the whole context slice handed to teardown (today's behavior). | opt-in (loud consent) |
| `read_file_contents` | `file_read` / edit block *bodies* (source code) within the granted read scope. | opt-in (loud consent) — separable because code is the highest-value exfil target |

`read_transcript` is **retained** (additive-compatible) as the widest read scope; the new
scopes slice *below* it. A plugin that names only `read_transcript` keeps today's
semantics. Built-ins (first-party) are exempt from the subtractive default — they ship
in-tree and are trusted — but still *declare* scopes for honesty and for `/insights`/audit
display.

### 2.2 Write/propose scopes

| Scope | Grants |
|---|---|
| `propose_edit` | Attach a proposed diff to a finding. **Propose-only, retained.** Never applied without explicit user confirm (D9). |
| `propose_message` | Surface a suggested chat message / note (weaker than an edit). |

There is deliberately **no** `write_file`, `run_shell`, or `network` scope for third-party
plugins in v1 — those are exactly what declarative-only forbids. They are listed here as
**reserved/forbidden** so the absence is explicit, not an oversight.

### 2.3 Redaction interaction (AS-056)

`read_*` scopes define *which blocks* a plugin sees; AS-056's redaction-at-capture defines
*what is inside* a block. They compose: even at `read_transcript`, a plugin sees the
**post-redaction** content if AS-056 lands redaction. The two are orthogonal layers and
should stay so — scope is access control, redaction is data minimization. This spike does
not require AS-056 to ship first; it requires that, when it does, scope evaluation runs
**over already-redacted blocks** (a one-line ordering constraint for the future enforcer).

---

## 3. Default context-slice exclusions for third-party plugins

When (future) a third-party sub-agent receives a teardown slice, the framework derives the
slice from its granted scopes. Defaults for an *untrusted* (third-party) plugin, **before**
any consent-granted widening:

1. **Start from `read_metadata`** — envelopes only.
2. **Exclude file/edit block bodies** unless `read_file_contents` is granted (code is the
   highest-value exfil target).
3. **Exclude blocks outside the plugin's declared `scope`** unless `read_transcript` is
   granted (`read_own_span` is the middle ground).
4. **Exclude provider/auth metadata** always — API keys, key-storage handles, raw auth
   headers never enter any slice for any plugin (first- or third-party). This is not a
   grantable scope; it is a hard floor. **Note this floor is defense-in-depth, not the
   primary control:** credentials/auth metadata should never reach the *persisted event
   log* in the first place — they are scrubbed at the capture layer (AS-056 redaction-at-
   capture, and the key-storage path in AS-017 never puts raw keys in a block). If a secret
   is in a block, that is a capture-layer bug to fix there; the slice floor exists so a
   capture-layer miss does not also become a plugin-exfil channel.
5. **Apply AS-056 redaction** at capture (see §2.3) so the blocks 1–4 operate over are
   *already redacted*; the scope filter never sees raw secrets and is not the redaction step.

First-party built-ins bypass 1–3 (trusted, in-tree) but **not** 4–5.

This is a *specification*, not yet code: today's `declarative` sub-agent receives the raw
slice but ignores it, so there is no live leak. The exclusion logic is implemented by the
follow-up ticket that introduces a model-using or proposal-consuming third-party path
(AS-111 below), gated behind the scope check so it lands **with** the first real consumer,
not speculatively (YAGNI).

---

## 4. Declarative-only enforcement & residual risk (D0)

### 4.1 What technically enforces the line

- **No code path executes plugin-supplied logic.** A third-party plugin is a `Manifest`
  decoded by `ParseManifest`; it is wrapped in `declarative{}`, whose `Init/Observe/
  Teardown` are no-ops. There is no `exec`, no `plugin.Open`, no script interpreter, no
  template-with-side-effects. *This is the enforcement* — it is structural, not a check
  that can be bypassed.
- **Strict parse.** `DisallowUnknownFields` means a manifest cannot smuggle fields a
  newer/older binary would silently accept and act on.
- **Validation at load.** Unknown kind/schedule/scope/permission → rejected before run.
- **Budget cap.** Even a (future) model-using plugin spends against a bounded `budget.Guard`.

### 4.2 Residual risks the declarative line does NOT close (documented punts, D0)

These are **real and intentionally deferred** — D9 punts plugin-injection defense for v1;
documenting them is the requirement, not solving them:

1. **Prompt content is still attacker-controlled.** A declarative plugin ships a *prompt*.
   Once a model-using kind exists, that prompt runs against the model with whatever context
   slice the scopes allow. A malicious prompt can attempt **prompt-injection / data
   exfiltration via its own model calls** (encode transcript into a tool argument or a
   crafted finding). Mitigations available *without* breaking declarative-only: the scope
   slice (§3) limits what it can see; the model call is the harness's own provider (the
   plugin cannot choose an exfil endpoint); findings are surfaced to the user, not auto-
   acted. **Not closed:** a user who reads and applies a malicious proposed edit. This is
   the documented v1 punt.
2. **Manifest social engineering.** A plugin named `security-reviewer` that over-claims
   scopes relies on the user granting them at install. Mitigation: the consent screen (§6)
   shows scopes in plain language; high-risk scopes (`read_transcript`, `read_file_contents`)
   are flagged loudly. **Not closed:** user fatigue / blind-granting.
3. **Supply-chain of the manifest source.** Where the manifest *came from* (marketplace,
   §7.26) is out of scope here; AS-059 governs the *runtime* trust model. Distribution
   integrity (signing, provenance) is a marketplace-ticket concern.
4. **Findings as a side channel.** A finding's text is user-visible and could carry
   injection aimed at the *next* turn's model. Mitigation: findings land in the insights
   Store, **never on the event log** (already true, AS-044), so they are not silently
   re-fed into context.

### 4.3 The enforcement test

The declarative boundary should be **guarded by a test**, not just a comment: a test that
asserts (a) `LoadManifest` of any manifest yields a sub-agent whose `Init/Observe/Teardown`
produce no findings and zero spend, and (b) there is no code path from a parsed third-party
manifest to `exec`/`os` write/network. (a) is a unit test; (b) is partially an `archtest`
assertion (the `subagent` package must not import `os/exec`, `net/http` for the declarative
path). **Implemented (AS-112):** (a) `TestDeclarativeBoundaryNoOp` in
`internal/subagent`, (b) `TestDeclarativePluginBoundaryHasNoExecOrEgress` in
`internal/archtest`.

---

## 5. The future code question (WASM / subprocess / never)

If plugins ever run code, the options and the recommendation:

| Option | Pros | Cons | Verdict |
|---|---|---|---|
| **Never** | Zero RCE surface; the current state. | Caps third-party capability at "a prompt + a manifest." | **Default. Stay here until a concrete need clears the bar in §5.1.** |
| **WASM (wasip1)** | Deterministic, no ambient authority, capability-passed host functions, cross-platform, Go has first-class `GOOS=wasip1`. Sandbox is the *default*, not bolted on. | Plugins must compile to WASM; host-function ABI is work; no threads/limited syscalls. | **Recommended *if* code ever runs.** Aligns with AS-079's WASM observability core — shared toolchain investment. |
| **Subprocess + OS sandbox** (seccomp/landlock/sandbox-exec) | Any language. | Per-OS sandbox stories diverge wildly (Linux seccomp vs macOS sandbox-exec vs Windows); ambient authority is the default you must claw back; heavier. | **Reject** for plugins — the cross-platform burden contradicts "provider-agnostic, one codebase." (Note: the *shell tool* AS-015 already does OS-level sandboxing for first-party use — that is a different trust domain.) |

### 5.1 Criteria for revisiting "never"

Move off declarative-only **only** when all hold: (1) a concrete capability genuinely
needs computation a prompt can't express (e.g. a deterministic AST-based analyzer); (2)
WASM host-function ABI can express that capability under the §2 scopes (no ambient fs/net);
(3) AS-056 redaction and §3 exclusions are live (so the sandbox receives minimized data);
(4) the consent UX (§6) can express "this plugin runs code." Until then, **never** is the
answer and the punt stays documented.

---

## 6. Trust UX (install-time consent)

When a third-party manifest is installed (a future marketplace/`smith plugin add` flow,
not v1), show a consent screen built **from the manifest**, no guesswork:

- **Identity:** `name`, source/origin, and (future) signing status.
- **Scopes in plain language:** "Reads metadata of your session" (low) … "Reads the full
  transcript including file contents" (**flagged high-risk, red**). Derived from the
  `permissions:` list via a fixed scope→sentence table.
- **What it can do:** "Suggests edits you must approve" / "Suggests notes." Never "edits
  files" (forbidden).
- **Cost:** per-session budget cap.
- **Update semantics:** bind the stored consent to the **manifest's content hash**, not just
  its scope set. A declarative plugin *is* its prompt, so a prompt (or any functional-field)
  change is a behavior change even when scopes are unchanged — silently applying it is a
  silent-hijack channel (an approved `security-reviewer` re-pointed at an exfiltration prompt
  while keeping its already-granted `read_transcript`). Therefore: any update whose hash
  differs **re-prompts**; a scope-*widening* update re-prompts loudly (escalation). Only an
  identical-hash re-install applies without a prompt. This is stricter than "more access
  needs a fresh decision" — *any* functional change does.

Display, not enforcement: the screen is how a user makes the trust decision the §4.2
residual risks require a human for.

---

## 7. Q12 resolution

**Q12 is narrowed, not fully closed.** Resolved here: the permission-scope vocabulary
(§2), default exclusions (§3), the declarative-enforcement mechanism and its documented
residual risks (§4), the future-code recommendation (WASM-if-ever, §5), and the consent UX
shape (§6). What remains genuinely open is the **v1 punt D9 already named** — defense
against prompt-injection *via a plugin's own prompt/model calls* — which stays deferred
until a model-using third-party kind exists, and the **marketplace distribution/signing**
trust (§7.26, not yet ticketed). Both are now *named* rather than open-ended, which is the
spike's job (D0).

---

## 8. Follow-up implementation tickets (drafted)

- **AS-111 — Scope-gated context slices for third-party sub-agents.** Implement §2 read
  scopes + §3 default exclusions; derive the teardown slice from granted scopes; lands with
  the first real third-party consumer (model-using kind or proposal-consuming path). Depends
  AS-044; interacts with AS-056.
- **AS-112 — Guard the declarative-only boundary with a test + archtest.** Implement §4.3:
  a unit test that `LoadManifest` yields a no-op/zero-spend sub-agent, and an `archtest`
  assertion that the declarative path imports no `os/exec`/`net/http`. Depends AS-044, AS-098.
- **AS-113 — Plugin consent screen + scope→sentence table.** Implement §6 when an install
  flow exists; re-prompt on scope escalation. Depends AS-044; blocked by a plugin-install/
  marketplace ticket (§7.26, not yet filed).

These are filed as ticket files in `docs/project/tickets/` and indexed in the README.
</content>
</invoke>
