// ── tooltips ────────────────────────────────────────────
// One floating panel, measured and then placed. It prefers to sit below and
// left-aligned with its bubble, and flips or slides whenever that would put it
// off screen, so a bubble near the right edge or the bottom still reads.
const tipEl = document.getElementById("tip");

function placeTip(anchor, text) {
  tipEl.textContent = text;
  tipEl.classList.add("on");
  // Measure first: size depends on the text, and position depends on size.
  tipEl.style.left = "0px";
  tipEl.style.top = "0px";
  const pad = 10;
  // Never wider than the window, so the panel cannot introduce a horizontal
  // scrollbar no matter how long the text is.
  tipEl.style.maxWidth = Math.min(420, window.innerWidth - pad * 2) + "px";
  const a = anchor.getBoundingClientRect();
  const t = tipEl.getBoundingClientRect();

  let left = a.left;
  // Slide back inside rather than letting it spill off the right edge.
  if (left + t.width > window.innerWidth - pad) left = window.innerWidth - t.width - pad;
  if (left < pad) left = pad;

  // Below by default, above when there is no room below but there is above.
  let top = a.bottom + 9;
  const roomBelow = window.innerHeight - a.bottom;
  if (roomBelow < t.height + pad && a.top > t.height + pad) top = a.top - t.height - 9;
  if (top < pad) top = pad;

  tipEl.style.left = Math.round(left) + "px";
  tipEl.style.top = Math.round(top) + "px";
}

function hideTip() { tipEl.classList.remove("on"); tipSoon(null); tipAt = null; }

// HALF A SECOND OF STAYING PUT, then it appears.
//
// Instant was right for a control you went to on purpose and wrong for
// everything else, and the board is dense with these: moving the pointer
// across a card set off three bubbles on the way to whatever you were
// reaching for. A delay is the difference between hovering something and
// passing over it, and it is the only thing that can tell them apart.
//
// Keyboard focus is NOT delayed. Tabbing to a `?` is deliberate every time,
// and waiting half a second after a keystroke reads as the page being slow.
const tipAfter = 500;
let tipTimer = 0;

function tipSoon(fn) {
  clearTimeout(tipTimer);
  if (fn) tipTimer = setTimeout(fn, tipAfter);
}

// EVERY `data-tip` ON THE BOARD, not only the `?` bubbles. A native `title`
// draws the browser's own white box in the system font whatever the skin is,
// so the board does not use one: `scripts/check-titles.sh` fails the build on
// a new one. The listeners are on the document, so nothing is wired per render
// and a card repainted every second costs nothing here.
//
// On a phone a tap fires pointerout on lift, before the timer, so a tap never
// opens one and a long press does.
function tipAnchor(node) {
  const a = node && node.closest && node.closest("[data-tip]");
  return a && a.dataset.tip ? a : null;
}
let tipAt = null;

function showTip(anchor) {
  // Repainted out from under the pointer while it waited: no pointerout came.
  if (!anchor.isConnected || !anchor.dataset.tip) return hideTip();
  tipAt = anchor;
  placeTip(anchor, anchor.dataset.tip);
}

document.addEventListener("pointerover", e => {
  let a = tipAnchor(e.target);
  if (a === tipAt && a) return;
  // A card's details popover is that card's one hover (js/peek.js).
  if (a && typeof peekOwnsTip === "function" && peekOwnsTip(a, e)) a = null;
  if (!a) { if (tipAt) hideTip(); return; }
  // Re-read when it fires rather than captured now, so a tooltip whose text
  // was rewritten while you hovered shows what it says at the moment it
  // appears.
  tipSoon(() => showTip(a));
  tipAt = a;
});
document.addEventListener("pointerout", e => {
  // Moving between the children of one anchor is not leaving it.
  const a = tipAnchor(e.target);
  if (a && a.contains(e.relatedTarget)) return;
  if (a) hideTip();
});
// A click is the answer to what the tooltip was explaining. The native one
// goes away on press too.
document.addEventListener("pointerdown", hideTip, true);
document.addEventListener("focusin", e => {
  const a = tipAnchor(e.target);
  if (!a) return;
  // A `?` opens on any focus, which is how it has always been read. Anything
  // else only for the keyboard, or every click on a button would pin its
  // tooltip up until the pointer left.
  if (!a.classList.contains("help") && !e.target.matches(":focus-visible")) return;
  tipSoon(null);
  showTip(a);
});
document.addEventListener("focusout", hideTip);
window.addEventListener("scroll", e => { if (!fromTerminal(e)) hideTip(); }, true);
window.addEventListener("resize", hideTip);

// The copy mode button lives in static markup, so it has no text until
// something paints it. Painting only on attach left it blank until clicked.
paintCopyMode();

wireFilters("rules-seg", "rules-q", paintRules);
wireFilters("hist-seg", "hist-q", paintHistory);
// The show pills are built and wired by paintStackShow, since each is a
// toggle rather than one of a set. Only the search box needs wiring here.
paintStackShow();
document.getElementById("stack-q").addEventListener("input", paintStack);
paintStackSort();
paintGroupSegs();
wireSeg("d-ev-seg", paintTimeline);
loadGlobalAuto();
rememberOpen("rules-d", "atrium.rules.open");
rememberOpen("hist-d", "atrium.history.open");

