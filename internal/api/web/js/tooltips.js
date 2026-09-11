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

function hideTip() { tipEl.classList.remove("on"); tipSoon(null); }

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

document.addEventListener("pointerover", e => {
  const help = e.target.closest && e.target.closest(".help");
  if (!help) return;
  // Re-read when it fires rather than captured now, so a tooltip whose text
  // was rewritten while you hovered shows what it says at the moment it
  // appears.
  tipSoon(() => placeTip(help, help.dataset.tip));
});
document.addEventListener("pointerout", e => {
  if (e.target.closest && e.target.closest(".help")) hideTip();
});
document.addEventListener("focusin", e => {
  const help = e.target.closest && e.target.closest(".help");
  if (help) { tipSoon(null); placeTip(help, help.dataset.tip); }
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

