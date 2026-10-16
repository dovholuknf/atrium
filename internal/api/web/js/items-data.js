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
  const ask = r.state === "blocked" ? { who: r.owner_kind === "human" ? "" : (r.owner || ""), text: r.note || "(no reason given)",
    since: r.updated_at || null, options: [] } : null;
  return Object.assign({}, r, {
    id: r.item_id, backlog: r.backlog_id || r.work_id || r.item_id, title: r.title || "", body: r.body || "", priority: r.priority || "", dept: r.dept || "",
    cards: r.card ? [{ name: r.card, id: r.card, room: r.room || "" }] : [], steps, ask,
    tokens: null, usd: null, coord_usd: null, history: r.history || []
  });
}

// What the hub answered, taken as an item whichever shape it is.
function itemFromAnswer(r) {
  if (!r) return null;
  if (Array.isArray(r.steps) && r.steps.length) {
    return Object.assign({ id: r.id || r.item_id, backlog: r.backlog || r.backlog_id || r.work_id || r.id || r.item_id, title: "", body: "", priority: "",
      dept: "", cards: [], ask: null, tokens: null, usd: null, coord_usd: null, history: [] }, r);
  }
  return itemFromHub(r);
}

function itemsStatsFrom(list, given) {
  if (given) return given;
  const blockedMe = list.filter(it => it.state === "blocked" && it.owner_kind === "human").length;
  const flying = list.filter(it => it.state === "running" || it.state === "blocked");
  const working = flying.filter(it => { const i = itemCurrent(it); return i >= 2 && i <= 5; }).length;
  return { needs_you: blockedMe, who: "you", in_flight: flying.length, working, spend_today: null, spend_coord: null,
    landed_week: null, landed_median: "", landed_cost: null };
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
  if (!w || !Array.isArray(w.steps) || !w.steps.length) return it;
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
  const out = Object.assign({}, it, { steps });
  // The assignment row has no title of its own: the work row does, the backlog item's title.
  const wk = w.work && typeof w.work === "object" ? w.work : w;
  if (!out.title && wk.title) out.title = wk.title;
  if (!out.body && wk.body) out.body = wk.body;
  if (!out.priority && wk.priority) out.priority = wk.priority;
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

const itemsData = {
  async list() {
    if (itemsMockOn()) {
      const a = await window.itemsMock.list();
      const items = (a.items || []).map(itemFromAnswer);
      return { items, stats: itemsStatsFrom(items, a.stats) };
    }
    const [a, work] = await Promise.all([itemsFetch("/_hub/items"), itemsWorkByBacklog()]);
    const items = ((a && a.items) || []).map(itemFromAnswer).map(it => itemWithWork(it, work[it.backlog]));
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
    return (await this.list()).items.filter(it => it.state === "blocked" && it.owner_kind === "human").length;
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
