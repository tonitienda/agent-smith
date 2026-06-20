#!/usr/bin/env sh
# full — the canonical quality gate; run before every commit or handoff.
# Wraps ./scripts/agent-quality-gate.sh (make fmt test vet lint) unchanged.
# Run from the repository root.
set -eu
dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$dir/_lib.sh"
harness_init full
harness_run ./scripts/agent-quality-gate.sh
