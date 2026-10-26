// hubrepos: the hub's repositories page.
//
// Units: hubRepos, hubReposShelf, hubReposLedger, hubReposTimelineDots, hubReposFeed, hubReposBranches.
// `units` is what the harness runs, in suite order. `sections` is for HEADLESS_ONLY, which names a section and not a unit.
// Alone: `node scripts/board-tests/hubrepos.js` runs just these units, the way the full run would.
const path = require("path");
const http = require("http");
const { slow, frames, settleStyle } = require("../lib/browser.js");
const { mock } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");
const { hubReposNoOverflow } = require("../lib/helpers.js");

async function hubReposSection(browser, base) {
  const wasHub = mock.hubMode;
  mock.hubMode = true;
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const errors = [];
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    let mode = "repos";
    const ago1 = new Date(Date.now() - 3600 * 1000).toISOString();
    const repos = [
      { host: "github", owner: "openziti", repo: "ziti", url: "git@hub.atrium:openziti/ziti.git", path: "/git/hub/github/openziti/ziti.git",
        main: { sha: "abcdef0123456789", at: ago1 },
        branches: [
          { name: "fix/x", sha: "1234567890abcdef", room: "sg4", card: "c-known", at: ago1, released: false },
          { name: "old/y", sha: "fedcba9876543210", room: "sg4", card: "c-gone", at: ago1, released: true }] },
      { host: "gitlab.com", owner: "acme", repo: "thing", url: "git@hub.atrium:gitlab.com/acme/thing.git", path: "/git/hub/gitlab.com/acme/thing.git",
        main: { sha: "", at: null }, branches: [] }];
    await p.route(/\/_hub\/git\/repos$/, r => {
      if (mode === "fail") return r.fulfill({ status: 404, body: "no" });
      r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ repos: mode === "empty" ? [] : repos }) });
    });
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => { const t = document.querySelector('.tab[data-view="hubrepos"]'); return t && !t.hidden; }, null, { timeout: slow(15000) });
    // The board puts you back on the remembered view half a second after it loads, so wait that out or it lands on top of ours.
    await p.waitForFunction(() => history.state && history.state.atrium, null, { timeout: slow(5000) }).catch(() => {});
    const open = async () => {
      await p.evaluate(() => {
        lastTasks = [{ id: "c-known", room: "sg4", title: "fix the x", display_title: "fix the x", status: "working" }];
        return switchView("hubrepos");
      });
      await p.waitForFunction(() => (hubRepos.loaded || hubRepos.note) && !hubRepos.inflight, null, { timeout: slow(10000) });
      await frames(p, 2);
    };
    await open();
    const got = await p.evaluate(() => {
      const rows = [...document.querySelectorAll("#hubrepos-list .hr-repo")];
      return {
        n: rows.length,
        copies: [...document.querySelectorAll(".hr-copy")].map(b => b.dataset.copy),
        first: rows[0] && rows[0].textContent,
        count: rows[0] && rows[0].querySelector(".hr-count-big").textContent,
        second: rows[1] && rows[1].textContent,
        branches: [...document.querySelectorAll(".hr-branch")].map(b => b.textContent),
        released: document.querySelectorAll(".hr-branch.released").length,
        tip: !!document.querySelector(".hr-branch .hr-when[data-tip]"),
        unlabeled: [...document.querySelectorAll(".hr-copy")].filter(b => !b.getAttribute("aria-label")).length,
        origin: location.origin
      };
    });
    if (got.n !== 2) fail("hubRepos: expected two repos, got " + got.n);
    if (!got.copies.includes("git@hub.atrium:openziti/ziti.git") || !got.copies.includes("git@hub.atrium:gitlab.com/acme/thing.git"))
      fail("hubRepos: the ssh copy values are wrong: " + JSON.stringify(got.copies));
    await p.click('.hr-repo[data-repo="openziti/ziti"] [data-act="mode"][data-mode="http"]');
    const httpCopy = await p.evaluate(() => document.querySelector('.hr-repo[data-repo="openziti/ziti"] .hr-clone .hr-copy').dataset.copy);
    if (httpCopy !== got.origin + "/git/hub/github/openziti/ziti.git") fail("hubRepos: the http copy value is wrong: " + httpCopy);
    await p.click('.hr-repo[data-repo="openziti/ziti"] [data-act="mode"][data-mode="ssh"]');
    if (!/main\s*abcdef0/.test(got.first) || /abcdef012/.test(got.first)) fail("hubRepos: main is not shown short: " + got.first);
    if (!/^2\s*branches/.test(got.count)) fail("hubRepos: the card does not count branches: " + got.count);
    if (!/empty/i.test(got.second) || !/Nothing pushed yet/.test(got.second) || !/git remote add hub git@hub\.atrium:gitlab\.com\/acme\/thing\.git/.test(got.second) || !/git push hub/.test(got.second))
      fail("hubRepos: the empty repo has no how-to: " + got.second);
    if (!got.copies.includes("git remote add hub git@hub.atrium:gitlab.com/acme/thing.git\ngit push hub <branch>")) fail("hubRepos: the how-to copy value is wrong: " + JSON.stringify(got.copies));
    if (!got.tip) fail("hubRepos: a branch age has no exact-time tooltip");
    if (got.unlabeled) fail("hubRepos: " + got.unlabeled + " copy buttons have no aria-label");
    if (!/fix\/x/.test(got.branches[0]) || !/sg4 c-known/.test(got.branches[0]) || !/fix the x/.test(got.branches[0]))
      fail("hubRepos: the known-card branch is wrong: " + got.branches[0]);
    if (!/sg4 c-gone/.test(got.branches[1]) || /undefined|null/.test(got.branches[1]) || !/released/.test(got.branches[1]))
      fail("hubRepos: the gone-card released branch is wrong: " + got.branches[1]);
    if (got.released !== 1) fail("hubRepos: released should be marked once, got " + got.released);
    // THE SWITCHER: Shelf with nothing stored, a click moves it, the arrow keys move it as one radio group, an unknown
    // stored value is Shelf, and another window's choice is followed through the storage event.
    const viewNow = () => p.evaluate(() => ({ view: document.getElementById("hubrepos-list").dataset.view, stored: localStorage.getItem("atrium.reposView"),
      checked: [...document.querySelectorAll("#hubrepos-views [role=radio]")].filter(b => b.getAttribute("aria-checked") === "true").map(b => b.dataset.hrview),
      tabbable: [...document.querySelectorAll("#hubrepos-views [role=radio]")].filter(b => b.tabIndex === 0).map(b => b.dataset.hrview) }));
    await p.evaluate(() => { localStorage.removeItem("atrium.reposView"); hubReposPaint(); });
    let v = await viewNow();
    if (v.view !== "shelf" || v.checked.join() !== "shelf" || v.tabbable.join() !== "shelf") fail("hubRepos: nothing stored is not Shelf: " + JSON.stringify(v));
    if ((await p.locator("#hubrepos-views [role=radio]").count()) !== 5 || (await p.getAttribute("#hubrepos-views", "role")) !== "radiogroup") fail("hubRepos: the switcher is not a five-way radio group");
    await p.click('#hubrepos-views [data-hrview="ledger"]');
    v = await viewNow();
    if (v.view !== "ledger" || v.stored !== "ledger" || v.checked.join() !== "ledger") fail("hubRepos: clicking Ledger did not stick: " + JSON.stringify(v));
    await p.focus('#hubrepos-views [data-hrview="ledger"]');
    await p.keyboard.press("ArrowRight");
    v = await viewNow();
    if (v.view !== "feed" || v.stored !== "feed" || v.tabbable.join() !== "feed") fail("hubRepos: ArrowRight did not move to Feed: " + JSON.stringify(v));
    if ((await p.evaluate(() => document.activeElement.dataset.hrview)) !== "feed") fail("hubRepos: the arrow key did not carry focus along");
    await p.keyboard.press("ArrowRight");
    if ((await viewNow()).view !== "requests") fail("hubRepos: ArrowRight from Feed is not Requests");
    await p.keyboard.press("ArrowRight");
    if ((await viewNow()).view !== "branches") fail("hubRepos: ArrowRight from Requests is not Branches");
    await p.keyboard.press("ArrowRight");
    if ((await viewNow()).view !== "shelf") fail("hubRepos: the arrows do not wrap round to Shelf");
    await p.keyboard.press("End");
    if ((await viewNow()).view !== "branches") fail("hubRepos: End is not the last view");
    await p.keyboard.press("Home");
    if ((await viewNow()).view !== "shelf") fail("hubRepos: Home is not the first view");
    await p.evaluate(() => { localStorage.setItem("atrium.reposView", "bogus"); window.dispatchEvent(new StorageEvent("storage", { key: "atrium.reposView" })); });
    v = await viewNow();
    if (v.view !== "shelf" || v.checked.join() !== "shelf") fail("hubRepos: an unknown stored value is not Shelf: " + JSON.stringify(v));
    // A real second window of the same browser: its write reaches this one as a storage event.
    const other = await ctx.newPage();
    await other.goto(base + "/", { waitUntil: "domcontentloaded" });
    await other.evaluate(() => localStorage.setItem("atrium.reposView", "feed"));
    await p.waitForFunction(() => document.getElementById("hubrepos-list").dataset.view === "feed", null, { timeout: slow(5000) })
      .catch(() => fail("hubRepos: another window's choice was not followed"));
    await other.close();
    await p.evaluate(() => localStorage.removeItem("atrium.reposView"));
    await p.evaluate(() => { hubReposPaint(); });
    // Copy feedback: "copied", then back to "copy".
    await ctx.grantPermissions(["clipboard-read", "clipboard-write"]).catch(() => {});
    await p.evaluate(() => { window.__clip = []; navigator.clipboard.writeText = async t => { window.__clip.push(t); }; });
    await p.click('.hr-copy[data-id="openziti/ziti#ssh"]');
    await p.waitForFunction(() => /copied/.test(document.querySelector('.hr-copy[data-id="openziti/ziti#ssh"]').textContent), null, { timeout: slow(5000) })
      .catch(() => fail("hubRepos: the copy button never said copied"));
    if ((await p.evaluate(() => window.__clip))[0] !== "git@hub.atrium:openziti/ziti.git") fail("hubRepos: copy wrote the wrong text");
    await p.waitForFunction(() => document.querySelector('.hr-copy[data-id="openziti/ziti#ssh"]').textContent.trim() === "copy", null, { timeout: slow(5000) })
      .catch(() => fail("hubRepos: the copy button never went back to copy"));
    // Two quick refreshes make one request: the in-flight guard.
    let asks = 0;
    await p.route(/\/_hub\/git\/repos$/, async r => { asks++; await new Promise(x => setTimeout(x, 300)); r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ repos }) }); });
    await p.evaluate(() => { loadHubRepos(); loadHubRepos(); });
    await p.waitForFunction(() => !hubRepos.inflight, null, { timeout: slow(5000) });
    if (asks !== 1) fail("hubRepos: two quick refreshes asked " + asks + " times");
    await p.unroute(/\/_hub\/git\/repos$/);
    await p.route(/\/_hub\/git\/repos$/, r => {
      if (mode === "fail") return r.fulfill({ status: 404, body: "no" });
      r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ repos: mode === "empty" ? [] : repos }) });
    });
    mode = "empty";
    await p.evaluate(() => { hubRepos.loaded = false; });
    await p.click("#hubrepos-refresh");
    await p.waitForFunction(() => /atrium git setup/.test(document.getElementById("hubrepos-list").textContent), null, { timeout: slow(10000) });
    mode = "fail";
    await p.click("#hubrepos-refresh");
    await p.waitForFunction(() => /not answering/.test(document.getElementById("hubrepos-list").textContent), null, { timeout: slow(10000) });
    // The choice survives a reload.
    mode = "repos";
    await p.click('#hubrepos-views [data-hrview="ledger"]');
    await p.reload({ waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => { const t = document.querySelector('.tab[data-view="hubrepos"]'); return t && !t.hidden; }, null, { timeout: slow(15000) });
    await open();
    if ((await p.evaluate(() => document.getElementById("hubrepos-list").dataset.view)) !== "ledger") fail("hubRepos: the view did not survive a reload");
    if (!(await p.locator("#hubrepos-list .hr-ledger").count())) fail("hubRepos: the ledger was not drawn after a reload");
    await p.evaluate(() => localStorage.removeItem("atrium.reposView"));
    if (errors.length) fail("hubRepos: page errors: " + errors.join(" | "));
  } finally { mock.hubMode = wasHub; await ctx.close(); }
  if (!bad) console.log("hubRepos ok");
}

// ---- the repos tab's three views on the fixture hub (scripts/hubrepos-fixture.js) -------------------------------------
// Five repos under three owners: one empty, one main-only, one with a dozen branches (one very long name), one stale pile
// with released branches, one fresh. One section per view; each draws the tab on the fixture, and with HR_SHOTS=<dir> also
// saves the full and empty states at 2000 and 390, paper and dark, for looking at.

async function hubReposFixturePage(browser, base, o) {
  const fx = require("../hubrepos-fixture.js")(Date.now());
  const repos = o.repos || (o.empty ? [fx.repos[4]] : fx.repos);
  const ctx = await browser.newContext({ viewport: { width: o.w || 1600, height: o.h || 1000 } });
  const errors = [];
  const p = await ctx.newPage();
  p.on("pageerror", e => errors.push(e.message));
  await p.route(/\/_hub\/git\/repos$/, r => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ repos }) }));
  await p.route(/\/_hub\/git\/branches$/, r => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(o.branches || fx.branches) }));
  await p.goto(base + "/", { waitUntil: "domcontentloaded" });
  await p.waitForFunction(() => { const t = document.querySelector('.tab[data-view="hubrepos"]'); return t && !t.hidden; }, null, { timeout: slow(15000) });
  await p.evaluate(({ tasks, view }) => {
    lastTasks = tasks;
    if (view) localStorage.setItem("atrium.reposView", view); else localStorage.removeItem("atrium.reposView");
    return switchView("hubrepos");
  }, { tasks: fx.tasks, view: o.view === undefined ? null : o.view });
  await p.waitForFunction(() => hubRepos.loaded && document.querySelector("#hubrepos-list .hr-root"), null, { timeout: slow(10000) });
  await settleStyle(p);
  return { ctx, p, errors, fx, repos };
}

async function hubReposShots(browser, base, view) {
  const dir = process.env.HR_SHOTS;
  if (!dir) return;
  require("fs").mkdirSync(dir, { recursive: true });
  for (const state of ["full", "empty"]) for (const w of [2000, 390]) for (const skin of ["paper", "dark"]) {
    const { ctx, p } = await hubReposFixturePage(browser, base, { w, h: 900, view, empty: state === "empty" });
    try {
      await p.evaluate(s => { if (s === "paper") document.documentElement.setAttribute("data-skin", "paper"); else document.documentElement.removeAttribute("data-skin"); }, skin);
      await settleStyle(p);
      const h = await p.evaluate(() => { const l = document.getElementById("hubrepos-list"); return Math.ceil(l.scrollHeight + l.getBoundingClientRect().top + 24); });
      await p.setViewportSize({ width: w, height: Math.min(6000, Math.max(600, h)) });
      await settleStyle(p);
      await p.screenshot({ path: require("path").join(dir, view + "-" + state + "-" + w + "-" + skin + ".png") });
    } finally { await ctx.close(); }
  }
}

// What every view owes: one copy button per URL, inside its own row and level with it, every copy labelled.
async function hubReposCopyRows(p, tag) {
  const r = await p.evaluate(() => {
    const rows = [...document.querySelectorAll("#hubrepos-list .hr-clone, #hubrepos-list .hr-termbar, #hubrepos-list .hr-cr, #hubrepos-list .hr-howto, #hubrepos-list .hr-herorow")];
    return {
      rows: rows.length,
      multi: rows.filter(x => x.querySelectorAll(".hr-copy").length !== 1).length,
      outside: rows.filter(x => { const b = x.querySelector(".hr-copy"), a = x.getBoundingClientRect(), c = b.getBoundingClientRect();
        return c.right > a.right + 1 || c.left < a.left - 1 || c.top < a.top - 1 || c.bottom > a.bottom + 1 || Math.abs((c.top + c.bottom) / 2 - (a.top + a.bottom) / 2) > 6; }).length,
      unlabeled: [...document.querySelectorAll("#hubrepos-list .hr-copy")].filter(b => !b.getAttribute("aria-label")).length
    };
  });
  if (!r.rows) fail(tag + ": no URL rows to check");
  if (r.multi) fail(tag + ": " + r.multi + " URL rows do not have exactly one copy button");
  if (r.outside) fail(tag + ": " + r.outside + " copy buttons sit outside or off-centre of their row");
  if (r.unlabeled) fail(tag + ": " + r.unlabeled + " copy buttons have no aria-label");
}

// Nothing a repo, branch, room or card is called may become markup.
async function hubReposEscaped(p, tag) {
  await p.evaluate(() => {
    const at = new Date(Date.now() - 600e3).toISOString();
    window.__x = 0;
    hubRepos.repos = [{ host: "gitlab.com", owner: "<b>o</b>", repo: '"><img src=x onerror=window.__x=1>', url: "git@hub:<i>x</i>.git", path: "/g/<u>.git",
      main: { sha: "<s>abc1234", at }, branches: [{ name: "<img src=x onerror=window.__x=1>", sha: "1234567", room: "<r>", card: "<c>", at, released: false }] }];
    hubRepos.sel = null; hubRepos.filter = ""; hubRepos.mode = {};
    hubReposPaint();
  });
  await p.waitForTimeout(100); // wait: a negative check, no onerror may fire
  const r = await p.evaluate(() => { const l = document.getElementById("hubrepos-list");
    return { x: window.__x, img: l.querySelectorAll("img").length, raw: /<img/.test(l.innerHTML), text: l.textContent }; });
  if (r.x || r.img || r.raw) fail(tag + ": a name became markup: " + JSON.stringify({ x: r.x, img: r.img, raw: r.raw }));
  if (!r.text.includes("<img src=x onerror=window.__x=1>")) fail(tag + ": the hostile name is not shown as text");
}

async function hubReposCopyWorks(ctx, p, sel, want, tag) {
  await ctx.grantPermissions(["clipboard-read", "clipboard-write"]).catch(() => {});
  await p.evaluate(() => { window.__clip = []; navigator.clipboard.writeText = async t => { window.__clip.push(t); }; });
  const first = (await p.locator(sel).first().locator(".hr-copylabel").textContent()).trim();
  // "copied" is on screen for 1.4s and then gone, so a poll can step over it on a loaded machine. A page-side observer
  // sees it the moment it is written and keeps the fact.
  await p.evaluate(s => {
    window.__sawCopied = false;
    const look = () => { const e = document.querySelector(s); if (e && /copied/.test(e.textContent)) window.__sawCopied = true; };
    new MutationObserver(look).observe(document.body, { subtree: true, childList: true, characterData: true });
  }, sel);
  await p.locator(sel).first().click();
  await p.waitForFunction(() => window.__sawCopied, null, { timeout: slow(5000) }).catch(() => fail(tag + ": the copy button never said copied"));
  const got = (await p.evaluate(() => window.__clip))[0];
  if (got !== want) fail(tag + ": copy wrote " + JSON.stringify(got) + ", wanted " + JSON.stringify(want));
  await p.waitForFunction(({ s, t }) => document.querySelector(s).textContent.trim() === t, { s: sel, t: first }, { timeout: slow(5000) }).catch(() => fail(tag + ": the copy button never went back to " + first));
}

// The empty state every view shares: a hero that can carry the page.
async function hubReposEmptyCheck(browser, base, view, tag) {
  const { ctx, p, errors } = await hubReposFixturePage(browser, base, { w: 2000, h: 1100, view, empty: true });
  try {
    const r = await p.evaluate(() => { const g = document.querySelector("#hubrepos-list .hr-go"), a = document.querySelector("#hubrepos-list .hr-art");
      return { hero: !!document.querySelector("#hubrepos-list .hr-hero"), copy: g && g.dataset.copy, h: g && g.getBoundingClientRect().height, label: g && g.textContent.trim(),
        art: a && a.getBoundingClientRect().width, steps: document.querySelectorAll("#hubrepos-list .hr-steps li").length, cards: document.querySelectorAll("#hubrepos-list .hr-repo").length,
        h2: (document.querySelector("#hubrepos-list .hr-hero h2") || {}).textContent, size: parseFloat(getComputedStyle(document.querySelector("#hubrepos-list .hr-hero h2")).fontSize) }; });
    if (!r.hero) fail(tag + " empty: no hero");
    if (r.copy !== "git remote add hub git@hub.atrium:dovholuknf/atrium.git\ngit push hub <branch>") fail(tag + " empty: the primary action copies " + JSON.stringify(r.copy));
    if (!/Copy the clone command/.test(r.label) || r.h < 56) fail(tag + " empty: the primary action is not big and clear: " + r.label + " " + r.h);
    if (r.art < 400 || r.steps !== 3 || r.cards || r.size < 40) fail(tag + " empty: the hero has no presence: " + JSON.stringify(r));
    await hubReposCopyRows(p, tag + " empty");
    await hubReposCopyWorks(ctx, p, "#hubrepos-list .hr-go", "git remote add hub git@hub.atrium:dovholuknf/atrium.git\ngit push hub <branch>", tag + " empty");
    await p.click('#hubrepos-list [data-act="mode"][data-mode="http"]');
    const http = await p.evaluate(() => document.querySelector("#hubrepos-list .hr-go").dataset.copy);
    if (!http.includes(await p.evaluate(() => location.origin) + "/git/hub/github/dovholuknf/atrium.git")) fail(tag + " empty: http is not what the action copies: " + http);
    if (!/^HTTP/i.test((await p.evaluate(() => document.activeElement.textContent)).trim()) && (await p.evaluate(() => document.activeElement.dataset.mode)) !== "http") fail(tag + " empty: focus was dropped by the toggle");
    // No repos at all: the same hero, pointed at `atrium git setup`.
    await p.evaluate(() => { hubRepos.repos = []; hubReposPaint(); });
    const none = await p.evaluate(() => { const g = document.querySelector("#hubrepos-list .hr-go"); return { copy: g && g.dataset.copy, text: document.getElementById("hubrepos-list").textContent }; });
    if (none.copy !== "atrium git setup" || !/atrium git setup/.test(none.text)) fail(tag + " no repos: " + JSON.stringify(none));
    if (errors.length) fail(tag + " empty: page errors: " + errors.join(" | "));
  } finally { await ctx.close(); }
  const ph = await hubReposFixturePage(browser, base, { w: 390, h: 844, view, empty: true });
  try { await hubReposNoOverflow(ph.p, tag + " empty at 390"); } finally { await ph.ctx.close(); }
}

const HR_LONG = "feat/oidc-device-flow-with-a-very-long-descriptive-branch-name-that-keeps-going-and-going";

async function hubReposShelfSection(browser, base) {
  const wasHub = mock.hubMode; mock.hubMode = true;
  const tag = "hubReposShelf";
  try {
    // Nothing stored is Shelf, drawn as a grid.
    const { ctx, p, errors } = await hubReposFixturePage(browser, base, { w: 2000, h: 1200 });
    try {
      const r = await p.evaluate(() => { const cards = [...document.querySelectorAll("#hubrepos-list .hr-repo")];
        const z = document.querySelector('.hr-repo[data-repo="openziti/ziti"]'), zk = document.querySelector('.hr-repo[data-repo="openziti/zrok"]');
        return { view: document.getElementById("hubrepos-list").dataset.view, n: cards.length, lefts: new Set(cards.map(c => Math.round(c.getBoundingClientRect().left))).size,
          big: document.querySelectorAll("#hubrepos-list .hr-summary .hr-big").length, empty: document.querySelectorAll(".hr-repo.is-empty").length, mainOnly: document.querySelectorAll(".hr-repo.main").length,
          count: z.querySelector(".hr-count-big b").textContent, sha: z.querySelector(".hr-mainsha").textContent, bars: z.querySelectorAll(".hr-spark i.on").length,
          zstate: z.querySelector(".hr-state").textContent.trim(), stale: zk.querySelector(".hr-state").textContent.trim(), tiles: document.querySelectorAll(".hr-repo .hr-tile svg").length,
          more: z.querySelector(".hr-more").textContent.trim(), rows: z.querySelectorAll(".hr-branch").length, name: getComputedStyle(z.querySelector(".hr-repoName")).fontSize }; });
      if (r.view !== "shelf") fail(tag + ": nothing stored is not Shelf: " + r.view);
      if (r.n !== 5 || r.empty !== 1 || r.mainOnly !== 1 || r.big !== 4 || r.tiles !== 5) fail(tag + ": the shelf is wrong " + JSON.stringify(r));
      if (r.lefts < 3) fail(tag + ": at 2000px the cards do not use the width (" + r.lefts + " across)");
      if (r.count !== "12" || r.sha.length !== 7 || r.bars < 4 || r.zstate !== "Active" || r.stale !== "Stale") fail(tag + ": a card's headline is wrong " + JSON.stringify(r));
      if (!/^9 more branches/.test(r.more) || r.rows !== 3 || parseFloat(r.name) < 28) fail(tag + ": the branch list or the name type is wrong " + JSON.stringify(r));
      await hubReposCopyRows(p, tag);
      // Expanding shows the rest, and the very long name stays inside its card.
      await p.click('.hr-repo[data-repo="openziti/ziti"] .hr-more');
      const x = await p.evaluate((long) => { const z = document.querySelector('.hr-repo[data-repo="openziti/ziti"]'), n = z.querySelector('.hr-name[data-tip^="' + long.slice(0, 30) + '"]');
        return { rows: z.querySelectorAll(".hr-branch").length, expanded: z.querySelector(".hr-more").getAttribute("aria-expanded"), cut: n && n.scrollWidth > n.clientWidth, inside: n && n.getBoundingClientRect().right <= z.getBoundingClientRect().right,
          full: n && n.getAttribute("data-tip") }; }, HR_LONG);
      if (x.rows !== 12 || x.expanded !== "true") fail(tag + ": show more did not show all twelve " + JSON.stringify(x));
      if (!x.cut || !x.inside || x.full !== HR_LONG) fail(tag + ": the long branch name is not cut inside its card with the whole name in the tooltip " + JSON.stringify(x));
      // A released branch is struck through and marked.
      const rel = await p.evaluate(() => { const b = document.querySelector('.hr-repo[data-repo="openziti/zrok"] .hr-branch.released');
        return b && { deco: getComputedStyle(b.querySelector(".hr-name")).textDecorationLine, mark: /released/.test(b.textContent) }; });
      if (!rel || !/line-through/.test(rel.deco) || !rel.mark) fail(tag + ": a released branch is not marked " + JSON.stringify(rel));
      // Copy: feedback and the value, then http.
      await hubReposCopyWorks(ctx, p, '.hr-repo[data-repo="openziti/zrok"] .hr-clone .hr-copy', "git@hub.atrium:openziti/zrok.git", tag);
      await p.click('.hr-repo[data-repo="openziti/zrok"] [data-act="mode"][data-mode="http"]');
      const origin = await p.evaluate(() => location.origin);
      await hubReposCopyWorks(ctx, p, '.hr-repo[data-repo="openziti/zrok"] .hr-clone .hr-copy', origin + "/git/hub/github/openziti/zrok.git", tag + " http");
      await hubReposCopyRows(p, tag + " after toggle");
      await hubReposEscaped(p, tag);
      if (errors.length) fail(tag + ": page errors: " + errors.join(" | "));
    } finally { await ctx.close(); }
    // On a phone: one column, nothing sideways.
    const ph = await hubReposFixturePage(browser, base, { w: 390, h: 844 });
    try {
      const col = await ph.p.evaluate(() => new Set([...document.querySelectorAll(".hr-repo")].map(c => Math.round(c.getBoundingClientRect().left))).size);
      if (col !== 1) fail(tag + ": at 390px the shelf is not one column (" + col + ")");
      await hubReposNoOverflow(ph.p, tag + " at 390");
      await ph.p.click('.hr-repo[data-repo="openziti/ziti"] .hr-more');
      await hubReposNoOverflow(ph.p, tag + " at 390, expanded");
    } finally { await ph.ctx.close(); }
    await hubReposEmptyCheck(browser, base, "shelf", tag);
    await hubReposShots(browser, base, "shelf");
  } finally { mock.hubMode = wasHub; }
  if (!bad) console.log("hubReposShelf ok");
}

async function hubReposLedgerSection(browser, base) {
  const wasHub = mock.hubMode; mock.hubMode = true;
  const tag = "hubReposLedger";
  try {
    const { ctx, p, errors } = await hubReposFixturePage(browser, base, { w: 2000, h: 1200, view: "ledger" });
    try {
      const r = await p.evaluate(() => { const li = [...document.querySelectorAll("#hubrepos-list .hr-li")], d = document.querySelector(".hr-detail");
        return { n: li.length, tags: [...new Set(li.map(b => b.tagName))].join(), cur: li.filter(b => b.getAttribute("aria-current") === "true").map(b => b.dataset.repo),
          name: d.querySelector(".hr-dname").textContent, count: d.querySelector(".hr-fact.ac b").textContent, sha: d.querySelector(".hr-fact b.mono").textContent,
          ev: d.querySelectorAll(".hr-ev").length, rel: d.querySelectorAll(".hr-ev.released").length, days: [...d.querySelectorAll(".hr-day")].map(x => x.textContent.replace(/\d+$/, "").trim()),
          nsz: parseFloat(getComputedStyle(d.querySelector(".hr-dname")).fontSize), cols: d.querySelector(".hr-cols").getBoundingClientRect().width, listW: document.querySelector(".hr-list").getBoundingClientRect().width,
          who: d.querySelectorAll(".hr-side .hr-whorow").length, avatars: d.querySelectorAll(".hr-ev .hr-av").length, term: d.querySelector(".hr-termcmd").textContent,
          chip: d.querySelector(".hr-ev .hr-chip").textContent, title: !![...d.querySelectorAll(".hr-ev .hr-title")].find(t => /link flap/.test(t.textContent)),
          cut: (() => { const n = d.querySelector('.hr-name[data-tip^="feat/oidc-device"]'); if (!n) return null;
            // Whether the name overflows its column at 2000px depends on the monospace font, and a narrow one fits it.
            // So the box is held to 200px, which no font fits it in, and the rule that cuts it is read back.
            n.style.maxWidth = "200px"; const cs = getComputedStyle(n);
            return n.scrollWidth > n.clientWidth && cs.overflow === "hidden" && cs.textOverflow === "ellipsis" && n.getAttribute("data-tip"); })() }; });
      if (r.n !== 6 || r.tags !== "BUTTON" || r.cur.join() !== "openziti/ziti") fail(tag + ": the list is wrong " + JSON.stringify(r));
      if (r.name !== "ziti" || r.count !== "12" || r.sha.length !== 7 || r.nsz < 44) fail(tag + ": the repo hero is wrong " + JSON.stringify(r));
      if (r.ev !== 12 || r.rel !== 2 || r.days.join() !== "Today,Yesterday,This week,Earlier" || r.who !== 3 || r.avatars !== 12) fail(tag + ": the timeline is wrong " + JSON.stringify(r));
      if (r.term !== "$ git clone git@hub.atrium:openziti/ziti.git" || !/^sg4 c-101$/.test(r.chip) || !r.title) fail(tag + ": the terminal or the who/when is wrong " + JSON.stringify(r));
      if (r.cut !== HR_LONG) fail(tag + ": the long branch name is not cut with its tooltip: " + r.cut);
      if (r.cols < 900 || r.listW > 420) fail(tag + ": at 2000px the panes do not use the width " + JSON.stringify({ cols: r.cols, listW: r.listW }));
      await hubReposCopyRows(p, tag);
      await hubReposCopyWorks(ctx, p, "#hubrepos-list .hr-termbar .hr-copy", "git clone git@hub.atrium:openziti/ziti.git", tag);
      await p.click('#hubrepos-list .hr-termbar [data-act="mode"][data-mode="http"]');
      const origin = await p.evaluate(() => location.origin);
      await hubReposCopyWorks(ctx, p, "#hubrepos-list .hr-termbar .hr-copy", "git clone " + origin + "/git/hub/github/openziti/ziti.git", tag + " http");
      // Open another repo: the pane follows, the pressed row moves and keyboard focus stays on the row.
      await p.focus('.hr-li[data-repo="openziti/zrok"]');
      await p.keyboard.press("Enter");
      const z = await p.evaluate(() => ({ name: document.querySelector(".hr-dname").textContent, ev: document.querySelectorAll(".hr-detail .hr-ev").length, rel: document.querySelectorAll(".hr-detail .hr-ev.released").length,
        cur: document.querySelector('.hr-li[aria-current="true"]').dataset.repo, focus: document.activeElement.dataset.repo, state: document.querySelector(".hr-detail .hr-state").textContent.trim() }));
      if (z.name !== "zrok" || z.ev !== 7 || z.rel !== 4 || z.cur !== "openziti/zrok" || z.focus !== "openziti/zrok" || z.state !== "Stale") fail(tag + ": opening zrok with the keyboard went wrong " + JSON.stringify(z));
      // Main only, then the empty repo (a hero in the pane), then all repos (a feed in the pane).
      await p.click('.hr-li[data-repo="netfoundry/docs"]');
      if (!/Only main so far/.test(await p.textContent(".hr-detail"))) fail(tag + ": a main-only repo does not say so");
      await p.click('.hr-li[data-repo="dovholuknf/atrium"]');
      const e = await p.evaluate(() => ({ hero: !!document.querySelector(".hr-detail .hr-hero"), term: !!document.querySelector(".hr-detail .hr-term"), go: document.querySelector(".hr-detail .hr-go").dataset.copy }));
      if (!e.hero || e.term || e.go !== "git remote add hub git@hub.atrium:dovholuknf/atrium.git\ngit push hub <branch>") fail(tag + ": the empty repo is not a hero in the pane " + JSON.stringify(e));
      await p.click('.hr-li[data-repo=""]');
      const a = await p.evaluate(() => ({ items: document.querySelectorAll(".hr-detail .hr-it").length, sum: !!document.querySelector(".hr-detail .hr-summary"), cur: document.querySelector('.hr-li[aria-current="true"]').dataset.repo }));
      if (a.items !== 27 || !a.sum || a.cur !== "") fail(tag + ": all repos is not the whole feed " + JSON.stringify(a));
      await hubReposEscaped(p, tag);
      if (errors.length) fail(tag + ": page errors: " + errors.join(" | "));
    } finally { await ctx.close(); }
    const ph = await hubReposFixturePage(browser, base, { w: 390, h: 844, view: "ledger" });
    try {
      const row = await ph.p.evaluate(() => getComputedStyle(document.querySelector(".hr-list")).flexDirection);
      if (row !== "row") fail(tag + ": at 390px the list is not a strip");
      const fade = await ph.p.evaluate(() => { const l = getComputedStyle(document.querySelector(".hr-list")); return (l.maskImage || l.webkitMaskImage || "none") + "|" + l.scrollSnapType; });
      if (/^none/.test(fade) || /\|none$/.test(fade)) fail(tag + ": the strip's edge cuts a repo with no fade or snap: " + fade);
      await hubReposNoOverflow(ph.p, tag + " at 390");
      await ph.p.click('.hr-li[data-repo=""]');
      await hubReposNoOverflow(ph.p, tag + " at 390, all repos");
    } finally { await ph.ctx.close(); }
    await hubReposEmptyCheck(browser, base, "ledger", tag);
    await hubReposShots(browser, base, "ledger");
  } finally { mock.hubMode = wasHub; }
  if (!bad) console.log("hubReposLedger ok");
}

// The ledger's branch timeline: each ring is centred on the one line, at two zoom levels and in a light and a dark skin.
// With DOTS_SHOT=<file> it also saves the timeline as a picture.
async function hubReposTimelineDotsSection(browser, base) {
  const wasHub = mock.hubMode; mock.hubMode = true;
  const tag = "hubReposTimelineDots";
  try {
    const { ctx, p, errors } = await hubReposFixturePage(browser, base, { w: 1600, h: 1000, view: "ledger" });
    try {
      for (const skin of ["paper", "dark"]) for (const zoom of [1, 1.5]) {
        await p.evaluate(({ s, z }) => {
          if (s === "paper") document.documentElement.setAttribute("data-skin", "paper"); else document.documentElement.removeAttribute("data-skin");
          document.body.style.zoom = String(z);
        }, { s: skin, z: zoom });
        await settleStyle(p);
        const r = await p.evaluate(() => {
          const rail = document.querySelector(".hr-detail .hr-rail"), rs = getComputedStyle(rail, "::before");
          const line = parseFloat(rs.left) + parseFloat(rs.width) / 2;
          const rings = [...rail.querySelectorAll(".hr-ev")].map(ev => { const s = getComputedStyle(ev, "::before");
            return ev.offsetLeft + ev.clientLeft + parseFloat(s.left) + parseFloat(s.width) / 2; });
          return { line, rings, flat: rs.backgroundImage === "none" };
        });
        const t = tag + " (" + skin + ", zoom " + zoom + ")";
        if (r.rings.length < 5) fail(t + ": too few rings to check " + JSON.stringify(r));
        const off = r.rings.filter(x => Math.abs(x - r.line) > 1);
        if (off.length) fail(t + ": " + off.length + " rings are off the line at x=" + r.line + ": " + off.slice(0, 3).join(","));
        if (!r.flat) fail(t + ": the line is a gradient, not one colour");
      }
      if (process.env.DOTS_SHOT) {
        await p.evaluate(() => { document.body.style.zoom = "1"; document.documentElement.setAttribute("data-skin", "paper"); });
        await settleStyle(p);
        const rail = p.locator(".hr-detail .hr-rail"); await rail.scrollIntoViewIfNeeded();
        await rail.screenshot({ path: process.env.DOTS_SHOT });
      }
      if (errors.length) fail(tag + ": page errors: " + errors.join(" | "));
    } finally { await ctx.close(); }
  } finally { mock.hubMode = wasHub; }
  if (!bad) console.log("hubReposTimelineDots ok");
}

// The repos tab's fifth view, Branches (docs/fabric/branch-status-design.md section 9, phase 2): the fixture's GET
// /_hub/git/branches, chips with counts, landed folded until asked for, how and kind on the rows, every repository with a
// choice to narrow it, a hostile branch name drawn as text, state that is this window's and never another's. With
// HR_SHOTS=<dir> it also saves the view at 2000 and 390, paper and dark.
async function hubReposBranchesSection(browser, base) {
  const wasHub = mock.hubMode; mock.hubMode = true;
  const tag = "hubReposBranches";
  try {
    const { ctx, p, errors } = await hubReposFixturePage(browser, base, { w: 1600, h: 1000 });
    try {
      await p.click('#hubrepos-views [data-hrview="branches"]');
      await p.waitForFunction(() => document.querySelector("#hubrepos-list .hr-brtable"), null, { timeout: slow(8000) });
      const read = () => p.evaluate(() => ({
        view: document.getElementById("hubrepos-list").dataset.view, stored: localStorage.getItem("atrium.reposView"),
        chips: [...document.querySelectorAll(".hr-bchip")].map(c => c.dataset.mode + ":" + c.querySelector("b").textContent + ":" + c.getAttribute("aria-pressed")).join(),
        rows: [...document.querySelectorAll(".hr-brrow")].map(r => r.querySelector(".hr-bstatus").textContent + "/" + (r.querySelector(".hr-bkind, .hr-bhow") || { textContent: "" }).textContent).join(),
        names: [...document.querySelectorAll(".hr-brrow .hr-name")].map(n => n.textContent),
        foot: (document.querySelector(".hr-bfoot") || { textContent: "" }).textContent.replace(/\s+/g, " ").trim(),
        count: document.getElementById("hubrepos-count").textContent, computed: !!document.querySelector(".hr-bcomp time[data-tip]"),
        sort: document.getElementById("hubrepos-sort").hidden, img: document.querySelectorAll("#hubrepos-list img").length, x: window.__x || 0,
        buttons: [...document.querySelectorAll("#hubrepos-list button")].map(b => b.textContent.trim()).filter(Boolean) }));
      let r = await read();
      if (r.view !== "branches" || r.stored === "branches") fail(tag + ": the view is not this window's alone " + JSON.stringify({ view: r.view, stored: r.stored }));
      if (r.chips !== "orphaned:5:true,landed:10:false") fail(tag + ": the chips are wrong " + r.chips);
      if (r.rows !== "orphaned/code,orphaned/code,orphaned/code,orphaned/docs,orphaned/docs") fail(tag + ": landed is not folded, or kind is missing " + r.rows);
      if (r.foot !== "show landed 10landed by: patch-id 4, subject 2, changelog 2, ancestry 2") fail(tag + ": the fold line is wrong " + r.foot);
      if (r.img || r.x || !r.names[0].startsWith("claude/wip-<img")) fail(tag + ": a branch name became markup " + JSON.stringify(r.names));
      if (r.count !== "15" || !r.computed || !r.sort) fail(tag + ": the head is wrong " + JSON.stringify({ count: r.count, computed: r.computed, sort: r.sort }));
      if (r.buttons.some(b => /^(land|keep|delete|push|open diff|read)$/i.test(b))) fail(tag + ": the view offers an action " + r.buttons.join());
      // Unfold landed: the how chip appears on each, and the button says how to fold it again.
      await p.click('.hr-bfoot [data-act="brchip"]');
      r = await read();
      if (!/landed\/patch-id/.test(r.rows) || !/landed\/subject/.test(r.rows) || !/landed\/changelog/.test(r.rows) || !/landed\/ancestry/.test(r.rows) || r.rows.split(",").length !== 15) fail(tag + ": unfolded rows are wrong " + r.rows);
      if (!/^fold landed/.test(r.foot) || r.chips !== "orphaned:5:true,landed:10:true") fail(tag + ": the fold control did not follow " + JSON.stringify({ foot: r.foot, chips: r.chips }));
      // Narrow to one repository: counts follow the repository, not the chips, and the repo line goes.
      await p.selectOption("#hr-br-repo", "github/openziti/zrok");
      r = await read();
      if (r.chips !== "orphaned:1:true,landed:3:true" || r.names.length !== 4) fail(tag + ": narrowing to zrok went wrong " + JSON.stringify(r));
      if ((await p.evaluate(() => document.activeElement.id)) !== "hr-br-repo") fail(tag + ": the repo choice lost focus");
      await p.selectOption("#hr-br-repo", "");
      // Kind filter narrows the orphans only.
      await p.click('[data-act="brkind"][data-mode="docs"]');
      r = await read();
      if (r.rows.split(",").filter(x => /^orphaned/.test(x)).length !== 2 || r.rows.split(",").length !== 12) fail(tag + ": kind docs is wrong " + r.rows);
      await p.click('[data-act="brkind"][data-mode=""]');
      // Turning the orphaned chip off hides its rows and leaves the count on the chip.
      await p.click('.hr-bchip[data-mode="orphaned"]');
      r = await read();
      if (/orphaned\//.test(r.rows) || !/^orphaned:5:false/.test(r.chips)) fail(tag + ": the orphaned chip did not hide its rows " + JSON.stringify(r));
      // Not shared: another window never follows this one into Branches, and its storage event does not move this one.
      const other = await ctx.newPage();
      await other.goto(base + "/", { waitUntil: "domcontentloaded" });
      await other.evaluate(() => localStorage.setItem("atrium.reposView", "ledger"));
      await p.waitForTimeout(300); // wait: a negative check, the storage event must not move this window
      if ((await read()).view !== "branches") fail(tag + ": another window's choice moved this one off Branches");
      await other.close();
      // Leaving and coming back keeps this window's chips, and the switcher walks off it by key.
      await p.click('#hubrepos-views [data-hrview="feed"]');
      await p.click('#hubrepos-views [data-hrview="branches"]');
      if ((await read()).chips.split(",")[0] !== "orphaned:5:false") fail(tag + ": the chips were forgotten on the way out");
      await p.focus('#hubrepos-views [data-hrview="branches"]');
      await p.keyboard.press("ArrowRight");
      r = await read();
      if (r.view !== "shelf" || r.stored !== "shelf") fail(tag + ": ArrowRight from Branches is not Shelf " + JSON.stringify({ view: r.view, stored: r.stored }));
      // A reload is back on the remembered view, and Branches was never it.
      await p.reload({ waitUntil: "domcontentloaded" });
      await p.waitForFunction(() => { const t = document.querySelector('.tab[data-view="hubrepos"]'); return t && !t.hidden; }, null, { timeout: slow(15000) });
      await p.evaluate(() => switchView("hubrepos"));
      await p.waitForFunction(() => hubRepos.loaded, null, { timeout: slow(10000) });
      if ((await read()).view === "branches") fail(tag + ": a reload landed on Branches");
      // The hub still measuring, then an old hub: both say so rather than draw an empty table.
      for (const [body, status, want] of [[{ computed: "", building: true, counts: {}, branches: [] }, 200, "not measured its branches yet"], [{}, 404, "not answering"]]) {
        await p.unroute(/\/_hub\/git\/branches$/);
        await p.route(/\/_hub\/git\/branches$/, rt => rt.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) }));
        await p.evaluate(() => { hubRepos.br.loaded = false; hubRepos.br.data = null; });
        await p.click('#hubrepos-views [data-hrview="branches"]');
        await p.waitForFunction(w => document.getElementById("hubrepos-list").textContent.includes(w), want, { timeout: slow(8000) })
          .catch(() => fail(tag + ": the " + status + " answer is not explained"));
        await p.evaluate(() => { hubRepos.mem = ""; });
      }
      if (errors.length) fail(tag + ": page errors: " + errors.join(" | "));
    } finally { await ctx.close(); }
    const dir = process.env.HR_SHOTS;
    if (dir) {
      require("fs").mkdirSync(dir, { recursive: true });
      for (const w of [2000, 390]) for (const skin of ["paper", "dark"]) {
        const { ctx, p } = await hubReposFixturePage(browser, base, { w, h: 900 });
        try {
          await p.click('#hubrepos-views [data-hrview="branches"]');
          await p.waitForFunction(() => document.querySelector("#hubrepos-list .hr-brtable"), null, { timeout: slow(8000) });
          await p.evaluate(s => { if (s === "paper") document.documentElement.setAttribute("data-skin", "paper"); else document.documentElement.removeAttribute("data-skin"); }, skin);
          await p.click('.hr-bfoot [data-act="brchip"]');
          await settleStyle(p);
          const h = await p.evaluate(() => { const l = document.getElementById("hubrepos-list"); return Math.ceil(l.scrollHeight + l.getBoundingClientRect().top + 24); });
          await p.setViewportSize({ width: w, height: Math.min(6000, Math.max(600, h)) });
          await settleStyle(p);
          await p.screenshot({ path: require("path").join(dir, "branches-" + w + "-" + skin + ".png") });
        } finally { await ctx.close(); }
      }
    }
  } finally { mock.hubMode = wasHub; }
  if (!bad) console.log("hubReposBranches ok");
}

async function hubReposFeedSection(browser, base) {
  const wasHub = mock.hubMode; mock.hubMode = true;
  const tag = "hubReposFeed";
  try {
    const { ctx, p, errors } = await hubReposFixturePage(browser, base, { w: 2000, h: 1200, view: "feed" });
    try {
      const r = await p.evaluate((long) => { const items = [...document.querySelectorAll("#hubrepos-list .hr-it")], chips = [...document.querySelectorAll(".hr-chipbtn")];
        const n = document.querySelector('.hr-bn[data-tip^="feat/oidc-device"]');
        return { chips: chips.length, pressed: chips.filter(c => c.getAttribute("aria-pressed") === "true").length, items: items.length, mains: document.querySelectorAll(".hr-it.main").length,
          rel: document.querySelectorAll(".hr-it.released").length, days: [...document.querySelectorAll(".hr-day")].map(x => x.textContent.replace(/\d+$/, "").trim()),
          cols: new Set(items.map(i => Math.round(i.getBoundingClientRect().left))).size, clones: [...document.querySelectorAll(".hr-cr")].map(c => c.querySelector(".hr-copy").dataset.copy),
          rooms: document.querySelectorAll(".hr-side .hr-panel:not(.hr-clones) .hr-whorow").length, big: document.querySelectorAll(".hr-summary .hr-big").length,
          cut: n && n.scrollWidth > n.clientWidth && n.getAttribute("data-tip") === long, pills: document.querySelectorAll(".hr-it .hr-pill .hr-tile").length,
          first: items[0].textContent, known: !![...document.querySelectorAll(".hr-it .hr-title")].find(t => /chase the link flap/.test(t.textContent)), hot: document.querySelectorAll(".hr-it.hot").length }; }, HR_LONG);
      if (r.chips !== 6 || r.pressed !== 1) fail(tag + ": the repo rail is wrong " + JSON.stringify(r));
      if (r.items !== 27 || r.mains !== 4 || r.rel !== 7 || r.days.join() !== "Today,Yesterday,This week,Earlier") fail(tag + ": the feed is wrong " + JSON.stringify(r));
      if (!/^sg4pushed to openziti\/ziti/.test(r.first.replace(/\s+/g, "")) && !/sg4/.test(r.first)) fail(tag + ": the newest push is not first: " + r.first);
      if (r.cols < 2 || r.pills !== 27 || !r.cut || !r.known || r.big !== 4 || r.rooms !== 3) fail(tag + ": at 2000px the feed is not laid out " + JSON.stringify(r));
      if (r.clones.length !== 5 || !r.clones.includes("git@hub.atrium:openziti/ziti.git")) fail(tag + ": the clone panel is wrong " + JSON.stringify(r.clones));
      await hubReposCopyRows(p, tag);
      await hubReposCopyWorks(ctx, p, '.hr-cr:nth-of-type(2) .hr-copy', "git@hub.atrium:openziti/zrok.git", tag);
      // Click a repo: its header, its own clone bar, only its pushes.
      await p.click('.hr-chipbtn[data-repo="openziti/zrok"]');
      const f = await p.evaluate(() => ({ head: !!document.querySelector(".hr-rhead .hr-clone"), copies: document.querySelectorAll(".hr-rhead .hr-copy").length, items: document.querySelectorAll(".hr-it").length,
        panel: !!document.querySelector(".hr-clones"), pressed: document.querySelector('.hr-chipbtn[aria-pressed="true"]').dataset.repo, name: document.querySelector(".hr-rhead .hr-repoName").textContent,
        focus: document.activeElement.dataset.repo, others: [...document.querySelectorAll(".hr-it .hr-pill")].filter(x => !/openziti\/zrok/.test(x.textContent)).length }));
      if (!f.head || f.copies !== 1 || f.items !== 8 || f.panel || f.pressed !== "openziti/zrok" || f.name !== "zrok" || f.focus !== "openziti/zrok" || f.others) fail(tag + ": filtering to zrok went wrong " + JSON.stringify(f));
      await hubReposCopyWorks(ctx, p, ".hr-rhead .hr-copy", "git@hub.atrium:openziti/zrok.git", tag + " header");
      await p.click('.hr-rhead [data-act="mode"][data-mode="http"]');
      const origin = await p.evaluate(() => location.origin);
      await hubReposCopyWorks(ctx, p, ".hr-rhead .hr-copy", origin + "/git/hub/github/openziti/zrok.git", tag + " header http");
      await hubReposCopyRows(p, tag + " filtered");
      await p.click('.hr-chipbtn[data-repo=""]');
      if ((await p.locator(".hr-it").count()) !== 27) fail(tag + ": All repos did not bring the whole feed back");
      await hubReposEscaped(p, tag);
      if (errors.length) fail(tag + ": page errors: " + errors.join(" | "));
    } finally { await ctx.close(); }
    const ph = await hubReposFixturePage(browser, base, { w: 390, h: 844, view: "feed" });
    try {
      await hubReposNoOverflow(ph.p, tag + " at 390");
      await ph.p.click('.hr-chipbtn[data-repo="openziti/ziti"]');
      await hubReposNoOverflow(ph.p, tag + " at 390, one repo");
    } finally { await ph.ctx.close(); }
    await hubReposEmptyCheck(browser, base, "feed", tag);
    await hubReposShots(browser, base, "feed");
  } finally { mock.hubMode = wasHub; }
  if (!bad) console.log("hubReposFeed ok");
}

const units = [
  { name: "hubRepos", run: ({ browser, base }) => hubReposSection(browser, base) },
  { name: "hubReposShelf", run: ({ browser, base }) => hubReposShelfSection(browser, base) },
  { name: "hubReposLedger", run: ({ browser, base }) => hubReposLedgerSection(browser, base) },
  { name: "hubReposTimelineDots", run: ({ browser, base }) => hubReposTimelineDotsSection(browser, base) },
  { name: "hubReposFeed", run: ({ browser, base }) => hubReposFeedSection(browser, base) },
  { name: "hubReposBranches", run: ({ browser, base }) => hubReposBranchesSection(browser, base) },
];
const sections = { hubRepos: hubReposSection, hubReposShelf: hubReposShelfSection, hubReposLedger: hubReposLedgerSection, hubReposFeed: hubReposFeedSection, hubReposBranches: hubReposBranchesSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
