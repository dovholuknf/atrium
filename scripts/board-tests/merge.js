// merge: the board shows nothing of the hub merge queue. A red or refused request goes to the orchestrator instead.
//
// Units: mergeStripGone.
// `units` is what the harness runs, in suite order. Alone: `node scripts/board-tests/merge.js`.
const { slow } = require("../lib/browser.js");
const { mock } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");

async function mergeStripGoneSection(browser, base) {
  const was = mock.hubMode;
  mock.hubMode = true;
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 800 } });
  const errors = [];
  let gets = 0;
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    await p.route(/\/_hub\/merge(\/[^?]*)?(\?.*)?$/, r => {
      gets++;
      return r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({
        requests: [{ n: 7, repo: "github.com/o/r", branch: "claude/x", state: "red", failing: ["TestA"],
          asked_at: new Date().toISOString(), ended_at: new Date().toISOString() }],
        held: { "github.com/o/other": "no room is attached" }, unlanded: { "github.com/o/r": 2 } }) });
    });
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.evaluate(() => switchView("terms"));
    await p.waitForFunction(() => { const t = document.getElementById("terms"); return t && !t.hidden; }, null, { timeout: slow(15000) });
    const left = await p.evaluate(() => ({
      strip: !!document.getElementById("term-merge"), dialog: !!document.getElementById("mergedlg"),
      buttons: document.querySelectorAll("[data-mq]").length, load: typeof loadMerge,
      land: /Land now|since the last land|queue and history/.test(document.body.textContent),
    }));
    if (left.strip || left.dialog || left.buttons || left.load !== "undefined" || left.land) {
      fail("mergeStripGone: the merge strip is still on the board " + JSON.stringify(left));
    }
    if (gets) fail("mergeStripGone: the board still asks /_hub/merge");
    if (errors.length) fail("mergeStripGone: threw " + errors.join(" | "));
  } finally { await ctx.close(); mock.hubMode = was; }
  if (!bad) console.log("mergeStripGone ok");
}

const units = [
  { name: "mergeStripGone", run: ({ browser, base }) => mergeStripGoneSection(browser, base) },
];
const sections = { mergeStripGone: mergeStripGoneSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
