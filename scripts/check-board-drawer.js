// The filter drawer, in a real browser with every endpoint mocked (docs/backlog/ui/u-011-interview.md).
//
//   NODE_PATH=<dir with playwright> node scripts/check-board-drawer.js
//
// The nine steps the interview asks for, on the stack, the board and the terminals tab, at 1280px with a mouse and at
// 390px with a finger. Skips itself, exit 0, when Playwright is not installed, like the other browser checks.
//
//   1  no stored state: the drawer is closed, the line shows the summary and the search box, the defaults are in force
//   2  ctrl-/ with nothing focused: the search box has focus, the drawer is still closed, typing narrows the list
//   3  a click on the line outside the search box opens it over the list, which did not move or resize
//   4  a click on a chip applies at once, its count is right, the drawer stays open, `filters N` went up by one
//   5  a click on a card below the drawer closes it and the card did NOT open (and does when the drawer is shut)
//   6  open it again and Esc: it closes and focus is back where it was
//   7  each tab keeps its own sort, show and group across a tab switch (and a reload)
//   8  390px, touch: a quarter of the way down snaps back, half opens, a swipe up closes, a drag on the list scrolls
//   9  with an xterm focused, ctrl-/ goes to the terminal and not the search box
//
// And the second round of the review: a right-click or a scrolled touch outside leaves nothing armed, the close handle
// answers Enter and Space, the line and drawer are still there on a phone with a terminal attached (hidden only by
// full screen), and the attached card stays on all three tabs through search, chips and the subagent rollup.

const http = require("http");
const fs = require("fs");
const path = require("path");
const { wholeBoard } = require("./board-source.js");

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) {
  try { ({ chromium } = require("playwright")); } catch (e2) { console.log("playwright is not installed"); process.exit(0); }
}

const WEB = path.join(__dirname, "..", "internal", "api", "web");
const HTML = wholeBoard();
const TYPES = { ".js": "text/javascript", ".css": "text/css", ".png": "image/png", ".svg": "image/svg+xml",
  ".gif": "image/gif", ".woff2": "font/woff2" };

// ── the cards ───────────────────────────────────────────────────────────────
const at = mins => new Date(Date.now() - mins * 60000).toISOString();
const card = (id, title, status, extra) => Object.assign({
  id, display_title: title, status, runner: "claude", rank: 1, worktree: "/w/" + id, why: "", idle_seconds: 0,
  wait_seconds: 0, created_at: at(600), last_activity_at: at(5), tags: [], supervised: false, offline: false,
  pinned: false, auto_approve: false
}, extra || {});
const SUB = "atrium:subagent";
const TASKS = [
  card("r1", "alpha ready one", "needs-input", { supervised: true, last_activity_at: at(1) }),
  card("r2", "beta ready two", "needs-input", { supervised: true, last_activity_at: at(2) }),
  card("r3", "gamma ready three", "needs-input", { supervised: true, last_activity_at: at(3) }),
  ...Array.from({ length: 14 }, (_, i) => card("f" + i, "filler " + i, "needs-input", { supervised: true, last_activity_at: at(10 + i) })),
  card("p1", "parent run", "needs-input", { supervised: true, last_activity_at: at(1), worktree: "/w/parent" }),
  card("c1", "child one", "running", { supervised: true, tags: [SUB], spawned_by_id: "p1", worktree: "/w/parent/c1" }),
  card("c2", "child two", "running", { supervised: true, tags: [SUB], spawned_by_id: "p1", worktree: "/w/parent/c2" }),
  card("n1", "second runner", "running", { supervised: true }),
  card("w1", "needs permission", "needs-permission", { supervised: true }),
  card("i1", "inbox item", "backlog"),
  card("d1", "finished one", "done"),
  card("d2", "finished two", "done"),
  card("s1", "paused one", "paused"),
  card("s2", "paused two", "paused"),
  card("k1", "pinned and gone", "dead", { pinned: true }),
  card("e1", "ended worker", "done", { worktree: "/w/ended" })
];
const byStatus = (...ss) => TASKS.filter(t => ss.includes(t.status) && !(t.tags || []).includes(SUB));

// ── the server ──────────────────────────────────────────────────────────────
let cardGets = [];
let seen = [];
const server = http.createServer((req, res) => {
  const p = new URL(req.url, "http://x").pathname;
  seen.push(req.method + " " + p);
  const json = o => { res.writeHead(200, { "Content-Type": "application/json" }); res.end(JSON.stringify(o)); };
  if (p === "/" || p === "/index.html") { res.writeHead(200, { "Content-Type": "text/html" }); return res.end(HTML); }
  if ((p.startsWith("/vendor/") || p.startsWith("/css/") || p === "/working.gif") && !p.includes("..")) {
    const f = path.join(WEB, p);
    if (fs.existsSync(f) && fs.statSync(f).isFile()) {
      res.writeHead(200, { "Content-Type": TYPES[path.extname(f)] || "application/octet-stream" });
      return res.end(fs.readFileSync(f));
    }
    res.writeHead(404); return res.end("");
  }
  if (p.startsWith("/v1/events")) { res.writeHead(200, { "Content-Type": "text/event-stream" }); return res.write(": open\n\n"); }
  if (p === "/v1/tasks") return json({ tasks: TASKS });
  const one = /^\/v1\/tasks\/([^/]+)$/.exec(p);
  if (one) { cardGets.push(one[1]); const t = TASKS.find(x => x.id === one[1]); return t ? json(t) : json({}); }
  if (p === "/v1/health") return json({ build: "check", settling: false, halted: false });
  if (p === "/v1/settings") return json({ global_auto: false, board_skin: "harbour", board_skins: ["harbour"], scrollback_lines: 5000 });
  if (p === "/v1/permissions") return json({ permissions: [] });
  if (p === "/v1/harnesses") return json({ harnesses: [] });
  return json({});
});

let failed = 0;
let onFail = null;   // a step's way to say why, printed under the FAIL line and never starting with it
const check = (ok, what) => { console.log((ok ? "ok   " : "FAIL ") + what); if (!ok) { failed++; if (onFail) onFail(); } };
const sleep = ms => new Promise(r => setTimeout(r, ms));
// Reads until `ok` holds or `ms` have passed, and answers the last read: a step waits on the state it asserts.
const until = async (read, ok, ms = 3000) => {
  const t0 = Date.now();
  let v = await read();
  while (!ok(v) && Date.now() - t0 < ms) { await sleep(50); v = await read(); }
  return v;
};

// The point a click should land on, read again until two reads agree: a drawer's slide and an accordion's re-flow move it.
const still = async (read, ms = 3000) => {
  const t0 = Date.now();
  let a = await read();
  while (Date.now() - t0 < ms) {
    await sleep(10);
    const b = await read();
    if (a && b && Math.abs(a.x - b.x) < 0.5 && Math.abs(a.y - b.y) < 0.5) return b;
    a = b;
  }
  return a;
};

// ── what each tab is, to the steps ──────────────────────────────────────────
// `rows` are the cards a person would click, `chip` the one step 4 presses and what its count must read.
const TABS = {
  stack: {
    view: "stack", host: "fd-stack", q: "stack-q", rows: "#stack-list .stackrow",
    chip: { v: "running", count: byStatus("running").length, rowsAfter: null },
    sort: { id: "stack-sort", to: "name" },
    defaults: async page => ({ on: await chipsOn(page, "stack-seg") }),
    wantDefault: ["needs-input"]
  },
  board: {
    view: "board", host: "board-bar", q: "board-q", rows: "#board .card[data-id]",
    chip: { v: "done", count: byStatus("done", "dead", "paused").length },
    sort: { id: "board-sort", to: "name" },
    wantDefault: ["backlog", "needs-you", "working", "done"]
  },
  terms: {
    view: "terms", host: "fd-terms", q: "term-q", rows: "#term-list .card.tab",
    chip: { v: "paused", count: byStatus("paused").length },
    sort: { id: "term-sort", to: "name" },
    wantDefault: []
  }
};

async function chipsOn(page, seg) {
  return page.evaluate(id => [...document.querySelectorAll("#" + id + " button.on")].map(b => b.dataset.v), seg);
}
const sameSet = (a, b) => a.length === b.length && a.every(x => b.includes(x));

// One finger or one mouse, whichever the width calls for.
function pointer(page, mobile) {
  return {
    async click(x, y) { if (mobile) await page.touchscreen.tap(x, y); else await page.mouse.click(x, y); await sleep(250); },
    async clickOn(loc, position) { if (mobile) await loc.tap({ position }); else await loc.click({ position }); await sleep(250); }
  };
}

const state = page => page.evaluate(() => {
  const h = [...document.querySelectorAll(".fdrawer")].filter(e => e.offsetParent !== null)[0];
  if (!h) return null;
  const panel = h.querySelector(".fpanel"), line = h.querySelector(".fline"), q = h.querySelector(".fq");
  const r = e => { const b = e.getBoundingClientRect(); return { x: b.x, y: b.y, w: b.width, h: b.height, b: b.bottom }; };
  return {
    tab: h.dataset.tab, open: h.classList.contains("open"), inert: panel.inert,
    panelVisible: getComputedStyle(panel).visibility === "visible" && panel.offsetHeight > 0,
    panel: r(panel), line: r(line), q: r(q), qid: q.id,
    sum: h.querySelector(".fsum").textContent, sumHidden: h.querySelector(".fsum").hidden,
    active: document.activeElement ? (document.activeElement.id || document.activeElement.tagName) : "",
    activeIsQ: document.activeElement === q
  };
});

const geometry = (page, sel) => page.evaluate(s => {
  const e = document.querySelector(s); if (!e) return null;
  const b = e.getBoundingClientRect(); return [Math.round(b.x), Math.round(b.y), Math.round(b.width), Math.round(b.height)].join(",");
}, sel);

async function open(browser, width, mobile) {
  const ctx = await browser.newContext(mobile
    ? { viewport: { width, height: 844 }, hasTouch: true, isMobile: true, deviceScaleFactor: 2 }
    : { viewport: { width, height: 900 } });
  // A real phone is sent to /m/ (the separate phone app, which has none of these tabs). This is the board at phone width.
  if (mobile) await ctx.addInitScript(() => { try { sessionStorage.setItem("atrium.m.desktop", "1"); } catch (e) {} });
  const page = await ctx.newPage();
  page.on("pageerror", e => check(false, "page error: " + e.message));
  await page.goto("http://127.0.0.1:" + server.address().port + "/");
  await page.waitForFunction(() => typeof switchView === "function" && document.querySelectorAll(".card, .stackrow").length > 0, null, { timeout: 15000 });
  // The terminals drawer is an accordion by default (FD_TERMS_PUSH, its own headless unit termsDrawerPush). These steps are
  // the overlay path: the terminal's box must not move, so flip the switch the way the unit does.
  await page.evaluate(() => {
    FD_TERMS_PUSH = false;
    const h = document.getElementById("fd-terms");
    if (h) h.classList.add("foverlay");
  });
  return { ctx, page };
}

async function goto(page, view) {
  await page.evaluate(v => switchView(v), view);
  await sleep(700);
}

// The point on the line that is not the search box: the line's own padding at the left on a desktop, where the summary
// fills the room. At phone width the search box takes everything the summary gave up, so it is the filters button.
async function linePoint(page) {
  const s = await state(page);
  if (s.sumHidden || s.q.x < s.line.x + 40) {
    const b = await page.evaluate(() => {
      const h = [...document.querySelectorAll(".fdrawer")].filter(e => e.offsetParent !== null)[0];
      const r = h.querySelector(".filtersbtn").getBoundingClientRect(); return { x: r.x + r.width / 2, y: r.y + r.height / 2 };
    });
    return b;
  }
  return { x: s.line.x + 3, y: s.line.y + s.line.h / 2 };
}

// Puts focus on a visible button that is not part of the drawer, and marks it, so "back where it was" can be told.
async function focusSomething(page) {
  const ok = await page.evaluate(() => {
    const b = [...document.querySelectorAll("header button, .tab, button")]
      .find(e => e.offsetParent !== null && !e.closest(".fdrawer") && e.tabIndex >= 0 && e.tagName === "BUTTON");
    if (!b) return false;
    b.dataset.checkFocus = "1"; b.focus(); return document.activeElement === b;
  });
  if (!ok) {
    check(false, "there is a visible button to put focus on");
    if (process.env.DRAWER_SHOT) await page.screenshot({ path: process.env.DRAWER_SHOT + page.viewportSize().width + ".png" });
  }
}

async function steps(browser, width, mobile, tabName) {
  const T = TABS[tabName];
  const tag = `${width}px ${tabName}`;
  const { ctx, page } = await open(browser, width, mobile);
  const mouse = pointer(page, mobile);
  await goto(page, T.view);

  // 1: no stored state ----------------------------------------------------------------------------
  let s = await state(page);
  check(!!s && s.tab === T.view, `${tag} 1: the drawer for this tab is on screen`);
  check(s && !s.open && s.inert && !s.panelVisible, `${tag} 1: closed on load, and its panel is out of reach`);
  check(s && s.q.w > 40 && s.q.h > 10, `${tag} 1: the search box is on the collapsed line`);
  check(s && (mobile ? s.sum.length > 0 : s.sum.length > 0 && !s.sumHidden), `${tag} 1: the line says what the tab is set to ("${s && s.sum}")`);
  let wantChips = null;
  if (tabName === "stack" || tabName === "board") {
    const seg = tabName === "stack" ? "stack-seg" : "board-seg";
    const on = await chipsOn(page, seg);
    check(sameSet(on, T.wantDefault), `${tag} 1: the default chips are ${JSON.stringify(T.wantDefault)} (${JSON.stringify(on)})`);
    wantChips = on;
  } else {
    const on = await chipsOn(page, "term-show");
    check(on.length === 0, `${tag} 1: no terminals chip is on by default (${JSON.stringify(on)})`);
    const rows = await page.$$eval("#term-list .card.tab", els => els.map(e => e.dataset.id));
    check(!rows.includes("s1") && !rows.includes("s2") && !rows.includes("k1"), `${tag} 1: no paused or dead rows (${rows.join(",")})`);
    check(await page.$$eval("#term-list .endedrow, #term-list .termended", e => e.length) === 0, `${tag} 1: no ended group`);
    const folded = await page.$$eval("#term-list .tkidfold.shut", e => e.length);
    check(folded === 1, `${tag} 1: the parent starts folded, with its count on the chevron (${folded})`);
    check(!rows.includes("c1") && !rows.includes("c2"), `${tag} 1: its subagents are not rows`);
  }
  if (tabName === "stack" || tabName === "board") {
    const subs = await page.evaluate(() => [...document.querySelectorAll(".chip.subs")].map(e => e.textContent));
    check(subs.length > 0 && /2 subagents/.test(subs[0]), `${tag} 1: the parent says "2 subagents" and the children are not cards (${subs})`);
  }

  // 2: ctrl-/ ----------------------------------------------------------------------------------------
  await page.evaluate(() => document.activeElement && document.activeElement.blur());
  const before = await page.$$eval(T.rows, e => e.length);
  await page.keyboard.press("Control+/");
  s = await state(page);
  check(s && s.activeIsQ && !s.open, `${tag} 2: ctrl-/ puts focus in the search box and leaves the drawer shut (${s && s.active})`);
  await page.keyboard.type("alpha");
  await sleep(300);
  const after = await page.$$eval(T.rows, e => e.length);
  check(after > 0 && after < before, `${tag} 2: typing narrows the list (${before} -> ${after})`);
  s = await state(page);
  check(s && !s.open, `${tag} 2: typing did not open the drawer`);
  // Esc clears the text, a second one leaves the box.
  await page.keyboard.press("Escape");
  check((await page.$eval("#" + T.q, e => e.value)) === "", `${tag} 2: Esc in the box clears it`);
  s = await state(page);
  check(s.activeIsQ, `${tag} 2: and stays in the box`);
  await page.keyboard.press("Escape");
  s = await state(page);
  check(!s.activeIsQ, `${tag} 2: a second Esc leaves the box`);
  await sleep(300);
  check((await page.$$eval(T.rows, e => e.length)) === before, `${tag} 2: the list is whole again (${before})`);

  // 3: a click on the line opens it, over the list ---------------------------------------------------------
  const listBox = T.view === "terms" ? "#term-list" : T.view === "stack" ? "#stack-list" : "#board";
  const geoBefore = await geometry(page, listBox);
  await focusSomething(page);
  let p = await linePoint(page);
  await mouse.click(p.x, p.y);
  s = await until(() => state(page), x => x && x.open && x.panelVisible && !x.inert && x.panel.y >= x.line.b - 2);
  check(s && s.open && s.panelVisible && !s.inert, `${tag} 3: a click on the line opens the drawer`);
  check(s && s.panel.y >= s.line.b - 2, `${tag} 3: it hangs under the line`);
  await sleep(300);   // the accordion's slide
  s = await state(page);
  const geoOpen = await geometry(page, listBox);
  if (T.view === "terms") check(geoBefore === geoOpen, `${tag} 3: the list's box did not move or resize (${geoBefore} / ${geoOpen})`);
  else {
    // The stack and the board are an accordion: the list is pushed down by the drawer, not covered by it.
    const top = g => Number(g.split(",")[1]);
    check(top(geoOpen) - top(geoBefore) >= s.panel.h - 16 && top(geoOpen) >= s.panel.b - 16,
      `${tag} 3: the list is pushed down by the drawer's height (${geoBefore} / ${geoOpen}, drawer ${s.panel.h})`);
  }
  const covers = await page.evaluate(() => {
    const p = document.querySelector(".fdrawer.open .fpanel").getBoundingClientRect();
    const e = document.elementFromPoint(p.x + p.width / 2, p.y + Math.min(30, p.height / 2));
    return !!(e && e.closest(".fpanel"));
  });
  if (T.view === "terms") check(covers, `${tag} 3: it is over the list, not beside it`);

  // 4: a chip ----------------------------------------------------------------------------------------------
  const chip = page.locator(`.fdrawer.open .seg button[data-v="${T.chip.v}"]`).first();
  const chipAt = await still(async () => {
    const r = await chip.boundingBox();
    return r && { x: r.x + r.width / 2, y: r.y + r.height / 2 };
  });
  await mouse.click(chipAt.x, chipAt.y);
  s = await state(page);
  check(s && s.open, `${tag} 4: a click inside the drawer leaves it open`);
  const n = await page.$eval(`.fdrawer.open .seg button[data-v="${T.chip.v}"] .pillcount`, e => e.textContent);
  check(Number(n) === T.chip.count, `${tag} 4: the ${T.chip.v} chip counts ${T.chip.count} (${n})`);
  const lit = await page.$eval(`.fdrawer.open .seg button[data-v="${T.chip.v}"]`, e => e.classList.contains("on"));
  if (tabName === "board") check(!lit, `${tag} 4: the chip went off`); else check(lit, `${tag} 4: the chip lit`);
  if (tabName === "stack") {
    const rows = await page.$$eval("#stack-list .stackrow", els => els.length);
    check(rows === byStatus("needs-input", "running").length, `${tag} 4: the list now holds ready and running (${rows})`);
  }
  if (tabName === "terms") {
    const rows = await page.$$eval("#term-list .card.tab", els => els.map(e => e.dataset.id));
    check(rows.includes("s1") && rows.includes("s2"), `${tag} 4: the paused rows are listed (${rows.join(",")})`);
  }

  let control = null;
  // 5: a click below the drawer is swallowed ----------------------------------------------------------------
  const target = await page.evaluate(rowsSel => {
    const p = document.querySelector(".fdrawer.open .fpanel").getBoundingClientRect();
    const rows = [...document.querySelectorAll(rowsSel)].filter(e => e.offsetParent !== null);
    const r = rows.map(e => ({ e, b: e.getBoundingClientRect() }))
      .find(x => x.b.top > p.bottom + 6 && x.b.bottom < innerHeight - 4 && x.b.left >= 0 && x.b.right <= innerWidth + 2);
    return r ? { id: r.e.dataset.id || "", dx: Math.min(40, r.b.width / 3), dy: Math.min(14, r.b.height / 2), x: r.b.left + Math.min(40, r.b.width / 3), y: r.b.top + Math.min(14, r.b.height / 2) } : null;
  }, T.rows);
  check(!!target, `${tag} 5: there is a card below the drawer to click`);
  if (target) {
    // A mouse resting on a terminal row warms its card with a fetch (js/terminal-list.js), so rest first and count from there.
    if (!mobile) { await page.mouse.move(target.x, target.y); await sleep(400); }
    cardGets = [];
    await mouse.click(target.x, target.y);
    s = await state(page);
    check(s && !s.open, `${tag} 5: the click outside closes the drawer`);
    const opened = () => page.evaluate(() => !!document.querySelector("dialog[open]") || (typeof termTask !== "undefined" && !!termTask));
    check(cardGets.length === 0 && !(await opened()), `${tag} 5: and the card did not open (fetched: ${cardGets.join(",") || "nothing"})`);
    control = target;

  }

  // 6: Esc closes it and gives the focus back --------------------------------------------------------------
  await focusSomething(page);
  p = await linePoint(page);
  await mouse.click(p.x, p.y);
  s = await state(page);
  check(s && s.open, `${tag} 6: opened again`);
  await page.keyboard.press("Escape");
  await sleep(200);
  s = await state(page);
  check(s && !s.open, `${tag} 6: Esc closes it`);
  check(await page.evaluate(() => !!(document.activeElement && document.activeElement.dataset.checkFocus)), `${tag} 6: and focus is back where it was (${s && s.active})`);

  // 9: a focused terminal keeps ctrl-/ --------------------------------------------------------------------------
  if (tabName === "terms") {
    await page.evaluate(() => {
      const host = document.createElement("div");
      host.id = "check-xterm";
      host.style.cssText = "position:fixed;left:0;bottom:0;width:300px;height:80px;z-index:1";
      document.body.appendChild(host);
      const t = new Terminal({ rows: 4, cols: 40 });
      t.open(host);
      t.focus();
    });
    const inX = await page.evaluate(() => !!(document.activeElement && document.activeElement.closest(".xterm")));
    check(inX, `${tag} 9: an xterm has focus`);
    await page.keyboard.press("Control+/");
    s = await state(page);
    check(s && !s.activeIsQ && !s.open, `${tag} 9: ctrl-/ did not take focus from the terminal (${s && s.active})`);
    check(await page.evaluate(() => !!document.activeElement.closest(".xterm")), `${tag} 9: the terminal still has it`);
  }
  // 5, the control, last because opening a card re-lays the page out for good: the same click with the drawer shut DOES
  // open it, or the swallow above proved nothing.
  if (control) {
    await page.evaluate(() => { const x = document.getElementById("check-xterm"); if (x) x.remove(); });
    // The row is found when the click happens: a stored point goes stale when the accordion or a chip re-lays the page out.
    const row = control.id ? page.locator("[data-id=\"" + control.id + "\"]:visible").first() : null;
    const at = { x: control.dx, y: control.dy };
    if (!mobile) { if (row) await row.hover({ position: at }); else await page.mouse.move(control.x, control.y); await sleep(400); }
    cardGets = [];
    seen = [];
    if (row) await mouse.clickOn(row, at); else await mouse.click(control.x, control.y);
    const opened = await until(
      () => page.evaluate(() => !!document.querySelector("dialog[open]") || (typeof termTask !== "undefined" && !!termTask)),
      v => v || cardGets.length > 0);
    const ok = cardGets.length > 0 || opened;
    const why = ok ? null : await page.evaluate(({ x, y, id }) => {
      const sel = e => e ? e.tagName.toLowerCase() + (e.id ? "#" + e.id : "") + [...e.classList].map(c => "." + c).join("") : "none";
      const t = [...document.querySelectorAll('[data-id="' + id + '"]')].find(e => e.offsetParent !== null) || null;
      const b = t && t.getBoundingClientRect(), top = document.elementFromPoint(x, y);
      const cs = t && getComputedStyle(t);
      return `click=(${Math.round(x)},${Math.round(y)}) target=${sel(t)} box=${b ? [b.x, b.y, b.width, b.height].map(Math.round).join(",") : "none"}` +
        ` visibility=${cs ? cs.visibility : "-"} display=${cs ? cs.display : "-"} pointer-events=${cs ? cs.pointerEvents : "-"}` +
        ` elementFromPoint=${sel(top)} covered=${!!(t && top && !t.contains(top) && !top.contains(t))} viewport=${innerWidth}x${innerHeight}` +
        ` dialogOpen=${!!document.querySelector("dialog[open]")} termTask=${typeof termTask !== "undefined" ? !!termTask : "n/a"}`;
    }, control);
    check(ok, `${tag} 5: the same click with the drawer shut does open it (fetched: ${cardGets.join(",") || "nothing"})`);
    if (!ok) {
      console.log("     click target: " + why);
      console.log("     drawer now: " + JSON.stringify(await state(page)));
      console.log("     requests since the click: " + (seen.join(", ") || "none"));
      console.log("     pending timers: the page does not expose them");
    }
  }
  await ctx.close();
}

// 7: each tab keeps its own ------------------------------------------------------------------------------------
async function keeps(browser, width, mobile) {
  const tag = `${width}px`;
  const { ctx, page } = await open(browser, width, mobile);
  const press = async (view, sel) => {
    await goto(page, view);
    const p = await linePoint(page);
    await pointer(page, mobile).click(p.x, p.y);
    const b = await still(async () => {
      const r = await page.locator(`.fdrawer.open ${sel}`).first().boundingBox();
      return r && { x: r.x + r.width / 2, y: r.y + r.height / 2 };
    });
    await pointer(page, mobile).click(b.x, b.y);
  };
  await press("stack", '#stack-sort button[data-sort="name"]');
  await press("stack", '#stack-seg button[data-v="finished"]');
  await press("stack", '#stack-group button[data-group="off"]');
  await press("board", '#board-sort button[data-sort="manual"]');
  await press("board", '#board-seg button[data-v="done"]');
  await press("board", '#board-group button[data-group="tag"]');
  await press("terms", '#term-sort button:nth-child(3)');
  await press("terms", '#term-show button[data-v="dead"]');
  await press("terms", '#term-drawer-group button[data-group="recency"]');

  const read = () => page.evaluate(() => {
    const on = id => [...document.querySelectorAll("#" + id + " button.on")].map(b => b.dataset.v || b.dataset.sort || b.dataset.group || b.textContent.trim()).join("|");
    return {
      stack: [on("stack-sort"), on("stack-seg"), on("stack-group")].join(" / "),
      board: [on("board-sort"), on("board-seg"), on("board-group")].join(" / "),
      terms: [on("term-sort"), on("term-show"), on("term-drawer-group")].join(" / ")
    };
  });
  const want = {
    stack: "name / needs-input|finished / off",
    board: "manual / backlog|needs-you|working / tag",
    terms: "started / dead / recency"
  };
  const got = await read();
  check(got.stack.startsWith("name / ") && got.stack.endsWith(" / off") && got.stack.includes("finished") && got.stack.includes("needs-input"),
    `${tag} 7: stack kept name, ready+finished, off (${got.stack})`);
  check(got.board.startsWith("manual / ") && got.board.endsWith(" / tag") && !got.board.includes("done"),
    `${tag} 7: board kept manual, no done column, by tag (${got.board})`);
  check(got.terms === want.terms, `${tag} 7: terminals kept started, dead, by age (${got.terms})`);
  // Away and back, then a reload.
  await goto(page, "board"); await goto(page, "stack"); await goto(page, "terms");
  const again = await read();
  check(JSON.stringify(again) === JSON.stringify(got), `${tag} 7: a switch from tab to tab changes none of them`);
  await page.reload();
  await page.waitForFunction(() => typeof switchView === "function" && document.querySelectorAll(".card, .stackrow").length > 0, null, { timeout: 15000 });
  await sleep(500);
  const reloaded = await read();
  check(JSON.stringify(reloaded) === JSON.stringify(got), `${tag} 7: a reload keeps them (${JSON.stringify(reloaded)})`);
  const nOpen = await page.evaluate(() => document.querySelectorAll(".fdrawer.open").length);
  check(nOpen === 0, `${tag} 7: and the drawer is shut again after it`);
  // The search text is the session's, per tab.
  await goto(page, "stack");
  await page.fill("#stack-q", "beta");
  await goto(page, "board");
  check((await page.$eval("#board-q", e => e.value)) === "", `${tag} 7: the board's search box did not inherit the stack's`);
  await goto(page, "stack");
  check((await page.$eval("#stack-q", e => e.value)) === "beta", `${tag} 7: the stack's search text is still there`);
  await ctx.close();
}

// 8: the finger ----------------------------------------------------------------------------------------------------
async function touch(browser, tabName) {
  const T = TABS[tabName];
  const tag = `390px ${tabName}`;
  const { ctx, page } = await open(browser, 390, true);
  const cdp = await ctx.newCDPSession(page);
  await goto(page, T.view);
  await page.evaluate(() => {
    window.__touches = 0;
    window.__last = null;
    ["touchstart", "touchmove", "touchend", "touchcancel"].forEach(t => document.addEventListener(t, e => {
      window.__touches++;
      const p = e.touches[0];
      window.__last = t + ":" + (p ? Math.round(p.clientY) : null);
    }, { capture: true, passive: true }));
  });
  const received = () => page.evaluate(() => ({ n: window.__touches, last: window.__last }));
  // The drag's `d` is private to filter-drawer.js, so `on` and `y1` come from the class and grid row it writes.
  const drawerNow = () => page.evaluate(() => {
    const h = [...document.querySelectorAll(".fdrawer")].filter(e => e.offsetParent !== null)[0];
    if (!h) return "no drawer";
    const panel = h.querySelector(".fpanel"), wrap = h.querySelector(".fwrap");
    return `open=${h.classList.contains("open")} on=${h.classList.contains("fdragging")}` +
      ` drawn=${getComputedStyle(panel).visibility === "visible" && panel.offsetHeight > 0}` +
      ` y1=${wrap.style.gridTemplateRows || panel.style.transform || "-"} h=${Math.round(panel.scrollHeight)}/${panel.offsetHeight}`;
  });
  const trace = [];
  let began = 0;
  onFail = () => { console.log("     touch log of the last gesture (sent | received | drawer after):"); trace.forEach(l => console.log(l)); };
  const send = async (type, x, y) => {
    const want = type.toLowerCase() + ":" + (type === "touchEnd" ? null : Math.round(y));
    const { n } = await received();
    if (type === "touchStart") { began = Date.now(); trace.length = 0; }
    const under = type === "touchStart" ? " under=" + await page.evaluate(([px, py]) => {
      const e = document.elementFromPoint(px, py);
      return e ? e.tagName.toLowerCase() + (e.id ? "#" + e.id : "") + [...e.classList].map(c => "." + c).join("") : "none";
    }, [x, y]) : "";
    const sent = `${type} x=${x === undefined ? "-" : Math.round(x)} y=${y === undefined ? "-" : Math.round(y)}${under} +${Date.now() - began}ms`;
    await cdp.send("Input.dispatchTouchEvent", { type, touchPoints: type === "touchEnd" ? [] : [{ x, y }] });
    const got = await until(received, v => v.n > n && v.last === want);
    trace.push(`     ${sent} | page n=${got.n} last=${got.last} +${Date.now() - began}ms | ${await drawerNow()}`);
  };
  const quiet = () => until(
    () => page.evaluate(() => {
      const h = [...document.querySelectorAll(".fdrawer")].filter(e => e.offsetParent !== null)[0];
      return !h || (h.getAnimations({ subtree: true }).length === 0 && !h.classList.contains("fdragging"));
    }),
    ok => ok);
  const drag = async (x, y0, y1, steps) => {
    await quiet();
    await send("touchStart", x, y0);
    for (let i = 1; i <= steps; i++) await send("touchMove", x, y0 + (y1 - y0) * i / steps);
    await sleep(120);   // a finger that stops before it lifts leaves no fling, and a tap during one is eaten
    await send("touchEnd");
    await quiet();
  };
  // The line and the panel are read when a gesture starts: a late re-layout moves them, and a finger on a stale point lands elsewhere.
  const lineDrag = async (frac, steps) => {
    await quiet();
    const g = await still(async () => {
      const b = await linePoint(page);
      const h = await page.evaluate(() => [...document.querySelectorAll(".fdrawer")].filter(e => e.offsetParent !== null)[0].querySelector(".fpanel").scrollHeight);
      return { x: b.x, y: b.y, h };
    });
    await drag(g.x, g.y, g.y + g.h * frac, steps);
  };
  let s = await state(page);
  const panelH = await page.evaluate(() => [...document.querySelectorAll(".fdrawer")].filter(e => e.offsetParent !== null)[0].querySelector(".fpanel").scrollHeight);
  check(panelH > 60, `${tag} 8: the panel has a height to pull (${panelH})`);

  await lineDrag(0.25, 6);
  s = await state(page);
  check(s && !s.open, `${tag} 8: a quarter of the way down snaps back`);
  await lineDrag(0.5, 8);
  s = await state(page);
  check(s && s.open, `${tag} 8: half way opens it`);
  check(s && s.panelVisible, `${tag} 8: and it is drawn`);
  const cap = await page.evaluate(() => [...document.querySelectorAll(".fdrawer")].filter(e => e.offsetParent !== null)[0].querySelector(".fpanel").offsetHeight / innerHeight);
  check(cap <= 0.71, `${tag} 8: the open drawer is capped at 70% of the screen (${cap.toFixed(2)})`);
  // Swipe up on the drawer.
  const mid = { x: s.panel.x + s.panel.w / 2, y: s.panel.y + Math.min(s.panel.h - 10, 40) };
  await drag(mid.x, mid.y + 20, mid.y - 60, 6);
  s = await state(page);
  check(s && !s.open, `${tag} 8: a swipe up on the drawer closes it`);
  // A drag that starts on the list scrolls and never opens it.
  const r = await page.evaluate(rowsSel => {
    const e = [...document.querySelectorAll(rowsSel)].filter(x => x.offsetParent !== null)[1];
    if (!e) return null; const b = e.getBoundingClientRect(); return { x: b.left + 40, y: b.top + 10 };
  }, T.rows);
  if (r) {
    await drag(r.x, r.y, r.y + 160, 8);
    s = await state(page);
    check(s && !s.open, `${tag} 8: a drag down that starts on the list does not open the drawer`);
    await drag(r.x, Math.min(r.y + 300, 800), r.y, 8);
    s = await state(page);
    check(s && !s.open, `${tag} 8: a drag up that starts on the list does not open it either`);
  }
  // The pull-to-refresh is off on the line.
  const ob = await page.evaluate(() => getComputedStyle([...document.querySelectorAll(".fdrawer")].filter(e => e.offsetParent !== null)[0].querySelector(".fline")).overscrollBehaviorY);
  check(ob === "none", `${tag} 8: overscroll-behavior is off on the line (${ob})`);
  // A tap on the search box only types. Sent as the same touch pair the drags use: mixing `touchscreen.tap` into a session
  // that has just run raw CDP gestures can lose the tap, which says nothing about the app.
  const settledClosed = async () => {
    const t0 = Date.now();
    for (;;) {
      const k = await page.evaluate(() => {
        const h = [...document.querySelectorAll(".fdrawer")].filter(e => e.offsetParent !== null)[0];
        return !!h && !h.classList.contains("open") && !h.classList.contains("fdragging") && h.getAnimations({ subtree: true }).length === 0;
      });
      if (k || Date.now() - t0 >= 3000) return k;
      await sleep(10);
    }
  };
  await settledClosed();
  const q = await still(async () => (await state(page)).q);
  const qx = q.x + q.w / 2, qy = q.y + q.h / 2;
  await send("touchStart", qx, qy);
  await sleep(40);
  await send("touchEnd");
  await sleep(300);
  s = await state(page);
  check(s && s.activeIsQ && !s.open, `${tag} 8: a tap on the search box focuses it and does not open the drawer`);
  onFail = null;
  await ctx.close();
}

// Swallow edges, and the close handle by keyboard ------------------------------------------------------------------
// A press that cannot become a click must not leave the swallow armed for the next real one (review finding 3), and the
// handle is a button a key can press (finding 4). The probe for "was it swallowed" is the drawer's own line: a click on it
// opens the drawer unless the swallow ate it.
async function swallowEdges(browser) {
  {
    const tag = "1280px terms";
    const { ctx, page } = await open(browser, 1280, false);
    await goto(page, "terms");
    const openIt = async () => { const p = await linePoint(page); await page.mouse.click(p.x, p.y); await sleep(250); };
    await openIt();
    let s = await state(page);
    check(s && s.open, `${tag} 3b: the drawer is open before the right-click`);
    const row = await page.evaluate(() => {
      const p = document.querySelector(".fdrawer.open .fpanel").getBoundingClientRect();
      const e = [...document.querySelectorAll("#term-list .card.tab")].map(x => ({ x, b: x.getBoundingClientRect() }))
        .find(r => r.b.top > p.bottom + 6 && r.b.bottom < innerHeight - 4);
      return e ? { x: e.b.left + 40, y: e.b.top + 12 } : null;
    });
    check(!!row, `${tag} 3b: there is a row below the drawer to right-click`);
    if (row) {
      await page.mouse.click(row.x, row.y, { button: "right" });
      await sleep(150);
      s = await state(page);
      check(s && !s.open, `${tag} 3b: a right-click outside closes the drawer`);
      // Within the 1.5 s: a plain click must get through. It is on the line, so it only opens the drawer when not eaten.
      const p = await linePoint(page);
      await page.mouse.click(p.x, p.y);
      await sleep(250);
      s = await state(page);
      check(s && s.open, `${tag} 3b: the click that follows a right-click is not swallowed`);
      await page.keyboard.press("Escape");
    }
    // Finding 4: Enter on the focused handle closes it.
    await sleep(200);
    await openIt();
    s = await state(page);
    check(s && s.open, `${tag} 4b: opened again`);
    await page.focus(".fdrawer.open .fhandle");
    await page.keyboard.press("Enter");
    await sleep(250);
    s = await state(page);
    check(s && !s.open, `${tag} 4b: Enter on the close handle closes the drawer`);
    await openIt();
    await page.focus(".fdrawer.open .fhandle");
    await page.keyboard.press("Space");
    await sleep(250);
    s = await state(page);
    check(s && !s.open, `${tag} 4b: and so does Space`);
    // ...while a key still does not OPEN it: the line's own click with no pointer behind it.
    await page.evaluate(() => document.querySelector("#fd-terms .fline").click());
    s = await state(page);
    check(s && !s.open, `${tag} 4b: a click with no pointer behind it still does not open the drawer`);
    await ctx.close();
  }
  {
    const tag = "390px terms";
    const { ctx, page } = await open(browser, 390, true);
    const cdp = await ctx.newCDPSession(page);
    await goto(page, "terms");
    const send = (type, x, y) => cdp.send("Input.dispatchTouchEvent", { type, touchPoints: type === "touchEnd" ? [] : [{ x, y }] });
    const p = await linePoint(page);
    const tap = async (x, y) => { await send("touchStart", x, y); await sleep(40); await send("touchEnd"); await sleep(300); };
    await tap(p.x, p.y);
    let s = await state(page);
    check(s && s.open, `${tag} 3c: a tap on the line opens the drawer`);
    // A finger that lands below the drawer and is carried away more than 8px: the browser scrolls or cancels, no click.
    const r = await page.evaluate(() => {
      const pn = document.querySelector(".fdrawer.open .fpanel").getBoundingClientRect();
      const e = [...document.querySelectorAll("#term-list .card.tab")].map(x => ({ x, b: x.getBoundingClientRect() }))
        .find(q => q.b.top > pn.bottom + 6 && q.b.bottom < innerHeight - 4);
      return e ? { x: e.b.left + 40, y: e.b.top + 10 } : null;
    });
    check(!!r, `${tag} 3c: there is a row below the drawer to drag from`);
    if (r) {
      await send("touchStart", r.x, r.y);
      for (let i = 1; i <= 6; i++) { await send("touchMove", r.x, r.y + i * 8); await sleep(16); }
      await sleep(120);   // as in `drag`: stopped before it lifts, so no fling eats the next tap
      await send("touchEnd");
      await sleep(200);
      s = await state(page);
      check(s && !s.open, `${tag} 3c: a touch that scrolls outside closes the drawer`);
      await tap(p.x, p.y);
      s = await state(page);
      check(s && s.open, `${tag} 3c: and the tap right after it is not swallowed`);
    }
    await ctx.close();
  }
}

// The phone with a terminal attached (review finding 1) -------------------------------------------------------------
// `body.term-phone` is set by `syncPhoneView` for good once a terminal opens. The terminals line must still be there,
// above the terminal and the session list, and hide only while the terminal fills the screen (`term-full`).
async function phoneAttached(browser) {
  const tag = "390px terms attached";
  const { ctx, page } = await open(browser, 390, true);
  await goto(page, "terms");
  const set = await page.evaluate(() => {
    try { paintPaneBg({ background: "#101828", foreground: "#e6e6e6", cursor: "#4ea1ff" }); } catch (e) {}
    document.body.classList.add("term-phone");
    const lay = document.getElementById("term-layout");
    lay.classList.add("has-term");
    return { phone: document.body.classList.contains("term-phone"), hasTerm: lay.classList.contains("has-term") };
  });
  check(set.phone && set.hasTerm, `${tag}: the phone layout with a terminal attached is set (${JSON.stringify(set)})`);
  await sleep(300);
  let s = await state(page);
  check(s && s.tab === "terms" && s.q.w > 40 && s.line.h > 20, `${tag}: the line and the search box are on screen`);
  const geo0 = await geometry(page, "#term-layout");
  const p = await linePoint(page);
  await page.touchscreen.tap(p.x, p.y);
  await sleep(300);
  s = await state(page);
  check(s && s.open && s.panelVisible, `${tag}: a tap on the line opens the drawer`);
  const cap = await page.evaluate(() => [...document.querySelectorAll(".fdrawer")].filter(e => e.offsetParent !== null)[0].querySelector(".fpanel").offsetHeight / innerHeight);
  check(cap <= 0.71, `${tag}: it is capped at 70% of the screen (${cap.toFixed(2)})`);
  check(geo0 === await geometry(page, "#term-layout"), `${tag}: and the terminal's box did not move or resize`);
  const top = await page.evaluate(() => {
    const pn = document.querySelector(".fdrawer.open .fpanel").getBoundingClientRect();
    const e = document.elementFromPoint(pn.x + pn.width / 2, pn.y + Math.min(30, pn.height / 2));
    return !!(e && e.closest(".fpanel"));
  });
  check(top, `${tag}: it is over the terminal, not under it`);
  const chip = page.locator('.fdrawer.open .seg button[data-v="paused"]').first();
  const cb = await chip.boundingBox();
  await page.touchscreen.tap(cb.x + cb.width / 2, cb.y + cb.height / 2);
  await sleep(250);
  s = await state(page);
  check(s && s.open, `${tag}: a chip applies and the drawer stays open`);
  await page.keyboard.press("Escape");
  await sleep(250);
  s = await state(page);
  check(s && !s.open, `${tag}: Esc closes it`);
  await page.evaluate(() => document.activeElement && document.activeElement.blur());
  await page.keyboard.press("Control+/");
  s = await state(page);
  check(s && s.activeIsQ && !s.open, `${tag}: ctrl-/ reaches the search box and leaves the drawer shut`);
  await page.keyboard.press("Escape");
  await page.keyboard.press("Escape");
  // Full screen takes it away, and leaving full screen brings it back.
  await page.evaluate(() => document.body.classList.add("term-full"));
  await sleep(150);
  check(await page.evaluate(() => getComputedStyle(document.getElementById("fd-terms")).display === "none"), `${tag}: a terminal that fills the screen hides the line`);
  await page.evaluate(() => document.body.classList.remove("term-full"));
  await sleep(150);
  check(await page.evaluate(() => getComputedStyle(document.getElementById("fd-terms")).display !== "none"), `${tag}: and it is back when the terminal does not`);
  await ctx.close();
}

// The attached card stays (review finding 5) --------------------------------------------------------------------------
// Whatever the search says and whichever chip is off, on all three tabs. `termTask` is set and the tab painted in one
// evaluate, so the terminals tab's own reconcile of a card with no runner does not clear it in between.
async function attachedStays(browser) {
  const { ctx, page } = await open(browser, 1280, false);
  const drawn = (tab, id, task, q) => page.evaluate(async ([tab, id, task, q]) => {
    termTask = task ? { id: task } : null;
    document.getElementById({ stack: "stack-q", board: "board-q", terms: "term-q" }[tab]).value = q || "";
    if (tab === "stack") paintStack(); else if (tab === "board") await renderBoard(); else await renderTermList();
    const root = { stack: "#stack-list", board: "#board", terms: "#term-list" }[tab];
    const n = document.querySelectorAll(root + ' [data-id="' + id + '"]').length;
    const badge = [...document.querySelectorAll(root + " .chip.subs")].map(e => e.textContent).join("|");
    termTask = null;
    return { n, badge };
  }, [tab, id, task, q]);

  await goto(page, "stack");
  let r = await drawn("stack", "c1", null, "");
  check(r.n === 0 && /2 subagents/.test(r.badge), `stack attached: a subagent is rolled up when it is not attached (${r.n}, ${r.badge})`);
  r = await drawn("stack", "c1", "c1", "");
  check(r.n > 0, `stack attached: the attached subagent is still a row with the subagents chip off (${r.n})`);
  check(/(^|\|)1 subagent($|\|)/.test(r.badge), `stack attached: and its parent no longer counts it (${r.badge})`);
  r = await drawn("stack", "c1", "c1", "alpha");
  check(r.n > 0, `stack attached: and it stays through a search that does not match it (${r.n})`);
  r = await drawn("stack", "d1", null, "");
  check(r.n === 0, `stack attached: a finished card is not drawn with the finished chip off (${r.n})`);
  r = await drawn("stack", "d1", "d1", "");
  check(r.n > 0, `stack attached: the attached finished card stays with the finished chip off (${r.n})`);
  r = await drawn("stack", "d1", "d1", "alpha");
  check(r.n > 0, `stack attached: and through a search (${r.n})`);

  await goto(page, "board");
  await page.evaluate(() => { toggleBoardShow("done"); });
  await sleep(300);
  r = await drawn("board", "d1", null, "");
  check(r.n === 0, `board attached: a finished card is not drawn with the finished column off (${r.n})`);
  r = await drawn("board", "d1", "d1", "");
  check(r.n > 0, `board attached: the attached finished card is, and its column with it (${r.n})`);
  r = await drawn("board", "d1", "d1", "alpha");
  check(r.n > 0, `board attached: and through a search (${r.n})`);
  r = await drawn("board", "c1", null, "");
  check(r.n === 0, `board attached: a subagent is rolled up into its parent's card (${r.n})`);
  r = await drawn("board", "c1", "c1", "");
  check(r.n > 0 && /(^|\|)1 subagent($|\|)/.test(r.badge), `board attached: the attached subagent is a card, and the parent counts one less (${r.n}, ${r.badge})`);
  await page.evaluate(() => { toggleBoardShow("done"); });

  await goto(page, "terms");
  r = await drawn("terms", "c1", null, "");
  check(r.n === 0, `terminals attached: a subagent under a folded parent is not a row (${r.n})`);
  r = await drawn("terms", "c1", "c1", "");
  check(r.n === 0, `terminals attached: a fold hides every child, the attached one too (${r.n})`);
  r = await drawn("terms", "c1", "c1", "run");
  check(r.n === 0, `terminals attached: and through a search that matches its parent only (${r.n})`);
  r = await drawn("terms", "c1", "c1", "alpha");
  check(r.n > 0, `terminals attached: and through a search that matches neither (${r.n})`);
  r = await drawn("terms", "r2", "r2", "alpha");
  check(r.n > 0, `terminals attached: the attached ordinary row stays through a search it does not match (${r.n})`);
  r = await drawn("terms", "r2", null, "alpha");
  check(r.n === 0, `terminals attached: and goes when it is not attached (${r.n})`);
  // The chip: with the subagents chip toggled and toggled back, the attached child is still on screen.
  r = await page.evaluate(async () => {
    termTask = { id: "c1" };
    toggleTermShow("subagents"); await renderTermList();
    toggleTermShow("subagents"); await renderTermList();
    const n = document.querySelectorAll('#term-list [data-id="c1"]').length;
    termTask = null;
    return n;
  });
  check(r > 0, `terminals attached: toggling the subagents chip does not fold the attached child out of sight (${r})`);
  await ctx.close();
}

(async () => {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const browser = await chromium.launch();
  try {
    for (const [width, mobile] of [[1280, false], [390, true]]) {
      for (const tab of Object.keys(TABS)) await steps(browser, width, mobile, tab);
      await keeps(browser, width, mobile);
    }
    for (const tab of Object.keys(TABS)) await touch(browser, tab);
    await swallowEdges(browser);
    await phoneAttached(browser);
    await attachedStays(browser);
  } catch (e) {
    check(false, "the check itself threw: " + (e && e.stack || e));
  }
  await browser.close();
  server.close();
  console.log(failed ? `${failed} check(s) failed` : "the filter drawer holds.");
  process.exit(failed ? 1 : 0);
})();
