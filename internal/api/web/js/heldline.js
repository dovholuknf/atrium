// "Messages waiting, clear the line": a small notice in the attached terminal pane while peer messages are queued
// BECAUSE the operator's own typed line is holding them (the injector only types into an empty, quiet line).
//
// The signal is the card's live activity, already on the list the terminals view reads: `held_peer` with
// `held_for` "line". A turn or dialog hold is the card chip's business and shows nothing here. No timer and no
// request: it is repainted whenever the list is, and goes when the next list no longer holds anything.
//
// It is an absolute overlay in `.term-body`, so it never resizes the grid or refits, takes no focus and no
// pointer events, and on a phone sits in the body above the key bar. Guests and unsupervised cards get nothing.
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
  if (!show) { if (!el.hidden) { el.hidden = true; el.innerHTML = ""; } return; }
  const html = heldLineText(count);
  if (el.innerHTML !== html) el.innerHTML = html;
  el.hidden = false;
}
