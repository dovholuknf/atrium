// The pulls view: the PRs atrium is reviewing, one row each, newest first.
//
// docs/rnd/pulls-view-design.md section 4, over the API in docs/rnd/pulls-api.md (/v1/prs). A row is a fact about a
// run, written by the daemon. This file holds the rows it was told about and draws them, and decides nothing.
//
// LIVE THE WAY THE REST OF THE BOARD IS: the rows are read once at load and again when the event stream reopens,
// and a `pr` event carries the whole row, which replaces the one held. No timer. ONE RENDER FUNCTION draws a row
// whether it came from the list answer or from an event (`pullRowHTML`).
//
// THE NAV COUNT is the rows waiting on the operator, ready plus failed and not archived, recounted from the held
// rows on every change, so an event moves it without a read. It rides the alerting path a question does
// (`alerting.check` in notify.js, kind "pulls") and the window title (`retitleAgain`).

const pulls = {
  rows: [],          // every row held, newest first
  loaded: false,
  loading: false,
  note: "",          // a sentence under the header: a refusal, or the halted word
  open: "",          // the row whose findings are showing
  findings: {},      // row id -> the findings answer, for the open row
  logs: {},          // row id -> run_log text, for a failed row whose log was opened
  filterState: "open",
  filterRepo: ""
};
let pullsNavN = 0;

const pullsLive = r => r.state === "ready" || r.state === "failed";
const pullsWaiting = r => !r.archived_at && pullsLive(r);

function pullsSort() {
  pulls.rows.sort((a, b) => (a.created_at < b.created_at ? 1 : a.created_at > b.created_at ? -1 : a.id < b.id ? 1 : -1));
}

// The one place a row enters. The list answer and the event both come here.
function pullsApplyRow(row) {
  if (!row || !row.id) return;
  const i = pulls.rows.findIndex(r => r.id === row.id);
  if (i >= 0) pulls.rows[i] = row; else pulls.rows.push(row);
  pullsSort();
}

function pullsSetRows(rows) {
  pulls.rows = (rows || []).slice();
  pullsSort();
}

// Held rows to the nav item, the title and the alerting path.
function pullsRecount() {
  pullsNavN = pulls.rows.filter(pullsWaiting).length;
  if (typeof badge === "function" && document.getElementById("c-pulls")) badge("c-pulls", pullsNavN);
  if (typeof retitleAgain === "function") retitleAgain();
  if (typeof alerting !== "undefined" && pulls.loaded) {
    alerting.check("pulls", pulls.rows.filter(pullsWaiting).map(r => ({
      id: r.id + "#" + r.state + "#" + (r.ready_at || r.run_error || ""),
      task_id: r.id,
      display_title: r.org_repo + " #" + r.number,
      row: r
    })), i => ({
      title: i.row.state === "failed"
        ? `${i.display_title} review failed`
        : `${i.display_title} is ready to walk`,
      body: i.row.state === "failed" ? i.row.run_error : (i.row.title || "")
    }));
  }
}

function pullsChanged() {
  pullsRecount();
  pullsPaint();
}

function pullsSay(text) {
  pulls.note = text || "";
  const n = document.getElementById("pulls-note");
  if (!n) return;
  n.textContent = pulls.note;
  n.hidden = !pulls.note;
}

// An error from api() to a sentence. A halted store answers with the halted body, whose `error` says so.
function pullsErr(e) {
  return (e && e.message) || "that did not work";
}

// The tab stays hidden until the index answers 200 with a prs array: a daemon with no /v1/prs (an older build, a hub
// that does not proxy it) gets no tab. A read asked for while one is in flight runs again when it ends.
async function loadPulls() {
  if (pulls.loading) { pulls.again = true; return; }
  pulls.loading = true;
  try {
    const all = pulls.filterState === "all";
    const body = await api("/v1/prs" + (all ? "?archived=1" : ""));
    if (!body || !Array.isArray(body.prs)) throw new Error("no pulls here");
    pullsSetRows(body.prs);
    pulls.loaded = true;
    const tab = document.querySelector('.tab[data-view="pulls"]');
    if (tab) tab.hidden = false;
    // Only a note this read itself raised is cleared: a refusal that asked for this read stays on screen.
    if (pulls.loadFailed) pullsSay("");
    pulls.loadFailed = false;
    // nav_count is what the daemon counted. The held rows say the same, and say it again after every event.
    pullsChanged();
  } catch (e) {
    pulls.loadFailed = true;
    pullsSay(pullsErr(e));
    pullsPaint();
  } finally {
    pulls.loading = false;
    if (pulls.again) { pulls.again = false; loadPulls(); }
  }
}

// The `pr` event: one whole row, which replaces the one held.
function onPrEvent(ev) {
  let d;
  try { d = JSON.parse(ev.data); } catch (e) { return; }
  if (!d || !d.pr) return;
  pullsApplyRow(d.pr);
  pullsChanged();
}

// The stream opened: events may have been missed while it was down, so read the index again.
function onPullsStreamOpen() { loadPulls(); }

// ── the words ───────────────────────────────────────────

const PULL_SEVS = [["high", "high"], ["med", "med"], ["low", "low"], ["nit", "nit"]];

function pullFindingTotal(r) {
  const f = r.findings || {};
  return (f.high || 0) + (f.med || 0) + (f.low || 0) + (f.nit || 0);
}

// What the row says its state is. `running` is drawn from run_state: the API has no N of M.
function pullStateWords(r) {
  const w = r.walk || {};
  switch (r.state) {
    case "queued": return "queued";
    case "fetching": return "fetching";
    case "running": return "reviewing" + (r.run_state ? " " + r.run_state : "");
    case "failed": return "failed" + (r.run_error ? ": " + r.run_error : "");
    case "aborted": return "aborted";
    case "ready": {
      const total = pullFindingTotal(r);
      const seen = (w.done || 0) + (w.skipped || 0) + (w.deferred || 0);
      if (total > 0 && !w.open && !w.deferred) return "walked";
      if (seen > 0) return "walking " + seen + " of " + total;
      return "ready to walk";
    }
  }
  return r.state || "";
}

function pullCounts(r) {
  const f = r.findings || {};
  const parts = PULL_SEVS.filter(s => f[s[0]]).map(s => f[s[0]] + " " + s[1]);
  if (f.leak) parts.push(f.leak + (f.leak === 1 ? " leak" : " leaks"));
  return parts.join(" · ");
}

function pullSecond(r) {
  const s = r.second || {};
  if (s.state === "done") return "2nd: " + (s.summary || "done");
  if (s.state === "pending") return "2nd: pending";
  if (s.state === "failed") return "2nd: failed" + (s.error ? ": " + s.error : "");
  return "";
}

function pullAgo(iso) {
  const t = Date.parse(iso || "");
  if (isNaN(t)) return "";
  return ago(Math.max(0, (Date.now() - t) / 1000)) + " ago";
}

function pullBtn(act, id, label, tip) {
  return '<button type="button" class="pull-btn" data-act="' + act + '" data-id="' + esc(id) + '"' +
    (tip ? ' data-tip="' + esc(tip) + '"' : "") + ">" + esc(label) + "</button>";
}

// One row. The same markup for a list answer and an event.
function pullRowHTML(r) {
  const title = r.title || r.url || "";
  const cost = r.cost_usd ? "$" + Number(r.cost_usd).toFixed(2) + (r.state === "running" || r.state === "fetching" ? " so far" : "") : "";
  const when = pullAgo(r.state === "ready" && r.ready_at ? r.ready_at : r.created_at);
  const counts = r.state === "ready" ? pullCounts(r) : "";
  const second = pullSecond(r);
  const acts = [];
  if (r.state === "queued") acts.push(pullBtn("start", r.id, "review", "start the review of this PR"));
  if (r.state === "queued" || r.state === "fetching" || r.state === "running") acts.push(pullBtn("abort", r.id, "abort"));
  if (r.state === "failed" || r.state === "aborted") acts.push(pullBtn("retry", r.id, "retry"));
  if (r.state === "failed") acts.push(pullBtn("log", r.id, "log", "the tail of run.log"));
  if (r.state === "ready") {
    acts.push(pullBtn("open", r.id, pulls.open === r.id ? "close" : "walk", "the findings of this review"));
    acts.push(pullBtn("walker", r.id, r.walker_task ? "walker" : "launch walker",
      r.walker_task ? "a walker is on this review: " + r.walker_task : "start a session to talk the findings through"));
  }
  const open = pulls.open === r.id && r.state === "ready";
  return '<div class="pull" data-id="' + esc(r.id) + '" data-state="' + esc(r.state) + '">' +
    '<div class="pull-main">' +
      '<div class="pull-who"><span class="pull-repo">' + esc(r.org_repo) + " #" + esc(r.number) + "</span>" +
        '<span class="pull-head7">' + esc(r.head7 || "") + "</span>" +
        (r.author ? '<span class="pull-author">' + esc(r.author) + "</span>" : "") + "</div>" +
      '<div class="pull-title">' + esc(title) + "</div>" +
      (r.why ? '<div class="pull-why">' + esc(r.why) + "</div>" : "") +
      (counts ? '<div class="pull-counts">' + esc(counts) + "</div>" : "") +
      (second ? '<div class="pull-second">' + esc(second) + "</div>" : "") +
    "</div>" +
    '<div class="pull-side">' +
      '<span class="pull-state">' + esc(pullStateWords(r)) + "</span>" +
      '<span class="pull-meta">' + esc([cost, when].filter(Boolean).join(" · ")) + "</span>" +
      '<span class="pull-acts">' + acts.join("") + "</span>" +
    "</div>" +
    (pulls.logs[r.id] != null && r.state === "failed" ? '<pre class="pull-log">' + esc(pulls.logs[r.id]) + "</pre>" : "") +
    (open ? pullFindingsHTML(r) : "") +
  "</div>";
}

function pullFindingsHTML(r) {
  const body = pulls.findings[r.id];
  if (!body) return '<div class="pull-findings"><span class="pull-empty">reading the findings</span></div>';
  if (body.error) return '<div class="pull-findings"><span class="pull-empty">' + esc(body.error) + "</span></div>";
  if (!body.findings.length) return '<div class="pull-findings"><span class="pull-empty">no findings</span></div>';
  return '<div class="pull-findings">' + body.findings.map(f => {
    const st = (f.walk && f.walk.state) || "open";
    const b = (state, label) => '<button type="button" class="pull-btn' + (st === state ? " on" : "") +
      '" data-act="mark" data-id="' + esc(r.id) + '" data-key="' + esc(f.key) + '" data-state="' + state + '">' + label + "</button>";
    return '<div class="pull-finding" data-key="' + esc(f.key) + '" data-walk="' + esc(st) + '">' +
      '<span class="pull-sev sev-' + esc(f.sev) + '">' + esc(f.sev) + "</span>" +
      '<span class="pull-loc">' + esc(f.path) + (f.line ? ":" + esc(f.line) : "") + "</span>" +
      (f.leak ? '<span class="pull-leak">leak</span>' : "") +
      '<span class="pull-code">' + esc(f.code) + "</span>" +
      '<span class="pull-marks">' + b("done", "done") + b("skipped", "skip") + b("deferred", "defer") + b("open", "open") + "</span>" +
    "</div>";
  }).join("") + "</div>";
}

// ── drawing ─────────────────────────────────────────────

function pullsShown() {
  return pulls.rows.filter(r => !pulls.filterRepo || r.org_repo === pulls.filterRepo);
}

function pullsPaintFilters() {
  const sel = document.getElementById("pulls-repo");
  if (!sel) return;
  const repos = [...new Set(pulls.rows.map(r => r.org_repo))].sort();
  if (pulls.filterRepo && !repos.includes(pulls.filterRepo)) repos.push(pulls.filterRepo);
  // Only when the set changes, so an open select is not closed by every event.
  const key = repos.join("|");
  if (sel.dataset.repos !== key) {
    sel.dataset.repos = key;
    sel.innerHTML = '<option value="">all repos</option>' +
    repos.map(n => '<option value="' + esc(n) + '"' + (n === pulls.filterRepo ? " selected" : "") + ">" + esc(n) + "</option>").join("");
  }
  sel.value = pulls.filterRepo;
  const st = document.getElementById("pulls-state");
  if (st) st.value = pulls.filterState;
}

function pullsPaint() {
  const list = document.getElementById("pulls-list");
  if (!list) return;
  pullsPaintFilters();
  const rows = pullsShown();
  if (!rows.length) {
    list.innerHTML = '<p class="pane-lead">' + (pulls.loaded
      ? "No reviews yet. Paste a PR above and atrium will review it."
      : "Reading the reviews.") + "</p>";
    return;
  }
  const anchor = typeof scrollAnchor === "function" ? scrollAnchor(list, ".pull") : null;
  list.innerHTML = rows.map(pullRowHTML).join("");
  if (anchor && typeof restoreScrollAnchor === "function") restoreScrollAnchor(list, ".pull", anchor);
}

// ── acting ──────────────────────────────────────────────

async function pullsPost(path, body) {
  return api(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body || {}) });
}

// start, retry and abort answer {pr: <row>}. A refusal says why, and the row is read again, since a 409 means
// the state moved under the click.
async function pullsRowAction(id, verb) {
  try {
    const out = await pullsPost("/v1/prs/" + encodeURIComponent(id) + "/" + verb);
    if (out && out.pr) pullsApplyRow(out.pr);
    pullsSay("");
    pullsChanged();
  } catch (e) {
    pullsSay(pullsErr(e));
    loadPulls();
  }
}

async function pullsPaste() {
  const url = document.getElementById("pulls-url");
  const why = document.getElementById("pulls-why");
  const u = url ? url.value.trim() : "";
  if (!u) { pullsSay("paste the URL of a pull request"); return; }
  try {
    const out = await pullsPost("/v1/prs", { url: u, why: why ? why.value.trim() : "" });
    if (out && out.pr) pullsApplyRow(out.pr);
    pullsSay(out && out.created === false ? "that PR already has a review at that head" : "");
    if (url) url.value = "";
    if (why) why.value = "";
    pullsChanged();
  } catch (e) {
    pullsSay(pullsErr(e));
  }
}

async function pullsToggleFindings(id) {
  if (pulls.open === id) { pulls.open = ""; pullsPaint(); return; }
  pulls.open = id;
  delete pulls.findings[id];
  pullsPaint();
  try {
    pulls.findings[id] = await api("/v1/prs/" + encodeURIComponent(id) + "/findings");
    if (pulls.findings[id].pr) pullsApplyRow(pulls.findings[id].pr);
  } catch (e) {
    pulls.findings[id] = { error: pullsErr(e) };
  }
  pullsChanged();
}

async function pullsMark(id, key, state) {
  try {
    const out = await pullsPost("/v1/prs/" + encodeURIComponent(id) + "/findings/" + encodeURIComponent(key) + "/walk", { state });
    const f = ((pulls.findings[id] || {}).findings || []).find(x => x.key === key);
    if (f && out && out.walk) f.walk = out.walk;
    const row = pulls.rows.find(r => r.id === id);
    if (row && out && out.counts) row.walk = out.counts;
    pullsSay("");
  } catch (e) {
    pullsSay(pullsErr(e));
  }
  pullsChanged();
}

async function pullsWalker(id) {
  try {
    const out = await pullsPost("/v1/prs/" + encodeURIComponent(id) + "/walker", { action: "launch" });
    if (out && out.pr) pullsApplyRow(out.pr);
    pullsSay("");
    pullsChanged();
  } catch (e) {
    pullsSay(pullsErr(e));
  }
}

async function pullsLog(id) {
  if (pulls.logs[id] != null) { delete pulls.logs[id]; pullsPaint(); return; }
  try {
    const out = await api("/v1/prs/" + encodeURIComponent(id));
    pulls.logs[id] = (out && out.run_log) || "(no log)";
  } catch (e) {
    pulls.logs[id] = pullsErr(e);
  }
  pullsPaint();
}

document.addEventListener("DOMContentLoaded", () => {
  const list = document.getElementById("pulls-list");
  if (list) list.addEventListener("click", e => {
    const b = e.target.closest("button[data-act]");
    if (!b) return;
    const id = b.dataset.id;
    switch (b.dataset.act) {
      case "start": pullsRowAction(id, "start"); break;
      case "retry": pullsRowAction(id, "retry"); break;
      case "abort": pullsRowAction(id, "abort"); break;
      case "open": pullsToggleFindings(id); break;
      case "mark": pullsMark(id, b.dataset.key, b.dataset.state); break;
      case "walker": pullsWalker(id); break;
      case "log": pullsLog(id); break;
    }
  });
  const go = document.getElementById("pulls-paste");
  if (go) go.addEventListener("click", pullsPaste);
  const url = document.getElementById("pulls-url");
  if (url) url.addEventListener("keydown", e => { if (e.key === "Enter") { e.preventDefault(); pullsPaste(); } });
  const st = document.getElementById("pulls-state");
  if (st) st.addEventListener("change", () => { pulls.filterState = st.value; loadPulls(); });
  const repo = document.getElementById("pulls-repo");
  if (repo) repo.addEventListener("change", () => { pulls.filterRepo = repo.value; pullsPaint(); });
});
