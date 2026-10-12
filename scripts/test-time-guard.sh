#!/usr/bin/env bash
# Fails when the Go suite gets slow. Reads `go test -json` output.
#
#   scripts/test-time-guard.sh build.claude/ci/go-test.json
#   go test -json ./... | scripts/test-time-guard.sh
#
# A package over 60s, or a single test over 5s, fails unless scripts/test-time-allow.txt has a line for it. The
# parsing is scripts/ci-report.go's, run with -guard-only, so there is one reader of the stream. Run by scripts/ci.sh
# after the tests. Exit 0 when nothing is over, 1 when something is, 2 when it could not run.
#
# ALLOW=path uses another allowlist, which the self-test does. TEST_TIME_SCALE=2 multiplies every limit and ceiling,
# which scripts/ci.sh sets on a hosted runner: the ceilings were measured on a developer's box.

set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"

allow="${ALLOW:-scripts/test-time-allow.txt}"
if [ $# -gt 1 ]; then
  echo "usage: scripts/test-time-guard.sh [go-test.json]" >&2
  exit 2
fi
if [ $# -eq 1 ]; then
  if [ ! -f "$1" ]; then
    echo "test-time-guard: no such file: $1" >&2
    exit 2
  fi
  exec go run scripts/ci-report.go -guard-only -scale "${TEST_TIME_SCALE:-1}" -allow "$allow" < "$1"
fi
exec go run scripts/ci-report.go -guard-only -scale "${TEST_TIME_SCALE:-1}" -allow "$allow"
