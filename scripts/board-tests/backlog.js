// backlog: the backlog page, the hub's items and the reports directors left (js/backlog.js).
//
// Units: backlogAbsent, backlogRows, backlogSort, backlogPrios, backlogPriority, backlogFilters.
// Until stage B2 only bootClean covered this page. These read it through the hub routes the page calls, answered here
// from the page's own context so each unit shapes the items it needs, and none reads what another left on the mock.
// `units` is what the harness runs, in suite order. Alone: `node scripts/board-tests/backlog.js`.
const { slow } = require("../lib/browser.js");
const { mock } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");

const ago = d => new Date(Date.now() - d * 86400000).toISOString();
// the items every unit starts from. interviewed first, then priority high to low, then newest. `i-done` is closed.
const ITEMS = () => [
  { id: "bk-low", dept: "fabric", title: "A low one", status: "open", priority: "low", created_at: ago(1), changed_at: ago(1) },
  { id: "bk-high-old", dept: "runtime", title: "High and old", status: "open", priority: "high", created_at: ago(9), changed_at: ago(2) },
  { id: "bk-high-new", dept: "fabric", title: "High and new", status: "built", priority: "high", created_at: ago(2), changed_at: ago(2) },
  { id: "bk-med", dept: "ui", title: "A medium one", status: "open", priority: "medium", created_at: ago(5), changed_at: ago(3) },
  { id: "bk-none", dept: "ui", title: "No priority", status: "open", created_at: ago(3), changed_at: ago(3) },
  { id: "bk-int", dept: "rnd", title: "Interviewed, so first", status: "open", priority: "low", interviewed: true, created_at: ago(20), changed_at: ago(4) },
  { id: "bk-done", dept: "fabric", title: "Finished work", status: "done", priority: "medium", created_at: ago(30), changed_at: ago(6) },
];
const REPORTS = () => [{ id: "rp_1", to_dept: "", from_by: "fabric", from_room: "sg4", subject: "bk-high-new built", body: "done and tested", at: ago(1), read_at: null, read_by: null }];

// A hub board with the backlog routes answered here. `posts` and `gets` are what the page asked, `answer` shapes the
// POST. The caller closes the context and puts hubMode back.
async function backlogBoard(browser, base, o) {
  o = o || {};
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const errors = [], gets = [], posts = [];
  const items = o.items || ITEMS();
  const p = await ctx.newPage();
  p.on("pageerror", e => errors.push(e.message));
  await p.addInitScript(() => { localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true })); });
  const json = (r, status, o2) => r.fulfill({ status, contentType: "application/json", body: JSON.stringify(o2) });
  await p.route(/\/_hub\/(backlog|reports)(\/[^/?]+)?(\?.*)?$/, r => {
    const req = r.request(), u = new URL(req.url());
    if (req.method() === "POST") {
      const id = decodeURIComponent(u.pathname.split("/")[3] || "");
      posts.push(Object.assign({ id }, JSON.parse(req.postData())));
      return o.postStatus && o.postStatus !== 200 ? json(r, o.postStatus, { error: o.postError || "no" }) : json(r, 200, { ok: true });
    }
    if (u.pathname === "/_hub/reports") return json(r, 200, { reports: o.reports || REPORTS() });
    gets.push(u.search);
    let list = items;
    if (u.searchParams.get("open") === "1") list = list.filter(b => b.status !== "done" && b.status !== "dropped");
    if (u.searchParams.get("dept")) list = list.filter(b => b.dept === u.searchParams.get("dept"));
    return json(r, 200, { items: list });
  });
  await p.goto(base + "/", { waitUntil: "domcontentloaded" });
  if (o.hub === false) return { ctx, p, errors, gets, posts };
  await p.waitForFunction(() => { const t = document.querySelector('.tab[data-view="backlog"]'); return t && !t.hidden; }, null, { timeout: slow(15000) });
  await p.evaluate(() => switchView("backlog"));
  await p.waitForFunction(() => document.querySelectorAll("#backlog-items .bl-row[data-id]").length > 0, null, { timeout: slow(10000) });
  return { ctx, p, errors, gets, posts };
}
const ids = p => p.evaluate(() => [...document.querySelectorAll("#backlog-items .bl-row[data-id]")].map(r => r.dataset.id));
const settle = (p, ms) => p.waitForTimeout(ms || 150);

// A plain daemon has no hub, so the tab stays hidden and the page asks the hub for nothing.
async function backlogAbsentSection(browser, base) {
  const was = mock.hubMode;
  mock.hubMode = false;
  let b;
  try {
    b = await backlogBoard(browser, base, { hub: false });
    await b.p.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await settle(b.p, 800);
    const tab = await b.p.evaluate(() => document.querySelector('.tab[data-view="backlog"]').hidden);
    if (!tab) fail("backlogAbsent: the backlog tab shows on a board that is not a hub");
    if (b.gets.length) fail("backlogAbsent: the board asked a plain daemon for the backlog: " + b.gets.join(" "));
    if (b.errors.length) fail("backlogAbsent: threw " + b.errors.join(" | "));
  } finally { if (b) await b.ctx.close(); mock.hubMode = was; }
  if (!bad) console.log("backlogAbsent ok");
}

// The rows, in the default order, with the closed item left out and the report listed.
async function backlogRowsSection(browser, base) {
  const was = mock.hubMode;
  mock.hubMode = true;
  let b;
  try {
    b = await backlogBoard(browser, base);
    const p = b.p;
    // the reports are read after the items, so the rows being there says nothing about them yet
    await p.waitForFunction(() => document.querySelectorAll("#backlog-reports details").length > 0, null, { timeout: slow(10000) });
    const got = await p.evaluate(() => ({
      rows: [...document.querySelectorAll("#backlog-items .bl-row[data-id]")].map(r => r.dataset.id),
      head: [...document.querySelectorAll("#backlog-items .bl-head .bl-sort")].map(x => x.textContent.trim()),
      first: document.querySelector('#backlog-items .bl-row[data-id="bk-high-new"]').textContent.replace(/\s+/g, " "),
      prio: [...document.querySelectorAll("#backlog-items .bl-prio")].map(s => s.dataset.id + ":" + s.value).join(),
      open: document.getElementById("backlog-open").checked,
      chips: [...document.querySelectorAll("#backlog-prios button")].map(x => x.dataset.prio + ":" + x.getAttribute("aria-pressed")).join(),
      reports: [...document.querySelectorAll("#backlog-reports details")].map(d => d.textContent.replace(/\s+/g, " ")),
    }));
    if (got.rows.join() !== "bk-int,bk-high-new,bk-high-old,bk-med,bk-low,bk-none") fail("backlogRows: the default order is " + got.rows);
    if (got.rows.includes("bk-done")) fail("backlogRows: a finished item is listed with open only on");
    if (!got.open) fail("backlogRows: open only is not on by default");
    if (!b.gets.length || !b.gets.every(q => q.includes("open=1"))) fail("backlogRows: the first read did not ask for open items only: " + b.gets.join(" "));
    if (got.head.join() !== "status,dept,id,priority,title,created,changed") fail("backlogRows: the header is " + got.head);
    for (const w of ["built", "fabric", "bk-high-new", "High and new"]) if (!got.first.includes(w)) fail("backlogRows: a row lacks " + w + ": " + got.first);
    if (got.prio !== "bk-int:low,bk-high-new:high,bk-high-old:high,bk-med:medium,bk-low:low,bk-none:unset") fail("backlogRows: the priority selects read " + got.prio);
    if (got.chips !== "high:false,medium:false,low:false,unset:false") fail("backlogRows: the priority chips start as " + got.chips);
    if (got.reports.length !== 1 || !/unread/.test(got.reports[0]) || !got.reports[0].includes("bk-high-new built")) fail("backlogRows: the report row is " + JSON.stringify(got.reports));
  } finally { if (b) await b.ctx.close(); mock.hubMode = was; }
  // an empty hub says so, rather than drawing a header over nothing
  mock.hubMode = true;
  try {
    const empty = await browser.newContext({ viewport: { width: 1280, height: 800 } });
    const p = await empty.newPage();
    const errors = [];
    p.on("pageerror", e => errors.push(e.message));
    await p.route(/\/_hub\/(backlog|reports)/, r => r.fulfill({ status: 200, contentType: "application/json", body: r.request().url().includes("reports") ? '{"reports":[]}' : '{"items":[]}' }));
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => { const t = document.querySelector('.tab[data-view="backlog"]'); return t && !t.hidden; }, null, { timeout: slow(15000) });
    await p.evaluate(() => switchView("backlog"));
    await p.waitForFunction(() => /No items/.test(document.getElementById("backlog-items").textContent), null, { timeout: slow(10000) });
    await p.waitForFunction(() => /No reports/.test(document.getElementById("backlog-reports").textContent), null, { timeout: slow(10000) }).catch(() => {});
    const none = await p.evaluate(() => ({ head: document.querySelectorAll("#backlog-items .bl-head").length, reports: document.getElementById("backlog-reports").textContent }));
    if (none.head !== 0) fail("backlogRows: an empty backlog still draws a header");
    if (!/No reports/.test(none.reports)) fail("backlogRows: no reports does not say so: " + none.reports);
    if (errors.length) fail("backlogRows: threw " + errors.join(" | "));
    await empty.close();
  } finally { mock.hubMode = was; }
  if (!bad) console.log("backlogRows ok");
}

// A header sorts, a second click reverses it, and a time column leads with the newest.
async function backlogSortSection(browser, base) {
  const was = mock.hubMode;
  mock.hubMode = true;
  let b;
  try {
    b = await backlogBoard(browser, base);
    const p = b.p;
    const click = async key => { await p.click('#backlog-items .bl-sort[data-sort="' + key + '"]'); await settle(p); };
    const head = key => p.evaluate(k => { const x = document.querySelector('#backlog-items .bl-sort[data-sort="' + k + '"]'); return { sort: x.getAttribute("aria-sort"), on: x.classList.contains("on"), text: x.textContent }; }, key);
    await click("id");
    let rows = await ids(p);
    if (rows.join() !== "bk-high-new,bk-high-old,bk-int,bk-low,bk-med,bk-none") fail("backlogSort: id ascending gave " + rows);
    let h = await head("id");
    if (h.sort !== "ascending" || !h.on || !/▲/.test(h.text)) fail("backlogSort: the id header after one click is " + JSON.stringify(h));
    await click("id");
    rows = await ids(p);
    if (rows.join() !== "bk-none,bk-med,bk-low,bk-int,bk-high-old,bk-high-new") fail("backlogSort: id descending gave " + rows);
    h = await head("id");
    if (h.sort !== "descending" || !/▼/.test(h.text)) fail("backlogSort: the id header after two clicks is " + JSON.stringify(h));
    await click("created");
    rows = await ids(p);
    if (rows.join() !== "bk-low,bk-high-new,bk-none,bk-med,bk-high-old,bk-int") fail("backlogSort: created leads with the newest, gave " + rows);
    if ((await head("id")).sort !== "none") fail("backlogSort: the id header still says it sorts after another header was clicked");
    await click("priority");
    rows = await ids(p);
    if (rows.slice(0, 2).sort().join() !== "bk-high-new,bk-high-old" || rows[rows.length - 1] !== "bk-none") fail("backlogSort: priority did not lead with high and end with unset: " + rows);
    await click("title");
    rows = await ids(p);
    if (rows.join() !== "bk-med,bk-high-new,bk-high-old,bk-int,bk-low,bk-none".split(",").sort((x, y) => title(x).localeCompare(title(y))).join()) fail("backlogSort: title A to Z gave " + rows);
    if (b.errors.length) fail("backlogSort: threw " + b.errors.join(" | "));
  } finally { if (b) await b.ctx.close(); mock.hubMode = was; }
  if (!bad) console.log("backlogSort ok");
}
const title = id => ITEMS().find(x => x.id === id).title.toLowerCase();

// The priority chips filter, as a set: none on is everything, and a second click takes one off.
async function backlogPriosSection(browser, base) {
  const was = mock.hubMode;
  mock.hubMode = true;
  let b;
  try {
    b = await backlogBoard(browser, base);
    const p = b.p;
    const chip = async prio => { await p.click('#backlog-prios button[data-prio="' + prio + '"]'); await settle(p); };
    const pressed = () => p.evaluate(() => [...document.querySelectorAll("#backlog-prios button")].filter(x => x.getAttribute("aria-pressed") === "true").map(x => x.dataset.prio).join());
    // the board's own event stream can ask for one more read just after it boots. let it, then count
    await settle(p, 1500);
    const before = b.gets.length;
    await chip("high");
    let rows = await ids(p);
    if (rows.join() !== "bk-high-new,bk-high-old") fail("backlogPrios: high alone gave " + rows);
    if ((await pressed()) !== "high") fail("backlogPrios: the high chip is not pressed");
    await chip("unset");
    rows = await ids(p);
    if (rows.join() !== "bk-high-new,bk-high-old,bk-none") fail("backlogPrios: high and unset gave " + rows);
    await chip("high");
    rows = await ids(p);
    if (rows.join() !== "bk-none") fail("backlogPrios: unset alone gave " + rows);
    await chip("unset");
    rows = await ids(p);
    if (rows.length !== 6) fail("backlogPrios: no chip on is not every open item: " + rows);
    if (b.gets.length !== before) fail("backlogPrios: a chip read the hub again, it filters what is already here (" + (b.gets.length - before) + " reads)");
    await chip("medium"); await chip("low");
    rows = await ids(p);
    if (rows.join() !== "bk-int,bk-med,bk-low") fail("backlogPrios: medium and low gave " + rows);
    // a filter that leaves nothing says so
    await chip("medium"); await chip("low"); await chip("high");
    await p.evaluate(() => { backlogList = backlogList.filter(x => x.priority !== "high"); paintBacklog(); });
    rows = await ids(p);
    if (rows.length !== 0 || !/No items/.test(await p.evaluate(() => document.getElementById("backlog-items").textContent))) fail("backlogPrios: a filter with no match does not say No items: " + rows);
    if (b.errors.length) fail("backlogPrios: threw " + b.errors.join(" | "));
  } finally { if (b) await b.ctx.close(); mock.hubMode = was; }
  if (!bad) console.log("backlogPrios ok");
}

// The select on a row is the one thing the board changes: it posts the priority, reads the list again, and says why on a
// refusal.
async function backlogPrioritySection(browser, base) {
  const was = mock.hubMode;
  mock.hubMode = true;
  let b;
  try {
    b = await backlogBoard(browser, base);
    const p = b.p;
    const before = b.gets.length;
    await p.selectOption('#backlog-items select.bl-prio[data-id="bk-low"]', "medium");
    await settle(p, 400);
    if (b.posts.length !== 1 || JSON.stringify(b.posts[0]) !== JSON.stringify({ id: "bk-low", priority: "medium", by: "board" })) fail("backlogPriority: the post was " + JSON.stringify(b.posts));
    if (b.gets.length <= before) fail("backlogPriority: the list was not read again after the change");
    await p.selectOption('#backlog-items select.bl-prio[data-id="bk-med"]', "unset");
    await settle(p, 400);
    if (b.posts.length !== 2 || b.posts[1].id !== "bk-med" || b.posts[1].priority !== "") fail("backlogPriority: unset must post an empty priority, got " + JSON.stringify(b.posts[1]));
    if (await p.evaluate(() => document.getElementById("backlog-note").textContent)) fail("backlogPriority: a change that worked left a note");
    if (b.errors.length) fail("backlogPriority: threw " + b.errors.join(" | "));
    await b.ctx.close(); b = null;
    // the hub refuses: its sentence is shown, and the list is read again so the select shows what the hub holds
    b = await backlogBoard(browser, base, { postStatus: 409, postError: "item is closed" });
    await b.p.selectOption('#backlog-items select.bl-prio[data-id="bk-low"]', "high");
    await b.p.waitForFunction(() => /Could not change/.test(document.getElementById("backlog-note").textContent), null, { timeout: slow(8000) });
    const note = await b.p.evaluate(() => document.getElementById("backlog-note").textContent);
    if (note !== "Could not change bk-low: item is closed") fail("backlogPriority: the refusal note is " + JSON.stringify(note));
    await b.p.waitForFunction(() => document.querySelector('#backlog-items select.bl-prio[data-id="bk-low"]').value === "low", null, { timeout: slow(8000) }).catch(() => {});
    const sel = await b.p.evaluate(() => document.querySelector('#backlog-items select.bl-prio[data-id="bk-low"]').value);
    if (sel !== "low") fail("backlogPriority: after a refusal the select reads " + sel + ", not what the hub holds");
  } finally { if (b) await b.ctx.close(); mock.hubMode = was; }
  if (!bad) console.log("backlogPriority ok");
}

// Open only and the department go to the hub, so what comes back is what the hub filtered.
async function backlogFiltersSection(browser, base) {
  const was = mock.hubMode;
  mock.hubMode = true;
  let b;
  try {
    b = await backlogBoard(browser, base);
    const p = b.p;
    await p.click("#backlog-open");
    await p.waitForFunction(() => document.querySelector('#backlog-items .bl-row[data-id="bk-done"]'), null, { timeout: slow(8000) });
    if (b.gets[b.gets.length - 1].includes("open=")) fail("backlogFilters: open only off still asked for open items: " + b.gets[b.gets.length - 1]);
    let rows = await ids(p);
    if (rows.length !== 7) fail("backlogFilters: with open only off the list has " + rows.length + " rows, not 7");
    await p.fill("#backlog-dept", "ui");
    await p.dispatchEvent("#backlog-dept", "change");
    await p.waitForFunction(() => document.querySelectorAll("#backlog-items .bl-row[data-id]").length === 2, null, { timeout: slow(8000) });
    if (!b.gets[b.gets.length - 1].includes("dept=ui")) fail("backlogFilters: the department was not sent: " + b.gets[b.gets.length - 1]);
    rows = await ids(p);
    if (rows.slice().sort().join() !== "bk-med,bk-none") fail("backlogFilters: the ui items are " + rows);
    await p.click("#backlog-open");
    await settle(p, 400);
    if (!/open=1/.test(b.gets[b.gets.length - 1]) || !/dept=ui/.test(b.gets[b.gets.length - 1])) fail("backlogFilters: open only back on lost the department: " + b.gets[b.gets.length - 1]);
    if (b.errors.length) fail("backlogFilters: threw " + b.errors.join(" | "));
  } finally { if (b) await b.ctx.close(); mock.hubMode = was; }
  if (!bad) console.log("backlogFilters ok");
}

const units = [
  { name: "backlogAbsent", run: ({ browser, base }) => backlogAbsentSection(browser, base) },
  { name: "backlogRows", run: ({ browser, base }) => backlogRowsSection(browser, base) },
  { name: "backlogSort", run: ({ browser, base }) => backlogSortSection(browser, base) },
  { name: "backlogPrios", run: ({ browser, base }) => backlogPriosSection(browser, base) },
  { name: "backlogPriority", run: ({ browser, base }) => backlogPrioritySection(browser, base) },
  { name: "backlogFilters", run: ({ browser, base }) => backlogFiltersSection(browser, base) },
];
const sections = {
  backlogAbsent: backlogAbsentSection, backlogRows: backlogRowsSection, backlogSort: backlogSortSection,
  backlogPrios: backlogPriosSection, backlogPriority: backlogPrioritySection, backlogFilters: backlogFiltersSection,
};

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
