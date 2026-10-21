// ── keeping a popup on screen ───────────────────────────────────────────
// ONE place that answers "where does this go so it is all visible". Every menu, flyout, hover popup and tooltip on
// the board asks here rather than doing its own arithmetic, because each of the private versions got one edge
// wrong: the card menu's flyout ran off the bottom of a short window for as long as it only knew about the right.
//
// The rule: inside the viewport on all four edges. It flips to the other side of its anchor (left, or up) when the
// preferred side has no room, and when it is taller than the window it is capped and scrolls inside itself.
// Plain functions on the global scope like the rest of the board, so the headless test can call them directly.

const POP_PAD = 8;

// The room a popup has: the layout viewport's width, and the visual height so a phone keyboard counts.
function popView() {
  const vv = window.visualViewport;
  return {
    w: document.documentElement.clientWidth || innerWidth,
    h: Math.min(innerHeight, vv && vv.height ? Math.round(vv.height + vv.offsetTop) : innerHeight),
  };
}

// An anchor is a point `{x, y}` or a rect `{left, right, top, bottom}` (a DOMRect will do).
function popRect(a) {
  if (a && a.getBoundingClientRect) a = a.getBoundingClientRect();
  if (a && a.left !== undefined) return { left: a.left, right: a.right, top: a.top, bottom: a.bottom };
  return { left: a.x, right: a.x, top: a.y, bottom: a.y };
}

// Cap the height to `room` and let it scroll inside itself. A cap the stylesheet already set below that is kept,
// and so is an `overflow` the stylesheet already chose.
function popCap(el, room) {
  el.style.maxHeight = "";
  const own = parseFloat(getComputedStyle(el).maxHeight);
  if (!(own <= room)) el.style.maxHeight = Math.max(40, Math.floor(room)) + "px";
  // ONLY WHEN IT IS NEEDED. `overflow` other than visible clips an absolute child, and the card menu draws its
  // flyouts inside itself, so a menu that fits must not be made to scroll.
  el.style.overflowY = "";
  if (getComputedStyle(el).overflowY === "visible" && el.scrollHeight > el.clientHeight + 1) el.style.overflowY = "auto";
}

// Place a fixed position popup `el` against `anchor`. The element must already be displayed, so it has a size.
//   axis    "y" (default) sits under the anchor and flips above it; "x" sits to its right and flips to its left
//   prefer  "above" starts on the other side, for a bubble that belongs over its anchor
//   align   along the other axis: "start" (default), "end" or "center"
//   gap     between the anchor and the popup
//   slide   at the right edge, slide back in rather than flipping to the anchor's left (a point you pointed at)
//   shrink  with no room on either side, take the larger side and scroll in it rather than cover the anchor
// Returns the rect it ended up with.
function popFit(el, anchor, o) {
  o = o || {};
  const pad = o.pad === undefined ? POP_PAD : o.pad, gap = o.gap || 0, v = popView(), r = popRect(anchor);
  popCap(el, v.h - pad * 2);
  let w = el.offsetWidth, h = el.offsetHeight;
  let left, top;
  if (o.axis === "x") {
    left = r.right + gap;
    if (left + w > v.w - pad && r.left - gap - w >= pad) left = r.left - gap - w;
    top = o.align === "end" ? r.bottom - h : o.align === "center" ? (r.top + r.bottom - h) / 2 : r.top;
  } else {
    left = o.align === "end" ? r.right - w : o.align === "center" ? (r.left + r.right - w) / 2 : r.left;
    if (!o.slide && o.align !== "end" && o.align !== "center" && left + w > v.w - pad && r.right - w >= pad) left = r.right - w;
    const below = v.h - pad - (r.bottom + gap), above = r.top - gap - pad;
    const down = o.prefer === "above" ? h > above && h <= below : h <= below || h > above;
    if (h > below && h > above && o.shrink) {
      const room = Math.max(below, above);
      popCap(el, room);
      h = el.offsetHeight;
      top = below >= above ? r.bottom + gap : r.top - gap - h;
    } else top = down ? r.bottom + gap : r.top - gap - h;
    // Content that grows after it opens must scroll in the room below its top, not in the whole window's height.
    if (down && getComputedStyle(el).overflowY !== "visible") popCap(el, v.h - pad - Math.min(Math.max(top, pad), v.h));
  }
  left = Math.min(Math.max(left, pad), Math.max(pad, v.w - w - pad));
  top = Math.min(Math.max(top, pad), Math.max(pad, v.h - h - pad));
  el.style.left = Math.round(left) + "px";
  el.style.top = Math.round(top) + "px";
  return { left, top, right: left + w, bottom: top + h };
}

// Bring a popup that is drawn INSIDE its owner (absolute, so it moves with the owner: a flyout under a menu row, a
// hover menu under a line) back inside the viewport. It is shown already, at its natural place. It goes to the
// other side first when `flip` names a class that does that, then is capped, then is slid the last few pixels.
// Run again on every open, so a window that changed size since is read afresh.
function popFly(el, o) {
  o = o || {};
  const pad = o.pad === undefined ? POP_PAD : o.pad, v = popView();
  el.style.translate = "";
  el.style.maxHeight = "";
  if (o.flip) el.classList.remove(o.flip);
  popCap(el, v.h - pad * 2);
  let r = el.getBoundingClientRect();
  if (o.flip && (r.right > v.w - pad || r.left < pad)) {
    el.classList.add(o.flip);
    const f = el.getBoundingClientRect();
    // The flipped side only wins when it fits better than the side it left.
    if (Math.max(0, f.right - (v.w - pad)) + Math.max(0, pad - f.left) <= Math.max(0, r.right - (v.w - pad)) + Math.max(0, pad - r.left)) r = f;
    else el.classList.remove(o.flip);
  }
  const dx = r.right > v.w - pad ? v.w - pad - r.right : 0;
  const dy = r.bottom > v.h - pad ? v.h - pad - r.bottom : 0;
  let x = dx, y = dy;
  if (r.left + x < pad) x = pad - r.left;
  if (r.top + y < pad) y = pad - r.top;
  if (x || y) el.style.translate = Math.round(x) + "px " + Math.round(y) + "px";
}
