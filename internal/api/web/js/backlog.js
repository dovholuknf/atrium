// The backlog pane: the items filed on the hub and the reports directors left. Items sort by a click on a header (and
// back again on a second), filter by priority chip, and take a new priority from the select on a row.
//
// The hub holds both so a director on any room sees what another room filed. See docs/rnd/reports-channel-design.md.
// Filing, changing a status and adding a report are `atrium backlog` and `atrium reports`, and the tools of the same
// names. The board only changes a priority.
//
// HUB-ONLY. `/_hub/backlog` is a 404 on a plain daemon, so the tab shows once the hub probe in rooms.js has found a
// hub, through `paintBacklogTab`. It is live by the `backlog` and `report` events, and fetched on open.
let backlogDept = "";
let backlogOpenOnly = true;
// Priorities shown, as a set. Empty means all of them. `unset` is the items with none.
const backlogPrios = new Set();
// The header the list is sorted by, or null for the default order. `dir` is 1 or -1.
let backlogSort = null;
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

function backlogVisible(list) {
  if (!backlogPrios.size) return list;
  return list.filter(b => backlogPrios.has(backlogPrio(b)));
}

function backlogClickHeader(key) {
  if (backlogSort && backlogSort.key === key) backlogSort = { key, dir: -backlogSort.dir };
  else backlogSort = { key, dir: backlogFirstDir(key) };
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

function paintBacklogChips() {
  const el = document.getElementById("backlog-prios");
  if (!el) return;
  el.innerHTML = BACKLOG_PRIOS.map(p => '<button type="button" data-prio="' + p + '"' +
    (backlogPrios.has(p) ? ' class="on" aria-pressed="true"' : ' aria-pressed="false"') + ">" + p + "</button>").join("");
}

function paintBacklog() {
  const items = document.getElementById("backlog-items");
  if (!items) return;
  paintBacklogChips();
  const list = backlogSorted(backlogVisible(backlogList));
  items.innerHTML = list.length ? backlogHeadRow() + list.map(backlogItemRow).join("") : '<p class="pane-lead">No items.</p>';
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
    const q = new URLSearchParams();
    if (backlogDept) q.set("dept", backlogDept);
    if (backlogOpenOnly) q.set("open", "1");
    const body = await backlogGet("/_hub/backlog?" + q.toString());
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
  const dept = document.getElementById("backlog-dept");
  const open = document.getElementById("backlog-open");
  if (dept) dept.addEventListener("change", () => { backlogDept = dept.value.trim(); loadBacklog(); });
  if (open) open.addEventListener("change", () => { backlogOpenOnly = open.checked; loadBacklog(); });
  const chips = document.getElementById("backlog-prios");
  if (chips) chips.addEventListener("click", ev => {
    const b = ev.target.closest("button[data-prio]");
    if (!b) return;
    const p = b.dataset.prio;
    if (backlogPrios.has(p)) backlogPrios.delete(p); else backlogPrios.add(p);
    paintBacklog();
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
