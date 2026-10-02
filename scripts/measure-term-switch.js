const { chromium } = require("playwright");
const base = process.env.BASE || "http://127.0.0.1:7781";
(async () => {
  const b = await chromium.launch();
  const p = await (await b.newContext({ viewport: { width: 1500, height: 900 } })).newPage();
  await p.addInitScript(() => {
    window.__ws = [];
    const W = window.WebSocket;
    window.WebSocket = new Proxy(W, { construct(T, a) {
      const s = new T(...a);
      const r = { url: a[0], t0: performance.now(), open: 0, first: 0, last: 0, bytes: 0, frames: 0 };
      window.__ws.push(r);
      s.addEventListener("open", () => r.open = performance.now());
      s.addEventListener("message", e => { const n = performance.now(); if (!r.first) r.first = n; r.last = n; r.frames++; r.bytes += e.data.byteLength != null ? e.data.byteLength : e.data.length; });
      return s;
    }});
  });
  await p.goto(base + "/", { waitUntil: "load" });
  await p.waitForTimeout(3000);
  const tasks = await p.evaluate(() => fetch("/v1/tasks").then(r => r.json()).then(j => j.tasks.filter(t => t.status !== "done" && t.status !== "backlog").map(t => ({ id: t.id, title: t.title, host: t.hostname }))));
  const ids = process.env.IDS ? process.env.IDS.split(",") : tasks.map(t => t.id);
  const out = [];
  for (const id of ids) {
    const r = await p.evaluate(async id => {
      const t = await new Promise(res => { const raf = () => res(performance.now()); raf(); });
      const t0 = performance.now(); performance.mark("sw-click");
      const before = window.__ws.length;
      attachTask(id);
      // paint: first rAF after click, then wait for socket quiet
      const firstRaf = await new Promise(res => requestAnimationFrame(() => res(performance.now())));
      let tTerm = 0;
      for (let i = 0; i < 400; i++) { // poll 10ms up to 4s
        await new Promise(r => setTimeout(r, 10));
        const ws = window.__ws[before];
        if (ws && ws.last && performance.now() - ws.last > 150) break;
      }
      const ws = window.__ws[before] || {};
      const drain = await new Promise(res => term ? term.write("", () => res(performance.now())) : res(0));
      const renderer = term && term._core && term._core._renderService && term._core._renderService._renderer && term._core._renderService._renderer.value && term._core._renderService._renderer.value.constructor.name;
      const rows = (term && term.buffer.active.length) || 0;
      return { id, t0, wsNew: before !== window.__ws.length, ctor: ws.t0 - t0, open: ws.open - t0, first: ws.first - t0, last: ws.last - t0, bytes: ws.bytes, frames: ws.frames, firstRaf: firstRaf - t0, bufLines: rows, drain: drain - t0, renderer };
    }, id);
    const title = (tasks.find(t => t.id === id) || {}).title;
    out.push({ title, ...r });
    await p.waitForTimeout(300);
  }
  for (const r of out) console.log(JSON.stringify(Object.fromEntries(Object.entries(r).map(([k, v]) => [k, typeof v === "number" ? Math.round(v) : v]))));
  await b.close();
})();
