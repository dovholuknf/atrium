// ── a terminal remembers where you left it ───────────────────────────────────
//
// Leaving a terminal (another card, another tab, the window losing focus) writes down where you were, and coming back
// puts you there with what is new marked, the way Slack and Mattermost do: a divider at the first line written after
// you left and a pill counting the lines below the view. The setting `atrium.termReturn` picks between that ("left",
// the default) and the bottom, which is where a terminal always opened before. The divider is drawn either way, so
// with "bottom" it is still there in the scrollback.
//
// WHAT COUNTS AS NEW. A runner like claude redraws its own screen in place (a spinner, a status line, the input box)
// and never stops, so "the buffer changed" would be true every frame. What is counted is the cursor's ABSOLUTE ROW:
// `baseY + cursorY` when you left, against now. A repaint moves the cursor up and back to the same row and adds
// nothing, while every line appended below it moves the row down by one. The divider sits at the cursor's row when
// that row was empty (the next line written lands on it) and one below it when it held text (an input box that the
// new output pushes down). Limits, said plainly: a live region that grows (a taller input box, a new status line)
// counts its growth as new lines, and a runner that scrolls the whole screen to redraw it is counted as it scrolled.
//
// STAYING ON ITS LINE. While the page is up the position is an xterm marker, which follows its line through a
// resize's reflow, through scrollback trimming, and goes to line -1 when the line is gone. A marker does not survive
// `term.reset()` (a reattach replays the scrollback into a reset terminal), a reload, or a closed terminal, so the
// same position is also kept as a FINGERPRINT in this browser's localStorage: the text of the two nearest meaningful
// rows above it and how far below them it was. Coming back without a live marker searches the buffer for that
// fingerprint, nearest the old row first. NOT FOUND MEANS NO DIVIDER, never one in a guessed place: a room restart
// or a context cycle gives a history the fingerprint is not in, and the terminal opens at the bottom as before.
//
// THE STORE is the browser's, per card (`atrium.lastread.<bare id>`), and only while somebody is away: the record is
// removed when they are back. A store on the card's side would add what this cannot: the same place on another
// browser or the phone, and a position that survives a cleared browser. It would also let the room say how many lines
// arrived while nobody was looking, instead of this page working it out from the replay.

// A window losing focus for less than this is not leaving. Alt-tabbing to copy a path should not move the view.
let lrBlurMin = 5000;
const LR_KEY = "atrium.termReturn";
const LR_STORE = "atrium.lastread.";
const LR_KEEP = 60;
const LR_STALE = 7 * 24 * 3600 * 1000;

// "left" (where I left off) or "bottom".
function termReturnMode() {
  try { return localStorage.getItem(LR_KEY) === "bottom" ? "bottom" : "left"; } catch (e) { return "left"; }
}
function setTermReturn(v) {
  try { localStorage.setItem(LR_KEY, v === "bottom" ? "bottom" : "left"); } catch (e) {}
}
prefLive(LR_KEY, () => {
  const sel = document.getElementById("s-termreturn");
  if (sel) sel.value = termReturnMode();
});

function lrOf(t) { return t._lr || (t._lr = { away: false }); }

function lrText(b, row) {
  const l = b.getLine(row);
  return l ? l.translateToString(true).trim() : "";
}
// A row worth fingerprinting: long enough, and not a rule of dashes or a run of one character.
function lrGood(s) { return s.length >= 8 && new Set(s).size >= 4; }

// The fingerprint of `row`: the two nearest good rows at or above `from`, and how far `row` is below the first.
function lrSig(b, row, from) {
  const t = [];
  let first = -1;
  for (let r = from; r >= 0 && r > from - 400 && t.length < 2; r--) {
    const s = lrText(b, r);
    if (!lrGood(s)) continue;
    if (first < 0) first = r;
    t.push(s);
  }
  return t.length === 2 ? { t, d: row - first } : null;
}

// The row a fingerprint now names, nearest `hint`, or -1.
function lrLocate(b, sig, hint) {
  if (!sig) return -1;
  let best = -1;
  for (let r = b.length - 1, seen = 0; r >= 0 && seen < 20; r--) {
    if (lrText(b, r) !== sig.t[0]) continue;
    let up = r - 1;
    for (; up >= 0 && up > r - 400; up--) if (lrGood(lrText(b, up))) break;
    if (up < 0 || lrText(b, up) !== sig.t[1]) continue;
    seen++;
    const row = r + sig.d;
    if (row < 0 || row >= b.length) continue;
    if (best < 0 || Math.abs(row - hint) < Math.abs(best - hint)) best = row;
  }
  return best;
}

function lrStored(id) {
  try {
    const rec = JSON.parse(localStorage.getItem(LR_STORE + bareId(id)));
    if (rec && rec.end && Date.now() - rec.at < LR_STALE) return rec;
  } catch (e) {}
  return null;
}
function lrStore(id, rec) {
  try {
    localStorage.setItem(LR_STORE + bareId(id), JSON.stringify(rec));
    const keys = [];
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k.startsWith(LR_STORE)) {
        let at = 0;
        try { at = JSON.parse(localStorage.getItem(k)).at || 0; } catch (e) {}
        keys.push([at, k]);
      }
    }
    keys.sort((a, b) => b[0] - a[0]);
    for (const [, k] of keys.slice(LR_KEEP)) localStorage.removeItem(k);
  } catch (e) {}
}
function lrForget(id) { try { localStorage.removeItem(LR_STORE + bareId(id)); } catch (e) {} }

function lrClearDiv(L) {
  if (!L.div) return;
  try { L.div.dec.dispose(); } catch (e) {}
  try { L.div.marker.dispose(); } catch (e) {}
  L.div = null;
}
function lrDropMarks(L) {
  for (const k of ["mkEnd", "mkTop"]) {
    if (L[k]) { try { L[k].dispose(); } catch (e) {} L[k] = null; }
  }
}
function lrMarkLine(m) { return m && !m.isDisposed && m.line >= 0 ? m.line : -1; }

// Writes down where this terminal was left. Idempotent while away.
function lrLeave(why) {
  const t = term;
  if (!t || !termTask || !t.buffer || !t.element) return;
  if (document.getElementById("terms").hidden) return;
  const L = lrOf(t);
  if (L.away) return;
  const b = t.buffer.active;
  const cur = b.baseY + b.cursorY;
  const k = lrText(b, cur) === "" ? 0 : 1;
  const atBottom = b.viewportY >= b.baseY;
  const rec = { end: lrSig(b, cur, cur - 1), endRow: cur, k, top: null, topRow: 0, div: null, at: Date.now() };
  if (!atBottom) { rec.top = lrSig(b, b.viewportY, b.viewportY); rec.topRow = b.viewportY; }
  // A divider that is still there when the terminal is reset for a replay is carried over it. Any other time leaving
  // retires it: what it marked has been seen.
  const dl = L.div ? lrMarkLine(L.div.marker) : -1;
  if (why === "reset" && dl >= 0) rec.div = lrSig(b, dl, dl - 1);
  else lrClearDiv(L);
  lrDropMarks(L);
  L.mkEnd = t.registerMarker(0);
  if (!atBottom) L.mkTop = t.registerMarker(b.viewportY - cur);
  L.rec = rec;
  L.away = true;
  L.awayAt = Date.now();
  L.awayWhy = why;
  L.watch = false;
  lrStore(termTask.id, rec);
  lrPill(t);
}

// Runs `fn` now and again as the layout settles, unless the operator has moved the view themselves.
function lrHold(t, fn) {
  const L = lrOf(t);
  L.moved = false;
  const go = () => { if (term !== t || L.moved) return; try { fn(); } catch (e) {} };
  go();
  requestAnimationFrame(go);
  setTimeout(go, 120);
  setTimeout(go, 400);
}

function lrDraw(t, row, cur, k) {
  const L = lrOf(t);
  const marker = t.registerMarker(row - cur);
  if (!marker) return;
  let dec;
  try { dec = t.registerDecoration({ marker, x: 0, width: t.cols, height: 1 }); } catch (e) {}
  if (!dec) { marker.dispose(); return; }
  dec.onRender(el => {
    el.classList.add("atrium-newdiv");
    el.style.width = "100%";
  });
  L.div = { marker, dec };
  L.divK = k;
}

// Coming back. `why` is tab, focus, visible (the window or tab came back), show (a kept terminal was shown) or attach
// (a terminal was built or reconnected and its replay has settled).
function lrReturn(why) {
  const t = term;
  if (!t || !termTask || !t.buffer) return;
  const L = lrOf(t);
  const fresh = why === "attach" || why === "show";
  if (!L.away && !fresh) return;
  const id = termTask.id;
  const rec = L.rec || lrStored(id);
  if (rec && L.away && (L.awayWhy === "blur" || L.awayWhy === "hidden") && Date.now() - L.awayAt < lrBlurMin) {
    lrDropMarks(L);
    L.rec = null;
    L.away = false;
    lrForget(id);
    return;
  }
  if (typeof releaseScrollHold === "function") releaseScrollHold();
  const b = t.buffer.active;
  const cur = b.baseY + b.cursorY;
  const mode = termReturnMode();
  const bottom = () => lrHold(t, () => t.scrollToBottom());
  lrClearDiv(L);
  L.watch = false;
  if (!rec) { lrDropMarks(L); L.away = false; lrPill(t); if (fresh) bottom(); return; }

  const liveEnd = lrMarkLine(L.mkEnd);
  const endRow = liveEnd >= 0 ? liveEnd : lrLocate(b, rec.end, rec.endRow);
  const topLive = lrMarkLine(L.mkTop);
  const topRow = !rec.top ? -1 : topLive >= 0 ? topLive : lrLocate(b, rec.top, rec.topRow);
  lrDropMarks(L);
  L.rec = null;
  L.away = false;
  lrForget(id);

  const carry = rec.div || L.carry;
  L.carry = null;
  let divRow = -1, k = rec.k;
  if (endRow >= 0 && cur > endRow) divRow = endRow + rec.k;
  else if (carry) { divRow = lrLocate(b, carry, 0); k = 0; }
  if (divRow >= 0 && divRow <= cur) lrDraw(t, divRow, cur, k);

  if (mode === "bottom" || endRow < 0) bottom();
  else if (rec.top) {
    if (topRow >= 0) lrHold(t, () => t.scrollToLine(topRow)); else bottom();
  } else if (divRow >= 0 && L.div) {
    lrHold(t, () => { const m = lrMarkLine(L.div && L.div.marker); if (m >= 0) t.scrollToLine(Math.max(0, m - 2)); });
  } else bottom();

  L.watch = mode === "left" && !!L.div && !carry;
  // Reaching the bottom retires the pill, but not before the scroll above has landed.
  L.armed = false;
  setTimeout(() => { L.armed = true; lrPill(t); }, 600);
  lrPill(t);
}

// The pill: "N new lines", while there are lines below the view that arrived since the divider.
function lrPillEl() {
  let el = document.getElementById("t-newpill");
  if (el) return el;
  const host = document.getElementById("term-pane");
  if (!host) return null;
  el = document.createElement("button");
  el.id = "t-newpill";
  el.type = "button";
  el.hidden = true;
  el.addEventListener("click", () => { if (term) { term.scrollToBottom(); lrPill(term); focusTerm(); } });
  host.appendChild(el);
  return el;
}
function lrPill(t) {
  if (t !== term) return;
  const el = lrPillEl();
  if (!el) return;
  const L = t._lr;
  let n = 0;
  if (L && L.watch && L.div) {
    const b = t.buffer.active;
    const m = lrMarkLine(L.div.marker);
    if (m < 0) L.watch = false;
    else if (b.viewportY >= b.baseY) { if (L.armed) L.watch = false; }
    else n = Math.max(0, b.baseY + b.cursorY - m + L.divK);
  }
  el.hidden = n <= 0;
  if (n > 0) el.textContent = "↓ " + n + " new line" + (n === 1 ? "" : "s");
}

// Per terminal, once it is built.
function lrInit(t) {
  // xterm leaves a marker where it was after a reset, pointing at whatever now has that number. Nothing may trust one.
  const reset = t.reset.bind(t);
  t.reset = () => {
    const L = lrOf(t), b = t.buffer.active;
    const dl = L.div ? lrMarkLine(L.div.marker) : -1;
    if (dl >= 0) L.carry = lrSig(b, dl, dl - 1);
    lrClearDiv(L);
    lrDropMarks(L);
    return reset();
  };
  t.onWriteParsed(() => { lrOf(t).lastWrite = Date.now(); lrPill(t); });
  t.onScroll(() => lrPill(t));
}

// A socket opened and its replay is on its way: the return happens when the replay has gone quiet.
function lrAfterOpen(t) {
  const L = lrOf(t);
  L.lastWrite = Date.now();
  clearInterval(L.poll);
  const t0 = Date.now();
  L.poll = setInterval(() => {
    if (term !== t) { clearInterval(L.poll); return; }
    if (Date.now() - L.lastWrite >= 250 || Date.now() - t0 > 4000) {
      clearInterval(L.poll);
      lrReturn("attach");
    }
  }, 100);
}

// What the operator did themselves ends any hold on the view.
for (const ev of ["wheel", "pointerdown", "keydown"]) {
  document.addEventListener(ev, e => {
    if (term && term._lr && e.target && e.target.closest && e.target.closest("#t-screen")) term._lr.moved = true;
  }, true);
}
document.addEventListener("visibilitychange", () => { if (document.hidden) lrLeave("hidden"); else lrReturn("visible"); });
window.addEventListener("blur", () => lrLeave("blur"));
window.addEventListener("focus", () => lrReturn("focus"));
