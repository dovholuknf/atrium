// A stand-in for @fabric's stage 5 change-request routes, for building the board before the hub has them. ONE FILE, SWAPPABLE:
// delete it, and the `crMock` lines in changereq-core.js, once the hub answers. It is used only when asked (`?crmock=1`, or
// `localStorage["atrium.crMock"] = "1"`), never silently, so a real board never shows these rows as if they were real.
//
// It follows the draft and adds nothing to it: the object shape, the 201/400/404/409 on create, `merged` for the operator
// with a 409 when the sha is not reachable from the target, and `pushed` on a single request. State lives in this page only.
(function () {
  "use strict";

  const now = Date.now();
  const ago = m => new Date(now - m * 60000).toISOString();
  const REPO = "github/netfoundry/atrium";
  const seed = () => [
    { id: "cr_7", repo: REPO, source: { room: "sg3", branch: "claude/fabric-hub-push", sha: "9c41d07aa11" }, target: { branch: "main" },
      title: "Hub receives pushes: store, pre-receive, push log", why: "Finished and reviewed at hub-ok. The hub holds main, so this goes there: the receive path and the push log are the whole of stage 1.\n\nTests ran on sg3 at this head.",
      change: "chg_41", state: "open", created_by: { room: "sg3", card: "c-4471" }, created_at: ago(95), closed_at: null, closed_by: null, note: "", merged_sha: null, owner: { room: "sg3", card: "c-4471" },
      _pushed: { state: "matches", hub_sha: "9c41d07aa11", room: "sg3", card: "c-4471", at: ago(98), released: false } },
    { id: "cr_6", repo: REPO, source: { room: "m1mini", branch: "claude/u-scm5", sha: "5be20a1cc02" }, target: { branch: "claude/landing" },
      title: "Board: pushed line and change requests", why: "The board half of stage 5. Built on a mock layer until the hub's answer shape lands.",
      change: "chg_44", state: "open", created_by: { room: "m1mini", card: "c-5120" }, created_at: ago(310), closed_at: null, closed_by: null, note: "", merged_sha: null, owner: { room: "m1mini", card: "c-5120" },
      _pushed: { state: "behind", hub_sha: "29efa25bb90", room: "m1mini", card: "c-5120", at: ago(420), released: false } },
    { id: "cr_5", repo: REPO, source: { room: "sg4", branch: "claude/runtime-forwarder", sha: "7d03e5c9f00" }, target: { branch: "main" },
      title: "Room forwarder with card tokens", why: "r-new-hub-remote: the stable hub remote on the agent listener and the card tokens in the environment.",
      change: "chg_39", state: "open", created_by: { room: "sg4", card: "c-4302" }, created_at: ago(1500), closed_at: null, closed_by: null, note: "", merged_sha: null, owner: { room: "sg4", card: "c-4302" },
      _pushed: { state: "diverged", hub_sha: "6a11f90ee21", room: "sg4", card: "c-4302", at: ago(1600), released: false } },
    { id: "cr_4", repo: REPO, source: { room: "sg3", branch: "claude/fabric-git-store", sha: "e40a3c2d001" }, target: { branch: "main" },
      title: "Hub git store and the one seed of main", why: "f-new-hub-git-store: the hub's store and the one seed of main.",
      change: "chg_36", state: "merged", created_by: { room: "sg3", card: "c-4290" }, created_at: ago(4300), closed_at: ago(3900), closed_by: { room: "sg3", card: "operator" }, note: "merged on the hub, re-signed, pushed", merged_sha: "e40a3c2d001", owner: { room: "sg3", card: "c-4290" },
      _pushed: { state: "ahead", hub_sha: "e40a3c2d001", room: "sg3", card: "c-4290", at: ago(4200), released: true } },
    { id: "cr_3", repo: REPO, source: { room: "m1mini", branch: "claude/u-mobile-pass" }, target: { branch: "claude/landing" },
      title: "Mobile pass: ledger strip and chart lows", why: "Landed on claude/landing another way.",
      change: "chg_33", state: "closed", created_by: { room: "m1mini", card: "c-5001" }, created_at: ago(2900), closed_at: ago(2700), closed_by: { room: "m1mini", card: "operator" }, note: "landed another way", merged_sha: null, owner: { room: "m1mini", card: "c-5001" },
      _pushed: { state: "matches", hub_sha: "147e7c7a001", room: "m1mini", card: "c-5001", at: ago(2950), released: false } },
    { id: "cr_2", repo: REPO, source: { room: "sg4", branch: "claude/scratch-spike" }, target: { branch: "main" },
      title: "Spike: sparse clone of the scm folder", why: "Not needed after the stage 2 decision.", change: "",
      state: "withdrawn", created_by: { room: "sg4", card: "c-4210" }, created_at: ago(6200), closed_at: ago(6000), closed_by: { room: "sg4", card: "c-4210" }, note: "", merged_sha: null, owner: { room: "sg4", card: "c-4210" },
      _pushed: { state: "not-pushed" } },
  ];
  let rows = seed();
  let n = 7;

  const RECORD = {
    chg_41: { Tested: "yes at 9c41d07 (go test ./internal/..., sg3, pass) . 0 commits since", Reviewed: "hub-ok by @review, covers 3 of 3 commits . room-ok: none yet", Open: "0" },
    chg_44: { Tested: "yes at 29efa25 . 2 commits since", Reviewed: "doc-ok by @review, covers 1 of 3 commits", Open: "1: swap the mock" },
    chg_39: { Tested: "yes at 6a11f90 . 4 commits since", Reviewed: "none yet", Open: "2: the replay, the token scope" },
    chg_36: { Tested: "yes at e40a3c2", Reviewed: "hub-ok by @review, covers 2 of 2 commits", Open: "0" },
    chg_33: { Tested: "yes at 147e7c7", Reviewed: "doc-ok by @review", Open: "0" },
  };

  const pub = r => { const o = Object.assign({}, r); delete o._pushed; return o; };
  const res = (status, body) => ({ ok: status >= 200 && status < 300, status, body, text: body ? JSON.stringify(body) : "", offline: false });
  const KNOWN = ["nope/"];   // a source branch starting with this is "not found", for the 404

  function handle(method, path, body) {
    const u = new URL(path, "http://x");
    const p = u.pathname, qs = u.searchParams;
    if (method === "GET" && p === "/_hub/git/pushed") {
      const hit = rows.find(r => r.source.branch === qs.get("branch"));
      return Promise.resolve(res(200, hit ? hit._pushed : { state: "not-pushed" }));
    }
    const m = /^\/_hub\/change-requests(?:\/([^/]+))?$/.exec(p);
    if (!m) return Promise.resolve(res(404, { error: "no such route" }));
    const id = m[1];
    if (method === "GET" && !id) {
      const st = qs.get("state") || "open";
      let l = rows.filter(r => st === "all" || (st === "open" ? r.state === "open" : r.state !== "open"));
      if (qs.get("repo")) l = l.filter(r => r.repo === qs.get("repo"));
      if (qs.get("target")) l = l.filter(r => r.target.branch === qs.get("target"));
      if (qs.get("room")) l = l.filter(r => r.source.room === qs.get("room") || (r.owner && r.owner.room === qs.get("room")));
      return Promise.resolve(res(200, { requests: l.map(pub) }));
    }
    if (method === "GET" && id) {
      const r = rows.find(x => x.id === id);
      return Promise.resolve(r ? res(200, Object.assign(pub(r), { pushed: r._pushed })) : res(404, { error: "no change request " + id }));
    }
    if (method === "POST" && !id) {
      const ok = ["repo", "source", "target", "title", "why", "change"];
      const bad = Object.keys(body || {}).find(k => !ok.includes(k));
      if (bad) return Promise.resolve(res(400, { error: "unknown field " + bad }));
      if (!body || !body.repo || !body.source || !body.source.branch || !body.target || !body.target.branch || !body.title) return Promise.resolve(res(400, { error: "repo, source, target and title are required" }));
      if (KNOWN.some(k => body.source.branch.startsWith(k))) return Promise.resolve(res(404, { error: "the hub has no branch " + body.source.branch }));
      const dup = rows.find(r => r.state === "open" && r.repo === body.repo && r.source.branch === body.source.branch && r.target.branch === body.target.branch);
      if (dup) return Promise.resolve(res(409, Object.assign(pub(dup), { error: "an open request for this source and target exists: " + dup.id })));
      const r = { id: "cr_" + (++n), repo: body.repo, source: body.source, target: body.target, title: body.title, why: body.why || "", change: body.change || "", state: "open",
        created_by: { room: body.source.room || "hub", card: "operator" }, created_at: new Date().toISOString(), closed_at: null, closed_by: null, note: "", merged_sha: null, owner: { room: body.source.room || "hub", card: "operator" },
        _pushed: { state: "matches", hub_sha: "0123456", room: body.source.room || "hub", card: "operator", at: new Date().toISOString(), released: false } };
      rows.unshift(r);
      return Promise.resolve(res(201, pub(r)));
    }
    if (method === "POST" && id) {
      const r = rows.find(x => x.id === id);
      if (!r) return Promise.resolve(res(404, { error: "no change request " + id }));
      if (r.state !== "open") return Promise.resolve(res(409, { error: id + " is already " + r.state }));
      const d = body && body.do;
      const stamp = (state, extra) => Object.assign(r, { state, closed_at: new Date().toISOString(), closed_by: { room: "m1mini", card: "operator" } }, extra || {});
      if (d === "close") stamp("closed", { note: (body.note || "").slice(0, 300) });
      else if (d === "withdraw") stamp("withdrawn");
      else if (d === "merged") {
        if (!/^[0-9a-f]{7,40}$/i.test(body.sha || "")) return Promise.resolve(res(400, { error: "sha must be 7 to 40 hex characters" }));
        if (/^0+$/.test(body.sha)) return Promise.resolve(res(409, { error: body.sha + " is not reachable from " + r.target.branch + " on the hub" }));
        stamp("merged", { merged_sha: body.sha });
      } else return Promise.resolve(res(400, { error: "unknown do " + d }));
      return Promise.resolve(res(200, pub(r)));
    }
    return Promise.resolve(res(405, { error: "not allowed" }));
  }

  window.crMock = { handle, record: r => RECORD[r.change] || null, reset: () => { rows = seed(); n = 7; }, repo: REPO };
})();
