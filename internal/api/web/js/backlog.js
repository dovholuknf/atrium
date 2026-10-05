// The backlog pane: the items filed on the hub and the reports directors left, read-only, newest first.
//
// The hub holds both so a director on any room sees what another room filed. See docs/rnd/reports-channel-design.md.
// Filing, changing a status and adding a report are `atrium backlog` and `atrium reports`, and the tools of the same
// names. This pane only reads.
//
// HUB-ONLY. `/_hub/backlog` is a 404 on a plain daemon, so the tab shows once the hub probe in rooms.js has found a
// hub, through `paintBacklogTab`. It is live by the `backlog` and `report` events, and fetched on open.

let backlogDept = "";
let backlogOpenOnly = true;

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

function backlogItemRow(b) {
  return '<div class="aud-row" data-id="' + esc(b.id || "") + '">' +
    '<span class="aud-when">' + esc(b.status || "") + "</span>" +
    '<span class="aud-room">' + esc(b.dept || "") + "</span>" +
    '<span class="aud-kind">' + esc(b.id || "") + (b.priority ? " " + esc(b.priority) : "") + "</span>" +
    '<span class="aud-detail">' + esc(b.title || "") + "</span>" +
    "</div>";
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
    items.innerHTML = list.length ? list.map(backlogItemRow).join("") : '<p class="pane-lead">No items.</p>';
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
});
