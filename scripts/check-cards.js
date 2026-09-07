// A card's invariants, checked against the real file.
//
// All of these are about ONE thing: a card is text you copy off. A path, a
// branch, a wire name, an error. Dragging a card between columns was the
// kanban idea, it did not pan out, and while it existed none of that text
// could be selected: `draggable` on an element means the pointerdown starts a
// drag, so the selection never begins. It was removed on 2026-09-06.
//
// It is the kind of thing that comes back. A board with columns invites a
// drag, `draggable="true"` is one attribute, and the cost is invisible until
// somebody tries to copy a path and gets a ghost image of a card instead. So
// the shape is checked here rather than trusted to memory.
//
// The other half is the opposite mistake: deleting something because it is
// named drag. Dropping FILES onto a terminal is a different target, a
// different feature, and it works. Two rules below exist to fail if a future
// pass at "remove the drag code" takes it.
//
// There is no browser here, so what is checked is the shape that made each bug
// possible. A rule that fails is not style: it is the bug, back.
const fs = require("fs");

const html = fs.readFileSync(process.argv[2], "utf8");
let bad = 0;

function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

// The card's own markup, which is where a `draggable` would be reintroduced.
// Sliced rather than pattern matched: the function is one template literal
// several screens long, and anything anchored on a closing brace stops inside
// it.
const cardAt = html.indexOf("function cardHTML(");
if (cardAt < 0) {
  fail("there is no cardHTML. This check cannot find the card's markup, so it is " +
    "proving nothing. Point it at the function that draws a card.");
}
const card = cardAt < 0 ? "" : html.slice(cardAt, cardAt + 6000);

// Rule 1: a card is not draggable.
//
// This is the bug itself. `draggable` is not a hint that a drag is available,
// it is the browser taking the pointerdown, and a card whose pointerdown is
// taken cannot have its text selected at all.
if (/draggable\s*=\s*["']?true/.test(card)) {
  fail("a card is `draggable` again. That takes the pointerdown, so a path or an " +
    "error printed on the card cannot be selected to be copied, which is the " +
    "cost that got dragging removed in the first place.");
}

// Rule 2: nothing starts a card drag.
//
// The attribute is only half of it. A `dragstart` listener on a card means
// somebody has wired the gesture back up by hand, and the attribute is one
// line away from following.
if (/\.card\b[^]{0,200}addEventListener\(\s*["']dragstart/.test(html) ||
    /ondragstart/.test(card)) {
  fail("something listens for `dragstart` on a card. Cards are not dragged: status " +
    "changes go through the card menu, and the order within a column goes through " +
    "its up and down entries.");
}

// Rule 3: no `user-select: none` on a card.
//
// This is the leftover that keeps the bug after the drag code is gone. A rule
// added to stop text being selected mid-drag outlives the drag, and the
// symptom is identical: a card whose text will not select.
//
// `.card` and not `.cardgroup`, which is why the class name is closed off:
// `.cardgroup > summary` is a heading you click to fold, it holds a project
// name and a count, and nobody copies either. Without the boundary this rule
// fails on correct code and gets deleted for crying wolf.
const noSelect = /\.card(?![\w-])[^{}]*\{[^{}]*user-select\s*:\s*none/.test(html) ||
  /\.card(?![\w-])[^{}]*\{[^{}]*-webkit-user-select\s*:\s*none/.test(html);
if (noSelect) {
  fail("a rule sets `user-select: none` on a card. That is the same bug as the drag " +
    "by another route: the text on a card is a path, a branch and an error, and all " +
    "of it is there to be copied.");
}

// Rule 4: neither button opens the card menu over a selection.
//
// A card opens its menu on `click`, and a click is also what ends a sweep
// across the text on it. Right click matters more: it is the button the
// browser's own Copy lives on, and a card menu that has no copy entry of its
// own, drawn over a highlighted path, takes the clipboard away at the moment
// somebody reached for it. Standing aside means returning BEFORE
// `preventDefault`, which is what lets the native menu through.
if (!/function selectionTouches\(/.test(html)) {
  fail("there is no `selectionTouches`. Both buttons on a card open atrium's menu, and " +
    "both have to stand down while text is highlighted: the left one because a click " +
    "is how a sweep ends, the right one because that is where Copy is.");
} else {
  const menuAt = html.indexOf("async function cardMenu(");
  const menu = menuAt < 0 ? "" : html.slice(menuAt, menuAt + 1400);
  const guardAt = menu.indexOf("selectionTouches(");
  const preventAt = menu.indexOf("e.preventDefault()");
  if (guardAt < 0) {
    fail("`cardMenu` does not consult `selectionTouches`. Selecting a path on a card " +
      "then ends with a menu open over it, and right click cannot reach Copy.");
  } else if (preventAt >= 0 && guardAt > preventAt) {
    fail("`cardMenu` calls preventDefault before it checks the selection. That kills the " +
      "browser's own context menu, which is the one with Copy on it, so a right click " +
      "over a highlighted path offers no way to copy it.");
  }
  if (/e\.type === "click"/.test(menu)) {
    fail("`cardMenu` guards only the left click. Right click over a selection then draws " +
      "atrium's menu instead of the browser's, and atrium's has no copy entry.");
  }
}

// Rule 5: a card group carries a reconciler key.
//
// This is the half that would make the other half worthless. A card's age is
// part of its markup and ticks in seconds under a minute, so the board
// repaints constantly whether or not anything happened. `setHTML` reconciles
// rather than replacing, which is what lets a selection survive that, and it
// rests entirely on `morphKey` finding a key on the row. An element with no
// key is destroyed and rebuilt on every paint, exactly as it was before the
// reconciler existed, and the board looks correct while doing it.
//
// This rule is here rather than in `check-morph.js` because of how it nearly
// broke: `data-status` on a card group was put there for the DROP TARGET, to
// answer "what status is a card dropped here", and removing the drag removed
// it. It had quietly become the group's morph key. Anything that deletes an
// attribute for being drag machinery has to check that first.
const groupAt = html.indexOf("function groupHTML(");
if (groupAt < 0) {
  fail("there is no groupHTML. This check cannot find the card group's markup.");
} else {
  const group = html.slice(groupAt, groupAt + 900);
  if (!/data-status=/.test(group)) {
    fail("a card group has no `data-status`, which is its reconciler key. `morphKey` " +
      "cannot match it across a repaint, so the group is rebuilt on every poll and " +
      "takes the scroll position and any selection inside it along. It reads like drag " +
      "machinery and is not.");
  }
}

// Rule 6: every status a card can be given by hand is still reachable.
//
// Dragging onto a column was the ONLY way to file a card in one, and `done`
// had already been taken off the menu on the strength of it. Removing the drag
// without replacing it left no path at all to `done` from the board, which is
// the state a human declares and no agent ever will.
if (!/function moveItem\(/.test(html)) {
  fail("there is no `moveItem`, so nothing on the card menu files a card into another " +
    "column. `done` is a state only a human declares: with no drag and no menu entry " +
    "there is no way to declare it.");
} else {
  const moveAt = html.indexOf("function moveItem(");
  const move = html.slice(moveAt, moveAt + 1600);
  // Built from COLUMNS, so a column added to the board is offered without
  // anybody remembering to add it here, and `accepts: false` still keeps the
  // report columns out.
  if (!/COLUMNS/.test(move)) {
    fail("`moveItem` does not build its destinations from COLUMNS. A hand-written list " +
      "means a column added to the board is not offered, and that the `accepts: false` " +
      "rule now lives in two places.");
  }
  if (!/accepts !== false/.test(move)) {
    fail("`moveItem` ignores `accepts: false`. `needs input` and `needs permission` are " +
      "reports of what an agent said: filing a card into needs-permission by hand " +
      "claims a request that does not exist.");
  }
}

// Rule 7: the order within a column is still settable.
//
// `rank` is the operator's own order, the column is sorted by it, and dragging
// was the only thing that ever wrote it. A board that sorts by a number
// nothing can set is a column ordered at random by whatever created the cards.
if (!/function nudgeCard\(/.test(html)) {
  fail("nothing writes `rank` any more. The column is ordered by it and dragging used " +
    "to be what set it, so without the menu's up and down the field sorts every " +
    "column and nobody can reach it.");
} else if (!/data-rank=/.test(card)) {
  fail("a card no longer carries `data-rank`. `nudgeCard` computes the midpoint from " +
    "the ranks that are on screen, so without it every move files the card at one end.");
}

// Rule 8: dropping FILES onto a terminal still works.
//
// A different target, a different feature, in daily use. It is named drag, so
// it is what a future pass at "delete the drag code" will take by accident.
// The pane's handlers are the whole feature: without the `dragover` that
// preventDefaults, the browser navigates away to the file instead.
if (!/function wireTerminalDrops\(/.test(html)) {
  fail("`wireTerminalDrops` is gone. Dropping a file onto the terminal pane is a " +
    "different target from a card and it is used: the bytes land in the working " +
    "directory and the path is typed into the runner's prompt.");
} else {
  const dropAt = html.indexOf("function wireTerminalDrops(");
  const drops = html.slice(dropAt, dropAt + 1200);
  for (const ev of ["dragover", "dragleave", "drop"]) {
    if (!new RegExp('addEventListener\\("' + ev + '"').test(drops)) {
      fail(`the terminal pane has no \`${ev}\` handler. All three are the feature: ` +
        `without the dragover that calls preventDefault the browser leaves the board ` +
        `and opens the file.`);
    }
  }
  if (!/dataTransfer\s*&&\s*e\.dataTransfer\.files/.test(drops)) {
    fail("the terminal drop does not read `dataTransfer.files`. That is the payload: a " +
      "handler that ignores it accepts the gesture and uploads nothing.");
  }
}

// Rule 9: the two drags that ARE gestures are still wired.
//
// Resizing the terminal list and moving the skin lab panel are pointer drags
// on a handle, not HTML5 drag and drop, and neither one sits over text
// somebody wants to copy. They are here for the same reason as the rule above:
// they are named drag.
if (!/function startGrip\(/.test(html)) {
  fail("`startGrip` is gone. That is the handle between the terminal list and the " +
    "terminal, which is a pointer drag on a grip rather than a card being dragged, " +
    "and it is how the list gets its width.");
}
if (!/function startLabDrag\(/.test(html)) {
  fail("`startLabDrag` is gone. That is the skin lab's floating panel, which is " +
    "dragged anywhere on purpose and covers no text.");
}

if (bad) {
  console.error(`\n${bad} card invariant(s) broken. Each one is either the drag bug ` +
    `coming back or a working drop target deleted for having drag in its name.`);
  process.exit(1);
}
console.log("the card invariants hold.");
