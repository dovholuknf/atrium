// Change requests, as the "Requests" view of the repos area (next to Shelf, Ledger and Feed). docs/rnd/hub-forge-design.md
// section 6. A request asks for a branch the hub holds to go into a target; atrium's stage 5 is MANUAL, so a request into
// `main` waits for the orchestrator or clint to merge on the hub's side, and the board only ever RECORDS that it happened.
//
//   a "needs the orchestrator or clint" strip on top, one card per open request into main
//   a ledger below it: open | closed, grouped by target on the left, the selected request's page on the right
//   a page: source -> target, the Pushed line (the hub's push log), why, the change record, the request's history, and the
//           operator's actions (withdraw, close with a note, record the sha it was merged at)
//
// THE DATA IS THE HUB'S, through js/changereq-core.js (which answers from js/changereq-mock.js only when asked). Title and
// why are DATA: every one goes through esc() into text, and the why keeps its line breaks with CSS, never as markup.
// READ ON ENTRY and on the refresh button, and again after every write this page makes. The hub also says `change-request`
// on its event stream, which re-reads the list when it has been read (see crOnEvent at the bottom).
//
// THE BOARD NEVER MERGES. There is no merge button. The one action that touches a merge is "record that it was merged", a
// sha field the operator fills after doing it, checked as 7 to 40 hex characters here and by the hub's 409 there.

const cr = { reqs: [], loaded: false, offline: false, note: "", tab: "open", sel: "", detail: {}, form: null, act: "", draft: { sha: "", note: "" }, err: "", busy: false, inflight: false };

const crRepoKey = r => (r.host || "github") + "/" + r.owner + "/" + r.repo;
const crOpen = () => cr.reqs.filter(r => r.state === "open");
const crClosed = () => cr.reqs.filter(r => r.state !== "open");
const crTone = c => "style=\"--c:var(--" + c + ")\"";

function crPill(label, tone) {
  return '<span class="hr-state cr-pill" ' + crTone(tone) + "><i></i>" + esc(label) + "</span>";
}
const crStatePill = r => crPill(crCore.STATE[r.state] || r.state, crCore.STATE_TONE[r.state] || "dim");
const crPushedPill = p => p ? crPill(crCore.PUSHED_SHORT[p.state] || p.state, crCore.PUSHED_TONE[p.state] || "dim") : "";

// ---- reading -------------------------------------------------------------------------------------------------------

async function crLoad() {
  if (cr.inflight) return;
  cr.inflight = true;
  try {
    const r = await crCore.api.list({ state: "all" });
    if (r.ok && r.body && Array.isArray(r.body.requests)) {
      cr.reqs = r.body.requests;
      cr.note = ""; cr.offline = false; cr.loaded = true;
    } else {
      cr.loaded = false;
      cr.offline = r.offline || r.status >= 500;
      cr.note = r.offline ? "The hub is not answering, so there is nothing to show here until it is back."
        : r.status === 404 ? "This hub does not have change requests yet. It may be too old."
        : "The hub would not list change requests: " + crCore.errText(r);
    }
  } finally { cr.inflight = false; }
  if (cr.loaded && !cr.reqs.some(x => x.id === cr.sel)) cr.sel = "";
  if (cr.loaded && !cr.sel) { const first = (cr.tab === "open" ? crOpen() : crClosed())[0] || cr.reqs[0]; cr.sel = first ? first.id : ""; }
  if (cr.sel) await crDetail(cr.sel, true);
  hubReposPaint();
}

// One request with its `pushed` answer. A list row has no `pushed`: the hub answers it per request.
async function crDetail(id, quiet) {
  const r = await crCore.api.get(id);
  if (r.ok && r.body && r.body.id === id) cr.detail[id] = r.body;
  else cr.detail[id] = { error: r.offline ? "the hub is not answering" : crCore.errText(r) };
  if (!quiet && cr.sel === id) hubReposPaint();
}

// ---- the page ------------------------------------------------------------------------------------------------------

const crWho = x => x ? (x.card === "operator" ? "the operator" : ((x.room || "") + (x.card ? " " + x.card : "")).trim()) : "";

function crLane(r) {
  const room = crCore.sourceRoom(r);
  return '<div class="cr-lane ' + hubReposAccent("room" + room) + '"><div class="cr-end"><small>from</small><span class="cr-who">' + hubReposAvatar(room, 38) +
    '<span class="cr-br" title="' + esc(r.source.branch) + '">' + esc(r.source.branch) + "</span></span></div>" +
    '<span class="cr-arr" aria-hidden="true"><svg viewBox="0 0 120 12" preserveAspectRatio="none" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M2 6h112M108 1l6 5-6 5"/></svg></span>' +
    '<div class="cr-end"><small>into</small><span class="cr-br" title="' + esc(r.target.branch) + '">' + esc(r.target.branch) + "</span></div></div>";
}

function crSteps() {
  return '<ol class="cr-steps"><li><b>1</b>Merge it on the hub\'s side</li><li><b>2</b>Re-sign and push</li><li><b>3</b>Record the sha here</li></ol>';
}

// What the operator may do. Everything here is a write, so a read-only board gets a line and no buttons.
function crOperator(r) {
  if (crCore.state.readOnly) return '<p class="cr-ro" role="note">' + esc(crCore.readOnlyLine) + "</p>";
  const sha = cr.draft.sha.trim(), okSha = crCore.isSha(sha), dis = cr.busy ? " disabled" : "";
  const err = cr.err ? '<p class="cr-err" role="alert">' + esc(cr.err) + "</p>" : "";
  const merge = '<div class="cr-field"><label for="cr-sha">Merged at (sha)</label><div class="cr-row"><input id="cr-sha" class="cr-in mono" data-crf="sha" value="' + esc(cr.draft.sha) +
    '" placeholder="e40a3c2" spellcheck="false" autocomplete="off" maxlength="40" aria-describedby="cr-shahint"><button type="button" class="cr-btn go" data-cr="merge-do" ' + (okSha ? "" : "disabled ") + dis.trim() + '>Record that it was merged</button></div>' +
    '<small id="cr-shahint" class="cr-hint' + (sha && !okSha ? " bad" : "") + '">' + (sha && !okSha ? "A sha is 7 to 40 hex characters." : "Only records it. The board does not merge anything.") + "</small></div>";
  const closing = cr.act === "close"
    ? '<div class="cr-field"><label for="cr-note">Why close it (optional)</label><div class="cr-row"><input id="cr-note" class="cr-in" data-crf="note" value="' + esc(cr.draft.note) + '" maxlength="300" autocomplete="off">' +
      '<button type="button" class="cr-btn" data-cr="close-do"' + dis + '>Close request</button><button type="button" class="cr-btn quiet" data-cr="close-cancel">Cancel</button></div></div>'
    : "";
  return merge + err + '<div class="cr-acts"><button type="button" class="cr-btn" data-cr="withdraw"' + dis + '>Withdraw</button>' +
    (cr.act === "close" ? "" : '<button type="button" class="cr-btn" data-cr="close-open"' + dis + ">Close with a note</button>") + "</div>" + closing;
}

function crGate(r) {
  if (!crCore.isOpen(r)) return "";
  const main = crCore.needsOperator(r);
  return '<section class="cr-gate' + (main ? " main" : "") + '"><b>' + (main ? "Needs the orchestrator or clint" : "Waiting to be merged") + "</b>" +
    "<span>" + (main ? "A request into main is merged on the hub's side for now. Nothing on the board does it." : "The board does not merge. Merge it on the hub's side, then record it here.") + "</span>" +
    crSteps() + crOperator(r) + "</section>";
}

// What happened to a request that is no longer open, said first.
function crEnded(r) {
  if (crCore.isOpen(r)) return "";
  const ev = r.state === "merged" ? "Recorded as merged" + (r.merged_sha ? " at " + hubReposSha(r.merged_sha) : "")
    : r.state === "withdrawn" ? "Withdrawn by its owner" : "Closed";
  const by = crWho(r.closed_by);
  return '<section class="cr-ended ' + r.state + '"><b>' + esc(ev) + "</b><span>" + (by ? "by " + esc(by) + " " : "") + hubReposAge(r.closed_at, "hr-when") +
    (r.note ? '</span><q class="cr-note">' + esc(r.note) + "</q>" : "</span>") + "</section>";
}

function crPushedPanel(r, d) {
  const p = d && d.pushed;
  let body;
  if (d && d.error) body = '<p class="cr-quiet">The hub did not say where this branch is: ' + esc(d.error) + ".</p>";
  else if (!d) body = '<p class="cr-quiet">Reading the push log.</p>';
  else if (p.state === "not-pushed") body = '<p class="cr-line">' + esc(crCore.pushedLine(p, r.source.branch)) + '</p><p class="cr-quiet">' + esc(crCore.notPushedHow(r.source.branch)) + "</p>";
  else {
    body = '<p class="cr-line"><span class="cr-pk">Pushed</span>' + esc(crCore.pushedLine(p, r.source.branch)) + "</p>" +
      '<dl class="cr-kv"><dt>hub head</dt><dd class="mono">' + esc(hubReposSha(p.hub_sha)) + "</dd>" + (r.source.sha ? "<dt>request</dt><dd class=\"mono\">" + esc(hubReposSha(r.source.sha)) + "</dd>" : "") +
      (p.room ? "<dt>pushed by</dt><dd>" + esc(p.room + (p.card ? " " + p.card : "")) + (hubReposCardTitle(p.room, p.card) ? " · " + esc(hubReposCardTitle(p.room, p.card)) : "") + "</dd>" : "") +
      (p.at ? "<dt>pushed</dt><dd>" + hubReposAge(p.at, "hr-when") + "</dd>" : "") + (p.released ? '<dt>state</dt><dd>released</dd>' : "") + "</dl>";
  }
  return '<section class="cr-panel"><h3 class="hr-h">On the hub</h3>' + body + "</section>";
}

function crRecordPanel(r) {
  let body;
  if (!r.change) body = '<p class="cr-quiet">No change record is attached to this request.</p>';
  else {
    const lines = crCore.recordLines(r), d = cr.detail[r.id];
    body = '<p class="cr-quiet">Attached: <code>' + esc(r.change) + "</code></p>";
    if (lines) body += '<dl class="cr-rec">' + Object.entries(lines).map(([k, v]) => "<dt>" + esc(k) + "</dt><dd>" + esc(v) + "</dd>").join("") +
      "<dt>Pushed</dt><dd>" + esc(d && d.pushed ? crCore.pushedLine(d.pushed, r.source.branch) : "reading the push log") + "</dd></dl>";
    else body += '<p class="cr-quiet">The hub does not serve the record\'s four lines to the board yet. The Pushed line is on the left.</p>';
  }
  return '<section class="cr-panel"><h3 class="hr-h">Change record</h3>' + body + "</section>";
}

function crTimeline(r, d) {
  const ev = crCore.timeline(r, d && d.pushed);
  return '<section class="cr-panel"><h3 class="hr-h">History</h3><ol class="cr-tl">' + ev.map(e =>
    '<li class="' + esc(e.kind) + '"><span class="cr-tlt">' + (e.who ? "<b>" + esc(e.who) + "</b> " : "") + esc(e.text) + "</span>" + hubReposAge(e.at, "hr-when") +
    (e.note ? '<q class="cr-note">' + esc(e.note) + "</q>" : "") + "</li>").join("") + "</ol></section>";
}

function crPage(r) {
  const d = cr.detail[r.id];
  const pushed = d && d.pushed ? crPushedPill(d.pushed) : "";
  const room = crCore.sourceRoom(r);
  return '<main class="cr-detail ' + hubReposAccent("room" + room) + '" aria-live="polite"><div class="cr-crumb">' + esc(crCore.repoShort(r.repo)) + " · " + esc(r.id) + " · " + hubReposAge(r.created_at, "hr-when") + "</div>" +
    crLane(r) + '<div class="hr-badges">' + crStatePill(r) + pushed + "</div>" + '<h2 class="cr-title">' + esc(r.title) + "</h2>" +
    crEnded(r) + crGate(r) +
    '<div class="cr-cols"><section class="cr-panel cr-whyp"><h3 class="hr-h">Why</h3>' + (r.why ? '<p class="cr-why">' + esc(r.why) + "</p>" : '<p class="cr-quiet">No reason was given.</p>') + "</section>" + crPushedPanel(r, d) + "</div>" +
    '<div class="cr-cols">' + crRecordPanel(r) + crTimeline(r, d) + "</div></main>";
}

// ---- the list ------------------------------------------------------------------------------------------------------

function crItem(r) {
  const room = crCore.sourceRoom(r), sel = r.id === cr.sel;
  return '<button type="button" class="cr-li ' + hubReposAccent("room" + room) + (sel ? " sel" : "") + '" data-cr="sel" data-id="' + esc(r.id) + '"' + (sel ? ' aria-current="true"' : "") + ">" +
    hubReposAvatar(room, 40) + '<span class="cr-lit"><span class="cr-lt">' + esc(r.title) + '</span><span class="cr-lm">' + esc(r.source.branch) + " → " + esc(r.target.branch) + " · " + esc(hubReposWhen(r.created_at)) + "</span></span>" +
    (crCore.needsOperator(r) ? '<span class="cr-needs">needs you</span>' : crStatePill(r)) + "</button>";
}

function crList() {
  const rs = cr.tab === "open" ? crOpen() : crClosed();
  const seg = '<div class="cr-seg" role="group" aria-label="which requests">' +
    ["open", "closed"].map(t => '<button type="button" data-cr="tab" data-tab="' + t + '" aria-pressed="' + (cr.tab === t) + '">' + (t === "open" ? "Open" : "Closed") + " <b>" + (t === "open" ? crOpen().length : crClosed().length) + "</b></button>").join("") + "</div>";
  const groups = crCore.byTarget(rs).map(g => '<div class="cr-tg"><span>into ' + esc(g.target) + "</span><b>" + g.reqs.length + "</b></div>" + g.reqs.map(crItem).join("")).join("");
  return '<nav class="cr-list" aria-label="change requests">' + seg + (groups || '<p class="cr-quiet cr-none">' + (cr.tab === "open" ? "Nothing is open." : "Nothing has been closed yet.") + "</p>") + "</nav>";
}

// One card per open request into main: the strongest "someone has to act" signal, above everything else.
function crStrip() {
  const need = crOpen().filter(crCore.needsOperator);
  if (!need.length) return "";
  return '<section class="cr-need" aria-label="needs the orchestrator or clint">' + need.map(r => {
    const room = crCore.sourceRoom(r);
    return '<article class="cr-hero ' + hubReposAccent("room" + room) + '"><span class="cr-k">Needs the orchestrator or clint</span><h3>' + esc(r.title) + '</h3>' +
      '<div class="cr-row"><span class="cr-who">' + hubReposAvatar(room, 26) + '<span class="cr-br">' + esc(r.source.branch) + '</span></span><span class="cr-arrow" aria-hidden="true">→</span><span class="cr-br">main</span></div>' +
      '<div class="cr-row"><span class="cr-quiet">' + esc(r.id) + " · " + esc(hubReposWhen(r.created_at)) + '</span><button type="button" class="cr-btn" data-cr="sel" data-id="' + esc(r.id) + '">Open</button></div></article>';
  }).join("") + "</section>";
}

// ---- opening one ---------------------------------------------------------------------------------------------------

function crBranches(repoKey) {
  const r = (hubRepos.repos || []).find(x => crRepoKey(x) === repoKey);
  return r ? (r.branches || []).map(b => b.name) : [];
}

function crForm() {
  const f = cr.form, repos = (hubRepos.repos || []).map(crRepoKey);
  const names = crBranches(f.repo), dl = names.concat(["main"]);
  const repoField = repos.length > 1 ? '<select id="cr-f-repo" class="cr-in" data-crf="f-repo">' + repos.map(k => '<option' + (k === f.repo ? " selected" : "") + ">" + esc(k) + "</option>").join("") + "</select>"
    : '<input id="cr-f-repo" class="cr-in mono" data-crf="f-repo" value="' + esc(f.repo) + '" autocomplete="off">';
  return '<section class="cr-form" aria-label="open a change request"><h3 class="hr-h">Open a request</h3>' +
    '<p class="cr-quiet">Asks for a branch on the hub to go into another. The board does not merge it: the orchestrator or clint does, on the hub\'s side.</p>' +
    '<label for="cr-f-repo">Repo</label>' + repoField +
    '<label for="cr-f-branch">Branch to send</label><input id="cr-f-branch" class="cr-in mono" data-crf="f-branch" list="cr-dl" value="' + esc(f.branch) + '" autocomplete="off" spellcheck="false">' +
    '<datalist id="cr-dl">' + dl.map(n => '<option value="' + esc(n) + '">').join("") + "</datalist>" +
    '<label for="cr-f-target">Into</label><input id="cr-f-target" class="cr-in mono" data-crf="f-target" list="cr-dl" value="' + esc(f.target) + '" autocomplete="off" spellcheck="false">' +
    '<label for="cr-f-title">Title</label><input id="cr-f-title" class="cr-in" data-crf="f-title" value="' + esc(f.title) + '" maxlength="200" autocomplete="off">' +
    '<label for="cr-f-why">Why</label><textarea id="cr-f-why" class="cr-in" data-crf="f-why" rows="4" maxlength="2000">' + esc(f.why) + "</textarea>" +
    (f.err ? '<p class="cr-err" role="alert">' + esc(f.err) + "</p>" : "") +
    '<div class="cr-acts"><button type="button" class="cr-btn go" data-cr="new-do"' + (cr.busy ? " disabled" : "") + '>Open request</button><button type="button" class="cr-btn quiet" data-cr="new-cancel">Cancel</button></div></section>';
}

// ---- the view ------------------------------------------------------------------------------------------------------

function crEmpty() {
  return '<section class="hr-hero"><div class="hr-halo">' + HR_SVG.art + "</div><h2>Nothing is waiting to be <em>merged.</em></h2>" +
    '<p class="hr-lead">A room asks for its finished branch to go into <code>main</code> or another branch, and the request lands here with its change record. Open one from a branch in the Ledger, or from here.</p>' +
    (crCore.state.readOnly ? '<p class="cr-ro">' + esc(crCore.readOnlyLine) + "</p>" : '<div class="hr-herorow"><button type="button" class="cr-btn go" data-cr="new">Open a request</button></div>') + "</section>";
}

function crView() {
  if (cr.note && !cr.loaded) return '<div class="cr-offline" role="alert"><p class="pane-lead hr-note">' + esc(cr.note) + '</p><button type="button" class="cr-btn" data-cr="retry">Try again</button></div>';
  if (!cr.loaded) return '<p class="cr-quiet">Reading the hub\'s change requests.</p>';
  const top = '<div class="cr-bar"><h2 class="cr-h2">Change requests</h2>' + (crCore.state.readOnly ? "" : '<button type="button" class="cr-btn go" data-cr="new">Open a request</button>') + "</div>";
  if (cr.form) return top + crForm();
  if (!cr.reqs.length) return crEmpty();
  const r = cr.reqs.find(x => x.id === cr.sel) || cr.reqs[0];
  return top + crStrip() + '<div class="cr-ledger">' + crList() + crPage(r) + "</div>";
}

// ---- the branch rows of the repos Ledger ---------------------------------------------------------------------------
// A pushed branch that already has an open request shows it; one that has none offers to open one.

function crBranchBit(repo, b) {
  if (!cr.loaded) return "";
  const key = crRepoKey(repo);
  const open = cr.reqs.find(r => r.state === "open" && r.repo === key && r.source.branch === b.name);
  if (open) return '<button type="button" class="cr-chip" data-cr="goto" data-id="' + esc(open.id) + '" aria-label="open request ' + esc(open.id) + '">' + esc(open.id) + " · into " + esc(open.target.branch) + "</button>";
  if (b.released || crCore.state.readOnly) return "";
  return '<button type="button" class="cr-chip ask" data-cr="new" data-repo="' + esc(key) + '" data-branch="' + esc(b.name) + '">ask to merge</button>';
}

// ---- acting --------------------------------------------------------------------------------------------------------

function crSayErr(msg) { cr.err = msg; hubReposPaint(); }

async function crWrite(fn, after) {
  if (cr.busy) return;
  cr.busy = true; cr.err = "";
  hubReposPaint();
  try { await fn(); } finally { cr.busy = false; }
  if (after) await after();
}

async function crDoAct(id, body, done) {
  await crWrite(async () => {
    const r = await crCore.api.act(id, body);
    if (r.ok) { cr.act = ""; cr.draft = { sha: "", note: "" }; await crLoadAfter(id); return; }
    cr.err = crCore.state.readOnly ? crCore.readOnlyLine : r.offline ? "The hub is not answering. Nothing was changed." : crCore.errText(r);
    hubReposPaint();
  });
}
async function crLoadAfter(id) { cr.sel = id; cr.inflight = false; delete cr.detail[id]; await crLoad(); }

async function crSubmitNew() {
  const f = cr.form;
  const title = f.title.trim(), branch = f.branch.trim(), target = f.target.trim();
  if (!branch || !target || !title) { f.err = "A request needs the branch to send, where it goes and a title."; hubReposPaint(); return; }
  await crWrite(async () => {
    const r = await crCore.api.create({ repo: f.repo, source: { branch }, target: { branch: target }, title, why: f.why.trim() });
    if (r.ok || r.status === 409) {
      const got = r.body && r.body.id ? r.body : null;
      if (r.status === 409 && !got) { f.err = crCore.errText(r); hubReposPaint(); return; }
      cr.form = null; cr.tab = "open";
      await crLoadAfter(got ? got.id : cr.sel);
      if (r.status === 409) { cr.err = "An open request for this branch and target already exists: it is shown here."; hubReposPaint(); }
      return;
    }
    f.err = crCore.state.readOnly ? crCore.readOnlyLine : r.offline ? "The hub is not answering. Nothing was opened."
      : r.status === 404 ? "The hub has no branch called " + branch + ". " + crCore.notPushedHow(branch) : crCore.errText(r);
    hubReposPaint();
  });
}

function crClick(el) {
  const act = el.dataset.cr, id = el.dataset.id;
  if (act === "tab") { cr.tab = el.dataset.tab; cr.form = null; const l = cr.tab === "open" ? crOpen() : crClosed(); if (!l.some(x => x.id === cr.sel) && l[0]) { cr.sel = l[0].id; cr.act = ""; cr.err = ""; crDetail(cr.sel); } hubReposPaint(); }
  else if (act === "sel" || act === "goto") {
    cr.form = null; cr.act = ""; cr.err = ""; cr.draft = { sha: "", note: "" }; cr.sel = id;
    const r = cr.reqs.find(x => x.id === id); if (r) cr.tab = r.state === "open" ? "open" : "closed";
    if (act === "goto") setHubReposView("requests"); else hubReposPaint();
    crDetail(id);
  } else if (act === "new") {
    const repo = el.dataset.repo || ((hubRepos.repos || [])[0] ? crRepoKey(hubRepos.repos[0]) : "github/");
    const branch = el.dataset.branch || "";
    const card = (() => { const r = (hubRepos.repos || []).find(x => crRepoKey(x) === repo); const b = r && (r.branches || []).find(x => x.name === branch); return b ? hubReposCardTitle(b.room, b.card) : ""; })();
    cr.form = { repo, branch, target: "main", title: card || branch, why: "", err: "" };
    if (hubReposView() !== "requests") setHubReposView("requests"); else hubReposPaint();
  } else if (act === "new-cancel") { cr.form = null; hubReposPaint(); }
  else if (act === "new-do") crSubmitNew();
  else if (act === "withdraw") crDoAct(cr.sel, { do: "withdraw" });
  else if (act === "close-open") { cr.act = "close"; hubReposPaint(); const n = document.getElementById("cr-note"); if (n) n.focus(); }
  else if (act === "close-cancel") { cr.act = ""; hubReposPaint(); }
  else if (act === "close-do") crDoAct(cr.sel, { do: "close", note: cr.draft.note.trim() });
  else if (act === "merge-do") { const sha = cr.draft.sha.trim(); if (crCore.isSha(sha)) crDoAct(cr.sel, { do: "merged", sha }); }
  else if (act === "retry") crLoad();
}

// Typing keeps its text in `cr` and repaints nothing, so a field never loses what was typed. The sha field only moves its
// own button and hint.
function crInput(el) {
  const k = el.dataset.crf, v = el.value;
  if (k === "sha") {
    cr.draft.sha = v;
    const ok = crCore.isSha(v.trim()), btn = document.querySelector('[data-cr="merge-do"]'), hint = document.getElementById("cr-shahint");
    if (btn) btn.disabled = !ok || cr.busy;
    if (hint) { hint.classList.toggle("bad", !!v.trim() && !ok); hint.textContent = v.trim() && !ok ? "A sha is 7 to 40 hex characters." : "Only records it. The board does not merge anything."; }
  } else if (k === "note") cr.draft.note = v;
  else if (cr.form && k.startsWith("f-")) cr.form[k.slice(2)] = v;
}

crCore.onReadOnly(() => { if (typeof hubReposPaint === "function") hubReposPaint(); });

document.addEventListener("DOMContentLoaded", () => {
  const list = document.getElementById("hubrepos-list");
  if (!list) return;
  list.addEventListener("click", e => { const c = e.target.closest("[data-cr]"); if (c && !c.disabled) crClick(c); });
  list.addEventListener("input", e => { if (e.target.dataset && e.target.dataset.crf) crInput(e.target); });
});

// The hub says `change-request` on /v1/events (data {id,state,repo,title,source,target,owner}) on create and every state
// change, and settings-spine.js passes it here. Read again once the view has been read at all, never before: a board nobody
// opened the requests on does not fetch them for an event.
let crEventTimer = 0;
function crOnEvent() {
  if (!cr.loaded) return;
  clearTimeout(crEventTimer);
  crEventTimer = setTimeout(() => crLoad(), 300);
}
