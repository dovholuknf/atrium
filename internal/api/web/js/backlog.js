// The backlog pane: the items filed on the hub and the reports directors left. It wears the same view bar as the stack,
// board and terminals (js/filter-drawer.js): a search box, then SHOW (status), PRIORITY, DEPT, SORT BY and GROUP. Items
// also sort by a click on a header (and back again on a second), and take a new priority from the select on a row.
//
// The hub holds both so a director on any room sees what another room filed. See docs/rnd/reports-channel-design.md.
// Filing, changing a status and adding a report are `atrium backlog` and `atrium reports`, and the tools of the same
// names. The board only changes a priority.
//
// HUB-ONLY. `/_hub/backlog` is a 404 on a plain daemon, so the tab shows once the hub probe in rooms.js has found a
// hub, through `paintBacklogTab`. It is live by the `backlog` and `report` events, and fetched on open.
// Every chip row is a set and an empty set means all of it, as on the other tabs. Each is kept per browser under its own key.
const BACKLOG_SHOW_KEY = "atrium.backlog.show";
const BACKLOG_PRIO_KEY = "atrium.backlog.prios";
const BACKLOG_DEPT_KEY = "atrium.backlog.depts";
const BACKLOG_SORT_KEY = "atrium.backlog.sort";
const BACKLOG_GROUP_KEY = "atrium.backlog.group";
const BACKLOG_STATUSES = ["open", "in-progress", "held", "done", "dropped"];
const BACKLOG_SHOW_DEFAULT = ["open", "in-progress"];
const BACKLOG_GROUPS = [["dept", "by dept"], ["status", "by status"], ["priority", "by priority"], ["", "off"]];
const BACKLOG_SORTS = [["changed", "changed"], ["created", "created"], ["priority", "priority"], ["id", "id"]];

function backlogStored(key, fallback) {
  try {
    const raw = localStorage.getItem(key);
    return raw === null ? fallback : JSON.parse(raw);
  } catch (e) { return fallback; }
}
function backlogStore(key, value, isDefault) {
  try {
    if (isDefault) localStorage.removeItem(key); else localStorage.setItem(key, JSON.stringify(value));
  } catch (e) {}
}

// Statuses shown. Open and in-progress unless the operator chose otherwise. Empty means every status.
let backlogShow = new Set(backlogStored(BACKLOG_SHOW_KEY, BACKLOG_SHOW_DEFAULT));
// Priorities shown, as a set. Empty means all of them. `unset` is the items with none.
const backlogPrios = new Set(backlogStored(BACKLOG_PRIO_KEY, []));
const backlogDepts = new Set(backlogStored(BACKLOG_DEPT_KEY, []));
let backlogGroup = backlogStored(BACKLOG_GROUP_KEY, "dept");
// The header the list is sorted by, or null for the default order. `dir` is 1 or -1.
let backlogSort = backlogStored(BACKLOG_SORT_KEY, null);
let backlogList = [];

const BACKLOG_PRIOS = ["high", "medium", "low", "unset"];
const BACKLOG_COLS = [
  { key: "status", label: "status" },
  { key: "dept", label: "dept" },
  { key: "id", label: "id" },
  { key: "priority", label: "priority" },
  { key: "title", label: "title" },
  { key: "created", label: "created" },
  { key: "changed", label: "changed" },
];

function paintBacklogTab() {
  const tab = document.querySelector('.tab[data-view="backlog"]');
  if (!tab) return;
  tab.hidden = typeof hubIsHub === "undefined" ? true : !hubIsHub;
}

async function backlogGet(path) {
  const fetcher = typeof plainFetch === "function" ? plainFetch : window.fetch.bind(window);
  const res = await fetcher(path);
  if (!res.ok) throw new Error(path + " unavailable");
  return res.json();
}

// The hub keeps high, medium, low or "" and sends nothing for unset.
function backlogPrio(b) {
  const p = String((b && b.priority) || "").trim().toLowerCase();
  return BACKLOG_PRIOS.includes(p) && p !== "unset" ? p : "unset";
}

function backlogRank(b) { return BACKLOG_PRIOS.indexOf(backlogPrio(b)); }

function backlogTime(s) {
  const t = s ? Date.parse(s) : NaN;
  return isNaN(t) ? 0 : t;
}

// The default order: interviewed items first (the field is empty until the hub has an interviewed state), then
// priority high to low, then newest.
function backlogDefaultCompare(a, b) {
  return (b.interviewed ? 1 : 0) - (a.interviewed ? 1 : 0) ||
    backlogRank(a) - backlogRank(b) ||
    backlogTime(b.created_at) - backlogTime(a.created_at) ||
    String(a.id).localeCompare(String(b.id));
}

// What a header sorts on. Text sorts A to Z, priority sorts high first, the times sort oldest first, and the first
// click on a column takes backlogFirstDir for it, so created and changed lead with the newest.
function backlogKeyOf(b, key) {
  switch (key) {
    case "priority": return backlogRank(b);
    case "created": return backlogTime(b.created_at);
    case "changed": return backlogTime(b.changed_at);
    default: return String(b[key] || "").toLowerCase();
  }
}

function backlogFirstDir(key) { return key === "created" || key === "changed" ? -1 : 1; }

function backlogSorted(list) {
  const out = list.slice();
  if (!backlogSort) return out.sort(backlogDefaultCompare);
  const { key, dir } = backlogSort;
  return out.sort((a, b) => {
    const x = backlogKeyOf(a, key), y = backlogKeyOf(b, key);
    const c = typeof x === "number" ? x - y : x < y ? -1 : x > y ? 1 : 0;
    return c * dir || backlogDefaultCompare(a, b);
  });
}

// The search text as words and #dept tokens, the way #tag works on the other tabs. A word has to be in the id, title, dept or
// body, and any one of the #depts has to be the item's.
function backlogQuery() {
  const words = [], depts = [];
  for (const w of fdSearchText("backlog").split(" ").filter(Boolean)) {
    if (w.startsWith("#")) { if (w.length > 1) depts.push(w.slice(1)); } else words.push(w);
  }
  return { words, depts };
}
function backlogMatchesText(b, q) {
  if (q.depts.length && !q.depts.includes(String(b.dept || "").toLowerCase())) return false;
  if (!q.words.length) return true;
  const hay = [b.id, b.title, b.dept, b.body].join(" ").toLowerCase();
  return q.words.every(w => hay.includes(w));
}

// `skip` leaves one row's filter out, so each chip's count is what clicking it would ADD to the list.
function backlogVisible(list, skip) {
  const q = backlogQuery();
  return list.filter(b => {
    if (skip !== "show" && backlogShow.size && !backlogShow.has(String(b.status || ""))) return false;
    if (skip !== "prio" && backlogPrios.size && !backlogPrios.has(backlogPrio(b))) return false;
    if (skip !== "dept" && backlogDepts.size && !backlogDepts.has(String(b.dept || ""))) return false;
    return backlogMatchesText(b, q);
  });
}

function backlogClickHeader(key) {
  if (backlogSort && backlogSort.key === key) backlogSort = { key, dir: -backlogSort.dir };
  else backlogSort = { key, dir: backlogFirstDir(key) };
  backlogStore(BACKLOG_SORT_KEY, backlogSort, false);
  paintBacklog();
}

function toggleBacklogSet(set, key, value) {
  if (set.has(value)) set.delete(value); else set.add(value);
  backlogStore(key, [...set], set.size === 0);
  paintBacklog();
}
function toggleBacklogShow(v) {
  if (v === "") backlogShow = new Set();
  else if (backlogShow.has(v)) backlogShow.delete(v); else backlogShow.add(v);
  const isDefault = backlogShow.size === BACKLOG_SHOW_DEFAULT.length && BACKLOG_SHOW_DEFAULT.every(x => backlogShow.has(x));
  backlogStore(BACKLOG_SHOW_KEY, [...backlogShow], isDefault);
  paintBacklog();
}
function toggleBacklogDept(d) { toggleBacklogSet(backlogDepts, BACKLOG_DEPT_KEY, d); }
function setBacklogGroup(g) {
  backlogGroup = g;
  backlogStore(BACKLOG_GROUP_KEY, g, g === "dept");
  paintBacklog();
}

function backlogHeadRow() {
  return '<div class="aud-row bl-row bl-head" role="row">' + BACKLOG_COLS.map(c => {
    const on = backlogSort && backlogSort.key === c.key;
    const arrow = on ? (backlogSort.dir === 1 ? " ▲" : " ▼") : "";
    return '<button type="button" class="bl-sort' + (on ? " on" : "") + '" data-sort="' + c.key + '"' +
      ' aria-sort="' + (on ? (backlogSort.dir === 1 ? "ascending" : "descending") : "none") + '">' +
      esc(c.label) + arrow + "</button>";
  }).join("") + "</div>";
}

function backlogStamp(s) {
  const t = backlogTime(s);
  return t ? new Date(t).toLocaleDateString() : "";
}

function backlogItemRow(b) {
  const p = backlogPrio(b);
  const opts = BACKLOG_PRIOS.map(o => '<option value="' + o + '"' + (o === p ? " selected" : "") + ">" + o + "</option>").join("");
  return '<div class="aud-row bl-row" data-id="' + esc(b.id || "") + '">' +
    '<span class="aud-when">' + esc(b.status || "") + "</span>" +
    '<span class="aud-room">' + esc(b.dept || "") + "</span>" +
    '<span class="aud-kind">' + esc(b.id || "") + "</span>" +
    '<select class="bl-prio" data-prio="' + p + '" data-id="' + esc(b.id || "") + '" aria-label="priority of ' + esc(b.id || "") + '">' +
    opts + "</select>" +
    '<span class="aud-detail">' + esc(b.title || "") + "</span>" +
    '<span class="aud-when" data-tip="' + esc(b.created_at || "") + '">' + esc(backlogStamp(b.created_at)) + "</span>" +
    '<span class="aud-when" data-tip="' + esc(b.changed_at || "") + '">' + esc(backlogStamp(b.changed_at)) + "</span>" +
    "</div>";
}

// What each chip would add, counted over the other rows' filters.
function paintBacklogChips() {
  const count = (skip, key, zeros) => {
    const c = {};
    zeros.forEach(z => { c[z] = 0; });
    backlogVisible(backlogList, skip).forEach(b => { const k = key(b); c[k] = (c[k] || 0) + 1; });
    return c;
  };
  const chip = (label, on, onclick, v) => `<button type="button" class="${on ? "on" : ""}" data-v="${esc(v)}" onclick="${onclick}">${esc(label)} <i class="pillcount"></i></button>`;
  const show = document.getElementById("backlog-show");
  if (show) show.innerHTML = chip("all", backlogShow.size === 0, "toggleBacklogShow('')", "") +
    BACKLOG_STATUSES.map(s => chip(s, backlogShow.has(s), "toggleBacklogShow('" + s + "')", s)).join("");
  const prios = document.getElementById("backlog-prios");
  if (prios) prios.innerHTML = BACKLOG_PRIOS.map(p => `<button type="button" data-prio="${p}" data-v="${p}"${backlogPrios.has(p) ? ' class="on" aria-pressed="true"' : ' aria-pressed="false"'}>${p} <i class="pillcount"></i></button>`).join("");
  const depts = [...new Set(backlogList.map(b => String(b.dept || "")).filter(Boolean))].sort();
  const dsel = document.getElementById("backlog-depts");
  if (dsel) dsel.innerHTML = depts.map(d => chip(d, backlogDepts.has(d), "toggleBacklogDept('" + esc(d).replace(/'/g, "&#39;") + "')", d)).join("");
  const sort = document.getElementById("backlog-sort");
  if (sort) sort.innerHTML = BACKLOG_SORTS.map(([k, label]) => {
    const on = backlogSort && backlogSort.key === k;
    return `<button type="button" class="${on ? "on" : ""}" data-sort="${k}" onclick="backlogClickHeader('${k}')">${esc(label)}${on ? (backlogSort.dir === 1 ? " ↑" : " ↓") : ""}</button>`;
  }).join("");
  const grp = document.getElementById("backlog-group");
  if (grp) grp.innerHTML = BACKLOG_GROUPS.map(([k, label]) => `<button type="button" class="${backlogGroup === k ? "on" : ""}" data-group="${k}" onclick="setBacklogGroup('${k}')">${esc(label)}</button>`).join("");
  pillCounts("backlog-show", Object.assign({ "": backlogVisible(backlogList, "show").length }, count("show", b => String(b.status || ""), BACKLOG_STATUSES)));
  pillCounts("backlog-prios", count("prio", backlogPrio, BACKLOG_PRIOS));
  pillCounts("backlog-depts", count("dept", b => String(b.dept || ""), depts));
}

function backlogGroupName(b) {
  if (backlogGroup === "status") return String(b.status || "none");
  if (backlogGroup === "priority") return backlogPrio(b);
  return String(b.dept || "none");
}
function backlogGroupOrder(a, b) {
  if (backlogGroup === "status") {
    const at = BACKLOG_STATUSES.indexOf(a), bt = BACKLOG_STATUSES.indexOf(b);
    return (at < 0 ? 99 : at) - (bt < 0 ? 99 : bt) || a.localeCompare(b);
  }
  if (backlogGroup === "priority") return BACKLOG_PRIOS.indexOf(a) - BACKLOG_PRIOS.indexOf(b);
  return a.localeCompare(b);
}

function backlogSummary(n) {
  const what = backlogShow.size ? BACKLOG_STATUSES.filter(s => backlogShow.has(s)).join(" + ") : "all";
  const s = backlogSort ? backlogSort.key + (backlogSort.dir === 1 ? " ↑" : " ↓") : "default order";
  const g = (BACKLOG_GROUPS.find(x => x[0] === backlogGroup) || BACKLOG_GROUPS[0])[1];
  return [what + " " + n, s, g].join(" · ");
}

function paintBacklog() {
  const items = document.getElementById("backlog-items");
  if (!items) return;
  if (backlogList.length) {
    const have = new Set(backlogList.map(b => String(b.dept || "")));
    [...backlogDepts].forEach(d => { if (!have.has(d)) backlogDepts.delete(d); });
  }
  paintBacklogChips();
  const list = backlogSorted(backlogVisible(backlogList));
  fdStatus("backlog", { summary: backlogSummary(list.length), match: list.length === backlogList.length ? "" : list.length + " of " + backlogList.length });
  if (!list.length) { items.innerHTML = '<p class="pane-lead">No items.</p>'; return; }
  let body;
  if (!backlogGroup) body = list.map(backlogItemRow).join("");
  else {
    const by = new Map();
    list.forEach(b => { const n = backlogGroupName(b); if (!by.has(n)) by.set(n, []); by.get(n).push(b); });
    body = [...by.keys()].sort(backlogGroupOrder).map(n =>
      `<div class="bl-group" data-group="${esc(n)}"><span class="gname">${esc(n)}</span> <span class="gn">${by.get(n).length}</span></div>` +
      by.get(n).map(backlogItemRow).join("")).join("");
  }
  items.innerHTML = backlogHeadRow() + body;
}

async function backlogSetPriority(id, priority) {
  const note = document.getElementById("backlog-note");
  if (note) note.textContent = "";
  try {
    const fetcher = typeof plainFetch === "function" ? plainFetch : window.fetch.bind(window);
    const res = await fetcher("/_hub/backlog/" + encodeURIComponent(id), {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ priority: priority === "unset" ? "" : priority, by: "board" })
    });
    if (!res.ok) {
      let why = res.status + "";
      try { why = (await res.json()).error || why; } catch (e) { /* the hub's sentence is a nicety */ }
      if (note) note.textContent = "Could not change " + id + ": " + why;
    }
  } catch (e) {
    if (note) note.textContent = "Could not change " + id + ": the hub is not answering.";
  }
  loadBacklog();
}
function backlogReportRow(r) {
  const when = r.at ? new Date(r.at) : null;
  const stamp = when && !isNaN(when) ? when.toLocaleString() : "";
  const state = r.read_at ? "read" : "unread";
  const from = (r.from_by || "") + (r.from_room ? " on " + r.from_room : "");
  return '<details class="aud-row" data-id="' + esc(r.id || "") + '"><summary>' +
    '<span class="aud-when">' + esc(stamp) + " " + state + "</span> " +
    '<span class="aud-room">' + esc(r.to_dept || "orchestrator") + "</span> " +
    '<span class="aud-kind">' + esc(from) + "</span> " +
    '<span class="aud-detail">' + esc(r.subject || "") + "</span></summary>" +
    '<pre class="aud-detail">' + esc(r.body || "") + "</pre></details>";
}

async function loadBacklog() {
  const items = document.getElementById("backlog-items");
  const reps = document.getElementById("backlog-reports");
  if (!items || !reps) return;
  try {
    const body = await backlogGet("/_hub/backlog?bodies=1");
    const list = (body && body.items) || [];
    backlogList = list;
    paintBacklog();
  } catch (e) {
    items.innerHTML = '<p class="pane-lead">The backlog is not answering right now.</p>';
  }
  try {
    const body = await backlogGet("/_hub/reports?limit=100");
    const list = (body && body.reports) || [];
    reps.innerHTML = list.length ? list.map(backlogReportRow).join("") : '<p class="pane-lead">No reports.</p>';
  } catch (e) {
    reps.innerHTML = '<p class="pane-lead">The reports are not answering right now.</p>';
  }
}

function onBacklogEvent() {
  const view = document.getElementById("backlog");
  if (view && !view.hidden) loadBacklog();
}

document.addEventListener("DOMContentLoaded", () => {
  const chips = document.getElementById("backlog-prios");
  if (chips) chips.addEventListener("click", ev => {
    const b = ev.target.closest("button[data-prio]");
    if (!b) return;
    const p = b.dataset.prio;
    toggleBacklogSet(backlogPrios, BACKLOG_PRIO_KEY, p);
  });
  const items = document.getElementById("backlog-items");
  if (items) {
    items.addEventListener("click", ev => {
      const h = ev.target.closest("button[data-sort]");
      if (h) backlogClickHeader(h.dataset.sort);
    });
    items.addEventListener("change", ev => {
      const s = ev.target.closest("select.bl-prio");
      if (s) backlogSetPriority(s.dataset.id, s.value);
    });
  }
});
