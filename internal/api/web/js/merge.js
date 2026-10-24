// The merge queue on the Terminals tab: a strip of what wants a person, and the whole queue and its history in a
// dialog behind one link from it. See docs/rnd/merge-queue.md, section "The board".
//
// HUB-ONLY. `/_hub/merge` is a 404 on a plain daemon, so nothing is asked until the hub probe in rooms.js has found a
// hub. It fetches when the Terminals tab draws (at most every MERGE_POLL_MS) and again on every `merge` event.
//
// A red or refused request wants a person until it is dismissed here or the branch is asked for again. The hub cannot
// tell whether its requester is still at work on it, so the strip errs on showing it and the dismissal is per browser.

const MERGE_POLL_MS = 30000;
const MERGE_DISMISS_KEY = "atrium.merge.dismissed";
const MERGE_WANT_DAYS = 3;
let mergeQ = { at: 0, requests: [], held: {}, room: "", why: "" };
let mergeOpenN = 0;
// The test output of a request, read from /_hub/merge/<n> when its row is opened, since the list leaves it out.
const mergeOutputs = {};

function mergeDismissed() {
  try {
    const d = JSON.parse(localStorage.getItem(MERGE_DISMISS_KEY) || "[]");
    return Array.isArray(d) ? d : [];
  } catch (e) { return []; }
}

function mergeDismiss(n) {
  const d = mergeDismissed().concat(n).slice(-200);
  try { localStorage.setItem(MERGE_DISMISS_KEY, JSON.stringify(d)); } catch (e) {}
  paintMergeStrip();
}

function mergeAge(at) {
  const t = Date.parse(at || "");
  return isNaN(t) ? "" : ago(Math.max(0, (Date.now() - t) / 1000));
}

function mergeOpenState(s) { return s === "queued" || s === "merging" || s === "testing" || s === "conflict"; }

// mergeWants is what a person can act on: a held repository, a conflict, and the newest red or refused request of a
// branch, newest first.
function mergeWants(q) {
  const out = [];
  for (const [repo, why] of Object.entries(q.held || {})) out.push({ held: repo, why });
  const seen = new Set(), hide = mergeDismissed();
  const old = Date.now() - MERGE_WANT_DAYS * 86400000;
  for (const m of q.requests || []) {
    const key = m.repo + " " + m.branch;
    if (seen.has(key)) continue;
    seen.add(key);
    if (m.state === "conflict") out.push({ m });
    else if ((m.state === "red" || m.state === "refused") && !hide.includes(m.n) && Date.parse(m.ended_at || "") > old) {
      out.push({ m });
    }
  }
  return out;
}

function mergeWhy(m) {
  if (m.state === "red" && m.failing && m.failing.length) return "failing " + m.failing.join(", ");
  if (m.state === "conflict") return (m.reason || "") + (m.merger ? ", merger " + m.merger : "");
  return m.reason || "";
}

// mergeLandRows is Land now with its count, for each repository with requests on claude/main no land carries.
function mergeLandRows(q) {
  const rows = [];
  for (const [repo, n] of Object.entries(q.unlanded || {})) {
    if (!n) continue;
    rows.push('<div class="mq-row mq-land"><span class="mq-text">' + n + (n === 1 ? " request" : " requests") +
      " on claude/main of " + esc(repo) + ' since the last land</span><button type="button" class="go" data-mq="land" ' +
      'data-repo="' + esc(repo) + '" data-tip="one commit of claude/main on the hub\'s main, by you">Land now (' +
      n + ")</button></div>");
  }
  return rows;
}

function paintMergeStrip() {
  const host = document.getElementById("term-merge");
  if (!host) return;
  const on = typeof hubIsHub !== "undefined" && hubIsHub;
  const wants = on ? mergeWants(mergeQ) : [];
  const open = on ? (mergeQ.requests || []).filter(m => mergeOpenState(m.state) && m.state !== "conflict") : [];
  const lands = on ? mergeLandRows(mergeQ) : [];
  host.hidden = wants.length === 0 && open.length === 0 && lands.length === 0;
  if (host.hidden) { host.replaceChildren(); return; }
  const rows = lands.concat(wants.map(w => {
    if (w.held) {
      return '<div class="mq-row mq-held"><span class="mq-text">merges in ' + esc(w.held) + " are held: " + esc(w.why) +
        '</span><button type="button" data-mq="queue">set the merge room</button></div>';
    }
    const m = w.m;
    const acts = '<button type="button" data-mq="show" data-n="' + m.n + '">output</button>' +
      (m.state === "conflict"
        ? '<button type="button" data-mq="cancel" data-n="' + m.n + '" data-tip="refuse it and stop its merger">give up</button>'
        : '<button type="button" data-mq="dismiss" data-n="' + m.n + '" data-tip="hide it in this browser">dismiss</button>');
    return '<div class="mq-row mq-' + esc(m.state) + '"><span class="mq-text">#' + m.n + " " + esc(m.branch) + " " +
      esc(m.state) + " " + esc(mergeAge(m.ended_at || m.started_at || m.asked_at)) + ": " + esc(mergeWhy(m)) +
      "</span>" + acts + "</div>";
  }));
  const testing = open.find(m => m.state === "merging" || m.state === "testing");
  const queued = open.filter(m => m.state === "queued").length;
  const line = (testing ? "testing " + esc(testing.branch) + (queued ? ", " : "") : "") + (queued ? queued + " queued" : "");
  rows.push('<div class="mq-row mq-sum"><span class="mq-text">merge queue' + (line ? ": " + line : "") +
    '</span><button type="button" data-mq="queue">queue and history</button></div>');
  host.innerHTML = rows.join("");
}

async function loadMerge(force) {
  if (typeof hubIsHub === "undefined" || !hubIsHub) { paintMergeStrip(); return; }
  if (!force && Date.now() - mergeQ.at < MERGE_POLL_MS) return;
  mergeQ.at = Date.now();
  const fetcher = typeof plainFetch === "function" ? plainFetch : window.fetch.bind(window);
  try {
    const res = await fetcher("/_hub/merge" + (mergeDlgOpen() ? "?all=1" : ""));
    if (!res.ok) throw new Error(String(res.status));
    const body = await res.json();
    mergeQ = { at: mergeQ.at, requests: body.requests || [], held: body.held || {}, room: body.room || "", why: body.why || "",
      unlanded: body.unlanded || {} };
  } catch (e) {
    // A hub mid-restart is not a finding. The strip keeps what it had and the next event or draw asks again.
  }
  paintMergeStrip();
  if (mergeDlgOpen()) paintMergeDlg();
}

function onMergeEvent() {
  for (const k of Object.keys(mergeOutputs)) delete mergeOutputs[k];
  loadMerge(true);
}

function mergeDlgOpen() {
  const d = document.getElementById("mergedlg");
  return !!(d && d.open);
}

function openMergeDlg(n) {
  const d = document.getElementById("mergedlg");
  if (!d) return;
  mergeOpenN = n || 0;
  if (!d.open) d.showModal();
  paintMergeDlg();
  loadMerge(true);
  if (mergeOpenN && !(mergeOpenN in mergeOutputs)) mergeLoadOutput(mergeOpenN);
}

async function mergeLoadOutput(n) {
  const fetcher = typeof plainFetch === "function" ? plainFetch : window.fetch.bind(window);
  try {
    const res = await fetcher("/_hub/merge/" + n);
    if (!res.ok) return;
    mergeOutputs[n] = (await res.json()).output || "";
  } catch (e) { return; }
  if (mergeDlgOpen()) paintMergeDlg();
}

function mergeRow(m) {
  const when = m.landed ? "landed " + m.landed.slice(0, 10) : esc(m.state);
  const head = '<button type="button" class="mq-h" data-mq="show" data-n="' + m.n + '">' +
    '<span class="mq-n">#' + m.n + '</span><span class="mq-state mq-' + esc(m.state) + '">' + when + "</span>" +
    '<span class="mq-branch">' + esc(m.branch) + '</span><span class="mq-who">' + esc(m.requester || "") +
    (m.requester_room ? "@" + esc(m.requester_room) : "") + '</span><span class="mq-age">' +
    esc(mergeAge(m.ended_at || m.asked_at)) + "</span></button>";
  if (m.n !== mergeOpenN) return '<div class="mq-item">' + head + "</div>";
  const times = [["asked", m.asked_at], ["started", m.started_at], ["tested", m.tested_at], ["ended", m.ended_at]]
    .filter(t => t[1]).map(t => t[0] + " " + new Date(t[1]).toLocaleString()).join(", ");
  const why = mergeWhy(m);
  const out = m.output || mergeOutputs[m.n] || "";
  return '<div class="mq-item open">' + head + '<div class="mq-detail"><div>' + esc(m.repo) +
    (m.sha ? " at " + esc(m.sha.slice(0, 10)) : "") + (m.room ? ", tested on " + esc(m.room) : "") + "</div><div>" + esc(times) + "</div>" +
    (why ? "<div>" + esc(why) + "</div>" : "") +
    (m.state === "queued" || m.state === "conflict" ? '<button type="button" data-mq="cancel" data-n="' + m.n + '">' +
      (m.state === "conflict" ? "give up" : "cancel") + "</button>" : "") +
    (out ? '<pre class="mq-out">' + esc(out) + "</pre>" : "") + "</div></div>";
}

function paintMergeDlg() {
  const list = document.getElementById("mergedlg-list");
  if (!list) return;
  const rs = mergeQ.requests || [];
  list.innerHTML = rs.length ? rs.map(mergeRow).join("") : '<p class="pane-lead">Nothing asked yet.</p>';
  const room = document.getElementById("mergedlg-room");
  const note = document.getElementById("mergedlg-using");
  if (note) note.textContent = mergeQ.room ? "tests run on " + mergeQ.room + (mergeQ.why ? ", " + mergeQ.why : "") : mergeQ.why || "";
  if (room && document.activeElement !== room) {
    const names = (typeof hubRooms !== "undefined" ? hubRooms : []).map(r => r.name);
    room.innerHTML = '<option value="">the hub\'s own room</option>' +
      names.map(n => '<option value="' + esc(n) + '">' + esc(n) + "</option>").join("");
    room.value = names.includes(mergeQ.room) ? mergeQ.room : "";
  }
}

async function mergePost(path, body) {
  const fetcher = typeof plainFetch === "function" ? plainFetch : window.fetch.bind(window);
  try {
    const res = await fetcher(path, {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body)
    });
    if (!res.ok) {
      let why = res.status + "";
      try { why = (await res.json()).error || why; } catch (e) {}
      toast("the merge queue said no", why);
    }
  } catch (e) {
    toast("the merge queue said no", "the hub is not answering");
  }
  loadMerge(true);
}

document.addEventListener("click", e => {
  const b = e.target && e.target.closest && e.target.closest("[data-mq]");
  if (!b) return;
  const n = Number(b.dataset.n) || 0;
  switch (b.dataset.mq) {
    case "queue": openMergeDlg(0); break;
    case "show":
      if (mergeDlgOpen() && n === mergeOpenN) { mergeOpenN = 0; paintMergeDlg(); } else openMergeDlg(n);
      break;
    case "dismiss": mergeDismiss(n); break;
    case "land":
      if (confirm("Land now: put claude/main of " + b.dataset.repo + " on the hub's main as one commit?")) {
        mergePost("/_hub/merge/land", { repo: b.dataset.repo });
      }
      break;
    case "cancel": mergePost("/_hub/merge/" + n + "/cancel", {}); break;
    case "room": {
      const sel = document.getElementById("mergedlg-room");
      mergePost("/_hub/merge/room", { room: sel ? sel.value : "", by: "board" });
      break;
    }
    case "close": { const d = document.getElementById("mergedlg"); if (d) d.close(); break; }
  }
});
