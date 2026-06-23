# Hosting a Live Agent for Strangers — design spike (AS-080)

> Status: **accepted as design input — recommendation: do not host a live agent for strangers; ship AS-079 (read-only inspector) as the public demo.** · Owner: Agent Smith · Spike for PRD **§9, §10 Q12**, grounded in **D9**; sibling of the plugin-trust spike (AS-059, [plugin-trust.md](plugin-trust.md)).
> Retrieval date for external references: **2026-06-23** (US).

This document answers the question the GUI grilling surfaced: should we stand up a
publicly reachable web instance where **strangers** drive a *live* Agent Smith —
real model calls, real shell, real file writes — on our infrastructure?

The short answer is **no, not now**, and this spike writes down *why* (the threat
model), *what it would cost* if the project ever revisits it (the isolation bar),
and *what we ship instead* (AS-079). Per **D0**, the punt is documented, never
silent.

---

## 1. Why this is not a UI feature

The thin-client architecture (AS-077 `smith serve` + AS-078 web client) is
**identical** for local and hosted use. The browser holds no keys, runs no tools,
and contains no agent logic; it renders the `UIEvent` stream and sends turns. All
execution happens on whatever machine runs `smith serve`.

That symmetry is exactly the trap. Pointing the same client at a *hosted* `serve`
does not add a feature — it changes the **security envelope** from "your machine,
your privileges, your risk" to "our machine, every stranger's privileges, our
risk." The code is the same; the threat model is not.

| | Local `smith serve` | Hosted `smith serve` for strangers |
|---|---|---|
| Whose machine | The user's own | Ours / shared infrastructure |
| Whose privileges | The user's | Ambient on the host unless isolated |
| Who approves actions | The user, for their own machine | Nobody meaningfully — the "approver" is the attacker |
| Blast radius of a bad turn | The user's own files/secrets | Other tenants, our infra, our keys |
| Covered by D9 today | **Yes** | **No** |

---

## 2. The collision with D9

> **D9:** "Agent Smith runs with your privileges in your environment; you approve
> actions. It is *not* a sandbox."

D9 is not an accident or a gap to be filled later by this ticket — it is a
deliberate scope line. The permission model (ask / allowlist / auto) is a
**consent** mechanism for a user acting on their *own* machine. It is not, and was
never built as, a containment boundary against a hostile operator. Hosting a live
agent for strangers asks the permission model to do a job it was explicitly not
designed for.

Concretely, a hosted live agent is **multi-tenant arbitrary code execution**:
every stranger's turn can run shell commands and write files. That is the single
hardest problem in the security space (untrusted-code-as-a-service), and the PRD
deliberately punted OS-level sandboxing in D9.

### Threat model (what a hostile "user" gets for free if we host naively)

1. **RCE by design.** The shell tool *is* remote code execution; an ask-mode
   prompt the attacker answers themselves is not a control. Auto/allowlist modes
   make it worse.
2. **Secret exfiltration.** Any provider API key, cloud credential, or
   environment secret reachable from the `serve` process is one `cat`/`env`/HTTP
   call away. A shared key model means one abuser drains the budget for everyone.
3. **Lateral movement / noisy-neighbor.** Without per-session isolation, one
   tenant reads or corrupts another tenant's filesystem, sessions, or memory.
4. **Egress abuse.** The host becomes a launchpad: crypto mining, DoS reflection,
   SSRF into our internal network, spam.
5. **Resource exhaustion.** Unbounded CPU/RAM/disk/time per anonymous session is
   a trivial denial-of-wallet and denial-of-service.

The permission model addresses **none** of these against a hostile operator,
because the operator *is* the one clicking "allow."

---

## 3. Relationship to AS-059 (plugin trust)

Plugin trust (AS-059) and hosted stranger execution are **different threat
models** and must not be conflated:

| | AS-059 plugin trust | AS-080 hosted strangers |
|---|---|---|
| Untrusted thing | A third-party **plugin/sub-agent** | A third-party **human operator** |
| Code execution | **None** — declarative-only (manifest + prompt, no arbitrary code) | **Full** — the operator drives shell + file writes |
| Boundary that helps | Strict manifest parsing + permission scopes + no-op lifecycle | OS/VM-level isolation (the thing D9 punted) |
| Status | Narrowed; v1 line set (declarative-only) | **Out of scope now** |

They share *vocabulary* (permission scopes, default context-slice exclusions,
egress posture) but the plugin spike's "no arbitrary code" escape hatch does not
exist here: the whole point of a live agent is that the operator runs code. The
local plugin work must **not** be blocked on solving hosted multi-tenancy.

---

## 4. What we ship instead: AS-079

The public-demo *desire* behind this wish is satisfied without the threat model:

- **AS-079** is a static, **read-only** session inspector — the pure-compute
  observability views (`/context`, cost, composition, clean/compact preview) that
  genuinely compile to WASM. It runs entirely in the visitor's browser over
  canned or visitor-uploaded session logs.
- There is **no host-side execution**, no shared keys, no multi-tenancy, no
  egress surface. The blast radius is the visitor's own tab.
- It is the genuine WASM payoff *and* the safe public demo, and it is already on
  the roadmap (depends only on the substrate: 005/006/020/061 + 038).

So the recommendation is not "no demo" — it is "the demo is AS-079, not a hosted
live agent."

---

## 5. Recommendation

**Close AS-080 in favour of AS-079.** Do not host a live, stranger-driven Agent
Smith. Local `smith serve` (your machine, your risk) stays fully supported and is
covered by D9; the public web demo is the read-only inspector (AS-079).

This decision does **not** block any current work: AS-077 (`smith serve`) and
AS-078 (web thin client) ship for **local** use unchanged. Only the *hosting it
for untrusted users* step is out of scope.

---

## 6. If the project ever revisits hosted live execution (not now)

Documented so the punt is explicit, not a silent gap. The minimum bar before any
stranger-facing live execution would be a *product + security* effort spawning its
own tickets, not a UI change:

- **Per-session isolation:** one ephemeral microVM or hardened container per
  session (e.g. Firecracker / gVisor / Kata-class boundary), destroyed on
  disconnect. No shared kernel trust across tenants.
- **Ephemeral, scoped filesystem:** fresh per session, no host mounts, wiped on
  teardown.
- **No ambient secrets:** the sandbox holds **no** provider keys. Either
  bring-your-own-key, or a per-session abuse-capped/metered key with hard spend
  limits.
- **Strict egress policy:** default-deny network, explicit allowlist (provider
  APIs only), block internal ranges to kill SSRF.
- **Quotas:** hard CPU/RAM/disk/wall-clock/turn caps per anonymous session;
  global rate limits; queueing.
- **Abuse handling:** logging/attribution, kill switch, terms, and a plan for the
  inevitable malicious traffic.

These are follow-on decisions if and when the project chooses to enter the
untrusted-code-as-a-service business — they are **not** prerequisites for
AS-077/AS-078, and no tickets are created for them now (revisiting the decision is
the trigger).

---

## 7. PRD / D9 posture

D9's stated posture is unchanged and now explicitly covers this case: *"Agent
Smith runs with your privileges in your environment; you approve actions. It is
not a sandbox."* Hosting a live agent for strangers would require *being* a
sandbox, which D9 declines. The public demo is AS-079. (PRD §10 Q12 trail updated
to point here.)
