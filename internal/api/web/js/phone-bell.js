// u-026: ON A PHONE A TOAST IS A NUDGE OF THE BELL, NOT A BOX.
//
// A toast drew over a large part of the terminal, and on a phone the terminal is the screen. So on a phone the
// toast is never drawn: `toast` (wrapped in toast-log.js) records the entry as it always did, and this file
// shakes the bell once. The count on the bell goes up because the entry is in the log, and tapping the bell
// opens that log, where each entry still lands where the toast's click would have.
//
// The same goes for the held message notice (heldline.js): a phone shows the envelope and its count instead of
// a box in the pane.
//
// Two bells exist and only one is ever on screen. The header's own bell is the one while the header is showing.
// `#phone-bell` is a small fixed badge in the top right corner that takes over whenever the header's bell has no
// box (the full screen terminal hides the whole header), so the alerts are never out of reach. Nothing here
// polls: it repaints when the log's badge does, when a held count arrives, and when the body's class or the
// window's size changes.
//
// NOT NUDGED, ON PURPOSE: the hub restart gate's two toasts (js/hubrestart.js). They are built there, not by
// `toast`, and they carry the pause and resume buttons, which are the only way to act on the restart. They stay
// as toasts on a phone as well.

const PHONE_NUDGE_MQ = "(max-width: 900px) and (pointer: coarse)";

function phoneToasts() {
  return !!(window.matchMedia && window.matchMedia(PHONE_NUDGE_MQ).matches);
}

// What `toast` answers on a phone, so a caller that takes the return value back finds the shape it expects.
const phoneToastStub = {
  dismiss() {}, again() {}, answered() {}, remove() {}, dataset: {},
  classList: { contains() { return false; }, add() {}, remove() {} }
};

// Shakes one glyph. A class plus a counter: the class is what the CSS animates and the counter is what says a
// nudge happened even after the class has gone.
function nudgeEl(el) {
  if (!el) return;
  el.dataset.nudges = String(Number(el.dataset.nudges || 0) + 1);
  el.classList.remove("nudge");
  void el.offsetWidth;
  el.classList.add("nudge");
  clearTimeout(el._nudgeT);
  el._nudgeT = setTimeout(() => el.classList.remove("nudge"), 900);
}

// `which` is "bell" or "mail". Both bells are nudged, and whichever is hidden costs nothing.
function nudgeBell(which) {
  const sel = which === "mail"
    ? ["#toastlog-open .hmsg", "#phone-bell .pb-mail"]
    : ["#toastlog-open .glyph", "#phone-bell .pb-bell"];
  sel.forEach(s => nudgeEl(document.querySelector(s)));
}

// The header's bell has a box, or it does not.
function headerBellShown() {
  const b = document.getElementById("toastlog-open");
  return !!(b && b.getClientRects().length);
}

let phoneHeldN = 0;

// Repaints both bells' extras from what is known: the unseen count, the notify-off glyph and the held count.
function phoneBellPaint() {
  const pb = document.getElementById("phone-bell");
  if (!pb) return;
  const hb = document.getElementById("toastlog-open");
  const cnt = hb && hb.querySelector(".count");
  const n = cnt ? cnt.textContent : "";
  const set = (sel, txt) => { const e = pb.querySelector(sel); if (e) e.textContent = txt; };
  set(".pb-bell .pb-n", n);
  const g = hb && hb.querySelector(".glyph");
  if (g) set(".pb-bell .glyph", g.textContent);
  const label = hb ? hb.getAttribute("aria-label") : "";
  if (label) { pb.setAttribute("aria-label", label); pb.dataset.tip = label; }
  pb.querySelector(".pb-bell").classList.toggle("has", !!n);
  const mail = pb.querySelector(".pb-mail");
  mail.hidden = phoneHeldN <= 0;
  set(".pb-mail .pb-n", phoneHeldN > 0 ? String(phoneHeldN) : "");
  const hm = hb && hb.querySelector(".hmsg");
  if (hm) {
    hm.hidden = phoneHeldN <= 0;
    const hn = hm.querySelector(".pb-n");
    if (hn) hn.textContent = phoneHeldN > 0 ? String(phoneHeldN) : "";
  }
  // One bell on screen: this badge only while the header's own has no box.
  pb.hidden = !phoneToasts() || headerBellShown();
}

// A held message count, from `followHeldLine`. A rise, or the first one, nudges the envelope.
function phoneHeld(count) {
  const next = Math.max(0, Number(count) || 0);
  const rose = next > phoneHeldN;
  phoneHeldN = next;
  phoneBellPaint();
  if (rose) nudgeBell("mail");
}

// The auto mode dot rides on the bell, so its words ride on the bell's label too. Phone only, so a desktop's
// bell says exactly what it always did.
function phoneBellAutoNote() {
  if (!phoneToasts() || typeof globalAuto === "undefined" || !globalAuto) return "";
  return ". auto mode is on, every request is approved without asking";
}

addEventListener("DOMContentLoaded", () => {
  phoneBellPaint();
  if (window.MutationObserver) {
    new MutationObserver(phoneBellPaint).observe(document.body, { attributes: true, attributeFilter: ["class"] });
  }
});
addEventListener("resize", phoneBellPaint);
