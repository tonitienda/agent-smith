# Competitive agent-workflow / sandbox / secrets research (AS-158)

> Status: **Complete** · Ticket: AS-158 · Source PRD:
> [smith-orchestrator-dogfood-prd.md](../project/smith-orchestrator-dogfood-prd.md) ·
> ADR: [orchestrator-architecture.md](../architecture/orchestrator-architecture.md)
>
> Research spike feeding **AS-148** (GitHub auth), **AS-153** (sandbox),
> **AS-154** (secrets/redaction), **AS-156** (private VPC), **AS-157** (auto-merge).

This spike surveys how current agent-workflow systems handle always-on jobs,
GitHub automation, sandboxes, secrets, and multi-provider execution so Smith's
dogfood orchestrator does not reinvent avoidable mistakes. Systems covered:
**Anthropic Claude Code** (GitHub Action / web / Routines), **OpenAI Codex**
(cloud + CLI), **Cursor** (Cloud/Background Agents + Automations + Bugbot),
**Coder**, **Ona** (formerly Gitpod), and the adjacent agents **GitHub Copilot
coding agent**, **Devin** (Cognition), and **Google Jules**.

> Note on "Ona": Gitpod rebranded to **Ona** at **ona.com** in September 2025,
> repositioning from cloud dev environments to an AI agent-orchestration platform
> ([Gitpod is now Ona](https://ona.com/stories/gitpod-is-now-ona);
> [InfoQ](https://www.infoq.com/news/2025/09/gitpod-ona/);
> [The Register](https://www.theregister.com/software/2025/09/03/gitpod-rebrands-as-ona-now-an-ai-driven-dev-platform/)).
> This is **not** the unrelated humanitarian-data company Ona Systems (`ona.io`).
> `ona.com` blocked automated fetches during this spike, so deep doc URLs below
> were gathered via search extracts and should be re-verified before being quoted
> as guarantees; the platform and its capabilities are corroborated by the
> rebrand coverage above and the Gitpod whitepaper.

Date: 2026-06-30. Sourcing caveat: several vendor doc domains
(`anthropic.com/engineering`, `developers.openai.com`, `cursor.com`,
`docs.github.com`, `docs.devin.ai`, `jules.google`, `coder.com/docs`,
`ona.com/docs`) block automated fetches; facts marked below were read via
search-engine extracts of the official pages rather than full-page reads, and
some items are flagged **[uncertain]**. Primary-source URLs are listed per row so
claims can be re-verified before any are treated as a hard guarantee.

## 1. Comparison matrix

Axes are the ones AS-158 requires: scheduling, GitHub triggers, PR
create/update, review/merge policy, sandbox isolation, credentials, env/secrets,
secret redaction, artifact retention, audit logs.

### Scheduling / always-on jobs

| System | Always-on model | Min cadence |
| --- | --- | --- |
| Claude Code | **Routines** (cloud, Anthropic-managed) = scheduled + API + GitHub triggers; Actions `schedule:` cron; session-local `/loop`+cron (not always-on, 7-day expiry) | Routines **1 hour** min |
| Codex | **Automations**: thread (heartbeat) + standalone, cron cadence — but currently run on the **dev's local machine**; cloud scheduling announced as future | minute-based local |
| Cursor | **Automations** = recurring (preset or **cron**) or event-driven; each run = fresh sandbox | cron |
| Copilot | Runs on schedule or on events via GitHub-native automation; ephemeral Actions env per run | — |
| Devin | First-class **Schedule triggers** (hourly/daily/weekly) + event Automations | hourly |
| Jules | **Scheduled Tasks** (recurring maintenance); fresh VM per run | — |
| Coder | Autostart/autostop + quiet hours + warm **prebuilds** (reconcile loop) | per-template |
| Ona | Ephemeral + prebuilds; automations run agents on schedules | [uncertain] |

### GitHub triggers

| System | Triggers |
| --- | --- |
| Claude Code | `@claude` mentions (issue/PR comment), Action on `issues`/`issue_comment`/`pull_request_review_comment`; **Routines** = PR events (opened/closed/labeled/synchronized/merged) + Release events, with PR filters (labels, is-merged, branch, regex) |
| Codex | `@codex` on PR/issue (review or task); auto-review on PR open; Codex GitHub Action for CI |
| Cursor | `@cursor` on PR/issue; Slack `@Cursor`; GitHub issue-comment / PR-review-comment / review-submitted / workflow-run-completed; also GitLab/Linear/PagerDuty/webhooks |
| Copilot | **Assign issue to Copilot**; `@copilot` PR comment; from failing Actions runs; Agents panel |
| Devin | Webhooks: PR review comment, **check-run completion** (filter `conclusion=failure`), push (file filters). **GitHub automations private-repo-only** |
| Jules | UI/CLI/Gemini-CLI; CI auto-fix; issue/label-trigger parity [uncertain] |
| Coder | Customer-authored **GitHub Actions** create Tasks (issue_comment `/coder`, label, pr-opened). Tasks deprecating → Coder Agents (ESR 2026-06-02, removed v2.37) |
| Ona | First-party **GitHub App**: PR events (changes/reviews/merges), comments, schedule, webhooks |

### PR create / update

| System | Behavior |
| --- | --- |
| Claude Code | Action default does **not** auto-create PR (returns PR-create link, human clicks); web/Routines push to **`claude/`-prefixed** branches only by default; `Claude-Session` git trailer |
| Codex | Cloud opens PRs (parallel tasks → separate PRs); can read CI, push fix commits; **tends to open new branch/PR per iteration** [uncertain on always-same-branch] |
| Cursor | Works on **`cursor/`** branch, can auto-create PR (draft supported via API) |
| Copilot | Opens a **draft PR**, iterates on `@copilot` feedback on the same PR |
| Devin | Creates PRs, responds to PR comments; needs Contents r/w + PRs r/w |
| Jules | Opens PR from UI; `AUTO_CREATE_PR` mode |
| Coder / Ona | Agent pushes via workspace git creds; Ona has native "create PR from session" |

### Review / merge policy

| System | Auto-merge? | Gating |
| --- | --- | --- |
| Claude Code | **No native auto-merge** found | Relies on GitHub branch protection (inferred from normal-push model); `claude/`-branch default; web auto-fix pushes fix on CI fail / review comment, asks human when ambiguous |
| Codex | **No native auto-merge**; human gate | Respects repo permissions + branch protection |
| Cursor | **No native auto-merge** (merge-queue = open community ask) | **Bugbot** can be a **required status check**; Autofix spawns agent |
| Copilot | **No** | **Hard rule: agent PRs need a human "Approve and run workflows"; the requester cannot self-approve.** Admin can opt out |
| Devin / Jules | No native auto-merge | Native GitHub branch protection / required checks |
| Coder / Ona | Governed runtime, not merge gate | GitHub branch protection / required reviews |

**Finding: no surveyed system ships prompt-driven auto-merge. Every one defers
the merge decision to GitHub branch protection + a human gate.** This validates
the ADR's D-ORCH-6 fail-closed stance directly.

### Sandbox isolation & network egress

| System | Compute | Egress default |
| --- | --- | --- |
| Claude Code | web: Anthropic VM (~4 vCPU/16 GB/30 GB); Action: your GH runner | **HTTP(S) proxy, allowlist** (None/Trusted/Full/Custom); blocked → `403 x-deny-reason: host_not_allowed`; Anthropic API still reachable even when net "disabled" |
| Codex | OpenAI container per task | **Egress OFF in agent phase**; setup phase has net; allowlist + only GET/HEAD/OPTIONS when filtered |
| Cursor | Isolated **Ubuntu VM on AWS**; `.cursor/environment.json` (Dockerfile/snapshot); encrypted disk snapshots | **Allowlist**; Tailscale for private nets |
| Copilot | **Ephemeral GitHub Actions** dev env | **Firewall + recommended allowlist ON by default**; org-level firewall settings; admin can extend/disable |
| Devin | Ephemeral **Devbox**, destroyed at session end | **Domain allowlist, default-deny** |
| Jules | Short-lived **Google Cloud VM** | **Open internet access** (no default-deny allowlist documented) — notable outlier |
| Coder | Terraform-pluggable: K8s pod / container / VM, any cloud; isolated external provisioners | Network policies per template |
| Ona | **VM-per-agent** on runners **inside your VPC** (EC2 + ECS orchestrator) | Full egress control, HTTP proxy, kernel-level policy, IAM boundaries |

### Credentials / GitHub auth

| System | Mechanism |
| --- | --- |
| Claude Code | **Claude GitHub App** → **short-lived token scoped to the specific repo** (Contents/Issues/PRs r/w); or BYO App / `GITHUB_TOKEN`; web keeps real token in a **proxy outside the sandbox**, inside git uses scoped cred, push restricted to current branch; only write-access users can trigger |
| Codex | **Short-lived, least-privilege GitHub App installation tokens, minted per operation**; user auth via API key or ChatGPT OAuth; MFA required |
| Cursor | Admin connects source control (OAuth/GitHub App), repo **read-write**; exact App manifest not enumerated [uncertain]; **privilege-escalation caveat**: "team follow-ups" can run with another user's secrets |
| Copilot | Distinct GitHub identity; runs in repo Actions context with scoped perms; token TTL/scope mechanics [uncertain] |
| Devin | **GitHub App** install, org/repo-scoped, short-lived scoped creds; least-privilege service users; session-scoped secrets via API |
| Jules | GitHub OAuth/app; scope/TTL [uncertain] |
| Coder | External Auth OAuth2 (GitHub/GitLab/Bitbucket); per-user SSH keypairs; Vault modules |
| Ona | OAuth App (recommended) or PAT; scopes `repo`, `read:user`, `user:email`, `workflow` |

### Env vars / secrets provisioning & scoping

| System | Model |
| --- | --- |
| Claude Code | Env vars per cloud environment in `.env` format; **no dedicated secret store yet** — env+setup-script visible to anyone who can edit the environment (docs warn) |
| Codex | **Two-tier**: env vars (non-sensitive, throughout) vs **secrets available only in setup phase, removed before agent phase**; setup script in separate shell; default `KEY`/`SECRET`/`TOKEN` env filter |
| Cursor | Secrets UI (preferred over env file); **3 types: Env Var (visible) / Runtime Secret (redacted) / Build Secret (Docker-build only)**; encrypted at rest, deleted with agent |
| Copilot | **Dedicated "Agents" secret bucket the agent cannot cross into Actions/Codespaces/Dependabot secrets**; MCP secrets need `COPILOT_MCP_` prefix; org/repo level |
| Devin | Central Secrets feature + per-session secrets via API |
| Jules | Repo-level env vars, opt-in per task, encrypted |
| Coder | User secrets injected at start; Terraform `sensitive=true`; source from Vault/cloud SM; DB encryption AES-256-GCM |
| Ona | env-var type or **file type** (preferred, not in process lists/logs); **AES-256-GCM at rest**, Ona staff can't decrypt; **credential proxy** swaps dummy→real secret in HTTPS header so real value never enters env |

### Secret redaction

| System | Redaction |
| --- | --- |
| Claude Code | Action output **sanitized by default** (`show_full_output:false`); best-effort subprocess env scrub of Anthropic/cloud/GH secrets; prompt-injection input sanitization; web transcripts **not** auto-redacted (user must check before sharing) |
| Codex | "Privacy by default" + secret redaction; OTel prompt redaction default-on; **setup-phase-only secret model is the primary control** |
| Cursor | **Runtime Secrets → `[REDACTED]`** in tool results/chat/commits; but still real env vars, visible to anyone with Terminal access |
| Copilot | No explicit coding-agent log-redaction statement; relies on Actions secret masking [uncertain] |
| Devin | Explicit: **"secrets are redacted from logs"** |
| Jules | Encrypted storage, no explicit log-redaction claim [uncertain] |
| Coder | TF `sensitive=true` hides from build logs; sensitive headers redacted in debug; template file contents NOT hidden from template editors |
| Ona | Credential-proxy = real value never in env; file secrets avoid process-list/log leak; no explicit transcript-scrub [uncertain] |

### Artifact retention

| System | Retention |
| --- | --- |
| Claude Code | web sessions persist (transcript URL), restorable after VM reclaim; archive/delete; env snapshot ~7 days; exact data-retention durations [uncertain] |
| Codex | task diff/commits/logs/PR; cloud sessions persist as archived conversations; granular delete = open gap [uncertain] |
| Cursor | conversation history (prompts/responses/tool-calls/demos); Enterprise retention **Indefinite or 90 days** |
| Copilot | ephemeral Actions logs via GitHub; agent-specific window [uncertain] |
| Devin | session transcript exportable to SIEM; retained for customer-relationship duration; sandbox destroyed at session end |
| Jules | short-lived VM destroyed after task; retention [uncertain] |
| Coder | audit/connection/provisioner logs shipped to your log backend; window deployment-dependent |
| Ona | every human + AI action logged; retention [uncertain] |

### Audit logs

| System | Audit |
| --- | --- |
| Claude Code | web proxy keeps **DNS-level hostname audit trail**; transcript = per-run record; GitHub-side via commits/comments + `Claude-Session` trailer; formal enterprise audit-log product [uncertain] |
| Codex | Enterprise Compliance/Logs API with **`CODEX_LOG` / `CODEX_SECURITY_LOG`** event types; **retention up to 30 days** then export; SCIM "Codex Admin" group; opt-in OTel |
| Cursor | **Enterprise-only**; JSON events (incl. Cloud Agent env + secrets lifecycle) streamable to SIEM/S3/Elastic; **does NOT log agent responses/generated code** (recommends hooks) |
| Copilot | inherits GitHub audit/identity; dedicated agent audit surface [uncertain] |
| Devin | **Dedicated Audit Logs API** (`/v2/audit-logs`); every commit/comment/merge ties to a session; SOC 2 Type II |
| Jules | no dedicated audit feature found [uncertain] |
| Coder | **Premium-gated** audit logs; filter by resource_type/action; API + Splunk |
| Ona | every human + AI action logged for SOC 2 / ISO 27001; SOC 2 Type II |

## 2. Patterns to copy, avoid, or differ from

### Copy

- **Two-phase execution (Codex): network-on setup phase, then network-off agent
  phase.** The cleanest exfiltration control seen — secrets needed only to build
  the workspace never live during the cognition phase. Strong fit for Smith's
  fail-closed principle.
- **Default-deny egress with an allowlist** (Codex, Devin, Copilot, Claude web).
  The security-forward norm. Jules (open internet) is the outlier to *not* copy.
- **Short-lived, per-operation, repo-scoped GitHub tokens** (Codex, Claude App,
  Devin). No long-lived broad PATs in the run path.
- **Credential proxy keeping the real secret outside the sandbox** (Ona, Claude
  web GitHub proxy). The real value is swapped into the outbound request at the
  proxy; the runner only ever holds a dummy. This is the single most useful
  redaction primitive found and directly satisfies AS-154's "values never stored
  in plaintext / never leave the runner."
- **Separate secret bucket the agent cannot cross into** (Copilot's "Agents"
  secrets, isolated from Actions/Codespaces/Dependabot). Maps to AS-154 declared
  scopes — a job only sees secrets for scopes it declares.
- **Redacted-secret env type** (Cursor Runtime Secrets → `[REDACTED]` in
  output/commits). Cheap redaction-at-capture for AS-154.
- **`<vendor>/`-prefixed branch sandboxing** (Claude `claude/`, Cursor
  `cursor/`). Smith already uses `claude/...` branches — keep it; constrain push
  to the run's own branch like Claude web does.
- **Human-gated merge, never prompt-driven** (universal). Copilot's "requester
  cannot self-approve + Actions don't run until a human approves" is the
  strongest published guardrail and the model AS-157 should mirror.
- **Session/transcript as the audit record + map every commit/comment/merge back
  to a run** (Devin, Claude `Claude-Session` trailer). Smith already plans this
  (event-log integration, AS-151) — reinforce it.

### Avoid

- **Plaintext env/setup config visible to any environment editor** (Claude web
  current state, "no dedicated secret store yet"). Smith must ship the secret
  scope/proxy contract from day one (AS-154), not bolt it on.
- **Secrets that survive into the agent/terminal phase** (Cursor Runtime Secrets
  are still real env vars visible via Terminal; redaction only covers output).
  Prefer Codex's "removed before agent phase" + Ona's proxy over output-only
  redaction.
- **Open internet egress** (Jules). Conflicts with fail-closed.
- **Cross-user privilege escalation via shared follow-ups** (Cursor's documented
  "team follow-ups run with another user's secrets" caveat). Smith jobs run
  under one declared owner/scope; do not let one trigger inherit another's creds.
- **Audit logs that omit the agent's actual output/code** (Cursor). Smith's
  session log already captures narrative — keep that as the audit truth.
- **Building on a deprecating primitive** (Coder Tasks → Coder Agents). N/A to
  Smith but a reminder to depend on stable seams.

### Differ (Smith's deliberate stance)

- **Smith owns merge/label/retry policy deterministically in the shell, not the
  model** (ADR §2). Most vendors make the *agent* propose and a *human* gate;
  Smith additionally removes the agent from the policy decision entirely. Keep
  this as the differentiator.
- **One observability path** — orchestrated runs are normal Smith append-only
  sessions (ADR D-ORCH-4), not a second analytics stack. Vendors bolt on
  separate audit-log products; Smith reuses `/context` `/cost` `/insights`.
- **Repo-only job specs** (ADR Q4): no UI-editable live specs, reinforcing
  "Smith does not edit its own job specs." Cursor/Devin/Jules allow UI-driven
  config; Smith keeps the repo as source of truth.
- **MVP 0 is honestly "no sandbox / local checkout"** (ADR Q8) — do not pretend
  local execution is isolation (AS-153). Container/microVM comes behind the
  interface later, informed by Ona's VPC-runner and Codex's two-phase model.

## 3. Concrete recommendations per ticket

### AS-148 — GitHub authentication strategy

- MVP 0: **tightly scoped maintainer fine-grained token** (matches ADR Q-148);
  required access set = Contents r/w, Pull requests r/w, Issues r/w, Checks read
  (mirrors Claude App's exact grant).
- Migration target: **GitHub App with per-operation, repo-scoped, short-lived
  installation tokens** (Codex/Claude/Devin model). Document the App as the MVP 1+
  path; the token is the bridge.
- Keep the **real credential in a proxy outside the runner** (Claude web GitHub
  proxy; Ona credential proxy) so the run only ever holds a scoped/dummy cred and
  push is restricted to the run's own `claude/...` branch.
- Permission failures = explicit operator action (re-grant scope), never an agent
  decision — fail closed with a named missing-scope error.

### AS-153 — Sandbox abstraction and execution environments

- Interface backends, weakest→strongest: **local checkout (no isolation,
  labelled as such)** → private VPC runner → rootless container → microVM.
  Document per-backend what isolation it does and does NOT provide (Ona's
  explicit per-backend guarantees are the model).
- Adopt **two-phase lifecycle** in the interface: a network-enabled
  setup/checkout phase and a **default-deny-egress agent phase** (Codex). Egress
  policy = allowlist with a deny→clear-error path (Claude `403 host_not_allowed`).
- Interface must cover: image/profile, repo checkout, workspace lifecycle,
  resource limits, **egress policy**, artifact extraction, teardown, telemetry —
  the union of Cursor `environment.json` + Ona runner + Codex environments.
- MVP 0 ships the **local backend only**, behind the interface, with no security
  claims (ADR Q8); the VPC runner (AS-156) is the first real-isolation backend.

### AS-154 — Secret management and redaction contract

- **Declared named scopes per job; validation fails on undeclared reference**
  (Copilot's isolated "Agents" bucket → only-declared-scopes-visible).
- **Credential proxy** so secret values never enter the runner / job spec / run
  DB / event log (Ona + Claude web). This is the primary control; output
  redaction is secondary.
- **Setup-phase-only secrets removed before the agent phase** for any secret not
  needed during cognition (Codex).
- **Redaction-at-capture**: `[REDACTED]` substitution before logs/artifacts leave
  the runner (Cursor Runtime Secrets). Prefer **file-type secrets** over env vars
  to dodge process-list/crash-dump leaks (Ona).
- Injection audit record = name/scope/expiry/recipient/run-IDs, **never values**
  (matches ticket AC).
- Secret classes: model-provider creds, GitHub creds, optional user/team secrets,
  Smith service creds (as ticketed) — apply the same scope+proxy to all.

### AS-156 — Private VPC deployment

- **Single-tenant VM-per-agent runner inside the maintainer's VPC** is the
  proven shape (Ona AWS runner: EC2 instances + ECS-style orchestrator,
  egress-controlled). Hetzner equivalent: one always-on daemon host + ephemeral
  run workspaces.
- **Webhook delivery**: document tunnel (e.g. smee/cloudflared) vs public
  endpoint trade-off; Routines/Devin/Ona all front GitHub events via a hosted App
  webhook — for a private host a tunnel is the low-friction MVP 1 choice.
- Runbooks: deploy/upgrade/rollback, pause-all-jobs, inspect failures, rotate
  creds, restore DB backup (ticket AC). Encrypt the run DB at rest (Coder/Ona
  AES-256-GCM precedent).
- Keep multi-tenant assumptions out (D9); the VPC host is the first real sandbox
  backend for AS-153.

### AS-157 — Auto-merge policies and safety gates

- Mirror **Copilot's hard gate**: a Smith-authored PR's checks/merge require an
  independent human approval; the trigger/requester identity cannot satisfy it.
- Auto-merge **off unless both job spec and repo policy explicitly allow**
  (ADR D-ORCH-6). Inputs: author-is-Smith, branch ownership, required labels
  (`smith-generated`, `smith-auto-merge`), required checks green, branch
  protection satisfied, changed-file allow/deny, budget outcome.
- **Failed / pending / missing / unknown checks block merge** (universal vendor
  behavior). Use GitHub's native auto-merge (enable only when policy passes) —
  same pattern this repo already uses for its own PRs — rather than a custom
  merge action.
- Deny-list high-risk paths (workflow files, secret files, job specs) → require
  stronger approval. Record every evaluated input + allow/deny reason in run DB +
  session log; explicit, audited manual override.

## 4. Unresolved unknowns

- **Exact data-retention windows** for cloud transcripts: Claude, Codex
  (Enterprise audit = 30 days, sessions unclear), Jules, Coder/Ona — all
  unconfirmed in public docs.
- **GitHub App permission manifests**: Cursor's and Copilot's exact fine-grained
  permission sets / token TTLs not enumerated publicly.
- **Codex PR-update behavior**: pushes follow-up commits to a PR branch vs opens
  a new branch/PR per iteration — community reports conflict; affects whether
  Smith should expect update-in-place from any vendor agent it routes to.
- **Automatic transcript/log secret-scrubbing** for Coder/Ona/Jules: not
  documented; would need vendor confirmation.
- **microVM vs rootless-container choice** for AS-153's first real sandbox is
  left open — Ona (VM) and Codex/Devin (container) both work; decide in AS-153
  against the VPC host's constraints.
- Doc-domain fetch blocks mean exact wording on several rows should be
  re-verified at the cited URLs before being quoted as a guarantee.

## 5. Primary sources

**Claude Code** — [github-actions](https://code.claude.com/docs/en/github-actions) ·
[action security.md](https://github.com/anthropics/claude-code-action/blob/main/docs/security.md) ·
[routines](https://code.claude.com/docs/en/routines) ·
[on the web](https://code.claude.com/docs/en/claude-code-on-the-web) ·
[sandbox environments](https://code.claude.com/docs/en/sandbox-environments) ·
[scheduled tasks](https://code.claude.com/docs/en/scheduled-tasks) ·
[WIF + GitHub Actions](https://platform.claude.com/docs/en/manage-claude/wif-providers/github-actions)

**Codex** — [automations](https://developers.openai.com/codex/app/automations) ·
[cloud](https://developers.openai.com/codex/cloud) ·
[cloud environments](https://developers.openai.com/codex/cloud/environments) ·
[internet access](https://developers.openai.com/codex/cloud/internet-access) ·
[GitHub integration](https://developers.openai.com/codex/integrations/github) ·
[agent approvals & security](https://developers.openai.com/codex/agent-approvals-security) ·
[security](https://developers.openai.com/codex/security) ·
[audit logs API](https://help.openai.com/en/articles/9687866-admin-and-audit-logs-api-for-the-api-platform)

**Cursor** — [cloud agent](https://cursor.com/docs/cloud-agent) ·
[automations](https://cursor.com/docs/cloud-agent/automations) ·
[setup](https://cursor.com/docs/cloud-agent/setup) ·
[security & network](https://cursor.com/docs/cloud-agent/security-network) ·
[bugbot](https://cursor.com/docs/bugbot) ·
[compliance & monitoring](https://cursor.com/docs/enterprise/compliance-and-monitoring) ·
[privacy & data governance](https://cursor.com/docs/enterprise/privacy-and-data-governance)

**Copilot coding agent** — [about](https://docs.github.com/copilot/concepts/agents/coding-agent/about-coding-agent) ·
[allowlist reference](https://docs.github.com/en/copilot/reference/copilot-allowlist-reference) ·
[firewall](https://docs.github.com/copilot/customizing-copilot/customizing-or-disabling-the-firewall-for-copilot-coding-agent) ·
[secrets & variables](https://docs.github.com/en/copilot/how-tos/copilot-on-github/customize-copilot/customize-cloud-agent/configure-secrets-and-variables) ·
[reviewing a Copilot PR](https://docs.github.com/copilot/how-tos/agents/copilot-coding-agent/reviewing-a-pull-request-created-by-copilot)

**Devin** — [GitHub integration](https://docs.devin.ai/integrations/gh) ·
[automations](https://docs.devin.ai/product-guides/automations) ·
[security](https://docs.devin.ai/admin/security) ·
[audit logs API](https://docs.devin.ai/api-reference/v2/audit-logs)

**Jules** — [scheduled tasks](https://jules.google/docs/scheduled-tasks/) ·
[running tasks](https://jules.google/docs/running-tasks/) ·
[environment](https://jules.google/docs/environment/) ·
[FAQ](https://jules.google/docs/faq/)

**Coder** — [workspace scheduling](https://coder.com/docs/user-guides/workspace-scheduling) ·
[github to tasks](https://coder.com/docs/ai-coder/github-to-tasks) ·
[external auth](https://coder.com/docs/admin/external-auth) ·
[user secrets](https://coder.com/docs/user-guides/user-secrets) ·
[audit logs](https://coder.com/docs/admin/security/audit-logs)

**Ona** — [how Ona works](https://ona.com/docs/ona/understanding/how-ona-works) ·
[PR triggers](https://ona.com/docs/ona/automations/triggers/pullrequest) ·
[AWS runner](https://ona.com/docs/ona/runners/aws/overview) ·
[github source control](https://ona.com/docs/ona/source-control/github) ·
[secrets / env vars](https://ona.com/docs/ona/configuration/secrets/environment-variables)
