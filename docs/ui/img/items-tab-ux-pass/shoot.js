// node docs/ui/img/items-tab-ux-pass/shoot.js after OUTDIR
// Serves the real board with the headless suite's mock daemon, answers /_hub/items and /_hub/work from fixture.js, opens the Items tab
// and shoots it at 2338 and 1280 wide. Run it on the built tab for the after shots. The before shots were taken the same way on the old tab.
const path = require("path");
const root = path.join(__dirname, "..", "..", "..", "..");
const { chromium, withClock } = require(path.join(root, "scripts/lib/browser.js"));
const { mock, server } = require(path.join(root, "scripts/lib/mock.js"));
const fx = require("./fixture.js");
(async () => {
  const mode = process.argv[2] || "before", out = process.argv[3] || __dirname;
  mock.hubMode = true;
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const base = "http://127.0.0.1:" + server.address().port;
  const browser = withClock(await chromium.launch());
  try {
    for (const [w, h] of [[2338, 1800], [1280, 1800]]) {
      const ctx = await browser.newContext({ viewport: { width: w, height: h } });
      const p = await ctx.newPage();
      p.on("pageerror", e => console.error("pageerror", e.message));
      await p.route(/\/_hub\/work(\/[^/?]+)?(\?.*)?$/, r => r.fulfill({ contentType: "application/json", body: JSON.stringify({ work: fx.work }) }));
      await p.route(/\/_hub\/items(\/[^/?]+)?(\?.*)?$/, r => r.fulfill({ contentType: "application/json", body: JSON.stringify({ items: fx.rows }) }));
      await p.route(/\/v1\/tasks(\?.*)?$/, r => r.fulfill({ contentType: "application/json", body: JSON.stringify(fx.tasks) }));
      await p.goto(base + "/", { waitUntil: "domcontentloaded" });
      await p.waitForFunction(() => { const t = document.querySelector('.tab[data-view="items"]'); return t && !t.hidden; }, null, { timeout: 15000 });
      await p.click('.tab[data-view="items"]');
      await p.waitForSelector(".it-row", { timeout: 10000 });
      await p.evaluate(t => { lastTasks = t; itemsHtml = ""; itemsPaint(); }, fx.tasks);
      await p.waitForTimeout(400);
      const name = mode + "-" + (w === 2338 ? "2338" : "1280") + ".png";
      await p.screenshot({ path: path.join(out, name) });
      console.log("wrote", name);
      await ctx.close();
    }
  } finally { await browser.close(); server.close(); }
  process.exit(0);
})().catch(e => { console.error(e); process.exit(1); });
