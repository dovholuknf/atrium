// launch: starting work: the launch dropdowns, the new work dialog, throwaway runners.
//
// Units: launchRepoUrls, launchLink, launchThrowaway, launchSetup, launchPresets, newWorkDialog.
// `units` is what the harness runs, in suite order. `sections` is for HEADLESS_ONLY, which names a section and not a unit.
// Alone: `node scripts/board-tests/launch.js` runs just these units, the way the full run would.
const path = require("path");
const { slow, frames } = require("../lib/browser.js");
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

// THE SETUP SELECT in the launch dialog (docs/changes/r-superlean-launch-p3.md): full setup, lean, superlean, for a claude
// runner only. Full setup (the default) sends neither flag, lean sends lean:true, superlean sends superlean:true alone. It
// resets to full setup on every open, and is hidden for another runner and for a resume. SETUP_SHOTS=<dir> writes the dialog.
async function launchSetupSection(browser, base) {
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
    const state = () => p.evaluate(() => ({ shown: !document.getElementById("l-setup-field").hidden,
      value: document.getElementById("l-setup").value, options: [...document.getElementById("l-setup").options].map(o => o.value) }));
    const launch = async setup => {
      await p.evaluate(async () => { await openLaunch(null, "", "/work/here", null, null); });
      if (setup) await p.selectOption("#l-setup", setup);
      await p.evaluate(() => doLaunch());
      await p.waitForFunction(() => !document.getElementById("launch").open, null, { timeout: slow(5000) }).catch(() => fail("launchSetup: the dialog stayed open"));
      return bodies[bodies.length - 1] || {};
    };
    await p.evaluate(async () => { await openLaunch(null, "", "/work/here", null, null); });
    let s = await state();
    if (!s.shown || s.value !== "full" || s.options.join() !== "full,lean,superlean") fail("launchSetup: a fresh dialog " + JSON.stringify(s));
    if (process.env.SETUP_SHOTS) {
      await p.selectOption("#l-setup", "superlean");
      await p.locator("dialog#launch").screenshot({ path: process.env.SETUP_SHOTS + "/launch-setup.png" });
    }
    await p.evaluate(() => document.getElementById("launch").close());
    let b = await launch("");
    if ("lean" in b || "superlean" in b) fail("launchSetup: full setup sent a flag " + JSON.stringify(b));
    b = await launch("lean");
    if (b.lean !== true || "superlean" in b) fail("launchSetup: lean body " + JSON.stringify(b));
    b = await launch("superlean");
    if (b.superlean !== true || "lean" in b) fail("launchSetup: superlean body " + JSON.stringify(b));
    await p.evaluate(async () => { await openLaunch(null, "", "/work/here", null, null); });
    if ((await state()).value !== "full") fail("launchSetup: the choice carried over to the next open");
    // another runner and a resume are not offered it
    await p.evaluate(() => { document.getElementById("l-setup").value = "lean"; setLaunchModel({ id: "x", cmd: "C:\\bin\\codex.exe" }); });
    s = await state();
    if (s.shown || s.value !== "full") fail("launchSetup: a runner that is not claude was offered it " + JSON.stringify(s));
    await p.evaluate(() => document.getElementById("launch").close());
    await p.evaluate(async () => { await openLaunch(null, "abc", "/work/here", null, null); });
    if ((await state()).shown) fail("launchSetup: a resume was offered it");
    await p.evaluate(() => document.getElementById("launch").close());
    if (errors.length) fail("launchSetup: page errors " + errors.join(" | "));
  } finally {
    await ctx.close();
  }
  if (!bad) console.log("launchSetup ok");
}

// THE NAMED PROMPTS in the launch dialog: the enabled ones are offered over the first instruction, a pick fills the
// box and the launch sends that text. None enabled hides the list, and every open starts unpicked.
async function launchPresetsSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const bodies = [];
  try {
    const p = await ctx.newPage();
    const errors = [];
    p.on("pageerror", e => errors.push(e.message));
    let actions = [
      { id: "critique", label: "critique", prompt: "Critique the branch.", enabled: true },
      { id: "off", label: "off", prompt: "Not offered.", enabled: false },
    ];
    await p.route(/\/v1\/actions(\?|$)/, r => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ actions }) }));
    await p.route(/\/v1\/launch(\?|$)/, async r => {
      bodies.push(JSON.parse(r.request().postData() || "{}"));
      await r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ id: "t1", status: "running" }) });
    });
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof openLaunch === "function" && typeof doLaunch === "function");
    const state = () => p.evaluate(() => ({ shown: !document.getElementById("l-preset").hidden,
      value: document.getElementById("l-preset").value, options: [...document.getElementById("l-preset").options].map(o => o.value) }));
    await p.evaluate(async () => { await openLaunch(null, "", "/work/here", null, null); });
    await p.waitForFunction(() => !document.getElementById("l-preset").hidden, null, { timeout: slow(5000) }).catch(() => fail("launchPresets: the list never showed"));
    const s = await state();
    if (s.value !== "" || s.options.join() !== ",critique") fail("launchPresets: a fresh dialog " + JSON.stringify(s));
    await p.selectOption("#l-preset", "critique");
    if (await p.inputValue("#l-prompt") !== "Critique the branch.") fail("launchPresets: the pick did not fill the box");
    await p.evaluate(() => doLaunch());
    await p.waitForFunction(() => !document.getElementById("launch").open, null, { timeout: slow(5000) }).catch(() => fail("launchPresets: the dialog stayed open"));
    if ((bodies[0] || {}).prompt !== "Critique the branch.") fail("launchPresets: body " + JSON.stringify(bodies));
    actions = [];
    await p.evaluate(async () => { await openLaunch(null, "", "/work/here", null, null); });
    await p.waitForFunction(() => document.getElementById("l-preset").hidden && document.getElementById("l-preset").value === "", null, { timeout: slow(5000) })
      .catch(async () => fail("launchPresets: none enabled still offered or kept the pick " + JSON.stringify(await state())));
    await p.evaluate(() => document.getElementById("launch").close());
    if (errors.length) fail("launchPresets: page errors " + errors.join(" | "));
  } finally {
    await ctx.close();
  }
  if (!bad) console.log("launchPresets ok");
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
    // The list is held 1500ms, so painting before that is the claim. The click and the poll are the harness's round trips and
    // cost a few hundred ms on a loaded runner: the budget sits well under the hold and well over those.
    if (Date.now() - t0 > slow(1000)) fail("repoList: a second open took " + (Date.now() - t0) + "ms to paint");
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
    await p.waitForFunction(() => typeof window.__consumer === "function", null, { timeout: slow(5000) }).catch(() => {});
    if (!(await p.evaluate(() => typeof window.__consumer === "function"))) { fail("launchLink: the board registered no launchQueue consumer."); return; }
    await p.evaluate(() => { window.__lands = []; window.landOnAlert = (...a) => { window.__lands.push(a); }; });
    await p.evaluate(u => window.__consumer({ targetURL: u }), base + "/#term=s1");
    await p.waitForFunction(() => window.__lands.length > 0, null, { timeout: slow(5000) }).catch(() => {});
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
    const streamed = q.waitForRequest(r => new URL(r.url()).pathname.startsWith("/v1/events"), { timeout: slow(15000) }).catch(() => {});
    await q.goto(base + "/", { waitUntil: "load" });
    await streamed;
    await frames(q, 2);
    if (e2.length) fail("launchLink: without launchQueue the board threw " + e2.join(" | "));
    await ctx2.close();
  } finally { await ctx.close(); }
  if (errors.length) fail("launchLink: threw " + errors.join(" | "));
  if (!bad) console.log("launchLink ok");
}

// A REPO LINK IN THE LAUNCH DIALOG (docs/changes/bug-launch-repo-box-takes-urls.md). Every form a person copies reduces to
// host/org/repo through the one parser (parseRepoText, js/repolink.js). Pasted into the working directory box it moves to
// the repo panel; typed in the filter it selects the repo (or says it will be fetched), `start new work` waits until the
// box matches, and an empty name is made. LAUNCHURL_SHOTS=<dir> writes the dialog after Ryan's URL, named by LAUNCHURL_TAG.
async function launchRepoUrlsSection(browser, base) {
  const wasHub = mock.hubMode;
  mock.hubMode = true;
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 1000 } });
  const calls = [];
  const ryan = "https://github.com/dovholuknf/dotfiles/tree/main/claude/skills/slop-review";
  try {
    const p = await ctx.newPage();
    const errors = [];
    p.on("pageerror", e => errors.push(e.message));
    await p.route(/\/v1\/checkouts(\?|$)/, r => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ checkouts: [
      { repo: "netfoundry", slug: "bitbucket.org/netfoundry/netfoundry", path: "/work/nf", branch: "main", linked: false, root: "/work/nf", last: "2026-10-14T10:00:00Z" }] }) }));
    await p.route(/\/_hub\/git\/repos$/, r => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ repos: [
      { host: "github.com", owner: "acme", repo: "widget", url: "u", path: "/p", main: { sha: "", at: null }, branches: [] }] }) }));
    await p.route(/\/_hub\/git\/init$/, r => { calls.push(["init", JSON.parse(r.request().postData() || "{}").url]); r.fulfill({ status: 200, contentType: "application/json", body: "{}" }); });
    await p.route(/\/v1\/new(\?|$)/, r => {
      const b = JSON.parse(r.request().postData() || "{}");
      calls.push([b.prepare ? "prepare" : "new", b.repo, b.name || "", b.from || ""]);
      r.fulfill({ status: 201, contentType: "application/json", body: JSON.stringify({ card: "nw1", worktree: "/wt/x", created: true }) });
    });
    await p.route(/\/v1\/tasks\/nw1(\?|$)/, r => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ id: "nw1", title: "x", status: "running" }) }));
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    // LAUNCHURL_TAG=before runs against the old scripts (no parser): only the picture is taken, of the same steps.
    if (process.env.LAUNCHURL_TAG === "before") {
      await p.waitForFunction(() => typeof openLaunch === "function" && hubIsHub, null, { timeout: slow(15000) });
      await p.evaluate(async () => { await openLaunch(null, "", "", null, null); });
      await p.fill("#l-cwd", ryan);
      await p.click("#l-nw-btn");
      await p.waitForFunction(() => document.querySelectorAll("#l-nw-list .pvrepo").length, null, { timeout: slow(5000) });
      await p.fill("#l-nw-filter", ryan);
      await p.locator("dialog#launch").screenshot({ path: process.env.LAUNCHURL_SHOTS + "/launch-before.png" });
      return;
    }
    await p.waitForFunction(() => typeof parseRepoText === "function" && typeof openLaunch === "function" && hubIsHub, null, { timeout: slow(15000) });

    // one case per form: [text, strict, slug, branch]
    const forms = [
      ["https://github.com/o/r", true, "github.com/o/r", ""],
      ["https://github.com/o/r.git", true, "github.com/o/r", ""],
      ["https://github.com/o/r/", true, "github.com/o/r", ""],
      [ryan, true, "github.com/dovholuknf/dotfiles", "main"],
      ["https://github.com/o/r/blob/dev/a/b.go", true, "github.com/o/r", "dev"],
      ["https://github.com/o/r/pull/12", true, "github.com/o/r", ""],
      ["https://github.com/o/r/issues", true, "github.com/o/r", ""],
      ["git@github.com:o/r.git", true, "github.com/o/r", ""],
      ["git@github.com:o/r", true, "github.com/o/r", ""],
      ["ssh://git@github.com/o/r.git", true, "github.com/o/r", ""],
      ["ssh://git@bitbucket.org:7999/o/r.git", true, "bitbucket.org/o/r", ""],
      ["https://bitbucket.org/o/r/src/feature/x/", true, "bitbucket.org/o/r", "feature"],
      ["https://bitbucket.org/o/r/pull-requests/3", true, "bitbucket.org/o/r", ""],
      ["https://gitlab.com/g/s/r/-/tree/dev/docs", true, "gitlab.com/g/s/r", "dev"],
      ["https://gitlab.com/g/s/r.git", true, "gitlab.com/g/s/r", ""],
      ["https://gitlab.com/g/r/-/merge_requests/4", true, "gitlab.com/g/r", ""],
      ["github.com/o/r", false, "github.com/o/r", ""],
      ["github/o/r", false, "github.com/o/r", ""],
    ];
    const got = await p.evaluate(fs => fs.map(f => { const t = parseRepoText(f[0], f[1]); return t ? [t.slug, t.branch] : null; }), forms);
    forms.forEach((f, i) => { if (!got[i] || got[i][0] !== f[2] || got[i][1] !== f[3]) fail("launchRepoUrls: " + f[0] + " read as " + JSON.stringify(got[i])); });
    const nots = await p.evaluate(() => ["/work/o/r", "C:\\work\\o\\r", "github.com/o", "o/r/x", "just text", ""].map(x => !!parseRepoText(x, true) || !!parseRepoText(x, false)));
    if (nots.some(Boolean)) fail("launchRepoUrls: a path or plain text was read as a repo " + JSON.stringify(nots));

    const seen = () => p.evaluate(() => ({ cwd: document.getElementById("l-cwd").value, panel: !document.getElementById("l-nw").hidden,
      filter: document.getElementById("l-nw-filter").value, repo: document.getElementById("l-nw-repo").value,
      repoText: document.getElementById("l-nw-repo").selectedOptions[0].textContent, from: document.getElementById("l-nw-from").value,
      go: document.getElementById("l-nw-go").disabled, rows: [...document.querySelectorAll("#l-nw-list .nm")].map(e => e.textContent) }));
    await p.evaluate(async () => { await openLaunch(null, "", "", null, null); });
    // the URL pasted into the working directory box moves to the repo panel, which shows the repo it names
    await p.fill("#l-cwd", ryan);
    await p.waitForFunction(() => document.getElementById("l-nw-repo").selectedOptions[0] && document.getElementById("l-nw-repo").value === "github.com/dovholuknf/dotfiles", null, { timeout: slow(5000) })
      .catch(() => fail("launchRepoUrls: the directory box paste did not select the repo"));
    let s = await seen();
    if (s.cwd !== "" || !s.panel || s.filter !== ryan || !s.repoText.includes("will be fetched") || s.from !== "main" || s.go) fail("launchRepoUrls: after the paste " + JSON.stringify(s));
    if (process.env.LAUNCHURL_SHOTS) await p.locator("dialog#launch").screenshot({ path: process.env.LAUNCHURL_SHOTS + "/launch-" + (process.env.LAUNCHURL_TAG || "after") + ".png" });
    // a repo already held is selected, not shown as a fetch, and a hand-changed box turns start off
    await p.fill("#l-nw-filter", "git@bitbucket.org:netfoundry/netfoundry.git");
    s = await seen();
    if (s.repo !== "bitbucket.org/netfoundry/netfoundry" || s.repoText.includes("fetched") || s.go) fail("launchRepoUrls: a held repo was not selected " + JSON.stringify(s));
    await p.selectOption("#l-nw-repo", "github.com/acme/widget");
    if (!(await seen()).go) fail("launchRepoUrls: start new work stayed on with a box that is not what was typed");
    await p.selectOption("#l-nw-repo", "bitbucket.org/netfoundry/netfoundry");
    if ((await seen()).go) fail("launchRepoUrls: start new work stayed off with a box that matches");
    // start, with no name typed: the hub fetches, the room prepares, then the work starts on the branch the link named
    await p.fill("#l-nw-filter", ryan);
    calls.length = 0;
    await p.click("#l-nw-go");
    await p.waitForFunction(() => !document.getElementById("launch").open, null, { timeout: slow(5000) }).catch(() => fail("launchRepoUrls: start did not finish " + JSON.stringify(calls)));
    const want = ["init", "prepare", "new"];
    if (calls.map(c => c[0]).join() !== want.join() || calls[0][1] !== "https://github.com/dovholuknf/dotfiles" || calls[2][1] !== "github.com/dovholuknf/dotfiles" ||
        !/^claude\/work-\d{6}-\d{4}$/.test(calls[2][2]) || calls[2][3] !== "main") fail("launchRepoUrls: the calls were " + JSON.stringify(calls));
    if (errors.length) fail("launchRepoUrls: page errors " + errors.join(" | "));
  } finally {
    mock.hubMode = wasHub;
    await ctx.close();
  }
  if (!bad) console.log("launchRepoUrls ok");
}

const units = [
  { name: "launchRepoUrls", run: ({ browser, base }) => launchRepoUrlsSection(browser, base) },
  { name: "launchLink", run: ({ browser, base }) => launchLinkSection(browser, base) },
  { name: "launchThrowaway", run: ({ browser, base }) => launchThrowawaySection(browser, base) },
  { name: "launchSetup", run: ({ browser, base }) => launchSetupSection(browser, base) },
  { name: "launchPresets", run: ({ browser, base }) => launchPresetsSection(browser, base) },
  { name: "newWorkDialog", run: ({ browser, base }) => newWorkDialogSection(browser, base) },
];
const sections = { launchRepoUrls: launchRepoUrlsSection, launchLink: launchLinkSection, launchThrowaway: launchThrowawaySection, launchSetup: launchSetupSection, launchPresets: launchPresetsSection, newWorkDialog: newWorkDialogSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
