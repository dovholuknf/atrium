#!/usr/bin/env bash
#
# Prove a scrollback change does what it claims, against what the operator
# actually saw.
#
# The problem this answers: "it is fixed" has been said about this twice and
# been wrong twice, both times on the strength of tests that agreed with the
# change because they were written alongside it. A number that only compares a
# renderer to itself cannot catch that.
#
# So this compares against two things neither renderer has any say in:
#
#   1. A CAPTURE OF THE PANE, taken with ctrl-a ctrl-c while the operator was
#      looking at it. That is ground truth for what the running build produces.
#   2. THE RUNNING BUILD ITSELF, which is whatever `git` has at HEAD.
#
# The proof is in two steps and the FIRST ONE IS THE IMPORTANT ONE:
#
#   - Render the ring with HEAD's renderer. If that reproduces the capture,
#     the harness models reality, and the second step means something.
#   - Render it with the working tree's renderer and show the difference.
#
# If step one does NOT reproduce the capture, stop. The harness is measuring
# something other than what the operator sees and any improvement it reports is
# unearned. That is the failure mode worth having a script for.
#
#   bash scripts/replay-proof.sh <raw.bin> <what-the-pane-showed.txt> <cols> <rows>

set -u
cd "$(dirname "$0")/.."

raw=${1:?usage: replay-proof.sh <raw.bin> <pane-capture.txt> <cols> <rows>}
saw=${2:?}
cols=${3:?}
rows=${4:?}
out=$(dirname "$raw")

strip() { sed -e 's/\x1b\[[0-9;?]*[a-zA-Z]//g' -e 's/\x1b\][^\x07]*\x07//g' -e 's/\r//g' "$1"; }

render() {
  ATRIUM_DUMP_IN="$raw" ATRIUM_DUMP_OUT="$out" \
    ATRIUM_DUMP_COLS="$cols" ATRIUM_DUMP_ROWS="$rows" \
    go test ./internal/daemon/ -run TestDumpBothReplayPaths -count=1 > /dev/null 2>&1
}

# HEAD's renderer, swapped in and put back. The working tree is restored on any
# exit, including a failed build, because leaving somebody's screen.go replaced
# by an older copy is a worse outcome than any answer this prints.
keep=$(mktemp)
cp internal/daemon/screen.go "$keep"
restore() { cp "$keep" internal/daemon/screen.go; rm -f "$keep"; }
trap restore EXIT

echo "== step 1: does HEAD's renderer reproduce what the pane showed? =="
git show HEAD:internal/daemon/screen.go > internal/daemon/screen.go
render
cp "$out/screen.txt" "$out/head.txt"
restore
trap - EXIT

echo "== step 2: the working tree =="
render
cp "$out/screen.txt" "$out/tree.txt"

# Compared on the lines, not the bytes. The capture carries the live tail and a
# width note that the replay alone does not, so byte equality is the wrong bar.
# What has to match is the text.
strip "$saw" | sed 's/[[:space:]]*$//' | sort -u > /tmp/saw.lines
for f in head tree; do
  strip "$out/$f.txt" | sed 's/[[:space:]]*$//' | sort -u > /tmp/$f.lines
done

n_saw=$(wc -l < /tmp/saw.lines)
echo
printf 'the pane showed %d distinct lines\n\n' "$n_saw"
for f in head tree; do
  same=$(comm -12 /tmp/saw.lines /tmp/$f.lines | wc -l)
  blank=$(awk '{ if ($0 ~ /^[[:space:]]*\r?$/) b++ } END { print b+0 }' "$out/$f.txt")
  tot=$(wc -l < "$out/$f.txt")
  awk -v n="$f" -v s="$same" -v t="$n_saw" -v b="$blank" -v l="$tot" \
    'BEGIN { printf "%-6s matches %4d of %4d pane lines (%5.1f%%)   %5d lines, %4d blank (%.0f%%)\n",
             n, s, t, t?100*s/t:0, l, b, l?100*b/l:0 }'
done

sawblank=$(awk '{ if ($0 ~ /^[[:space:]]*\r?$/) b++ } END { print b+0 }' "$saw")
sawtot=$(wc -l < "$saw")
awk -v b="$sawblank" -v l="$sawtot" \
  'BEGIN { printf "%-6s %45s   %5d lines, %4d blank (%.0f%%)\n", "pane", "", l, b, 100*b/l }'
