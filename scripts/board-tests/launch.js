// launch: starting work: the launch dropdowns, the new work dialog, throwaway runners.
//
// Units: launchLink, launchThrowaway, newWorkDialog.
// `units` is what the harness runs, in suite order. `sections` is for HEADLESS_ONLY, which names a section and not a unit.
// Alone: `node scripts/board-tests/launch.js` runs just these units, the way the full run would.
const path = require("path");
const { slow } = require("../lib/browser.js");
const { mock } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");

// THE TEMPORARY-DIRECTORY TICK in the new-agent dialog: ticked, the path box goes quiet and the launch carries
// throwaway:true with no directory (and no clean-up tick, a throwaway has nothing of a link's to clean up). Unticked, the
// path is back and is what is sent. A resume does not offer it. THROWAWAY_SHOTS=<dir> writes the dialog, light and dark.
async function launchThrowawaySection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const bodies = [];
  try {
    const p = await ctx.newPage();
    const errors = [];
    p.on("pageerror", e => errors.push(e.message));
    await p.route(/\/v1\/launch(\?|$)/, async r => {
      bodies.push(JSON.parse(r.request().postData() || "{}"));
      await r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ id: "t1", status: "running" }) });
    });
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof openLaunch === "function" && typeof doLaunch === "function");
    const state = () => p.evaluate(() => ({
      field: !!document.getElementById("l-throwaway-field") && !document.getElementById("l-throwaway-field").hidden, on: !!(document.getElementById("l-throwaway-on") || {}).checked,
      cwdOff: document.getElementById("l-cwd").disabled, cwd: document.getElementById("l-cwd").value,
      cleanup: !!document.getElementById("l-cleanup-field") && !document.getElementById("l-cleanup-field").hidden }));
    await p.evaluate(async () => { await openLaunch(null, "", "/work/here", null, null); });
    let s = await state();
    if (!s.field || s.on || s.cwdOff || s.cwd !== "/work/here") fail("launchThrowaway: a fresh dialog " + JSON.stringify(s));
    if (process.env.THROWAWAY_SHOTS) {
      for (const theme of ["light", "dark"]) {
        await p.emulateMedia({ colorScheme: theme });
        await p.evaluate(t => { document.documentElement.dataset.theme = t; }, theme);
        await p.locator("dialog#launch").screenshot({ path: process.env.THROWAWAY_SHOTS + "/launch-" + (process.env.THROWAWAY_TAG || "after") + "-" + theme + ".png" });
      }
    }
    await p.click("#l-throwaway-on");
    s = await state();
    if (!s.on || !s.cwdOff || s.cwd !== "/work/here") fail("launchThrowaway: ticking left the path box live or lost its value " + JSON.stringify(s));
    await p.evaluate(() => doLaunch());
    await p.waitForFunction(() => !document.getElementById("launch").open, null, { timeout: slow(5000) }).catch(() => fail("launchThrowaway: the dialog stayed open"));
    if (bodies.length !== 1 || bodies[0].throwaway !== true || bodies[0].cwd !== "") fail("launchThrowaway: ticked body " + JSON.stringify(bodies));
    await p.evaluate(async () => { await openLaunch(null, "", "/work/here", null, null); });
    s = await state();
    if (s.on || s.cwdOff) fail("launchThrowaway: the tick carried over to the next open " + JSON.stringify(s));
    await p.evaluate(() => doLaunch());
    await p.waitForFunction(() => !document.getElementById("launch").open, null, { timeout: slow(5000) }).catch(() => fail("launchThrowaway: the dialog stayed open"));
    const b = bodies[1] || {};
    if (b.throwaway || b.cwd !== "/work/here") fail("launchThrowaway: unticked body " + JSON.stringify(bodies));
    await p.evaluate(async () => { await openLaunch(null, "abc", "/work/here", null, null); });
    if ((await state()).field) fail("launchThrowaway: a resume offered a throwaway");
    await p.evaluate(() => document.getElementById("launch").close());
    if (errors.length) fail("launchThrowaway: page errors " + errors.join(" | "));
  } finally {
    await ctx.close();
  }
  if (!bad) console.log("launchThrowaway ok");
}

// THE LAUNCH DIALOG'S `repo` LIST (js/launchrepo.js, docs/changes/launch-repo-picker.md). `repo` opens a panel inside the
// dialog and never a second modal. It lists the room's checkouts (GET /v1/checkouts) and the hub's repos
// (GET /_hub/git/repos) and asks for NO providers. Pick fills the directory. A hub repo the room has no checkout of
// offers `clone from hub`, which POSTs /v1/checkouts/clone to the room the dialog's room control names. The last answers
// paint at once on the next open, whatever the network is doing. Under the list is `start new work` (POST /v1/new), which
// keeps its refusal-in-the-dialog behaviour. NEWWORK_SHOTS=<dir> writes the dialog with the list open.
async function newWorkDialogSection(browser, base) {
  const wasHub = mock.hubMode;
  mock.hubMode = true;
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 1000 } });
  const asked = [];
  const cloned = [];
  const listed = [];
  let providersAsked = 0;
  let refuse = "";
  let cloneRefuse = "";
  let hubInitRefuse = "";
  const hubInit = [];
  const order = [];
  let holdList = 0;
  try {
    const p = await ctx.newPage();
    const errors = [];
    p.on("pageerror", e => errors.push(e.message));
    await p.route(/\/v1\/providers(\/|\?|$)/, r => { providersAsked++; r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ providers: [] }) }); });
    const checkoutsByRoom = {
      "": [
        { repo: "ziti", slug: "github/openziti/ziti", path: "/work/ziti-wt/fix-it", branch: "fix/it", linked: true, root: "/work/ziti", last: "2026-10-14T10:00:00Z" },
        { repo: "ziti", slug: "github/openziti/ziti", path: "/work/ziti", branch: "main", linked: false, root: "/work/ziti", last: "2026-10-13T10:00:00Z" },
        { repo: "scratch", slug: "", path: "/work/scratch", branch: "", linked: false, root: "/work/scratch", last: "2026-10-12T10:00:00Z" }],
      w2: [{ repo: "other", slug: "github/o/r", path: "/work/w2/other", branch: "main", linked: false, root: "/work/w2/other", last: "2026-10-12T10:00:00Z" }]
    };
    await p.route(/\/v1\/checkouts(\?|$)/, async r => {
      const room = r.request().headers()["x-atrium-room"] || "";
      listed.push(room);
      if (holdList) await new Promise(res => setTimeout(res, holdList));
      r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ checkouts: checkoutsByRoom[room] || [] }) });
    });
    await p.route(/\/v1\/checkouts\/clone(\?|$)/, r => {
      cloned.push({ room: r.request().headers()["x-atrium-room"] || "", body: JSON.parse(r.request().postData() || "{}") });
      order.push("room");
      if (cloneRefuse) return r.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: cloneRefuse }) });
      const repo = cloned[cloned.length - 1].body.repo;
      r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ path: "/work/git/" + (repo.startsWith("github/") ? repo : repo), state: "cloned" }) });
    });
    await p.route(/\/_hub\/git\/init$/, r => {
      hubInit.push(JSON.parse(r.request().postData() || "{}"));
      order.push("hub");
      if (hubInitRefuse) return r.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: hubInitRefuse }) });
      r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ repo: "gitlab.example.com/team/tool", created: true }) });
    });
    await p.route(/\/_hub\/git\/repos$/, r => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ repos: [
      { host: "github", owner: "openziti", repo: "ziti", url: "u", path: "/p", main: { sha: "", at: null }, branches: [] },
      { host: "github", owner: "acme", repo: "widget", url: "u", path: "/p", main: { sha: "", at: null }, branches: [] }] }) }));
    await p.route(/\/v1\/new(\?|$)/, r => {
      asked.push(JSON.parse(r.request().postData() || "{}"));
      if (refuse) return r.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: refuse }) });
      return r.fulfill({ status: 201, contentType: "application/json", body: JSON.stringify({ card: "nw1", worktree: "/wt/o/r/some.work", repo: "github.com/o/r", created: true }) });
    });
    await p.route(/\/v1\/tasks\/nw1(\?|$)/, r => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ id: "nw1", title: "some.work", status: "running", worktree: "/wt/o/r/some.work" }) }));
    await p.route(/\/v1\/browse(\?|$)/, r => r.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ error: "the disk said no" }) }));
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof openLaunch === "function" && typeof newWorkGo === "function" && hubIsHub, null, { timeout: slow(15000) });
    const seen = () => p.evaluate(() => {
      const shown = id => { const e = document.getElementById(id); return !!e && !e.hidden; };
      return { launch: document.getElementById("launch").open, ask: document.getElementById("ask").open,
        panel: shown("l-nw"), cwd: document.getElementById("l-cwd").value,
        rows: [...document.querySelectorAll("#l-nw-list .pvrepo")].map(e => e.querySelector(".nm").textContent + "|" + (e.querySelector("button") ? "button" : "")),
        text: document.getElementById("l-nw-list").textContent,
        repos: [...document.getElementById("l-nw-repo").options].map(o => o.value),
        note: document.getElementById("l-nw-note").textContent, bnote: document.getElementById("b-note").textContent };
    });
    const shot = async name => {
      if (process.env.NEWWORK_SHOTS) await p.locator("dialog#launch").screenshot({ path: process.env.NEWWORK_SHOTS + "/" + name + ".png" });
    };
    const openList = async () => {
      await p.evaluate(async () => { await openLaunch(null, "", "/work/here", null, null); });
      await p.click("#l-nw-btn");
      await p.waitForFunction(() => document.querySelectorAll("#l-nw-list .pvrepo").length, null, { timeout: slow(5000) });
    };

    // ONE list, one row per repository, the host/org/repo slug and nothing else, a checkout with no slug its folder name: this room's checkouts newest first (a repo with a
    // main checkout and a worktree is one row), then the hub's repos it has none of. No badge, no clone button.
    await openList();
    let s = await seen();
    if (!s.launch || s.ask || !s.panel) fail("repoList: the panel did not open inline " + JSON.stringify(s));
    const want = ["github/openziti/ziti|", "scratch|", "github/acme/widget|"];
    if (s.rows.join() !== want.join()) fail("repoList: the rows were " + JSON.stringify(s.rows));
    if (s.text.includes("/work/") || s.text.includes("fix/it") || s.text.includes("worktree")) fail("repoList: a row shows more than the repo name: " + s.text);
    if (/on the hub|clone from hub/.test(s.text) || await p.locator("#l-nw-list button, #l-nw-list .chip").count()) fail("repoList: the hub badge or the clone button is still drawn: " + s.text);
    if (s.repos.join() !== "github.com/acme/widget,github.com/openziti/ziti") fail("repoList: the new-work repos were " + JSON.stringify(s.repos));
    if (providersAsked) fail("repoList: opening the list asked for providers " + providersAsked + " times");
    await shot("launch-repo-list");

    // the filter narrows what is drawn and reads nothing
    const before = listed.length;
    await p.fill("#l-nw-filter", "scratch");
    s = await seen();
    if (s.rows.join() !== "scratch|" || listed.length !== before) fail("repoList: the filter " + JSON.stringify(s.rows) + " reads " + (listed.length - before));
    await p.fill("#l-nw-filter", "acme");
    s = await seen();
    if (s.rows.join() !== "github/acme/widget|") fail("repoList: the filter did not find the hub repo " + JSON.stringify(s.rows));
    await p.fill("#l-nw-filter", "zzz");
    if (!(await seen()).text.includes("nothing matches")) fail("repoList: an empty filter does not say so");
    await p.fill("#l-nw-filter", "");

    // pick a repo the room has: its main checkout, not the worktree, and nothing is cloned
    await p.locator("#l-nw-list .pvrepo").first().click();
    s = await seen();
    if (s.cwd !== "/work/ziti" || s.panel || !s.launch) fail("repoList: picking did not fill the directory " + JSON.stringify({ cwd: s.cwd, panel: s.panel }));
    if (cloned.length || hubInit.length) fail("repoList: picking a checkout cloned something " + JSON.stringify({ cloned, hubInit }));

    // pick a repo the room lacks: it is cloned from the hub, to the room the dialog names, with no further click. A
    // refusal is a line in the dialog and nothing closes.
    await p.evaluate(() => {
      document.getElementById("l-room-field").hidden = false;
      document.getElementById("l-room").innerHTML = '<option value="">here</option><option value="w2">w2</option>';
      document.getElementById("l-room").value = "";
    });
    await p.click("#l-nw-btn");
    await p.waitForFunction(() => document.querySelectorAll("#l-nw-list .pvrepo").length, null, { timeout: slow(5000) });
    cloneRefuse = "this room has no hub to clone from";
    await p.locator("#l-nw-list .pvrepo", { hasText: "widget" }).click();
    await p.waitForFunction(() => document.getElementById("l-nw-note").textContent.includes("no hub"), null, { timeout: slow(5000) })
      .catch(() => fail("repoList: a refused clone was not said in the dialog"));
    s = await seen();
    if (!s.launch || !s.panel || s.ask || !s.note.includes("clone github/acme/widget")) fail("repoList: a refused clone closed something or lost its name " + JSON.stringify(s));
    if (!(await p.locator("#l-nw-note.warn").count())) fail("repoList: a refused clone is not marked as a warning");
    cloneRefuse = "";
    await p.locator("#l-nw-list .pvrepo", { hasText: "widget" }).click();
    await p.waitForFunction(() => document.getElementById("l-cwd").value === "/work/git/github/acme/widget", null, { timeout: slow(5000) })
      .catch(() => fail("repoList: picking a repo the room lacks did not fill the directory"));
    if (cloned.length !== 2 || cloned[1].body.repo !== "github/acme/widget" || cloned[1].room !== "") fail("repoList: the clone calls were " + JSON.stringify(cloned));
    if (hubInit.length) fail("repoList: a repo the hub has was fetched onto the hub again " + JSON.stringify(hubInit));
    if ((await seen()).panel) fail("repoList: the panel stayed open after a clone");

    // a repo the hub lacks, typed as host/org/repo: the hub clones it from the forge, THEN the room clones from the hub
    await p.click("#l-nw-btn");
    await p.waitForFunction(() => document.querySelectorAll("#l-nw-list .pvrepo").length, null, { timeout: slow(5000) });
    await p.fill("#l-nw-filter", "gitlab.example.com/team/tool");
    s = await seen();
    if (s.rows.join() !== "gitlab.example.com/team/tool|") fail("repoList: a typed repo was not offered as a row " + JSON.stringify(s.rows));
    order.length = 0;
    hubInitRefuse = "the forge refused: repository not found";
    await p.locator("#l-nw-list .pvrepo").click();
    await p.waitForFunction(() => document.getElementById("l-nw-note").textContent.includes("forge refused"), null, { timeout: slow(5000) })
      .catch(() => fail("repoList: a forge refusal was not said in the dialog"));
    s = await seen();
    if (!s.panel || !s.launch || s.cwd !== "/work/git/github/acme/widget" || order.join() !== "hub") fail("repoList: a forge refusal went on or closed " + JSON.stringify({ s, order }));
    await shot("launch-repo-error");
    hubInitRefuse = "";
    await p.locator("#l-nw-list .pvrepo").click();
    await p.waitForFunction(() => document.getElementById("l-cwd").value === "/work/git/gitlab.example.com/team/tool", null, { timeout: slow(5000) })
      .catch(() => fail("repoList: a typed repo did not fill the directory"));
    if (order.join() !== "hub,hub,room") fail("repoList: the hub was not asked before the room: " + order.join());
    if (hubInit[hubInit.length - 1].url !== "https://gitlab.example.com/team/tool" || cloned[cloned.length - 1].body.repo !== "gitlab.example.com/team/tool") fail("repoList: typed calls were " + JSON.stringify({ hubInit, cloned }));
    // a URL is the same thing, and a typed repo the list already has is not offered a second time
    await p.click("#l-nw-btn");
    await p.waitForFunction(() => document.querySelectorAll("#l-nw-list .pvrepo").length, null, { timeout: slow(5000) });
    await p.fill("#l-nw-filter", "https://github.com/acme/widget.git");
    s = await seen();
    if (s.rows.length > 1) fail("repoList: a typed repo the list has was offered twice " + JSON.stringify(s.rows));
    await p.fill("#l-nw-filter", "https://gitlab.example.com/team/tool.git");
    if ((await seen()).rows.join() !== "gitlab.example.com/team/tool|") fail("repoList: a typed URL was not offered");
    await p.evaluate(() => { document.getElementById("l-nw").hidden = true; });
    // the room control decides the list and where a clone lands
    await p.evaluate(() => { document.getElementById("l-room").value = "w2"; });
    await p.click("#l-nw-btn");
    await p.waitForFunction(() => [...document.querySelectorAll("#l-nw-list .pvrepo .nm")].some(e => e.textContent === "github/o/r"), null, { timeout: slow(5000) })
      .catch(() => fail("repoList: the room control did not change the list"));
    if (!listed.includes("w2")) fail("repoList: w2's checkouts were never asked for: " + JSON.stringify(listed));
    await p.evaluate(() => { document.getElementById("l-room").value = ""; document.getElementById("l-room").dispatchEvent(new Event("change", { bubbles: true })); });
    await p.waitForFunction(() => [...document.querySelectorAll("#l-nw-list .pvrepo .nm")].some(e => e.textContent === "scratch"), null, { timeout: slow(5000) })
      .catch(() => fail("repoList: changing the room control did not repaint an open list"));
    await p.evaluate(() => { document.getElementById("l-room").value = "w2"; document.getElementById("l-room").dispatchEvent(new Event("change", { bubbles: true })); });
    await p.waitForFunction(() => [...document.querySelectorAll("#l-nw-list .pvrepo .nm")].some(e => e.textContent === "github/acme/widget"), null, { timeout: slow(5000) });
    await p.locator("#l-nw-list .pvrepo", { hasText: "widget" }).click();
    await p.waitForFunction(() => document.getElementById("l-cwd").value === "/work/git/github/acme/widget" && document.getElementById("l-nw").hidden, null, { timeout: slow(5000) });
    const last = cloned[cloned.length - 1];
    if (last.room !== "w2" || last.body.repo !== "github/acme/widget") fail("repoList: the clone did not go to the room the dialog names " + JSON.stringify(last));
    await p.evaluate(() => { document.getElementById("l-room").value = ""; document.getElementById("l-room-field").hidden = true; });

    // the last answer paints on the next open before the network has answered
    holdList = 1500;
    await p.evaluate(async () => { await openLaunch(null, "", "/work/here", null, null); });
    const t0 = Date.now();
    await p.click("#l-nw-btn");
    await p.waitForFunction(() => document.querySelectorAll("#l-nw-list .pvrepo").length >= 3, null, { timeout: slow(1200) })
      .catch(() => fail("repoList: a second open waited on the network instead of painting what it had"));
    if (Date.now() - t0 > slow(400)) fail("repoList: a second open took " + (Date.now() - t0) + "ms to paint");
    holdList = 0;

    // the directory picker's failure is a line in the picker
    await p.evaluate(async () => { await openLaunch(null, "", "", null, null); await openBrowse(); });
    s = await seen();
    if (s.ask || !s.bnote.includes("cannot browse")) fail("repoList: a browse failure was not inline " + JSON.stringify(s));
    await p.evaluate(() => document.getElementById("browse").close());

    // text selection: a drag-select in an input that ends on the backdrop keeps the dialog open and the selection
    await p.evaluate(async () => { await openLaunch(null, "", "/work/some/long/path", null, null); });
    {
      const el = p.locator("#l-cwd");
      await el.fill("alpha bravo charlie delta echo");
      const bx = await el.boundingBox();
      const y = bx.y + bx.height / 2;
      const selOf = () => p.evaluate(() => { const e = document.getElementById("l-cwd"); return [e.selectionStart, e.selectionEnd]; });
      await p.mouse.move(bx.x + 8, y); await p.mouse.down(); await p.mouse.move(bx.x + 120, y, { steps: 6 }); await p.mouse.up();
      const inside = await selOf();
      if (inside[1] - inside[0] < 3) fail("selection: a drag inside the input selected nothing " + inside);
      await p.evaluate(() => { const e = document.getElementById("l-cwd"); e.setSelectionRange(0, 0); });
      await p.mouse.move(bx.x + 60, y); await p.mouse.down(); await p.mouse.move(2, 2, { steps: 12 }); await p.mouse.up();
      const out = await selOf();
      const st = await p.evaluate(() => ({ open: document.getElementById("launch").open, focus: document.activeElement.id }));
      if (!st.open) fail("selection: a drag ending on the backdrop closed the dialog");
      if (out[0] !== 0 || out[1] < 3) fail("selection: a drag ending on the backdrop lost the selection " + out);
      await p.mouse.dblclick(bx.x + 30, y);
      const word = await selOf();
      if (word[1] - word[0] < 3) fail("selection: a double-click did not select a word " + word);
      await p.mouse.click(bx.x + 8, y);
      await p.keyboard.down("Shift"); await p.mouse.click(bx.x + 100, y); await p.keyboard.up("Shift");
      const shift = await selOf();
      if (shift[1] - shift[0] < 3) fail("selection: shift-click did not extend " + shift);
      const lab = p.locator("#launch .dlg-sec-h").first();
      const lb = await lab.boundingBox();
      await p.mouse.move(lb.x + 2, lb.y + 6); await p.mouse.down(); await p.mouse.move(lb.x + 100, lb.y + 6, { steps: 6 }); await p.mouse.up();
      if (!(await p.evaluate(() => String(getSelection()))).length) fail("selection: label text could not be selected");
      if (!(await p.evaluate(() => document.getElementById("launch").open))) fail("selection: selecting label text closed the dialog");
    }

    // start new work: a refusal is a line in the dialog with the form still filled in, then the call
    await openList();
    await p.selectOption("#l-nw-repo", "github.com/openziti/ziti");
    await p.fill("#l-nw-name", "some.work");
    await p.fill("#l-nw-from", "release");
    refuse = "no such ref: release";
    await p.click("#l-nw-go");
    await p.waitForFunction(() => document.getElementById("l-nw-note").textContent.includes("no such ref"), null, { timeout: slow(5000) })
      .catch(() => fail("newWork: a refusal was not said in the dialog"));
    s = await seen();
    if (!s.launch || s.ask || await p.inputValue("#l-nw-name") !== "some.work") fail("newWork: a refusal closed the dialog or lost the form " + JSON.stringify(s));
    refuse = "";
    await p.click("#l-nw-go");
    await p.waitForFunction(() => !document.getElementById("launch").open, null, { timeout: slow(5000) }).catch(() => fail("newWork: the dialog stayed open"));
    const b = asked[asked.length - 1] || {};
    if (asked.length !== 2 || b.repo !== "github.com/openziti/ziti" || b.name !== "some.work" || b.from !== "release") fail("newWork: the call was " + JSON.stringify(asked));
    if (providersAsked) fail("repoList: providers were asked for " + providersAsked + " times over the whole dialog");
    if (errors.length) fail("repoList: page errors " + errors.join(" | "));
  } finally {
    mock.hubMode = wasHub;
    await ctx.close();
  }
  if (!bad) console.log("newWorkDialog ok");
}

// A board link clicked outside the browser reaches an installed app window through launchQueue, and is routed by the same
// openAtriumLink a link printed in a terminal uses. No launchQueue means nothing is registered.
async function launchLinkSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  const errors = [];
  p.on("pageerror", e => errors.push(String(e && e.message || e)));
  try {
    await p.addInitScript(() => {
      window.__consumer = null;
      Object.defineProperty(window, "launchQueue", { configurable: true, value: { setConsumer: fn => { window.__consumer = fn; } } });
    });
    await p.goto(base + "/", { waitUntil: "load" });
    await p.waitForTimeout(1200);
    if (!(await p.evaluate(() => typeof window.__consumer === "function"))) { fail("launchLink: the board registered no launchQueue consumer."); return; }
    await p.evaluate(() => { window.__lands = []; window.landOnAlert = (...a) => { window.__lands.push(a); }; });
    await p.evaluate(u => window.__consumer({ targetURL: u }), base + "/#term=s1");
    await p.waitForTimeout(200);
    let lands = await p.evaluate(() => window.__lands);
    if (lands.length !== 1 || lands[0][0] !== "s1") fail("launchLink: a #term= link landed " + JSON.stringify(lands));
    await p.evaluate(() => window.__consumer({ targetURL: "https://example.com/#term=s2" }));
    await p.evaluate(() => window.__consumer({}));
    lands = await p.evaluate(() => window.__lands);
    if (lands.length !== 1) fail("launchLink: a foreign or empty launch landed " + JSON.stringify(lands));
    const man = await p.evaluate(() => document.querySelector('link[rel="manifest"]') && document.querySelector('link[rel="manifest"]').getAttribute("href"));
    if (man !== "/manifest.webmanifest") fail("launchLink: the board links manifest " + man);
    // no launchQueue: boots clean and registers nothing
    const ctx2 = await browser.newContext();
    const q = await ctx2.newPage();
    const e2 = [];
    q.on("pageerror", e => e2.push(String(e && e.message || e)));
    await q.goto(base + "/", { waitUntil: "load" });
    await q.waitForTimeout(800);
    if (e2.length) fail("launchLink: without launchQueue the board threw " + e2.join(" | "));
    await ctx2.close();
  } finally { await ctx.close(); }
  if (errors.length) fail("launchLink: threw " + errors.join(" | "));
  if (!bad) console.log("launchLink ok");
}

const units = [
  { name: "launchLink", run: ({ browser, base }) => launchLinkSection(browser, base) },
  { name: "launchThrowaway", run: ({ browser, base }) => launchThrowawaySection(browser, base) },
  { name: "newWorkDialog", run: ({ browser, base }) => newWorkDialogSection(browser, base) },
];
const sections = { launchLink: launchLinkSection, launchThrowaway: launchThrowawaySection, newWorkDialog: newWorkDialogSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
