# PRD: Local Agent Workboard

> Status: Approved · Owner: Product · Date: 2026-06-30

## Problem

Smith can run async/background work, subagents, and orchestrated jobs, but users do not yet have a single place to see many concurrent tasks, dependencies, branches/worktrees, costs, approvals, and outcomes. Competitors are teaching users to expect parallel agents, background tasks, Kanban boards, and cloud task pages.

## Goal

Create a local-first **Agent Workboard** that lets users plan, launch, monitor, pause, resume, cancel, review, and merge multiple Smith tasks across TUI, headless, and daemon workflows.

## Non-goals

- Hosted multi-tenant live agents; D9 keeps that out of scope.
- Replacing GitHub issues or project management tools.
- Automatic merging without policy gates and explicit audit records.

## Users

- Power users running multiple fixes/features in parallel.
- Maintainers reviewing several Smith-generated PRs or branches.
- Teams dogfooding Smith as an orchestrator over repository maintenance.

## Requirements

1. Model each work item as a durable job linked to prompt, branch/worktree, dependencies, budget, policy gates, artifacts, and event-log session IDs.
2. Provide a TUI workboard with columns such as Planned, Running, Needs Review, Blocked, Done, and Failed.
3. Support local worktree isolation for parallel tasks and clearly show write scope.
4. Surface cost, model/provider route, quality-gate status, approval requirements, and merge readiness per card.
5. Integrate with existing daemon/orchestrator verbs rather than creating a second control plane.
6. Allow dependency chains where one card waits for another branch/artifact.
7. Export a machine-readable snapshot for headless/CI consumers.

## Acceptance criteria

- [ ] A user can launch at least three independent tasks, each in its own worktree, and monitor them from one TUI surface.
- [ ] The workboard shows which tasks are spending tokens or waiting for approval.
- [ ] A failed quality gate leaves the card in Needs Review/Blocked with the exact command and failure summary.
- [ ] The final merge/PR action is policy-checked, audited, and reversible where possible.
- [ ] The feature reuses existing orchestrator/run-store concepts and documents any required schema additions as additive-only.

## Open questions for debrief

- Should the initial workboard be TUI-only, CLI-only, or both?
  - Both.
- How should the workboard relate to GitHub issues and PRs in AS-147/AS-149?
  - GH is one of the potential triggers for background tasks. We should not couple workboard with GH.
  - In the future we will get multiple triggers: scheduled, slack, etc for the workboard they are just trigger sources
- Should auto-commit be opt-in per card, per project, or per policy profile?
  - All of them. We can offer a hierarchical configuration. e.g. project allows for auto-commit but a given task blocks it.

## Competitive inspiration

See [competitors.md](competitors.md), especially Cline Kanban, OpenCode multi-session, Claude background mode, and Codex cloud tasks.
