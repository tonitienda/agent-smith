#!/usr/bin/env sh
# arch — architecture/package-boundary checks; run after moving packages,
# adding interfaces, or changing dependency direction. Run from the repo root.
set -eu
dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$dir/_lib.sh"
harness_init arch
harness_run go test ./internal/archtest/...
