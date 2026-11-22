// YOUR QUEUE (docs/design/your-queue.md): every ask waiting on the operator, one row each, stuck first, newest first. The
// Queue tab draws it, and the header badge counts it on every tab.
//
// An ask is { key, verb, stuck, title, since, why, card, item, merge }. Its verb is one of answer, approve, try, sign,
// unblock. Asks come from the hub's items, its merge queue, and the card list, and a card an item already covers is
// not asked twice.
const YQ_VERBS = ["ANSWER", "APPROVE", "TRY", "SIGN", "UNBLOCK"];
const YQ_HUB_MS = 60000;
const yqHub = { items: null, merges: null, at: 0, busy: false };
const YQ_FOLD_KEY = "atrium.queue.folded";

function yqOn() {
  return typeof isViewing === "function" && isViewing("queue");
}

async function yqLoadHub() {
  if (yqHub.busy || Date.now() - yqHub.at < YQ_HUB_MS) return;
  if (typeof hubIsHub !== "undefined" && !hubIsHub) return;
  yqHub.busy = true;
  try {
    const [items, merges] = await Promise.all([
      typeof itemsData === "object" ? itemsData.list().then(a => a.items || []).catch(() => null) : null,
      typeof itemsFetch === "function" ? itemsFetch("/_hub/merge?all=1").then(a => (a && a.requests) || []).catch(() => null) : null
    ]);
    yqHub.items = items;
    yqHub.merges = merges;
  } finally {
    yqHub.at = Date.now();
    yqHub.busy = false;
  }
  yqPaint();
}

function yqMs(iso) {
  const t = iso ? Date.parse(iso) : NaN;
  return isNaN(t) ? 0 : t;
}

// The backlog id a card works on, from its `atrium:item:<id>` tag.
function yqCardItem(t) {
  const tag = ((t && t.tags) || []).find(x => /^atrium:item:./.test(String(x)));
  return tag ? String(tag).slice("atrium:item:".length) : "";
}

// A launcher that is still running owns its worker's report, so the worker's ask is not the operator's.
function yqLauncherLive(t, tasks) {
  if (!t.spawned_by && !t.spawned_by_id) return false;
  if (t.spawned_by === "@human") return false;
  const l = typeof launcherOf === "function" ? launcherOf(t, tasks) : null;
  return !!l && typeof cardLive === "function" && cardLive(l);
}

// Something was said to the card after its report, so the report has its answer. A look or a click is not an answer.
function yqAnswered(t) {
  return yqMs(t.prompted_at) > yqMs(t.reported_at || t.recap_at);
}

// The pages a card's report or ask points at: a board link as its own path, so the frame is same-origin, or an .html
// path in the card's files.
function yqPages(t) {
  const text = [t.ask, t.recap].filter(Boolean).join("\n");
  const out = [];
  for (const m of text.matchAll(/https?:\/\/[^\s)"'<>]+/g)) {
    const u = m[0].replace(/[.,;]+$/, "");
    try {
      const p = new URL(u);
      if (/\/room\/[^/]+\/[^/]+\/files\//.test(p.pathname)) out.push(p.pathname + p.search);
      else if (/\.html?$/i.test(p.pathname)) out.push(u);
    } catch (e) { /* not a link */ }
  }
  const rooms = typeof hubRooms !== "undefined" && hubRooms.length === 1 ? hubRooms[0].name : "";
  const room = (typeof roomOf === "function" ? roomOf(t.id) : "") || t.room || rooms;
  for (const m of text.matchAll(/(?:^|[\s(`'"])((?:[\w.-]+\/)*[\w.-]+\.html)\b/g)) {
    if (/^https?:/i.test(m[1]) || !room) continue;
    out.push("/room/" + encodeURIComponent(room) + "/" + encodeURIComponent(bareId(t.id)) + "/files/" + m[1]);
  }
  return [...new Set(out)];
}

function yqBlockedNeed(t) {
  const m = /(?:^|\n)needs: (.*)/.exec(String(t.recap || ""));
  return m ? m[1].trim() : "";
}

function yqCardAsk(t, tasks) {
  const title = t.display_title || t.title || t.id;
  const base = { key: "card:" + t.id, card: t, title };
  if (typeof isBlocker === "function" && isBlocker(t)) {
    return Object.assign(base, { verb: "UNBLOCK", stuck: true, since: t.escalation.since, why: typeof blockerReason === "function" ? blockerReason(t) : "" });
  }
  if (typeof isStuck === "function" && isStuck(t)) {
    return Object.assign(base, { verb: "UNBLOCK", stuck: true, since: t.escalation.since, why: t.escalation.text || "stuck" });
  }
  if (t.ask_verb && t.ask && !t.ask_peer && !(yqMs(t.prompted_at) > yqMs(t.ask_at))) {
    const verb = String(t.ask_verb).toUpperCase();
    return Object.assign(base, { verb, stage: t.ask_stage || "", gate: true, stuck: verb === "UNBLOCK", since: t.ask_at, why: t.ask,
      pages: verb === "APPROVE" || verb === "TRY" ? yqPages(t) : [] });
  }
  if (t.ask_verb && t.ask && !t.ask_peer) return null;
  base.nogate = true;
  const need = yqBlockedNeed(t);
  if (need && t.status !== "running" && !yqAnswered(t)) {
    return Object.assign(base, { verb: "UNBLOCK", stuck: true, since: t.reported_at || t.recap_at, why: need });
  }
  const waiting = typeof cardWaiting === "function" && cardWaiting(t);
  if (waiting && typeof isInterview === "function" && isInterview(t)) {
    return Object.assign(base, { verb: "ANSWER", since: t.ask_at || t.last_activity_at, why: t.ask || "the interview is waiting on its question" });
  }
  if (t.status === "needs-permission" && waiting) {
    const perm = typeof permsLocal !== "undefined" ? permsLocal.find(p => p.task_id === t.id) : null;
    base.nogate = false;
    return Object.assign(base, { verb: "APPROVE", perm, since: (perm && perm.requested_at) || t.last_activity_at,
      why: perm ? perm.tool + ": " + perm.command : "a permission prompt", pages: [] });
  }
  if ((waiting || t.status === "done") && (cardHasOpenQuestion(t) || (t.ask && !t.ask_peer))) {
    return Object.assign(base, { verb: "ANSWER", since: t.ask_at || t.last_activity_at, why: t.ask || "an open question" });
  }
  const pages = t.status === "done" || waiting ? yqPages(t) : [];
  if (pages.length && !yqAnswered(t)) {
    const recap = String(t.recap || t.ask || "");
    const tryIt = /\b(try|live|interactive)\b/i.test(recap) && !/\breview\b/i.test(recap);
    return Object.assign(base, { verb: tryIt ? "TRY" : "APPROVE", pages, since: t.reported_at || t.recap_at || t.ask_at,
      why: recap.split("\n").find(s => s.trim()) || "a page to look at" });
  }
  if (t.status === "done" && t.report_sha && !yqAnswered(t) && !yqLauncherLive(t, tasks)) {
    const recap = String(t.recap || "");
    return Object.assign(base, { verb: "SIGN", since: t.reported_at || t.recap_at,
      why: (recap.split("\n").find(s => s.trim()) || "done") + " (" + String(t.report_sha).slice(0, 8) + ")" });
  }
  return null;
}

// A red merge is one whose branch has nothing newer in the queue, and that the hub has not found stale.
function yqMergeAsks() {
  const newest = new Map();
  for (const m of yqHub.merges || []) {
    const k = m.repo + " " + m.branch;
    if (!newest.has(k) || newest.get(k).n < m.n) newest.set(k, m);
  }
  return [...newest.values()].filter(m => m.state === "red" && !m.stale).map(m => ({
    key: "merge:" + m.n, verb: "UNBLOCK", stuck: true, merge: m, title: "Merge red: " + m.branch,
    since: m.ended_at || m.tested_at || m.asked_at,
    why: (m.failing && m.failing.length ? m.failing.join(", ") : m.reason || "the merge tests failed") + " (request " + m.n + ")"
  }));
}

function yqItemAsks(tasks) {
  const out = [];
  for (const it of yqHub.items || []) {
    if (typeof itemNeedsYou !== "function" || !itemNeedsYou(it)) continue;
    const cards = (it.fold && it.fold.cards) || [];
    const live = cards.find(c => c.kind === "live") || cards[0];
    const card = live ? tasks.find(t => t.id === live.id) || null : null;
    const approve = typeof itemAwaitsApproval === "function" && itemAwaitsApproval(it);
    out.push({ key: "item:" + it.backlog, verb: approve ? "APPROVE" : "ANSWER", item: it, card, title: it.title || it.backlog,
      since: (it.ask && it.ask.since) || it.updated_at, why: (it.ask && (it.ask.brief || it.ask.text)) || "" });
  }
  return out;
}

function yqAsks(tasks) {
  tasks = (tasks || []).filter(t => !t.offline);
  const items = yqItemAsks(tasks);
  const covered = new Set(items.map(a => a.item.backlog));
  const byCard = new Set(items.map(a => a.card && a.card.id).filter(Boolean));
  const cards = tasks.filter(t => !byCard.has(t.id) && !covered.has(yqCardItem(t))).map(t => yqCardAsk(t, tasks)).filter(Boolean);
  return [...yqMergeAsks(), ...items, ...cards].sort((a, b) => (b.stuck ? 1 : 0) - (a.stuck ? 1 : 0) || yqMs(b.since) - yqMs(a.since));
}

// The item a row is on: the one the ask came from, else the one its card's tag names.
function yqItemOf(a) {
  if (a.item) return a.item;
  const id = a.card && yqCardItem(a.card);
  return id ? (yqHub.items || []).find(it => it.backlog === id) || null : null;
}

function yqStepper(it) {
  if (!it || !Array.isArray(it.steps) || !it.steps.length) return `<span class="yq-step none" data-tip="no item behind this ask"></span>`;
  const cur = typeof itemCurrent === "function" ? itemCurrent(it) : -1;
  const cls = (s, i) => i === cur && s.state !== "done" ? "cur" : s.state === "done" ? "done" : s.state === "skipped" ? "skip" : "";
  const tip = it.steps.map((s, i) => s.name + " (" + (i === cur ? "here" : s.state) + ")").join(", ");
  return `<span class="yq-step" data-tip="${esc(tip)}">${it.steps.map((s, i) => `<i class="${cls(s, i)}"></i>`).join("")}</span>`;
}

function yqAge(a) {
  const ms = yqMs(a.since);
  return ms ? ago((Date.now() - ms) / 1000) : "";
}

function yqRowHTML(a) {
  const old = yqMs(a.since) && Date.now() - yqMs(a.since) >= 4 * 3600000;
  return `<div class="card tab yq-row v-${a.verb.toLowerCase()}${a.stuck ? " stuck" : ""}${yqView.key === a.key ? " sel" : ""}" data-yq="${esc(a.key)}" data-tip="${esc(a.title)}" onclick="yqAct(this)">
    <div class="card-line"><div class="title"><span class="tstack"><span class="tname">${esc(a.title)}</span><span class="tpath yq-line"><span class="yq-verb">${a.verb}</span><span class="yq-age${old ? " old" : ""}">${esc(yqAge(a))}</span>${a.nogate ? `<span class="yq-nogate">no gate set</span>` : a.stage ? `<span class="yq-stage">${esc(a.stage)}</span>` : ""}${yqStepper(yqItemOf(a))}</span></span></div></div></div>`;
}

function yqFolded() {
  try { return new Set(JSON.parse(localStorage.getItem(YQ_FOLD_KEY) || "[]")); } catch (e) { return new Set(); }
}

function yqFold(id) {
  const f = yqFolded();
  if (f.has(id)) f.delete(id); else f.add(id);
  try { localStorage.setItem(YQ_FOLD_KEY, JSON.stringify([...f])); } catch (e) {}
  yqRender();
}

function yqGroupHTML(id, name, rows, cls) {
  if (!rows.length) return "";
  const folded = yqFolded().has(id);
  return `<button class="tgroup${cls}" data-yq-group="${esc(id)}" onclick="yqFold(this.dataset.yqGroup)" data-tip="${folded ? "show" : "hide"} ${esc(name)}"><span class="tcaret">${folded ? "&#9656;" : "&#9662;"}</span><span class="tgname">${esc(name)}</span><span class="tgcount">${rows.length}</span></button>` +
    (folded ? "" : rows.map(yqRowHTML).join(""));
}

// ---- the filter drawer's rows (js/filter-drawer.js): what the terminals tab has, where it means something for an ask ----
const YQ_SHOW_KEY = "atrium.queue.show", YQ_SORT_KEY = "atrium.queue.sort", YQ_GROUP_KEY = "atrium.queue.group";
const YQ_CHIPS = [
  ["paused", "asks from cards with no agent running that you can resume: ones you paused, and idle ones that were parked"],
  ["dead", "asks from cards whose session has exited, pinned or closed"],
  ["subagents", "asks from cards a launcher started"],
  ["minions", "asks from the throwaway sessions an item launched"]
];
const YQ_SORTS = [
  ["name", "name", "alphabetical by the ask's title"],
  ["activity", "activity", "newest ask first, by how long it has waited"],
  ["started", "started", "newest card first, by when the card behind the ask was started"]
];
const YQ_GROUP_MODES = [
  ["need", "stuck first", "the stuck asks in a group of their own, everything else waiting under them"],
  ["project", "by project", "cut the asks into the projects of the cards asking, or the repository of a merge"],
  ["tag", "by tag", "cut the asks into the tags of the cards asking. one with several appears under each"],
  ["age", "by age", "1h, 4h, today, yesterday, this week, this month, and everything older, by when the ask was raised"],
  ["off", "off", "one flat list, in the order the sort above put them"]
];

function yqShow() {
  try {
    const v = JSON.parse(localStorage.getItem(YQ_SHOW_KEY) || "null");
    if (Array.isArray(v)) return new Set(v.filter(x => YQ_CHIPS.some(c => c[0] === x)));
  } catch (e) {}
  return new Set(YQ_CHIPS.map(c => c[0]));
}

function yqPick(key, list, def) {
  const v = localStorage.getItem(key);
  return list.some(x => x[0] === v) ? v : def;
}

function yqToggleShow(id) {
  const on = yqShow();
  if (on.has(id)) on.delete(id); else on.add(id);
  try { localStorage.setItem(YQ_SHOW_KEY, JSON.stringify([...on])); } catch (e) {}
  yqPaintBar();
  yqRender();
}

function yqSet(key, v) {
  try { localStorage.setItem(key, v); } catch (e) {}
  yqPaintBar();
  yqRender();
}

function yqPaintBar() {
  const show = yqShow(), sort = yqPick(YQ_SORT_KEY, YQ_SORTS, "activity"), group = yqPick(YQ_GROUP_KEY, YQ_GROUP_MODES, "need");
  const pills = (list, on, key, attr) => list.map(([v, label, tip]) =>
    `<button class="${v === on ? "on" : ""}" data-${attr}="${v}" onclick="yqSet('${key}', '${v}')" data-tip="${esc(tip)}">${esc(label)}</button>`).join("");
  const s = document.getElementById("yq-show");
  if (s) s.innerHTML = YQ_CHIPS.map(([v, tip]) => fdChip(v, show.has(v), `yqToggleShow('${v}')`, tip, v)).join("");
  const o = document.getElementById("yq-sort");
  if (o) o.innerHTML = pills(YQ_SORTS, sort, YQ_SORT_KEY, "sort");
  const g = document.getElementById("yq-group");
  if (g) g.innerHTML = pills(YQ_GROUP_MODES, group, YQ_GROUP_KEY, "group");
}

// Which chips an ask sits behind, from the card asking. An ask with no card is behind none.
function yqKinds(a, tasks) {
  const t = a.card;
  if (!t) return [];
  const k = typeof termRowKind === "function" ? termRowKind(t) : "";
  const out = [];
  if (k === "paused") out.push("paused");
  if (k === "dead" || k === "gone") out.push("dead");
  if (typeof nestsUnderLauncher === "function" && nestsUnderLauncher(t) && typeof launcherOf === "function" && launcherOf(t, tasks)) out.push("subagents");
  if (typeof isMinion === "function" && isMinion(t)) out.push("minions");
  return out;
}

function yqMatches(a, q) {
  if (!q) return true;
  if (a.card && typeof cardMatchesText === "function" && cardMatchesText(a.card, q)) return true;
  return !q.startsWith("#") && [a.title, a.why, a.verb, a.merge && a.merge.branch].join(" ").toLowerCase().includes(q);
}

function yqStarted(a) {
  return yqMs((a.card && a.card.created_at) || (a.item && a.item.created_at) || (a.merge && a.merge.asked_at) || a.since);
}

function yqSorter(sort) {
  if (sort === "name") return (a, b) => String(a.title).localeCompare(String(b.title));
  if (sort === "started") return (a, b) => yqStarted(b) - yqStarted(a);
  return (a, b) => yqMs(b.since) - yqMs(a.since);
}

const YQ_NONE = { project: "no project", tag: typeof UNTAGGED === "string" ? UNTAGGED : "untagged" };
let yqProjectBy = null;
function yqGroupNames(a, mode) {
  if (mode === "need") return [a.stuck ? "stuck" : "waiting"];
  if (mode === "age") return [typeof recencyBucket === "function" ? recencyBucket({ id: "", last_activity_at: a.since }) : "dormant"];
  if (mode === "tag") return a.card && (a.card.tags || []).length ? a.card.tags : [YQ_NONE.tag];
  if (!yqProjectBy && typeof compiled === "function") yqProjectBy = compiled("", DEFAULT_GROUP_BY, "task");
  let p = "";
  try { p = a.card && yqProjectBy ? String(yqProjectBy(a.card) || "") : ""; } catch (e) { p = ""; }
  if (!p && a.merge) p = String(a.merge.repo || "").split("/").slice(-2).join("/");
  return [p || YQ_NONE.project];
}

function yqGroupOrder(mode) {
  if (mode === "need") return (x, y) => (x === "stuck" ? 0 : 1) - (y === "stuck" ? 0 : 1);
  if (mode === "age") return (x, y) => RECENCY_ORDER.indexOf(x) - RECENCY_ORDER.indexOf(y);
  return (x, y) => (x === YQ_NONE[mode]) - (y === YQ_NONE[mode]) || x.localeCompare(y);
}

let yqLast = [];
const yqSeen = { all: 0, counts: {} };
function yqListHTML(tasks) {
  const all = yqAsks(tasks);
  const show = yqShow(), q = typeof fdSearchText === "function" ? fdSearchText("queue") : "";
  const sort = yqPick(YQ_SORT_KEY, YQ_SORTS, "activity"), mode = yqPick(YQ_GROUP_KEY, YQ_GROUP_MODES, "need");
  const counts = Object.fromEntries(YQ_CHIPS.map(c => [c[0], 0]));
  const l = yqLast = all.filter(a => {
    const kinds = yqKinds(a, tasks);
    kinds.forEach(k => counts[k]++);
    return kinds.every(k => show.has(k)) && yqMatches(a, q);
  }).sort(yqSorter(sort));
  Object.assign(yqSeen, { all: all.length, counts });
  if (!all.length) return `<div class="yq-empty"><div class="big">&#10003;</div><div>Nothing is waiting on you</div></div>`;
  if (!l.length) return `<div class="yq-empty"><div>Nothing here matches the filters</div></div>`;
  if (mode === "off") return l.map(yqRowHTML).join("");
  const groups = new Map();
  for (const a of l) for (const n of yqGroupNames(a, mode)) (groups.get(n) || groups.set(n, []).get(n)).push(a);
  return [...groups.keys()].sort(yqGroupOrder(mode)).map(n =>
    yqGroupHTML(mode === "need" ? n : mode + ":" + n, n, groups.get(n), mode === "need" && n === "stuck" ? " yq-stuckhead" : "")).join("");
}

// Brings `old` to look like `fresh` without replacing a node whose tag is the same, so a row keeps its node (and the scroll under it) when only its age changed.
function yqMorph(old, fresh) {
  for (const a of [...old.attributes]) if (!fresh.hasAttribute(a.name)) old.removeAttribute(a.name);
  for (const a of fresh.attributes) if (old.getAttribute(a.name) !== a.value) old.setAttribute(a.name, a.value);
  const o = [...old.childNodes], f = [...fresh.childNodes];
  f.forEach((n, i) => {
    const c = o[i];
    if (!c) old.appendChild(n);
    else if (c.nodeType === 3 && n.nodeType === 3) { if (c.data !== n.data) c.data = n.data; }
    else if (c.nodeType === 1 && n.nodeType === 1 && c.tagName === n.tagName) yqMorph(c, n);
    else old.replaceChild(n, c);
  });
  o.slice(f.length).forEach(c => c.remove());
}

// The list's own children are matched by key (a row's ask, a group's id), so a row that moved is moved and one that stayed is only morphed.
function yqPatch(host, html) {
  const t = document.createElement("template");
  t.innerHTML = html;
  const keyOf = n => n.nodeType === 1 ? n.dataset.yq || n.dataset.yqGroup || "" : "";
  const have = new Map();
  [...host.children].forEach(n => { const k = keyOf(n); if (k) have.set(k, n); });
  let at = host.firstChild;
  for (const n of [...t.content.childNodes]) {
    const k = keyOf(n), cur = k && have.get(k);
    let use = n;
    if (cur && cur.tagName === n.tagName) { yqMorph(cur, n); use = cur; have.delete(k); }
    if (use === at) at = at.nextSibling;
    else host.insertBefore(use, at);
  }
  while (at) { const next = at.nextSibling; at.remove(); at = next; }
  have.forEach(n => n.remove());
}

function yqRender() {
  const host = document.getElementById("yq-list");
  if (!host || !yqOn()) return;
  yqLoadHub();
  const html = yqListHTML(typeof lastTasks !== "undefined" ? lastTasks : []);
  if (host.__yq !== html) { host.__yq = html; yqPatch(host, html); }
  if (typeof pillCounts === "function") pillCounts("yq-show", yqSeen.counts);
  if (typeof fdStatus === "function") {
    const sort = yqPick(YQ_SORT_KEY, YQ_SORTS, "activity"), mode = yqPick(YQ_GROUP_KEY, YQ_GROUP_MODES, "need");
    fdStatus("queue", {
      summary: [yqLast.length + " shown", sort, YQ_GROUP_MODES.find(g => g[0] === mode)[1]].join(" · "),
      match: yqLast.length !== yqSeen.all ? `${yqLast.length} of ${yqSeen.all}` : ""
    });
  }
  yqPaintAsk();
}

function yqFind(el) {
  const row = el.closest("[data-yq]");
  return row ? yqLast.find(a => a.key === row.dataset.yq) : null;
}

function yqAct(el) {
  const a = yqFind(el);
  if (a) yqOpenAsk(a);
}

// The badge in the header, from the board's last card list and the hub's last answer.
function yqPaint() {
  const el = document.getElementById("yq-badge");
  if (!el) return;
  const l = yqAsks(typeof lastTasks !== "undefined" ? lastTasks : []);
  el.hidden = !l.length;
  el.classList.toggle("stuck", l.some(a => a.stuck));
  el.textContent = l.length + " for you";
  el.dataset.tip = l.length ? YQ_VERBS.map(v => [v, l.filter(a => a.verb === v).length]).filter(x => x[1]).map(x => x[1] + " " + x[0].toLowerCase()).join(", ") : "";
  if (typeof badge === "function" && document.getElementById("c-queue")) badge("c-queue", l.length);
  yqRender();
}

function yqGo() {
  if (typeof switchView === "function") switchView("queue");
}

// ---- the ask, in the Queue tab's pane, with the card's terminal one click away on the Terminals tab ----
const yqView = { key: "", ask: null, mode: "ask", at: 0, qi: 0, drafts: {}, last: {}, sent: null, text: {}, drop: false, page: 0 };

function yqAskEl() {
  const el = document.getElementById("yq-ask");
  const term = document.getElementById("term-pane");
  if (el && !el.__yqWired) {
    el.__yqWired = true;
    el.addEventListener("click", yqAskClick);
    el.addEventListener("input", e => { if (e.target.dataset && e.target.dataset.yqText) yqView.text[e.target.dataset.yqText] = e.target.value; });
  }
  if (term && !document.getElementById("yq-back")) {
    const back = document.createElement("button");
    back.id = "yq-back";
    back.className = "yq-back";
    back.hidden = true;
    back.dataset.yqDo = "mode-ask";
    back.addEventListener("click", yqAskClick);
    term.appendChild(back);
  }
  return el;
}

function yqOpenAsk(a) {
  Object.assign(yqView, { key: a.key, ask: a, mode: "ask", at: Date.now(), qi: 0, drop: false, page: 0, sent: null, listed: false });
  if (!yqOn() && typeof switchView === "function") switchView("queue");
  yqRender();
  yqPaintAsk(true);
}

// The card's own terminal, on the Terminals tab, with a pill there back to the ask.
function yqToTerm() {
  const a = yqView.ask;
  if (!a || !a.card) return;
  yqView.mode = "term";
  yqView.at = Date.now();
  // Switching first showed whatever card the pane already held until a dead card's read came back, or for good if it failed.
  if (typeof attachTask === "function") attachTask(a.card.id);
  yqPaintAsk(true);
}

function yqCloseAsk() {
  yqView.key = "";
  yqView.ask = null;
  yqView.mode = "ask";
  yqRender();
  yqPaintAsk(true);
}

// Who a send goes to, as handle@room.
function yqTarget(id) {
  const t = ((typeof lastTasks !== "undefined" && lastTasks) || []).find(x => x.id === id);
  const room = typeof roomOf === "function" ? roomOf(id) : "";
  return ((t && (t.alias || t.display_title || t.title)) || bareId(id)) + (room ? "@" + room : "");
}

function yqCard() {
  const a = yqView.ask;
  if (!a || !a.card) return null;
  return ((typeof lastTasks !== "undefined" && lastTasks) || []).find(t => t.id === a.card.id) || a.card;
}

// A question as the agent wrote it: its lines, a list of choices when it has one, yes or no when it reads that way,
// and the agent's own recommendation when it gave one.
function yqParseQ(s) {
  const lines = String(s || "").split("\n");
  const choiceRe = /^\s*(?:[-*]\s*)?\(?(?:[a-hA-H]|\d{1,2})[).:]\s+(.+)$/;
  const choices = lines.map(l => choiceRe.exec(l)).filter(Boolean).map(m => m[1].trim());
  const text = choices.length >= 2 ? lines.filter(l => !choiceRe.test(l)).join("\n").trim() : String(s || "").trim();
  const rm = /\b(?:recommend(?:ed|ation)?|suggest(?:ed|ion)?|my pick)\b[:\s-]*([^\n]+)/i.exec(String(s || ""));
  const yes = choices.length < 2 && (/\(y\/n\)|\byes or no\b/i.test(text) ||
    /^\s*(should|shall|is|are|do|does|did|can|could|will|would|may|has|have|ok|okay)\b[^\n]*\?\s*$/i.test(text.split("\n")[0]));
  return { text, choices: choices.length >= 2 ? choices : yes ? ["Yes", "No"] : [], rec: rm ? rm[1].trim() : "" };
}

function yqIsRec(choice, rec) {
  const c = String(choice).toLowerCase(), r = String(rec).toLowerCase();
  return !!r && (r === c || r.startsWith(c + " ") || r.startsWith(c + ".") || r.startsWith(c + ","));
}

function yqGroups(t) {
  const at = (t.seen && t.seen.questions_at) || "";
  const have = yqView.drafts[t.id];
  if (have && have.at === at) return have.groups;
  const groups = typeof qaGroups === "function" ? qaGroups(t) : [];
  yqView.drafts[t.id] = { at, groups };
  return groups;
}

function yqBox(key, ph, rows) {
  return `<textarea class="yq-reply" data-yq-text="${esc(key)}" rows="${rows || 3}" placeholder="${esc(ph)}">${esc(yqView.text[key] || "")}</textarea>`;
}

function yqInterviewHTML(t) {
  const groups = yqGroups(t);
  const cur = groups.find(g => g.current);
  if (!cur) return yqReplyHTML(t);
  const qs = cur.qs, a = cur.a, qi = Math.min(yqView.qi, qs.length - 1);
  const q = yqParseQ(qs[qi]);
  const done = a.filter(x => String(x).trim()).length;
  const picked = String(a[qi] || "").split(" - ")[0];
  return `<div class="yq-iv"><div class="yq-rail">${qs.map((x, i) => `<div class="yq-qi${i === qi ? " on" : ""}" data-yq-do="q" data-i="${i}"><span class="q">${i + 1}. ${esc(yqParseQ(x).text.split("\n")[0].slice(0, 90))}</span><span class="a${String(a[i] || "").trim() ? "" : " none"}">${String(a[i] || "").trim() ? "&rarr; " + esc(String(a[i]).slice(0, 50)) : "not answered"}</span></div>`).join("")}</div>
    <div class="yq-ivmain"><div class="yq-dim">Question ${qi + 1} of ${qs.length}</div><div class="yq-prog"><i style="width:${(done / qs.length) * 100}%"></i></div>
    <div class="yq-q">${esc(q.text)}</div>
    ${q.rec ? `<div class="yq-rec"><b>${esc(t.display_title || "the agent")} recommends</b>${esc(q.rec)}</div>` : ""}
    ${q.choices.length ? `<div class="yq-choices">${q.choices.map(c => `<button class="yq-btn${yqIsRec(c, q.rec) ? " rec" : ""}${picked === c ? " sel" : ""}" data-yq-do="pick" data-v="${esc(c)}">${esc(c)}</button>`).join("")}</div>` : ""}
    ${yqBox("q:" + t.id + ":" + qi, q.choices.length ? "Optional: add a note to your answer" : "Your answer")}
    <div class="yq-actions"><button class="yq-btn primary" data-yq-do="save">${q.choices.length ? "Save note and next" : "Save answer and next"}</button>
      ${q.rec ? `<button class="yq-btn" data-yq-do="userec">Use the recommendation</button>` : ""}
      ${qi > 0 ? `<button class="yq-btn" data-yq-do="q" data-i="${qi - 1}">back</button>` : ""}
      ${done ? `<button class="yq-btn send" data-yq-do="sendqs">Send ${done} answer${done === 1 ? "" : "s"}</button>` : ""}</div></div></div>`;
}

// The card's last reply is the question of an interview, and `ask` is only set by a report, so it is read once per turn.
function yqLastReply(t) {
  const at = (t.seen && t.seen.turn_ended_at) || t.last_activity_at || "";
  const have = yqView.last[t.id];
  if (have && have.at === at) return have.text;
  const mine = yqView.last[t.id] = { at, text: have ? have.text : "" };
  api(`/v1/tasks/${encodeURIComponent(t.id)}/replies?n=1`).then(r => {
    const rs = (r && r.replies) || [];
    mine.text = rs.length ? String(rs[rs.length - 1].text || "") : "";
  }).catch(() => {}).then(() => yqPaintAsk());
  return mine.text;
}

function yqReplyHTML(t) {
  const a = yqView.ask;
  const said = t.ask ? "" : yqLastReply(t);
  const q = said && window.mMd ? `<div class="yq-q md">${window.mMd.render(said, { id: t.id, noFiles: true })}</div>` : `<div class="yq-q">${esc(t.ask || said || a.why || "")}</div>`;
  return `<div class="yq-askbody">${q}
    ${yqBox("r:" + t.id, "Your answer", 4)}<div class="yq-actions"><button class="yq-btn primary" data-yq-do="reply">Send</button></div></div>`;
}

function yqItemHTML(it) {
  const ask = it.ask || {};
  if (typeof itemAwaitsApproval === "function" && itemAwaitsApproval(it)) {
    return `<div class="yq-askbody"><div class="yq-q">${esc(it.title || it.backlog)}</div><p>${esc(ask.text || "")}</p>
      ${yqBox("i:" + it.id, "Send back: what should change", 3)}
      <div class="yq-actions"><button class="yq-btn primary" data-yq-do="iapprove">Approve</button><button class="yq-btn" data-yq-do="isendback">Send back</button>
      <button class="yq-btn" data-yq-do="iopen">Open the item</button></div></div>`;
  }
  const opts = Array.isArray(ask.options) ? ask.options : [];
  return `<div class="yq-askbody"><div class="yq-q">${esc(ask.text || it.title || "")}</div>
    ${opts.length ? `<div class="yq-choices">${opts.map((o, i) => `<button class="yq-btn${i === 0 ? " rec" : ""}" data-yq-do="ianswer" data-v="${esc(o)}">${esc(o)}</button>`).join("")}</div>` : ""}
    ${yqBox("i:" + it.id, "Your answer", 3)}
    <div class="yq-actions"><button class="yq-btn primary" data-yq-do="ianswer">Send</button><button class="yq-btn" data-yq-do="iopen">Open the item</button></div></div>`;
}

function yqPageHTML(t, a) {
  const pages = a.pages || yqPages(t);
  const src = pages[Math.min(yqView.page, pages.length - 1)] || "";
  return `<div class="yq-pagewrap"><div class="yq-pagebar">${pages.length > 1 ? pages.map((p, i) => `<button class="yq-btn${i === yqView.page ? " on" : ""}" data-yq-do="page" data-i="${i}">${esc(p.split("/").pop())}</button>`).join("") : `<span class="yq-dim">${esc(src)}</span>`}
      <span class="sp"></span><span class="yq-dim">Marks <span class="yq-help" tabindex="0" data-tip="Press Mark up the page, pick Arrow, Circle or Box and drag on the page, then write a note on the mark. Each mark tells the agent the element and the position it points at, so no screenshot is needed.">?</span></span>
      <span class="yq-seg" id="yqm-tools" hidden><button data-yq-do="mk-tool" data-v="arrow">Arrow</button><button data-yq-do="mk-tool" data-v="circle">Circle</button><button data-yq-do="mk-tool" data-v="box">Box</button></span>
      <button class="yq-btn" data-yq-do="mk-toggle">Mark up the page</button><button class="yq-btn send" id="yqm-send" data-yq-do="mk-send" hidden></button>
      <a class="yq-btn" href="${esc(src)}" target="_blank" rel="noopener">open in a tab</a></div>
    <div class="yqm-wrap" id="yqm-wrap"><iframe class="yq-frame" id="yq-frame" src="${esc(src)}"></iframe><svg class="yqm-ov" id="yqm-ov"></svg><div class="yqm-pop" id="yqm-pop" hidden></div></div>
    <div class="yq-actions">${yqBox("p:" + t.id, "A note for the agent: what to change, or anything else", 2)}</div>
    <div class="yq-actions"><button class="yq-btn" data-yq-do="choices">Send my choices on the page</button>
      <button class="yq-btn primary" data-yq-do="approve">Approve</button><button class="yq-btn" data-yq-do="sendback">Send back</button></div></div>`;
}

function yqSignHTML(t, a) {
  return `<div class="yq-askbody"><div class="yq-q">${esc(t.display_title || "")}</div><p>${esc(a.why || "")}</p>
    <p class="yq-dim">${esc(String(t.recap || "").slice(0, 600))}</p>
    ${yqBox("s:" + t.id, "Send back: what to change", 2)}
    <div class="yq-actions"><button class="yq-btn" data-yq-do="walk">Walk the change</button><button class="yq-btn primary" data-yq-do="sign">Sign</button>
      <button class="yq-btn" data-yq-do="sendback">Send back</button></div></div>`;
}

function yqPermHTML(t, a) {
  const p = a.perm;
  return `<div class="yq-askbody"><div class="yq-q">${esc(t.display_title || "")} is waiting on a permission</div><p><code>${esc(a.why || "")}</code></p>
    <div class="yq-actions">${p ? `<button class="yq-btn primary" data-yq-do="permok" data-id="${esc(p.id)}">Approve once</button><button class="yq-btn" data-yq-do="permno" data-id="${esc(p.id)}">Block once</button>` : ""}
      <button class="yq-btn" data-yq-do="mode-term">Look at the terminal</button></div></div>`;
}

function yqUnblockHTML(t, a) {
  return `<div class="yq-askbody"><div class="yq-q">${esc(a.why || "")}</div>
    ${yqBox("u:" + t.id, "Unblock: what it needs to hear", 3)}
    <div class="yq-actions"><button class="yq-btn" data-yq-do="restart">Restart</button><button class="yq-btn primary" data-yq-do="unblock">Unblock</button>
      <button class="yq-btn danger" data-yq-do="drop">${yqView.drop ? "Drop it: click again" : "Drop"}</button>
      <button class="yq-btn" data-yq-do="mode-term">Look at the terminal</button></div></div>`;
}

function yqMergeHTML(m) {
  return `<div class="yq-askbody"><div class="yq-q">Merge request ${m.n} is red: ${esc(m.branch)}</div>
    <p>${esc((m.failing || []).join(", ") || m.reason || "")}</p>${m.requester ? `<p class="yq-dim">asked by ${esc(m.requester)}${m.requester_room ? "@" + esc(m.requester_room) : ""}</p>` : ""}
    ${m.output ? `<pre class="yq-out">${esc(String(m.output).slice(-4000))}</pre>` : ""}</div>`;
}

function yqAskBody(a) {
  const t = yqCard();
  if (a.merge) return yqMergeHTML(a.merge);
  if (a.item && (!t || a.verb === "APPROVE")) return yqItemHTML(a.item);
  if (!t) return `<div class="yq-askbody"><p>${esc(a.why || "")}</p></div>`;
  if (a.verb === "UNBLOCK") return yqUnblockHTML(t, a);
  if (a.perm !== undefined) return yqPermHTML(t, a);
  if (a.verb === "SIGN") return yqSignHTML(t, a);
  if (a.verb === "TRY" || a.verb === "APPROVE") return a.pages && a.pages.length ? yqPageHTML(t, a) : yqSignHTML(t, a);
  if (a.item) return yqItemHTML(a.item);
  return typeof qaOpenQs === "function" && qaOpenQs(t).length ? yqInterviewHTML(t) : yqReplyHTML(t);
}

function yqPaintAsk(force) {
  const el = yqAskEl();
  if (!el) return;
  const back = document.getElementById("yq-back");
  const a = yqView.ask;
  const t = a && a.card;
  if (a && t && yqView.mode === "term" && typeof termTask !== "undefined" && termTask && bareId(termTask.id) !== bareId(t.id) && Date.now() - yqView.at > 4000) {
    yqView.mode = "ask";
  }
  if (back) back.hidden = !a || yqView.mode === "ask";
  if (!yqView.ask) {
    const s = yqView.sent;
    const empty = (s ? `<p class="yq-sent${s.bad ? " bad" : ""}" role="status">${esc(s.line)}</p>` : "") + `<p class="yq-pick">Pick an ask on the left.</p>`;
    if (el.__yq !== empty) { el.__yq = empty; el.innerHTML = empty; }
    return;
  }
  const left = typeof lastTasks !== "undefined" && (!yqView.ask.item || yqHub.items) && !yqAsks(lastTasks).some(x => x.key === yqView.key);
  if (left && yqView.listed) return yqCloseAsk();
  if (!left) yqView.listed = true;
  const fresh = yqLast.find(x => x.key === yqView.key);
  if (fresh) yqView.ask = fresh;
  const ask = yqView.ask;
  back.className = "yq-back v-" + ask.verb.toLowerCase();
  back.textContent = "back to the " + ask.verb.toLowerCase();
  const html = `<div class="yq-askbar v-${ask.verb.toLowerCase()}"><span class="yq-verb">${ask.verb}</span><strong>${esc(ask.title)}</strong><span class="sp"></span>
    ${ask.card ? `<span class="yq-seg"><button class="on" data-yq-do="mode-ask">the ask</button><button data-yq-do="mode-term">plain terminal</button></span>` : ""}
    <button class="yq-btn" data-yq-do="close" aria-label="close the ask">&times;</button></div>
    <div class="yq-askscroll v-${ask.verb.toLowerCase()}">${ask.card ? `<p class="yq-to" role="status">Sends to <b>${esc(yqTarget(ask.card.id))}</b></p>` : ""}${yqView.sent && (yqView.sent.bad || yqView.sent.stay) ? `<p class="yq-sent bad" role="status">${esc(yqView.sent.line)}</p>` : ""}${yqAskBody(ask)}</div>`;
  if (!force && el.__yq === html) return;
  if (!force && el.contains(document.activeElement) && document.activeElement.tagName === "TEXTAREA") return;
  el.__yq = html;
  el.innerHTML = html;
  if (typeof yqmPaint === "function") yqmPaint();
}

function yqBoxVal(prefix) {
  const box = document.querySelector(`#yq-ask textarea[data-yq-text^="${prefix}"]`);
  return box ? box.value.trim() : "";
}

function yqClearBox(prefix) {
  Object.keys(yqView.text).filter(k => k.startsWith(prefix)).forEach(k => delete yqView.text[k]);
}

async function yqSay(id, text, said) {
  if (!text) { toast("nothing to send", "type something first"); return false; }
  const to = yqTarget(id);
  let r = null;
  try {
    r = await api(`/v1/tasks/${encodeURIComponent(id)}/message`, {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ text })
    });
  } catch (e) {
    yqView.sent = { to, bad: true, line: "Not sent to " + to + ": " + e.message + ". Your text is still in the box." };
    toast("did not send", e.message);
    yqPaintAsk(true);
    return false;
  }
  const d = String((r && r.delivered) || "queued");
  const how = d === "terminal" ? "typed into its terminal now" : d === "undeliverable" ? "queued, but nothing will read it until it runs again"
    : d === "queued-unconfirmed" ? "queued, delivery is not confirmed" : "queued, it reaches the agent when its input is clear";
  yqView.sent = { to, bad: d === "undeliverable", line: (said ? said[0].toUpperCase() + said.slice(1) : "Sent") + " to " + to + ": " + how + "." + (r && r.warning ? " " + r.warning : "") };
  toast(said || "sent", "to " + to + ": " + how);
  if (typeof refreshSoon === "function") refreshSoon();
  return true;
}

// What the human set on an agent's page: every box ticked or not, every choice picked, every field filled, each with
// the words beside it. Read straight from the frame, which is same-origin, so the page needs nothing of its own.
function yqPageChoices(frame) {
  let doc = null;
  try { doc = frame && frame.contentDocument; } catch (e) { doc = null; }
  if (!doc) return "";
  const words = el => {
    const lab = (el.id && doc.querySelector(`label[for="${CSS.escape(el.id)}"]`)) || el.closest("label");
    const box = lab || (el.tagName === "SELECT" ? null : el.closest("li, tr, p, div") || el.parentElement);
    return String((box && box.textContent) || el.name || "").replace(/\s+/g, " ").trim().slice(0, 160);
  };
  const lines = [];
  doc.querySelectorAll("input, select, textarea").forEach(el => {
    const type = (el.type || "").toLowerCase();
    if (["hidden", "button", "submit", "reset", "search"].includes(type)) return;
    if (type === "checkbox") lines.push((el.checked ? "[x] " : "[ ] ") + words(el));
    else if (type === "radio") { if (el.checked) lines.push((el.name ? el.name + ": " : "") + words(el)); }
    else if (el.tagName === "SELECT") lines.push(words(el) + ": " + ((el.selectedOptions[0] && el.selectedOptions[0].textContent) || el.value).trim());
    else if (String(el.value || "").trim()) lines.push((el.name || el.placeholder || words(el) || "field") + ": " + String(el.value).trim());
  });
  return lines.join("\n");
}

async function yqAskClick(e) {
  const b = e.target.closest && e.target.closest("[data-yq-do]");
  if (!b || !yqView.ask) return;
  const a = yqView.ask, t = yqCard(), id = t && t.id, d = b.dataset.yqDo;
  if (d.startsWith("mk-")) return yqmClick(b, d);
  if (d === "close") return yqCloseAsk();
  if (d === "mode-term") return yqToTerm();
  if (d === "mode-ask") { yqView.mode = "ask"; if (typeof switchView === "function") switchView("queue"); return yqPaintAsk(true); }
  if (d === "q") { yqView.qi = Number(b.dataset.i) || 0; return yqPaintAsk(true); }
  if (d === "page") { yqView.page = Number(b.dataset.i) || 0; return yqPaintAsk(true); }
  if ((d === "permok" || d === "permno") && typeof decideFromTerm === "function") return decideFromTerm(b.dataset.id, d === "permok" ? "approve" : "block", b);
  if ((d === "pick" || d === "save" || d === "userec") && t) {
    const cur = yqGroups(t).find(g => g.current);
    if (!cur) return;
    const qi = Math.min(yqView.qi, cur.qs.length - 1), q = yqParseQ(cur.qs[qi]);
    const note = yqBoxVal("q:" + id + ":" + qi);
    const picked = String(cur.a[qi] || "").split(" - ")[0];
    if (d === "pick") cur.a[qi] = b.dataset.v + (note ? " - " + note : "");
    else if (d === "userec") cur.a[qi] = q.rec;
    else cur.a[qi] = q.choices.length ? (picked ? picked + (note ? " - " + note : "") : note) : note;
    if (typeof qaSaveNow === "function") qaSaveNow(id, yqGroups(t));
    if (d !== "pick" && qi < cur.qs.length - 1) yqView.qi = qi + 1;
    return yqPaintAsk(true);
  }
  if (d === "sendqs" && t) {
    const groups = yqGroups(t);
    if (await yqSay(id, typeof qaCompose === "function" ? qaCompose(groups) : "", "answers sent")) {
      delete yqView.drafts[id];
      if (typeof qaSaveNow === "function") qaSaveNow(id, []);
      yqCloseAsk();
    }
    return;
  }
  if (d === "reply" && t) { if (await yqSay(id, yqBoxVal("r:" + id), "answer sent")) { yqClearBox("r:" + id); yqCloseAsk(); } return; }
  if (d === "unblock" && t) { if (await yqSay(id, yqBoxVal("u:" + id), "unblocked")) { yqClearBox("u:" + id); yqCloseAsk(); } return; }
  if (d === "restart" && t && typeof restartCard === "function") { restartCard(id); return yqToTerm(); }
  if (d === "drop" && t) {
    if (!yqView.drop) { yqView.drop = true; return yqPaintAsk(true); }
    if (typeof endCardKeeping === "function") endCardKeeping(id, t);
    return yqCloseAsk();
  }
  if (d === "walk" && t && typeof walkOfferOpen === "function") { yqToTerm(); return walkOfferOpen(id); }
  if ((d === "approve" || d === "sign") && t) {
    const note = yqBoxVal("p:" + id) || yqBoxVal("s:" + id);
    const page = a.pages && a.pages[yqView.page] ? " " + a.pages[yqView.page] : "";
    const text = d === "sign" ? "Signed" + (t.report_sha ? " " + String(t.report_sha).slice(0, 8) : "") + ": land it." : "Approved" + page + ".";
    if (await yqSay(id, text + (note ? " " + note : ""), d === "sign" ? "signed" : "approved")) { yqClearBox("p:" + id); yqClearBox("s:" + id); yqCloseAsk(); }
    return;
  }
  if (d === "sendback" && t) {
    const note = yqBoxVal("p:" + id) || yqBoxVal("s:" + id);
    if (!note) { toast("say what to change", "a send back carries a note"); return; }
    if (await yqSay(id, "Sent back: " + note, "sent back")) { yqClearBox("p:" + id); yqClearBox("s:" + id); yqCloseAsk(); }
    return;
  }
  if (d === "choices" && t) {
    const got = yqPageChoices(document.getElementById("yq-frame"));
    if (!got) { toast("no choices on the page", "nothing on it can be read as a choice. type a note instead"); return; }
    const note = yqBoxVal("p:" + id);
    const page = a.pages && a.pages[yqView.page] ? a.pages[yqView.page] : "the page";
    if (await yqSay(id, "My choices on " + page + ":\n" + got + (note ? "\n" + note : ""), "choices sent")) { yqClearBox("p:" + id); yqView.sent.stay = true; yqPaintAsk(true); }
    return;
  }
  const it = a.item;
  if (!it) return;
  if (d === "iopen") { if (typeof switchView === "function") switchView("items"); if (typeof itemsShow === "function") itemsShow(it.id); return; }
  try {
    if (d === "iapprove") await itemsData.act(it.id, it.ask && it.ask.sha ? { do: "approve", sha: it.ask.sha } : { do: "approve" }, it.backlog);
    else if (d === "isendback") {
      const note = yqBoxVal("i:" + it.id);
      if (!note) { toast("say what to change", "a send back carries a note"); return; }
      await itemsData.act(it.id, { do: "send_back", note }, it.backlog);
    } else if (d === "ianswer") {
      const answer = b.dataset.v || yqBoxVal("i:" + it.id);
      if (!answer) { toast("nothing to send", "type the answer first"); return; }
      await itemsData.act(it.id, { do: "unblock", answer }, it.backlog);
    } else return;
  } catch (err) {
    toast("could not " + d.slice(1), err.message);
    return;
  }
  const to = it.backlog || it.id;
  const said = d === "iapprove" ? "Approved" : d === "isendback" ? "Sent back" : "Answer sent";
  yqView.sent = { to, bad: false, line: said + " to " + to + "." };
  toast("sent to " + to, "");
  yqHub.at = 0;
  yqCloseAsk();
}

// A page an agent made may post its own answer: parent.postMessage({ atrium: "reply", text }) from the page in the frame.
window.addEventListener("message", e => {
  const frame = document.getElementById("yq-frame");
  const t = yqCard();
  if (!frame || !t || e.source !== frame.contentWindow || !e.data || e.data.atrium !== "reply") return;
  const text = String(e.data.text || "").trim();
  if (text) yqSay(t.id, text, "sent from the page");
});

yqPaintBar();
setInterval(() => { yqLoadHub(); yqPaint(); yqPaintAsk(); }, 5000);
