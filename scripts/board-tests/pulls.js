// pulls: pull requests: the page, the drawer, the panel, moving one.
//
// Units: pullsLiveSteps, pullsPasteOpening, pulls, pullsPanelStage, pullsPage, pullsAbsent, pullsDrawer, prMove.
// `units` is what the harness runs, in suite order. `sections` is for HEADLESS_ONLY, which names a section and not a unit.
// Alone: `node scripts/board-tests/pulls.js` runs just these units, the way the full run would.
const path = require("path");
const crypto = require("crypto");
const { slow, until } = require("../lib/browser.js");
const { openStreams, mock, landCard } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");
const { attachRetry, landContext } = require("../lib/helpers.js");

// ── the pulls view ───────────────────────────────────────
// /v1/prs is mocked in the page (nothing real behind it), the event stream is the harness's real one.
// One card's menu, read or pressed: the `...` opens #cardmenu, whose rows are sentences.
// Scrolled into view and settled first: a scroll after the press closes the menu, as it should.
const pullMenuOpen = async (p, id) => {
  const sel = '#pulls-list .pull[data-id="' + id + '"] button[data-act="menu"]';
  await p.evaluate(s => { const b = document.querySelector(s); if (b) b.scrollIntoView({ block: "center" }); return new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))); }, sel);
  // A scroll the press itself caused can close the menu again, so a second press is allowed.
  for (let i = 0; ; i++) {
    await p.click(sel);
    try { await p.waitForSelector("#cardmenu.on button", { timeout: slow(2500) }); return; } catch (e) { if (i >= 2) throw e; }
  }
};

const pullMenuItems = async (p, id) => { await pullMenuOpen(p, id); const t = await p.evaluate(() => [...document.querySelectorAll("#cardmenu button")].map(b => b.textContent.trim())); await p.evaluate(() => closeCardMenu()); return t; };

const pullMenuPress = async (p, id, re) => { await pullMenuOpen(p, id); await p.evaluate(src => [...document.querySelectorAll("#cardmenu button")].find(b => new RegExp(src).test(b.textContent)).click(), re.source); };

const pullPaste = async (p, url, why) => { if (await p.isHidden("#pulls-pop")) await p.click("#pulls-paste"); await p.fill("#pulls-pop-url", url); await p.fill("#pulls-pop-why", why || ""); await p.click("#pulls-pop-go"); };

async function pullsSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const errors = [];
  const row = (id, extra) => Object.assign({
    id, url: "https://github.com/openziti/" + id, host: "github.com", org: "openziti", repo: "r", org_repo: "openziti/r",
    number: 1, title: "title of " + id, why: "", head: "", head7: "abc1234", state: "queued", run_state: "", run_error: "",
    cost_usd: 0, started_at: "", ready_at: "", created_at: "2026-10-01T10:00:00Z", archived_at: "", run_dir: "",
    second: { state: "none", summary: "", error: "" }, author: "ekoby",
    findings: { high: 0, med: 0, low: 0, nit: 0, leak: 0 }, walk: { accepted: 0, done: 0, skipped: 0, deferred: 0, open: 0 }, walker_task: ""
  }, extra);
  const st = {
    halted: false,
    rows: [
      row("pr_a", { number: 11, org_repo: "openziti/zrok", state: "ready", created_at: "2026-10-01T09:00:00Z", ready_at: "2026-10-01T09:20:00Z", disk_bytes: 734003200,
        findings: { high: 0, med: 6, low: 7, nit: 4, leak: 1 }, walk: { accepted: 0, done: 0, skipped: 0, deferred: 0, open: 17 },
        second: { state: "done", summary: "codex: 2 disputed, settled", error: "" } }),
      row("pr_b", { number: 12, state: "running", run_state: "verify", cost_usd: 0.71, created_at: "2026-10-01T10:00:00Z", title: "running one" }),
      row("pr_c", { number: 13, state: "failed", run_error: "fetch: gh said no", created_at: "2026-10-01T08:00:00Z" }),
      row("pr_d", { number: 14, state: "queued", created_at: "2026-10-01T07:00:00Z" }),
      row("pr_e", { number: 15, state: "aborted", created_at: "2026-10-01T06:00:00Z" }),
      row("pr_f", { number: 16, state: "fetching", created_at: "2026-10-01T05:00:00Z" })
    ],
    posts: [], gets: 0
  };
  const FINDINGS = [
    { key: "f-1", position: 1, file: "01-med-b.go-L7.txt", sev: "med", path: "a/b.go", line: 7, code: "x = 1", link: "", comment: "* c", evidence: {}, leak: "",
      hunk: "", walk: { state: "open", at: "", url: "" }, text: "t", hash: "h1" },
    { key: "f-2", position: 2, file: "02-low-c.go-L0.txt", sev: "low", path: "c.go", line: 0, code: "y", link: "", comment: "* c", evidence: {}, leak: "secret",
      hunk: "", walk: { state: "open", at: "", url: "" }, text: "t", hash: "h2" }
  ];
  const HALTED = { error: "atrium is halted and will not recover without a restart", cause: "disk full", halted: true };
  const navOf = () => st.rows.filter(r => !r.archived_at && (r.state === "ready" || r.state === "failed")).length;
  const json = (route, status, body) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
  const emit = (r) => put(r) || openStreams.forEach(s => { try { s.write("event: pr\ndata: " + JSON.stringify({ pr: r }) + "\n\n"); } catch (e) {} });
  const put = (r) => { const i = st.rows.findIndex(x => x.id === r.id); if (i >= 0) st.rows[i] = r; else st.rows.push(r); };
  // the walker card does not exist in this mock, so the attach after a launch fails and the board stays on the pulls view
  await ctx.route(/\/v1\/tasks\/t-walker$/, route => json(route, 404, { error: "no such task" }));
  // closing the walker card: the preview asks about one dirty worktree, and the close is recorded
  await ctx.route(/\/v1\/tasks\/t-walker\/close$/, async route => {
    const req = route.request();
    if (req.method() === "GET") return json(route, 200, { card: "t-walker", disk_bytes: 1048576, can_stash: true,
      items: [{ seq: 1, kind: "worktree", ref: "/wt/r/feat", action: "ask" }, { seq: 4, kind: "review", ref: "pr_b", action: "free" }],
      asks: [{ seq: 1, path: "/wt/r/feat", branch: "feat", dirty: 2, unpushed: 0 }], warnings: [], kept: ["the card and its transcript"] });
    st.posts.push("POST /v1/tasks/t-walker/close " + (req.postData() || ""));
    return json(route, 200, { card: "t-walker", closed: true, items: [], stashes: [], left: 2, warnings: [] });
  });
  // the sweep: a done card and an owner with no card left things behind, and one worktree nobody owns
  await ctx.route(/\/v1\/sweep$/, route => json(route, 200, { swept_at: "2026-10-01T12:00:00Z", freed: [], warnings: [],
    leftovers: [
      { owner: "t-done", title: "o/r #3", status: "done", rows: [{ seq: 1, kind: "worktree", ref: "/wt/o/r/three" }], disk_bytes: 2097152 },
      { owner: "pending:x", no_card: true, rows: [{ seq: 1, kind: "ref", ref: "refs/atrium/pr/9" }], disk_bytes: 0 }
    ],
    not_owned: [{ path: "/scm/worktrees/github.com/o/r/stray" }] }));
  await ctx.route(/\/v1\/sweep\/close$/, async route => {
    const body = route.request().postData() || "";
    st.posts.push("POST /v1/sweep/close " + body);
    if (!JSON.parse(body).confirm) return json(route, 200, { card: "pending:x", disk_bytes: 0, can_stash: false,
      items: [{ seq: 1, kind: "ref", ref: "refs/atrium/pr/9", action: "free" }], asks: [], warnings: [], kept: ["this list"] });
    return json(route, 200, { card: "pending:x", closed: true, items: [], stashes: [], left: 0, warnings: [] });
  });
  // the paste field opens the link: one call, answered as the room's open verb does
  await ctx.route(/\/v1\/open$/, async route => {
    const req = route.request();
    st.posts.push("POST /v1/open " + (req.postData() || ""));
    const b = JSON.parse(req.postData() || "{}");
    if (st.halted) return json(route, 503, HALTED);
    if (b.url.indexOf("nowhere") >= 0) return json(route, 422, { error: "no recogniser matches this", code: "no_recogniser", step: "recognise" });
    if (b.url.indexOf("dup") >= 0) return json(route, 200, { key: "k", card: "t-walker", pr: st.rows[0].id, created: false });
    if (b.url.indexOf("issues") >= 0) return json(route, 201, { key: "github.com/openziti/r/i7", kind: "issue", card: "t-walker",
      worktree: "/wt/issue-7", repo: "github.com/openziti/r", created: true, title: "issue-7" });
    put(row("pr_new", { number: 99, title: "", state: "queued", created_at: "2026-10-01T12:00:00Z", why: b.why, url: b.url }));
    return json(route, 201, { key: "k", card: "t-walker", pr: "pr_new", created: true });
  });
  await ctx.route(/\/v1\/prs(\/|\?|$)/, async route => {
    const req = route.request();
    const u = new URL(req.url());
    const m = req.method();
    const p = u.pathname;
    if (m === "GET" && p === "/v1/prs") {
      st.gets++;
      if (st.halted) return json(route, 503, HALTED);
      const rows = st.rows.slice().sort((a, b) => (a.created_at < b.created_at ? 1 : -1));
      const counts = { queued: 0, fetching: 0, running: 0, ready: 0, failed: 0, aborted: 0 };
      st.rows.filter(r => !r.archived_at).forEach(r => { counts[r.state]++; });
      return json(route, 200, { prs: rows, counts, nav_count: navOf() });
    }
    st.posts.push(m + " " + p + " " + (req.postData() || ""));
    if (m === "POST" && p === "/v1/prs") {
      const b = JSON.parse(req.postData() || "{}");
      if (st.halted) return json(route, 503, HALTED);
      if (b.url.indexOf("nowhere") >= 0) return json(route, 422, { error: "no recogniser matches this", code: "no_recogniser" });
      if (b.url.indexOf("dup") >= 0) return json(route, 200, { pr: st.rows[0], created: false });
      const r = row("pr_new", { number: 99, title: "", state: "queued", created_at: "2026-10-01T12:00:00Z", why: b.why, url: b.url });
      put(r);
      return json(route, 201, { pr: r, created: true });
    }
    const mm = p.match(/^\/v1\/prs\/([^/]+)(\/.*)?$/);
    const id = mm && mm[1], rest = (mm && mm[2]) || "";
    const cur = st.rows.find(r => r.id === id);
    if (!cur) return json(route, 404, { error: "no such pr", code: "not_found" });
    if (m === "POST" && rest === "/abort") {
      if (["queued", "fetching", "running"].indexOf(cur.state) < 0) return json(route, 409, { error: "only a queued, fetching or running row can be aborted", code: "not_abortable", state: cur.state });
      const r = Object.assign({}, cur, { state: "aborted", run_state: "" }); put(r); return json(route, 200, { pr: r });
    }
    if (m === "POST" && rest === "/retry") {
      if (cur.state !== "failed" && cur.state !== "aborted") return json(route, 409, { error: "only a failed or aborted row can be retried", code: "not_retryable", state: cur.state });
      const r = Object.assign({}, cur, { state: "queued", run_error: "" }); put(r); return json(route, 202, { pr: r }); }
    if (m === "POST" && rest === "/start") {
      if (cur.state !== "queued") return json(route, 409, { error: "only a queued row can be started", code: "not_startable", state: cur.state });
      const r = Object.assign({}, cur, { state: "fetching", run_state: "fetch" }); put(r); return json(route, 202, { pr: r }); }
    if (m === "GET" && rest === "/findings") {
      if (cur.state !== "ready") return json(route, 409, { error: "the review is not ready", code: "not_ready", state: cur.state });
      return json(route, 200, { pr: cur, findings: FINDINGS });
    }
    if (m === "GET" && rest === "") return json(route, 200, { pr: cur, run_log: "14:02:11 fetch start\n14:02:12 fetch FAILED\n" });
    if (m === "POST" && /^\/findings\/[^/]+\/walk$/.test(rest)) {
      const b = JSON.parse(req.postData());
      if (["done", "accepted", "skipped", "deferred", "open"].indexOf(b.state) < 0) return json(route, 400, { error: "state is open, accepted, done, skipped or deferred", code: "bad_request" });
      return json(route, 200, { ok: true, walk: { state: b.state, at: "2026-10-01T14:20:00Z", url: "" }, counts: { accepted: 0, done: 1, skipped: 0, deferred: 0, open: 16 } });
    }
    if (m === "POST" && rest === "/walker") {
      if (cur.state !== "ready") return json(route, 409, { error: "the review is not ready", code: "not_ready", state: cur.state });
      const r = Object.assign({}, cur, { walker_task: "t-walker" }); put(r); return json(route, 201, { pr: r, task: "t-walker", launched: true });
    }
    return json(route, 404, { error: "no route", code: "not_found" });
  });
  const open = async () => {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof pullsPaint === "function" && typeof switchView === "function", null, { timeout: slow(15000) });
    await p.waitForFunction(() => !document.querySelector('.tab[data-view="pulls"]').hidden, null, { timeout: slow(10000) })
      .catch(() => fail("pulls: the tab did not appear once /v1/prs answered"));
    await p.evaluate(() => switchView("pulls"));
    await p.waitForFunction(() => document.querySelectorAll("#pulls-list .pull").length > 0, null, { timeout: slow(10000) });
    return p;
  };
  const ids = p => p.evaluate(() => [...document.querySelectorAll("#pulls-list .pull")].map(e => e.dataset.id).join());
  const text = (p, id, sel) => p.evaluate(([id, sel]) => { const e = document.querySelector('#pulls-list .pull[data-id="' + id + '"] ' + sel); return e ? e.textContent : null; }, [id, sel]);
  const nav = p => p.evaluate(() => document.getElementById("c-pulls").textContent);
  const waitNav = (p, n, what) => p.waitForFunction(n => document.getElementById("c-pulls").textContent === n, n, { timeout: slow(5000) })
    .catch(async () => fail("pulls: nav count is " + JSON.stringify(await nav(p)) + " not " + n + " " + what));
  try {
    const p = await open();
    // newest first
    let v = await ids(p);
    if (v !== "pr_a,pr_b,pr_d,pr_f,pr_c,pr_e") fail("pulls: not grouped by need, newest first inside a group: " + v);
    const groups = await p.evaluate(() => [...document.querySelectorAll("#pulls-list .pull-group")].map(g => g.dataset.group + ":" + g.querySelector(".pull-group-head em").textContent).join());
    if (groups !== "ready:1,progress:3,failed:2") fail("pulls: the groups are " + groups);
    if (await p.evaluate(() => document.querySelector("#pulls-url, #pulls-why"))) fail("pulls: the link and why inputs are still on the tab");
    const attn = () => p.evaluate(() => [...document.querySelectorAll("#pulls-attn button")].map(b => b.dataset.group + "=" + b.querySelector("i").textContent).join());
    if (await attn() !== "needs=0,ready=1,progress=3,failed=2") fail("pulls: the attention strip says " + await attn());
    // each counter is a filter: one click shows only that group, a second clears it
    for (const [g, want] of [["ready", "pr_a"], ["progress", "pr_b,pr_d,pr_f"], ["failed", "pr_c,pr_e"]]) {
      await p.click('#pulls-attn button[data-group="' + g + '"]');
      if (await ids(p) !== want) fail("pulls: the " + g + " counter shows " + await ids(p));
      if (await p.getAttribute('#pulls-attn button[data-group="' + g + '"]', "aria-pressed") !== "true") fail("pulls: the " + g + " counter is not pressed");
      await p.click('#pulls-attn button[data-group="' + g + '"]');
      if (await ids(p) !== "pr_a,pr_b,pr_d,pr_f,pr_c,pr_e") fail("pulls: the " + g + " counter did not clear: " + await ids(p));
    }
    await p.click('#pulls-attn button[data-group="needs"]');
    if (!/Nothing in that group/.test(await p.textContent("#pulls-list"))) fail("pulls: an empty group says nothing about it");
    await p.click('#pulls-attn button[data-group="needs"]');
    if (await p.textContent("#pulls-n-open") !== "6") fail("pulls: the Open count is " + await p.textContent("#pulls-n-open"));
    // the verdict is a pill, the board's guess dashed, and the primary is one button per state
    const pill = await p.evaluate(() => { const e = document.querySelector('#pulls-list .pull[data-id="pr_a"] .pull-state'); return e.textContent + "|" + e.classList.contains("guess"); });
    if (pill !== "changes|true") fail("pulls: a ready row's verdict pill is " + pill);
    const prim = id => p.evaluate(id => [...document.querySelectorAll('#pulls-list .pull[data-id="' + id + '"] .pull-btn.pri')].map(b => b.dataset.act + ":" + b.textContent).join(), id);
    const primWant = { pr_a: "walk:Post review", pr_b: "", pr_c: "retry:Retry", pr_d: "start:Start review", pr_e: "retry:Retry", pr_f: "" };
    for (const k of Object.keys(primWant)) if (await prim(k) !== primWant[k]) fail("pulls: the primary of " + k + " is " + JSON.stringify(await prim(k)));
    // the strip: a failed fetch is the worktree dot
    const dots = id => p.evaluate(id => [...document.querySelectorAll('#pulls-list .pull[data-id="' + id + '"] .pull-strip .d')].map(d => d.className.replace("d", "").trim() || "-").join(), id);
    if (await dots("pr_c") !== "f,-,-,-") fail("pulls: a failed fetch draws " + await dots("pr_c"));
    if (await dots("pr_a") !== "x,x,x,x") fail("pulls: a ready row draws " + await dots("pr_a"));
    if (await dots("pr_d") !== "-,-,-,-") fail("pulls: a queued row draws " + await dots("pr_d"));
    // every state draws its words
    const words = { pr_a: "changes", pr_b: "reviewing", pr_c: "failed at worktree", pr_d: "queued", pr_e: "aborted", pr_f: "fetching" };
    for (const k of Object.keys(words)) {
      const w = await text(p, k, ".pull-state");
      if (w !== words[k]) fail("pulls: " + k + " says " + JSON.stringify(w) + " not " + JSON.stringify(words[k]));
    }
    // running: the step and the cost, never N of M
    const meta = await text(p, "pr_b", ".pull-meta");
    if (!/\$0\.71 so far/.test(meta || "")) fail("pulls: a running row has no cost so far: " + meta);
    if (/\d+ of \d+/.test((await text(p, "pr_b", ".pull-stage")) || "")) fail("pulls: a running row says N of M");
    if ((await text(p, "pr_b", ".pull-stage")) !== "verify") fail("pulls: a running row does not say its step: " + await text(p, "pr_b", ".pull-stage"));
    if ((await text(p, "pr_c", ".pull-stage")) !== "gh said no") fail("pulls: a failed row's note is " + await text(p, "pr_c", ".pull-stage"));
    // a ready row: counts and the second opinion
    const chipsOf = await p.evaluate(() => [...document.querySelectorAll('#pulls-list .pull[data-id="pr_a"] .pull-counts span')].map(e => e.textContent).join(" | "));
    if (chipsOf !== "6 med | 7 low | 4 nit | 1 leak") fail("pulls: counts: " + chipsOf);
    if ((await text(p, "pr_a", ".pull-second")) !== "2nd: codex: 2 disputed, settled") fail("pulls: second: " + await text(p, "pr_a", ".pull-second"));
    // buttons per state
    const btns = id => p.evaluate(id => [...document.querySelectorAll('#pulls-list .pull[data-id="' + id + '"] .pull-acts button')].map(b => b.dataset.act).join(), id);
    const want = { pr_a: "walk,menu", pr_b: "menu", pr_c: "retry,menu", pr_d: "start,menu", pr_e: "retry,menu", pr_f: "menu" };
    for (const k of Object.keys(want)) { const b = await btns(k); if (b !== want[k]) fail("pulls: buttons of " + k + ": " + b); }
    // the rest is in the menu, and its labels are sentences
    const menus = { pr_b: "Abort the run|Open on GitHub|Copy PR link", pr_c: "Show the run log|Open on GitHub|Copy PR link", pr_d: "Abort the run|Open on GitHub|Copy PR link" };
    for (const k of Object.keys(menus)) { const m = (await pullMenuItems(p, k)).join("|"); if (m !== menus[k]) fail("pulls: the menu of " + k + " is " + m); }
    // nav count from the load: one ready, one failed
    if (await nav(p) !== "2") fail("pulls: nav count at load: " + await nav(p));
    // an SSE event updates a row without a refetch
    const gets = st.gets;
    await p.evaluate(() => {
      window.__pullAlerts = [];
      const o = alerting.check;
      alerting.check = (k, items, d) => { if (k === "pulls") window.__pullAlerts.push(items.map(i => i.id)); return o.call(alerting, k, items, d); };
    });
    const b2 = st.rows.find(r => r.id === "pr_b");
    emit(Object.assign({}, b2, { run_state: "critics", cost_usd: 1.2 }));
    await p.waitForFunction(() => /critics/.test(document.querySelector('#pulls-list .pull[data-id="pr_b"] .pull-stage').textContent), null, { timeout: slow(5000) })
      .catch(() => fail("pulls: an event did not move the step"));
    if (!/\$1\.20/.test((await text(p, "pr_b", ".pull-meta")) || "")) fail("pulls: an event did not move the cost");
    if (!/700 MB/.test((await text(p, "pr_a", ".pull-meta")) || "")) fail("pulls: the walker card's disk is not on the row: " + await text(p, "pr_a", ".pull-meta"));
    if (st.gets !== gets) fail("pulls: an event caused a refetch");
    // the running row turns ready, then a new failed row arrives: the count follows each
    emit(Object.assign({}, b2, { state: "ready", run_state: "", ready_at: "2026-10-01T10:09:00Z", findings: { high: 0, med: 1, low: 0, nit: 0, leak: 0 }, walk: { accepted: 0, done: 0, skipped: 0, deferred: 0, open: 1 } }));
    await waitNav(p, "3", "after a row turned ready");
    emit(row("pr_g", { number: 21, state: "failed", run_error: "panel: budget", created_at: "2026-10-01T11:00:00Z" }));
    await waitNav(p, "4", "after a new row arrived failed");
    v = await ids(p);
    if (v.split(",").indexOf("pr_g") !== v.split(",").indexOf("pr_c") - 1) fail("pulls: a new failed row is not first in its group: " + v);
    const alerts = await p.evaluate(() => window.__pullAlerts);
    if (!alerts.length || !alerts[alerts.length - 1].some(i => i.indexOf("pr_g#failed") === 0)) fail("pulls: the failed row did not reach the alerting path: " + JSON.stringify(alerts));
    if (!(await p.title()).startsWith("(")) fail("pulls: the window title does not carry the count: " + await p.title());
    // retry lowers the count and updates the row
    await p.click('#pulls-list .pull[data-id="pr_g"] button[data-act="retry"]');
    await waitNav(p, "3", "after retry");
    if ((await text(p, "pr_g", ".pull-state")) !== "queued") fail("pulls: retry did not update the row");
    if (!st.posts.some(x => x.startsWith("POST /v1/prs/pr_g/retry"))) fail("pulls: retry did not post");
    // abort posts and updates
    await pullMenuPress(p, "pr_f", /Abort the run/);
    await p.waitForFunction(() => document.querySelector('#pulls-list .pull[data-id="pr_f"] .pull-state').textContent === "aborted", null, { timeout: slow(5000) })
      .catch(() => fail("pulls: abort did not update the row"));
    // a refused abort says why
    await p.evaluate(() => { pulls.rows.find(r => r.id === "pr_a").state = "running"; pullsPaint(); });
    st.rows.find(r => r.id === "pr_a").state = "ready";
    await pullMenuPress(p, "pr_a", /Abort the run/);
    await p.waitForFunction(() => /only a queued/.test(document.getElementById("pulls-note").textContent), null, { timeout: slow(5000) })
      .catch(() => fail("pulls: a 409 on abort was not shown"));
    // review on a queued row
    await p.click('#pulls-list .pull[data-id="pr_d"] button[data-act="start"]');
    await p.waitForFunction(() => document.querySelector('#pulls-list .pull[data-id="pr_d"] .pull-state').textContent === "fetching", null, { timeout: slow(5000) })
      .catch(() => fail("pulls: review did not start the row"));
    // paste a PR: created, existing, and the 422 word
    await pullPaste(p, "https://github.com/openziti/r/pull/99", "security look");
    await p.waitForFunction(() => !!document.querySelector('#pulls-list .pull[data-id="pr_new"]'), null, { timeout: slow(5000) })
      .catch(() => fail("pulls: a pasted PR did not appear"));
    if (!st.posts.some(x => x.startsWith("POST /v1/open ") && x.indexOf("security look") >= 0)) fail("pulls: the paste did not open the url with its why");
    if (await p.inputValue("#pulls-pop-url") !== "" || !(await p.isHidden("#pulls-pop"))) fail("pulls: the popover was not cleared and closed");
    await pullPaste(p, "https://github.com/dup/r/pull/1");
    await p.waitForFunction(() => /already/.test(document.getElementById("pulls-note").textContent), null, { timeout: slow(5000) })
      .catch(() => fail("pulls: a 200 existing answer was not said"));
    await pullPaste(p, "https://nowhere.example/x");
    await p.waitForFunction(() => /no recogniser matches this/.test((document.querySelector(".card.linkopen.failed") || {}).textContent || ""), null, { timeout: slow(5000) })
      .catch(() => fail("pulls: the 422 word was not shown"));
    await p.evaluate(() => [...linkOpenings.keys()].forEach(linkOpeningEnd));
    // an issue opens a card and no row: the note says so and the card is attached
    await p.evaluate(() => { window.__attached = []; const was = attachTask; attachTask = id => { window.__attached.push(id); return was(id); }; });
    await pullPaste(p, "https://github.com/openziti/r/issues/7");
    await p.waitForFunction(() => /issue-7 as a card.*not a pull request/.test(document.getElementById("pulls-note").textContent),
      null, { timeout: slow(5000) }).catch(() => fail("pulls: a pasted issue was not said as a card with no row"));
    if ((await p.evaluate(() => window.__attached)).join() !== "t-walker") fail("pulls: a pasted issue's card was not attached");
    // halted
    st.halted = true;
    await pullPaste(p, "https://github.com/openziti/r/pull/5");
    await p.waitForFunction(() => /halted/.test((document.querySelector(".card.linkopen.failed") || {}).textContent || ""), null, { timeout: slow(5000) })
      .catch(() => fail("pulls: a halted 503 did not show the halted word"));
    await p.evaluate(() => [...linkOpenings.keys()].forEach(linkOpeningEnd));
    st.halted = false;
    // the walk button on a ready row (pr_b is ready now) starts a walker. The drawer itself is pullsDrawer's
    await p.click('#pulls-list .pull[data-id="pr_b"] button[data-act="walk"]');
    await p.waitForFunction(() => pulls.rows.find(r => r.id === "pr_b").walker_task === "t-walker", null, { timeout: slow(5000) })
      .catch(() => fail("pulls: the walker launch did not land"));
    if (!st.posts.some(x => x === 'POST /v1/prs/pr_b/walker {"action":"launch"}')) fail("pulls: the walk button did not post a launch");
    if (await p.evaluate(() => document.querySelector("#pulls-list .pull-finding"))) fail("pulls: findings are still drawn inline");
    // close on a row with a walker: the dirty worktree is asked about with no default, then the close is confirmed
    if (!(await pullMenuItems(p, "pr_b")).some(t => /^Close card and free its worktree, branch and review$/.test(t))) fail("pulls: a row with a walker has no close in its menu");
    await pullMenuPress(p, "pr_b", /^Close card/);
    await p.waitForFunction(() => document.getElementById("ask").open && /^close /.test(document.getElementById("ask-title").textContent), null, { timeout: slow(5000) })
      .catch(() => fail("pulls: close did not open its dialog"));
    // ONE dialog: the dirty worktree is a row in it with its choice, stash preselected
    const asked = await p.evaluate(() => [...document.querySelectorAll("#ask-actions button")].map(b => b.textContent).join());
    if (asked !== "cancel,close it") fail("pulls: the close dialog offers " + asked);
    if (await p.locator("#ask-body .close-wt").count() !== 1) fail("pulls: the dirty worktree is not a row in the close dialog");
    if (await p.locator('#ask-body .close-wt input:checked').getAttribute("value") !== "stash") fail("pulls: stash is not preselected");
    await p.click('#ask-body .close-wt input[value="keep"]');
    await p.click('#ask-actions button:has-text("close it")');
    for (let i = 0; i < 50 && !st.posts.some(x => x.startsWith("POST /v1/tasks/t-walker/close ")); i++) await p.waitForTimeout(100);
    const closePost = st.posts.filter(x => x.startsWith("POST /v1/tasks/t-walker/close ")).map(x => JSON.parse(x.slice("POST /v1/tasks/t-walker/close ".length)))[0];
    if (!closePost || closePost.confirm !== true || JSON.stringify(closePost.answers) !== '{"1":"keep"}' || !closePost.progress) fail("pulls: the close did not post the answer: " + st.posts.filter(x => x.indexOf("close") >= 0).join(" | "));
    await p.waitForFunction(() => !document.getElementById("ask").open, null, { timeout: slow(5000) })
      .catch(() => fail("pulls: the close dialog did not close itself when done"));
    // leftovers: grouped by owner with one close each, and a worktree nobody owns listed with none
    await p.click("#pulls-more");
    await p.evaluate(() => [...document.querySelectorAll("#cardmenu button")].find(b => /Find unclaimed worktrees/.test(b.textContent)).click());
    await p.waitForFunction(() => document.querySelectorAll('#pulls-left button[data-act="left-close"]').length === 2, null, { timeout: slow(5000) })
      .catch(() => fail("pulls: the leftovers did not draw two groups with a close each"));
    const leftText = await p.evaluate(() => document.getElementById("pulls-left").textContent);
    if (!/o\/r #3/.test(leftText) || !/no card/.test(leftText) || !/not owned/.test(leftText) || !/stray/.test(leftText)) fail("pulls: the leftovers say " + leftText);
    await p.click('#pulls-left button[data-act="left-close"][data-id="1"]');
    await p.waitForFunction(() => document.getElementById("ask").open && /^close /.test(document.getElementById("ask-title").textContent), null, { timeout: slow(5000) })
      .catch(() => fail("pulls: closing an owner with no card did not confirm"));
    await p.click('#ask-actions button:has-text("close it")');
    for (let i = 0; i < 50 && !st.posts.some(x => x.indexOf('"confirm":true') >= 0 && x.startsWith("POST /v1/sweep/close ")); i++) await p.waitForTimeout(100);
    if (!st.posts.some(x => /^POST \/v1\/sweep\/close \{"owner":"pending:x","confirm":true,"answers":\{\},"progress":"close-[a-z0-9]+"\}$/.test(x))) fail("pulls: the owner's close did not post: " + st.posts.filter(x => x.indexOf("sweep") >= 0).join(" | "));
    // the log of a failed row
    await pullMenuPress(p, "pr_c", /Show the run log/);
    await p.waitForFunction(() => /fetch FAILED/.test((document.querySelector('#pulls-list .pull[data-id="pr_c"] .pull-log') || {}).textContent || ""), null, { timeout: slow(5000) })
      .catch(() => fail("pulls: the log did not open"));
    // a client that reconnects refetches
    st.rows.push(row("pr_h", { number: 31, created_at: "2026-10-01T13:00:00Z" }));
    openStreams.forEach(s => { try { s.destroy(); } catch (e) {} });
    await p.waitForFunction(() => !!document.querySelector('#pulls-list .pull[data-id="pr_h"]'), null, { timeout: slow(20000) })
      .catch(() => fail("pulls: a reconnect did not refetch the index"));
    // the repo filter
    await p.selectOption("#pulls-repo", "openziti/zrok");
    if (await ids(p) !== "pr_a") fail("pulls: the repo filter shows " + await ids(p));
    if (errors.length) fail("pulls: page errors: " + errors.join(" | "));
    await p.close();
    // an empty index draws an empty state
    st.rows = [];
    const q = await ctx.newPage();
    q.on("pageerror", e => errors.push(e.message));
    await q.goto(base + "/", { waitUntil: "domcontentloaded" });
    await q.waitForFunction(() => typeof pullsPaint === "function", null, { timeout: slow(15000) });
    await q.evaluate(() => switchView("pulls"));
    await q.waitForFunction(() => /Nothing under review/.test(document.getElementById("pulls-list").textContent), null, { timeout: slow(10000) })
      .catch(() => fail("pulls: an empty index drew no empty state"));
    if (await nav(q) !== "") fail("pulls: nav count shows on an empty index: " + await nav(q));
    if (errors.length) fail("pulls: page errors: " + errors.join(" | "));
  } finally { await ctx.close(); }
  if (!bad) console.log("pulls ok");
}

// ── the panel is a stage, not the row's state ────────────
// A ready row whose panel found no reviewer says so in one muted line, and its state words are the review's own.
// PSSHOT=/dir writes the picture.
async function pullsPanelStageSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 500 } });
  const errors = [];
  const json = (route, status, body) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
  const row = (id, extra) => Object.assign({
    id, url: "https://github.com/openziti/zrok/pull/967", host: "github.com", org: "openziti", repo: "zrok", org_repo: "openziti/zrok",
    number: 967, title: "title of " + id, why: "", head: "", head7: "abc1234", state: "ready", run_state: "", run_error: "",
    cost_usd: 0, started_at: "", ready_at: "2026-10-08T09:20:00Z", created_at: "2026-10-08T09:00:00Z", archived_at: "", run_dir: "",
    second: { state: "none", summary: "", error: "" }, author: "ekoby",
    findings: { high: 0, med: 1, low: 0, nit: 0, leak: 0 }, walk: { accepted: 0, done: 0, skipped: 0, deferred: 0, open: 1 }, walker_task: ""
  }, extra);
  const rows = [
    row("pr_skip", { number: 967, stage_note: "panel skipped: no reviewer applies", created_at: "2026-10-08T09:03:00Z" }),
    row("pr_miss", { number: 968, stage_note: "panel: missing agent files review-panel, go-reviewer", created_at: "2026-10-08T09:02:00Z" }),
    row("pr_plain", { number: 969, created_at: "2026-10-08T09:01:00Z" }),
    row("pr_dead", { number: 970, state: "failed", run_error: "fetch: gh said no", created_at: "2026-10-08T09:00:00Z" })
  ];
  // PSBEFORE=1 draws the rows as the old daemon sent them, the panel's failure as the row's own, for the "before" picture.
  if (process.env.PSBEFORE) {
    for (const r of rows.slice(0, 3)) {
      delete r.stage_note;
      Object.assign(r, { state: "failed", run_error: "panel: no reviewer in the panel applies, or none of their agent files was found" });
    }
  }
  await ctx.route(/\/v1\/prs(\/|\?|$)/, route => json(route, 200, { prs: rows, counts: {}, nav_count: 4 }));
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof pullsPaint === "function" && typeof switchView === "function", null, { timeout: slow(15000) });
    await p.waitForFunction(() => !document.querySelector('.tab[data-view="pulls"]').hidden, null, { timeout: slow(10000) });
    await p.evaluate(() => switchView("pulls"));
    await p.waitForFunction(() => document.querySelectorAll("#pulls-list .pull").length === 4, null, { timeout: slow(10000) });
    if (process.env.PSBEFORE) {
      if (process.env.PSSHOT) await p.screenshot({ path: process.env.PSSHOT });
      await ctx.close();
      return;
    }
    const q = (id, sel) => p.evaluate(([id, sel]) => {
      const e = document.querySelector('#pulls-list .pull[data-id="' + id + '"] ' + sel); return e ? e.textContent : null;
    }, [id, sel]);
    for (const [id, state, note] of [
      ["pr_skip", "changes", "panel skipped: no reviewer applies"],
      ["pr_miss", "changes", "panel: missing agent files review-panel, go-reviewer"],
      ["pr_plain", "changes", null],
      ["pr_dead", "failed at worktree", "gh said no"]
    ]) {
      if (await q(id, ".pull-state") !== state) fail("pullsPanelStage: " + id + " says " + JSON.stringify(await q(id, ".pull-state")));
      if (await q(id, ".pull-stage") !== note) fail("pullsPanelStage: " + id + " stage line is " + JSON.stringify(await q(id, ".pull-stage")));
    }
    // muted, not the red of a failure: the same colour as the other quiet lines
    const colours = await p.evaluate(() => {
      const c = s => getComputedStyle(document.querySelector(s)).color;
      return { stage: c('.pull[data-id="pr_skip"] .pull-stage'), why: c('.pull[data-id="pr_skip"] .pull-title') };
    });
    if (/^rgb\(2[0-9]{2}, [0-9]{1,2}, [0-9]{1,2}\)$/.test(colours.stage)) fail("pullsPanelStage: the stage line is red: " + colours.stage);
    if (process.env.PSSHOT) await p.screenshot({ path: process.env.PSSHOT });
    if (errors.length) fail("pullsPanelStage: page errors: " + errors.join(" | "));
  } finally { await ctx.close(); }
  if (!bad) console.log("pullsPanelStage ok");
}

// ── the pulls page: verdict, groups, one primary, phone ───────────────
// The redesign of docs/rnd/pulls-redesign.md over rows shaped like the live /v1/prs. The walker's card is stood in for by
// replacing pullCard in the page, so a task poll cannot move it. PGSHOT=/dir writes the pictures
// (loaded, empty, loading and menu, at 1280 and 390).
async function pullsPageSection(browser, base) {
  const errors = [];
  const json = (route, status, body) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
  const ago = m => new Date(Date.now() - m * 60000).toISOString();
  const row = (id, extra) => Object.assign({
    id, url: "https://github.com/openziti/x/pull/1", host: "github.com", org: "openziti", repo: "x", org_repo: "openziti/x",
    number: 1, title: "", why: "", head: "", head7: "abc1234", state: "queued", run_state: "", run_error: "", cost_usd: 0,
    started_at: "", ready_at: "", created_at: ago(5), archived_at: "", run_dir: "",
    second: { state: "none", summary: "", error: "" }, author: "ekoby",
    findings: { high: 0, med: 0, low: 0, nit: 0, leak: 0 }, walk: { accepted: 0, done: 0, skipped: 0, deferred: 0, open: 0 }, walker_task: ""
  }, extra);
  const ready = (id, extra) => row(id, Object.assign({ state: "ready", ready_at: ago(30), walker_task: "" }, extra));
  const live = [
    row("pr_wait", { org_repo: "openziti/zrok", number: 11, title: "Add reconnect backoff to the agent", state: "running", run_state: "review", cost_usd: 1.04, created_at: ago(31), walker_task: "t-wait" }),
    ready("pr_block", { org_repo: "openziti/tlsuv", number: 378, title: "Rework the tls handshake", verdict: "blocked", cost_usd: 2.4, created_at: ago(60), ready_at: ago(60),
      findings: { high: 2, med: 6, low: 7, nit: 0, leak: 0 }, tests: { ran: 38, passed: 36, failed: 2, skipped: 0 } }),
    ready("pr_chg", { org_repo: "openziti/zrok", number: 14, title: "Expose a --headless flag on enable", author: "mike-ziti", verdict: "changes", cost_usd: 1.58, created_at: ago(120), ready_at: ago(120),
      findings: { high: 0, med: 3, low: 5, nit: 0, leak: 0 }, tests: { ran: 12, passed: 12, failed: 0, skipped: 0 } }),
    ready("pr_ok", { org_repo: "openziti/sdk-golang", number: 991, title: "Fix a data race in the xgress reader", verdict: "approve", cost_usd: 1.92, created_at: ago(40), ready_at: ago(40),
      findings: { high: 0, med: 0, low: 3, nit: 0, leak: 0 }, tests: { ran: 41, passed: 41, failed: 0, skipped: 0 } }),
    row("pr_run", { org_repo: "openziti/ziti", number: 7402, title: "Router: drop dead links before rerouting", author: "plorenz", state: "running", run_state: "panel", cost_usd: 0.71, created_at: ago(12) }),
    row("pr_q", { org_repo: "openziti/edge-api", number: 55, title: "Regenerate clients", author: "mike-ziti", created_at: ago(2) }),
    row("pr_fail", { org_repo: "openziti/zrok", number: 13, title: "Bump deps", author: "dependabot", state: "failed", run_error: "deps: npm install exited 1", created_at: ago(9) })
  ];
  const closedRows = [
    row("pr_c1", { org_repo: "openziti/zrok", number: 9, title: "Typo in the README", state: "ready", archived_at: ago(60 * 26), created_at: ago(60 * 30) }),
    row("pr_c2", { org_repo: "openziti/ziti", number: 7390, title: "Bump go to 1.26", state: "ready", archived_at: ago(60 * 50), created_at: ago(60 * 52) })
  ];
  const shot = process.env.PGSHOT || "";
  // The pictures are the board's own palette (harbour, the one in :root), whatever skin the mock daemon names.
  const skinned = (p, name) => p.evaluate(n => { if (n) document.documentElement.setAttribute("data-skin", n); else document.documentElement.removeAttribute("data-skin"); }, name || "").then(() => p.waitForTimeout(500));
  const st = { rows: live, gate: null, posts: [] };
  const open = async (ctx, view) => {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof pullsPaint === "function" && typeof switchView === "function", null, { timeout: slow(15000) });
    return p;
  };
  const wire = async ctx => {
    await ctx.route(/\/v1\/prs(\/|\?|$)/, async route => {
      const req = route.request();
      const u = new URL(req.url());
      if (req.method() === "GET" && u.pathname === "/v1/prs") {
        if (st.gate) await st.gate;
        const all = u.searchParams.get("archived") === "1";
        const rows = (all ? st.rows.concat(closedRows) : st.rows).slice();
        return json(route, 200, { prs: rows, counts: {}, nav_count: 0 });
      }
      st.posts.push(req.method() + " " + u.pathname);
      return json(route, 404, { error: "no route" });
    });
  };
  const standIn = p => p.evaluate(() => {
    pullCard = r => (r.walker_task === "t-wait"
      ? { id: "t-wait", status: "needs-input", seen: { answered: false, open_questions: ["keep the retry cap at 5?"] } } : null);
    pulls.cardSig = undefined;
    pullsTasksMoved();
  });
  const ids = p => p.evaluate(() => [...document.querySelectorAll("#pulls-list .pull")].map(e => e.dataset.id).join());
  const show = async p => {
    await p.waitForFunction(() => !document.querySelector('.tab[data-view="pulls"]').hidden, null, { timeout: slow(10000) });
    await p.evaluate(() => switchView("pulls"));
    await p.waitForFunction(() => document.querySelectorAll("#pulls-list .pull").length > 0, null, { timeout: slow(10000) });
  };
  try {
    // ── desktop ──
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 1000 } });
    await wire(ctx);
    const p = await open(ctx);
    await show(p);
    await standIn(p);
    const order = await ids(p);
    if (order !== "pr_wait,pr_ok,pr_block,pr_chg,pr_q,pr_run,pr_fail") fail("pullsPage: the order is " + order);
    const groups = await p.evaluate(() => [...document.querySelectorAll("#pulls-list .pull-group")].map(g => g.dataset.group + ":" + g.querySelector(".pull-group-head em").textContent).join());
    if (groups !== "needs:1,ready:3,progress:2,failed:1") fail("pullsPage: the groups are " + groups);
    const attn = await p.evaluate(() => [...document.querySelectorAll("#pulls-attn button")].map(b => b.dataset.group + "=" + b.querySelector("i").textContent).join());
    if (attn !== "needs=1,ready=3,progress=2,failed=1") fail("pullsPage: the strip says " + attn);
    // the card: pill, chips, stage note, strip, one primary
    const card = id => p.evaluate(id => {
      const c = document.querySelector('#pulls-list .pull[data-id="' + id + '"]');
      const q = s => { const e = c.querySelector(s); return e ? e.textContent : null; };
      return { pill: q(".pull-state"), guess: !!c.querySelector(".pull-state.guess"), chips: [...c.querySelectorAll(".pull-counts span")].map(e => e.textContent + (e.classList.contains("bad") ? "!" : "")).join(" | "),
        note: q(".pull-stage"), noteCls: (c.querySelector(".pull-stage") || {}).className || "", dots: [...c.querySelectorAll(".pull-strip .d")].map(d => d.className.replace("d", "").trim() || "-").join(),
        prim: [...c.querySelectorAll(".pull-btn.pri")].map(b => b.dataset.act + ":" + b.textContent + ":" + b.className.replace(/pull-btn|pri/g, "").trim()).join(), title: q(".pull-title"), meta: q(".pull-meta") };
    }, id);
    const w = await card("pr_wait");
    if (w.pill !== "waiting on you" || w.note !== 'the card asks: "keep the retry cap at 5?"' || !/ q$/.test(w.noteCls) || w.dots !== "x,x,x,r" || w.prim !== "open:Answer:ask") fail("pullsPage: the waiting card is " + JSON.stringify(w));
    const b = await card("pr_block");
    if (b.pill !== "blocked" || b.guess || b.chips !== "2 blockers | 6 med | 7 low | tests 38 ran, 2 failed!" || b.prim !== "walk:Post review:" || b.dots !== "x,x,x,x") fail("pullsPage: the blocked card is " + JSON.stringify(b));
    const a = await card("pr_ok");
    if (a.pill !== "approve" || a.chips !== "3 low | tests 41 ran, 41 passed") fail("pullsPage: the approve card is " + JSON.stringify(a));
    const r = await card("pr_run");
    if (r.pill !== "reviewing" || r.dots !== "x,x,r,-" || r.prim !== "" || !/\$0\.71 so far/.test(r.meta)) fail("pullsPage: the running card is " + JSON.stringify(r));
    const q = await card("pr_q");
    if (q.pill !== "queued" || q.prim !== "start:Start review:mute" || q.dots !== "-,-,-,-") fail("pullsPage: the queued card is " + JSON.stringify(q));
    const f = await card("pr_fail");
    if (f.pill !== "failed at deps" || f.note !== "npm install exited 1" || !/ e$/.test(f.noteCls) || f.dots !== "x,f,-,-" || f.prim !== "retry:Retry:mute") fail("pullsPage: the failed card is " + JSON.stringify(f));
    // the colour bar says the verdict: red, teal and orange are three different colours, and none is the idle one
    const bar = id => p.evaluate(id => getComputedStyle(document.querySelector('#pulls-list .pull[data-id="' + id + '"] .pull-bar')).backgroundColor, id);
    const bars = [await bar("pr_block"), await bar("pr_ok"), await bar("pr_wait"), await bar("pr_chg"), await bar("pr_q")];
    if (new Set(bars).size !== 5) fail("pullsPage: the bars are not five colours: " + bars.join(" "));
    // Answer attaches the card
    await p.evaluate(() => { window.__att = []; attachTask = id => { window.__att.push(id); return Promise.resolve(); }; });
    await p.click('#pulls-list .pull[data-id="pr_wait"] button[data-act="open"]');
    if ((await p.evaluate(() => window.__att)).join() !== "t-wait") fail("pullsPage: Answer did not attach the card");
    // a walked review: continue while some are posted, open the card once all are
    const prim = async mut => p.evaluate(m => { const r = pulls.rows.find(x => x.id === "pr_chg"); Object.assign(r, JSON.parse(m)); pullsPaint(); return [...document.querySelectorAll('#pulls-list .pull[data-id="pr_chg"] .pull-btn.pri')].map(b => b.textContent).join(); }, JSON.stringify(mut));
    if (await prim({ walk: { accepted: 0, done: 2, skipped: 0, deferred: 0, open: 6 } }) !== "Continue review") fail("pullsPage: a half posted review does not offer Continue review");
    if (await prim({ walk: { accepted: 0, done: 6, skipped: 2, deferred: 0, open: 0 }, walker_task: "t-done" }) !== "Open card") fail("pullsPage: a fully posted review does not offer Open card");
    await p.evaluate(() => loadPulls());
    await p.waitForFunction(() => pulls.rows.find(x => x.id === "pr_chg").walk.done === 0, null, { timeout: slow(5000) });
    // the header: the segmented control reads All with the closed ones, in their own group, dimmed and with nothing to press
    await p.click('#pulls-seg button[data-state="all"]');
    await p.waitForFunction(() => document.querySelector('#pulls-list .pull-group[data-group="closed"]'), null, { timeout: slow(5000) })
      .catch(() => fail("pullsPage: All shows no closed group"));
    if (await p.locator('#pulls-list .pull[data-state="closed"] button').count() !== 0) fail("pullsPage: a closed card offers buttons");
    if (await p.textContent("#pulls-n-all") !== "9") fail("pullsPage: the All count is " + await p.textContent("#pulls-n-all"));
    await p.click('#pulls-seg button[data-state="open"]');
    await p.waitForFunction(() => !document.querySelector('#pulls-list .pull-group[data-group="closed"]'), null, { timeout: slow(5000) })
      .catch(() => fail("pullsPage: Open still shows the closed group"));
    // the paste button is one popover, and the header menu holds the leftovers
    await p.click("#pulls-paste");
    if (await p.isHidden("#pulls-pop")) fail("pullsPage: Paste a PR did not open its popover");
    await p.keyboard.press("Escape");
    if (!(await p.isHidden("#pulls-pop"))) fail("pullsPage: Escape did not close the popover");
    if (shot) {
      await p.evaluate(() => { window.scrollTo(0, 0); });
      await skinned(p);
      await p.screenshot({ path: shot + "/loaded-desktop.png" });
      await skinned(p, "paper");
      await p.screenshot({ path: shot + "/loaded-desktop-paper.png" });
      await skinned(p);
      await pullMenuOpen(p, "pr_block");
      await p.screenshot({ path: shot + "/menu-desktop.png" });
      await p.evaluate(() => closeCardMenu());
    }
    if (errors.length) fail("pullsPage: page errors: " + errors.join(" | "));
    await p.close();

    // ── loading, then empty with the last closed ones under it, at both widths ──
    for (const [vw, vh, tag] of [[1280, 1000, "desktop"], [390, 844, "phone"]]) {
      const sc = await browser.newContext({ viewport: { width: vw, height: vh }, hasTouch: vw < 640 });
      await wire(sc);
      let release;
      st.gate = new Promise(res => { release = res; });
      const l = await open(sc);
      await l.evaluate(() => switchView("pulls"));
      await l.waitForFunction(() => document.querySelectorAll("#pulls-list .pull-skel").length === 3, null, { timeout: slow(10000) })
        .catch(() => fail("pullsPage: loading drew no three skeletons at " + tag));
      if (await l.textContent("#pulls-n-open") !== "…") fail("pullsPage: the header count while loading is " + await l.textContent("#pulls-n-open"));
      if (shot) await skinned(l), await l.screenshot({ path: shot + "/loading-" + tag + ".png" });
      st.gate = null; release();
      await l.close();
      st.rows = [];
      const e = await open(sc);
      await e.waitForFunction(() => !document.querySelector('.tab[data-view="pulls"]').hidden, null, { timeout: slow(10000) });
      await e.evaluate(() => switchView("pulls"));
      await e.waitForFunction(() => /Nothing under review/.test(document.getElementById("pulls-list").textContent) && document.querySelectorAll("#pulls-list .pull").length === 2, null, { timeout: slow(10000) })
        .catch(() => fail("pullsPage: the empty page is not the panel with two closed cards under it at " + tag));
      await e.click('#pulls-list button[data-act="paste"]');
      if (await e.isHidden("#pulls-pop")) fail("pullsPage: the empty page's paste button did not open the popover");
      await e.keyboard.press("Escape");
      if (shot) await skinned(e), await e.screenshot({ path: shot + "/empty-" + tag + ".png" });
      await e.close();
      st.rows = live;
      await sc.close();
    }
    await ctx.close();

    // ── phone ──
    const pctx = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true });
    await wire(pctx);
    const ph = await open(pctx);
    await show(ph);
    await standIn(ph);
    const m = await ph.evaluate(() => {
      const list = document.getElementById("pulls-list");
      const btn = document.querySelector('#pulls-list .pull[data-id="pr_block"] .pull-btn.pri').getBoundingClientRect();
      const more = document.querySelector('#pulls-list .pull[data-id="pr_block"] .pull-btn.more').getBoundingClientRect();
      const cols = getComputedStyle(document.getElementById("pulls-attn")).gridTemplateColumns.split(" ").length;
      const ask = document.querySelector("#pulls-list .pull-btn.pri.ask");
      const btnFs = getComputedStyle(document.querySelector('#pulls-list .pull[data-id="pr_block"] .pull-btn.pri')).fontSize;
      return { over: list.scrollWidth - list.clientWidth, page: document.documentElement.scrollWidth - window.innerWidth, btnH: btn.height, moreH: more.height, cols, askH: ask ? ask.getBoundingClientRect().height : null, askFs: ask ? getComputedStyle(ask).fontSize : null, btnFs };
    });
    if (m.askH === null || m.askH !== m.btnH || m.askFs !== m.btnFs) fail("pullsPage: the phone Answer button is not the size of Post review: " + JSON.stringify(m));
    if (m.over > 0 || m.page > 0) fail("pullsPage: the phone page scrolls sideways: " + JSON.stringify(m));
    if (m.btnH < 36 || m.moreH < 36) fail("pullsPage: the phone buttons are not a touch size: " + JSON.stringify(m));
    if (m.cols !== 2) fail("pullsPage: the phone strip is not two columns: " + JSON.stringify(m));
    if (shot) {
      await skinned(ph);
      await ph.screenshot({ path: shot + "/loaded-phone.png" });
    }
    // the menu is a sheet on the foot of the screen
    await pullMenuOpen(ph, "pr_block");
    const sheet = await ph.evaluate(() => { const r = document.getElementById("cardmenu").getBoundingClientRect(); return { left: r.left, right: r.right, bottom: r.bottom, vw: window.innerWidth, vh: window.innerHeight }; });
    if (sheet.left !== 0 || sheet.right !== sheet.vw || Math.abs(sheet.bottom - sheet.vh) > 1) fail("pullsPage: the phone menu is not a bottom sheet: " + JSON.stringify(sheet));
    if (shot) await ph.screenshot({ path: shot + "/menu-phone.png" });
    await ph.evaluate(() => closeCardMenu());
    if (errors.length) fail("pullsPage: page errors: " + errors.join(" | "));
    await pctx.close();
  } finally { /* contexts close with the browser */ }
  if (!bad) console.log("pullsPage ok");
}

// ── moving a review to another room ──────────────────────
// A fake hub in the page: the four calls of the move, in order, and nothing after a hub refusal.
async function prMoveSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const errors = [];
  const json = (route, status, body) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
  const ready = {
    id: "pr_m", url: "https://github.com/openziti/zrok/pull/11", host: "github.com", org: "openziti", repo: "zrok", org_repo: "openziti/zrok",
    number: 11, title: "move me", why: "", head: "", head7: "abc1234", state: "ready", run_state: "", run_error: "", cost_usd: 0,
    started_at: "", ready_at: "2026-10-01T09:20:00Z", created_at: "2026-10-01T09:00:00Z", archived_at: "", run_dir: "",
    second: { state: "none", summary: "", error: "" }, author: "ekoby",
    findings: { high: 0, med: 1, low: 0, nit: 0, leak: 0 }, walk: { accepted: 0, done: 0, skipped: 0, deferred: 0, open: 1 }, walker_task: "t-old"
  };
  const st = { calls: [], refuse: "", rooms: ["alpha"] };
  await ctx.route(/\/_hub\/rooms$/, route => json(route, 200, { rooms: st.rooms.map(n => ({ name: n, host: n })) }));
  await ctx.route(/\/(v1|_hub)\/(prs|pr-claims|providers|launch)(\/|\?|$)/, async route => {
    const req = route.request();
    const p = new URL(req.url()).pathname, m = req.method();
    if (m === "GET" && p === "/v1/prs") return json(route, 200, { prs: [ready], counts: {}, nav_count: 1 });
    if (m === "GET" && p === "/_hub/pr-claims") return json(route, 200, { claims: [{ key: "github.com/openziti/zrok/11", room: "alpha" }] });
    if (m === "GET" && p === "/v1/providers") return json(route, 200, { providers: [{ name: "gh", host: "github.com", worktrees: true }] });
    st.calls.push(m + " " + p + " " + (req.postData() || ""));
    if (p === "/_hub/pr-claims/move") {
      if (st.refuse) return json(route, 409, { error: st.refuse });
      return json(route, 200, { key: "github.com/openziti/zrok/11", room: "beta", review: "moved" });
    }
    if (p === "/v1/providers/gh/pr-worktree") return json(route, 200, { path: "/wt/zrok-11", existed: false });
    if (p === "/v1/prs") return json(route, 200, { pr: ready, created: false });
    if (p === "/v1/launch") return json(route, 200, { id: "t-new", supervised: false });
    if (p === "/v1/prs/pr_m/walker") return json(route, 200, { pr: Object.assign({}, ready, { walker_task: "t-new" }) });
    return json(route, 404, { error: "no route" });
  });
  const open = async rooms => {
    st.rooms = rooms;
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof pullsPaint === "function" && typeof prMove === "function", null, { timeout: slow(15000) });
    await p.waitForFunction(() => document.querySelectorAll("#pulls-list .pull").length > 0 || pulls.loaded, null, { timeout: slow(10000) });
    await p.waitForFunction(n => hubIsHub && hubRooms.length === n, rooms.length, { timeout: slow(10000) });
    await p.evaluate(() => { switchView("pulls"); pullsPaint(); });
    return p;
  };
  const moves = async p => (await pullMenuItems(p, "pr_m")).filter(t => /^Move review and card to another room$/.test(t)).length;
  try {
    // one room: no move action
    const one = await open(["alpha"]);
    if (await moves(one) !== 0) fail("prMove: a one-room board shows the move action");
    if (process.env.PMSHOT) await one.screenshot({ path: process.env.PMSHOT + "/before-one-room.png" });
    await one.close();

    const p = await open(["alpha", "beta"]);
    if (await moves(p) !== 1) fail("prMove: the move action is missing with two rooms");
    // a refusal stops everything after the hub call
    st.refuse = "beta is offline, so the review cannot go there. bring it back or pick another room";
    st.calls = [];
    await pullMenuPress(p, "pr_m", /^Move review/);
    const items = await p.waitForSelector("#cardmenu button, .cardmenu button", { timeout: slow(5000) }).catch(() => null);
    if (!items) fail("prMove: the room picker did not open");
    if (process.env.PMSHOT) await p.screenshot({ path: process.env.PMSHOT + "/after-picker.png" });
    const names = await p.evaluate(() => [...document.querySelectorAll("#cardmenu button")].map(b => b.textContent.trim()).join());
    if (names.indexOf("beta") < 0 || names.indexOf("alpha") >= 0) fail("prMove: the picker should list beta and not the PR's own room alpha: " + names);
    await p.evaluate(() => [...document.querySelectorAll("#cardmenu button")].find(b => /beta/.test(b.textContent)).click());
    await p.waitForFunction(() => /offline/.test(document.body.textContent), null, { timeout: slow(5000) })
      .catch(() => fail("prMove: the hub's refusal was not shown"));
    if (st.calls.length !== 1 || st.calls[0].indexOf("POST /_hub/pr-claims/move") !== 0) fail("prMove: a refusal went on: " + st.calls.join(" | "));
    await p.evaluate(() => { const d = document.querySelector("dialog[open]"); if (d) d.close(); });

    // a success runs the four calls in order
    st.refuse = "";
    st.calls = [];
    await pullMenuPress(p, "pr_m", /^Move review/);
    await p.waitForSelector("#cardmenu button", { timeout: slow(5000) });
    await p.evaluate(() => [...document.querySelectorAll("#cardmenu button")].find(b => /beta/.test(b.textContent)).click());
    await p.waitForFunction(() => /moved to beta/.test(document.getElementById("toasts").textContent), null, { timeout: slow(8000) })
      .catch(() => fail("prMove: no toast after the move: " + st.calls.join(" | ")));
    if (process.env.PMSHOT) await p.screenshot({ path: process.env.PMSHOT + "/after-toast.png" });
    const order = st.calls.map(c => c.split(" ")[1]);
    const want = ["/_hub/pr-claims/move", "/v1/providers/gh/pr-worktree", "/v1/prs", "/v1/launch", "/v1/prs/pr_m/walker"];
    if (order.join() !== want.join()) fail("prMove: the calls ran as " + order.join() + ", wanted " + want.join());
    const launch = JSON.parse(st.calls[3].split(" ").slice(2).join(" "));
    if (launch.cwd !== "/wt/zrok-11" || launch.tags.join() !== "pr,pr:openziti/zrok#11") fail("prMove: the launch was " + JSON.stringify(launch));
    if (!/The old card is still on alpha/.test(await p.evaluate(() => document.getElementById("toasts").textContent))) fail("prMove: the toast does not name the old room");
    if (st.calls[4].indexOf('"action":"set"') < 0 || st.calls[4].indexOf("t-new") < 0) fail("prMove: the walker was not set: " + st.calls[4]);
    if (errors.length) fail("prMove: page errors: " + errors.join(" | "));
  } finally { await ctx.close(); }
  if (!bad) console.log("prMove ok");
}

// The daemon answers /v1/prs with a plain-text 404 (nothing serves it yet): the tab stays hidden and nothing throws.
async function pullsAbsentSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const errors = [];
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    let asked = 0;
    p.on("request", r => { if (new URL(r.url()).pathname === "/v1/prs") asked++; });
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof pullsPaint === "function", null, { timeout: slow(15000) });
    // The first read starts when the event stream opens, so "not loading" is also true before it has begun.
    await p.waitForFunction(() => pulls.loaded || pulls.loadFailed, null, { timeout: slow(10000) })
      .catch(() => fail("pullsAbsent: the first read of /v1/prs never ended"));
    if (!asked) fail("pullsAbsent: the board never asked for /v1/prs");
    if (await p.evaluate(() => !document.querySelector('.tab[data-view="pulls"]').hidden)) fail("pullsAbsent: the pulls tab shows with no /v1/prs");
    if (errors.length) fail("pullsAbsent: page errors: " + errors.join(" | "));
  } finally { await ctx.close(); }
  if (!bad) console.log("pullsAbsent ok");
}

// The walk drawer over the real pulls routes: findings from /v1/prs/{id}/findings, an edit behind a content hash,
// walk marks that move the row's counts, and the walker launch from the row's walk button.
async function pullsDrawerSection(browser, base) {
  const crypto = require("crypto");
  const sha = t => crypto.createHash("sha256").update(t).digest("hex");
  const was = mock.tasksMode;
  mock.tasksMode = "land";
  mock.landList = [
    landCard("land-pw1", { supervised: true, worktree: "/pr/walker" }),
    landCard("land-pw2", { supervised: true, worktree: "/pr/walker2" }),
    landCard("land-pw3", { supervised: true, worktree: "/pr/walker3" }),
    landCard("land-pw4", { supervised: true, worktree: "/pr/walker4" })
  ];
  mock.landPerms = [];
  const errors = [];
  const PR = "https://github.com/openziti/r/pull/1";
  const mk = (key, pos, sev, file, line, code, bullet) => {
    const text = PR + "\n" + sev.toUpperCase() + " " + file + " line " + line + ": " + code + "\n" + PR + "/files#diff-a" + line + "\n\n" +
      bullet + "\n\nEvidence\nId: " + key.slice(2) + "\n";
    return { key, position: pos, file: String(pos).padStart(2, "0") + "-" + sev + "-" + file.split("/").pop() + "-L" + line + ".txt", sev, path: file,
      line, code, link: PR + "/files#diff-a" + line, comment: bullet, evidence: {}, leak: "",
      hunk: "@@ -" + (line - 2) + ",3 +" + (line - 2) + ",4 @@\n before\n context\n+" + code + "\n after",
      walk: { state: "open", at: "", url: "" }, text, hash: sha(text) };
  };
  const row = (id, extra) => Object.assign({
    id, url: PR, host: "github.com", org: "openziti", repo: "r", org_repo: "openziti/r", number: 1, title: "title of " + id, why: "",
    head: "", head7: "abc1234", state: "ready", run_state: "", run_error: "", cost_usd: 0, started_at: "", ready_at: "2026-10-01T09:20:00Z",
    created_at: "2026-10-01T09:00:00Z", archived_at: "", run_dir: "", second: { state: "none", summary: "", error: "" }, author: "ekoby",
    findings: { high: 0, med: 1, low: 1, nit: 0, leak: 0 }, walk: { accepted: 0, done: 0, skipped: 0, deferred: 0, open: 2 }, walker_task: ""
  }, extra);
  const st = {
    rows: [row("pr_w", { walker_task: "land-pw1", number: 1 }), row("pr_l", { number: 2, created_at: "2026-10-01T08:00:00Z" }),
      // its walker's card is done, which the row does not know
      row("pr_d", { number: 3, walker_task: "land-pwdead", created_at: "2026-10-01T07:00:00Z" }),
      row("pr_x", { number: 4, walker_task: "land-pw4", created_at: "2026-10-01T06:00:00Z" })],
    f: {}, puts: [], posts: [], files: 0, findingGets: 0
  };
  const fresh = () => [mk("f-1", 1, "med", "a/b.go", 7, "x = 1", "* first bullet"), mk("f-2", 2, "low", "c.go", 20, "y = 2", "* second bullet")];
  st.f.pr_w = fresh();
  st.f.pr_l = fresh();
  st.f.pr_d = fresh();
  st.f.pr_x = fresh();
  st.gate = null;
  const counts = id => {
    const c = { accepted: 0, done: 0, skipped: 0, deferred: 0, open: 0 };
    for (const f of st.f[id]) c[f.walk.state]++;
    return c;
  };
  const json = (route, status, body) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
  const ctx = await landContext(browser);
  await ctx.addInitScript(() => {
    const Real = window.WebSocket;
    window.WebSocket = function (url, protocols) {
      if (!/\/attach(\?|$)/.test(url)) return new Real(url, protocols);
      const s = { url, readyState: 0, binaryType: "arraybuffer", onopen: null, onclose: null, onmessage: null,
        onerror: null, send() {}, close() { this.readyState = 3; } };
      setTimeout(() => { s.readyState = 1; if (s.onopen) s.onopen({}); }, 0);
      return s;
    };
    Object.assign(window.WebSocket, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });
  });
  const entry = { name: "01-med-b.go-L7.txt", dir: false, mtime: "2026-10-01T09:00:00Z", size: 10 };
  await ctx.route(/\/v1\/tasks\/land-pw4\/files\/list/, route => json(route, 200, { entries: [entry] }));
  await ctx.route(/\/v1\/tasks\/land-pw4\/files\/text/, route => json(route, 200, { text: st.f.pr_x[0].text, hash: "h", eol: "\n" }));
  // The walk reads the head tree's copy of a finding's file under src/ for its context lines (u-008), so only other reads count.
  await ctx.route(/\/v1\/tasks\/land-pw[12]\/files\//, async route => { if (!/[?&]path=src%2F/.test(route.request().url())) st.files++; return json(route, 404, { error: "no files here" }); });
  await ctx.route(/\/v1\/prs(\/|\?|$)/, async route => {
    const req = route.request();
    const m = req.method();
    const p = new URL(req.url()).pathname;
    if (m === "GET" && p === "/v1/prs") {
      if (st.gate) await st.gate;
      st.rows.forEach(r => { r.walk = counts(r.id); });
      return json(route, 200, { prs: st.rows, counts: { queued: 0, fetching: 0, running: 0, ready: 2, failed: 0, aborted: 0 }, nav_count: 2 });
    }
    if (m !== "GET") st.posts.push(m + " " + p + " " + (req.postData() || ""));
    const mm = p.match(/^\/v1\/prs\/([^/]+)(\/.*)?$/);
    const id = mm && mm[1], rest = (mm && mm[2]) || "";
    const cur = st.rows.find(r => r.id === id);
    if (!cur) return json(route, 404, { error: "no such pr", code: "not_found" });
    if (m === "GET" && rest === "/findings") {
      st.findingGets++;
      cur.walk = counts(id);
      return json(route, 200, { pr: cur, findings: st.f[id] });
    }
    const fm = rest.match(/^\/findings\/([^/]+)(\/walk)?$/);
    const f = fm && st.f[id].find(x => x.key === decodeURIComponent(fm[1]));
    if (fm && !f) return json(route, 404, { error: "no such finding", code: "not_found" });
    if (m === "PUT" && fm && !fm[2]) {
      const b = JSON.parse(req.postData() || "{}");
      st.puts.push({ key: f.key, hash: b.hash, text: b.text, eol: b.eol });
      if (!b.hash) return json(route, 400, { error: "a write has to say what it was based on", code: "bad_request" });
      if (b.hash !== f.hash) {
        return json(route, 409, { error: "that file changed while you were editing it. nothing was written.", code: "changed", text: f.text, hash: f.hash });
      }
      f.text = b.text;
      f.hash = sha(b.text);
      if (b.text.indexOf("NEWKEY") >= 0) f.key = f.key + "x";
      return json(route, 200, { ok: true, hash: f.hash, key: f.key });
    }
    if (m === "POST" && fm && fm[2]) {
      const b = JSON.parse(req.postData() || "{}");
      if (["done", "accepted", "skipped", "deferred", "open"].indexOf(b.state) < 0) return json(route, 400, { error: "state is open, accepted, done, skipped or deferred", code: "bad_request" });
      f.walk = b.state === "open" ? { state: "open", at: "", url: "" } : { state: b.state, at: "2026-10-01T14:20:00Z", url: b.url || "" };
      return json(route, 200, { ok: true, walk: f.walk, counts: counts(id) });
    }
    if (m === "POST" && rest === "/walker") {
      // a live walker is answered as it is, a done one is replaced
      if (cur.walker_task && cur.walker_task !== "land-pwdead") return json(route, 200, { pr: cur, task: cur.walker_task, launched: false });
      cur.walker_task = cur.id === "pr_d" ? "land-pw3" : "land-pw2";
      return json(route, 201, { pr: cur, task: cur.walker_task, launched: true });
    }
    return json(route, 404, { error: "no route", code: "not_found" });
  });
  const S = "#walk-drawer ";
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector('#stack-list .stackrow[data-id="land-pw1"]', { state: "attached", timeout: slow(15000) });
    await p.waitForFunction(() => typeof pulls !== "undefined" && pulls.loaded, null, { timeout: slow(15000) });
    await attachRetry(p, { unit: "pullsDrawer", id: "land-pw1", check: () => termSock && termSock.readyState === 1 && termTask && termTask.id === "land-pw1" });
    await p.waitForSelector("#t-walk:not([hidden])", { timeout: slow(10000) })
      .catch(() => fail("pullsDrawer: the walker of a pulls row has no walk button"));
    await p.click("#t-walk");
    await p.waitForFunction(() => document.querySelectorAll("#walk-rail .wk-row").length === 2, null, { timeout: slow(10000) })
      .catch(() => fail("pullsDrawer: the rail did not draw the two findings of /v1/prs/pr_w/findings"));
    if (!st.findingGets) fail("pullsDrawer: the findings were not read from /v1/prs");
    if (st.files) fail("pullsDrawer: the drawer read the card's files: " + st.files);
    const comment = () => p.textContent(S + ".wk-comment");
    if (!/first bullet/.test(await comment())) fail("pullsDrawer: the comment is " + (await comment()));
    const code = await p.$$eval(S + ".wk-code .wk-l", e => e.length);
    if (code < 3) fail("pullsDrawer: the hunk drew " + code + " code lines");

    // an edit quotes the hash it read, and follows the key the daemon gives back
    const hash0 = st.f.pr_w[0].hash;
    await p.focus("#walk-drawer");
    await p.keyboard.press("e");
    await p.fill("#walk-edit-text", "MED a/b.go line 7: x = 1\n\n* edited NEWKEY");
    await p.click(S + '[data-act="save"]');
    await p.waitForFunction(() => /edited NEWKEY/.test(document.querySelector("#walk-item .wk-comment")?.textContent || ""), null, { timeout: slow(5000) })
      .catch(() => fail("pullsDrawer: a saved edit did not show"));
    if (st.puts.length !== 1 || st.puts[0].key !== "f-1" || st.puts[0].hash !== hash0 || !/edited NEWKEY/.test(st.puts[0].text)) {
      fail("pullsDrawer: the edit put " + JSON.stringify(st.puts));
    }
    if (await p.evaluate(() => dock.cur) !== "f-1x") fail("pullsDrawer: the drawer did not follow the new key: " + await p.evaluate(() => dock.cur));
    const hash1 = st.f.pr_w[0].hash;
    await p.focus("#walk-drawer");
    await p.keyboard.press("e");
    await p.fill("#walk-edit-text", "MED a/b.go line 7: x = 1\n\n* edited again");
    await p.click(S + '[data-act="save"]');
    await p.waitForFunction(() => /edited again/.test(document.querySelector("#walk-item .wk-comment")?.textContent || ""), null, { timeout: slow(5000) })
      .catch(() => fail("pullsDrawer: a second edit did not show"));
    if (st.puts.length !== 2 || st.puts[1].key !== "f-1x" || st.puts[1].hash !== hash1) fail("pullsDrawer: the second edit put " + JSON.stringify(st.puts[1]));

    // a stale hash is said in words and nothing is written until it is chosen
    const f0 = st.f.pr_w[0];
    f0.text = f0.text.replace("* edited again", "* changed elsewhere");
    f0.hash = sha(f0.text);
    const diskHash = f0.hash;
    await p.focus("#walk-drawer");
    await p.keyboard.press("e");
    await p.fill("#walk-edit-text", "MED a/b.go line 7: x = 1\n\n* mine");
    await p.click(S + '[data-act="save"]');
    await p.waitForSelector("#walk-cmp-disk", { timeout: slow(5000) })
      .catch(() => fail("pullsDrawer: a stale hash did not open the compare"));
    if (!/changed elsewhere/.test(await p.textContent("#walk-cmp-disk")) || !/mine/.test(await p.textContent("#walk-cmp-mine"))) fail("pullsDrawer: the compare lost a side");
    if (!/nothing was written/.test(await p.textContent(S + ".wk-warns") || "")) fail("pullsDrawer: the stale answer was not said in words");
    if (f0.text.indexOf("* mine") >= 0) fail("pullsDrawer: a stale write landed");
    await p.click(S + '[data-act="keep"]');
    await p.waitForFunction(() => /mine/.test(document.querySelector("#walk-item .wk-comment")?.textContent || ""), null, { timeout: slow(5000) })
      .catch(() => fail("pullsDrawer: keep mine did not write"));
    if (st.puts.length !== 4 || st.puts[3].hash !== diskHash) fail("pullsDrawer: keep mine put " + JSON.stringify(st.puts[3]));

    // walk marks move the row's counts
    const walkOf = () => p.evaluate(() => JSON.stringify(pulls.rows.find(r => r.id === "pr_w").walk));
    await p.focus("#walk-drawer");
    await p.keyboard.press("s");
    await p.waitForFunction(() => document.querySelector("#walk-rail .wk-row.cur.dismissed"), null, { timeout: slow(5000) })
      .catch(() => fail("pullsDrawer: s did not mark the row dismissed"));
    if (await walkOf() !== JSON.stringify({ accepted: 0, done: 0, skipped: 1, deferred: 0, open: 1 })) fail("pullsDrawer: the row counts after a skip: " + await walkOf());
    await p.keyboard.press("j");
    await p.keyboard.press("y");
    await p.waitForFunction(() => document.querySelector("#walk-rail .wk-row.cur.accepted"), null, { timeout: slow(5000) })
      .catch(() => fail("pullsDrawer: y did not accept the finding"));
    if (await walkOf() !== JSON.stringify({ accepted: 1, done: 0, skipped: 1, deferred: 0, open: 0 })) fail("pullsDrawer: the row counts after an accept: " + await walkOf());
    const midway = await p.evaluate(() => pullProgress(pulls.rows.find(r => r.id === "pr_w")));
    if (midway !== "1 of 2: 1 accepted, 0 posted, 1 dismissed") fail("pullsDrawer: an accepted finding is not reviewed yet: " + JSON.stringify(midway));

    await p.focus("#walk-drawer");
    await p.keyboard.press("f");
    await p.waitForFunction(() => document.querySelector("#walk-rail .wk-row.cur.deferred"), null, { timeout: slow(5000) })
      .catch(() => fail("pullsDrawer: f did not defer the finding"));
    if (await walkOf() !== JSON.stringify({ accepted: 0, done: 0, skipped: 1, deferred: 1, open: 0 })) fail("pullsDrawer: the row counts after a defer: " + await walkOf());
    if (!st.posts.some(x => x.indexOf("/findings/f-2/walk") >= 0 && x.indexOf('"state":"deferred"') >= 0)) fail("pullsDrawer: the defer did not post");
    await p.keyboard.press("u");
    await p.waitForFunction(() => !document.querySelector("#walk-rail .wk-row.deferred"), null, { timeout: slow(5000) })
      .catch(() => fail("pullsDrawer: u did not put the finding back to open"));
    if (await walkOf() !== JSON.stringify({ accepted: 0, done: 0, skipped: 1, deferred: 0, open: 1 })) fail("pullsDrawer: the row counts after an undo: " + await walkOf());
    await p.evaluate(() => switchView("pulls"));
    const said = await p.evaluate(() => document.querySelector('#pulls-list .pull[data-id="pr_w"] .pull-progress').textContent);
    if (said !== "1 of 2: 0 accepted, 0 posted, 1 dismissed") fail("pullsDrawer: the pulls row says " + JSON.stringify(said));

    // the walk button launches a walker when there is none, attaches it and opens the drawer
    await p.click('#pulls-list .pull[data-id="pr_l"] button[data-act="walk"]');
    await p.waitForFunction(() => termTask && termTask.id === "land-pw2" && !document.getElementById("walk-drawer").hidden, null, { timeout: slow(10000) })
      .catch(() => fail("pullsDrawer: the walk button did not open the new walker's drawer"));
    if (!st.posts.some(x => x === 'POST /v1/prs/pr_l/walker {"action":"launch"}')) fail("pullsDrawer: no walker launch was posted: " + st.posts.join(" | "));
    await p.waitForFunction(() => document.querySelectorAll("#walk-rail .wk-row").length === 2, null, { timeout: slow(10000) })
      .catch(() => fail("pullsDrawer: the launched walker's drawer has no findings"));
    // the walker of a row finished: the daemon replaces a done walker and answers a live one as it is
    await p.evaluate(() => switchView("pulls"));
    await p.click('#pulls-list .pull[data-id="pr_d"] button[data-act="walk"]');
    await p.waitForFunction(() => termTask && termTask.id === "land-pw3" && !document.getElementById("walk-drawer").hidden, null, { timeout: slow(10000) })
      .catch(() => fail("pullsDrawer: walk on a row whose walker is done did not reach a new walker"));
    await p.evaluate(() => switchView("pulls"));
    await p.click('#pulls-list .pull[data-id="pr_d"] button[data-act="walk"]');
    await p.waitForTimeout(300);
    if (st.posts.filter(x => x === 'POST /v1/prs/pr_d/walker {"action":"launch"}').length !== 2) fail("pullsDrawer: walk did not ask the daemon for the walker every time");
    if (await p.evaluate(() => termTask.id) !== "land-pw3") fail("pullsDrawer: a live walker was replaced");
    await p.close();

    // At phone width every state is one tap, and the buttons have to be reachable: the drawer is not cut off
    // below the rail, the finding pane scrolls, and each button is tapped here, not keyed.
    // `WALK_SHOTS=dir` shoots the buttons in view at each width and a finding after each state.
    for (const vp of [{ width: 390, height: 844 }, { width: 360, height: 740 }]) {
      const tag = "pullsDrawer phone " + vp.width + ": ";
      st.f.pr_w = fresh();
      const ph = await ctx.newPage();
      ph.on("pageerror", e => errors.push(String(e)));
      await ph.setViewportSize(vp);
      // the board re-attaches the remembered card half a second after load, which would take the drawer from land-pw1
      await ph.addInitScript(() => localStorage.removeItem("atrium.term"));
      await ph.goto(base, { waitUntil: "domcontentloaded" });
      await ph.waitForFunction(() => typeof pulls !== "undefined" && pulls.loaded, null, { timeout: slow(15000) });
      await attachRetry(ph, { unit: "pullsDrawer", id: "land-pw1", check: () => termSock && termSock.readyState === 1 && termTask && termTask.id === "land-pw1" });
      await ph.waitForSelector("#t-walk:not([hidden])", { timeout: slow(10000) });
      await ph.click("#t-walk");
      await ph.waitForSelector("#walk-item .wk-actions button[data-act=y]", { state: "attached", timeout: slow(10000) });
      const shoot = async name => {
        if (process.env.WALK_SHOTS) await ph.screenshot({ path: path.join(process.env.WALK_SHOTS, vp.width + "-" + name + ".png") });
      };
      const btn = act => ph.locator("#walk-item .wk-actions button[data-act=" + act + "]");
      await shoot("finding");
      // the finding pane is what scrolls: bring the buttons into view and say where they landed
      await btn("y").scrollIntoViewIfNeeded();
      const geo = await ph.evaluate(() => {
        const b = [...document.querySelectorAll("#walk-item .wk-actions button")].map(x => x.getBoundingClientRect());
        const d = document.getElementById("walk-drawer").getBoundingClientRect();
        const t = document.getElementById("t-screen").getBoundingClientRect();
        return { top: Math.min(...b.map(r => r.top)), bottom: Math.max(...b.map(r => r.bottom)), h: Math.min(...b.map(r => r.height)),
          dBottom: d.bottom, tTop: t.top, vh: innerHeight, dTop: d.top };
      });
      console.log(tag + JSON.stringify(geo));
      if (geo.top < geo.dTop - 1 || geo.bottom > geo.dBottom + 1) fail(tag + "the action buttons are outside the drawer: " + JSON.stringify(geo));
      if (geo.bottom > geo.vh + 1) fail(tag + "the action buttons are below the viewport: " + JSON.stringify(geo));
      if (geo.h < 39) fail(tag + "a button is under 40px tall: " + JSON.stringify(geo));
      await shoot("buttons");
      const rowState = () => ph.evaluate(() => document.querySelector("#walk-rail .wk-row.cur").className.replace(/\b(wk-row|cur|brk)\b/g, "").trim());
      for (const [act, want] of [["y", "accepted"], ["d", "posted"], ["s", "dismissed"], ["f", "deferred"]]) {
        await btn(act).scrollIntoViewIfNeeded();
        await btn(act).click();
        // posted asks for the comment's URL, and is one more tap
        if (act === "d") await ph.locator("#ask-actions button", { hasText: "mark posted" }).click();
        await ph.waitForFunction(w => document.querySelector("#walk-rail .wk-row.cur." + w), want, { timeout: slow(5000) })
          .catch(() => fail(tag + "tapping " + act + " did not mark the finding " + want));
        const c = await ph.evaluate(() => JSON.stringify(pulls.rows.find(r => r.id === "pr_w").walk));
        const wantKey = { accepted: "accepted", posted: "done", dismissed: "skipped", deferred: "deferred" }[want];
        if (JSON.parse(c)[wantKey] !== 1) fail(tag + "the row counts after " + act + ": " + c);
        if (!(await ph.locator("#walk-item .wk-state." + want).count())) fail(tag + "the finding does not show " + want);
        await ph.evaluate(() => { document.getElementById("walk-item").scrollTop = 0; });
        await shoot("state-" + want);
      }
      await btn("u").click();
      await ph.waitForFunction(() => !document.querySelector("#walk-rail .wk-row.cur.accepted, #walk-rail .wk-row.cur.posted, #walk-rail .wk-row.cur.dismissed, #walk-rail .wk-row.cur.deferred"), null, { timeout: slow(5000) })
        .catch(() => fail(tag + "tapping u did not put the finding back to open"));
      const open = await ph.evaluate(() => JSON.stringify(pulls.rows.find(r => r.id === "pr_w").walk));
      if (JSON.parse(open).open !== 2) fail(tag + "the row counts after undo: " + open);
      await ph.evaluate(() => { document.getElementById("walk-item").scrollTop = 0; });
      await shoot("state-undo");
      await ph.close();
    }

    // a walker's card attached before the rows are known is re-probed when they arrive, so its marks go to walk.txt
    let release;
    st.gate = new Promise(r => { release = r; });
    const q = await ctx.newPage();
    q.on("pageerror", e => errors.push(String(e)));
    await q.goto(base, { waitUntil: "domcontentloaded" });
    await q.waitForSelector('#stack-list .stackrow[data-id="land-pw4"]', { state: "attached", timeout: slow(15000) });
    await attachRetry(q, { unit: "pullsDrawer", id: "land-pw4", check: () => !!document.querySelector("#t-walk:not([hidden])") });
    if (await q.evaluate(() => walkTenant.prId)) fail("pullsDrawer: the review was known before the rows were");
    st.gate = null;
    release();
    await q.waitForFunction(() => walkTenant.prId === "pr_x", null, { timeout: slow(10000) })
      .catch(() => fail("pullsDrawer: a walker attached before the rows loaded stayed on the files source"));
    if (errors.length) fail("pullsDrawer: the page threw: " + errors.join(" | "));
  } finally {
    await ctx.close();
    mock.tasksMode = was;
    mock.landList = [];
  }
  if (!bad) console.log("pullsDrawer ok");
}

async function pullsPasteOpeningSection(browser, base) {
  const errors = [];
  const json = (route, status, body) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
  const shots = process.env.PASTE_SHOTS || "", tag = process.env.PASTE_TAG || "after";
  const shot = (p, name) => shots ? p.screenshot({ path: path.join(shots, name + "-" + tag + ".png") }) : null;
  const rowOf = {
    id: "pr_z", url: "https://github.com/openziti/x/pull/1", host: "github.com", org: "openziti", repo: "x", org_repo: "openziti/x",
    number: 1, title: "an existing review", why: "", head: "", head7: "abc1234", state: "ready", run_state: "", run_error: "",
    cost_usd: 0, started_at: "", ready_at: "2026-10-01T09:20:00Z", created_at: "2026-10-01T09:00:00Z", archived_at: "", run_dir: "",
    second: { state: "none", summary: "", error: "" }, author: "ekoby",
    findings: { high: 0, med: 0, low: 0, nit: 0, leak: 0 }, walk: { accepted: 0, done: 0, skipped: 0, deferred: 0, open: 0 }, walker_task: ""
  };
  const opens = [];
  let release = null;
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  await ctx.route(/\/v1\/prs(\/|\?|$)/, route => json(route, 200, { prs: [rowOf], counts: {}, nav_count: 0 }));
  await ctx.route(/\/v1\/open$/, async route => {
    opens.push(JSON.parse(route.request().postData() || "{}"));
    await new Promise(r => { release = r; });
    return json(route, 422, { error: "could not fetch the head of pull request 972", code: "failed" });
  });
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof pullsPaint === "function" && typeof switchView === "function", null, { timeout: slow(15000) });
    await p.waitForFunction(() => !document.querySelector('.tab[data-view="pulls"]').hidden, null, { timeout: slow(10000) });
    await p.evaluate(() => switchView("pulls"));
    await p.waitForSelector("#pulls-paste", { timeout: slow(10000) });
    await p.click("#pulls-paste");
    await p.fill("#pulls-pop-url", "https://github.com/openziti/x/pull/972");
    await shot(p, "popover");
    await p.click("#pulls-pop-go");
    await p.waitForSelector(".card.linkopen", { state: "attached", timeout: slow(5000) })
      .catch(() => fail("pullsPasteOpening: the popover paste drew no card"));
    await shot(p, "in-flight");
    if (!await p.locator("#pulls-list .card.linkopen").isVisible()) fail("pullsPasteOpening: the card is not visible on the pulls page");
    if (!(await p.isHidden("#pulls-pop"))) fail("pullsPasteOpening: the popover stayed open while the open was in flight");
    await until(() => opens.length, 5000, "/v1/open was sent").catch(() => {});
    if (!opens.length || !/^op-/.test(opens[0].progress || "") || opens[0].url !== "https://github.com/openziti/x/pull/972")
      fail("pullsPasteOpening: /v1/open was sent " + JSON.stringify(opens));
    if (await p.evaluate(() => [...linkOpenings.keys()][0]) !== opens[0].progress) fail("pullsPasteOpening: the card is not keyed by the progress id");
    await p.evaluate(() => {
      const o = [...linkOpenings.values()][0];
      linkOpeningStep([...linkOpenings.keys()][0], "fetching the head of pull request 972");
      o.since = Date.now() - 14000;
      linkOpeningPaint();
    });
    const line = () => p.evaluate(() => ((document.querySelector(".card.linkopen .linkopen-say") || {}).textContent || "").trim());
    if (!/fetching the head of pull request 972… 1[4-6]s$/.test(await line())) fail("pullsPasteOpening: the step line is " + JSON.stringify(await line()));
    await shot(p, "slow-step");
    const before = await line();
    await p.waitForFunction(b => (document.querySelector(".card.linkopen .linkopen-say") || {}).textContent !== b, before, { timeout: slow(4000) })
      .catch(() => fail("pullsPasteOpening: the elapsed seconds were not repainted"));
    await p.evaluate(() => linkOpeningStep([...linkOpenings.keys()][0], "making the worktree"));
    if (/\ds$/.test(await line())) fail("pullsPasteOpening: a new step kept the old step's seconds: " + await line());
    release();
    await p.waitForSelector(".card.linkopen.failed", { state: "attached", timeout: slow(5000) })
      .catch(() => fail("pullsPasteOpening: a failed open did not stay as a card"));
    if (!/could not fetch the head/.test(await p.textContent(".card.linkopen.failed"))) fail("pullsPasteOpening: the failure had no reason");
    await p.evaluate(() => [...linkOpenings.keys()].forEach(linkOpeningEnd));
    if (await p.evaluate(() => linkOpeningTimer)) fail("pullsPasteOpening: the timer kept running with no opening in flight");
  } finally { await ctx.close(); }
  if (errors.length) fail("pullsPasteOpening: page errors: " + errors.join(" | "));
  if (!bad) console.log("pullsPasteOpening ok");
}

// ── a live run says what it is doing ─────────────────────
// A fetching or running row carries the tail of run.log, and the board draws the last lines with the seconds of the one the
// run is on. An event moves them without a read, and a run that ends takes them away. PLSHOT=/dir writes the picture and
// PLBEFORE=1 draws the rows without the steps, as the board did before them.
async function pullsLiveStepsSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const errors = [];
  const json = (route, status, body) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
  const clock = ms => new Date(Date.now() - ms).toISOString().slice(11, 19);
  const line = (ago, text) => clock(ago) + " " + text + "\n";
  const row = (id, extra) => Object.assign({
    id, url: "https://github.com/openziti/ziti-sdk-c/pull/1186", host: "github.com", org: "openziti", repo: "ziti-sdk-c", org_repo: "openziti/ziti-sdk-c",
    number: 1186, title: "title of " + id, why: "", head: "", head7: "abc1234", state: "fetching", run_state: "fetch", run_error: "",
    cost_usd: 0, started_at: "", ready_at: "", created_at: "2026-10-20T15:02:00Z", archived_at: "", run_dir: "",
    second: { state: "none", summary: "", error: "" }, author: "ekoby", run_log: "",
    findings: { high: 0, med: 0, low: 0, nit: 0, leak: 0 }, walk: { accepted: 0, done: 0, skipped: 0, deferred: 0, open: 0 }, walker_task: ""
  }, extra);
  const st = { rows: [
    row("pr_fetch", { number: 1186, created_at: "2026-10-20T15:04:00Z", run_log:
      line(52000, "fetch start") + line(51000, "fetch: asking the forge") + line(47000, "fetch: forge answered (4.1s)") +
      line(46000, "fetch: opening the hub's store") + line(46000, "fetch: hub's store open (0.0s)") +
      line(45000, "fetch: fetching the head into the source folder") }),
    row("pr_prime", { number: 1187, state: "running", run_state: "prime", created_at: "2026-10-20T15:03:00Z", run_log:
      line(90000, "fetch start") + line(60000, "fetch end 0.00") + line(60000, "prime start") +
      line(59000, "prime: reading the bundle and preparing the agent") + line(58000, "prime: agent prepared (0.4s)") +
      line(58000, "prime: launching the agent prime") }),
    row("pr_new", { number: 1188, created_at: "2026-10-20T15:05:00Z" }),
    row("pr_bad", { number: 1189, state: "failed", run_state: "", created_at: "2026-10-20T15:01:00Z", run_error: "fetch: gh pr view: could not resolve to a PullRequest" })
  ] };
  const emit = r => {
    const i = st.rows.findIndex(x => x.id === r.id);
    if (i >= 0) st.rows[i] = r;
    openStreams.forEach(s => { try { s.write("event: pr\ndata: " + JSON.stringify({ pr: r }) + "\n\n"); } catch (e) {} });
  };
  let gets = 0;
  await ctx.route(/\/v1\/prs(\/|\?|$)/, route => {
    gets++;
    json(route, 200, { prs: st.rows, counts: {}, nav_count: 1 });
  });
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof pullsPaint === "function" && typeof switchView === "function", null, { timeout: slow(15000) });
    await p.waitForFunction(() => !document.querySelector('.tab[data-view="pulls"]').hidden, null, { timeout: slow(10000) });
    await p.evaluate(() => switchView("pulls"));
    await p.waitForFunction(() => document.querySelectorAll("#pulls-list .pull").length === 4, null, { timeout: slow(10000) });
    if (process.env.PLBEFORE) {
      await p.evaluate(() => { pullStepsHTML = () => ""; pullsPaint(); });
      if (process.env.PLSHOT) await p.screenshot({ path: process.env.PLSHOT });
      return;
    }
    const sel = id => '#pulls-list .pull[data-id="' + id + '"] .pull-steps';
    const texts = (id, cls) => p.evaluate(([s, c]) => [...document.querySelectorAll(s + " " + c)].map(e => e.textContent.trim()), [sel(id), cls]);
    let was = await texts("pr_fetch", ".was"), now = await texts("pr_fetch", ".now");
    if (was.length !== 5 || was[4] !== "fetch: hub's store open (0.0s)") fail("pullsLiveSteps: the earlier lines are " + JSON.stringify(was));
    if (!/^fetch: fetching the head into the source folder \d+s$/.test(now[0] || "")) fail("pullsLiveSteps: the line it is on is " + JSON.stringify(now));
    if (was[0] !== "fetch: starting") fail("pullsLiveSteps: a start line is not said as a step: " + was[0]);
    if ((await texts("pr_prime", ".was"))[1] !== "fetch: done") fail("pullsLiveSteps: an end line is not said as done: " + (await texts("pr_prime", ".was"))[1]);
    if (!/^prime: launching the agent prime (\d+m )?\d+s$/.test((await texts("pr_prime", ".now"))[0] || "")) fail("pullsLiveSteps: a running row does not say it is launching the agent");
    if (!/^starting/.test((await texts("pr_new", ".now"))[0] || "")) fail("pullsLiveSteps: a row with no log yet says " + JSON.stringify(await texts("pr_new", ".now")));
    if (await p.evaluate(() => document.querySelector('#pulls-list .pull[data-id="pr_bad"] .pull-steps'))) fail("pullsLiveSteps: a failed row draws steps");
    const failedNote = await p.evaluate(() => document.querySelector('#pulls-list .pull[data-id="pr_bad"] .pull-stage').textContent);
    const failedPill = await p.evaluate(() => document.querySelector('#pulls-list .pull[data-id="pr_bad"] .pull-state').textContent);
    if (failedNote !== "gh pr view: could not resolve to a PullRequest" || !/^failed at /.test(failedPill)) fail("pullsLiveSteps: a failure says " + failedPill + " / " + failedNote);
    const secs = () => p.evaluate(() => document.querySelector('#pulls-list .pull[data-id="pr_fetch"] .pull-el').textContent);
    const first = await secs();
    await p.waitForFunction(f => document.querySelector('#pulls-list .pull[data-id="pr_fetch"] .pull-el').textContent !== f, first, { timeout: slow(4000) })
      .catch(() => fail("pullsLiveSteps: the seconds did not count up from " + first));
    if (process.env.PLSHOT) await p.screenshot({ path: process.env.PLSHOT });
    await until(() => openStreams.length || openStreams.size, 5000, "the pulls stream opened");
    const before = gets;
    emit(Object.assign({}, st.rows[0], { run_log: st.rows[0].run_log + line(0, "fetch: head fetched (44.0s)") + line(0, "fetch: making the worktree") }));
    await p.waitForFunction(() => /making the worktree/.test(document.querySelector('#pulls-list .pull[data-id="pr_fetch"] .pull-steps .now').textContent), null, { timeout: slow(5000) })
      .catch(() => fail("pullsLiveSteps: an event did not move the line"));
    was = await texts("pr_fetch", ".was");
    if (was.length !== 5 || was[4] !== "fetch: head fetched (44.0s)") fail("pullsLiveSteps: the old line is not history: " + JSON.stringify(was));
    if (gets !== before) fail("pullsLiveSteps: an event caused a read");
    emit(Object.assign({}, st.rows[1], { state: "failed", run_state: "", run_error: "prime: claude exited 1", run_log: "" }));
    await p.waitForFunction(() => !document.querySelector('#pulls-list .pull[data-id="pr_prime"] .pull-steps'), null, { timeout: slow(5000) })
      .catch(() => fail("pullsLiveSteps: a failed run kept its steps"));
    if ((await p.evaluate(() => document.querySelector('#pulls-list .pull[data-id="pr_prime"] .pull-stage').textContent)) !== "claude exited 1") fail("pullsLiveSteps: the failure is not the first line of why");
    emit(Object.assign({}, st.rows[0], { state: "failed", run_error: "fetch: no", run_log: "" }));
    emit(Object.assign({}, st.rows[2], { state: "failed", run_error: "fetch: no", run_log: "" }));
    await p.waitForFunction(() => !document.querySelector("#pulls-list .pull-el"), null, { timeout: slow(5000) });
    await p.waitForFunction(() => !pullsElTimer, null, { timeout: slow(4000) }).catch(() => fail("pullsLiveSteps: the timer kept running with no live row"));
    if (errors.length) fail("pullsLiveSteps: page errors: " + errors.join(" | "));
  } finally { await ctx.close(); }
  if (!bad) console.log("pullsLiveSteps ok");
}

const units = [
  { name: "pullsLiveSteps", run: ({ browser, base }) => pullsLiveStepsSection(browser, base) },
  { name: "pullsPasteOpening", run: ({ browser, base }) => pullsPasteOpeningSection(browser, base) },
  { name: "pulls", run: ({ browser, base }) => pullsSection(browser, base) },
  { name: "pullsPanelStage", run: ({ browser, base }) => pullsPanelStageSection(browser, base) },
  { name: "pullsPage", run: ({ browser, base }) => pullsPageSection(browser, base) },
  { name: "pullsAbsent", run: ({ browser, base }) => pullsAbsentSection(browser, base) },
  { name: "pullsDrawer", run: ({ browser, base }) => pullsDrawerSection(browser, base) },
  { name: "prMove", run: ({ browser, base }) => prMoveSection(browser, base) },
];
const sections = { pullsLiveSteps: pullsLiveStepsSection, pullsPasteOpening: pullsPasteOpeningSection, pulls: pullsSection, pullsPanelStage: pullsPanelStageSection, pullsPage: pullsPageSection, pullsAbsent: pullsAbsentSection, pullsDrawer: pullsDrawerSection, prMove: prMoveSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
