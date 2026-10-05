// Board performance measurements, one entry point. Reruns give a table that diffs.
//
//   node scripts/perf-board.js [--label NAME] [--out FILE.json] [--cards 120] [--live 12] [--terms 4]
//        [--reps 3] [--idle-secs 20] [--mem-mins 20] [--only load,idle,burst,attach,anims,mem,lag] [--lag-secs 30] [--lag-trace FILE] [--dir WORKTREE] [--inject EXPERIMENT.css]
//   node scripts/perf-board.js --diff A.json B.json
//
// README: starts its own `atrium preview` (own db, ports and address file, never the machine's atrium) on a copy of
// the board served from --dir (default this worktree, so a branch measures its own files), seeds --cards cards
// (--live of them working, the rest idle, every card a real pty), then measures in headed-headless Chromium on the
// real GPU: load (cold and service worker warm), idle CPU and GPU with --terms terminals attached, an SSE burst,
// attach to first byte painted (WebGL against the DOM renderer), and heap and node counts over --mem-mins (the item
// asked for hours, 20 minutes plus a growth rate per hour is the shortened form). Prints a markdown table, writes
// the numbers to --out. --diff prints two runs side by side with the change. Needs playwright or playwright-core
// resolvable (NODE_PATH) and a built atrium: ATRIUM_BIN, default build.claude/atrium. macOS only for GPU numbers.
const fs = require("fs");
const os = require("os");
const path = require("path");
const http = require("http");
const { spawn, execSync } = require("child_process");

const argv = process.argv.slice(2);
const opt = (n, d) => { const i = argv.indexOf("--" + n); return i >= 0 ? argv[i + 1] : d; };

// ── diff mode ────────────────────────────────────────────────────────────────
if (argv.includes("--diff")) {
  const [a, b] = argv.slice(argv.indexOf("--diff") + 1).map(f => JSON.parse(fs.readFileSync(f, "utf8")));
  const keys = [...new Set([...Object.keys(a.metrics), ...Object.keys(b.metrics)])];
  const w = Math.max(...keys.map(k => k.length));
  console.log(`| ${"metric".padEnd(w)} | ${a.label.padStart(10)} | ${b.label.padStart(10)} | change |`);
  console.log(`|${"-".repeat(w + 2)}|${"-".repeat(12)}|${"-".repeat(12)}|--------|`);
  for (const k of keys) {
    const x = a.metrics[k], y = b.metrics[k];
    const ch = typeof x === "number" && typeof y === "number" && x ? ((y - x) / Math.abs(x) * 100).toFixed(0) + "%" : "";
    console.log(`| ${k.padEnd(w)} | ${String(x ?? "").padStart(10)} | ${String(y ?? "").padStart(10)} | ${ch.padStart(6)} |`);
  }
  process.exit(0);
}

let pw;
try { pw = require("playwright"); } catch (e) { pw = require("playwright-core"); }
const { chromium } = pw;

const label = opt("label", "run");
const outFile = opt("out", "");
const NCARDS = +opt("cards", 120), NLIVE = +opt("live", 12), NTERMS = +opt("terms", 4);
const REPS = +opt("reps", 3), IDLE_SECS = +opt("idle-secs", 20), MEM_MINS = +opt("mem-mins", 20);
const only = new Set(opt("only", "load,idle,burst,attach,anims,mem").split(","));
const root = path.resolve(opt("dir", path.join(__dirname, "..")));
const bin = process.env.ATRIUM_BIN || path.join(root, "build.claude", "atrium");
const work = fs.mkdtempSync(path.join(os.tmpdir(), "perf-board-"));
const PORT = 18100 + Math.floor(Math.random() * 400);
const BOARD = `http://127.0.0.1:${PORT}`, AGENT = `http://127.0.0.1:${PORT + 1}`;
const sleep = ms => new Promise(r => setTimeout(r, ms));
const M = {}; // the table: name -> number
const median = a => { const s = [...a].sort((x, y) => x - y); return s.length ? s[Math.floor(s.length / 2)] : NaN; };
const r1 = n => Math.round(n * 10) / 10;
const log = (...a) => console.error("[perf]", ...a);

async function j(method, url, body) {
  const r = await fetch(url, { method, headers: { "content-type": "application/json" }, body: body ? JSON.stringify(body) : undefined });
  const t = await r.text();
  if (!r.ok) throw new Error(method + " " + url + " " + r.status + " " + t);
  return t ? JSON.parse(t) : null;
}

// ── the hub and its cards ────────────────────────────────────────────────────
let hub, ids = [];
async function startHub() {
  const fake = path.join(work, "fakeagent.sh");
  fs.writeFileSync(fake, `#!/bin/bash
i=0
while true; do i=$((i+1))
printf '\\033[36m● Read\\033[0m(src/file%d.go)\\n' $((i%40)); printf '  \\033[2m⎿ %d lines\\033[0m\\n' $((RANDOM%400))
printf '\\033[33m✻ Thinking… (%ds · esc to interrupt)\\033[0m\\n' $((i%90)); sleep \${FAKE_DELAY:-0.15}; done
`, { mode: 0o755 });
  const env = { ...process.env }; delete env.ATRIUM_LOCATION; delete env.ATRIUM_DEBUG_INPUTLAG;
  hub = spawn(bin, ["preview", "--dir", root, "--db", path.join(work, "p.db"), "--http", PORT, "--agent", PORT + 1, "--live-board"],
    { env, stdio: ["ignore", "ignore", fs.openSync(path.join(work, "hub.log"), "w")] });
  for (let i = 0; i < 100; i++) { try { await fetch(BOARD + "/v1/tasks"); break; } catch (e) { await sleep(200); } }
  await j("PUT", BOARD + "/v1/harnesses/fake", { label: "fake", enabled: true, cmd: "fakeagent", bin_path: fake, launch_mode: "pty" });
  await j("PUT", BOARD + "/v1/harnesses/idle", { label: "idle", enabled: true, cmd: "cat", bin_path: "/bin/cat", launch_mode: "pty" });
  const repos = ["api", "board", "fabric", "runtime", "ui", "docs", "zrok", "ziti", "edge", "tunnel"];
  for (let i = 0; i < NCARDS; i++) {
    const n = "card" + String(i).padStart(3, "0"), repo = repos[i % repos.length];
    const dir = path.join(work, "w", n); fs.mkdirSync(dir, { recursive: true });
    const t = await j("POST", BOARD + "/v1/launch", { harness: i < Math.max(NLIVE, NTERMS) ? "fake" : "idle", cwd: dir, name: n,
      title: n + " " + repo + " work", repo: "openziti/" + repo, branch: "claude/" + n });
    ids.push([t.id, t.wire_name || n]);
  }
  log("seeded", ids.length, "cards");
}
const actStat = {};
const act = (i, ev, tool) => fetch(AGENT + "/activity", { method: "POST", headers: { "content-type": "application/json" },
  body: JSON.stringify({ agent: ids[i][1], task_id: ids[i][0], event: ev, tool }) }).then(r => { actStat[r.status] = (actStat[r.status] || 0) + 1; }).catch(() => { actStat.err = (actStat.err || 0) + 1; });
async function setLive() {
  const tools = ["Bash", "Edit", "Read", "Grep"];
  for (let i = 0; i < ids.length; i++) await (i < NLIVE ? (i % 3 === 2 ? act(i, "prompt") : act(i, "tool-start", tools[i % 4])) : act(i, "idle"));
}

// ── the browser ──────────────────────────────────────────────────────────────
async function launch(opts = {}) {
  // headed-headless on the real GPU: the default headless build forces SwiftShader, which measures the CPU and not the GPU.
  const b = await chromium.launch({ headless: false, executablePath: process.env.PERF_CHROME || chromium.executablePath(),
    ignoreDefaultArgs: ["--enable-unsafe-swiftshader"], args: ["--headless=new", "--window-size=1440,900"] });
  const ctx = await b.newContext({ viewport: { width: 1440, height: 813 }, deviceScaleFactor: 2, ...opts });
  // --inject FILE.css: an experiment, applied to every page before it paints, so a fix can be measured without editing it in.
  if (opt("inject", "")) { const css = fs.readFileSync(opt("inject", ""), "utf8");
    await ctx.addInitScript(c => { document.addEventListener("DOMContentLoaded", () => { const st = document.createElement("style"); st.textContent = c; document.head.appendChild(st); }); }, css); }
  return { b, ctx, page: await ctx.newPage() };
}
function psTime(pid) {
  try {
    const m = execSync(`ps -o time= -p ${pid}`).toString().trim().match(/(?:(\d+):)?(\d+):(\d+\.\d+)/) || [];
    return (+(m[1] || 0)) * 3600 + (+m[2]) * 60 + (+m[3]);
  } catch (e) { return NaN; }
}
const metricsOf = async cs => Object.fromEntries((await cs.send("Performance.getMetrics")).metrics.map(x => [x.name, x.value]));
// CPU of every browser process over a window, as a percent of one core, plus the renderer's own work split.
async function cpuWindow(page, bs, ms) {
  const cs = await page.context().newCDPSession(page); await cs.send("Performance.enable");
  const m0 = await metricsOf(cs), p0 = (await bs.send("SystemInfo.getProcessInfo")).processInfo;
  const ps0 = Object.fromEntries(p0.map(p => [p.id, psTime(p.id)])); const t0 = Date.now();
  await sleep(ms);
  const dt = (Date.now() - t0) / 1000, m1 = await metricsOf(cs), p1 = (await bs.send("SystemInfo.getProcessInfo")).processInfo;
  const ps1 = Object.fromEntries(p1.map(p => [p.id, psTime(p.id)]));
  await cs.detach();
  const o = { gpu: 0, renderer: 0, browser: 0 };
  for (const p of p1) { const q = p0.find(x => x.id === p.id); if (!q) continue;
    const v = (ps1[p.id] - ps0[p.id]) / dt * 100; // ps, not cpuTime: cpuTime is coarse on macOS
    if (p.type === "GPU") o.gpu += v; else if (p.type === "renderer") o.renderer = Math.max(o.renderer, v); else if (p.type === "browser") o.browser += v; }
  o.script = (m1.ScriptDuration - m0.ScriptDuration) / dt * 100;
  o.layout = (m1.LayoutDuration - m0.LayoutDuration) / dt * 100;
  o.style = (m1.RecalcStyleDuration - m0.RecalcStyleDuration) / dt * 100;
  o.layouts = (m1.LayoutCount - m0.LayoutCount) / dt; o.styles = (m1.RecalcStyleCount - m0.RecalcStyleCount) / dt;
  o.dt = dt; return o;
}
// frames the compositor drew in a window: from a short trace, which is the only honest fps for "does it repaint at idle".
async function fpsWindow(bs, ms) {
  let n = 0; const on = e => { for (const x of e.value) if (x.name === "Display::DrawAndSwap") n++; };
  bs.on("Tracing.dataCollected", on);
  const done = new Promise(r => bs.once("Tracing.tracingComplete", r));
  await bs.send("Tracing.start", { transferMode: "ReportEvents", traceConfig: { recordMode: "recordUntilFull", includedCategories: ["viz"] } });
  await sleep(ms); await bs.send("Tracing.end"); await done; bs.off("Tracing.dataCollected", on);
  return n / (ms / 1000);
}
const rowsJS = () => document.querySelectorAll("#term-list .card.tab, #board .card, #stack [data-id]").length;
async function openBoard(page, view) {
  await page.goto(BOARD, { waitUntil: "load" });
  await page.waitForFunction(() => typeof switchView === "function");
  await page.evaluate(v => switchView(v), view); await page.waitForTimeout(1500);
}
async function attachN(page, n) {
  for (let i = 0; i < n; i++) {
    await page.evaluate(id => attachTask(id), ids[i][0]); await page.waitForTimeout(1800);
  }
}

// ── measurements ─────────────────────────────────────────────────────────────
async function mLoad() {
  // cold: a new context every rep (no HTTP cache, no service worker). warm: same context, worker active, reloaded.
  const cold = [], warm = [];
  for (let r = 0; r < REPS; r++) {
    const { b, ctx, page } = await launch();
    for (const mode of ["cold", "warm"]) {
      if (mode === "warm") { await page.evaluate(() => navigator.serviceWorker && navigator.serviceWorker.ready); await sleep(1000); }
      await page.addInitScript(() => {
        window.__lt = 0; try { new PerformanceObserver(l => { for (const e of l.getEntries()) window.__lt += Math.max(0, e.duration - 50); }).observe({ type: "longtask", buffered: true }); } catch (e) {}
      });
      const t0 = Date.now();
      await page.goto(BOARD, { waitUntil: "load" });
      // interactive: the board's own scripts are up, and the first poll has painted rows.
      await page.waitForFunction(() => typeof switchView === "function" && document.querySelectorAll("[data-id]").length > 0, null, { timeout: 30000 });
      const inter = Date.now() - t0;
      await page.waitForTimeout(1500);
      const d = await page.evaluate(() => {
        const nav = performance.getEntriesByType("navigation")[0], res = performance.getEntriesByType("resource");
        const fcp = (performance.getEntriesByName("first-contentful-paint")[0] || {}).startTime;
        return { ttfb: nav.responseStart, fcp, dcl: nav.domContentLoadedEventEnd, load: nav.loadEventEnd, tbt: window.__lt,
          reqs: res.length + 1, kb: (res.reduce((s, e) => s + (e.transferSize || 0), nav.transferSize || 0)) / 1024,
          rawkb: (res.reduce((s, e) => s + (e.decodedBodySize || 0), nav.decodedBodySize || 0)) / 1024,
          nodes: document.getElementsByTagName("*").length };
      });
      d.inter = inter; (mode === "cold" ? cold : warm).push(d);
    }
    await b.close();
  }
  for (const [mode, a] of [["cold", cold], ["warm", warm]]) {
    for (const k of ["ttfb", "fcp", "dcl", "load", "inter", "tbt"]) M[`load.${mode}.${k}_ms`] = Math.round(median(a.map(x => x[k])));
    M[`load.${mode}.requests`] = median(a.map(x => x.reqs));
    M[`load.${mode}.transfer_kb`] = Math.round(median(a.map(x => x.kb)));
  }
  M["load.decoded_kb"] = Math.round(median(cold.map(x => x.rawkb)));
  M["load.dom_nodes"] = median(cold.map(x => x.nodes));
}

async function mIdle() {
  const { b, page } = await launch(); const bs = await b.newBrowserCDPSession();
  await openBoard(page, "terms"); await attachN(page, NTERMS);
  for (const [name, view] of [["terms", "terms"], ["board", "board"], ["stack", "stack"]]) {
    await page.evaluate(v => switchView(v), view); await page.waitForTimeout(2500);
    if (name === "terms") { await page.evaluate(id => attachTask(id), ids[0][0]); await page.waitForTimeout(2500); }
    const cpu = await cpuWindow(page, bs, IDLE_SECS * 1000);
    const fps = await fpsWindow(bs, 5000);
    const k = `idle.${name}`;
    M[`${k}.gpu_cpu_pct`] = r1(cpu.gpu); M[`${k}.renderer_cpu_pct`] = r1(cpu.renderer); M[`${k}.browser_cpu_pct`] = r1(cpu.browser);
    M[`${k}.script_pct`] = r1(cpu.script); M[`${k}.layouts_per_s`] = r1(cpu.layouts); M[`${k}.styles_per_s`] = r1(cpu.styles);
    M[`${k}.frames_per_s`] = r1(fps);
  }
  M["idle.anims_running"] = await page.evaluate(() => document.getAnimations().filter(a => a.playState === "running").length);
  await b.close();
}

let burstSeq = 0;
// What is animating at idle, by name: the first place to look when frames_per_s is 60 and nothing is happening.
async function mAnims() {
  const { b, page } = await launch();
  await openBoard(page, "terms"); await attachN(page, NTERMS);
  for (const view of ["board", "terms", "stack"]) {
    await page.evaluate(v => switchView(v), view); await page.waitForTimeout(2500);
    const a = await page.evaluate(() => { const m = {}; for (const x of document.getAnimations()) if (x.playState === "running") { const n = x.animationName || x.constructor.name + ":" + (x.transitionProperty || ""); m[n] = (m[n] || 0) + 1; } return m; });
    for (const [n, c] of Object.entries(a)) M[`anims.${view}.${n}`] = c;
  }
  await b.close();
}

async function mBurst() {
  const { b, ctx, page } = await launch();
  // The stream's own "task" events, counted from a listener of ours, so the table says how many arrived.
  await ctx.addInitScript(() => {
    window.__sse = { n: 0 }; const E = window.EventSource;
    window.EventSource = new Proxy(E, { construct(T, a) { const s = new T(...a); s.addEventListener("task", () => { window.__sse.n++; }); return s; } });
  });
  await openBoard(page, "board");
  for (const view of ["board", "terms", "stack"]) {
    await page.evaluate(v => switchView(v), view); await page.waitForTimeout(1500);
    // Every row node is tagged, so after the burst the ones that are not tagged were rebuilt. Mutations are counted
    // on the document, fetches on window.fetch (the board re-reads lists on events).
    await page.evaluate(() => {
      document.querySelectorAll("[data-id]").forEach(e => { e.__keep = 1; });
      window.__mut = { added: 0, removed: 0, attrs: 0, text: 0 };
      window.__mo && window.__mo.disconnect();
      window.__mo = new MutationObserver(l => { for (const m of l) {
        if (m.type === "childList") { for (const n of m.addedNodes) if (n.nodeType === 1) window.__mut.added++; for (const n of m.removedNodes) if (n.nodeType === 1) window.__mut.removed++; }
        else if (m.type === "attributes") window.__mut.attrs++; else window.__mut.text++; } });
      window.__mo.observe(document.body, { subtree: true, childList: true, attributes: true, characterData: true });
      window.__r0 = performance.getEntriesByType("resource").length; window.__sse0 = window.__sse ? window.__sse.n : 0;
      window.__lt2 = 0; window.__po && window.__po.disconnect();
      try { window.__po = new PerformanceObserver(l => { for (const e of l.getEntries()) window.__lt2 += Math.max(0, e.duration - 50); }); window.__po.observe({ type: "longtask" }); } catch (e) {}
    });
    const cs = await page.context().newCDPSession(page); await cs.send("Performance.enable");
    const m0 = await metricsOf(cs); const t0 = Date.now();
    // 200 card changes in about 1 s across 50 cards, each one a `task` event carrying the whole row (a `why` edit, the
    // cheapest write that always publishes) with an activity hook beside it, which is what sixteen live agents send.
    // Activity alone is not enough: the hub publishes a card only when something on its row changed.
    const tools = ["Bash", "Edit", "Read", "Grep"];
    for (let k = 0; k < 4; k++) {
      const ps = [];
      for (let i = 0; i < Math.min(50, ids.length); i++) {
        ps.push(j("PATCH", BOARD + "/v1/tasks/" + ids[i][0], { why: "burst " + burstSeq + " " + i }));
        ps.push(act(i, k % 2 ? "tool-end" : "tool-start", tools[i % 4]));
      }
      burstSeq++; await Promise.all(ps); await sleep(200);
    }
    await page.waitForTimeout(2000); 
    const m1 = await metricsOf(cs), dt = (Date.now() - t0) / 1000;
    const d = await page.evaluate(() => ({ mut: window.__mut, lt: window.__lt2, sse: window.__sse.n - window.__sse0,
      fetch: (rs => ({ n: rs.length, bytes: rs.reduce((t, e) => t + (e.transferSize || 0), 0) }))(performance.getEntriesByType("resource").slice(window.__r0).filter(e => e.name.includes("/v1/"))),
      rows: document.querySelectorAll("[data-id]").length, rebuilt: [...document.querySelectorAll("[data-id]")].filter(e => !e.__keep).length }));
    await cs.detach();
    const k = `burst.${view}`;
    M[`${k}.script_ms`] = Math.round((m1.ScriptDuration - m0.ScriptDuration) * 1000);
    M[`${k}.style_ms`] = Math.round((m1.RecalcStyleDuration - m0.RecalcStyleDuration) * 1000);
    M[`${k}.layout_ms`] = Math.round((m1.LayoutDuration - m0.LayoutDuration) * 1000);
    M[`${k}.layouts`] = m1.LayoutCount - m0.LayoutCount; M[`${k}.styles`] = m1.RecalcStyleCount - m0.RecalcStyleCount;
    M[`${k}.long_task_ms`] = Math.round(d.lt);
    M[`${k}.sse_events`] = d.sse; M[`${k}.fetches`] = d.fetch.n; M[`${k}.fetch_kb`] = Math.round(d.fetch.bytes / 1024);
    M[`${k}.dom_added`] = d.mut.added; M[`${k}.dom_removed`] = d.mut.removed; M[`${k}.attr_writes`] = d.mut.attrs; M[`${k}.text_writes`] = d.mut.text;
    M[`${k}.rows_rebuilt`] = `${d.rebuilt}/${d.rows}`;
  }
  await b.close();
}

async function mAttach() {
  const { b, ctx, page } = await launch();
  await ctx.addInitScript(() => {
    window.__ws = []; const W = window.WebSocket;
    window.WebSocket = new Proxy(W, { construct(T, a) { const s = new T(...a); const r = { t0: performance.now(), first: 0 }; window.__ws.push(r);
      s.addEventListener("message", () => { if (!r.first) r.first = performance.now(); }); return s; } });
  });
  await openBoard(page, "terms");
  // Nothing kept, so every attach is a fresh one with a ws of its own, and one throwaway attach pays the page's cold start.
  await page.evaluate(() => localStorage.setItem("atrium.termKeep", "0"));
  await page.evaluate(id => attachTask(id), ids[Math.min(8, ids.length - 1)][0]); await page.waitForTimeout(2500);
  for (const mode of ["webgl", "dom"]) {
    if (mode === "dom") await page.evaluate(() => { window.WebglAddon = undefined; });
    const wsT = [], paintT = [], gl = [];
    for (let i = 0; i < Math.min(REPS * 2, ids.length, 8); i++) {
      const idx = i % Math.max(NLIVE, NTERMS, 1) % ids.length; // streaming cards, a fresh attach each time
      await page.evaluate(() => { window.__ws.length = 0; });
      const t0 = await page.evaluate(id => { window.__c0 = performance.now(); attachTask(id); return window.__c0; }, ids[idx][0]);
      // painted: two animation frames after the first byte reached the page, which is when xterm has written and drawn it.
      const r = await page.evaluate(() => new Promise(res => {
        const t = setInterval(() => { const w = window.__ws[0]; if (w && w.first) { clearInterval(t); requestAnimationFrame(() => requestAnimationFrame(() => res({ first: w.first - window.__c0, painted: performance.now() - window.__c0, gl: (typeof term !== "undefined" && !!term && !!term._atriumGl) }))); } }, 1);
        setTimeout(() => { clearInterval(t); res(null); }, 8000); }));
      if (r) { wsT.push(r.first); paintT.push(r.painted); gl.push(r.gl); }
      await page.waitForTimeout(800);
    }
    M[`attach.${mode}.first_byte_ms`] = Math.round(median(wsT)); M[`attach.${mode}.painted_ms`] = Math.round(median(paintT));
    M[`attach.${mode}.gl_on`] = gl.length ? (gl.every(Boolean) ? "yes" : gl.some(Boolean) ? "some" : "no") : "n/a";
  }
  await b.close();
}

async function mMem() {
  const { b, page } = await launch();
  await openBoard(page, "terms"); await attachN(page, NTERMS);
  const cs = await page.context().newCDPSession(page); await cs.send("Performance.enable"); await cs.send("HeapProfiler.enable");
  const pts = []; const t0 = Date.now(); const every = MEM_MINS >= 10 ? 60000 : 15000; let tick = 0, last = -1;
  const sample = async () => {
    await cs.send("HeapProfiler.collectGarbage"); const m = await metricsOf(cs);
    const extra = await page.evaluate(() => ({ timers: 0, terms: (window.termSlots ? Object.keys(window.termSlots).length : 0) })).catch(() => ({}));
    pts.push({ min: (Date.now() - t0) / 60000, heap: m.JSHeapUsedSize / 1048576, nodes: m.Nodes, listeners: m.JSEventListeners, docs: m.Documents, ...extra });
    log("mem", JSON.stringify(pts[pts.length - 1]));
  };
  await sample();
  // Load for the whole run: chatter on twelve cards, a view cycle and a terminal switch once a minute, which is how a
  // board left open all day behaves.
  const end = t0 + MEM_MINS * 60000, nextSample = () => t0 + pts.length * every;
  while (Date.now() < end) {
    const i = tick++ % NLIVE; await act(i, tick % 2 ? "tool-end" : "tool-start", ["Bash", "Edit", "Read"][tick % 3]); await sleep(150);
    if (tick % 100 === 0) { const v = ["board", "stack", "terms"][(tick / 100) % 3]; await page.evaluate(v => switchView(v), v).catch(() => {});
      if (v === "terms") await page.evaluate(id => attachTask(id), ids[(tick / 100) % NTERMS][0]).catch(() => {}); }
    if (Date.now() >= nextSample()) await sample();
  }
  await sample();
  // Growth is fitted from minute 2 on a long run, so the terminals attaching at the start are not counted as a leak.
  const fp = MEM_MINS >= 10 ? pts.filter(p => p.min >= 2) : pts;
  const fit = k => { const pts = fp, n = pts.length, sx = pts.reduce((s, p) => s + p.min, 0), sy = pts.reduce((s, p) => s + p[k], 0),
    sxy = pts.reduce((s, p) => s + p.min * p[k], 0), sxx = pts.reduce((s, p) => s + p.min * p.min, 0); return (n * sxy - sx * sy) / (n * sxx - sx * sx) * 60; };
  M["mem.minutes"] = MEM_MINS; M["mem.heap_start_mb"] = r1(pts[0].heap); M["mem.heap_end_mb"] = r1(pts[pts.length - 1].heap);
  M["mem.heap_growth_mb_per_h"] = r1(fit("heap")); M["mem.nodes_start"] = pts[0].nodes; M["mem.nodes_end"] = pts[pts.length - 1].nodes;
  M["mem.nodes_growth_per_h"] = Math.round(fit("nodes")); M["mem.listeners_start"] = pts[0].listeners; M["mem.listeners_end"] = pts[pts.length - 1].listeners;
  M["mem.listeners_growth_per_h"] = Math.round(fit("listeners"));
  M._memseries = pts.map(p => [r1(p.min), r1(p.heap), p.nodes, p.listeners]);
  await b.close();
}

// Typing while --terms terminals stream and the live cards chatter: long tasks per minute, the worst animation frame,
// the GPU process, and every layout script forced, by who forced it and whether it was inside an animation frame.
// --lag-secs (default 30) long, --lag-trace FILE keeps the trace. The repro for the "rAF handler took 147ms" report.
async function mLag() {
  const secs = +opt("lag-secs", 30);
  const { b, ctx, page } = await launch(); const bs = await b.newBrowserCDPSession();
  // --lag-dom: no WebGL addon, so every terminal draws with xterm's DOM renderer, as it does when WebGL is refused.
  if (argv.includes("--lag-dom")) await ctx.addInitScript(() => { Object.defineProperty(window, "WebglAddon", { get: () => undefined, set() {} }); });
  await openBoard(page, "terms"); await attachN(page, NTERMS);
  await page.waitForTimeout(2000);
  // xterm's viewport refresh, the frame callback that forces the layout, timed on the shown terminal and on the kept
  // hidden ones apart. A private method of the vendored xterm 5.5, read here only to measure.
  await page.evaluate(() => {
    window.__vp = { shown: { n: 0, ms: 0, max: 0 }, hidden: { n: 0, ms: 0, max: 0 } };
    const vp = term && term._core && term._core.viewport;
    if (!vp) return;
    const P = Object.getPrototypeOf(vp), real = P._innerRefresh;
    P._innerRefresh = function () {
      const t0 = performance.now(); const r = real.apply(this, arguments); const d = performance.now() - t0;
      const k = this._viewportElement && this._viewportElement.offsetParent ? "shown" : "hidden";
      const o = window.__vp[k]; o.n++; o.ms += d; o.max = Math.max(o.max, d); return r;
    };
  });
  // --lag-probe: who asks the selection to redraw, by stack, since a DOM renderer redraws every row for it even paused.
  if (argv.includes("--lag-probe")) await page.evaluate(() => {
    window.__selwho = {}; const ss = term._core._selectionService, P = Object.getPrototypeOf(ss), real = P.refresh;
    P.refresh = function () { const k = String(new Error().stack).split("\n").slice(2, 6).map(l => l.trim().replace(/https?:\/\/[^/]+/, "")).join(" < ");
      window.__selwho[k] = (window.__selwho[k] || 0) + 1; return real.apply(this, arguments); };
  });
  const cs = await page.context().newCDPSession(page);
  const events = [];
  cs.on("Tracing.dataCollected", e => { for (const x of e.value) events.push(x); });
  const done = new Promise(r => cs.once("Tracing.tracingComplete", r));
  await cs.send("Tracing.start", { transferMode: "ReportEvents", traceConfig: { recordMode: "recordContinuously", includedCategories: [
    "devtools.timeline", "disabled-by-default-devtools.timeline", "disabled-by-default-devtools.timeline.stack", "toplevel"] } });
  let stop = false, tick = 0;
  // Activity on the live cards four times a second, and every two seconds a burst of card edits across 50 cards, so
  // the board re-renders its lists while the terminals stream: what a hub with a dozen working agents sends.
  const chatter = (async () => { while (!stop) { const i = tick++ % Math.max(NLIVE, 1);
    await act(i, tick % 2 ? "tool-end" : "tool-start", ["Bash", "Edit", "Read"][tick % 3]);
    if (tick % 8 === 0) await Promise.all(ids.slice(0, 50).map((c, k) => j("PATCH", BOARD + "/v1/tasks/" + c[0], { why: "lag " + tick + " " + k }).catch(() => {})));
    await sleep(250); } })();
  // The visible terminal is the last attached, a fake agent: what is typed goes nowhere that matters.
  await page.focus("#t-screen textarea").catch(() => {});
  const typing = (async () => { while (!stop) { await page.keyboard.press("a").catch(() => {}); await sleep(150); } })();
  const cpu = await cpuWindow(page, bs, secs * 1000);
  stop = true; await Promise.all([chatter, typing]);
  await cs.send("Tracing.end"); await done;
  if (opt("lag-trace", "")) fs.writeFileSync(opt("lag-trace", ""), JSON.stringify({ traceEvents: events }));
  const main = events.find(e => e.name === "thread_name" && e.args && e.args.name === "CrRendererMain" &&
    events.some(x => x.pid === e.pid && x.tid === e.tid && x.name === "FireAnimationFrame"));
  const on = events.filter(e => main && e.pid === main.pid && e.tid === main.tid && e.ph === "X");
  const ms = e => (e.dur || 0) / 1000;
  const tasks = on.filter(e => e.name === "RunTask"), raf = on.filter(e => e.name === "FireAnimationFrame");
  const inRaf = e => raf.some(o => e.ts >= o.ts && e.ts < o.ts + o.dur);
  const forced = on.filter(e => (e.name === "Layout" || e.name === "UpdateLayoutTree") && e.args && e.args.beginData &&
    e.args.beginData.stackTrace && e.args.beginData.stackTrace.length);
  const who = {};
  for (const e of forced) {
    const f = e.args.beginData.stackTrace.slice(0, +opt("lag-depth", 2)).map(f => (f.functionName || "(anon)") + "@" + String(f.url || "").split("/").pop() + ":" + f.lineNumber + ":" + f.columnNumber).join(" < ");
    const k = (inRaf(e) ? "rAF " : "task ") + e.name + " " + f;
    who[k] = who[k] || { n: 0, ms: 0 }; who[k].n++; who[k].ms += ms(e);
  }
  for (const [k, v] of Object.entries(who).sort((a, b) => b[1].ms - a[1].ms).slice(0, 12)) log("forced", v.n + "x", v.ms.toFixed(1) + "ms", k);
  M["lag.long_tasks_per_min"] = r1(tasks.filter(e => ms(e) >= 50).length * 60 / secs);
  M["lag.worst_task_ms"] = r1(tasks.reduce((m, e) => Math.max(m, ms(e)), 0));
  M["lag.worst_raf_ms"] = r1(raf.reduce((m, e) => Math.max(m, ms(e)), 0));
  M["lag.raf_over_16ms"] = raf.filter(e => ms(e) > 16).length;
  M["lag.forced_layouts_in_raf"] = forced.filter(inRaf).length;
  M["lag.forced_layout_ms_in_raf"] = r1(forced.filter(inRaf).reduce((t, e) => t + ms(e), 0));
  M["lag.forced_layouts"] = forced.length;
  M["lag.layout_ms_per_s"] = r1(on.filter(e => e.name === "Layout").reduce((t, e) => t + ms(e), 0) / secs);
  M["lag.gpu_cpu_pct"] = r1(cpu.gpu); M["lag.renderer_cpu_pct"] = r1(cpu.renderer);
  if (argv.includes("--lag-probe")) log("kept", JSON.stringify(await page.evaluate(() => Array.from(keptTerms.values()).map(s => {
    const rs = s.term._core._renderService; return { gl: !!s.term._atriumGl, paused: rs._isPaused, renderer: rs._renderer && rs._renderer.value && rs._renderer.value.constructor.name }; }))));
  if (argv.includes("--lag-probe")) log("selection refresh", JSON.stringify(await page.evaluate(() => window.__selwho), null, 1));
  const vp = await page.evaluate(() => window.__vp);
  for (const k of ["shown", "hidden"]) { M[`lag.viewport_${k}_calls`] = vp[k].n; M[`lag.viewport_${k}_ms`] = r1(vp[k].ms); M[`lag.viewport_${k}_max_ms`] = r1(vp[k].max); }
  M["lag.webgl_contexts"] = await page.evaluate(() => [...document.querySelectorAll(".xterm canvas")].filter(c => !c.classList.contains("xterm-link-layer")).length);
  await b.close();
}

function table() {
  const keys = Object.keys(M).filter(k => !k.startsWith("_")); const w = Math.max(...keys.map(k => k.length));
  const lines = [`| ${"metric".padEnd(w)} | ${label.padStart(10)} |`, `|${"-".repeat(w + 2)}|${"-".repeat(12)}|`];
  for (const k of keys) lines.push(`| ${k.padEnd(w)} | ${String(M[k]).padStart(10)} |`);
  return lines.join("\n");
}

(async () => {
  const stop = () => { try { hub && hub.kill(); } catch (e) {} };
  process.on("exit", stop); process.on("SIGINT", () => { stop(); process.exit(130); });
  await startHub(); await setLive(); await sleep(1500);
  for (const [name, fn] of [["load", mLoad], ["idle", mIdle], ["burst", mBurst], ["attach", mAttach], ["anims", mAnims], ["mem", mMem], ["lag", mLag]]) {
    if (!only.has(name)) continue;
    log("measuring", name); await setLive(); await sleep(1000);
    try { await fn(); } catch (e) { M[name + ".error"] = String(e.message || e).slice(0, 80); log(name, "failed:", e); }
  }
  const head = execSync("git rev-parse --short HEAD", { cwd: root }).toString().trim();
  const meta = { label, head, cards: NCARDS, live: NLIVE, terms: NTERMS, reps: REPS, idleSecs: IDLE_SECS, memMins: MEM_MINS,
    host: os.hostname(), chrome: (await chromium.launch({ headless: true }).then(async x => { const v = x.version(); await x.close(); return v; })) };
  if (outFile) fs.writeFileSync(outFile, JSON.stringify({ label, meta, metrics: Object.fromEntries(Object.entries(M).filter(([k]) => !k.startsWith("_"))), series: M._memseries }, null, 1));
  console.log(`# ${label} ${JSON.stringify(meta)}\n` + table());
  stop(); await sleep(300); process.exit(0);
})();
