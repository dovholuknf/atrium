// Where the items tab gets its data. ONE FILE, THE SEAM: js/items.js draws and never fetches, so wiring the hub's L2 to L5 routes is
// an edit to this file and nothing else.
//
// WHAT THE TAB DRAWS (docs/rnd/item-lifecycle-design.md, section 7). An item is:
//
//   id          what /_hub/items/<id> is called with (the answer and cancel POSTs use it too)
//   backlog     the id shown, the backlog id (`id` until the hub says otherwise)
//   title, body, priority, dept, created_by, owner_kind, owner, room, card, created_at, updated_at
//   state       blocked | running | queued | finished     (cancelled is a finished item whose `note` says so)
//   cards       [{ name, id, room }]     the cards on it, `id` is the one whose terminal the chip opens
//   steps       eight of { name, state, from, to, note, when, cards:[{name, role, coord}], sha, strength, fact, reason, usd, tokens }
//               state is done | active | blocked | stale | skipped | pending, tokens is { in, cw, cr, out } or null
//   ask         { who, text, brief, since, options:[] } or null, the question a blocked item is waiting on (`brief` is its one line for the list)
//               `kind: "approval"` (with the design `sha`) is a design waiting on the human: its answers are `approve` and `send back`, not text
//               typed to a worker. `options` is up to three answers the worker suggested, the first its own recommendation
//   tokens, usd, coord_usd     the item's steps, and the coordination dollars apart. `usd` null is "unpriced", drawn "?"
//   coordination, prices, price_table   (detail only) [{ name, note, usd }], [{ k, v }], "v3, list prices"
//
// A list answer is { items, stats } and a detail answer is one item. `stats` is optional and derived from the rows when the
// hub sends none. Money is an ESTIMATE from real token counts (clint, section 11 answer 4): the tokens are always kept.
//
// L7: `/_hub/work` (L2) is read beside the items and its steps are laid over the row with the same backlog id (`itemWithWork`), and a design
// the engine is waiting on the human for becomes the approval question. Both are best effort: an older hub's 404 leaves the one step below.
//
// TODAY the hub's `/_hub/items` rows are the older shape (item_id, card, state, note) with no steps, so `fromHub` makes the one
// step it can honestly say, `building`, as the backfill of section 9 does. A row that already carries `steps` is taken as it is.
// A mock (js/items-mock.js) answers instead of the hub only when the URL asks (`?itemsmock=1`), for that load alone, and says so.
const ITEM_STEPS = ["designed", "approved", "building", "tested", "reviewed", "landed", "deployed", "pushed"];

function itemsMockOn() {
  try { return /[?&]itemsmock=1(&|$)/.test(location.search) && !!window.itemsMock; } catch (e) { return false; }
}

// While the mock is on, a corner note says nothing here reaches the hub. It is fixed, so it moves nothing on the page.
(function () {
  function bar() {
    if (!itemsMockOn() || document.getElementById("it-mockbar")) return;
    const b = document.createElement("div");
    b.id = "it-mockbar"; b.className = "it-mockbar"; b.setAttribute("role", "status");
    b.textContent = "Fixture data: nothing here reaches the hub.";
    document.body.appendChild(b);
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", bar); else bar();
})();

async function itemsFetch(path, init) {
  const fetcher = typeof plainFetch === "function" ? plainFetch : window.fetch.bind(window);
  const res = await fetcher(path, init);
  let body = null;
  try { body = await res.json(); } catch (e) { /* an empty body is fine */ }
  if (!res.ok) throw new Error((body && body.error) || res.status + "");
  return body;
}

// The order the board is in when it asks the hub for the step a row is at.
function itemsBlankSteps() {
  return ITEM_STEPS.map(name => ({ name, state: "pending", from: null, to: null, when: "", note: "", cards: [], sha: "", strength: "", fact: "",
    reason: "", usd: null, tokens: null }));
}

// One older-shaped hub row, as the item it is. Only `building` is known, so it is the only step that is not pending.
function itemFromHub(r) {
  const steps = itemsBlankSteps();
  const b = steps[2];
  b.from = r.created_at || null;
  if (r.state === "finished") { b.state = "done"; b.to = r.updated_at || null; }
  else if (r.state === "blocked") b.state = "blocked";
  else if (r.state === "running") b.state = "active";
  else steps[0].state = "active";
  // No note is no question: the text stays empty and `itemNeedsYou` reads that as nothing to answer.
  const ask = r.state === "blocked" ? { who: r.owner_kind === "human" ? "" : (r.owner || ""), text: r.note || "",
    since: r.updated_at || null, options: [] } : null;
  return Object.assign({}, r, {
    hasWork: false, id: r.item_id, backlog: r.backlog_id || r.work_id || r.item_id, title: r.title || "", body: r.body || "", priority: r.priority || "", dept: r.dept || "",
    cards: r.card ? [{ name: r.card, id: r.card, room: r.room || "" }] : [], steps, ask,
    tokens: null, usd: null, coord_usd: null, history: r.history || []
  });
}

// What the hub answered, taken as an item whichever shape it is.
function itemFromAnswer(r) {
  if (!r) return null;
  if (Array.isArray(r.steps) && r.steps.length) {
    return Object.assign({ id: r.id || r.item_id, backlog: r.backlog || r.backlog_id || r.work_id || r.id || r.item_id, title: "", body: "", priority: "",
      dept: "", cards: [], ask: null, tokens: null, usd: null, coord_usd: null, history: [], hasWork: true }, r);
  }
  return itemFromHub(r);
}

// The counts for the chips, from the folded rows. The hub's own `stats` is read for spend and landed only, which it alone knows.
function itemsStatsFrom(list, given) {
  const n = g => list.filter(it => it.fold && it.fold.group === g).length;
  const g = given || {};
  return { needs_you: n("needs-you"), working: n("working"), idle: n("idle"), queued: n("queued"), who: g.who || "you",
    spend_today: g.spend_today == null ? null : g.spend_today, spend_coord: g.spend_coord == null ? null : g.spend_coord,
    landed_week: g.landed_week == null ? null : g.landed_week, landed_median: g.landed_median || "", landed_cost: g.landed_cost == null ? null : g.landed_cost };
}

// ---- one row per backlog id ----
// `/_hub/items` answers one ASSIGNMENT per card. A row is a BACKLOG id, so the assignments are folded here
// (docs/ui/items-tab-ux-pass.md). Everything in this section is pure, so scripts/test-items-fold.js runs it on stubs.

// The steps a CARD does. Approved is the human's, deployed and pushed are atrium's: only a card step can be idle.
const ITEM_CARD_STEPS = ["designed", "building", "tested", "reviewed", "landed"];

function itemsDataMs(iso) {
  const t = iso ? Date.parse(iso) : NaN;
  return isNaN(t) ? 0 : t;
}

// What one assignment's card is: none (not started), live, offline (its room is not answering, so unknown and never idle) or ended.
// `tasks` null is a board with no card list yet: a running assignment is then given the benefit of the doubt.
function itemCardKind(a, tasks, stateOf) {
  if (!a.card) return "none";
  if (a.state === "finished") return "ended";
  if (!tasks) return "live";
  const t = tasks.find(x => x.id === a.card && (!x.room || !a.room || x.room === a.room));
  if (!t) return "ended";
  const st = stateOf ? stateOf(t) : t.offline ? "unreachable" : "live";
  return st === "off" ? "ended" : st === "unreachable" ? "offline" : "live";
}

// The question an assignment holds for the person: its ask text, else its note. Empty is nothing to answer.
function itemQuestionText(a) { return ((a.ask && a.ask.text) || a.note || "").trim(); }

// THE ONE ANSWER to "does this item need clint". The header chip, the group and the tab's badge all read it. A design waiting on
// the approval, or a block owned by the human that carries a question. A block with no text is nothing to answer, and a block owned
// by a session is that session's to answer.
function itemNeedsYou(it) {
  if (!it) return false;
  if (it.fold) return it.fold.group === "needs-you";
  return itemAwaitsApproval(it) || (it.state === "blocked" && it.owner_kind === "human" && !!itemQuestionText(it));
}

// Fold assignments (each already an item, with the work steps laid over it) into one row per backlog id. A row is its driver's fields
// plus `assignments` (all of them, newest first) and `fold`: { group, cards, live, offline, ended, waitingOn, noCard, cancelled, step }.
// The driver is the newest assignment whose card is live, else one on an offline room, else the newest open one, else the newest.
function foldItems(list, tasks, stateOf) {
  const by = new Map();
  for (const a of list) {
    const k = a.backlog || a.id;
    if (!by.has(k)) by.set(k, []);
    by.get(k).push(a);
  }
  const rows = [];
  for (const as of by.values()) {
    as.sort((x, y) => itemsDataMs(y.updated_at) - itemsDataMs(x.updated_at));
    const kinds = as.map(a => itemCardKind(a, tasks, stateOf));
    const rank = i => kinds[i] === "live" ? 0 : kinds[i] === "offline" ? 1 : as[i].state !== "finished" ? 2 : 3;
    let di = 0;
    for (let i = 1; i < as.length; i++) if (rank(i) < rank(di)) di = i;
    const d = as[di];
    const cards = as.map((a, i) => ({ name: (a.cards && a.cards[0] && a.cards[0].name) || a.card || "", id: a.card || "", room: a.room || "", kind: kinds[i],
      state: a.state, note: a.note || "", at: a.created_at || a.updated_at || null })).filter(c => c.id);
    const live = kinds.filter(k => k === "live").length, offline = kinds.filter(k => k === "offline").length;
    const approval = as.find(a => itemAwaitsApproval(a));
    const row = Object.assign({}, d, { assignments: as });
    if (approval) { row.ask = approval.ask; row.state = "blocked"; row.owner_kind = "human"; }
    const steps = row.steps || [], cur = itemCurrent(row), step = steps[cur] || { name: "" };
    // The last step done, or a backlog item closed, is finished whatever an earlier step says.
    const pushed = steps.length > 0 && steps[steps.length - 1].state === "done";
    const closed = row.backlogStatus === "done" || row.backlogStatus === "dropped";
    const allDone = pushed || closed || (steps.length > 0 && steps.every(s => s.state === "done" || s.state === "skipped"));
    const everyEnded = as.every(a => a.state === "finished");
    const cancelled = everyEnded && d.state === "finished" && /cancelled/i.test(d.note || "");
    const humanAsks = !!approval || (d.state === "blocked" && d.owner_kind === "human" && !!itemQuestionText(d));
    let group;
    if (cancelled || (row.hasWork ? allDone : everyEnded)) group = "finished";
    else if (humanAsks) group = "needs-you";
    else if (as.every(a => a.state === "queued")) group = "queued";
    else if (ITEM_CARD_STEPS.includes(step.name) && !live && !offline) group = "idle";
    else group = "working";
    const waitingOn = d.state === "blocked" && !humanAsks ? (d.owner_kind === "human" ? "you" : d.owner || "its owner") : "";
    row.fold = { group, driverKind: kinds[di], cards, live, offline, ended: kinds.filter(k => k === "ended").length, waitingOn, cancelled, step: cur,
      noCard: group === "working" && !live && !offline && step.name !== "approved" && !ITEM_CARD_STEPS.includes(step.name) };
    rows.push(row);
  }
  return rows;
}

// The index of the step an item is at: the one active or blocked, else the first that is not done, else the last.
function itemCurrent(it) {
  const s = it.steps || [];
  let i = s.findIndex(x => x.state === "active" || x.state === "blocked" || x.state === "stale");
  if (i < 0) i = s.findIndex(x => x.state === "pending");
  return i < 0 ? s.length - 1 : i;
}

// A design waiting on the human's click: the question is the approval, and its answers are `approve` and `send back`, which are
// POSTs to /_hub/work/<backlog id> (L2), not a line typed to a worker.
function itemAwaitsApproval(it) { return !!it && it.state === "blocked" && !!it.ask && it.ask.kind === "approval"; }

// What /_hub/work (L2) says of one backlog id, laid over the item: its steps, and the approval when the human is the party. A
// step row is { step, state, party, started_at, ended_at, sha, cards, reason }, `cards` a JSON list. The item row stays whatever
// the hub's `/_hub/items` said unless the engine is waiting on the human for the design.
function itemWithWork(it, w) {
  const tasks = w && Array.isArray(w.tasks) && w.tasks.length ? w.tasks : null;
  if (!w || !Array.isArray(w.steps) || !w.steps.length) return tasks ? Object.assign({}, it, { tasks }) : it;
  const by = {};
  for (const s of w.steps) by[s.step] = s;
  const steps = itemsBlankSteps().map(b => {
    const s = by[b.name];
    if (!s) return b;
    let cards = [];
    try { cards = (typeof s.cards === "string" ? JSON.parse(s.cards || "[]") : s.cards || []).map(c => ({ name: c.name || c.card || "", role: c.role || "", coord: !!c.coord })); } catch (e) { /* none */ }
    if (s.party === "human") cards = [{ name: "you", role: "human", coord: false }];
    return Object.assign(b, { state: s.state || "pending", from: s.started_at || null, to: s.ended_at || null, sha: s.sha || "", cards,
      reason: s.state === "skipped" ? s.reason || "" : "", note: s.state === "active" ? s.reason || "" : "" });
  });
  const out = Object.assign({}, it, { steps, hasWork: true });
  if (tasks) out.tasks = tasks;
  // The landed check (hub internal/link/docdebt_landed.go): {not_said} for a feature, {offer: {id, title}} for a doc worker's item.
  if (w.docs && typeof w.docs === "object") out.docs = w.docs;
  // The assignment row has no title of its own: the work row does, the backlog item's title.
  const wk = w.work && typeof w.work === "object" ? w.work : w;
  if (!out.title && wk.title) out.title = wk.title;
  if (!out.body && wk.body) out.body = wk.body;
  if (!out.priority && wk.priority) out.priority = wk.priority;
  if (wk.status) out.backlogStatus = wk.status;
  if (!out.dept && wk.dept) out.dept = wk.dept;
  const ap = by.approved;
  if (ap && (ap.state === "active" || ap.state === "stale") && ap.party === "human") {
    out.state = "blocked"; out.owner_kind = "human";
    out.ask = { who: "approval", kind: "approval", sha: ap.sha || "", since: ap.started_at || it.updated_at || null, options: [],
      text: "The design is ready" + (ap.sha ? " at " + ap.sha.slice(0, 7) : "") + (ap.state === "stale" ? ", and the doc has moved since it was read" : "") +
        ". Approve it, or send it back with a note for the designer." };
  }
  return out;
}

// The engine's rows by backlog id, or none when the hub has no such route yet (an older hub answers 404 and the tab carries on).
async function itemsWorkByBacklog() {
  try {
    const a = await itemsFetch("/_hub/work");
    const m = {};
    for (const w of (a && a.items) || (a && a.work) || []) m[w.backlog_id] = w;
    return m;
  } catch (e) { return {}; }
}

// The board's cards for the live test, or null while it has none (the phone page has no list at all, and a board still loading has an empty one).
function itemsKnownCards() {
  return !itemsMockOn() && typeof lastTasks !== "undefined" && lastTasks && lastTasks.length ? lastTasks : null;
}

// The board's own word for a card (live, working, waiting, unreachable, off), where there is one.
function itemsStateOf() { return typeof cardState === "function" ? cardState : null; }

const itemsData = {
  async list() {
    if (itemsMockOn()) {
      const a = await window.itemsMock.list();
      const items = foldItems((a.items || []).map(itemFromAnswer), null);
      return { items, stats: itemsStatsFrom(items, a.stats) };
    }
    const [a, work] = await Promise.all([itemsFetch("/_hub/items"), itemsWorkByBacklog()]);
    const items = foldItems(((a && a.items) || []).map(itemFromAnswer).map(it => itemWithWork(it, work[it.backlog])), itemsKnownCards(), itemsStateOf());
    return { items, stats: itemsStatsFrom(items, a && a.stats) };
  },
  // The efficacy page's one read (internal/link/efficacy_report.go): { by: "day"|"week", observedOnly }.
  async efficacy(q) {
    if (itemsMockOn()) return window.itemsMock.efficacy(q);
    return itemsFetch("/_hub/work/efficacy?by=" + encodeURIComponent(q.by || "week") + (q.observedOnly ? "&observed_only=1" : ""));
  },
  // The count of items blocked on the human, for the tab's badge, when the tab is not the one on screen.
  async blockedCount() {
    // A design waiting for approval is blocked on the human too, and only the step engine says so, so the count reads the list.
    return (await this.list()).items.filter(itemNeedsYou).length;
  },
  async detail(id) {
    if (itemsMockOn()) return itemFromAnswer(await window.itemsMock.detail(id));
    const a = await itemsFetch("/_hub/items/" + encodeURIComponent(id));
    const it = itemFromAnswer(a && a.item ? Object.assign({}, a.item, a, { item: undefined }) : a);
    if (a && a.history) it.history = a.history;
    let w = null;
    try { w = await itemsFetch("/_hub/work/" + encodeURIComponent(it.backlog)); } catch (e) { /* an older hub: the one step it can say */ }
    return itemWithWork(it, w);
  },
  // { do: "unblock", answer } or { do: "cancel" }, as the board, which the hub takes from its own machine only.
  async act(id, body, backlog) {
    if (itemsMockOn()) return window.itemsMock.act(id, body);
    // The human's two acts on a design are the step engine's, one route per backlog id: {do: approve, sha?} or {do: send_back, note}.
    if (body.do === "approve" || body.do === "send_back") {
      return itemsFetch("/_hub/work/" + encodeURIComponent(backlog || id), {
        method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(Object.assign({ by: "board" }, body))
      });
    }
    return itemsFetch("/_hub/items/" + encodeURIComponent(id), {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(Object.assign({ by: "board" }, body))
    });
  }
};
