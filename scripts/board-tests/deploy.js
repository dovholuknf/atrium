// deploy: landing and deploying from the board. The deploy ready chip is gone, so only the land units remain.
//
// Units: land, landThePlane.
// `units` is what the harness runs, in suite order. `sections` is for HEADLESS_ONLY, which names a section and not a unit.
// Alone: `node scripts/board-tests/deploy.js` runs just these units, the way the full run would.
const { slow, frames, until } = require("../lib/browser.js");
const { mock, openStreams, landCard, T1 } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");
const { landContext } = require("../lib/helpers.js");

// Records each attach at `openTerm`, which every landing on a terminal reaches.
function spyAttach(p) {
  return p.evaluate(() => {
    window.__opened = [];
    const real = openTerm;
    openTerm = t => { window.__opened.push(t.id); return real(t); };
  });
}

// Where the page is: the view showing, the last card attached, and whether the
// card detail is open and on what.
function landedAt(p) {
  return p.evaluate(() => ({
    view: VIEWS.find(v => !document.getElementById(v).hidden) || "",
    opened: (window.__opened || []).slice(-1)[0] || "",
    detail: document.getElementById("detail").open ? document.getElementById("d-title").textContent : "",
    dialogs: [...document.querySelectorAll("dialog[open]")].map(d => d.id)
  }));
}

async function landSection(browser, base) {
  const was = mock.tasksMode;
  mock.tasksMode = "land";
  const poke = () => openStreams.forEach(r => { try { r.write("event: task\ndata: {}\n\n"); } catch (e) {} });
  const reset = p => p.evaluate(() => {
    closeTerm(true);
    document.querySelectorAll("dialog[open]").forEach(d => d.close());
    document.querySelectorAll("#toasts .toast").forEach(t => t.remove());
    switchView("stack");
    window.__opened = [];
  });
  const clickToast = (p, text) => p.click(`#toasts .toast:has-text("${text}") b`);
  // Settles on a place, rather than asserting at a fixed moment: the race case
  // lands after a wait, and a place asserted early is the wrong place.
  const settle = async (p, want, ms) => {
    let at;
    await until(async () => want(at = await landedAt(p)), ms || 6000, "a landing", 50).catch(() => {});
    return at;
  };
  landCard("land-live", { supervised: true, created_at: "2026-09-19T12:00:00Z" });
  landCard("land-dead", { status: "dead", supervised: false, created_at: "2026-09-19T12:00:00Z" });
  landCard("land-pc", { status: "needs-permission", supervised: false, created_at: "2026-09-19T12:00:00Z" });
  mock.landList = [mock.LAND["land-live"], mock.LAND["land-dead"]];
  mock.landPerms = [];
  const errors = [];
  let ctx = await landContext(browser);
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    if (process.env.DEBUG_HEADLESS) p.on("console", m => console.error("[land] " + m.text()));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await p.evaluate(() => runRefresh());
    await spyAttach(p);

    // 1. A new card whose terminal is not there yet, from its own toast.
    landCard("land-new", { supervised: false });
    mock.landList = mock.landList.concat(mock.LAND["land-new"]);
    poke();
    await p.waitForSelector('#toasts .toast:has-text("land new is on the board")', { timeout: slow(10000) });
    await clickToast(p, "land new is on the board");
    setTimeout(() => { mock.LAND["land-new"].supervised = true; }, 1500);
    let at = await settle(p, a => a.opened === "land-new");
    if (at.opened !== "land-new" || at.view !== "terms") {
      fail("a new card's toast, clicked before its terminal opened, landed on " + JSON.stringify(at) +
        ", not its terminal.");
    }

    // 2. The service worker's message, which is how a desktop notification
    // clicked with the board in the background arrives. Same race.
    await reset(p);
    landCard("land-new2", { supervised: false });
    mock.landList = mock.landList.concat(mock.LAND["land-new2"]);
    await p.evaluate(() => navigator.serviceWorker.dispatchEvent(new MessageEvent("message",
      { data: { type: "goTo", view: "stack", taskFor: "land-new2", key: "" } })));
    setTimeout(() => { mock.LAND["land-new2"].supervised = true; }, 1000);
    at = await settle(p, a => a.opened === "land-new2");
    if (at.opened !== "land-new2" || at.view !== "terms") {
      fail("a desktop notification for a new card landed on " + JSON.stringify(at) + ", not its terminal.");
    }

    // 3. The toast log.
    await reset(p);
    await p.evaluate(() => { toast("land live is ready", "", "stack", null, "land-live"); });
    await p.evaluate(() => document.querySelectorAll("#toasts .toast").forEach(t => t.remove()));
    await p.evaluate(() => openToastLog());
    await p.click('#toastlog-list .tlrow.clickable:has-text("land live is ready")');
    at = await settle(p, a => a.opened === "land-live");
    if (at.opened !== "land-live" || at.view !== "terms" || at.dialogs.includes("toastlog")) {
      fail("a toast log row for a live card landed on " + JSON.stringify(at) + ", not its terminal.");
    }

    // 4. A card with no terminal: its detail, not the stack.
    await reset(p);
    await p.evaluate(() => { toast("land dead has stopped", "", "stack", null, "land-dead"); });
    await clickToast(p, "land dead has stopped");
    at = await settle(p, a => !!a.detail);
    if (at.detail !== "land dead" || at.opened) {
      fail("an alert for a card with no terminal landed on " + JSON.stringify(at) + ", not its detail.");
    }

    // 5. A request on a card with no terminal: the request, flashed.
    await reset(p);
    mock.landPerms = [{ id: "perm-land", task_id: "land-pc", agent: "land pc", tool: "Bash", command: "ls",
      requested_at: new Date().toISOString().replace("Z", "") }];
    await p.evaluate(() => runRefresh());
    await until(() => p.evaluate(() => permsLocal.some(q => q.id === "perm-land")), 10000, "the request never reached the pass", 10);
    await p.evaluate(() => document.querySelectorAll("#toasts .toast").forEach(t => t.remove()));
    await p.evaluate(() => { toast("land pc needs permission", "Bash: ls", "perms", "perm-land", "land-pc"); });
    await clickToast(p, "land pc needs permission");
    const flash = () => p.evaluate(() => {
      const el = document.querySelector('#perms-list .perm[data-id="perm-land"]');
      return !!el && el.classList.contains("flash");
    });
    await until(async () => (await landedAt(p)).view === "perms" && await flash(), 5000, "the request was never flashed", 10).catch(() => {});
    at = await landedAt(p);
    const flashed = await flash();
    if (at.view !== "perms" || !flashed || at.opened) {
      fail("a request on a card with no terminal landed on " + JSON.stringify(at) +
        (flashed ? "" : ", with the request not flashed") + ".");
    }
    mock.landPerms = [];
    await p.evaluate(() => runRefresh());

    // 6. A toast about nothing: nowhere, and the dialog under it stays.
    await reset(p);
    await p.evaluate(() => { toast("land nowhere", "copied"); document.getElementById("toastlog").showModal(); });
    await clickToast(p, "land nowhere");
    await frames(p, 3);
    at = await landedAt(p);
    if (at.view !== "stack" || !at.dialogs.includes("toastlog")) {
      fail("a toast about nothing took the board to " + JSON.stringify(at) + ".");
    }

    // 7. A popped-out window's alert about a card that is not its own: the
    // board lands it. And one about its own card leaves the board alone.
    await reset(p);
    const solo = await ctx.newPage();
    solo.on("pageerror", e => errors.push(String(e)));
    await solo.goto(base + "/#term=s1", { waitUntil: "domcontentloaded" });
    await solo.waitForFunction(() => typeof soloID !== "undefined" && soloID === "s1", null, { timeout: slow(10000) });
    await solo.evaluate(() => { toast("land live is ready", "", "stack", null, "land-live"); });
    await clickToast(solo, "land live is ready");
    at = await settle(p, a => a.opened === "land-live");
    if (at.opened !== "land-live") {
      fail("a popped-out window's alert for another card did not land it on the board: " + JSON.stringify(at));
    }
    await reset(p);
    await solo.evaluate(() => { toast("solo card is ready", "", "stack", null, "s1"); });
    await clickToast(solo, "solo card is ready");
    await frames(p, 3);
    at = await landedAt(p);
    if (at.opened) fail("a popped-out window's alert for its own card attached the board to " + at.opened);
    await solo.close();

    // 8. A board opened by a desktop notification with none open.
    await p.goto(base + "/?land=land-live&view=stack", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof openTerm === "function", null, { timeout: slow(10000) });
    await spyAttach(p);
    at = await settle(p, a => a.view === "terms");
    const left = await p.evaluate(() => location.search);
    const term = await p.evaluate(() => termTask && termTask.id);
    if (at.view !== "terms" || term !== "land-live") {
      fail("a board opened at ?land=land-live landed on " + JSON.stringify(at) + " holding " + term + ".");
    }
    if (left) fail("the landing query stayed on the address: " + left);
  } finally {
    await ctx.close();
  }

  // 9. A plain desktop notification, raised with the board unfocused and
  // clicked: the terminal, and a line in the toast log that lands there too.
  ctx = await landContext(browser, true);
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await p.evaluate(() => runRefresh());
    await spyAttach(p);
    landCard("land-new3", { supervised: false });
    mock.landList = mock.landList.concat(mock.LAND["land-new3"]);
    poke();
    await p.waitForFunction(() => window.__notes.some(n => n.title === "land new3 is on the board"), null,
      { timeout: slow(10000) }).catch(() => fail("no desktop notification for a new card with the board unfocused."));
    await p.evaluate(() => window.__notes.find(n => n.title === "land new3 is on the board").onclick());
    setTimeout(() => { mock.LAND["land-new3"].supervised = true; }, 1000);
    const at = await settle(p, a => a.opened === "land-new3");
    if (at.opened !== "land-new3" || at.view !== "terms") {
      fail("a desktop notification for a new card, clicked, landed on " + JSON.stringify(at) + ".");
    }
    const logged = await p.evaluate(() => toastLog().find(t => t.title === "land new3 is on the board"));
    if (!logged || logged.taskFor !== "land-new3") {
      fail("the desktop notification's log line does not carry its card: " + JSON.stringify(logged));
    }
  } finally {
    await ctx.close();
  }
  if (errors.length) fail("the landing pages threw: " + errors.join(" | "));
  mock.landList = []; mock.landPerms = [];
  mock.tasksMode = was;
}

// The land-the-plane line: a per-browser pref (atrium.landThePlaneK, js/peek.js), no badge, the danger line, the amber
// zone below it, the gear field, the popover meter and a phone-width row.
async function landThePlaneSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const wp = await ctx.newPage();
  const errors = [];
  wp.on("pageerror", e => errors.push(String(e)));
  await wp.addInitScript(() => {
    let all = {};
    try { all = JSON.parse(localStorage.getItem("atrium.skipconfirm") || "{}"); } catch (e) {}
    all["width-floor"] = true;
    localStorage.setItem("atrium.skipconfirm", JSON.stringify(all));
  });
  const was = mock.tasksMode;
  const row = (id, tokens) => Object.assign({}, T1, {
    id, display_title: "row " + id, supervised: true, pinned: true, worktree: "/tmp/cl/" + id,
    context_size: { tokens, warn: tokens >= 150000, threshold_k: 150 }
  });
  try {
    mock.wornTasks = [row("lp-100", 100000), row("lp-170", 170000), row("lp-201", 201000)];
    mock.tasksMode = "worn";
    await wp.goto(base, { waitUntil: "domcontentloaded" });
    await wp.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await wp.click('.tab[data-view="terms"]');
    const read = () => wp.evaluate(async () => {
      await renderTermList();
      const o = {};
      for (const el of document.querySelectorAll('#term-list .card.tab[data-id^="lp-"]')) {
        const b = el.querySelector(".peek-bar.ctxline");
        o[el.dataset.id] = { cls: b ? b.className : null, badge: (el.querySelector(".chip.ctxland") || {}).textContent || "",
          mark: !!el.querySelector(".chip.ctxwarn:not(.ctxland)"), tip: b ? b.getAttribute("data-tip") : null,
          border: getComputedStyle(el).borderTopColor };
      }
      return o;
    });
    let r = await read();
    // Default 200k: only 201k is past it. 170k is amber with the small mark, 100k is neutral.
    if (!/ hot over/.test(r["lp-201"].cls) || r["lp-201"].badge) fail("201k with the default line is not a danger line without a badge: " + JSON.stringify(r["lp-201"]));
    if (!/ hot$/.test(r["lp-170"].cls) || r["lp-170"].badge || !r["lp-170"].mark) fail("170k (85% of the line) is not danger with the small mark: " + JSON.stringify(r["lp-170"]));
    if (/ (warm|hot)/.test(r["lp-100"].cls) || r["lp-100"].badge) fail("100k is not neutral: " + JSON.stringify(r["lp-100"]));
    if (r["lp-201"].border !== r["lp-100"].border) fail("a card past the line has a different border from a plain one.");
    // The pref moves the line. 160k: 170k and 201k are past it, 100k still not.
    await wp.evaluate(() => localStorage.setItem("atrium.landThePlaneK", "160"));
    r = await read();
    if (r["lp-170"].badge || !/ hot over/.test(r["lp-170"].cls)) fail("a 160k line does not mark 170k: " + JSON.stringify(r["lp-170"]));
    if (!/^170k of 160k \(land the plane\)/.test(r["lp-170"].tip)) fail("the tooltip does not follow the pref: " + r["lp-170"].tip);
    if (r["lp-100"].badge) fail("a 160k line marks 100k.");
    // Junk reads as 200. A line under the warn line is held at the warn line (150k), so 160k is past it, 100k is not.
    for (const junk of ["abc", "-5", "0", "5", "9", "99999", "1e3", ""]) {
      await wp.evaluate(v => localStorage.setItem("atrium.landThePlaneK", v), junk);
      const got = await wp.evaluate(() => [landThePlaneRawK(), landThePlaneK({ context_size: { threshold_k: 150 } })]);
      if (got[0] !== 200 || got[1] !== 200) fail("junk " + JSON.stringify(junk) + " reads as " + got);
    }
    await wp.evaluate(() => localStorage.setItem("atrium.landThePlaneK", "50"));
    const clamp = await wp.evaluate(() => landThePlaneK({ context_size: { threshold_k: 150 } }));
    if (clamp !== 150) fail("a line typed under the warn line is " + clamp + "k, want 150k.");
    // A broken localStorage never throws.
    const safe = await wp.evaluate(() => {
      const g = Storage.prototype.getItem, st = Storage.prototype.setItem;
      Storage.prototype.getItem = () => { throw new Error("no"); };
      Storage.prototype.setItem = () => { throw new Error("no"); };
      try { setLandThePlaneK("180"); return landThePlaneRawK(); } catch (e) { return "threw " + e; }
      finally { Storage.prototype.getItem = g; Storage.prototype.setItem = st; }
    });
    if (safe !== 200) fail("a broken localStorage: " + safe);
    // The gear field: refuses junk, saves a number, empties to the default, and moves the rows.
    await wp.evaluate(() => localStorage.removeItem("atrium.landThePlaneK"));
    const gear = await wp.evaluate(async () => {
      await renderTermList();
      fillLandK();
      const box = document.getElementById("s-landk");
      if (!box) return { missing: true };
      const out = { placeholder: box.placeholder, hint: (document.querySelector("#s-landk-field .hintline") || {}).textContent || "" };
      box.value = "5"; saveLandK(); out.junk = localStorage.getItem("atrium.landThePlaneK");
      const before = document.querySelector('#term-list [data-id="lp-170"] .ctxline.over') ? "badge" : "none";
      box.value = "160"; saveLandK(); out.saved = localStorage.getItem("atrium.landThePlaneK");
      const over = () => document.querySelector('#term-list [data-id="lp-170"] .ctxline.over');
      for (const end = Date.now() + 5000; !over() && Date.now() < end;) await new Promise(r => requestAnimationFrame(r));
      out.rowBefore = before;
      out.rowAfter = document.querySelector('#term-list [data-id="lp-170"] .ctxline.over') ? "badge" : "none";
      box.value = ""; saveLandK(); out.cleared = localStorage.getItem("atrium.landThePlaneK");
      return out;
    });
    if (gear.missing) fail("no land-the-plane field in the gear.");
    else {
      if (gear.placeholder !== "200") fail("the gear field's default is " + gear.placeholder);
      if (!/this browser/i.test(gear.hint) || !/LAND THE PLANE/.test(gear.hint)) fail("the gear hint does not say it is this browser's and matches the status line: " + gear.hint);
      if (gear.junk !== null) fail("the gear saved a junk value: " + gear.junk);
      if (gear.saved !== "160") fail("the gear did not save 160: " + gear.saved);
      if (gear.rowBefore !== "none" || gear.rowAfter !== "badge") fail("a saved line did not redraw the rows: " + gear.rowBefore + " -> " + gear.rowAfter);
      if (gear.cleared !== null) fail("an emptied gear field did not return to the default: " + gear.cleared);
    }
    // The popover, over the line: the same drawing, red, with the tooltip.
    const pop = await wp.evaluate(() => {
      const d = document.createElement("div");
      d.innerHTML = peekBody({ id: "x", resume_id: "r", context_size: { tokens: 201000, warn: true, threshold_k: 150 },
        telemetry: { window: 1000000 } }, { context_now: 201000, totals: {} });
      document.body.appendChild(d);
      const c = d.querySelector(".peek-ctx"), i = d.querySelector(".peek-bar i");
      const o = { cls: c.className, tip: d.querySelector(".peek-bar").getAttribute("data-tip"), scale: d.querySelector(".peek-scale").textContent,
        h: d.querySelector(".peek-bar").getBoundingClientRect().height, bg: getComputedStyle(i).backgroundImage };
      d.remove();
      return o;
    });
    if (!/peek-ctx warn hot/.test(pop.cls)) fail("the popover is not hot past the line: " + pop.cls);
    if (pop.tip !== "201k of 200k (land the plane), window 1M") fail("the popover tooltip: " + pop.tip);
    if (!/lands 200k/.test(pop.scale)) fail("the popover scale does not name the line: " + pop.scale);
    if (pop.h < 8) fail("the popover bar is a thin line: " + pop.h + "px.");
    if (!/rgb\(255, 107, 107\)|rgb\(\d+, \d+, \d+\)/.test(pop.bg)) fail("the popover fill has no colour: " + pop.bg);
    // Phone width: no badge, the strip is inside the row, and the row does not scroll sideways.
    await wp.setViewportSize({ width: 340, height: 800 });
    const ph = await wp.evaluate(async () => {
      await renderTermList();
      const el = document.querySelector('#term-list [data-id="lp-201"]');
      if (!el) return { none: true };
      const b = el.querySelector(".peek-bar.ctxline"), rr = el.getBoundingClientRect();
      const br = b && b.getBoundingClientRect();
      return { has: !!b, badge: !!el.querySelector(".chip.ctxland"),
        inside: !!br && br.left >= rr.left - 1 && br.right <= rr.right + 1 && br.width > 0,
        page: document.documentElement.scrollWidth <= window.innerWidth + 1 };
    });
    if (ph.none || !ph.has) fail("no context line on a phone-width row: " + JSON.stringify(ph));
    else {
      if (ph.badge) fail("a LAND badge on a phone-width row.");
      if (!ph.inside) fail("the context line is clipped at 340px: " + JSON.stringify(ph));
      if (!ph.page) fail("the page scrolls sideways at 340px.");
    }
  } finally {
    await ctx.close();
    mock.tasksMode = was;
  }
  if (errors.length) fail("the land-the-plane section threw: " + errors.join(" | "));
}

const units = [
  { name: "land", run: ({ browser, base }) => landSection(browser, base) },
  { name: "landThePlane", run: ({ browser, base }) => landThePlaneSection(browser, base) },
];
const sections = { land: landSection, landThePlane: landThePlaneSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
