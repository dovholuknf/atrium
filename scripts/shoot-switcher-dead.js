// The switcher with nothing typed, on a hub with two rooms, where a reboot left cards dead.
//
//   NODE_PATH=<dir with playwright> node scripts/shoot-switcher-dead.js
//   SHOT=<png> is where it saves. SKIN picks the skin.
//
// Every endpoint is mocked. Five cards run, seven on claude-sg4 and one on sg4-control are dead with `revive` set
// (a failure killed them), and two are dead without it (closed on purpose). Only the seven and the one belong under
// Dead.

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
const SKIN = process.env.SKIN || "harbour";

const card = (room, id, title, more) => Object.assign({
  id: room + "~" + id, room, status: "needs-input", display_title: title, runner: "claude", rank: 1,
  worktree: "D:/worktrees/" + title, why: "", idle_seconds: 0, wait_seconds: 0,
  created_at: "2026-10-27T12:00:00Z", last_activity_at: "2026-10-27T12:00:00Z", tags: [], supervised: true,
  offline: false, pinned: false, auto_approve: false,
}, more || {});
const dead = { status: "dead", supervised: false, revive: true, last_activity_at: "2026-10-27T12:10:00Z" };
const list = [
  card("claude-sg4", "a1", "dotfiles"), card("claude-sg4", "a2", "ui-for-config"),
  card("claude-sg4", "a3", "ziti-hook", { status: "running" }), card("claude-sg4", "a4", "rnd"),
  card("claude-sg4", "a5", "blog"),
  ...["card-proc-attribution", "add-by-url-lab", "hook-fail-fast", "pr-394", "pr-395", "enrollment-links",
    "zab-exploration"].map((n, i) => card("claude-sg4", "d" + i, n, dead)),
  card("sg4-control", "o1", "ogorch", dead),
  card("claude-sg4", "x1", "closed-on-purpose", { status: "dead", supervised: false }),
  card("claude-sg4", "x2", "finished-last-week", { status: "done", supervised: false }),
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
  if (p === "/v1/health") return json({ build: "check", settling: false, halted: false });
  if (p === "/v1/settings") return json({ global_auto: false, board_skin: SKIN, board_skins: [SKIN], scrollback_lines: 5000 });
  if (p === "/v1/permissions") return json({ permissions: [] });
  if (p === "/v1/harnesses") return json({ harnesses: [] });
  if (p === "/_hub/rooms") return json({ rooms: [{ name: "claude-sg4", online: true }, { name: "sg4-control", online: true }] });
  if (p === "/_hub/inventory") return json({ error: "none" }, 404);
  return json({});
});

(async () => {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1400, height: 1700 } });
  await page.goto("http://127.0.0.1:" + server.address().port + "/");
  await page.waitForFunction(() => typeof openSwitcher === "function" && cardsLoaded);
  await page.evaluate(skin => document.documentElement.setAttribute("data-skin", skin), SKIN);
  await page.evaluate(() => openSwitcher());
  await page.waitForSelector("#sw-list .swrow");
  await page.waitForTimeout(600);
  const rows = await page.evaluate(() => [...document.querySelectorAll("#sw-list .swrow")].map(r => r.innerText.replace(/\s+/g, " ").trim()));
  console.log(rows.join("\n"));
  const box = await page.evaluate(() => {
    const r = document.getElementById("switcher").getBoundingClientRect();
    return { x: Math.max(0, r.x - 20), y: Math.max(0, r.y - 20), width: r.width + 40, height: r.height + 40 };
  });
  await page.screenshot({ path: process.env.SHOT || "switcher.png", clip: box });
  await browser.close(); server.close();
})();
