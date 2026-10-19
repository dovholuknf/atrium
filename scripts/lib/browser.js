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
        const why = (/net::ERR_NO_BUFFER_SPACE/.exec(String(e && e.message)) || /interrupted by another navigation to "chrome-error:/.exec(String(e && e.message)) || [])[0];
        if (n >= 3 || !why) throw e;
        console.log("RETRY page.goto " + url + " attempt " + n + ": " + (why.startsWith("net::") ? why : "chrome-error"));
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

// Which call in the suite a page action came from. A Playwright timeout's own stack names the unit function at best,
// so every page and locator action is wrapped: the call site is read as it starts, a rejection carries it as
// `calledAt` for threwAt(), and HEADLESS_TRACE=1 prints each one as it starts, with the unit it belongs to.
const TRACED_PAGE = ["goto", "click", "dblclick", "fill", "type", "press", "hover", "focus", "tap", "check", "uncheck",
  "selectOption", "setInputFiles", "dispatchEvent", "waitForSelector", "waitForFunction", "waitForURL", "waitForEvent",
  "waitForRequest", "waitForResponse", "waitForLoadState"];
const TRACED_LOCATOR = ["click", "dragTo", "dblclick", "fill", "type", "press", "hover", "focus", "tap", "check", "uncheck",
  "selectOption", "dispatchEvent", "waitFor"];
const traced = new WeakSet();
function callSite() {
  const l = String(new Error().stack).split("\n").slice(1)
    .find(s => /scripts[\\/]/.test(s) && !/tracedCall|lib[\\/]browser\.js|node_modules|node:internal/.test(s));
  return l ? l.trim().replace(/^at /, "") : "";
}
function argText(a) {
  if (a === undefined) return "";
  if (typeof a === "function") return "function";
  try { return JSON.stringify(a).slice(0, 120); } catch (e) { return String(a).slice(0, 120); }
}
const inflight = new WeakMap();
const sleep = ms => new Promise(r => setTimeout(r, ms));
const within = (p, ms) => Promise.race([p, sleep(ms).then(() => { throw new Error("diag step timed out"); })]);
// One TIMEOUT-DIAG line per Playwright TimeoutError: what was waited for and what the page looked like. Never throws.
async function timeoutDiag(e, page, sel, what, who, method, args, waitedMs) {
  if (!e || e.name !== "TimeoutError" || e.diagged) return;
  e.diagged = true;
  const d = { unit: who() || null, wait: what, selector: sel || null, error: String(e.message).split("\n")[0].slice(0, 200) };
  if (waitedMs !== undefined) d.waitedMs = waitedMs;
  if (method === "goto") d.url = String(args[0]).slice(0, 200);
  try {
    d.inflight = inflight.has(page) ? inflight.get(page).size : null;
    if (sel) {
      const loc = page.locator(sel);
      d.matches = await within(loc.count(), 1500);
      if (d.matches) {
        const first = loc.first();
        d.visible = await within(first.isVisible(), 1500);
        d.box = await within(first.boundingBox({ timeout: 1000 }), 1500);
        if (d.box) {
          d.atCentre = await within(page.evaluate(([x, y]) => {
            const n = document.elementFromPoint(x, y);
            return n ? { tag: n.tagName.toLowerCase(), id: n.id || "", classes: typeof n.className === "string" ? n.className : "" } : null;
          }, [d.box.x + d.box.width / 2, d.box.y + d.box.height / 2]), 1500);
        }
      }
    }
  } catch (err) { d.diagError = String(err && err.message).slice(0, 120); }
  try { console.log("TIMEOUT-DIAG " + JSON.stringify(d)); } catch (err) {}
}
const WAIT_KINDS = new Set(["goto", "dragTo", "click", "waitForSelector", "waitForFunction"]);
const waits = [];
const SLOW_WAIT_MS = 1000;
function recordWait(kind, who, what, ms) {
  try {
    const unit = who() || null;
    waits.push({ kind, unit, what, ms });
    if (ms > SLOW_WAIT_MS) console.log("SLOW-WAIT " + JSON.stringify({ kind, unit, ms, what }));
  } catch (err) {}
}
const pct = (sorted, q) => sorted[Math.min(sorted.length - 1, Math.ceil(q * sorted.length) - 1)];
function waitSummary() {
  const out = {};
  for (const kind of WAIT_KINDS) {
    const w = waits.filter(x => x.kind === kind).sort((a, b) => a.ms - b.ms);
    if (!w.length) continue;
    const ms = w.map(x => x.ms);
    out[kind] = { count: w.length, p50: pct(ms, 0.5), p95: pct(ms, 0.95), max: ms[ms.length - 1],
      slowest: w.slice(-5).reverse().map(x => ({ unit: x.unit, what: x.what, ms: x.ms })) };
  }
  return out;
}
process.on("exit", () => {
  try { if (waits.length) console.log("WAIT-TIMES " + JSON.stringify(waitSummary())); } catch (err) {}
});
function traceMethods(obj, names, prefix, who, page, locSel) {
  for (const m of names) {
    if (typeof obj[m] !== "function") continue;
    const orig = obj[m].bind(obj);
    obj[m] = function tracedCall(...args) {
      const what = prefix + m + "(" + argText(args[0]) + ")", at = callSite();
      const where = what + (at ? " at " + at : "");
      if (process.env.HEADLESS_TRACE) console.log("TRACE " + (who() || "-") + " " + where);
      const sel = m === "goto" ? null : locSel !== undefined ? locSel : (typeof args[0] === "string" ? args[0] : null);
      const t0 = performance.now();
      const waited = () => Math.round(performance.now() - t0);
      return orig(...args).then(v => {
        if (WAIT_KINDS.has(m)) recordWait(m, who, what, waited());
        return v;
      }, async e => {
        if (WAIT_KINDS.has(m)) recordWait(m, who, what, waited());
        if (e && typeof e === "object" && !e.calledAt) e.calledAt = where;
        await timeoutDiag(e, page, sel, where, who, m, args, waited());
        throw e;
      });
    };
  }
  return obj;
}
function withTrace(browser, who) {
  const wrapPage = page => {
    if (traced.has(page)) return page;
    traced.add(page);
    const open = new Set();
    inflight.set(page, open);
    page.on("request", r => open.add(r));
    page.on("requestfinished", r => open.delete(r));
    page.on("requestfailed", r => open.delete(r));
    traceMethods(page, TRACED_PAGE, "", who, page);
    const locator = page.locator.bind(page);
    page.locator = (...a) => traceMethods(locator(...a), TRACED_LOCATOR, "locator(" + argText(a[0]) + ").", who, page, typeof a[0] === "string" ? a[0] : null);
    return page;
  };
  const newContext = browser.newContext.bind(browser);
  browser.newContext = async (...a) => {
    const ctx = await newContext(...a);
    ctx.setDefaultTimeout(DEFAULT_WAIT());
    ctx.setDefaultNavigationTimeout(NAV_WAIT());
    ctx.on("page", wrapPage);
    const newPage = ctx.newPage.bind(ctx);
    ctx.newPage = async (...b) => wrapPage(await newPage(...b));
    return ctx;
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
// A normal UI step takes well under a second, so Playwright's 30 s default only hides a stuck one. A wait that is slow on
// purpose passes its own slow(n).
const DEFAULT_WAIT = () => slow(5000);
// A page load waits for the load event, which on a loaded CI runner with several shards takes well past an action's budget.
const NAV_WAIT = () => slow(30000);

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

// pauseAt(future) would jump there and fire every due timer, the 60s resync included, mid-test. The clock runs live, so
// a target too close is already past by the time it lands: widen the margin until it is not.
const fakePause = async p => {
  for (const ms of [2, 10, 50, 250]) {
    try { return await p.clock.pauseAt(await p.evaluate(m => Date.now() + m, ms)); }
    catch (e) { if (!/past/.test(e.message)) throw e; }
  }
  throw new Error("fakePause: the page clock outran every margin");
};

// `ms` of page time on the fake clock, every timer fired as it comes due, so a poll or a chained fetch still shows in a
// request count. The real pause per step lets a fetch or a stream event land before the clock moves on.
async function fakeTick(p, ms, step = 250) {
  for (let t = 0; t < ms; t += step) { await p.clock.runFor(Math.min(step, ms - t)); await new Promise(r => setTimeout(r, 10)); }
}

// `n` requestAnimationFrame turns of the page, so a style recalc or a layout has run.
const frames = (p, n = 2) => p.evaluate(k => new Promise(r => { const go = i => (i <= 0 ? r() : requestAnimationFrame(() => go(i - 1))); go(k); }), n);

// A condition spun on from Node in short real steps under one deadline, throwing `what` when it never holds. A page on
// the fake clock uses fakeWait instead.
async function until(fn, ms, what, step = 25) {
  const end = Date.now() + slow(ms);
  for (;;) {
    const v = await fn();
    if (v) return v;
    if (Date.now() > end) throw new Error("until timed out after " + ms + "ms: " + what);
    await new Promise(r => setTimeout(r, step));
  }
}

// Waits for the page's styles to stop moving: two frames so the transition the last change started has begun, then no
// running single-iteration animation or transition, twice running so one queued behind another is seen. Throws at the deadline
// naming what still ran.
async function settleStyle(p, ms = 5000) {
  const end = Date.now() + slow(ms);
  let last = [];
  for (let clean = 0; clean < 2;) {
    await frames(p, 2);
    // A repeating animation (a spinner, a three-pulse dot of 5.7s) says nothing about whether a style change has landed.
    last = await p.evaluate(() => document.getAnimations().filter(a => { const it = a.effect.getComputedTiming().iterations; return !(it > 1); }).map(a => {
      const t = a.effect.target, c = a.effect.getComputedTiming();
      return (a.animationName || a.transitionProperty || "?") + " on " + (t ? t.tagName.toLowerCase() + (t.id ? "#" + t.id : "") + (typeof t.className === "string" && t.className ? "." + t.className.trim().split(/\s+/).join(".") : "") : "none") +
        " " + a.playState + " iterations=" + c.iterations + " duration=" + c.duration + " currentTime=" + a.currentTime;
    }));
    clean = last.length ? 0 : clean + 1;
    if (clean < 2 && Date.now() > end) throw new Error("settleStyle: animations still running after " + ms + "ms: " + last.join("; "));
  }
}

// Counts the requests a page makes, from now, whose url matches `re`. waitFor(n) is the sentinel: do the thing that must
// follow, wait for its request, then assert the earlier one never came.
function watchRequests(page, re, method) {
  const seen = [];
  const on = r => { if (re.test(r.url()) && (!method || r.method() === method)) seen.push(r.url()); };
  page.on("request", on);
  return {
    seen,
    get count() { return seen.length; },
    waitFor: (n, ms = 5000) => until(() => seen.length >= n, ms, "request " + re + " x" + n + ", saw " + seen.length),
    stop: () => page.off("request", on),
  };
}

// "The page was not reloaded" as a fact: a window flag, and a beforeunload that marks sessionStorage for the document
// that replaces this one. armUnload after the load, reloaded() after a frame or two.
const armUnload = p => p.evaluate(() => {
  window.__armed = true;
  window.addEventListener("beforeunload", () => { try { sessionStorage.setItem("__unloaded", "1"); } catch (e) {} });
});
const reloaded = p => p.evaluate(() => !window.__armed || sessionStorage.getItem("__unloaded") === "1");

// term.write with its callback: resolves once the terminal has parsed the data, so a read after it sees the result.
const termWrite = (p, data, term = "term") => p.evaluate(([d, t]) => new Promise(r => (0, eval)(t).write(d, r)), [data, term]);

// For a page on the fake clock: runs `fn(arg, done)` in the page, where `done(value)` is how it answers, and moves the
// page's clock until it has. The page-side timers of a test are fired by the clock, not waited out.
async function fakeRun(p, fn, arg, ms = 5000) {
  await p.evaluate(([src, a]) => { window.__fr = undefined; (0, eval)("(" + src + ")")(a, v => { window.__fr = { v }; }); }, [fn.toString(), arg]);
  await fakeWait(p, () => window.__fr, null, ms);
  return p.evaluate(() => window.__fr.v);
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

module.exports = { DEFAULT_WAIT, NAV_WAIT, slow, fakeWait, fakeTick, fakePause, fakeRun, frames, until, settleStyle, watchRequests, armUnload, reloaded, termWrite, RUN_T0, CLOCK_OFFSET, withClock, withTrace, chromium };
