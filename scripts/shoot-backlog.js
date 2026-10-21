// The backlog page in a real browser with every endpoint mocked, as PNGs.
//
//   NODE_PATH=<dir with playwright> node scripts/shoot-backlog.js
//   ROOT=<checkout>  the tree whose board is drawn (default: this one). Point it at an older checkout for a "before".
//   OUT=<dir>        where the PNGs go (default docs/changes)
//   NAME=<prefix>    PNG name prefix (default backlog-priority-after)
//   WIDTH, HEIGHT    viewport (default 1200x560). DRAWER=1 opens the view bar drawer first
//   STEPS=1          also shoot the page sorted by a header and filtered by a chip (needs the new page)
//
// It serves the concatenated board from board-source.js, as scripts/check-new-card-row.js does. The hub answers from
// memory: a handful of items with free-text-era data already normalised (high, medium, low, unset), one of them
// interviewed, so the default order (interviewed, then priority, then newest) shows.

const http = require("http");
const path = require("path");
const fs = require("fs");

const ROOT = path.resolve(process.env.ROOT || path.join(__dirname, ".."));
const { wholeBoard } = require(path.join(ROOT, "scripts", "board-source.js"));

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) {
  try { ({ chromium } = require("playwright")); } catch (e2) { console.log("playwright is not installed"); process.exit(0); }
}

const OUT = path.resolve(process.env.OUT || path.join(__dirname, "..", "docs", "changes"));
const NAME = process.env.NAME || "backlog-priority-after";
const HTML = wholeBoard();

const items = [
  { id: "f-001", dept: "fabric", title: "An inventory of resources", status: "open", priority: "low", created_at: "2026-10-01T09:00:00Z", changed_at: "2026-10-02T09:00:00Z" },
  { id: "f-002", dept: "fabric", title: "Tunnel health on the board", status: "in-progress", priority: "high", created_at: "2026-10-03T09:00:00Z", changed_at: "2026-10-08T09:00:00Z" },
  { id: "r-037", dept: "runtime", title: "Resume a card after a hub restart", status: "open", priority: "medium", created_at: "2026-10-05T09:00:00Z", changed_at: "2026-10-05T09:00:00Z" },
  { id: "u-010", dept: "ui", title: "Sort the backlog by a header", status: "open", priority: "", created_at: "2026-10-09T09:00:00Z", changed_at: "2026-10-09T09:00:00Z" },
  { id: "u-011", dept: "ui", title: "Phone layout for the audit pane", status: "held", priority: "high", created_at: "2026-10-07T09:00:00Z", changed_at: "2026-10-07T12:00:00Z" },
  { id: "m-004", dept: "merge-flow", title: "An interviewed item floats to the top", status: "open", priority: "low", created_at: "2026-09-28T09:00:00Z", changed_at: "2026-10-06T09:00:00Z", interviewed: true },
];

const server = http.createServer((req, res) => {
  const u = new URL(req.url, "http://x");
  const p = u.pathname;
  const json = (o, code) => { res.writeHead(code || 200, { "Content-Type": "application/json" }); res.end(JSON.stringify(o)); };
  if (p === "/" || p === "/index.html") { res.writeHead(200, { "Content-Type": "text/html" }); return res.end(HTML); }
  if (p.startsWith("/v1/events")) { res.writeHead(200, { "Content-Type": "text/event-stream" }); return res.write(": open"); }
  if (p === "/_hub/rooms") return json({ rooms: [{ name: "sg4" }] });
  if (p === "/_hub/inventory") return json({ rooms: [] });
  if (p === "/_hub/backlog" && req.method === "GET") {
    let list = items;
    if (u.searchParams.get("open") === "1") list = list.filter(b => b.status !== "done" && b.status !== "dropped");
    return json({ items: list });
  }
  const m = /^\/_hub\/backlog\/([a-z0-9-]+)$/.exec(p);
  if (m && req.method === "POST") {
    let raw = "";
    req.on("data", d => { raw += d; });
    req.on("end", () => {
      const b = items.find(x => x.id === m[1]);
      const want = JSON.parse(raw || "{}").priority;
      if (!b) return json({ error: "no backlog item has that id" }, 404);
      b.priority = want;
      json(b);
    });
    return;
  }
  if (p === "/_hub/reports") return json({ reports: [] });
  if (req.method !== "GET") return json({ ok: true });
  if (p === "/v1/tasks") return json({ tasks: [] });
  if (p === "/v1/health") return json({ build: "shoot", settling: false, halted: false });
  if (p === "/v1/settings") return json({ global_auto: false, board_skin: "daylight", board_skins: ["daylight"], scrollback_lines: 5000 });
  if (p === "/v1/permissions") return json({ permissions: [] });
  if (p === "/v1/harnesses") return json({ harnesses: [] });
  return json({});
});

(async () => {
  fs.mkdirSync(OUT, { recursive: true });
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: +(process.env.WIDTH||1200), height: +(process.env.HEIGHT||560) } });
  const errors = [];
  page.on("pageerror", e => errors.push(String(e)));
  await page.goto("http://127.0.0.1:" + server.address().port + "/");
  await page.waitForFunction(() => typeof loadBacklog === "function" && typeof switchView === "function");
  await page.evaluate(() => { hubIsHub = true; paintBacklogTab(); switchView("backlog"); return loadBacklog(); });
  await page.waitForFunction(() => document.querySelectorAll("#backlog-items .aud-row[data-id]").length > 0);
  const ids = () => page.$$eval("#backlog-items .aud-row[data-id]", rows => rows.map(r => r.dataset.id));
  await page.waitForTimeout(300);
  console.log("order", (await ids()).join(" "));
  if (process.env.DRAWER) { await page.evaluate(() => fdSet("backlog", true)); await page.waitForTimeout(300); }
  if (process.env.STEPS) {
    await page.screenshot({ path: path.join(OUT, NAME + ".png") });
    await page.click('button[data-sort="dept"]');
    console.log("by dept", (await ids()).join(" "));
    await page.click('button[data-sort="dept"]');
    console.log("by dept again", (await ids()).join(" "));
    await page.click('button[data-prio="high"]');
    await page.screenshot({ path: path.join(OUT, NAME.replace(/after$/, "") + "sorted-filtered.png") });
    console.log("high only", (await ids()).join(" "));
  } else {
    await page.screenshot({ path: path.join(OUT, NAME + ".png") });
  }
  if (errors.length) { console.log("page errors:", errors.join(" | ")); process.exitCode = 1; }
  await browser.close(); server.close();
})();
