#!/usr/bin/env bash
# Every skin sets the same variables, and every one of them exists in :root.
#
# WHY THIS IS A SCRIPT AND NOT A LOOK. A skin that forgets one variable does
# not fail. It falls through to `:root`, which is the DEFAULT skin, so the
# board comes up with one element still wearing the navy palette. On a board
# this size that is a single chip somewhere, and nobody finds it for a month.
#
# The first skin block in the file is the reference. Adding a variable to the
# palette therefore fails here until all ten carry it, which is the point:
# the expensive mistake is the tenth one being forgotten, not the first.

set -euo pipefail

board="$(dirname "$0")/../internal/api/web/index.html"
[ -f "$board" ] || { echo "no board at $board" >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Every `body[data-skin="x"] { ... }` block, flattened to `skin var` pairs.
awk '
  /^  :root\[data-skin=/ {
    match($0, /"[^"]+"/)
    skin = substr($0, RSTART + 1, RLENGTH - 2)
    inblock = 1
    next
  }
  inblock && /^  }/ { inblock = 0; next }
  inblock {
    n = split($0, parts, ";")
    for (i = 1; i <= n; i++) {
      if (match(parts[i], /--[a-z0-9-]+[ ]*:/)) {
        v = substr(parts[i], RSTART, RLENGTH)
        sub(/[ ]*:$/, "", v)
        print skin, v
      }
    }
  }
' "$board" | sort -u > "$work/pairs"

# THE SKIN AND THE DERIVATIONS MUST BE ON THE SAME ELEMENT.
#
# The palette derives its solids from its triples, and a custom property is
# resolved against the element it is DECLARED on. Declaring the derivations on
# `:root` and the overrides on `body` therefore leaves every derived colour at
# the default while every plain hex changes. That does not look like a bug: it
# looks like a skin that is nearly right, with blue borders and blue pills on a
# correct background.
#
# Nothing above catches it, because every skin still declares every variable.
if grep -q '^  body\[data-skin=' "$board"; then
  echo "a skin is declared on \`body\`. it has to be on \`:root\`, beside the" >&2
  echo "derivations, or every derived colour stays at the default." >&2
  exit 1
fi

skins=$(cut -d' ' -f1 "$work/pairs" | sort -u)
count=$(echo "$skins" | wc -l | tr -d ' ')

if [ "$count" -lt 2 ]; then
  echo "found $count skins. expected the whole set." >&2
  exit 1
fi

reference=$(echo "$skins" | head -1)
grep "^$reference " "$work/pairs" | cut -d' ' -f2 | sort > "$work/reference"
echo "reference skin: $reference ($(wc -l < "$work/reference" | tr -d ' ') variables)"

fail=0
for skin in $skins; do
  grep "^$skin " "$work/pairs" | cut -d' ' -f2 | sort > "$work/this"
  missing=$(comm -23 "$work/reference" "$work/this")
  extra=$(comm -13 "$work/reference" "$work/this")
  if [ -n "$missing" ]; then
    echo "  $skin is MISSING: $(echo "$missing" | tr '\n' ' ')" >&2
    fail=1
  fi
  if [ -n "$extra" ]; then
    echo "  $skin sets what no other skin sets: $(echo "$extra" | tr '\n' ' ')" >&2
    fail=1
  fi
  [ -n "$missing$extra" ] || echo "  ok   $skin"
done

# A skin variable with no default is one the default board renders wrong.
root=$(awk '/^  :root \{/ { inroot = 1; next } inroot && /^  \}/ { inroot = 0 } inroot' "$board")
while read -r v; do
  echo "$root" | grep -q -- "$v[ ]*:" || { echo "  $v is set by skins but has no :root default" >&2; fail=1; }
done < "$work/reference"

# The daemon holds the list the picker is built from and the validator refuses
# against. A skin in the stylesheet and not in that list cannot be chosen; one
# in the list and not in the stylesheet saves cleanly and changes nothing.
go="$(dirname "$0")/../internal/api/skins.go"
if [ -f "$go" ]; then
  awk '/^var Skins = \[\]string\{/ { inlist = 1; next }
       inlist && /^\}/ { inlist = 0 }
       inlist { if (match($0, /"[^"]+"/)) print substr($0, RSTART + 1, RLENGTH - 2) }
      ' "$go" | sort > "$work/golist"
  # The default lives in `:root` rather than in a block of its own, so it is
  # named in Go and has no `body[data-skin=]` rule. Expected, and dropped here
  # rather than being a special case in the stylesheet.
  default=$(grep -oE 'DefaultSkin = "[^"]+"' "$go" | head -1 | cut -d'"' -f2)
  grep -v "^$default$" "$work/golist" > "$work/golist-css" || true
  echo "$skins" | sort > "$work/csslist"
  onlygo=$(comm -23 "$work/golist-css" "$work/csslist")
  onlycss=$(comm -13 "$work/golist-css" "$work/csslist")
  if [ -n "$onlygo" ]; then
    echo "  in skins.go with no rule in the stylesheet: $(echo "$onlygo" | tr '\n' ' ')" >&2
    fail=1
  fi
  if [ -n "$onlycss" ]; then
    echo "  in the stylesheet but not offered by skins.go: $(echo "$onlycss" | tr '\n' ' ')" >&2
    fail=1
  fi
  [ -n "$onlygo$onlycss" ] || echo "  ok   skins.go offers every one of them, and no others"
fi

[ "$fail" -eq 0 ] || exit 1
echo "all $count skins agree, and every variable has a default."
