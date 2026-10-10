#!/usr/bin/env bash
# The Go tests. One script, run by you and by CI.
#
#   bash scripts/test.sh                        unit tests, every package
#   bash scripts/test.sh unit ./internal/store/ unit tests, one package
#   bash scripts/test.sh all                    unit AND integration tests. CI runs this
#
# Anything after the mode goes to `go test` as given: packages, -run, -v, -json, -p, -timeout.
#
# UNIT AND INTEGRATION ARE SPLIT BY A BUILD TAG. A test that starts a process (git, a shell, a pseudo terminal),
# connects a real hub to a real room, or waits on a real timer of a second or more lives in a file named
# `*_integration_test.go` whose first line is
#
#   //go:build integration
#
# Without `-tags integration` Go does not compile those files at all, so `unit` cannot run one by accident. They
# run in CI. Run them by hand only when CI fails on one, or when you add or change one.

set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"

mode="${1:-unit}"
case "$mode" in
  unit) tags=() ;;
  all) tags=(-tags integration) ;;
  -*|./*) mode=unit; tags=() ; set -- unit "$@" ;;
  *) echo "usage: scripts/test.sh [unit|all] [go test arguments]" >&2; exit 2 ;;
esac
shift

# No package named means every package.
pkgs=()
for a in "$@"; do
  case "$a" in ./*|github.com/*) pkgs=(x) ;; esac
done
[ ${#pkgs[@]} -eq 0 ] && set -- "$@" ./...

# -count=1 because a cached pass is not a pass.
exec go test -count=1 ${tags[@]+"${tags[@]}"} "$@"
