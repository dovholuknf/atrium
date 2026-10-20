#!/usr/bin/env bash
# Self-test of scripts/test-time-guard.sh against fixtures. Run by scripts/ci.sh. No bats.
set -uo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"
export ALLOW=scripts/testdata/time-guard-allow.txt
fail=0

expect() { # expect <fixture> <label> <text the output must hold>...
  local fixture="$1" label="$2"; shift 2
  out="$(bash scripts/test-time-guard.sh "scripts/testdata/$fixture" 2>&1)"; rc=$?
  if [ "$rc" != 0 ]; then echo "FAILED: $label should exit 0, got $rc: $out" >&2; fail=1; fi
  local want
  for want in "$@"; do
    case "$out" in *"$want"*) ;; *) echo "FAILED: $label output lacks: $want" >&2; fail=1 ;; esac
  done
}

expect time-guard-fast.json "fast fixture" "0 failures, 0 warnings"

expect time-guard-pkgonly.json "package over its limit" \
  "WARN PACKAGE example.com/atrium/internal/quick took 61.4s, the limit is 60s" "0 failures, 1 warnings"
case "$out" in *"WARN TEST"*) echo "FAILED: package-only fixture warned about a test" >&2; fail=1 ;; esac

expect time-guard-testonly.json "test over its limit" \
  "WARN TEST example.com/atrium/internal/quick TestTooSlow took 7.50s, the limit is 5s" "0 failures, 1 warnings"
case "$out" in *"WARN PACKAGE"*) echo "FAILED: test-only fixture warned about a package" >&2; fail=1 ;; esac

expect time-guard-slow.json "slow fixture" \
  "WARN TEST example.com/atrium/internal/quick TestTooSlow took 7.50s, the limit is 5s" \
  "WARN PACKAGE example.com/atrium/internal/quick took 61.4s, the limit is 60s" \
  "WARN TEST example.com/atrium/internal/slowok TestSlowButListed took 13.00s, the limit is 10s (its allowlist ceiling)" \
  "0 failures, 3 warnings" \
  "add a line to scripts/test-time-allow.txt"
case "$out" in *"SLOW "*) echo "FAILED: a SLOW line is still printed" >&2; fail=1 ;; esac

out="$(bash scripts/test-time-guard.sh scripts/testdata/time-guard-slow.json < /dev/null 2>&1 | grep -c 'TestA ')"
if [ "$out" != 0 ]; then echo "FAILED: a fast test was reported" >&2; fail=1; fi

out="$(bash scripts/test-time-guard.sh < scripts/testdata/time-guard-slow.json 2>&1)"; rc=$?
if [ "$rc" != 0 ]; then echo "FAILED: stdin should work too, exit $rc" >&2; fail=1; fi

bash scripts/test-time-guard.sh scripts/testdata/no-such-file.json > /dev/null 2>&1; rc=$?
if [ "$rc" != 2 ]; then echo "FAILED: a missing file should exit 2, got $rc" >&2; fail=1; fi

[ "$fail" = 0 ] && echo "ok: test-time-guard self-test"
exit "$fail"
