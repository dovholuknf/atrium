// The Queue tab's scroll on a LIVE board, with its terminals running behind it. Reads only: it opens the board and scrolls.
//
//   NODE_PATH=<dir with playwright> BOARD=http://127.0.0.1:7781/ node scripts/measure-queue-live.js
//   CHANNEL=msedge|chrome   GPU=0 for --disable-gpu   SECS=8 seconds of wheel
//
// Prints frame times, long tasks, and the main thread's busiest trace events during the scroll.
let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) { ({ chromium } = require("playwright")); }

(async () => {
  const board = process.env.BOARD || "http://127.0.0.1:7781/";
  const browser = await chromium.launch({ channel: process.env.CHANNEL || "msedge", headless: false, args: process.env.GPU === "0" ? ["--disable-gpu"] : [] });
  const ctx = await browser.newContext({ viewport: { width: 1500, height: 900 } });
  const page = await ctx.newPage();
  await page.goto(board, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => typeof switchView === "function" && typeof yqRender === "function", null, { timeout: 30000 });
  await page.waitForTimeout(4000);
  await page.evaluate(() => switchView("queue"));
  await page.waitForTimeout(2000);
  const info = await page.evaluate(() => {
    const l = document.getElementById("yq-list");
    return { rows: l.querySelectorAll(".yq-row").length, sh: l.scrollHeight, ch: l.clientHeight, terms: document.querySelectorAll(".xterm").length,
      cards: (window.lastTasks || []).length, x: l.getBoundingClientRect().x + 100, y: l.getBoundingClientRect().y + 200 };
  });
  console.log(JSON.stringify(info));
  await page.mouse.move(info.x, info.y);
  await page.evaluate(() => {
    window.__ft = []; window.__lt = []; let last = performance.now();
    const tick = t => { window.__ft.push(t - last); last = t; requestAnimationFrame(tick); };
    requestAnimationFrame(tick);
    new PerformanceObserver(l => l.getEntries().forEach(e => window.__lt.push({ d: Math.round(e.duration), at: Math.round(e.startTime) }))).observe({ entryTypes: ["longtask"] });
  });
  const cdp = await ctx.newCDPSession(page);
  await cdp.send("Tracing.start", { transferMode: "ReturnAsStream", categories: "devtools.timeline,disabled-by-default-devtools.timeline,v8,blink,cc" });
  await page.waitForTimeout(200);
  await page.evaluate(() => { window.__ft = []; window.__lt = []; });
  const steps = +(process.env.SECS || 8) * 50;
  for (let i = 0; i < steps; i++) { await page.mouse.wheel(0, Math.floor(i / 25) % 2 ? -120 : 120); await page.waitForTimeout(18); }
  await page.waitForTimeout(200);
  const r = await page.evaluate(() => ({ ft: window.__ft.slice(), lt: window.__lt.slice(), top: document.getElementById("yq-list").scrollTop }));
  const done = new Promise(res => cdp.once("Tracing.tracingComplete", res));
  await cdp.send("Tracing.end");
  const { stream } = await done;
  let text = "";
  for (;;) { const c = await cdp.send("IO.read", { handle: stream }); text += c.data; if (c.eof) break; }
  const evs = JSON.parse(text).traceEvents || JSON.parse(text);
  const sum = {};
  for (const e of evs) if (e.ph === "X" && e.dur > 1000 && !/^(RunTask|ThreadControllerImpl|RunMicrotasks|ProxyMain|ThreadPool|BeginFrame)/.test(e.name)) {
    const k = e.name + (e.args && e.args.data && e.args.data.url ? " " + e.args.data.url.split("?")[0].slice(-40) : "");
    const s = sum[k] || (sum[k] = { n: 0, ms: 0, max: 0 });
    s.n++; s.ms += e.dur / 1000; s.max = Math.max(s.max, e.dur / 1000);
  }
  const ft = r.ft.sort((a, b) => a - b);
  const q = f => ft[Math.min(ft.length - 1, Math.floor(ft.length * f))];
  console.log(JSON.stringify({ frames: ft.length, median: +q(0.5).toFixed(1), p95: +q(0.95).toFixed(1), max: +ft[ft.length - 1].toFixed(1), over20: ft.filter(x => x > 20).length,
    over33: ft.filter(x => x > 33).length, longtasks: r.lt.length, longtaskMs: r.lt.map(t => t.d).join(","), scrollTop: r.top }));
  Object.entries(sum).sort((a, b) => b[1].ms - a[1].ms).slice(0, 14).forEach(([k, v]) => console.log(k, v.n, v.ms.toFixed(1) + "ms", "max", v.max.toFixed(1)));
  await browser.close();
})().catch(e => { console.error(e); process.exit(1); });
