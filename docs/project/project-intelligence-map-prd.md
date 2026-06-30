# PRD: Project Intelligence Map

> Status: Approved · Owner: Product · Date: 2026-06-30

This PRD assumes the broader product premise that Smith should prefer progressive
disclosure over command sprawl: new capabilities should fit a small, learnable
surface before introducing another top-level command.

## Problem

Agent Smith has strong session-level context observability, but it does not yet give users and agents a durable, inspectable map of the codebase itself. Competitors have memorable primitives here: Aider's repo map summarizes important symbols, while IDE agents benefit from editor, linter, and navigation awareness. Smith should turn this into a provider-neutral, event-log-native capability.

## Goal

Create a versioned **Project Intelligence Map** that summarizes repository structure, symbols, ownership boundaries, test/build commands, dependency edges, and recent hot spots with citations and freshness metadata. The map should be cheap to project into model context, easy to inspect in `/context`, and safe to refresh incrementally.

## Non-goals

- Replace LSPs or full static analysis for every language.
- Introduce non-stdlib dependencies without an explicit ticket.
- Make broad automated refactors without the normal approval and quality gates.

## Users

- Developers starting a session in a large or unfamiliar repo.
- Background/async agents that need cheap orientation before editing.
- Reviewers who want to understand why Smith selected files or commands for context.

## Requirements

1. Build a repository map with file tree summaries, detected languages/frameworks, important symbols, package boundaries, test commands, and known agent instructions.
2. Store map updates as append-only events or derived artifacts with provenance to source files and generation inputs.
3. Track freshness: git commit, file mtime/hash, analyzer version, and stale entries.
4. Expose `/map` and `/context map` views that show what is included, why, token cost, and citations.
5. Let the context projection include selected map slices instead of repeatedly reading broad file sets.
6. Support incremental refresh after file edits and an explicit full rebuild command.
7. Keep failures non-blocking: if a language analyzer fails, Smith falls back to file/path summaries and records the limitation.

## Acceptance criteria

- [ ] A cold-start session can generate a map for a medium repo and cite source files for every symbol/package claim.
- [ ] A subsequent turn can include a compact map slice with lower token cost than rereading the same files.
- [ ] `/context` shows map slices as first-class context blocks with provenance and freshness.
- [ ] Editing a mapped file marks affected entries stale until refresh.
- [ ] The feature is covered by offline fixtures and does not require network access.

## Open questions for debrief

- Which language(s) should get structured symbol extraction first?
  - Let's start with Golang, JS, TS, Python
- Should map artifacts live in the session log only, or also in a project cache?
  - Project cache as well
- How much of this belongs in the existing `/init`, `/context`, and Coding Mode surfaces versus a new `/map` command?
  - We did not release the product yet. Prefer collapsing this into existing surfaces unless a standalone `/map` command proves clearly better.

## Competitive inspiration

See [competitors.md](competitors.md), especially Aider repo maps, Cursor/Cascade real-time codebase awareness, and OpenCode LSP-enabled context.
