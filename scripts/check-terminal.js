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
  if (!/wasCols \|\| term\.rows !== wasRows/.test(body)) {
    fail("onTermResize does not check whether the size actually changed. A fit that changes " +
      "nothing still reaches xterm's resize, which snaps a scrolled-up terminal to the bottom.");
  }
  if (!/holdScrollAt\(/.test(body)) {
    fail("onTermResize never restores the viewport. A resize that changes the row count clamps " +
      "to the bottom, so a terminal somebody scrolled up loses their place on any layout event.");
  }
}

// Rule 10: xterm does not get to decide when input scrolls.
//
// `scrollOnUserInput` defaults to true and fires on more than typing: focus,
// and a click into the terminal. Scrolling up and clicking anywhere put the
// viewport back at the end, and no amount of fixing the resize path touches it.
// `sendInput` is where atrium does this deliberately, and the reason it is
// there rather than here is that a paste and a dropped file never reach xterm.
if (/new Terminal\(/.test(html) && !/scrollOnUserInput:\s*false/.test(html)) {
  fail("the terminal is built without `scrollOnUserInput: false`, so xterm scrolls to the " +
    "bottom on focus and on a click. atrium already scrolls on input in sendInput, which is " +
    "the path that also covers a paste and a dropped file.");
}

// Rule 11: focusing the terminal does not take the viewport with it.
//
// THE CAUSE OF FOUR FAILED FIXES. xterm keeps a hidden textarea for keystrokes
// and moves it to the CURSOR, which is at the bottom. Browsers scroll a focused
// element into view, so clicking the terminal or returning to the window took
// the scroll position to the end. Nothing calls a scroll function, which is why
// patching every scroll API found nothing.
//
// The position is recorded at the two moments before a focus can happen, a
// pointer going down and the window leaving, and put back after.
// Rule 12: a focus report is not a keystroke.
//
// THE ONE THAT COST A WHOLE EVENING. Claude Code asks to be told about focus
// and about the mouse, and xterm answers by sending escape sequences THROUGH
// `onData`, the same channel as typing: `ESC[I` on focus, `ESC[O` on blur, a
// coordinate report per click.
//
// `sendInput` scrolls to the bottom on input, so clicking in a terminal you had
// scrolled up threw your position away, and every theory blamed the browser,
// xterm, the reconciler and the resize path in turn. `noteTyped` has the same
// exposure from the other side: it abandons its buffer on anything unprintable,
// so a focus change silently switched off path completion.
if (!/function isAutoReport\(/.test(html)) {
  fail("nothing tells a focus or mouse report apart from a keystroke. xterm delivers both on " +
    "`onData`, so a click becomes input, and input scrolls to the bottom.");
} else {
  const at = html.indexOf("term.onData(");
  const body = at < 0 ? "" : html.slice(at, at + 300);
  if (!/isAutoReport\(d\)/.test(body)) {
    fail("the onData handler does not check `isAutoReport`. A mouse or focus report reaching " +
      "sendInput as ordinary input is what makes a click jump to the bottom.");
  }
  if (!/function sendInput\(text, quiet\)/.test(html)) {
    fail("sendInput cannot be told the input was not typed, so it scrolls for xterm's own " +
      "focus and mouse reports.");
  }
}

if (!/\.xterm-helper-textarea\s*\{[^}]*top:\s*0\s*!important/.test(html)) {
  fail("xterm's helper textarea is not pinned. It is a 20x20 box parked at the CURSOR, and a " +
    "browser scrolls a focused element into view, so clicking the terminal, returning to the " +
    "window or closing the find bar all take the viewport to the bottom. Nothing calls a scroll " +
    "function, so this cannot be fixed anywhere in the scroll code: four attempts proved it.");
}

// And the focuses atrium performs itself do not scroll either.
//
// Belt to the pin's braces, and cheap. `closeFind` is the one that bit: search
// for something above the fold, press escape, and the match was gone.
if (!/preventScroll/.test(html)) {
  fail("something focuses the terminal without `preventScroll`. Use `focusTerm`, which focuses " +
    "xterm's textarea without asking the browser to scroll to it.");
}

// And the position atrium holds must be releasable by hand.
//
// `holdScrollAt` re-asserts where you were for a moment, because the runner
// repaints when told its new size and that repaint arrives after any single
// restore. A hold with no release fights the wheel.
if (/function holdScrollAt\(/.test(html) && !/releaseScrollHold/.test(html)) {
  fail("the scroll hold has no release. It re-asserts a position for several hundred " +
    "milliseconds, so a wheel or a scrollbar drag during that window is fought rather than " +
    "obeyed.");
}

// Rule 13: a clicked path opens WHERE THE CLICK WAS.
//
// `files/open` runs a command on the daemon's machine. That is right for the
// `open there` chip in the file browser, which says so, and wrong for every
// path in the terminal: the board is meant to be driven from another machine
// over a share, so opening there puts a window in front of nobody.
//
// It is an easy mistake to make and an impossible one to notice, because on
// the developer's own machine both are the same machine and both look correct.
// The only place it shows up is on somebody else's screen, not opening.
if (/registerLinkProvider/.test(html)) {
  const at = html.indexOf("function openFromTerminal(");
  if (at < 0) {
    fail("the terminal registers a link provider and there is no openFromTerminal. " +
      "Clicking a path has to go through one place, or the next caller picks the " +
      "wrong one of the two ways to open a file.");
  } else {
    const body = html.slice(at, at + 2000);
    if (/openOnDaemon|files\/open/.test(body)) {
      fail("a path clicked in the terminal opens through files/open, which starts an " +
        "editor on the DAEMON'S machine. Over a share that window appears where " +
        "nobody is sitting. Use openEditor, which opens in this browser.");
    }
    if (!/openEditor\(/.test(body)) {
      fail("openFromTerminal never reaches openEditor, so clicking a path opens nothing " +
        "the person who clicked can see.");
    }
  }
  // And the answer has to be the daemon's, not a guess made by looking.
  //
  // Deciding locally is the version of this feature that gets written by
  // accident, and it is wrong in the same way every time: `v2.1.263`,
  // `zrok.io` and `foo.bar()` are all shaped like filenames, so a page of
  // ordinary output comes out underlined half way across.
  if (!/files\/probe/.test(html)) {
    fail("the terminal's links are decided without asking files/probe. Anything that " +
      "tells a filename from a version string by looking at it underlines half the " +
      "words on the screen.");
  }
}

// Rule 14: two link providers, and the URL one goes first.
//
// The terminal has two things that underline text: xterm's web-links addon,
// which knows a URL by its scheme, and atrium's path provider, which asks the
// daemon. They can both match inside one run of characters.
//
// xterm collects every provider's answer for a row and then DROPS the links
// that overlap something an earlier provider already claimed. So registration
// order is the whole tie-break, and getting it backwards does not fail: the
// path matcher claims part of a URL, the link is still drawn, and clicking it
// opens atrium's file viewer on something that was never a file. A link that
// opens the wrong thing is worse than one that is missing, and nothing about
// it looks wrong in a diff.
if (/WebLinksAddon/.test(html)) {
  if (!/vendor\/xterm-addon-web-links\.js/.test(html)) {
    fail("WebLinksAddon is used and /vendor/xterm-addon-web-links.js is never loaded. " +
      "The board has to work offline, so the addon is vendored, not fetched.");
  }
  if (!/typeof WebLinksAddon === "undefined"/.test(html)) {
    fail("nothing says so when the web links addon is missing. A bundle that did not " +
      "load looks exactly like URLs never having been clickable, which is the state " +
      "this replaced.");
  }
  const web = html.indexOf("useWebLinks(term)");
  const file = html.indexOf("useFileLinks(term");
  if (web < 0 || file < 0) {
    fail("useWebLinks or useFileLinks is not called from openTerm, so one of the two " +
      "link providers is never registered on the terminal.");
  } else if (web > file) {
    fail("the file-path link provider is registered BEFORE the web-links addon. xterm " +
      "gives a disputed run of text to whichever registered first, so a path matched " +
      "inside a URL now wins and clicking it opens atrium's file viewer on something " +
      "that is not a file.");
  }
  // And a URL is not a file, so it must not go anywhere near the file viewer.
  const at = html.indexOf("function openTermURL(");
  if (at < 0) {
    fail("there is no openTermURL. A clicked URL has to go through one place, or the " +
      "next caller picks the wrong one of the ways to open something.");
  } else {
    const body = html.slice(at, at + 600);
    if (/openEditor|openOnDaemon|files\//.test(body)) {
      fail("a clicked URL is routed through a file endpoint. It is not a file in the " +
        "card, and the daemon has no business being asked about it.");
    }
    if (!/noreferrer/.test(body)) {
      fail("a URL opened from the terminal carries a referrer. A published board's " +
        "address is not something to hand to whatever an agent printed a link to.");
    }
  }
}

// Rule 15: a theme is looked up through a table that inherits nothing.
//
// `TERM_THEMES` was an object literal, so `TERM_THEMES["constructor"]` answered
// a function rather than nothing, and the caller tested the answer for
// truthiness. A card whose theme was set to `constructor`, `valueOf` or
// `toString` therefore handed xterm a function to read colours off, and every
// colour came back undefined. It needed no brought theme to do it: the fixture
// dialog has always taken a theme name.
//
// The fix is `allThemes()` building the merged table with a null prototype, and
// `themeNamed` being the only way the page looks one up. Both halves are
// checked, because either alone brings it back: a null-prototype table read by
// a new direct-index call site is the same bug.
if (!/Object\.assign\(Object\.create\(null\), TERM_THEMES/.test(html)) {
  fail("the merged theme table is not built with a null prototype, so a theme named " +
    "`constructor` or `toString` resolves to a function and xterm is handed one instead " +
    "of a palette.");
}
const themeIndex = (html.match(/TERM_THEMES\[(?!"[a-z-]+"\])/g) || []).length;
if (themeIndex) {
  fail(`a theme is indexed out of TERM_THEMES directly in ${themeIndex} place(s). Use ` +
    `themeNamed(), which reads the merged null-prototype table: brought themes are ` +
    `missing from the shipped one, and a name off the prototype chain answers a function.`);
}

// Rule 16: the clipboard API is never awaited without a bound, and there is
// always the box.
//
// `navigator.clipboard.read` and `readText` are gated by a permission granted
// per ORIGIN. Loopback is one origin, answered once and remembered. EVERY
// SHARE IS A NEW ORIGIN, so the prompt goes up unanswered and the promise
// NEVER SETTLES. Not a rejection, which would have been caught and said out
// loud: nothing at all. Right click on a shared board did nothing, silently,
// while the same board on loopback pasted.
//
// Two halves, and both are needed. The wait has to end, and what it ends in
// has to be a paste that asks the browser for no permission at all: a text box
// the operator presses ctrl-v into, which is the clipboard arriving as a
// gesture rather than as a read.
if (/function pasteIntoTerm\(/.test(html)) {
  const at = html.indexOf("async function pasteIntoTerm(");
  const body = html.slice(at, at + 700);
  if (!/Promise\.race\(/.test(body)) {
    fail("pasteIntoTerm awaits the clipboard without a bound. An unanswered permission " +
      "prompt never settles, so right click on a shared board hangs forever and says " +
      "nothing. Race it against a timeout.");
  }
  if (!/openPasteBox\(/.test(body)) {
    fail("pasteIntoTerm has no fallback to the paste box. A board on a share cannot read " +
      "the clipboard from script at all, so a bounded wait that ends in a toast leaves " +
      "the operator with no way to paste from the page.");
  }
}
if (/openPasteBox\(/.test(html)) {
  for (const id of ["t-paste", "t-paste-in"]) {
    if (!html.includes(`id="${id}"`)) {
      fail(`the paste box calls for #${id} and the markup does not have it. The fallback ` +
        `path falls back to nothing.`);
    }
  }
  const sendAt = html.indexOf("function sendPasteBox(");
  if (sendAt < 0 || !/sendPasteText\(/.test(html.slice(sendAt, sendAt + 400))) {
    fail("the paste box does not send through sendPasteText. Skipping it means no " +
      "bracketed paste and no carriage returns, so a multi-line paste out of the box " +
      "submits its first line and drops the rest.");
  }
  const closeAt = html.indexOf("function closePasteBox(");
  if (closeAt < 0 || !/ta\.value = ""/.test(html.slice(closeAt, closeAt + 400))) {
    fail("closePasteBox does not clear the box. What gets pasted into a terminal is " +
      "often a secret, and a hidden box still holds it.");
  }
}

// Rule 17: the thing a clicked path opens has to be ON SCREEN.
//
// `#t-edit` is a panel INSIDE `#t-files-panel`, and the drawer starts hidden.
// `openEditor` unhides the editor and knows nothing about the drawer around
// it, so a path clicked in the terminal was read, filled into the box, and
// drawn nowhere. Nothing failed and nothing said so.
//
// The way it surfaced is worth keeping: the file stayed invisible until
// something else opened the drawer, and then a file clicked minutes earlier
// appeared. So the click read as doing nothing, and the NEXT click read as
// opening the wrong file. Neither symptom points at the container.
if (/function openFromTerminal\(/.test(html)) {
  const at = html.indexOf("function openFromTerminal(");
  const body = html.slice(at, at + 1200);
  if (!/setTermFiles\(true\)/.test(body)) {
    fail("openFromTerminal opens a file without opening the drawer. The editor is a panel " +
      "inside #t-files-panel, so unhiding it while the drawer is hidden puts the file on " +
      "screen nowhere and says nothing about it.");
  }
  // Both branches, and this is the half that regresses: the directory branch
  // opens the drawer on its own, so a fix that only covers that one looks
  // right in a diff and leaves clicking a FILE silent.
  const dirAt = body.indexOf("hit.dir");
  const openAt = body.indexOf("setTermFiles(true)");
  if (dirAt >= 0 && openAt > dirAt) {
    fail("openFromTerminal opens the drawer inside the directory branch only, so clicking a " +
      "FILE still opens an editor nobody can see. The drawer is opened for both.");
  }
}

// Rule 13: the font size goes through the resize path, and dies with the pane.
//
// Changing xterm's `fontSize` changes how many rows and columns fit, so it is a
// resize whether or not it is called one. Fitting on its own here would be a
// second copy of what rules 9 through 12 protect: a fit that changed nothing
// still reaches xterm's `resize` and snaps a scrolled-up terminal to the
// bottom, and a fit that DID change something has to put the viewport back and
// hold it while the runner repaints.
//
// SEARCHED FOR RATHER THAN SLICED AT. A sibling check reads a fixed window of
// characters from the top of a function and fails anything that pushes what it
// wants past the end, which has nothing to do with what it protects. This takes
// the whole function and looks inside it.
if (/function setTermFont\(/.test(html)) {
  const fontAt = html.indexOf("function setTermFont(");
  const fontEnd = html.indexOf("\nfunction ", fontAt + 1);
  const fontBody = html.slice(fontAt, fontEnd < 0 ? html.length : fontEnd);
  if (!/onTermResize\(\)/.test(fontBody)) {
    fail("setTermFont does not go through onTermResize. A font change is a resize, and " +
      "fitting on its own is a second copy of the viewport handling that rules 9 to 12 " +
      "exist to protect.");
  }
  // IT SURVIVES A SWITCH, and this half of the rule asserted the opposite for
  // one build. `openTerm` runs every time you switch to another session, so a
  // size that resets there is a size cleared by clicking a second card and
  // coming back. The operator found that within a minute of it shipping.
  if (!/localStorage/.test(fontBody)) {
    fail("setTermFont does not remember the size, so switching to another session and back " +
      "clears it. It is kept per card in localStorage, the way a popped-out window's size " +
      "already is.");
  }
  // AND NOT ON THE DAEMON. This is a property of the screen somebody is
  // reading rather than of the work, so it has no business travelling to
  // another machine or being served to a guest holding a share of one session.
  if (/patchTask|\/v1\//.test(fontBody)) {
    fail("setTermFont sends the size to the daemon. It belongs to the browser reading the " +
      "terminal, not to the card, and a guest holding a share must not inherit it.");
  }
  const openAt = html.indexOf("function openTerm(");
  if (openAt >= 0) {
    const openEnd = html.indexOf("\nfunction ", openAt + 1);
    const openBody = html.slice(openAt, openEnd < 0 ? html.length : openEnd);
    if (!/termFontSize = readTermFont\(/.test(openBody)) {
      fail("openTerm does not restore this card's font size, so switching sessions loses it. " +
        "It used to reset to the default here, which is the bug rather than the design.");
    }
  }
}

if (bad) {
  console.error(`\n${bad} terminal invariant(s) broken. Each one is a bug somebody has ` +
    `already hit, not a style preference.`);
  process.exit(1);
}
console.log("the terminal invariants hold.");
