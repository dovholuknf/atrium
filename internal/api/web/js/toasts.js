// ── toasts ──────────────────────────────────────────────
// Attaches to a task's terminal when atrium owns its runner. Returns whether
// the click was DEALT WITH, so a caller can fall back to the tab it would
// otherwise have opened.
//
// The popped-out guard is `attachTask`'s and it belongs here too. This is the
// path a toast and a desktop notification land on, so without it clicking an
// alert for a card that is in a window of its own pulled the terminal out of
// that window and into the board's pane, silently, and answered `true` as
// though attaching had been the right thing to do. Two views onto one terminal
// is the situation `docs/supervision-design.md` says nothing arbitrates, which
// is exactly why `attachTask` refuses it.
//
// Dealt with covers the raise: you asked to be taken to the session and you
// are being taken to it, in the window that has it.
async function attachIfSupervised(taskID) {
  try {
    const t = await api(`/v1/tasks/${taskID}`);
    if (!t.supervised) return false;
    if (poppedOut(taskID)) {
      // Nothing more is said. Raised, the window coming to the front says it.
      // Opened, the new window does. Every other answer has already said what
      // happened.
      await popOutTask(taskID);
      return true;
    }
    openTerm(t);
    return true;
  } catch (e) { return false; }
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
}

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
  const last = host.lastElementChild;
  if (last && !last.classList.contains("leaving") &&
      last.dataset.sig === title + " " + (body || "")) {
    last.dataset.n = String(Number(last.dataset.n || 1) + 1);
    const n = last.querySelector(".ntimes");
    if (n) n.textContent = "×" + last.dataset.n;
    // Back to the top of its life, since it is being said again NOW.
    if (last.again) last.again();
    return last;
  }

  const el = document.createElement("div");
  el.dataset.sig = title + " " + (body || "");
  el.className = "toast";
  // A toast tied to a pending item is cleared when that item is answered,
  // whether it was answered here or anywhere else.
  if (key) el.dataset.key = key;
  el.innerHTML = `<div class="body"><b></b><span class="ntimes"></span><span class="what"></span></div>
    <button class="copy" title="copy this message">&#128203;</button>
    <button class="x" title="dismiss">&times;</button>`;
  el.querySelector("b").textContent = title;
  el.querySelector(".what").textContent = body || "";
  const dismiss = () => {
    el.classList.add("leaving");
    setTimeout(() => el.remove(), 200);
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
    // If atrium owns the runner, the terminal is where the work is. Landing
    // in the perms tab would mean answering and then going to find it.
    if (taskFor && await attachIfSupervised(taskFor)) return;
    if (goTo) switchView(goTo);
    // Land on the actual request, not just the right tab. The list may still
    // be rendering, so wait a frame before hunting for the card.
    if (key) setTimeout(() => focusPerm(key), 60);
  });
  el.dismiss = dismiss;
  // Everything clears itself. A permission gets longer since it blocks an
  // agent. The pending count in the tab is the durable signal, not the toasts.
  //
  // Held so a collapsed repeat can restart it. A message said again is new
  // again, and inheriting the first one's remaining second would have the
  // count tick up on a toast that vanishes as you look at it.
  const life = goTo === "perms" ? 30000 : 9000;
  let timer = setTimeout(dismiss, life);
  el.again = () => { clearTimeout(timer); timer = setTimeout(dismiss, life); };

  host.appendChild(el);
  // After the content is in, so the host is put back on top of whatever is
  // open at the moment there is something to show.
  raiseToasts();
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
  const cap = innerWidth <= PHONE ? 1 : 3;
  const plain = () => [...host.children].filter(c => !c.classList.contains("sticky"));
  for (let p = plain(); p.length > cap; p = plain()) p[0].remove();
  return el;
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
    if (!liveKeys.has(el.dataset.key) && el.dismiss) el.dismiss();
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

