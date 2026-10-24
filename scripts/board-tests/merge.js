// merge: the merge queue's strip on the Terminals tab and its dialog (js/merge.js).
//
// Units: mergeStrip.
// `units` is what the harness runs, in suite order. Alone: `node scripts/board-tests/merge.js`.
const { slow } = require("../lib/browser.js");
const { mock } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");

const ago = h => new Date(Date.now() - h * 3600000).toISOString();
const REQUESTS = () => [
  { n: 9, repo: "github.com/o/r", branch: "claude/run", state: "testing", requester: "w9", requester_room: "sg4", asked_at: ago(0.1), started_at: ago(0.05) },
  { n: 8, repo: "github.com/o/r", branch: "claude/q", state: "queued", requester: "w8", requester_room: "sg4", asked_at: ago(0.1) },
  { n: 7, repo: "github.com/o/r", branch: "claude/<img src=x onerror=window.__xss=1>", state: "red", failing: ["TestA"],
    requester: "w7", requester_room: "sg4", asked_at: ago(2), ended_at: ago(1) },
  { n: 6, repo: "github.com/o/r", branch: "claude/c", state: "conflict", reason: "conflicts in a.go", merger: "m1@sg4",
    requester: "w6", requester_room: "sg4", asked_at: ago(3) },
  { n: 5, repo: "github.com/o/r", branch: "claude/fixed", state: "landed", landed: "abcdef0123456789", requester: "w5",
    requester_room: "sg4", asked_at: ago(4), ended_at: ago(3) },
  { n: 4, repo: "github.com/o/r", branch: "claude/fixed", state: "red", failing: ["TestB"], requester: "w5",
    requester_room: "sg4", asked_at: ago(6), ended_at: ago(5) },
  { n: 3, repo: "github.com/o/r", branch: "claude/stale", state: "refused", reason: "old", requester: "w3",
    requester_room: "sg4", asked_at: ago(200), ended_at: ago(199) },
];

// MERGE_SHOT=<dir> writes merge-strip.png and merge-dialog.png.
async function mergeStripSection(browser, base) {
  const was = mock.hubMode;
  mock.hubMode = true;
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 800 } });
  const errors = [], posts = [];
  let gets = 0, didLand = false;
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    await p.addInitScript(() => localStorage.removeItem("atrium.merge.dismissed"));
    const json = (r, o) => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(o) });
    await p.route(/\/_hub\/merge(\/[^?]*)?(\?.*)?$/, r => {
      const req = r.request(), u = new URL(req.url());
      if (req.method() === "POST") {
        posts.push(u.pathname + " " + (req.postData() || ""));
        if (u.pathname === "/_hub/merge/land") didLand = true;
        return json(r, { ok: true });
      }
      if (u.pathname === "/_hub/merge/7") return json(r, Object.assign(REQUESTS()[2], { output: "--- FAIL: TestA\nboom" }));
      gets++;
      return json(r, { requests: REQUESTS(), held: { "github.com/o/other": "no room is attached" }, room: "sg4",
        why: "the hub's own room", unlanded: didLand ? {} : { "github.com/o/r": 2 } });
    });
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.evaluate(() => switchView("terms"));
    await p.waitForFunction(() => !document.getElementById("term-merge").hidden, null, { timeout: slow(15000) });
    const rows = () => p.evaluate(() => [...document.querySelectorAll("#term-merge .mq-row .mq-text")].map(r => r.textContent));
    let got = await rows();
    if (got.length !== 5) fail("mergeStrip: want land, held, red, conflict and the summary, got " + JSON.stringify(got));
    if (got[0] !== "2 requests on claude/main of github.com/o/r since the last land") fail("mergeStrip: the land line reads " + got[0]);
    got.shift();
    if (!/held: no room is attached/.test(got[0])) fail("mergeStrip: the held line reads " + got[0]);
    if (!/^#7 claude\/<img src=x.* red .*: failing TestA$/.test(got[1] || "")) fail("mergeStrip: the red line reads " + got[1]);
    if (!/^#6 claude\/c conflict .*conflicts in a.go, merger m1@sg4$/.test(got[2] || "")) fail("mergeStrip: the conflict line reads " + got[2]);
    if (got[3] !== "merge queue: testing claude/run, 1 queued") fail("mergeStrip: the summary reads " + got[3]);
    if (await p.evaluate(() => document.querySelectorAll("#term-merge img").length + (window.__xss || 0))) fail("mergeStrip: a branch was drawn as markup");

    if (process.env.MERGE_SHOT) await p.screenshot({ path: process.env.MERGE_SHOT + "/merge-strip.png" });
    await p.click('#term-merge [data-mq="dismiss"][data-n="7"]');
    got = await rows();
    if (got.some(t => t.startsWith("#7"))) fail("mergeStrip: dismiss left the red line");

    p.once("dialog", d => d.dismiss());
    await p.click('#term-merge [data-mq="land"]');
    await p.waitForTimeout(200); // wait: negative check, a declined confirm must post nothing
    if (posts.length) fail("mergeStrip: Land now posted without a yes: " + JSON.stringify(posts));
    p.once("dialog", d => d.accept());
    await p.click('#term-merge [data-mq="land"]');
    await p.waitForFunction(() => !document.querySelector('#term-merge [data-mq="land"]'), null, { timeout: slow(5000) });
    if (posts[0] !== '/_hub/merge/land {"repo":"github.com/o/r"}') fail("mergeStrip: Land now posted " + JSON.stringify(posts));
    if (await p.evaluate(() => /push|not on main/.test(document.getElementById("term-merge").textContent))) fail("mergeStrip: the strip offers a push");
    posts.length = 0;
    got = await rows();
    if (got.some(t => t.startsWith("#7"))) fail("mergeStrip: dismiss left the red line");

    await p.click('#term-merge [data-mq="cancel"][data-n="6"]');
    await p.waitForTimeout(200); // wait: the click posts with no page signal to wait on
    if (!String(posts[0]).startsWith("/_hub/merge/6/cancel ")) fail("mergeStrip: give up posted " + JSON.stringify(posts));

    await p.click('#term-merge .mq-sum [data-mq="queue"]');
    await p.waitForFunction(() => document.querySelectorAll("#mergedlg-list .mq-item").length === 7, null, { timeout: slow(5000) });
    await p.click('#mergedlg-list .mq-h[data-n="7"]');
    await p.waitForSelector("#mergedlg-list .mq-item.open .mq-out", { timeout: slow(5000) }).catch(() => {});
    const out = await p.evaluate(() => (document.querySelector("#mergedlg-list .mq-item.open .mq-out") || {}).textContent || "");
    if (!out.includes("--- FAIL: TestA")) fail("mergeStrip: the dialog does not show the test output: " + JSON.stringify(out));
    const landed = await p.evaluate(() => document.querySelector('#mergedlg-list .mq-h[data-n="5"] .mq-state').textContent);
    if (process.env.MERGE_SHOT) await p.screenshot({ path: process.env.MERGE_SHOT + "/merge-dialog.png" });
    if (landed !== "landed abcdef0123") fail("mergeStrip: a landed row reads " + landed);
    if (!gets) fail("mergeStrip: the board never asked the queue");
    if (errors.length) fail("mergeStrip: threw " + errors.join(" | "));
  } finally { await ctx.close(); mock.hubMode = was; }
  if (!bad) console.log("mergeStrip ok");
}

const units = [
  { name: "mergeStrip", run: ({ browser, base }) => mergeStripSection(browser, base) },
];
const sections = { mergeStrip: mergeStripSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
