# ADR-0003 — GitHub authentication strategy for the orchestrator (AS-148)

> Status: **accepted** · Scope: how the dogfood orchestrator (PRD §4.6, §5, Q-148) receives limited GitHub access · Date: 2026-07-01

## Context

AS-147 landed deterministic GitHub event ingestion (webhook → normalized
trigger) and the deterministic action hooks (labels, PR create/update, comment,
status, guarded merge) live in `internal/orchestrator` (see
[orchestrator-architecture.md](../architecture/orchestrator-architecture.md), row
"GitHub integration"). Those actions all need a GitHub credential. This ADR
resolves **Q-148** (deferred from AS-159): *does MVP 0 start with a GitHub App,
or a tightly scoped maintainer token while the App is spiked?*

Inputs: the AS-158 competitive spike
([orchestrator-competitive-research.md §3 AS-148](../research/orchestrator-competitive-research.md))
surveyed how Claude Code (Action/web/Routines), Codex, Cursor, Copilot, Devin,
Jules, Coder, and Ona handle GitHub credentials. The convergent pattern: a
credential proxy holds the real credential outside the runner, the run only ever
holds a scoped/short-lived token, and push is restricted to the run's own
prefixed branch.

This is a **private single-tenant dogfood** ("Smith implements Smith"), not a
hosted multi-tenant product (PRD §3 non-goals; D9 "not a sandbox"). That scoping
is what makes a maintainer token acceptable for MVP 0.

## Decision

**MVP 0 uses a single tightly scoped, fine-grained maintainer Personal Access
Token** granted only on the dogfood repo(s). The **GitHub App** minting
short-lived per-operation installation tokens is the **MVP 1+ migration target**,
not an MVP 0 prerequisite. The fine-grained token is the bridge; the App is not
on the critical path for dogfood.

Rationale (why token first, App later):

- **The App's value is multi-tenant onboarding and short-lived tokens.** For a
  single maintainer on their own repo, the onboarding value is nil and the
  short-lived-token value is approximated by a narrowly scoped, rotatable PAT.
  Building App installation-token minting now is effort spent on MVP 3's problem.
- **Fine-grained PATs already give per-repo, per-permission scoping** — the exact
  grant set below mirrors what the Claude GitHub App itself requests, so the
  security posture is close, and the migration is a credential-source swap behind
  one seam, not a redesign.
- **Fail-closed is a property of how we *use* the credential**, not of which kind
  it is (see below); it ships in MVP 0 regardless.

### Alternatives considered

- **GitHub App from day one.** Correct end state (per-operation, repo-scoped,
  short-lived installation tokens — the Codex/Claude/Devin model), but front-loads
  App registration, private-key custody, and installation-token exchange for zero
  MVP 0 benefit on a private repo. **Deferred, not rejected** — it is the
  documented MVP 1+ target below.
- **Local `gh` CLI delegation** (inherit the operator's `gh` auth). Zero setup,
  but the credential is the operator's *full-scope* GitHub identity — no per-repo
  or per-permission narrowing, no distinct audit identity, and it leaks ambient
  scope into every run. Rejected for the always-on daemon; acceptable only for a
  developer running steps by hand.
- **Classic (non-fine-grained) PAT.** Rejected: scopes are coarse
  (`repo` grants all repos), no per-repo restriction, weaker audit story than
  fine-grained.

## Required access per flow

The credential is granted **only on the dogfood repo(s)**, with the minimum
fine-grained permissions for the deterministic action set (D-ORCH-1: Smith owns
the deterministic shell; models never hold the credential):

| Flow | Fine-grained permission | Level |
| --- | --- | --- |
| Read issues / PRs | Issues, Pull requests | read |
| Read checks / statuses | Checks, Commit statuses | read |
| Create branches, push contents | Contents | read/write |
| Open / update PRs | Pull requests | read/write |
| Comment | Issues, Pull requests | read/write |
| Label (add/remove) | Issues, Pull requests | read/write |
| Merge / auto-merge (guarded) | Contents, Pull requests | read/write |

Net grant set = **Contents r/w, Pull requests r/w, Issues r/w, Checks read,
Commit statuses read** — mirroring the Claude GitHub App's grant. No
Administration, no Actions-write, no org scopes, no Secrets.

## Fail-closed: permission failures are operator actions

A missing scope or a `403`/`404` from a write is **never** an agent decision to
retry, escalate, or route around. It is a named, terminal, operator-actionable
failure (D-ORCH-6):

- The action step fails closed with a `missing-scope` / `insufficient-permission`
  error that names the exact permission and flow (e.g. *"merge requires Contents:
  write on `<repo>`; re-grant on the fine-grained token"*).
- The run is marked failed in the run store; no partial GitHub mutation is
  presumed to continue.
- The operator re-grants the scope and re-triggers; Smith does not "work around"
  a denial. This matches the surveyed systems (Claude/Ona), all of which surface
  re-grant as an explicit human step.

## Credential lifetime, storage, rotation, audit

- **Storage.** The real credential lives in **one place, outside the run's
  reach** — the operator's secret store / the daemon host environment, referenced
  from job specs only as a declared **scope name** (`${secrets.github_token}`),
  never a literal. Job specs and the event log carry a *handle*, never plaintext;
  the AS-054/AS-115/AS-154 redaction-at-capture contract and the load-time
  plaintext-credential guard (`internal/orchestrator/spec/secrets.go`, which
  already rejects `github_pat_*`/`ghp_*` literals) enforce this. The run holds a
  scoped credential; branch pushes are restricted to the run's own
  `claude/…`-prefixed branch (never force-push, never bypass protection).
- **Lifetime.** Fine-grained PATs are created with a **fixed expiry**
  (≤ 90 days recommended); GitHub emails on approaching expiry. The App migration
  replaces this with per-operation installation tokens (~1 h TTL) minted on
  demand, which is the point of moving to the App.
- **Rotation.** Rotation = mint a new fine-grained token, update the one secret
  reference, restart the daemon. Because specs and logs hold only the scope name,
  rotation touches no job spec and no history. The proxy/App seam (below) makes
  this a source swap, not a code change.
- **Audit.** Two layers: (1) GitHub's own audit log / token-activity for the
  maintainer identity, and (2) Smith's append-only event log + run-store audit
  rows, which record every deterministic GitHub action (actor = the job/run, not
  a human) with job ID, trigger, PR/commit refs, and policy decision (PRD §4.5,
  D-ORCH-4). Cost/narrative stay in the session log; run-control + audit stay in
  the SQLite run store.

## Migration path: MVP dogfood token → GitHub App

The token and the App sit behind one seam so the swap is source-only:

1. **MVP 0 (now).** Fine-grained maintainer PAT resolved from the secret store by
   scope name. All action steps call an internal credential accessor; they never
   read an env var or file directly.
2. **MVP 1+ (App).** Register a GitHub App with the same permission set,
   installed on the dogfood repo(s). A **credential proxy** outside the runner
   holds the App private key and mints **short-lived, per-operation, repo-scoped
   installation tokens**; the runner receives only the scoped token for the
   action it is about to perform. The accessor from step 1 now returns an
   installation token instead of the PAT — no action-step code changes.
3. **MVP 3 (hosted, future).** App onboarding for selected repos/orgs (PRD §5
   MVP 3); multi-tenant boundaries only after dogfood stabilizes. Out of scope
   here; the proxy + per-operation-token shape from step 2 is what carries
   forward.

## Consequences

- MVP 0 ships GitHub automation without building App infrastructure; the security
  gap vs the App is bounded to *token TTL* (fixed-expiry PAT vs ~1 h installation
  token), which is acceptable for a private single-tenant repo and closes at
  MVP 1.
- The "credential behind an accessor + proxy seam, declared by scope name,
  push restricted to the run branch, fail-closed on missing scope" shape is fixed
  now and is invariant across the PAT→App migration.
- Q-148 is resolved; downstream AS-149 (PR lifecycle), AS-154 (secrets/redaction),
  AS-156 (private VPC), and AS-157 (auto-merge) build on this credential model.
