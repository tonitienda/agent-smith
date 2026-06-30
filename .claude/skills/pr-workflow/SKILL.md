---
name: pr-workflow
description: >
  Agent Smith pull-request lifecycle — open a PR, subscribe to activity, reply to
  and resolve every review thread, handle the Gemini/Copilot review order, and
  auto-merge when clean. Use when work on a branch is committed and pushed, when a
  PR exists for the branch, or when handling review comments or CI on a PR.
  Triggers: "open a PR", "PR review", "review comment", "CI failed on the PR",
  "merge the PR", "is the PR ready".
license: MIT
---

# PR workflow

## Open a PR (automatically)

When a unit of work on a feature branch is done — full gate
(`./scripts/agent-quality-gate.sh`) passes, committed and pushed — **open a PR
without waiting to be asked.** Clear title; body summarises the change, the
ticket(s) it closes, and how it was verified. Exceptions: a PR already exists
for the branch (push to it instead), or the user said not to.

## Subscribe and follow through

As soon as a PR exists (opened by you or from the Claude Code UI), call
`subscribe_pr_activity` for it. Then investigate every event: push fixes for
failing CI and actionable review feedback; ask via `AskUserQuestion` when a fix
is ambiguous. Keep watching until the PR is merged or closed, or the user says
stop (`unsubscribe_pr_activity`). CI *success* is not delivered as an event —
don't rely on the session noticing green.

## Reply to and resolve every review thread — no exceptions

For each thread (human or bot), once you've pushed a change addressing it (or
decided to decline): (1) post a short reply saying what you did and referencing
the commit, or why you're declining; (2) mark it resolved
(`resolve_review_thread`). Never leave an addressed comment silent; never resolve
without a reply. Skip only a thread that is your own reply echoed back.

## Review order

- **Copilot review: DISABLED (2026-06-19).** Do **not** call
  `request_copilot_review` — Copilot isn't reviewing right now. Skip entirely
  until this note is removed. (When re-enabled: request a Copilot review only
  once Gemini's review is posted and every Gemini thread is addressed and
  resolved, once per review cycle.)

## Auto-merge once clean

When every review thread (Gemini and any human) is resolved and the work is
complete, enable GitHub's native auto-merge (`enable_pr_auto_merge`) so it
merges the moment required checks pass — prefer this over manual merge, since CI
success doesn't wake the session. Use `merge_pull_request` directly only as a
fallback when you're already awake and have confirmed CI is green. Never enable
auto-merge while a thread is unresolved or a fix is pending.
