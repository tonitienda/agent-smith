#!/usr/bin/env sh
# quick — inner-loop gate while editing: format plus a fast test subset.
# Usage: scripts/harness/quick.sh [packages...]
# Pass the packages you changed (e.g. ./internal/loop/...) for fast, focused
# feedback; with no arguments it falls back to ./... Run from the repo root.
# This is not a substitute for `full` before handoff.
set -eu
dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$dir/_lib.sh"
harness_init quick
harness_run make fmt
if [ "$#" -eq 0 ]; then
	set -- ./...
fi
harness_run go test "$@"
