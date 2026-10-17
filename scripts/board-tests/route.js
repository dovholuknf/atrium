// route: the address carries the view, the repos sub-view, the change request and its tab.
//
// Units: deepLinkView, deepLinkRequests, deepLinkBack.
// `units` is what the harness runs, in suite order. `sections` is for HEADLESS_ONLY, which names a section and not a unit.
// Alone: `node scripts/board-tests/route.js` runs just these units, the way the full run would.
// Every unit loads an address and waits on `data-route="ready"` on <html>, and on nothing timed.
const { slow } = require("../lib/browser.js");
const { mock } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");

const ready = p => p.waitForFunction(() => document.documentElement.dataset.route === "ready", null, { timeout: slow(15000) });
const readyAt = (p, hash) => p.waitForFunction(h => document.documentElement.dataset.route === "ready" && location.hash === h, hash, { timeout: slow(15000) });
const state = p => p.evaluate(() => ({
  view: VIEWS.find(isViewing), hash: location.hash, search: location.search, missing: document.documentElement.dataset.routeMissing || "",
  sub: typeof hubReposView === "function" ? hubReposView() : "", tab: cr.tab, cur: [...document.querySelectorAll('.cr-li[aria-current="true"]')].map(b => b.dataset.id).join(),
  pane: (document.querySelector("#hubrepos-list .cr-detail") || {}).textContent || "", list: (document.getElementById("hubrepos-list") || {}).textContent || "" }));

async function openAt(browser, base, hash, o) {
  o = o || {};
  const ctx = await browser.newContext({ viewport: { width: 1600, height: 900 } });
  const errors = [];
  if (o.init) await ctx.addInitScript(o.init);
  const p = await ctx.newPage();
  p.on("pageerror", e => errors.push(e.message));
  if (o.hub) {
    const fx = require("../hubrepos-fixture.js")(Date.now());
    const hub = require("../changereq-fixture.js")(Date.now());
    await p.route(/\/_hub\/git\/repos$/, r => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ repos: fx.repos }) }));
    await hub.route(p, { mode: "ok" });
    if (o.gate) await p.route(/\/_hub\/change-requests(\?|$)/, async r => { await o.gate; r.fallback(); });
  }
  await p.goto(base + "/" + (o.search || "") + hash, { waitUntil: "domcontentloaded" });
  return { ctx, p, errors };
}

async function deepLinkViewSection(browser, base) {
  const tag = "deepLinkView";
  for (const [hash, view, missing, want] of [["#/runners", "runners", "", "#/runners"], ["#/history", "history", "", "#/history"], ["#/nope", "board", "view=nope", "#/board"]]) {
    const { ctx, p, errors } = await openAt(browser, base, hash);
    try {
      await ready(p);
      const s = await state(p);
      if (s.view !== view || s.missing !== missing || s.hash !== want) fail(tag + ": " + hash + " landed on " + JSON.stringify(s));
      if (errors.length) fail(tag + ": " + hash + " threw " + errors.join(" | "));
    } finally { await ctx.close(); }
  }
  const stored = await openAt(browser, base, "#/audit", { init: () => localStorage.setItem("atrium.view", "runners") });
  try {
    await ready(stored.p);
    const s = await state(stored.p);
    if (s.view !== "audit") fail(tag + ": a route lost to the stored view " + JSON.stringify(s));
  } finally { await stored.ctx.close(); }
  const bare = await openAt(browser, base, "", { init: () => localStorage.setItem("atrium.view", "runners") });
  try {
    await ready(bare.p);
    await bare.p.waitForFunction(() => isViewing("runners"), null, { timeout: slow(10000) });
    const s = await state(bare.p);
    if (s.missing) fail(tag + ": a bare address names a missing part " + s.missing);
  } finally { await bare.ctx.close(); }
  if (!bad) console.log("deepLinkView ok");
}

async function deepLinkRequestsSection(browser, base) {
  const tag = "deepLinkRequests";
  const was = mock.hubMode; mock.hubMode = true;
  try {
    const cases = [
      ["#/hubrepos/requests/closed/cr_6", { tab: "closed", cur: "cr_6", missing: "", hash: "#/hubrepos/requests/closed/cr_6", pane: /Bump Go/ }],
      ["#/hubrepos/requests/open/cr_6", { tab: "closed", cur: "cr_6", missing: "", hash: "#/hubrepos/requests/closed/cr_6", pane: /Bump Go/ }],
      ["#/hubrepos/requests/open", { tab: "open", cur: "cr_9", missing: "", hash: "#/hubrepos/requests/open/cr_9", pane: /router link flap/ }],
      ["#/hubrepos/requests/open/cr_nope", { tab: "open", cur: "", missing: "id=cr_nope", hash: "#/hubrepos/requests/open", pane: /No change request cr_nope/ }],
      ["#/hubrepos/requests/later/cr_8", { tab: "open", cur: "cr_8", missing: "tab=later", hash: "#/hubrepos/requests/open/cr_8", pane: /Edge router policies/ }],
      ["#/hubrepos/nope", { sub: "shelf", missing: "sub=nope", hash: "#/hubrepos/shelf" }],
      ["#/hubrepos/ledger", { sub: "ledger", missing: "", hash: "#/hubrepos/ledger" }],
    ];
    for (const [hash, want] of cases) {
      const { ctx, p, errors } = await openAt(browser, base, hash, { hub: true, init: () => localStorage.setItem("atrium.reposView", "feed") });
      try {
        await ready(p);
        const s = await state(p);
        for (const k of Object.keys(want)) {
          const ok = want[k] instanceof RegExp ? want[k].test(s[k]) : s[k] === want[k];
          if (!ok) fail(tag + ": " + hash + " " + k + " is " + JSON.stringify(s[k]) + ", want " + want[k]);
        }
        if (errors.length) fail(tag + ": " + hash + " threw " + errors.join(" | "));
      } finally { await ctx.close(); }
    }

    let release;
    const gate = new Promise(r => { release = r; });
    const held = await openAt(browser, base, "#/hubrepos/requests/closed/cr_6", { hub: true, gate });
    try {
      await held.p.waitForFunction(() => isViewing("hubrepos") && document.documentElement.dataset.route === "pending", null, { timeout: slow(15000) });
      const s = await state(held.p);
      if (s.cur || /Bump Go|router link flap/.test(s.list) || !/Reading/.test(s.list)) fail(tag + ": before the list arrived the pane showed " + JSON.stringify(s.list.slice(0, 120)));
      release();
      await ready(held.p);
      const d = await state(held.p);
      if (d.cur !== "cr_6" || !/Bump Go/.test(d.pane)) fail(tag + ": after the list arrived " + JSON.stringify([d.cur, d.pane.slice(0, 60)]));
    } finally { release(); await held.ctx.close(); }

    const m = await openAt(browser, base, "#/hubrepos/requests/open/cr_7", { search: "?crmock=1" });
    try {
      await ready(m.p);
      const s = await state(m.p);
      if (s.search !== "?crmock=1" || s.cur !== "cr_7" || s.hash !== "#/hubrepos/requests/open/cr_7") fail(tag + ": crmock with a route " + JSON.stringify(s));
      if (m.errors.length) fail(tag + ": crmock threw " + m.errors.join(" | "));
    } finally { await m.ctx.close(); }
  } finally { mock.hubMode = was; }
  if (!bad) console.log("deepLinkRequests ok");
}

async function deepLinkBackSection(browser, base) {
  const tag = "deepLinkBack";
  const was = mock.hubMode; mock.hubMode = true;
  const { ctx, p, errors } = await openAt(browser, base, "#/runners", { hub: true });
  try {
    await ready(p);
    await p.evaluate(() => { window.__applies = 0; const o = routeApply; routeApply = function () { window.__applies++; return o.apply(this, arguments); }; });
    const applies = () => p.evaluate(() => { const n = window.__applies; window.__applies = 0; return n; });
    await p.evaluate(() => switchView("hubrepos"));
    await p.evaluate(() => setHubReposView("requests"));
    await p.waitForFunction(() => cr.loaded && document.querySelector(".cr-li"), null, { timeout: slow(10000) });
    await p.click('.cr-seg [data-tab="closed"]');
    await p.click('.cr-li[data-id="cr_6"]');
    await p.waitForFunction(() => /Bump Go/.test((document.querySelector(".cr-title") || {}).textContent || ""), null, { timeout: slow(10000) });
    const trail = ["#/runners", "#/hubrepos/shelf", "#/hubrepos/requests/open/cr_9", "#/hubrepos/requests/closed/cr_6"];
    if ((await state(p)).hash !== trail[3]) fail(tag + ": the address after the clicks is " + (await state(p)).hash);
    await applies();
    for (const want of [trail[2], trail[1], trail[0]]) {
      await p.goBack();
      await readyAt(p, want);
      const n = await applies();
      if (n !== 1) fail(tag + ": back to " + want + " applied " + n + " times");
    }
    const s0 = await state(p);
    if (s0.view !== "runners") fail(tag + ": back ended on " + s0.view);
    for (const want of [trail[1], trail[2], trail[3]]) {
      await p.goForward();
      await readyAt(p, want);
      const n = await applies();
      if (n !== 1) fail(tag + ": forward to " + want + " applied " + n + " times");
    }
    const s1 = await state(p);
    if (s1.cur !== "cr_6" || s1.tab !== "closed") fail(tag + ": forward ended on " + JSON.stringify([s1.cur, s1.tab]));
    await p.evaluate(() => { location.hash = "#/history"; });
    await readyAt(p, "#/history");
    const n = await applies();
    if (n !== 1 || (await state(p)).view !== "history") fail(tag + ": a typed hash applied " + n + " times and landed on " + (await state(p)).view);
    if (errors.length) fail(tag + ": threw " + errors.join(" | "));
  } finally { await ctx.close(); mock.hubMode = was; }
  if (!bad) console.log("deepLinkBack ok");
}

const units = [
  { name: "deepLinkView", run: ({ browser, base }) => deepLinkViewSection(browser, base) },
  { name: "deepLinkRequests", run: ({ browser, base }) => deepLinkRequestsSection(browser, base) },
  { name: "deepLinkBack", run: ({ browser, base }) => deepLinkBackSection(browser, base) },
];
const sections = { deepLinkView: deepLinkViewSection, deepLinkRequests: deepLinkRequestsSection, deepLinkBack: deepLinkBackSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
