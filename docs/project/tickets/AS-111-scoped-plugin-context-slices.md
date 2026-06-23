---
id: AS-111
title: Scope-gated context slices for third-party sub-agents
status: ready-to-implement
github_issue: 377
depends_on: [AS-044]
area: security
priority: P1
source: docs/design/plugin-trust.md §2–§3; spun out of AS-059
---

# AS-111 · Scope-gated context slices for third-party sub-agents

**Status: ready to implement** *(spun out of the AS-059 plugin-trust spike)*

## Description

AS-059 defined a subtractive permission-scope vocabulary (`read_metadata`,
`read_own_span`, `read_transcript`, `read_file_contents`, plus `propose_edit` /
`propose_message`) and the default context-slice exclusions a third-party
(untrusted) sub-agent gets before consent widens them. Today `Teardown` receives
the **raw** block slice; the `declarative` wrapper ignores it, so there is no live
leak — but the moment a model-using or proposal-consuming third-party path exists,
the slice is the exfiltration channel.

This ticket implements the scope vocabulary and the slice derivation:

1. Add the new scope values to `internal/subagent` `Permission` (additive — keep
   `read_transcript`/`propose_edit`).
2. Derive a third-party sub-agent's teardown slice from its granted scopes per the
   AS-059 §3 default exclusions: start at metadata-only, widen per scope, always
   strip provider/auth metadata (a hard floor, not a grantable scope). Redaction is
   **not** a step in this filter: AS-056 redacts at capture, so the blocks the scope
   filter operates on are *already redacted* — the filter only selects/narrows, it
   never sees or scrubs raw secrets.
3. First-party built-ins bypass the subtractive default (trusted, in-tree) but not
   the auth-metadata floor or redaction.

Land this **with** the first real third-party consumer, not speculatively (YAGNI).

## Acceptance criteria

- [ ] The new read/propose scopes parse and validate; unknown scopes still reject.
- [ ] A third-party sub-agent with only `read_metadata` receives envelopes (no bodies).
- [ ] `read_file_contents` is required to see `file_read`/edit block bodies.
- [ ] Provider/auth metadata never appears in any slice for any sub-agent.
- [ ] First-party built-ins still receive full slices (minus the auth floor).

## Dependencies

- AS-044 (the registry/manifest); interacts with AS-056 (redaction-at-capture).
</content>
