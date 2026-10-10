#!/usr/bin/env bash
# Run chattiness.sh against every room's transcripts and merge the rows into one summary.
#
#   scripts/chattiness/collect.sh --since 2026-10-09T22:00:00-04:00 --until 2026-10-10T08:00:00-04:00
#   scripts/chattiness/collect.sh --rooms m1mini,sg4,sg3 --tsv merged.tsv
#
# A room named like this machine (or `local`) runs here, on ~/.claude/projects. Any other room is reached with
# `ssh <host> bash -s`: sg3 and sg4 are Windows and read C:/Users/claude/.claude/projects, the rest their default.
# Each needs ssh without a prompt, bash (git-bash) and jq on the other side. chattiness.sh itself travels over stdin, so nothing is installed there. A room that cannot be reached is named on stderr with the exact error
# and left out of the merge. The summary lists which rooms made it in.
set -uo pipefail
here="$(cd "$(dirname "$0")" && pwd)"

since="1970-01-01T00:00:00Z"
until_="2999-01-01T00:00:00Z"
rooms="m1mini,sg4,sg3"
tsv="chattiness-merged.tsv"
$1win_bash="C:/PROGRA~1/Git/bin/bash.exe"
me="$(hostname 2>/dev/null | cut -d. -f1 | tr "[:upper:]" "[:lower:]")"

while [ $# -gt 0 ]; do
  case "$1" in
    --since) since="$2"; shift 2 ;;
    --until) until_="$2"; shift 2 ;;
    --rooms) rooms="$2"; shift 2 ;;
    --tsv) tsv="$2"; shift 2 ;;
    --win-root) win_root="$2"; shift 2 ;;
    --win-bash) win_bash="$2"; shift 2 ;;
    -h|--help) sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "collect: unknown argument $1" >&2; exit 2 ;;
  esac
done

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
covered=""
missed=""

for room in ${rooms//,/ }; do
  out="$work/$room.tsv"
  lc="$(printf "%s" "$room" | tr "[:upper:]" "[:lower:]")"
  if [ "$room" = local ] || [ "$lc" = "$me" ]; then
    "$here/chattiness.sh" --since "$since" --until "$until_" --tsv - > "$out" 2> "$work/$room.err"
  else
    # The program and its arguments both travel on stdin. Nothing but the shell's own path is on the ssh command line,
    # because the default shell on a Windows room is cmd or PowerShell and mangles quotes and $ in a jq program.
    remote_bash=bash
    args=(--since "$since" --until "$until_" --tsv -)
    case "$room" in sg3|sg4) remote_bash="$win_bash"; args=(--root "$win_root" "${args[@]}") ;; esac
    { printf 'set --'; printf ' %q' "${args[@]}"; printf '\n'; cat "$here/chattiness.sh"; } |
      ssh -o BatchMode=yes -o ConnectTimeout=10 "$room" "$remote_bash -s" > "$out" 2> "$work/$room.err"
  fi
  rc=$?
  if [ $rc -ne 0 ] || [ ! -s "$out" ]; then
    echo "collect: room $room failed (exit $rc): $(tr '\n' ' ' < "$work/$room.err")" >&2
    missed="$missed $room"
    continue
  fi
  covered="$covered $room"
done

[ -n "$covered" ] || { echo "collect: no room answered" >&2; exit 1; }

first=1
: > "$tsv"
for room in $covered; do
  "$here/chattiness.sh" --check-tsv "$work/$room.tsv" > "$work/$room.clean" || continue
  if [ $first = 1 ]; then head -n 1 "$work/$room.clean" >> "$tsv"; first=0; fi
  tail -n +2 "$work/$room.clean" | jq -Rr --arg r "$room" 'split("\t") | .[1] = $r + ":" + .[1] | @tsv' | tr -d '\r' >> "$tsv"
done

"$here/chattiness.sh" --since "$since" --until "$until_" --from-tsv "$tsv"
echo
echo "Rooms covered:${covered}."
[ -z "$missed" ] || echo "Rooms NOT covered:${missed} (see stderr for the error)."
