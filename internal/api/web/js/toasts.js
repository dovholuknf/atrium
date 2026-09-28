// ── toasts ──────────────────────────────────────────────
// WHERE A CLICK ON AN ALERT LANDS. One function, and every alert reaches it: a
// toast, a row in the toast log, a plain desktop notification, and the service
// worker's, whether the board was in front, behind something, or not open.
//
//   - about one card with a live terminal: that terminal, attached and focused,
//     in the window that has it if the card is popped out
//   - about one card with no live terminal: a pending request on it lands on
//     the request, and anything else on the card, with its detail open
//   - about no one card: the view it names, on the request when there is one
//   - about nothing: nowhere
//
// A permission lands on the terminal too, because a supervised session shows
// the request in its own pane with the buttons that answer it.
//
// THE CARD CAN BE AHEAD OF ITS TERMINAL. "X is on the board" fires on the poll
// that first lists the card, and a session atrium is launching is listed a
// moment before its runner is. So a card that is new and not yet over is asked
// again for a few seconds before the click settles for its detail. Landing on
// the stack for that race was the bug that asked for this function.
//
// The popped-out guard is `attachTask`'s: an alert for a card in a window of
// its own raises that window rather than pulling the terminal into the board.
const landWaitMs = 8000;
const landStepMs = 400;
let landSeq = 0;
async function landOnAlert(taskFor, goTo, key) {
  // The latest click wins. A second click while the first is still waiting on
  // a terminal must not have the first land on top of it later.
  const seq = ++landSeq;
  if (termOnly()) { landFromSolo(taskFor, goTo, key); return; }
  const card = taskFor ? await landableCard(taskFor, () => seq !== landSeq) : null;
  if (seq !== landSeq) return;
  if (card && card.supervised && !card.offline) {
    await attachTask(card.id);
    if (term && !poppedOut(card.id)) term.focus();
    return;
  }
  if (key && (goTo === "perms" || !card)) {
    switchView("perms");
    // The list may still be rendering, so wait a frame before hunting for the
    // request.
    setTimeout(() => focusPerm(key), 60);
    return;
  }
  if (card) {
    if (goTo) switchView(goTo);
    if (typeof newCardClear === "function") newCardClear(card.id);
    try { await openTask(card.id); } catch (e) { toast("could not open that card", e.message); }
    return;
  }
  if (goTo) switchView(goTo);
}

// The card an alert is about, once it is worth landing on. Answers null when
// it is gone.
//
// Waits while the card is new and its terminal may still be coming: missing
// (the hub can list a card a room has not answered for yet), or not yet
// supervised while it is fresh and not over. Anything else answers at once.
async function landableCard(id, superseded) {
  const until = Date.now() + landWaitMs;
  for (;;) {
    let t = null;
    try { t = await api(`/v1/tasks/${id}`); } catch (e) {}
    if (t && (t.supervised || !terminalComing(t))) return t;
    if (Date.now() >= until || superseded()) return t;
    await new Promise(r => setTimeout(r, landStepMs));
  }
}

// Whether a card without a terminal is about to have one: seen in the last
// minute, not over, not in the inbox, and not a card on another machine that
// is offline.
function terminalComing(t) {
  if (t.offline || ["done", "dead", "shelved", "backlog"].includes(t.status)) return false;
  const born = Date.parse(t.created_at || "");
  return !isNaN(born) && Date.now() - born < 60000;
}

// A popped-out window is one card. An alert for that card lands on its own
// terminal. An alert for anything else belongs to the board, which is asked to
// land it, and brought forward if this window opened it.
function landFromSolo(taskFor, goTo, key) {
  if (!taskFor || sameCard(taskFor, soloID)) {
    window.focus();
    if (term) term.focus();
    return;
  }
  if (soloBus) soloBus.postMessage({ type: "land", taskFor, goTo: goTo || "", key: key || "" });
  try { if (window.opener && !window.opener.closed) window.opener.focus(); } catch (e) {}
}

// A desktop notification clicked with no board open. The service worker opens
// one at `/?land=<card>&view=<view>&key=<request>`, and this lands it once the
// board is back where it was, then takes the query off the address so a reload
// does not land again.
function landFromURL() {
  const q = new URLSearchParams(location.search);
  if (!q.has("land") && !q.has("view")) return;
  const taskFor = q.get("land") || "", goTo = q.get("view") || "", key = q.get("key") || "";
  history.replaceState(history.state, "", location.pathname + location.hash);
  landOnAlert(taskFor, goTo, key);
}

// Which modal is on top. Modals stack by the order showModal was called, and
// that order cannot be read back from the DOM, so it is recorded here. Two open
// at once is normal: a confirmation over a list, or the steps over the hooks
// they came from.
const modalStack = [];
(function trackModals() {
  const proto = window.HTMLDialogElement && HTMLDialogElement.prototype;
  if (!proto || !proto.showModal) return;
  const show = proto.showModal;
  proto.showModal = function () {
    const i = modalStack.indexOf(this);
    if (i >= 0) modalStack.splice(i, 1);
    modalStack.push(this);
    const r = show.apply(this, arguments);
    raiseToasts();
    return r;
  };
  // Capture, because `close` does not bubble. A dialog underneath can be
  // closed out of order, so it is removed wherever it sits.
  document.addEventListener("close", e => {
    const i = modalStack.indexOf(e.target);
    if (i >= 0) modalStack.splice(i, 1);
    raiseToasts();
  }, true);
})();

// Puts the toast host where it will be both seen AND clickable.
//
// A modal dialog is in the browser's top layer, so nothing outside it draws
// over it at any z-index, and it makes everything outside itself inert. Those
// two rule out both shortcuts: z-index draws under it, and a popover draws
// over it but takes no clicks, which is worse than hidden because the buttons
// look live. Inside the topmost modal is the only place that works.
//
// Position is fixed either way, so moving does not change where they appear.
function raiseToasts() {
  let top = null;
  for (let i = modalStack.length - 1; i >= 0; i--) {
    if (modalStack[i].open) { top = modalStack[i]; break; }
  }
  const parent = top || document.body;
  // THE TOOLTIP TRAVELS WITH THEM, for exactly the same reason and it was
  // missed the first time. Every `?` in the settings dialog and on the runners
  // page drew its panel UNDERNEATH the dialog, so hovering one did nothing
  // visible: the text was rendered, positioned correctly, and in the wrong
  // layer. Those are the bubbles carrying the longest explanations on the
  // board, which is to say the ones that had to work.
  ["toasts", "tip"].forEach(id => {
    const host = document.getElementById(id);
    if (host && host.parentElement !== parent) parent.appendChild(host);
  });
  placeToasts();
}

// OVER A TERMINAL THE STACK HANGS FROM THE TOP. Bottom right is where a
// terminal keeps its input line and its status bar, so a toast there sat on
// exactly what was being typed. Everywhere else keeps bottom right.
//
// Just under the terminal's bar and inside the pane's right edge, rather than
// the window's top corner, which is the header's. Anything floating over the
// top of the pane (the paste indicator, the find bar) pushes it further down,
// so the stack never covers them.
//
// A class and two variables, not a new element or a rebuilt stack, so a view
// switch with toasts up moves them without replaying their entrance.
function placeToasts() {
  const host = document.getElementById("toasts");
  const pane = document.getElementById("term-pane");
  if (!host) return;
  const onTerm = !!pane && (termOnly() || isViewing("terms"));
  host.classList.toggle("top", onTerm);
  if (!onTerm) return;
  const r = pane.getBoundingClientRect();
  let below = r.top;
  pane.querySelectorAll(".term-bar, #t-pasting, #t-find").forEach(el => {
    if (el.hidden) return;
    const b = el.getBoundingClientRect();
    if (b.height) below = Math.max(below, b.bottom);
  });
  host.style.setProperty("--toast-top", Math.round(below + 12) + "px");
  host.style.setProperty("--toast-right", Math.max(8, Math.round(innerWidth - r.right + 12)) + "px");
}
addEventListener("resize", placeToasts);

// Typing in a guarded dialog marks it as holding something not yet saved.
//
// A flag rather than a comparison against the opening values: the point is
// only to tell "you have been editing this" from "you opened it and read it",
// and typing a character then deleting it is close enough to editing.
document.addEventListener("input", e => {
  const dlg = e.target.closest && e.target.closest("dialog[data-guard]");
  if (dlg) dlg.dataset.dirty = "1";
}, true);

// Opening one starts it clean, so edits abandoned last time are not still
// being guarded against this time.
if (window.MutationObserver) {
  new MutationObserver(records => {
    records.forEach(r => {
      const d = r.target;
      if (d.matches && d.matches("dialog[data-guard]") && d.open) delete d.dataset.dirty;
    });
  }).observe(document.body, { subtree: true, attributes: true, attributeFilter: ["open"] });
}

// Saving needs no counterpart to the above. The flag is only ever read while
// a dialog is open, and saving closes it, so a saved form is never asked
// about. Opening it again starts clean.

// LIGHT-DISMISS, AND WHICH DIALOGS GET IT.
//
// Clicking outside a dialog closes it. This is one rule for the board rather
// than a handler per control: a dialog that writes as you change it, or writes
// nothing at all, closes when you click away, because it is holding nothing you
// have not already been given. A `data-guard` dialog does NOT, and the
// attribute already says why: it holds edits kept only when you press save, so
// discarding them on a twitch of the pointer is the one thing that must not
// happen. Those keep their explicit close.
//
// THE TARGET IS THE DIALOG EITHER WAY. With `showModal` the backdrop belongs to
// the dialog element, so the usual `e.target === dlg` test reads a click on the
// dialog's own padded edge as outside, and the dialog shuts with the pointer
// well inside it. Comparing the click against `getBoundingClientRect` asks the
// question that was meant.
//
// The press and the release both have to be outside. Selecting text in a dialog
// and letting go past its edge is one click, reported on the dialog, at
// coordinates outside it, and it is the opposite of asking to leave.
let dlgDownOn = null;
document.addEventListener("pointerdown", e => { dlgDownOn = e.target; }, true);
document.addEventListener("click", e => {
  const dlg = e.target;
  if (dlgDownOn !== dlg) return;
  if (!dlg || dlg.tagName !== "DIALOG" || !dlg.open) return;
  if (dlg.hasAttribute("data-guard")) return;
  // A keyboard activation dispatches a click carrying no coordinates, which
  // reads as the top left of the window and so as outside nearly everything.
  if (!e.detail) return;
  const r = dlg.getBoundingClientRect();
  const inside = e.clientX >= r.left && e.clientX <= r.right &&
    e.clientY >= r.top && e.clientY <= r.bottom;
  if (!inside) dlg.close();
});

// Closes every open dialog, for a click that is leaving whatever is on screen.
//
// Asks first when one of them is holding edits nobody has saved. Throwing away
// a half-filled runner form because a toast arrived is worse than the toast
// being ignored.
//
// The open dialogs are captured before the question is asked, so the dialog
// doing the asking is not in the list and closing it is left to itself.
async function closeOpenDialogs() {
  const open = [...document.querySelectorAll("dialog[open]")]
    .filter(d => d.id !== "ask");
  if (!open.length) return true;
  // WHO ASKED. A dialog closing on its own is the kind of thing that gets
  // reported as "it just vanished", and by the time anybody looks there is
  // nothing left to inspect. The stack names the path.
  rlog("closing dialogs:", open.map(d => d.id).join(", "), "\n" + new Error().stack);

  const dirty = open.filter(d => d.dataset.dirty === "1");
  if (dirty.length) {
    const what = dirty.map(d => d.id).join(" and ");
    if (!await confirmUser("leave without saving?",
      `The <b>${esc(what)}</b> form has changes that have not been saved. ` +
      "Going where you clicked closes it and those changes go with it.",
      "discard them")) {
      return false;
    }
  }

  open.forEach(d => {
    try { d.close(); } catch (e) {}
  });
  return true;
}


// Returns the toast, so a caller that is about to say something better can
// take back what it just said.
function toast(title, body, goTo, key, taskFor) {
  const host = document.getElementById("toasts");

  // THE SAME THING TWICE IS ONE THING WITH A COUNT.
  //
  // Four alerts saying `the file exists` say nothing the first one did not,
  // and they cover the screen while doing it. The newest is checked rather
  // than the whole stack, so two alternating messages still both show: the
  // case worth collapsing is a repeat, not a recurrence.
  const last = toastQueue.length ? toastQueue[toastQueue.length - 1] : host.lastElementChild;
  if (last && !last.classList.contains("leaving") &&
      last.dataset.sig === title + "\0" + (body || "")) {
    last.dataset.n = String(Number(last.dataset.n || 1) + 1);
    const n = last.querySelector(".ntimes");
    if (n) n.textContent = "×" + last.dataset.n;
    // Back to the top of its life, since it is being said again NOW.
    if (last.again) last.again();
    return last;
  }

  const el = document.createElement("div");
  el.dataset.sig = title + "\0" + (body || "");
  el.className = "toast";
  // A toast tied to a pending item is cleared when that item is answered,
  // whether it was answered here or anywhere else.
  if (key) el.dataset.key = key;
  el.innerHTML = `<div class="body"><b></b><span class="ntimes"></span><span class="what"></span></div>
    <button class="copy" aria-label="copy this message" data-tip="copy this message">&#128203;</button>
    <button class="x" aria-label="dismiss" data-tip="dismiss">&times;</button>`;
  el.querySelector("b").textContent = title;
  el.querySelector(".what").textContent = body || "";
  const dismiss = () => {
    clearTimeout(timer);
    const q = toastQueue.indexOf(el);
    if (q >= 0) { toastQueue.splice(q, 1); return; }
    if (el.classList.contains("leaving")) return;
    el.classList.add("leaving");
    setTimeout(() => { el.remove(); showQueuedToasts(); }, 200);
  };
  el.addEventListener("click", async e => {
    if (e.target.classList.contains("x")) { dismiss(); return; }
    if (e.target.classList.contains("copy")) {
      // The text, not the toast. Errors and resume reasons are the ones worth
      // copying, and reading a path off a toast to retype it is the worst way
      // to get it.
      const text = body ? title + ": " + body : title;
      try {
        await navigator.clipboard.writeText(text);
        e.target.textContent = "✓";
        setTimeout(() => { e.target.innerHTML = "&#128203;"; }, 1200);
      } catch (err) {
        e.target.textContent = "✗";
      }
      return;
    }
    window.focus();
    // A toast about nothing in particular goes nowhere, and must not take an
    // open dialog down on its way out. See `landOnAlert`.
    if (!taskFor && !goTo && !key) { dismiss(); return; }
    // A toast on top of an open dialog lives INSIDE that dialog, because the
    // top layer is the only place anything can draw over one. So a click that
    // navigates has to take the dialog with it: without this the view changes
    // underneath and the dialog stays put, covering the thing you clicked to
    // go and look at.
    //
    // Refused means unsaved edits are being kept, so the toast stays too.
    // Dismissing it would leave nothing to click a second time.
    if (!await closeOpenDialogs()) return;
    dismiss();
    landOnAlert(taskFor, goTo, key);
  });
  el.dismiss = dismiss;
  // Everything clears itself. A permission gets longer since it blocks an
  // agent. The pending count in the tab is the durable signal, not the toasts.
  //
  // Held so a collapsed repeat can restart it. A message said again is new
  // again, and inheriting the first one's remaining second would have the
  // count tick up on a toast that vanishes as you look at it.
  const life = goTo === "perms" ? 30000 : 9000;
  // The clock starts when it is on screen, not when it was raised, so a toast
  // that waited its turn still gets its whole life.
  let timer = 0, end = 0, left = life;
  let hovered = false;
  el.hovered = () => hovered;
  const arm = ms => {
    clearTimeout(timer);
    end = Date.now() + ms;
    if (!hovered) timer = setTimeout(dismiss, ms);
  };
  el.again = () => { if (el.born) arm(life); };
  el.show = () => { el.born = Date.now(); host.appendChild(el); arm(life); };
  // ANSWERED IS NOT GONE. A card waiting on you that starts running again, or a
  // request answered from anywhere, used to take its toast down on that poll,
  // and a held message typed in as the turn ends does that inside a second. It
  // says so instead, and goes when an ordinary toast would.
  el.answered = () => {
    el.classList.add("answered");
    if (!el.born) return;
    const by = el.born + 9000 - Date.now();
    if (Date.now() + by < end) arm(Math.max(0, by));
  };
  // HOVERING HOLDS IT. Reading the toast is the one time it must not leave.
  el.addEventListener("mouseenter", () => {
    hovered = true;
    clearTimeout(timer);
    left = Math.max(0, end - Date.now());
  });
  el.addEventListener("mouseleave", () => {
    hovered = false;
    if (el.born) arm(Math.max(left, 2000));
    makeToastRoom();
  });

  // Never let the stack grow past what fits on screen. Three, not four: the
  // fourth was always half off the bottom on a laptop.
  //
  // One on a phone, and that number was measured rather than guessed. At 390
  // pixels a toast carrying a command is 194px tall, so three is 582 of an
  // 844 pixel screen, drawn over the permissions queue. The queue is the only
  // reason the board is open on a phone at all, so the announcements about it
  // do not get to bury it.
  //
  // A STICKY TOAST IS NOT COUNTED AND NOT EVICTED. The hub restart gate's two
  // are the only place to act on what they say, so the oldest ordinary toast
  // goes instead.
  //
  // A FULL STACK QUEUES, IT DOES NOT EVICT ON THE SPOT. Taking the oldest down
  // the moment a fourth arrived made a burst of alerts a blink: each one gone as
  // the next came in. The newcomer waits, and the oldest makes room once it has
  // been up long enough to read. See `makeToastRoom`.
  //
  // More up than the cap allows is a window that got narrower, a desktop stack
  // on a phone-sized screen. That excess goes at once, oldest first, as it did.
  for (let s = shownToasts(); s.length > toastCap(); s = shownToasts()) s[0].remove();
  if (shownToasts().length >= toastCap()) {
    toastQueue.push(el);
    makeToastRoom();
  } else {
    el.show();
  }
  // After the content is in, so the host is put back on top of whatever is
  // open at the moment there is something to show.
  raiseToasts();
  return el;
}

// Toasts raised while the stack was full, oldest first.
const toastQueue = [];
// How long a toast is up before a queued one may take its place.
const TOAST_READ_MS = 6000;
let toastRoomTimer = 0;

function toastCap() { return innerWidth <= PHONE ? 1 : 3; }

// The ordinary toasts on screen and staying: not sticky, not on the way out.
function shownToasts() {
  const host = document.getElementById("toasts");
  return host ? [...host.children].filter(c =>
    !c.classList.contains("sticky") && !c.classList.contains("leaving")) : [];
}

function showQueuedToasts() {
  while (toastQueue.length && shownToasts().length < toastCap()) toastQueue.shift().show();
  raiseToasts();
  makeToastRoom();
}

// With toasts waiting, the oldest one that has been read long enough and is not
// under the pointer goes. Otherwise this looks again when the next one will be.
function makeToastRoom() {
  clearTimeout(toastRoomTimer);
  if (!toastQueue.length) return;
  if (shownToasts().length < toastCap()) { showQueuedToasts(); return; }
  const now = Date.now();
  let soonest = Infinity;
  for (const el of shownToasts()) {
    if (!el.born || el.hovered()) continue;
    const due = el.born + TOAST_READ_MS;
    if (due <= now) { el.dismiss(); return; }
    soonest = Math.min(soonest, due);
  }
  if (soonest < Infinity) toastRoomTimer = setTimeout(makeToastRoom, soonest - now);
}

// Whether a pending request's own row is in front of you right now.
//
// Three things have to be true and each rules out a different way of being
// wrong about it: the document has to be the one you are looking at, the
// perms view has to be the one showing, and the row has to be inside the
// viewport rather than merely somewhere in a list that is taller than the
// screen. On a phone the third does most of the work, because two waiting
// requests are already taller than the screen.
//
// THREE ANSWERS, NOT TWO, AND THE THIRD IS THE ONE THAT WAS WRONG.
//
// A missing row is not an absent row. `refresh` polls the permissions list
// and repaints it from two separate requests, so on the first poll after a
// reload the alerting pass runs while `renderPerms` has not appended anything
// yet. Reading that as "not on screen" put a toast over the card you were
// looking at, once, on every single reload, which is the one moment you are
// most certainly looking at it. So say `unknown` and let the caller wait for
// the poll five seconds later, by which time there is a list to measure.
function permShowing(id) {
  if (!inForeground() || !isViewing("perms")) return "off-screen";
  const el = document.querySelector(`#perms-list .perm[data-id="${id}"]`);
  if (!el) return "unknown";
  const r = el.getBoundingClientRect();
  // Some of it, not all of it. A card taller than the screen can never be
  // fully inside one, and demanding that would make the tallest requests the
  // ones that always toast.
  return r.bottom > 0 && r.top < innerHeight ? "on-screen" : "off-screen";
}

// Scrolls a pending request into view and flashes it, so clicking a toast puts
// you on the thing that needs answering rather than near it.
function focusPerm(id) {
  const el = document.querySelector(`#perms-list .perm[data-id="${id}"]`);
  if (!el) return;
  el.scrollIntoView({ behavior: "smooth", block: "center" });
  el.classList.remove("flash");
  void el.offsetWidth;
  el.classList.add("flash");
  const cmd = el.querySelector(".cmd.edit");
  if (cmd) cmd.focus();
}

// Clears toasts whose subject is no longer pending. Called on every poll, so
// answering a request from anywhere retires its toast.
function reapToasts(liveKeys) {
  document.querySelectorAll("#toasts .toast[data-key]").forEach(el => {
    if (!liveKeys.has(el.dataset.key) && el.answered) el.answered();
  });
  toastQueue.slice().forEach(el => {
    if (el.dataset.key && !liveKeys.has(el.dataset.key)) el.dismiss();
  });
  reapNotifications(liveKeys);
}

// Takes down notifications that have expired or been answered.
//
// A permission notification with no expiry is sticky so a blocked agent does
// not scroll away unnoticed, and sticky means nothing removes it on its own.
function reapNotifications(liveKeys) {
  if (!swReg || !swReg.getNotifications) return;
  const now = Date.now();
  swReg.getNotifications().then(list => {
    list.forEach(n => {
      const d = n.data || {};
      // The worker keeps its own timer and sweeps on every wake-up, but a
      // browser can kill it at any time. This runs every poll while a page is
      // open.
      if (d.expireAt && now >= d.expireAt) { n.close(); return; }
      // Anything still pending is left alone, and so is a summary of several,
      // which names no one subject and so cannot be said to be answered.
      const subject = d.subject || d.permId;
      if (subject && !liveKeys.has(subject)) n.close();
    });
  }).catch(() => {});
}

