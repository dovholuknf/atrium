// ── emerging features ────────────────────────────────────
// One per-browser switch for each area that is not finished. Off by default, except for a browser that had the old single
// "unfinished tabs" switch on: its choice stands in for every area until that area is set. Loaded by the board and the phone page.
const EMERGING_KEY = "atrium.emerging";
const FLAGS_KEY = "atrium.flags";
const EMERGING_AREAS = ["board", "stack", "history", "audit", "items", "pulls", "hubrepos", "backlog", "hubdocs"];
const EMERGING_VIEWS = ["board", "stack", "history", "audit", "items", "pulls", "hubrepos", "backlog"];
const EMERGING_PLAIN_TABS = ["board", "stack", "history"];

function emergingLegacy() {
  try { return localStorage.getItem(EMERGING_KEY) === "1"; } catch (e) { return false; }
}

function areaOn(name) {
  const v = flagsRead()[name];
  return typeof v === "boolean" ? v : emergingLegacy();
}

function emergingOn() {
  return EMERGING_AREAS.some(areaOn);
}

function emergingTabs() {
  EMERGING_PLAIN_TABS.forEach(v => { const t = document.querySelector('.tab[data-view="' + v + '"]'); if (t) t.hidden = !areaOn(v); });
}

function emergingApply() {
  document.documentElement.toggleAttribute("data-emerging", emergingOn());
  document.querySelectorAll("input[data-flag]").forEach(b => { b.checked = flagOn(b.dataset.flag); });
  emergingTabs();
  if (typeof paintAuditTab === "function") paintAuditTab();
  if (typeof paintItemsTab === "function") paintItemsTab();
  if (typeof paintHubReposTab === "function") paintHubReposTab();
  if (typeof paintBacklogTab === "function") paintBacklogTab();
  const pt = document.querySelector('.tab[data-view="pulls"]');
  const on = areaOn("pulls");
  if (pt) {
    pt.hidden = !(on && typeof pulls !== "undefined" && pulls.loaded);
    if (on && typeof loadPulls === "function") loadPulls();
  }
  window.dispatchEvent(new Event("emerging"));
  const cur = document.querySelector(".tab.on");
  if (cur && (cur.hidden || viewOff(cur.dataset.view)) && typeof switchView === "function") switchView("terms");
}

function emergingSet(on) {
  const f = flagsRead();
  EMERGING_AREAS.forEach(a => { f[a] = !!on; });
  try { localStorage.setItem(EMERGING_KEY, on ? "1" : "0"); localStorage.setItem(FLAGS_KEY, JSON.stringify(f)); } catch (e) {}
  flagsApply();
}

document.documentElement.toggleAttribute("data-emerging", emergingOn());
emergingTabs();
prefLive(EMERGING_KEY, emergingApply);

// Per-browser on/off switches for finished parts of the board, and the review (walk), which is off until it is finished.
const FLAGS = ["walk", "queue", "chat", "usage", "chatter", "pathlinks", "mic", "popout", "lastread", "artifacts", "answers",
  "replies", "seen", "switcher", "paste", "attention", "micin", "headsup"];
const FLAGS_DEFAULT_OFF = ["walk", "attention", "micin", "headsup"];
const FLAG_VIEWS = { queue: "queue", chat: "chat", usage: "usage", chatter: "chatter" };

function flagsRead() {
  try { return JSON.parse(localStorage.getItem(FLAGS_KEY) || "{}") || {}; } catch (e) { return {}; }
}

function flagOn(name) {
  if (EMERGING_AREAS.includes(name)) return areaOn(name);
  const v = flagsRead()[name];
  return typeof v === "boolean" ? v : !FLAGS_DEFAULT_OFF.includes(name);
}

function viewOff(name) {
  return (EMERGING_VIEWS.includes(name) && !areaOn(name)) || (!!FLAG_VIEWS[name] && !flagOn(FLAG_VIEWS[name]))
    || (name === "queue" && flagOn("attention"));
}

function flagsMark() {
  document.documentElement.setAttribute("data-off", FLAGS.filter(f => !flagOn(f)).join(" "));
  Object.keys(FLAG_VIEWS).forEach(v => { const t = document.querySelector('.tab[data-view="' + v + '"]'); if (t) t.hidden = viewOff(v); });
  document.documentElement.toggleAttribute("data-emerging", emergingOn());
}

function flagsApply() {
  emergingApply();
  flagsMark();
  document.querySelectorAll("input[data-flag]").forEach(b => { b.checked = flagOn(b.dataset.flag); });
  window.dispatchEvent(new Event("emerging"));
  const cur = document.querySelector(".tab.on");
  if (cur && viewOff(cur.dataset.view) && typeof switchView === "function") switchView("terms");
  if (!flagOn("walk") && typeof dock !== "undefined" && dock.open && typeof walkClose === "function") walkClose();
  if (typeof paintSoon === "function") paintSoon();
}

function flagSet(name, on) {
  const f = flagsRead();
  f[name] = !!on;
  try { localStorage.setItem(FLAGS_KEY, JSON.stringify(f)); } catch (e) {}
  flagsApply();
}

flagsMark();
prefLive(FLAGS_KEY, flagsApply);
