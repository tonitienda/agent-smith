#!/usr/bin/env sh
# Shared helpers for scripts/harness/*.sh. Sourced, never run directly.
#
# Each wrapper sources this, calls harness_run for every command it executes,
# and gets two guarantees for free: every command is printed before it runs,
# and a concise summary is written to an ignored file under .cache/harness/.
# Underlying exit codes are preserved because the wrappers run under `set -e`:
# the first failing harness_run aborts the script with that command's status.

harness_cache="${HARNESS_CACHE_DIR:-.cache/harness}"

harness_init() {
	harness_name="$1"
	mkdir -p "$harness_cache"
	harness_summary="$harness_cache/$harness_name.log"
	{
		printf 'harness: %s\n' "$harness_name"
		printf 'started: %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
	} >"$harness_summary"
	trap 'exit 129' HUP
	trap 'exit 130' INT
	trap 'exit 143' TERM
	trap 'harness_finish $?' EXIT
}

# harness_run CMD [ARGS...] — print the command, record it, then run it.
harness_run() {
	printf '+ %s\n' "$*"
	printf 'run: %s\n' "$*" >>"$harness_summary"
	"$@"
}

harness_finish() {
	printf 'finished: %s exit=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$1" >>"$harness_summary"
}
