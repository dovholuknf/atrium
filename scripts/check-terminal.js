// The terminal pane's invariants, checked against the real file.
//
// These exist because the same bug came back three times in one evening and it
// is invisible until you type: every keystroke arriving twice, as `eexxiitt`,
// which reads like a broken keyboard rather than like a leaked listener. There
// is no browser here to test it in, so what is checked is the SHAPE that made
// it possible.
//
// Each rule below names the failure it prevents. A rule that fails is not
// style: it is the bug, back.
const fs = require("fs");

const html = fs.readFileSync(process.argv[2], "utf8");
let bad = 0;

function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

// Rule 1: ONE keystroke listener, and it is disposable.
//
// `term.onData` registers a listener and returns a disposable. `connectTerm`
// runs again on every reconnect and the terminal object survives a reconnect,
// so a registration that is not disposed first leaves the old listener behind
// and every keypress is sent once per listener. Every reconnect path hits it:
// the restart retry, the popped-out window's wait, and falling back to the
// agent when a shell closes.
const onData = (html.match(/\bterm\.onData\(/g) || []).length;
if (onData !== 1) {
  fail(`term.onData is registered ${onData} times. It must be exactly once, and the ` +
    `previous listener disposed first, or keystrokes are sent once per listener.`);
}
// The dispose has to come BEFORE the registration, so the window between them
// is checked by position rather than by presence.
const onDataAt = html.indexOf("term.onData(");
const disposeAt = html.lastIndexOf("termData.dispose()", onDataAt);
if (onDataAt < 0 || disposeAt < 0) {
  fail("the keystroke listener is not disposed before being replaced. " +
    "Look for `if (termData) { termData.dispose() }` above `term.onData`.");
}

// Rule 2: ONE socket per pane.
//
// Reconnecting is scheduled from several branches of `onclose`. Nothing stops
// `connectTerm` being entered while an earlier socket is still open, and the
// old one is not closed by being replaced: it keeps receiving and writes into
// the same terminal, so every byte from the runner lands twice.
if (!/if \(termSock\) \{ try \{ termSock\.close\(\)/.test(html)) {
  fail("connectTerm does not close any existing socket before opening a new one. " +
    "Two open sockets write the runner's output into one terminal twice.");
}

// Rule 3: the message handler is guarded by socket identity, like onclose.
//
// Closing is asynchronous, so a socket that has stopped being the current one
// can still deliver messages that were already in flight. Writing them puts
// one runner's output into a terminal showing something else.
const onmessage = html.slice(html.indexOf("termSock.onmessage"),
  html.indexOf("termSock.onmessage") + 400);
if (!/termSock !== sock/.test(onmessage)) {
  fail("termSock.onmessage has no `termSock !== sock` guard. A socket that is no " +
    "longer current can still deliver what was already in flight.");
}

// Rule 4: input goes through one function, and that function scrolls.
//
// Paste and dropped files do not go through xterm at all, so a scroll attached
// to the keystroke handler misses exactly the inputs most likely to happen
// when you are scrolled up.
// Sliced rather than matched with a lazy body pattern: the function contains
// an object literal, so anything anchored on the next `}` stops early and the
// check fails on correct code.
const sendInputAt = html.indexOf("function sendInput(");
if (sendInputAt < 0) {
  fail("there is no sendInput. Every path that sends input is supposed to go through it.");
} else if (!/scrollToBottom\(\)/.test(html.slice(sendInputAt, sendInputAt + 400))) {
  fail("sendInput does not scroll to the bottom. Typing scrolls and pasting does not, " +
    "which is the case that matters: a paste is what happens when you are scrolled up.");
}

// And the scroll has to OUTLIVE the send, which is the half that was missing.
//
// The rule above passed the whole time a paste was landing short. Scrolling
// when the bytes are sent scrolls a buffer the runner has not answered yet: the
// echo comes back afterwards, the prompt redraws, and the bottom moves without
// the view. So the write path has to keep the view down for a moment too, and
// it has to do it in write's CALLBACK, because `term.write` parses later than
// it is called.
if (!/followScroll/.test(html)) {
  fail("nothing follows the output down after input is sent. `sendInput` scrolling by " +
    "itself scrolls before the runner has echoed anything, so a multi-line paste lands " +
    "with the view short by however many lines it was.");
} else {
  const onmsgAt = html.indexOf("termSock.onmessage");
  const onmsg = html.slice(onmsgAt, onmsgAt + 600);
  if (!/term\.write\([^)]*,\s*followScroll\)/.test(onmsg)) {
    fail("the follow-scroll is not passed as term.write's callback. Called after the " +
      "write instead, it runs before the bytes have been parsed and scrolls a buffer " +
      "that has not grown yet.");
  }
}

// Rule 5: a paste is framed as a paste.
//
// atrium intercepts `ctrl-v` so the clipboard's files are reachable, which
// means xterm never sees the text and never applies the framing it would have.
// Without bracketed paste markers every newline in a multi-line paste is Enter:
// the first line submits and the rest land in a prompt that is now busy. A big
// paste survives it by accident, because the runner detects a paste from the
// size of the burst, so this fails only for small ones.
if (!/bracketedPasteMode/.test(html)) {
  fail("nothing consults `term.modes.bracketedPasteMode`. A multi-line paste sent " +
    "without the markers arrives as one Enter per line.");
}
if (!/\\x1b\[200~/.test(html) || !/\\x1b\[201~/.test(html)) {
  fail("the bracketed paste markers are missing. See `sendPasteText`.");
}

// Rule 6: search decorations need the proposed API turned on.
//
// `registerDecoration` is how the search addon paints a hit and marks the
// scrollbar, and in xterm 5.5 it throws unless `allowProposedApi` is set. The
// addon calls it from inside `findNext`, BEFORE it searches, so passing
// `decorations` without the option makes the first search throw and every
// search after it do the same. Find reports zero matches against a buffer full
// of them.
//
// This shipped and went unnoticed for months, because the call site turned the
// exception into an empty count, which looks exactly like a search that found
// nothing. Neither half is checkable from a parser, so the pairing is checked
// here: either both are present or neither is.
const wantsDecorations = /decorations:\s*\{/.test(html);
const proposedOn = /allowProposedApi:\s*true/.test(html);
if (wantsDecorations && !proposedOn) {
  fail("the search options ask for `decorations` and the terminal is built without " +
    "`allowProposedApi: true`. registerDecoration throws on that, from inside findNext, " +
    "so EVERY search reports zero matches. Set it in the Terminal constructor.");
}

// Rule 7: the teardown for a dead terminal has a caller.
//
// `markTermDead` strips the bar down to a close button and is the only caller
// of `offerSoloClose`, which is what closes a popped-out window when its runner
// exits. It was defined, correct, and CALLED FROM NOWHERE for months. The
// branch that runs on exit wrote a line of text and returned.
//
// Three separate fixes were made downstream of it, all of them to code that
// could not run. Nothing caught it: the file parses, the function is
// syntactically fine, and a diff reader sees a definition and assumes a caller.
// A count is the whole check.
const deadCalls = (html.match(/markTermDead\(\)/g) || []).length;
const deadDef = /function markTermDead\(/.test(html);
if (deadDef && deadCalls < 2) {
  fail("markTermDead is defined and never called. It is the teardown for a terminal " +
    "whose runner exited, and the only caller of offerSoloClose, so a popped-out " +
    "window will sit there forever with a live-looking toolbar.");
}

// Rule 8: Tab is the runner's unless atrium is confident.
//
// Path completion works from outside somebody else's input line, which is only
// safe because it gives up early. Two properties keep it safe and neither is
// visible in a diff:
//
// The tracked buffer must be ABANDONED on anything ambiguous. An arrow key
// moves the cursor, so the tail of what atrium sent is no longer the token
// under it, and completing against it would insert text in the wrong place.
//
// Tab must PASS THROUGH when there is nothing to complete, or claude's own
// completion stops working everywhere this does not apply.
if (/completePath/.test(html)) {
  if (!/typedSure\s*=\s*false/.test(html)) {
    fail("path completion never abandons its tracked buffer. An arrow key or an escape " +
      "sequence moves the cursor, and completing against a stale buffer types into the " +
      "wrong place.");
  }
  if (!/if \(!done\) sendInput\("\\t"\)/.test(html)) {
    fail("Tab is swallowed without being passed through when nothing was completed, so the " +
      "runner's own completion stops working everywhere atrium cannot help.");
  }
  // And there has to be a way back other than Enter.
  //
  // A passed-through Tab arrives at the tracker as `\t`, which abandons it. If
  // Enter is the only reset, then one Tab that completes nothing turns the
  // feature off until the next submitted line, and every Tab after that keeps
  // it off. The trigger disables the trigger.
  if (!/d === " " && !typedSure/.test(html)) {
    fail("nothing re-arms path tracking except Enter. A passed-through Tab abandons it, so " +
      "one unproductive Tab disables completion until the next submitted line.");
  }
}

// Rule 9: a fit does not move the viewport.
//
// `fit()` hands new rows and columns to xterm's `resize()`, and xterm clamps
// the viewport to the bottom when the row count changes. Every layout event
// runs it, including ones a window focus change produces, so scrolling up and
// clicking away used to put you back at the bottom.
//
// Two properties keep it fixed and neither survives a casual edit. A fit that
// changed nothing must not be treated as a resize, and a fit that DID change
// something must put the viewport back where it was.
const fitAt = html.indexOf("function onTermResize(");
if (fitAt < 0) {
  fail("there is no onTermResize. The layout handler is where a fit gets its position wrong.");
} else {
  const body = html.slice(fitAt, fitAt + 1400);
  if (!/term\.cols === wasCols && term\.rows === wasRows/.test(body)) {
    fail("onTermResize does not check whether the size actually changed. A fit that changes " +
      "nothing still reaches xterm's resize, which snaps a scrolled-up terminal to the bottom.");
  }
  if (!/scrollToLine\(/.test(body)) {
    fail("onTermResize never restores the viewport. A resize that changes the row count clamps " +
      "to the bottom, so a terminal somebody scrolled up loses their place on any layout event.");
  }
}

if (bad) {
  console.error(`\n${bad} terminal invariant(s) broken. Each one is a bug somebody has ` +
    `already hit, not a style preference.`);
  process.exit(1);
}
console.log("the terminal invariants hold.");
