// boot: what every page needs: the board loads with no page error, the service worker, contrast.
//
// Units: bootClean, pinHold.
// `units` is what the harness runs, in suite order. `sections` is for HEADLESS_ONLY, which names a section and not a unit.
// Alone: `node scripts/board-tests/boot.js` runs just these units, the way the full run would.
const fs = require("fs");
const path = require("path");
const { slow, frames } = require("../lib/browser.js");
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
        // Boot's last step either way: the board reads its cards after the stream, a pop-out opens the stream after.
        const streamed = p.waitForRequest(r => new URL(r.url()).pathname.startsWith("/v1/events"), { timeout: slow(15000) });
        const read = p.waitForResponse(r => new URL(r.url()).pathname.startsWith("/v1/tasks"), { timeout: slow(15000) });
        await p.goto(base + at, { waitUntil: "load" });
        await Promise.all([streamed, read]).catch(() => fail(tag + "never opened its stream and read its cards"));
        await frames(p, 2);
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
      const doc = p.waitForResponse(r => new URL(r.url()).pathname.startsWith("/v1/tasks/land-live/"), { timeout: slow(15000) });
      await p.goto(base + "/read.html#land-live/docs%2Fplan.md", { waitUntil: "load" });
      await doc.catch(() => fail(tag + "never read the document"));
      await frames(p, 2);
      const have = await p.evaluate(() => ["width", "skin", "zoom-up"].filter(id => !document.getElementById(id)));
      if (have.length) fail(tag + "has no " + have.join(", "));
    } finally {
      await ctx.close();
    }
    if (errors.length) fail(tag + "threw while booting: " + errors.join(" | "));
  }
  if (!bad) console.log("bootClean ok");
}

async function pinHoldSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  try {
    await p.goto(base + "/raw", { waitUntil: "load" });
    await p.waitForFunction(() => typeof togglePin === "function" && typeof loadCards === "function");
    const got = await p.evaluate(async () => {
      const realApi = api;
      let land;
      const row = () => ({ id: "c1", name: "c1", pinned: false });
      api = (url, o) => o && o.method === "PATCH" ? new Promise(r => { land = r; }) : Promise.resolve({ tasks: [row()] });
      try {
        cardRows = new Map([["c1", row()]]);
        const done = togglePin("c1", true);
        await loadCards();
        const during = cardRows.get("c1").pinned;
        land({});
        await done;
        await new Promise(r => setTimeout(r, 0));
        await loadCards();
        return { during, after: cardRows.get("c1").pinned, held: pinHeld.size };
      } finally { api = realApi; }
    });
    if (got.during !== true) fail("pinHold: a list read before the PATCH landed turned the star off");
    if (got.after !== false || got.held !== 0) fail("pinHold: the hold outlived the PATCH " + JSON.stringify(got));
  } finally {
    await ctx.close();
  }
  console.log("pinHold ok");
}

const units = [
  { name: "bootClean", run: ({ browser, base }) => bootCleanSection(browser, base) },
  { name: "pinHold", run: ({ browser, base }) => pinHoldSection(browser, base) },
];
const sections = { bootClean: bootCleanSection, pinHold: pinHoldSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
