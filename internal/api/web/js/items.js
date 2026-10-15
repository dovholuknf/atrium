// The items tab: every backlog item the hub holds, from designed to pushed. HUB-ONLY, like the backlog: `/_hub/items` is a 404 on a
// plain daemon, so the tab shows once the hub probe in rooms.js has found a hub, through `paintItemsTab`.
// The look is docs/rnd/item-lifecycle-design.md section 7 (list, detail, blocked), in the board's own tokens: css/items.css.
//
// THIS FILE ONLY DRAWS. Where the data comes from is js/items-data.js (`itemsData`), the one file that changes when the hub's
// step, git and cost routes land. Every number it shows is already an item field, and every dollar is an ESTIMATE from real token
// counts, with the tokens always beside it.
//
// The LIST: a strip of four numbers, then groups in this order: blocked (waiting on a person, the oldest question first), running,
// queued, finished. A row has the title and its ids, an eight-segment progress bar, the tokens and dollars and the age. A blocked
// row carries its question, and `answer` opens the answer box on the row itself, so answering is no trip to the detail.
// The DETAIL is a page: a click on a row opens it, the crumb or `back` returns. Its timeline is one card per step.
//
// The board acts as the human owner: it sends `by: board`, which the hub takes from its own machine only (internal/link/items.go,
// ownerMayAct). `cancel` is behind a confirm on every item that is not finished.
//
// The count on the tab is the items blocked on the human. Live by the hub's `item` event, and polled every 5s while the tab is on
// screen as a backstop, since an event is not guaranteed to reach a board that was asleep. See docs/changes/items-view.md and
// docs/changes/item-lifecycle-l6.md.
let itemsList = [];
let itemsStats = null;
let itemsOpen = null;          // the id whose detail page is open
let itemsDetail = null;        // what the detail answered for it
let itemsFilter = "all";       // all, a department, or mine
const itemsFolded = {};        // a group folded shut, by state
const itemsAsking = {};        // a blocked row whose answer box is open, by id
let itemsShowAll = false;      // every finished row, not only the newest few
const itemsDraft = {};         // an answer typed and not yet sent, by id, so a repaint does not eat it
let itemsTimer = 0;

const ITEM_GROUPS = [
  { state: "blocked", label: "blocked" },
  { state: "running", label: "running" },
  { state: "queued", label: "queued" },
  { state: "finished", label: "finished" },
];
const ITEMS_FINISHED_SHOWN = 5;
// What a step that has not been reached says it will do, until the hub has a fact to say instead.
const ITEM_STEP_HINT = {
  designed: "a designer writes the design doc", approved: "you approve the design, or send it back", building: "a builder works on a branch",
  tested: "the room runs the touched tests at the building tip", reviewed: "a reviewer takes the branch",
  landed: "fast-forward onto claude/main", deployed: "rooms and build", pushed: "origin/main"
};
const ITEM_STEP_VERB = { designed: "designing", approved: "waiting", building: "building", tested: "testing", reviewed: "reviewing",
  landed: "landing", deployed: "deploying", pushed: "pushing" };

function paintItemsTab() {
  const tab = document.querySelector('.tab[data-view="items"]');
  if (!tab) return;
  tab.hidden = typeof hubIsHub === "undefined" ? true : !hubIsHub;
  if (!tab.hidden) loadItemsBadge();
}

function itemsVisible() {
  const v = document.getElementById("items");
  return !!v && !v.hidden && !document.hidden;
}

function itemBlockedOnHuman(it) { return it.state === "blocked" && it.owner_kind === "human"; }

function itemsBadge(n) {
  const el = document.getElementById("c-items");
  if (!el) return;
  el.textContent = n || "";
  el.classList.toggle("zero", !n);
}

// The count alone, for when the tab is not the one on screen.
async function loadItemsBadge() {
  try { itemsBadge(await itemsData.blockedCount()); } catch (e) { /* the badge keeps what it had */ }
}

async function loadItems() {
  const box = document.getElementById("items-list");
  if (!box) return;
  try {
    const a = await itemsData.list();
    itemsList = a.items;
    itemsStats = a.stats;
    itemsBadge(itemsList.filter(itemBlockedOnHuman).length);
    itemsPaint();
  } catch (e) {
    box.innerHTML = '<p class="pane-lead">The items are not answering right now.</p>';
  }
  if (itemsOpen) loadItemDetail(itemsOpen, true);
}

// ---- numbers and words ----

function itemsMs(iso) {
  const t = iso ? Date.parse(iso) : NaN;
  return isNaN(t) ? null : t;
}

// "17m", "1h 39m", "3h 04m", "1d 2h": how long, from two instants or one and now.
function itemDur(from, to) {
  const a = itemsMs(from);
  if (a == null) return "";
  const b = to ? itemsMs(to) : Date.now();
  if (b == null) return "";
  const m = Math.max(0, Math.round((b - a) / 60000));
  if (m < 1) return "just now";
  if (m < 60) return m + "m";
  if (m < 1440) return Math.floor(m / 60) + "h " + String(m % 60).padStart(2, "0") + "m";
  return Math.floor(m / 1440) + "d " + Math.floor((m % 1440) / 60) + "h";
}

// The short form, for how long ago something was done: "1h", "1d".
function itemAge(iso) {
  const t = itemsMs(iso);
  if (t == null) return "";
  const s = Math.max(0, (Date.now() - t) / 1000);
  if (s < 45) return "just now";
  if (s < 3600) return Math.round(s / 60) + "m";
  if (s < 86400) return Math.round(s / 3600) + "h";
  return Math.round(s / 86400) + "d";
}

function itemClock(iso) {
  const t = itemsMs(iso);
  if (t == null) return "";
  const d = new Date(t);
  return String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
}

function itemTokN(t) { return t ? t.in + t.cw + t.cr + t.out : 0; }

// 412k, 2.3M. `fine` gives the millions a second decimal, as the detail's total does.
function itemTok(n, fine) {
  if (n < 1000) return String(n);
  if (n < 1e6) return Math.round(n / 1000) + "k";
  return (n / 1e6).toFixed(fine ? 2 : 1) + "M";
}

// A dollar figure. Null is a price the hub does not have, and says so: "?" and never "$0.00".
function itemUsd(v) { return v == null ? "?" : "$" + v.toFixed(2); }
function itemUsd1(v) { return v == null ? "?" : "$" + v.toFixed(1); }

function itemCancelled(it) { return it.state === "finished" && /cancelled/i.test(it.note || ""); }
function itemStateWord(it) { return itemCancelled(it) ? "cancelled" : it.state; }

function itemCard(it) {
  if (typeof lastTasks === "undefined") return null;
  return lastTasks.find(x => x.id === it.card && (!x.room || !it.room || x.room === it.room)) || null;
}

// The board has no title for an older item, so the card that runs it supplies one.
function itemTitle(it) {
  if (it.title) return it.title;
  const t = itemCard(it);
  return (t && (t.display_title || t.title)) || it.card || it.id;
}

// The chips for a card: the board's name for it when it has one.
function itemCards(it) {
  return (it.cards || []).map(c => {
    const t = typeof lastTasks === "undefined" ? null : lastTasks.find(x => x.id === c.id && (!x.room || !c.room || x.room === c.room));
    return { name: (t && t.wire_name) || c.name || c.id, id: c.id };
  });
}

function itemOwnerText(it) {
  if (it.owner_kind === "human") return it.owner || "you";
  if (it.owner_kind === "role") return it.owner + " role";
  return it.owner || "";
}

function itemChip(name, id) {
  if (!id) return '<span class="it-card">' + esc(name) + "</span>";
  return '<button type="button" class="it-card" data-act="card" data-card="' + esc(id) + '" data-tip="open this card\'s terminal">' + esc(name) + "</button>";
}

function itemStack(t, cls) {
  const n = itemTokN(t);
  if (!n) return '<div class="it-stack ' + (cls || "") + '"></div>';
  return '<div class="it-stack ' + (cls || "") + '">' + ["in", "cw", "cr", "out"].map(k => '<i class="t-' + k + '" style="width:' + (t[k] * 100 / n) + '%"></i>').join("") + "</div>";
}

// ---- the list ----

function itemsMatches(it) {
  if (itemsFilter === "all") return true;
  if (itemsFilter === "mine") return it.owner_kind === "human";
  return it.dept === itemsFilter;
}

function itemStatCards(s) {
  const who = s.who || "you";
  const spend = s.spend_today == null ? '<div class="it-v">&ndash;<small>no usage yet</small></div>'
    : '<div class="it-v">' + itemUsd1(s.spend_today) + "<small>incl. " + itemUsd1(s.spend_coord || 0) + " coordination</small></div>";
  const landed = s.landed_week == null ? '<div class="it-v">&ndash;<small>none yet</small></div>'
    : '<div class="it-v">' + s.landed_week + "<small>median " + esc(s.landed_median || "&ndash;") + ", " + itemUsd(s.landed_cost) + "</small></div>";
  return '<div class="it-stat' + (s.needs_you > 0 ? " hot" : "") + '" data-stat="needs"><div class="it-k">needs you</div><div class="it-v">' + s.needs_you +
    "<small>blocked on " + esc(who) + "</small></div></div>" +
    '<div class="it-stat" data-stat="flight"><div class="it-k">in flight</div><div class="it-v">' + s.in_flight + "<small>" + (s.working || 0) +
    " building or landing</small></div></div>" +
    '<div class="it-stat" data-stat="spend"><div class="it-k">spend today</div>' + spend + "</div>" +
    '<div class="it-stat" data-stat="landed"><div class="it-k">landed this week</div>' + landed + "</div>";
}

function itemProgress(it) {
  const blocked = it.state === "blocked";
  const seg = { done: "d", active: "c", blocked: "c", stale: "s", skipped: "k", pending: "" };
  return '<div class="it-prog' + (blocked ? " b" : "") + '">' + it.steps.map(s => '<span class="' + (seg[s.state] || "") + '" data-tip="' + esc(s.name) + '"></span>').join("") + "</div>";
}

// The line under the bar: the step by name, and how far along and how long.
function itemCurrentLine(it) {
  const i = itemCurrent(it), s = it.steps[i] || { name: "" }, of = it.steps.length;
  if (it.state === "finished") {
    const n = it.steps.filter(x => x.state === "done" || x.state === "skipped").length;
    const last = [...it.steps].reverse().find(x => x.state === "done" || x.state === "skipped");
    if (itemCancelled(it)) return { b: "cancelled", t: "", em: n + " of " + of + " steps" };
    return { b: n === of ? s.name : (last ? last.name : "finished"), t: "", em: (n === of ? "all " + of : n + " of " + of) + " steps · " + (last ? last.name : "finished") };
  }
  const step = "step " + (i + 1) + " of " + of;
  if (it.state === "blocked") {
    const since = it.ask && it.ask.since ? it.ask.since : it.updated_at;
    return { b: (it.owner_kind === "human" ? "waiting on you at " : "blocked at ") + s.name, t: "w", em: step + " · blocked " + itemDur(since) };
  }
  if (it.state === "queued") return { b: s.name, t: "t", em: step + " · queued " + itemDur(it.created_at) };
  return { b: s.name, t: "t", em: step + " · " + (s.note || (ITEM_STEP_VERB[s.name] || s.name) + " " + itemDur(s.from)) };
}

function itemRow(it) {
  const id = esc(it.id);
  const blocked = it.state === "blocked";
  const line = itemCurrentLine(it);
  const tokens = it.tokens;
  const n = itemTokN(tokens);
  const tokCell = n ? itemTok(n) + "<small>in " + itemTok(tokens.in) + " · cache " + itemTok(tokens.cw + tokens.cr) + " · out " + itemTok(tokens.out) + "</small>"
    : (it.state === "queued" ? "0<small>not started</small>" : "&ndash;<small>no usage yet</small>");
  const usdCell = it.usd == null && !n ? "&ndash;" : itemUsd(it.usd);
  const coord = it.coord_usd != null && (n || it.usd) ? "<small>+ " + itemUsd(it.coord_usd) + " coordination</small>" : "<small></small>";
  const age = it.state === "finished" ? "done " + itemAge(it.updated_at) : itemDur(it.created_at);
  const prio = it.priority ? '<span class="it-tag' + (it.priority === "high" ? " hi" : "") + '">' + esc(it.priority) + "</span>" : "";
  const dept = it.dept ? '<span class="it-tag">' + esc(it.dept) + "</span>" : "";
  const open = itemsOpen === it.id;
  let h = '<div class="it-row st-' + esc(itemStateWord(it)) + '" data-item="' + id + '">' +
    '<div class="it-main" role="button" tabindex="0" aria-expanded="' + open + '" data-act="open" data-id="' + id + '">' +
    '<div class="it-title">' + esc(itemTitle(it)) + "</div>" +
    '<div class="it-meta"><span class="it-id">' + esc(it.backlog) + "</span>" + prio + dept +
    itemCards(it).map(c => itemChip(c.name, c.id)).join("") + "</div></div>" +
    '<div class="it-pcol">' + itemProgress(it) + '<div class="it-cur"><b class="' + line.t + '">' + esc(line.b) + "</b><em>" + esc(line.em) + "</em></div>" +
    '<span class="it-state">' + esc(itemStateWord(it)) + "</span></div>" +
    '<div class="it-tok">' + tokCell + "</div>" +
    '<div class="it-usd">' + usdCell + coord + "</div>" +
    '<div class="it-age" data-tip="' + esc(it.updated_at || "") + '">' + esc(age) + "</div>";
  if (blocked) {
    const ask = it.ask || { text: it.note || "(no reason given)" };
    h += '<div class="it-ask"><b>asks you</b><span class="it-q">' + esc(ask.brief || ask.text) + "</span>" +
      '<button type="button" class="it-btn go" data-act="ask" data-id="' + id + '" aria-expanded="' + !!itemsAsking[it.id] + '">answer</button></div>';
    if (itemsAsking[it.id]) {
      h += '<div class="it-answer"><input type="text" class="it-answer-box" data-id="' + id + '" placeholder="answer" aria-label="answer for ' + id +
        '" value="' + esc(itemsDraft[it.id] || "") + '"><button type="button" class="it-btn go it-send" data-act="answer" data-id="' + id + '">send</button></div>';
    }
  }
  return h + "</div>";
}

function itemsGroupHtml(g) {
  let rows = itemsList.filter(it => it.state === g.state && itemsMatches(it));
  if (!rows.length) return "";
  // Inside blocked the oldest question is first. The rest stay as the hub gave them, newest change first.
  if (g.state === "blocked") rows = rows.slice().sort((a, b) => (itemsMs(a.ask && a.ask.since) || 0) - (itemsMs(b.ask && b.ask.since) || 0));
  const folded = !!itemsFolded[g.state];
  const shown = g.state === "finished" && !itemsShowAll ? rows.slice(0, ITEMS_FINISHED_SHOWN) : rows;
  const more = rows.length - shown.length;
  return '<section class="it-group" data-state="' + g.state + '"><button type="button" class="it-head" data-act="fold" data-state="' + g.state +
    '" aria-expanded="' + !folded + '"><span>' + g.label + '</span><span class="it-n">' + rows.length + "</span></button>" +
    (folded ? "" : shown.map(itemRow).join("") + (more > 0 ? '<button type="button" class="it-more" data-act="more">show ' + more + " more</button>" : "")) + "</section>";
}

function itemListHtml() {
  const depts = [...new Set(itemsList.map(it => it.dept).filter(Boolean))].sort();
  const pill = (f, label) => '<button type="button" class="it-pill' + (itemsFilter === f ? " on" : "") + '" data-act="filter" data-filter="' + esc(f) + '">' + esc(label) + "</button>";
  const groups = ITEM_GROUPS.map(itemsGroupHtml).join("");
  return '<div class="it-top"><h1>Items</h1><div class="it-sub">Every backlog item in flight, from designed to pushed. Spend is an estimate, priced from the room usage export.</div></div>' +
    '<div class="it-stats">' + itemStatCards(itemsStats || itemsStatsFrom(itemsList)) + "</div>" +
    '<div class="it-filters">' + pill("all", "all") + depts.map(d => pill(d, d)).join("") + pill("mine", "mine") +
    '<span class="it-sp"></span><span class="it-pill it-sort">sort: needs me first</span></div>' +
    (groups || '<p class="pane-lead">Nothing matches.</p>');
}

// ---- one item ----

function itemStateText(it) {
  if (it.state === "blocked") return it.owner_kind === "human" ? "blocked on you" : "blocked";
  if (it.state === "finished") return itemCancelled(it) ? "cancelled" : "finished";
  if (it.state === "queued") return "queued";
  const s = it.steps[itemCurrent(it)];
  return s ? (s.note || ITEM_STEP_VERB[s.name] || s.name) : "running";
}

function itemRange(it, s, i) {
  if (s.when) return s.when;
  if (s.state === "pending" || !s.from) return "";
  let r = itemClock(s.from) + " → " + (s.to ? itemClock(s.to) : "now") + " · " + itemDur(s.from, s.to);
  if (s.state === "blocked" && it.ask && it.ask.since) r += " · blocked " + itemDur(it.ask.since);
  return r;
}

function itemStepHtml(it, s, i) {
  const cls = { done: "d", active: "c", blocked: "b", stale: "w", skipped: "k", pending: "f" }[s.state] || "f";
  const known = {};
  for (const c of it.cards || []) known[c.name] = c.id;
  const who = (s.cards || []).map(c => (c.role === "human" ? '<span class="it-card">' + esc(c.name) + "</span>" : itemChip(c.name, known[c.name])) +
    "<span>" + esc(c.role || "") + "</span>").join("");
  const reached = s.state !== "pending";
  const fact = s.state === "skipped" ? "skipped: " + (s.reason || "not needed") : (s.fact || (reached ? "" : ITEM_STEP_HINT[s.name] || ""));
  const strength = s.strength ? '<span class="it-obs' + (s.strength === "reported" ? " r" : "") + '">' + esc(s.strength) + "</span>" : "";
  const sha = s.sha ? '<span class="it-sha">' + esc(s.sha) + "</span>" : "";
  const n = itemTokN(s.tokens);
  let right = '<div class="it-usd none">&ndash;</div>';
  if (reached && s.usd != null) {
    right = '<div class="it-usd">' + itemUsd(s.usd) + '</div><div class="it-tok">' + (n ? itemTok(n) + " tokens" : "no model turns") + "</div>" + itemStack(s.tokens);
  } else if (reached && n) right = '<div class="it-usd">?</div><div class="it-tok">' + itemTok(n) + " tokens</div>" + itemStack(s.tokens);
  return '<div class="it-step ' + cls + '" data-step="' + esc(s.name) + '"><div class="it-node">' +
    (s.state === "done" ? "✓" : s.state === "skipped" ? "–" : i + 1) + '</div><div class="it-sc"><div><div class="it-sn">' + esc(s.name) +
    "<small>" + esc(itemRange(it, s, i)) + "</small></div>" +
    '<div class="it-who">' + who + sha + strength + (fact ? '<span class="it-fact">' + esc(fact) + "</span>" : "") + "</div></div>" +
    '<div class="it-sr">' + right + "</div></div></div>";
}

function itemSideHtml(it) {
  const n = itemTokN(it.tokens);
  const reached = it.steps.filter(s => s.state !== "pending" && s.usd != null);
  const max = Math.max(0.0001, ...reached.map(s => s.usd));
  const coord = it.coordination || [];
  const cardRows = {};
  for (const s of it.steps) {
    for (const c of s.cards || []) {
      if (c.coord || c.role === "human" || s.state === "pending") continue;
      const r = cardRows[c.name] || (cardRows[c.name] = { steps: [], usd: 0 });
      r.steps.push(s.name);
      if (c === s.cards.find(x => !x.coord && x.role !== "human")) r.usd += s.usd || 0;
    }
  }
  const known = {};
  for (const c of it.cards || []) known[c.name] = c.id;
  const kv = (left, usd) => '<div class="it-kv"><span>' + left + "</span><b>" + usd + "</b></div>";
  const total = it.coord_usd == null || it.usd == null ? null : it.usd + it.coord_usd;
  const done = it.state === "finished";
  return '<div class="it-side"><div><h3>Item total &middot; estimated</h3><div class="it-big">' + (it.usd == null && !n ? "&ndash;" : itemUsd(it.usd)) +
    "<small>" + (done ? "total" : "so far") + " &middot; " + (n ? itemTok(n, true) + " tokens" : "no usage yet") + "</small></div>" +
    itemStack(it.tokens, "tall") +
    '<div class="it-leg"><span><i class="t-in"></i>input</span><span><i class="t-cw"></i>cache write</span><span><i class="t-cr"></i>cache read</span><span><i class="t-out"></i>output</span></div></div>' +
    "<div><h3>Cost by step</h3>" + reached.map(s => '<div class="it-bar"><span>' + esc(s.name) + '</span><div><i style="width:' + (s.usd * 100 / max) + '%"></i></div><b>' + itemUsd(s.usd) + "</b></div>").join("") +
    (coord.length ? '<div class="it-kv it-sum"><span>coordination (' + esc(coord.map(c => c.name.replace(/-\d+$/, "")).sort().join(", ")) + ")</span><b>" + itemUsd(it.coord_usd) + "</b></div>" +
      (total == null ? "" : '<div class="it-kv"><span>with coordination</span><b>' + itemUsd(total) + "</b></div>") : "") + "</div>" +
    "<div><h3>Cards on this item</h3>" +
    Object.keys(cardRows).map(name => kv(itemChip(name, known[name]) + ' <span class="it-dim">' + esc(cardRows[name].steps.join(", ")) + "</span>", itemUsd(cardRows[name].usd))).join("") +
    coord.map(c => kv(itemChip(c.name, known[c.name]) + ' <span class="it-dim">' + esc(c.note || "") + "</span>", itemUsd(c.usd))).join("") +
    (!Object.keys(cardRows).length && !coord.length ? '<div class="it-kv"><span class="it-dim">no cards yet</span></div>' : "") + "</div>" +
    "<div><h3>Prices used, per million tokens</h3>" + (it.prices || []).map(p => kv(esc(p.k), esc(p.v))).join("") +
    (it.price_table ? kv("price table", esc(it.price_table + ", estimated")) : "") + "</div></div>";
}

function itemQuestionHtml(it) {
  if (it.state !== "blocked") return "";
  const ask = it.ask || { text: it.note || "(no reason given)", options: [] };
  const id = esc(it.id);
  const since = ask.since ? " · blocked " + itemDur(ask.since) : "";
  const opts = (ask.options || []).slice(0, 3);
  return '<div class="it-qbox"><h2>asks you' + (ask.who ? " · " + esc(ask.who) : "") + esc(since) + '</h2><p class="it-q">' + esc(ask.text) + "</p>" +
    (opts.length ? '<div class="it-opts">' + opts.map((o, n) => '<button type="button" class="it-btn' + (n === 0 ? " go" : "") + '" data-act="option" data-id="' + id +
      '" data-i="' + n + '">' + esc(o) + "</button>").join("") + "</div>" : "") +
    '<div class="it-ans"><input type="text" class="it-answer-box" data-id="' + id + '" placeholder="or type an answer to the worker" aria-label="answer for ' + id +
    '" value="' + esc(itemsDraft[it.id] || "") + '"><button type="button" class="it-btn go it-send" data-act="answer" data-id="' + id + '">send</button></div></div>';
}

function itemDetailHtml(it) {
  const d = itemsDetail && itemsDetail.id === it.id ? itemsDetail : it;
  const hist = d.history || [];
  const meta = '<span class="it-id">' + esc(d.backlog) + "</span>" + (d.priority ? '<span class="it-tag' + (d.priority === "high" ? " hi" : "") + '">' + esc(d.priority) + "</span>" : "") +
    (d.dept ? '<span class="it-tag">' + esc(d.dept) + "</span>" : "") +
    (d.created_by ? "<span>filed by " + esc(d.created_by) + "</span><span>&middot;</span>" : "") + "<span>owner " + esc(itemOwnerText(d)) + "</span>" +
    (d.room ? "<span>&middot;</span><span>room " + esc(d.room) + "</span>" : "");
  const blockedCls = d.state === "blocked" ? " b" : "";
  return '<div class="it-detail" data-detail="' + esc(d.id) + '"><div class="it-crumb"><a href="#" data-act="back">Items</a> &rsaquo; ' + esc(d.backlog) + "</div>" +
    '<div class="it-headrow"><div class="it-l"><span class="it-pillstate' + blockedCls + '">' + esc(itemStateText(d)) + "</span><h1>" + esc(itemTitle(d)) + "</h1>" +
    '<div class="it-meta big">' + meta + "</div>" + (d.body ? '<div class="it-bodyex">' + esc(d.body) + "</div>" : "") +
    (d.note && d.state === "finished" ? '<div class="it-bodyex">' + esc(d.note) + "</div>" : "") + "</div>" +
    '<div class="it-actions"><button type="button" class="it-btn" data-act="backlog" data-backlog="' + esc(d.backlog) + '">open backlog item</button>' +
    (d.state !== "finished" ? '<button type="button" class="it-btn it-cancel" data-act="cancel" data-id="' + esc(d.id) + '" aria-label="cancel item ' + esc(d.id) + '">cancel item</button>' : "") +
    "</div></div>" + itemQuestionHtml(d) +
    '<div class="it-grid"><div class="it-tl">' + d.steps.map((s, i) => itemStepHtml(d, s, i)).join("") +
    (hist.length ? '<ol class="it-history">' + hist.map(e => "<li><b>" + esc(e.state) + "</b> r" + e.revision + ' <time data-tip="' + esc(e.at) + '">' + esc(itemAge(e.at)) + " ago</time></li>").join("") + "</ol>" : "") +
    "</div>" + itemSideHtml(d) + "</div></div>";
}

function itemsPaint() {
  const box = document.getElementById("items-list");
  if (!box) return;
  // What is being typed survives the repaint: the answer box, with the caret where it was.
  const had = document.activeElement && document.activeElement.classList && document.activeElement.classList.contains("it-answer-box")
    ? { id: document.activeElement.dataset.id, at: document.activeElement.selectionStart } : null;
  for (const el of box.querySelectorAll(".it-answer-box")) itemsDraft[el.dataset.id] = el.value;
  const open = itemsOpen && (itemsList.find(x => x.id === itemsOpen) || (itemsDetail && itemsDetail.id === itemsOpen ? itemsDetail : null));
  if (open) box.innerHTML = itemDetailHtml(open);
  else if (itemsOpen) box.innerHTML = '<p class="pane-lead">Loading ' + esc(itemsOpen) + "&hellip;</p>";
  else if (!itemsList.length) box.innerHTML = '<p class="pane-lead">No items.</p>';
  else box.innerHTML = itemListHtml();
  if (had) {
    const el = box.querySelector('.it-answer-box[data-id="' + CSS.escape(had.id) + '"]');
    if (el) { el.focus(); try { el.setSelectionRange(had.at, had.at); } catch (e) { /* not a text field */ } }
  }
}

async function loadItemDetail(id, quiet) {
  try {
    const d = await itemsData.detail(id);
    if (itemsOpen !== id) return;
    itemsDetail = d;
    if (!quiet || document.querySelector('.it-detail[data-detail="' + CSS.escape(id) + '"]')) itemsPaint();
  } catch (e) { /* the page still shows what the list had */ }
}

function itemsNote(text) {
  const n = document.getElementById("items-note");
  if (n) n.textContent = text || "";
}

async function itemsAct(id, body) {
  itemsNote("");
  try {
    await itemsData.act(id, body);
    return true;
  } catch (e) {
    itemsNote("Could not " + body.do + " " + id + ": " + e.message);
    return false;
  }
}

async function itemsAnswer(id, text) {
  const box = document.querySelector('.it-answer-box[data-id="' + CSS.escape(id) + '"]');
  const answer = (text != null ? text : box ? box.value : "").trim();
  if (!answer) { itemsNote("Type the answer first."); if (box) box.focus(); return; }
  if (await itemsAct(id, { do: "unblock", answer })) {
    delete itemsDraft[id];
    delete itemsAsking[id];
    // The room moves the item to running only once the worker's turn starts, so say what happened rather than wait.
    itemsNote("Sent the answer for " + id + ". It shows as running once the worker starts its turn.");
  }
  loadItems();
}

async function itemsCancel(id) {
  const ok = await askUser({
    title: "cancel " + id + "?",
    body: "The item is finished as <b>cancelled</b> and its worker is asked to leave. This cannot be undone.",
    buttons: [{ label: "keep it", value: null }, { label: "cancel the item", value: true, style: "go" }]
  });
  if (!ok) return;
  await itemsAct(id, { do: "cancel" });
  loadItems();
}

async function itemsOpenCard(cardId) {
  if (!cardId) return;
  if (typeof attachTask !== "function") return;
  if (typeof switchView === "function") switchView("terms");
  await attachTask(cardId);
}

// The backlog tab, with the item's row brought into view once the backlog has drawn.
function itemsOpenBacklog(id) {
  if (typeof switchView !== "function") return;
  switchView("backlog");
  setTimeout(() => {
    const row = document.querySelector('#backlog-items [data-id="' + CSS.escape(id) + '"]');
    if (row && row.scrollIntoView) row.scrollIntoView({ block: "center" });
  }, 400);
}

// Opens the item's page, or goes back to the list when it is the one already open.
function itemsToggle(id) { itemsShow(itemsOpen === id ? null : id); }

function itemsShow(id) {
  itemsOpen = id;
  itemsDetail = null;
  itemsPaint();
  const sc = document.getElementById("items-list");
  if (sc) sc.scrollTop = 0;
  if (itemsOpen) loadItemDetail(itemsOpen, false);
}

// Called from switchView when the tab opens, and by the poll.
function startItems() {
  loadItems();
  clearInterval(itemsTimer);
  itemsTimer = setInterval(() => {
    if (itemsVisible()) loadItems();
    else { clearInterval(itemsTimer); itemsTimer = 0; }
  }, 5000);
}

function onItemEvent() {
  if (itemsVisible()) loadItems(); else loadItemsBadge();
}

document.addEventListener("DOMContentLoaded", () => {
  const box = document.getElementById("items-list");
  if (!box) return;
  box.addEventListener("click", ev => {
    const b = ev.target.closest("[data-act]");
    if (!b) {
      // The rest of a row opens it too, but not the question under it.
      const row = ev.target.closest(".it-row");
      if (row && !ev.target.closest(".it-ask, .it-answer")) itemsToggle(row.dataset.item);
      return;
    }
    const act = b.dataset.act;
    if (act === "back") { ev.preventDefault(); itemsShow(null); }
    else if (act === "fold") { itemsFolded[b.dataset.state] = !itemsFolded[b.dataset.state]; itemsPaint(); }
    else if (act === "more") { itemsShowAll = true; itemsPaint(); }
    else if (act === "filter") { itemsFilter = b.dataset.filter; itemsPaint(); }
    else if (act === "ask") {
      itemsAsking[b.dataset.id] = !itemsAsking[b.dataset.id];
      itemsPaint();
      const el = box.querySelector('.it-answer-box[data-id="' + CSS.escape(b.dataset.id) + '"]');
      if (el) el.focus();
    }
    else if (act === "answer") itemsAnswer(b.dataset.id);
    else if (act === "option") {
      const it = itemsDetail && itemsDetail.id === b.dataset.id ? itemsDetail : itemsList.find(x => x.id === b.dataset.id);
      const o = it && it.ask && it.ask.options ? it.ask.options[+b.dataset.i] : "";
      if (o) itemsAnswer(b.dataset.id, o);
    }
    else if (act === "cancel") itemsCancel(b.dataset.id);
    else if (act === "card") itemsOpenCard(b.dataset.card);
    else if (act === "backlog") itemsOpenBacklog(b.dataset.backlog);
    else if (act === "open") itemsToggle(b.dataset.id);
  });
  box.addEventListener("keydown", ev => {
    if (ev.target.classList.contains("it-answer-box") && ev.key === "Enter") { ev.preventDefault(); itemsAnswer(ev.target.dataset.id); }
    else if (ev.target.classList.contains("it-main") && (ev.key === "Enter" || ev.key === " ")) { ev.preventDefault(); itemsToggle(ev.target.dataset.id); }
  });
  box.addEventListener("input", ev => {
    if (ev.target.classList.contains("it-answer-box")) itemsDraft[ev.target.dataset.id] = ev.target.value;
  });
});
