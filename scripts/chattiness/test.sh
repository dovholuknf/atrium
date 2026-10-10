#!/usr/bin/env bash
# Checks chattiness.sh against the synthetic transcript in testdata/. If Claude Code changes its transcript format,
# the counts here stop matching and this fails.
#
#   scripts/chattiness/test.sh
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
root="$here/testdata/projects"
fail=0

# col <tsv-row-file> <name> : the value of one column.
col() { jq -Rrn --arg n "$2" '[inputs | split("\t")] | (.[0] | index($n)) as $i | .[1][$i]' < "$1"; }
want() { # <file> <name> <expected>
  got="$(col "$1" "$2")"
  if [ "$got" != "$3" ]; then echo "FAIL $2: got $got, want $3"; fail=1; fi
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Inside the window the early turn and the subagent file are left out. The offset form checks the RFC3339 parsing.
"$here/chattiness.sh" --root "$root" --since 2026-10-09T22:00:00-04:00 --until 2026-10-10T08:00:00-04:00 --tsv - > "$tmp/win.tsv"
[ "$(wc -l < "$tmp/win.tsv" | tr -d ' ')" = 2 ] || { echo "FAIL: want a header and one session row"; fail=1; }
f="$tmp/win.tsv"
want "$f" turns 6
want "$f" human 1
want "$f" a_turns 5
want "$f" say_turns 2
want "$f" notice_turns 1
want "$f" stop_turns 1
want "$f" nudge_turns 1
want "$f" mid_perm 1
want "$f" mid_say 1
want "$f" mid_notice 1
want "$f" mid_nudge 1
want "$f" tok_in 73
want "$f" tok_out 50
want "$f" tok_cread 730
want "$f" a_in 63
want "$f" a_out 45
want "$f" a_cread 630
want "$f" a_wall_s 69
want "$f" a_wall_max_s 30
want "$f" a_noact 3
want "$f" say_noact 1
want "$f" notice_noact 1
want "$f" stop_noact 0
want "$f" nudge_noact 1
want "$f" say_tok 582
want "$f" repeats 1

# With no window the early human turn comes in.
"$here/chattiness.sh" --root "$root" --tsv - > "$tmp/all.tsv"
want "$tmp/all.tsv" turns 7
want "$tmp/all.tsv" human 2
want "$tmp/all.tsv" tok_in 172

# The summary reads the same rows and says what it should.
"$here/chattiness.sh" --root "$root" --since 2026-10-09T22:00:00-04:00 --tsv "$tmp/out.tsv" > "$tmp/summary.md"
grep -q '| turns atrium started | 5 (83.3% of all turns) |' "$tmp/summary.md" || { echo "FAIL: summary turn share"; fail=1; }
grep -q '| sessions over the threshold | 1 of 1 |' "$tmp/summary.md" || { echo "FAIL: summary threshold"; fail=1; }
"$here/chattiness.sh" --since 2026-10-09T22:00:00-04:00 --from-tsv "$tmp/out.tsv" > "$tmp/summary2.md"
cmp -s "$tmp/summary.md" "$tmp/summary2.md" || { echo "FAIL: --from-tsv differs from the direct summary"; fail=1; }

[ "$fail" = 0 ] && echo "chattiness: ok"
exit "$fail"
