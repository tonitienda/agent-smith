---
id: AS-163
title: Orchestrator job-spec model + validator
status: done
area: orchestrator
priority: P2
github_issue: null
depends_on: [AS-159, AS-160]
source: docs/design/job-spec-dsl.md
---

# AS-163 · Orchestrator job-spec model + validator

## Description

Carved out of **AS-161** so the daemon/scheduler/SQLite store can be built
against a stable, executable target. AS-160 *froze the format* of
`.agent-smith/jobs/*.yaml` ([job-spec-dsl.md](../../design/job-spec-dsl.md)) but
shipped no code; every downstream orchestrator ticket (AS-147/149 actions,
AS-150 routing, AS-151 session-log integration, AS-152 dogfood pack, AS-154
secrets, AS-157 merge gating) consumes a *parsed, validated* spec. This ticket
turns the 17 normative validation rules (§5) into a typed Go model and a
fail-closed validator.

Scope is the **model + validator only**. It is decoding-agnostic: `spec.Load`
takes the generic map both `encoding/json` and `gopkg.in/yaml.v3` produce, so the
package stays stdlib-only and the YAML-file plumbing (read `.agent-smith/jobs/`,
`yaml.Unmarshal`, cross-file `id` collision via `spec.CheckUnique`) lands with the
**AS-161** daemon, where the dependency decision belongs.

## Acceptance criteria

- [x] `internal/orchestrator/spec` package with typed `Spec` and `spec.Load`.
- [x] All 17 §5 validation rules enforced; each `Error` names file, field path,
      and rule number ("reads like a review comment, not a stack trace").
- [x] DSL duration grammar `^[0-9]+(s|m|h|d)$` parsed by an orchestrator-local
      parser (wider than `time.ParseDuration`; `90d` legal, `1h30m` not).
- [x] Fail-closed: unknown keys/kinds/actions/predicates, missing required
      blocks, unbounded concurrency/follow-up, undeclared labels/secret scopes,
      and plaintext-credential literals are all rejected.
- [x] Table tests cover the valid canonical spec plus one breakage per rule;
      cross-spec `id` uniqueness and the multi-trigger input cross-check covered.
- [x] `internal/orchestrator/spec` guarded stdlib-only by `internal/archtest`
      (orchestration tier reserved in `package-contracts.md`, ADR D-ORCH-3).

## Dependencies

[AS-159, AS-160]

## Notes

Deferred to **AS-161** (daemon): the YAML/file loader over `.agent-smith/jobs/`,
the SQLite run store, the scheduler, and the `smith runs daemon` surface. Action
*semantics* (AS-147/149), routing *resolution* (AS-150), and secret-scope→credential
*mapping* (AS-154) consume this model and are out of scope here — this ticket
fixes only the call shape and the load-time validity contract.
