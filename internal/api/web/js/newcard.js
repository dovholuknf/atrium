// ── a card this window has not seen before ─────────────────
//
// A launch, an adopt, or a room attaching with a card new to it put a card on
// the board mid-list with nothing to say so. This marks it: a ring that pulses
// for a few seconds, then a quiet `new` chip that stays until the card is
// clicked, rested on for a second, attached, or five minutes have gone by. The
// same mark in the stack, on the board and in the terminals pane, because all
// three draw from `lastTasks` and all three call `newCardNote` with it.
//
// SEEN IS REMEMBERED PER BROWSER, keyed by the bare id. A reload has to know
// which cards were already there, and a hub restart flips every id between
// `room~id` and bare (see `bareId`), so neither the page's memory nor the raw
// id can answer it. The first list a page load draws is taken as already there
// whatever the book says, which is the same rule the arrival alert follows.
//
// STORAGE IS WRITTEN WHEN A CARD IS FIRST SEEN OR CLEARED, NEVER WHILE
// RENDERING. A render-time write is what spun the board's fold loop. The five
// minute expiry is worked out when the chip is drawn and writes nothing.

const NEWCARD_KEY = "atrium.newcards";
// How many ids the book keeps. A card still on the board is never dropped, so
// this only bounds the ones that have gone.
const NEWCARD_KEEP = 800;
const NEWCARD_GLOW_MS = 3200;
const NEWCARD_CHIP_MS = 5 * 60 * 1000;
const NEWCARD_HOVER_MS = 1000;
// A reader who scrolled this recently is reading, and is not moved.
const NEWCARD_READING_MS = 4000;

// bare id -> when this browser first saw it, or 0 for seen and cleared. Held in
// insertion order, which is what pruning drops from. Null until the first list.
let newCardBook = null;
// Whether this page load has taken its first list as the baseline.
let newCardSeeded = false;
// bare id -> when its pulse ends. This window's only: a reload does not pulse.
const newCardGlow = new Map();
// bare id -> until when to try to bring it into view.
const newCardReveal = new Map();
let newCardUserScroll = 0;

function newCardRead() {
  try {
    const o = JSON.parse(localStorage.getItem(NEWCARD_KEY) || "null");
    if (o && typeof o === "object" && !Array.isArray(o)) {
      return new Map(Object.entries(o).map(([k, v]) => [k, Number(v) || 0]));
    }
  } catch (e) {}
  return new Map();
}

function newCardWrite(live) {
  if (newCardBook.size > NEWCARD_KEEP) {
    for (const id of [...newCardBook.keys()]) {
      if (newCardBook.size <= NEWCARD_KEEP) break;
      if (!live.has(id)) newCardBook.delete(id);
    }
  }
  try { localStorage.setItem(NEWCARD_KEY, JSON.stringify(Object.fromEntries(newCardBook))); } catch (e) {}
}

// Called with every list a view draws, before it draws it.
function newCardNote(list) {
  const ids = (list || []).map(t => bareId(t.id)).filter(Boolean);
  if (!newCardBook) newCardBook = newCardRead();
  // A board with nothing on it yet is a hub waiting for its rooms, not a
  // baseline: taking it as one would pulse every card the room brings back.
  if (!ids.length) return;
  const fresh = ids.filter(id => !newCardBook.has(id));
  if (!newCardSeeded) {
    newCardSeeded = true;
    if (fresh.length) {
      fresh.forEach(id => newCardBook.set(id, 0));
      newCardWrite(new Set(ids));
    }
    return;
  }
  if (!fresh.length) return;
  const now = Date.now();
  for (const id of fresh) {
    newCardBook.set(id, now);
    newCardGlow.set(id, now + NEWCARD_GLOW_MS);
    newCardReveal.set(id, now + 2000);
  }
  newCardWrite(new Set(ids));
  requestAnimationFrame(newCardBringIntoView);
  setTimeout(newCardBringIntoView, 400);
}

function newCardIs(t) {
  const at = newCardBook && t ? newCardBook.get(bareId(t.id)) : 0;
  return at > 0 && Date.now() - at < NEWCARD_CHIP_MS;
}

// The classes a card, a stack row or a terminal row wears while it is new.
function newCardClass(t) {
  if (!newCardIs(t)) return "";
  return (newCardGlow.get(bareId(t.id)) || 0) > Date.now() ? " arrived glow" : " arrived";
}

function newCardChip(t) {
  if (!newCardIs(t)) return "";
  return `<span class="chip arrived"
    data-tip="new on the board. clears when you click it, rest the pointer on it or attach it, and on its own after five minutes"
    >new</span>`;
}

// Off the card now, in storage and on screen. The screen is changed directly
// so the chip goes at once whatever the next repaint is waiting on.
function newCardClear(id) {
  const bare = bareId(id);
  if (!newCardBook || !(newCardBook.get(bare) > 0)) return;
  newCardBook.set(bare, 0);
  newCardGlow.delete(bare);
  newCardReveal.delete(bare);
  newCardWrite(new Set((lastTasks || []).map(t => bareId(t.id))));
  newCardUnmark(bare);
}

function newCardUnmark(bare) {
  document.querySelectorAll(".arrived[data-id]").forEach(el => {
    if (bareId(el.dataset.id) !== bare) return;
    el.classList.remove("arrived", "glow");
    el.querySelectorAll(".chip.arrived").forEach(c => c.remove());
  });
}

// Bring a new card into view in the list it landed in, once, and only for
// somebody who is not scrolling. `block: "nearest"` moves nothing when it is
// already on screen, and scrolling never moves focus.
function newCardBringIntoView() {
  const now = Date.now();
  for (const [id, until] of [...newCardReveal]) {
    if (until < now) { newCardReveal.delete(id); continue; }
    const el = [...document.querySelectorAll(".arrived[data-id]")].find(e =>
      bareId(e.dataset.id) === id && e.getClientRects().length);
    if (!el) continue;
    newCardReveal.delete(id);
    if (now - newCardUserScroll < NEWCARD_READING_MS) continue;
    const r = el.getBoundingClientRect();
    let box = el.parentElement;
    while (box && box !== document.body) {
      const o = getComputedStyle(box).overflowY;
      if ((o === "auto" || o === "scroll") && box.scrollHeight > box.clientHeight) break;
      box = box.parentElement;
    }
    const view = box && box !== document.body ? box.getBoundingClientRect()
      : { top: 0, bottom: innerHeight };
    if (r.top < view.top || r.bottom > view.bottom) el.scrollIntoView({ block: "nearest", inline: "nearest" });
  }
}

// A reader scrolling. Wheel, touch, a drag on a scrollbar, and the keys that
// scroll a page. A terminal's own keys are typing, not scrolling.
function newCardReading(e) {
  // A press is only a scroll when it lands on a scrollbar, which is past the
  // element's client width. Any other press, launch included, is not reading.
  if (e.type === "pointerdown") {
    const t = e.target;
    if (!(t instanceof Element) || !t.clientWidth || e.offsetX < t.clientWidth) return;
  }
  if (e.type === "keydown") {
    if (!["PageUp", "PageDown", "ArrowUp", "ArrowDown", "Home", "End", " "].includes(e.key)) return;
    const t = e.target;
    if (t && t.closest && t.closest("input, textarea, select, [contenteditable], .xterm")) return;
  }
  newCardUserScroll = Date.now();
}
["wheel", "touchmove", "keydown", "pointerdown"].forEach(k =>
  addEventListener(k, newCardReading, { capture: true, passive: true }));

// Clicked, or rested on for a second.
let newCardHover = null;
let newCardHoverTimer = 0;
document.addEventListener("click", e => {
  const el = e.target && e.target.closest && e.target.closest(".arrived[data-id]");
  if (el) newCardClear(el.dataset.id);
}, true);
document.addEventListener("pointerover", e => {
  const el = e.target && e.target.closest && e.target.closest(".arrived[data-id]");
  if (el === newCardHover) return;
  clearTimeout(newCardHoverTimer);
  newCardHover = el;
  if (el) newCardHoverTimer = setTimeout(() => newCardClear(el.dataset.id), NEWCARD_HOVER_MS);
}, true);
document.addEventListener("pointerout", e => {
  if (!newCardHover || newCardHover.contains(e.relatedTarget)) return;
  clearTimeout(newCardHoverTimer);
  newCardHover = null;
}, true);

// Another window cleared one. Its chip goes here too.
addEventListener("storage", e => {
  if (e.key !== NEWCARD_KEY || !newCardBook) return;
  const next = newCardRead();
  for (const [id, at] of newCardBook) {
    if (at > 0 && next.get(id) === 0) { newCardBook.set(id, 0); newCardGlow.delete(id); newCardUnmark(id); }
  }
});
