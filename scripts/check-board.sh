#!/usr/bin/env bash
# Syntax-check the board's JavaScript.
#
# The board is one HTML file with one large script block in it, and nothing
# parsed that script until a browser did. A syntax error therefore shipped as a
# blank dashboard with one line in a console nobody had open, and the error
# pointed at whatever token came after the mistake rather than at the mistake.
#
# The specific way it happened is worth knowing, because it will happen again:
# an HTML comment written INSIDE a JavaScript template literal, containing a
# backtick. The comment is not a comment to the JavaScript parser, so the
# backtick ended the string and the rest of the file parsed as nonsense.
#
# Run by hand, and by CI, which does nothing but check out and call this.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
page="$here/internal/api/web/index.html"
sw="$here/internal/api/web/sw.js"

if ! command -v node >/dev/null 2>&1; then
  echo "node is not on PATH, so the board's script cannot be parsed." >&2
  echo "this check is skipped rather than failed: it is a lint, not a build step." >&2
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Everything between the first <script> and the matching </script>. There is
# exactly one such block, which awk asserts rather than assumes: a second one
# would mean this check silently covers only the first.
blocks=$(grep -c '^<script>$' "$page" || true)
if [ "$blocks" != "1" ]; then
  echo "expected exactly one script block in index.html, found $blocks." >&2
  echo "this check extracts one. teach it about the others before adding them." >&2
  exit 1
fi

awk '/^<script>$/{on=1;next} /^<\/script>$/{on=0} on' "$page" > "$tmp/board.js"

if [ ! -s "$tmp/board.js" ]; then
  echo "extracted no javascript from index.html. the check itself is broken." >&2
  exit 1
fi

fail=0
# --check parses without running, which is what is wanted: this code expects a
# browser and would not survive being executed here.
if ! node --check "$tmp/board.js"; then
  echo "the board's script does not parse. see the line above." >&2
  echo "line numbers are relative to the script block, which starts at line" \
       "$(grep -n '^<script>$' "$page" | cut -d: -f1) of index.html." >&2
  fail=1
fi

if ! node --check "$sw"; then
  echo "sw.js does not parse." >&2
  fail=1
fi

# The settings dialog is one flow of fields, and its left-hand nav is built at
# runtime by cutting that flow at every `h3.s-section` that is a DIRECT child
# of the dialog body. Nesting one inside a field, or moving a control out of
# the body, breaks the dialog into one giant pane or none.
#
# Parsing cannot catch that: the markup is still valid and the script still
# runs. So it is checked here, against the real file, rather than discovered by
# opening the gear.
if ! node "$here/scripts/check-settings-panes.js" "$page"; then
  echo "the settings dialog would not partition into panes. see above." >&2
  fail=1
fi

if [ "$fail" != "0" ]; then
  exit 1
fi

echo "the board parses."
