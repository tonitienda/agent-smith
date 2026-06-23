---
id: AS-120
title: User-delegated subagents — per-child cost itemization, prompt attribution, budget ceiling
status: done
github_issue: 385
depends_on: [AS-046, AS-020, AS-041]
area: cost
priority: P2
source: spun out of AS-046
---

# AS-120 · Per-child `/cost` itemization + prompt attribution + child budget

**Status: ready to implement** *(spun out of AS-046)*

## Description

AS-046 rolls a delegated child's usage onto the parent log as a sidechain
(`Thread.IsSidechain`, `Thread.AgentID`), so the parent's `/cost` total and
budget guard already include the spend. Three refinements remain to fully satisfy
the AS-046 "costs itemize per child" and "child permission prompts attributed"
acceptance criteria:

1. **`/cost` itemization.** The sidechain usage carries the child's `AgentID`, but
   `cost.Summarize` / the `/cost` view does not group or surface a per-child
   breakdown. Add a delegated-spend section (per child session: turns, tokens,
   dollars), without disturbing the existing flat turn list.

2. **Permission prompt attribution.** A child's tool call currently prompts
   through the parent's gate but is not visibly marked as coming from a delegated
   agent. Thread the child's identity into the permission `Request`/`Asker` so the
   TUI prompt reads e.g. "delegated agent <id> wants to run …".

3. **Per-child budget ceiling.** A child is bounded only by the loop's
   max-iterations today. Add an explicit (config-defaulted) per-delegation dollar
   ceiling so a runaway child halts on spend, independent of the parent ceiling.

## Acceptance criteria

- [x] `/cost` shows delegated spend itemized per child session, and the grand
      total still matches the rolled-up sum. (`cost.Summary.Delegated`, rendered
      as a "Delegated spend (per child, included above)" section; the children's
      turns stay in the flat list and grand total.)
- [x] A child's permission prompt in the TUI is attributed to the delegating
      agent. (`permission.Request.AgentID`, threaded through `tool.WithAgent` on
      the child's gate; the TUI card/modal show "delegated agent <id>".)
- [x] A delegation halts when its own spend reaches the configured per-child
      ceiling; the default and a `0`/unset value (no extra ceiling) are documented.
      (`budget.per_child_limit_usd`, default `0` = no extra ceiling; wired onto the
      child loop via `delegate.Parent.ChildBudgetUSD` + `Pricing`.)

## Dependencies

- AS-046 (delegation + rollup), AS-020 (`/cost`), AS-041 (budget guardrails).
