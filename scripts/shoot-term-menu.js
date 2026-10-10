// Shoots the terminals tab with a card's right-click menu open, in a real browser with every endpoint mocked.
//
//   NODE_PATH=<dir with playwright> SHOT=<png> [STOPPED=1] node scripts/shoot-term-menu.js
//   STOPPED=1 opens the menu on the stopped card instead of the running one.

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

const card0 = (id, title, more) => Object.assign({
  id, status: "running", display_title: title, runner: "claude", rank: 1, worktree: "/tmp/" + id, why: "",
  idle_seconds: 0, wait_seconds: 0, created_at: "2026-09-19T12:00:00Z", last_activity_at: "2026-09-19T12:00:00Z",
  tags: [], supervised: false, offline: false, pinned: false, auto_approve: false,
}, more || {});


const card = card0;
const list = [
  card("tm-run", "fix the login redirect", { status: "running", supervised: true, rank: 1 }),
  card("tm-wait", "write the release notes", { status: "needs-input", supervised: true, rank: 2 }),
  card("tm-stop", "old spike on caching", { status: "shelved", supervised: true, rank: 3 }),
];
const server = http.createServer((req, res) => {
  const p = new URL(req.url, "http://x").pathname;
  const json = (o, code) => { res.writeHead(code || 200, { "Content-Type": "application/json" }); res.end(JSON.stringify(o)); };
  if (p === "/" || p === "/index.html") { res.writeHead(200, { "Content-Type": "text/html" }); return res.end(HTML); }
  if ((p.startsWith("/vendor/") || p.startsWith("/css/") || p === "/working.gif") && !p.includes("..")) {
    const f = path.join(WEB, p);
    if (fs.existsSync(f) && fs.statSync(f).isFile()) { res.writeHead(200, { "Content-Type": TYPES[path.extname(f)] || "application/octet-stream" }); return res.end(fs.readFileSync(f)); }
    res.writeHead(404); return res.end("");
  }
  if (p.startsWith("/v1/events")) { res.writeHead(200, { "Content-Type": "text/event-stream" }); return res.write(": open"); }
  if (req.method !== "GET") return json({ ok: true });
  if (p === "/v1/tasks") return json({ tasks: list });
  if (p.startsWith("/v1/tasks/")) { const t = list.find(x => x.id === p.split("/")[3]); if (t) return json(t); }
  if (p === "/v1/health") return json({ build: "check", settling: false, halted: false });
  if (p === "/v1/settings") return json({ global_auto: false, board_skin: "daylight", board_skins: ["daylight"], scrollback_lines: 5000 });
  if (p === "/v1/permissions") return json({ permissions: [] });
  if (p === "/v1/harnesses") return json({ harnesses: [] });
  if (p === "/_hub/rooms" || p === "/_hub/inventory") return json({ error: "not a hub" }, 404);
  return json({});
});
(async () => {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  await page.goto("http://127.0.0.1:" + server.address().port + "/");
  await page.waitForFunction(() => typeof renderBoard === "function" && cardsLoaded);
  await page.evaluate(() => { try { localStorage.setItem("atrium.showstopped", "on"); } catch (e) {} switchView("terms"); });
  const id = process.env.STOPPED ? "tm-stop" : "tm-run";
  const sel = '#term-list [oncontextmenu*="' + id + '"]';
  await page.waitForSelector(sel);
  await page.evaluate(s => document.querySelector(s).dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 90, clientY: 120 })), sel);
  await page.waitForSelector("#cardmenu.on button");
  await page.waitForTimeout(400);
  await page.screenshot({ path: process.env.SHOT || "term-menu.png" });
  await browser.close(); server.close();
})();


