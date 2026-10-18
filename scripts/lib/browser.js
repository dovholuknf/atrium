// The browser side of the headless board suite: the shifted clock every context gets, the slow() scale for waits, the
// fake-clock helpers, and the Playwright import. Nothing here knows the board or the mocked daemon (lib/mock.js).
//
// Loading this installs the clock into Node and finds Chromium. If Playwright or its browser is missing it prints why and
// exits 0: the headless check skips rather than fails, the same way check-board.sh treats node itself.

// One clock for the whole suite, so no section depends on the time of day the
// run happens to start. A fixture built from Date.now() and checked as HH:MM
// broke whenever the real time sat near midnight, because the board then
// rightly says "tomorrow 00:04". So the run is SHIFTED, not frozen: Date.now()
// and a no-argument `new Date()` answer real time plus a constant, which keeps
// timers, debounces and performance.now working while every run starts at noon,
// twelve hours from either midnight. Node and every browser context take the
// same offset, so a fixture built in Node agrees with the page reading it.
//   HEADLESS_CLOCK=real    no shift at all, for debugging against the real time
//   HEADLESS_CLOCK=23:58   start the run at that local time instead of noon
// A context that must see the real clock says so: newContext(o, { realClock: true }).
// Workers do not get it, and no section here runs one that reads the time.
function clockOffset() {
  const spec = process.env.HEADLESS_CLOCK || "12:00";
  if (spec === "real") return 0;
  const m = /^(\d{1,2}):(\d\d)$/.exec(spec);
  if (!m || +m[1] > 23 || +m[2] > 59) {
    console.error("HEADLESS_CLOCK is 'real' or HH:MM, not '" + spec + "'");
    process.exit(2);
  }
  const now = new Date();
  const target = new Date(now.getFullYear(), now.getMonth(), now.getDate(), +m[1], +m[2], 0, 0);
  return target.getTime() - now.getTime();
}
const CLOCK_OFFSET = clockOffset();
// Written as a function so the very same text runs in Node and, stringified, in
// the page. A Proxy rather than a subclass keeps Date.prototype, instanceof and
// Date.name exactly as they were: only construct, apply and now are answered here.
// A section that replaces Date.now (to jump past a cap) replaces the proxy's `now`, never the real one, or the
// shifted clock would call the replacement that calls it back.
function installClock(offset) {
  if (!offset) return;
  const Real = Date;
  const realNow = Real.now.bind(Real);
  const shifted = () => realNow() + offset;
  let now = shifted;
  const proxy = new Proxy(Real, {
    construct(target, args, newTarget) {
      return Reflect.construct(target, args.length ? args : [shifted()], newTarget);
    },
    apply() { return new Real(shifted()).toString(); },
    get(target, key, recv) {
      if (key === "now") return now;
      return Reflect.get(target, key, target === recv ? target : recv);
    },
    set(target, key, value) {
      if (key === "now") { now = value; return true; }
      return Reflect.set(target, key, value);
    },
  });
  globalThis.Date = proxy;
}
installClock(CLOCK_OFFSET);
// When the run started, on the shifted clock. The clock section runs late in a long run, so it checks the start here
// and the page against Node's clock now.
const RUN_T0 = Date.now();
const CLOCK_INIT = "(" + installClock.toString() + ")(" + CLOCK_OFFSET + ");";
// Wrap the browser so no section can forget the clock: it is the first init
// script of every context, ahead of any a section adds. browser.newPage() makes
// its own context, so it goes through the same door.
function retryGoto(page) {
  const goto = page.goto.bind(page);
  page.goto = async (url, o) => {
    for (let n = 1; ; n++) {
      try { return await goto(url, o); } catch (e) {
        if (n >= 3 || !/net::ERR_NO_BUFFER_SPACE/.test(String(e && e.message))) throw e;
        console.log("RETRY page.goto " + url + " attempt " + n + ": ERR_NO_BUFFER_SPACE");
        await new Promise(r => setTimeout(r, 250 * n));
      }
    }
  };
  return page;
}
function withClock(browser) {
  const newContext = browser.newContext.bind(browser);
  browser.newContext = async (opts, more) => {
    const ctx = await newContext(opts || {});
    if (!(more && more.realClock)) await ctx.addInitScript({ content: CLOCK_INIT });
    // A phone-sized test context would be sent to /m by the board root. Every context opts out unless it is the redirect's own.
    if (!(more && more.redirect)) await ctx.addInitScript(() => { try { sessionStorage.setItem("atrium.m.desktop", "1"); } catch (e) {} });
    const newPage = ctx.newPage.bind(ctx);
    ctx.newPage = async () => retryGoto(await newPage());
    return ctx;
  };
  browser.newPage = async opts => {
    const ctx = await browser.newContext(opts);
    const page = await ctx.newPage();
    const close = page.close.bind(page);
    page.close = async o => { try { await close(o); } finally { await ctx.close().catch(() => {}); } };
    return page;
  };
  return browser;
}

// Every Playwright timeout here goes through slow(). A loaded machine, a
// `go test` beside this run say, can take longer than the budgets below, which
// were set on an idle one. HEADLESS_SLOW=3 gives every wait three times as long
// without editing the file. Sleeps are not scaled: a sleep that proves something
// stays put has to keep its length.
const SLOW = Number(process.env.HEADLESS_SLOW) > 0 ? Number(process.env.HEADLESS_SLOW) : 1;
const slow = ms => Math.round(ms * SLOW);

// For a page on Playwright's fake clock (`page.clock.install()` before the goto): waitForFunction, with the page's
// clock moved forward a step at a time instead of waited out. `ms` is page time. A short real pause per step lets a
// fetch or an event land. Rejects on timeout, as waitForFunction does. `step` 0 leaves the page's clock alone and
// waits `ms` of real time: for what the browser does on its own clock, an EventSource reconnecting say. A fake-clock
// page cannot use waitForFunction for that, because its polling runs on the faked requestAnimationFrame.
async function fakeWait(p, fn, arg, ms, step = 100) {
  const end = Date.now() + slow(ms);
  for (let t = 0; ; t += step) {
    if (await p.evaluate(fn, arg)) return true;
    if (step ? t >= ms : Date.now() > end) throw new Error("fakeWait timed out after " + ms + "ms");
    if (step) await p.clock.runFor(step);
    await new Promise(r => setTimeout(r, step ? 5 : 50));
  }
}

// `ms` of page time on the fake clock, every timer fired as it comes due, so a poll or a chained fetch still shows in a
// request count. The real pause per step lets a fetch or a stream event land before the clock moves on.
async function fakeTick(p, ms, step = 250) {
  for (let t = 0; t < ms; t += step) { await p.clock.runFor(Math.min(step, ms - t)); await new Promise(r => setTimeout(r, 10)); }
}

// Playwright is a devDependency (see package.json) and CI may run without it.
let chromium;
try {
  ({ chromium } = require("@playwright/test"));
} catch (e) {
  try { ({ chromium } = require("playwright")); } catch (e2) {
    console.log("playwright is not installed, so the headless board check is " +
      "skipped. run `npm install` and `npx playwright install chromium` to enable it.");
    process.exit(0);
  }
}
let exePath = "";
try { exePath = chromium.executablePath(); } catch (e) {}
if (!exePath || !require("fs").existsSync(exePath)) {
  console.log("the chromium browser is not installed, so the headless board " +
    "check is skipped. run `npx playwright install chromium` to enable it.");
  process.exit(0);
}

module.exports = { slow, fakeWait, fakeTick, RUN_T0, CLOCK_OFFSET, withClock, chromium };
