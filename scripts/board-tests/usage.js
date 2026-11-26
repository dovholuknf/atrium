// usage: usage: the tab, burn charts, context, turn limits, cache.
//
// Units: usageCacheReads, usageTurns, usagePolish, usageLimits, usageGroups, contextSize, ctxLine, ctxLimitLayers, contextCycle, cacheChip, clock, usageTab, usageWeekReset, usageNever.
// `units` is what the harness runs, in suite order. `sections` is for HEADLESS_ONLY, which names a section and not a unit.
// Alone: `node scripts/board-tests/usage.js` runs just these units, the way the full run would.
const fs = require("fs");
const path = require("path");
const { slow, fakeWait, fakeTick, fakePause, frames, until, settleStyle, RUN_T0, CLOCK_OFFSET } = require("../lib/browser.js");
const { landCard, mock, T1, SKINS } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");
const { CTX_CARDS, CTX_USAGE, docImgDir } = require("../lib/helpers.js");
const SCRIPTS = path.join(__dirname, "..");

// The stream opening reloads the tab once; the onopen handler starts that read synchronously, so live and idle is settled.
const usageSettled = p => p.waitForFunction(() => document.getElementById("conn").classList.contains("live") && !UC.inflight,
  null, { timeout: slow(10000) });

// ── u-032: the cache chip and the cache line ───────────────────────────────────────────────────────────
// The table of states as `kaModel` answers it at a fixed clock, then the same chips drawn on the stack, the
// terminals list, the board and the attached terminal's header, then a card whose cache runs out flipping
// with no request, then a phone. The line counts with the chip's own bucket.
function kaFix(id, ka, over) {
  return landCard(id, Object.assign({ supervised: true, created_at: "2026-09-19T12:00:00Z",
    keepalive: Object.assign({ state: "on", refreshes: 0, state_at: "2026-09-29T00:00:00Z" }, ka) }, over || {}));
}

async function cacheChipSection(browser, base) {
  const was = mock.tasksMode;
  mock.tasksMode = "land";
  const errors = [];
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const shots = process.env.CACHE_SHOTS || "";
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    kaFix("cc-seed", { why: "not due", warm_until: new Date(Date.now() + 3600000).toISOString() });
    mock.landList = [mock.LAND["cc-seed"]];
    mock.landPerms = [];
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof kaModel === "function", null, { timeout: slow(15000) });

    // 1. The table, at a fixed clock (local 03:00 on 2026-09-30), so every time is HH:MM.
    const rows = await p.evaluate(() => {
      const at = (d, h, m) => new Date(2026, 8, d, h, m, 0).getTime();
      const iso = ms => new Date(ms).toISOString();
      const now = at(30, 3, 0);
      const card = ka => ({ id: "x", keepalive: Object.assign({ state: "on", refreshes: 0 }, ka) });
      const out = [];
      const add = (name, ka, n) => {
        const m = kaModel(card(ka), n || now);
        out.push({ name, full: m && m.full, short: m && m.short, tip: m && m.tip.join(". "), bucket: m && m.bucket });
      };
      const w = iso(at(30, 3, 32));
      add("break-even", { state: "stopped:break-even", warm_until: w });
      add("miss", { state: "stopped:miss", missed: 1 });
      add("failing", { state: "stopped:failing" });
      add("acted", { state: "stopped:acted" });
      add("off warm", { state: "off", warm_until: w });
      add("off cold", { state: "off", warm_until: iso(at(30, 2, 0)) });
      add("off unknown", { state: "off" });
      add("kept next", { refreshes: 3, why: "not due", warm_until: w });
      add("kept next exact", { refreshes: 3, why: "not due", warm_until: w, next_refresh_at: iso(at(30, 3, 26)) });
      add("kept busy", { refreshes: 3, why: "not idle", warm_until: w });
      add("warm", { why: "not due", warm_until: w });
      add("warm nowhy", { warm_until: w });
      for (const why of ["not idle", "context under 50k", "not on the 1h cache", "local hooks",
        "a permission dialog is open", "budget spent", "parked"]) add("wont " + why, { why, warm_until: w });
      add("cold", { why: "parked", warm_until: iso(at(30, 2, 4)) });
      add("cold yesterday", { warm_until: iso(at(29, 20, 4)) });
      add("no cache", { why: "no session id yet" });
      add("tomorrow", { why: "not due", warm_until: iso(at(31, 0, 10)), refreshes: 0 }, at(30, 23, 50));
      add("far date", { warm_until: iso(at(30, 3, 32)) }, at(20, 3, 0));
      out.push({ name: "not claude", full: kaModel({ id: "y" }, now) });
      return out;
    });
    const by = Object.fromEntries(rows.map(r => [r.name, r]));
    const want = (n, full, short) => {
      const r = by[n];
      if (!r || r.full !== full) fail("cacheChip table " + n + ": wanted " + JSON.stringify(full) + " got " + JSON.stringify(r && r.full));
      if (short !== undefined && r.short !== short) fail("cacheChip table " + n + ": short wanted " + JSON.stringify(short) + " got " + JSON.stringify(r.short));
    };
    want("break-even", "\u2298 stopped \u00b7 not worth it", "\u2298 not worth it");
    want("miss", "\u2298 stopped \u00b7 cache missed");
    want("failing", "\u2298 stopped \u00b7 refresh failing");
    want("acted", "\u2298 stopped \u00b7 refresh used a tool");
    if (!/stopped:acted/.test(by.acted.tip) || !/stopped:break-even/.test(by["break-even"].tip)) fail("cacheChip: a stop's raw state is not in its tooltip");
    want("off warm", "\u25cb off \u00b7 warm \u2192 03:32", "\u25cb off");
    want("off cold", "\u25cb off \u00b7 cold");
    want("off unknown", "\u25cb off \u00b7 cold");
    want("kept next", "\u2744 kept warm 3\u00d7 \u00b7 next ~03:27", "\u2744 3\u00d7 next ~03:27");
    want("kept next exact", "\u2744 kept warm 3\u00d7 \u00b7 next 03:26");
    want("kept busy", "\u2744 kept warm 3\u00d7 \u2192 03:32 \u00b7 won't refresh: busy");
    if (/next/.test(by["kept busy"].full)) fail("cacheChip: a busy card promises a next refresh");
    want("warm", "\u2744 warm \u2192 03:32", "\u2744 \u2192 03:32");
    want("warm nowhy", "\u2744 warm \u2192 03:32");
    const words = { "not idle": "busy", "context under 50k": "small", "not on the 1h cache": "5m cache", "local hooks": "local hooks",
      "a permission dialog is open": "dialog open", "budget spent": "budget spent", "parked": "parked" };
    for (const [why, word] of Object.entries(words)) {
      want("wont " + why, "\u2744 warm \u2192 03:32 \u00b7 won't refresh: " + word);
      if (!by["wont " + why].tip.includes(why)) fail("cacheChip: the raw why " + why + " is not in the tooltip");
    }
    want("cold", "\u2744 cold since 02:04", "\u2744 cold");
    want("cold yesterday", "\u2744 cold since yesterday 20:04");
    want("no cache", "\u2744 no cache yet");
    want("tomorrow", "\u2744 warm \u2192 tomorrow 00:10", "\u2744 \u2192 00:10");
    if (!/^\u2744 warm \u2192 [A-Z][a-z]{2} \d+ 03:32$/.test(by["far date"].full)) fail("cacheChip: a far time is not a date: " + by["far date"].full);
    if (by["not claude"].full !== null) fail("cacheChip: a card with no keepalive has a model");
    for (const [n, b] of [["warm", "warm"], ["kept next", "kept"], ["cold", "cold"], ["off warm", "warm"], ["no cache", "cold"]]) {
      if (by[n].bucket !== b) fail("cacheChip: " + n + " is in bucket " + by[n].bucket + ", wanted " + b);
    }

    // 2. The drawn chips on every surface, with no hover.
    const soon = m => new Date(Date.now() + m * 60000).toISOString();
    kaFix("cc-kept", { refreshes: 3, why: "not due", warm_until: soon(30) });
    kaFix("cc-warm", { why: "not idle", warm_until: soon(20) });
    kaFix("cc-cold", { why: "parked", warm_until: soon(-30) });
    kaFix("cc-stop", { state: "stopped:break-even", refreshes: 5, warm_until: soon(-10) });
    kaFix("cc-off", { state: "off", warm_until: soon(-1) });
    kaFix("cc-none", { why: "no session id yet" });
    landCard("cc-shell", { supervised: true, created_at: "2026-09-19T12:00:00Z", runner: "shell" });
    const ids = ["cc-kept", "cc-warm", "cc-cold", "cc-stop", "cc-off", "cc-none", "cc-shell"];
    mock.landList = ids.map(i => mock.LAND[i]);
    const reqs = [];
    p.on("request", r => reqs.push(r.url()));
    await p.reload({ waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await p.evaluate(() => setStackShow(new Set()));
    const texts = sel => p.evaluate(s => {
      const o = {};
      document.querySelectorAll(s).forEach(row => {
        o[row.dataset.id] = [...row.querySelectorAll(".chip.cache")].filter(e => e.getBoundingClientRect().width > 0)
          .map(e => e.querySelector(".cfull").textContent.trim());
      });
      return o;
    }, sel);
    const expect = {
      // these fixtures are on the real clock, so near midnight a time carries its day
      "cc-kept": /^\u2744 kept warm 3\u00d7 \u00b7 next ~(?:tomorrow )?\d\d:\d\d$/,
      "cc-warm": /^\u2744 warm \u2192 (?:tomorrow )?\d\d:\d\d \u00b7 won't refresh: busy$/,
      "cc-cold": /^\u2744 cold since (?:yesterday )?\d\d:\d\d$/, "cc-stop": /^\u2298 stopped \u00b7 not worth it$/,
      "cc-off": /^\u25cb off \u00b7 cold$/, "cc-none": /^\u2744 no cache yet$/
    };
    const check = (where, o, hidden = []) => {
      for (const [id, re] of Object.entries(expect)) {
        if (hidden.includes(id)) {
          if (o[id] && o[id].length) fail("cacheChip " + where + ": " + id + " drew a chip the row hides " + JSON.stringify(o[id]));
          continue;
        }
        if (!o[id] || o[id].length !== 1 || !re.test(o[id][0])) fail("cacheChip " + where + ": " + id + " shows " + JSON.stringify(o[id]));
      }
      if (o["cc-shell"] && o["cc-shell"].length) fail("cacheChip " + where + ": a non-Claude card drew a chip " + JSON.stringify(o["cc-shell"]));
    };
    await p.waitForFunction(() => document.querySelectorAll("#stack-list .stackrow .chip.cache").length >= 6, null, { timeout: slow(10000) });
    check("stack", await texts("#stack-list .stackrow"));
    // A dot, never words: the operator saw the chip squeeze a title to one letter. The words are in the tooltip.
    const dot = await p.evaluate(() => [...document.querySelectorAll("#stack-list .chip.cache")].map(e => {
      const r = e.getBoundingClientRect(); return { w: r.width, h: r.height, text: e.innerText.trim() };
    }).filter(d => d.w > 16 || d.h > 16 || d.text));
    if (dot.length) fail("cacheChip: chips are not dots " + JSON.stringify(dot));
    const tip = await p.evaluate(() => document.querySelector('#stack-list .chip.cache[data-cid="cc-warm"]').dataset.tip);
    if (!/not idle/.test(tip) || !/busy/.test(tip)) fail("cacheChip: the tooltip lacks the raw why: " + tip);
    if (shots) { fs.mkdirSync(shots, { recursive: true }); await p.screenshot({ path: path.join(shots, "u-032-stack-1400x900.png") }); }

    await p.evaluate(() => switchView("terms"));
    await p.waitForSelector(".term-list .card", { timeout: slow(10000) });
    await p.waitForFunction(() => document.querySelectorAll(".term-list .card .chip.cache").length >= 1, null, { timeout: slow(10000) });
    check("terminals list", await texts(".term-list .card"), ["cc-warm", "cc-cold", "cc-stop", "cc-off", "cc-none"]);
    if (shots) await p.screenshot({ path: path.join(shots, "u-032-terms-1400x900.png") });

    // The header: attach a card and its chip is in the bar. A fake socket, as the phone sections use.
    await p.evaluate(() => {
      const Real = window.WebSocket;
      window.WebSocket = function (url, protocols) {
        if (!/\/attach(\?|$)/.test(url)) return new Real(url, protocols);
        const s = { url, readyState: 0, binaryType: "arraybuffer", bufferedAmount: 0, send() {}, close() { this.readyState = 3; } };
        setTimeout(() => { s.readyState = 1; if (s.onopen) s.onopen({}); }, 0);
        return s;
      };
      Object.assign(window.WebSocket, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });
      attachTask("cc-kept");
    });
    await p.waitForFunction(() => termTask && termTask.id === "cc-kept" && document.querySelector("#t-chips .chip.cache"), null, { timeout: slow(10000) });
    const head = await p.evaluate(() => document.querySelector("#t-chips .chip.cache .cfull").textContent.trim());
    if (!expect["cc-kept"].test(head)) fail("cacheChip header: " + JSON.stringify(head));
    if (await p.evaluate(() => document.querySelectorAll("#t-chips .chip.cache").length) !== 1) fail("cacheChip header: more than one chip");

    await p.evaluate(() => switchView("board"));
    await p.waitForFunction(() => document.querySelectorAll("#board .chip.cache").length >= 6, null, { timeout: slow(10000) });
    const bo = await p.evaluate(() => {
      const o = {};
      document.querySelectorAll("#board .card").forEach(c => { o[c.dataset.id] = [...c.querySelectorAll(".chip.cache")].map(e => e.querySelector(".cfull").textContent.trim()); });
      return o;
    });
    check("board", bo);

    // 3. A card 2 seconds from cold flips with no request, from one armed timer. The card arrives warm for an hour, through
    // the real paint, and its time is brought to two seconds from now in the page once that paint and its fetch are done,
    // since a busy machine can spend the two seconds on the way here.
    kaFix("cc-flip", { why: "not idle", warm_until: new Date(Date.now() + 3600000).toISOString() });
    mock.landList = [mock.LAND["cc-flip"]];
    await p.evaluate(() => switchView("stack"));
    const fetched = p.waitForResponse(r => /\/v1\/tasks(\?|$)/.test(r.url()), { timeout: slow(10000) });
    await p.evaluate(() => tasksSoon());
    await fetched;
    await p.waitForFunction(() => { const e = document.querySelector('#stack-list [data-cid="cc-flip"] .cfull'); return e && /^\u2744 warm/.test(e.textContent); }, null, { timeout: slow(10000) });
    // No read left to come: a late one would bring the hour back over the time set below.
    await p.waitForFunction(() => !tasksTimer && !refreshTimer && !refreshInFlight && !refreshDirty && !want.tasks, null, { timeout: slow(15000) });
    await p.waitForFunction(() => kaTimer > 0, null, { timeout: slow(5000) });
    const n0 = reqs.length;
    const first = await p.evaluate(() => {
      KA_SEEN.get("cc-flip").keepalive.warm_until = new Date(Date.now() + 2500).toISOString();
      kaRepaint();
      const now = document.querySelector('#stack-list [data-cid="cc-flip"] .cfull').textContent;
      kaArm();
      return now;
    });
    if (!/^\u2744 warm/.test(first)) fail("cacheChip flip: starts as " + first);
    const armed = await p.evaluate(() => kaTimer > 0);
    if (!armed) fail("cacheChip flip: no timer is armed for the soonest card");
    await p.waitForFunction(() => /cold since/.test(document.querySelector('#stack-list [data-cid="cc-flip"] .cfull').textContent),
      null, { timeout: slow(8000) }).catch(() => fail("cacheChip flip: the chip did not flip to cold by itself"));
    if (reqs.length !== n0) fail("cacheChip flip: the flip made " + (reqs.length - n0) + " requests: " + reqs.slice(n0).join(" "));
    // A throttled tab is put right when it becomes visible again.
    // The cold card is no longer remembered, so the card is handed back as it would be by a list.
    const back = JSON.parse(JSON.stringify(mock.LAND["cc-flip"]));
    back.keepalive.warm_until = new Date(Date.now() + 90 * 60000).toISOString();
    await p.evaluate(t => {
      KA_SEEN.set(t.id, t);
      document.dispatchEvent(new Event("visibilitychange"));
    }, back);
    if (!/^\u2744 warm/.test(await p.evaluate(() => document.querySelector('#stack-list [data-cid="cc-flip"] .cfull').textContent))) {
      fail("cacheChip: visibilitychange did not repaint the chip");
    }
    await ctx.close();

    // 4. A phone: the short text, and nothing runs off the right edge.
    mock.landList = ids.map(i => mock.LAND[i]);
    const pctx = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });
    await pctx.addInitScript(() => localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true })));
    const q = await pctx.newPage();
    q.on("pageerror", e => errors.push(String(e)));
    await q.goto(base, { waitUntil: "domcontentloaded" });
    await q.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await q.evaluate(() => setStackShow(new Set()));
    for (const view of ["stack", "terms"]) {
      await q.evaluate(v => switchView(v), view);
      const sel = view === "terms" ? ".term-list .card" : "#stack-list .stackrow";
      await q.waitForFunction(([s, n]) => document.querySelectorAll(s + " .chip.cache").length >= n, [sel, view === "terms" ? 1 : 6], { timeout: slow(10000) });
      if (shots) { fs.mkdirSync(shots, { recursive: true }); await q.screenshot({ path: path.join(shots, "u-032-" + view + "-390x844.png") }); }
      const r = await q.evaluate(s => {
        const vw = document.documentElement.clientWidth, bad = [], shorts = [];
        document.querySelectorAll(s).forEach(c => {
          const cr = c.getBoundingClientRect();
          if (cr.right > vw + 0.5) bad.push("card");
          c.querySelectorAll(".chip.cache").forEach(ch => {
            const r = ch.getBoundingClientRect();
            if (r.width && (r.right > cr.right + 0.5 || r.right > vw + 0.5)) bad.push("chip " + ch.innerText);
            if (r.width) shorts.push(ch.innerText.trim());
            if (r.width > 16 || r.height > 16) bad.push("chip is " + r.width + "x" + r.height + ", not a dot");
            if (ch.getAttribute("aria-label").indexOf(ch.querySelector(".cfull").textContent) !== 0) bad.push("aria " + ch.innerText);
          });
        });
        return { bad, shorts, pg: document.documentElement.scrollWidth - innerWidth };
      }, sel);
      if (r.bad.length || r.pg > 0) fail("cacheChip phone " + view + ": " + JSON.stringify(r));
      if (r.shorts.some(t => t !== "")) {
        fail("cacheChip phone " + view + ": a chip shows words " + JSON.stringify(r.shorts));
      }
    }
    await pctx.close();

    // 5. The pie: its fraction at a fixed clock, and each state's look and tooltip, drawn for real.
    {
      const pctx2 = await browser.newContext({ viewport: { width: 1400, height: 900 } });
      const r = await pctx2.newPage();
      r.on("pageerror", e => errors.push(String(e)));
      const mk = (ka, o) => Object.assign({ state: "on", refreshes: 0 }, ka, o || {});
      const hr = ms => new Date(Date.now() + ms).toISOString();
      const min = 60000;
      // next is 5 minutes before warm_until, so a card refreshed `ago` minutes back has a 55 minute fill
      const filling = (ago) => mk({ refreshes: 2, why: "not due", last_refresh_at: hr(-ago * min),
        warm_until: hr((60 - ago) * min), next_refresh_at: hr((55 - ago) * min) });
      kaFix("pp-start", filling(1));
      kaFix("pp-half", filling(27.5));
      kaFix("pp-due", filling(53));
      kaFix("pp-stop", mk({ state: "stopped:break-even", refreshes: 5, warm_until: hr(-10 * min) }));
      kaFix("pp-cold", mk({ why: "parked", warm_until: hr(-30 * min) }));
      kaFix("pp-first", mk({ why: "not due", warm_until: hr(30 * min) }));
      const pids = ["pp-start", "pp-half", "pp-due", "pp-stop", "pp-cold", "pp-first"];
      mock.landList = pids.map(i => mock.LAND[i]);
      await r.goto(base, { waitUntil: "domcontentloaded" });
      await r.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
      await r.waitForFunction(() => typeof kaModel === "function", null, { timeout: slow(15000) });
      const fr = await r.evaluate(() => {
        const now = Date.now(), o = {};
        const at = ms => new Date(now + ms).toISOString();
        const mm = (ka) => kaModel({ id: "x", keepalive: Object.assign({ state: "on" }, ka) }, now);
        const cyc = (ago, extra) => mm(Object.assign({ refreshes: 1, why: "not due", last_refresh_at: at(-ago * 60000),
          warm_until: at((60 - ago) * 60000), next_refresh_at: at((55 - ago) * 60000) }, extra));
        o.empty = cyc(0).pie; o.half = cyc(27.5).pie; 
        o.past = cyc(58).pie; o.nearFull = cyc(54.9).pie; o.dueAt = cyc(52).due; o.notDue = cyc(40).due;
        o.noLast = mm({ why: "not due", warm_until: at(30 * 60000) }).pie;
        o.busy = mm({ why: "not idle", warm_until: at(30 * 60000) }).pie;
        o.cold = mm({ warm_until: at(-60000) }).pie;
        o.stop = mm({ state: "stopped:miss" }).pie;
        return o;
      });
      const wantPie = { empty: 0, half: 180, nearFull: 345, past: -1, dueAt: true, notDue: false, noLast: 195, busy: -1, cold: -1, stop: -1 };
      for (const [k, v] of Object.entries(wantPie)) if (fr[k] !== v) fail("cachePie fraction " + k + ": wanted " + v + " got " + fr[k]);
      await r.waitForFunction(() => document.querySelectorAll("#stack-list .chip.cache").length >= 6, null, { timeout: slow(10000) });
      const looks = await r.evaluate(() => {
        const o = {};
        document.querySelectorAll("#stack-list .chip.cache").forEach(e => {
          const cs = getComputedStyle(e);
          o[e.dataset.cid] = { cls: e.className, pie: e.style.getPropertyValue("--pie"), img: cs.backgroundImage, w: e.getBoundingClientRect().width,
            ring: cs.borderTopColor, tip: e.dataset.tip };
        });
        return o;
      });
      const g = id => looks[id] || fail("cachePie: no chip for " + id) || {};
      if (g("pp-start").pie !== "0deg" || !/conic-gradient/.test(g("pp-start").img)) fail("cachePie: a fresh refresh is not an empty pie " + JSON.stringify(g("pp-start")));
      if (g("pp-half").pie !== "180deg" || !/conic-gradient/.test(g("pp-half").img)) fail("cachePie: half way is not half a pie " + JSON.stringify(g("pp-half")));
      if (/ due/.test(g("pp-half").cls)) fail("cachePie: half way looks about to fire");
      if (!/ due/.test(g("pp-due").cls) || !/about to refresh/.test(g("pp-due").tip)) fail("cachePie: about to fire has no look or tooltip " + JSON.stringify(g("pp-due")));
      if (g("pp-due").ring === g("pp-half").ring) fail("cachePie: about to fire has the same ring as filling");
      if (!/next refresh at/.test(g("pp-half").tip)) fail("cachePie: the filling tooltip has no next time " + g("pp-half").tip);
      if (g("pp-stop").pie !== "" || g("pp-stop").img !== "none" || !/off cold/.test(g("pp-stop").cls) || !/stopped at break-even/.test(g("pp-stop").tip)) {
        fail("cachePie: a stopped cold card is not drawn as an off cold one with its reason " + JSON.stringify(g("pp-stop")));
      }
      if (/conic/.test(g("pp-cold").img) || g("pp-cold").pie !== "" || !/expired/.test(g("pp-cold").tip)) fail("cachePie: a cold card draws a pie " + JSON.stringify(g("pp-cold")));
      if (!/conic/.test(g("pp-first").img) || g("pp-first").pie !== "195deg") fail("cachePie: a card with no refresh yet fills from its cache write " + JSON.stringify(g("pp-first")));
      // The coarse timer: a filling pie arms a timer within a minute and the tick makes no request.
      const armed = await r.evaluate(() => kaTimer > 0);
      if (!armed) fail("cachePie: no timer is armed for a filling pie");
      const sh = process.env.CACHE_SHOTS || "";
      if (sh) {
        fs.mkdirSync(sh, { recursive: true });
        await r.evaluate(() => switchView("board"));
        await r.waitForFunction(() => document.querySelectorAll("#board .chip.cache").length >= 6, null, { timeout: slow(10000) });
        await r.screenshot({ path: path.join(sh, (process.env.CACHE_SHOT_PREFIX || "after") + "-board-1400x900.png") });
        await r.evaluate(() => switchView("stack"));
        await r.waitForSelector("#stack-list .stackrow", { timeout: slow(10000) });
        await r.screenshot({ path: path.join(sh, (process.env.CACHE_SHOT_PREFIX || "after") + "-stack-1400x900.png") });
      }
      await pctx2.close();
    }
  } finally {
    mock.tasksMode = was;
    try { await ctx.close(); } catch (e) {}
  }
  if (errors.length) fail("cacheChip: the page threw: " + errors.join(" | "));
}

// The per-runner layer of the context limit: a row says which layer its limit came from, a card with no limit of
// its own shows its runner's, and the runner field saves. See docs/backlog/ui/u-new-context-bar-on-rows.md.
async function ctxLimitLayersSection(browser, base) {
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
  const row = (id, source, k, extra) => Object.assign({}, T1, {
    id, display_title: "row " + id, supervised: true, pinned: true, worktree: "/tmp/cll/" + id,
    context_size: { tokens: 160000, warn: 160000 >= k * 1000, threshold_k: k, source }
  }, extra || {});
  try {
    mock.wornTasks = [row("ll-default", "default", 200), row("ll-hub", "hub", 300), row("ll-card", "card", 120)];
    mock.tasksMode = "worn";
    await wp.goto(base, { waitUntil: "domcontentloaded" });
    await wp.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await wp.click('.tab[data-view="terms"]');
    const got = await wp.evaluate(async () => {
      await loadCards().catch(() => {}).then(renderTermList);
      const out = {};
      for (const el of document.querySelectorAll('#term-list .card.tab[data-id^="ll-"]')) {
        const b = el.querySelector(".peek-bar.ctxline");
        out[el.dataset.id] = b ? b.getAttribute("data-tip") : null;
      }
      return out;
    });
    for (const [id, want] of [["ll-default", "limit 200k from default"], ["ll-hub", "limit 300k from hub"],
        ["ll-card", "limit 120k from card"]]) {
      if (!got[id] || !got[id].includes(want)) fail(id + " tooltip does not say '" + want + "': " + got[id]);
    }
    // The runner layer is gone: the runners page has no limit field, and a save sends none.
    await wp.evaluate(() => goRunners("runners"));
    await wp.waitForSelector('#harness-list .chip.toggle[data-id="hon"]', { timeout: slow(15000) });
    await wp.evaluate(() => editHarness("hon", ""));
    await wp.waitForSelector("#harness[open]", { timeout: slow(15000) });
    if (await wp.$("#h-ctxlimit")) fail("the runner editor still has a context limit field");
    const put = wp.waitForResponse(r => r.url().split("?")[0].endsWith("/v1/harnesses/hon") &&
      r.request().method() === "PUT", { timeout: slow(15000) });
    await wp.evaluate(() => saveHarness());
    const sent = JSON.parse((await put).request().postData() || "{}");
    if ("context_limit_k" in sent) fail("a runner save still sends context_limit_k: " + sent.context_limit_k);
    if (errors.length) fail("page errors: " + errors.join("; "));
  } finally {
    mock.tasksMode = was;
    await ctx.close();
  }
}

// THE CONTEXT CYCLE ON THE BOARD (docs/runtime/context-cycle-design.md): the details dialog's switch and limit write the
// card's overrides, the settings save one per-harness list through the hub, and the chip names the step.
async function contextCycleSection(browser, base) {
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
  const card = Object.assign({}, T1, {
    id: "ll-cycle", display_title: "cycling card", supervised: true, pinned: true, worktree: "/tmp/cll/cycle",
    context_size: { tokens: 210000, warn: true, threshold_k: 200, source: "hub", cycle: true },
    new_context: { step: "limit", n: 1, of: 3, label: "waiting for atrium ready (asked 1 times)", auto: true },
    overrides: {}
  });
  const patches = [], posts = [];
  await ctx.route("**/v1/tasks/ll-cycle", async route => {
    const r = route.request();
    if (r.method() !== "PATCH") return route.fallback();
    const body = JSON.parse(r.postData() || "{}");
    patches.push(body);
    card.overrides = Object.assign({}, card.overrides, body.overrides || {});
    await route.fulfill({ json: card });
  });
  await ctx.route("**/v1/settings", async route => {
    const r = route.request();
    if (r.method() === "POST") {
      const body = JSON.parse(r.postData() || "{}");
      posts.push(body);
      const limits = body.context_limits && Object.keys(body.context_limits).length ? body.context_limits : { claude: 200 };
      return route.fulfill({ json: { context_limits: limits } });
    }
    return route.fulfill({ json: { context_limits: { claude: 200 }, context_limits_default: { claude: 200 },
      context_limit_k_min: 50, context_limit_k_max: 2000, context_handoff_dir: "" } });
  });
  const was = mock.tasksMode;
  try {
    mock.wornTasks = [card];
    mock.tasksMode = "worn";
    await wp.goto(base, { waitUntil: "domcontentloaded" });
    await wp.waitForSelector('#stack-list .stackrow[data-id="ll-cycle"]', { state: "attached", timeout: slow(15000) });

    // The chip names the step in words.
    const chip = await wp.evaluate(() => newContextChip(lastTasks.find(t => t.id === "ll-cycle")));
    if (!/context 1\/3: waiting for ack/.test(chip)) fail("the cycle chip does not say 'context 1/3: waiting for ack': " + chip);
    const words = await wp.evaluate(() => ["clear", "wake"].map(step => newContextChip({ id: "x",
      new_context: { step, n: step === "clear" ? 2 : 3, of: 3, label: "" } })));
    if (!/2\/3: clearing/.test(words[0]) || !/3\/3: waking/.test(words[1])) fail("the clear and wake chips read " + words.join(" | "));

    // THE WAY OUT AND THE QUEUE. The card says how many messages wait and carries a "let me type" that
    // stops the cycle; the room refusing a key opens the compose cbox seeded with it; Enter queues it as a
    // message; "let me type" in the cbox dismisses the cycle and Escape discards.
    const wayOut = await wp.evaluate(() => newContextChip({ id: "x",
      new_context: { step: "limit", n: 1, of: 3, label: "", queued: 1 } }));
    if (!/1 message queued/.test(wayOut)) fail("the cycle chip has no 'N message queued': " + wayOut);
    const ncbar = await wp.evaluate(() => {
      termTask = { id: "ll-cycle" };
      composeBar({ new_context: { step: "limit", n: 1, of: 3, label: "waiting for atrium ready", queued: 1 } });
      const b = document.getElementById("t-ncbar");
      return { hidden: b.hidden, text: b.textContent.replace(/\s+/g, " ") };
    });
    if (ncbar.hidden || !/1 message queued/.test(ncbar.text) || /let me type/.test(ncbar.text))
      fail("the terminal bar is missing its count or still carries the button: " + JSON.stringify(ncbar));
    // THE DO-NOT-ENTER SIGN is up from the cycle's first step to its end, and gone for a failed cycle and a capture.
    const sign = step => wp.evaluate(step => {
      composeBar({ new_context: step === "none" ? null : { step, n: 1, of: 3, label: "waiting for atrium ready" } });
      const el = document.getElementById("t-ctxbox");
      return !!el && !el.hidden ? el.textContent.replace(/\s+/g, " ").trim() : "";
    }, step);
    const upAt = await sign("limit");
    if (!/step 1\/3: waiting for atrium ready/.test(upAt) || /typing is held/.test(upAt) || await wp.evaluate(() => document.querySelectorAll("#t-nodev").length)) fail("not one panel with the step on the cycle's first step: " + JSON.stringify(upAt));
    const unarmed = await wp.evaluate(() => {
      composeBar({ new_context: { step: "limit", n: 1, of: 3, label: "waiting for your line to clear", held: false } });
      const was = termTask, kind = termKind;
      termTask = { id: "ll-cycle", new_context: { step: "limit", held: false } };
      termKind = "runner";
      const gated = composeGate("x");
      termTask = was;
      termKind = kind;
      return { sign: !document.getElementById("t-ctxbox").hidden, gated };
    });
    if (unarmed.sign || unarmed.gated) fail("typing is held before the cycle found the line clear: " + JSON.stringify(unarmed));
    if (process.env.NODEV_SHOTS) {
      fs.mkdirSync(process.env.NODEV_SHOTS, { recursive: true });
      const name = process.env.NODEV_NAME || "nodev";
      for (const [w, h] of [[1400, 900], [390, 844]]) for (const step of ["limit", "clear", "wake"]) {
        await wp.setViewportSize({ width: w, height: h });
        await sign(step);
        await wp.evaluate(() => {
          for (let e = document.getElementById("t-ctxbox"); e && e !== document.body; e = e.parentElement) {
            e.hidden = e.id === "t-ctxbox" ? e.hidden : false;
            if (getComputedStyle(e).display === "none") e.style.display = "block";
          }
        });
        await wp.evaluate(() => document.getElementById("t-ctxbox").scrollIntoView({ block: "center" }));
        await wp.screenshot({ path: path.join(process.env.NODEV_SHOTS, name + "-" + step + "-" + w + ".png") });
      }
      process.exit(0);
    }
    const composed = [], freed = [];
    let freeWake = null;
    const nextFree = () => new Promise(r => { freeWake = r; });
    await wp.route("**/v1/tasks/ll-cycle/message", async route => {
      composed.push(JSON.parse(route.request().postData() || "{}"));
      await route.fulfill({ json: { delivered: "queued" } });
    });
    await wp.route("**/v1/tasks/ll-cycle/new-context", async route => {
      if (route.request().method() !== "DELETE") return route.fallback();
      freed.push(1);
      if (freeWake) { const w = freeWake; freeWake = null; w(); }
      await route.fulfill({ json: { card: "ll-cycle", cleared: true } });
    });
    const gate = await wp.evaluate(() => {
      const realSend = window.send, frames = [];
      window.send = m => { frames.push(m); };
      termTask = { id: "ll-cycle", new_context: { step: "wake", n: 3, of: 3, label: "waking it to read x" } };
      termKind = "runner";
      termSock = { readyState: WebSocket.OPEN, close() {} };
      window.gateFrames = frames; window.gateSend = realSend;
      sendInput("asdf");
      const held = { frames: frames.length };
      sendInput("[I", true);
      return { held, quiet: frames.length };
    });
    // TYPING IS NEVER BLOCKED: the key releases the hold (one DELETE) and goes through.
    await wp.waitForFunction(() => window.gateFrames.some(f => f.d === "asdf"), null, { timeout: slow(5000) });
    if (gate.held.frames || gate.quiet !== 1) fail("a quiet report was held or a key skipped the release: " + JSON.stringify(gate));
    if (freed.length !== 1) fail("typing into a held terminal did not release the hold: " + freed.length);
    await wp.evaluate(() => { window.send = window.gateSend; });
    freed.length = 0;
    if (await wp.evaluate(() => composeOpen())) fail("the panel stayed open after typing released the hold");
    for (const step of ["clear", "wake"]) if (!(await sign(step))) fail("no do-not-enter sign on the " + step + " step");
    for (const step of ["failed", "capture", "none"]) if (await sign(step)) fail("a do-not-enter sign on a " + step + " cycle");
    await sign("limit");
    await wp.evaluate(() => { termTask = { id: "ll-cycle" }; termKind = "runner"; document.getElementById("t-ctxbox-in").value = ""; composeNoteTyped("h"); composeRefused(); });
    const cbox = await wp.evaluate(() => ({ open: composeOpen(), text: document.getElementById("t-ctxbox-in").value }));
    if (!cbox.open || cbox.text !== "h") fail("a refused key did not open the compose cbox seeded with it: " + JSON.stringify(cbox));
    if (/Typing dropped/.test(await wp.evaluate(() => document.body.innerText))) fail("the Typing dropped toast is still shown");
    // COMPOSE_SHOTS=<dir> writes compose-before.png (the old toast) and compose-after.png (the box and the bar).
    if (process.env.COMPOSE_SHOTS) {
      await wp.evaluate(() => {
        for (let e = document.getElementById("t-ctxbox"); e && e !== document.body; e = e.parentElement) {
          e.hidden = false;
          if (getComputedStyle(e).display === "none") e.style.display = "block";
        }
        // The box is only ever up while atrium itself is typing, so that is the state the bar shows.
        composeBar({ new_context: { step: "limit", n: 1, of: 3, label: "atrium is typing into the terminal", typing: true, queued: 1 } });
        const ta = document.getElementById("t-ctxbox-in");
        ta.value = "also fix the failing test";
      });
      await wp.screenshot({ path: path.join(process.env.COMPOSE_SHOTS, "compose-after.png") });
      await wp.evaluate(() => {
        document.getElementById("t-ctxbox").hidden = true;
        toast("Typing dropped", "input is refused during a new context", null, "in-refused");
      });
      await settleStyle(wp);
      await wp.screenshot({ path: path.join(process.env.COMPOSE_SHOTS, "compose-before.png") });
      process.exit(0);
    }
    await wp.evaluate(() => { document.getElementById("t-ctxbox-in").value = "hello after the cycle"; });
    await wp.evaluate(() => document.getElementById("t-ctxbox-in").dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true })));
    await wp.waitForFunction(() => !composeOpen(), null, { timeout: slow(5000) });
    if (composed.length !== 1 || composed[0].text !== "hello after the cycle") fail("Enter did not queue the text: " + JSON.stringify(composed));
    await wp.evaluate(() => composeRefused());
    await wp.evaluate(() => { document.getElementById("t-ctxbox-in").value = "never mind"; });
    let woke = nextFree();
    await wp.evaluate(() => document.getElementById("t-ctxbox-in").dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true })));
    await woke;
    if (await wp.evaluate(() => composeOpen()) || composed.length !== 2 || composed[1].text !== "never mind" || freed.length !== 1) fail("Escape did not dismiss the panel and release the hold.");
    await wp.evaluate(() => composeRefused());
    woke = nextFree();
    await wp.evaluate(() => document.getElementById("t-ctxbox-free").click());
    await woke;
    await wp.waitForFunction(() => !composeOpen(), null, { timeout: slow(5000) });
    if (freed.length !== 2) fail("'let me type' did not dismiss the cycle.");
    await sign("limit");
    woke = nextFree();
    await wp.evaluate(() => document.getElementById("t-ctxbox-free").click());
    await woke;
    await wp.waitForFunction(() => document.getElementById("t-ctxbox").hidden, null, { timeout: slow(5000) });
    if (freed.length !== 3) fail("'let me type' on the panel did not dismiss the cycle.");
    // DISMISSABLE AT EVERY STEP: Escape and the x both release the hold, whatever step the cycle is on.
    for (const how of ["Escape", "x"]) for (const step of ["limit", "clear", "wake"]) {
      freed.length = 0;
      await sign(step);
      const woke = nextFree();
      if (how === "Escape") await wp.evaluate(() => document.getElementById("t-ctxbox-in").dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true })));
      else await wp.evaluate(() => document.querySelector("#t-ctxbox [aria-label^=dismiss]").click());
      await woke;
      await wp.waitForFunction(() => document.getElementById("t-ctxbox").hidden, null, { timeout: slow(5000) });
      if (freed.length !== 1) fail(how + " on the " + step + " step did not release the hold: " + freed.length);
    }
    freed.length = 0;
    // THE END FRAME TAKES THE STRIP AND THE BOX AWAY (r-new-context-strip-stuck-at-1of3). The bar followed only the list
    // pass, so a window that never ran it kept "1/3" and "let me type" after the cycle ended.
    const endFrame = await wp.evaluate(() => {
      termTask = { id: "ll-cycle" }; termKind = "runner";
      composeFollow({ id: "ll-cycle", row: 1, new_context: { step: "clear", n: 2, of: 3, label: "clearing the context" } });
      const mid = { hidden: document.getElementById("t-ncbar").hidden, text: document.getElementById("t-ncbar-say").textContent };
      composeRefused();
      document.getElementById("t-ctxbox-in").value = "typed during the cycle";
      const realSend = window.sendInput, typed = [];
      window.sendInput = text => { typed.push(text); };
      composeFollow({ id: "ll-cycle", row: 1 });
      window.sendInput = realSend;
      return { mid, hidden: document.getElementById("t-ncbar").hidden, box: composeOpen(), typed };
    });
    if (composed.length !== 2 || JSON.stringify(endFrame.typed) !== JSON.stringify(["typed during the cycle"]))
      fail("the end frame did not put the box text on the input line: " + JSON.stringify({ composed, typed: endFrame.typed }));
    if (endFrame.mid.hidden || !/2\/3: clearing the context/.test(endFrame.mid.text))
      fail("a mid-cycle frame did not move the bar to step 2: " + JSON.stringify(endFrame.mid));
    if (!endFrame.hidden || endFrame.box) fail("the end frame left the strip or 'let me type' on screen: " + JSON.stringify(endFrame));
    // WEDGED: the card still says the cycle is on its limit step (the mock never ends it), and the bar's own
    // "let me type" must still cancel it and put the bar away. No cycle step has to finish first.
    await wp.evaluate(() => {
      composeBar({ new_context: { step: "limit", n: 1, of: 3, label: "waiting for atrium ready" } });
      document.getElementById("t-ctxbox-free").click();
    });
    await wp.waitForFunction(() => document.getElementById("t-ncbar").hidden, null, { timeout: slow(5000) });
    if (freed.length !== 1) fail("the modal's 'let me type' did not cancel a cycle that never ends: " + freed.length);
    // IMAGE PASTE: a file pasted into the panel goes up the way a terminal paste does and its path joins the text.
    await wp.route("**/v1/tasks/ll-cycle/files", async route => {
      await route.fulfill({ json: { paths: ["/w/.atrium/incoming/pasted.png"] } });
    });
    await sign("limit");
    await wp.evaluate(() => {
      const ta = document.getElementById("t-ctxbox-in");
      ta.value = "look: ";
      ta.setSelectionRange(6, 6);
      const dt = new DataTransfer();
      dt.items.add(new File([new Uint8Array([1, 2, 3])], "a.png", { type: "image/png" }));
      ta.dispatchEvent(new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true }));
    });
    await wp.waitForFunction(() => document.getElementById("t-ctxbox-in").value.includes(".atrium/incoming/pasted.png"), null, { timeout: slow(5000) });
    await wp.evaluate(() => document.getElementById("t-ctxbox-in").dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true })));
    await wp.waitForFunction(() => !composeOpen(), null, { timeout: slow(5000) });
    if (composed.length !== 3 || !/^look: .*\.atrium\/incoming\/pasted\.png$/.test(composed[2].text))
      fail("the pasted image path was not queued with the text: " + JSON.stringify(composed));
    await wp.evaluate(() => { termTask = null; });

    await wp.evaluate(() => openTask("ll-cycle"));
    await wp.waitForSelector("#detail[open]", { timeout: slow(15000) });

    // A limit its runner's compaction point cut says so, and the card's own limit has a clear button.
    const capped = await wp.evaluate(() => {
      const t = { overrides: { context_limit_k: "900" }, context_size: { tokens: 1, threshold_k: 200, source: "runner",
        own_k: 900, ceiling_k: 200, wanted_k: 900, wanted_from: "card" } };
      return { from: limitFrom(t), own: typeof ownLimitNote === "function" ? ownLimitNote(t) : "" };
    });
    if (!/200k, capped by its runner from 900k \(card\)/.test(capped.from) || !/above its runner's ceiling of 200k/.test(capped.own))
      fail("a capped limit reads " + JSON.stringify(capped));

    // The terminal cog's context limit row is a stepper that saves the same override.
    const cog = await wp.evaluate(async () => {
      termTask = current;
      await termSettings({ preventDefault() {}, stopPropagation() {}, clientX: 10, clientY: 10, currentTarget: document.body });
      const row = [...document.querySelectorAll("#card-menu .stepper, .stepper")]
        .find(r => /context limit/.test(r.textContent));
      if (!row) return null;
      const before = Number(row.querySelector(".stepval").value);
      row.querySelector(".stepup").click();
      return { before, after: Number(row.querySelector(".stepval").value) };
    });
    if (!cog || cog.after !== cog.before + 1) fail("the cog's context limit row did not step: " + JSON.stringify(cog));
    const cogK = String(cog && cog.before + 1);
    await wp.waitForFunction(v => termTask.overrides && termTask.overrides.context_limit_k === v, cogK, { timeout: slow(5000) })
      .catch(() => {});
    if (!patches.some(p => p.overrides && p.overrides.context_limit_k === cogK))
      fail("the cog's context limit did not save as an override: " + JSON.stringify(patches));

    await wp.evaluate(() => document.getElementById("detail").close());

    // The settings: one harness=k list, saved through the hub, and a bad entry refused before it is sent.
    const before = posts.length;
    await wp.evaluate(async () => {
      await loadHousekeeping();
      const box = document.getElementById("s-ctxlimits");
      box.value = "claude=220, codex=300";
      box.dispatchEvent(new Event("change"));
    });
    await until(() => posts.slice(before).some(p => p.context_limits), 5000, "the limits POST").catch(() => {});
    const sent = posts.slice(before).find(p => p.context_limits);
    if (!sent || sent.context_limits.claude !== 220 || sent.context_limits.codex !== 300) fail("the per-harness list saved " + JSON.stringify(posts));
    const n = posts.length;
    await wp.evaluate(() => saveContextLimits("claude 200"));
    if (posts.length !== n) fail("a list that is not harness=k was sent: " + JSON.stringify(posts.slice(n)));
    if (errors.length) fail("page errors: " + errors.join("; "));
  } finally {
    mock.tasksMode = was;
    await ctx.close();
  }
  if (!bad) console.log("contextCycle ok");
}

// The thin context line along the bottom edge of a terminals row (ctxLine in js/board.js), drawn with the
// same meter as the details popover (ctxMeter in js/peek.js).
async function ctxLineSection(browser, base) {
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
  const row = (id, tokens, extra) => Object.assign({}, T1, {
    id, display_title: "row " + id, supervised: true, pinned: true, worktree: "/tmp/cl/" + id,
    context_size: tokens == null ? undefined : { tokens, warn: tokens >= 150000, threshold_k: 150 }
  }, extra || {});
  try {
    mock.wornTasks = [row("cl-40", 60000), row("cl-70", 105000), row("cl-90", 135000), row("cl-warm", 170000),
      row("cl-over", 201000, { telemetry: { window: 1000000 } }),
      row("cl-316", 316985, { context_size: { tokens: 316985, warn: true, threshold_k: 200, source: "card" } }),
      row("cl-long", 135000, { display_title: "a very long card title that has to wrap onto a second line in a narrow list" }),
      row("cl-none", null), row("cl-shelf", 201000, { status: "paused" }),
      row("cl-cycle", 201000, { new_context: { step: "clear", n: 2, of: 4, label: "clearing" } })];
    mock.tasksMode = "worn";
    await wp.goto(base, { waitUntil: "domcontentloaded" });
    await wp.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await wp.click('.tab[data-view="terms"]');
    const got = await wp.evaluate(async () => {
      await loadCards().catch(() => {}).then(renderTermList);
      const out = {};
      for (const el of document.querySelectorAll('#term-list .card.tab[data-id^="cl-"]')) {
        const b = el.querySelector(".peek-bar.ctxline");
        const r = el.getBoundingClientRect();
        const br = b && b.getBoundingClientRect();
        const i = b && b.querySelector("i"), s = b && b.querySelector("s");
        if (!b) out.plainH = r.height;
        out[el.dataset.id] = !b ? null : {
          cls: b.className, width: i.style.width, tick: s.style.left, tip: b.getAttribute("data-tip"),
          bottom: r.bottom - br.bottom >= 0 && r.bottom - br.bottom <= 8, h: br.height, wide: br.width > r.width - 4,
          flush: Math.abs(br.left - r.left) <= 2 && Math.abs(r.right - br.right) <= 2,
          barBg: getComputedStyle(b).backgroundColor, fillBg: getComputedStyle(i).backgroundColor,
          rowBg: getComputedStyle(el).backgroundColor, rowBorder: getComputedStyle(el).borderTopColor,
          pe: getComputedStyle(b).pointerEvents, z: getComputedStyle(b).zIndex,
          rowH: r.height, hitTop: br.top,
          clash: (() => {
            // The strip's box (hit area excluded) against every text run and chip in the row.
            const hits = [];
            const w = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
            for (let n; (n = w.nextNode());) {
              if (!n.textContent.trim() || b.contains(n)) continue;
              const rg = document.createRange(); rg.selectNodeContents(n);
              for (const q of rg.getClientRects()) if (q.width && q.bottom > br.top - 0.5 && q.top < br.bottom) hits.push("text:" + n.textContent.trim().slice(0, 20));
            }
            for (const ch of el.querySelectorAll(".chip, .pin, .rmark, svg")) {
              if (b.contains(ch)) continue;
              const q = ch.getBoundingClientRect();
              if (q.width && q.bottom > br.top - 0.5 && q.top < br.bottom) hits.push("chip:" + ch.className);
            }
            return hits;
          })(),
          landchip: !!el.querySelector(".chip.ctxland"), chiptext: (el.querySelector(".chip.ctxland") || {}).textContent,
          anim: getComputedStyle(i).animationIterationCount, name: getComputedStyle(i).animationName,
          fill: getComputedStyle(i).backgroundImage,
          chip: !!el.querySelector(".chip.ctxwarn"), cycle: !!el.querySelector(".chip") && /context 2\/4/.test(el.textContent)
        };
      }
      // The popover's own body, drawn by the same function.
      const body = document.createElement("div");
      body.innerHTML = peekBody({ id: "x", resume_id: "r", context_size: { tokens: 201000, warn: true, threshold_k: 150 } },
        { context_now: 201000, totals: {} });
      const pb = body.querySelector(".peek-ctx .peek-bar");
      const shared = document.createElement("div");
      shared.innerHTML = ctxMeter(201000, 200000, "", "201k of 200k (land the plane)");
      const memOf = t => { const d = document.createElement("div"); d.innerHTML = peekBody(t, { context_now: 1000, totals: {} }); const r = d.querySelector(".peek-row[data-tip*='Process Explorer']"); return r ? r.textContent : null; };
      const mem = { live: memOf({ id: "x", resume_id: "r", status: "running", pid: 1234, mem: 250 * 1048576 }), none: memOf({ id: "x", resume_id: "r", status: "running" }),
        over: memOf({ id: "x", resume_id: "r", status: "done", mem: 250 * 1048576 }) };
      return { rows: out, popover: pb ? pb.outerHTML : null, meter: shared.firstElementChild.outerHTML, mem };
    });
    // For checking by eye: ROWFLOOD_SHOTS=<dir> ROWFLOOD_TAG=before|after writes four rows side by side (a plain one,
    // and a context at 10%, 80% and 130% of the land line) at 2000px and 390px on a light and a dark skin.
    if (process.env.ROWFLOOD_SHOTS) {
      const dir = process.env.ROWFLOOD_SHOTS, tag = process.env.ROWFLOOD_TAG || "shot";
      const keep = mock.wornTasks;
      mock.wornTasks = [row("sh-plain", null, { display_title: "runtime", status: "working", worktree: "/tmp/cl/runtime" }),
        row("sh-10", 40000, { display_title: "review (green, 20%)", worktree: "/tmp/cl/review" }),
        row("sh-70", 140000, { display_title: "r-owed-answers (yellow, 70%)", worktree: "/tmp/cl/r-owed-answers" }),
        row("sh-90", 180000, { display_title: "ui (red, 90%)", worktree: "/tmp/cl/ui" }),
        row("sh-130", 260000, { display_title: "fabric (at limit, 130%)", worktree: "/tmp/cl/fabric", telemetry: { window: 1000000 } }),
        row("sh-long", 135000, { display_title: "a very long card title that has to wrap onto a second line in a narrow list", worktree: "/tmp/cl/orchestrator-with-a-long-path" })];
      for (const [w, h] of [[2000, 700], [390, 700]]) {
        await wp.setViewportSize({ width: w, height: h });
        for (const skin of ["paper", "graphite"]) {
          await wp.evaluate(async sk => { applySkin(sk); await loadCards().catch(() => {}); await renderTermList(); }, skin);
          await settleStyle(wp);
          await wp.screenshot({ path: require("path").join(dir, `${tag}-${w}-${skin}.png`) });
        }
      }
      await wp.setViewportSize({ width: 1400, height: 900 });
      mock.wornTasks = keep;
      await wp.evaluate(async () => { await loadCards().catch(() => {}); await renderTermList(); });
    }
    // ROWFLOOD_REAL=<tasks.json> ROWFLOOD_REAL_SHOTS=<dir> ROWFLOOD_TAG=before|after: the board drawn from a COPY of a live
    // /v1/tasks answer, set up like a real operator's: rows with context, five pinned, cards wearing a theme, a room chip
    // (hub mode) on some, nested children under one, a wide list so full titles show; at default, tight (0.72) and
    // tightest (0.3) density, on paper and graphite, at 2000px on the desktop board and 390px in a real phone context
    // (touch, mobile viewport, so phone.css applies).
    if (process.env.ROWFLOOD_REAL) {
      const d = JSON.parse(require("fs").readFileSync(process.env.ROWFLOOD_REAL, "utf8"));
      const list = (Array.isArray(d) ? d : d.tasks).filter(t => t.context_size).slice(0, 14);
      const themes = ["active-work", "electric-purple", "crimson-cave", "deep-ocean", "garnet", "forest-night", "", "cocoa"];
      const keep = mock.wornTasks;
      const roomOfRow = i => i % 3 === 0 ? "room-e-control" : i % 3 === 1 ? "claude-room-e" : "";
      const tagged = (i, id) => roomOfRow(i) ? roomOfRow(i) + "~" + id : id;
      mock.wornTasks = list.map((t, i) => Object.assign({}, t, {
        id: tagged(i, t.id), supervised: true, pinned: i < 5, theme: themes[i % themes.length], room: roomOfRow(i),
        // rows 8 to 10 are subagents of row 7, so row 7 wears a child count
        spawned_by_id: i >= 8 && i <= 10 ? list[7].id : t.spawned_by_id,
        tags: i >= 8 && i <= 10 ? Array.from(new Set((t.tags || []).concat("atrium:subagent"))) : t.tags
      }));
      const tag = process.env.ROWFLOOD_TAG || "shot", dir = process.env.ROWFLOOD_REAL_SHOTS;
      const path = require("path");
      const prep = async page => {
        await page.evaluate(async () => { hubIsHub = true; try { setTermW(520); } catch (e) {} if (!termSubsOn()) toggleTermShow("subagents"); await loadCards().catch(() => {}); await renderTermList(); });
      };
      const shoot = async (page, kind, shotsAt) => {
        for (const dens of ["1", "0.72", "0.3"]) {
          for (const skin of ["paper", "graphite"]) {
            await page.evaluate(async ([sk, dn]) => {
              applySkin(sk); document.documentElement.style.setProperty("--density", dn);
              await renderTermList();
            }, [skin, dens]);
            await settleStyle(page);
            // A parent row is as tall as it is without its fold button, and the pinned heading is not under the
            // controls tray (the list's own scrollTop, not the header, is what a screenshot can get wrong).
            const m = await page.evaluate(async () => {
              // The list keeps whatever scrollTop an earlier render left it at (60px at density 1, 48 at 0.72), which
              // slid the heading under the tray in the shots. Start every shot from the top of the list.
              const top = document.querySelector(".termscroll"); if (top) top.scrollTop = 0;
              await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));
              const row = [...document.querySelectorAll("#term-list .card .tkidfold")].map(b => b.closest(".card"))[0];
              const out = { row: !!row };
              if (row) {
                out.h = row.getBoundingClientRect().height;
                const b = row.querySelector(".tkidfold"), nx = b.nextSibling, par = b.parentNode;
                par.removeChild(b); out.bare = row.getBoundingClientRect().height; par.insertBefore(b, nx);
              }
              const sc = document.querySelector(".termscroll"), hd = document.querySelector("#term-list .pinnedhead");
              out.scrollTop = sc ? sc.scrollTop : -1;
              if (sc && hd) { out.headTop = hd.getBoundingClientRect().top; out.scTop = sc.getBoundingClientRect().top; }
              return out;
            });
            if (!m.row) fail(`${kind} density ${dens}: no parent row with a fold button.`);
            else if (Math.abs(m.h - m.bare) > 0.5) fail(`${kind} ${skin} density ${dens}: a parent row is ${m.h}px, ${m.bare}px without its fold button.`);
            if (m.headTop !== undefined && m.headTop < m.scTop - 0.5) fail(`${kind} ${skin} density ${dens}: the pinned heading is ${(m.scTop - m.headTop).toFixed(1)}px under the controls tray at scrollTop ${m.scrollTop}.`);
            if (process.env.ROWFLOOD_DEBUG) console.log("rowflood", kind, skin, dens, JSON.stringify(m));
            await page.screenshot({ path: path.join(dir, `${tag}-${kind}-${skin}-density${dens}.png`) });
          }
        }
      };
      await wp.setViewportSize({ width: 2000, height: 1000 });
      await prep(wp);
      await shoot(wp, "2000");
      await wp.evaluate(() => document.documentElement.style.removeProperty("--density"));
      const phoneCtx = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true, deviceScaleFactor: 2 });
      const pp = await phoneCtx.newPage();
      await pp.addInitScript(() => {
        let all = {};
        try { all = JSON.parse(localStorage.getItem("atrium.skipconfirm") || "{}"); } catch (e) {}
        all["width-floor"] = true;
        localStorage.setItem("atrium.skipconfirm", JSON.stringify(all));
      });
      await pp.goto(base, { waitUntil: "domcontentloaded" });
      await pp.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
      await pp.click('.tab[data-view="terms"]').catch(() => {});
      await prep(pp);
      await shoot(pp, "phone390");
      await phoneCtx.close();
      await wp.setViewportSize({ width: 1400, height: 900 });
      mock.wornTasks = keep;
      await wp.evaluate(async () => { hubIsHub = false; await loadCards().catch(() => {}); await renderTermList(); });
    }
    const r = got.rows;
    if (!(r.plainH > 0)) fail("no plain row to compare the height with.");
    for (const [id, w, cls] of [["cl-40", "27.3%", "peek-bar ctxline"], ["cl-70", "47.7%", "peek-bar ctxline"],
      ["cl-90", "61.4%", "peek-bar ctxline warm"], ["cl-warm", "77.3%", "peek-bar ctxline hot"],
      ["cl-over", "91.4%", "peek-bar ctxline hot over"]]) {
      const b = r[id];
      if (!b) { fail(id + " has no context line."); continue; }
      if (b.cls !== cls) fail(id + " line class is " + b.cls + ", want " + cls);
      if (b.width !== w) fail(id + " fill is " + b.width + ", want " + w);
      if (b.tick !== "90.9%") fail(id + " tick is at " + b.tick);
      // A thin line along the row's bottom edge, inside the card and flush with its sides (as it was at 3e9ca7a1), never
      // a fill behind the title and path.
      if (!b.bottom || b.h > 4 || b.h < 2 || !b.flush) fail(id + " context line is not a thin strip on the bottom edge: " + JSON.stringify(b));
      if (b.z !== "auto" || b.pe === "none") fail(id + " context line layering/pointer (no z-index layer behind the row, the bar takes the hover): " + JSON.stringify(b));
      if (b.clash.length) fail(id + " context line overlaps the row's own text or chips: " + b.clash.join(", "));
      if (Math.abs(b.rowH - r.plainH) > 0.5) fail(id + " row is " + b.rowH + "px tall, a row without a line is " + r.plainH + "px.");
      if (b.fill === "none") fail(id + " context line has no fill colour.");
    }
    if (r["cl-316"] && (r["cl-316"].cls !== "peek-bar ctxline hot over" || r["cl-316"].width !== "100%")) {
      fail("316985 tokens against a 200k limit is not a full red line: " + JSON.stringify(r["cl-316"]));
    }
    if (r["cl-over"] && (r["cl-over"].anim !== "1" || r["cl-over"].name !== "ctxline-pulse")) {
      fail("past the limit the line does not pulse exactly once: " + r["cl-over"].anim + " " + r["cl-over"].name);
    }
    if (r["cl-warm"] && r["cl-warm"].name !== "none") fail("a line under the land line animates.");
    if (r["cl-over"] && r["cl-over"].tip !== "201k of 200k (land the plane), window 1M") fail("the line's tooltip: " + (r["cl-over"] || {}).tip);
    if (r["cl-over"] && r["cl-over"].landchip) fail("past the land line the row draws a LAND badge: " + JSON.stringify(r["cl-over"]));
    if (r["cl-over"] && !r["cl-over"].chip) fail("past the land line the row lost the amber warn mark.");
    // Heat steps by the fraction of the limit: good under 60% (105k of 200k), amber from 60% (135k), danger from 85%
    // (170k) and past the limit (201k): three different fills, and the tooltip says the figure on every one.
    if (r["cl-70"] && r["cl-90"] && r["cl-over"] && new Set([r["cl-70"].fill, r["cl-90"].fill, r["cl-over"].fill]).size !== 3) {
      fail("the context line's heat steps are not three colours: " + [r["cl-70"].fill, r["cl-90"].fill, r["cl-over"].fill]);
    }
    if (r["cl-warm"] && r["cl-over"] && r["cl-warm"].fill !== r["cl-over"].fill) fail("85% of the limit is not the danger fill.");
    for (const [id, tip] of [["cl-70", "105k of 200k"], ["cl-90", "135k of 200k"], ["cl-warm", "170k of 200k"]]) {
      if (r[id] && r[id].tip !== tip) fail(id + " tooltip is " + r[id].tip + ", want " + tip);
    }
    // A row with a context figure looks as strong as a plain one: same row background and border whatever the context.
    for (const id of ["cl-40", "cl-warm", "cl-over"]) {
      if (r[id] && r["cl-70"] && (r[id].rowBg !== r["cl-70"].rowBg || r[id].rowBorder !== r["cl-70"].rowBorder)) {
        fail(id + " row is tinted or bordered differently from a low-context row: " + JSON.stringify([r[id].rowBg, r[id].rowBorder, r["cl-70"].rowBg, r["cl-70"].rowBorder]));
      }
    }    if (r["cl-warm"] && (!r["cl-warm"].chip || r["cl-warm"].landchip)) fail("between warn and land the row must have the amber mark and no badge.");
    if (r["cl-70"] && (r["cl-70"].chip || r["cl-70"].landchip)) fail("a card under the warn line has a mark.");
    if (r["cl-none"]) fail("a row with no context_size has a line.");
    if (r["cl-shelf"]) fail("a paused card has a line.");
    if (!r["cl-long"]) fail("the long-title row has no context line.");
    else if (r["cl-long"].clash.length) fail("a two-line title overlaps the context line: " + r["cl-long"].clash.join(", "));
    // Tight density and a small text size shrink the row's padding: the line must still sit clear of the path and chips.
    for (const [dens, scale] of [["0.3", "1"], ["0.5", "1"], ["1", "0.6"], ["0.3", "0.6"]]) {
      const hits = await wp.evaluate(async ([d, u]) => {
        const rs = document.documentElement.style;
        rs.setProperty("--density", d); rs.setProperty("--uiscale", u);
        await renderTermList();
        const out = [];
        for (const id of ["cl-40", "cl-warm", "cl-over", "cl-long"]) {
          const el = document.querySelector(`#term-list [data-id="${id}"]`), b = el && el.querySelector(".peek-bar.ctxline");
          if (!b) { out.push(id + ": no line"); continue; }
          const br = b.getBoundingClientRect(), w = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
          for (let n; (n = w.nextNode());) {
            if (!n.textContent.trim() || b.contains(n)) continue;
            const rg = document.createRange(); rg.selectNodeContents(n);
            for (const q of rg.getClientRects()) if (q.width && q.bottom > br.top + 0.01) out.push(id + ": text " + n.textContent.trim().slice(0, 16) + " reaches " + (q.bottom - br.top).toFixed(1) + "px into the line");
          }
          for (const ch of el.querySelectorAll(".chip, .pin, .rmark, svg")) {
            if (b.contains(ch)) continue;
            const q = ch.getBoundingClientRect();
            if (q.width && q.bottom > br.top + 0.01) out.push(id + ": chip " + ch.className);
          }
        }
        rs.removeProperty("--density"); rs.removeProperty("--uiscale");
        return out;
      }, [dens, scale]);
      if (hits.length) fail("at density " + dens + " and uiscale " + scale + " the context line meets the row's text or chips: " + hits.join(" | "));
    }
    await wp.evaluate(async () => { await renderTermList(); });
    const c = r["cl-cycle"];
    if (!c) fail("a cycling card has no line.");
    else {
      if (/ over\b/.test(c.cls)) fail("a cycling card pulses.");
      if (c.chip || c.landchip) fail("a cycling card draws the limit chip or badge instead of the cycle.");
      if (!c.cycle) fail("a cycling card does not show the new-context chip.");
    }
    if (!/^memory\s*250(\.0)?M\s+pid 1234$/.test(got.mem.live || "")) fail("the details popover has no memory line for a live card: " + got.mem.live);
    if (got.mem.none || got.mem.over) fail("the memory line is drawn for a card with no figure or one that is over: " + JSON.stringify(got.mem));
    if (!got.popover) fail("the details popover lost its .peek-bar.");
    else if (got.popover !== got.meter) fail("the popover's meter is not ctxMeter's drawing:\n" + got.popover + "\n" + got.meter);
    // Light and dark: the line's fill resolves to a colour in both.
    for (const skin of ["graphite", "paper"]) {
      const fillOk = await wp.evaluate(async sk => {
        try { applySkin(sk); } catch (e) { return "noskin"; }
        const i = document.querySelector('#term-list [data-id="cl-over"] .ctxline i');
        return /rgb/.test(getComputedStyle(i).backgroundImage);
      }, skin);
      if (fillOk === false) fail("the line has no fill colour in skin " + skin);
    }
  } finally {
    await ctx.close();
    mock.tasksMode = was;
  }
  if (errors.length) fail("the terminals list threw: " + errors.join(" | "));
}

// PAST THE GEAR'S CONTEXT THRESHOLD, A CARD WEARS A MARK AND NO NUMBER, on the
// stack and the board. The number is in its details, one compact view reached
// three ways: a second on the card, "details" on its menu, and the expando
// on the terminal's shortcut strip, which slides it up as a drawer. The details
// read the card's usage when they open and never before. The gear holds the
// threshold, 150k by default, and saves it to the room.
async function contextSizeSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const sp = await ctx.newPage();
  const errors = [];
  sp.on("pageerror", e => errors.push(String(e)));
  const usageReads = [];
  await ctx.route("**/v1/tasks/*/usage*", async route => {
    const id = decodeURIComponent(route.request().url().split("/v1/tasks/")[1].split("/")[0]);
    usageReads.push(id);
    await route.fulfill({ json: CTX_USAGE[id] || { context_now: 0, totals: {}, rows: [] } });
  });
  const shots = process.env.CTX_SHOTS || "";
  const shoot = async (name, clip) => {
    if (!shots) return;
    await settleStyle(sp);
    await sp.screenshot({ path: `${shots}/${name}.png`, clip });
  };
  const peekState = () => sp.evaluate(() => {
    const p = document.querySelector(".peek");
    if (!p) return null;
    const r = p.getBoundingClientRect();
    return { on: p.classList.contains("on"), pinned: p.classList.contains("pinned"), id: p.dataset.id,
      text: p.textContent.replace(/\s+/g, " ").trim(), warn: !!p.querySelector(".peek-ctx.warn"),
      title: p.querySelector("[title]") !== null, x: r.left, y: r.top, w: r.width, h: r.height };
  });
  const posts = [];
  await ctx.route("**/v1/settings", async route => {
    const r = route.request();
    if (r.method() === "POST") {
      const body = JSON.parse(r.postData() || "{}");
      posts.push(body);
      const limits = body.context_limits && Object.keys(body.context_limits).length ? body.context_limits : { claude: 200 };
      await route.fulfill({ json: { context_limits: limits } });
      return;
    }
    await route.fulfill({ json: { context_limits: { claude: 200 }, context_limits_default: { claude: 200 },
      context_limit_k_min: 50, context_limit_k_max: 2000 } });
  });
  const was = mock.tasksMode;
  mock.tasksMode = "ctxsize";
  try {
    await sp.goto(base, { waitUntil: "domcontentloaded" });
    await sp.waitForSelector('#stack-list .stackrow[data-id="cx-big"]', { state: "attached", timeout: slow(15000) });
    await sp.evaluate(() => runRefresh());
    await sp.waitForFunction(() => !!document.querySelector('#stack-list .stackrow[data-id="cx-big"] .chip.ctxwarn'), null, { timeout: slow(5000) })
      .catch(() => {});
    // CTX_SKIN=noir draws it all in another skin, for the screenshots.
    if (process.env.CTX_SKIN) await sp.evaluate(s => applySkin(s), process.env.CTX_SKIN);

    // The mark: on the card past the line only, in the warn colour, and no
    // number anywhere on a card face, a stack row or the terminal bar.
    const got = await sp.evaluate(cards => {
      const probe = document.createElement("span");
      probe.style.color = "var(--warn)";
      document.body.appendChild(probe);
      const warnColour = getComputedStyle(probe).color;
      probe.remove();
      const read = root => {
        const m = root && root.querySelector(".chip.ctxwarn");
        return { mark: m ? { warnColour: getComputedStyle(m).color === warnColour, text: m.textContent.trim(),
          tip: m.getAttribute("data-tip") || "", svg: !!m.querySelector("svg"), land: m.classList.contains("ctxland") } : null,
          // No number on a face: the old LAND badge was the one place it was drawn.
          number: /\b(90|212)k\b/.test(root ? root.textContent : "") };
      };
      const row = id => read(document.querySelector(`#stack-list .stackrow[data-id="${id}"]`));
      const board = card => {
        const b = document.createElement("div");
        b.innerHTML = cardHTML(card);
        document.body.appendChild(b);
        const out = read(b);
        b.remove();
        return out;
      };
      const bar = document.querySelector(".term-bar");
      return { small: row("cx-small"), big: row("cx-big"), none: row("cx-none"),
        boardBig: board(cards[1]), boardSmall: board(cards[0]),
        barSlot: !!document.getElementById("t-ctxsize"), barNumber: /\b(90|212)k\b/.test(bar ? bar.textContent : "") };
    }, CTX_CARDS);
    if (!got.big.mark) fail("a card past the context threshold has no mark on its stack row.");
    else {
      // 212k is past the land-the-plane line (200k): no LAND badge any more (the card is auto-cleared there), the amber mark stays.
      if (got.big.mark.land || got.big.mark.text !== "" || !got.big.mark.svg) fail("a card past the land-the-plane line wears something other than the amber mark: " + JSON.stringify(got.big.mark));
    }
    if (got.small.mark || got.none.mark) fail("a card under the threshold, or with no size, wears the context mark.");
    if (!got.boardBig.mark) fail("a board card past the threshold has no context mark.");
    if (got.boardSmall.mark) fail("a board card under the threshold wears the context mark.");
    for (const [where, r] of [["stack", got.small], ["stack", got.big], ["board", got.boardBig], ["board", got.boardSmall]]) {
      if (r.number) fail("the context number is on the " + where + " face.");
    }
    if (got.barSlot || got.barNumber) fail("the terminal bar still carries the context number.");
    if (usageReads.length) fail("the board read a card's usage before anything was opened: " + usageReads.join(","));

    // A second on a stack row opens the details, and not half a second.
    await sp.hover('#stack-list .stackrow[data-id="cx-big"] .title, #stack-list .stackrow[data-id="cx-big"]');
    await new Promise(r => setTimeout(r, 500)); // wait: the half second the details must not open within
    let pk = await peekState();
    if (pk && pk.on) fail("the details opened after half a second of hover, not one.");
    await sp.waitForFunction(() => { const p = document.querySelector(".peek"); return p && p.classList.contains("on"); },
      null, { timeout: slow(4000) }).catch(() => fail("a second on a card did not open its details."));
    await sp.waitForFunction(() => /212k/.test((document.querySelector(".peek") || {}).textContent || ""),
      null, { timeout: slow(4000) }).catch(() => {});
    pk = await peekState();
    if (pk) {
      if (pk.id !== "cx-big" || !/212k/.test(pk.text) || !pk.warn) fail("the hover details do not show 212k past the line: " + pk.text);
      // The peek was cut to header, needs you, recap, context bar and actions (bcaa01422), so the totals live on the
      // usage tab now. What stays here is that no dollar figure leaks in.
      if (/\$/.test(pk.text)) fail("the hover details carry a dollar figure: " + pk.text);
      if (!/warns at 150k/.test(pk.text)) fail("the hover details do not name the threshold: " + pk.text);
      if (pk.pinned) fail("a hover opened the pinned details.");
      if (pk.title) fail("the details use a native title tooltip.");
      if (pk.x < 0 || pk.y < 0 || pk.x + pk.w > 1400 || pk.y + pk.h > 900) fail("the details are off the screen: " + JSON.stringify(pk));
    }
    if (usageReads.filter(x => x === "cx-big").length !== 1) fail("opening the details read the usage " + usageReads.length + " times, not once.");
    await shoot("hover-" + (await sp.evaluate(() => document.documentElement.getAttribute("data-skin") || "harbour")));
    // Off the card and off the popover: it goes.
    await sp.mouse.move(1390, 890);
    await sp.waitForFunction(() => !document.querySelector(".peek.on"), null, { timeout: slow(3000) })
      .catch(() => fail("the hover details stayed after the pointer left."));

    // The menu's "details": the same view, pinned until a click elsewhere.
    await sp.click('#stack-list .stackrow[data-id="cx-small"]', { button: "right" });
    const item = sp.locator("#cardmenu button", { hasText: /^details$/ }).first();
    await item.waitFor({ state: "visible", timeout: slow(5000) }).catch(() => fail("the card menu has no details entry."));
    if (await item.count()) {
      await item.click();
      await sp.waitForFunction(() => /90k/.test((document.querySelector(".peek.on") || {}).textContent || ""),
        null, { timeout: slow(4000) }).catch(() => fail("the menu's details did not open on 90k."));
      pk = await peekState();
      if (pk && (!pk.pinned || pk.warn)) fail("the menu's details are not pinned, or warn under the line: " + JSON.stringify(pk));
      await sp.mouse.move(1390, 890);
      await new Promise(r => setTimeout(r, 600)); // wait: a negative check, the pinned details must not close
      if (!(await peekState()).on) fail("the menu's details closed when the pointer left.");
      await shoot("menu-" + (await sp.evaluate(() => document.documentElement.getAttribute("data-skin") || "harbour")));
      await sp.keyboard.press("Escape");
      if ((await peekState()).on) fail("escape did not close the details.");
    }

    // The shortcut strip's expando slides the drawer up for the attached card.
    await sp.evaluate(() => document.querySelector('.tab[data-view="terms"]').click());
    await sp.waitForFunction(() => !document.getElementById("terms").hidden, null, { timeout: slow(5000) });
    // Attached, as far as the strip is concerned: no socket in a mocked room.
    await sp.evaluate(card => { termTask = card; }, CTX_CARDS[1]);
    await sp.waitForSelector("#t-expando", { state: "visible", timeout: slow(5000) })
      .catch(() => fail("the shortcut strip has no details expando."));
    const reads = usageReads.length;
    await sp.click("#t-expando").catch(e => fail("the expando could not be clicked: " + e.message));
    await sp.waitForFunction(() => /212k/.test(document.getElementById("t-drawer-body").textContent),
      null, { timeout: slow(4000) }).catch(() => fail("the drawer did not open on the attached card's details."));
    await settleStyle(sp);
    const dr = await sp.evaluate(() => {
      const d = document.getElementById("t-drawer"), help = document.querySelector(".term-help");
      const r = d.getBoundingClientRect(), h = help.getBoundingClientRect();
      return { open: d.classList.contains("open"), above: r.bottom <= h.top + 1, visible: getComputedStyle(d).visibility,
        expanded: document.getElementById("t-expando").getAttribute("aria-expanded") };
    });
    if (!dr.open || dr.visible !== "visible" || !dr.above || dr.expanded !== "true") {
      fail("the drawer is not open above the strip: " + JSON.stringify(dr));
    }
    if (usageReads.length !== reads + 1) fail("opening the drawer did not read the usage exactly once.");
    await shoot("drawer-" + (await sp.evaluate(() => document.documentElement.getAttribute("data-skin") || "harbour")));
    // The line itself closes it.
    await sp.click(".term-help-keys");
    const shut = await sp.evaluate(() => document.getElementById("t-drawer").classList.contains("open"));
    if (shut) fail("clicking the strip did not close the drawer.");

    // The gear: claude=200 by default, and a typed list is saved through the hub.
    const gear = await sp.evaluate(async () => {
      await loadHousekeeping();
      const box = document.getElementById("s-ctxlimits");
      const before = { placeholder: box.placeholder, value: box.value };
      box.value = "claude=250, codex=300";
      box.dispatchEvent(new Event("change"));
      return before;
    });
    if (gear.placeholder !== "claude=200" || gear.value !== "") fail("the context limit does not default to claude=200: " + JSON.stringify(gear));
    await sp.waitForFunction(() => !document.getElementById("s-ctxlimits-reset").hidden, null, { timeout: slow(5000) })
      .catch(() => fail("a saved limit list did not offer a reset to the default."));
    if (!posts.some(p => p.context_limits && p.context_limits.claude === 250 && p.context_limits.codex === 300)) {
      fail("the context limits were not saved: " + JSON.stringify(posts));
    }
  } finally {
    await ctx.close();
    mock.tasksMode = was;
  }
  if (errors.length) fail("the context size page threw: " + errors.join(" | "));
}

// The usage tab's turns atrium caused: tokens and an estimated dollar figure by kind, per card and per day, the
// costliest turns, and the noise line. TURNCOST_SHOT=<file.png> also writes a screenshot of the section.
async function usageTurnsSection(browser, base) {
  const errors = [];
  const at = new Date(Math.floor(Date.now() / 900000) * 900000 - 900000).toISOString();
  const mk = n => ({ rows: 1, replies: 1, input: n, output: 0, cache_write_5m: 0, cache_write_1h: 0, cache_read: n * 10, cost: 0 });
  const row = (day, id, name, kind, turns, input, output, cw, cr, cost, noise, none, ack) => ({ day, task_id: id, name, kind, turns,
    input, output, cache_write: cw, cache_read: cr, cost, noise_turns: noise, noise_tokens: noise ? input + output + cw : 0,
    noise_cost: noise ? cost : 0, none_turns: none, ack_turns: ack, deliveries: turns });
  const turns = { since: at, until: at, pending: 2, rows: [
    row("2026-10-07", "tc-a", "orchestrator", "say", 14, 300, 9000, 210000, 3200000, 3.41, 3, 1, 2),
    row("2026-10-07", "tc-a", "orchestrator", "notice", 9, 120, 2100, 90000, 1500000, 1.12, 8, 5, 3),
    row("2026-10-07", "tc-b", "worker-r1", "nudge", 4, 40, 600, 30000, 400000, 0.31, 4, 4, 0),
    row("2026-10-07", "tc-b", "worker-r1", "operator", 6, 80, 7000, 150000, 2100000, 2.2, 0, 0, 0),
    row("2026-10-06", "tc-a", "orchestrator", "report", 5, 90, 4000, 120000, 1900000, 1.8, 0, 0, 0),
    row("2026-10-06", "tc-b", "worker-r1", "restart-wake", 1, 10, 300, 60000, 100000, 0.4, 1, 0, 1)],
    top: [
    { id: "d1", task_id: "tc-a", name: "orchestrator", from: "worker-r1", kind: "report", reply: "work", input: 40, output: 3000, cache_write_5m: 90000, cache_write_1h: 0, cache_read: 900000, cost: 1.1 },
    { id: "d2", task_id: "tc-a", name: "orchestrator", from: "atrium", kind: "notice", reply: "ack", input: 3, output: 40, cache_write_5m: 70000, cache_write_1h: 0, cache_read: 200000, cost: 0.4 }] };
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 1100 } });
  const sp = await ctx.newPage();
  sp.on("pageerror", e => errors.push(String(e)));
  await ctx.route(/\/v1\/usage\?/, route => route.fulfill({ json: { buckets: [
    { t: at, total: mk(3000), cards: { "tc-a": mk(2000), "tc-b": mk(1000) }, causes: { operator: mk(3000) } }] } }));
  await ctx.route(/\/v1\/usage\/items/, route => route.fulfill({ json: { items: [], unlinked: 0 } }));
  let asked = "";
  await ctx.route(/\/v1\/usage\/turns/, route => { asked = route.request().url(); route.fulfill({ json: turns }); });
  await ctx.route("**/v1/settings", route => route.fulfill({ json: { usage_cache_reads: false, board_skin: "harbour", board_skins: SKINS } }));
  try {
    await sp.goto(base, { waitUntil: "domcontentloaded" });
    await sp.waitForSelector("#stack-list .stackrow", { state: "attached", timeout: slow(15000) });
    await sp.evaluate(() => { UC.sub = "reports"; switchView("usage"); });
    await sp.waitForSelector("#uc-body [data-role=turns-kind]", { timeout: slow(10000) });
    if (!/since=/.test(asked)) fail("usageTurns: the read named no range: " + asked);
    const t = await sp.evaluate(() => ({
      kinds: [...document.querySelectorAll("#uc-body [data-role=turns-kind] > span")].map(x => x.textContent),
      top: document.querySelector("#uc-body [data-role=turns-top]").textContent,
      noise: document.querySelector("#uc-body [data-role=turns-noise]").textContent,
      text: document.getElementById("uc-body").textContent }));
    if (t.kinds[5] !== "say") fail("usageTurns: the biggest kind is not first: " + t.kinds.slice(5, 10));
    if (!t.kinds.includes("$3.41") || !t.kinds.includes("$2.20")) fail("usageTurns: a kind has no dollar estimate: " + t.kinds);
    if (!/orchestrator/.test(t.text) || !/2026-10-07/.test(t.text) || !/worker-r1/.test(t.text)) fail("usageTurns: per card or per day is missing.");
    if (/operator/.test(t.top)) fail("usageTurns: the operator's turn is among the costliest atrium caused: " + t.top);
    if (!/report/.test(t.top) || !/from worker-r1/.test(t.top)) fail("usageTurns: the costliest turn does not say what caused it: " + t.top);
    // the operator's is not atrium's: 3+8+4+1 turns of noise, 10 none and 6 ack
    if (!/noise: 16 turns/.test(t.noise) || !/10 no reply, 6 ack/.test(t.noise) || !/2 delivered, turn not ended yet/.test(t.noise)) fail("usageTurns: the noise line reads " + t.noise);
    if (process.env.TURNCOST_SHOT) {
      await sp.evaluate(() => { const b = document.querySelector("#uc-body [data-role=turns-kind]"); const hd = b && b.previousElementSibling; if (hd) hd.scrollIntoView(); });
      await settleStyle(sp);
      await sp.screenshot({ path: process.env.TURNCOST_SHOT });
    }
  } finally { await ctx.close(); }
  if (errors.length) fail("usageTurns: the page threw: " + errors.join(" | "));
}

// The usage tab and its cache reads toggle. Off by default: four counted kinds
// stacked, the cache reads named on one line under the split, five kinds still on
// the hover. On is the tab as it always was. The choice is the daemon's.
async function usageCacheReadsSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const sp = await ctx.newPage();
  const errors = [];
  sp.on("pageerror", e => errors.push(String(e)));
  const posts = [];
  let stored = false;
  const at = new Date(Math.floor(Date.now() / 900000) * 900000 - 900000).toISOString();
  const sums = { rows: 2, replies: 9, input: 1000, output: 2000, cache_write_5m: 3000, cache_write_1h: 4000, cache_read: 90000, cost: 0 };
  await ctx.route("**/v1/usage*", route => route.fulfill({ json: { buckets: [
    { t: at, total: sums, cards: { "uc-a": sums }, causes: { operator: sums } }] } }));
  await ctx.route("**/v1/settings", async route => {
    const r = route.request();
    if (r.method() === "POST") {
      const body = JSON.parse(r.postData() || "{}");
      posts.push(body);
      if ("usage_cache_reads" in body) stored = body.usage_cache_reads;
    }
    await route.fulfill({ json: { usage_cache_reads: stored, board_skin: "harbour", board_skins: SKINS } });
  });
  const look = () => sp.evaluate(() => {
    const kinds = new Set([...document.querySelectorAll('#uc-body .ucchart[data-chart=burn] rect')].map(r => r.getAttribute("class")));
    const g = document.querySelector('#uc-body .ucchart[data-chart=burn] g[data-t]');
    const out = { kinds: [...kinds].sort(), line: /not in these charts/.test(document.getElementById("uc-body").textContent),
      on: document.getElementById("uc-cache").getAttribute("aria-pressed"), hasG: !!g };
    UC.sub = "reports"; ucPaint();
    out.causes = (document.querySelector("#uc-body .uccauses") || {}).textContent || "";
    UC.sub = "charts"; ucPaint();
    return out;
  });
  try {
    await sp.goto(base, { waitUntil: "domcontentloaded" });
    await sp.waitForSelector("#stack-list .stackrow", { state: "attached", timeout: slow(15000) });
    await sp.evaluate(() => switchView("usage"));
    await sp.waitForSelector("#uc-body .ucchart[data-chart=burn] rect", { timeout: slow(10000) });
    let s = await look();
    if (s.on !== "false") fail("usageCacheReads: the toggle is pressed by default.");
    if (s.kinds.length !== 4 || s.kinds.includes("uck-read")) fail("usageCacheReads: off should stack four kinds: " + s.kinds);
    if (!s.line) fail("usageCacheReads: off has no cache reads line.");
    const txt = await sp.textContent("#uc-body .uccacheline");
    if (!/cache reads 90k · not in these charts · 90% of input was served from the cache/.test(txt)) fail("usageCacheReads: the line reads " + txt);
    if (!/9 calls/.test(s.causes)) fail("usageCacheReads: a cause row does not show its calls: " + s.causes);
    // A repaint of #uc-body between the hover and the read draws a fresh "hover a bar", so hover again until the
    // readout answers (at most three times) rather than reading once.
    let read = "";
    for (let i = 0; i < 3; i++) {
      await sp.hover('#uc-body .ucchart[data-chart=burn] g[data-t] rect', { position: { x: 1, y: 1 + i } });
      read = await sp.waitForFunction(() => { const r = document.querySelector("#uc-body .ucchart[data-chart=burn] .ucread"); return r && r.textContent !== "hover a bar" && r.textContent; },
        null, { timeout: slow(1500) }).then(h => h.jsonValue()).catch(() => "");
      if (read) break;
    }
    if (!read) read = await sp.textContent("#uc-body .ucchart[data-chart=burn] .ucread");
    if (!/cache read 90k/.test(read) || (read.match(/ · /g) || []).length !== 5) fail("usageCacheReads: the hover does not list five kinds: " + read);

    await sp.click("#uc-cache");
    await sp.waitForFunction(() => document.getElementById("uc-cache").getAttribute("aria-pressed") === "true", null, { timeout: slow(5000) });
    s = await look();
    if (s.kinds.length !== 5) fail("usageCacheReads: on should stack five kinds: " + s.kinds);
    if (s.line) fail("usageCacheReads: on still has the cache reads line.");
    if (!posts.some(b => b.usage_cache_reads === true)) fail("usageCacheReads: the daemon was not told: " + JSON.stringify(posts));
  } finally {
    await ctx.close();
  }
  if (errors.length) fail("usageCacheReads: the page threw: " + errors.join(" | "));
}

// u-027: the usage tab's neutral by-card colour, the toggle that says its state, and the chart's total.
// NOTHING HERE READS THE REAL CLOCK. Every fixture is built from one `now` chosen below and handed to the
// functions that take it (ucAxis(series, now)) or set as UC.now,
// which ucNow() answers for the whole tab. USAGE_SHOT=<file.png> also writes a screenshot of the tab.
// A picture, not a check, so it may use the real clock: a day of turns, four cards, cache reads dominant.
// It touches nothing that u-027 added, so USAGE_SHOT_ONLY=1 can take the "before" from the old board.
async function usageShot(sp, file) {
  await sp.evaluate(() => {
    UC.cacheReads = false; UC.range = "24h"; UC.bw = 900; UC.group = "card";
    UC.since = Math.floor((Date.now() - 86400000) / 1000) * 1000;
    const last = Math.floor(Date.now() / 900000) * 900000;
    const ids = ["atrium", "zrok-docs", "board-ui", "reviewer"];
    const buckets = new Map();
    for (let i = 1; i <= 90; i++) {
      const t = last - i * 900000;
      const cards = {}, total = ucSums();
      ids.forEach((id, k) => {
        if ((i * 7 + k * 13) % 5 === 0) return;
        const v = 200 + ((i * 37 + k * 91) % 900) * (4 - k);
        const s = { rows: 1, replies: 2, input: v, output: v / 2, cache_write_5m: v * 3, cache_write_1h: 0, cache_read: v * 40, cost: 0 };
        cards[id] = ucAdd(ucSums(), s);
        ucAdd(total, s);
      });
      buckets.set(t, { t, total, cards, causes: { operator: total }, groups: {} });
    }
    UC.now = 0;
    UC.rooms = { "": { state: "ok", why: "", noGroups: false, buckets } };
    ucPaint();
  });
  await sp.screenshot({ path: file });
}

async function usageTabSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const sp = await ctx.newPage();
  const errors = [];
  sp.on("pageerror", e => errors.push(String(e)));
  let stored = false;
  await ctx.route("**/v1/usage*", route => route.fulfill({ json: { buckets: [] } }));
  await ctx.route("**/v1/settings", async route => {
    const r = route.request();
    if (r.method() === "POST") {
      const body = JSON.parse(r.postData() || "{}");
      if ("usage_cache_reads" in body) stored = body.usage_cache_reads;
    }
    await route.fulfill({ json: { usage_cache_reads: stored, board_skin: "harbour", board_skins: SKINS } });
  });
  try {
    await sp.goto(base, { waitUntil: "domcontentloaded" });
    await sp.waitForSelector("#stack-list .stackrow", { state: "attached", timeout: slow(15000) });
    await sp.evaluate(() => switchView("usage"));
    await sp.waitForFunction(() => document.getElementById("uc-body") && typeof ucPaint === "function", null, { timeout: slow(10000) });
    await usageSettled(sp);
    if (process.env.USAGE_SHOT_ONLY) { await usageShot(sp, process.env.USAGE_SHOT_ONLY); return; }

    // by card and by group: one neutral fill, and totals that follow the toggle
    const tiles = await sp.evaluate(() => {
      const now = new Date(2026, 8, 15, 15, 30).getTime();
      UC.now = now; UC.range = "24h"; UC.bw = 900; UC.since = now - 86400000; UC.group = "dept"; UC.groupKey = null;
      const mk = (i, o, r) => ({ rows: 1, replies: 1, input: i, output: o, cache_write_5m: 0, cache_write_1h: 0, cache_read: r, cost: 0 });
      const t0 = Math.floor(now / 900000) * 900000 - 900000;
      const sA = mk(1000, 500, 90000), sB = mk(200, 100, 40000);
      const bucket = t => ({ t, total: ucAdd(ucAdd(ucSums(), sA), sB), cards: { "uc-a": ucAdd(ucSums(), sA), "uc-b": ucAdd(ucSums(), sB) },
        causes: { operator: ucAdd(ucAdd(ucSums(), sA), sB) }, groups: { eng: ucAdd(ucSums(), sA), ops: ucAdd(ucSums(), sB) } });
      UC.rooms = { "": { state: "ok", why: "", buckets: new Map([[t0, bucket(t0)]]), noGroups: false } };
      const out = {};
      for (const on of [false, true]) {
        UC.cacheReads = on;
        ucPaint();
        const q = sel => [...document.querySelectorAll(sel)];
        const readFill = getComputedStyle(document.querySelector("#uc-body .uclegend .uck-read") || document.querySelector(".uck-read")).backgroundColor;
        const cards = q('#uc-body .ucminis:not([data-role=groups]) .ucmini[data-id]');
        const groups = q('#uc-body [data-role=groups] .ucmini');
        const rects = q("#uc-body .ucmini svg rect");
        out[on] = {
          rectClasses: [...new Set(rects.map(r => r.getAttribute("class")))],
          rectFills: [...new Set(rects.map(r => getComputedStyle(r).fill))],
          readFill,
          cardTotals: cards.map(c => c.querySelector("b").textContent),
          groupTotals: groups.map(c => c.querySelector("b").textContent),
          want: { a: on ? ucTokens(sA) : ucCounted(sA), b: on ? ucTokens(sB) : ucCounted(sB) },
          fmt: usageTokens(on ? ucTokens(sA) : ucCounted(sA)) + "|" + usageTokens(on ? ucTokens(sB) : ucCounted(sB)),
          nCards: cards.length, nGroups: groups.length,
        };
      }
      return out;
    });
    for (const on of ["false", "true"]) {
      const s = tiles[on];
      if (s.nCards !== 2 || s.nGroups !== 2) fail("usageTab: cards " + s.nCards + " groups " + s.nGroups + " with cache reads " + on);
      if (s.rectClasses.join() !== "uck-bar") fail("usageTab: by-card and group bars use " + s.rectClasses + " with cache reads " + on);
      if (s.rectFills.includes(s.readFill) || s.rectFills.length !== 1 || !s.rectFills[0]) fail("usageTab: the bar fill " + s.rectFills + " is the cache read colour " + s.readFill);
      if (s.cardTotals.join("|") !== s.fmt) fail("usageTab: card totals " + s.cardTotals + " want " + s.fmt + " with cache reads " + on);
      if (s.groupTotals.join("|") !== s.fmt) fail("usageTab: group totals " + s.groupTotals + " want " + s.fmt + " with cache reads " + on);
    }
    if (tiles.false.cardTotals.join() === tiles.true.cardTotals.join()) fail("usageTab: the totals ignore the toggle.");

    // the toggle says its state: on a click and on a settings event from another tab
    const label = () => sp.evaluate(() => ({ t: document.getElementById("uc-cache-label").textContent,
      p: document.getElementById("uc-cache").getAttribute("aria-pressed"), tip: document.getElementById("uc-cache").getAttribute("data-tip") }));
    await sp.evaluate(() => { UC.cacheReads = false; ucPaint(); });
    let l = await label();
    if (l.t !== "cache reads hidden" || l.p !== "false" || !/counted apart/.test(l.tip)) fail("usageTab: off reads " + JSON.stringify(l));
    // the click's POST answer lands in ucHaveSetting late and would overwrite the settings event below
    await sp.evaluate(() => { window.__posted = new Promise(r => { const o = ucHaveSetting; ucHaveSetting = s => { o(s); r(); }; }); });
    await sp.click("#uc-cache");
    await sp.waitForFunction(() => document.getElementById("uc-cache-label").textContent === "cache reads shown", null, { timeout: slow(5000) });
    await sp.evaluate(() => window.__posted);
    l = await label();
    if (l.p !== "true" || !/drawn in the charts/.test(l.tip)) fail("usageTab: on reads " + JSON.stringify(l));
    if (!(await sp.evaluate(() => !!document.querySelector("#uc-cache i.uck-read")))) fail("usageTab: the swatch is gone.");
    await sp.evaluate(() => ucHaveSetting({ usage_cache_reads: false }));
    l = await label();
    if (l.t !== "cache reads hidden" || l.p !== "false") fail("usageTab: a settings event off reads " + JSON.stringify(l));
    await sp.evaluate(() => ucHaveSetting({ usage_cache_reads: true }));
    l = await label();
    if (l.t !== "cache reads shown" || l.p !== "true") fail("usageTab: a settings event on reads " + JSON.stringify(l));

    // the chart's running total equals the range total on the by-kind line in a real paint
    const paint = await sp.evaluate(() => {
      const now = new Date(2026, 8, 15, 15, 30).getTime();
      UC.now = now; UC.range = "24h"; UC.bw = 900; UC.since = now - 86400000; UC.group = "card";
      // the same two cards again: the toggle above may have had the tab re-read, which is the empty answer
      const mk = (i, o, r) => ({ rows: 1, replies: 1, input: i, output: o, cache_write_5m: 0, cache_write_1h: 0, cache_read: r, cost: 0 });
      const t0 = Math.floor(now / 900000) * 900000 - 900000;
      const sA = mk(1000, 500, 90000), sB = mk(200, 100, 40000);
      const all = ucAdd(ucAdd(ucSums(), sA), sB);
      UC.rooms = { "": { state: "ok", why: "", noGroups: false, buckets: new Map([[t0, { t: t0, total: all,
        cards: { "uc-a": ucAdd(ucSums(), sA), "uc-b": ucAdd(ucSums(), sB) }, causes: { operator: all }, groups: {} }]]) } };
      const out = [];
      for (const on of [false, true]) {
        UC.cacheReads = on;
        ucPaint();
        const kinds = [...document.querySelectorAll('#uc-body [data-n^="kind:"]')].map(e => e.textContent);
        const h = [...document.querySelectorAll("#uc-body h4")].map(e => e.textContent.split(" ")[0] + " " + e.textContent.split(" ")[1]);
        out.push({ total: ((document.querySelector("#uc-body [data-n=corrun]") || {}).textContent || "").replace(/ counted$/, ""), kinds, h,
          want: usageTokens(on ? 131800 : 1800) });
      }
      UC.now = 0;
      return out;
    });
    for (const [i, on] of ["off", "on"].entries()) {
      const p = paint[i];
      if (p.total !== p.want) fail("usageTab: the chart counts " + p.total + ", the range total is " + p.want + " with cache reads " + on);
      const order = p.h.join("|");
      if (order !== "tokens and|by card|burn rate|tokens by") fail("usageTab: heading order " + order);
    }

    if (process.env.USAGE_SHOT) await usageShot(sp, process.env.USAGE_SHOT);
  } finally {
    await ctx.close();
  }
  if (errors.length) fail("usageTab: the page threw: " + errors.join(" | "));
}

// The usage tab's weekly reset: a "this week" range from the last reset, the reset marked on the charts it
// crosses, the pace line naming the next one, and the setting that says when it is. NOTHING HERE READS THE REAL
// CLOCK: UC.now is set. The reset is worked out in its own zone, but the pace line's "by midnight" is the browser's,
// so that is pinned below.
// USAGE_WEEK_SHOT=<file.png> also writes a screenshot of the tab, for the before and after pictures.
async function usageWeekResetSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 1500 }, timezoneId: "America/New_York" });
  const sp = await ctx.newPage();
  const errors = [], reads = [], posts = [];
  sp.on("pageerror", e => errors.push(String(e)));
  let stored = { day: "sun", time: "18:00", tz: "America/New_York" };
  await ctx.route("**/v1/usage?*", route => { reads.push(route.request().url()); route.fulfill({ json: { buckets: [] } }); });
  await ctx.route("**/v1/settings", async route => {
    const r = route.request();
    if (r.method() === "POST") {
      const body = JSON.parse(r.postData() || "{}");
      posts.push(body);
      if (body.usage_week_reset) {
        if (body.usage_week_reset.tz === "Mars/Olympus") { await route.fulfill({ status: 400, json: { error: "tz is not a time zone name" } }); return; }
        stored = body.usage_week_reset;
      }
    }
    await route.fulfill({ json: { usage_cache_reads: false, usage_week_reset: stored, board_skin: "harbour", board_skins: SKINS } });
  });
  try {
    await sp.goto(base, { waitUntil: "domcontentloaded" });
    await sp.waitForSelector("#stack-list .stackrow", { state: "attached", timeout: slow(15000) });
    await sp.evaluate(() => switchView("usage"));
    await sp.waitForFunction(() => document.getElementById("uc-body") && typeof ucPaint === "function", null, { timeout: slow(10000) });
    await usageSettled(sp);

    // the tab on Monday 12:00 EDT: last week's turns, then this week's. 40k an hour since the reset.
    const draw = range => sp.evaluate(range => {
      const now = Date.UTC(2026, 9, 5, 16, 0), reset = Date.UTC(2026, 9, 4, 22, 0);
      UC.now = now; UC.cacheReads = false; UC.group = "card"; UC.range = range; UC.room = ""; UC.card = null;
      UC.bw = range === "7d" ? 3600 : 900;
      const span = { "1h": 3600e3, "6h": 21600e3, "24h": 86400e3, "7d": 604800e3, week: now - reset }[range];
      UC.since = now - span;
      const ms = UC.bw * 1000, buckets = new Map();
      for (let t = Math.floor(UC.since / ms) * ms; t < now; t += ms) {
        const v = t < reset ? 50000 : 10000 * ms / 900000;
        const s = ucAdd(ucSums(), { rows: 1, replies: 1, input: v, output: 0, cache_write_5m: 0, cache_write_1h: 0, cache_read: 0, cost: 0 });
        buckets.set(t, { t, total: s, cards: { "uc-a": ucAdd(ucSums(), s) }, causes: { operator: s }, groups: {} });
      }
      UC.rooms = { "": { state: "ok", why: "", buckets, noGroups: false } };
      ucPaint();
      const q = sel => [...document.querySelectorAll(sel)];
      return {
        on: q("#uc-ranges button.on").map(b => b.dataset.range).join(),
        corMarks: q(".ucchart[data-chart=correlate] .ucwkresetln").map(l => Number(l.dataset.at)),
        burnMarks: q(".ucchart[data-chart=burn] .ucwkresetln").map(l => Number(l.dataset.at)),
        label: (document.querySelector(".ucchart[data-chart=burn] .ucwkresetlab") || {}).textContent || "",
        total: (document.querySelector("#uc-body [data-n=corrun]") || {}).textContent || "",
      };
    }, range);

    // USAGE_WEEK_SHOT_ONLY=<file.png> with USAGE_WEEK_RANGE draws the picture and checks nothing, so the "before"
    // can be taken from a board that has no weekly reset. The old board has no "week" range, so it is asked for 7d.
    if (process.env.USAGE_WEEK_SHOT_ONLY) {
      await draw(process.env.USAGE_WEEK_RANGE || "week");
      await settleStyle(sp);
      await sp.screenshot({ path: process.env.USAGE_WEEK_SHOT_ONLY, fullPage: true });
      return;
    }

    // the reset itself: New York Sunday 18:00, on both sides of the clock change, and a zone that is not New York
    const r = await sp.evaluate(() => {
      const o = {};
      UC.weekReset = { day: "sun", time: "18:00", tz: "America/New_York" };
      o.edt = ucWeekSince(Date.UTC(2026, 9, 5, 16, 0)) === Date.UTC(2026, 9, 4, 22, 0);          // Mon 12:00 EDT
      o.sameDay = ucWeekSince(Date.UTC(2026, 9, 4, 21, 59)) === Date.UTC(2026, 9, 4 - 7, 22, 0); // 17:59 on the day: last week's
      o.atReset = ucWeekSince(Date.UTC(2026, 9, 4, 22, 0)) === Date.UTC(2026, 9, 4, 22, 0);
      o.est = ucWeekSince(Date.UTC(2026, 10, 2, 17, 0)) === Date.UTC(2026, 10, 1, 23, 0);         // Nov 1 is EST: 23:00 UTC
      o.next = ucWeekNext(Date.UTC(2026, 9, 5, 16, 0)) === Date.UTC(2026, 9, 11, 22, 0);
      UC.weekReset = { day: "mon", time: "09:30", tz: "Europe/London" };
      o.london = ucWeekSince(Date.UTC(2026, 9, 7, 12, 0)) === Date.UTC(2026, 9, 5, 8, 30);         // BST until Oct 25
      UC.weekReset = { day: "sun", time: "18:00", tz: "Mars/Olympus" };
      o.bad = ucWeekResets(Date.UTC(2026, 9, 5, 16, 0), 0).length === 0;
      UC.weekReset = { day: "sun", time: "18:00", tz: "America/New_York" };
      return o;
    });
    for (const [k, v] of Object.entries(r)) if (!v) fail("usageWeekReset: the reset worked out wrong: " + k);

    const reset = Date.UTC(2026, 9, 4, 22, 0);
    const wk = await draw("week");
    if (wk.on !== "week") fail("usageWeekReset: the range on is " + wk.on);
    // 18 hours at 40k an hour: 720k. last week's turns are not in it.
    if (wk.total !== "720k counted") fail("usageWeekReset: this week totals " + wk.total + ", want 720k with last week left out");
    if (wk.burnMarks.join() !== String(reset)) fail("usageWeekReset: this week's mark " + wk.burnMarks);
    if (wk.label !== "weekly reset Sun 18:00 EDT") fail("usageWeekReset: the mark reads '" + wk.label + "'");

    const w7 = await draw("7d");
    if (w7.corMarks.join() !== String(reset) || w7.burnMarks.join() !== String(reset)) fail("usageWeekReset: 7d marks " + w7.corMarks + " and " + w7.burnMarks);
    const d24 = await draw("24h");
    if (d24.corMarks.join() !== String(reset) || d24.burnMarks.join() !== String(reset)) fail("usageWeekReset: 24h crosses the reset and shows " + d24.corMarks + " and " + d24.burnMarks);
    const h1 = await draw("1h");
    if (h1.corMarks.length || h1.burnMarks.length) fail("usageWeekReset: 1h does not cross the reset and shows a mark");

    // a click on "this week" asks the room for turns since the reset
    await sp.evaluate(() => { UC.now = Date.UTC(2026, 9, 5, 16, 0); });
    reads.length = 0;
    await sp.click("#uc-week");
    await sp.waitForFunction(() => UC.range === "week" && !UC.inflight, null, { timeout: slow(5000) });
    const since = reads.map(u => new URL(u).searchParams.get("since")).pop();
    if (since !== "2026-10-04T22:00:00.000Z") fail("usageWeekReset: this week asked since " + since);

    // the setting: the form reads it, saves it, a bad zone is refused in the form, and a settings event moves it
    await sp.evaluate(() => { ulAdd({ at: Date.now(), room: "", card: "uc-a", kind: "weekly", pct: 5, reset: 0 }); ucPaint(); });
    await sp.click("#uc-reset");
    const f = await sp.evaluate(() => ({ shown: !document.getElementById("uc-reset-form").hidden, day: document.getElementById("uc-reset-day").value,
      time: document.getElementById("uc-reset-time").value, tz: document.getElementById("uc-reset-tz").value, label: document.getElementById("uc-reset-label").textContent }));
    if (!f.shown || f.day !== "sun" || f.time !== "18:00" || f.tz !== "America/New_York" || !/^resets Sun 18:00 E[SD]T$/.test(f.label)) fail("usageWeekReset: the form reads " + JSON.stringify(f));
    await sp.fill("#uc-reset-tz", "Mars/Olympus");
    await sp.click("#uc-reset-save");
    await sp.waitForFunction(() => document.getElementById("uc-reset-err").textContent);
    if (await sp.evaluate(() => document.getElementById("uc-reset-form").hidden)) fail("usageWeekReset: a refused zone closed the form.");
    await sp.selectOption("#uc-reset-day", "mon");
    await sp.fill("#uc-reset-time", "09:30");
    await sp.fill("#uc-reset-tz", "Europe/London");
    await sp.click("#uc-reset-save");
    await sp.waitForFunction(() => document.getElementById("uc-reset-form").hidden);
    const sent = posts.filter(b => b.usage_week_reset).pop().usage_week_reset;
    if (sent.day !== "mon" || sent.time !== "09:30" || sent.tz !== "Europe/London") fail("usageWeekReset: the board sent " + JSON.stringify(sent));
    await sp.waitForFunction(() => UC.weekReset.day === "mon" && !UC.inflight, null, { timeout: slow(5000) });
    const since2 = reads.map(u => new URL(u).searchParams.get("since")).pop();
    if (since2 !== "2026-10-05T08:30:00.000Z") fail("usageWeekReset: after the change this week asked since " + since2);
    await sp.evaluate(() => ucHaveSetting({ usage_week_reset: { day: "fri", time: "07:00", tz: "UTC" } }));
    if (await sp.evaluate(() => UC.weekReset.day + UC.weekReset.tz) !== "friUTC") fail("usageWeekReset: a settings event did not move the reset.");

    if (process.env.USAGE_WEEK_SHOT) {
      await sp.evaluate(() => { UC.weekReset = { day: "sun", time: "18:00", tz: "America/New_York" }; });
      await draw(process.env.USAGE_WEEK_RANGE || "week");
      await settleStyle(sp);
      await sp.screenshot({ path: process.env.USAGE_WEEK_SHOT, fullPage: true });
    }
  } finally {
    await ctx.close();
  }
  if (errors.length) fail("usageWeekReset: the page threw: " + errors.join(" | "));
}

// The usage tab grouped by department or director, and tokens per accepted item (u-014).
async function usageGroupsSection(browser, base) {
  const errors = [];
  const at = new Date(Math.floor(Date.now() / 900000) * 900000 - 900000).toISOString();
  const mk = n => ({ rows: 1, replies: 1, input: n, output: 0, cache_write_5m: 0, cache_write_1h: 0, cache_read: n * 10, cost: 0 });
  const total = mk(3000), sA = mk(2000), sB = mk(1000);
  const groupsFor = g => g === "dept" ? { "": mk(500), eng: mk(2000), "@before": mk(1000) }
    : { "": mk(700), atlas: mk(2300) };
  const itemsBody = { items: [
    { item: "u-005", title: "review tab design", counted: 2100000, cache_read: 88000000, cards: 3, split: 1 },
    { item: "r-004", title: "worktree reaper fix", counted: 4800000, cache_read: 210000000, cards: 2, split: 2 },
    { item: "t-002", title: "banner copies", counted: 1200000, cache_read: 40000000, cards: 1, split: 1 }], unlinked: 2 };
  const run = async (opts, fn) => {
    const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
    const sp = await ctx.newPage();
    sp.on("pageerror", e => errors.push(String(e)));
    const reads = [];
    await ctx.route(/\/v1\/usage\?/, route => {
      const u = new URL(route.request().url());
      const g = u.searchParams.get("group") || "";
      reads.push({ group: g, at: Date.now() });
      const b = { t: at, total, cards: { "uc-a": sA, "uc-b": sB }, causes: { operator: total } };
      if (g && !opts.old) b.groups = groupsFor(g);
      route.fulfill({ json: { buckets: [b] } });
    });
    await ctx.route(/\/v1\/usage\/items/, route =>
      opts.items404 ? route.fulfill({ status: 404, body: "not found" }) : route.fulfill({ json: itemsBody }));
    await ctx.route("**/v1/settings", route => route.fulfill({ json: { usage_cache_reads: false, board_skin: "harbour", board_skins: SKINS } }));
    try {
      await sp.clock.install();
      await sp.goto(base, { waitUntil: "domcontentloaded" });
      await sp.waitForSelector("#stack-list .stackrow", { state: "attached", timeout: slow(15000) });
      await sp.evaluate(() => switchView("usage"));
      await sp.waitForSelector("#uc-body .ucchart[data-chart=burn] rect", { timeout: slow(10000) });
      await fakePause(sp);
      await fakeTick(sp, 1500);
      await fn(sp, reads);
    } finally { await ctx.close(); }
  };
  const tiles = sp => sp.evaluate(() => [...document.querySelectorAll("#uc-body [data-role=groups] .ucmini")]
    .map(m => m.querySelector(".uctitle").textContent));
  const pick = async (sp, v) => {
    await sp.evaluate(v => { UC.group = v; UC.groupKey = null; UC.stale = false; loadUsageTab(); }, v);
    await fakeWait(sp, v => UC.group === v && !UC.inflight, v, 5000);
    await fakeTick(sp, 200);
  };
  await run({}, async (sp, reads) => {
    if (await sp.$("#uc-body [data-role=groups]")) fail("usageGroups: card view draws group tiles.");
    if (reads.some(r => r.group)) fail("usageGroups: card view sent a group.");
    await sp.evaluate(() => { lastTasks = [
      { id: "uc-a", display_title: "alpha", tags: ["dept:eng"] },
      { id: "uc-b", display_title: "beta", tags: [], spawned_by: "atlas" }].concat(lastTasks || []); });
    await pick(sp, "dept");
    if (!reads.some(r => r.group === "dept")) fail("usageGroups: department sent no group=dept: " + JSON.stringify(reads.map(r => r.group)));
    const t = await tiles(sp);
    if (t.join("|") !== "eng|before grouping|no department") fail("usageGroups: dept tiles are " + t.join("|"));
    await sp.click('#uc-body .ucmini[data-group="eng"]');
    const cards = await sp.evaluate(() => [...document.querySelectorAll("#uc-body .ucmini[data-id]")].map(m => m.dataset.id));
    if (cards.join() !== "uc-a") fail("usageGroups: a click on eng left cards " + cards);
    await sp.click('#uc-body .ucmini[data-group="eng"]');
    const all = await sp.evaluate(() => document.querySelectorAll("#uc-body .ucmini[data-id]").length);
    if (all !== 2) fail("usageGroups: a second click did not clear the filter: " + all);
    await pick(sp, "launcher");
    if (!reads.some(r => r.group === "launcher")) fail("usageGroups: director sent no group=launcher.");
    const l = await tiles(sp);
    if (l.join("|") !== "atlas|you") fail("usageGroups: director tiles are " + l.join("|"));
    await sp.click('#uc-body .ucmini[data-group="atlas"]');
    const c2 = await sp.evaluate(() => [...document.querySelectorAll("#uc-body .ucmini[data-id]")].map(m => m.dataset.id));
    if (c2.join() !== "uc-b") fail("usageGroups: a click on atlas left cards " + c2);
    await sp.evaluate(() => { UC.cacheReads = true; UC.groupKey = null; ucPaint(); });
    if ((await tiles(sp)).join("|") !== "atlas|you") fail("usageGroups: tiles lost their order with cache reads on.");
    await sp.evaluate(() => { UC.cacheReads = false; });
    await pick(sp, "card");
    if (reads[reads.length - 1].group !== "") fail("usageGroups: back on card still sends a group.");
    const items = await sp.evaluate(() => (UC.sub = "reports", ucPaint(), {
      text: (document.querySelector("#uc-body .ucitems") || {}).textContent || "",
      n: document.querySelectorAll("#uc-body .ucitems > span:nth-child(4n+2)").length,
      foot: (document.querySelector("#uc-body [data-role=items-foot]") || {}).textContent || "",
      tips: [...document.querySelectorAll("#uc-body .ucitems > span[data-tip]")].map(s => s.getAttribute("data-tip")) }));
    if (items.n !== 3) fail("usageGroups: the items table has " + items.n + " rows.");
    if (!/median 2\.1M per item · 3 items accepted/.test(items.foot)) fail("usageGroups: the footer reads " + items.foot);
    if (!/2 accepted items have no card on record/.test(items.foot)) fail("usageGroups: no unlinked line: " + items.foot);
    if (items.tips.length !== 4 || !items.tips.every(t => /shared/.test(t))) fail("usageGroups: the split hint is " + JSON.stringify(items.tips));
    if (items.text.indexOf("r-004") > items.text.indexOf("u-005")) fail("usageGroups: items are not biggest first.");
  });
  await run({ old: true }, async sp => {
    await pick(sp, "dept");
    const s = await sp.evaluate(() => ({ note: (document.querySelector("#uc-body [data-role=nogroups]") || {}).textContent || "",
      tiles: document.querySelectorAll("#uc-body [data-role=groups] .ucmini").length,
      cards: document.querySelectorAll("#uc-body .ucmini[data-id]").length }));
    if (!/does not group usage yet/.test(s.note)) fail("usageGroups: a room without groups says " + s.note);
    if (s.tiles !== 0 || s.cards !== 2) fail("usageGroups: a room without groups drew " + JSON.stringify(s));
  });
  await run({ items404: true }, async sp => {
    const s = await sp.evaluate(() => (UC.sub = "reports", ucPaint(), { old: (document.querySelector("#uc-body [data-role=items-old]") || {}).textContent || "",
      table: !!document.querySelector("#uc-body .ucitems") }));
    if (!/too old/.test(s.old) || s.table) fail("usageGroups: a 404 on items gave " + JSON.stringify(s));
  });
  await run({}, async (sp, reads) => {
    await pick(sp, "dept");
    reads.length = 0;
    for (let i = 0; i < 6; i++) {
      await sp.evaluate(() => { for (let k = 0; k < 10; k++) onUsageEvent({ data: JSON.stringify({ task_id: "uc-a", ended_at: new Date().toISOString(), input: 1, output: 1, cause: "operator" }) }); });
      await fakeTick(sp, 1000);
    }
    await fakeTick(sp, 500);
    const n = reads.filter(r => r.group === "dept").length;
    if (n !== 1) fail("usageGroups: a burst of usage events for 6.5s gave " + n + " re-reads, want 1.");
  });
  if (errors.length) fail("usageGroups: the page threw: " + errors.join(" | "));
}

// The usage tab's polish: the too-old line with and without a build, the keep-alive
// phrase on the cache line, the backfilled label, the fade and the grow on a live
// event, and reduced motion turning them off.
// The usage tab's limits: the bars, the 5h band, and the flameout. See js/usage-limits.js.
async function usageLimitsSection(browser, base) {
  const errors = [];
  const at = new Date(Math.floor(Date.now() / 900000) * 900000 - 900000).toISOString();
  const total = { rows: 2, replies: 9, input: 1000, output: 2000, cache_write_5m: 3000, cache_write_1h: 4000, cache_read: 90000, cost: 0 };
  const iso = ms => new Date(Date.now() + ms).toISOString();
  const MIN = 60000, HOUR = 3600000;
  const run = async (limits, fn) => {
    const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
    const sp = await ctx.newPage();
    sp.on("pageerror", e => errors.push(String(e)));
    const asked = [];
    await ctx.route("**/v1/usage*", route => route.fulfill({ json: { buckets: [
      { t: at, total, cards: { "uc-a": total }, causes: { operator: total } }] } }));
    await ctx.route("**/v1/usage/limits*", route => { asked.push(route.request().url()); return limits(route); });
    await ctx.route("**/v1/settings", route => route.fulfill({ json: { usage_cache_reads: false, board_skin: "harbour", board_skins: SKINS } }));
    try {
      await sp.goto(base, { waitUntil: "domcontentloaded" });
      await sp.waitForSelector("#stack-list .stackrow", { state: "attached", timeout: slow(15000) });
      await sp.evaluate(() => switchView("usage"));
      await sp.waitForSelector("#uc-body .ucchart[data-chart=burn] rect", { timeout: slow(10000) });
      await sp.waitForFunction(() => document.getElementById("conn").classList.contains("live") && !UC.inflight, null, { timeout: slow(15000) }); // the stream opening reloads the tab once
      await sp.evaluate(() => { UC.sub = "reports"; ucPaint(); });
      await sp.waitForSelector("#uc-body #ul-limits", { timeout: slow(5000) });
      await fn(sp, asked);
    } finally { await ctx.close(); }
  };
  const rowsText = sp => sp.evaluate(() => [...document.querySelectorAll("#ul-limits .ulrow")].map(r => ({
    kind: r.dataset.kind, none: r.classList.contains("ulnone"), text: r.textContent, proj: (r.querySelector(".ulproj") || {}).textContent || "",
    cls: (r.querySelector(".ulproj") || { className: "" }).className, tip: (r.querySelector(".ulproj") || { dataset: {} }).dataset.tip || "",
    fill: (r.querySelector(".ulfill") || { style: {} }).style.width })));
  // Readings from the room, in the shape internal/api/limits.go answers.
  const reading = (agoMin, card, kind, pct, resetsInMs) => ({ at: iso(-agoMin * MIN), card, kind, pct,
    ...(resetsInMs == null ? {} : { resets_at: iso(resetsInMs) }) });
  const answer = readings => route => route.fulfill({ json: { readings } });
  // The rows are drawn from what the page holds; a scenario sets that and repaints.
  const set = (sp, rs) => sp.evaluate(rs => {
    UL.readings = []; UL.last.clear(); UL.html = "";
    for (const r of rs) ulAdd({ at: Date.now() - r.agoMin * 60000, room: "", card: r.card, kind: r.kind, pct: r.pct, reset: Date.now() + r.resetMs });
    ucPaint();
  }, rs);
  const line = (n, pct, agoFirst, agoLast, resetMs) => {
    const out = [];
    for (let i = 0; i < n; i++) out.push({ agoMin: agoFirst + (agoLast - agoFirst) * i / (n - 1), card: "c1", kind: "five_hour", pct: Math.round(pct[0] + (pct[1] - pct[0]) * i / (n - 1)), resetMs });
    return out;
  };
  const five = async sp => (await rowsText(sp)).find(x => x.kind === "five_hour");
  const bands = sp => sp.evaluate(() => {
    UC.sub = "charts"; ucPaint();
    const b = document.querySelector("#uc-body .ulband");
    const out = { n: document.querySelectorAll("#uc-body .ulband").length, label: (document.querySelector("#uc-body .ulbandlab") || {}).textContent,
      fill: b ? getComputedStyle(b).fill : "" };
    UC.sub = "reports"; ucPaint();
    return out;
  });

  // The bar, the card, the dash, and the band at 6h+ and not at 1h.
  await run(answer([reading(3, "sa-orch", "five_hour", 64, 156 * MIN), reading(8, "u-005", "weekly", 31, 3 * 24 * HOUR)]), async (sp, asked) => {
    if (!asked.some(u => /since=/.test(u))) fail("usageLimits: the readings were not asked for: " + asked);
    const rows = await rowsText(sp);
    const f = rows.find(r => r.kind === "five_hour"), week = rows.find(r => r.kind === "weekly");
    if (!f || f.none || f.fill !== "64%" || !/64%/.test(f.text) || !/sa-orch/.test(f.text) || !/3m ago/.test(f.text) || !/resets .*\(in 2h 36m\)/.test(f.text))
      fail("usageLimits: the 5h row reads " + JSON.stringify(f));
    if (!week || week.none || !/31%/.test(week.text) || !/u-005/.test(week.text)) fail("usageLimits: the week row reads " + JSON.stringify(week));
    const band = await bands(sp);
    if (band.n !== 1 || band.label !== "5h window") fail("usageLimits: the 24h chart has no 5h band: " + JSON.stringify(band));
    if (!/rgba?\(/.test(band.fill)) fail("usageLimits: the band has no fill: " + band.fill);
    await sp.evaluate(() => { UC.sub = "charts"; UC.range = "1h"; loadUsageTab(); });
    await sp.waitForFunction(() => !UC.inflight && document.querySelector("#uc-body .ucchart"), null, { timeout: slow(5000) });
    if (await sp.evaluate(() => document.querySelectorAll("#uc-body .ulband, #uc-body .ulbandlab").length)) fail("usageLimits: the 1h chart has a 5h band.");
    await sp.evaluate(() => { UC.range = "6h"; loadUsageTab(); });
    await sp.waitForFunction(() => !UC.inflight && document.querySelector("#uc-body .ulband"), null, { timeout: slow(5000) });
  });

  // Nothing recent is a dash on both rows, never a zero.
  await run(answer([reading(90, "old", "five_hour", 50, 60 * MIN)]), async sp => {
    const rows = await rowsText(sp);
    if (rows.length !== 2 || !rows.every(r => r.none && /–/.test(r.text) && !/0%/.test(r.text))) fail("usageLimits: nothing recent should be two dashes: " + JSON.stringify(rows));
    if ((await bands(sp)).n) fail("usageLimits: a band with no recent reading.");
  });

  // Two reset times are two accounts and two rows, never summed or blended. The projections run on this page.
  await run(answer([reading(3, "a1", "five_hour", 64, 156 * MIN), reading(4, "b1", "five_hour", 20, 250 * MIN)]), async sp => {
    const rows = (await rowsText(sp)).filter(r => r.kind === "five_hour");
    if (rows.length !== 2 || rows[0].fill !== "64%" || rows[1].fill !== "20%") fail("usageLimits: two resets should give two rows: " + JSON.stringify(rows));
    if ((await bands(sp)).n !== 2) fail("usageLimits: two windows should draw two bands.");

    // Warn: 100% lands before the reset.
    await set(sp, line(6, [40, 60], 60, 3, 156 * MIN));
    let r = await five(sp);
    // A projection past midnight names the day ("100% at Wed 00:10"), so the day is optional.
    if (!/at this pace: 100% at (\w{3} )?\d\d:\d\d, .* before the reset/.test(r.proj) || !/flameout before reset/.test(r.proj) || !/ulwarn/.test(r.cls) || /uldanger/.test(r.cls))
      fail("usageLimits: warn line reads " + JSON.stringify(r));
    if (!/it does not see the future/.test(r.tip) || !/spend outside atrium/.test(r.tip)) fail("usageLimits: the honesty text is missing: " + r.tip);
    // Danger: within 30 minutes.
    await set(sp, line(6, [65, 95], 60, 3, 156 * MIN));
    r = await five(sp);
    if (!/flameout before reset/.test(r.proj) || !/uldanger/.test(r.cls)) fail("usageLimits: danger line reads " + JSON.stringify(r));
    // Not this window: slow, so 100% would land after the reset.
    await set(sp, line(6, [45, 50], 60, 3, 120 * MIN));
    r = await five(sp);
    if (!/not this window/.test(r.proj) || /flameout/.test(r.proj) || /ulwarn|uldanger/.test(r.cls)) fail("usageLimits: not-this-window reads " + JSON.stringify(r));
    // A flat pace projects nothing.
    await set(sp, line(5, [50, 50], 40, 3, 156 * MIN));
    r = await five(sp);
    if (r.proj !== "no pace to project") fail("usageLimits: a flat pace reads " + JSON.stringify(r));
    // Scattered readings say rough.
    await set(sp, [[60, 30], [50, 55], [40, 35], [30, 70], [20, 45], [3, 80]].map(([m, p]) => ({ agoMin: m, card: "c1", kind: "five_hour", pct: p, resetMs: 156 * MIN })));
    r = await five(sp);
    if (!/rough/.test(r.proj)) fail("usageLimits: scattered readings should say rough: " + JSON.stringify(r));
    // Too few readings: the token-burn fallback, marked as such. Every minute of the range holds 1000 counted tokens.
    await sp.evaluate(() => {
      UC.range = "6h"; UC.bw = 60; UC.since = Date.now() - 3 * 3600000;
      const b = new Map();
      for (let t = Math.floor(UC.since / 60000) * 60000; t <= Date.now(); t += 60000)
        b.set(t, { t, total: Object.assign(ucSums(), { input: 1000 }), cards: {}, causes: {} });
      UC.rooms = { "": { state: "ok", why: "", buckets: b } };
    });
    await set(sp, [{ agoMin: 40, card: "c1", kind: "five_hour", pct: 50, resetMs: 240 * MIN }, { agoMin: 2, card: "c1", kind: "five_hour", pct: 60, resetMs: 240 * MIN }]);
    r = await five(sp);
    if (!/\(estimated from token burn\)/.test(r.proj) || !/flameout before reset/.test(r.proj) || !/can only undercount/.test(r.tip))
      fail("usageLimits: the fallback reads " + JSON.stringify(r));
    // One reading has nothing to project.
    await set(sp, [{ agoMin: 2, card: "c1", kind: "five_hour", pct: 60, resetMs: 240 * MIN }]);
    r = await five(sp);
    if (r.proj !== "no pace to project") fail("usageLimits: one reading reads " + JSON.stringify(r));
    // Nothing notifies.
    if (await sp.evaluate(() => document.querySelectorAll(".toast").length)) fail("usageLimits: the flameout raised a toast.");
  });

  // An older room answers 404: live telemetry only, and nothing says it is an error.
  await run(route => route.fulfill({ status: 404, body: "404 page not found" }), async sp => {
    let rows = await rowsText(sp);
    if (!rows.every(r => r.none)) fail("usageLimits: a 404 should leave dashes: " + JSON.stringify(rows));
    if (await sp.evaluate(() => document.querySelectorAll("#uc-body .ucmissing").length)) fail("usageLimits: a 404 from limits showed a message.");
    await sp.evaluate(() => {
      lastTasks = [{ id: "live-1", display_title: "livecard", status: "running", telemetry: { seconds: 5, five_hour: { pct: 42, resets_at: new Date(Date.now() + 2 * 3600000).toISOString() } } }];
      ulOnCards();
    });
    rows = await rowsText(sp);
    const f = rows.find(r => r.kind === "five_hour");
    if (!f || f.none || f.fill !== "42%" || !/livecard/.test(f.text)) fail("usageLimits: live telemetry did not draw after a 404: " + JSON.stringify(rows));
  });
  if (errors.length) fail("usageLimits: the page threw: " + errors.join(" | "));
}

async function usagePolishSection(browser, base) {
  const errors = [];
  const at = new Date(Math.floor(Date.now() / 900000) * 900000 - 900000).toISOString();
  const sums = { rows: 2, replies: 9, input: 1000, output: 2000, cache_write_5m: 3000, cache_write_1h: 4000, cache_read: 90000, cost: 0 };
  const ka = { rows: 3, replies: 3, input: 100, output: 200, cache_write_5m: 0, cache_write_1h: 0, cache_read: 5000, cost: 0 };
  const bf = { rows: 4, replies: 4, input: 10, output: 20, cache_write_5m: 0, cache_write_1h: 0, cache_read: 50, cost: 0 };
  const total = { rows: 9, replies: 16, input: 1110, output: 2220, cache_write_5m: 3000, cache_write_1h: 4000, cache_read: 95050, cost: 0 };
  const reloaded = async sp => {
    await sp.evaluate(() => { window.__items0 = UC.items; window.__turns0 = UC.turns; loadUsageTab(); });
    await sp.waitForFunction(() => !UC.inflight && UC.items !== window.__items0 && UC.turns !== window.__turns0, null, { timeout: slow(10000), polling: 10 });
  };
  const run = async (reduced, fn) => {
    const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 }, reducedMotion: reduced ? "reduce" : "no-preference" });
    const sp = await ctx.newPage();
    sp.on("pageerror", e => errors.push(String(e)));
    await ctx.route("**/v1/usage*", route => route.fulfill({ json: { buckets: [
      { t: at, total, cards: { "uc-a": total }, causes: { operator: sums, keepalive: ka, backfill: bf } }] } }));
    // a name arriving late repaints without the fade, so the card has none to ask for
    await ctx.route("**/v1/tasks/uc-a", route => route.fulfill({ status: 404, body: "" }));
    await ctx.route("**/v1/settings", route => route.fulfill({ json: { usage_cache_reads: false, board_skin: "harbour", board_skins: SKINS } }));
    try {
      await sp.goto(base, { waitUntil: "domcontentloaded" });
      await sp.waitForSelector("#stack-list .stackrow", { state: "attached", timeout: slow(15000) });
      await sp.evaluate(() => switchView("usage"));
      await sp.waitForSelector("#uc-body .ucchart[data-chart=burn] rect", { timeout: slow(10000) });
      await sp.waitForFunction(() => document.getElementById("conn").classList.contains("live") && !UC.inflight, null, { timeout: slow(15000) }); // the stream opening reloads the tab once
      await fn(sp);
    } finally { await ctx.close(); }
  };
  await run(false, async sp => {
    await sp.evaluate(() => { lastTasks = [{ id: "uc-a", keepalive: { refreshes: 2 } }].concat(lastTasks || []); ucPaint(); });
    const line = await sp.textContent("#uc-body .uccacheline");
    if (!/keep-alive spent 300 counted and kept 1 card warm/.test(line)) fail("usagePolish: the keep-alive phrase reads " + line);
    const tip = await sp.getAttribute("#uc-body .uccacheline", "data-tip");
    if (!tip) fail("usagePolish: the cache line has no hint.");
    await sp.evaluate(() => { UC.sub = "reports"; ucPaint(); });
    const causes = await sp.textContent("#uc-body .uccauses");
    if (!/backfilled/.test(causes) || /\bbackfill\b/.test(causes)) fail("usagePolish: the backfill cause reads " + causes);
    const bt = await sp.evaluate(() => USAGE_TIPS.cause_backfill);
    if (!/atrium usage backfill/.test(bt)) fail("usagePolish: the backfilled hint is " + bt);
    // Every number on the tab carries a hint.
    const bare = await sp.evaluate(() => [...document.querySelectorAll("#uc-body [data-n]")]
      .filter(e => !e.closest("[data-tip]") && !e.getAttribute("data-tip")).map(e => e.getAttribute("data-n")));
    if (bare.length) fail("usagePolish: numbers with no data-tip: " + bare);
    // The too-old line, with a build and without.
    const old = await sp.evaluate(() => {
      UC.sub = "charts"; hubRooms = [{ name: "room-d", version: "2f715b5a9c1d" }]; hubInventory = [];
      UC.rooms = { "room-d": { state: "old", why: "too old to keep usage", buckets: new Map() },
        "room-e": { state: "old", why: "too old to keep usage", buckets: new Map() } };
      ucPaint();
      return [...document.querySelectorAll("#uc-body .ucmissing")].map(e => e.textContent);
    });
    if (!old.includes("room-d build 2f715b5 predates usage (needs 39a8da1 or later). Update the room to see its usage here."))
      fail("usagePolish: the too-old line with a build reads " + JSON.stringify(old));
    if (!old.includes("room-e predates usage (needs 39a8da1 or later). Update the room to see its usage here."))
      fail("usagePolish: the too-old line without a build reads " + JSON.stringify(old));
    // A live row fades the number that changed and grows a new bar.
    await reloaded(sp);
    await sp.waitForSelector("#uc-body .ucchart[data-chart=burn] rect", { timeout: slow(10000) });
    await sp.evaluate(() => {
      ucApply({ task_id: "uc-a", ended_at: new Date().toISOString(), input: 500, output: 500, cache_write_5m: 0, cache_write_1h: 0, cache_read: 0 });
      ucSchedulePaint();
    });
    await sp.waitForSelector("#uc-body .ucfade", { state: "attached", timeout: slow(10000) });
    const anim = await sp.evaluate(() => ({
      fade: getComputedStyle(document.querySelector("#uc-body .ucfade")).animationName,
      grow: (document.querySelector("#uc-body g.ucgrow") ? getComputedStyle(document.querySelector("#uc-body g.ucgrow")).animationName : "none") }));
    if (anim.fade !== "ucfade") fail("usagePolish: a changed number does not fade: " + anim.fade);
    if (anim.grow !== "ucgrow") fail("usagePolish: a new bar does not grow: " + anim.grow);
  });
  await run(true, async sp => {
    await reloaded(sp);
    await sp.evaluate(() => {
      ucApply({ task_id: "uc-a", ended_at: new Date().toISOString(), input: 500, output: 500, cache_write_5m: 0, cache_write_1h: 0, cache_read: 0 });
      ucSchedulePaint();
    });
    await sp.waitForSelector("#uc-body .ucfade", { state: "attached", timeout: slow(10000) });
    const anim = await sp.evaluate(() => getComputedStyle(document.querySelector("#uc-body .ucfade")).animationName);
    if (anim !== "none") fail("usagePolish: reduced motion left the fade on: " + anim);
    const grow = await sp.evaluate(() => [...document.querySelectorAll("#uc-body g.ucgrow")]
      .map(g => getComputedStyle(g).animationName).filter(n => n !== "none"));
    if (grow.length) fail("usagePolish: reduced motion left the grow on: " + grow);
  });
  if (errors.length) fail("usagePolish: the page threw: " + errors.join(" | "));
}

// The suite's one clock (see CLOCK_OFFSET at the top). The page starts the run at
// noon or at HEADLESS_CLOCK, a window the page opens agrees, Node and the page
// read the same time, and the clock still advances. A context that asked for the
// real clock does not get the shift.
async function clockSection(browser, base) {
  const spec = process.env.HEADLESS_CLOCK || "12:00";
  const want = spec === "real" ? null : spec.split(":").map(Number);
  // Minutes past midnight, compared round the clock so 23:58 and 00:01 are near.
  const apart = (h, m, h2, m2) => { const d = Math.abs((h * 60 + m) - (h2 * 60 + m2)); return Math.min(d, 1440 - d); };
  const t0 = new Date(RUN_T0);
  if (want && apart(t0.getHours(), t0.getMinutes(), want[0], want[1]) > 1) {
    fail("clock: the run started at " + t0.getHours() + ":" + t0.getMinutes() + ", not " + spec);
  }
  // The page against Node's shifted clock now, since this section runs however far into the run it falls.
  const near = (h, m) => { const n = new Date(); return apart(h, m, n.getHours(), n.getMinutes()) <= 1; };
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  try {
    await p.goto(base, { waitUntil: "domcontentloaded" });
    const read = pg => pg.evaluate(() => ({
      h: new Date().getHours(), m: new Date().getMinutes(), now: Date.now(),
      inst: new Date() instanceof Date, name: Date.name, str: typeof Date(),
      dated: new Date(0).getTime(), parsed: Date.parse("2026-01-01T00:00:00Z"),
      proto: Object.getPrototypeOf(new Date()) === Date.prototype,
    }));
    const a = await read(p);
    if (want && !near(a.h, a.m)) fail("clock: the page reads " + a.h + ":" + a.m + ", not near " + spec);
    if (Math.abs(a.now - Date.now()) > 2000) fail("clock: the page and Node disagree by " + (a.now - Date.now()) + "ms");
    if (!a.inst || a.name !== "Date" || a.str !== "string" || a.dated !== 0 || !a.proto ||
        a.parsed !== Date.UTC(2026, 0, 1)) fail("clock: Date is not itself any more: " + JSON.stringify(a));
    const popup = p.waitForEvent("popup", { timeout: slow(10000) });
    await p.evaluate(b => { window.open(b + "/#term=s1"); }, base);
    const w = await popup;
    const wa = await read(w);
    if (want && !near(wa.h, wa.m)) fail("clock: a pop-out window reads " + wa.h + ":" + wa.m + ", not near " + spec);
    if (Math.abs(wa.now - Date.now()) > 2000) fail("clock: a pop-out window disagrees with Node by " + (wa.now - Date.now()) + "ms");
    await p.waitForFunction(t0 => Date.now() - t0 >= 400, a.now, { timeout: slow(5000) });
    const b = await read(p);
    if (b.now - a.now < 300 || b.now - a.now > 3000) fail("clock: time did not advance sanely: " + (b.now - a.now) + "ms in 400ms");
    if (CLOCK_OFFSET) {
      const rctx = await browser.newContext({ viewport: { width: 800, height: 600 } }, { realClock: true });
      const rp = await rctx.newPage();
      await rp.goto(base, { waitUntil: "domcontentloaded" });
      const r = await rp.evaluate(() => Date.now());
      await rctx.close();
      const drift = Math.abs(r - (Date.now() - CLOCK_OFFSET));
      if (drift > 2000) fail("clock: a realClock context was shifted, off by " + drift + "ms");
    }
  } finally {
    await ctx.close();
  }
}

// Past pages and the tokens-and-percent chart: the pager steps to yesterday and asks with until, the percents ride
// a second axis, one cursor reads tokens and both percents, and the live-only sections stay off a past page.
async function usageHistorySection(browser, base) {
  const errors = [];
  const HOUR = 3600000;
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const sp = await ctx.newPage();
  sp.on("pageerror", e => errors.push(String(e)));
  const asked = [];
  const tot = n => ({ rows: 1, replies: 1, input: n, output: 0, cache_write_5m: 0, cache_write_1h: 0, cache_read: 0, cost: 0 });
  await ctx.route("**/v1/usage*", route => {
    const u = new URL(route.request().url());
    asked.push(u.search);
    if (!u.searchParams.get("since")) return route.fulfill({ json: {} });
    const since = Date.parse(u.searchParams.get("since")), bw = Number(u.searchParams.get("bucket")) * 1000;
    const t = new Date(Math.floor((since + 2 * HOUR) / bw) * bw).toISOString();
    return route.fulfill({ json: { buckets: [{ t, total: tot(3000), cards: { "uc-a": tot(2000), "uc-b": tot(1000) }, causes: { operator: tot(3000) } }] } });
  });
  await ctx.route("**/v1/usage/limits*", route => {
    const u = new URL(route.request().url());
    const at = Date.parse(u.searchParams.get("since")) + 7 * 24 * HOUR + HOUR;
    return route.fulfill({ json: { readings: [
      { at: new Date(at).toISOString(), card: "uc-a", kind: "weekly", pct: 40, resets_at: new Date(at + 5 * 24 * HOUR).toISOString() },
      { at: new Date(at).toISOString(), card: "uc-a", kind: "five_hour", pct: 100, resets_at: new Date(at + 3 * HOUR).toISOString() }] } });
  });
  await ctx.route("**/v1/settings", route => route.fulfill({ json: { usage_cache_reads: false, board_skin: "harbour", board_skins: SKINS } }));
  try {
    await sp.goto(base, { waitUntil: "domcontentloaded" });
    await sp.waitForSelector("#stack-list .stackrow", { state: "attached", timeout: slow(15000) });
    await sp.evaluate(() => { UC.range = "24h"; switchView("usage"); });
    await usageSettled(sp);
    await sp.waitForSelector("#uc-body .ucchart[data-chart=correlate]", { timeout: slow(10000) });

    const pure = await sp.evaluate(() => {
      const now = Date.now(), wk = ucPage("week", -1, now), day = ucPage("24h", -1, now);
      const mid = new Date(now); mid.setHours(0, 0, 0, 0);
      const H = 3600000;
      const steps = ucPctSteps([{ at: 0, room: "", card: "a", kind: "five_hour", pct: 30, reset: 2 * H },
        { at: H, room: "", card: "b", kind: "five_hour", pct: 70, reset: 4 * H }], "five_hour", 0, 6 * H);
      return { wkEnds: wk.until === ucWeekSince(now), wkLong: wk.until - wk.since > 6 * 24 * H,
        dayEnds: day.until === mid.getTime(), live: ucPage("week", 0, now).until, steps };
    });
    if (!pure.wkEnds || !pure.wkLong || !pure.dayEnds || pure.live !== 0) fail("usageHistory: the pages are off: " + JSON.stringify(pure));
    if (JSON.stringify(pure.steps) !== JSON.stringify([[0, 30], [3600000, 70], [14400000, 0]])) fail("usageHistory: the percent steps read " + JSON.stringify(pure.steps));

    const state = () => sp.evaluate(() => ({
      label: document.getElementById("uc-pagelab").textContent, next: document.getElementById("uc-next").disabled,
      pw: document.querySelectorAll("#uc-body [data-chart=correlate] path.ucpw").length,
      p5: document.querySelectorAll("#uc-body [data-chart=correlate] path.ucp5").length,
      tok: document.querySelectorAll("#uc-body [data-chart=correlate] path.uck-line").length,
      hit: document.querySelectorAll("#uc-body [data-chart=correlate] .ulhit").length,
      top: document.querySelectorAll("#uc-body .ucmini[data-id]").length,
      limits: !!document.getElementById("ul-limits"), cum: !!document.querySelector("#uc-body [data-chart=cumulative]"),
      read: document.querySelector("#uc-body [data-chart=correlate] .ucread").textContent }));
    let s = await state();
    if (s.label !== "last 24h" || !s.next || s.limits || s.cum || s.pw !== 1 || s.p5 !== 0 || s.top !== 2 || s.read !== "")
      fail("usageHistory: the live page reads " + JSON.stringify(s));
    const reports = () => sp.evaluate(() => {
      document.querySelector("#uc-sub [data-sub=reports]").click();
      const r = { limits: !!document.getElementById("ul-limits"), causes: !!document.querySelector("#uc-body .uccauses") };
      document.querySelector("#uc-sub [data-sub=charts]").click();
      return r;
    });
    const rp = await reports();
    if (!rp.limits || !rp.causes) fail("usageHistory: the live reports read " + JSON.stringify(rp));

    asked.length = 0;
    await sp.click("#uc-prev");
    await sp.waitForFunction(() => UC.off === -1 && UC.until && !UC.inflight, null, { timeout: slow(5000) });
    s = await state();
    if (!asked.some(q => /until=/.test(q))) fail("usageHistory: yesterday was not asked with until: " + asked.join(" "));
    if (!/^yesterday · \w{3} \w{3} \d+$/.test(s.label) || s.next || s.limits || s.cum || s.pw !== 1)
      fail("usageHistory: yesterday reads " + JSON.stringify(s));
    if ((await reports()).limits) fail("usageHistory: yesterday's reports show the live limits.");

    await sp.click("#uc-pick [data-line=p5]");
    await sp.waitForFunction(() => document.querySelector("#uc-body [data-chart=correlate] path.ucp5"), null, { timeout: slow(5000) });
    s = await state();
    if (s.hit !== 1) fail("usageHistory: the 5h line at 100% drew no hit mark: " + JSON.stringify(s));

    const tag = await sp.evaluate(() => {
      const st = document.querySelector("#uc-body [data-chart=correlate] .ucstage"), r = st.getBoundingClientRect();
      const x = r.left + r.width * ((2 + 7 / 60) / 24);
      st.dispatchEvent(new PointerEvent("pointermove", { bubbles: true, clientX: x, clientY: r.top + 20 }));
      const t = st.querySelector(".uchovtag");
      return { hidden: t.hidden, text: t.textContent, dots: st.querySelectorAll(".uchovdot:not([hidden])").length };
    });
    if (tag.hidden || !/\d\d-\d\d \d\d:\d\d\n\S+ tokens\n5h 100%\nweek 40%\n\S+\/h/.test(tag.text) || /top: /.test(tag.text) || tag.dots !== 3)
      fail("usageHistory: the cursor reads " + JSON.stringify(tag));

    await sp.click("#uc-pick [data-line=tok]");
    await sp.waitForFunction(() => !document.querySelector("#uc-body [data-chart=correlate] path.uck-line"), null, { timeout: slow(5000) });

    await sp.click("#uc-now");
    await sp.waitForFunction(() => UC.off === 0 && !UC.until && !UC.inflight, null, { timeout: slow(5000) });
  } finally { await ctx.close(); }
  if (errors.length) fail("usageHistory: the page threw: " + errors.join(" | "));
}

// A ROOM NO CARD HAS REPORTED FROM says so. No limits bar of dashes, no weekly reset to set, and opening the tab logs no
// missing renderer, nor does a repaint on capabilities or items. The first reading draws the tab as usual.
async function usageNeverSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const sp = await ctx.newPage();
  const errors = [], logs = [];
  sp.on("pageerror", e => errors.push(String(e)));
  sp.on("console", m => { if (/no renderer/.test(m.text())) logs.push(m.text()); });
  await ctx.route("**/v1/usage*", route => route.fulfill({ json: { buckets: [] } }));
  try {
    await sp.goto(base, { waitUntil: "domcontentloaded" });
    await sp.waitForSelector("#stack-list .stackrow", { state: "attached", timeout: slow(15000) });
    await sp.evaluate(() => document.querySelector('header .tab[data-view="usage"]').click());
    await sp.waitForFunction(() => document.getElementById("uc-body") && typeof ucPaint === "function", null, { timeout: slow(10000) });
    await usageSettled(sp);
    const read = () => sp.evaluate(() => {
      const body = document.getElementById("uc-body");
      return { never: !!body.querySelector(".ucnever"), text: body.textContent, limits: !!document.getElementById("ul-limits"),
        reset: !document.getElementById("uc-reset").parentElement.hidden };
    });
    await sp.evaluate(() => { UL.readings.length = 0; UL.last.clear(); ucPaint(); });
    const a = await read();
    if (!a.never || a.limits || a.reset || /resets|–|--/.test(a.text)) fail("usageNever: an empty room does not say so plainly: " + JSON.stringify(a));
    await sp.evaluate(() => { ulAdd({ at: Date.now(), room: "", card: "uc-a", kind: "weekly", pct: 5, reset: 0 }); ulRefresh(); });
    const b = await read();
    if (b.never || !b.reset) fail("usageNever: a reading does not draw the tab: " + JSON.stringify(b));
    await sp.evaluate(async () => { for (const v of ["caps", "items"]) { switchView(v); await repaintLists(); } switchView("usage"); });
    if (logs.length) fail("usageNever: " + logs.join(" | "));
    if (errors.length) fail("usageNever: page errors: " + errors.join(" | "));
  } finally { await ctx.close(); }
}

const units = [
  { name: "usageHistory", run: ({ browser, base }) => usageHistorySection(browser, base) },
  { name: "usageCacheReads", run: ({ browser, base }) => usageCacheReadsSection(browser, base) },
  { name: "usageTurns", run: ({ browser, base }) => usageTurnsSection(browser, base) },
  { name: "usagePolish", run: ({ browser, base }) => usagePolishSection(browser, base) },
  { name: "usageLimits", run: ({ browser, base }) => usageLimitsSection(browser, base) },
  { name: "usageGroups", run: ({ browser, base }) => usageGroupsSection(browser, base) },
  { name: "contextSize", run: ({ browser, base }) => contextSizeSection(browser, base) },
  { name: "ctxLine", run: ({ browser, base }) => ctxLineSection(browser, base) },
  { name: "ctxLimitLayers", run: ({ browser, base }) => ctxLimitLayersSection(browser, base) },
  { name: "contextCycle", run: ({ browser, base }) => contextCycleSection(browser, base) },
  { name: "cacheChip", run: ({ browser, base }) => cacheChipSection(browser, base) },
  { name: "clock", run: ({ browser, base }) => clockSection(browser, base) },
  { name: "usageTab", run: ({ browser, base }) => usageTabSection(browser, base) },
  { name: "usageWeekReset", run: ({ browser, base }) => usageWeekResetSection(browser, base) },
  { name: "usageNever", run: ({ browser, base }) => usageNeverSection(browser, base) },
];
const sections = { usageHistory: usageHistorySection, usageCacheReads: usageCacheReadsSection, usageTurns: usageTurnsSection, usagePolish: usagePolishSection, usageLimits: usageLimitsSection, usageGroups: usageGroupsSection, contextSize: contextSizeSection, ctxLine: ctxLineSection, ctxLimitLayers: ctxLimitLayersSection, contextCycle: contextCycleSection, cacheChip: cacheChipSection, clock: clockSection, usageTab: usageTabSection, usageWeekReset: usageWeekResetSection, usageNever: usageNeverSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
