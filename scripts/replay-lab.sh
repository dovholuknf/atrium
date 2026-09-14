#!/usr/bin/env bash
#
# Render a LIVE card's scrollback both ways, without restarting the daemon.
#
# Every change to how scrollback renders is in Go, so seeing its effect used to
# mean a build, a wind-down, a restart, and every session on the board
# interrupted. That loop is slow enough that the rendering was twice changed on
# the strength of unit tests alone, and both times the tests were measuring the
# wrong thing and the pane was worse.
#
# This pulls the card's ring through `/v1/scrollback/raw`, which is the exact
# bytes `attach` replays, and runs both renderers over it with `go test`. The
# running daemon is not touched and nothing is restarted: the renderers being
# exercised are the ones in the working tree, not the ones in the binary that
# is serving.
#
#   bash scripts/replay-lab.sh <card-id-or-substring> [outdir]
#
# With a native capture of the same session at $ATRIUM_TRUTH, it also scores
# both renderings against it. That is the part that stops this becoming another
# metric that agrees with itself: the truth file is what a real terminal showed,
# captured with ctrl-a ctrl-c, and neither renderer has any say in it.

set -u
cd "$(dirname "$0")/.."

want=${1:-}
out=${2:-C:/temp/replay}
board=${ATRIUM_BOARD_URL:-http://localhost:7778}

if [ -z "$want" ]; then
  echo "usage: bash scripts/replay-lab.sh <card-id-or-substring> [outdir]" >&2
  echo >&2
  echo "supervised cards on this board:" >&2
  curl -s "$board/v1/tasks" |
    tr ',' '\n' | grep -E '"(id|display_title|supervised)"' |
    paste - - - 2>/dev/null | grep 'true' >&2
  exit 2
fi

mkdir -p "$out"

# The id, from a substring of the id or of the name. A full id works too, since
# a string contains itself.
id=$(curl -s "$board/v1/tasks" | tr '{' '\n' |
  grep -i "$want" | grep '"supervised":true' |
  grep -oE '"id":"[^"]+"' | head -1 | cut -d'"' -f4)
if [ -z "$id" ]; then
  echo "no supervised card matching '$want'" >&2
  exit 1
fi
echo "card: $id"

# The bytes, and the shape they were drawn at. Headers rather than a wrapper
# around the body, so what lands on disk is exactly what the ring holds and can
# be fed to a renderer without being unpacked.
hdr="$out/raw.headers"
curl -s -D "$hdr" -o "$out/raw.bin" "$board/v1/tasks/$id/scrollback/raw"
if [ ! -s "$out/raw.bin" ]; then
  echo "no ring for that card. is its runner still supervised?" >&2
  exit 1
fi
rows=$(grep -i '^X-Atrium-Rows:' "$hdr" | tr -d '\r' | awk '{print $2}')
cols=$(grep -i '^X-Atrium-Cols:' "$hdr" | tr -d '\r' | awk '{print $2}' | tr ',' ' ')
last=$(echo "$cols" | awk '{print $NF}')
echo "bytes: $(wc -c < "$out/raw.bin")   rows: $rows   widths: $cols"
echo

ATRIUM_DUMP_IN="$out/raw.bin" ATRIUM_DUMP_OUT="$out" \
  ATRIUM_DUMP_COLS="$last" ATRIUM_DUMP_ROWS="$rows" \
  go test ./internal/daemon/ -run TestDumpBothReplayPaths -count=1 -v |
  grep -E 'screen_dump_test|FAIL'
echo

# Trailing whitespace is the flattener's signature. It has to replace a cursor
# move with something and what it has is spaces, so a padded line is a line it
# guessed at.
for f in flatten screen; do
  awk -v n="$f" '
    { t++ ; if ($0 ~ /[ ]+\r?$/) p++ }
    END { printf "%-8s %5d lines, %4d padded (%.0f%%)\n", n, t, p+0, t ? 100*p/t : 0 }
  ' "$out/$f.txt"
done

truth=${ATRIUM_TRUTH:-}
[ -z "$truth" ] && { echo; echo "set ATRIUM_TRUTH to a native ctrl-a capture to score these"; exit 0; }

echo
strip() { sed -e 's/\x1b\[[0-9;?]*[a-zA-Z]//g' -e 's/\x1b\][^\x07]*\x07//g' -e 's/\r//g' "$1"; }
strip "$truth" | sed 's/[[:space:]]*$//' | grep -vE '^[[:space:]]*$' |
  awk 'length($0) >= 25' | sort -u > "$out/truth.lines"
total=$(wc -l < "$out/truth.lines")
echo "scored against $(basename "$truth"): $total distinct lines of 25+ chars"
for f in flatten screen; do
  strip "$out/$f.txt" | sed 's/[[:space:]]*$//' | sort -u > "$out/$f.lines"
  hit=$(comm -12 "$out/truth.lines" "$out/$f.lines" | wc -l)
  awk -v n="$f" -v h="$hit" -v t="$total" \
    'BEGIN { printf "%-8s %4d of %4d  %5.1f%%\n", n, h, t, t ? 100*h/t : 0 }'
done
echo
echo "lines the truth has that the screen model does not:"
comm -23 "$out/truth.lines" "$out/screen.lines" | head -20
