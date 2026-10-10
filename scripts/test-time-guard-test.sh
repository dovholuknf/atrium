#!/usr/bin/env bash
# Self-test of scripts/test-time-guard.sh against fixtures. Run by scripts/ci.sh. No bats.
set -uo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"
export ALLOW=scripts/testdata/time-guard-allow.txt
fail=0

out="$(bash scripts/test-time-guard.sh scripts/testdata/time-guard-fast.json 2>&1)"; rc=$?
if [ "$rc" != 0 ]; then echo "FAILED: fast fixture should pass, exit $rc: $out" >&2; fail=1; fi

out="$(bash scripts/test-time-guard.sh scripts/testdata/time-guard-slow.json 2>&1)"; rc=$?
if [ "$rc" != 1 ]; then echo "FAILED: slow fixture should exit 1, got $rc: $out" >&2; fail=1; fi
for want in "SLOW TEST example.com/atrium/internal/quick TestTooSlow took 7.50s, the limit is 5s" \
            "SLOW PACKAGE example.com/atrium/internal/quick took 61.4s, the limit is 60s" \
            "SLOW TEST example.com/atrium/internal/slowok TestSlowButListed took 13.00s, the limit is 10s (its allowlist ceiling)" \
            "add a line to scripts/test-time-allow.txt"; do
  case "$out" in *"$want"*) ;; *) echo "FAILED: slow fixture output lacks: $want" >&2; fail=1 ;; esac
done

out="$(bash scripts/test-time-guard.sh scripts/testdata/time-guard-slow.json < /dev/null 2>&1 | grep -c 'TestA ')"
if [ "$out" != 0 ]; then echo "FAILED: a fast test was reported" >&2; fail=1; fi

out="$(bash scripts/test-time-guard.sh < scripts/testdata/time-guard-slow.json 2>&1)"; rc=$?
if [ "$rc" != 1 ]; then echo "FAILED: stdin should work too, exit $rc" >&2; fail=1; fi

[ "$fail" = 0 ] && echo "ok: test-time-guard self-test"
exit "$fail"