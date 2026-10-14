// "Messages waiting, clear the line": a small notice in the attached terminal pane while peer messages are queued
// BECAUSE the operator's own typed line is holding them (the injector only types into an empty, quiet line).
//
// The signal is the card's live activity, already on the list the terminals view reads: `held_peer` with
// `held_for` "line". A turn or dialog hold is the card chip's business and shows nothing here. No timer and no
// request: it is repainted whenever the list is, and goes when the next list no longer holds anything.
//
// It is an absolute overlay in `.term-body`, so it never resizes the grid or refits and takes no focus. Hovering it
// opens a menu with "Let it through", below. Guests and unsupervised cards get nothing.
function heldLineText(count) {
  const n = Math.max(1, Number(count) || 1);
  return "&#9993;&#xFE0E; " + n + (n === 1 ? " message" : " messages") + " waiting, clear the line to deliver";
}

function followHeldLine(list) {
  const el = document.getElementById("t-heldline");
  if (!el) return;
  let show = false, count = 1;
  if (typeof termTask !== "undefined" && termTask && !isGuest() && Array.isArray(list)) {
    const t = list.find(x => x && sameCard(x.id, termTask.id));
    const a = t && t.supervised !== false && t.activity;
    if (a && a.held_peer && a.held_for === "line") { show = true; count = a.held_count; }
  }
  // A phone draws no notice box: the envelope on the bell nudges and carries the count. See js/phone-bell.js.
  if (typeof phoneToasts === "function" && phoneToasts()) {
    if (!el.hidden) { el.hidden = true; el.innerHTML = ""; }
    phoneHeld(show ? count : 0);
    return;
  }
  if (typeof phoneHeld === "function") phoneHeld(0);
  if (!show) { if (!el.hidden) { el.hidden = true; el.innerHTML = ""; } return; }
  const html = '<span class="hl-text">' + heldLineText(count) + "</span>" +
    '<span class="hl-menu"><button type="button" class="hl-item" data-act="let-through">Let it through</button></span>';
  // Only the words move when the count does, so a menu the pointer is in is not rebuilt under it.
  const words = el.querySelector(".hl-text");
  if (!words) el.innerHTML = html;
  else if (words.innerHTML !== heldLineText(count)) words.innerHTML = heldLineText(count);
  el.hidden = false;
}

// "Let it through": the operator's line is holding messages and they want them now. The menu opens on hover, so the
// chip stays out of the way until it is aimed at. The terminal has focus and the chip never takes it: the button
// keeps the mousedown from moving it.
//
// 1. The line atrium thinks is typed is copied to the clipboard here, in the click, because only a click may write it.
// 2. If atrium cannot read the line exactly, that is all that happens, and a toast says so. A retype of a guess
//    would put the wrong words in front of the operator.
// 3. Otherwise the room clears the line, delivers every waiting message and types the line back without Enter, one
//    step under the input lock so a key pressed meanwhile is held until it is done. See internal/daemon/letthrough.go.
let letThroughBusy = false;
async function letItThrough() {
  const t = typeof termTask !== "undefined" ? termTask : null;
  if (!t || letThroughBusy) return;
  letThroughBusy = true;
  try {
    const base = "/v1/tasks/" + encodeURIComponent(t.id);
    let s = null;
    try { s = await api(base + "/typing"); } catch (e) { toast("could not read your line", (e && e.message) || String(e)); return; }
    const line = s && s.line ? String(s.line) : "";
    let copied = false;
    if (line) {
      try { await navigator.clipboard.writeText(line); copied = true; } catch (e) {}
    }
    if (!line || !s.exact) {
      toast(copied ? "your text is on the clipboard" : "could not read your line",
        copied ? "atrium cannot be sure of the line, so it left it alone" : "nothing was cleared");
      return;
    }
    try {
      await api(base + "/let-through", { method: "POST", body: JSON.stringify({ line }) });
    } catch (e) {
      toast("could not let it through", (copied ? "your text is on the clipboard. " : "") + ((e && e.message) || String(e)));
    }
  } finally {
    letThroughBusy = false;
  }
}

document.addEventListener("mousedown", e => {
  if (e.target && e.target.closest && e.target.closest("#t-heldline")) e.preventDefault();
});
document.addEventListener("click", e => {
  const b = e.target && e.target.closest && e.target.closest("#t-heldline [data-act=let-through]");
  if (b) letItThrough();
});

// The menu opens by CSS on hover or focus, so it is fitted when either happens, once it is showing (js/popfit.js).
for (const ev of ["pointerover", "focusin"]) {
  document.addEventListener(ev, e => {
    const hl = e.target.closest && e.target.closest(".heldline");
    const menu = hl && hl.querySelector(".hl-menu");
    if (menu) requestAnimationFrame(() => popFly(menu));
  });
}
