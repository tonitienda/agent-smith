#!/usr/bin/env sh
# ci-local — approximate every CI job locally, in CI job order, before pushing
# a larger branch. Mirrors .github/workflows/ci.yml (see the CI/local parity
# table in docs/agent-quality-gates.md). Run from the repository root.
set -eu
dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$dir/_lib.sh"
harness_init ci-local
harness_run make build
harness_run make test
harness_run make vet
harness_run make lint
