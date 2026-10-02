// Before/after of the hover prewarm on a LIVE board: click-to-ws-created ("ctor") with and without it, optionally with
// artificial latency on the card fetch to stand in for a hub. The "after" run patches the two changed scripts into the
// served page (so the daemon need not be redeployed); "before" is the page as served.
//   SRC=before|after DELAY=<ms added to GET /v1/tasks/<id>> BASE=http://127.0.0.1:7781 node scripts/measure-term-prewarm.js
const { chromium } = require("playwright");
const base = process.env.BASE || "http://127.0.0.1:7781";
const { execSync } = require("child_process");
const WT = require("path").join(__dirname, "..");
const SRC = process.env.SRC || "after";
(async () => {
  const b = await chromium.launch();
  const p = await (await b.newContext({ viewport: { width: 1500, height: 1000 } })).newPage();
  await p.addInitScript(() => { window.__ws = []; const W = window.WebSocket;
    window.WebSocket = new Proxy(W, { construct(T, a) { const s = new T(...a); const r = { t0: performance.now(), open: 0 }; window.__ws.push(r); s.addEventListener("open", () => r.open = performance.now()); return s; } }); });
  const DELAY = +process.env.DELAY || 0;
  if (DELAY) await p.route(/\/v1\/tasks\/[^/?]+$/, async r => { if (r.request().method() === "GET") await new Promise(x => setTimeout(x, DELAY)); await r.continue(); });
  if (SRC === "after") await p.route(/\/js\/(terminal-list|settings-spine)\.js(\?.*)?$/, async r => {
    const name = new URL(r.request().url()).pathname.replace(/^\/js\//, "");
    const res = await r.fetch(); let text = await res.text();
    const mine = require("fs").readFileSync(`${WT}/internal/api/web/js/terminal-list.js`, "utf8");
    const anchor = "// ── hiding the agent-launched doers";
    if (name === "terminal-list.js") { const blk = mine.slice(mine.indexOf("// ── warming a card on hover"), mine.indexOf(anchor)); text = text.replace(anchor, blk + anchor); }
    else text = text.replace("openTerm(await api(`/v1/tasks/${id}`))", "openTerm(await (takePrewarmed(id) || api(`/v1/tasks/${id}`)))");
    await r.fulfill({ body: text, contentType: "text/javascript" });
  });
  await p.goto(base); await p.waitForTimeout(3000);
  await p.click('.tab[data-view="terms"]'); await p.waitForTimeout(1500);
  const ids = await p.evaluate(() => [...document.querySelectorAll("#term-list .card.tab[data-id]:not(.cold)")].map(e => e.dataset.id).slice(0, 5));
  async function run(hover) {
    const out = [];
    for (const id of ids) {
      if (!(await p.locator(`#term-list .card.tab[data-id="${id}"]`).count())) continue;
      const row = p.locator(`#term-list .card.tab[data-id="${id}"]`);
      if (hover) { await row.hover(); await p.waitForTimeout(350); }
      await p.evaluate(() => { window.__t = performance.now(); window.__n = window.__ws.length; });
      if (!hover) await row.hover({ force: true }).catch(() => {});
      await p.evaluate(() => { window.__t = performance.now(); window.__n = window.__ws.length; });
      await p.evaluate(id => { window.__t = performance.now(); window.__n = window.__ws.length; document.querySelector(`#term-list .card.tab[data-id="${id}"]`).click(); }, id);
      await p.waitForTimeout(700);
      out.push(await p.evaluate(() => { const r = window.__ws[window.__n]; return r ? r.t0 - window.__t : -1; }));
      await p.mouse.move(2, 2); await p.waitForTimeout(3200);
    }
    return out;
  }
  const a = await run(SRC === 'after'); const c = a;
  const med = x => { x = x.filter(v => v >= 0).sort((m, n) => m - n); return x[x.length >> 1]; };
  console.log(SRC + " (hover first: " + (SRC === "after") + ") ctor ms:", a.map(Math.round).join(" "), "median", Math.round(med(a)));
  
  await b.close();
})();
