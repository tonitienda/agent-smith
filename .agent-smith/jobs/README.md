# `.agent-smith/jobs/` — orchestrator job specs

Versioned, declarative job specifications for the Smith orchestrator. One job per
`*.yaml` file, reviewed through normal PRs. The repository is the source of truth;
no cloud UI is required to read, write, or load a spec.

- **Format and full schema:** [docs/design/job-spec-dsl.md](../../docs/design/job-spec-dsl.md) (AS-160).
- **Who loads them:** the daemon (`smith runs daemon`, AS-161) discovers `*.yaml`
  here, validates each against the spec's validation contract, and fails closed on
  any violation.
- **Safety:** Smith never edits its own job specs and jobs never create other jobs
  ([ADR-159 D-ORCH-6](../../docs/architecture/orchestrator-architecture.md#d-orch-6--non-goals-fail-closed)).
  Labels, PR actions, and merges are declared workflow steps, never prompt text.

This directory holds no live specs yet — the loader arrives with AS-161. Until then
the worked examples in the spec doc are the canonical reference.
