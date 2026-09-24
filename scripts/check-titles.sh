#!/usr/bin/env bash
# No native `title` tooltips on the board.
#
# A `title` draws the browser's own box: white, in the system font, whatever
# skin the board is wearing. The board has one styled tooltip instead, `#tip`,
# and anything carrying a `data-tip` gets it (js/tooltips.js). This fails on a
# `title=` attribute, a `.title =` assignment, or a setAttribute("title") under
# internal/api/web, unless the line is on the allowlist.
#
# An icon-only control still needs a name a screen reader can read, which a
# title used to give it for free. Give it an `aria-label` next to the
# `data-tip`, not a title.
#
# The allowlist is scripts/title-allowlist.txt, one `file:pattern` per line,
# where file is relative to internal/api/web and pattern is an extended regex
# matched against the offending line. Say why after a `#`: an entry nobody can
# explain is an entry nobody can remove.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
web="$here/internal/api/web"
allow="$here/scripts/title-allowlist.txt"

# `document.title` is the window's name, not a tooltip, so it never counts.
hits=$(cd "$web" && grep -rnaE \
  "(^|[[:space:]\`'\"])title=|\.title[[:space:]]*=[^=]|setAttribute\(['\"]title['\"]" \
  --include='*.html' --include='*.js' . \
  | grep -avE '(^|[^.[:alnum:]_])document\.title[[:space:]]*=' || true)

fail=0
while IFS= read -r hit; do
  [ -z "$hit" ] && continue
  file="${hit%%:*}"
  file="${file#./}"
  rest="${hit#*:}"
  text="${rest#*:}"
  ok=0
  if [ -f "$allow" ]; then
    while IFS= read -r entry; do
      entry="${entry%%#*}"
      entry="$(printf '%s' "$entry" | sed 's/[[:space:]]*$//')"
      [ -z "$entry" ] && continue
      afile="${entry%%:*}"
      apat="${entry#*:}"
      if [ "$afile" = "$file" ] && printf '%s' "$text" | grep -qaE -- "$apat"; then
        ok=1
        break
      fi
    done < "$allow"
  fi
  if [ "$ok" = "0" ]; then
    if [ "$fail" = "0" ]; then
      echo "a native title tooltip, which ignores the skin. use data-tip, and an" >&2
      echo "aria-label as well when the control has no text of its own:" >&2
    fi
    echo "  $file:${rest%%:*}: $(printf '%s' "$text" | sed 's/^[[:space:]]*//' | cut -c1-140)" >&2
    fail=1
  fi
done <<< "$hits"

if [ "$fail" != "0" ]; then
  echo "or, if the browser's own tooltip really is wanted, add the line to" >&2
  echo "scripts/title-allowlist.txt with the reason." >&2
  exit 1
fi
echo "no native title tooltips on the board."
