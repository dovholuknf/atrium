// A hub's change-request routes for the headless sections, in the shape of @fabric's stage 5 draft, so the board's LIVE path
// (js/changereq-core.js talking to /_hub/change-requests) is tested and not the mock (js/changereq-mock.js, tested on its own).
// The repo is the one scripts/hubrepos-fixture.js makes (openziti/ziti), so a request's source branch is one of its pushed
// branches and the repos Ledger can say so.
//
//   const hub = require("./changereq-fixture.js")(Date.now());      hub.rows, hub.calls (what the board POSTed)
//   await hub.route(page, { mode: "ok" | "readonly" | "forbid" | "offline" | "old" | "empty" })
(function () {
  const REPO = "github/openziti/ziti";
  function make(now) {
    const ago = m => new Date(now - m * 60000).toISOString();
    const hubSha = "abcdef0123456789abcdef0123456789abcdef01";
    const rows = [
      { id: "cr_9", repo: REPO, source: { branch: "fix/router-link-flap" }, target: { branch: "main" }, title: "Fix the router link flap", why: "Finished and reviewed.\nTests ran on sg4 at this head.",
        change: "chg_9", state: "open", created_by: { room: "sg4", card: "c-101" }, created_at: ago(40), closed_at: null, closed_by: null, note: "", merged_sha: null, owner: { room: "sg4", card: "c-101" },
        pushed: { state: "matches", hub_sha: hubSha, room: "sg4", card: "c-101", at: ago(45), released: false } },
      { id: "cr_8", repo: REPO, source: { branch: "feat/edge-router-policies-v2" }, target: { branch: "main" }, title: "Edge router policies v2", why: "Needs a second look at the policy order.",
        change: "chg_8", state: "open", created_by: { room: "m1mini", card: "c-102" }, created_at: ago(200), closed_at: null, closed_by: null, note: "", merged_sha: null, owner: { room: "m1mini", card: "c-102" },
        pushed: { state: "behind", hub_sha: hubSha, room: "m1mini", card: "c-102", at: ago(210), released: false } },
      { id: "cr_7", repo: REPO, source: { room: "sg3", branch: "docs/quickstart-refresh" }, target: { branch: "release/1.x" }, title: "Quickstart refresh into the release line", why: "",
        change: "", state: "open", created_by: { room: "sg3", card: "c-104" }, created_at: ago(900), closed_at: null, closed_by: null, note: "", merged_sha: null, owner: { room: "sg3", card: "c-104" },
        pushed: { state: "diverged", hub_sha: hubSha, room: "sg3", card: "c-104", at: ago(950), released: false } },
      { id: "cr_6", repo: REPO, source: { branch: "chore/bump-go-1.25" }, target: { branch: "main" }, title: "Bump Go to 1.25", why: "Landed.", change: "chg_6", state: "merged",
        created_by: { room: "sg3", card: "c-106" }, created_at: ago(3000), closed_at: ago(2800), closed_by: { room: "sg3", card: "operator" }, note: "merged on the hub, re-signed", merged_sha: "e40a3c2d00112233445566778899aabbccddeeff", owner: { room: "sg3", card: "c-106" },
        pushed: { state: "ahead", hub_sha: hubSha, room: "sg3", card: "c-106", at: ago(3100), released: false } },
      { id: "cr_5", repo: REPO, source: { branch: "test/flaky-terminator-race" }, target: { branch: "main" }, title: "Terminator race test", why: "Superseded.", change: "", state: "withdrawn",
        created_by: { room: "sg3", card: "c-109" }, created_at: ago(5000), closed_at: ago(4900), closed_by: { room: "sg3", card: "c-109" }, note: "", merged_sha: null, owner: { room: "sg3", card: "c-109" },
        pushed: { state: "not-pushed" } },
      { id: "cr_4", repo: REPO, source: { branch: "fix/xgress-retransmit-window" }, target: { branch: "release/1.x" }, title: "Xgress retransmit window", why: "Landed another way.", change: "chg_4", state: "closed",
        created_by: { room: "sg4", card: "c-107" }, created_at: ago(6000), closed_at: ago(5900), closed_by: { room: "sg4", card: "operator" }, note: "landed another way", merged_sha: null, owner: { room: "sg4", card: "c-107" },
        pushed: { state: "matches", hub_sha: hubSha, room: "sg4", card: "c-107", at: ago(6100), released: false } },
    ];
    const state = { rows, calls: [], mode: "ok" };
    const pub = r => { const o = Object.assign({}, r); delete o.pushed; return o; };
    const json = (r, status, body) => r.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
    let n = 9;
    async function route(page, o) {
      state.mode = (o && o.mode) || "ok";
      await page.route(/\/_hub\/(change-requests|git\/pushed)/, async r => {
        const req = r.request(), u = new URL(req.url()), m = req.method();
        if (state.mode === "offline") return r.abort("failed");
        if (state.mode === "old") return r.fulfill({ status: 404, body: "no" });
        if (state.mode === "down") return json(r, 503, { error: "hub is restarting" });
        const body = m === "POST" ? JSON.parse(req.postData() || "{}") : null;
        if (m === "POST") state.calls.push({ path: u.pathname, body, card: req.headers()["x-atrium-card"] || req.headers()["x-atrium-card-room"] || "" });
        if (m === "POST" && state.mode === "readonly") return json(r, 403, { error: "this link is a read-only share" });
        if (m === "POST" && state.mode === "forbid") return json(r, 403, { error: "only the owner may withdraw this request" });
        if (u.pathname === "/_hub/git/pushed") { const b = u.searchParams.get("branch"); const x = rows.find(q => q.source.branch === b); return json(r, 200, x ? x.pushed : { state: "not-pushed" }); }
        const id = (/change-requests\/([^/]+)$/.exec(u.pathname) || [])[1];
        if (m === "GET" && !id) {
          const st = u.searchParams.get("state") || "open";
          const l = state.mode === "empty" ? [] : rows.filter(x => st === "all" || (st === "open" ? x.state === "open" : x.state !== "open"));
          return json(r, 200, { requests: l.map(pub) });
        }
        if (m === "GET") { const x = rows.find(q => q.id === id); return x ? json(r, 200, Object.assign(pub(x), { pushed: x.pushed })) : json(r, 404, { error: "no such request" }); }
        if (!id) {
          if (body.source && /^nope\//.test(body.source.branch)) return json(r, 404, { error: "no branch " + body.source.branch });
          const dup = rows.find(q => q.state === "open" && q.source.branch === body.source.branch && q.target.branch === body.target.branch);
          if (dup) return json(r, 409, pub(dup));
          const x = { id: "cr_" + (++n), repo: body.repo, source: body.source, target: body.target, title: body.title, why: body.why || "", change: "", state: "open", created_by: { room: "m1mini", card: "operator" },
            created_at: new Date().toISOString(), closed_at: null, closed_by: null, note: "", merged_sha: null, owner: { room: "m1mini", card: "operator" }, pushed: { state: "matches", hub_sha: hubSha, room: "sg4", card: "c-101", at: ago(5), released: false } };
          rows.unshift(x); return json(r, 201, pub(x));
        }
        const x = rows.find(q => q.id === id);
        if (!x) return json(r, 404, { error: "no such request" });
        if (body.do === "merged") {
          if (/^0+$/.test(body.sha)) return json(r, 409, { error: body.sha + " is not reachable from " + x.target.branch });
          Object.assign(x, { state: "merged", merged_sha: body.sha, closed_at: new Date().toISOString(), closed_by: { room: "m1mini", card: "operator" } });
        } else if (body.do === "withdraw") Object.assign(x, { state: "withdrawn", closed_at: new Date().toISOString(), closed_by: { room: "m1mini", card: "operator" } });
        else if (body.do === "close") Object.assign(x, { state: "closed", note: body.note || "", closed_at: new Date().toISOString(), closed_by: { room: "m1mini", card: "operator" } });
        return json(r, 200, pub(x));
      });
    }
    return Object.assign(state, { route, REPO });
  }
  if (typeof module !== "undefined") module.exports = make;
})();
