// The permissions queue at phone width, checked against the real file.
//
// Backlog item 11 is answering an approval from a phone. The transport for it
// already existed: the board is reachable over an overlay and there is a login
// in front of it. What was missing was that the screen was unusable at 390
// pixels, and every rule below is one of the ways it was unusable, found by
// narrowing a window onto a queue with three frozen agents in it.
//
// None of this is reachable from a parser. The markup stays valid and the
// script keeps running with every one of these broken. What breaks is which
// button your thumb lands on, which is not a thing a test suite normally gets
// to see, so what is checked here is the SHAPE that makes each failure
// possible.
//
// A rule that fails is not style. It is the bug, back.
const fs = require("fs");

const html = fs.readFileSync(process.argv[2], "utf8");
let bad = 0;

function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

// The phone block, as text, so the rules below can ask what is in it.
const phoneAt = html.indexOf("@media (max-width: 480px)");
const phone = phoneAt < 0 ? "" : html.slice(phoneAt, html.indexOf("\n  }", phoneAt));
if (!phone) {
  fail("there is no `@media (max-width: 480px)` block. The phone layout is the " +
    "whole of backlog item 11 and it lives in one block.");
}

// Rule 1: THE QUESTION ABOVE THE ANSWER.
//
// The 900px block sets `.row.perm .actions { order: 1 }` and `.who { order: 2 }`,
// which is right for a popped-out window: one card, one screen, and the buttons
// are what you came for. Inherited by a phone it puts `approve once` directly
// under the header with the command it would approve below the fold, so the
// first thing reachable by thumb is a decision about something you have not
// read. The phone block has to put them back.
if (!/\.row\.perm \.who \{[^}]*order:\s*1/.test(phone) ||
    !/\.row\.perm \.actions \{[^}]*order:\s*2/.test(phone)) {
  fail("the phone block does not put `.row.perm .who` at order 1 and `.actions` at " +
    "order 2. Without both, the 900px block's order wins and the four decision " +
    "buttons sit ABOVE the command they decide on.");
}

// Rule 2: A THUMB NEEDS 44 PIXELS.
//
// Padding alone did not get there. The four decision buttons measured 42 and
// the scope chips 37, and these are the controls where pressing the wrong one
// approves something you meant to block.
for (const sel of [".row.perm .actions button", ".row.perm .hints button"]) {
  const re = new RegExp(sel.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "\\s*\\{[^}]*min-height:\\s*44px");
  if (!re.test(phone)) {
    fail(`\`${sel}\` has no \`min-height: 44px\` in the phone block. It came out at ` +
      `42 and 37 pixels on padding alone.`);
  }
}

// Rule 3: THE COMMAND BOX IS SIZED WHERE IT IS DRAWN.
//
// `autosize` sets height from `scrollHeight`, and a DETACHED element reports
// zero. `permCard` builds a detached card, so autosizing there wrote
// `height: 2px` inline and left it: every command clipped to whatever
// `min-height` gave it, forever, with the inline height beating any rule that
// would have grown it. A six line command showed two.
//
// It cost nothing on a desktop, where two lines is most commands, and made the
// phone unusable, where two lines is none of them.
const cardAt = html.indexOf("function permCard(");
const cardEnd = html.indexOf("\nasync function renderPerms", cardAt);
const card = cardAt < 0 ? "" : html.slice(cardAt, cardEnd);
if (/^\s*autosize\(cmd\);\s*$/m.test(card)) {
  fail("permCard autosizes the command box itself. The card is still detached " +
    "there, so scrollHeight is 0 and it writes `height: 2px` inline, which " +
    "nothing later overrides. Size it from `sizeCommands` once it is in the " +
    "document.");
}
if (!/function sizeCommands\(\)/.test(html)) {
  fail("there is no `sizeCommands`. Something has to grow the command boxes once " +
    "they are in the document, or every command is clipped to two lines.");
}
// Three callers, and each answers a different way of being the wrong size: a
// card that was just added, a card added while the view was hidden (hidden
// measures as zero exactly like detached), and a window that changed width
// (rotating a phone rewraps every command, and crossing 480 changes the font
// size and rewraps them again).
// Anchored on the DECLARATION, not the name: `switchView` and `renderPerms`
// are both called from a dozen places, and searching from the first mention
// measures the distance from some unrelated call site.
for (const [caller, why] of [
  ["async function renderPerms(", "a newly added card is never measured"],
  ["function switchView(", "a card built while the perms view was hidden stays " +
    "clipped, because hidden measures as zero exactly like detached"],
  ["addEventListener(\"resize\"", "rotating a phone rewraps every command and " +
    "nothing resizes the box"]
]) {
  const from = html.indexOf(caller);
  const to = from < 0 ? -1 : html.indexOf("sizeCommands", from);
  if (from < 0 || to < 0 || to - from > 2000) {
    fail(`\`sizeCommands\` is not called from \`${caller}\`, so ${why}.`);
  }
}

// Rule 4: THE ANNOUNCEMENTS DO NOT BURY THE QUEUE.
//
// Measured, not guessed: at 390 pixels a toast carrying a command wrapped to
// five lines and stood 194px tall, so the cap of three was 582 of an 844 pixel
// screen, drawn on top of the permissions queue. The queue is the only reason
// the board is open on a phone at all.
if (!/const cap = innerWidth <= PHONE \? 1 : 3/.test(html)) {
  fail("the toast stack is not capped to one at phone width. Three toasts at 390 " +
    "pixels cover 69% of the screen, and what they cover is the queue they are " +
    "telling you about.");
}
if (!/-webkit-line-clamp/.test(phone)) {
  fail("the phone block does not clamp the toast body. A command wraps to five " +
    "lines at this width, which is what made one toast 194px tall.");
}

// Rule 5: ONE PHONE WIDTH, DECLARED TWICE, AND THE TWO AGREE.
//
// A media query cannot read a constant and `matchMedia` would put the literal
// back on the script side anyway, so the number exists in both places. Two of
// them apart is not a crash: it is a toast cap applying at a width where the
// layout it was written for has not started, which looks like nothing at all
// until somebody counts toasts on a phone.
const constAt = /const PHONE = (\d+);/.exec(html);
if (!constAt) {
  fail("there is no `const PHONE = <n>;` in the board's script.");
} else if (constAt[1] !== "480") {
  fail(`\`PHONE\` is ${constAt[1]} and the media query is 480. They have to be the ` +
    `same number, or the script's phone rules apply at a width where the CSS ` +
    `phone rules do not.`);
}

// Rule 6: A TOAST DOES NOT TALK OVER THE ROW IT IS ABOUT.
//
// `notify` already refuses to raise an operating system notification when you
// can see the board, on the grounds that the toast has already told you. The
// same argument one level down: if the request's own row is on screen, with
// its command and its four buttons, a floating copy of the first 120
// characters is a panel drawn over the answer.
//
// Three-valued on purpose. A missing row is not an absent row: the alerting
// pass and the repaint are two separate requests, so on the first poll after a
// reload there is no list yet, and reading that as "not on screen" toasted the
// card you were looking at on every reload.
if (!/function permShowing\(id\)/.test(html)) {
  fail("there is no `permShowing`. Without it the nag toasts a request whose own " +
    "row is already on screen.");
}
if (!/return "unknown"/.test(html) || !/if \(showing === "unknown"\) return;/.test(html)) {
  fail("`permShowing` is not three-valued, or nag does not skip the undecidable " +
    "tick. On the first poll after a reload the list has not rendered, and " +
    "answering `off-screen` there puts a toast over the card you are reading.");
}
// The slot has to be claimed AFTER the question is asked. Claiming it first and
// then bailing spends the minute's one alert on a tick that raised nothing.
const nagAt = html.indexOf("function nag(perms)");
const nagBody = nagAt < 0 ? "" : html.slice(nagAt, nagAt + 3000);
if (nagBody.indexOf("permShowing(p.id)") > nagBody.indexOf("nagged[p.id] = slot")) {
  fail("nag claims the slot before asking `permShowing`. An undecidable tick then " +
    "spends the minute's one alert on nothing at all.");
}

// Rule 7: THE SERVICE WORKER'S LAST RESORT EXISTS.
//
// A notification button is how this gets answered from a pocket, and the one
// thing that can go wrong with it is answering a request somebody already
// answered. That path raises a "too late" notification and then retires it by
// tag, through a function that was never written: pressing approve on a stale
// notification threw a ReferenceError inside a worker and put nothing at all
// on screen, which is the same silence the message exists to prevent.
const sw = fs.readFileSync(process.argv[3], "utf8");
for (const name of (sw.match(/\bawait (\w+)\(/g) || []).map(m => m.slice(6, -1))) {
  if (["sleep", "fetch"].includes(name)) continue;
  if (!new RegExp(`function ${name}\\b`).test(sw)) {
    fail(`sw.js awaits \`${name}()\` and never defines it. In a service worker ` +
      `that is a ReferenceError nobody sees, on a path that only runs when ` +
      `something has already gone wrong.`);
  }
}

if (bad) {
  console.error(`\n${bad} phone invariant${bad === 1 ? "" : "s"} broken.`);
  process.exit(1);
}
console.log("the phone invariants hold.");
