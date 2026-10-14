// Terminals filter drawer, overlay (PREFIX=before, FD_TERMS_PUSH off) and accordion (PREFIX=after) (docs/changes/terms-drawer-push.md).
//
//   NODE_PATH=<dir with playwright> PREFIX=before node scripts/shoot-terms-drawer-push.js
//
// The Terminals list with the paused and dead chips off and every parent open, and the popups of a parent with children, a
// paused card and a dead card, at 1280px and 390px. Writes docs/changes/img/terms-drawer-push/<PREFIX>-<what>-<width>.png.

const http = require("http");
const fs = require("fs");
const path = require("path");
const { wholeBoard } = require("./board-source.js");

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) {
  try { ({ chromium } = require("playwright")); } catch (e2) { console.log("playwright is not installed"); process.exit(0); }
}

const WEB = path.join(__dirname, "..", "internal", "api", "web");
const OUT = path.join(__dirname, "..", "docs", "changes", "img", "terms-drawer-push");
const PREFIX = process.env.PREFIX || "after";
const HTML = wholeBoard();
const TYPES = { ".js": "text/javascript", ".css": "text/css", ".png": "image/png", ".svg": "image/svg+xml", ".gif": "image/gif", ".woff2": "font/woff2" };
const at = mins => new Date(Date.now() - mins * 60000).toISOString();
const card = (id, title, status, extra) => Object.assign({
  id, display_title: title, status, runner: "claude", rank: 1, worktree: "/w/" + id, why: "", idle_seconds: 0,
  wait_seconds: 0, created_at: at(600), last_activity_at: at(5), tags: [], supervised: false, offline: false,
  pinned: false, auto_approve: false, resume_id: "r-" + id
}, extra || {});
const SUB = "atrium:subagent";
const PARKED = { parked_at: at(40), last_activity_at: at(40) };
const live = (id, title, extra) => card(id, title, "running", Object.assign({ supervised: true, pid: 9, activity: { what: "tool", tool: "Bash" } }, extra));
const TASKS = [
  live("P", "build the thing", { tags: ["origin:agent"], worktree: "/w/build" }),
  live("k1", "worker one", { tags: [SUB], spawned_by_id: "P" }),
  card("k2", "worker two (asks)", "needs-input", { supervised: true, pid: 9, tags: [SUB], spawned_by_id: "P", ask: "which branch?" }),
  card("k3", "r-provision-sgg", "needs-input", Object.assign({ tags: [SUB], spawned_by_id: "P" }, PARKED)),
  card("k4", "attach-crossed-pane", "needs-input", Object.assign({ tags: [SUB], spawned_by_id: "P" }, PARKED)),
  card("k5", "launch-dialog", "paused", { tags: [SUB], spawned_by_id: "P" }),
  card("k6", "ci-triage", "dead", { tags: [SUB], spawned_by_id: "P" }),
  live("Q", "second runner", { tags: ["origin:agent"] }),
  card("g1", "clintify bakeoff round 2", "needs-input", Object.assign({ tags: [SUB], spawned_by_id: "Q" }, PARKED)),
  card("z1", "paused on its own", "paused"),
  card("d1", "dead with a folder", "dead")
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
    // Parents open (the subagents chip), the paused and dead chips off: how the list comes.
    await ctx.addInitScript(() => { try { localStorage.setItem("atrium.terms.show", JSON.stringify(["subagents"])); } catch (e) {} });
    const page = await ctx.newPage();
    await page.goto("http://127.0.0.1:" + server.address().port + "/");
    await page.waitForFunction(() => typeof switchView === "function" && document.querySelectorAll(".card, .stackrow").length > 0, null, { timeout: 15000 });
    const shot = what => page.screenshot({ path: path.join(OUT, `${PREFIX}-${what}-${width}.png`) });
    await page.evaluate(() => switchView("terms")); await sleep(900);
    await page.evaluate(p => { window.FD_TERMS_PUSH = p; }, PREFIX !== "before");
    await shot("closed");
    await page.evaluate(() => fdSet("terms", true)); await sleep(700);
    await shot("open");
    await ctx.close();
  }
  await browser.close();
  server.close();
})();
