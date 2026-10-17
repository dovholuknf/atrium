// changereq: the Requests view of the repos page (js/changereq.js), one small unit for each thing it does.
//
// Units: changeReqList, changeReqBehind, changeReqDiverged, changeReqShaRule, changeReqRefused, changeReqRecord,
// changeReqClosedMerged, changeReqUnseenBranch, changeReqWithdraw, changeReqClose, changeReqReadOnly, changeReqStates,
// changeReqLedgerRows, changeReqOpen, changeReqText, changeReqStateWord, changeReqMock, changeReqMockOff,
// changeReqCanon, changeReqShareForbid, changeReqOwnForbid, changeReqOneRead, changeReqPhone.
// `units` is what the harness runs, in suite order. `sections` is for HEADLESS_ONLY, which names a section and not a unit.
// Alone: `node scripts/board-tests/changereq.js` runs just these units, the way the full run would.
// Every unit loads an address and waits on `data-route="ready"` on <html>, and on nothing timed. The contrast check and the
// screenshots stay in the harness as the `changeReq` unit, because they measure what the page draws.
const { slow } = require("../lib/browser.js");
const { mock } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");
const { hubReposNoOverflow } = require("../lib/helpers.js");

const CR_FULL = "e40a3c2d00112233445566778899aabbccddeeff", CR_ZERO = "0".repeat(40), CR_64 = "ab".repeat(32);
const CR_SHAS = [["xyz", false], ["e40a3c2", false], [CR_FULL.slice(1), false], [CR_FULL + "0", false], [CR_FULL.replace("e", "g"), false], [CR_FULL.toUpperCase(), true], [CR_FULL, true], [CR_64, true], [CR_64.slice(1), false]];

const REQ = "#/hubrepos/requests/";
const ready = p => p.waitForFunction(() => document.documentElement.dataset.route === "ready", null, { timeout: slow(15000) });
const settled = p => p.waitForFunction(() => hubRepos.loaded && !hubRepos.inflight && !cr.inflight, null, { timeout: slow(10000) });
const text = (p, sel) => p.evaluate(s => (document.querySelector(s) || {}).textContent || "", sel);

async function openAt(browser, base, hash, o) {
  o = o || {};
  const fx = require("../hubrepos-fixture.js")(Date.now());
  const hub = require("../changereq-fixture.js")(Date.now());
  if (o.mutate) o.mutate(hub);
  const ctx = await browser.newContext({ viewport: { width: o.w || 2000, height: o.h || 1100 } });
  const errors = [];
  if (o.init) await ctx.addInitScript(o.init);
  const p = await ctx.newPage();
  p.on("pageerror", e => errors.push(e.message));
  await p.route(/\/_hub\/git\/repos$/, r => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ repos: fx.repos }) }));
  if (!o.noHub) await hub.route(p, { mode: o.mode || "ok" });
  await p.goto(base + "/" + (o.query || "") + hash, { waitUntil: "domcontentloaded" });
  await ready(p);
  await settled(p);
  return { ctx, p, errors, hub, fx };
}

const sectionOf = (name, body) => async (browser, base) => {
  const was = mock.hubMode; mock.hubMode = true;
  try { await body(browser, base, name); } finally { mock.hubMode = was; }
};

const withPage = (name, hash, o, body) => sectionOf(name, async (browser, base, tag) => {
  const { ctx, p, errors, hub } = await openAt(browser, base, hash, o);
  try {
    await body(p, tag, hub);
    if (errors.length) fail(tag + ": page errors: " + errors.join(" | "));
  } finally { await ctx.close(); }
});

const sections = {};
const add = (name, fn) => { sections[name] = fn; };

add("changeReqList", withPage("changeReqList", REQ + "open", {}, async (p, tag) => {
  const r = await p.evaluate(() => ({
    count: document.getElementById("hubrepos-count").textContent,
    strip: [...document.querySelectorAll(".cr-hero")].map(h => h.querySelector("h3").textContent),
    items: [...document.querySelectorAll(".cr-li")].map(b => b.dataset.id), groups: [...document.querySelectorAll(".cr-tg span")].map(x => x.textContent),
    needs: document.querySelectorAll(".cr-li .cr-needs").length, cur: [...document.querySelectorAll('.cr-li[aria-current="true"]')].map(b => b.dataset.id),
    title: (document.querySelector(".cr-title") || {}).textContent, lane: (document.querySelector(".cr-lane") || {}).innerText.replace(/\s+/g, " "),
    pills: [...document.querySelectorAll(".cr-detail .hr-badges .hr-state")].map(x => x.textContent.trim()), gate: (document.querySelector(".cr-gate") || {}).textContent,
    panels: [...document.querySelectorAll(".cr-detail .cr-panel .hr-h")].map(x => x.textContent), why: (document.querySelector(".cr-why") || {}).textContent,
    pushedLine: (document.querySelector(".cr-line") || {}).textContent, tl: document.querySelectorAll(".cr-tl li").length }));
  const views = await p.evaluate(() => [...document.querySelectorAll("#hubrepos-views [role=radio]")].map(b => b.dataset.hrview + ":" + b.getAttribute("aria-checked")).join());
  if (views !== "shelf:false,ledger:false,feed:false,requests:true,branches:false") fail(tag + ": the switcher is wrong " + views);
  if (r.count !== "3") fail(tag + ": the count is the open requests, want 3, got " + r.count);
  if (r.strip.join("|") !== "Fix the router link flap|Edge router policies v2") fail(tag + ": the strip is not the open requests into main " + JSON.stringify(r.strip));
  if (r.items.join() !== "cr_9,cr_8,cr_7" || r.groups.join() !== "into main,into release/1.x" || r.needs !== 2 || r.cur.join() !== "cr_9") fail(tag + ": the list is wrong " + JSON.stringify(r));
  if (r.title !== "Fix the router link flap" || !/^from sg4?\s*fix\/router-link-flap.*into main$/i.test(r.lane.replace(/\n/g, " "))) fail(tag + ": the page head is wrong " + JSON.stringify([r.title, r.lane]));
  if (r.pills.join() !== "Open,pushed, hub head matches") fail(tag + ": the state and Pushed pills are wrong " + r.pills);
  if (!/Needs the orchestrator or clint/.test(r.gate) || !/Nothing on the board does it/.test(r.gate)) fail(tag + ": a request into main does not say it needs the orchestrator or clint " + r.gate);
  if (r.panels.join() !== "Why,On the hub,Change record,History" || r.tl < 2) fail(tag + ": the page does not fill " + JSON.stringify([r.panels, r.tl]));
  if (r.pushedLine !== "Pushedpushed at abcdef0, hub head matches") fail(tag + ": the Pushed line is wrong " + JSON.stringify(r.pushedLine));
  if (!/Finished and reviewed\.\nTests ran/.test(r.why)) fail(tag + ": the why lost its line break " + JSON.stringify(r.why));
  const buttons = await p.evaluate(() => [...document.querySelectorAll("#hubrepos-list button")].map(b => b.textContent.trim()).filter(Boolean));
  const merge = buttons.filter(b => /merge/i.test(b));
  if (merge.join() !== "Record that it was merged") fail(tag + ": merge wording on a button is " + JSON.stringify(merge));
  if (/\b(merge now|merge it|merge request)\b/i.test((await text(p, "#hubrepos-list")).replace(/Merge it on the hub's side/g, ""))) fail(tag + ": the page says the board merges");
}));

add("changeReqBehind", withPage("changeReqBehind", REQ + "open/cr_8", {}, async (p, tag) => {
  if (!/hub is behind/.test(await text(p, ".cr-detail .hr-badges"))) fail(tag + ": the pill does not say behind");
  if (!/pushed at abcdef0, hub is behind/.test(await text(p, ".cr-line"))) fail(tag + ": behind is not said " + await text(p, ".cr-line"));
}));

add("changeReqDiverged", withPage("changeReqDiverged", REQ + "open/cr_7", {}, async (p, tag) => {
  if (!/diverged/.test(await text(p, ".cr-detail .hr-badges"))) fail(tag + ": the pill does not say diverged");
  const d = await p.evaluate(() => ({ gate: document.querySelector(".cr-gate > b").textContent, why: [...document.querySelectorAll(".cr-detail .cr-quiet")].map(x => x.textContent).join("|") }));
  if (d.gate !== "Waiting to be merged" || !/No reason was given/.test(d.why) || !/No change record is attached/.test(d.why)) fail(tag + ": a request with no why or record reads wrong " + JSON.stringify(d));
}));

add("changeReqShaRule", withPage("changeReqShaRule", REQ + "open/cr_9", {}, async (p, tag) => {
  const dis = () => p.evaluate(() => ({ off: document.querySelector('[data-cr="merge-do"]').disabled, bad: document.getElementById("cr-shahint").classList.contains("bad"), hint: document.getElementById("cr-shahint").textContent }));
  let s = await dis();
  if (!s.off) fail(tag + ": record merged is enabled with no sha");
  for (const [v, ok] of CR_SHAS) {
    for (let i = 0; i < 5; i++) { await p.fill("#cr-sha", v); if (await p.evaluate(v => cr.draft.sha === v, v)) break; }
    s = await dis();
    if (s.off === ok) fail(tag + ": sha " + JSON.stringify(v) + " should be " + (ok ? "accepted" : "refused"));
    if (v && !ok && !s.bad) fail(tag + ": a bad sha has no hint " + v);
    if (v && !ok && !/full 40 \(or 64\) hex/.test(s.hint)) fail(tag + ": the hint does not say the rule " + s.hint);
  }
}));

add("changeReqRefused", withPage("changeReqRefused", REQ + "open/cr_9", {}, async (p, tag, hub) => {
  await p.fill("#cr-sha", CR_ZERO);
  await p.click('[data-cr="merge-do"]');
  await p.waitForSelector(".cr-err");
  if ((await text(p, ".cr-err")) !== CR_ZERO + " is not reachable from main") fail(tag + ": the server's 409 text is not shown " + await text(p, ".cr-err"));
  if (!/Needs the orchestrator/.test(await text(p, ".cr-gate"))) fail(tag + ": a refused record changed the request");
  const post = hub.calls.filter(c => c.body.do === "merged").map(c => JSON.stringify(c.body));
  if (post.join() !== '{"do":"merged","sha":"' + CR_ZERO + '"}') fail(tag + ": the board posted " + post);
}));

add("changeReqRecord", withPage("changeReqRecord", REQ + "open/cr_9", {}, async (p, tag, hub) => {
  await p.fill("#cr-sha", CR_FULL);
  await p.click('[data-cr="merge-do"]');
  await p.waitForFunction(() => /Recorded as merged at e40a3c2/.test((document.querySelector(".cr-ended") || {}).textContent || ""), null, { timeout: slow(10000) });
  const post = hub.calls.filter(c => c.body.do === "merged").map(c => JSON.stringify(c.body));
  if (post.join() !== '{"do":"merged","sha":"' + CR_FULL + '"}') fail(tag + ": the board posted " + post);
  if (await p.evaluate(() => !!document.querySelector(".cr-gate"))) fail(tag + ": a merged request still has the gate");
  if (await p.evaluate(() => document.querySelectorAll('.cr-li[data-id="cr_9"]').length)) fail(tag + ": a merged request is still in the open list");
  await p.click('.cr-seg [data-tab="closed"]');
  const cl = await p.evaluate(() => [...document.querySelectorAll(".cr-li")].map(b => b.dataset.id).join());
  if (cl !== "cr_9,cr_6,cr_5,cr_4") fail(tag + ": the closed list is " + cl);
}));

add("changeReqClosedMerged", withPage("changeReqClosedMerged", REQ + "closed/cr_6", {}, async (p, tag) => {
  const m = await p.evaluate(() => ({ ended: document.querySelector(".cr-ended").innerText.replace(/\s+/g, " "), tl: [...document.querySelectorAll(".cr-tl li")].map(x => x.innerText.replace(/\s+/g, " ")) }));
  if (!/Recorded as merged at e40a3c2 by the operator/.test(m.ended) || !/merged on the hub, re-signed/.test(m.ended)) fail(tag + ": a merged request does not say how it ended " + JSON.stringify(m.ended));
  if (m.tl.length !== 3 || !/pushed abcdef0 to the hub/.test(m.tl[0]) || !/opened into main/.test(m.tl[1]) || !/recorded as merged at e40a3c2/.test(m.tl[2])) fail(tag + ": the history is wrong " + JSON.stringify(m.tl));
}));

add("changeReqUnseenBranch", withPage("changeReqUnseenBranch", REQ + "closed/cr_5", {}, async (p, tag) => {
  const w = await p.evaluate(() => ({ ended: document.querySelector(".cr-ended").textContent, line: document.querySelector(".cr-line").textContent, how: [...document.querySelectorAll(".cr-detail .cr-quiet")].map(x => x.textContent).join("|"), pill: [...document.querySelectorAll(".cr-detail .hr-badges .hr-state")].map(x => x.textContent.trim()).join() }));
  if (!/Withdrawn/.test(w.ended) || /by its owner/.test(w.ended) || !/never seen test\/flaky-terminator-race/.test(w.line) || !/git push hub test\/flaky-terminator-race/.test(w.how) || w.pill !== "Withdrawn,not pushed") fail(tag + ": an unseen branch reads wrong " + JSON.stringify(w));
}));

add("changeReqWithdraw", withPage("changeReqWithdraw", REQ + "open/cr_9", {}, async (p, tag, hub) => {
  await p.click('[data-cr="withdraw"]');
  await p.waitForFunction(() => /Withdrawn/.test((document.querySelector(".cr-ended") || {}).textContent || ""), null, { timeout: slow(10000) });
  if (hub.calls.map(c => JSON.stringify(c.body)).join() !== '{"do":"withdraw"}') fail(tag + ": posted " + hub.calls.map(c => JSON.stringify(c.body)));
}));

add("changeReqClose", withPage("changeReqClose", REQ + "open/cr_8", {}, async (p, tag, hub) => {
  await p.click('[data-cr="close-open"]');
  await p.fill("#cr-note", "landed on the other branch");
  await p.click('[data-cr="close-do"]');
  await p.waitForFunction(() => /Closed/.test((document.querySelector(".cr-ended") || {}).textContent || ""), null, { timeout: slow(10000) });
  if (hub.calls.map(c => JSON.stringify(c.body)).join() !== '{"do":"close","note":"landed on the other branch"}') fail(tag + ": posted " + hub.calls.map(c => JSON.stringify(c.body)));
  if (!/landed on the other branch/.test(await text(p, ".cr-ended"))) fail(tag + ": the close note is not shown");
}));

add("changeReqReadOnly", withPage("changeReqReadOnly", REQ + "open/cr_9", { mode: "readonly" }, async (p, tag) => {
  await p.click('[data-cr="withdraw"]');
  await p.waitForSelector(".cr-ro");
  const ro = await p.evaluate(() => ({ line: document.querySelector(".cr-ro").textContent, btns: [...document.querySelectorAll("#hubrepos-list button")].map(b => b.dataset.cr).filter(Boolean).filter(x => x !== "sel" && x !== "tab"), input: !!document.getElementById("cr-sha"), open: !!document.querySelector('.cr-bar [data-cr="new"]') }));
  if (!/public share without a login/.test(ro.line) || ro.btns.length || ro.input || ro.open) fail(tag + ": a read-only board still offers writes " + JSON.stringify(ro));
}));

add("changeReqStates", sectionOf("changeReqStates", async (browser, base, tag) => {
  for (const [mode, want] of [["empty", /Nothing is waiting to be merged/], ["offline", /hub is not answering/], ["down", /would not list change requests: hub is restarting/], ["old", /does not have change requests yet/]]) {
    const { ctx, p, errors } = await openAt(browser, base, REQ + "open", { mode });
    try {
      const t = await text(p, "#hubrepos-list");
      if (!want.test(t)) fail(tag + " " + mode + ": reads " + t.slice(0, 200));
      if (mode === "empty" && !(await p.$('.hr-hero [data-cr="new"]'))) fail(tag + " empty: no way to open a request");
      if (mode !== "empty" && !(await p.$('[data-cr="retry"]'))) fail(tag + " " + mode + ": no try again");
      if (errors.length) fail(tag + " " + mode + ": page errors: " + errors.join(" | "));
    } finally { await ctx.close(); }
  }
}));

add("changeReqLedgerRows", withPage("changeReqLedgerRows", "#/hubrepos/ledger", {}, async (p, tag) => {
  const rows = await p.evaluate(() => [...document.querySelectorAll(".hr-ev")].map(e => ({ name: (e.querySelector(".hr-name") || {}).textContent, chip: (e.querySelector(".cr-chip") || {}).textContent || "" })));
  const flap = rows.find(x => x.name === "fix/router-link-flap"), docs = rows.find(x => x.name === "feat/metrics-exemplars"), other = rows.find(x => x.name === "docs/quickstart-refresh"), old = rows.find(x => x.name === "refactor/identity-store-batching");
  if (!flap || flap.chip !== "cr_9 · into main") fail(tag + ": a branch with an open request does not show it " + JSON.stringify(flap));
  if (!docs || docs.chip !== "ask to merge" || !old || old.chip !== "") fail(tag + ": a branch with none offers one, a released one does not " + JSON.stringify([docs, old]));
  if (!other || other.chip !== "cr_7 · into release/1.x") fail(tag + ": a request into another branch is not shown on its branch " + JSON.stringify(other));
}));

add("changeReqOpen", withPage("changeReqOpen", "#/hubrepos/ledger", {}, async (p, tag, hub) => {
  await p.click('.hr-ev:has(.hr-name:text-is("feat/metrics-exemplars")) .cr-chip.ask');
  await p.waitForSelector(".cr-form");
  const f = await p.evaluate(() => ({ view: hubReposView(), repo: document.getElementById("cr-f-repo").value, branch: document.getElementById("cr-f-branch").value, target: document.getElementById("cr-f-target").value }));
  if (f.view !== "requests" || f.repo !== "github/openziti/ziti" || f.branch !== "feat/metrics-exemplars" || f.target !== "main") fail(tag + ": the form is not filled from the branch " + JSON.stringify(f));
  await p.fill("#cr-f-branch", "nope/missing");
  await p.fill("#cr-f-title", "A request");
  await p.click('[data-cr="new-do"]');
  await p.waitForSelector(".cr-form .cr-err");
  await p.waitForFunction(() => !cr.busy, null, { timeout: slow(10000) });
  if (!/hub has no branch called nope\/missing.*git push hub nope\/missing/.test(await text(p, ".cr-form .cr-err"))) fail(tag + ": the 404 is not explained " + await text(p, ".cr-form .cr-err"));
  await p.fill("#cr-f-branch", "feat/metrics-exemplars");
  await p.fill("#cr-f-why", "line one\n<b>not bold</b>");
  await p.click('[data-cr="new-do"]');
  await p.waitForSelector(".cr-ledger");
  const created = hub.calls.filter(c => c.path === "/_hub/change-requests").pop().body;
  if (JSON.stringify(created) !== '{"repo":"github/openziti/ziti","source":{"branch":"feat/metrics-exemplars"},"target":{"branch":"main"},"title":"A request","why":"line one\\n<b>not bold</b>"}') fail(tag + ": the create body is " + JSON.stringify(created));
  if (!/A request/.test(await text(p, ".cr-title")) || await p.evaluate(() => !!document.querySelector(".cr-why b"))) fail(tag + ": the new request is not shown as text");
  await p.evaluate(() => crClick({ dataset: { cr: "new", repo: "github/openziti/ziti", branch: "fix/router-link-flap" } }));
  await p.waitForSelector(".cr-form");
  await p.fill("#cr-f-title", "Again");
  await p.click('[data-cr="new-do"]');
  await p.waitForFunction(() => /already exists/.test((document.querySelector(".cr-err") || {}).textContent || ""), null, { timeout: slow(10000) });
  if (!/Fix the router link flap/.test(await text(p, ".cr-title"))) fail(tag + ": a 409 does not open the request that exists");
}));

const markup = h => { h.rows[0].title = '<img src=x onerror=window.__x=1>Title'; h.rows[0].why = '<script>window.__x=2<\/script> why'; h.rows[0].source.branch = '"><i>b</i>'; h.rows[0].note = '<u>n</u>'; };
add("changeReqText", withPage("changeReqText", REQ + "open", { mutate: markup }, async (p, tag) => {
  const x = await p.evaluate(() => ({ x: window.__x, img: [...document.querySelectorAll("#hubrepos-list img, #hubrepos-list i, #hubrepos-list u, #hubrepos-list script")].filter(e => e.tagName !== "I" || e.textContent).length, t: document.getElementById("hubrepos-list").textContent }));
  if (x.x || x.img || !x.t.includes("<img src=x onerror=window.__x=1>Title") || !x.t.includes("<script>window.__x=2</script> why")) fail(tag + ": a title or why became markup or was lost " + JSON.stringify({ x: x.x, img: x.img }));
}));

add("changeReqStateWord", withPage("changeReqStateWord", REQ + "closed/cr_6", { mutate: h => { h.rows.find(r => r.id === "cr_6").state = 'merged" data-x="1'; } }, async (p, tag) => {
  await p.waitForSelector(".cr-ended");
  if (await p.evaluate(() => !!document.querySelector(".cr-ended[data-x]"))) fail(tag + ": a state word became an attribute");
}));

add("changeReqMock", withPage("changeReqMock", REQ + "open", { noHub: true, query: "?crmock=1" }, async (p, tag) => {
  const mk = await p.evaluate(() => ({ on: crCore.mockOn(), n: document.querySelectorAll(".cr-li").length, t: document.querySelector(".cr-title").textContent }));
  if (!mk.on || mk.n !== 3 || !/Hub receives pushes/.test(mk.t)) fail(tag + ": the mock layer did not serve the view " + JSON.stringify(mk));
  const bn = await p.evaluate(() => { const b = document.getElementById("cr-mockbar"); const r = b && b.getBoundingClientRect(); return b ? { t: b.textContent, pos: getComputedStyle(b).position, vis: r.width > 100 && r.height > 10, btn: !!b.querySelector("button") } : null; });
  if (!bn || !/Mock data: nothing here reaches the hub/.test(bn.t) || bn.pos !== "fixed" || !bn.vis || !bn.btn) fail(tag + ": the mock is on without a banner " + JSON.stringify(bn));
  await p.fill("#cr-sha", CR_ZERO); await p.click('[data-cr="merge-do"]'); await p.waitForSelector(".cr-err");
  if (!/not reachable from main/.test(await text(p, ".cr-err"))) fail(tag + ": the mock does not follow the draft's 409");
  await p.click("#cr-mockbar button");
  await p.waitForFunction(() => !/crmock/.test(location.search) && !document.getElementById("cr-mockbar") && document.readyState === "complete" && typeof crCore !== "undefined", null, { timeout: slow(8000) });
  if (await p.evaluate(() => crCore.mockOn())) fail(tag + ": turn off left the mock on");
}));

add("changeReqMockOff", withPage("changeReqMockOff", REQ + "open", { noHub: true }, async (p, tag) => {
  if (await p.evaluate(() => crCore.mockOn())) fail(tag + ": the mock is on without being asked");
  if (!/hub is not answering|does not have change requests|would not list/.test(await text(p, "#hubrepos-list"))) fail(tag + ": with the mock off and no hub the view shows rows: " + (await text(p, "#hubrepos-list")).slice(0, 120));
  if (await p.evaluate(() => !!document.getElementById("cr-mockbar"))) fail(tag + ": a banner with the mock off");
  await p.evaluate(() => localStorage.setItem("atrium.crMock", "1"));
  await p.reload({ waitUntil: "load" });
  await ready(p);
  if (await p.evaluate(() => crCore.mockOn() || !!document.getElementById("cr-mockbar"))) fail(tag + ": localStorage still turns the mock on");
}));

add("changeReqCanon", withPage("changeReqCanon", REQ + "open/cr_9", {}, async (p, tag, hub) => {
  const cn = await p.evaluate(() => ["o/r", "github/o/r", "gitlab.example/o/r", " o/r ", ""].map(crCore.canonRepo).join("|"));
  if (cn !== "github/o/r|github/o/r|gitlab.example/o/r|github/o/r|") fail(tag + ": canonRepo is wrong " + cn);
  await p.click('[data-cr="withdraw"]');
  await p.waitForFunction(() => /Withdrawn/.test((document.querySelector(".cr-ended") || {}).textContent || ""), null, { timeout: slow(10000) });
  if (hub.calls.some(c => c.card)) fail(tag + ": a write carried a card header " + JSON.stringify(hub.calls));
}));

add("changeReqShareForbid", sectionOf("changeReqShareForbid", async (browser, base, tag) => {
  const { ctx, p, errors } = await openAt(browser, base, REQ + "open/cr_9", {});
  try {
    await p.route(/change-requests\/cr_9$/, r => r.request().method() === "POST" ? r.fulfill({ status: 403, contentType: "application/json", body: JSON.stringify({ error: "you cannot share this card here" }) }) : r.fallback());
    await p.click('[data-cr="withdraw"]');
    await p.waitForSelector(".cr-err");
    if (await p.evaluate(() => !!document.querySelector(".cr-ro") || crCore.state.readOnly)) fail(tag + ": a 403 that says share made the board read-only");
    if (errors.length) fail(tag + ": page errors: " + errors.join(" | "));
  } finally { await ctx.close(); }
}));

add("changeReqOwnForbid", withPage("changeReqOwnForbid", REQ + "open/cr_9", { mode: "forbid" }, async (p, tag) => {
  await p.click('[data-cr="withdraw"]');
  await p.waitForSelector(".cr-err");
  const fb = await p.evaluate(() => ({ err: document.querySelector(".cr-err").textContent, ro: !!document.querySelector(".cr-ro"), flag: crCore.state.readOnly, withdraw: !!document.querySelector('[data-cr="withdraw"]') }));
  if (fb.err !== "only the owner may withdraw this request" || fb.ro || fb.flag || !fb.withdraw) fail(tag + ": an action's own 403 hid the page's actions " + JSON.stringify(fb));
}));

add("changeReqOneRead", withPage("changeReqOneRead", REQ + "open", {}, async (p, tag) => {
  const lr = await p.evaluate(async () => {
    const f = window.fetch; let now = 0, max = 0, n = 0;
    window.fetch = function (u) { const lst = /change-requests\?/.test(String(u)); if (lst) { now++; n++; max = Math.max(max, now); } return f.apply(this, arguments).finally(() => { if (lst) now--; }); };
    await Promise.all([crLoad(), crLoad(), crLoad()]);
    window.fetch = f;
    return { max, n, run: cr.run, inflight: cr.inflight };
  });
  if (lr.max !== 1 || lr.n !== 2 || lr.run || lr.inflight) fail(tag + ": list reads overlap or are not settled " + JSON.stringify(lr));
}));

add("changeReqPhone", withPage("changeReqPhone", REQ + "open", { w: 390, h: 844 }, async (p, tag) => {
  await hubReposNoOverflow(p, tag + " at 390");
  const ph = await p.evaluate(() => ({ disp: getComputedStyle(document.querySelector(".cr-ledger")).display, cols: getComputedStyle(document.querySelector(".cr-cols")).gridTemplateColumns.split(" ").length, hit: Math.min(...[...document.querySelectorAll(".cr-btn, .cr-li")].map(b => b.getBoundingClientRect().height)) }));
  if (ph.disp !== "block" || ph.cols !== 1 || ph.hit < 34) fail(tag + ": at 390px the layout is wrong " + JSON.stringify(ph));
}));

const units = Object.keys(sections).map(name => ({ name, run: ({ browser, base }) => sections[name](browser, base) }));

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
