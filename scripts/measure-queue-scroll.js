// Frame times while the Queue tab's list is wheel-scrolled, headed, on this machine's browser.
//
//   NODE_PATH=<dir with playwright> node scripts/measure-queue-scroll.js
//   CHANNEL=msedge|chrome   the installed browser (default msedge)
//   GPU=0                   add --disable-gpu, so raster is software
//   ROWS=58,500             row counts, one run each
//   SKIN=linen              the skin worn
//   CSS=<file>              a stylesheet added after the board's, to try a change
//
// No hub and no room: the board is served from memory. Prints median and p95 frame time, frames over 20ms, and the
// paint and raster trace time per scrolled frame.

const http = require("http");
const path = require("path");
const fs = require("fs");

const ROOT = path.resolve(path.join(__dirname, ".."));
const { wholeBoard } = require(path.join(ROOT, "scripts", "board-source.js"));
let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) { ({ chromium } = require("playwright")); }
const HTML = wholeBoard();

const server = http.createServer((req, res) => {
  const p = new URL(req.url, "http://x").pathname;
  const json = (o) => { res.writeHead(200, { "Content-Type": "application/json" }); res.end(JSON.stringify(o)); };
  if (p.startsWith("/js/") || p.startsWith("/css/")) {
    const file = path.join(ROOT, "internal", "api", "web", p);
    if (!p.includes("..") && fs.existsSync(file)) {
      res.writeHead(200, { "Content-Type": file.endsWith(".css") ? "text/css" : "application/javascript" });
      return res.end(fs.readFileSync(file));
    }
  }
  if (p === "/" || p === "/index.html") { res.writeHead(200, { "Content-Type": "text/html" }); return res.end(HTML); }
  if (p.startsWith("/v1/events")) { res.writeHead(200, { "Content-Type": "text/event-stream" }); return res.write(": open"); }
  if (p === "/v1/tasks") return json({ tasks: [] });
  if (p === "/v1/health") return json({ build: "measure", settling: false, halted: false });
  if (p === "/v1/settings") return json({ global_auto: false, board_skin: process.env.SKIN || "linen", board_skins: [], scrollback_lines: 5000 });
  return json({});
});

async function run(browser, rows) {
  const ctx = await browser.newContext({ viewport: { width: 1500, height: 900 } });
  const page = await ctx.newPage();
  await page.goto(base + "/", { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => typeof yqGo === "function" && typeof switchView === "function");
  if (process.env.CSS) await page.addStyleTag({ content: fs.readFileSync(process.env.CSS, "utf8") });
  await page.evaluate((n) => {
    const ago = m => new Date(Date.now() - m * 60000).toISOString();
    const card = (id, more) => Object.assign({ id, status: "needs-input", supervised: true, pinned: false, title: id, display_title: id,
      tags: [], worktree: "/w/github/o/" + id, created_at: ago(300), last_activity_at: ago(1) }, more);
    const verbs = ["approve", "answer", "try", "sign"];
    window.__cards = [];
    for (let i = 0; i < n; i++) {
      window.__cards.push(i % 12 === 0 ? card("c" + i, { recap: "needs: a token", reported_at: ago(5 + i) })
        : card("c" + i, { ask: "approve the mock " + i, ask_at: ago(30 + i), ask_verb: verbs[i % 4], created_at: ago(10 + i) }));
    }
    boardCards = async () => window.__cards;
    lastTasks = window.__cards;
    switchView("queue");
  }, rows);
  await page.evaluate(() => yqRender());
  await page.waitForSelector("#yq-list .yq-row");
  const box = await page.evaluate(() => { const r = document.getElementById("yq-list").getBoundingClientRect(); return { x: r.x + r.width / 2, y: r.y + r.height / 2, sh: document.getElementById("yq-list").scrollHeight, ch: document.getElementById("yq-list").clientHeight }; });
  await page.mouse.move(box.x, box.y);
  await page.evaluate(() => {
    window.__ft = []; let last = performance.now();
    const tick = t => { window.__ft.push(t - last); last = t; requestAnimationFrame(tick); };
    requestAnimationFrame(tick);
  });
  const cdp = await ctx.newCDPSession(page);
  await cdp.send("Tracing.start", { transferMode: "ReturnAsStream", categories: "devtools.timeline,disabled-by-default-devtools.timeline,cc,gpu" });
  await page.waitForTimeout(300);
  await page.evaluate(() => { window.__ft = []; });
  const steps = 60;
  for (let i = 0; i < steps; i++) { await page.mouse.wheel(0, i < steps / 2 ? 120 : -120); await page.waitForTimeout(16); }
  await page.waitForTimeout(200);
  const ft = await page.evaluate(() => window.__ft.slice());
  const done = new Promise(r => cdp.once("Tracing.tracingComplete", r));
  await cdp.send("Tracing.end");
  const { stream } = await done;
  let text = "";
  for (;;) { const c = await cdp.send("IO.read", { handle: stream }); text += c.data; if (c.eof) break; }
  const evs = JSON.parse(text).traceEvents || JSON.parse(text);
  const sum = {};
  for (const e of evs) if (e.ph === "X" && /^(Paint|RasterTask|UpdateLayer|Layerize|PrePaint|Layout|CompositeLayers|ImageDecodeTask|UpdateLayoutTree)$/.test(e.name)) {
    sum[e.name] = (sum[e.name] || 0) + e.dur / 1000;
  }
  ft.sort((a, b) => a - b);
  const q = f => ft[Math.min(ft.length - 1, Math.floor(ft.length * f))];
  const out = { rows, scrollable: box.sh - box.ch, frames: ft.length, median: +q(0.5).toFixed(1), p95: +q(0.95).toFixed(1), max: +ft[ft.length - 1].toFixed(1), over20: ft.filter(x => x > 20).length };
  for (const k in sum) out[k + "_ms"] = +sum[k].toFixed(1);
  console.log(JSON.stringify(out));
  await ctx.close();
}

let base;
(async () => {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  base = "http://127.0.0.1:" + server.address().port;
  const args = process.env.GPU === "0" ? ["--disable-gpu"] : [];
  const browser = await chromium.launch({ channel: process.env.CHANNEL || "msedge", headless: false, args });
  console.log("channel", process.env.CHANNEL || "msedge", "gpu", process.env.GPU === "0" ? "off" : "on", "browser", browser.version());
  for (const n of (process.env.ROWS || "58,500").split(",")) await run(browser, +n);
  await browser.close();
  server.close();
})().catch(e => { console.error(e); process.exit(1); });
