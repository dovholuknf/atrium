// ── one terminal, in its own window ─────────────────────
//
// The habit this competes with is alt-tab, which is faster than a click into
// an app and then a click onto a tab, and which nothing here can beat while a
// session is a pane inside a page.
//
// So: the same page, in terminal-only mode, addressed by `#term=<id>`. NOT a
// second HTML file. `CLAUDE.md` calls index.html "the whole board, one file",
// and a second page would be a second copy of the xterm wiring, the resize
// handling and the attach lifecycle, which is two copies of one flow waiting
// to diverge on the first fix.
//
// What this deliberately is NOT is handing the session to Windows Terminal.
// That would mean atrium stops owning the pty, which costs attach, the
// activity badge, the liveness check and stop, and `docs/supervision-design.md`
// records that there is no reattach on Windows to get them back. A browser
// window is what is buildable; the backlog is right that a real terminal is
// what you actually want and that it is not available at this price.

// What a popped-out window is called in alt-tab.
//
// `display_title` is written for a card sitting in a column, where the
// surrounding board already says which machine and which project. A window in
// alt-tab has no surroundings, so it needs the whole address:
//
//   github/openziti/ziti-tunnel-sdk-c:nightly-failures
//
// Anchored on the REPO NAME rather than on path depth or on the last segment.
// Depth varies, and the last segment is the worktree DIRECTORY, which is not
// the branch: one card here sits in `.../desktop-edge-win/fix-app-version`
// while actually being on `promote-2.11.3.1-and-beta`, because somebody
// switched branches inside the worktree. The directory is the stale half, so
// the branch is what gets shown.
//
// Finding the repo segment gives the two before it for free, which on this
// layout are the host and the org and are exactly what is missing.
function windowTitle(task) {
  const label = terminalLabel(task);
  return label ? label + " - atrium" : "atrium terminal";
}

function terminalLabel(task) {
  if (!task) return "";
  const path = String(task.worktree || "").replace(/\\/g, "/").replace(/\/+$/, "");
  const segs = path.split("/").filter(Boolean);
  const repo = String(task.repo || "").trim();
  const branch = String(task.branch || "").trim();

  // Last match, not first: a repo called `ziti` under `.../github/openziti/ziti`
  // would otherwise anchor on the org when the two share a name.
  let at = -1;
  for (let i = 0; i < segs.length; i++) {
    if (repo && segs[i] === repo) at = i;
  }

  let where = "";
  if (at >= 0) {
    where = segs.slice(Math.max(0, at - 2), at + 1).join("/");
  } else if (segs.length) {
    // No repo on the card. That happens for a worktree atrium has not been
    // able to ask git about, and for a directory that is not a checkout at
    // all.
    //
    // The last segment is dropped when it IS the branch, or the name appears
    // twice: `openziti/ziti/discourse-6036:discourse-6036` reads as two
    // different things and is one. Dropping it leaves the repo as the last
    // segment, which is what the branch should hang off.
    let tail = segs;
    if (branch && tail[tail.length - 1] === branch) tail = tail.slice(0, -1);
    // Never the drive letter, which is the same on every card and says
    // nothing.
    where = tail.slice(Math.max(1, tail.length - 3)).join("/");
  }

  if (where && branch) return where + ":" + branch;
  if (where) return where;
  return task.display_title || "";
}

// popOutTerm opens whatever is attached here in its own window.
function popOutTerm() {
  if (termTask) popOutTask(termTask.id);
}

// A terminal in its own window, from wherever a card is drawn.
//
// The stack is the case that matters. Attaching first and then popping out is
// two steps to reach one window, and it makes the board briefly the owner of a
// terminal it is about to give away.
// Windows this page has opened, so a second press raises rather than opens.
//
// The window NAME already does most of this: `window.open` with a name that is
// already taken returns the existing window. It stops being enough as soon as
// the opener reloads, which this board now does by itself when the daemon
// serves a new build, and a handle held across that reload is gone while the
// window is still very much there.
//
// So there are two answers and both are needed. The handle covers this page's
// own presses. The broadcast covers a window this page never opened, which is
// every window opened before the last reload.
const popOuts = new Map();

// Returns what it did: `raised` for a window that was already there, `opened`
// for a new one, `unreachable` for one that is out there and cannot be
// reached, `blocked` when the browser refused. The caller says something
// different about each, and saying the wrong one is how "raised it for you"
// ended up pointing at a window nobody could find.
//
// Async because ONE of those four answers has to be asked for rather than
// worked out. See `stillOutThere`.
async function popOutTask(id) {
  const held = popOuts.get(id);
  if (held && !held.closed) { held.focus(); return "raised"; }

  // Opened by an earlier incarnation of this page, so there is no handle. Get
  // one back by NAME.
  //
  // `window.open("", name)` with an empty url returns an existing window under
  // that name WITHOUT navigating it, so the scrollback survives. Names live in
  // the browsing context group, which a popup stays in across the opener's
  // reload, and the board reloads itself whenever the daemon serves a new
  // build.
  //
  // **This is the half that actually raises.** Asking the window to focus
  // itself over the broadcast does not work and never did: `window.focus()` in
  // a background tab has no user gesture behind it and every browser refuses
  // it. The board is the one holding the click, so the board has to do it.
  // That is why "raised it for you" was routinely a lie.
  if (poppedOut(id)) {
    const found = reopenByName(id);
    if (found) { popOuts.set(id, found); found.focus(); return "raised"; }
    // Nothing under that name, and that is TWO different situations wearing
    // one face.
    //
    // The window may have gone in the last few seconds, leaving a claim that
    // has not expired yet. Or it may be sitting right there and be
    // unreachable: a window opened by pasting a `#term=<id>` url into a tab
    // has no name, so `window.open("", name)` will never find it, no matter
    // how alive it is.
    //
    // Falling through was right for the first and wrong for the second, where
    // it opened a SECOND window onto one terminal, which is the thing
    // `docs/supervision-design.md` says nothing arbitrates, and then said "it
    // was not there any more" about a window still on screen.
    //
    // So the difference is asked for rather than guessed at. A window that
    // answers is out there, and the honest answer is that this board cannot
    // bring it forward.
    if (await stillOutThere(id)) {
      tellUser("it is in a window this board did not open",
        "That window is still there and still has the terminal, and a page may only raise a " +
        "window it opened itself, so this board cannot bring it forward. Alt-tab to it, or " +
        "close it and pop the card out again from here.");
      return "unreachable";
    }
  }

  // The card id, so the new window resolves the session itself rather than
  // being handed a name that may be stale by the time it loads.
  const url = location.pathname + "#term=" + encodeURIComponent(id);
  const size = popOutSize(id);
  // Named per card, which is the first line of defence against a second one.
  const win = window.open(url, "atrium-term-" + id,
    `width=${size.w},height=${size.h},menubar=no,toolbar=no,location=no,status=no`);
  if (win) popOuts.set(id, win);
  if (!win) {
    tellUser("the browser blocked it", "allow pop-ups for this board and try again.");
    return "blocked";
  }
  win.focus();
  // Detached only when the board is holding THIS card, because two views onto
  // one terminal both writing input is the situation
  // `docs/supervision-design.md` says nothing arbitrates. Popping out one card
  // from the stack must not tear down the unrelated terminal the board has
  // open, which is what an unconditional `closeTerm` did.
  if (termTask && termTask.id === id) closeTerm();
  return "opened";
}

// reopenByName finds a popped-out window this page did not open.
//
// The catch is that `window.open("", name)` OPENS A BLANK WINDOW when the name
// is free, so the answer has to be checked and an unwanted one closed again.
// A window that is really the terminal is same-origin and is on this board's
// path; a fresh one is `about:blank`.
function reopenByName(id) {
  let w = null;
  try { w = window.open("", "atrium-term-" + id); } catch (e) { return null; }
  if (!w) return null;
  try {
    if (w.location.href !== "about:blank") return w;
  } catch (e) {
    // Cross-origin, which cannot be one of ours. Left alone rather than
    // closed: it belongs to something else.
    return null;
  }
  w.close();
  return null;
}

// How long the board waits for one window to answer for one card.
//
// Shorter than `soloRollCall`, which is paid once at load and is sized for a
// restart reloading every document at once. This one is paid on a click, with
// a person watching, and it is asking a question a window that is already
// running answers in the same frame it hears it.
const soloAnswerFor = 350;

// Whether a window claiming this card is STILL out there, asked rather than
// assumed.
//
// `soloHeld` is a heartbeat and it is up to `soloClaimFor` stale, which is
// long enough to be wrong about a window that has just gone. The roll call
// turns that into a fresh answer: anything that replies stamps its claim now,
// so a claim newer than the question is a window that spoke after being
// asked.
//
// False when there is no BroadcastChannel at all. Without one the board never
// hears from a window it did not open, so it has nothing to be sure about and
// opening one is the better of the two mistakes.
function stillOutThere(id) {
  if (!soloBus) return Promise.resolve(false);
  const asked = Date.now();
  try { soloBus.postMessage({ type: "solo-who" }); } catch (e) { return Promise.resolve(false); }
  return new Promise(done => {
    setTimeout(() => done((soloHeld.get(id) || 0) >= asked), soloAnswerFor);
  });
}

// Says something in the window that holds a card, rather than in the one that
// was clicked.
//
// The board raises a popped-out window and then has something to say about
// having done it. Saying it here draws it in the window you are leaving. It
// belongs where you are going.
//
// Returns whether it was HANDED OVER, which is not the same as drawn: a
// broadcast has no delivery report, and a window running an older build of
// this page would ignore the message. The claim the caller makes on the back
// of a `true` is only that this board said nothing itself, which is a fact
// about this document and is verifiable here.
function sayInSoloWindow(id, title, body) {
  if (!soloBus) return false;
  try { soloBus.postMessage({ type: "solo-toast", task: id, title, body }); }
  catch (e) { return false; }
  return true;
}

// How big a popped-out window opens.
//
// Set by resizing one, not by filling in a form. The size you want is a thing
// you find by dragging an edge until the terminal looks right, and having
// found it there is nothing left for a settings field to ask.
//
// Per card first, then whatever you last sized ANY window to, then a default.
// Both halves earn their place: a card you keep in a tall narrow window beside
// an editor stays that shape, and a card popped out for the first time opens
// at the size the last one taught, rather than at a number chosen here.
//
// Held in localStorage rather than on the card, because it is a fact about
// this screen. The same card popped out on a laptop wants a different window,
// and a size synced from the desktop would be wrong on arrival.
const POPOUT_DEFAULT = { w: 900, h: 620 };
function popOutSize(id) {
  return readSize("atrium.popout." + id) || readSize("atrium.popout") || POPOUT_DEFAULT;
}
function readSize(key) {
  try {
    const v = JSON.parse(localStorage.getItem(key) || "null");
    // Guarded because a stored zero or a NaN from an older shape would open a
    // window with no area, which on Windows is a window you cannot grab.
    if (v && v.w > 200 && v.h > 150) return { w: Math.round(v.w), h: Math.round(v.h) };
  } catch (e) {}
  return null;
}
// Records a size, on this card and optionally as the default for every card
// that has none of its own.
//
// Asked for rather than learned. It used to save on every resize, which meant
// dragging an edge to see something behind the window silently became the size
// every card opened at. A preference nobody expressed is worse than no
// preference, because it is indistinguishable from a bug.
function rememberPopOutSize(id, w, h, alsoDefault) {
  if (!(w > 200 && h > 150)) return;
  const size = JSON.stringify({ w: Math.round(w), h: Math.round(h) });
  try {
    if (id) localStorage.setItem("atrium.popout." + id, size);
    if (alsoDefault) localStorage.setItem("atrium.popout", size);
  } catch (e) {}
}

// termOnly reports whether this window is a popped-out terminal.
function termOnly() { return /^#term=/.test(location.hash); }

// Strips the page down to one terminal.
//
// Everything else is hidden rather than removed, so the same code paths keep
// working: the switcher, the card dialog and the settings all still exist and
// are simply never shown.
// Whether this window may have the card named in its address, asked of every
// window that might already hold it.
//
// TWO THINGS A NAIVE VERSION GETS WRONG, and either one makes it worse than
// the bug it closes.
//
// **A claim has to expire.** `soloHeld` is a heartbeat and it is up to
// `soloClaimFor` stale, so believing it would lock a card out for fifteen
// seconds after a window closed, and a browser that skipped `pagehide`
// entirely would lock it out until something else corrected the ledger. So
// this asks the way `attachTask` asks, through `stillOutThere`: a roll call,
// and only a window that ANSWERS counts as a holder. A claim nobody speaks for
// is not one.
//
// **The refusal has to be recoverable in one click.** A dialog saying the card
// is open elsewhere and nothing more strands whoever cannot find that window,
// and this page cannot help them: a page may only focus a window it opened
// itself, and a window opened from a pasted url has no opener here. So the
// dialog offers to TAKE the card, which asks the holder to let go and then
// attaches.
//
// Taking rather than CLOSING the other window was the choice. Closing one is a
// call browsers refuse silently unless they agree this script opened it, which
// is exactly the case that is not true here, so a close button would have
// claimed to do something it had not roughly half the time. See
// `closeThisWindow`, which exists because of that.
//
// Returns whether to go ahead and attach.
async function takeSoloCard(id) {
  if (!id) return true;
  if (!(await stillOutThere(id))) return true;

  // Said in the title bar too, because this window may already be behind
  // something by the time the dialog is up, and the title is what alt-tab
  // shows.
  document.title = "atrium: already open elsewhere";
  const take = await askUser({
    title: "that card is open in another window",
    body: "atrium puts one view on a terminal. two of them and the daemon sizes the " +
      "session to the smaller window, so the bigger one redraws every wrapped line on " +
      "top of itself and eats a character at each wrap.<br><br>" +
      "this page cannot bring the other window forward: a page may only raise a window " +
      "it opened itself. so go to that window, or take the terminal from it here.",
    buttons: [
      { label: "leave it there", value: null },
      { label: "take it anyway", value: true, style: "go" }
    ]
  });
  if (take !== true) {
    termWait("this card is already open in another window. close this one, or reload to ask again.");
    return false;
  }

  if (soloBus) soloBus.postMessage({ type: "solo-yield", task: id });
  // Asked again rather than assumed, because a broadcast has no delivery
  // report and a window running an older build of this page has never heard of
  // `solo-yield`. Going ahead either way is deliberate: this is the click that
  // means "I want it here", and refusing after being told to take it would
  // leave nowhere left to go.
  const letGo = !(await stillOutThere(id));
  // The ledger entry goes whatever the answer was. Left there, `poppedOut` in
  // this document would go on saying this window's own card is somewhere else.
  soloHeld.delete(id);
  if (!letGo) {
    toast("the other window did not let go",
      "it may be an older build of this page. close it if this terminal draws wrong.");
  }
  return true;
}

async function bootTerminalOnly() {
  const want = decodeURIComponent(location.hash.slice("#term=".length));
  document.body.classList.add("solo");

  // ONE VIEW PER TERMINAL, AND THIS IS THE DOOR THAT WAS LEFT OPEN.
  //
  // Every other route already refuses: `attachTask` raises the window instead
  // of attaching, `soloSwitch` turns a switch onto a held card away, and
  // `popOutTask` will not open a second one. A `#term=<id>` url pasted into a
  // tab went round all three.
  //
  // What it costs is not cosmetic. The daemon sizes the pty to the SMALLEST
  // viewer and nothing tells the larger window's xterm about the size that was
  // agreed, so every cursor-positioning escape the runner emits is computed for
  // a narrower line than the one it lands on. A wrapped input line redraws on
  // top of itself and eats a character per wrap column. The narrow window looks
  // perfect throughout, which is why the window being typed in reads as the
  // broken one.
  //
  // So ask first, and do not attach if somebody answers.
  if (!(await takeSoloCard(want))) return;
  soloID = want;

  // THE CLAIM IS THE FIRST THING THIS WINDOW DOES ONCE IT HAS THE CARD.
  //
  // It used to be posted after the card was fetched and the terminal opened,
  // which is a round trip and a WebGL context later. A restart reloads the
  // board and every popped-out window at once, and the board is a smaller page
  // that finishes first: it asked who was out there, nobody had claimed
  // anything yet, and it rang for a card that was a few hundred milliseconds
  // away from ringing for itself. Two chimes for one event.
  //
  // Nothing here depends on the card existing. The id is in the URL, this
  // window is going to speak for it either way, and claiming a card that turns
  // out to be gone costs one alert the board skips for a session that has
  // nothing to alert about.
  //
  // The roll call above is the one thing now in front of it, and it fits: it
  // waits `soloAnswerFor`, and the board waits `soloRollCall` before its first
  // poll, which is nearly three times as long. The margin that stopped the two
  // chimes is still there.
  if (soloBus) {
    soloBus.postMessage({ type: "solo-claim", task: soloID });
    // `pagehide` rather than `unload`, which a browser is free to skip when it
    // freezes a page into the back/forward cache. A claim that is never
    // released would leave the board permanently silent about this card.
    addEventListener("pagehide", () => soloBus.postMessage({ type: "solo-release", task: soloID }));
  }

  let task;
  try {
    task = await api("/v1/tasks/" + encodeURIComponent(soloID));
  } catch (e) {
    document.title = "atrium: no such card";
    tellUser("nothing to attach to", e.message);
    return;
  }
  soloTask = task;
  // The title bar is the whole reason this window is worth having: it is what
  // alt-tab shows.
  paintSoloTitle();
  openTerm(task);

  // Coming back to the window IS reading the alert, so the mark clears without
  // touching anything. Leaving it up until the next poll meant a title that
  // still said a session was blocked while you sat looking at it.
  addEventListener("focus", () => { soloMark = ""; paintSoloTitle(); });

}

// What the title bar says, and the mark in front of it.
//
// The mark is the point, not the toast. A toast lives inside a window you may
// not be looking at, which is exactly the case this window exists for; a title
// bar is what alt-tab and the taskbar show while the window is buried, and it
// cannot be missed or dismissed by accident.
//
// Two marks, so they read differently at a glance in a switcher: a blocked
// agent is doing nothing until you answer, a ready one is merely done.
let soloMark = "";
function paintSoloTitle() {
  document.title = (soloMark ? soloMark + " " : "") + windowTitle(soloTask);
}

// One card's worth of the board's polling pass.
//
// Deliberately not `alerting.check`, which keeps board-wide sets of what it
// has already rung for. Two documents sharing that logic would each seed their
// own copy and disagree about what is new. This tracks one card and two
// booleans, which is all a single session can be.
// `null` until the first poll lands, which is what makes opening the window
// silent. A card that has been sitting ready for an hour is why you popped it
// out, so announcing it as news the moment you get there is both wrong and the
// most annoying possible moment to be wrong. Only a change after that rings.
//
// The board draws the same distinction in `alerting.check` and it is the one
// thing this had to copy from it.
let soloKnown = { perm: null, ready: null };
async function soloRefresh() {
  if (!soloID) return;
  // The heartbeat behind every board's `soloHeld`. On the poll rather than a
  // timer of its own: it is the same question at the same rate, and a second
  // timer is a second thing to get wrong.
  //
  // Not while yielded. This window handed the card to another one, and a
  // heartbeat for a card it no longer shows would have the board refuse to
  // attach to it and "raise" a window with nothing in it.
  if (soloBus && !soloYielded) soloBus.postMessage({ type: "solo-claim", task: soloID });

  const [task, waiting, perms] = await Promise.all([
    api("/v1/tasks/" + encodeURIComponent(soloID)).catch(() => null),
    api("/v1/waiting").then(r => r.tasks || []).catch(() => null),
    api("/v1/permissions").then(r => r.permissions || []).catch(() => null)
  ]);

  // A branch can change under a running session, and the title is the whole
  // product here, so it is re-read rather than fixed at open.
  if (task) { soloTask = task; }

  // THE WATCHDOG. If this window has no terminal and its card has one, attach.
  //
  // Everything else about reconnecting hangs off the socket's retry loop, and
  // a retry loop can only run while something is holding it. Any path that
  // disposes the terminal ends the loop, and this window then sits on a dead
  // page with a live session behind it, which is what F5 was fixing by hand.
  //
  // This asks the only question that matters, on the poll that is already
  // running, and needs nothing to have survived. It is deliberately the last
  // line of defence rather than the mechanism: reattaching costs the
  // scrollback, so the retry loop keeping one socket alive is still the good
  // outcome and this is the one that stops a window being useless.
  //
  // Not while yielded, which is the one teardown that was a decision rather
  // than an outage. Without that the watchdog would put back, five seconds
  // later, the terminal this window was asked to give up.
  if (!term && task && task.supervised && !soloYielded) {
    rlog("solo window has no terminal and the card does. attaching");
    openTerm(task);
  }

  const mine = perms ? perms.filter(p => (p.task_id || p.id) === soloID) : null;
  const ready = waiting
    ? waiting.some(t => t.id === soloID && t.status !== "needs-permission")
    : null;

  if (mine !== null) soloAlert("perm", mine.length > 0, mine[0]);
  if (ready !== null) soloAlert("ready", ready, task);
  paintSoloTitle();
  renderTermPerm();

  // A halted daemon is a fact about this terminal too: the session behind it
  // is parked and nothing said so.
  api("/v1/health").then(h => {
    checkBuild(h.build);
    const el = document.getElementById("halted");
    el.style.display = h.halted ? "flex" : "none";
    if (h.halted) {
      document.getElementById("halt-t").innerHTML =
        `agents are parked and will not reconnect until you restart. <code>${esc(h.cause)}</code>`;
    }
  }).catch(() => {});
}

// Rings once on the way in, and only on the way in.
function soloAlert(kind, now, item) {
  const was = kind === "perm" ? soloKnown.perm : soloKnown.ready;
  if (kind === "perm") soloKnown.perm = now; else soloKnown.ready = now;
  // Falling edge: answered, so the mark goes. A permission outranks a ready
  // card, since one is blocked and the other is only idle.
  if (!now) {
    if (soloMark === (kind === "perm" ? "(!)" : "*")) soloMark = "";
    return;
  }
  // The first poll only learns. The state is shown, since a window that opens
  // on a blocked session should say so in its title, but nothing is played and
  // nothing is put on screen: you are looking straight at it.
  if (was === null) {
    if (kind === "perm") soloMark = "(!)";
    else if (!soloMark) soloMark = "*";
    return;
  }
  if (was) return;

  soloMark = kind === "perm" ? "(!)" : "*";
  const who = soloTask ? (soloTask.display_title || "this session") : "this session";
  const title = kind === "perm"
    ? `${who} needs permission`
    : `${who} is ${soloTask && justStarted(soloTask) ? "up" : "ready"}`;
  const body = kind === "perm" && item
    ? `${item.tool}: ${(item.command || "").slice(0, 120)}`
    : readyBecause(soloTask || {});

  alerting.play(kind === "perm" ? "permission" : "waiting", soloTask && soloTask.sound);
  // VISIBLE IS ENOUGH HERE, and `inForeground` is the wrong test for this
  // window.
  //
  // `inForeground` is visible AND focused, which is right for the board: it is
  // a tab among many and being visible does not mean you are reading it.
  //
  // A popped-out terminal is not that. It exists to be looked at and holds one
  // session, so a second monitor with it open on it is exactly the case the
  // operator complained about: the window was telling them, out loud, about a
  // thing they were watching happen. Focus is about which window takes the
  // keyboard, and that is a different question from whether you can see it.
  if (onScreen()) {
    // In front of you, so the toast is the whole message. No `goTo`: there is
    // nowhere in this window to go, and clicking through to a board is the
    // thing this window refuses to become.
    toast(title, body, null, item && item.id, soloID);
  } else {
    // Behind something. The mark on the title bar is already up, and the
    // desktop notification is what makes you look at the switcher at all. The
    // board is holding its own copy back because of the claim.
    // taskFor is this window's own card, which is what the picture belongs to.
    // Not passed as the suppression key: this window IS the one that should
    // speak, and handing it its own claim would silence it.
    alerting.notify(title, body, kind === "perm" ? "perms" : "stack", "", soloID, "",
      iconForAlert(soloTask), soloID);
  }
}

// Back to the view you were on, and to the terminal you were reading.
//
// The daemon reloads every board it is serving when it comes up with a new
// build, so this runs several times an evening while atrium is being worked
// on. Landing on the board each time and clicking `terminals` and then the
// same card is two clicks to undo something nobody asked for.
//
// Both halves are checked rather than trusted. A remembered view that is not a
// view, and a remembered card that is gone or is no longer supervised, both
// mean the same thing: whatever was written down does not describe this board
// any more, so it is dropped and you get the board.
async function restoreWhereYouWere() {
  let view = "", card = "";
  try {
    view = localStorage.getItem("atrium.view") || "";
    card = localStorage.getItem("atrium.term") || "";
  } catch (e) { return; }
  rlog("boot. remembered view", view || "(none)", "card", card || "(none)");
  if (!VIEWS.includes(view) || view === "board") return;
  switchView(view);
  if (view !== "terms" || !card) return;

  // Popped out since, in which case the board must not take it back: two
  // views on one terminal both writing input is what popping out exists to
  // prevent.
  if (poppedOut(card)) return;
  waitAndAttach(card);
}

// How long to keep waiting for a remembered session to come back.
//
// THE RACE THIS EXISTS FOR: the daemon restarting is what reloads the board,
// and the daemon answers HTTP before its fixtures have started. So the board
// comes up, asks about the card it was reading, and is told it is not
// supervised, which is true and is about to stop being true. Asking once put
// you on an empty terminals page every single restart.
//
// Ninety seconds, because a fixture is a real agent starting: claude resuming
// a long conversation takes a while, and the cost of waiting is one request a
// second at a moment when nothing else is happening.
const restoreWaitFor = 90 * 1000;

// How long the board waits for popped-out windows to say they exist.
//
// Every one of them answers `solo-who` immediately, so this is a message
// between two documents in one browser and nothing more. The number is
// generous because being wrong costs a duplicate alert and a terminal stolen
// out of somebody's window, and being slow costs half a second at page load.
// A popped-out window claims its card as its first act now, so the answer is
// back almost immediately. The margin is for the case that actually bit: a
// restart reloads both documents at once, the board is the smaller page and
// finishes first, and it must not get to its first alert before the other one
// has drawn breath.
const soloRollCall = 900;

// Reconnecting is the one thing on this page that happens while nobody is
// watching and cannot be reproduced on demand: it needs the daemon to go away.
// So it says what it is doing, on the console, with a timestamp.
//
// Left ON rather than behind a flag. It is a handful of lines per restart, it
// costs nothing, and the alternative is asking somebody to turn on logging and
// then restart the daemon again to reproduce what they just saw.
function rlog() {
  const t = new Date().toISOString().slice(11, 23);
  console.log("[atrium " + t + "]", ...arguments);
}

// One waiter at a time. Every teardown starts one, and a restart produces
// several in a row: the socket closing, the poll that finds nothing to attach
// to, and the runner-exited event are three separate teardowns of the same
// outage, and three loops all calling `openTerm` on the same card would attach
// it and then tear it down again as each of the others noticed.
let waitingFor = "";

async function waitAndAttach(card) {
  if (waitingFor === card) { rlog("already waiting for", card); return; }
  waitingFor = card;
  rlog("waiting for", card, "to come back");
  try { await waitLoop(card); } finally { if (waitingFor === card) waitingFor = ""; }
}

async function waitLoop(card) {
  const until = Date.now() + restoreWaitFor;
  let said = false, tries = 0;
  while (Date.now() < until) {
    // Somebody got there first: attached something by hand, went to another
    // view, or popped this one out. Any of those is a decision, and a restore
    // that overrode it would be the board arguing with you.
    if (term || termTask) { rlog("stopped: something else attached"); termWait(""); return; }
    if (!isViewing("terms")) { rlog("stopped: not on the terminals view"); termWait(""); return; }
    if (poppedOut(card)) { rlog("stopped: it is in its own window"); termWait(""); return; }

    let task = null, err = "";
    try { task = await api("/v1/tasks/" + encodeURIComponent(card)); }
    catch (e) { err = e.message; }
    tries++;
    rlog("try", tries, err ? "no answer: " + err
      : "supervised=" + !!(task && task.supervised) +
        " archived=" + !!(task && task.archived_at));
    // Gone for good rather than not back yet. A card that has been archived is
    // never going to be supervised again, so waiting out the ninety seconds
    // would be waiting for nothing.
    if (task && task.archived_at) { rlog("stopped: the card is archived"); termWait(""); return; }
    if (task && task.supervised) { rlog("it is back. attaching"); termWait(""); openTerm(task); return; }
    // Named as soon as there is a name to use, which is the first answer that
    // comes back. Before that it is `the session`, which is still better than
    // an empty pane saying nothing attached while something is happening.
    if (!said || task) {
      said = true;
      termWait("atrium is restarting. waiting for " + waitName(task) + " to come back…");
    }
    await new Promise(r => setTimeout(r, 1000));
  }
  // Ninety seconds and it never came back. Saying nothing would leave the
  // spinner up forever.
  rlog("gave up after", restoreWaitFor / 1000, "seconds");
  termWait("");
  toast("could not get back to your session", "it has not come back. pick one from the list.");
}

// Which view is on screen, read off the page. The same question
// `rememberWhereYouAre` asks, and asked the same way for the same reason.
function isViewing(name) {
  const el = document.getElementById(name);
  return !!el && !el.hidden;
}

// Every dialog says when it closed and what closed it.
//
// `close` fires for all of them: the close button, escape, a form submit, and
// a script calling `close()`. The stack tells them apart, which is the whole
// point: a settings dialog that shuts by itself while an agent is typing is
// not something that can be caught by watching.
document.querySelectorAll("dialog").forEach(d => {
  d.addEventListener("close", () => {
    rlog("dialog closed:", d.id, "\n" + new Error().stack);
  });
});

