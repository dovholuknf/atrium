#!/usr/bin/env bash
# Measure what atrium's messages cost a Claude Code session. Bash and jq only.
#
#   scripts/chattiness/chattiness.sh --since 2026-10-09T22:00:00-04:00 --until 2026-10-10T08:00:00-04:00
#   scripts/chattiness/chattiness.sh --root /path/to/projects --tsv rows.tsv
#   scripts/chattiness/chattiness.sh --from-tsv merged.tsv        (summary only, for collect.sh)
#
# Reads <root>/<project>/<session>.jsonl (not subagent files: those sit one level deeper and are not atrium's
# conversation). Writes one TSV row per session to --tsv (default ./chattiness.tsv, `-` for stdout with no
# summary) and a markdown summary to stdout. The columns, the markers and the verdict thresholds are in
# docs/runtime/chattiness.md. Change the two together.
set -euo pipefail

root="${HOME}/.claude/projects"
since="1970-01-01T00:00:00Z"
until_="2999-01-01T00:00:00Z"
tsv="chattiness.tsv"
from_tsv=""
check_tsv=""

while [ $# -gt 0 ]; do
  case "$1" in
    --root) root="$2"; shift 2 ;;
    --since) since="$2"; shift 2 ;;
    --until) until_="$2"; shift 2 ;;
    --tsv) tsv="$2"; shift 2 ;;
    --from-tsv) from_tsv="$2"; shift 2 ;;
    --check-tsv) check_tsv="$2"; shift 2 ;;
    -h|--help) sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "chattiness: unknown argument $1" >&2; exit 2 ;;
  esac
done

command -v jq >/dev/null 2>&1 || { echo "chattiness: jq is required" >&2; exit 2; }

# Date arithmetic by hand. Windows jq has no strptime, so fromdateiso8601, mktime and todate are not used anywhere
# in this script. A test checks that. days() is days from 1970-01-01 for a civil date, iso() turns epoch seconds back into a timestamp.
read -r -d '' CIVIL_JQ <<'JQ' || true
def num: if length > 1 and startswith("0") then .[1:] | tonumber else tonumber end;
def days($y; $m; $d):
  ($y - (if $m <= 2 then 1 else 0 end)) as $yy
  | ($yy / 400 | floor) as $era | ($yy - $era * 400) as $yoe
  | ((153 * ($m + (if $m > 2 then -3 else 9 end)) + 2) / 5 | floor) as $doy0
  | ($yoe * 365 + ($yoe / 4 | floor) - ($yoe / 100 | floor) + $doy0 + $d - 1) as $doe
  | $era * 146097 + $doe - 719468;
def pad: tostring | if length < 2 then "0" + . else . end;
def iso: floor as $s
  | ($s / 86400 | floor) as $dd | ($s - $dd * 86400) as $sec
  | ($dd + 719468) as $z | ($z / 146097 | floor) as $era | ($z - $era * 146097) as $doe
  | (($doe - ($doe / 1460 | floor) + ($doe / 36524 | floor) - ($doe / 146096 | floor)) / 365 | floor) as $yoe
  | ($doe - (365 * $yoe + ($yoe / 4 | floor) - ($yoe / 100 | floor))) as $doy
  | ((5 * $doy + 2) / 153 | floor) as $mp
  | ($doy - ((153 * $mp + 2) / 5 | floor) + 1) as $d
  | ($mp + (if $mp < 10 then 3 else -9 end)) as $m
  | ($yoe + $era * 400 + (if $m <= 2 then 1 else 0 end)) as $y
  | "\($y)-\($m | pad)-\($d | pad)T\($sec / 3600 | floor | pad):\($sec % 3600 / 60 | floor | pad):\($sec % 60 | pad)Z";
def stamp: days(.[0:4] | num; .[5:7] | num; .[8:10] | num) * 86400
  + (.[11:13] | num) * 3600 + (.[14:16] | num) * 60 + (.[17:19] | num);
JQ

# RFC3339 with an offset to epoch seconds.
read -r -d '' RFC_JQ <<'JQ' || true
def rfc: capture("^(?<d>[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2})(?<f>[.][0-9]+)?(?<z>Z|[+-][0-9]{2}:[0-9]{2})$")
  | (.d | stamp)
    - (if .z == "Z" then 0
       else ((if .z[0:1] == "-" then -1 else 1 end) * ((.z[1:3] | num) * 3600 + (.z[4:6] | num) * 60)) end);
JQ
RFC_JQ="$CIVIL_JQ"$'\n'"$RFC_JQ"

# The per-session program. Input is the raw JSONL lines (jq -nR). It segments the session into turns at every
# non-tool-result user entry and at every Stop hook that carried a message, tags each turn by what started it,
# and sums the assistant usage inside it.
read -r -d '' SESSION_JQ <<'JQ' || true
def epoch: stamp
  + (([.[19:] | capture("^(?<f>[.][0-9]+)").f | tonumber] | .[0]) // 0);
def txt: if type == "string" then .
  elif type == "array" then (map(select(type == "object" and .type == "text") | .text) | join("\n"))
  else "" end;
def rtxt: (.content | if type == "string" then . elif type == "array" then (map(.text? // "") | join("\n")) else "" end);
def key: gsub("</?pasted_content[^>]*>"; "") | gsub("^[ \t\n\r]+|[ \t\n\r]+$"; "") | gsub("[ \t\n\r]+"; " ") | .[0:200];
def src: if test("^[[]atrium[]] atrium says:") then "nudge"
  elif test("^[[]atrium[]] [^ ]+ says:") then "say"
  elif startswith("[atrium]") then "notice"
  elif startswith("Another Claude session sent a message") then "say"
  else null end;

def ev:
  (.timestamp | epoch) as $t
  | if .type == "assistant" then
      {t: $t, k: "asst", id: (.message.id // "x\($t)"), u: (.message.usage // {}),
       tools: [.message.content[]? | select(.type == "tool_use") | .name]}
    elif .type == "user" then
      (.message.content) as $c
      | if ($c | type) == "array" and ($c | length) > 0 and ($c[0] | type) == "object" and $c[0].type == "tool_result" then
          ($c[] | select(.type == "tool_result") | . as $r | ($r | rtxt) as $x
            | if $r.is_error == true and ($x | test("^PreToolUse:[^ ]+ hook error: (Message from|[[]atrium[]])")) then
                {t: $t, k: "mid", src: "perm", key: ($x | sub("^PreToolUse:[^ ]+ hook error: "; "") | key)}
              else empty end)
        else
          ($c | txt) as $x
          | ($x | src) as $s
          | if $s != null then {t: $t, k: "start", src: $s, key: ($x | key)}
            elif .isMeta == true or $x == "" or ($x | test("^(This session is being continued|Read BRIEF.md|<|Caveat:|[[]Request interrupted)"))
              then {t: $t, k: "start", src: "other"}
            else {t: $t, k: "start", src: "human"} end
        end
    elif .type == "attachment" then
      .attachment as $a
      | if $a.type == "queued_command" then
          (($a.prompt | txt) as $x | ($x | src) as $s
            | if $s != null then {t: $t, k: "mid", src: $s, key: ($x | key)} else empty end)
        elif $a.type == "silent_turn_reminder" then {t: $t, k: "mid", src: "nudge"}
        else empty end
    elif .type == "system" and .subtype == "stop_hook_summary" then
      ((.hookErrors // []) | map(tostring) | join("\n")) as $x
      | if $x | test("Message from|open items from your workers|[[]atrium[]]") then {t: $t, k: "start", src: "stop", key: ($x | key)}
        else empty end
    else empty end;

def fin: . + {
  tok: ([.msgs[]] | {i: (map((.input_tokens // 0) + (.cache_creation_input_tokens // 0)) | add // 0),
                     o: (map(.output_tokens // 0) | add // 0),
                     c: (map(.cache_read_input_tokens // 0) | add // 0)}),
  wall: (.last - .t0),
  act: ([.tools[] | select(test("atrium_(say|report|done|blocked)$") | not)] | length > 0)
} | del(.msgs);
def seg($t; $s; $k): {src: $s, key: $k, t0: $t, last: $t, msgs: {}, tools: [], n: 0};

def cols: ["session", "project", "first", "turns", "human", "a_turns", "mid_perm", "mid_say", "mid_notice", "mid_nudge",
  "tok_in", "tok_out", "tok_cread", "a_in", "a_out", "a_cread", "a_wall_s", "a_wall_max_s", "a_noact", "repeats"]
  + ([("say", "notice", "stop", "nudge") as $s | ($s + "_turns", $s + "_tok", $s + "_noact", $s + "_wall_s")]);

if $hdr == "1" then cols | @tsv else
  [inputs | fromjson? | select(type == "object" and .isSidechain != true and .timestamp != null)
   | ev | select(.t >= $lo and .t <= $hi)] as $evs
  | (reduce $evs[] as $e ({segs: [], cur: null, mids: [], keys: []};
      if $e.k == "start" then
        (if .cur != null and .cur.n > 0 then .segs += [.cur | fin] else . end)
        | .cur = seg($e.t; $e.src; $e.key) | .keys += (if $e.key != null then [$e.key] else [] end)
      elif $e.k == "mid" then
        .mids += [$e] | .keys += (if $e.key != null then [$e.key] else [] end)
      else
        (if .cur == null then .cur = seg($e.t; "pre"; null) else . end)
        | .cur.msgs[$e.id] = $e.u | .cur.tools += $e.tools | .cur.last = $e.t | .cur.n += 1
      end)
     | (if .cur != null and .cur.n > 0 then .segs += [.cur | fin] else . end)) as $st
  | $st.segs as $segs
  | if ($segs | length) == 0 then empty else
      def sum(f): (map(f) | add // 0);
      def tk: .tok.i + .tok.o + .tok.c;
      ["say", "notice", "stop", "nudge"] as $A
      | [$segs[] | select(.src as $s | $A | index($s))] as $as
      | [$segs[] | select(.src != "pre")] as $turns
      | ($st.keys | group_by(.) | map(length - 1 | select(. > 0)) | add // 0) as $rep
      | [$sid, $proj, ($segs[0].t0 | iso),
         ($turns | length), ([$turns[] | select(.src == "human")] | length), ($as | length),
         ([$st.mids[] | select(.src == "perm")] | length), ([$st.mids[] | select(.src == "say")] | length),
         ([$st.mids[] | select(.src == "notice")] | length), ([$st.mids[] | select(.src == "nudge")] | length),
         ($segs | sum(.tok.i)), ($segs | sum(.tok.o)), ($segs | sum(.tok.c)),
         ($as | sum(.tok.i)), ($as | sum(.tok.o)), ($as | sum(.tok.c)),
         ($as | sum(.wall) | round), ($as | map(.wall) | max // 0 | round),
         ([$as[] | select(.act | not)] | length), $rep]
        + [$A[] as $s | ([$segs[] | select(.src == $s)]) as $g
           | ($g | length), ($g | sum(tk)), ([$g[] | select(.act | not)] | length), ($g | sum(.wall) | round)]
      | @tsv
    end
end
JQ
SESSION_JQ="$CIVIL_JQ"$'\n'"$SESSION_JQ"

# The summary. Input is the merged TSV, header first.
read -r -d '' SUMMARY_JQ <<'JQ' || true
def pct($a; $b): if $b == 0 then 0 else (($a * 1000 / $b) | round) / 10 end;
def n: if . >= 1000000 then "\((. / 100000 | round) / 10)M" elif . >= 1000 then "\((. / 100 | round) / 10)k" else tostring end;
def sum(f): (map(f) | add // 0);
([inputs] | map(split("\t"))) as $lines
| ($lines[0]) as $h
| ($lines[1:] | map(. as $r | [range(0; $h | length)] | map({key: $h[.], value: ($r[.] // "")}) | from_entries
    | with_entries(if .key == "session" or .key == "project" or .key == "first" then . else .value |= tonumber end))) as $rows
| ["say", "notice", "stop", "nudge"] as $A
| def atok: .a_in + .a_out + .a_cread;
  def alltok: .tok_in + .tok_out + .tok_cread;
  def chatty: .turns > 0 and (.a_turns > 0)
     and ((.a_turns / .turns > $tt) or (alltok > 0 and (atok / alltok) > $tk))
     and (.a_noact * 2 > .a_turns);
  ($rows | sum(.turns)) as $T | ($rows | sum(.a_turns)) as $AT
  | ($rows | sum(alltok)) as $K | ($rows | sum(atok)) as $AK
  | ($rows | sum(.a_noact)) as $NA
  | ($rows | sum(.mid_perm)) as $mp | ($rows | sum(.mid_say)) as $ms | ($rows | sum(.mid_notice)) as $mn | ($rows | sum(.mid_nudge)) as $mu
  | "# atrium chattiness\n",
    "Window: \($since) to \($until). Sessions: \($rows | length). Rooms: \($rows | map(.project) | unique | length) projects.\n",
    "## Totals\n",
    "| measure | value |", "|---|---|",
    "| turns, all | \($T) |",
    "| turns typed by a human | \($rows | sum(.human)) |",
    "| turns atrium started | \($AT) (\(pct($AT; $T))% of all turns) |",
    "| tokens, all (input + cache write + output + cache read) | \($K | n) |",
    "| tokens in atrium-started turns | \($AK | n) (\(pct($AK; $K))% of all tokens) |",
    "| atrium-started turns with no action | \($NA) (\(pct($NA; $AT))% of atrium-started turns) |",
    "| messages delivered mid-turn (not a turn start): permission hook / queued say / queued notice / reminder | \($mp) / \($ms) / \($mn) / \($mu) |",
    "| repeated deliveries of the same text to one session | \($rows | sum(.repeats)) |",
    "| sessions over the threshold | \([$rows[] | select(chatty)] | length) of \($rows | length) |\n",
    "## By source\n",
    "Turn share and token share are of ALL turns and ALL tokens. No action means the turn made no tool call other than",
    "atrium_say, atrium_report, atrium_done or atrium_blocked.\n",
    "| source | turns | turn share | tokens | token share | no action | mean wall s | verdict |", "|---|---|---|---|---|---|---|---|",
    ($A[] as $s
      | ($rows | sum(.[$s + "_turns"])) as $st | ($rows | sum(.[$s + "_tok"])) as $sk
      | ($rows | sum(.[$s + "_noact"])) as $sn | ($rows | sum(.[$s + "_wall_s"])) as $sw
      | (if $st == 0 then "none seen"
         elif $sn * 2 > $st and (pct($st; $T) >= 5 or pct($sk; $K) >= 5) then "PROBLEM"
         elif $sn * 2 > $st then "wasteful but small"
         else "fine" end) as $v
      | "| \($s) | \($st) | \(pct($st; $T))% | \($sk | n) | \(pct($sk; $K))% | \($sn) (\(pct($sn; $st))%) | \(if $st == 0 then 0 else ($sw / $st | round) end) | \($v) |"),
    "",
    "Mid-turn messages: permission hook \($mp), say \($ms), notice \($mn), silent-turn reminder \($mu). They start no turn, so they carry no",
    "token or wall figure of their own.\n",
    "## Worst sessions (by tokens in atrium-started turns)\n",
    "| session | project | turns | atrium turns | turn share | atrium tokens | token share | no action | over threshold |", "|---|---|---|---|---|---|---|---|---|",
    ($rows | sort_by(-atok) | .[0:10][]
      | "| \(.session[0:8]) | \(.project | .[-34:]) | \(.turns) | \(.a_turns) | \(pct(.a_turns; .turns))% | \(atok | n) | \(pct(atok; alltok))% | \(.a_noact) | \(if chatty then "yes" else "no" end) |"),
    "",
    "Thresholds (fixed before the first run): a session is chatty when atrium-started turns are over \($tt * 100)% of its turns or",
    "over \($tk * 100)% of its tokens, and more than half of those turns end with no action. A source is a PROBLEM when more than",
    "half of its turns end with no action and it holds 5% or more of all turns or of all tokens."
JQ

# Rows come back from Windows jq with CRLF line ends, and a row from a broken run can be short or hold text where a
# number belongs. Strip the CR, keep the header, and drop any row whose column count is not the header's or whose
# numeric columns (the fourth on) are not numbers. The drop count goes to stderr.
clean_tsv() {
  local n kept total
  local good='select(. != "") | select(split("\t") as $f | ($f | length) == $n and ($f[3:] | all(test("^[0-9.eE+-]+$"))))'
  n="$(head -n 1 "$1" | tr -d '\r' | jq -Rr 'split("\t") | length')"
  kept="$(tr -d '\r' < "$1" | tail -n +2 | jq -Rr --argjson n "$n" "$good" | wc -l | tr -d ' ')"
  head -n 1 "$1" | tr -d '\r'
  tr -d '\r' < "$1" | tail -n +2 | jq -Rr --argjson n "$n" "$good"
  total="$(tr -d '\r' < "$1" | tail -n +2 | jq -Rr 'select(. != "")' | wc -l | tr -d ' ')"
  [ "$total" = "$kept" ] || echo "chattiness: dropped $((total - kept)) malformed row(s) from $1" >&2
}

if [ -n "$from_tsv" ]; then
  [ -r "$from_tsv" ] || { echo "chattiness: cannot read $from_tsv" >&2; exit 2; }
  clean_tsv "$from_tsv" | jq -nRr --arg since "$since" --arg until "$until_" --argjson tt 0.25 --argjson tk 0.20 "$SUMMARY_JQ"
  exit 0
fi
if [ -n "$check_tsv" ]; then
  [ -r "$check_tsv" ] || { echo "chattiness: cannot read $check_tsv" >&2; exit 2; }
  clean_tsv "$check_tsv"
  exit 0
fi

[ -d "$root" ] || { echo "chattiness: no such directory $root" >&2; exit 2; }

# Bounds go through jq, which parses RFC3339 with an offset the same way on every platform.
lo=$(jq -n --arg s "$since" "$RFC_JQ \$s | rfc" | tr -d '\r') || { echo "chattiness: --since is not RFC3339" >&2; exit 2; }
hi=$(jq -n --arg s "$until_" "$RFC_JQ \$s | rfc" | tr -d '\r') || { echo "chattiness: --until is not RFC3339" >&2; exit 2; }

out="$(mktemp)"
trap 'rm -f "$out"' EXIT
jq -nr --arg hdr 1 --arg sid "" --arg proj "" --argjson lo 0 --argjson hi 0 "$SESSION_JQ" | tr -d '\r' > "$out"

find "$root" -mindepth 2 -maxdepth 2 -name '*.jsonl' | sort | while IFS= read -r f; do
  proj="$(basename "$(dirname "$f")")"
  sid="$(basename "$f" .jsonl)"
  jq -nRr --arg hdr 0 --arg sid "$sid" --arg proj "$proj" --argjson lo "$lo" --argjson hi "$hi" "$SESSION_JQ" < "$f" | tr -d '\r' >> "$out" ||
    echo "chattiness: skipped $f (jq failed)" >&2
done

if [ "$tsv" = "-" ]; then
  cat "$out"
  exit 0
fi
cp "$out" "$tsv"
jq -nRr --arg since "$since" --arg until "$until_" --argjson tt 0.25 --argjson tk 0.20 "$SUMMARY_JQ" < "$out"
