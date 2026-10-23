// The pulls view: the PRs atrium is reviewing, one row each, newest first.
//
// docs/rnd/pulls-view-design.md section 4, over the API in docs/review/pulls-api.md (/v1/prs). A row is a fact about a
// run, written by the daemon. This file holds the rows it was told about and draws them, and decides nothing.
//
// LIVE THE WAY THE REST OF THE BOARD IS: the rows are read once at load and again when the event stream reopens,
// and a `pr` event carries the whole row, which replaces the one held. The one timer counts up the seconds of the step a
// live run is on, and runs only while one is drawn. ONE RENDER FUNCTION draws a row
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
  logs: {},          // row id -> run_log text, for a failed row whose log was opened
  filterState: "open",
  filterRepo: "",
  filterGroup: "",   // an attention counter held down: needs, ready, progress or failed
  recent: [],        // the last closed rows, read when nothing is open so the empty page is never blank
  allN: null         // every row with the closed ones, known once "all" has been read
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
  // A walker's card that was attached before its row was known gets its drawer now.
  if (typeof termTask !== "undefined" && termTask && (!dock.tenant || !walkTenant.prId) && walkPrOf(termTask.id)) walkProbe(termTask);
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
    if (all) pulls.allN = pulls.rows.length;
    pulls.recent = all || pulls.rows.some(r => !r.archived_at) ? [] : await pullsReadRecent();
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

// The last few closed rows, for the empty page. A failed read is an empty list: it is decoration.
async function pullsReadRecent() {
  try {
    const body = await api("/v1/prs?archived=1");
    return ((body && body.prs) || []).filter(r => r.archived_at)
      .sort((a, b) => (a.archived_at < b.archived_at ? 1 : -1)).slice(0, 3);
  } catch (e) { return []; }
}

// The board's task list moved: a walker card that began waiting, or stopped, moves its row between groups. Redrawn only when
// that changed, so a poll that changes nothing here does not rewrite the list under the pointer.
function pullsTasksMoved() {
  if (!pulls.loaded) return;
  const sig = pulls.rows.filter(r => r.walker_task).map(r => r.id + ":" + pullQuestion(r)).join("|");
  if (sig === pulls.cardSig) return;
  pulls.cardSig = sig;
  pullsPaint();
}

// The `pr` event: one whole row, which replaces the one held.
function onPrEvent(ev) {
  let d;
  try { d = JSON.parse(ev.data); } catch (e) { return; }
  if (!d || !d.pr) return;
  pullsApplyRow(d.pr);
  pullsChanged();
  // The walk drawer is open on this review: the findings may have moved.
  if (typeof walkTenant !== "undefined" && walkTenant.prId === d.pr.id && typeof dockKick === "function") dockKick(dock.taskId);
}

// The stream opened: events may have been missed while it was down, so read the index again.
function onPullsStreamOpen() { loadPulls(); }

// ── the words ───────────────────────────────────────────
// docs/rnd/pulls-redesign.md. A row is drawn from what the daemon sent and, for the card, from the board's own task
// list. The daemon does not write a verdict, a tests total or per-stage status yet (docs/rnd/pulls-overhaul.md phase 1),
// so the board derives them and prefers the daemon's word the day it sends one (`verdict`, `tests`, `stages`).

const PULL_STAGES = ["worktree", "deps", "panel", "review"];
// The word before the colon in a run_error, to the stage it names. The fetch is the worktree.
const PULL_STAGE_OF = { fetch: 0, worktree: 0, deps: 1, panel: 2, review: 3 };
const PULL_GROUPS = [
  ["needs", "Needs you"], ["ready", "Ready to post"], ["progress", "In progress"], ["failed", "Failed"],
  ["closed", "Closed recently"]
];

const pullClosed = r => !!r.archived_at;

// The walker's card in the board's task list, or null. No API call: the list is already held.
function pullCard(r) {
  if (!r.walker_task || typeof lastTasks === "undefined") return null;
  return lastTasks.find(t => t.id === r.walker_task) || null;
}

// What the card is waiting for, as a sentence, or "" when it is not waiting. The question is the card's own.
function pullQuestion(r) {
  const t = pullCard(r);
  if (!t || pullClosed(r)) return "";
  const asked = typeof cardHasOpenQuestion === "function" && cardHasOpenQuestion(t);
  if (t.status !== "needs-permission" && !asked) return "";
  const qs = (t.seen && t.seen.open_questions) || [];
  const q = qs[0];
  const text = typeof q === "string" ? q : (q && (q.text || q.question)) || "";
  if (text) return 'the card asks: "' + text + '"';
  return t.status === "needs-permission" ? "the card is waiting on a permission" : "the card asked a question";
}

function pullFindingTotal(r) {
  const f = r.findings || {};
  return (f.high || 0) + (f.med || 0) + (f.low || 0) + (f.nit || 0);
}

// Reviewed is every finding posted or dismissed. Accepted and deferred are not, and accepted counts as open.
function pullSeen(r) {
  const w = r.walk || {};
  return (w.done || 0) + (w.skipped || 0);
}
const pullReviewed = r => r.state === "ready" && pullFindingTotal(r) > 0 && pullSeen(r) >= pullFindingTotal(r);

// How far a walk has got, as a sentence, or "" when it has not begun or is over. Progress is the strip and the verdict,
// so this is only the line a half-walked review needs under them.
function pullProgress(r) {
  const w = r.walk || {};
  const seen = pullSeen(r);
  if (r.state !== "ready" || pullReviewed(r) || (!seen && !w.accepted && !w.deferred)) return "";
  return seen + " of " + pullFindingTotal(r) + ": " + (w.accepted || 0) + " accepted, " + (w.done || 0) + " posted, " +
    (w.skipped || 0) + " dismissed" + (w.deferred ? ", " + w.deferred + " deferred" : "");
}

// The group a row sits in: what the operator has to do about it.
function pullGroup(r) {
  if (pullClosed(r)) return "closed";
  if (pullQuestion(r)) return "needs";
  if (r.state === "failed" || r.state === "aborted") return "failed";
  if (r.state === "ready" && !pullReviewed(r)) return "ready";
  return "progress";
}

// The verdict: the daemon's when it sent one, otherwise the board's guess, which is drawn dashed so it reads as a guess.
function pullVerdict(r) {
  const v = String(r.verdict || "").toLowerCase();
  if (v === "approve" || v === "changes" || v === "blocked") return { kind: v, interim: false };
  const f = r.findings || {};
  return { kind: f.high ? "blocked" : pullFindingTotal(r) ? "changes" : "approve", interim: true };
}

// The stage a failed run broke at, from the sentence before the first colon of its error.
function pullFailedStage(r) {
  const m = /^\s*([a-z]+)\s*:/i.exec(r.run_error || "");
  const i = m ? PULL_STAGE_OF[m[1].toLowerCase()] : undefined;
  return i === undefined ? 3 : i;
}

// The four dots. "x" done, "r" running, "f" failed, "" not reached.
function pullStages(r) {
  if (Array.isArray(r.stages) && r.stages.length) {
    const by = {};
    r.stages.forEach(s => { by[s.name] = s.state; });
    return PULL_STAGES.map(n => [n, by[n] === "done" ? "x" : by[n] === "running" ? "r" : by[n] === "failed" ? "f" : ""]);
  }
  const at = (n, state) => PULL_STAGES.map((name, i) => [name, i < n ? "x" : i === n ? state : ""]);
  if (r.state === "ready") return PULL_STAGES.map((n, i) => [n, i === 3 && pullQuestion(r) ? "r" : "x"]);
  if (r.state === "fetching") return at(0, "r");
  if (r.state === "running") return at(/synth|review|final|post/.test(r.run_state || "") ? 3 : 2, "r");
  if (r.state === "failed") return at(pullFailedStage(r), "f");
  return PULL_STAGES.map(n => [n, ""]);
}

// The pill beside the title: the verdict once there is one, and until then what the run is.
function pullPill(r) {
  if (pullClosed(r)) return { kind: "closed", text: "closed" };
  if (pullQuestion(r)) return { kind: "waiting", text: "waiting on you" };
  switch (r.state) {
    case "queued": return { kind: "queued", text: "queued" };
    case "fetching": return { kind: "working", text: "fetching" };
    case "running": return { kind: "working", text: "reviewing" };
    case "failed": return { kind: "failed", text: "failed at " + PULL_STAGES[pullFailedStage(r)] };
    case "aborted": return { kind: "queued", text: "aborted" };
    case "ready": {
      const v = pullVerdict(r);
      return { kind: v.kind, text: v.kind, interim: v.interim };
    }
  }
  return { kind: "queued", text: r.state || "" };
}

// The muted line over the strip: a question (orange), a failure (red), a step, or the note a stage left.
function pullNote(r) {
  if (pullClosed(r)) return null;
  const q = pullQuestion(r);
  if (q) return { cls: " q", text: q };
  if (r.state === "failed" && r.run_error) return { cls: " e", text: r.run_error.replace(/^\s*[a-z]+\s*:\s*/i, "") };
  if (r.state === "running" && r.run_state && !r.run_log) return { cls: "", text: r.run_state };
  if (r.stage_note) return { cls: "", text: r.stage_note };
  return null;
}

// The finding chips: blockers red, med amber, low and nit plain, tests green or red.
function pullChips(r) {
  if (r.state !== "ready") return "";
  const f = r.findings || {};
  const chip = (cls, text) => '<span class="' + cls + '">' + esc(text) + "</span>";
  const out = [];
  if (f.high) out.push(chip("h", f.high + (f.high === 1 ? " blocker" : " blockers")));
  if (f.med) out.push(chip("m", f.med + " med"));
  if (f.low) out.push(chip("", f.low + " low"));
  if (f.nit) out.push(chip("", f.nit + " nit"));
  if (f.leak) out.push(chip("h", f.leak + (f.leak === 1 ? " leak" : " leaks")));
  const t = r.tests;
  if (t && t.ran) out.push(chip("t " + (t.failed ? "bad" : "ok"), "tests " + t.ran + " ran, " + (t.failed ? t.failed + " failed" : t.passed + " passed")));
  return out.join("");
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

// The ONE button on a row, by what the operator does next, or null. The colour follows the state.
function pullPrimary(r) {
  if (pullClosed(r)) return null;
  if (pullQuestion(r)) return { act: "open", label: "Answer", tone: "ask", tip: "open the card with its question in view" };
  switch (r.state) {
    case "queued": return { act: "start", label: "Start review", tone: "mute", tip: "start the review of this PR" };
    case "fetching":
    case "running":
      return r.walker_task ? { act: "open", label: "Open card", tone: "blue", tip: "open the walker's terminal" } : null;
    case "failed":
    case "aborted": return { act: "retry", label: "Retry", tone: "mute", tip: "run the review again" };
    case "ready":
      if (pullReviewed(r) && r.walker_task) return { act: "open", label: "Open card", tone: "blue", tip: "open the walker's terminal" };
      return { act: "walk", label: pullSeen(r) > 0 ? "Continue review" : "Post review", tone: "",
        tip: r.walker_task ? "open the walker's terminal with the findings beside it" : "start a walker session and open the findings beside it" };
  }
  return null;
}

function pullBtn(act, id, label, tip, cls) {
  return '<button type="button" class="pull-btn' + (cls ? " " + cls : "") + '" data-act="' + act + '" data-id="' + esc(id) + '"' +
    (tip ? ' data-tip="' + esc(tip) + '"' : "") + ">" + esc(label) + "</button>";
}

// WHAT A LIVE RUN IS DOING, from the tail of run.log the row carries while it is fetching or running. A line is
// `HH:MM:SS text` in UTC. `X start` and `X end 0.12` are the steps and `X error why` is a failure, and any other line is a
// sub-step the runner said, such as `fetch: forge answered (3.1s)`.
const PULL_STEPS_SHOWN = 6;

function pullLogLines(r) {
  const out = [];
  String(r.run_log || "").split("\n").forEach(l => {
    const m = /^(\d\d):(\d\d):(\d\d) (.+)$/.exec(l);
    if (!m) return;
    const d = new Date();
    d.setUTCHours(+m[1], +m[2], +m[3], 0);
    let at = d.getTime();
    if (at > Date.now() + 60000) at -= 86400000;
    let text = m[4], cls = "";
    let x;
    if ((x = /^(\w+) start$/.exec(text))) text = x[1] + ": starting";
    else if ((x = /^(\w+) end ([\d.]+)$/.exec(text))) text = x[1] + ": done" + (+x[2] > 0 ? " ($" + x[2] + ")" : "");
    else if ((x = /^(\w+) (?:error|failed) (.*)$/.exec(text))) { text = x[1] + ": " + x[2]; cls = " bad"; }
    out.push({ at, text, cls });
  });
  return out;
}

function pullElapsed(at) {
  const s = Math.max(0, Math.floor((Date.now() - at) / 1000));
  return s < 60 ? s + "s" : Math.floor(s / 60) + "m " + String(s % 60).padStart(2, "0") + "s";
}

function pullStepsHTML(r) {
  if (r.state !== "fetching" && r.state !== "running") return "";
  const lines = pullLogLines(r).slice(-PULL_STEPS_SHOWN);
  if (!lines.length) {
    return '<div class="pull-steps" role="status"><div class="now"><span class="pull-spin" aria-hidden="true"></span>starting&hellip;</div></div>';
  }
  const last = lines.length - 1;
  return '<div class="pull-steps" role="status">' + lines.map((l, i) => i < last
    ? '<div class="was' + l.cls + '">' + esc(l.text) + "</div>"
    : '<div class="now' + l.cls + '"><span class="pull-spin" aria-hidden="true"></span>' + esc(l.text) +
      ' <span class="pull-el" data-at="' + l.at + '">' + esc(pullElapsed(l.at)) + "</span></div>").join("") + "</div>";
}

// The elapsed time of the line a run is on counts up while one is drawn, and the timer stops when none is.
let pullsElTimer = 0;

function pullsElTick() {
  const els = document.querySelectorAll("#pulls-list .pull-el");
  if (!els.length) {
    clearInterval(pullsElTimer);
    pullsElTimer = 0;
    return;
  }
  els.forEach(el => { el.textContent = pullElapsed(+el.dataset.at); });
}

// One card. The same markup for a list answer and an event.
function pullRowHTML(r) {
  const title = r.title || r.url || "";
  const closed = pullClosed(r);
  const cost = r.cost_usd ? "$" + Number(r.cost_usd).toFixed(2) + (r.state === "running" || r.state === "fetching" ? " so far" : "") : "";
  const when = closed ? "closed " + pullAgo(r.archived_at)
    : pullAgo(r.state === "ready" && r.ready_at ? r.ready_at : r.created_at);
  const pill = pullPill(r);
  const note = pullNote(r);
  const second = pullSecond(r);
  const progress = pullProgress(r);
  const prim = pullPrimary(r);
  const chips = pullChips(r);
  const meta = [cost ? "<b>" + esc(cost) + "</b>" : "", esc(diskLabel(r.disk_bytes) || ""), esc(when)].filter(Boolean).join(" &middot; ");
  const strip = pullStages(r).map((s, i) => (i ? '<span class="ln"></span>' : "") + '<span class="d ' + s[1] + '">' + esc(s[0]) + "</span>").join("");
  return '<div class="pull" data-id="' + esc(r.id) + '" data-state="' + esc(closed ? "closed" : r.state) + '" data-group="' + pullGroup(r) +
    '" data-v="' + esc(pill.kind) + '">' +
    '<span class="pull-bar"></span>' +
    '<div class="pull-main">' +
      '<div class="pull-who"><span class="pull-repo"' + (r.head7 ? ' data-tip="head ' + esc(r.head7) + '"' : "") + ">" + esc(r.org_repo) +
        ' <b class="pull-num">#' + esc(r.number) + "</b></span>" +
        '<span class="pull-title">' + esc(title) + "</span>" +
        (r.author ? '<span class="pull-author">' + esc(r.author) + "</span>" : "") + "</div>" +
      '<div class="pull-l2"><span class="pull-state ' + esc(pill.kind) + (pill.interim ? " guess" : "") + '"' +
        (pill.interim ? ' data-tip="the board guessed this from the finding counts, until the panel writes a verdict"' : "") + ">" + esc(pill.text) + "</span>" +
        (chips ? '<span class="pull-counts">' + chips + "</span>" : "") + "</div>" +
      (r.why ? '<div class="pull-why">' + esc(r.why) + "</div>" : "") +
      (progress ? '<div class="pull-progress">' + esc(progress) + "</div>" : "") +
      (second ? '<div class="pull-second">' + esc(second) + "</div>" : "") +
      (note ? '<div class="pull-stage' + note.cls + '">' + esc(note.text) + "</div>" : "") +
      pullStepsHTML(r) +
      (closed ? "" : '<div class="pull-strip">' + strip + "</div>") +
    "</div>" +
    '<div class="pull-side">' +
      '<span class="pull-meta">' + meta + "</span>" +
      (closed ? "" : '<span class="pull-acts">' +
        (prim ? pullBtn(prim.act, r.id, prim.label, prim.tip, "pri" + (prim.tone ? " " + prim.tone : "")) : "") +
        pullBtn("menu", r.id, "…", "more", "more") + "</span>") +
    "</div>" +
    (pulls.logs[r.id] != null && r.state === "failed" ? '<pre class="pull-log">' + esc(pulls.logs[r.id]) + "</pre>" : "") +
  "</div>";
}

// ── drawing ─────────────────────────────────────────────

// The "open" filter leaves a closed row at once: an event can carry an archived row the list read never had.
function pullsShown() {
  return pulls.rows.filter(r => (!pulls.filterRepo || r.org_repo === pulls.filterRepo) &&
    (pulls.filterState === "all" || !r.archived_at) &&
    (!pulls.filterGroup || pullGroup(r) === pulls.filterGroup));
}

// The rows the attention counters count: the repo filter applies and the group filter does not, and a closed row is
// nobody's business.
function pullsCounted() {
  return pulls.rows.filter(r => !r.archived_at && (!pulls.filterRepo || r.org_repo === pulls.filterRepo));
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
}

// The header's segmented control and the four counters. An ellipsis while the first read is out, so nothing reads zero.
function pullsPaintFrame() {
  const dots = !pulls.loaded;
  const open = pulls.rows.filter(r => !r.archived_at).length;
  const n = (id, v) => { const e = document.getElementById(id); if (e) e.textContent = dots ? "…" : v == null ? "" : String(v); };
  n("pulls-n-open", open);
  n("pulls-n-all", pulls.filterState === "all" ? pulls.rows.length : pulls.allN);
  document.querySelectorAll("#pulls-seg button").forEach(b => b.classList.toggle("on", b.dataset.state === pulls.filterState));
  const by = { needs: 0, ready: 0, progress: 0, failed: 0 };
  pullsCounted().forEach(r => { by[pullGroup(r)]++; });
  document.querySelectorAll("#pulls-attn button").forEach(b => {
    const g = b.dataset.group;
    const e = b.querySelector("i");
    if (e) e.textContent = dots ? "…" : String(by[g]);
    b.classList.toggle("on", pulls.filterGroup === g);
    b.setAttribute("aria-pressed", pulls.filterGroup === g ? "true" : "false");
  });
}

function pullsGroupHTML(key, label, rows) {
  return '<section class="pull-group" data-group="' + key + '"><div class="pull-group-head">' + esc(label) + " <em>" + rows.length +
    "</em></div>" + rows.map(pullRowHTML).join("") + "</section>";
}

const PULL_SKELETON = '<div class="pull-skel"></div>'.repeat(3);

function pullsPaint() {
  pullsPaintRows();
}

function pullsPaintRows() {
  const list = document.getElementById("pulls-list");
  if (!list) return;
  pullsPaintFilters();
  pullsPaintFrame();
  if (!pulls.loaded) {
    list.innerHTML = pulls.loadFailed ? "" : PULL_SKELETON;
    return;
  }
  const rows = pullsShown();
  if (!rows.length) {
    const held = !!pulls.filterGroup;
    list.innerHTML = '<div class="pulls-empty"><div class="ico">' + (held ? "…" : "✓") + "</div>" +
      "<h2>" + (held ? "Nothing in that group" : "Nothing under review") + "</h2>" +
      "<p>" + (held ? "Click the counter again to see everything."
        : "Paste a pull request and atrium makes a worktree, reviews it, and puts the findings here ready to post.") + "</p>" +
      (held ? "" : '<button type="button" class="go" data-act="paste">+ Paste a PR</button>') + "</div>" +
      (!held && pulls.recent.length ? pullsGroupHTML("closed", "Closed recently", pulls.recent) : "");
    return;
  }
  const anchor = typeof scrollAnchor === "function" ? scrollAnchor(list, ".pull") : null;
  list.innerHTML = PULL_GROUPS.map(g => {
    const rs = rows.filter(r => pullGroup(r) === g[0]);
    return rs.length ? pullsGroupHTML(g[0], g[1], rs) : "";
  }).join("");
  if (anchor && typeof restoreScrollAnchor === "function") restoreScrollAnchor(list, ".pull", anchor);
  if (!pullsElTimer && list.querySelector(".pull-el")) pullsElTimer = setInterval(pullsElTick, 1000);
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

// Closing the row's card, which archives the row with it.
async function pullsClose(id) {
  const r = pulls.rows.find(x => x.id === id);
  if (!r || !r.walker_task) return;
  await closeCardAsk(r.walker_task, { display_title: r.org_repo + " #" + r.number });
  loadPulls();
}

async function pullsPaste() {
  const url = document.getElementById("pulls-pop-url");
  const why = document.getElementById("pulls-pop-why");
  const u = url ? url.value.trim() : "";
  if (!u) { pullsSay("paste the URL of a pull request"); return; }
  // ONE CALL: the worktree, the review row and its card, on whichever room the hub picks (js/fixtures.js has the
  // launch dialog's door to the same verb). The row arrives on the event stream, and the card is attached.
  const whyText = why ? why.value.trim() : "";
  if (url) url.value = "";
  if (why) why.value = "";
  pullsPopToggle(false);
  const id = linkOpeningId();
  linkOpeningBegin(id, u);
  let out;
  try {
    out = await pullsPost("/v1/open", { progress: id, url: u, why: whyText });
  } catch (e) {
    linkOpeningFail(id, pullsErr(e));
    return;
  }
  linkOpeningEnd(id);
  // A link that is not a pull request opens a card and no row, so the board would show nothing changed here.
  const pr = !out || !!out.pr;
  pullsSay(out && out.created === false
    ? "that " + (pr ? "PR" : "link") + " already has its card, so that one is attached"
    : pr ? "" :"opened " + (out.title || out.key) + " as a card. it is not a pull request, so it has no row here");
  loadPulls();
  if (out && out.card) await attachTask(out.card);
}

// The walk button: the findings are read and walked in the walk drawer beside the walker's terminal (js/walk.js),
// so this makes sure there is a walker, attaches its terminal and asks for the drawer.
async function pullsWalk(id) {
  const row = pulls.rows.find(r => r.id === id);
  if (!row) return;
  let task = "";
  try {
    // Always asked: the daemon answers the live walker as it is and starts a new one only when the old card is done.
    const out = await pullsPost("/v1/prs/" + encodeURIComponent(id) + "/walker", { action: "launch" });
    if (out && out.pr) pullsApplyRow(out.pr);
    task = (out && out.task) || (out && out.pr && out.pr.walker_task) || "";
    pullsChanged();
    pullsSay("");
  } catch (e) {
    pullsSay(pullsErr(e));
    return;
  }
  if (!task) return;
  dock.want = task;
  await attachTask(task);
  // Already attached to this card, or the row reached the board after the attach: ask the drawer again.
  if (!dock.open && typeof termTask !== "undefined" && termTask && termTask.id === task) walkProbe(termTask);
}

// ── leftovers: what no live card holds ──────────────────
// internal/api/sweep.go. One sweep per room (each named on a hub), drawn grouped by owner with one close per group.
// Nothing is removed until that close is pressed, and it is the card's own close (closeCardAsk in js/sharing.js).

const pullsLeft = { groups: [], notOwned: [] };

async function pullsSweep() {
  const box = document.getElementById("pulls-left");
  if (!box) return;
  if (pulls.leftOpen) { pulls.leftOpen = false; box.hidden = true; return; }
  pulls.leftOpen = true;
  box.hidden = false;
  box.innerHTML = '<p class="pane-lead">Sweeping.</p>';
  const rooms = typeof hubIsHub !== "undefined" && hubIsHub && typeof hubRooms !== "undefined" && hubRooms.length
    ? hubRooms.map(r => r.name) : [""];
  const groups = [], notOwned = [], quiet = [];
  await Promise.all(rooms.map(async room => {
    try {
      const out = await api("/v1/sweep", {
        method: "POST", headers: room ? { "X-Atrium-Room": room } : {}
      });
      (out.leftovers || []).forEach(g => groups.push(Object.assign({ room }, g)));
      (out.not_owned || []).forEach(n => notOwned.push(Object.assign({ room }, n)));
    } catch (e) {
      quiet.push((room || "this room") + ": " + pullsErr(e));
    }
  }));
  pullsLeft.groups = groups;
  pullsLeft.notOwned = notOwned;
  pullsLeftPaint(quiet);
}

// What a group is, in a word: why nothing live holds it.
function pullsLeftWhy(g) {
  if (g.no_card) return "no card";
  if (g.closed) return "kept when it was closed";
  return g.status || "";
}

function pullsLeftPaint(quiet) {
  const box = document.getElementById("pulls-left");
  if (!box) return;
  const at = r => (r ? " on " + r : "");
  const html = pullsLeft.groups.map((g, i) =>
    '<div class="pull">' +
      '<div class="pull-main">' +
        '<div class="left-head"><span class="pull-repo">' + esc(g.title || g.owner) + "</span>" +
          "<span>" + esc(pullsLeftWhy(g) + at(g.room)) + "</span>" +
          "<span>" + esc(diskLabel(g.disk_bytes)) + "</span>" +
          pullBtn("left-close", String(i), "close these", "the card's close: shows what goes, asks, then frees it") + "</div>" +
        '<ul class="left-rows">' + (g.rows || []).map(r => "<li>" + esc(r.kind + " " + r.ref) + "</li>").join("") + "</ul>" +
      "</div>" +
    "</div>").join("") +
    (pullsLeft.notOwned.length
      ? '<div class="pull"><div class="pull-main"><div class="left-head"><span class="pull-repo">not owned</span>' +
        "<span>worktrees under atrium's own folder that no card names. listed only</span></div>" +
        '<ul class="left-rows">' + pullsLeft.notOwned.map(n => "<li>" + esc(n.path + at(n.room)) + "</li>").join("") +
        "</ul></div></div>"
      : "") +
    (quiet || []).map(q => '<p class="pane-lead">' + esc(q) + "</p>").join("");
  box.innerHTML = html || '<p class="pane-lead">Nothing left over. Every card that is done gave back what it held.</p>';
}

async function pullsLeftClose(i) {
  const g = pullsLeft.groups[i];
  if (!g) return;
  const title = { display_title: g.title || g.owner };
  // A card is closed as itself, by its tagged id on a hub. An owner with no card is closed through the sweep.
  if (g.no_card) await closeCardAsk(g.owner, title, { owner: g.owner, room: g.room });
  else await closeCardAsk(g.room ? g.room + "~" + g.owner : g.owner, title);
  pulls.leftOpen = false;
  await pullsSweep();
  loadPulls();
}

// ── moving a review to another room ─────────────────────
// docs/rnd/pr-review-workflow.md 5.1. The hub moves the claim and the review with it, then this makes the PR's worktree
// on the new room, launches a new card there and sets it as the walker. The old card is left alone.

// The online rooms. Nothing to move to when there is one, and the action is not drawn.
function prMoveRooms() {
  return typeof hubRooms === "undefined" || !hubIsHub ? [] : hubRooms;
}

const prMoveKey = pr => (pr.host + "/" + pr.org + "/" + pr.repo + "/" + pr.number).toLowerCase();

// The rooms a PR can go to: every online room but the one that owns it. `from` is empty when the owner is unknown.
function prMoveTargets(from) {
  return prMoveRooms().map(r => r.name).filter(n => n !== from);
}

// The room that owns the PR, from the hub's claims.
async function prMoveOwner(pr) {
  const out = await api("/_hub/pr-claims");
  const c = ((out && out.claims) || []).find(x => x.key === prMoveKey(pr));
  return c ? c.room : "";
}

// The row, and with it whether to offer the action at all: more than one room attached.
const prMoveOffered = () => prMoveRooms().length > 1;

// The room picker, then the move. `anchor` is the click that asked, `walker` the card to take the harness from.
async function prMoveAsk(e, row, walker) {
  let from = "";
  try { from = await prMoveOwner(row); } catch (err) { pullsSay(pullsErr(err)); return; }
  const to = prMoveTargets(from);
  if (!to.length) { pullsSay("there is no other online room to move it to"); return; }
  showMenu(e, to.map(name => ({ label: name, act: () => prMove(row, name, from, walker) })));
  pullsSheet();
}

// The four calls, in order. A refusal at the hub stops everything after it and says the hub's own sentence.
async function prMove(row, to, from, walker) {
  const label = row.org + "/" + row.repo + "#" + row.number;
  const say = (text, warn) => pullsSay(warn ? text : "");
  try {
    await api("/_hub/pr-claims/move", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ key: prMoveKey(row), to })
    });
  } catch (e) {
    pullsSay(pullsErr(e));
    tellUser("the review was not moved", pullsErr(e));
    return;
  }
  const pr = { host: row.host, org: row.org, repo: row.repo, number: row.number };
  const at = { room: to, url: row.url, say };
  try {
    const wt = await makePastedWorktree(pr, at);
    if (!wt) throw new Error("no provider makes worktrees for " + (pr.host || "that host"));
    const review = await startPastedReview(pr, to, at);
    const task = await api("/v1/launch", {
      method: "POST", headers: { "Content-Type": "application/json", "X-Atrium-Room": to },
      body: JSON.stringify({
        harness: (walker && walker.harness) || "claude", task_id: "", cwd: wt.path,
        title: label, tags: ["pr", "pr:" + label],
        repo: row.repo, org: row.org, host: row.host, source_kind: "pull-request", source_url: row.url
      })
    });
    if (review && task && task.id) await setPastedWalker(review.row, task.id, to, at);
    toast(label, "moved to " + to + ". The old card is still on " + (from || "the old room") + ".");
    loadPulls();
  } catch (e) {
    pullsSay("moved to " + to + ", but the card there was not made: " + pullsErr(e));
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

// The paste popover: one link, an optional why. The rare case, so it takes no room until asked.
function pullsPopToggle(on) {
  const pop = document.getElementById("pulls-pop");
  if (!pop) return;
  pop.hidden = on === undefined ? !pop.hidden : !on;
  if (!pop.hidden) { const u = document.getElementById("pulls-pop-url"); if (u) u.focus(); }
}

// Opening the walker's card, with its question in view when it has one: the card's own drawer shows it.
async function pullsOpenCard(id) {
  const row = pulls.rows.find(r => r.id === id);
  if (!row) return;
  if (!row.walker_task) { pullsSay("this review has no card yet"); return; }
  dock.want = row.walker_task;
  await attachTask(row.walker_task);
}

// The `...` of one card. Labels are sentences, since "move" and "leftovers" did not say what they did.
function pullsMenu(e, id) {
  const r = pulls.rows.find(x => x.id === id);
  if (!r) return;
  const live = r.state === "queued" || r.state === "fetching" || r.state === "running";
  const items = [];
  if (live) items.push({ label: "Abort the run", act: () => pullsRowAction(id, "abort") });
  if (r.state === "failed") items.push({ label: "Show the run log", act: () => pullsLog(id) });
  if (prMoveOffered()) items.push({ label: "Move review and card to another room", act: () => prMoveAsk(e, r, null) });
  if (r.walker_task) items.push({ label: "Close card and free its worktree, branch and review", danger: true, act: () => pullsClose(id) });
  if (items.length) items.push({ sep: true });
  if (r.url) items.push({ label: "Open on GitHub", out: true, act: () => window.open(r.url, "_blank", "noopener") });
  if (r.url) items.push({ label: "Copy PR link", act: () => { if (navigator.clipboard) navigator.clipboard.writeText(r.url).catch(() => {}); toast(r.org_repo + " #" + r.number, "link copied"); } });
  showMenu(e, items);
  pullsSheet();
}

// On a phone the menu is a bottom sheet: a popover on a 390px screen lands under the thumb. css/pulls.css draws it.
function pullsSheet() {
  if (typeof cardMenuEl === "undefined" || !cardMenuEl) return;
  const phone = window.matchMedia("(max-width: 640px)").matches;
  cardMenuEl.classList.toggle("pull-sheet", phone);
  // showMenu positions with inline left and top, which only inline styles can undo. Taken back when the menu closes.
  if (phone) Object.assign(cardMenuEl.style, { left: "0px", right: "0px", top: "auto", bottom: "0px", width: "auto" });
  if (pullsSheet.watching) return;
  pullsSheet.watching = true;
  new MutationObserver(() => {
    // Only when there is something to undo: a class write that changes nothing still queues this observer again.
    if (cardMenuEl.classList.contains("on") || !cardMenuEl.classList.contains("pull-sheet")) return;
    cardMenuEl.classList.remove("pull-sheet");
    Object.assign(cardMenuEl.style, { right: "", bottom: "", width: "" });
  }).observe(cardMenuEl, { attributes: true, attributeFilter: ["class"] });
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
      case "walk": pullsWalk(id); break;
      case "open": pullsOpenCard(id); break;
      case "menu": e.stopPropagation(); pullsMenu(e, id); break;
      case "paste": e.stopPropagation(); pullsPopToggle(true); break;
    }
  });
  const left = document.getElementById("pulls-left");
  if (left) left.addEventListener("click", e => {
    const b = e.target.closest("button[data-act='left-close']");
    if (b) pullsLeftClose(Number(b.dataset.id));
  });
  const more = document.getElementById("pulls-more");
  if (more) more.addEventListener("click", e => {
    e.stopPropagation();
    showMenu(e, [{ label: "Find unclaimed worktrees", tip: "what no live card holds any more: worktrees, branches, refs, reviews", act: pullsSweep }]);
  });
  const paste = document.getElementById("pulls-paste");
  if (paste) paste.addEventListener("click", e => { e.stopPropagation(); pullsPopToggle(); });
  const pop = document.getElementById("pulls-pop");
  if (pop) {
    pop.addEventListener("click", e => e.stopPropagation());
    pop.addEventListener("keydown", e => {
      if (e.key === "Enter" && e.target.tagName === "INPUT") { e.preventDefault(); pullsPaste(); }
      if (e.key === "Escape") { e.stopPropagation(); pullsPopToggle(false); }
    });
    const go = document.getElementById("pulls-pop-go");
    if (go) go.addEventListener("click", pullsPaste);
    const no = document.getElementById("pulls-pop-cancel");
    if (no) no.addEventListener("click", () => pullsPopToggle(false));
    document.addEventListener("click", () => pullsPopToggle(false));
  }
  const seg = document.getElementById("pulls-seg");
  if (seg) seg.addEventListener("click", e => {
    const b = e.target.closest("button[data-state]");
    if (!b || b.dataset.state === pulls.filterState) return;
    pulls.filterState = b.dataset.state;
    pullsPaint();
    loadPulls();
  });
  const attn = document.getElementById("pulls-attn");
  if (attn) attn.addEventListener("click", e => {
    const b = e.target.closest("button[data-group]");
    if (!b) return;
    pulls.filterGroup = pulls.filterGroup === b.dataset.group ? "" : b.dataset.group;
    pullsPaint();
  });
  const repo = document.getElementById("pulls-repo");
  if (repo) repo.addEventListener("change", () => { pulls.filterRepo = repo.value; pullsPaint(); });
});
