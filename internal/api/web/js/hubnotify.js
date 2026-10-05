// ── the hub's notify command, and the board's presence (u-030) ───────────────
//
// Two halves of one feature. The hub can run a command when something wants a human (a phone push, say), and it
// holds that back while a board tab is visible, so it has to be told which tabs are. See f-017.
//
// Only on a hub. A board served by a room with no hub gets 404 from every `/_hub/*` and then draws no row and sends
// no more presence. Guests never reach `/_hub/*` at all.

// ── presence ────────────────────────────────────────────────────────────────

// One id per page load, kept in sessionStorage so a reload is the same tab to the hub. The stream carries it as
// `?tab=`, and the hub drops the tab when that stream closes.
let hubTabId = "";
try { hubTabId = sessionStorage.getItem("atrium.tab") || ""; } catch (e) {}
if (!hubTabId) {
  hubTabId = "t" + Math.random().toString(36).slice(2, 10) + Date.now().toString(36);
  try { sessionStorage.setItem("atrium.tab", hubTabId); } catch (e) {}
}
// The hub said 404 once: this page is not on a hub, or the hub is too old. Nothing more is sent this page load.
let presenceOff = false;

function presenceVisible() { return document.visibilityState !== "hidden"; }

function sendPresence(viaBeacon) {
  if (presenceOff || typeof isGuest === "function" && isGuest()) return;
  const body = JSON.stringify({ visible: presenceVisible(), tab: hubTabId });
  // A beacon cannot be read back, so a page that is going away cannot learn of a 404. That is fine: the next
  // page load will.
  if (viaBeacon && navigator.sendBeacon) {
    try {
      // text/plain, which is what keeps a beacon from being a CORS preflight.
      navigator.sendBeacon("/_hub/presence", new Blob([body], { type: "text/plain" }));
      return;
    } catch (e) {}
  }
  plainFetch("/_hub/presence", { method: "POST", headers: { "Content-Type": "application/json" }, body })
    .then(r => { if (r.status === 404) presenceOff = true; })
    .catch(() => {});
}

document.addEventListener("visibilitychange", () => sendPresence(false));
window.addEventListener("pagehide", () => sendPresence(true));
// On load. Deferred one task so that boot.js has asked whether this is a guest, and then waits for the answer: a
// guest cannot reach `/_hub/*` and must not be seen trying.
setTimeout(() => {
  Promise.resolve(typeof guestKnown !== "undefined" ? guestKnown : "").then(() => sendPresence(false));
}, 0);

// ── the gear row ────────────────────────────────────────────────────────────

// Which spelling of the command the field takes: ONE ARGUMENT PER LINE. A single line split on spaces would make
// a path with a space in it, which is the usual case on Windows, impossible to write, and a quoting syntax would
// be a second thing to get wrong on a field that runs a program.
function hnEl(id) { return document.getElementById(id); }

function hnAgo(iso) {
  if (!iso) return "never";
  const t = Date.parse(iso);
  if (isNaN(t)) return String(iso);
  const s = Math.max(0, Math.round((Date.now() - t) / 1000));
  if (s < 60) return s + " seconds ago";
  if (s < 3600) return Math.round(s / 60) + " minutes ago";
  if (s < 86400) return Math.round(s / 3600) + " hours ago";
  return Math.round(s / 86400) + " days ago";
}

function hnPaint(n, keepEdits) {
  // After a test the fields are left alone: the test ran what was SAVED, and repainting would throw away an edit
  // that has not been saved yet.
  if (!keepEdits) {
    hnEl("s-hn-enabled").checked = !!n.enabled;
    hnEl("s-hn-command").value = (n.command || []).join("\n");
  }
  const rows = [];
  rows.push("last ok: " + hnAgo(n.last_ok_at));
  if (n.last_run_at) rows.push("last run: " + hnAgo(n.last_run_at));
  rows.push("last error: " + (n.last_error || "none"));
  rows.push("failures in a row: " + (n.failures || 0));
  if (n.disabled_reason) rows.push("switched off: " + n.disabled_reason);
  rows.push((n.sent || 0) + " sent, " + (n.dropped || 0) + " dropped, " + (n.suppressed || 0) +
    " held back because a board was open");
  rows.push((n.visible_tabs || 0) + " board tabs visible now");
  const box = hnEl("s-hn-status");
  box.textContent = "";
  for (const r of rows) {
    const d = document.createElement("div");
    d.textContent = r;
    box.appendChild(d);
  }
}

function hnArgv() {
  return hnEl("s-hn-command").value.split("\n").map(s => s.replace(/\r$/, "")).filter(s => s.trim() !== "");
}

// Read on open, after save and after test. There is no timer.
async function loadHubNotify(keepEdits) {
  const row = hnEl("s-hn-row");
  if (!row) return;
  if (typeof isGuest === "function" && isGuest()) { row.hidden = true; return; }
  let r;
  try { r = await plainFetch("/_hub/notify"); } catch (e) { return; }
  if (!r.ok) { row.hidden = true; return; }
  let n;
  try { n = await r.json(); } catch (e) { return; }
  row.hidden = false;
  hnPaint(n, keepEdits);
}

async function saveHubNotify() {
  const msg = hnEl("s-hn-msg");
  msg.textContent = "saving";
  try {
    const r = await plainFetch("/_hub/notify", { method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ enabled: hnEl("s-hn-enabled").checked, command: hnArgv() }) });
    if (!r.ok) { msg.textContent = "the hub refused it: " + ((await r.text()) || r.status); return; }
    hnPaint(await r.json());
    msg.textContent = "saved";
  } catch (e) { msg.textContent = "could not reach the hub"; }
}

async function testHubNotify() {
  const out = hnEl("s-hn-test");
  out.hidden = false;
  out.textContent = "running";
  try {
    const r = await plainFetch("/_hub/notify/test", { method: "POST" });
    if (!r.ok) { out.textContent = "the hub refused it: " + ((await r.text()) || r.status); return; }
    const a = await r.json();
    out.textContent = (a.ok ? "ok" : "failed") + ", exit code " + a.exit_code + ", " + a.took_ms + " ms" +
      (a.output ? "\n" + a.output : "");
  } catch (e) { out.textContent = "could not reach the hub"; return; }
  loadHubNotify(true);
}

// ── the growler reminder ladder, per reason ─────────────────────────────────

const GL_HINT = "Off for questions by default: a question alerts once, then waits in the bell. A permission blocks " +
  "its card, so it keeps its reminders. Saved on the hub, so every board follows it.";

async function loadGrowlLadder() {
  const row = hnEl("s-gl-row");
  if (!row) return;
  if (typeof isGuest === "function" && isGuest()) { row.hidden = true; return; }
  let l;
  try {
    const r = await plainFetch("/_hub/growl-ladder");
    if (!r.ok) { row.hidden = true; return; }
    l = await r.json();
  } catch (e) { return; }
  row.hidden = false;
  hnEl("s-gl-permission").checked = !!l.permission;
  hnEl("s-gl-question").checked = !!l.question;
}

async function saveGrowlLadder() {
  const msg = hnEl("s-gl-msg");
  try {
    const r = await plainFetch("/_hub/growl-ladder", { method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ permission: hnEl("s-gl-permission").checked, question: hnEl("s-gl-question").checked }) });
    if (!r.ok) { msg.textContent = "the hub refused it: " + ((await r.text()) || r.status); return; }
    const l = await r.json();
    hnEl("s-gl-permission").checked = !!l.permission;
    hnEl("s-gl-question").checked = !!l.question;
    msg.textContent = "saved. " + GL_HINT;
  } catch (e) { msg.textContent = "could not reach the hub"; }
}
