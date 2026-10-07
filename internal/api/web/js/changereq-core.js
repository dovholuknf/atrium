// Change requests between rooms: the one place the board talks to the hub about them, and the words both the desktop
// board and the phone use for what comes back. Stage 5 of docs/fabric/hub-forge-design.md section 6.
//
// THE HUB'S SIDE IS A DRAFT (@fabric's stage 5 shape) and live data only exists after a hub deploy. Every call here goes
// through `call`, which answers from `window.crMock` (js/changereq-mock.js) when the mock is on and from the hub when it is
// not. The mock is ON only when the URL asks (`?crmock=1`), for that load alone, and a fixed banner says so for as long as it
// is on. Swapping it out is deleting that file and the `crMock` lines in `call`. Nothing here invents behaviour the draft does not name.
//
//   GET  /_hub/change-requests?state=open|closed|all&room=&target=&repo=   -> { requests: [...] }
//   GET  /_hub/change-requests/<id>                                       -> the request plus `pushed`
//   POST /_hub/change-requests {repo, source:{room?,branch}, target:{branch}, title, why, change?}
//   POST /_hub/change-requests/<id> {do:"close",note} | {do:"withdraw"} | {do:"merged",sha}    (merged: operator only)
//   GET  /_hub/git/pushed?repo=&branch=&head=   -> { state: matches|behind|ahead|diverged|not-pushed, hub_sha, room, card, at, released }
//
// TITLE AND WHY ARE DATA. Nothing here puts them in markup; the callers set them as text.
// THE BOARD NEVER MERGES. `merged` records that the operator did it on the hub's side. No word in this file says otherwise.
(function () {
  "use strict";

  // The hub's ValidSHA: a full sha, 40 hex (sha-1) or 64 (sha-256). The board agrees with the hub, not a looser rule.
  const SHA_RE = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/i;
  const SHA_HINT = "A sha is the full 40 (or 64) hex characters.";

  // Only the URL turns the mock on, and only for that load: nothing is remembered, so a reload without the parameter is live.
  function mockOn() {
    try { return /[?&]crmock=1(&|$)/.test(location.search); } catch (e) { return false; }
  }
  // Leaves the mock: the same page without the parameter.
  function mockOff() {
    try {
      const u = new URL(location.href);
      u.searchParams.delete("crmock");
      location.assign(u.toString());
    } catch (e) {}
  }
  // While the mock is on, a banner says nothing here reaches the hub. It sits on every page that loads this file.
  function mockBanner() {
    if (!mockOn() || document.getElementById("cr-mockbar")) return;
    const bar = document.createElement("div");
    bar.id = "cr-mockbar"; bar.className = "cr-mockbar"; bar.setAttribute("role", "status");
    const t = document.createElement("span"); t.textContent = "Mock data: nothing here reaches the hub.";
    const b = document.createElement("button"); b.type = "button"; b.textContent = "Turn off"; b.addEventListener("click", mockOff);
    bar.appendChild(t); bar.appendChild(b);
    document.body.appendChild(bar);
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", mockBanner); else mockBanner();

  // { ok, status, body, text, offline }. A thrown fetch is `offline`: the hub is not answering at all.
  async function call(method, path, body) {
    if (mockOn() && window.crMock) return window.crMock.handle(method, path, body);
    let res;
    try {
      res = await fetch(path, { method, headers: body ? { "Content-Type": "application/json" } : undefined, body: body ? JSON.stringify(body) : undefined });
    } catch (e) { return { ok: false, status: 0, body: null, text: "", offline: true }; }
    let text = "";
    try { text = await res.text(); } catch (e) {}
    let json = null;
    try { json = JSON.parse(text); } catch (e) {}
    return { ok: res.ok, status: res.status, body: json, text, offline: false };
  }

  // The words for a failed answer, the server's own when it sent any.
  function errText(r) {
    const b = r && r.body;
    const s = b && (b.error || b.message) ? String(b.error || b.message) : r && r.text ? String(r.text).trim() : "";
    return s.slice(0, 300) || (r && r.status ? "the hub answered " + r.status : "the hub did not answer");
  }

  // Writes are the operator's. A 401, or a 403 whose own words say the board is read-only, is how a read-only view is known,
  // and it stays known for this page, so the buttons go and a line says why. Any other 403 belongs to that action (a withdraw
  // by someone who is not the owner, say) and is shown as its error. See `readOnlyLine`.
  const state = { readOnly: false };
  const listeners = [];
  const readOnlyWords = r => /\bread[- ]only\b|\bpublic share\b/i.test(errText(r));
  function noteWrite(r) {
    if (r && (r.status === 401 || (r.status === 403 && readOnlyWords(r))) && !state.readOnly) {
      state.readOnly = true;
      listeners.forEach(f => { try { f(); } catch (e) {} });
    }
    return r;
  }
  const readOnlyLine = "This board cannot change requests: it is a public share without a login. Open the board as the operator to close, withdraw or record a merge.";

  const q = o => {
    const p = Object.entries(o || {}).filter(([, v]) => v !== undefined && v !== null && v !== "").map(([k, v]) => k + "=" + encodeURIComponent(v));
    return p.length ? "?" + p.join("&") : "";
  };
  const api = {
    list: o => call("GET", "/_hub/change-requests" + q(o)),
    get: id => call("GET", "/_hub/change-requests/" + encodeURIComponent(id)),
    pushed: (repo, branch, head) => call("GET", "/_hub/git/pushed" + q({ repo: canonRepo(repo), branch, head })),
    create: b => call("POST", "/_hub/change-requests", b).then(noteWrite),
    act: (id, b) => call("POST", "/_hub/change-requests/" + encodeURIComponent(id), b).then(noteWrite),
  };

  // ---- words ---------------------------------------------------------------------------------------------------------
  const STATE = { open: "Open", merged: "Merged", closed: "Closed", withdrawn: "Withdrawn" };
  const STATE_TONE = { open: "teal", merged: "blue", closed: "dim", withdrawn: "dim" };
  const PUSHED_TONE = { matches: "teal", behind: "warn", ahead: "blue", diverged: "danger", "not-pushed": "dim" };
  const PUSHED_SHORT = { matches: "pushed, hub head matches", behind: "hub is behind", ahead: "hub is ahead", diverged: "hub and room diverged", "not-pushed": "not pushed" };

  const sha7 = s => String(s || "").slice(0, 7);
  const isSha = s => SHA_RE.test(String(s || "").trim());
  // The hub names a repo host/owner/repo and answers o/r as github/o/r, so a repo typed or compared here is put the same way.
  const canonRepo = r => { const s = String(r || "").trim(); return s && s.split("/").length === 2 ? "github/" + s : s; };
  const repoShort = r => String(r || "").replace(/^github\//, "");
  // Open and aimed at main: stage 5 is manual, so someone merges on the hub's side and records it.
  const needsOperator = r => !!r && r.state === "open" && r.target && r.target.branch === "main";
  const isOpen = r => !!r && r.state === "open";
  const sourceLabel = r => (r.source && r.source.room ? r.source.room + " " : "hub ") + (r.source ? r.source.branch : "");
  const sourceRoom = r => (r.source && r.source.room) || (r.created_by && r.created_by.room) || "hub";

  // The change record's "Pushed" line, from the hub's push log. `p` is the pushed answer, or null when it has not been read.
  function pushedLine(p, branch) {
    if (!p) return "";
    const h = sha7(p.hub_sha);
    if (p.state === "matches") return "pushed at " + h + ", hub head matches";
    if (p.state === "behind") return "pushed at " + h + ", hub is behind";
    if (p.state === "ahead") return "pushed, hub is ahead at " + h;
    if (p.state === "diverged") return "pushed, hub is at " + h + " and the room has moved apart from it";
    if (p.state === "not-pushed") return "not pushed: the hub has never seen " + (branch ? branch : "this branch");
    return "";
  }
  // What to do about a branch the push log has not seen, as a sentence.
  const notPushedHow = branch => "Push it from its room with git push hub " + (branch || "<branch>") + ", and the push log picks it up.";

  // The request's own history, from the fields the hub keeps: created, then how it ended. Newest last.
  function timeline(r, p) {
    const ev = [];
    const by = x => x ? (x.card === "operator" ? "the operator" : (x.room || "") + (x.card ? " " + x.card : "")).trim() : "";
    ev.push({ at: r.created_at, kind: "open", who: by(r.created_by), text: "opened into " + (r.target ? r.target.branch : "") });
    if (p && p.at && p.state !== "not-pushed") ev.push({ at: p.at, kind: "push", who: (p.room || "") + (p.card ? " " + p.card : ""), text: "pushed " + sha7(p.hub_sha) + " to the hub" });
    if (r.state !== "open") {
      const word = { merged: "recorded as merged" + (r.merged_sha ? " at " + sha7(r.merged_sha) : ""), closed: "closed", withdrawn: "withdrawn" }[r.state] || r.state;
      ev.push({ at: r.closed_at, kind: r.state, who: by(r.closed_by) || (r.state === "withdrawn" ? by(r.created_by) : ""), text: word, note: r.note || "" });
    }
    return ev.filter(e => e.at).sort((a, b) => Date.parse(a.at) - Date.parse(b.at));
  }

  // The list grouped by target branch, `main` first, each group newest first.
  function byTarget(reqs) {
    const g = {};
    reqs.forEach(r => { (g[r.target.branch] = g[r.target.branch] || []).push(r); });
    Object.values(g).forEach(l => l.sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at)));
    return Object.keys(g).sort((a, b) => (a === "main" ? -1 : b === "main" ? 1 : a.localeCompare(b))).map(k => ({ target: k, reqs: g[k] }));
  }

  // The four lines a recorded change would show. The hub does not serve a record to the board yet, so in live mode this is
  // null and the page shows the record's id and the Pushed line only. The mock supplies lines so the page can be seen full.
  function recordLines(r) { return mockOn() && window.crMock ? window.crMock.record(r) : null; }

  window.crCore = {
    mockOn, mockOff, mockBanner, SHA_HINT, call, api, errText, state, readOnlyLine, onReadOnly: f => listeners.push(f),
    STATE, STATE_TONE, PUSHED_TONE, PUSHED_SHORT, sha7, isSha, canonRepo, repoShort, needsOperator, isOpen, sourceLabel, sourceRoom,
    pushedLine, notPushedHow, timeline, byTarget, recordLines,
  };
})();
