// ── deploy ready, the board's half ───────────────────────────────────────
//
// The hub says whether what is on its branch can be deployed, and one click does it. `GET /_hub/deploy-ready` is the report
// (state ready, blocked, current or unknown, a `line` to show, the blocking commits and notes, and the state of a deploy that
// is running or has run) and `POST /_hub/deploy-ready/deploy {tip}` starts one. See changelog/runtime/2026-10-01-r-deploy-ready.md.
//
// A pill in the header says the line. It is read at load, when the event stream opens and on the hub's `deploy-ready` event,
// never on a timer. On a daemon that is not a hub the GET is a 404 and the pill stays hidden.
//
// THE DEPLOY BUTTON IS ENABLED ONLY WHEN IT CAN WORK: ready, no deploy running, and this board on the machine the hub runs on.
// Otherwise it is shown greyed with the reason. It asks before it goes, since it installs a binary every hook on the machine
// then runs. The hub has the last word, and a refusal (403 over a share, 409 not ready, the tip moved, one already running) is
// said and the report is read again.
//
// Commit subjects and paths are shown to anyone past the share's password, and they are somebody's text: every one is set with
// textContent and never as HTML.

let drReport = null;     // the last answer
let drNote = "";         // what the last refusal said
let drRemote = "";       // set when the hub said a deploy is only started from its own machine
let drErr = "";          // why the last read failed, "" when it did not. The dialog then has no report to show.
let drPosting = false;
let drQueue = null;      // the last deploy queue: landed commits not yet live, and which deploy each needs. null when unread.

// The queue is read when the dialog opens and whenever the report is, never for the pill. A failed read leaves no queue.
async function loadDeployQueue() {
  let r;
  try { r = await plainFetch("/_hub/deploy-queue"); } catch (e) { drQueue = null; return; }
  if (!r.ok) { drQueue = null; return; }
  try { drQueue = await r.json(); } catch (e) { drQueue = null; }
  const dlg = document.getElementById("deployready");
  if (dlg && dlg.open) paintDeployDialog();
}

// One row per commit, oldest first: the sha, the item, and which deploy it needs. Every string is somebody's text and is
// set with textContent.
function drQueueBox(q) {
  const box = drEl("div", "dr-queue");
  box.appendChild(drEl("div", "dr-h", "deploy queue"));
  box.appendChild(drEl("div", q.error ? "dr-err" : "dr-facts", q.line || q.error || ""));
  if ((q.unreported || []).length) {
    box.appendChild(drEl("p", "dr-note", "no commit reported by " + q.unreported.join(", ") + ", so each is taken as behind"));
  }
  (q.entries || []).forEach(e => {
    const row = drEl("div", "dr-qrow");
    row.dataset.needs = e.needs || "";
    row.appendChild(drEl("code", "dr-sha", e.short || String(e.sha || "").slice(0, 8)));
    row.appendChild(drEl("span", "dr-qitem", e.item || ""));
    const rooms = (e.rooms || []).length ? " (" + e.rooms.join(", ") + ")" : "";
    row.appendChild(drEl("span", "dr-qneeds", (e.needs || "") + rooms));
    row.appendChild(drEl("span", "dr-subject", e.subject || ""));
    box.appendChild(row);
  });
  return box;
}

// A board served from the hub's own machine. The hub checks it again and has the last word.
function drLoopback() {
  const h = location.hostname;
  return h === "localhost" || h === "127.0.0.1" || h === "[::1]" || h === "::1" || /\.localhost$/.test(h);
}

async function loadDeployReady() {
  const pill = document.getElementById("deploy-pill");
  if (!pill) return;
  if (typeof isGuest === "function" && isGuest()) { pill.hidden = true; return; }
  let r;
  try { r = await plainFetch("/_hub/deploy-ready"); } catch (e) { return drFailed("could not reach the hub"); }
  if (r.status === 404) { drReport = null; drErr = ""; pill.hidden = true; return; }   // a daemon that is not a hub
  if (!r.ok) {
    let why = "the hub answered " + r.status;
    try { why = (await r.json()).error || why; } catch (e) {}
    return drFailed(why);
  }
  let v;
  try { v = await r.json(); } catch (e) { return drFailed("the answer could not be read"); }
  drReport = v;
  drErr = "";
  paintDeployReady();
  const dlg = document.getElementById("deployready");
  if (dlg && dlg.open) loadDeployQueue();
}

// A read that failed leaves no report: the old one is not kept, since it may no longer be true, and the Deploy button goes
// with it. The pill says so, and an open dialog says why.
function drFailed(why) {
  drReport = null;
  drErr = String(why || "unknown");
  const pill = document.getElementById("deploy-pill");
  if (pill) {
    pill.hidden = false;
    pill.textContent = "no report";
    pill.dataset.state = "unknown";
    delete pill.dataset.short;
    pill.dataset.tip = "no report: " + drErr;
    pill.setAttribute("aria-label", "deploy: no report: " + drErr);
  }
  const dlg = document.getElementById("deployready");
  if (dlg && dlg.open) paintDeployDialog();
}

function drLine(v) { return String((v && v.line) || (v && v.state) || ""); }

// The pill's short form for a narrow header (css/topnav.css), a whole phrase and never a cut one.
function drShort(v, run) {
  if (run && run.state === "running") return "deploying";
  const m = /(\d+) of (\d+)/.exec(drLine(v));
  const word = String(v.state || "unknown") === "blocked" ? "not ready" : String(v.state || "unknown");
  return m ? word + " " + m[1] + "/" + m[2] : word;
}

function paintDeployReady() {
  const pill = document.getElementById("deploy-pill");
  if (!pill || !drReport) return;
  const v = drReport, run = v.deploy || {};
  pill.hidden = false;
  pill.textContent = run.state === "running" ? "deploying " + String(run.tip || "").slice(0, 8) : drLine(v);
  pill.dataset.state = run.state === "running" ? "running" : String(v.state || "unknown");
  pill.dataset.short = drShort(v, run);
  // `data-tip` is the board's tooltip (js/tooltips.js), and here it is the line in full, which the pill cuts short. It is not the
  // report's git `tip`.
  pill.dataset.tip = drLine(v);
  pill.setAttribute("aria-label", "deploy: " + drLine(v));
  const dlg = document.getElementById("deployready");
  if (dlg && dlg.open) paintDeployDialog();
}

function openDeployReady() {
  const dlg = document.getElementById("deployready");
  if (!dlg) return;
  drNote = "";
  paintDeployDialog();
  if (!dlg.open) dlg.showModal();
  loadDeployReady();
}

function closeDeployReady() {
  const dlg = document.getElementById("deployready");
  if (dlg && dlg.open) dlg.close();
}

// Why the button is grey, or "" when it is not.
function drWhyNot(v) {
  if (!v) return "no report yet";
  const run = v.deploy || {};
  if (run.state === "running") return "a deploy is already running";
  if (drRemote) return drRemote;
  if (!drLoopback()) return "a deploy is started only from the machine the hub runs on";
  if (!v.ready) return "not ready: " + drLine(v);
  return "";
}

function drEl(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
}

function drWhen(at) {
  const d = new Date(at);
  return isNaN(d) || d.getFullYear() < 2000 ? "" : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function paintDeployDialog() {
  const host = document.getElementById("dr-body");
  if (!host) return;
  if (!drReport) {
    // Nothing is known, so nothing is claimed, and there is no deploy to start.
    const box = drEl("div", "dr");
    box.appendChild(drEl("div", "dr-err", "no report: " + (drErr || "not read yet")));
    const row = drEl("div", "dr-act");
    const go = drEl("button", "go", "Deploy");
    go.id = "dr-deploy";
    go.type = "button";
    go.disabled = true;
    row.appendChild(go);
    const m = drEl("span", "dr-why-not", "no report");
    m.id = "dr-msg";
    row.appendChild(m);
    box.appendChild(row);
    host.replaceChildren(box);
    return;
  }
  const v = drReport, run = v.deploy || { state: "idle" };
  const box = drEl("div", "dr");
  const head = drEl("div", "dr-head");
  head.appendChild(drEl("span", "dr-state", String(v.state || "unknown")));
  head.appendChild(drEl("span", "dr-line", drLine(v)));
  box.appendChild(head);
  const facts = [];
  if (v.branch) facts.push(v.branch + (v.tip ? " at " + String(v.tip).slice(0, 8) : ""));
  if (v.installed) facts.push("installed " + String(v.installed).slice(0, 8));
  if (v.commits) facts.push(v.commits + (v.commits === 1 ? " commit" : " commits") + " to deploy");
  if (facts.length) box.appendChild(drEl("div", "dr-facts", facts.join(", ")));
  if (v.error) box.appendChild(drEl("div", "dr-err", v.error));

  const blocking = v.blocking || [];
  if (blocking.length) {
    const list = drEl("div", "dr-blocking");
    list.appendChild(drEl("div", "dr-h", "holding it up"));
    blocking.forEach(b => {
      const d = document.createElement("details");
      d.className = "dr-blocker";
      const s = document.createElement("summary");
      s.appendChild(drEl("code", "dr-sha", b.short || String(b.sha || "").slice(0, 8)));
      s.appendChild(drEl("span", "dr-subject", b.subject || ""));
      s.appendChild(drEl("span", "dr-why", b.why || ""));
      d.appendChild(s);
      if (b.detail) d.appendChild(drEl("p", "dr-detail", b.detail));
      list.appendChild(d);
    });
    box.appendChild(list);
  }
  if (drQueue) box.appendChild(drQueueBox(drQueue));
  const notes = v.notes || [];
  if (notes.length) {
    const n = drEl("div", "dr-notes");
    n.appendChild(drEl("div", "dr-h", "notes"));
    notes.forEach(t => n.appendChild(drEl("p", "dr-note", t)));
    box.appendChild(n);
  }

  // The deploy: idle, running, or finished with how it ended.
  const dep = drEl("div", "dr-deploy");
  dep.dataset.state = run.state || "idle";
  let said = "no deploy has run since the hub started";
  if (run.state === "running") said = "deploying " + String(run.tip || "").slice(0, 8) + (run.started ? " since " + drWhen(run.started) : "");
  else if (run.state === "finished") {
    said = "last deploy of " + String(run.tip || "").slice(0, 8) + " finished" + (run.exit != null ? ", exit " + run.exit : "") +
      (run.error ? ": " + run.error : "") + (run.ended ? " at " + drWhen(run.ended) : "");
  }
  dep.appendChild(drEl("span", "dr-dstate", run.state || "idle"));
  dep.appendChild(drEl("span", "dr-dsay", said));
  box.appendChild(dep);

  const why = drWhyNot(v);
  const row = drEl("div", "dr-act");
  const go = drEl("button", "go", drPosting ? "starting" : "Deploy");
  go.id = "dr-deploy";
  go.type = "button";
  go.disabled = !!why || drPosting;
  go.addEventListener("click", deployNow);
  row.appendChild(go);
  const msg = drEl("span", "dr-why-not", drNote || why);
  msg.id = "dr-msg";
  row.appendChild(msg);
  box.appendChild(row);
  host.replaceChildren(box);
}

// Asks, then posts the full tip the report showed. Whatever the hub answers is said, and the report is read again.
async function deployNow() {
  const v = drReport;
  if (!v || drWhyNot(v) || drPosting) return;
  const tip = String(v.tip || "");
  const ok = await confirmUser("Deploy " + tip.slice(0, 8) + "?",
    esc("This installs the build of " + (v.branch || "the branch") + " at " + tip.slice(0, 8) + " and restarts the hub. Every hook on this machine runs the new binary. " + drLine(v)),
    "deploy");
  if (!ok) return;
  drPosting = true;
  drNote = "";
  paintDeployDialog();
  try {
    const r = await plainFetch("/_hub/deploy-ready/deploy", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ tip }) });
    let body = null;
    try { body = await r.json(); } catch (e) {}
    if (r.status === 202 || r.ok) {
      if (body && body.deploy) drReport = Object.assign({}, drReport, { deploy: body.deploy });
      drNote = "deploy started";
    } else if (r.status === 403) {
      drRemote = (body && body.error) || "a deploy is started only from the machine the hub runs on";
      drNote = drRemote;
    } else if (r.status === 409) {
      // Not ready, the tip moved, or one is running: the hub's own report, when it sent one, is the truth now.
      drNote = (body && body.error) || "the hub could not deploy that";
      if (body && body.report) drReport = Object.assign({}, drReport, body.report);
      if (body && body.deploy) drReport = Object.assign({}, drReport, { deploy: body.deploy });
    } else {
      drNote = (body && body.error) || "the hub refused it (" + r.status + ")";
    }
  } catch (e) {
    drNote = "could not reach the hub";
  }
  drPosting = false;
  paintDeployReady();
  paintDeployDialog();
  loadDeployReady().then(() => { if (drNote && document.getElementById("dr-msg")) document.getElementById("dr-msg").textContent = drNote; });
}
