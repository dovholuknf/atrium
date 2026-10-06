// A card's details, small. Context now, against the gear's threshold, and the
// token totals on record, in one compact view reached three ways:
//
//   - holding the pointer for a second on a card, on the board, the stack or
//     the terminals list. It opens under the pointer, not beside the card
//   - "details" on the card's menu
//   - the expando on the terminal's shortcut strip, which slides it up as a
//     drawer for the attached card
//
// One body for all three (`peekBody`), so they cannot drift. It reads
// GET /v1/tasks/{id}/usage when it opens and never otherwise: nothing polls,
// and the board's list carries only the warn flag. See js/usage.js for the
// full table in the details dialog, and internal/daemon/usage.go.

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
  return s === "card" || s === "hub" || s === "default" ? s : "";
}
// "200k from hub", or just "200k" when the source is not known.
function limitFrom(t) {
  const s = limitSource(t);
  return `${peekThresholdK(t)}k${s ? " from " + s : ""}`;
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


// ALL OF A CARD'S STATE, as rows, the same whichever part of it was hovered. Every badge a row draws says one fact
// about the card; this says them all, each only when it applies, urgent first: what needs you, what is in progress,
// what is held, then what the card is. Read from the card as the board last drew it, so it needs no request and
// cannot differ between the board, the stack and the terminals list.
function peekRows(t) {
  const sec = (cls, label, rows) => {
    rows = rows.filter(Boolean);
    return rows.length ? `<div class="peek-sec ${cls}"><div class="peek-sech">${esc(label)}</div>${rows.join("")}</div>` : "";
  };
  const row = (k, v) => v ? `<div class="peek-row"><b>${esc(k)}</b><span>${esc(v)}</span></div>` : "";
  const fn = (name, ...a) => typeof window[name] === "function" ? window[name](...a) : undefined;
  const clock = iso => { const d = new Date(iso); return isNaN(d) ? "" : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }); };
  const since = iso => { const ms = Date.parse(iso); return isNaN(ms) ? "" : fn("ago", Math.max(0, Math.floor((Date.now() - ms) / 1000))) || ""; };
  const a = t.activity || {};
  const n = t.new_context;
  const s = t.seen || {};
  const qs = s.open_questions || [];
  const need = [], doing = [], held = [], about = [];

  // NEEDS YOU
  if (fn("isBlocker", t)) need.push(row("blocked", fn("blockerReason", t)));
  else if (fn("isStuck", t)) need.push(row("stuck", t.escalation.text + (t.escalation.since ? ", since " + clock(t.escalation.since) : "")));
  if (t.offline) need.push(row("offline", "room " + (fn("roomOf", t.id) || t.room || "") + " is offline. cannot restore terminal"));
  if (fn("isOutOfContact", t)) need.push(row("no contact", "nothing heard from the session, and no process to ask. it may be working or may have ended"));
  if (fn("isWaiting", t)) {
    need.push(row(fn("statusLabel", t.status), (t.status === "needs-permission" ? "frozen waiting to be answered for " : "asked for you ") +
      fn("ago", fn("cardSecs", fn("cardWaitSeconds", t)))));
  }
  if (t.ask) {
    const more = (t.asks_open || 0) - 1;
    need.push(row(t.ask_peer ? "asked " + t.ask_peer : "question", t.ask + (more > 0 ? ` (+${more} more)` : "")));
  }
  if (s.answered === false && (qs.length || s.questions_unparsed)) {
    need.push(row("open questions", qs.length ? qs.map((q, i) => (i + 1) + ". " + q).join("\n") : "its last turn asked questions atrium could not read"));
  }
  if (s.unseen) need.push(row("unseen turn", "its last turn ended and nobody has looked at it since"));
  if (n && n.step === "failed") need.push(row("new context failed", (n.reason || "") + ". nothing further was typed"));
  if (t.report_unverified) need.push(row("sha unverified", "it reported done at " + (t.report_sha || "") + ", and that commit is not in its worktree"));
  if (t.held_notices) need.push(row("held notices", t.held_notices + " held notice" + (t.held_notices === 1 ? "" : "s") + " unread" + (t.oldest_held_at ? ", the oldest " + since(t.oldest_held_at) : "")));
  if (t.owed) {
    need.push(row("owed to its launcher", t.owed + " owed" + (t.owed_since ? " for " + since(t.owed_since) : "") +
      (t.owed_no_launcher ? ", and it has no launcher to keep it" : "")));
  }
  if (t.replies_owed) need.push(row("replies owed", t.replies_owed + " asked for a reply that has not come"));

  // IN PROGRESS
  if (n && n.step !== "failed") {
    const word = (typeof NEW_CONTEXT_WORDS !== "undefined" && NEW_CONTEXT_WORDS[n.step]) || n.step;
    doing.push(row("context cycle", `context ${n.n}/${n.of}: ${word}. waiting for: ${n.label}`));
  }
  if (a.looks_idle) doing.push(row("looks idle", "no turn-end from the agent. its screen has been idle for " + fn("ago", a.idle_seconds || 0)));
  else if (a.what) {
    const what = a.what === "tool" ? (a.tool ? "running " + a.tool : "running a tool") : a.what;
    doing.push(row("activity", what + (a.seconds > 5 ? " " + fn("ago", a.seconds) : "") +
      (a.what === "compacting" ? ". it is about to forget most of its context" : "")));
  }
  const subs = Math.max(a.subagents || 0, (a.running || []).length);
  if (subs) doing.push(row("subagents", fn("subagentTitle", a.subagents ? a : { subagents: subs, running: a.running }).replace(/\. click to expand\.$/, "")));
  if (t.restart_wake) doing.push(row("wake queued", "typed in once after the next restart brings it back: " + t.restart_wake.text));
  if (t.parked_at) doing.push(row("parked", "idle with no process since " + clock(t.parked_at) + ". a key in its terminal, or resume, wakes it"));
  if (t.starting && !t.supervised) doing.push(row("starting", "atrium is starting this one, its runner is not up yet"));
  if (t.held) doing.push(row("deploy hold", [t.held.kind, t.held.by && "by " + t.held.by, t.held.since && "since " + clock(t.held.since)].filter(Boolean).join(" ")));

  // HELD MESSAGES: how many, how long, and what holds them.
  if (a.held_peer) {
    const count = Number(a.held_count) || 1, secs = Number(a.held_seconds) || 0;
    held.push(row(a.held_quiet ? "queued" : "held", a.held_quiet ? fn("termQueuedTip", count, secs, a.held_turn) : fn("termHeldTip", count, secs, a.held_for, a.held_turn)));
  }

  // WHAT IT IS
  if (t.status === "shelved") about.push(row("shelved", fn("cannotResume", t) ? "comes off the shelf, but nothing starts: " + fn("cannotResume", t) : "start it again from where the conversation left off"));
  const room = fn("roomOf", t.id);
  if (room) about.push(row("room", room));
  if (t.runner) about.push(row("runner", t.runner));
  if (t.model || t.effort || (t.launch_args || []).length || (t.launch_env_keys || []).length) {
    about.push(row("launch", [t.model && "model " + t.model, t.effort && "effort " + t.effort,
      (t.launch_args || []).length && "args " + t.launch_args.join(" "),
      (t.launch_env_keys || []).length && "env " + t.launch_env_keys.join(", ")].filter(Boolean).join(", ")));
  }
  if (t.auto_approve) about.push(row("auto mode", "requests are approved without asking, and recorded" +
    (t.auto_until ? ". ends in " + fn("ago", Math.max(0, Math.floor((Date.parse(t.auto_until) - Date.now()) / 1000))) + ", at " + clock(t.auto_until) : "")));
  if (t.spawned_by || t.launcher_id) about.push(row("launched by", t.spawned_by || t.launcher_id));
  if (t.alias) about.push(row("alias", "@" + t.alias + (t.alias_note ? ". " + t.alias_note : "")));
  if ((t.tags || []).length) about.push(row("tags", t.tags.join(", ")));
  if (t.pinned) about.push(row("pinned", "always listed"));
  const origin = [t.source, t.external_id, t.url].filter(Boolean).join(" ");
  if (origin) about.push(row("origin", origin));
  if (typeof sharedCards !== "undefined" && sharedCards.has(t.id)) {
    about.push(row("shared", "published at " + (sharedCards.get(t.id).address || "") + ". anyone with that address types into it as you would"));
  }
  if ((t.note || "").trim()) about.push(row("note", "held, not sent: " + t.note.trim()));
  if (t.recap) about.push(row("recap", t.recap));
  else if (t.status === "done") about.push(row("recap", "finished without saying what it did"));
  if (t.telemetry) {
    const c = t.telemetry;
    const lim = [["5h limit", c.five_hour], ["weekly limit", c.weekly]].filter(x => x[1])
      .map(([l, v]) => `${l} ${v.pct}%${v.resets_at ? ", resets " + fn("resetsIn", v.resets_at) : ""}`);
    if (c.pct) about.push(row("statusline", [`${c.pct}% of the context window`, ...lim].join(" · ")));
    else if (lim.length) about.push(row("account", lim.join(" · ")));
  }
  return sec("need", "needs you", need) + sec("doing", "in progress", doing) + sec("held", "messages", held) + sec("about", "card", about);
}

// The body. `v` is the usage read, or null while it is under way, or an
// Error when it failed.
function peekBody(t, v) {
  const title = (t && (t.display_title || t.title)) || "this card";
  const model = v && !(v instanceof Error) && v.model ? v.model : "";
  const sub = [t && t.status, model].filter(Boolean).join(" · ");
  // The whole name and the repo/worktree:branch address, what a terminals row
  // said in its own tooltip. That tooltip gives way to this (see peekOwnsTip),
  // so what it said is here, unclipped.
  const where = t && typeof terminalLabel === "function" ? terminalLabel(t) : "";
  const cold = t && typeof termCold === "function" && termCold(t)
    ? "this one has exited. click it to start it again here" : "";
  const head = `<div class="peek-head"><span class="peek-title">${esc(title)}</span>` +
    (where && where !== title ? `<span class="peek-path">${esc(where)}</span>` : "") +
    (sub ? `<span class="peek-sub">${esc(sub)}</span>` : "") +
    (cold ? `<span class="peek-path">${esc(cold)}</span>` : "") + `</div>`;
  const rows = t ? peekRows(t) : "";
  if (!peekReadable(t)) {
    return head + rows + `<div class="peek-none">${t && t.offline
      ? "that machine is not answering, so nothing here can be read."
      : "no token use on record. only a Claude conversation keeps it."}</div>` + peekFoot(t);
  }
  if (v instanceof Error) {
    return head + rows + `<div class="peek-none">could not read it: ${esc(v.message)}</div>` + peekFoot(t);
  }
  const loading = !v;
  const ctx = loading ? 0 : Number(v.context_now) || 0;
  const k = peekThresholdK(t);
  const landK = landThePlaneK(t);
  const limit = landK * 1000;
  const warn = !loading && ctx >= k * 1000;
  const land = !loading && ctx >= limit;
  const tot = (v && v.totals) || {};
  const own = usageOwn(v && v.by_cause);
  // A field the room did not send is a dash, never a 0: a zero is a count the room reported.
  // No recorded rows is the same: the room counts a turn when it ends, so a card in its first turn has none yet.
  const NOT_SENT = "not reported by this room, or counted when the turn ends and none has yet";
  const has = (o, ...ks) => !!o && typeof o.rows === "number" && o.rows > 0 && ks.every(k => typeof o[k] === "number");
  const byOk = !!(v && v.by_cause && typeof v.by_cause === "object" && Object.keys(v.by_cause).length);
  const cell = (label, value, tip, missing) =>
    `<div class="peek-cell"${missing || tip ? ` data-tip="${esc(missing ? NOT_SENT : tip)}"` : ""}><b>${loading ? "&nbsp;" : missing ? "–" : esc(value)}</b>` +
    `<span>${esc(label)}</span></div>`;
  return head + rows +
    `<div class="peek-ctx${warn ? " warn" : ""}${land ? " hot" : ""}">
      <div class="peek-num"><b>${loading ? "&nbsp;" : usageTokens(ctx)}</b><span>context</span></div>
      ${ctxMeter(ctx, limit, "", loading ? "" : landTip({ context_size: { tokens: ctx, threshold_k: k, source: limitSource(t) }, telemetry: t && t.telemetry }))}
      <div class="peek-scale"><span>${land ? "land the plane" : warn ? "past the line" : ""}</span>` +
        `<span>warns at ${limitFrom(t)}, lands at ${landK}k</span></div>
    </div>
    <div class="peek-grid">
      ${cell("prompts", String(own.prompts), USAGE_TIPS.prompts + ". " + USAGE_TIPS.scope, !loading && !byOk)}
      ${cell("calls", String(own.calls), USAGE_TIPS.calls, !loading && !byOk)}
      ${cell("uncached in", usageTokens(tot.input), USAGE_TIPS.input, !loading && !has(tot, "input"))}
      ${cell("out", usageTokens(tot.output), USAGE_TIPS.output, !loading && !has(tot, "output"))}
      ${cell("cache read", usageTokens(tot.cache_read), USAGE_TIPS.read, !loading && !has(tot, "cache_read"))}
      ${cell("cache write", usageTokens((tot.cache_write_5m || 0) + (tot.cache_write_1h || 0)),
        "5m and 1h cache writes together. " + USAGE_TIPS.write5m + ". " + USAGE_TIPS.write1h,
        !loading && !has(tot, "cache_write_5m", "cache_write_1h"))}
    </div>` + peekFoot(t);
}

function peekFoot(t) {
  if (!t) return "";
  const parts = [];
  if (t.runner) parts.push(t.runner);
  // The line atrium holds the card to, and the size its runner compacts itself at.
  if (t.autocompact && t.autocompact.note) parts.push(t.autocompact.note);
  else if (t.autocompact) parts.push(`limit ${t.autocompact.limit_k}k · compacts at ${t.autocompact.window_k}k`);
  // Only for a card that says when it last did anything, by either field. The same number the sorts place it by.
  if (isFinite(cardIdleSeconds(t))) {
    const idle = cardIdleAge(t);
    parts.push(idle < 5 ? "active now" : "idle " + ago(idle));
  }
  const leaf = String(t.worktree || "").split(/[\\/]/).filter(Boolean).pop() || "";
  // The cache's words live here, and the rows carry only its dot. See js/keepalive.js.
  const cache = typeof peekCache === "function" ? peekCache(t) : "";
  return cache + `<div class="peek-foot"><span>${esc(parts.join(" · "))}</span><span>${esc(leaf)}</span></div>`;
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

// The restart action, under the details. Only on a card atrium supervises: a joined session has no terminal here
// to type exit keys into. See restartCard in js/terminal-debug.js.
function peekRestart(box, id) {
  const t = peekCard(id);
  if (!t || !t.supervised || t.status === "done") return;
  const b = document.createElement("button");
  b.type = "button";
  b.className = "peek-restart";
  b.textContent = "restart";
  b.dataset.tip = "exit it with its own keys and resume the same conversation on this card";
  b.onclick = () => { closePeek(); restartCard(id); };
  box.appendChild(b);
}

// Fills `box` with card `id`'s details: the card at once, the numbers when
// the read answers. `still` says whether the answer is still wanted.
async function peekFill(box, id, still) {
  const t = peekCard(id);
  box.innerHTML = peekBody(t, null);
  peekDocs(box, id);
  peekRestart(box, id);
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
  peekRestart(box, id);
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
  if (peekEl && peekEl.contains(e.target)) { clearTimeout(peekCloseTimer); return; }
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
