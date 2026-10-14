#!/usr/bin/env node
// How long the launch dialog's ROOM and RUNNER dropdowns take to answer a click, on a full board, headless.
//
//   NODE_PATH=<dir with @playwright/test> node scripts/measure-launch-dropdowns.js [cards] [--profile out.json]
//
// A native select's list cannot be seen from the page, so what is measured is what holds it back: the Event Timing
// input delay of the press on the select (press to the first handler running), long tasks on the main thread, and
// what the CPU profile says was running. A mocked hub answers every read; `cards` agents are on the board, most of
// them supervised, and the hub streams a state change every 250ms the way a busy board does.

const http = require("http");
const fs = require("fs");
const path = require("path");
const { wholeBoard, web } = require("./board-source.js");

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) { ({ chromium } = require("playwright")); }

const N = +(process.argv[2] && /^\d+$/.test(process.argv[2]) ? process.argv[2] : 120);
const profileOut = process.argv.includes("--profile") ? process.argv[process.argv.indexOf("--profile") + 1] : "";
const STREAM = process.env.MEASURE_STREAM !== "0";

const STATUSES = ["running", "needs-input", "idle", "working", "done", "blocked"];
const ROOMS = ["claude-sg4", "sg4-control", "alpha", "beta", "gamma"];
function card(i) {
  const room = ROOMS[i % ROOMS.length];
  return {
    id: room + "~c" + i, status: STATUSES[i % STATUSES.length], display_title: "agent " + i + " working on a thing",
    runner: "claude", rank: i, worktree: "D:/worktrees/github/org" + (i % 7) + "/repo" + (i % 13) + "/branch-" + i,
    why: "reason " + i, idle_seconds: i * 3, wait_seconds: i % 5 ? 0 : 120, pid: 1000 + i, supervised: i % 4 !== 3,
    resume_id: "sess-" + i, created_at: "2026-10-12T09:00:00Z", last_activity_at: "2026-10-12T12:00:00Z",
    tags: ["g" + (i % 6), "t" + (i % 3)], offline: false, pinned: i % 9 === 0, auto_approve: false,
    ask: i % 5 === 0 ? "question " + i + "?" : "", asks_open: i % 5 === 0 ? 1 : 0,
    recap: "Did step " + i + " of the work and the checks pass.", recap_at: "2026-10-12T11:40:00Z", room,
  };
}
const CARDS = Array.from({ length: N }, (_, i) => card(i));
const HARNESS = (room, id) => ({ id: id || "claude", label: id || "claude", cmd: "claude", args: [], resume_args: [],
  prompt_args: [], model_args: [], effort_args: [], cwd: "", env: {}, launch_mode: "pty", enabled: true,
  found: "C:/x/claude.exe", rules_source: "", package: "", notes: "", room });
const HARNESSES = ROOMS.flatMap(r => [HARNESS(r, "claude"), HARNESS(r, "codex"), HARNESS(r, "gemini")]);
const ROOMROWS = ROOMS.map(r => ({ name: r, host: r + "-host", version: "abc" }));

const streams = [];
const HTML = wholeBoard();
const json = (res, o) => { res.writeHead(200, { "Content-Type": "application/json" }); res.end(JSON.stringify(o)); };
const server = http.createServer((req, res) => {
  const url = req.url.split("?")[0];
  if (url === "/" || url === "/index.html") { res.writeHead(200, { "Content-Type": "text/html" }); res.end(HTML); return; }
  if (url.startsWith("/vendor/") && !url.includes("..")) {
    fs.readFile(path.join(web, url), (err, body) => {
      if (err) { res.writeHead(404); res.end(""); return; }
      res.writeHead(200, { "Content-Type": url.endsWith(".css") ? "text/css" : "application/javascript" });
      res.end(body);
    });
    return;
  }
  if (url.startsWith("/v1/events") || url.startsWith("/_hub/events")) {
    res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" });
    res.write(": open\n\n");
    streams.push(res);
    return;
  }
  if (url === "/_hub/rooms") return json(res, { rooms: ROOMROWS });
  if (url === "/_hub/inventory") return json(res, { rooms: ROOMROWS.map(r => Object.assign({ transport: "direct", attached: true,
    first_seen: "2026-10-01T06:00:00Z", last_seen: "2026-10-12T12:00:00Z" }, r)) });
  if (url === "/v1/tasks") return json(res, { tasks: CARDS });
  if (/^\/v1\/tasks\/[^/]+\/(events|messages|asks)$/.test(url)) return json(res, { events: [], messages: [], asks: [] });
  if (/^\/v1\/tasks\/[^/]+$/.test(url)) return json(res, CARDS[0]);
  if (url === "/v1/harnesses") return json(res, { harnesses: HARNESSES });
  if (url === "/v1/fixtures") return json(res, { fixtures: [] });
  if (url === "/v1/settings") return json(res, { board_skin: "harbour", board_skins: ["harbour"], global_auto: false });
  if (url === "/v1/health") return json(res, { build: "measure", settling: false, halted: false });
  if (url === "/v1/history") return json(res, { tasks: [], total: 0 });
  if (url === "/v1/waiting") return json(res, { tasks: [] });
  if (url === "/v1/permissions") return json(res, { permissions: [] });
  if (url === "/v1/rooms") return json(res, { rooms: [] });
  if (url === "/v1/providers") return json(res, { providers: [] });
  if (url === "/v1/sources" || url === "/v1/recognisers" || url === "/v1/actions" || url === "/v1/dispatch" || url === "/v1/shares")
    return json(res, { sources: [], recognisers: [], actions: [], dispatches: [], shares: [] });
  res.writeHead(404); res.end("");
});

const pct = (a, p) => { const s = a.slice().sort((x, y) => x - y); return s.length ? s[Math.min(s.length - 1, Math.floor(s.length * p))] : 0; };

(async () => {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const base = `http://127.0.0.1:${server.address().port}/`;
  const browser = await chromium.launch({ args: process.env.CHROME_ARGS ? process.env.CHROME_ARGS.split(" ") : [] });
  const ctx = await browser.newContext({ viewport: { width: 1600, height: 1000 } });
  const p = await ctx.newPage();
  const errs = [];
  p.on("pageerror", e => errs.push(String(e)));
  await p.addInitScript(() => {
    sessionStorage.setItem("atrium.m.desktop", "1");
    window.__long = [];
    window.__ev = [];
    try { new PerformanceObserver(l => l.getEntries().forEach(e => window.__long.push([Math.round(e.startTime), Math.round(e.duration)])))
      .observe({ type: "longtask", buffered: true }); } catch (e) {}
    try { new PerformanceObserver(l => l.getEntries().forEach(e => window.__ev.push({ name: e.name, target: e.target && (e.target.id || e.target.tagName),
      start: Math.round(e.startTime), delay: Math.round(e.processingStart - e.startTime), total: Math.round(e.duration) })))
      .observe({ type: "event", durationThreshold: 16, buffered: true }); } catch (e) {}
  });
  // TERMS=n: the terminals view with n real xterm sessions open, each fed colour output all the time, the way agents
  // at work feed them.
  const TERMS = +(process.env.TERMS || 0);
  if (TERMS) {
    const line = Buffer.from("\x1b[32m✓\x1b[0m compiling \x1b[1mpackage/very/long/path/name.go\x1b[0m \x1b[33m....\x1b[0m done in 12ms\r\n".repeat(+process.env.LINES || 40));
    await p.routeWebSocket(/\/attach/, ws => {
      const t = setInterval(() => { try { ws.send(line); } catch (e) {} }, 40);
      ws.onClose(() => clearInterval(t));
      ws.onMessage(() => {});
    });
  }
  await p.goto(base, { waitUntil: "domcontentloaded" });
  await p.waitForFunction(() => typeof hubRooms !== "undefined" && hubRooms.length >= 2, null, { timeout: 30000 });
  await p.waitForTimeout(2500);
  if (TERMS) {
    const step = (what, pr) => Promise.race([pr, new Promise((_, no) => setTimeout(() => no(new Error("stuck: " + what)), 15000))]);
    console.error("terms: switching view");
    await step("switchView", p.evaluate(() => switchView("terms")));
    await p.waitForTimeout(500);
    for (let i = 0; i < TERMS; i++) {
      console.error("terms: opening " + i);
      await step("openTerm " + i, p.evaluate(i => { const t = [...cardRows.values()].filter(x => x.supervised)[i]; if (t) openTerm(t); }, i));
      await p.waitForTimeout(700);
    }
    console.error("terms: open " + await p.evaluate(() => document.querySelectorAll(".xterm").length));
    // VIEW=board: the dialog is opened from the board with the terminals kept running out of sight.
    if (process.env.VIEW) { await step("view", p.evaluate(v => switchView(v), process.env.VIEW)); await p.waitForTimeout(800); }
  }
  const painted = await p.evaluate(() => ({ stack: document.querySelectorAll("#stack-list .stackrow").length,
    terms: document.querySelectorAll("#term-list .card").length, nodes: document.getElementsByTagName("*").length }));

  // The hub's board is busy: a state change for a few cards every 250ms.
  let tick = 0;
  const pump = STREAM ? setInterval(() => {
    tick++;
    const t = CARDS[tick % CARDS.length];
    t.idle_seconds += 1;
    t.last_activity_at = new Date().toISOString();
    const line = "event: task\ndata: " + JSON.stringify({ kind: "status-changed", task_id: t.id, id: t.id }) + "\n\n";
    streams.forEach(r => { try { if (!r.destroyed) r.write(line); } catch (e) {} });
  }, 250) : null;
  await p.waitForTimeout(1500);

  const cdp = profileOut ? await ctx.newCDPSession(p) : null;
  if (cdp) { await cdp.send("Profiler.enable"); await cdp.send("Profiler.setSamplingInterval", { interval: 200 }); await cdp.send("Profiler.start"); }

  // 1. The idle thread: a probe that asks for a timer every 10ms and records how late each one ran.
  const idle = await p.evaluate(() => new Promise(done => {
    const lates = []; let last = performance.now(); const end = last + 6000;
    const tickf = () => { const n = performance.now(); lates.push(n - last - 10); last = n; if (n < end) setTimeout(tickf, 10); else done(lates); };
    setTimeout(tickf, 10);
  }));

  // 2. The real thing, many times: a trusted press on the select, and how long until the page ran its handler.
  const results = { room: [], runner: [], open: [] };
  for (let i = 0; i < 12; i++) {
    const t0 = Date.now();
    await p.click(".go.newagent");
    await p.waitForFunction(() => document.getElementById("launch").open, null, { timeout: 15000 });
    results.open.push(Date.now() - t0);
    for (const [name, sel] of [["room", "#l-room"], ["runner", "#l-harness"]]) {
      await p.evaluate(() => { window.__ev.length = 0; });
      const box = await p.locator(sel).boundingBox();
      await p.mouse.move(box.x + 20, box.y + 10);
      await p.mouse.down();
      await p.mouse.up();
      await p.waitForTimeout(250);
      const ev = await p.evaluate(() => window.__ev.filter(e => e.name === "mousedown" || e.name === "pointerdown"));
      const worst = ev.reduce((m, e) => Math.max(m, e.delay), 0);
      results[name].push(worst);
      await p.keyboard.press("Escape");
    }
    await p.evaluate(() => { const d = document.getElementById("launch"); if (d.open) d.close(); });
    await p.waitForTimeout(400);
  }
  if (pump) clearInterval(pump);

  const long = await p.evaluate(() => window.__long);
  let profile = null;
  if (cdp) { profile = (await cdp.send("Profiler.stop")).profile; fs.writeFileSync(profileOut, JSON.stringify(profile)); }

  const s = a => `p50 ${Math.round(pct(a, 0.5))}  p90 ${Math.round(pct(a, 0.9))}  max ${Math.round(Math.max(0, ...a))}`;
  console.log(`cards ${N}  painted ${JSON.stringify(painted)}  stream ${STREAM}  errors ${errs.length}`);
  console.log("idle timer lateness ms (10ms timer, 6s):", s(idle), " late>50ms:", idle.filter(x => x > 50).length);
  console.log("long tasks (>50ms) during run:", long.length, " total ms:", long.reduce((a, l) => a + l[1], 0), " worst:", Math.max(0, ...long.map(l => l[1])));
  console.log("click on + new agent to dialog open, ms:", s(results.open));
  console.log("press on ROOM select: input delay ms:", s(results.room));
  console.log("press on RUNNER select: input delay ms:", s(results.runner));
  if (profile) {
    const byId = new Map(profile.nodes.map(n => [n.id, n]));
    const self = new Map();
    const dt = profile.timeDeltas;
    profile.samples.forEach((id, i) => { const n = byId.get(id); const k = n.callFrame.functionName + " " + path.basename(n.callFrame.url || "") + ":" + n.callFrame.lineNumber;
      self.set(k, (self.get(k) || 0) + (dt[i] || 0)); });
    const top = [...self.entries()].sort((a, b) => b[1] - a[1]).slice(0, 15);
    console.log("top self time (ms) over the whole run:");
    top.forEach(([k, v]) => console.log("  " + Math.round(v / 1000) + "  " + k));
  }
  if (errs.length) console.log(errs.slice(0, 3).join("\n"));
  streams.forEach(r => { try { r.destroy(); } catch (e) {} });
  process.exit(0);
})();
