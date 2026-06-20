#!/usr/bin/env sh
# install-git-hooks — point this clone's git hooks at the repo-owned .githooks/
# directory so `git commit` (and optionally `git push`) run the same harness
# scripts agents and CI use. Re-run any time; it is idempotent.
#
# Usage:
#   scripts/harness/install-git-hooks.sh                 # pre-commit (quick gate)
#   scripts/harness/install-git-hooks.sh --with-pre-push # also full gate on push
#   scripts/harness/install-git-hooks.sh --uninstall     # restore default hooks
#
# Bypass a hook for an emergency local commit/push with --no-verify.
set -eu
root=$(git rev-parse --show-toplevel)
cd "$root"

case "${1:-}" in
--uninstall)
	git config --unset core.hooksPath 2>/dev/null || true
	git config --unset harness.prePush 2>/dev/null || true
	printf 'Restored default git hooks (core.hooksPath unset).\n'
	exit 0
	;;
--with-pre-push)
	git config harness.prePush true
	;;
"") ;;
*)
	printf 'unknown argument: %s\n' "$1" >&2
	exit 2
	;;
esac

chmod +x .githooks/pre-commit .githooks/pre-push \
	scripts/harness/quick.sh scripts/harness/full.sh scripts/agent-quality-gate.sh
git config core.hooksPath "$root/.githooks"
printf 'Installed git hooks via core.hooksPath=.githooks\n'
printf '  pre-commit -> scripts/harness/quick.sh\n'
if [ "$(git config --bool harness.prePush 2>/dev/null || echo false)" = "true" ]; then
	printf '  pre-push   -> scripts/harness/full.sh\n'
else
	printf '  pre-push   -> disabled (enable with --with-pre-push)\n'
fi
