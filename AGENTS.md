# Agent instructions

Read and follow [`CLAUDE.md`](CLAUDE.md). It is the shared working guide for all coding agents in this repository so agent behavior stays consistent across tools.

Before any commit or handoff, follow the harness command contract in [`docs/agent-quality-gates.md`](docs/agent-quality-gates.md): run the `full` gate (`./scripts/agent-quality-gate.sh`) and use the `quick`/`arch` entry points for inner-loop feedback. The contract also documents CI/local parity and the failure-reporting format.
