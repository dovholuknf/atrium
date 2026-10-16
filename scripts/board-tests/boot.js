// boot: what every page needs: the board loads with no page error, the service worker, contrast.
//
// Units: bootClean.
// `units` is what the harness runs, in suite order. `sections` is for HEADLESS_ONLY, which names a section and not a unit.
// Alone: `node scripts/board-tests/boot.js` runs just these units, the way the full run would.
const fs = require("fs");
const path = require("path");
const { WEB_ROOT } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");

async function bootCleanSection(browser, base) {
  const raw = fs.readFileSync(path.join(WEB_ROOT, "index.html"));
  const views = [{ w: 1400, h: 900 }, { w: 390, h: 844, phone: true }, { w: 412, h: 915, phone: true }];
  for (const v of views) {
    for (const at of ["/raw", "/raw#term=s1", "/alias/rnd"]) {
      const ctx = await browser.newContext(v.phone
        ? { viewport: { width: v.w, height: v.h }, hasTouch: true, isMobile: true }
        : { viewport: { width: v.w, height: v.h } });
      const p = await ctx.newPage();
      await p.route(/\/alias\/rnd$/, r => r.fulfill({ status: 200, contentType: "text/html", body: raw }));
      const errors = [];
      p.on("pageerror", e => errors.push(String(e && e.message || e) + (process.env.BOOT_STACK ? " " + e.stack : "")));
      const tag = "bootClean " + v.w + " " + at + ": ";
      try {
        await p.goto(base + at, { waitUntil: "load" });
        await p.waitForTimeout(1500);
        const missing = await p.evaluate(() => ["alerting", "swReg", "cardUrlIsCard", "growlDraw", "termOnly", "peekChatter", "loadChatter"]
          .filter(n => { try { return typeof eval(n) === "undefined"; } catch (e) { return true; } }));
        if (missing.length) fail(tag + "booted without " + missing.join(", "));
      } finally {
        await ctx.close();
      }
      if (errors.length) fail(tag + "threw while booting: " + errors.join(" | "));
    }
  }
  // The read page too: its width and skin code runs at load, and a throw there leaves a header with dead buttons.
  for (const v of views) {
    const ctx = await browser.newContext(v.phone
      ? { viewport: { width: v.w, height: v.h }, hasTouch: true, isMobile: true }
      : { viewport: { width: v.w, height: v.h } });
    const p = await ctx.newPage();
    const errors = [];
    p.on("pageerror", e => errors.push(String(e && e.message || e)));
    const tag = "bootClean read.html " + v.w + ": ";
    try {
      await p.goto(base + "/read.html#land-live/docs%2Fplan.md", { waitUntil: "load" });
      await p.waitForTimeout(600);
      const have = await p.evaluate(() => ["width", "skin", "zoom-up"].filter(id => !document.getElementById(id)));
      if (have.length) fail(tag + "has no " + have.join(", "));
    } finally {
      await ctx.close();
    }
    if (errors.length) fail(tag + "threw while booting: " + errors.join(" | "));
  }
  if (!bad) console.log("bootClean ok");
}

const units = [
  { name: "bootClean", run: ({ browser, base }) => bootCleanSection(browser, base) },
];
const sections = { bootClean: bootCleanSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
