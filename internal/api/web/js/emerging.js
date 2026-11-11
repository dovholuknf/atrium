// ── emerging features ────────────────────────────────────
// One per-browser switch for the areas that are not finished: the items, pulls, repos and backlog tabs and the hub
// documents on the files list. Off by default. Loaded by the board and by the phone page, after prefs-live.js.
const EMERGING_KEY = "atrium.emerging";
const EMERGING_VIEWS = ["items", "pulls", "hubrepos", "backlog"];

function emergingOn() {
  try { return localStorage.getItem(EMERGING_KEY) === "1"; } catch (e) { return false; }
}

function emergingApply() {
  const on = emergingOn();
  document.documentElement.toggleAttribute("data-emerging", on);
  const box = document.getElementById("s-emerging");
  if (box) box.checked = on;
  if (typeof paintItemsTab === "function") paintItemsTab();
  if (typeof paintHubReposTab === "function") paintHubReposTab();
  if (typeof paintBacklogTab === "function") paintBacklogTab();
  const pt = document.querySelector('.tab[data-view="pulls"]');
  if (pt) {
    pt.hidden = !(on && typeof pulls !== "undefined" && pulls.loaded);
    if (on && typeof loadPulls === "function") loadPulls();
  }
  window.dispatchEvent(new Event("emerging"));
  const cur = document.querySelector(".tab.on");
  if (cur && cur.hidden && typeof switchView === "function") switchView("board");
}

function emergingSet(on) {
  try { localStorage.setItem(EMERGING_KEY, on ? "1" : "0"); } catch (e) {}
  emergingApply();
}

document.documentElement.toggleAttribute("data-emerging", emergingOn());
prefLive(EMERGING_KEY, emergingApply);
