// The growler reminder row in the settings dialog, in a real browser with every endpoint mocked.
//
//   NODE_PATH=<dir with playwright> node scripts/check-growl-ladder.js [shots-dir] [before]
//
// It serves the concatenated board from board-source.js, the way scripts/check-pr-paste.js does. It checks that the
// row shows what the hub answers (permission on, question off), that ticking question PUTs both reasons to
// /_hub/growl-ladder and the row repaints from the answer, and that a board with no hub draws no row. With a shots
// dir it writes the settings row as a PNG, named before.png or after.png (the word "before" as the second argument
// is for a tree that does not have the row yet, and skips the checks).

const http = require("http");
const fs = require("fs");
const path = require("path");
const { wholeBoard } = require("./board-source.js");

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) {
  try { ({ chromium } = require("playwright")); } catch (e2) { console.log("playwright is not installed"); process.exit(0); }
}

const WEB = path.join(__dirname, "..", "internal", "api", "web");
const SHOTS = process.argv[2] || "";
const BEFORE = process.argv[3] === "before";
const HTML = wholeBoard();
const TYPES = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".png": "image/png",
  ".svg": "image/svg+xml", ".gif": "image/gif", ".woff2": "font/woff2" };

let ladder, puts, hub;
const reset = () => { ladder = { permission: true, question: false, blocked: false, halt: true, "deploy-hold": true }; puts = []; hub = true; };
reset();

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
    return res.write(": open\n\n");
  }
  let body = "";
  for await (const c of req) body += c;
  if (p === "/_hub/growl-ladder") {
    if (!hub) return json({ error: "not a hub" }, 404);
    if (req.method === "PUT") { const set = JSON.parse(body); puts.push(set); Object.assign(ladder, set); }
    return json(ladder);
  }
  if (req.method !== "GET") return json({ ok: true });
  if (p === "/v1/harnesses") return json({ harnesses: [] });
  if (p === "/v1/providers") return json({ providers: [] });
  if (p === "/v1/tasks") return json({ tasks: [] });
  if (p === "/v1/health") return json({ build: "check", settling: false, halted: false });
  if (p === "/v1/settings") {
    return json({ global_auto: false, board_skin: "harbour", board_skins: ["harbour"], scrollback_lines: 5000 });
  }
  if (p.startsWith("/_hub/")) return json({ error: "not a hub" }, 404);
  return json({});
});

let fail = false;
const bad = msg => { console.error("FAIL: " + msg); fail = true; };

(async () => {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1280, height: 1000 } });
  const url = "http://127.0.0.1:" + server.address().port + "/";
  const openSettings = async () => {
    await page.evaluate(() => { const d = document.getElementById("settings"); if (d.open) d.close(); });
    await page.evaluate(() => document.getElementById("gear").click());
    await page.evaluate(() => showSettingsPane("notifications"));
    await page.waitForTimeout(400);
  };
  const shot = async name => {
    if (!SHOTS) return;
    fs.mkdirSync(SHOTS, { recursive: true });
    const row = page.locator(BEFORE ? "#s-growler" : "#s-gl-row").locator("xpath=ancestor-or-self::div[contains(@class,'field')]");
    await row.scrollIntoViewIfNeeded();
    await row.screenshot({ path: path.join(SHOTS, name) });
  };
  try {
    await page.goto(url);
    await page.waitForFunction(() => typeof openLaunch === "function");
    await openSettings();
    if (BEFORE) { await shot("before.png"); } else {
      const state = () => page.evaluate(() => ({ hidden: document.getElementById("s-gl-row").hidden,
        p: document.getElementById("s-gl-permission").checked, q: document.getElementById("s-gl-question").checked }));
      await page.waitForFunction(() => !document.getElementById("s-gl-row").hidden);
      let s = await state();
      if (!s.p || s.q) bad("the row should open as permission on, question off: " + JSON.stringify(s));
      await shot("after.png");
      await page.locator("#s-gl-question").check();
      await page.waitForFunction(() => /saved/.test(document.getElementById("s-gl-msg").textContent));
      if (puts.length !== 1 || puts[0].question !== true || puts[0].permission !== true) bad("tick question: " + JSON.stringify(puts));
      if (!ladder.question) bad("the hub did not keep question on");
      await shot("after-question-on.png");
      // Reopen: it reads the hub again and shows question on.
      await openSettings();
      s = await state();
      if (!s.q || !s.p) bad("reopened row should be both on: " + JSON.stringify(s));
      await page.locator("#s-gl-permission").uncheck();
      await page.waitForFunction(() => document.getElementById("s-gl-permission").checked === false);
      await page.waitForTimeout(200);
      if (puts.length !== 2 || puts[1].permission !== false) bad("untick permission: " + JSON.stringify(puts));
      // No hub: no row.
      reset(); hub = false;
      await page.goto(url);
      await page.waitForFunction(() => typeof openLaunch === "function");
      await openSettings();
      if (!(await state()).hidden) bad("a board with no hub should draw no row");
    }
  } catch (e) { bad(e.stack || String(e)); }
  await browser.close();
  server.close();
  if (fail) process.exit(1);
  console.log(BEFORE ? "before shot taken" : "growl ladder row ok");
})();
