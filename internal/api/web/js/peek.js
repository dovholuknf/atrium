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
// A pointer farther than this from where the hover timer began has not come to rest (hand jitter is less).
const PEEK_HOVER_JITTER_PX = 10;
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


// WHAT THE CARD ASKS OF YOU, in one line the peek opens with. Status is a column and activity is a badge
// (docs/runtime/activity-design.md), so a turn that is running says what it is doing over whatever the stored column
// still reads. A turn that ended with questions "needs you". A turn that ended with none is "your move", which asks
// nothing: it is not needs-you however the column spells it. `seen.open_questions` is what tells them apart.
// `cls` is the style, ps-ask (warn), ps-turn (teal), ps-work (live), ps-plain (dim). Prefixed, as a bare `ask` is a global.
function peekHasQuestions(t) {
  return !!t && (!!t.ask || (typeof cardHasOpenQuestion === "function" && cardHasOpenQuestion(t)));
}
// A turn is running now: the board's own test for the working badge, less its exclusion of a waiting card. The
// stored column can still read needs-input for a moment after a new prompt, and the live activity is the truth.
function peekRunning(t) {
  const a = t && t.activity;
  return !!(a && a.what && a.what !== "idle" && !a.looks_idle && t.status !== "paused" &&
    typeof staleActivity === "function" && !staleActivity(t));
}
function peekStatus(t) {
  if (!t) return null;
  const s = t.seen || {};
  const n = (s.open_questions || []).length;
  const waited = () => typeof ago === "function" && typeof cardSecs === "function" && typeof cardWaitSeconds === "function"
    ? ago(cardSecs(cardWaitSeconds(t))) : "";
  const since = () => waited() ? ", " + waited() : "";
  const waiting = typeof isWaiting === "function" && isWaiting(t);
  if (t.status === "needs-permission" && waiting) {
    return { cls: "ps-ask", word: "needs you", why: "permission to go on" + since() };
  }
  if (peekRunning(t)) {
    const a = t.activity;
    return { cls: "ps-work", word: "working", why: a.what === "tool" ? (a.tool ? "running " + a.tool : "running a tool") : a.what };
  }
  if (t.status === "needs-input" && waiting) {
    if (peekHasQuestions(t)) {
      return { cls: "ps-ask", word: "needs you", why: (n ? n + (n === 1 ? " question" : " questions") : "a question") + since() };
    }
    return { cls: "ps-turn", word: "your move", why: "its turn ended, nothing asked" + since() };
  }
  // A parked or paused card has no agent running: it is paused, whatever status it kept. Never "ready".
  if (typeof cardPaused === "function" && cardPaused(t)) {
    return { cls: "ps-plain", word: "paused", why: "no agent running. it can be resumed" };
  }
  const label = typeof statusLabel === "function" ? statusLabel(t.status) : t.status;
  return label ? { cls: "ps-plain", word: label, why: "" } : null;
}

// A recap older than the session's latest prompt is not what the card is doing now. It is LABELLED, not hidden: it
// is still the last thing the card said about itself, and a card prompted since is usually on the same work, so
// dropping it would leave the peek with nothing. The label says how old, so it is never read as current. Empty when
// it is current, or when either stamp is missing.
function peekRecapAge(t) {
  const r = Date.parse(t && t.recap_at), p = Date.parse(t && t.prompted_at);
  if (isNaN(r) || isNaN(p) || r >= p || typeof ago !== "function") return "";
  return "said " + ago((Date.now() - r) / 1000) + " ago, before its latest prompt";
}

// WHAT THE CARD WANTS FROM YOU, and its last recap: the rest of a card's state is on its row and in its details dialog.
// The peek is a glance, so it says the urgent things and nothing about how the card was launched, what it costs or
// whether its cache is warm. Read from the card as the board last drew it, so it needs no request and cannot differ
// between the board, the stack and the terminals list.
function peekRows(t) {
  const row = (k, v) => v ? `<div class="peek-row">${k ? `<b>${esc(k)}</b>` : ""}<span>${esc(v)}</span></div>` : "";
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
  // A needs-input card with nothing asked is the status line's "your move", not something it needs from you.
  if (fn("isWaiting", t) && (t.status !== "needs-input" || peekHasQuestions(t))) {
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
    need.push(row("", qs.length ? qs.map((q, i) => (i + 1) + ". " + q).join("\n") : "its last turn asked questions atrium could not read"));
  }
  if (n && n.step === "failed") need.push(row("new context failed", (n.reason || "") + ". nothing further was typed"));
  if (t.report_unverified) need.push(row("sha unverified", "it reported done at " + (t.report_sha || "") + ", and that commit is not in its worktree"));

  const kids = typeof termChildInfo === "function" && typeof lastTasks !== "undefined" ? termChildInfo(t, lastTasks) : null;
  const age = t.recap ? peekRecapAge(t) : "";
  const recap = t.recap || (t.status === "done" ? "finished without saying what it did" : "");
  // One short word: atrium_launch_worker cards are exited when their branch merges. A kept card says nothing.
  const needSum = (fn("isBlocker", t) || fn("isStuck", t)) ? "blocked or stuck" : peekHasQuestions(t) ? "a question" : "waiting on you";
  return (need.length ? `<div class="peek-sec need" data-drawer="need" data-label="needs you" data-sum="${esc(needSum)}">${need.join("")}</div>` : "") +
    peekKidsHtml(kids) +
    (t.lifetime ? `<div class="peek-sec">${row("lifetime", t.lifetime)}</div>` : "") +
    peekIdHtml(t) +
    peekScratchHtml(t) +
    (recap ? `<div class="peek-recap" data-drawer="recap" data-label="recap" data-sum="${esc(recap.split("\n")[0])}">${age ? `<div class="peek-rage">${esc(age)}</div>` : ""}<div class="peek-rtext">${esc(recap)}</div>` +
      `<button type="button" class="peek-more" hidden>more</button></div>` : "") +
    peekMemHtml(t);
}

// What the card's runner, its shell and everything they started hold now, from the pty host's job object or cgroup.
function peekMemHtml(t) {
  if (!t || !t.mem || over(t) || typeof bytes !== "function") return "";
  const tip = "memory this card's processes hold now. Process Explorer's Job tab names their job after the card";
  return `<div class="peek-sec"><div class="peek-row" data-tip="${esc(tip)}"><b>memory</b><span>${esc(bytes(t.mem))}${t.pid ? `  pid ${esc(t.pid)}` : ""}</span></div></div>`;
}

// THE CARD'S ID, copyable on click, so it can still be read once the runner has exited.
function peekIdHtml(t) {
  if (!t || !t.id) return "";
  return `<div class="peek-sec"><div class="peek-row"><b>id</b><span>${esc(t.id)}</span>` +
    `<button type="button" class="peek-copyid link" data-id="${esc(t.id)}" data-tip="copy the card id">copy id</button></div></div>`;
}
document.addEventListener("click", async ev => {
  const b = ev.target && ev.target.closest && ev.target.closest(".peek-copyid");
  if (!b) return;
  try { await navigator.clipboard.writeText(b.dataset.id); b.textContent = "copied"; } catch (e) { b.textContent = "copy failed"; }
});

// THE CARD'S SCRATCH FOLDER: where the room told it to put throwaway files, set by the room's scratch_root. Absent when the
// room has none. "files" lists the folder and each file is a link to download it, both read with `?root=scratch`, which
// the daemon resolves to the one folder it made for this card.
function peekScratchHtml(t) {
  if (!t || !t.scratch) return "";
  return `<div class="peek-sec scratch"><div class="peek-row"><b>scratch</b><span>${esc(t.scratch)}</span></div>` +
    `<div class="peek-row"><button type="button" class="peek-scratch link" data-id="${esc(t.id)}" data-tip="list the files in it">files</button></div>` +
    `<div class="peek-scratch-list"></div></div>`;
}
async function peekScratchList(btn, path) {
  const box = btn.closest(".peek-sec").querySelector(".peek-scratch-list");
  const id = btn.dataset.id;
  let res;
  try {
    res = await api(`/v1/tasks/${encodeURIComponent(id)}/files/list?root=scratch` + (path ? `&path=${encodeURIComponent(path)}` : ""));
  } catch (e) {
    box.textContent = e.message;
    return;
  }
  const rows = (res.entries || []).map(e => e.dir
    ? `<div class="peek-sfile"><button type="button" class="peek-sdir link" data-path="${esc(e.path)}">${esc(e.name)}/</button></div>`
    : `<div class="peek-sfile"><a download href="/v1/tasks/${encodeURIComponent(id)}/files?root=scratch&path=${encodeURIComponent(e.path)}">${esc(e.name)}</a>` +
      `<span>${typeof bytes === "function" ? esc(bytes(e.size)) : ""}</span></div>`);
  const up = res.parent ? `<div class="peek-sfile"><button type="button" class="peek-sdir link" data-path="${esc(res.parent)}">..</button></div>` : "";
  box.innerHTML = up + (rows.join("") || `<div class="peek-sfile">nothing here.</div>`);
  box.querySelectorAll(".peek-sdir").forEach(b => { b.onclick = () => peekScratchList(btn, b.dataset.path); });
}
document.addEventListener("click", ev => {
  const b = ev.target && ev.target.closest && ev.target.closest(".peek-scratch");
  if (b) peekScratchList(b, "");
});

// THE CHILDREN SECTION: what the badges on the card's row mean. It is SHUT by default and its heading is the summary,
// `children  40 · 3 need you · 1 running · 8 paused · 28 dead`, a part left out when it is zero. Open, it is one child per
// line, the ones that need you first, each a link that opens its card with a short status word at the right. Children the
// filters hide are counted in the summary and not listed. A card with no children has no section. `info` is `termChildInfo`.
function peekKidsSummary(info) {
  const dead = info.hidden - info.paused;
  return [info.total, info.waiting.length && info.waiting.length + " need you", info.running && info.running + " running",
    info.paused && info.paused + " paused", dead && dead + " dead"].filter(Boolean).join(" · ");
}
// A child's title, with where it lives added when another child in the list has the same one.
function peekKidName(c, list) {
  const title = c.display_title || c.title || c.id;
  if (list.filter(o => (o.display_title || o.title || o.id) === title).length < 2) return title;
  const who = c.wire_name && typeof localWire === "function" ? localWire(c.wire_name) : (typeof roomOf === "function" && roomOf(c.id)) || c.room || "";
  return who && title.indexOf("(" + who + ")") < 0 ? title + " (" + who + ")" : title;
}
function peekKidsHtml(info) {
  if (!info) return "";
  const lines = info.list.map(c => {
    const st = typeof peekStatus === "function" ? peekStatus(c) : null;
    return `<div class="peek-kidrow"><button type="button" class="peek-kid link" data-id="${esc(c.id)}" data-tip="open its card" onclick="peekOpenKid(this.dataset.id)">${
      esc(peekKidName(c, info.list))}</button>${st ? `<span class="peek-kidst ${esc(st.cls)}">${esc(st.word)}</span>` : ""}</div>`;
  }).join("");
  return `<div class="peek-sec kids" data-drawer="kids" data-label="children" data-sum="${esc(peekKidsSummary(info))}">${lines}</div>`;
}
// A child named in the section opens its card: attached where it has a terminal, its settings where it has not.
function peekOpenKid(id) {
  const c = peekCard(id);
  closePeek();
  if (c && c.supervised) attachTask(id); else openTask(id);
}

// The body. `v` is the usage read, or null while it is under way, or an
// Error when it failed. Only the context number and its bar come from the read.
function peekBody(t, v) {
  const title = (t && (t.display_title || t.title)) || "this card";
  const model = v && !(v instanceof Error) && v.model ? v.model : (t && t.model) || "";
  const sub = model;
  const st = peekStatus(t);
  // The repo/worktree:branch address, what a terminals row said in its own tooltip. That tooltip gives way to this
  // (see peekOwnsTip), so what it said is here. Title on one line, address and status under it on one.
  const where = t && typeof terminalLabel === "function" ? terminalLabel(t) : "";
  const cold = t && typeof termCold === "function" && termCold(t)
    ? (cardPaused(t) ? "paused: nothing is running. resume it below or from the right-click menu"
      : "this one has exited. start a new agent in its folder from below") : "";
  // A folded parent draws none of its children, so the warn chip on its row has to be explained here.
  const kidsWait = t && typeof termKidsShut === "function" && (termKids.get(t.id) || []).length && termKidsShut(t)
    ? termKidsCount(t.id, termOwesHuman) : 0;
  const head = `<div class="peek-head"><span class="peek-title">${esc(title)}</span>` +
    (st ? `<div class="peek-status ${st.cls}"><b>${esc(st.word)}</b>${st.why ? `<span>${esc(st.why)}</span>` : ""}</div>` : "") +
    (kidsWait ? `<div class="peek-status ps-ask peek-kidwait"><b>${kidsWait} ${kidsWait === 1 ? "card" : "cards"}</b><span>under this one ${
      kidsWait === 1 ? "needs" : "need"} you</span></div>` : "") +
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
    `<div class="peek-ctx${warn ? " warn" : ""}${land ? " hot" : ""}" data-drawer="ctx" data-label="context" data-sum="${loading ? "" : esc(usageTokens(ctx).replace(/\.0k$/, "k") + " of " + usageTokens(limit).replace(/\.0k$/, "k"))}">
      ${ctxMeter(ctx, limit, "", loading ? "" : landTip({ context_size: { tokens: ctx, threshold_k: k, source: limitSource(t) }, telemetry: t && t.telemetry }))}
      <div class="peek-scale"><span class="peek-num"><b>${loading ? "&nbsp;" : usageTokens(ctx)}</b> ${state}</span>` +
        `<span>${landK !== Number(k) ? `warns at ${limitFrom(t)}, lands ${landK}k` : `warns and lands at ${limitFrom(t)}`}</span></div>
      ${t && t.context_size && t.context_size.low ? `<div class="peek-scale"><span class="warn">${esc(t.context_size.low)}</span></div>` : ""}
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

// CHATTER: what this card said to other sessions and what they said to it, newest first, twenty to a page. One read of
// GET /v1/tasks/{id}/says when the details open, so nothing polls. A card with no says draws no section at all.
// The other party links to its card when the board knows it. A row opens to the full text, which the daemon keeps
// up to its own bound (store.SayTextLen) and a say from before it was kept shows its preview.
function peekChatterLoad(id, before) {
  return api(`/v1/tasks/${encodeURIComponent(id)}/says` + (before ? `?before=${encodeURIComponent(before)}` : ""))
    .catch(() => null);
}

function peekChatterWho(s, out) {
  const wire = out ? (s.to || s.to_input) : s.from;
  const other = out ? s.to_task : s.from_task;
  const all = typeof lastTasks !== "undefined" ? lastTasks : [];
  const card = other && all.find(t => bareId(t.id) === other);
  return { wire: wire || "unknown", card };
}

function peekChatterRow(id, s) {
  const out = s.from_task === bareId(id);
  const who = peekChatterWho(s, out);
  const row = document.createElement("div");
  row.className = "chat-row " + (out ? "out" : "in");
  const secs = (Date.now() - Date.parse(s.sent_at)) / 1000;
  const when = isNaN(secs) ? "" : (typeof ago === "function" ? ago(secs) : Math.round(secs) + "s") + " ago";
  const state = s.state + (s.state === "delivered" && s.channel ? " by " + s.channel : "");
  const head = document.createElement("div");
  head.className = "chat-head";
  const dir = document.createElement("span");
  dir.className = "chat-dir";
  dir.textContent = out ? "to" : "from";
  head.appendChild(dir);
  let name;
  if (who.card) {
    name = document.createElement("button");
    name.type = "button";
    name.className = "chat-who link";
    name.dataset.tip = "open its card";
    name.onclick = e => { e.stopPropagation(); closePeek(); who.card.supervised ? attachTask(who.card.id) : openTask(who.card.id); };
  } else {
    name = document.createElement("span");
    name.className = "chat-who";
  }
  name.textContent = who.wire;
  head.appendChild(name);
  const meta = document.createElement("span");
  meta.className = "chat-meta";
  meta.textContent = [when, state].filter(Boolean).join(", ");
  head.appendChild(meta);
  const body = document.createElement("div");
  body.className = "chat-text";
  body.hidden = true;
  const full = s.text || s.preview || "";
  // s.chars counts runes, so the kept text is counted in runes too, not in UTF-16 units.
  body.textContent = full + (s.text && Array.from(s.text).length < s.chars ? `\n[cut: the say was ${s.chars} characters]` : "");
  const toggle = document.createElement("button");
  toggle.type = "button";
  toggle.className = "chat-toggle";
  toggle.textContent = s.preview || "(empty)";
  toggle.setAttribute("aria-expanded", "false");
  toggle.onclick = () => {
    body.hidden = !body.hidden;
    toggle.classList.toggle("open", !body.hidden);
    toggle.setAttribute("aria-expanded", String(!body.hidden));
  };
  row.append(head, toggle, body);
  return row;
}

// `first` is the page read when the fill began. A repaint of the same box reuses it rather than asking again.
function peekChatter(box, id, first) {
  const sec = document.createElement("div");
  sec.className = "peek-sec chatter";
  sec.hidden = true;
  box.appendChild(sec);
  sec.dataset.drawer = "chatter";
  sec.dataset.label = "messages between agents";
  sec.dataset.help = "Messages other agent sessions sent this card, and what it sent them. Folded because they rarely need you. Your own typing is not counted.";
  peekDrawers(box);
  const drawer = sec.closest(".peek-drawer");
  drawer.hidden = true;
  const paint = (page, replace) => {
    if (!page || !page.says) return;
    if (replace) sec.textContent = "";
    if (!sec.querySelector(".chat-title")) {
      const t = document.createElement("div");
      t.className = "chat-title";
      t.textContent = "messages between agents";
      sec.appendChild(t);
    }
    sec.querySelector(".chat-more")?.remove();
    for (const s of page.says) sec.appendChild(peekChatterRow(id, s));
    sec.hidden = !sec.querySelector(".chat-row");
    drawer.hidden = sec.hidden;
    const n = sec.querySelectorAll(".chat-row").length;
    drawer.querySelector(".peek-dsum").textContent = n + (page.more ? " or more" : "");
    if (page.more && page.says.length) {
      const more = document.createElement("button");
      more.type = "button";
      more.className = "chat-more";
      more.textContent = "more";
      more.onclick = async () => {
        more.disabled = true;
        const next = await peekChatterLoad(id, page.says[page.says.length - 1].id);
        if (!sec.isConnected) return;
        if (!next) { more.disabled = false; return; }
        paint(next, false);
      };
      sec.appendChild(more);
    }
  };
  first.then(page => { if (sec.isConnected) paint(page, true); });
}

// DRAWERS. Every section under the header is a drawer with a one-line summary in its header: NEEDS YOU open, the rest
// shut. The open or shut state is kept per SECTION (not per card) in `atrium.peekDrawers`, a JSON object of
// key -> boolean, every read and write inside try/catch. A section marks itself with data-drawer="key", data-label and
// data-sum, and `peekDrawers` wraps it after the body is drawn, so the body's markup stays what it was. The header
// and the actions row are not drawers. Closed bodies are `hidden`, not removed, so their text is still in the box.
const PEEK_DRAWER_KEY = "atrium.peekDrawers";
const PEEK_DRAWERS_OPEN = { need: true };
// recap and context are always open (no toggle); chatter starts folded every time and is never kept.
const PEEK_DRAWERS_FIXED = { recap: true, ctx: true }, PEEK_DRAWERS_FORGET = { chatter: true };
// A forgotten drawer opened while the usage read was out stays open when the box is drawn again.
let peekHeldOpen = "";
function peekDrawerState() {
  let o = {};
  try { o = JSON.parse(localStorage.getItem(PEEK_DRAWER_KEY) || "{}") || {}; } catch (e) {}
  return o && typeof o === "object" ? o : {};
}
function peekDrawerOpen(key) {
  if (PEEK_DRAWERS_FIXED[key]) return true;
  if (PEEK_DRAWERS_FORGET[key]) return peekHeldOpen === key;
  const v = peekDrawerState()[key];
  return typeof v === "boolean" ? v : !!PEEK_DRAWERS_OPEN[key];
}
function peekDrawerSet(key, open) {
  if (PEEK_DRAWERS_FORGET[key]) return;
  const o = peekDrawerState();
  o[key] = open;
  try { localStorage.setItem(PEEK_DRAWER_KEY, JSON.stringify(o)); } catch (e) {}
}
function peekDrawerWrap(node) {
  const key = node.dataset.drawer;
  const d = document.createElement("div");
  d.className = "peek-drawer";
  d.dataset.key = key;
  const head = document.createElement("div");
  head.className = "peek-dh";
  head.setAttribute("role", "button");
  head.tabIndex = 0;
  const label = document.createElement("b");
  label.textContent = node.dataset.label || key;
  const sum = document.createElement("span");
  sum.className = "peek-dsum";
  sum.textContent = node.dataset.sum || "";
  head.append(label, sum);
  if (node.dataset.help) {
    // The board's usual "?" bubble. It sits in a header that toggles, so neither a click nor a key on it may.
    const help = document.createElement("span");
    help.className = "help";
    help.tabIndex = 0;
    help.dataset.tip = node.dataset.help;
    help.textContent = "?";
    help.addEventListener("click", e => e.stopPropagation());
    help.addEventListener("keydown", e => e.stopPropagation());
    head.appendChild(help);
  }
  const body = document.createElement("div");
  body.className = "peek-dbody";
  const set = open => {
    d.classList.toggle("open", open);
    head.setAttribute("aria-expanded", String(open));
    body.hidden = !open;
  };
  const flip = () => {
    const open = !d.classList.contains("open");
    set(open);
    peekDrawerSet(key, open);
    peekRecap(d);
  };
  if (PEEK_DRAWERS_FIXED[key]) {
    d.classList.add("fixed");
    head.removeAttribute("role");
    head.removeAttribute("tabindex");
    node.replaceWith(d);
    body.appendChild(node);
    d.append(head, body);
    set(true);
    return;
  }
  // A drag that selected text is not a click on the header.
  head.addEventListener("click", () => {
    const sel = window.getSelection && window.getSelection();
    if (sel && !sel.isCollapsed && head.contains(sel.anchorNode)) return;
    flip();
  });
  head.addEventListener("keydown", e => {
    if (e.key !== "Enter" && e.key !== " ") return;
    e.preventDefault();
    flip();
  });
  node.replaceWith(d);
  body.appendChild(node);
  d.append(head, body);
  set(peekDrawerOpen(key));
}
function peekDrawers(box) {
  for (const n of box.querySelectorAll("[data-drawer]")) if (!n.closest(".peek-drawer")) peekDrawerWrap(n);
}

// The recap's tail behind a click: shown only where the text runs past its four lines.
function peekRecap(box) {
  const text = box.querySelector(".peek-rtext"), more = box.querySelector(".peek-more");
  if (typeof qaClamp === "function") qaClamp(box);
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
  // PAUSED CAN BE RESUMED, DEAD CANNOT. A paused card has one row to resume it and no "new agent here", since its own
  // conversation is the thing to pick up. A dead card whose folder is still there has "new agent here" and no resume,
  // since there is no conversation to go back to. `cardPaused`, js/cardrules.js.
  if (cardPaused(t)) {
    add("resume", "start its agent again on the conversation it had", () =>
      cannotResume(t) ? toast("cannot resume", cannotResume(t)) : t.status === "paused" ? resumePausedCard(id) : resumeParked(id));
  } else if (cardState(t) === "off" && !t.supervised && t.worktree && !termClosedGone(t)) {
    add("new agent here", "start a fresh agent in this card's folder, on this card", () =>
      openLaunch(t.runner || null, "", t.worktree, id, null, "here"));
  }
  // Nothing runs on an off card (cardState, js/cardrules.js): no one to say it to and nothing to restart.
  const running = cardState(t) !== "off";
  if (running) add("say", "queue a message for it", () => peekSay(id));
  if (t.supervised && t.status !== "done") {
    add("exit", "ask it to exit with its own keys. its card stays", () => oneAtATime("kill:" + bareId(id), () => exitTermNow(t)));
    if (running) add("restart", "exit it with its own keys and resume the same conversation on this card", () => restartCard(id));
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
  const chat = peekChatterLoad(id);
  box.innerHTML = peekBody(t, null);
  peekDocs(box, id);
  peekChatter(box, id, chat);
  peekDrawers(box);
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
  peekHeldOpen = box.querySelector('.peek-drawer[data-key="chatter"].open') ? "chatter" : "";
  box.innerHTML = peekBody(peekCard(id) || t, v);
  peekDocs(box, id);
  peekChatter(box, id, chat);
  peekDrawers(box);
  peekHeldOpen = "";
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
  if (e.pointerType && e.pointerType !== "mouse") return;
  peekPointer = { x: e.clientX, y: e.clientY };
  // The second counts from rest: a pointer still travelling across the card starts it again.
  if (peekHoverFrom && peekHoverId && Math.hypot(e.clientX - peekHoverFrom.x, e.clientY - peekHoverFrom.y) > PEEK_HOVER_JITTER_PX) peekArmHover(peekHoverId);
}, { passive: true, capture: true });

// Under the pointer `at`: its top left corner just below and right of it,
// pushed in from any edge it would cross, and above the pointer when there is
// no room below. With no pointer known (a menu opened from the keyboard) it
// goes under the anchor, the card's element or a point.
let peekLast = null;
function peekPlace(anchor, at) {
  const el = peekPop();
  peekLast = { anchor, at };
  if (!at) {
    const r = anchor.getBoundingClientRect ? anchor.getBoundingClientRect() : null;
    at = r ? { x: r.left, y: r.bottom } : { x: anchor.x, y: anchor.y };
  }
  popFit(el, { x: at.x + 4, y: at.y }, { gap: 12, slide: true });
}

// Fit the popup again where it was, after its content changed size (a question opened to its full text).
function peekRefit() {
  if (peekLast && peekEl && peekEl.classList.contains("on")) peekPlace(peekLast.anchor, peekLast.at);
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

let peekHoverId = null, peekHoverEl = null, peekHoverTimer = null, peekHoverFrom = null;

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
  peekHoverFrom = null;
  peekHoverId = id;
  peekHoverEl = card;
  if (peekMode === "hover" && peekFor !== id) peekCloseSoon();
  if (!id) return;
  peekArmHover(id);
});

// Starts the second from where the pointer is now.
function peekArmHover(id) {
  clearTimeout(peekHoverTimer);
  peekHoverFrom = peekPointer ? { x: peekPointer.x, y: peekPointer.y } : null;
  peekHoverTimer = setTimeout(() => {
    peekHoverFrom = null;
    if (peekHoverId !== id || !peekCanHover()) return;
    const at = peekHoverEl && peekHoverEl.isConnected ? peekHoverEl
      : document.querySelector(`.card[data-id="${CSS.escape(id)}"], .stackrow[data-id="${CSS.escape(id)}"]`);
    if (at && peekMode !== "menu") openPeek(id, at, "hover");
  }, PEEK_HOVER_MS);
}

// A press is a click on the card, which opens its menu. Nothing pops over it.
document.addEventListener("pointerdown", e => {
  clearTimeout(peekHoverTimer);
  peekHoverFrom = null;
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
  termDrawerWatch();
  termDrawerFit();
  peekFill(document.getElementById("t-drawer-body"), id, () => seq === termDrawerSeq && termDrawerOpen).then(termDrawerFit);
}

// Stacked when the two panels fit above the strip, else side by side. The one measurement: the stacked height against
// the room between the top of the viewport and the strip. The room is also each panel's own height cap, so a panel
// still too tall scrolls inside itself. The layout itself is CSS, flipped by the `wide` class.
function termDrawerFit() {
  const drawer = document.getElementById("t-drawer");
  const foot = drawer && drawer.parentElement;
  if (!drawer || !termDrawerOpen) return;
  // The top is the viewport's or, when the strip sits in a pane that clips, the pane's.
  let top = 0;
  for (let n = foot.parentElement; n && n !== document.body; n = n.parentElement) {
    if (getComputedStyle(n).overflowY !== "visible") { top = Math.max(top, n.getBoundingClientRect().top); break; }
  }
  // The filter strip is above the pane and over this drawer (its z-index), so the room starts under it, or its search
  // box would take the clicks meant for the drawer's top rows.
  const strip = document.getElementById("fd-terms");
  if (strip && strip.offsetParent) top = Math.max(top, strip.getBoundingClientRect().bottom);
  const room = Math.max(120, Math.floor(foot.getBoundingClientRect().top - top - 8 - 8));
  drawer.style.setProperty("--drawer-room", room + "px");
  drawer.classList.remove("wide");
  const card = document.getElementById("t-drawer-body");
  const dbg = document.getElementById("t-drawer-debug");
  const stacked = card.offsetHeight + (dbg && !dbg.hidden ? 8 + dbg.offsetHeight : 0);
  if (stacked > room) drawer.classList.add("wide");
}

let termDrawerRO = null;
function termDrawerWatch() {
  if (termDrawerRO || typeof ResizeObserver !== "function") return;
  let queued = false;
  const again = () => {
    if (queued) return;
    queued = true;
    requestAnimationFrame(() => { queued = false; termDrawerFit(); });
  };
  termDrawerRO = new ResizeObserver(again);
  for (const id of ["t-drawer-body", "t-drawer-debug"]) {
    const el = document.getElementById(id);
    if (el) termDrawerRO.observe(el);
  }
  window.addEventListener("resize", again);
}

// The terminal moved to another card, or closed. An open drawer follows it.
function termDrawerFollow(task) {
  if (!termDrawerOpen) return;
  if (!task) { toggleTermDrawer(false); return; }
  toggleTermDrawer(true);
}
