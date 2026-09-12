#!/usr/bin/env bash
# Syntax-check the board's JavaScript, and everything else about the board that
# no compiler is going to notice.
#
# The board is an HTML file, a stylesheet, and about two dozen plain scripts
# loaded in order, and nothing parses any of it until a browser does. A syntax
# error therefore shipped once as a blank dashboard with one line in a console
# nobody had open, and the error pointed at whatever token came after the
# mistake rather than at the mistake.
#
# The specific way it happened is worth knowing, because it will happen again:
# an HTML comment written INSIDE a JavaScript template literal, containing a
# backtick. The comment is not a comment to the JavaScript parser, so the
# backtick ended the string and the rest of the file parsed as nonsense.
#
# THE SCRIPTS SHARE ONE GLOBAL SCOPE. They are classic scripts on one page, not
# modules, so a function declared in one is callable from all of them and the
# load order in the head is the order the code runs in. Two things follow, and
# both are checked below: the order matters, and the right thing to parse is
# the CONCATENATION rather than any one file.
#
# Run by hand, and by CI, which does nothing but check out and call this.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
web="$here/internal/api/web"
page="$web/index.html"
css="$web/board.css"
sw="$web/sw.js"

if ! command -v node >/dev/null 2>&1; then
  echo "node is not on PATH, so the board's script cannot be parsed." >&2
  echo "this check is skipped rather than failed: it is a lint, not a build step." >&2
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# The load order, read off the page. This is the new invariant: the page used
# to assert "exactly one script block", and what replaces it is that the head
# names the js files, in an order, and that every file on disk is named exactly
# once. A file added to `js/` and not referenced is dead code that looks live.
# A file referenced twice runs twice, which for this code means every listener
# bound twice and every keystroke sent twice.
grep -o 'src="/js/[A-Za-z0-9_-]*\.js"' "$page" | sed 's|src="/js/||; s|"||' > "$tmp/order.txt"

if [ ! -s "$tmp/order.txt" ]; then
  echo "index.html references no scripts under /js/. the check itself is broken," >&2
  echo "or the board was put back into one file without telling this script." >&2
  exit 1
fi

dupes=$(sort "$tmp/order.txt" | uniq -d)
if [ -n "$dupes" ]; then
  echo "these scripts are referenced more than once, so they would run twice:" >&2
  echo "$dupes" >&2
  exit 1
fi

( cd "$web/js" && ls *.js ) | sort > "$tmp/ondisk.txt"
sort "$tmp/order.txt" > "$tmp/named.txt"
if ! diff -u "$tmp/ondisk.txt" "$tmp/named.txt" > "$tmp/order.diff"; then
  echo "the files in internal/api/web/js do not match what index.html loads." >&2
  echo "a file on disk and not in the head never runs. one in the head and not" >&2
  echo "on disk is a 404 and everything after it still runs, which is worse." >&2
  sed -n '1,40p' "$tmp/order.diff" >&2
  exit 1
fi

# The whole script, in load order, and the whole page with it inlined. Both
# come from `board-source.js`, which the node checkers use too, so there is one
# answer to "what does the browser end up with" rather than two that drift.
node "$here/scripts/board-source.js" --script > "$tmp/board.js"
node "$here/scripts/board-source.js" > "$tmp/page.html"

if [ ! -s "$tmp/board.js" ]; then
  echo "the concatenated board script is empty. the check itself is broken." >&2
  exit 1
fi
if ! grep -q '^<script>$' "$tmp/page.html"; then
  echo "could not rebuild the one-file page for the checkers." >&2
  echo "the head's script tags are not in the shape board-source.js expects." >&2
  exit 1
fi
whole="$tmp/page.html"

fail=0
# --check parses without running, which is what is wanted: this code expects a
# browser and would not survive being executed here.
if ! node --check "$tmp/board.js"; then
  echo "the board's script does not parse. see the line above." >&2
  echo "line numbers are relative to the CONCATENATION of the files under" >&2
  echo "internal/api/web/js, in the order index.html loads them:" >&2
  awk '{ printf "  %s\n", $0 }' "$tmp/order.txt" >&2
  fail=1
fi

# And each file on its own, so the error names a file rather than an offset
# into 17,000 lines. A file can fail here and the concatenation still parse,
# since a block left open in one file is closed by the next, and that is worth
# reporting: it means a seam is in the wrong place.
while read -r f; do
  if ! node --check "$web/js/$f" 2>"$tmp/one.err"; then
    echo "js/$f does not parse on its own:" >&2
    cat "$tmp/one.err" >&2
    fail=1
  fi
done < "$tmp/order.txt"

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
if ! node "$here/scripts/check-settings-panes.js" "$whole"; then
  echo "the settings dialog would not partition into panes. see above." >&2
  fail=1
fi

# The runners page is cut the same way, by the same function, with the same
# hazard. Checked separately because its headings are labelled explicitly and
# its lists are named, and neither of those is true of the dialog.
if ! node "$here/scripts/check-runner-panes.js" "$whole"; then
  echo "the runners page would not partition into panes. see above." >&2
  fail=1
fi

# The terminal pane's invariants. Every one of these is a bug that has already
# been hit, and all of them are invisible until somebody types: a keystroke
# arriving twice, a paste arriving as one Enter per line, output from a socket
# that should have been closed. None of it is reachable from a parser.
if ! node "$here/scripts/check-terminal.js" "$whole"; then
  echo "a terminal invariant is broken. see above." >&2
  fail=1
fi

# The reconciler. Every list on the board paints through one function that no
# longer replaces what it draws into, and what that rests on is a key on every
# row. A row that loses its key is destroyed and rebuilt like it always was,
# and nothing says so: the board looks right and the scroll goes to the top.
if ! node "$here/scripts/check-morph.js" "$whole"; then
  echo "the board would throw away where you were. see above." >&2
  fail=1
fi

# And the reconciler RUN, against a few hundred lines of DOM implemented in the
# test. What it asserts is not that the board draws: it is that a row nobody
# changed comes out the same object, never written to, because that object is
# what holds the scroll and the selection. A reconciler that rebuilds a row it
# could have kept looks identical on screen and has the original bug.
if ! node "$here/scripts/test-morph.js"; then
  echo "the reconciler does not keep what did not change. see above." >&2
  fail=1
fi

# The terminal strip's grouping, RUN against a list of sessions. What it
# asserts is the nesting that comes out of the renderer rather than the tree
# that goes into it, because a correct tree flattened wrongly is exactly how a
# single member org ended up drawn inside the group above it, wearing the rest
# of its own path on the row.
if ! node "$here/scripts/test-term-nesting.js"; then
  echo "the terminal strip nests rows under the wrong headings. see above." >&2
  fail=1
fi

# The stack's and the strip's sort order, RUN against cards that tie. A sort
# that leaves ties to whatever order the poll handed it is right in every
# screenshot and still moves rows on a repaint that changed nothing, which reads
# as the board losing your place rather than as a sort. On the strip it moves a
# tab out from under a cursor already on its way to it.
if ! node "$here/scripts/test-sort-order.js"; then
  echo "the stack or the strip does not sort the same way twice. see above." >&2
  fail=1
fi

# The card's invariants. All of them are about one thing: the text on a card is
# there to be copied. Dragging a card between columns made that impossible for
# as long as it existed, and the same two attributes would do it again.
#
# The other half of that file guards the drop targets that are NOT cards, since
# they are what a future pass at "remove the drag code" takes by accident.
if ! node "$here/scripts/check-cards.js" "$whole"; then
  echo "a card invariant is broken. see above." >&2
  fail=1
fi

# Which words in the terminal get offered to `files/probe`, run against real
# lines of output. The endpoint decides what is a file, so the board's own job
# is the trim and a very small refusal, and both fail quietly: a path with a
# comma stuck to it is simply never a link, and there is nothing on screen that
# says why.
if ! node "$here/scripts/check-path-tokens.js" "$whole"; then
  echo "the terminal's path candidates are wrong. see above." >&2
  fail=1
fi

# The switcher: one keystroke, and a popped-out window that can change which
# card it is. Both halves fail silently. A browser that ignores preventDefault
# says nothing, and a claim that is not released heals itself in fifteen
# seconds, so the symptom is a flicker rather than an error.
if ! node "$here/scripts/check-switcher.js" "$whole"; then
  echo "a switcher invariant is broken. see above." >&2
  fail=1
fi

# The permissions queue at phone width. Same reasoning as the terminal: the
# markup stays valid and the script keeps running with every one of these
# broken, and what breaks instead is which button a thumb lands on. Nobody
# narrows a window to 390 pixels on the way past, so it is checked here.
if ! node "$here/scripts/check-phone.js" "$whole" "$sw"; then
  echo "a phone invariant is broken. see above." >&2
  fail=1
fi

if [ "$fail" != "0" ]; then
  exit 1
fi

echo "the board parses."
