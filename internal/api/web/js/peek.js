// A card's details, small. Context now, against the gear's threshold, and the
// token totals on record, in one compact view reached three ways:
//
//   - holding the pointer on a card, or its stack row, for two seconds
//   - "details" on the card's menu
//   - the expando on the terminal's shortcut strip, which slides it up as a
//     drawer for the attached card
//
// One body for all three (`peekBody`), so they cannot drift. It reads
// GET /v1/tasks/{id}/usage when it opens and never otherwise: nothing polls,
// and the board's list carries only the warn flag. See js/usage.js for the
// full table in the details dialog, and internal/daemon/usage.go.

const PEEK_HOVER_MS = 2000;
// How long the pointer may be off both the card and the popover before a
// hover-opened one goes. Enough to cross the gap between them.
const PEEK_GRACE_MS = 260;

let peekEl = null;
// Which card the popover shows, how it was opened ("hover" or "menu"), and a
// counter so a slow read for one card never paints over another.
let peekFor = null, peekMode = "", peekSeq = 0;
let peekCloseTimer = null;

// The card as the board last drew it, for the parts the usage read does not
// carry: its name, status, threshold.
function peekCard(id) {
  const all = typeof lastTasks !== "undefined" ? lastTasks : [];
  return all.find(t => t.id === id) ||
    (typeof termTask !== "undefined" && termTask && termTask.id === id ? termTask : null);
}

function peekThresholdK(t) {
  if (t && t.context_size && t.context_size.threshold_k) return t.context_size.threshold_k;
  if (typeof pastePrefs !== "undefined" && pastePrefs && pastePrefs.context_threshold_k_now) {
    return pastePrefs.context_threshold_k_now;
  }
  return 150;
}

// Whether there is anything to read: a Claude conversation behind the card,
// on a machine that is answering.
function peekReadable(t) {
  return !!(t && t.resume_id && !t.offline);
}

// The body. `v` is the usage read, or null while it is under way, or an
// Error when it failed.
function peekBody(t, v) {
  const title = (t && (t.display_title || t.title)) || "this card";
  const model = v && !(v instanceof Error) && v.model ? v.model : "";
  const sub = [t && t.status, model].filter(Boolean).join(" · ");
  const head = `<div class="peek-head"><span class="peek-title">${esc(title)}</span>` +
    (sub ? `<span class="peek-sub">${esc(sub)}</span>` : "") + `</div>`;
  if (!peekReadable(t)) {
    return head + `<div class="peek-none">${t && t.offline
      ? "that machine is not answering, so nothing here can be read."
      : "no token use on record. only a Claude conversation keeps it."}</div>` + peekFoot(t);
  }
  if (v instanceof Error) {
    return head + `<div class="peek-none">could not read it: ${esc(v.message)}</div>` + peekFoot(t);
  }
  const loading = !v;
  const ctx = loading ? 0 : Number(v.context_now) || 0;
  const k = peekThresholdK(t);
  const limit = k * 1000;
  const warn = !loading && ctx >= limit;
  // The bar runs to half as far again as the threshold, so the line sits at
  // two thirds and a card past it still has somewhere to go.
  const scale = limit * 1.5;
  const fill = Math.min(100, (ctx / scale) * 100);
  const tot = (v && v.totals) || {};
  const cell = (label, value) =>
    `<div class="peek-cell"><b>${loading ? "&nbsp;" : esc(value)}</b><span>${esc(label)}</span></div>`;
  return head +
    `<div class="peek-ctx${warn ? " warn" : ""}">
      <div class="peek-num"><b>${loading ? "&nbsp;" : usageTokens(ctx)}</b><span>context</span></div>
      <div class="peek-bar"><i style="width:${fill.toFixed(1)}%"></i><s style="left:${(100 / 1.5).toFixed(1)}%"
        ></s></div>
      <div class="peek-scale"><span>${warn ? "past the line" : ""}</span>` +
        `<span>warns at ${k}k</span></div>
    </div>
    <div class="peek-grid">
      ${cell("turns", String(tot.rows || 0))}
      ${cell("in", usageTokens(tot.input))}
      ${cell("out", usageTokens(tot.output))}
      ${cell("cache read", usageTokens(tot.cache_read))}
      ${cell("cache write", usageTokens((tot.cache_write_5m || 0) + (tot.cache_write_1h || 0)))}
      ${cell("est.", usageMoney(tot.cost))}
    </div>` + peekFoot(t);
}

function peekFoot(t) {
  if (!t) return "";
  const parts = [];
  if (t.runner) parts.push(t.runner);
  if (typeof t.idle_seconds === "number" && t.idle_seconds >= 0) {
    parts.push(t.idle_seconds < 5 ? "active now" : "idle " + ago(t.idle_seconds));
  }
  const leaf = String(t.worktree || "").split(/[\\/]/).filter(Boolean).pop() || "";
  return `<div class="peek-foot"><span>${esc(parts.join(" · "))}</span><span>${esc(leaf)}</span></div>`;
}

// Fills `box` with card `id`'s details: the card at once, the numbers when
// the read answers. `still` says whether the answer is still wanted.
async function peekFill(box, id, still) {
  const t = peekCard(id);
  box.innerHTML = peekBody(t, null);
  box.classList.toggle("loading", peekReadable(t));
  if (!peekReadable(t)) return;
  let v;
  try {
    v = await api(`/v1/tasks/${encodeURIComponent(id)}/usage?limit=1`);
  } catch (e) {
    v = e instanceof Error ? e : new Error(String(e));
  }
  if (!still()) return;
  box.classList.remove("loading");
  box.innerHTML = peekBody(peekCard(id) || t, v);
}

// ── the popover ─────────────────────────────────────────────────────────────

function peekPop() {
  if (peekEl) return peekEl;
  peekEl = document.createElement("div");
  peekEl.className = "peek";
  peekEl.setAttribute("role", "dialog");
  peekEl.setAttribute("aria-label", "card details");
  peekEl.innerHTML = `<div class="peek-body"></div>`;
  peekEl.addEventListener("pointerenter", () => clearTimeout(peekCloseTimer));
  peekEl.addEventListener("pointerleave", () => { if (peekMode === "hover") peekCloseSoon(); });
  document.body.appendChild(peekEl);
  return peekEl;
}

// Beside the card, on whichever side has room, and never off the screen. The
// anchor is the card's element, or a point for a menu opened somewhere with no
// card drawn under it.
function peekPlace(anchor) {
  const el = peekPop();
  const r = anchor.getBoundingClientRect ? anchor.getBoundingClientRect()
    : { left: anchor.x, right: anchor.x, top: anchor.y, bottom: anchor.y };
  const w = el.offsetWidth, h = el.offsetHeight, pad = 8, gap = 10;
  let x = r.right + gap, y = r.top;
  if (x + w > innerWidth - pad) x = r.left - w - gap;
  if (x < pad) {
    // No room either side: under it, or over it.
    x = Math.min(Math.max(r.left, pad), innerWidth - w - pad);
    y = r.bottom + gap + h > innerHeight - pad ? r.top - h - gap : r.bottom + gap;
  }
  y = Math.min(Math.max(y, pad), Math.max(pad, innerHeight - h - pad));
  el.style.left = Math.round(x) + "px";
  el.style.top = Math.round(y) + "px";
}

function openPeek(id, anchor, mode) {
  const el = peekPop();
  clearTimeout(peekCloseTimer);
  const seq = ++peekSeq;
  peekFor = id;
  peekMode = mode || "menu";
  el.dataset.id = id;
  el.classList.toggle("pinned", peekMode === "menu");
  const box = el.querySelector(".peek-body");
  const replace = () => {
    if (anchor && (anchor.isConnected || !anchor.getBoundingClientRect)) peekPlace(anchor);
  };
  peekFill(box, id, () => seq === peekSeq && peekFor === id).then(replace);
  replace();
  el.classList.add("on");
}

function closePeek() {
  clearTimeout(peekCloseTimer);
  peekSeq++;
  peekFor = null;
  peekMode = "";
  if (peekEl) peekEl.classList.remove("on");
}

function peekCloseSoon() {
  clearTimeout(peekCloseTimer);
  peekCloseTimer = setTimeout(closePeek, PEEK_GRACE_MS);
}

// ── two seconds on a card ───────────────────────────────────────────────────
//
// Keyed on the card id, not the element: the board redraws its cards on every
// event, and a busy agent redraws its own several times a second, so the
// element under a still pointer is replaced long before two seconds are up.

let peekHoverId = null, peekHoverEl = null, peekHoverTimer = null;

function peekCanHover() {
  if (document.querySelector("dialog[open]")) return false;
  if (typeof cardMenuEl !== "undefined" && cardMenuEl && cardMenuEl.classList.contains("on")) return false;
  const sel = window.getSelection && window.getSelection();
  return !(sel && !sel.isCollapsed);
}

function peekHoverCard(target) {
  return target && target.closest ? target.closest(".card[data-id], .stackrow[data-id]") : null;
}

document.addEventListener("pointerover", e => {
  if (e.pointerType && e.pointerType !== "mouse") return;
  if (peekEl && peekEl.contains(e.target)) { clearTimeout(peekCloseTimer); return; }
  const card = peekHoverCard(e.target);
  const id = card ? card.dataset.id : null;
  if (id && id === peekHoverId) {
    peekHoverEl = card;
    if (peekFor === id) clearTimeout(peekCloseTimer);
    return;
  }
  clearTimeout(peekHoverTimer);
  peekHoverId = id;
  peekHoverEl = card;
  if (peekMode === "hover" && peekFor !== id) peekCloseSoon();
  if (!id) return;
  peekHoverTimer = setTimeout(() => {
    if (peekHoverId !== id || !peekCanHover()) return;
    const at = peekHoverEl && peekHoverEl.isConnected ? peekHoverEl
      : document.querySelector(`.card[data-id="${CSS.escape(id)}"], .stackrow[data-id="${CSS.escape(id)}"]`);
    if (at && peekMode !== "menu") openPeek(id, at, "hover");
  }, PEEK_HOVER_MS);
});

// A press is a click on the card, which opens its menu. Nothing pops over it.
document.addEventListener("pointerdown", e => {
  clearTimeout(peekHoverTimer);
  peekHoverId = null;
  if (peekEl && peekEl.classList.contains("on") && !peekEl.contains(e.target)) closePeek();
}, true);

document.addEventListener("keydown", e => {
  if (e.key === "Escape" && peekEl && peekEl.classList.contains("on")) closePeek();
});

// ── the drawer on the terminal's shortcut strip ─────────────────────────────

let termDrawerOpen = false, termDrawerSeq = 0;

function toggleTermDrawer(force) {
  const open = typeof force === "boolean" ? force : !termDrawerOpen;
  termDrawerOpen = open;
  const drawer = document.getElementById("t-drawer");
  const btn = document.getElementById("t-expando");
  if (!drawer) return;
  drawer.classList.toggle("open", open);
  drawer.setAttribute("aria-hidden", open ? "false" : "true");
  if (btn) btn.setAttribute("aria-expanded", open ? "true" : "false");
  const id = typeof termTask !== "undefined" && termTask ? termTask.id : null;
  if (!open || !id) { termDrawerSeq++; return; }
  const seq = ++termDrawerSeq;
  peekFill(document.getElementById("t-drawer-body"), id, () => seq === termDrawerSeq && termDrawerOpen);
}

// The terminal moved to another card, or closed. An open drawer follows it.
function termDrawerFollow(task) {
  if (!termDrawerOpen) return;
  if (!task) { toggleTermDrawer(false); return; }
  toggleTermDrawer(true);
}
