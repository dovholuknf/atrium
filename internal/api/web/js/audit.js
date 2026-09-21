// The audit pane: a cross-cutting operational feed, newest first, read-only.
//
// It answers "what happened to atrium", which nothing else did: a room
// attaching, the hub starting, a launch refused by the cap, the board put on a
// share. Those were scattered across logs and the per-card stream. See
// docs/audit-design.md.
//
// HUB-ONLY. `/_hub/audit` is a 404 on a plain daemon, so the tab reveals itself
// only once the hub probe in rooms.js has found a hub, the same way the room
// counter does. `paintAuditTab` is called from there.
//
// LIVE THE WAY THE REST OF THE BOARD IS: it fetches on open, again on stream
// reconnect, and again on an `audit` delta. A missed delta costs one re-fetch,
// which is the recovery the whole event stream already rests on.

// The kinds the hub records, for the filter. A fixed list rather than one built
// from the current page, so filtering by a kind does not collapse the choice of
// kinds. Unknown kinds still show in the list, they are just not offered here.
const AUDIT_KINDS = [
  "hub-started", "room-attached", "room-detached", "room-going-down",
  "launch-refused", "board-share-opened",
  "session-start", "session-finish", "session-exit",
  "permission-requested", "permission-decided",
  "added", "removed",
  "marked-for-deletion", "unmarked", "joined", "secret-minted",
  "announced", "clear", "not-clear"
];

let auditRoomFilter = "";
let auditKindFilter = "";

// paintAuditTab shows the tab only on a hub. Called from rooms.js once the probe
// has answered.
function paintAuditTab() {
  const tab = document.querySelector('.tab[data-view="audit"]');
  if (!tab) return;
  tab.hidden = typeof hubIsHub === "undefined" ? true : !hubIsHub;
}

// auditQuery builds the request from the current filters.
function auditQuery() {
  const p = new URLSearchParams();
  p.set("limit", "200");
  if (auditRoomFilter) p.set("room", auditRoomFilter);
  if (auditKindFilter) p.set("kind", auditKindFilter);
  return "/_hub/audit?" + p.toString();
}

// loadAudit fetches the feed and draws it. Fail-open: a hub mid-restart or a
// store that answered an error draws a line saying so rather than a blank pane.
async function loadAudit() {
  const list = document.getElementById("audit-list");
  if (!list) return;
  const fetcher = typeof plainFetch === "function" ? plainFetch : window.fetch.bind(window);
  let events = [];
  try {
    const res = await fetcher(auditQuery());
    if (!res.ok) throw new Error("audit unavailable");
    const body = await res.json();
    events = (body && body.events) || [];
  } catch (e) {
    list.innerHTML = '<p class="pane-lead">The audit feed is not answering right now. ' +
      'It will fill in when the hub is back.</p>';
    return;
  }
  drawAuditFilters();
  if (!events.length) {
    list.innerHTML = '<p class="pane-lead">Nothing recorded yet.</p>';
    return;
  }
  list.innerHTML = events.map(auditRow).join("");
}

// auditRow is one line: when, which room, what kind, and the detail.
function auditRow(e) {
  const when = e.at ? new Date(e.at) : null;
  const stamp = when && !isNaN(when) ? when.toLocaleString() : "";
  const room = e.room
    ? '<span class="aud-room">' + esc(e.room) + "</span>"
    : '<span class="aud-room aud-hub">hub</span>';
  return '<div class="aud-row">' +
    '<span class="aud-when">' + esc(stamp) + "</span>" +
    room +
    '<span class="aud-kind">' + esc(e.kind || "") + "</span>" +
    '<span class="aud-detail">' + esc(e.detail || "") + "</span>" +
    "</div>";
}

// drawAuditFilters keeps the two selects in step with what exists: the rooms
// this hub knows about, and the fixed kind list.
function drawAuditFilters() {
  const roomSel = document.getElementById("audit-room");
  const kindSel = document.getElementById("audit-kind");
  if (roomSel) {
    const rooms = (typeof hubRooms !== "undefined" ? hubRooms : []).map(r => r.name);
    const opts = ['<option value="">all rooms</option>']
      .concat(rooms.map(n => '<option value="' + esc(n) + '"' +
        (n === auditRoomFilter ? " selected" : "") + ">" + esc(n) + "</option>"));
    roomSel.innerHTML = opts.join("");
  }
  if (kindSel && !kindSel.dataset.built) {
    const opts = ['<option value="">all kinds</option>']
      .concat(AUDIT_KINDS.map(k => '<option value="' + esc(k) + '">' + esc(k) + "</option>"));
    kindSel.innerHTML = opts.join("");
    kindSel.dataset.built = "1";
  }
  if (kindSel) kindSel.value = auditKindFilter;
}

// onAuditEvent is the delta handler: something was recorded, so re-fetch, but
// only while the pane is open. A closed pane pays nothing.
function onAuditEvent() {
  const view = document.getElementById("audit");
  if (view && !view.hidden) loadAudit();
}

document.addEventListener("DOMContentLoaded", () => {
  const roomSel = document.getElementById("audit-room");
  const kindSel = document.getElementById("audit-kind");
  if (roomSel) roomSel.addEventListener("change", () => { auditRoomFilter = roomSel.value; loadAudit(); });
  if (kindSel) kindSel.addEventListener("change", () => { auditKindFilter = kindSel.value; loadAudit(); });
});
