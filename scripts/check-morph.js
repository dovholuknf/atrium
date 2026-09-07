// The reconciler's preconditions, checked against the real file.
//
// The board used to repaint wholesale: `setHTML` assigned `innerHTML` for a
// whole list, so every node under it was destroyed and rebuilt. A browser has
// nowhere to put the scroll position, the text selection or the focus of an
// element that no longer exists, so the view snapped to the top on every
// event, and on a board with sixteen live agents that is constant.
//
// `setHTML` reconciles now. What that rests on is not visible from reading it,
// which is why these are checked here: rows are matched BY KEY, and a row with
// no key cannot be matched, so it is destroyed and rebuilt exactly the way the
// whole list used to be. A future edit that drops a `data-id` off a row brings
// the bug back for that one list and nothing complains.
//
// There is no browser here, so what is checked is the SHAPE that makes the fix
// possible. Each rule names the failure it prevents.
const fs = require("fs");

const html = fs.readFileSync(process.argv[2], "utf8");
let bad = 0;

function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

// The one script block, so a `data-id` written into a CSS rule or a comment
// elsewhere in the file cannot satisfy a rule below.
const script = html.slice(html.indexOf("\n<script>\n"), html.lastIndexOf("\n</script>\n"));

// Rule 1: `setHTML` reconciles, it does not swap.
//
// This is the whole fix in one line. Putting `innerHTML =` back on the target
// element restores the original bug for EVERY list on the board at once, since
// all of them paint through here.
const setHTML = script.slice(script.indexOf("function setHTML(el, html) {"),
  script.indexOf("function morphChildren("));
if (!setHTML) {
  fail("setHTML or morphChildren is gone. The board paints through setHTML and " +
    "setHTML reconciles through morphChildren.");
} else {
  if (!/morphChildren\(el, shadow\)/.test(setHTML)) {
    fail("setHTML does not reconcile against a parsed copy. If it assigns innerHTML " +
      "on the target, every scroll position, selection and focus under it dies on " +
      "every event.");
  }
  if (/\bel\.innerHTML\s*=/.test(setHTML)) {
    fail("setHTML assigns el.innerHTML. That destroys every node under the element, " +
      "which is the bug this function exists to not have.");
  }
}

// Rule 2: the reconciler compares before it writes.
//
// TIER TWO, and it is the half that is easy to drop. Matching rows by key
// keeps the container alive and the scroll with it, and then writing every
// matched row unconditionally destroys the selection anyway, because the age
// on a card changes every second. So the row being rewritten is the row being
// read, and the fix looks complete and is not.
const morphNode = script.slice(script.indexOf("function morphNode(mine, want) {"),
  script.indexOf("function morphAttrs("));
if (!/mine\.nodeValue === want\.nodeValue\) return false/.test(morphNode)) {
  fail("morphNode writes text without comparing it first. Ages tick every second, " +
    "so an unconditional write kills the selection on the card being read.");
}
if (!/mine\.getAttribute\(a\.name\) === a\.value\) continue/.test(script)) {
  fail("morphAttrs writes attributes without comparing them first. Setting `class` " +
    "to the value it already has restarts the element's CSS transitions, which is " +
    "the flicker.");
}

// Rule 3: every row that gets reconciled carries a key.
//
// Each of these is one list that silently falls back to being destroyed and
// rebuilt if its key goes. Checked by looking at the function that draws the
// row, because that is where the attribute would be deleted from.
const keyed = [
  ["cardHTML", "a board card"],
  ["stackRow", "a stack row"],
  ["renderTermList", "a terminal switcher row"],
];
for (const [fn, what] of keyed) {
  const at = script.indexOf("function " + fn + "(");
  const start = at >= 0 ? at : script.indexOf(fn + "(");
  if (start < 0) { fail(`${fn} is gone, so ${what} cannot be checked.`); continue; }
  const body = script.slice(start, start + 4000);
  if (!/data-id="\$\{t\.id\}"/.test(body)) {
    fail(`${what} does not carry data-id, so it cannot be matched across a repaint ` +
      `and is destroyed and rebuilt on every event. See morphKey.`);
  }
}

// Rule 4: nothing wires listeners onto board nodes after a paint.
//
// A card that was already on the board is the SAME ELEMENT after a repaint,
// with the listeners it was given the first time still on it. So a wiring pass
// that runs after every render adds a second copy of every handler each time,
// and a handler running twice acts twice.
//
// This rule used to name `wireDragging`, which was that pass and which carried
// a `__dragWired` flag per node to survive the reconciler. Dragging a card is
// gone, and so is the pass: every handler a card has is now an attribute in
// its own markup, which comes back with the card and cannot double.
//
// The rule is kept, aimed at the shape rather than at the function, because
// the hazard is a property of the reconciler and not of dragging. The next
// person to reach for a post-render wiring pass gets told here. A per-node
// flag or a `dataset` marker is what makes one safe, so either satisfies this.
const wiring = /querySelectorAll\(\s*["'`]#board[^)]*\)\s*\.forEach\([^)]*=>\s*\{[^]*?addEventListener/;
if (wiring.test(script) && !/__\w*Wired|dataset\.\w*wired/i.test(script)) {
  fail("something walks #board after a render and calls addEventListener with no " +
    "per-node guard. The board is reconciled, so a card that was already there comes " +
    "back with its listeners still attached and gets a second copy of every one. Mark " +
    "the node, or put the handler in the markup where it belongs.");
}

if (bad) {
  console.error(`${bad} reconciler invariant(s) broken.`);
  process.exit(1);
}
console.log("the reconciler's preconditions hold.");
