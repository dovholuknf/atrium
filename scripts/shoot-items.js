// The items tab (list, detail, blocked) in a real browser on the fixtures of js/items-mock.js, as PNGs at 1400 and 2400 wide.
//
//   NODE_PATH=<dir with playwright> node scripts/shoot-items.js
//   OUT=<dir>      where the PNGs go (default docs/rnd/img/item-lifecycle, beside the mockups they reproduce)
//   NAME=<suffix>  PNG name suffix (default built, so list-built-1400.png)
//   ROOT=<tree>    the checkout whose board is drawn
//   SKIN=<name>    a board skin to wear
//
// No hub and no room: the board is served from memory, `?itemsmock=1` makes the tab read the fixtures, and the clock is held so the
// times and ages are the mockups' (the list and the blocked item at 13:30 UTC, the detail at 19:20). The page is as tall as the tab.

const http = require("http");
const path = require("path");
const fs = require("fs");

const ROOT = path.resolve(process.env.ROOT || path.join(__dirname, ".."));
const { wholeBoard } = require(path.join(ROOT, "scripts", "board-source.js"));

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) {
  try { ({ chromium } = require("playwright")); } catch (e2) { console.log("playwright is not installed"); process.exit(0); }
}

const OUT = path.resolve(process.env.OUT || path.join(ROOT, "docs", "rnd", "img", "item-lifecycle"));
const NAME = process.env.NAME || "built";
const HTML = wholeBoard();

const server = http.createServer((req, res) => {
  const u = new URL(req.url, "http://x");
  const p = u.pathname;
  const json = (o, code) => { res.writeHead(code || 200, { "Content-Type": "application/json" }); res.end(JSON.stringify(o)); };
  if (p === "/" || p === "/index.html") { res.writeHead(200, { "Content-Type": "text/html" }); return res.end(HTML); }
  if (p.startsWith("/v1/events")) { res.writeHead(200, { "Content-Type": "text/event-stream" }); return res.write(": open"); }
  if (p === "/_hub/rooms") return json({ rooms: [{ name: "sg4" }] });
  if (p === "/_hub/inventory") return json({ rooms: [] });
  if (p === "/_hub/backlog") return json({ items: [] });
  if (p === "/_hub/reports") return json({ reports: [] });
  if (req.method !== "GET") return json({ ok: true });
  if (p === "/v1/tasks") return json({ tasks: [] });
  if (p === "/v1/health") return json({ build: "shoot", settling: false, halted: false });
  if (p === "/v1/settings") return json({ global_auto: false, board_skin: process.env.SKIN || "", board_skins: [], scrollback_lines: 5000 });
  if (p === "/v1/permissions") return json({ permissions: [] });
  if (p === "/v1/harnesses") return json({ harnesses: [] });
  return json({});
});

const VIEWS = [
  { name: "list", at: "2026-10-15T13:30:00Z", open: null },
  { name: "detail", at: "2026-10-15T19:20:00Z", open: "u-items-tab" },
  { name: "blocked", at: "2026-10-15T13:30:00Z", open: "f-ptyhost-reconnect" },
];

(async () => {
  fs.mkdirSync(OUT, { recursive: true });
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const base = "http://127.0.0.1:" + server.address().port;
  const browser = await chromium.launch();
  const errors = [];
  for (const width of [1400, 2400]) {
    for (const v of VIEWS) {
      const ctx = await browser.newContext({ viewport: { width, height: 1000 }, timezoneId: "UTC" });
      const page = await ctx.newPage();
      page.on("pageerror", e => errors.push(v.name + " " + width + ": " + e));
      // The mockups are drawn at a text scale of 1, the board's default is 1.1.
      await page.addInitScript(s => { try { localStorage.setItem("atrium.uiscale", s); } catch (e) { /* no storage */ } }, process.env.UISCALE || "1");
      await page.clock.install({ time: new Date(v.at) });
      await page.goto(base + "/?itemsmock=1");
      await page.waitForFunction(() => typeof itemsShow === "function" && typeof switchView === "function");
      await page.evaluate(async open => {
        hubIsHub = true; paintItemsTab();
        await switchView("items");
        await loadItems();
        if (open) { itemsShow(open); await loadItemDetail(open, false); }
      }, v.open);
      await page.addStyleTag({ content: ".it-mockbar { display: none !important; }" });
      await page.waitForSelector(v.open ? ".it-detail" : ".it-row");
      // The page is as tall as the tab: the board never scrolls as a whole, so size the window to the content.
      const h = await page.evaluate(() => {
        const l = document.getElementById("items-list");
        return Math.ceil(l.getBoundingClientRect().top + l.scrollHeight);
      });
      await page.setViewportSize({ width, height: h });
      await page.waitForTimeout(150);
      const file = path.join(OUT, v.name + "-" + NAME + "-" + width + ".png");
      await page.screenshot({ path: file });
      console.log(file, width + "x" + h);
      await ctx.close();
    }
  }
  if (errors.length) { console.log("page errors:", errors.join(" | ")); process.exitCode = 1; }
  await browser.close(); server.close();
})();
