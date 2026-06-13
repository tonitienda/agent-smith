#!/usr/bin/env sh
# Deterministic quality gate for human and agent-driven changes.
set -eu

make fmt
make test
make vet
make lint
