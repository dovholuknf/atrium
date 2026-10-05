// A new card is in the terminals list when its alert is, in a real browser with every endpoint mocked.
//
//   NODE_PATH=<dir with playwright> node scripts/check-new-card-row.js
//   SHOT=<png> saves the list. NO_STARTING=1 leaves `starting` off the mocked card, the shape of an older room.
//
// It serves the concatenated board from board-source.js, the way scripts/check-pr-paste.js does. The mocked room
// holds one card, the list is read at load, and then a card is launched: /v1/tasks gains it (running, no runner yet,
// the shape a launch makes before the runner registers) and a `task` event naming it goes down the stream, with no
// whole row on it, the way an older room sends one. It times the alert ("is on the board") and the row in the
// terminals list, and fails if the row is more than a second behind the alert, or the alert more than two seconds
// behind the event. The throttled re-read (TASKS_EVERY, five seconds) is what that fails on.

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
const TYPES = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".png": "image/png",
  ".svg": "image/svg+xml", ".gif": "image/gif", ".woff2": "font/woff2" };

const card = (id, title, more) => Object.assign({
  id, status: "running", display_title: title, runner: "claude", rank: 1, worktree: "/tmp/" + id, why: "",
  idle_seconds: 0, wait_seconds: 0, created_at: "2026-09-19T12:00:00Z", last_activity_at: "2026-09-19T12:00:00Z",
  tags: [], supervised: false, offline: false, pinned: false, auto_approve: false,
}, more || {});

let list = [card("old1", "old card", { supervised: true })];
const streams = [];

const server = http.createServer(async (req, res) => {
  const p = new URL(req.url, "http://x").pathname;
  const json = (o, code) => { res.writeHead(code || 200, { "Content-Type": "application/json" }); res.end(JSON.stringify(o)); };
  if (p === "/" || p === "/index.html") { res.writeHead(200, { "Content-Type": "text/html" }); return res.end(HTML); }
  if ((p.startsWith("/vendor/") || p.startsWith("/css/") || p === "/working.gif") && !p.includes("..")) {
    const f = path.join(WEB, p);
    if (fs.existsSync(f) && fs.statSync(f).isFile()) {
      res.writeHead(200, { "Content-Type": TYPES[path.extname(f)] || "application/octet-stream" });
      return res.end(fs.readFileSync(f));
    }
    res.writeHead(404); return res.end("");
  }
  if (p.startsWith("/v1/events")) {
    res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" });
    res.write(": open\n\n");
    streams.push(res);
    return;
  }
  if (req.method !== "GET") return json({ ok: true });
  if (p === "/v1/tasks") return json({ tasks: list });
  if (p === "/v1/health") return json({ build: "check", settling: false, halted: false });
  if (p === "/v1/settings") return json({ global_auto: false, board_skin: "harbour", board_skins: ["harbour"], scrollback_lines: 5000 });
  if (p === "/v1/permissions") return json({ permissions: [] });
  if (p === "/v1/harnesses") return json({ harnesses: [] });
  if (p === "/_hub/rooms" || p === "/_hub/inventory") return json({ error: "not a hub" }, 404);
  return json({});
});

let fail = false;
const bad = msg => { console.error("FAIL: " + msg); fail = true; };

(async () => {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  try {
    await page.goto("http://127.0.0.1:" + server.address().port + "/");
    await page.waitForFunction(() => typeof renderTermList === "function" && cardsLoaded);
    await page.evaluate(() => switchView("terms"));
    await page.waitForSelector('[data-id="old1"]');
    // Let the first pass settle so the throttle clock (cardsReadAt) is fresh, which is the busy board.
    await page.waitForTimeout(1200);
    await page.evaluate(() => {
      window.__t = { alert: 0, row: 0 };
      const watch = () => {
        const now = performance.now();
        if (!window.__t.alert && [...document.querySelectorAll("#toasts .toast")].some(t => /new card is on the board/.test(t.textContent))) window.__t.alert = now;
        if (!window.__t.row && document.querySelector('[data-id="new1"]')) window.__t.row = now;
        requestAnimationFrame(watch);
      };
      watch();
    });
    list = list.concat(card("new1", "new card", process.env.NO_STARTING ? {} : { starting: true }));
    const sent = await page.evaluate(() => performance.now());
    streams.forEach(r => r.write('event: task\ndata: {"id":"new1"}\n\n'));
    await page.waitForFunction(() => window.__t.row && window.__t.alert, null, { timeout: 12000 }).catch(() => {});
    const t = await page.evaluate(() => window.__t);
    console.log("event to alert " + Math.round(t.alert - sent) + "ms, alert to row " + Math.round(t.row - t.alert) + "ms");
    if (process.env.SHOT) {
      await page.waitForTimeout(300);
      await page.locator("#term-list, body").first().screenshot({ path: process.env.SHOT });
    }
    if (!process.env.NO_STARTING && t.row && !/starting/.test(await page.locator('[data-id="new1"]').textContent())) bad("the row does not say starting");
    if (!t.alert) bad("the alert never fired");
    if (!t.row) bad("the row never appeared in the terminals list");
    if (t.alert && t.alert - sent > 2000) bad("the alert came " + Math.round(t.alert - sent) + "ms after the event, want under 2000");
    if (t.alert && t.row && t.row - t.alert > 1000) bad("the row came " + Math.round(t.row - t.alert) + "ms after the alert, want under 1000");
  } catch (e) { bad(String((e && e.stack) || e)); }
  await browser.close();
  server.close();
  if (fail) process.exit(1);
  console.log("new card: the alert and the terminals row land together.");
})();
