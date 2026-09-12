// ── terminal ────────────────────────────────────────────
// Attaching to a runner atrium owns. Bytes arrive as binary frames and go
// straight to xterm. Everything sent back is a JSON control frame, so a
// keystroke and a resize stay distinguishable without inventing framing.
let term = null, termFit = null, termSock = null, termTask = null;
// The keystroke listener, held so a reconnect can replace it rather than stack
// another one on top. See `connectTerm`.
let termData = null;

// WHEN THE DAEMON SAID IT WAS GOING DOWN.
//
// Set by the `going-down` event and read by every path that has to decide
// whether a closed socket is worth waiting on. A timestamp rather than a flag,
// because it has to expire: a daemon that announced a restart and never came
// back would otherwise leave this window waiting forever, and a window left
// open overnight must not treat this morning's disconnect as last night's
// restart.
let restartAt = 0;
const restartWindow = 5 * 60 * 1000;

function armRestart(why) {
  restartAt = Date.now();
  // Broadcast, so a popped-out window learns it from the board even if its own
  // stream is the one that drops first.
  try { soloBus && soloBus.postMessage({ type: "going-down" }); } catch (e) {}
  if (term) {
    term.write("\r\n\x1b[38;5;244m[atrium] atrium is restarting" +
      (why ? ": " + why : "") + "\x1b[0m\r\n");
  }
  termWait("atrium is restarting. reconnecting to " + waitName(termTask) + "…");
}

// Whether a restart was announced recently enough to explain what just
// happened.
function restartComing() {
  return restartAt > 0 && Date.now() - restartAt < restartWindow;
}

// WHY THE LAST ATTACH ENDED, read off the websocket close frame.
//
// `whyClosed` in the daemon sends one of `restarting`, `shell closed` and
// `runner exited`, and that word is the whole question the pane has to answer
// when a socket drops. A restart is an outage worth waiting out. The other two
// are answers: the thing that ended was ended on purpose and nothing is coming
// back. The board used to throw the word away, so every teardown started the
// restart wait and a session somebody typed `exit` into sat for ninety seconds
// under a banner saying atrium was restarting, while the bar under it said
// nothing attached.
//
// Written down here rather than passed along the call chain, because the
// teardown is reached from several places and not all of them can see the
// close: the socket closing, the poll that finds the card no longer
// supervised, and the runner-exited event are three teardowns of one exit, and
// any of them can be the one that gets there first.
//
// A card AND a timestamp, for the same reason `restartAt` is a timestamp. A
// reason left lying around would go on answering for a close that happens an
// hour from now, on a different session.
let endedCard = "", endedWhy = "", endedAt = 0;
const endedWindow = 30 * 1000;

function noteAttachEnded(card, why) {
  endedCard = card || "";
  endedWhy = why || "";
  endedAt = Date.now();
}

// Did this card's attach end because somebody ended it, rather than because
// atrium went away underneath it.
function endedOnPurpose(card) {
  if (!card || endedCard !== card) return false;
  if (endedWhy !== "runner exited" && endedWhy !== "shell closed") return false;
  return Date.now() - endedAt < endedWindow;
}

// THE TERMINAL BEFORE THIS ONE, so an exit has somewhere to land.
//
// `atrium.term` is a single slot and every attach overwrites it, so by the
// time a session ends it holds the card that just died. This is the slot
// underneath it: the card that was attached before the current one, which is
// what "the last window" means when somebody exits and expects to be put
// somewhere rather than nowhere.
//
// Written from `openTerm` rather than from `rememberWhereYouAre`, because that
// one runs on every view change and would push the current card down into the
// previous slot without anything having changed.
function rememberPreviousTerm(now) {
  try {
    const was = localStorage.getItem("atrium.term") || "";
    if (was && was !== now) localStorage.setItem("atrium.termPrev", was);
  } catch (e) {}
}

// Copy on selection, off by default because it surprises people who drag to
// scroll. Remembered, like the other preferences.
let copyOnSelect = localStorage.getItem("atrium.copyOnSelect") === "1";

// Kept because several places call it. The button it used to paint is gone
// from the bar: `copy on select: off` was a permanent five word sentence
// occupying the same room as `exit`, for a setting changed about once.
function paintCopyMode() {
  const b = document.getElementById("t-copymode");
  if (!b) return;
  b.textContent = copyOnSelect ? "copy on select: on" : "copy on select: off";
  b.className = copyOnSelect ? "go" : "";
}

function toggleCopyOnSelect() {
  copyOnSelect = !copyOnSelect;
  localStorage.setItem("atrium.copyOnSelect", copyOnSelect ? "1" : "0");
  paintCopyMode();
}

// Everything about how THIS terminal looks and behaves, behind one cog.
//
// The bar was running out of room, and worse in a popped-out window, which is
// narrow by design and is exactly where the per-window settings belong. What
// went in here is what you set rarely: the palette, whether selecting copies,
// and how big this card's window opens.
//
// Size is set explicitly rather than learned from every drag. Remembering the
// last size automatically meant nudging an edge to see something behind the
// window silently became the size every card opened at, which is a preference
// nobody expressed.
// openOlderScrollback shows what a card's terminal held before the last
// restart, in a tab of its own.
//
// FETCHED BEFORE THE TAB IS OPENED, so a card with nothing saved says so here
// instead of opening a window onto an error page. The 404 carries the reason,
// which is the part nobody can guess: the file is written when atrium is
// stopped, so a card whose daemon was killed has none.
//
// A blob rather than the url directly. The endpoint answers `text/plain` and a
// browser would render it, but going through the fetch is what lets the
// failure be a toast, and the same bytes then open with no second request.
async function openOlderScrollback(id) {
  let body;
  try {
    const res = await fetch(`/v1/tasks/${id}/scrollback/older`);
    if (!res.ok) {
      toast("no history saved", (await res.text()).trim());
      return;
    }
    body = await res.blob();
  } catch (err) {
    toast("could not read the history", err.message);
    return;
  }
  const url = URL.createObjectURL(body);
  const w = open(url, "_blank");
  if (!w) {
    toast("the tab was blocked", "allow pop-ups for atrium to read the history");
  }
  // Released once the tab has had a chance to load it. Revoking immediately
  // races the open and gives a blank tab.
  setTimeout(() => URL.revokeObjectURL(url), 60000);
}

async function termSettings(e) {
  // Without this the menu opens and closes in the same event: `showMenu` draws
  // it, the click keeps bubbling to the document listener that dismisses any
  // open menu, and the cog reads as a dead button. `cardMenu` stops the same
  // event for the same reason.
  e.preventDefault();
  e.stopPropagation();
  const t = termTask;
  if (!t) return;
  // AWAITED. This used to start the fetch and build the menu without it, so
  // the first open of the session was missing `say something` and the second
  // had it: a menu whose contents depend on when you last opened it. One
  // request, once per page, and only the first click waits for it.
  if (!allActions.length) await loadActions();
  const own = readSize("atrium.popout." + t.id);
  const items = [
    // Reachable from the terminal you are looking at, which is where saying
    // something to a session is a thing you want to do.
    actionItems(t),
    // Here as well as on the card, because handing somebody a session is
    // something you decide while looking at it.
    shareItem(t),
    actionsFor(t).length ? { sep: true } : null,
    { label: "try a theme", note: t.theme || "from the project",
      help: "Puts a picker on the bar. Arrow through them and the terminal " +
        "you are looking at changes as you go, against the output already on " +
        "screen. Escape puts back what was there.",
      act: () => pickTheme() },
    { label: "notification icon…", note: t.icon || "the atrium mark",
      tip: "The mark this card wears on a desktop notification.",
      act: () => pickIcon(t.id, t.icon) },
    { label: "copy on select", on: copyOnSelect,
      tip: "Selecting text copies it, the way a terminal does.",
      act: () => toggleCopyOnSelect() },
    // The text in the terminal, and nothing else on the page. Browser zoom
    // takes the tabs, the strip and the chrome with it, which is the thing
    // this exists instead of.
    //
    // The current size is on the row rather than in the tip, because the one
    // question somebody opening this has is how big it is now.
    { step: true, label: "text size",
      min: TERM_FONT_MIN, max: TERM_FONT_MAX,
      help: "Scales the text in this terminal only, unlike browser zoom, " +
        "which takes the tabs and the strip with it. Type a size or press the " +
        "buttons. It lasts as long as the pane does and is deliberately not " +
        "remembered: it answers `I cannot read this right now`.",
      get: () => termFontSize,
      set: (n) => setTermFont(n) },
    // Only when it is not already there, since a reset that resets nothing is
    // a row you read and skip every time you open this.
    termFontSize !== TERM_FONT_DEFAULT ? {
      label: "reset text size", note: `back to ${TERM_FONT_DEFAULT}px`,
      act: () => setTermFont(TERM_FONT_DEFAULT)
    } : null,
    { sep: true },
    // WHAT THIS TERMINAL SAID BEFORE THE LAST RESTART.
    //
    // In a tab and not in the terminal, because xterm can only append. There
    // is no way to put bytes above what is already in its buffer, so "load
    // more at the top" would have to clear the pane and redraw it, which
    // throws away the live session's screen to show you history.
    //
    // It used to be joined onto the front of every attach and that was worse
    // than either: a resumed session reprints its own recent history, so you
    // got the last hour twice with nothing saying why.
    { label: "history from before the restart…", note: "opens in a tab",
      help: "What this card's terminal held when atrium was last stopped, as " +
        "plain text. Written at a clean stop, so there is none if the daemon " +
        "was killed. Not shown in the terminal because a terminal can only " +
        "add to the bottom.",
      act: () => openOlderScrollback(t.id) },
    { sep: true },
    // Only meaningful from the window whose size is being remembered. From the
    // board there is no popped-out window to measure.
    termOnly() ? {
      label: "remember this size", note: "for this card",
      tip: "This card's window opens at whatever size it is right now.",
      act: () => {
        rememberPopOutSize(t.id, outerWidth, outerHeight, false);
        toast("size remembered", `${Math.round(outerWidth)} by ${Math.round(outerHeight)}`);
      }
    } : null,
    termOnly() ? {
      label: "make this the default", note: "for every card",
      tip: "Every card without a size of its own opens at this one.",
      act: () => {
        rememberPopOutSize(t.id, outerWidth, outerHeight, true);
        toast("default size set", `${Math.round(outerWidth)} by ${Math.round(outerHeight)}`);
      }
    } : null,
    own ? {
      label: "forget this card's size", note: `${own.w} by ${own.h}`,
      tip: "Falls back to the default size.",
      act: () => {
        try { localStorage.removeItem("atrium.popout." + t.id); } catch (err) {}
        toast("forgotten", "this card opens at the default size now");
      }
    } : null
  ];
  showMenu(e, items);
}

function openTerm(task) {
  if (typeof Terminal === "undefined") {
    tellUser("atrium", "the terminal library did not load");
    return;
  }
  // Switching sessions means tearing the old one down first, or two sockets
  // write into one screen.
  if (termSock || term) closeTerm(true);

  // ONE ENTRY FOR THIS ARRIVAL, not two. This runs before the card is set, so
  // letting it record would leave `terms with nothing attached` in the history
  // between the view you came from and the session you asked for, and back
  // would need pressing twice to leave a place you were never in.
  const wasApplying = navApplying;
  navApplying = true;
  switchView("terms");
  navApplying = wasApplying;
  // A find left open belonged to the session that just went away, and its
  // decorations were painted on a buffer that no longer exists.
  const findbar = document.getElementById("t-find");
  if (findbar) findbar.hidden = true;
  termWait("");
  // A different card means the runner again. A shell belongs to the card it
  // was opened in, so carrying the choice across would either attach to the
  // wrong card's shell or, more likely, to nothing at all.
  //
  // Compared against a REMEMBERED ID rather than against `termTask`, because
  // switching terminals within one card tears the old socket down first and
  // `closeTerm` clears `termTask` on the way. Reading it here would say "a
  // different card" every time and put the pane straight back on the runner.
  if (termKindFor !== task.id) termKind = "runner";
  termKindFor = task.id;
  termTask = task;
  // Written down so a reload comes back here. Only in the board: a solo
  // window is addressed by its hash and has no business voting on where the
  // board lands.
  if (!termOnly()) {
    // The card being replaced goes into the slot underneath, so an exit has a
    // terminal to fall back to. See `rememberPreviousTerm`.
    rememberPreviousTerm(task.id);
    try { localStorage.setItem("atrium.term", task.id); } catch (e) {}
  }
  // AND A PLACE TO COME BACK TO. Switching session is the move made most
  // often after switching view, so back that skipped it would step over the
  // half of the journey somebody actually remembers.
  //
  // Here rather than in `attachTask`, because this is where the card becomes
  // the attached one and every route in passes through it: the switcher, a
  // card, the restore, a toast, and going back itself.
  if (!navApplying) navPush();
  // The same address the popped-out window carries, so a session is one name
  // whichever way you are looking at it. The full path matters here too: the
  // switcher beside it lists sessions whose short titles collide.
  document.getElementById("t-title").textContent = terminalLabel(task) || task.display_title;
  document.getElementById("t-title").title = task.worktree || "";
  // The runner as its mark, in front of the name, the way a card carries it.
  // A `claude` pill among the chips said the same thing in the place the eye
  // goes last, and read as one more fact rather than as whose terminal this is.
  document.getElementById("t-mark").innerHTML = runnerMark(task.runner);
  // What is left is what changes: the process, and where it is. The theme
  // moved into the cog, with the rest of how this terminal behaves.
  // The path copies itself. It is the thing most often wanted somewhere else,
  // and reading it off a screen to retype it is the worst way to spend a
  // minute.
  document.getElementById("t-chips").innerHTML =
    (task.pid ? `<span class="chip">pid ${task.pid}</span>` : "") +
    // THE GLYPH COPIES, THE PATH DOES NOT. The whole chip used to be the
    // button, so a row-width target sat over the bar saying `click to copy`
    // and swallowed clicks meant for what was behind it. A path is also a
    // thing you select with the mouse, which a click handler over all of it
    // makes awkward.
    `<span class="chip path" data-path="${esc(task.worktree || "")}"
       ><button class="copybit" title="copy this path"
         onclick="event.stopPropagation();copyPath(this.parentNode)"
         >${copyIcon()}</button>${esc(task.worktree || "")}</span>`;
  paintCopyMode();
  paintTermKind();

  const screen = document.getElementById("t-screen");
  screen.innerHTML = "";
  // WHATEVER THIS CARD WAS LAST READ AT. This line used to put it back to the
  // default, which meant switching to another session and back cleared a size
  // somebody had just set: `openTerm` runs on every switch, not only on the
  // first one.
  termFontSize = readTermFont(task.id);
  term = new Terminal({
    // Cascadia Mono ships with Windows Terminal and is drawn for exactly this:
    // it has the box drawing and powerline glyphs an agent's output uses, which
    // Consolas renders as boxes.
    fontFamily: "'Cascadia Mono', 'Cascadia Code', 'JetBrains Mono', " +
      "'Fira Code', Consolas, ui-monospace, monospace",
    fontSize: termFontSize,
    lineHeight: 1.2,
    letterSpacing: 0.2,
    cursorBlink: true,
    // ATRIUM DECIDES WHEN INPUT SCROLLS, so xterm must not also decide.
    //
    // This defaults to true, which snaps the viewport to the bottom whenever
    // xterm believes the user has interacted. That belief is broader than
    // typing: it fires on focus and on a click into the terminal, so scrolling
    // up and clicking anywhere put you back at the end.
    //
    // The board already does this deliberately in `sendInput`, and the comment
    // there says why it is done there rather than here: the inputs that matter
    // most, a paste and a dropped file, never reach xterm at all. So this
    // option was a second mechanism covering less, firing on things that are
    // not input, and it was the one nobody was controlling.
    scrollOnUserInput: false,
    // REQUIRED BY SEARCH, and its absence is why find never found anything.
    //
    // `registerDecoration` is what paints a hit and puts a mark down the
    // scrollbar, and in xterm 5.5 it is a proposed API that throws unless this
    // is set. The search addon calls it from inside `findNext`, before it has
    // searched anything, so passing `decorations` without this made the very
    // first search throw and every subsequent one do the same. Every match in
    // the buffer, reported as zero.
    //
    // It was invisible for months because the call site swallowed the
    // exception into an empty result count, which is indistinguishable from a
    // search that matched nothing. Do not remove this without also removing
    // the `decorations` block in `findOpts`.
    allowProposedApi: true,
    // The second of the two limits, and the smaller one wins. The daemon
    // keeps N megabytes of every runner's output and this decides how much of
    // it survives once it is here, so both are one setting in two units.
    //
    // Asked of the daemon rather than carried here, because a number in this
    // file and a number in the database can be configured to disagree and
    // then the buffer you paid for is not the one you get. `settingsSeen` is
    // whatever the last GET said, so a tab that has not talked to the daemon
    // yet gets the same default the daemon would have used.
    scrollback: scrollbackLines(),
    // The session's own theme, so a terminal looks the same here as it does
    // in a terminal window, and two sessions are tellable apart by color.
    theme: themeFor(termTask)
  });
  paintPaneBg(themeFor(termTask));
  termFit = new FitAddon.FitAddon();
  term.loadAddon(termFit);
  term.open(screen);
  useWebgl(term);
  useSearch(term);
  // THESE TWO ARE IN THIS ORDER ON PURPOSE. Both linkify the terminal, and
  // xterm gives a disputed run of text to whichever provider registered first,
  // so a URL wins over a path that matched inside it.
  useWebLinks(term);
  // A path in the output is a path you can click. The card comes with it,
  // because what makes a word a link is whether it is a file in that card's
  // directory, and nothing else.
  useFileLinks(term, task);
  // Drop a file onto the pane. The same pipeline as paste, because whatever
  // gesture produced the bytes, the bytes go to the same place.
  wireTerminalDrops(screen);
  wireTerminalPaste(screen);
  // A wheel or a drag on the scrollbar ends any position atrium is holding.
  // See `holdScrollAt`: it re-asserts where you were while a runner repaints,
  // and the one thing it must never do is fight the operator doing it by hand.
  screen.addEventListener("wheel", () => {
    noteScrollAct("wheel");
    releaseScrollHold();
  }, { passive: true });
  // BEFORE the focus, which is the only moment the position is still true.
  // Capture phase so it runs ahead of anything that might focus the textarea.
  screen.addEventListener("pointerdown", () => {
    noteScrollAct("click");
    releaseScrollHold();
  }, true);
  screen.addEventListener("focusin", () => noteScrollAct("focus"));
  screen.addEventListener("focusout", () => noteScrollAct("blur"));
  watchScroll(term);

  // Copy and paste have to win over the terminal, or selecting text and
  // pressing ctrl-c interrupts the runner instead of copying. Returning false
  // stops xterm treating the keystroke as input.
  term.attachCustomKeyEventHandler(e => {
    if (e.type !== "keydown") return true;
    const ctrl = e.ctrlKey || e.metaKey;

    // Anything typed puts the candidate list away. It sits over the terminal
    // and a list that outstays its welcome is in the way.
    //
    // Escape is SWALLOWED when the list is up, and passed through otherwise.
    // The hint on the list offers escape, and letting it reach the runner as
    // well would dismiss the list and cancel whatever the runner was doing,
    // which is one keypress doing two things the operator only asked for one
    // of.
    if (e.key === "Escape") {
      const list = document.getElementById("t-complete");
      if (list && !list.hidden) {
        hideCompletions();
        e.preventDefault();
        return false;
      }
    }
    if (e.key !== "Tab") hideCompletions();

    // TAB IS THE RUNNER'S UNLESS ATRIUM IS CONFIDENT.
    //
    // Confident means: the tracked buffer is still believable, the token under
    // the cursor looks like a path, and the daemon found something matching.
    // Any of those failing passes Tab straight through, so claude's own
    // completion keeps working everywhere this does not apply.
    //
    // The check is asynchronous and the handler is not, so the decision is made
    // optimistically: swallow the key, and if nothing could be completed, send
    // the Tab afterwards. A Tab arriving a few milliseconds late is invisible.
    // Sending it first and completing afterwards would be two inputs for one
    // press, which is the doubled-keystroke class of bug.
    if (e.key === "Tab" && !ctrl && !e.altKey) {
      const token = pathToken();
      // SAID, on Tab only, because this is invisible by design and that makes
      // it undebuggable. The rule is silence rather than a wrong guess, so a
      // Tab that atrium declines looks identical to one it never saw. One line
      // in the console names which of the two it was.
      console.debug("[atrium] tab:", JSON.stringify({
        typed, typedSure, token, kind: termKind
      }));
      if (token) {
        e.preventDefault();
        completePath().then(done => { if (!done) sendInput("\t"); });
        return false;
      }
    }

    // ctrl-shift-c is Chrome's inspect-element shortcut and the browser takes
    // it before the page ever sees it, so binding copy there both copied and
    // opened dev tools. The insert pair is the older terminal convention and
    // nothing claims it.
    if (ctrl && e.key === "Insert") { e.preventDefault(); copySelection(); return false; }

    // FIND. Both bindings, because both are muscle memory: ctrl-f is the
    // browser's and ctrl-shift-f is what several editors use for "find in
    // everything". Here they open the same bar, since there is one buffer to
    // search.
    //
    // `preventDefault` matters. The browser's own find is useless on this
    // terminal: it walks the DOM and a WebGL terminal draws to a canvas, so
    // ctrl-f opened a find box that could never match a single character of
    // what is on screen.
    if (ctrl && e.code === "KeyF") { e.preventDefault(); openFind(); return false; }

    // PASTE IS NOT PREVENTED HERE, and that is the whole fix.
    //
    // `preventDefault` on ctrl-v suppresses the browser's own `paste` event,
    // which is the only place `clipboardData` exists. Without it the code fell
    // back to `navigator.clipboard.read()`, and that API only offers the
    // sanitized types Chromium can re-encode. The Windows Snipping Tool writes
    // a `CF_DIBV5` bitmap that Chromium will not convert, so `read()` returned
    // no image, the paste silently did nothing, and the same screenshot pasted
    // through Paint worked because Paint rewrites it as a plain DIB.
    //
    // Returning false still stops xterm treating the keystroke as input, so
    // the runner never sees a bare control character. The default action goes
    // ahead and `wireTerminalPaste` takes it from there with the real
    // clipboard payload.
    if (e.shiftKey && e.key === "Insert") return false;
    if (ctrl && !e.shiftKey && e.code === "KeyV") return false;

    // CTRL-SHIFT-V ASKS THE CLIPBOARD FIRST, and opens the box only if it does
    // not answer.
    //
    // The box exists because `navigator.clipboard.read()` is a permission
    // granted per ORIGIN: every share is an origin nobody has answered for, and
    // an unanswered prompt makes the promise never settle. On loopback that
    // permission was answered once and is remembered, so the read comes back in
    // single-digit milliseconds and the box is a step for nothing. Pressed out
    // of habit on the board, it was two keystrokes and a dialog to do what
    // ctrl-v does on its own.
    //
    // So this is right click's path, which is `pasteIntoTerm`: race the read
    // against `pasteWaitMs` and fall back to the box. There is deliberately no
    // "am I on loopback" test anywhere here. WHETHER THE CLIPBOARD ANSWERS IS
    // THE TEST, it is the thing actually being asked about, and it stays right
    // on a browser, an overlay or a permission model nobody has anticipated.
    //
    // Bound here rather than left to the browser because nothing else claims it
    // and it is the pair to ctrl-shift-c on a terminal.
    if (ctrl && e.shiftKey && e.code === "KeyV") {
      e.preventDefault();
      pasteIntoTerm();
      return false;
    }

    // A newline in the prompt, rather than sending it.
    //
    // xterm sends a plain carriage return for Enter, Shift-Enter and
    // Ctrl-Enter alike: the modifier never reaches the runner, so all three
    // submitted and there was no way to write a second line. The terminal is
    // the only thing that can tell them apart, so it has to be the thing that
    // does.
    //
    // Two encodings, because there is no single answer that is right for every
    // runner and the two are wired to different keys ON PURPOSE:
    //
    //   Shift-Enter sends ESC CR, which is what a terminal emulator configured
    //   by `claude /terminal-setup` sends, and what Claude Code parses back
    //   into a newline. Exact for that harness, and meaningless to anything
    //   else. Worse than meaningless in a shell with vi-mode readline, where a
    //   bare ESC leaves insert mode.
    //
    //   Ctrl-Enter sends a line feed, which is what Ctrl-J has always sent and
    //   what almost everything treats as "insert a newline here". Works with
    //   nothing configured, and in some harnesses submits instead.
    //
    // If one of these turns out to be right everywhere, collapse them. Do not
    // collapse them on a guess: the failure is silent and looks like the key
    // doing nothing.
    if (!ctrl && e.shiftKey && e.key === "Enter") {
      e.preventDefault();
      send({ t: "in", d: "\x1b\r" });
      return false;
    }
    if (ctrl && e.key === "Enter") {
      e.preventDefault();
      send({ t: "in", d: "\n" });
      return false;
    }

    // Plain ctrl-c interrupts, unless something is selected, in which case
    // copying is what was meant. This is what every terminal does.
    if (ctrl && !e.shiftKey && e.code === "KeyC" && term.hasSelection()) {
      e.preventDefault();
      copySelection();
      return false;
    }
    return true;
  });

  term.onSelectionChange(() => {
    if (copyOnSelect && term.hasSelection()) copySelection(true);
  });

  // Fit after the pane has a size, or the first resize is computed against a
  // hidden element and the runner is told a nonsense width.
  requestAnimationFrame(() => {
    termFit.fit();
    connectTerm(task.id);
    renderTermList();
  });
}

function copySelection(quiet) {
  if (!term || !term.hasSelection()) return;
  const text = term.getSelection();
  navigator.clipboard.writeText(text)
    .then(() => { if (!quiet) toast("copied", `${text.length} characters`); })
    .catch(() => { if (!quiet) toast("could not copy", "the browser refused clipboard access"); });
}

// The browser's own paste event, which is where the clipboard actually is.
//
// `clipboardData` on this event carries what the source application wrote,
// handed over synchronously. `navigator.clipboard.read()` is the async API and
// is a different, narrower thing: Chromium will only offer types it can decode
// and re-encode itself, so a bitmap it does not like becomes no image at all
// rather than an error. That is why a Snipping Tool screenshot pasted nothing
// while the same screenshot round-tripped through Paint pasted fine.
//
// Capture phase, so this runs before xterm's own paste handler, and both
// stopped, so a text paste is not delivered twice.
//
// WIRED ONCE. `#t-screen` outlives every attach: `openTerm` empties it and
// builds a new terminal inside it, but the element itself is the same one it
// was at boot, so a listener added on attach is still there on the next one.
// Without this guard the fifth attach pasted five times, four of which lost
// the race to create the file and each of which said so in its own alert.
function wireTerminalPaste(screen) {
  if (!screen || screen.dataset.pastes) return;
  screen.dataset.pastes = "1";
  screen.addEventListener("paste", e => {
    const dt = e.clipboardData;
    if (!dt || !termTask) return;

    // `items` rather than `files`, because a screenshot arrives as an item of
    // kind `file` with no name, and browsers have not always populated
    // `files` for it.
    const files = [];
    for (const item of dt.items || []) {
      if (item.kind !== "file") continue;
      const f = item.getAsFile();
      if (f) files.push(f);
    }

    e.preventDefault();
    e.stopPropagation();
    // A guest has no file endpoint, so a pasted picture is offered nothing and
    // falls through to the text below it. Pasting TEXT is the terminal, which
    // is the one thing this link does share.
    if (files.length && !isGuest()) { uploadIntoTerm(files); return; }

    // Files first, because a clipboard holding a screenshot usually also holds
    // a text representation of it, and pasting that instead is how a paste
    // silently does nothing useful.
    const text = dt.getData("text/plain");
    if (text) sendPasteText(text);
  }, true);

  // RIGHT CLICK PASTES, the way every terminal emulator on Windows does.
  //
  // The clipboard is loaded and the hand is already on the mouse, so the
  // browser's own menu is the wrong thing to get: nothing on it applies to a
  // terminal, and reaching for `ctrl-v` instead means leaving the mouse for one
  // keystroke.
  //
  // SHIFT OR CTRL LETS THE BROWSER HAVE IT, which is the convention every site
  // that overrides this menu already follows, so `inspect` and `save image as`
  // are one modifier away rather than gone.
  //
  // Goes through `pasteIntoTerm` rather than reading the clipboard here,
  // because that is the one path that tries FILES first: right clicking with a
  // screenshot on the clipboard uploads it and hands the runner a path, the
  // same as `ctrl-v` does.
  screen.addEventListener("contextmenu", e => {
    if (!termTask || e.shiftKey || e.ctrlKey) return;
    e.preventDefault();
    e.stopPropagation();
    pasteIntoTerm();
  });
}

// A PASTE IS NOT A BURST OF TYPING, and the runner has to be told which it is.
//
// This path exists because atrium handles the clipboard itself: `ctrl-v` is
// returned as unhandled to xterm so the browser's own `paste` event fires,
// which is the only place a screenshot's bytes are reachable. The cost is that
// xterm never sees the text, so the framing it would have applied has to be
// applied here.
//
// Two things it does:
//
//   - CARRIAGE RETURNS. A clipboard gives `\n` or `\r\n`. A terminal delivers
//     `\r` for a line break, and an application reading raw input treats `\n`
//     as a line feed rather than as a key.
//   - BRACKETED PASTE, when the application asked for it. Without the markers
//     every newline in the pasted text is Enter, so pasting three lines into a
//     prompt submits the first line and drops the rest into a prompt that is
//     now busy. That is the whole bug: a big paste survived it because Claude
//     Code detects a paste from the size of the burst, and a two line paste is
//     not a big enough burst to be detected.
//
// `term.modes` is xterm's own record of what the application turned on, so
// this asks rather than assumes: sending the markers to something that never
// requested them would put `[200~` on screen.
function sendPasteText(text) {
  let body = String(text).replace(/\r\n/g, "\r").replace(/\n/g, "\r");
  const bracketed = term && term.modes && term.modes.bracketedPasteMode;
  if (bracketed) body = "\x1b[200~" + body + "\x1b[201~";
  // ONE FRAME. Splitting it is what makes a paste look like typing, which is
  // the thing the brackets exist to deny.
  sendInput(body);
}

// The fallback, for a paste that did not arrive as an event: right click, or
// a browser that refused the default action. Kept because those cases exist,
// and no longer the primary path.
//
// BOUNDED, and that is the fix for a share.
//
// `navigator.clipboard` is gated by a permission granted per ORIGIN. Loopback
// is one origin, answered once and remembered; every share is a new origin
// with no answer on file. What that looks like is not a refusal: the prompt
// goes up and the promise NEVER SETTLES while it is unanswered, so `await`
// here waited forever and right click did nothing at all, with no error and
// nothing said. Measured on a zrok share in both Chrome and Brave, where
// `clipboard-read` reads `prompt` and neither `read()` nor `readText()`
// resolves. Loopback in the same browser pastes, because the grant is there.
//
// So the wait is bounded and whatever comes back that is not text opens the
// paste box, which needs no permission at all.
async function pasteIntoTerm() {
  const mine = ++pasteAttempt;
  const got = await Promise.race([
    clipboardIntoTerm(mine),
    new Promise(r => setTimeout(
      () => r({ why: "the browser has not been told whether this page may read the clipboard" }),
      pasteWaitMs)),
  ]);
  if (got.ok) return;
  openPasteBox(got.why);
}

// How long to wait on the clipboard API before offering the box instead.
//
// Long enough that a granted origin never sees the box, short enough that an
// ungranted one is not left looking at a terminal that did nothing. A grant
// resolves in single-digit milliseconds; an unanswered prompt resolves never.
const pasteWaitMs = 1200;

// Which paste attempt is current.
//
// The bounded wait does not cancel the clipboard read, it only stops waiting
// on it. If the operator answers the prompt a minute later that read resolves
// and would paste on its own, on top of whatever went through the box. This
// is how a late answer is told it lost the race.
let pasteAttempt = 0;

// The clipboard API attempt, files first and then text, unchanged in what it
// does with either. Reports whether it got anywhere rather than toasting,
// because the caller has somewhere better to go than an error.
async function clipboardIntoTerm(mine) {
  if (!navigator.clipboard) {
    return { why: "this browser will not let a page read the clipboard here" };
  }
  try {
    if (await pasteFilesIntoTerm()) return { ok: mine === pasteAttempt };
  } catch (e) { /* text is still worth trying */ }
  try {
    const text = await navigator.clipboard.readText();
    if (mine !== pasteAttempt) return { ok: true };
    if (text) sendPasteText(text);
    return { ok: true };
  } catch (e) {
    return { why: "the browser refused clipboard access" };
  }
}

// THE PASTE BOX: the clipboard arriving by the door that is always open.
//
// `navigator.clipboard.readText()` is a page reading the clipboard, and that
// is a permission. A `paste` event on a focused text box is a PERSON handing
// the clipboard over, and no browser has ever asked permission for that. Same
// bytes, same clipboard, and it works on an origin that has answered nothing,
// which is every share.
//
// It carries files too, for the same reason `wireTerminalPaste` does: a
// screenshot arrives on the event as an item of kind `file`, so pasting one
// in here uploads it and hands the runner a path, exactly as ctrl-v over the
// terminal would.
function openPasteBox(why) {
  const box = document.getElementById("t-paste");
  const ta = document.getElementById("t-paste-in");
  const say = document.getElementById("t-paste-say");
  if (!box || !ta) {
    toast("could not paste", why || "the browser refused clipboard access");
    return;
  }
  // Any clipboard read still in flight has lost: the box is the answer now,
  // and a late one arriving on top of it is a paste nobody asked for twice.
  pasteAttempt++;
  wirePasteBox();
  if (say) say.textContent = why ? why + ", so paste it here" : "paste here";
  box.hidden = false;
  ta.value = "";
  ta.focus();
}

function closePasteBox() {
  const box = document.getElementById("t-paste");
  const ta = document.getElementById("t-paste-in");
  if (box) box.hidden = true;
  // CLEARED ON THE WAY OUT. What gets pasted into a terminal is often a
  // secret, and a hidden box still holds what was put in it.
  if (ta) ta.value = "";
  // Back to the terminal, or the next thing typed goes into a box nobody can
  // see.
  if (term) term.focus();
}

function sendPasteBox() {
  const ta = document.getElementById("t-paste-in");
  const text = ta ? ta.value : "";
  closePasteBox();
  // Through `sendPasteText`, so a multi-line paste from the box is bracketed
  // and carriage-returned the same as one that came off the terminal. A box
  // that skipped that would submit the first line and drop the rest.
  if (text) sendPasteText(text);
}

// WIRED ONCE, for the same reason `wireTerminalPaste` is: the box outlives
// every attach.
function wirePasteBox() {
  const box = document.getElementById("t-paste");
  const ta = document.getElementById("t-paste-in");
  if (!box || !ta || box.dataset.wired) return;
  box.dataset.wired = "1";

  ta.addEventListener("paste", e => {
    const dt = e.clipboardData;
    if (!dt) return;
    const files = [];
    for (const item of dt.items || []) {
      if (item.kind !== "file") continue;
      const f = item.getAsFile();
      if (f) files.push(f);
    }
    // Text is left alone and lands in the box, where it can be read before it
    // is sent. Files have nothing to read and no text form worth sending, so
    // they go straight up.
    if (!files.length) return;
    // A guest has no file endpoint. Left alone rather than refused: whatever
    // text form the clipboard also carries lands in the box, which is the same
    // thing this box does for every paste it cannot upload.
    if (isGuest()) return;
    e.preventDefault();
    closePasteBox();
    uploadIntoTerm(files);
  });

  // Enter writes a newline, because this is a box for multi-line pastes and
  // sending on Enter would make a two line paste impossible to hand over.
  ta.addEventListener("keydown", e => {
    if (e.key === "Escape") { e.preventDefault(); closePasteBox(); return; }
    if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); sendPasteBox(); }
  });
}

// Pasting a file, which over an overlay has no workaround at all.
//
// Claude Code reads images, and the clipboard is on the machine with the
// browser, not the machine with the daemon. So the bytes go up, and what comes
// back is a path the runner can open with its own file tool. There is no
// image-specific branch and no base64 inlining, which is exactly why one
// pipeline covers files of any kind.
//
// Returns whether it handled the paste.
async function pasteFilesIntoTerm() {
  if (!termTask || !navigator.clipboard || !navigator.clipboard.read) return false;
  // A guest has no file endpoint, so this hands the paste back and the text
  // branch behind it takes over. Not handled here means not handled, which is
  // what the caller already knows how to do something with.
  if (isGuest()) return false;
  let items;
  try { items = await navigator.clipboard.read(); } catch (e) { return false; }

  const files = [];
  for (const item of items) {
    // Anything that is not plain text. A screenshot is image/png; a copied
    // file is whatever it is.
    const type = item.types.find(t => t !== "text/plain" && t !== "text/html");
    if (!type) continue;
    try {
      const blob = await item.getType(type);
      files.push(new File([blob], "pasted." + (type.split("/")[1] || "bin"), { type }));
    } catch (e) { /* an item that will not read is one to skip */ }
  }
  if (!files.length) return false;

  await uploadIntoTerm(files);
  return true;
}

// The one pipeline for paste, drop and picker.
//
// Whatever gesture produced the bytes, the bytes go to the same endpoint and
// what comes back is a path. Charon's lesson, and worth taking whole.
async function uploadIntoTerm(files) {
  if (!termTask || !files || !files.length) return;
  const form = new FormData();
  for (const f of files) form.append("file", f, f.name);

  // Taken back when the answer arrives. `uploading` and `in the working
  // directory` are the same event twice, and the first one is only worth
  // saying while it is still true.
  const saying = toast("uploading", files.length === 1 ? files[0].name : files.length + " files");
  let res;
  try {
    res = await api(`/v1/tasks/${termTask.id}/files`, { method: "POST", body: form });
  } catch (e) {
    if (saying) saying.dismiss();
    toast("that did not go up", e.message);
    return;
  }
  if (saying) saying.dismiss();

  // Spliced into the stream and NOT submitted.
  //
  // handleMessage appends a newline because a message is a complete
  // instruction being sent. A pasted path is a fragment of an instruction the
  // human is still writing, and submitting it for them is the difference
  // between a helpful paste and a runner that starts working on half a
  // sentence. docs/supervision-design.md is why this matters: atrium owns a
  // real terminal that a person may be mid-command in.
  const paths = (res.paths || []).join(" ");
  // The preamble says what the path is for. A bare path is what a person types
  // when they mean "look at this", and it is not what they say.
  const pre = preambleOf(await pasteSettings());
  if (paths) sendInput(pre + paths + " ");
  toast(pastePrefs && pastePrefs.paste_keep === "scrap"
    ? "in the scratch folder" : "in the working directory", paths);
}

// Dropping a file onto the terminal is the same gesture with a different hand.
//
// A GUEST GETS NO DROP TARGET. `guestHandler` refuses `/v1/tasks/*/files`, so
// the highlight and the "uploading" toast were an invitation to something that
// could only end in a refusal.
//
// The listeners are still attached, and only the invitation is withdrawn. A
// page with no `drop` handler lets the browser have the file, which NAVIGATES
// AWAY from the terminal to display it, so removing them would cost a guest the
// session over a mis-aimed drag. So the drop is swallowed and answered with the
// daemon's own sentence.
function wireTerminalDrops(screen) {
  if (!screen || screen.dataset.drops) return;
  screen.dataset.drops = "1";
  screen.addEventListener("dragover", e => {
    if (!termTask) return;
    e.preventDefault();
    if (!isGuest()) screen.classList.add("dropping");
  });
  screen.addEventListener("dragleave", () => screen.classList.remove("dropping"));
  screen.addEventListener("drop", e => {
    screen.classList.remove("dropping");
    if (!termTask) return;
    const files = Array.from((e.dataTransfer && e.dataTransfer.files) || []);
    if (!files.length) return;
    e.preventDefault();
    if (isGuest()) { toast("files are not part of this link", guestWord); return; }
    uploadIntoTerm(files);
  });
}

// A path, onto the clipboard.
//
// Reading a truncated path off a screen to retype it is the worst way to spend
// a minute, and this list truncates on purpose. The full value is on the
// element, not the text, so what gets copied is what the card says rather than
// whatever fitted.
async function copyPath(el) {
  const path = el && el.dataset ? el.dataset.path : "";
  if (!path) return;
  try {
    await navigator.clipboard.writeText(path);
  } catch (e) {
    toast("could not copy it", e.message);
    return;
  }
  toast("copied", path);
}

// Sorting by most recent activity, so whichever agent just asked for something
// or just finished rises to the top. On by default: with several running, the
// one that moved is the one you want.
let sortByActivity = localStorage.getItem("atrium.termSort") !== "0";

function toggleTermSort() {
  sortByActivity = !sortByActivity;
  localStorage.setItem("atrium.termSort", sortByActivity ? "1" : "0");
  renderTermList();
}

