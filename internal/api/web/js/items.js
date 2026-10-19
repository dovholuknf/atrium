// The items tab: every backlog item the hub holds, from designed to pushed. HUB-ONLY, like the backlog: `/_hub/items` is a 404 on a
// plain daemon, so the tab shows once the hub probe in rooms.js has found a hub, through `paintItemsTab`.
// The look is docs/rnd/item-lifecycle-design.md section 7 (list, detail, blocked), in the board's own tokens: css/items.css.
//
// THIS FILE ONLY DRAWS. Where the data comes from is js/items-data.js (`itemsData`), the one file that changes when the hub's
// step, git and cost routes land. Every number it shows is already an item field, and every dollar is an ESTIMATE from real token
// counts, with the tokens always beside it.
//
// The LIST (docs/ui/items-tab-ux-pass.md): one row per BACKLOG id, its cards folded under it (items-data.js `foldItems`). A line of
// count chips, a legend, then groups: needs you (the oldest question first), idle, working, queued, finished. A row has the title and
// its backlog id, an eight-segment bar, and "step 3 of 8 · building". A row that needs you carries the real question, who asked and
// how long ago, and `answer` opens the answer box on the row itself, so answering is no trip to the detail. Two columns from 1700px.
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
let itemsPage = "";            // "" is the list, "efficacy" is the efficacy page (js/efficacy.js)
let itemsFilter = "all";       // all, a department, or mine
const itemsFolded = {};        // a group folded shut, by state
const itemsAsking = {};        // a blocked row whose answer box is open, by id
let itemsShowAll = false;      // every finished row, not only the newest few
const itemsDraft = {};         // an answer typed and not yet sent, by id, so a repaint does not eat it
let itemsTimer = 0;
let itemsListScroll = 0;       // where the list was scrolled when an item was opened
let itemsHtml = "";            // what the box last drew, so a poll that changed nothing repaints nothing

// A row is a backlog id, and its group is `fold.group` (items-data.js `foldItems`). `left` groups are the ones that want the person.
const ITEM_GROUPS = [
  { state: "needs-you", label: "needs you", left: true },
  { state: "idle", label: "idle", left: true, hint: "nothing is running on these" },
  { state: "working", label: "working" },
  { state: "queued", label: "queued" },
  { state: "finished", label: "finished" },
];
const itemsCardsOpen = {};     // a row's folded card list that is open, by id
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
  // The fixtures (`?itemsmock=1`) show the tab on a board with no hub, so the views can be read without one.
  tab.hidden = itemsMockOn() ? false : typeof hubIsHub === "undefined" ? true : !hubIsHub;
  if (!tab.hidden) loadItemsBadge();
}

function itemsVisible() {
  const v = document.getElementById("items");
  return !!v && !v.hidden && !document.hidden;
}


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
    itemsBadge(itemsList.filter(itemNeedsYou).length);
    itemsPaint();
  } catch (e) {
    box.innerHTML = '<p class="pane-lead">The items are not answering right now.</p>';
  }
  if (itemsPage === "efficacy") effLoad();
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

// The card behind a chip, from the board's list. Null when the board holds no such card, undefined when it has no list yet.
function itemChipCard(id) {
  if (typeof lastTasks === "undefined" || !lastTasks.length) return undefined;
  return lastTasks.find(x => x.id === id) || null;
}

// What a minion is doing, in a word: it needs you, it is not answering, it has ended, or it is running.
function itemMinionState(t) {
  if (cardNeedsYou(t)) return t.status === "needs-permission" ? "needs permission" : "needs you";
  const st = cardState(t);
  return st === "off" ? "ended" : st === "unreachable" ? "unreachable" : "running";
}

function itemChip(name, id) {
  if (!id) return '<span class="it-card">' + esc(name) + "</span>";
  const t = itemChipCard(id);
  if (t === null) return '<span class="it-card gone" data-tip="this card has ended and was removed">' + esc(name) + "</span>";
  if (t && isMinion(t)) {
    const st = itemMinionState(t);
    return '<button type="button" class="it-card minion' + (st.indexOf("needs") === 0 ? " hot" : "") + '" data-act="card" data-card="' + esc(id) +
      '" data-tip="open this minion\'s terminal">' + esc(name) + ' <i class="it-mst">' + esc(st) + "</i></button>";
  }
  return '<button type="button" class="it-card" data-act="card" data-card="' + esc(id) + '" data-tip="open this card\'s terminal">' + esc(name) + "</button>";
}

function itemStack(t, cls) {
  const n = itemTokN(t);
  if (!n) return '<div class="it-stack ' + (cls || "") + '"></div>';
  return '<div class="it-stack ' + (cls || "") + '">' + ["in", "cw", "cr", "out"].map(k => '<i class="t-' + k + '" style="width:' + (t[k] * 100 / n) + '%"></i>').join("") + "</div>";
}

// ---- the list ----

function itemsMatches(it) {
  return itemsFilter === "all" || it.dept === itemsFilter;
}

// What was drawn for the folded rows last: the card list is refolded against the board's cards at every paint, so a card that ended
// moves its item to idle without waiting for the hub.
function itemsRefold() {
  if (!itemsList.length || !itemsList[0].assignments) return;
  itemsList = foldItems(itemsList.flatMap(r => r.assignments), itemsKnownCards(), itemsStateOf());
  itemsStats = itemsStatsFrom(itemsList, itemsStats);
}

// One line of counts, then spend and landed only when the hub has sent a number for them (an empty chip says nothing to act on).
function itemCountChips(s) {
  const chip = (stat, n, label, hot) => '<span class="it-chip' + (hot && n > 0 ? " hot" : "") + '" data-stat="' + stat + '"><b>' + n + "</b> " + label + "</span>";
  return chip("needs", s.needs_you, "needs " + esc(s.who || "you"), true) + chip("working", s.working || 0, "working") + chip("idle", s.idle || 0, "idle") +
    chip("queued", s.queued || 0, "queued") +
    (s.spend_today == null ? "" : '<span class="it-chip" data-stat="spend"><b>' + itemUsd1(s.spend_today) + "</b> today</span>") +
    (s.landed_week == null ? "" : '<span class="it-chip" data-stat="landed"><b>' + s.landed_week + "</b> landed this week</span>");
}

// What each kind of bar segment means, once. The swatches are drawn by the same rules as the bars (css/items.css).
function itemLegendHtml() {
  const k = (c, t) => '<span><i class="' + c + '"></i>' + t + "</span>";
  return '<div class="it-lgd" aria-label="what the bars mean">' + k("d", "done") + k("c", "current step") + k("c w", "waiting on you") + k("c s", "stale step (commits since)") +
    k("i", "idle: nothing running") + k("", "not reached") + "<em>hover a segment for its step name</em></div>";
}

// The eight segments. Done and skipped are one look (the detail says which). The current segment is its own colour, taller and underlined,
// so it reads apart from done in every skin. A finished item has no current segment.
function itemProgress(it) {
  const g = it.fold ? it.fold.group : "working", cur = it.fold ? it.fold.step : itemCurrent(it);
  const cls = (s, i) => {
    if (g === "finished") return "d";
    if (i === cur) return "c" + (g === "needs-you" ? " w" : "") + (g === "idle" ? " i" : "") + (s.state === "stale" ? " s" : "");
    return s.state === "done" || s.state === "skipped" ? "d" : s.state === "stale" ? "s" : "";
  };
  return '<div class="it-prog">' + it.steps.map((s, i) => '<span class="' + cls(s, i) + '" data-tip="' + esc(s.name) + '"></span>').join("") + "</div>";
}

// The line under the bar: "step 3 of 8 · building" with the time in the step, in words rather than only a number.
function itemCurrentLine(it) {
  const f = it.fold, i = f.step, s = it.steps[i] || { name: "" }, of = it.steps.length;
  if (f.group === "finished") return { b: f.cancelled ? "cancelled" : s.name === "pushed" || !it.hasWork ? "pushed" : s.name, t: "", em: itemAge(it.updated_at) + " ago" };
  const step = "step " + (i + 1) + " of " + of + " · ";
  const since = s.from || it.created_at;
  if (f.group === "needs-you") {
    const ask = it.ask || {};
    return itemAwaitsApproval(it) ? { b: step + "waiting for your approval", t: "w", em: itemDur(ask.since || since) }
      : { b: step + "asked a question at " + s.name, t: "w", em: itemDur(ask.since || since) };
  }
  if (f.group === "queued") return { b: step + "queued", t: "", em: itemDur(it.created_at) };
  if (f.group === "idle") return { b: step + "idle at " + s.name, t: "", em: itemDur(it.updated_at) };
  const verb = s.name === "pushed" ? "waiting to push" : ITEM_STEP_VERB[s.name] || s.name;
  return { b: step + (s.note || verb), t: "t", em: itemDur(since) };
}

// Who asked: the worker's name when the ask carries one, else the card that is working on the item.
function itemAsker(it) {
  const a = it.ask || {};
  if (a.who && a.who !== "approval") return a.who;
  const c = it.fold.cards.find(x => x.kind === "live") || it.fold.cards[0];
  if (!c) return "a worker";
  const t = typeof lastTasks === "undefined" ? null : lastTasks.find(x => x.id === c.id && (!x.room || !c.room || x.room === c.room));
  return (t && t.wire_name) || c.name || c.id;
}

// The cards of a row, folded under "1 live, 3 ended". Live first with its age, then the unknown, then the ended struck through.
function itemCardsHtml(it) {
  const f = it.fold;
  if (!f.cards.length) return "";
  const parts = [];
  if (f.live) parts.push(f.live + " live");
  if (f.offline) parts.push(f.offline + " unknown");
  if (f.ended) parts.push(f.ended + " ended");
  const order = { live: 0, offline: 1, ended: 2, none: 3 };
  const rows = f.cards.slice().sort((a, b) => order[a.kind] - order[b.kind]).map(c => {
    if (c.kind === "live") return '<div class="it-cd">' + itemChip(c.name, c.id) + '<span class="it-dim">' + esc(itemDur(c.at)) + "</span></div>";
    if (c.kind === "offline") return '<div class="it-cd off"><span class="it-card">' + esc(c.name) + '</span><span class="it-dim">room ' + esc(c.room) + " offline</span></div>";
    return '<div class="it-cd end"><span class="it-card gone">' + esc(c.name) + '</span><span class="it-dim">' + (c.state === "finished" ? "finished" + (c.note ? ": " + esc(c.note) : "") : "ended, never reported") + "</span></div>";
  });
  return '<details class="it-cards" data-id="' + esc(it.id) + '"' + (itemsCardsOpen[it.id] ? " open" : "") + "><summary>" + esc(parts.join(", ")) + "</summary>" + rows.join("") + "</details>";
}

// The question, with who asked and how long ago, and the answers. Or, for a design, the approval.
function itemAskHtml(it) {
  const id = esc(it.id), ask = it.ask || { text: "" }, approval = itemAwaitsApproval(it), who = itemAsker(it);
  const since = itemDur(ask.since || it.updated_at);
  const head = approval ? who + " is waiting on your approval" : who + " asked";
  // The worker's own suggested answers are buttons on the row, so a click answers without opening anything. A design waiting on
  // approval has the two acts instead, and `send back` needs its note, so it opens the box.
  const opts = approval ? "" : (ask.options || []).slice(0, 3).map((o, n) => '<button type="button" class="it-btn it-opt' + (n === 0 ? " go" : "") + '" data-act="option" data-id="' + id +
    '" data-i="' + n + '">' + esc(o) + "</button>").join("");
  const term = it.card && it.fold.driverKind !== "ended" ? '<button type="button" class="it-btn" data-act="card" data-card="' + esc(it.card) + '">open terminal</button>' : "";
  const open = !!itemsAsking[it.id];
  return '<div class="it-ask"><b>' + esc(head) + ", " + esc(since) + " ago</b>" + (ask.text ? '<span class="it-q">' + esc(ask.text) + "</span>" : "") + '<div class="it-btns">' +
    (approval ? '<button type="button" class="it-btn go" data-act="approve" data-id="' + id + '">approve</button>' +
      '<button type="button" class="it-btn" data-act="ask" data-id="' + id + '" aria-expanded="' + open + '">send back</button>'
      : opts + '<button type="button" class="it-btn' + (opts ? "" : " go") + '" data-act="ask" data-id="' + id + '" aria-expanded="' + open + '">' + (opts ? "other" : "answer") + "</button>") +
    term + "</div></div>" +
    (open ? '<div class="it-answer"><input type="text" class="it-answer-box" data-id="' + id + '" placeholder="' + (approval ? "what the designer should change" : "answer") + '" aria-label="' +
      (approval ? "note for " : "answer for ") + id + '" value="' + esc(itemsDraft[it.id] || "") + '"><button type="button" class="it-btn' + (approval ? "" : " go") + ' it-send" data-act="' + (approval ? "sendback" : "answer") +
      '" data-id="' + id + '">send</button></div>' : "");
}

function itemRow(it) {
  const id = esc(it.id), f = it.fold, g = f.group;
  const line = itemCurrentLine(it);
  let note = "";
  if (f.waitingOn) note = f.waitingOn === "you" ? "Blocked, waiting on you" : "Blocked, waiting on " + f.waitingOn + ", not on you";
  else if (g === "working" && f.offline && !f.live) note = "room " + (f.cards.find(c => c.kind === "offline") || {}).room + " offline";
  else if (f.noCard) note = "No card is needed for this step";
  else if (g === "idle") note = "Nothing is running on this item";
  let h = '<div class="it-row st-' + esc(g) + '" data-item="' + id + '">' +
    '<div class="it-main" role="button" tabindex="0" aria-expanded="' + (itemsOpen === it.id) + '" data-act="open" data-id="' + id + '">' +
    '<div class="it-title"><span class="it-t">' + esc(itemTitle(it)) + '</span><span class="it-id">' + esc(it.backlog) + "</span></div></div>" +
    '<div class="it-pcol">' + itemProgress(it) + '<div class="it-cur"><b class="' + line.t + '">' + esc(line.b) + "</b><em>" + esc(line.em) + "</em></div>" +
    '<span class="it-state">' + esc(f.cancelled ? "cancelled" : g) + "</span></div>" + (note ? '<div class="it-note">' + esc(note) + "</div>" : "");
  if (g === "needs-you") h += itemAskHtml(it);
  else if (g === "idle") h += '<div class="it-btns it-acts"><button type="button" class="it-btn" data-act="cancel" data-id="' + id + '" aria-label="cancel item ' + id + '">cancel item</button>' +
    '<button type="button" class="it-btn" disabled data-tip="relaunch is not wired to the board yet">relaunch</button></div>';
  return h + itemCardsHtml(it) + "</div>";
}

function itemsGroupHtml(g) {
  let rows = itemsList.filter(it => it.fold.group === g.state && itemsMatches(it));
  if (!rows.length) return "";
  // Inside needs you the oldest question is first. The rest stay as the hub gave them, newest change first.
  if (g.state === "needs-you") rows = rows.slice().sort((a, b) => (itemsMs(a.ask && a.ask.since) || 0) - (itemsMs(b.ask && b.ask.since) || 0));
  const folded = !!itemsFolded[g.state];
  const shown = g.state === "finished" && !itemsShowAll ? rows.slice(0, ITEMS_FINISHED_SHOWN) : rows;
  const more = rows.length - shown.length;
  return '<section class="it-group" data-state="' + g.state + '"><button type="button" class="it-head" data-act="fold" data-state="' + g.state +
    '" aria-expanded="' + !folded + '"><span>' + g.label + '</span><span class="it-n">' + rows.length + "</span>" + (g.hint ? '<span class="it-gh">' + g.hint + "</span>" : "") + "</button>" +
    (folded ? "" : shown.map(itemRow).join("") + (more > 0 ? '<button type="button" class="it-more" data-act="more">show ' + more + " more</button>" : "")) + "</section>";
}

function itemListHtml() {
  const depts = [...new Set(itemsList.map(it => it.dept).filter(Boolean))].sort();
  const pill = (f, label) => '<button type="button" class="it-pill' + (itemsFilter === f ? " on" : "") + '" data-act="filter" data-filter="' + esc(f) + '">' + esc(label) + "</button>";
  const left = ITEM_GROUPS.filter(g => g.left).map(itemsGroupHtml).join(""), right = ITEM_GROUPS.filter(g => !g.left).map(itemsGroupHtml).join("");
  return '<div class="it-wrap"><div class="it-top"><h1>Items</h1><div class="it-sub">Every backlog item in flight, one row each, from designed to pushed.</div></div>' +
    '<div class="it-chips">' + itemCountChips(itemsStats || itemsStatsFrom(itemsList)) + "</div>" + itemLegendHtml() +
    '<div class="it-filters">' + (depts.length > 1 ? pill("all", "all") + depts.map(d => pill(d, d)).join("") : "") +
    '<span class="it-sp"></span><button type="button" class="it-pill" data-act="efficacy">efficacy</button></div>' +
    (left || right ? '<div class="it-cols' + (left && right ? "" : " one") + '"><div class="it-col it-col-l">' + left + '</div><div class="it-col">' + right + "</div></div>"
      : '<p class="pane-lead">Nothing matches.</p>') + "</div>";
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
    (it.price_table ? kv("price table", esc(it.price_table + ", estimated")) : "") + "</div>" + itemEfficacyHtml(it) + "</div>";
}

function itemQuestionHtml(it) {
  if (it.state !== "blocked" || (!itemAwaitsApproval(it) && !itemQuestionText(it))) return "";
  const ask = it.ask || { text: it.note || "", options: [] };
  const id = esc(it.id);
  const since = ask.since ? " · blocked " + itemDur(ask.since) : "";
  const opts = (ask.options || []).slice(0, 3);
  const approval = itemAwaitsApproval(it);
  // The first suggested answer, the worker's own recommendation, is the orange one. For a design the orange one is `approve`.
  const buttons = approval
    ? '<div class="it-opts"><button type="button" class="it-btn go" data-act="approve" data-id="' + id + '">approve</button></div>'
    : opts.length ? '<div class="it-opts">' + opts.map((o, n) => '<button type="button" class="it-btn' + (n === 0 ? " go" : "") + '" data-act="option" data-id="' + id +
      '" data-i="' + n + '">' + esc(o) + "</button>").join("") + "</div>" : "";
  return '<div class="it-qbox"><h2>asks you' + (ask.who ? " · " + esc(ask.who) : "") + esc(since) + '</h2><p class="it-q">' + esc(ask.text) + "</p>" + buttons +
    '<div class="it-ans"><input type="text" class="it-answer-box" data-id="' + id + '" placeholder="' + (approval ? "or send it back: what the designer should change" : "or type an answer to the worker") +
    '" aria-label="' + (approval ? "note for " : "answer for ") + id + '" value="' + esc(itemsDraft[it.id] || "") + '"><button type="button" class="it-btn' + (approval ? "" : " go") + ' it-send" data-act="' + (approval ? "sendback" : "answer") +
    '" data-id="' + id + '">' + (approval ? "send back" : "send") + "</button></div></div>";
}

// The landed check (docs/rnd/doc-debt-from-features.md 2.8). `docs.not_said` is a landed feature whose change note says nothing of its
// docs, and `docs.offer` a landed doc worker's item whose doc item is still open. Neither acts: the offer opens the doc item, and a person
// closes it. The board remembers a dismissed offer, since the hub does not.
const ITEMS_DOCS_KEY = "atrium.items.docsOfferDismissed";
function itemsDocsDismissed() {
  try { return JSON.parse(localStorage.getItem(ITEMS_DOCS_KEY) || "[]"); } catch (e) { return []; }
}
function itemDocsHtml(d) {
  const x = d.docs;
  if (!x) return "";
  if (x.not_said) return '<div class="it-docs note" data-docs="not-said">docs: not said <span class="it-dim">no <code>Docs: d-&lt;n&gt;</code> or <code>Docs: none, &lt;why&gt;</code> line in its change note</span></div>';
  if (x.offer && !itemsDocsDismissed().includes(x.offer.id)) {
    const id = esc(x.offer.id);
    return '<div class="it-docs offer" data-docs="offer">close ' + id + "?" + (x.offer.title ? ' <span class="it-dim">' + esc(x.offer.title) + "</span>" : "") +
      '<span class="it-docs-btns"><button type="button" class="it-btn go" data-act="backlog" data-backlog="' + id + '">open ' + id + '</button>' +
      '<button type="button" class="it-btn" data-act="docs-dismiss" data-id="' + id + '">not now</button></span></div>';
  }
  return "";
}

// The item's tasks, read only: the hub's `tasks` of /_hub/work/<id>. A task row is { task_id, title, state, attempt, line, why, files, reason }.
function itemTasksHtml(d) {
  if (!Array.isArray(d.tasks) || !d.tasks.length) return "";
  return '<div class="it-tasks" data-tasks="' + esc(d.backlog) + '"><h2>Tasks</h2>' + d.tasks.map(t => {
    const files = Array.isArray(t.files) ? t.files : [];
    return '<div class="it-task" data-task="' + esc(t.task_id) + '"><span class="it-id">' + esc(t.task_id) + '</span><span class="it-tag">' + esc(String(t.state || "").replace(/-/g, " ")) + '</span>' +
      '<b>' + esc(t.title || "") + '</b>' + (t.attempt > 1 ? ' <span class="it-dim">attempt ' + t.attempt + '</span>' : "") +
      (t.line ? '<div class="it-task-line">' + esc(t.line) + '</div>' : "") + (t.why ? '<div class="it-dim">why: ' + esc(t.why) + '</div>' : "") +
      (t.reason ? '<div class="it-dim">' + esc(t.reason) + '</div>' : "") +
      (files.length ? '<div class="it-dim">files: ' + files.map(f => esc(f.name)).join(", ") + '</div>' : "") + '</div>';
  }).join("") + '</div>';
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
    "</div></div>" + itemDocsHtml(d) + itemQuestionHtml(d) + itemTasksHtml(d) +
    '<div class="it-grid"><div class="it-tl">' + d.steps.map((s, i) => itemStepHtml(d, s, i)).join("") +
    (hist.length ? '<ol class="it-history">' + hist.map(e => "<li><b>" + esc(e.state) + "</b> r" + e.revision + ' <time data-tip="' + esc(e.at) + '">' + esc(itemAge(e.at)) + " ago</time></li>").join("") + "</ol>" : "") +
    "</div>" + itemSideHtml(d) + "</div></div>";
}

function itemsPaint(force) {
  const box = document.getElementById("items-list");
  if (!box) return;
  // What is being typed survives the repaint: the answer box, with the caret where it was.
  const had = document.activeElement && document.activeElement.classList && document.activeElement.classList.contains("it-answer-box")
    ? { id: document.activeElement.dataset.id, at: document.activeElement.selectionStart } : null;
  for (const el of box.querySelectorAll(".it-answer-box")) itemsDraft[el.dataset.id] = el.value;
  itemsRefold();
  const open = !itemsOpen && itemsPage === "efficacy" ? null : itemsOpen && (itemsDetail && itemsDetail.id === itemsOpen ? itemsDetail : itemsFind(itemsOpen));
  const html = !itemsOpen && itemsPage === "efficacy" ? efficacyHtml() : open ? itemDetailHtml(open) : itemsOpen ? '<p class="pane-lead">Loading ' + esc(itemsOpen) + "&hellip;</p>"
    : !itemsList.length ? '<p class="pane-lead">No items.</p>' : itemListHtml();
  // A poll that found nothing new leaves the page alone: a repaint drops the hover, the focus and a text selection, and reads as a flicker.
  if (!force && html === itemsHtml && box.firstChild) return;
  itemsHtml = html;
  const sv = scrollerOf(box), top = sv.scrollTop;
  box.innerHTML = html;
  sv.scrollTop = top;
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

// A folded row by the id of its driver or of any assignment under it, so an open page survives the driver changing.
function itemsFind(id) {
  return itemsList.find(x => x.id === id) || itemsList.find(x => (x.assignments || []).some(a => a.id === id)) || null;
}

function itemById(id) {
  return itemsDetail && itemsDetail.id === id ? itemsDetail : itemsFind(id);
}

async function itemsAct(id, body) {
  itemsNote("");
  try {
    await itemsData.act(id, body, (itemById(id) || {}).backlog);
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

// The Enter key in an answer box: the answer for a worker's question, the note for a design that is being sent back.
function itemsSubmit(id) {
  if (itemAwaitsApproval(itemById(id))) itemsSendBack(id); else itemsAnswer(id);
}

// Approve names the design sha the human was shown, so a design that moved since is refused by the hub rather than approved unread.
async function itemsApprove(id) {
  const it = itemById(id);
  if (!it) return;
  const sha = it.ask && it.ask.sha ? it.ask.sha : "";
  if (await itemsAct(id, sha ? { do: "approve", sha } : { do: "approve" })) {
    delete itemsDraft[id];
    delete itemsAsking[id];
    itemsNote("Approved " + (it.backlog || id) + (sha ? " at " + sha.slice(0, 7) : "") + ". The build step is next.");
  }
  loadItems();
}

async function itemsSendBack(id) {
  const it = itemById(id);
  const box = document.querySelector('.it-answer-box[data-id="' + CSS.escape(id) + '"]');
  const note = (box ? box.value : itemsDraft[id] || "").trim();
  if (!note) { itemsNote("A design is sent back with a note: type what the designer should change."); if (box) box.focus(); return; }
  if (await itemsAct(id, { do: "send_back", note })) {
    delete itemsDraft[id];
    delete itemsAsking[id];
    itemsNote("Sent " + ((it && it.backlog) || id) + " back. The note is typed to the designer, and the item shows as running once its turn starts.");
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

// `fromHistory` is the back button undoing a move, which must not record one (board.js `navApplying`).
function itemsShow(id, fromHistory) {
  const sc = document.getElementById("items-list");
  const leaving = itemsOpen;
  // The list keeps its place: where it was scrolled is remembered going in and put back coming out, and the row that was opened has the focus.
  if (id && !itemsOpen && sc) itemsListScroll = scrollerOf(sc).scrollTop;
  itemsOpen = id;
  itemsDetail = null;
  itemsPaint(true);
  const sv = sc && scrollerOf(sc);
  if (sv) sv.scrollTop = id ? 0 : itemsListScroll;
  if (!id && leaving && sc) {
    const m = sc.querySelector('.it-main[data-id="' + CSS.escape(leaving) + '"]');
    if (m) { try { m.focus({ preventScroll: true }); } catch (e) { /* no focus */ } }
  }
  if (!fromHistory && typeof navPush === "function") navPush();
  if (itemsOpen) loadItemDetail(itemsOpen, false);
}

// What scrolls: the list box itself when it has its own overflow, else the page's scrolling element.
function scrollerOf(box) {
  for (let el = box; el && el !== document.body; el = el.parentElement) {
    const o = getComputedStyle(el).overflowY;
    if ((o === "auto" || o === "scroll") && el.scrollHeight > el.clientHeight) return el;
  }
  return document.scrollingElement || box;
}

// The open item's id, for the board's history entry.
function itemsOpenId() { return itemsOpen || ""; }

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
      if (row && !ev.target.closest(".it-ask, .it-answer, .it-cards, .it-acts")) itemsToggle(row.dataset.item);
      return;
    }
    const act = b.dataset.act;
    if (act === "back") { ev.preventDefault(); itemsShow(null); }
    else if (act === "fold") { itemsFolded[b.dataset.state] = !itemsFolded[b.dataset.state]; itemsPaint(); }
    else if (act === "more") { itemsShowAll = true; itemsPaint(); }
    else if (act === "efficacy") { itemsPage = "efficacy"; effLoad(); itemsPaint(true); }
    else if (act === "eff-back") { ev.preventDefault(); itemsPage = ""; itemsPaint(true); }
    else if (act === "eff-by") { effBy = b.dataset.by; effLoad(); }
    else if (act === "eff-obs") { effObservedOnly = !effObservedOnly; effLoad(); }
    else if (act === "eff-sort") { effSort = effSort === "worst" ? "newest" : effSort === "newest" ? "cost" : "worst"; itemsPaint(); }
    else if (act === "filter") { itemsFilter = b.dataset.filter; itemsPaint(); }
    else if (act === "ask") {
      itemsAsking[b.dataset.id] = !itemsAsking[b.dataset.id];
      itemsPaint();
      const el = box.querySelector('.it-answer-box[data-id="' + CSS.escape(b.dataset.id) + '"]');
      if (el) el.focus();
    }
    else if (act === "answer") itemsAnswer(b.dataset.id);
    else if (act === "approve") itemsApprove(b.dataset.id);
    else if (act === "sendback") itemsSendBack(b.dataset.id);
    else if (act === "option") {
      const it = itemById(b.dataset.id);
      const o = it && it.ask && it.ask.options ? it.ask.options[+b.dataset.i] : "";
      if (o) itemsAnswer(b.dataset.id, o);
    }
    else if (act === "cancel") itemsCancel(b.dataset.id);
    else if (act === "card") itemsOpenCard(b.dataset.card);
    else if (act === "backlog") itemsOpenBacklog(b.dataset.backlog);
    else if (act === "docs-dismiss") {
      const seen = itemsDocsDismissed();
      if (!seen.includes(b.dataset.id)) try { localStorage.setItem(ITEMS_DOCS_KEY, JSON.stringify(seen.concat(b.dataset.id).slice(-200))); } catch (e) { /* kept for this page only */ }
      itemsPaint(true);
    }
    else if (act === "open") itemsToggle(b.dataset.id);
  });
  // A row's folded card list stays open across a repaint. `toggle` does not bubble, so it is caught on the way down.
  box.addEventListener("toggle", ev => {
    const d = ev.target;
    if (d.classList && d.classList.contains("it-cards")) { if (d.open) itemsCardsOpen[d.dataset.id] = true; else delete itemsCardsOpen[d.dataset.id]; }
  }, true);
  box.addEventListener("keydown", ev => {
    if (ev.target.classList.contains("it-answer-box") && ev.key === "Enter") { ev.preventDefault(); itemsSubmit(ev.target.dataset.id); }
    else if (ev.target.classList.contains("it-main") && (ev.key === "Enter" || ev.key === " ")) { ev.preventDefault(); itemsToggle(ev.target.dataset.id); }
  });
  box.addEventListener("input", ev => {
    if (ev.target.classList.contains("it-answer-box")) itemsDraft[ev.target.dataset.id] = ev.target.value;
  });
});
