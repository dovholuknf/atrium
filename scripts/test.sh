#!/usr/bin/env bash
# The Go tests. One script, run by you and by CI.
#
#   bash scripts/test.sh                        unit tests, every package
#   bash scripts/test.sh unit ./internal/store/ unit tests, one package
#   bash scripts/test.sh all                    unit AND integration tests. CI runs this, and only CI
#
# Anything after the mode goes to `go test` as given: packages, -run, -v, -json, -p, -timeout.
#
# UNIT AND INTEGRATION ARE SPLIT BY A BUILD TAG. A unit test runs in under 10ms, uses no external dependency (no
# database, no files on disk, no sockets, no processes) and never sleeps or waits on a timer. Every other test is
# an integration test and lives in a file named `*_integration_test.go` whose first line is
#
#   //go:build integration
#
# Without `-tags integration` Go does not compile those files at all, so `go test ./...` and `unit` cannot run one
# by accident. CI runs every test on every push, and CI is the only place integration tests run. Run one by hand
# only when CI fails on it, or when you add or change one.

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
#
# A UNIT RUN HAS A BUDGET: scripts/unit-budget.go reads the -json stream and fails the run, with names and times,
# when any test took over 10ms. -json given by hand opts out, since the stream is then yours.
if [ "$mode" = unit ]; then
  wrap=1; vflag=()
  for a in "$@"; do
    case "$a" in -json|-test.json) wrap=0 ;; -v|-test.v) vflag=(-v) ;; esac
  done
  if [ "$wrap" = 1 ]; then
    go test -json -count=1 "$@" | go run scripts/unit-budget.go ${vflag[@]+"${vflag[@]}"}
    codes=("${PIPESTATUS[@]}")
    [ "${codes[0]}" = 0 ] && [ "${codes[1]}" = 0 ]
    exit $?
  fi
fi
exec go test -count=1 ${tags[@]+"${tags[@]}"} "$@"
