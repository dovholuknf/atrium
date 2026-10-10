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
want "$f" turns 8
want "$f" human 1
want "$f" a_turns 7
want "$f" say_turns 4
want "$f" notice_turns 1
want "$f" stop_turns 1
want "$f" nudge_turns 1
want "$f" mid_perm 1
want "$f" mid_say 1
want "$f" mid_notice 1
want "$f" mid_nudge 1
want "$f" tok_in 96
want "$f" tok_out 140
want "$f" tok_cread 960
want "$f" a_in 86
want "$f" a_out 135
want "$f" a_cread 860
want "$f" a_wall_s 99
want "$f" a_wall_max_s 30
want "$f" a_noact 3
want "$f" say_noact 1
want "$f" notice_noact 1
want "$f" stop_noact 0
want "$f" nudge_noact 1
want "$f" say_tok 925
want "$f" repeats 1
want "$f" rep_say 1
want "$f" rep_notice 0

# What woke each turn: turns, fresh tokens (input + cache write + output), cache reads and wall seconds per class.
for kv in c_errand_turns=1 c_errand_fresh=51 c_errand_cread=110 c_errand_wall_s=10 \
          c_result_turns=1 c_result_fresh=62 c_result_cread=120 c_result_wall_s=20 \
          c_notice_turns=3 c_notice_fresh=26 c_notice_cread=130 c_notice_wall_s=29 \
          c_repeat_turns=1 c_repeat_fresh=60 c_repeat_cread=300 c_repeat_wall_s=30 \
          c_ack_turns=1 c_ack_fresh=22 c_ack_cread=200 c_ack_wall_s=10; do
  want "$f" "${kv%%=*}" "${kv#*=}"
done

# The first column is the turn time written back by hand-made date arithmetic.
want "$tmp/win.tsv" first 2026-10-10T03:00:00Z

# Windows jq has no strptime. None of the date builtins that need it may appear in the script's code.
if grep -v '^[[:space:]]*#' "$here/chattiness.sh" | grep -Eq 'fromdate|todate|strptime|strftime|mktime|gmtime'; then
  echo "FAIL: chattiness.sh uses a date builtin Windows jq lacks"; fail=1
fi

# A Windows-style TSV: CRLF line ends, plus one row a column short and one with text in a number column.
head -n 2 "$tmp/win.tsv" | sed 's/$/\r/' > "$tmp/crlf.tsv"
printf 'short\trow\r\n' >> "$tmp/crlf.tsv"
sed -n 2p "$tmp/win.tsv" | jq -Rr 'split("\t") | .[3] = "notanumber" | join("\t")' | sed 's/$/\r/' >> "$tmp/crlf.tsv"
"$here/chattiness.sh" --from-tsv "$tmp/crlf.tsv" > "$tmp/crlf.md" 2> "$tmp/crlf.err"
grep -q 'Sessions: 1\.' "$tmp/crlf.md" || { echo "FAIL: CRLF rows not read as one session"; fail=1; }
grep -q '| turns a message started (errands, results and chatter) | 7 (87.5% of all turns) |' "$tmp/crlf.md" || { echo "FAIL: CRLF summary figures"; fail=1; }
grep -q 'dropped 2 malformed row' "$tmp/crlf.err" || { echo "FAIL: malformed rows not reported"; fail=1; }
"$here/chattiness.sh" --check-tsv "$tmp/crlf.tsv" 2>/dev/null | grep -q "$(printf '\r')" && { echo "FAIL: CR left in cleaned rows"; fail=1; }

# With no window the early human turn comes in.
"$here/chattiness.sh" --root "$root" --tsv - > "$tmp/all.tsv"
want "$tmp/all.tsv" turns 9
want "$tmp/all.tsv" human 2
want "$tmp/all.tsv" tok_in 195

# The summary reads the same rows and says what it should.
"$here/chattiness.sh" --root "$root" --since 2026-10-09T22:00:00-04:00 --tsv "$tmp/out.tsv" > "$tmp/summary.md"
grep -q '| turns a message started (errands, results and chatter) | 7 (87.5% of all turns) |' "$tmp/summary.md" || { echo "FAIL: summary turn share"; fail=1; }
grep -q '| say | 5 | 1 | 20% |' "$tmp/summary.md" || { echo "FAIL: summary repeats by source"; fail=1; }
grep -qF '1 repeats in 1 session(s): `[atrium] peer-a@room says: hello there`' "$tmp/summary.md" || { echo "FAIL: summary top repeated text"; fail=1; }
grep -q '^\*\*Chatter: 5 of 8 turns (62.5%), 108 of 236 fresh tokens (45.8%), 630 of 960 cache-read tokens (65.6%).\*\*$' "$tmp/summary.md" ||
  { echo "FAIL: summary headline"; fail=1; }
grep -q '^| errand | 1 | 51 | 110 | 10 | 21.6% | 11.5% |$' "$tmp/summary.md" || { echo "FAIL: summary class row"; fail=1; }
# collect.sh with the local room, as a person runs it.
mkdir -p "$tmp/home/.claude"; cp -R "$root" "$tmp/home/.claude/projects"
HOME="$tmp/home" "$here/collect.sh" --rooms local --since 2026-10-09T22:00:00-04:00 --tsv "$tmp/local.tsv" > "$tmp/local.md" 2>&1 </dev/null || { echo "FAIL: collect.sh --rooms local"; fail=1; }
grep -q 'Rooms covered: local' "$tmp/local.md" || { echo "FAIL: collect.sh --rooms local covered nothing"; fail=1; }
grep -q '| sessions over the threshold | 1 of 1 |' "$tmp/summary.md" || { echo "FAIL: summary threshold"; fail=1; }
"$here/chattiness.sh" --since 2026-10-09T22:00:00-04:00 --from-tsv "$tmp/out.tsv" > "$tmp/summary2.md"
cmp -s "$tmp/summary.md" "$tmp/summary2.md" || { echo "FAIL: --from-tsv differs from the direct summary"; fail=1; }

# collect.sh sends the program and its arguments over stdin and puts only the shell on the ssh command line. A stub
# ssh stands in for the Windows room: it records its arguments and runs the last one, which is `bash -s`.
mkdir "$tmp/bin"
cat > "$tmp/bin/ssh" <<'STUB'
#!/usr/bin/env bash
for a in "$@"; do last="$a"; done
printf '%s\n' "$@" > "$SSH_ARGS_FILE"
exec bash -c "$last"
STUB
chmod +x "$tmp/bin/ssh"
SSH_ARGS_FILE="$tmp/ssh.args" PATH="$tmp/bin:$PATH" "$here/collect.sh" --rooms stubwin --win-rooms stubwin --win-bash bash --win-root "$root" \
  --since 2026-10-09T22:00:00-04:00 --until 2026-10-10T08:00:00-04:00 --tsv "$tmp/merged.tsv" > "$tmp/collect.md" 2> "$tmp/collect.err" ||
  { echo "FAIL: collect.sh over the stub ssh"; cat "$tmp/collect.err"; fail=1; }
grep -q 'Rooms covered: stubwin' "$tmp/collect.md" || { echo "FAIL: collect.sh did not cover the stub room"; fail=1; }
grep -q 'stubwin:-fixture-proj' "$tmp/merged.tsv" || { echo "FAIL: merged rows lack the room prefix"; fail=1; }
if grep -q 'select\|jq' "$tmp/ssh.args"; then echo "FAIL: a jq program reached the ssh command line"; fail=1; fi

# A worker's report arrives wrapped in pasted_content over several lines and does not open with done or blocked.
# It is still a result.
mkdir -p "$tmp/r2/p"
cat > "$tmp/r2/p/s.jsonl" <<'R2'
{"type":"user","isSidechain":false,"timestamp":"2026-10-10T03:00:00.000Z","message":{"role":"user","content":"[atrium] worker-x@room says: \n\n<pasted_content id=\"9\">\nreport from worker-x: finished the card\nsecond line\n</pasted_content>"}}
{"type":"assistant","isSidechain":false,"timestamp":"2026-10-10T03:00:10.000Z","message":{"id":"r1","role":"assistant","content":[{"type":"tool_use","id":"t","name":"Bash","input":{}}],"usage":{"input_tokens":10,"output_tokens":500,"cache_read_input_tokens":100,"cache_creation_input_tokens":0}}}
R2
"$here/chattiness.sh" --root "$tmp/r2" --tsv - > "$tmp/r2.tsv"
want "$tmp/r2.tsv" c_result_turns 1
want "$tmp/r2.tsv" c_errand_turns 0

[ "$fail" = 0 ] && echo "chattiness: ok"
exit "$fail"
