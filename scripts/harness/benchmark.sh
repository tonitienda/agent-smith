#!/usr/bin/env sh
# benchmark — D5 cost/speed guardrail suite (AS-030). Runs the offline scripted
# suite and writes JSON + Markdown reports under .cache/bench/. This is NOT a CI
# gate: it is on-demand, deterministic fixture validation. For a real-provider
# run, invoke `go run ./cmd/bench -provider <name>` directly. Run from the repo root.
set -eu
dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$dir/_lib.sh"
harness_init benchmark
harness_run go run ./cmd/bench -out .cache/bench "$@"
