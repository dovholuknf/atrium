// Before/after shots of the filter drawer strip (docs/backlog/ui/filter-drawer-strip-notes.md).
//
//   NODE_PATH=<dir with playwright> PREFIX=before node scripts/shoot-filter-drawer-strip.js
//
// Stack, Board and Terminals closed and open, and a terminals-list parent card with its subagent rollup chevron, at 1280px and 390px.
// Writes docs/backlog/ui/img/filter-drawer-strip/<PREFIX>-<what>-<width>.png.

const http = require("http");
const fs = require("fs");
const path = require("path");
const { wholeBoard } = require("./board-source.js");

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) {
  try { ({ chromium } = require("playwright")); } catch (e2) { console.log("playwright is not installed"); process.exit(0); }
}

const WEB = path.join(__dirname, "..", "internal", "api", "web");
const OUT = path.join(__dirname, "..", "docs", "backlog", "ui", "img", "filter-drawer-strip");
const PREFIX = process.env.PREFIX || "after";
const HTML = wholeBoard();
const TYPES = { ".js": "text/javascript", ".css": "text/css", ".png": "image/png", ".svg": "image/svg+xml", ".gif": "image/gif", ".woff2": "font/woff2" };
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
  ...Array.from({ length: 10 }, (_, i) => card("f" + i, "filler " + i, "needs-input", { supervised: true, last_activity_at: at(10 + i) })),
  card("p1", "parent run", "needs-input", { supervised: true, last_activity_at: at(1), worktree: "/w/parent" }),
  card("c1", "child one", "running", { supervised: true, tags: [SUB], spawned_by_id: "p1", worktree: "/w/parent/c1" }),
  card("c2", "child two", "running", { supervised: true, tags: [SUB], spawned_by_id: "p1", worktree: "/w/parent/c2" }),
  card("n1", "second runner", "running", { supervised: true }),
  card("w1", "needs permission", "needs-permission", { supervised: true }),
  card("d1", "finished one", "done"),
  card("s1", "paused one", "paused")
];
const server = http.createServer((req, res) => {
  const p = new URL(req.url, "http://x").pathname;
  const json = o => { res.writeHead(200, { "Content-Type": "application/json" }); res.end(JSON.stringify(o)); };
  if (p === "/" || p === "/index.html") { res.writeHead(200, { "Content-Type": "text/html" }); return res.end(HTML); }
  if ((p.startsWith("/vendor/") || p.startsWith("/css/") || p === "/working.gif") && !p.includes("..")) {
    const f = path.join(WEB, p);
    if (fs.existsSync(f) && fs.statSync(f).isFile()) { res.writeHead(200, { "Content-Type": TYPES[path.extname(f)] || "application/octet-stream" }); return res.end(fs.readFileSync(f)); }
    res.writeHead(404); return res.end("");
  }
  if (p.startsWith("/v1/events")) { res.writeHead(200, { "Content-Type": "text/event-stream" }); return res.write(": open\n\n"); }
  if (p === "/v1/tasks") return json({ tasks: TASKS });
  if (p === "/v1/health") return json({ build: "shoot", settling: false, halted: false });
  if (p === "/v1/settings") return json({ global_auto: false, board_skin: "harbour", board_skins: ["harbour"], scrollback_lines: 5000 });
  if (p === "/v1/permissions") return json({ permissions: [] });
  if (p === "/v1/harnesses") return json({ harnesses: [] });
  return json({});
});
const sleep = ms => new Promise(r => setTimeout(r, ms));

(async () => {
  fs.mkdirSync(OUT, { recursive: true });
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const browser = await chromium.launch();
  for (const [width, mobile] of [[1280, false], [390, true]]) {
    const ctx = await browser.newContext(mobile
      ? { viewport: { width, height: 844 }, hasTouch: true, isMobile: true, deviceScaleFactor: 2 }
      : { viewport: { width, height: 800 }, deviceScaleFactor: 2 });
    if (mobile) await ctx.addInitScript(() => { try { sessionStorage.setItem("atrium.m.desktop", "1"); } catch (e) {} });
    const page = await ctx.newPage();
    await page.goto("http://127.0.0.1:" + server.address().port + "/");
    await page.waitForFunction(() => typeof switchView === "function" && document.querySelectorAll(".card, .stackrow").length > 0, null, { timeout: 15000 });
    const shot = (what, opts) => page.screenshot(Object.assign({ path: path.join(OUT, `${PREFIX}-${what}-${width}.png`) }, opts));
    for (const view of ["stack", "board", "terms"]) {
      await page.evaluate(v => switchView(v), view); await sleep(700);
      await shot(view + "-closed");
      await page.evaluate(() => document.querySelector(".fdrawer:not([hidden]) .fline, .fdrawer .fline") && [...document.querySelectorAll(".fdrawer")].find(e => e.offsetParent).querySelector(".filtersbtn").click());
      // click() has detail 0 and is ignored by design, so press with a real pointer
      const b = await page.evaluate(() => { const r = [...document.querySelectorAll(".fdrawer")].find(e => e.offsetParent).querySelector(".filtersbtn").getBoundingClientRect(); return { x: r.x + r.width / 2, y: r.y + r.height / 2 }; });
      if (mobile) await page.touchscreen.tap(b.x, b.y); else await page.mouse.click(b.x, b.y);
      await sleep(500);
      await shot(view + "-open");
      if (mobile) await page.touchscreen.tap(b.x, b.y); else await page.mouse.click(b.x, b.y);
      await sleep(300);
    }
    // the terminals list, where a parent card carries the subagent rollup chevron
    const fold = page.locator(".tkidfold").first();
    if (await fold.count()) {
      const row = fold.locator("xpath=ancestor::*[contains(@class,'card')][1]");
      await row.screenshot({ path: path.join(OUT, `${PREFIX}-rollup-chevron-${width}.png`) });
    } else console.log("no .tkidfold at " + width);
    await ctx.close();
  }
  await browser.close();
  server.close();
})();
