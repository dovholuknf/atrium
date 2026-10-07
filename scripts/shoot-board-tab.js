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

const card0 = (id, title, more) => Object.assign({
  id, status: "running", display_title: title, runner: "claude", rank: 1, worktree: "/tmp/" + id, why: "",
  idle_seconds: 0, wait_seconds: 0, created_at: "2026-09-19T12:00:00Z", last_activity_at: "2026-09-19T12:00:00Z",
  tags: [], supervised: false, offline: false, pinned: false, auto_approve: false,
}, more || {});


const card = card0;
const SKIN = process.env.SKIN || "daylight";
const list = [];
for (let i = 0; i < 40; i++) list.push(card("r" + i, "ready card number " + i + " with a reasonably long title to wrap", { status: "needs-input", supervised: true, tags: ["alpha", "beta"], rank: i }));
for (let i = 0; i < 3; i++) list.push(card("u" + i, "running card " + i, { status: "running", supervised: true, rank: 100 + i }));
list.push(card("s0", "shelved card", { status: "shelved", rank: 200 }));
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
  if (p === "/v1/health") return json({ build: "check", settling: false, halted: false });
  if (p === "/v1/settings") return json({ global_auto: false, board_skin: SKIN, board_skins: [SKIN], scrollback_lines: 5000 });
  if (p === "/v1/permissions") return json({ permissions: [] });
  if (p === "/v1/harnesses") return json({ harnesses: [] });
  if (p === "/_hub/rooms" || p === "/_hub/inventory") return json({ error: "not a hub" }, 404);
  return json({});
});
(async () => {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1600, height: 900 } });
  await page.goto("http://127.0.0.1:" + server.address().port + "/");
  await page.waitForFunction(() => typeof renderBoard === "function" && cardsLoaded);
  await page.evaluate(skin => { document.documentElement.setAttribute("data-skin", skin); setGrouping({ on: false }, "board"); switchView("board"); }, SKIN);
  await page.waitForSelector('[data-column="needs-input"] .card');
  await page.waitForTimeout(800);
  const m = await page.evaluate(() => [...document.querySelectorAll(".card")].slice(0, 3).map(c => c.getBoundingClientRect().height));
  console.log("card heights", m, "skin", await page.evaluate(() => document.documentElement.dataset.skin));
  await page.screenshot({ path: process.env.SHOT || "board.png" });
  await browser.close(); server.close();
})();
