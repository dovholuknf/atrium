// A card's details, small. Its name and status, what needs you, the last recap, context now against the gear's
// threshold, and four actions, in one compact view reached three ways:
//
//   - holding the pointer for a second on a card, on the board, the stack or
//     the terminals list. It opens under the pointer, not beside the card
//   - "details" on the card's menu
//   - the expando on the terminal's shortcut strip, which slides it up as a
//     drawer for the attached card
//
// One body for all three (`peekBody`), so they cannot drift. It reads
// GET /v1/tasks/{id}/usage when it opens and never otherwise: nothing polls,
// and the board's list carries only the warn flag. The token totals, the launch command and the rest of the card are
// in the details dialog, see js/usage.js and internal/daemon/usage.go.

const PEEK_HOVER_MS = 1000;
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


// WHICH LAYER THE CONTEXT LIMIT CAME FROM, as the daemon resolved it onto the row: the card's own setting, the
// hub's limit for its harness, or the built-in default. Empty for a row from a daemon that does not say.
function limitSource(t) {
  const s = t && t.context_size && t.context_size.source;
  return s === "card" || s === "hub" || s === "default" || s === "runner" ? s : "";
}
// "200k from hub", or just "200k" when the source is not known. A limit its runner's own compaction point cut says
// so and says what it was cut from: "200k, capped by its runner from 900k (card)".
function limitFrom(t) {
  const s = limitSource(t);
  const c = t && t.context_size || {};
  if (s === "runner") {
    return `${peekThresholdK(t)}k, capped by its runner from ${c.wanted_k || "?"}k${c.wanted_from ? " (" + c.wanted_from + ")" : ""}`;
  }
  return `${peekThresholdK(t)}k${s ? " from " + s : ""}`;
}
// What the card's own limit is doing, for the details and the menu: empty when it has none.
function ownLimitNote(t) {
  const c = t && t.context_size;
  if (!c || !c.own_k) return "";
  return c.source === "runner" && c.wanted_from === "card"
    ? `its own limit, ${c.own_k}k, is above its runner's ceiling of ${c.ceiling_k}k`
    : `its own limit is ${c.own_k}k`;
}

function peekThresholdK(t) {
  if (t && t.context_size && t.context_size.threshold_k) return t.context_size.threshold_k;
  if (typeof pastePrefs !== "undefined" && pastePrefs && pastePrefs.context_limits &&
      pastePrefs.context_limits.claude) {
    return pastePrefs.context_limits.claude;
  }
  return 200;
}

// THE LAND-THE-PLANE LINE, a per-browser preference like the growler switch (js/growl.js): `atrium.landThePlaneK`, in
// thousands of tokens, every read and write inside try/catch, and a `storage` listener so another window follows.
// It matches the status line's LAND THE PLANE zone (150k sweet, 150-200k getting full, past 200k land the plane),
// which the board cannot read from a shell script, so it is typed once in the gear. Default 200, kept here and
// nowhere else. Junk reads as the default; the line is never below the warn line (threshold_k), since land is the
// second line and warn the first. Moving it into the daemon store beside `warn` is a possible later runtime step.
const LAND_K_KEY = "atrium.landThePlaneK";
const LAND_K_DEFAULT = 200;
const LAND_K_MIN = 10;
const LAND_K_MAX = 2000;
function landThePlaneRawK() {
  let v = "";
  try { v = localStorage.getItem(LAND_K_KEY) || ""; } catch (e) {}
  const n = /^\s*\d+\s*$/.test(v) ? Number(v) : NaN;
  return n >= LAND_K_MIN && n <= LAND_K_MAX ? n : LAND_K_DEFAULT;
}
function landThePlaneK(t) {
  return Math.max(landThePlaneRawK(), Number(peekThresholdK(t)) || 0);
}
function setLandThePlaneK(v) {
  const n = String(v == null ? "" : v).trim();
  try {
    if (n === "") localStorage.removeItem(LAND_K_KEY);
    else localStorage.setItem(LAND_K_KEY, n);
  } catch (e) {}
}
// Whether a card's context is at or past the land-the-plane line.
function landOver(t) {
  const c = t && t.context_size;
  return !!c && Number(c.tokens) >= landThePlaneK(t) * 1000;
}
// "201k of 200k (land the plane), window 1M": the bar's tooltip, and the badge's.
function landTip(t) {
  const c = t.context_size || {};
  const over = landOver(t);
  const win = t.telemetry && t.telemetry.window ? `, window ${usageTokens(t.telemetry.window).replace(/\.0M$/, "M")}` : "";
  return `${usageTokens(c.tokens)} of ${usageTokens(landThePlaneK(t) * 1000)}${over ? " (land the plane)" : ""}${limitSource(t) ? `, limit ${limitFrom(t)}` : ""}${win}`;
}

// THE ONE DRAWING OF THE CONTEXT METER, for the details popover and for the flood on a row, so the two cannot
// drift. `limit` is the land-the-plane line. The bar runs to the line plus ten percent, which is the runner's
// compaction window, so a card at the line looks nearly full, and the tick sits at the line. `cls` and `tip` are
// the row's; the popover passes neither.
const METER_SPAN = 1.1;
function ctxMeter(tokens, limit, cls, tip) {
  const fill = Math.min(100, (Number(tokens) / (limit * METER_SPAN)) * 100) || 0;
  return `<div class="peek-bar${cls ? " " + cls : ""}"${tip ? ` data-tip="${esc(tip)}"` : ""}>` +
    `<i style="width:${fill.toFixed(1)}%"></i><s style="left:${(100 / METER_SPAN).toFixed(1)}%"></s></div>`;
}

// Whether there is anything to read: a Claude conversation behind the card,
// on a machine that is answering.
function peekReadable(t) {
  return !!(t && t.resume_id && !t.offline);
}


// WHAT THE CARD WANTS FROM YOU, and its last recap: the rest of a card's state is on its row and in its details dialog.
// The peek is a glance, so it says the urgent things and nothing about how the card was launched, what it costs or
// whether its cache is warm. Read from the card as the board last drew it, so it needs no request and cannot differ
// between the board, the stack and the terminals list.
function peekRows(t) {
  const row = (k, v) => v ? `<div class="peek-row"><b>${esc(k)}</b><span>${esc(v)}</span></div>` : "";
  const fn = (name, ...a) => typeof window[name] === "function" ? window[name](...a) : undefined;
  const clock = iso => { const d = new Date(iso); return isNaN(d) ? "" : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }); };
  const n = t.new_context;
  const s = t.seen || {};
  const qs = s.open_questions || [];
  const need = [];

  if (fn("isBlocker", t)) need.push(row("blocked", fn("blockerReason", t)));
  else if (fn("isStuck", t)) need.push(row("stuck", t.escalation.text + (t.escalation.since ? ", since " + clock(t.escalation.since) : "")));
  if (t.offline) need.push(row("offline", "room " + (fn("roomOf", t.id) || t.room || "") + " is offline. cannot restore terminal"));
  if (fn("isOutOfContact", t)) need.push(row("no contact", "nothing heard from the session, and no process to ask. it may be working or may have ended"));
  if (fn("isWaiting", t)) {
    need.push(row(fn("statusLabel", t.status), (t.status === "needs-permission" ? "frozen waiting to be answered for " : "asked for you ") +
      fn("ago", fn("cardSecs", fn("cardWaitSeconds", t)))));
  }
  const hasQs = s.answered === false && (qs.length || s.questions_unparsed);
  // The question line is the first of the open questions, so it is only drawn when there is no list to show.
  if (t.ask && !hasQs) {
    const more = (t.asks_open || 0) - 1;
    need.push(row(t.ask_peer ? "asked " + t.ask_peer : "question", t.ask + (more > 0 ? ` (+${more} more)` : "")));
  }
  if (s.answered === false && qs.length && typeof peekQuestionsHtml === "function") {
    need.push(peekQuestionsHtml(t));
  } else if (hasQs) {
    need.push(row("open questions", qs.length ? qs.map((q, i) => (i + 1) + ". " + q).join("\n") : "its last turn asked questions atrium could not read"));
  }
  if (n && n.step === "failed") need.push(row("new context failed", (n.reason || "") + ". nothing further was typed"));
  if (t.report_unverified) need.push(row("sha unverified", "it reported done at " + (t.report_sha || "") + ", and that commit is not in its worktree"));

  const recap = t.recap || (t.status === "done" ? "finished without saying what it did" : "");
  return (need.length ? `<div class="peek-sec need"><div class="peek-sech">needs you</div>${need.join("")}</div>` : "") +
    (recap ? `<div class="peek-recap"><div class="peek-rtext">${esc(recap)}</div>` +
      `<button type="button" class="peek-more" hidden>more</button></div>` : "");
}

// The body. `v` is the usage read, or null while it is under way, or an
// Error when it failed. Only the context number and its bar come from the read.
function peekBody(t, v) {
  const title = (t && (t.display_title || t.title)) || "this card";
  const model = v && !(v instanceof Error) && v.model ? v.model : (t && t.model) || "";
  const sub = [t && t.status, model].filter(Boolean).join(" · ");
  // The repo/worktree:branch address, what a terminals row said in its own tooltip. That tooltip gives way to this
  // (see peekOwnsTip), so what it said is here. Title on one line, address and status under it on one.
  const where = t && typeof terminalLabel === "function" ? terminalLabel(t) : "";
  const cold = t && typeof termCold === "function" && termCold(t)
    ? "this one has exited. click it to start it again here" : "";
  const head = `<div class="peek-head"><span class="peek-title">${esc(title)}</span>` +
    `<div class="peek-meta">` +
    (where && where !== title ? `<span class="peek-path">${esc(where)}</span>` : "") +
    (sub ? `<span class="peek-sub">${esc(sub)}</span>` : "") + `</div>` +
    (cold ? `<span class="peek-path">${esc(cold)}</span>` : "") + `</div>`;
  const rows = t ? peekRows(t) : "";
  if (!peekReadable(t)) {
    return head + rows + `<div class="peek-none">${t && t.offline
      ? "that machine is not answering, so nothing here can be read."
      : "no token use on record. only a Claude conversation keeps it."}</div>`;
  }
  if (v instanceof Error) {
    return head + rows + `<div class="peek-none">could not read it: ${esc(v.message)}</div>`;
  }
  const loading = !v;
  const ctx = loading ? 0 : Number(v.context_now) || 0;
  const k = peekThresholdK(t);
  const landK = landThePlaneK(t);
  const limit = landK * 1000;
  const warn = !loading && ctx >= k * 1000;
  const land = !loading && ctx >= limit;
  const state = land ? "land the plane" : warn ? "past the line" : "context";
  return head + rows +
    `<div class="peek-ctx${warn ? " warn" : ""}${land ? " hot" : ""}">
      ${ctxMeter(ctx, limit, "", loading ? "" : landTip({ context_size: { tokens: ctx, threshold_k: k, source: limitSource(t) }, telemetry: t && t.telemetry }))}
      <div class="peek-scale"><span class="peek-num"><b>${loading ? "&nbsp;" : usageTokens(ctx)}</b> ${state}</span>` +
        `<span>warns at ${limitFrom(t)}${landK !== Number(k) ? `, lands ${landK}k` : ""}</span></div>
    </div>`;
}

// The whole command, for the card details to hold behind a fold.
function launchFull(t) {
  const c = t && t.launch_cmd;
  if (!c) return "";
  return [c.exe].concat(c.args || []).join(" ") + ((c.env_keys || []).length ? "\nenv: " + c.env_keys.join(", ") : "");
}

// The documents this card published, as one line under the details. Nothing at all on a board with no hub documents.
function peekDocs(box, id) {
  if (!window.mDocs) return;
  const b = document.createElement("button");
  b.type = "button";
  b.className = "c-docs peek-docs";
  b.hidden = true;
  box.appendChild(b);
  window.mDocs.paintCard(b, id);
}

// The recap's tail behind a click: shown only where the text runs past its four lines.
function peekRecap(box) {
  const text = box.querySelector(".peek-rtext"), more = box.querySelector(".peek-more");
  if (!text || !more) return;
  more.hidden = !(text.scrollHeight > text.clientHeight + 1);
  more.onclick = () => {
    const open = text.classList.toggle("open");
    more.textContent = open ? "less" : "more";
  };
}

// The actions, in one row under the details: open, say, exit and restart. Exit and restart need a terminal atrium owns
// (they type the runner's own exit keys), so they are only on a supervised card that has not finished. See
// exitTermNow and restartCard in js/terminal-debug.js. Open attaches where it can and opens the card's settings
// where it cannot.
function peekActions(box, id) {
  const t = peekCard(id);
  if (!t) return;
  const row = document.createElement("div");
  row.className = "peek-acts";
  const add = (label, tip, act) => {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "peek-act";
    b.textContent = label;
    b.dataset.tip = tip;
    b.onclick = () => { closePeek(); act(); };
    row.appendChild(b);
  };
  add("open", t.supervised ? "attach its terminal" : "open the card", () => t.supervised ? attachTask(id) : openTask(id));
  add("say", "queue a message for it", () => peekSay(id));
  if (t.supervised && t.status !== "done") {
    add("exit", "ask it to exit with its own keys. its card stays", () => oneAtATime("kill:" + bareId(id), () => exitTermNow(t)));
    add("restart", "exit it with its own keys and resume the same conversation on this card", () => restartCard(id));
  }
  box.appendChild(row);
}

// A message for the card, queued the way every say from the board is.
async function peekSay(id) {
  const t = peekCard(id);
  const title = (t && (t.display_title || t.title)) || "this card";
  const text = await askUser({ title: "message " + title, input: true,
    body: "It is queued and reaches the session on its next tool call.",
    buttons: [{ label: "cancel", value: null }, { label: "send", value: true, style: "go" }] });
  if (!text || !String(text).trim()) return;
  try {
    await api(`/v1/tasks/${encodeURIComponent(id)}/message`, { method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text: String(text).trim() }) });
  } catch (e) { toast("not sent", e.message); return; }
  toast("sent", title);
}

// Fills `box` with card `id`'s details: the card at once, the numbers when
// the read answers. `still` says whether the answer is still wanted.
async function peekFill(box, id, still) {
  const t = peekCard(id);
  box.innerHTML = peekBody(t, null);
  peekDocs(box, id);
  peekActions(box, id);
  peekRecap(box);
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
  peekDocs(box, id);
  peekActions(box, id);
  peekRecap(box);
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

// Where the pointer last was, in the viewport. The popover opens from here.
let peekPointer = null;
document.addEventListener("pointermove", e => {
  if (!e.pointerType || e.pointerType === "mouse") peekPointer = { x: e.clientX, y: e.clientY };
}, { passive: true, capture: true });

// Under the pointer `at`: its top left corner just below and right of it,
// pushed in from any edge it would cross, and above the pointer when there is
// no room below. With no pointer known (a menu opened from the keyboard) it
// goes under the anchor, the card's element or a point.
function peekPlace(anchor, at) {
  const el = peekPop();
  if (!at) {
    const r = anchor.getBoundingClientRect ? anchor.getBoundingClientRect() : null;
    at = r ? { x: r.left, y: r.bottom } : { x: anchor.x, y: anchor.y };
  }
  const w = el.offsetWidth, h = el.offsetHeight, pad = 8, gap = 12;
  let x = at.x + 4, y = at.y + gap;
  if (y + h > innerHeight - pad) y = at.y - h - gap;
  x = Math.min(Math.max(x, pad), Math.max(pad, innerWidth - w - pad));
  y = Math.min(Math.max(y, pad), Math.max(pad, innerHeight - h - pad));
  el.style.left = Math.round(x) + "px";
  el.style.top = Math.round(y) + "px";
}

function openPeek(id, anchor, mode) {
  const el = peekPop();
  // One box over a card at a time: a tooltip already up goes.
  if (typeof hideTip === "function") hideTip();
  clearTimeout(peekCloseTimer);
  const seq = ++peekSeq;
  peekFor = id;
  peekMode = mode || "menu";
  el.dataset.id = id;
  el.classList.toggle("pinned", peekMode === "menu");
  const box = el.querySelector(".peek-body");
  // Measured where the pointer was when it opened, and again from the same
  // point when the numbers land and it grows.
  const at = peekPointer;
  const replace = () => {
    if (seq !== peekSeq) return;
    if (anchor) peekPlace(anchor, at);
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
  if (typeof qaPeekClosed === "function") qaPeekClosed();
}

function peekCloseSoon() {
  clearTimeout(peekCloseTimer);
  peekCloseTimer = setTimeout(closePeek, PEEK_GRACE_MS);
}

// ── a second on a card ──────────────────────────────────────────────────────
//
// One listener for every tab: a board card, a stack row and a row on the
// terminals list all carry `data-id`. Keyed on the card id, not the element:
// the board redraws its cards on every event, and a busy agent redraws its own
// several times a second, so the element under a still pointer is replaced
// long before the second is up.

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

// ONE HOVER PER CARD. Under a mouse pointer nothing on a card shows its own
// tooltip: the popover is the card's one hover, and it says ALL of the card's
// state (`peekRows`), the same whichever badge the pointer is on. A chip's
// tooltip text is not shown anywhere else on the card, so nothing competes with
// the popover and nothing differs by badge. Before the second nothing shows.
// Asked by the tooltip's pointerover in js/tooltips.js. Keyboard focus and a
// long press are not asked, and show the tooltip as ever.
function peekOwnsTip(a, e) {
  if (e && e.pointerType && e.pointerType !== "mouse") return false;
  return !!peekHoverCard(a);
}

document.addEventListener("pointerover", e => {
  if (e.pointerType && e.pointerType !== "mouse") return;
  // Over comes before move, so the pointer is taken here too.
  peekPointer = { x: e.clientX, y: e.clientY };
  if ((peekEl && peekEl.contains(e.target)) || (e.target.closest && e.target.closest(".qa-fly"))) { clearTimeout(peekCloseTimer); return; }
  const card = peekHoverCard(e.target);
  const id = card ? card.dataset.id : null;
  if (id && id === peekHoverId) {
    peekHoverEl = card;
    if (peekFor === id) {
      clearTimeout(peekCloseTimer);
    }
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
  if (peekEl && peekEl.classList.contains("on") && !peekEl.contains(e.target) && !e.target.closest(".qa-fly")) closePeek();
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
  if (typeof dbgDrawer === "function") dbgDrawer(open && !!id);
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
