// rooms: rooms, the hub gate, the restart menu, transports, the down page.
//
// Units: restartGate, restartStays, atriumDown, roomsDash, transportChips, pathBadges, transportToggle, hubTransports, zitiLogo, zrokLogo, pollsGone, findingsProbe, roomsMachine, loginNeeded, restartMenu, roomsSetup, hubGate, recogniserRepo, roomsMenuScroll.
// `units` is what the harness runs, in suite order. `sections` is for HEADLESS_ONLY, which names a section and not a unit.
// Alone: `node scripts/board-tests/rooms.js` runs just these units, the way the full run would.
const path = require("path");
const fs = require("fs");
const os = require("os");
const { slow, fakeWait, fakeTick, fakePause, fakeRun, until: untilTrue } = require("../lib/browser.js");
const { mock, openStreams, landCard, mockSay, hubStreams } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");
const { attachRetry, countRequests, landContext, topCounts } = require("../lib/helpers.js");

// The hub restart gate, the board's half. The hub's events are written onto the
// open streams the way the hub writes them, and the board's three calls are
// counted by the mock. See docs/fabric/hub-restart-gate.md.
async function restartGateSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const gp = await ctx.newPage();
  const errors = [];
  gp.on("pageerror", e => errors.push(String(e)));
  const wasHub = mock.hubMode;
  mock.hubMode = true;
  mock.gatePaused = false;
  Object.assign(mock.gateCalls, { input: 0, pause: 0, resume: 0 });
  mock.sseRetry = 200;
  const say = state => {
    const line = "event: hub-restart\ndata: " + JSON.stringify(state) + "\n\n";
    openStreams.forEach(r => { try { if (!r.destroyed) r.write(line); } catch (e) {} });
  };
  const countdownText = () => gp.evaluate(() => {
    const el = document.querySelector(".toast.hubgate b");
    return el ? el.textContent : "";
  });
  // The countdowns, the toast lifetimes and the cover clock run on a fake clock: moved through, not waited out. The
  // real pause after each move lets a report the page makes reach the mock before it is counted.
  const run = async ms => { await gp.clock.runFor(ms); await new Promise(r => setTimeout(r, 100)); };
  try {
    await gp.clock.install();
    await gp.goto(base, { waitUntil: "domcontentloaded" });
    await fakeWait(gp, () => typeof hubIsHub !== "undefined" && hubIsHub &&
      document.getElementById("conn").classList.contains("live"), null, 15000);
    await fakePause(gp);

    // Input is reported, and throttled.
    await gp.mouse.click(700, 450);
    await gp.keyboard.press("Shift");
    await run(300);
    if (mock.gateCalls.input !== 1) fail("a click and a key inside the throttle reported input " +
      mock.gateCalls.input + " times, not once.");

    // The countdown shows and counts.
    say({ state: "countdown", seconds: 5 });
    await fakeWait(gp, () => !!document.querySelector(".toast.hubgate"), null, 5000)
      .catch(() => fail("a countdown from the hub drew no toast."));
    const first = await countdownText();
    if (!/atrium restarts in [45]s/.test(first)) {
      fail("the countdown toast said: " + first);
    }
    await run(1300);
    const later = await countdownText();
    if (!/in [34]s/.test(later)) fail("the countdown did not count down: " + first + " then " + later);

    // ONLY THE PAUSE BUTTON STOPS IT. Typing, clicking elsewhere, the banner's own
    // text and scrolling leave it running, and none is reported as input.
    await run(3100);
    const inputBefore = mock.gateCalls.input;
    await gp.mouse.click(700, 450);
    await gp.keyboard.press("a");
    await gp.mouse.wheel(0, 100);
    await gp.click(".toast.hubgate .what");
    await gp.click(".toast.hubgate b");
    await run(300);
    if (mock.gateCalls.pause !== 0) fail("a click on the banner's text paused the countdown.");
    if (mock.gateCalls.input !== inputBefore) fail("input with the countdown up was reported to the hub.");
    if (!(await gp.$(".toast.hubgate"))) fail("typing or clicking elsewhere took the countdown down.");
    await gp.click(".toast.hubgate .hubgate-act");
    await run(300);
    if (mock.gateCalls.pause !== 1) fail("clicking the countdown called pause " + mock.gateCalls.pause + " times.");
    if (await gp.$(".toast.hubgate")) fail("the countdown toast stayed up after it was clicked.");

    // The hub says paused: a sticky toast with a resume button, which survives
    // the stack filling up.
    say({ state: "paused" });
    await fakeWait(gp, () => {
      const el = document.querySelector(".toast.hubgate");
      return el && /restart on hold/.test(el.textContent);
    }, null, 5000).catch(() => fail("a pause from the hub drew no paused toast."));
    await gp.evaluate(() => { for (let i = 0; i < 5; i++) toast("filler " + i, "pushing the stack"); });
    await run(100);
    if (!(await gp.$(".toast.hubgate .hubgate-act"))) fail("the paused toast was pushed off the stack.");
    await run(9500);
    if (!(await gp.$(".toast.hubgate"))) fail("the paused toast timed out like an ordinary one.");
    await gp.click(".toast.hubgate .hubgate-act");
    await run(300);
    if (mock.gateCalls.resume !== 1) fail("the resume button called resume " + mock.gateCalls.resume + " times.");
    if (await gp.$(".toast.hubgate")) fail("the paused toast stayed up after resume.");

    // A window that opens while a pause is held shows it without an event.
    mock.gatePaused = true;
    const late = await ctx.newPage();
    await late.goto(base, { waitUntil: "domcontentloaded" });
    await fakeWait(late, () => {
      const el = document.querySelector(".toast.hubgate");
      return el && /restart on hold/.test(el.textContent);
    }, null, 15000).catch(() => fail("a board opened during a pause did not show it."));
    await late.close();
    mock.gatePaused = false;

    // THE COUNTDOWN STAYS until the hub says what comes next: not the toast cap,
    // not a removal, not its own clock running out, not a stream reopen.
    const hasCountdown = () => gp.evaluate(() =>
      [...document.querySelectorAll(".toast.hubgate")].some(el => /atrium restarts in/.test(el.textContent)));
    say({ state: "countdown", seconds: 3 });
    await fakeWait(gp, () => !!document.querySelector(".toast.hubgate"), null, 5000)
      .catch(() => fail("the second countdown drew no toast."));
    await gp.evaluate(() => { for (let i = 0; i < 5; i++) toast("filler " + i, "pushing the stack"); });
    await run(100);
    if (!(await hasCountdown())) fail("the countdown toast was pushed off the stack.");
    const plainCount = await gp.evaluate(() => document.querySelectorAll("#toasts .toast:not(.sticky)").length);
    if (plainCount !== 3) fail("with a countdown up the stack held " + plainCount + " ordinary toasts, not 3.");
    await gp.evaluate(() => document.querySelector(".toast.hubgate").remove());
    await run(100);
    if (!(await hasCountdown())) fail("the countdown toast stayed gone after something removed it.");
    await run(9500);
    if (!(await hasCountdown())) fail("the countdown toast went away on a timer.");
    // Every stream back, not only the one `#conn` watches. The hub's own stream
    // reopens on its own clock, and an event said before it is back is said to
    // nobody: the "cancelled countdown stayed on screen" flake.
    const streamsWere = openStreams.filter(r => !r.destroyed).length;
    openStreams.forEach(r => { try { r.destroy(); } catch (e) {} });
    await fakeWait(gp, () => document.getElementById("conn").classList.contains("live"), null, 15000).catch(() => fail("the stream did not come back."));
    for (const end = Date.now() + 15000;
      openStreams.filter(r => !r.destroyed).length < streamsWere && Date.now() < end;) {
      await run(50);
    }
    await run(500);
    if (!(await hasCountdown())) fail("the countdown toast went away when the stream reopened.");
    say({ state: "cancelled" });
    await fakeWait(gp, () => !document.querySelector(".toast.hubgate"), null, 5000)
      .catch(() => fail("a cancelled countdown stayed on screen."));

    // On a phone the cap is one, and a paused toast is on top of it, not in it.
    await gp.setViewportSize({ width: 400, height: 800 });
    say({ state: "paused" });
    await fakeWait(gp, () => !!document.querySelector(".toast.hubgate"), null, 5000)
      .catch(() => fail("a pause on a phone drew no toast."));
    await gp.evaluate(() => { for (let i = 0; i < 3; i++) toast("phone filler " + i, "pushing the stack"); });
    await run(100);
    const phone = await gp.evaluate(() => ({
      paused: !!document.querySelector(".toast.hubgate .hubgate-act"),
      plain: document.querySelectorAll("#toasts .toast:not(.sticky)").length
    }));
    if (!phone.paused || phone.plain !== 1) {
      fail("on a phone the stack was " + JSON.stringify(phone) + ", not the paused toast and one other.");
    }
    say({ state: "resumed" });
    await fakeWait(gp, () => !document.querySelector(".toast.hubgate"), null, 5000)
      .catch(() => fail("a resume did not take the paused toast down."));
    await gp.setViewportSize({ width: 1400, height: 900 });

    // A window opened during a countdown shows what is left of it.
    mock.gateCountdownLeft = 30;
    const mid = await ctx.newPage();
    await mid.goto(base, { waitUntil: "domcontentloaded" });
    await fakeWait(mid, () => {
      const el = document.querySelector(".toast.hubgate");
      return el && /atrium restarts in (29|30)s/.test(el.textContent);
    }, null, 15000).catch(() => fail("a board opened during a countdown did not show it."));
    await mid.close();
    mock.gateCountdownLeft = 0;

    // The cover is a wait card, and a wait card sets its own display, which
    // must not draw it while it is shut.
    if (await gp.evaluate(() => getComputedStyle(document.getElementById("hubrestart")).display !== "none")) {
      fail("the restarting cover is drawn while it is closed.");
    }

    // Restarting: a modal that Escape and closing every dialog both leave up.
    say({ state: "restarting" });
    await fakeWait(gp, () => document.getElementById("hubrestart").open, null, 5000)
      .catch(() => fail("the hub restarting drew no modal."));
    // It says how long it has been, and it wears the skin: its card is the
    // palette's own card colour, whatever skin is on.
    await run(1200);
    const cover = await gp.evaluate(() => {
      const probe = document.createElement("div");
      probe.style.background = "var(--card-0)";
      document.body.appendChild(probe);
      const card = getComputedStyle(probe).backgroundColor;
      probe.remove();
      return {
        card,
        clock: document.getElementById("hubrestart-el").textContent,
        say: document.getElementById("hubrestart-t").textContent,
        bg: getComputedStyle(document.getElementById("hubrestart")).backgroundImage
      };
    });
    if (!/^[1-9]\d*s$/.test(cover.clock)) fail("the restarting cover's clock said " + JSON.stringify(cover.clock));
    if (!/few seconds/.test(cover.say)) fail("the restarting cover said: " + cover.say);
    if (!cover.bg.includes(cover.card)) {
      fail("the restarting cover does not wear the skin: " + cover.bg + " has no " + cover.card);
    }
    await gp.keyboard.press("Escape");
    await gp.evaluate(() => closeOpenDialogs());
    await run(200);
    if (!(await gp.evaluate(() => document.getElementById("hubrestart").open))) {
      fail("the restarting modal came down while the hub was still away.");
    }
    // The hub goes and a new one answers: the stream reopens onto a hub with a
    // new name and the modal clears. See restartStaysSection for the old hub.
    mock.gateBoot = "boot-a2";
    openStreams.forEach(r => { try { r.destroy(); } catch (e) {} });
    await fakeWait(gp, () => !document.getElementById("hubrestart").open, null, 15000)
      .catch(() => fail("the restarting modal did not clear when the stream came back."));
    if (errors.length) fail("the restart gate page threw uncaught errors: " + errors.join(" | "));
  } finally {
    mock.hubMode = wasHub;
    mock.gatePaused = false;
    mock.gateCountdownLeft = 0;
    mock.sseRetry = 0;
    mock.gateBoot = "boot-a";
    await ctx.close();
  }
}

// THE RESTART NOTICE STAYS UNTIL THE NEW HUB IS UP AND THE BOARD HAS SETTLED.
// From the countdown to the cover to the new hub answering, something from the
// gate is on screen every frame. The cover came down on the first stream reopen,
// which could be the old hub's stream coming back from a blip, and a new build's
// reload took it down for good: the page came back bare while it settled.
async function restartStaysSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const gp = await ctx.newPage();
  const errors = [];
  gp.on("pageerror", e => errors.push(String(e)));
  if (process.env.DEBUG_HEADLESS) gp.on("console", m => console.error("[stays] " + m.text()));
  const wasHub = mock.hubMode;
  mock.hubMode = true;
  mock.gatePaused = false;
  mock.gateBoot = "boot-a";
  mock.healthBuild = "test";
  mock.sseRetry = 200;
  const say = state => {
    const line = "event: hub-restart\ndata: " + JSON.stringify(state) + "\n\n";
    openStreams.forEach(r => { try { if (!r.destroyed) r.write(line); } catch (e) {} });
  };
  const drop = () => openStreams.splice(0).forEach(r => { try { r.destroy(); } catch (e) {} });
  const coverUp = () => gp.evaluate(() => document.getElementById("hubrestart").open);
  const live = () => fakeWait(gp, () => document.getElementById("conn").classList.contains("live"), null, 15000, 0);
  const wait = (fn, ms) => fakeWait(gp, fn, null, ms);
  // What is on screen each frame, as runs: `t` a gate toast, `c` the cover, `-`
  // neither. A `-` anywhere but at the very end is a gap.
  const watch = () => gp.evaluate(() => {
    window.__seen = "";
    const now = () => {
      const cover = document.getElementById("hubrestart");
      if (cover && cover.open) return "c";
      return [...document.querySelectorAll(".toast.hubgate")].some(el => el.getClientRects().length > 0)
        ? "t" : "-";
    };
    const tick = () => {
      const s = now();
      if (!window.__seen.endsWith(s)) window.__seen += s;
      window.__watch = requestAnimationFrame(tick);
    };
    tick();
  });
  const seen = () => gp.evaluate(() => window.__seen);
  const gaps = async () => ((await seen()).replace(/-$/, "").match(/-/g) || []).length;
  try {
    await gp.addInitScript(() => {
      document.addEventListener("DOMContentLoaded", () => {
        window.__coverAtLoad = document.getElementById("hubrestart").open;
      });
    });
    await gp.clock.install();
    await gp.goto(base, { waitUntil: "domcontentloaded" });
    await wait(() => typeof hubIsHub !== "undefined" && hubIsHub &&
      document.getElementById("conn").classList.contains("live"), 15000);
    await fakePause(gp);
    await fakeTick(gp, 500);

    // Countdown to cover, with no frame between them.
    say({ state: "countdown", seconds: 1 });
    await wait(() => !!document.querySelector(".toast.hubgate"), 5000)
      .catch(() => fail("a countdown drew no toast."));
    await watch();
    await fakeTick(gp, 1500);
    if (!(await gp.$(".toast.hubgate"))) fail("the countdown toast went at 0 before the hub said restarting.");
    say({ state: "restarting" });
    await wait(() => document.getElementById("hubrestart").open, 5000)
      .catch(() => fail("restarting drew no cover."));
    if (await gaps()) fail("between the countdown and the cover a frame showed neither: " + (await seen()));

    // Nothing else on the board takes it down: a view switch, the terminals
    // pane redrawing, a refresh, every dialog being closed.
    await gp.evaluate(async () => {
      switchView("terms");
      if (typeof renderTerms === "function") await loadCards().catch(() => {}).then(renderTerms);
      switchView("stack");
      refreshSoon();
      await closeOpenDialogs();
      document.getElementById("hubrestart").close();
    });
    await fakeTick(gp, 1500);
    if (!(await coverUp())) fail("the cover came down on a view switch, a terminals redraw or a dialog close.");

    // The old hub goes. The cover holds while nothing answers.
    mock.hubAway = true;
    drop();
    await fakeTick(gp, 2500);
    if (!(await coverUp())) fail("the cover came down while the hub was away.");

    // The new hub answers on the same build: the cover clears, and not before.
    mock.gateBoot = "boot-b";
    mock.hubAway = false;
    await live().catch(() => fail("the stream did not come back from the new hub."));
    await wait(() => !document.getElementById("hubrestart").open, 10000)
      .catch(() => fail("the cover did not clear once the new hub answered."));
    await wait(() => window.__seen.endsWith("c-"), 5000)
      .catch(() => {});
    if ((await seen()) !== "tc-") fail("from the countdown to the new hub the screen went " + (await seen()) +
      ", not countdown, cover, clear.");
    await gp.evaluate(() => cancelAnimationFrame(window.__watch));

    // THE RELOAD PATH. The hub restarts onto a new board build, so the board
    // reloads. The cover is up when the reloaded page first paints and comes
    // down once that page has the new hub.
    say({ state: "countdown", seconds: 1 });
    await fakeTick(gp, 1300);
    say({ state: "restarting" });
    await wait(() => document.getElementById("hubrestart").open, 5000)
      .catch(() => fail("the second restarting drew no cover."));
    mock.hubAway = true;
    drop();
    await fakeTick(gp, 1500);
    let reloaded = false;
    gp.once("domcontentloaded", () => { reloaded = true; });
    mock.gateBoot = "boot-c";
    mock.healthBuild = "test2";
    mock.hubAway = false;
    for (const end = Date.now() + slow(20000); !reloaded;) {
      if (Date.now() > end) fail("a new build after the restart did not reload the board.");
      await fakeTick(gp, 250).catch(() => {});
    }
    const sawCoverWithReload = await wait(() => window.__coverAtLoad !== undefined, 5000)
      .then(() => gp.evaluate(() => window.__coverAtLoad));
    if (!sawCoverWithReload) fail("the board reloaded onto the new build with the cover down.");
    await wait(() => !document.getElementById("hubrestart").open, 15000)
      .catch(() => fail("the cover did not clear after the reload settled."));

    // A window that missed `restarting` (its stream was reconnecting when the
    // hub said it) keeps the countdown at 0 while the hub is away, and the
    // cover takes over from it when a different hub answers.
    await live().catch(() => fail("the reloaded board never went live."));
    await fakeTick(gp, 500);
    say({ state: "countdown", seconds: 1 });
    await wait(() => !!document.querySelector(".toast.hubgate"), 5000)
      .catch(() => fail("the reloaded board drew no countdown."));
    await watch();
    await fakeTick(gp, 1300);
    mock.hubAway = true;
    drop();
    await fakeTick(gp, 2500);
    if (!(await gp.$(".toast.hubgate"))) fail("a countdown that ran out went while the hub was away.");
    mock.gateBoot = "boot-d";
    mock.hubAway = false;
    await wait(() => window.__seen.endsWith("c-"), 15000)
      .catch(() => {});
    if ((await seen()) !== "tc-") fail("over a missed restarting the screen went " + (await seen()) +
      ", not countdown, cover, clear.");
    if (errors.length) fail("the restart-stays page threw: " + errors.join(" | "));
  } finally {
    mock.hubMode = wasHub;
    mock.hubAway = false;
    mock.gateBoot = "boot-a";
    mock.healthBuild = "test";
    mock.sseRetry = 0;
    await ctx.close();
  }
}

// ATRIUM IS DOWN, AND NOBODY SAID IT WOULD BE. An open board that loses atrium
// with no restart announced covers itself after five seconds and comes back by
// itself. A planned restart never shows it, and neither does a blip. A reload
// while atrium is down gets the service worker's `down.html` rather than the
// browser's error page, and that page reloads onto the board once atrium is back.
async function atriumDownSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const dp = await ctx.newPage();
  const errors = [];
  dp.on("pageerror", e => errors.push(String(e)));
  if (process.env.DEBUG_HEADLESS) dp.on("console", m => console.error("[down] " + m.text()));
  const wasHub = mock.hubMode;
  mock.hubMode = true;
  mock.gateBoot = "boot-a";
  mock.sseRetry = 200;
  const drop = () => openStreams.splice(0).forEach(r => { try { r.destroy(); } catch (e) {} });
  const downUp = () => dp.evaluate(() => { const d = document.getElementById("atriumdown"); return !!(d && d.open); });
  // The page runs on a fake clock: the five seconds, the backoff and the countdown are moved through, not waited out.
  const run = ms => fakeTick(dp, ms);
  const until = (fn, ms) => fakeWait(dp, fn, null, ms);
  const live = () => fakeWait(dp, () => document.getElementById("conn").classList.contains("live"), null, 15000, 0);
  // Whether the down cover opened at any frame since this was called.
  const watchDown = () => dp.evaluate(() => {
    window.__downSeen = false;
    const tick = () => {
      if (document.getElementById("atriumdown").open) window.__downSeen = true;
      window.__downWatch = requestAnimationFrame(tick);
    };
    tick();
  });
  try {
    await dp.clock.install();
    await dp.goto(base, { waitUntil: "domcontentloaded" });
    await until(() => typeof hubIsHub !== "undefined" && hubIsHub &&
      document.getElementById("conn").classList.contains("live"), 15000);
    await fakePause(dp);
    await run(500);

    // A blip is not atrium being down.
    await watchDown();
    drop();
    await live().catch(() => fail("the stream did not come back after a blip."));
    await run(6000);
    if (await dp.evaluate(() => window.__downSeen)) fail("a stream blip put the down cover up.");

    // Atrium stops with nothing said: nothing for five seconds, then the cover.
    mock.hubAway = true;
    drop();
    await run(3000);
    if (await downUp()) fail("the down cover went up before five seconds.");
    await until(() => document.getElementById("atriumdown").open, 8000)
      .catch(() => fail("atrium stopping with nothing said put no down cover up."));
    await run(1200);
    const card = await dp.evaluate(() => ({
      clock: document.getElementById("atriumdown-el").textContent,
      text: document.getElementById("atriumdown").textContent,
      restart: document.getElementById("hubrestart").open,
      edge: getComputedStyle(document.getElementById("atriumdown"), "::before").backgroundImage
    }));
    if (!/^down for [1-9]\d*s$/.test(card.clock)) fail("the down cover's clock said " + JSON.stringify(card.clock));
    if (!/atrium is not running/.test(card.text) || !/atrium run/.test(card.text)) {
      fail("the down cover said: " + card.text.replace(/\s+/g, " "));
    }
    if (card.restart) fail("an unplanned stop put the restart cover up.");
    if (/0, 227, 176/.test(card.edge)) fail("the down cover wears the teal edge, not the warning one: " + card.edge);
    await dp.keyboard.press("Escape");
    await dp.evaluate(async () => { await closeOpenDialogs(); document.getElementById("atriumdown").close(); });
    await run(300);
    if (!(await downUp())) fail("the down cover came down while atrium was still away.");

    // Atrium answers: the cover comes down on its own.
    mock.hubAway = false;
    await until(() => !document.getElementById("atriumdown").open, 15000)
      .catch(() => fail("the down cover did not come down once atrium answered."));
    await live().catch(() => fail("the stream did not come back after atrium did."));

    // A planned restart is the restart cover's, however long the hub is away.
    const say = state => {
      const line = "event: hub-restart\ndata: " + JSON.stringify(state) + "\n\n";
      openStreams.forEach(r => { try { if (!r.destroyed) r.write(line); } catch (e) {} });
    };
    await run(500);
    await watchDown();
    say({ state: "countdown", seconds: 1 });
    await run(1300);
    say({ state: "restarting" });
    await until(() => document.getElementById("hubrestart").open, 5000)
      .catch(() => fail("restarting drew no restart cover."));
    mock.hubAway = true;
    drop();
    await run(8000);
    if (await dp.evaluate(() => window.__downSeen)) fail("a planned restart put the down cover up.");
    mock.gateBoot = "boot-b";
    mock.hubAway = false;
    await until(() => !document.getElementById("hubrestart").open, 15000)
      .catch(() => fail("the restart cover did not clear on the new hub."));
    if (await dp.evaluate(() => window.__downSeen)) fail("the down cover showed as the planned restart ended.");
    await dp.evaluate(() => cancelAnimationFrame(window.__downWatch));
  } finally {
    await ctx.close();
  }

  // THE RELOAD. A worker has to be installed and holding the offline page first,
  // which only a page with atrium up can do.
  mock.serveSW = true;
  const sctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const sp = await sctx.newPage();
  sp.on("pageerror", e => errors.push(String(e)));
  try {
    await sp.goto(base, { waitUntil: "domcontentloaded" });
    const ready = await sp.waitForFunction(async () => {
      if (!navigator.serviceWorker || !navigator.serviceWorker.controller) return false;
      const reg = await navigator.serviceWorker.getRegistration();
      if (!reg || !reg.active || reg.active.state !== "activated" || reg.installing || reg.waiting) return false;
      return !!(await (await caches.open("atrium-down")).match("/down.html"));
    }, null, { timeout: slow(15000), polling: 250 }).then(() => true, () => false);
    if (!ready) {
      fail("the service worker did not take the page and keep down.html.");
    } else {
      await sp.waitForFunction(() => document.getElementById("conn").classList.contains("live"), null,
        { timeout: slow(15000) }).catch(() => {});
      const card0 = await sp.evaluate(() =>
        getComputedStyle(document.documentElement).getPropertyValue("--card-0").trim());
      mock.hubAway = true;
      drop();
      await sp.reload({ waitUntil: "load", timeout: slow(15000) })
        .catch(e => fail("a reload with atrium down did not load a page: " + e.message));
      const down = await sp.evaluate(() => ({
        title: document.title,
        text: document.body ? document.body.textContent : "",
        card0: getComputedStyle(document.documentElement).getPropertyValue("--card-0").trim(),
        board: !!document.getElementById("hubrestart")
      })).catch(e => ({ title: "", text: String(e) }));
      if (down.title !== "atrium is not running" || down.board) {
        fail("a reload with atrium down showed " + JSON.stringify(down.title) + ", not down.html.");
      }
      if (down.card0 && card0 && down.card0 !== card0) {
        fail("down.html wore " + down.card0 + ", not the board's " + card0 + ".");
      }
      const clockNow = () => sp.evaluate(() => document.getElementById("atriumdown-el").textContent).catch(() => "");
      await untilTrue(async () => /^down for \d+s$/.test(await clockNow()), 5000, "down.html's clock never drew").catch(() => {});
      const clock = await clockNow();
      if (!/^down for \d+s$/.test(clock)) fail("down.html's clock said " + JSON.stringify(clock));
      // Atrium comes back: the page reloads onto the board by itself.
      mock.hubAway = false;
      await sp.waitForFunction(() => !!document.getElementById("hubrestart"), null, { timeout: slow(15000) })
        .catch(() => fail("down.html did not reload onto the board once atrium answered."));
      await sp.waitForFunction(() => !document.getElementById("atriumdown").open, null, { timeout: slow(15000) })
        .catch(() => fail("the board came back from down.html with the down cover stuck up."));

      // A share in front of a stopped atrium answers 502: the same page.
      mock.page502 = true;
      await sp.reload({ waitUntil: "load", timeout: slow(15000) }).catch(() => {});
      const title = await sp.evaluate(() => document.title).catch(() => "");
      if (title !== "atrium is not running") fail("a 502 on reload showed " + JSON.stringify(title) + ", not down.html.");
      mock.page502 = false;
      await sp.waitForFunction(() => !!document.getElementById("hubrestart"), null, { timeout: slow(15000) })
        .catch(() => fail("down.html over a 502 did not reload onto the board once the page answered."));

      // A reload during a PLANNED restart says restarting, not down, and the
      // board it reloads onto puts the restart cover back until the new hub.
      await sp.waitForFunction(() => document.getElementById("conn").classList.contains("live"), null,
        { timeout: slow(15000) }).catch(() => fail("the board after down.html never went live."));
      await sp.waitForFunction(() => typeof hubIsHub !== "undefined" && hubIsHub, null, { timeout: slow(15000) });
      const say = state => {
        const line = "event: hub-restart\ndata: " + JSON.stringify(state) + "\n\n";
        openStreams.forEach(r => { try { if (!r.destroyed) r.write(line); } catch (e) {} });
      };
      say({ state: "countdown", seconds: 1 });
      await sp.waitForFunction(() => /restarts in 0s/.test((document.querySelector(".toast.hubgate b") || {}).textContent || ""),
        null, { timeout: slow(5000) }).catch(() => fail("the countdown never ran out."));
      say({ state: "restarting" });
      await sp.waitForFunction(() => document.getElementById("hubrestart").open, null, { timeout: slow(5000) })
        .catch(() => fail("restarting drew no restart cover before the reload."));
      mock.hubAway = true;
      drop();
      await sp.reload({ waitUntil: "load", timeout: slow(15000) }).catch(() => {});
      const planned = await sp.evaluate(() => ({
        title: document.title, text: document.body.textContent.replace(/\s+/g, " ")
      })).catch(() => ({ title: "", text: "" }));
      if (planned.title !== "atrium is restarting" || /not running/.test(planned.text)) {
        fail("a reload during a planned restart showed " + JSON.stringify(planned.title) + ": " + planned.text);
      }
      await sp.addInitScript(() => {
        new MutationObserver(list => {
          for (const m of list) if (m.target.id === "hubrestart" && m.target.open) window.coverWasUp = true;
        }).observe(document, { attributes: true, attributeFilter: ["open"], subtree: true });
      });
      mock.gateBoot = "boot-z";
      mock.hubAway = false;
      const back = await sp.waitForFunction(() => window.coverWasUp === true, null, { timeout: slow(15000) })
        .then(() => true, () => false);
      if (!back) fail("the board after a restart-time reload did not put the restart cover back.");
      await sp.waitForFunction(() => !document.getElementById("hubrestart").open &&
        !document.getElementById("atriumdown").open, null, { timeout: slow(15000) })
        .catch(() => fail("the restart cover did not clear after the restart-time reload."));
    }
    if (errors.length) fail("the atrium-down pages threw: " + errors.join(" | "));
  } finally {
    mock.serveSW = false;
    mock.page502 = false;
    mock.sseRetry = 0;
    mock.hubAway = false;
    mock.hubMode = wasHub;
    mock.gateBoot = "boot-a";
    await sctx.close();
  }
}

// A ROOM WHOSE CLAUDE LOGIN IS REVOKED SAYS SO. A red `login needed` chip on its row in the rooms list and on its tile
// in the rooms dashboard, none on a room that is fine, and pressing it asks first and then sends the room's own DELETE
// /v1/login-needed naming the room, which is how the refusal is lifted after a /login. Set LOGIN_SHOTS to a directory
// for a picture, named by LOGIN_NAME (default login-needed-after). See js/rooms.js and internal/daemon/loginneeded.go.
async function loginNeededSection(browser, base) {
  const wasHub = mock.hubMode, was = mock.ALPHA.login_needed;
  mock.hubMode = true;
  mock.ALPHA.login_needed = "claude needs a login on this machine. run /login in any session here";
  const shots = process.env.LOGIN_SHOTS || "", name = process.env.LOGIN_NAME || "login-needed-after";
  const errors = [];
  const lifts = [];
  try {
    const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.route("**/v1/login-needed", route => {
      lifts.push({ method: route.request().method(), room: route.request().headers()["x-atrium-room"] || "" });
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ login_needed: null }) });
    });
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof loginBadge === "function" && typeof loadHubRooms === "function",
      null, { timeout: slow(15000) });
    await p.evaluate(() => switchView("runners")); // the tab may sit in the `...` menu at this width
    await p.waitForFunction(() => hubIsHub && !!document.querySelector("#room-list .roomrow .loginlift"),
      null, { timeout: slow(10000) });
    const chips = await p.evaluate(() => Array.from(document.querySelectorAll("#room-list .roomrow")).map(r => ({
      room: r.querySelector(".roomname").textContent.trim(),
      chip: Array.from(r.querySelectorAll(".loginlift")).map(c => c.textContent.trim()).join("|") })));
    const alpha = chips.find(c => c.room === "alpha");
    if (!alpha || alpha.chip !== "login needed") fail("loginNeeded: alpha's row says " + JSON.stringify(alpha));
    if (chips.some(c => c.room !== "alpha" && c.chip)) fail("loginNeeded: a room that is fine wears the chip: " + JSON.stringify(chips));
    if (shots) await p.locator("#room-list").screenshot({ path: path.join(shots, name + ".png") }).catch(() => {});
    const tip = await p.evaluate(() => document.querySelector("#room-list .loginlift").dataset.tip);
    if (!/run \/login/.test(tip)) fail("loginNeeded: the tip does not say what to do: " + tip);
    await p.click("#room-list .loginlift");
    await p.waitForSelector("#ask[open]", { timeout: slow(5000) });
    if (lifts.length) fail("loginNeeded: lifted before it asked.");
    await p.click('#ask-actions button:has-text("lift it")');
    await p.waitForTimeout(300);
    if (lifts.length !== 1 || lifts[0].method !== "DELETE" || lifts[0].room !== "alpha") {
      fail("loginNeeded: the lift sent " + JSON.stringify(lifts));
    }
    await ctx.close();
    if (errors.length) fail("page errors: " + errors.join("; "));
  } finally {
    mock.hubMode = wasHub;
    if (was === undefined) delete mock.ALPHA.login_needed; else mock.ALPHA.login_needed = was;
  }
}

// ── the last polls are gone (u-009b) ─────────────────────────────────────────
//
// The walk drawer's 3s read of the findings folder, the `rooms` event's follow-up fetch, and the settling
// re-check of /v1/health. Each is now told by an event, and this proves none of them asks on a clock.
async function pollsGoneSection(browser, base) {
  const was = mock.tasksMode, wasB = mock.mockBroadcast, wasHub = mock.hubMode, wasSgg = mock.sggAttached;
  const id = "land-pgone";
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "atrium-pgone-"));
  fs.mkdirSync(path.join(dir, "findings"));
  fs.writeFileSync(path.join(dir, "findings", "01-high-a.go-L10.txt"),
    "HIGH a.go line 10: something\n\na comment\n\nEvidence\nx\n");
  mock.walkDirs[id] = dir;
  mock.tasksMode = "land";
  mock.mockBroadcast = false;
  mock.landList = [landCard(id, { supervised: true, worktree: "/walk/" + id })];
  mock.landPerms = [];
  const errors = [];
  // Every page here runs on a fake clock: the quiet spells that prove nothing polls are moved through, not waited out.
  // The real pause after each move lets a request the page made be counted.
  const run = async (p, ms) => { await p.clock.runFor(ms); await new Promise(r => setTimeout(r, 100)); };
  try {
    // (a) The walk drawer open for longer than the old 3s: no repeated request. An event for the card reads it.
    const ctx = await landContext(browser);
    const counts = countRequests(ctx);
    await ctx.addInitScript(() => {
      const Real = window.WebSocket;
      window.WebSocket = function (url, protocols) {
        if (!/\/attach(\?|$)/.test(url)) return new Real(url, protocols);
        const s = { url, readyState: 0, binaryType: "arraybuffer", onopen: null, onclose: null, onmessage: null,
          onerror: null, send() {}, close() { this.readyState = 3; } };
        setTimeout(() => { s.readyState = 1; if (s.onopen) s.onopen({}); }, 0);
        return s;
      };
      Object.assign(window.WebSocket, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });
    });
    try {
      await ctx.clock.install();
      const p = await ctx.newPage();
      p.on("pageerror", e => errors.push(String(e)));
      await p.goto(base, { waitUntil: "domcontentloaded" });
      await p.waitForSelector('#stack-list .stackrow[data-id="' + id + '"]', { state: "attached", timeout: slow(15000) });
      await attachRetry(p, { unit: "pollsGone", id, fake: true, check: id => !!document.querySelector("#t-walk:not([hidden])") && !!dock.tenant && dock.taskId === id });
      await p.click("#t-walk");
      await p.waitForSelector("#walk-drawer .wk-row", { timeout: slow(10000) });
      await run(p, 500);
      const listed = () => (counts.get("/v1/tasks/" + id + "/files/list") || 0);
      const n0 = listed();
      await run(p, 7000);
      if (listed() !== n0) fail("pollsGone: the open walk drawer read the folder " + (listed() - n0) + " times in 7s with no event");
      fs.writeFileSync(path.join(dir, "findings", "02-low-b.go-L2.txt"), "LOW b.go line 2: other\n\nanother\n\nEvidence\ny\n");
      mockSay("task", JSON.stringify({ id }));
      await p.waitForFunction(() => dock.items.length === 2, null, { timeout: slow(5000) })
        .catch(() => fail("pollsGone: an event for the card did not make the drawer read the folder again"));
    } finally {
      await ctx.close();
    }

    // (b) `rooms` with a payload paints with no fetch, and without `attached` still fetches.
    mock.hubMode = true;
    mock.sggAttached = false;
    const ctx2 = await browser.newContext({ viewport: { width: 1400, height: 900 } });
    const c2 = countRequests(ctx2);
    try {
      await ctx2.clock.install();
      const p = await ctx2.newPage();
      p.on("pageerror", e => errors.push(String(e)));
      const streams0 = hubStreams.length;
      await p.goto(base, { waitUntil: "domcontentloaded" });
      await p.waitForFunction(() => typeof hubRooms !== "undefined" && hubRooms.length === 1, null, { timeout: slow(15000) });
      // The event below goes to the streams open now, so the page's hub stream has to be one of them.
      for (const t0 = Date.now(); hubStreams.length <= streams0 && Date.now() - t0 < slow(5000);) await p.waitForTimeout(50);
      if (hubStreams.length <= streams0) fail("pollsGone: the page never opened its hub event stream");
      await run(p, 2500);
      c2.clear();
      const inv = [Object.assign({ transport: "direct", attached: true, first_seen: "2026-09-19T06:00:00Z" }, mock.ALPHA),
        Object.assign({ transport: "direct", attached: true, first_seen: "2026-09-19T06:00:00Z" }, mock.SGG)];
      hubStreams.forEach(r => { try { r.write("event: rooms\ndata: " + JSON.stringify({
        rooms: ["alpha", "sgg"], attached: [mock.ALPHA, mock.SGG], inventory: inv, durable: true }) + "\n\n"); } catch (e) {} });
      await p.waitForFunction(() => hubRooms.length === 2, null, { timeout: slow(5000) })
        .catch(() => fail("pollsGone: a rooms event with a payload did not paint the second room"));
      await run(p, 700);
      if (c2.get("/_hub/rooms") || c2.get("/_hub/inventory")) {
        fail("pollsGone: a rooms event with a payload still fetched: " + topCounts(c2));
      }
      await run(p, 2500);
      c2.clear();
      hubStreams.forEach(r => { try { r.write("event: rooms\ndata: {}\n\n"); } catch (e) {} });
      await run(p, 1200);
      if (!c2.get("/_hub/rooms")) fail("pollsGone: a rooms event without `attached` did not fall back to fetching");
    } finally {
      await ctx2.close();
      mock.hubMode = wasHub;
      mock.sggAttached = wasSgg;
    }

    // (c) `health`: halted shows the halt, settling then clear ends it, and nothing re-reads /v1/health.
    mock.mockBroadcast = false;
    const ctx3 = await browser.newContext({ viewport: { width: 1400, height: 900 } });
    const c3 = countRequests(ctx3);
    try {
      await ctx3.clock.install();
      const p = await ctx3.newPage();
      p.on("pageerror", e => errors.push(String(e)));
      await p.goto(base, { waitUntil: "domcontentloaded" });
      await p.waitForSelector("#stack-list .stackrow", { state: "attached", timeout: slow(15000) });
      await p.waitForSelector("#conn.live", { timeout: slow(10000) });
      await run(p, 2500);
      await p.waitForFunction(() => !refreshTimer && !refreshInFlight && !refreshDirty, null, { timeout: slow(10000) });
      c3.clear();
      const say = h => mockSay("health", JSON.stringify(h));
      const halt = () => p.evaluate(() => { const e = document.getElementById("halted");
        return e.style.display === "flex" ? e.textContent : ""; });
      say({ halted: true, settling: false, cause: "disk full" });
      await p.waitForFunction(() => document.getElementById("halted").style.display === "flex", null, { timeout: slow(5000) })
        .catch(() => fail("pollsGone: a health event with halted showed no halt"));
      if (!/disk full/.test(await halt())) fail("pollsGone: the halt did not say why");
      say({ halted: false, settling: true });
      await p.waitForFunction(() => document.getElementById("halted").style.display === "none", null, { timeout: slow(5000) })
        .catch(() => fail("pollsGone: a health event without halted left the halt up"));
      await run(p, 6500);
      say({ halted: false, settling: false });
      await run(p, 500);
      if (c3.get("/v1/health")) fail("pollsGone: /v1/health was read " + c3.get("/v1/health") + " times while events said it all");
    } finally {
      await ctx3.close();
    }
  } finally {
    mock.tasksMode = was;
    mock.mockBroadcast = wasB;
    mock.hubMode = wasHub;
    mock.landList = [];
    delete mock.walkDirs[id];
    try { fs.rmSync(dir, { recursive: true, force: true }); } catch (e) {}
  }
  if (errors.length) fail("pollsGone: the page threw: " + errors.join(" | "));
  console.log("pollsGone: ok");
}

// findingsProbe: a card holding no findings folder is asked once per session, not on every attach. A card with one is
// still probed and still gets its walk button. The card record carries no marker for a bare review folder, so the
// miss is cached by card id (u-findings-probe-404).
async function findingsProbeSection(browser, base) {
  const was = mock.tasksMode, wasB = mock.mockBroadcast;
  const plain = "land-fp-plain", review = "land-fp-review";
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "atrium-fp-"));
  fs.mkdirSync(path.join(dir, "findings"));
  fs.writeFileSync(path.join(dir, "findings", "01-high-a.go-L10.txt"),
    "HIGH a.go line 10: something\n\na comment\n\nEvidence\nx\n");
  mock.walkDirs[review] = dir;
  mock.tasksMode = "land";
  mock.mockBroadcast = false;
  mock.landList = [landCard(plain, { supervised: true, worktree: "/walk/" + plain }),
    landCard(review, { supervised: true, worktree: "/walk/" + review })];
  mock.landPerms = [];
  const errors = [];
  const ctx = await landContext(browser);
  const counts = countRequests(ctx);
  try {
    // The daemon's answer for a missing folder inside the card.
    await ctx.route(u => u.pathname === "/v1/tasks/" + plain + "/files/list", r =>
      r.fulfill({ status: 404, contentType: "application/json", body: '{"error":"not found"}' }));
    await ctx.addInitScript(() => {
      const Real = window.WebSocket;
      window.WebSocket = function (url, protocols) {
        if (!/\/attach(\?|$)/.test(url)) return new Real(url, protocols);
        const s = { url, readyState: 0, binaryType: "arraybuffer", onopen: null, onclose: null, onmessage: null,
          onerror: null, send() {}, close() { this.readyState = 3; } };
        setTimeout(() => { s.readyState = 1; if (s.onopen) s.onopen({}); }, 0);
        return s;
      };
      Object.assign(window.WebSocket, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });
    });
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector('#stack-list .stackrow[data-id="' + plain + '"]', { state: "attached", timeout: slow(15000) });
    const listed = id => (counts.get("/v1/tasks/" + id + "/files/list") || 0);
    const settle = () => new Promise(r => setTimeout(r, 300));
    for (const id of [plain, review, plain, review, plain]) { await p.evaluate(x => attachTask(x), id); await settle(); }
    if (listed(plain) !== 1) fail("findingsProbe: a card with no findings was probed " + listed(plain) + " times over 3 attaches, want 1");
    if (listed(review) < 1) fail("findingsProbe: a card holding findings was never probed");
    await p.evaluate(x => attachTask(x), review);
    await p.waitForSelector("#t-walk:not([hidden])", { timeout: slow(10000) })
      .catch(() => fail("findingsProbe: the card holding findings lost its walk button"));
  } finally {
    await ctx.close();
    mock.tasksMode = was;
    mock.mockBroadcast = wasB;
    mock.landList = [];
    delete mock.walkDirs[review];
    try { fs.rmSync(dir, { recursive: true, force: true }); } catch (e) {}
  }
  if (errors.length) fail("findingsProbe: the page threw: " + errors.join(" | "));
  console.log("findingsProbe: ok");
}

// The rooms dashboard: the room menu's tiles, fed `room-stats` snapshots.
//
// Drives `onRoomStats` directly with fixture snapshots, the same function the
// stream's listener calls, and checks what the tiles say: a band with no data
// is a dash, the all-rooms tile is the sum of the rooms, the limit bars say
// `highest seen` and are the highest and never a sum, and the open menu asks
// for nothing while it repaints. Then that `?demo=rooms` is the only way in:
// a plain board and a guest page run none of it. ROOMS_DASH_SHOTS=<dir> saves
// the open menu at desktop and phone width, in the default skin and paper.
// transportChips: the Rooms tab's transport chip wears a colour and an icon per transport, and the warning colour
// with the word "fallback" when the room is on a later rung. TRANSPORT_SHOTS=dir writes the light and dark PNGs.
async function transportChipsSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  const errors = [];
  p.on("pageerror", e => errors.push(String(e)));
  await p.addInitScript(() => localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true })));
  try {
    await p.goto(base + "/?demo=rooms", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof transportBadge === "function" && !document.getElementById("rooms").hidden,
      null, { timeout: slow(15000) });
    const got = await p.evaluate(() => {
      const host = document.createElement("div");
      host.id = "tp-probe";
      host.style.cssText = "position:fixed;left:20px;top:20px;z-index:99999;padding:12px;display:flex;gap:10px;" +
        "background:var(--bg-1);";
      host.innerHTML = [["ziti"], ["direct"], ["zrok"], ["ziti", true], ["direct", true], ["local"]]
        .map(a => transportBadge(a[0], a[1])).join("");
      document.body.appendChild(host);
      return [...host.children].map(c => {
        const cs = getComputedStyle(c);
        return { cls: c.className, text: c.textContent.trim(), svg: c.querySelectorAll("svg").length,
          color: cs.color, bg: cs.backgroundColor };
      });
    });
    const [ziti, mtls, zrok, zitiFb, mtlsFb, local] = got;
    if (!ziti.cls.includes("tp-ziti") || ziti.svg !== 1 || ziti.text !== "ziti") fail("ziti chip: " + JSON.stringify(ziti));
    if (!mtls.cls.includes("tp-mtls") || mtls.svg !== 1 || mtls.text !== "mTLS") fail("mTLS chip: " + JSON.stringify(mtls));
    if (!zrok.cls.includes("tp-zrok") || zrok.svg !== 1 || zrok.text !== "zrok") fail("zrok chip: " + JSON.stringify(zrok));
    if (new Set([ziti.color, mtls.color, zrok.color]).size !== 3) fail("the three transports share a colour: " + JSON.stringify(got));
    for (const f of [zitiFb, mtlsFb]) {
      if (!f.cls.includes("warn") || !/fallback$/.test(f.text)) fail("a fallback chip lacks warn or the word: " + JSON.stringify(f));
    }
    if (zitiFb.color !== mtlsFb.color) fail("fallback chips do not share the warning colour: " + JSON.stringify([zitiFb, mtlsFb]));
    if (zitiFb.color === ziti.color) fail("a fallback ziti chip still wears the ziti colour");
    if (local.svg !== 0 || local.cls.includes("tp-")) fail("the local chip should stay plain: " + JSON.stringify(local));

    const shots = process.env.TRANSPORT_SHOTS || "";
    const css = fs.readFileSync(path.join(__dirname, "../../internal/api/web/css/themes.css"), "utf8");
    const skins = [""].concat([...css.matchAll(/^ {2}:root\[data-skin="([a-z]+)"\] \{\s*$/gm)].map(m => m[1]));
    const lights = [];
    for (const skin of skins) {
      await p.evaluate(sk => applySkin(sk), skin);
      const probe = await p.evaluate(() => {
        const chan = s => {
          const v = s.match(/[\d.]+(?:e-?\d+)?/g).map(Number);
          return s.startsWith("color(") ? v.map((x, i) => i < 3 ? x * 255 : x) : v;
        };
        const lin = v => (v /= 255) <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
        const lum = c => 0.2126 * lin(c[0]) + 0.7152 * lin(c[1]) + 0.0722 * lin(c[2]);
        const host = document.getElementById("tp-probe");
        const under = chan(getComputedStyle(host).backgroundColor);
        const flatOver = (c, bgc) => { const b = chan(bgc), a = b.length > 3 ? b[3] : 1; return [0, 1, 2].map(i => b[i] * a + under[i] * (1 - a)); };
        const ratio = (a, b) => (Math.max(lum(a), lum(b)) + 0.05) / (Math.min(lum(a), lum(b)) + 0.05);
        const marks = [...host.querySelectorAll(".chip.tp-zrok:not(.warn), .chip.tp-ziti:not(.warn)")].map(c => {
          const bg = flatOver(c, getComputedStyle(c).backgroundColor);
          if (c.classList.contains("tp-zrok")) {
            const fill = getComputedStyle(c.querySelector("svg path")).fill;
            return { mark: "zrok", fill, ratio: ratio(chan(fill), bg) };
          }
          const ring = getComputedStyle(c.querySelector("svg circle")).stroke;
          return { mark: "ziti", fill: "ring " + ring, ratio: ratio(chan(ring), bg) };
        });
        return {
          marks,
          light: lum(under) > 0.4,
          chips: [...host.querySelectorAll(".chip.tp-ziti:not(.warn), .chip.tp-mtls:not(.warn), .chip.tp-zrok:not(.warn)")].map(c => {
            const cs = getComputedStyle(c);
            const bg = chan(cs.backgroundColor), a = bg.length > 3 ? bg[3] : 1;
            const flat = [0, 1, 2].map(i => bg[i] * a + under[i] * (1 - a));
            const fg = chan(cs.color);
            const hi = Math.max(lum(flat), lum(fg)), lo = Math.min(lum(flat), lum(fg));
            return { color: cs.color, ratio: (hi + 0.05) / (lo + 0.05), cls: c.className };
          }),
        };
      });
      const name = skin || "default";
      if (new Set(probe.chips.map(c => c.color)).size !== 3) fail("skin " + name + " collapsed the transport colours: " + JSON.stringify(probe.chips));
      for (const c of probe.chips) {
        if (c.ratio < 4.5) fail("skin " + name + " " + c.cls + " reads " + c.ratio.toFixed(2) + ":1, under 4.5:1");
      }
      if (probe.marks.length !== 2) fail("skin " + name + " drew " + probe.marks.length + " marks to check, expected zrok and ziti");
      for (const m of probe.marks) {
        if (m.ratio < 3) fail("skin " + name + " " + m.mark + " mark (" + m.fill + ") reads " + m.ratio.toFixed(2) + ":1, under 3:1");
      }
      if (probe.light) lights.push(name);
      if (shots) {
        fs.mkdirSync(shots, { recursive: true });
        await p.locator("#tp-probe").screenshot({ path: path.join(shots, `transport-chips-${name}.png`) });
      }
    }
    if (lights.length < 4) fail("fewer than four light skins found, so the contrast check proved little: " + lights);
    if (errors.length) fail("page errors: " + errors.join("; "));
  } finally {
    await ctx.close();
  }
}

// pathBadges: the Rooms tab draws one badge per transport kind, in the fixed order direct, ziti, zrok, wearing the state
// the room reported: on solid, up outlined, down the warning colour, none and unlistened grey, with the reason in the tip.
// A room that reports nothing draws nothing. PATHS_SHOTS=dir writes the room rows without (before) and with (after) them.
async function pathBadgesSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1100, height: 700 } });
  const p = await ctx.newPage();
  const errors = [];
  p.on("pageerror", e => errors.push(String(e)));
  await p.addInitScript(() => localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true })));
  try {
    await p.goto(base + "/?demo=rooms", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof pathBadges === "function" && !document.getElementById("rooms").hidden,
      null, { timeout: slow(15000) });
    const got = await p.evaluate(() => {
      const draw = ps => {
        const h = document.createElement("div");
        h.innerHTML = pathBadges(ps);
        document.body.appendChild(h);
        return [...h.querySelectorAll(".chip.pth")].map(c => ({ cls: c.className, text: c.textContent, tip: c.dataset.tip,
          bg: getComputedStyle(c).backgroundColor, color: getComputedStyle(c).color }));
      };
      return {
        none: pathBadges(undefined) + pathBadges([]),
        mixed: draw([{ kind: "zrok", state: "none" }, { kind: "ziti", state: "down", addr: "atrium-hub", last_error: "no answer" },
          { kind: "direct", state: "on", addr: "h:7779" }, { kind: "direct", state: "up", rtt_ms: 4 }]),
        every: draw([{ kind: "direct", state: "on" }, { kind: "ziti", state: "up", rtt_ms: 7 }, { kind: "zrok", state: "unlistened",
          last_error: "the hub is not listening on zrok" }]),
        hostile: draw([{ kind: "direct", state: "down", last_error: '"><img src=x onerror=alert(1)>' }]),
      };
    });
    if (got.none !== "") fail("a room that reports no paths drew badges: " + got.none);
    if (got.mixed.map(c => c.text).join() !== "direct,ziti,zrok") fail("badges are not one per kind in the fixed order: " + JSON.stringify(got.mixed));
    if (!got.mixed[0].cls.includes("pth-on")) fail("the live rung should win its kind: " + got.mixed[0].cls);
    if (!got.mixed[1].cls.includes("pth-down") || !/no answer/.test(got.mixed[1].tip)) fail("a down path lacks its class or reason: " + JSON.stringify(got.mixed[1]));
    if (!got.mixed[2].cls.includes("pth-none") || !/no way to dial/.test(got.mixed[2].tip)) fail("none: " + JSON.stringify(got.mixed[2]));
    if (!got.every[1].cls.includes("pth-up") || !/7 ms/.test(got.every[1].tip)) fail("up: " + JSON.stringify(got.every[1]));
    if (!got.every[2].cls.includes("pth-unlistened") || !/not listening/.test(got.every[2].tip)) fail("unlistened: " + JSON.stringify(got.every[2]));
    if (new Set(got.every.map(c => c.bg + "|" + c.color)).size !== 3) fail("on, up and unlistened look alike: " + JSON.stringify(got.every));
    if (got.hostile.length !== 1 || !/onerror/.test(got.hostile[0].tip)) fail("a path error was not kept as inert text: " + JSON.stringify(got.hostile));
    const mt = await p.evaluate(() => {
      const draw = ps => { const h = document.createElement("div"); h.innerHTML = pathBadges(ps); return [...h.querySelectorAll(".chip.pth")].map(c => ({ text: c.textContent, tip: c.dataset.tip })); };
      return { yes: draw([{ kind: "direct", state: "on", mtls: true, rtt_ms: 3 }, { kind: "ziti", state: "up", rtt_ms: 30, stale: true }]),
        no: draw([{ kind: "direct", state: "up" }]), drift: latencyRow("k", { drift_p99_ms: 2.1, drift_series_ms: [] }) };
    });
    if (mt.yes[0].text !== "mTLS" || !/connect time 3 ms/.test(mt.yes[0].tip)) fail("a rung with a client cert should read mTLS with its connect time: " + JSON.stringify(mt.yes[0]));
    if (mt.no[0].text !== "direct") fail("a direct rung without a client cert must not claim mTLS: " + JSON.stringify(mt.no));
    if (!/last measured connect time 30 ms/.test(mt.yes[1].tip)) fail("a stale rtt should say so: " + JSON.stringify(mt.yes[1]));
    if (!/timer drift/.test(mt.drift) || /<span>lag /.test(mt.drift) || !/Not network latency/.test(mt.drift)) fail("the lag tile should be timer drift and say it is not latency: " + mt.drift);
    const injected = await p.evaluate(() => !!document.querySelector("img[src=x]"));
    if (injected) fail("a path error from a room became markup");

    const shots = process.env.PATHS_SHOTS || "";
    const rows = await p.evaluate(() => {
      const h = document.createElement("div");
      h.id = "pth-rows";
      h.style.cssText = "position:fixed;left:10px;top:10px;width:900px;z-index:99999;background:var(--bg-1);padding:8px";
      document.body.appendChild(h);
      return ROOMS_DEMO_SET.length;
    });
    const paint = () => p.evaluate(() => { document.getElementById("pth-rows").innerHTML = ROOMS_DEMO_SET.map(r => roomRow(Object.assign({ attached: true }, r))).join(""); return document.querySelectorAll("#pth-rows .roomrow .pth").length; });
    if ((await paint()) < rows * 2) fail("the demo room rows carry no path badges");
    if (shots) {
      fs.mkdirSync(shots, { recursive: true });
      await p.evaluate(() => { window.__pb = pathBadges; window.pathBadges = () => ""; });
      await paint();
      await p.locator("#pth-rows").screenshot({ path: path.join(shots, "path-badges-before.png") });
      await p.evaluate(() => { window.pathBadges = window.__pb; });
      await paint();
      await p.locator("#pth-rows").screenshot({ path: path.join(shots, "path-badges-after.png") });
    }
    if (errors.length) fail("page errors: " + errors.join("; "));
  } finally {
    await ctx.close();
  }
}

// transportToggle: ziti and zrok badges on a room row are switches. An off transport reads off, a transport whose dials
// keep failing is flagged hot, and pressing a badge asks the room, through the hub, to turn that transport off or on
// (POST /v1/link/transports with the room header). The room's refusal is shown as it was said. direct has no switch.
// TOGGLE_SHOTS=dir writes the room rows before (no states, no switch) and after.
async function transportToggleSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1100, height: 400 } });
  const p = await ctx.newPage();
  const errors = [];
  p.on("pageerror", e => errors.push(String(e)));
  await p.addInitScript(() => localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "transport-off": true })));
  const calls = [];
  await p.route("**/v1/link/transports", route => {
    const req = route.request();
    calls.push({ room: req.headers()["x-atrium-room"], body: req.postData() });
    if (/"zrok"/.test(req.postData())) {
      return route.fulfill({ status: 409, contentType: "application/json",
        body: JSON.stringify({ error: "refused: zrok is this room's last working path to the hub, so it stays on" }) });
    }
    return route.fulfill({ status: 200, contentType: "application/json", body: "{}" });
  });
  try {
    await p.goto(base + "/?demo=rooms", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof pathBadges === "function" && !document.getElementById("rooms").hidden,
      null, { timeout: slow(15000) });
    const rooms = [
      { name: "sg3", transport: "direct", via: "direct", attached: true, paths: [{ kind: "direct", state: "on", mtls: true, addr: "h:7779" },
        { kind: "ziti", state: "off" }, { kind: "zrok", state: "none" }] },
      { name: "m1mini", transport: "ziti", via: "ziti", attached: true, paths: [{ kind: "direct", state: "up", mtls: true },
        { kind: "ziti", state: "on", hammering: true, last_error: "connection refused" }, { kind: "zrok", state: "up" }] },
    ];
    await p.evaluate(rs => {
      window.__rooms = rs;
      const h = document.createElement("div");
      h.id = "tt-rows";
      h.style.cssText = "position:fixed;left:10px;top:10px;width:900px;z-index:99999;background:var(--bg-1);padding:8px";
      document.body.appendChild(h);
      window.__paint = () => { h.innerHTML = window.__rooms.map(r => roomRow(r)).join(""); };
      window.__paint();
    }, rooms);
    const chips = await p.evaluate(() => [...document.querySelectorAll("#tt-rows .roomrow")].map(r => [...r.querySelectorAll(".chip.pth")].map(c => ({
      tag: c.tagName, cls: c.className, text: c.textContent.replace(/\s+/g, " ").trim(), kind: c.dataset.kind || "", on: c.dataset.on || "" }))));
    const [sg3, mini] = chips;
    if (sg3[0].tag !== "SPAN" || sg3[0].cls.includes("pthtoggle")) fail("direct must have no switch: " + JSON.stringify(sg3[0]));
    if (!sg3[1].cls.includes("pth-off") || sg3[1].tag !== "BUTTON" || sg3[1].on !== "1" || !/ziti off/.test(sg3[1].text)) {
      fail("an off ziti should read off and offer to turn on: " + JSON.stringify(sg3[1]));
    }
    if (sg3[2].tag !== "SPAN") fail("a kind the room cannot dial has a switch: " + JSON.stringify(sg3[2]));
    if (!mini[1].cls.includes("hot") || !/!/.test(mini[1].text)) fail("a hammering transport is not flagged: " + JSON.stringify(mini[1]));
    if (mini[1].on !== "0") fail("a live ziti should offer to turn off: " + JSON.stringify(mini[1]));

    const shots = process.env.TOGGLE_SHOTS || "";
    if (shots) {
      fs.mkdirSync(shots, { recursive: true });
      await p.evaluate(() => {
        // BEFORE: the badges as they were drawn, a state each and no switch.
        window.__after = pathBadges;
        window.pathBadges = ps => window.__after((ps || []).map(x => ({ kind: x.kind, state: x.state === "off" ? "none" : x.state, addr: x.addr, mtls: x.mtls })));
        window.__paint();
      });
      await p.locator("#tt-rows").screenshot({ path: path.join(shots, "transport-toggle-before.png") });
      await p.evaluate(() => { window.pathBadges = window.__after; window.__paint(); });
      await p.locator("#tt-rows").screenshot({ path: path.join(shots, "transport-toggle-after.png") });
    }

    await p.locator("#tt-rows .roomrow:nth-child(1) .pthtoggle").click();
    for (let i = 0; i < 50 && calls.length < 1; i++) await p.waitForTimeout(100);
    if (!calls[0] || calls[0].room !== "sg3" || calls[0].body !== JSON.stringify({ transport: "ziti", on: true })) {
      fail("turning ziti on asked the wrong thing: " + JSON.stringify(calls[0]));
    }
    // The refusal is shown as the room said it.
    await p.locator("#tt-rows .roomrow:nth-child(2) .pthtoggle").nth(1).click();
    for (let i = 0; i < 50 && calls.length < 2; i++) await p.waitForTimeout(100);
    if (!calls[1] || calls[1].room !== "m1mini" || calls[1].body !== JSON.stringify({ transport: "zrok", on: false })) {
      fail("turning zrok off asked the wrong thing: " + JSON.stringify(calls[1]));
    }
    await p.waitForFunction(() => /last working path to the hub/.test(document.body.innerText), null, { timeout: slow(5000) })
      .catch(() => fail("the room's refusal was not shown"));
    if (errors.length) fail("page errors: " + errors.join("; "));
  } finally {
    await ctx.close();
  }
}

// hubTransports: the gear lists the hub's ziti transports (the board, the room link, the CI hook) with a state, a
// service and an identity each. Turning one on or off PUTs {name, on, service, identity} to /_hub/transports, a flag
// seed is named, a 409 refusal is shown as the hub said it, and a 403 makes the row read-only.
// HT_SHOTS=dir writes the row before (no section) and after.
async function hubTransportsSection(browser, base) {
  const tag = "hubTransports: ";
  const open = async (mode) => {
    const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
    const st = { puts: [], gets: 0, on: false, zrok: false };
    const view = () => ({ transports: [
      { name: "board-ziti", label: "Board over ziti", kind: "board-ziti", on: st.on, state: st.on ? "bound" : "off", service: "atrium-board",
        default_service: "atrium-board", identity: "", identity_used: "/ids/hub.json", seeded: ["on"], switchable: true },
      { name: "room-ziti", label: "Room link over ziti", kind: "ziti", on: true, state: "failing", last_error: "controller unreachable",
        service: "atrium-hub", default_service: "atrium-hub", identity: "", identity_used: "/ids/hub.json", switchable: true },
      { name: "ci-hook", label: "CI hook", kind: "ci-hook", on: false, state: "off", service: "ziti-hook", default_service: "ziti-hook",
        identity: "", identity_used: "/ids/hub.json", switchable: true },
      { name: "room-zrok", label: "Room link over zrok", kind: "zrok", on: st.zrok, state: st.zrok ? "bound" : "off", service: "",
        default_service: "", identity: "", identity_used: "", seeded: ["on"], switchable: true, no_service: true },
    ] });
    await ctx.route("**/_hub/transports", route => {
      const req = route.request();
      if (mode === "none") return route.fulfill({ status: 404, body: "" });
      if (req.method() === "PUT") {
        if (mode === "remote") return route.fulfill({ status: 403, contentType: "application/json", body: JSON.stringify({ error: "only from the hub's machine" }) });
        const b = JSON.parse(req.postData());
        st.puts.push(b);
        if (b.name === "room-ziti") {
          return route.fulfill({ status: 409, contentType: "application/json", body: JSON.stringify({ error: "refused: m1mini reaches this hub only over ziti" }) });
        }
        if (b.name === "board-ziti") st.on = !!b.on;
        if (b.name === "room-zrok") st.zrok = !!b.on;
      } else st.gets++;
      return route.fulfill({ contentType: "application/json", body: JSON.stringify(view()) });
    });
    const errors = [];
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow, #term-list .card.tab", { state: "attached", timeout: slow(15000) });
    await p.waitForFunction(() => new Promise(r => requestAnimationFrame(() => r(!!document.getElementById("gear").onclick))),
      null, { timeout: slow(20000), polling: 10 });
    return { ctx, p, st, errors };
  };
  const showPane = async p => {
    await p.click("#gear");
    await p.evaluate(() => showSettingsPane("expose the board"));
  };
  let h = await open("hub");
  try {
    const { p, st } = h;
    await showPane(p);
    await p.waitForFunction(() => !document.getElementById("s-ht-row").hidden, null, { timeout: slow(10000) });
    const a = await p.evaluate(() => [...document.querySelectorAll("#s-ht-list .ht-item")].map(r => ({
      name: r.dataset.name, text: r.textContent.replace(/\s+/g, " "), btn: r.querySelector(".ht-switch").textContent })));
    if (a.map(x => x.name).join("|") !== "board-ziti|room-ziti|ci-hook|room-zrok") fail(tag + "the rows were " + JSON.stringify(a));
    if (!/set by flag: on/.test(a[0].text)) fail(tag + "a seeded value is not named: " + a[0].text);
    if (!/failing: controller unreachable/.test(a[1].text)) fail(tag + "a failing transport shows no reason: " + a[1].text);
    if (a[0].btn !== "turn on" || a[1].btn !== "turn off") fail(tag + "the switches read " + JSON.stringify(a.map(x => x.btn)));

    if (!/set by flag: on/.test(a[3].text) || a[3].btn !== "turn on") fail(tag + "the zrok row reads " + a[3].text + " / " + a[3].btn);
    const zrokBox = await p.evaluate(() => ({ inputs: document.querySelectorAll('#s-ht-list [data-name="room-zrok"] input').length,
      apply: document.querySelectorAll('#s-ht-list [data-name="room-zrok"] .ht-apply').length }));
    if (zrokBox.inputs || zrokBox.apply) fail(tag + "the zrok row has a service or identity box: " + JSON.stringify(zrokBox));
    const zrokShot = async n => { if (process.env.HT_SHOTS) { fs.mkdirSync(process.env.HT_SHOTS, { recursive: true }); await p.locator('#s-ht-list [data-name="room-zrok"]').screenshot({ path: path.join(process.env.HT_SHOTS, n) }); } };
    await zrokShot("zrok-row-before.png");
    await p.click('#s-ht-list [data-name="room-zrok"] .ht-switch');
    await p.waitForFunction(() => /turn off/.test(document.querySelector('#s-ht-list [data-name="room-zrok"] .ht-switch').textContent),
      null, { timeout: slow(5000) }).catch(() => fail(tag + "turning zrok on did not show."));
    await zrokShot("zrok-row-after.png");
    const zp = st.puts.find(x => x.name === "room-zrok");
    if (!zp || zp.on !== true || "service" in zp || "identity" in zp) fail(tag + "the zrok PUT was " + JSON.stringify(zp));
    st.puts.length = 0;

    const shots = process.env.HT_SHOTS || "";
    if (shots) {
      fs.mkdirSync(shots, { recursive: true });
      await p.evaluate(() => { document.getElementById("s-ht-row").style.display = "none"; });
      await p.locator("#settings").screenshot({ path: path.join(shots, "hub-transports-before.png") });
      await p.evaluate(() => { document.getElementById("s-ht-row").style.display = ""; document.getElementById("s-ht-row").scrollIntoView(); });
      await p.locator("#settings").screenshot({ path: path.join(shots, "hub-transports-after.png") });
    }

    await p.click('#s-ht-list [data-name="board-ziti"] .ht-switch');
    await p.waitForFunction(() => /turn off/.test(document.querySelector('#s-ht-list [data-name="board-ziti"] .ht-switch').textContent),
      null, { timeout: slow(5000) }).catch(() => fail(tag + "turning the board on did not show."));
    if (!st.puts[0] || st.puts[0].name !== "board-ziti" || st.puts[0].on !== true || st.puts[0].service !== "atrium-board") {
      fail(tag + "turn on PUT " + JSON.stringify(st.puts));
    }
    await p.click('#s-ht-list [data-name="room-ziti"] .ht-switch');
    await p.waitForFunction(() => /reaches this hub only over ziti/.test(document.getElementById("s-ht-msg").textContent),
      null, { timeout: slow(5000) }).catch(() => fail(tag + "the hub's refusal was not shown."));
    const gets = st.gets;
    await p.evaluate(() => document.getElementById("settings").close());
    await showPane(p);
    await p.waitForTimeout(400); // wait: settle for a render with no event to wait on
    if (st.gets <= gets) fail(tag + "opening the gear did not read the transports again.");
    if (h.errors.length) fail(tag + "page errors: " + h.errors.join(" | "));
  } finally { await h.ctx.close(); }

  h = await open("remote");
  try {
    const { p } = h;
    await showPane(p);
    await p.waitForFunction(() => !document.getElementById("s-ht-row").hidden, null, { timeout: slow(10000) });
    await p.click('#s-ht-list [data-name="board-ziti"] .ht-switch');
    await p.waitForFunction(() => !document.getElementById("s-ht-ro").hidden, null, { timeout: slow(5000) })
      .catch(() => fail(tag + "a 403 did not make the row read-only."));
    const r = await p.evaluate(() => ({ switches: document.querySelectorAll("#s-ht-list .ht-switch").length,
      said: document.getElementById("s-ht-ro").textContent }));
    if (r.switches || !/hub's machine/.test(r.said)) fail(tag + "the read-only row is wrong: " + JSON.stringify(r));
  } finally { await h.ctx.close(); }

  h = await open("none");
  try {
    const { p } = h;
    await showPane(p);
    await p.waitForTimeout(600); // wait: settle for a render with no event to wait on
    if (!(await p.evaluate(() => document.getElementById("s-ht-row").hidden))) fail(tag + "the row shows with no hub.");
  } finally { await h.ctx.close(); }
}

// zitiLogo: a ziti transport badge wears the vendored OpenZiti mark (an inline gradient disc, no fetch) in place of
// the ring glyph, at the badge's text height, with its own gradient id per badge; mTLS and zrok keep their icons.
// On the demo rooms page the ziti rooms' cards carry it. ZITI_LOGO_SHOTS=dir writes the rooms page.
async function zitiLogoSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  const errors = [];
  const fetched = [];
  p.on("pageerror", e => errors.push(String(e)));
  p.on("request", r => { if (/ziti|logo/i.test(r.url()) && /\.(svg|png)/i.test(r.url())) fetched.push(r.url()); });
  await p.addInitScript(() => localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true })));
  try {
    await p.goto(base + "/?demo=rooms", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof transportBadge === "function" && !document.getElementById("rooms").hidden,
      null, { timeout: slow(15000) });
    const got = await p.evaluate(() => {
      const host = document.createElement("div");
      host.id = "zl-probe";
      host.style.cssText = "position:fixed;left:20px;top:20px;z-index:99999;display:flex;gap:10px;";
      host.innerHTML = [["ziti"], ["ziti"], ["direct"], ["zrok"], ["ziti", true]].map(a => transportBadge(a[0], a[1])).join("");
      document.body.appendChild(host);
      return [...host.children].map(c => {
        const s = c.querySelector("svg");
        const r = s && s.getBoundingClientRect();
        const g = s && s.querySelector("linearGradient");
        return { logo: !!(s && s.classList.contains("zlogo")), w: r && r.width, h: r && r.height,
          fs: parseFloat(getComputedStyle(c).fontSize), gid: g && g.id,
          fill: s && s.querySelector("circle") && getComputedStyle(s.querySelector("circle")).fill };
      });
    });
    const [z1, z2, mtls, zrok, zfb] = got;
    for (const z of [z1, z2, zfb]) {
      if (!z.logo || !(z.h >= z.fs - 1 && z.h <= z.fs + 4) || !/^url\(/.test(z.fill || "")) fail("ziti badge lacks the logo: " + JSON.stringify(z));
    }
    if (z1.gid === z2.gid) fail("two ziti badges share a gradient id: " + z1.gid);
    if (mtls.logo || (zrok.logo && zrok.gid)) fail("mTLS or zrok wears the ziti logo: " + JSON.stringify([mtls, zrok]));
    if (fetched.length) fail("the logo was fetched: " + fetched.join(", "));
    const shots = process.env.ZITI_LOGO_SHOTS || "";
    if (shots) {
      fs.mkdirSync(shots, { recursive: true });
      await p.evaluate(() => { document.getElementById("zl-probe").remove(); openRooms(); });
      await p.waitForTimeout(600);
      await p.screenshot({ path: path.join(shots, "rooms-page.png") });
    }
    if (errors.length) fail("page errors: " + errors.join("; "));
  } finally {
    await ctx.close();
  }
}

// zrokLogo: a zrok transport badge wears the vendored rocket at the ziti logo's size, in its own green, with the state
// chips beside it, and overlays.js draws its mark from the same path. ZROK_LOGO_SHOTS=dir writes the screenshots.
async function zrokLogoSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  const errors = [];
  const fetched = [];
  p.on("pageerror", e => errors.push(String(e)));
  p.on("request", r => { if (/zrok|logo/i.test(r.url()) && /\.(svg|png)/i.test(r.url())) fetched.push(r.url()); });
  await p.addInitScript(() => localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true })));
  try {
    await p.goto(base + "/?demo=rooms", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof transportBadge === "function" && !document.getElementById("rooms").hidden,
      null, { timeout: slow(15000) });
    const got = await p.evaluate(() => {
      const host = document.createElement("div");
      host.id = "zr-probe";
      host.style.cssText = "position:fixed;left:20px;top:20px;z-index:99999;display:flex;gap:10px;";
      host.innerHTML = [["zrok"], ["ziti"], ["zrok", true]].map(a => transportBadge(a[0], a[1])).join("");
      document.body.appendChild(host);
      const sizes = [...host.children].map(c => {
        const s = c.querySelector("svg");
        const r = s.getBoundingClientRect();
        return { cls: s.getAttribute("class"), w: r.width, h: r.height, fs: parseFloat(getComputedStyle(c).fontSize),
          fill: s.querySelector("path") && getComputedStyle(s.querySelector("path")).fill,
          tip: c.getAttribute("data-tip"), text: c.textContent.trim() };
      });
      const chips = document.createElement("div");
      chips.id = "zr-chips";
      chips.style.cssText = "position:fixed;left:20px;top:60px;z-index:99999;display:flex;gap:10px;";
      chips.innerHTML = pathBadges([{ kind: "zrok", state: "on", addr: "private share" }], "r1") +
        pathBadges([{ kind: "zrok", state: "up", rtt_ms: 40 }], "r1") +
        pathBadges([{ kind: "zrok", state: "off" }], "r1") +
        pathBadges([{ kind: "zrok", state: "down", last_error: "share not found", hammering: true }], "r1");
      document.body.appendChild(chips);
      const states = [...chips.querySelectorAll(".pth")].map(c => ({ cls: c.className, tip: c.getAttribute("data-tip") }));
      const overlay = typeof OVERLAY_MARKS === "object" && OVERLAY_MARKS.zrok;
      return { sizes, states, shared: !!overlay && overlay.includes(ZROK_MARK.d) && overlay.includes(ZROK_MARK.viewBox) };
    });
    const [zr, zi, zfb] = got.sizes;
    for (const z of [zr, zfb]) {
      if (!/zrlogo/.test(z.cls || "") || !(z.h >= z.fs - 1 && z.h <= z.fs + 4) || !z.fill || z.fill === "none") {
        fail("zrok badge lacks the rocket: " + JSON.stringify(z));
      }
    }
    if (zr.h !== zi.h || zr.w !== zi.w) fail("zrok mark is not the ziti mark's size: " + JSON.stringify([zr, zi]));
    if (zr.text !== "zrok" || !/private zrok share/.test(zr.tip)) fail("zrok badge text or tip: " + JSON.stringify(zr));
    if (!/first choice/.test(zfb.tip) || !/fallback/.test(zfb.text)) fail("zrok fallback tip: " + JSON.stringify(zfb));
    if (!got.shared) fail("overlays.js does not draw its zrok mark from ZROK_MARK");
    const want = ["pth-on", "pth-up", "pth-off", "pth-down"];
    got.states.forEach((s, i) => {
      if (!s.cls.includes(want[i])) fail("zrok state chip " + i + " is not " + want[i] + ": " + JSON.stringify(s));
    });
    if (!/hot/.test(got.states[3].cls) || !/share not found/.test(got.states[3].tip)) fail("failing chip: " + JSON.stringify(got.states[3]));
    if (fetched.length) fail("the logo was fetched: " + fetched.join(", "));
    const shots = process.env.ZROK_LOGO_SHOTS || "";
    if (shots) {
      fs.mkdirSync(shots, { recursive: true });
      await p.screenshot({ path: path.join(shots, "probe.png"), clip: { x: 0, y: 0, width: 700, height: 100 } });
      await p.evaluate(() => { document.getElementById("zr-probe").remove(); document.getElementById("zr-chips").remove(); openRooms(); });
      await p.waitForTimeout(600); // wait: settle for a render with no event to wait on
      await p.screenshot({ path: path.join(shots, "rooms-page.png") });
    }
    if (errors.length) fail("page errors: " + errors.join("; "));
  } finally {
    await ctx.close();
  }
}

async function roomsDashSection(browser, base) {
  const skip = () => localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
  const shots = process.env.ROOMS_DASH_SHOTS || "";

  // ── the preview, then fixture snapshots through onRoomStats ─────────────
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  const errors = [];
  p.on("pageerror", e => errors.push(String(e)));
  await p.addInitScript(skip);
  try {
    await p.clock.install();
    await p.goto(base + "/?demo=rooms", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof roomsDemoLive !== "undefined" && roomsDemoLive &&
      roomsDemoTicks > 0 && !document.getElementById("rooms").hidden, null, { timeout: slow(15000) });
    await fakePause(p);
    const chip = await p.evaluate(() => document.getElementById("rooms-t").textContent);
    if (chip !== "3/4 rooms") fail("the demo's chip did not read 3/4 rooms: " + chip);

    await p.evaluate(() => openRooms());
    const drawn = await p.evaluate(() => {
      const m = document.getElementById("rooms-menu");
      return {
        open: !m.hidden,
        rooms: [...m.querySelectorAll(".rtile[data-room]")].map(t => t.dataset.room),
        all: m.querySelectorAll(".rtile.all").length,
        allFirst: !!(m.firstElementChild && m.firstElementChild.classList.contains("all")),
        off: [...m.querySelectorAll(".rtile-off")].map(b => b.dataset.room + (b.querySelector(".roomx") ? "+x" : "")),
        sparks: m.querySelectorAll("svg.rd-spark polyline").length,
        mini: /does not report them/.test((m.querySelector('.rtile[data-room="m1mini"] .rd-band.machine') || {}).textContent || ""),
        width: m.getBoundingClientRect().width,
      };
    });
    if (!drawn.open || drawn.rooms.join(",") !== "m1mini,sg3,sg4" || drawn.all !== 1 || !drawn.allFirst) {
      fail("the demo menu did not draw the all-rooms tile then one tile per live room: " + JSON.stringify(drawn));
    }
    if (drawn.off.join(",") !== "lab-pc+x") fail("the disconnected demo room is not below with its x: " + JSON.stringify(drawn.off));
    if (drawn.sparks < 5) fail("the demo tiles drew " + drawn.sparks + " sparklines, expected at least 5");
    if (!drawn.mini) fail("m1mini ships no machine in the demo and its band did not say the room does not report it");
    if (drawn.width < 500 || drawn.width > 600) fail("the rooms panel is not about 560px wide: " + drawn.width);

    if (shots) {
      fs.mkdirSync(shots, { recursive: true });
      await p.waitForTimeout(2300);
      for (const skin of ["", "paper"]) {
        await p.evaluate(sk => { applySkin(sk); }, skin);
        await p.waitForTimeout(250);
        await p.screenshot({ path: path.join(shots, `rooms-${skin || "default"}-1400.png`) });
      }
      await p.setViewportSize({ width: 390, height: 844 });
      await p.evaluate(() => { const m = document.getElementById("rooms-menu"); m.hidden = true; openRooms(); });
      for (const skin of ["", "paper"]) {
        await p.evaluate(sk => { applySkin(sk); }, skin);
        await p.waitForTimeout(250);
        await p.screenshot({ path: path.join(shots, `rooms-${skin || "default"}-390.png`) });
      }
      const phone = await p.evaluate(() => {
        const b = document.querySelector('.rtile[data-room="sg3"] .rt-bands');
        const m = document.getElementById("rooms-menu").getBoundingClientRect();
        return { cols: getComputedStyle(b).gridTemplateColumns.split(" ").length, right: m.right,
          sparks: document.querySelectorAll('.rtile[data-room="sg3"] svg.rd-spark').length };
      });
      if (phone.cols !== 1 || phone.right > 390 || phone.sparks < 2) {
        fail("at phone width the tile bands did not stack inside the window with their sparklines: " + JSON.stringify(phone));
      }
      await p.setViewportSize({ width: 1400, height: 900 });
      await p.evaluate(() => { applySkin(""); const m = document.getElementById("rooms-menu"); m.hidden = true; openRooms(); });
    }

    // The fixture's own ticking stops here, so what is drawn is only what this
    // test sends. The cards are set the same way the preview sets its own.
    const reqs = [];
    p.on("request", r => reqs.push(r.method() + " " + r.url().replace(base, "")));
    const got = await p.evaluate(() => {
      clearInterval(roomsDemoTimer);
      const now = Date.now();
      const iso = ms => new Date(ms).toISOString();
      const card = (id, room, status, over) => Object.assign({ id, room, status, display_title: id,
        last_activity_at: iso(now - 60000) }, over || {});
      roomsDemoCards = [
        card("a1", "sg3", "running", { telemetry: { seconds: 30, five_hour: { pct: 64, resets_at: iso(now + 3600e3) },
          weekly: { pct: 31 } } }),
        card("a2", "sg3", "running"),
        card("a3", "sg3", "needs-input"),
        card("a4", "sg3", "needs-input", { seen: { answered: false, open_questions: ["ok?"] } }),
        card("a5", "sg3", "done"),
        card("a6", "sg3", "done", { last_activity_at: iso(now - 30 * 3600e3) }),
        card("b1", "sg4", "running", { telemetry: { seconds: 60, five_hour: { pct: 82 } } }),
        card("b2", "sg4", "needs-permission"),
        // Stale: a report two hours old does not count.
        card("c1", "m1mini", "running", { telemetry: { seconds: 7200, five_hour: { pct: 99 }, weekly: { pct: 90 } } }),
      ];
      const tok = (rate, a, b, c, d) => ({ per_min_5m: rate, series_per_min: Array.from({ length: 60 }, (_, i) => rate + i),
        series_end: iso(now), last_24h: { in: a, out: b, cache_read: c, cache_write: d } });
      onRoomStats({ v: 1, room: "sg3", at: iso(now), tokens: tok(41200, 9100000, 1200000, 61000000, 2900000),
        disk: { path: "D:\\git", free_bytes: 392e9, total_bytes: 512e9, worktrees: 38 },
        runners: [{ kind: "claude", resolves: true }, { kind: "ollama", resolves: false }],
        machine: { cpu_pct: 82, mem_used_bytes: 21e9, mem_total_bytes: 32e9, cpu_series_pct: [71, 80, 82] } });
      // sg4 has shipped nothing but its CPU: tokens and disk are absent.
      onRoomStats({ v: 1, room: "sg4", at: iso(now), machine: { cpu_pct: 95, cpu_series_pct: [90, 95] } });
      onRoomStats({ v: 1, room: "m1mini", at: iso(now), tokens: tok(9000, 1000000, 100000, 5000000, 200000) });
      // A version this board does not know is dropped.
      onRoomStats({ v: 2, room: "sg3", at: iso(now), tokens: tok(1, 1, 1, 1, 1) });
      const m = document.getElementById("rooms-menu");
      const n = sel => { const e = m.querySelector(sel); return e ? Number(e.dataset.n) : null; };
      const t = name => m.querySelector(`.rtile[data-room="${name}"]`);
      const lim = (tile, k) => {
        const row = [...tile.querySelectorAll(".rd-lim")].find(r => r.querySelector(".k").textContent === k);
        return row ? (row.querySelector(".v") ? row.querySelector(".v").textContent : "dash") : "none";
      };
      return {
        sg3: { run: n('[data-v="r.sg3.run"]'), idle: n('[data-v="r.sg3.idle"]'), need: n('[data-v="r.sg3.need"]'),
          done: n('[data-v="r.sg3.done"]'), rate: n('[data-v="r.sg3.rate"]'), day: n('[data-v="r.sg3.24h"]'),
          cache: n('[data-v="r.sg3.cache"]'), five: lim(t("sg3"), "5h"), week: lim(t("sg3"), "week"),
          seen: /highest seen/.test(t("sg3").textContent), hot: t("sg3").classList.contains("hot"),
          build: (t("sg3").querySelector(".rd-build") || {}).dataset,
          via: [...t("sg3").querySelectorAll(".rt-head .chip")].map(c => c.textContent +
            (c.classList.contains("warn") ? "+warn" : "") + (/first choice did not answer/.test(c.dataset.tip || "") ? "+tip" : "")),
          off: t("sg3").querySelectorAll(".rd-kinds .off").length },
        sg4: { tokDash: !!t("sg4").querySelector(".rd-band.tokens > .rd-row .rd-dash"),
          diskDash: /disk\s*\u2014/.test(t("sg4").querySelector(".rd-band.machine").textContent),
          hot: t("sg4").classList.contains("hot"), five: lim(t("sg4"), "5h"),
          build: !!t("sg4").querySelector(".rd-build"),
          via: [...t("sg4").querySelectorAll(".rt-head .chip")].map(c => c.textContent + (c.classList.contains("warn") ? "+warn" : "")) },
        mini: { five: lim(t("m1mini"), "5h"), week: lim(t("m1mini"), "week") },
        all: { run: n('[data-v="all.run"]'), idle: n('[data-v="all.idle"]'), need: n('[data-v="all.need"]'),
          done: n('[data-v="all.done"]'), rate: n('[data-v="all.rate"]'), day: n('[data-v="all.24h"]'),
          cache: n('[data-v="all.cache"]'), five: lim(m.querySelector(".rtile.all"), "5h"),
          week: lim(m.querySelector(".rtile.all"), "week"), offbuild: n('[data-v="all.offbuild"]'),
          seen: /highest seen/.test(m.querySelector(".rtile.all").textContent) },
      };
    });
    const want = (label, a, b) => { if (JSON.stringify(a) !== JSON.stringify(b)) fail(`rooms dash: ${label} was ${JSON.stringify(a)}, expected ${JSON.stringify(b)}`); };
    want("sg3 agents", [got.sg3.run, got.sg3.idle, got.sg3.need, got.sg3.done], [2, 1, 1, 1]);
    want("sg3 rate", got.sg3.rate, 41200);
    want("sg3 24h (in + out + cache writes)", got.sg3.day, 9100000 + 1200000 + 2900000);
    want("sg3 cache reads", got.sg3.cache, 61000000);
    want("sg3 limits", [got.sg3.five, got.sg3.week], ["64%", "31%"]);
    if (!got.sg3.seen || !got.all.seen) fail("a limit bar is not labelled highest seen: " + JSON.stringify([got.sg3.seen, got.all.seen]));
    if (!got.sg3.hot) fail("sg3 has a card asking and its tile does not pulse");
    if (!got.sg3.build || !/3b578f0/.test(got.sg3.build.tip || "") || !/a91c2de/.test(got.sg3.build.tip || "")) {
      fail("sg3's not the hub's build chip does not name both builds: " + JSON.stringify(got.sg3.build));
    }
    want("sg3 runners not resolving", got.sg3.off, 1);
    // sg3 asks for direct first, and is on ziti because direct did not answer.
    if (!got.sg3.via.includes("ziti fallback+warn+tip")) fail("sg3 fell back to ziti and its tile does not say so: " + JSON.stringify(got.sg3.via));
    if (!got.sg4.via.includes("ziti")) fail("sg4 is on its first choice, ziti, and its chip is not plain: " + JSON.stringify(got.sg4.via));
    if (!got.sg4.tokDash || !got.sg4.diskDash) fail("sg4's absent tokens and disk did not draw dashes: " + JSON.stringify(got.sg4));
    if (!got.sg4.hot) fail("sg4 is at 95% cpu and waiting and does not pulse");
    if (got.sg4.build) fail("sg4 runs the hub's build and wears the not the hub's build chip");
    want("sg4 5h limit", got.sg4.five, "82%");
    want("m1mini's stale limits", [got.mini.five, got.mini.week], ["dash", "dash"]);
    want("all-rooms agents", [got.all.run, got.all.idle, got.all.need, got.all.done], [4, 1, 2, 1]);
    want("all-rooms rate", got.all.rate, 41200 + 9000);
    want("all-rooms 24h", got.all.day, 13200000 + 1300000);
    want("all-rooms cache", got.all.cache, 66000000);
    want("all-rooms limits (the highest, not a sum)", [got.all.five, got.all.week], ["82%", "31%"]);
    want("all-rooms rooms not on the hub's build", got.all.offbuild, 1);

    // A number that moved fades from its old value.
    const fade = await p.evaluate(() => {
      const s = roomStats.sg3;
      onRoomStats(Object.assign({}, s, { tokens: Object.assign({}, s.tokens, { per_min_5m: 50000 }) }));
      const e = document.querySelector('[data-v="r.sg3.rate"]');
      return { was: e.dataset.was, fading: e.classList.contains("rd-fade"),
        still: document.querySelector('[data-v="r.sg3.cache"]').classList.contains("rd-fade") };
    });
    if (fade.was !== "41k" || !fade.fading || fade.still) fail("the changed rate did not fade from its old value alone: " + JSON.stringify(fade));

    // The open menu asks for nothing while it repaints. The preview's ticking
    // comes back on for two ticks and nothing leaves the page for the rooms.
    // The board's own refresh keeps polling beside it, so a fetch is laid at the
    // dashboard's door only when the dashboard is on its stack, and anything to
    // the hub's endpoints counts whoever asked.
    await p.evaluate(() => {
      window.__rdFetches = [];
      const was = window.fetch;
      window.fetch = function (input, init) {
        const stack = String(new Error().stack || "");
        if (/rooms-dash|onRoomStats|roomsDemo|roomTileHTML|paintRoomTile/.test(stack)) {
          window.__rdFetches.push(String((input && input.url) || input));
        }
        return was.apply(this, arguments);
      };
      roomsDemoTimer = setInterval(roomsDemoTick, 2000);
    });
    await fakeTick(p, 4300);
    const dashFetches = await p.evaluate(() => window.__rdFetches);
    const hubReqs = reqs.filter(r => / \/_hub\//.test(r));
    if (dashFetches.length || hubReqs.length) {
      fail("the open rooms menu made requests while it repainted: " + dashFetches.concat(hubReqs).join(" | "));
    }
    if (await p.evaluate(() => document.getElementById("rooms-menu").hidden)) fail("the rooms menu closed during the repaint");
    // The demo keeps its choice in this tab and nothing real.
    const trace = await p.evaluate(() => ({ room: localStorage.getItem("atrium.room") }));
    if (trace.room) fail("the demo wrote a room into localStorage: " + trace.room);
    if (errors.length) fail("the rooms demo page threw: " + errors.join(" | "));
  } finally {
    await ctx.close();
  }

  // ── without the query string, none of it runs ───────────────────────────
  const plain = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const q = await plain.newPage();
  await q.addInitScript(skip);
  try {
    await q.clock.install();
    await q.goto(base + "/", { waitUntil: "domcontentloaded" });
    await q.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await fakePause(q);
    await fakeTick(q, 2300);
    const st = await q.evaluate(() => ({ demo: ROOMS_DEMO, live: roomsDemoLive, ticks: roomsDemoTicks,
      timer: roomsDemoTimer, cards: roomsDemoCards.length, chip: document.getElementById("rooms").hidden,
      stats: Object.keys(roomStats).length }));
    if (st.demo || st.live || st.ticks || st.timer || st.cards || !st.chip || st.stats) {
      fail("without ?demo=rooms some demo code ran: " + JSON.stringify(st));
    }
  } finally {
    await plain.close();
  }

  // ── nor on a guest page, even with it ───────────────────────────────────
  const guest = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const g = await guest.newPage();
  await g.addInitScript(skip);
  await g.clock.install();
  await g.route(u => new URL(u).pathname === "/v1/tasks",
    r => r.fulfill({ status: 403, contentType: "text/plain", body: "this link is one terminal." }));
  try {
    await g.goto(base + "/?demo=rooms", { waitUntil: "domcontentloaded" });
    await g.waitForFunction(() => document.body.classList.contains("guestonly"), null, { timeout: slow(15000) });
    await fakePause(g);
    await fakeTick(g, 2300);
    const st = await g.evaluate(() => ({ live: roomsDemoLive, ticks: roomsDemoTicks, timer: roomsDemoTimer,
      chip: document.getElementById("rooms").hidden }));
    if (st.live || st.ticks || st.timer || !st.chip) fail("a guest page opened with ?demo=rooms ran the demo: " + JSON.stringify(st));
  } finally {
    await guest.close();
  }
}

// THE CARD MENU'S RESTART. Offered on a supervised card and on a parked card with a resume id, left out of one
// atrium cannot restart. An idle card posts to /v1/tasks/<id>/restart at once, a working one asks first, and a
// refusal is a toast. `RESTART_SHOT=/path.png` also saves a picture of the menu.
async function restartMenuSection(browser, base) {
  const was = mock.tasksMode;
  mock.tasksMode = "land";
  landCard("land-rs-idle", { supervised: true, created_at: "2026-10-09T12:00:00Z" });
  landCard("land-rs-work", { supervised: true, activity: { what: "Bash" }, created_at: "2026-10-09T12:01:00Z" });
  landCard("land-rs-park", { supervised: false, parked_at: "2026-10-09T11:00:00Z", resume_id: "abc-123",
    created_at: "2026-10-09T12:02:00Z" });
  landCard("land-rs-bare", { supervised: false, pinned: true, created_at: "2026-10-09T12:03:00Z" });
  landCard("land-rs-paused", { supervised: false, status: "paused", resume_id: "def-456", worktree: "/tmp/paused", created_at: "2026-10-09T12:04:00Z" });
  mock.landList = [mock.LAND["land-rs-idle"], mock.LAND["land-rs-work"], mock.LAND["land-rs-park"], mock.LAND["land-rs-bare"], mock.LAND["land-rs-paused"]];
  mock.landPerms = [];
  const errors = [];
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const posts = [];
  let refuse = false;
  await ctx.route("**/v1/tasks/*/restart", route => {
    posts.push(new URL(route.request().url()).pathname + " " + route.request().method());
    if (refuse) return route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: "it did not exit when asked" }) });
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(mock.LAND["land-rs-idle"]) });
  });
  await ctx.route("**/v1/tasks/*/resume", route => {
    posts.push(new URL(route.request().url()).pathname + " " + route.request().method());
    return route.fulfill({ status: 200, contentType: "application/json", body: "{}" });
  });
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    const open = async (id) => {
      await p.click(`#stack-list .stackrow[data-id="${id}"]`, { button: "right" });
      await p.waitForSelector("#cardmenu button", { state: "visible", timeout: slow(5000) });
      // No two rows of one menu may share a label: the row's own text, without its help mark, arrow or flyout.
      const own = await p.evaluate(() => [...document.querySelectorAll("#cardmenu > button")]
        .map(b => b.firstChild.textContent.trim()));
      const twice = own.filter((l, i) => own.indexOf(l) !== i);
      if (twice.length) fail("restartMenu: the menu of " + id + " has two rows called " + JSON.stringify(twice) + ": " + JSON.stringify(own));
      if (process.env.RESTART_SHOT_DIR) await p.screenshot({ path: process.env.RESTART_SHOT_DIR + "/" + id + ".png" });
      return p.evaluate(() => [...document.querySelectorAll("#cardmenu button")].map(b => b.textContent));
    };
    const item = () => p.locator("#cardmenu button", { hasText: /^restart/ }).first();
    const dialogUp = () => p.locator("dialog[open] button", { hasText: "restart it" });

    // idle: offered, with no grey line under it, and one click posts with no question.
    let labels = await open("land-rs-idle");
    if (!labels.some(l => /^restart/.test(l))) {
      fail("restartMenu: no restart entry on a supervised card: " + JSON.stringify(labels));
    }
    if (await p.locator("#cardmenu .note").count()) fail("restartMenu: an entry still has a grey line under it.");
    if (!labels.some(l => /^handoff \/ clear context/.test(l)) || labels.some(l => /^new context/.test(l))) {
      fail("restartMenu: new context is not called handoff / clear context: " + JSON.stringify(labels));
    }
    // The never-used entries sit behind "more...", which opens them in place and keeps the menu open.
    if (labels.some(l => /^(attach terminal|open a shell here)/.test(l)) || !labels.some(l => /^more\.\.\./.test(l))) {
      fail("restartMenu: attach and shell are not behind more...: " + JSON.stringify(labels));
    }
    await p.locator("#cardmenu button", { hasText: /^more\.\.\./ }).click();
    if (process.env.RESTART_SHOT_DIR) await p.screenshot({ path: process.env.RESTART_SHOT_DIR + "/more.png" });
    const opened = await p.evaluate(() => ({ on: document.getElementById("cardmenu").classList.contains("on"),
      labels: [...document.querySelectorAll("#cardmenu > button")].map(b => b.textContent) }));
    if (!opened.on || opened.labels.some(l => /^more\.\.\./.test(l)) ||
        !opened.labels.some(l => /^attach terminal/.test(l)) || !opened.labels.some(l => /^open a shell here/.test(l))) {
      fail("restartMenu: more... did not open the hidden entries in place: " + JSON.stringify(opened));
    }
    await p.keyboard.press("Escape");
    labels = await open("land-rs-idle");
    if (process.env.RESTART_SHOT) await p.screenshot({ path: process.env.RESTART_SHOT });
    await item().click();
    await p.waitForTimeout(500);
    if (await dialogUp().count()) fail("restartMenu: an idle card asked before restarting.");
    if (posts.join() !== "/v1/tasks/land-rs-idle/restart POST") fail("restartMenu: idle restart did not POST once: " + JSON.stringify(posts));
    posts.length = 0;

    // working: asks, cancel posts nothing, confirming posts.
    await open("land-rs-work");
    await item().click();
    await dialogUp().waitFor({ state: "visible", timeout: slow(5000) })
      .catch(() => fail("restartMenu: a working card did not ask before restarting."));
    if (!/working right now/.test(await p.locator("dialog[open]").first().textContent())) fail("restartMenu: the question does not say it is working.");
    await p.locator("dialog[open] button", { hasText: "cancel" }).click();
    await p.waitForTimeout(300);
    if (posts.length) fail("restartMenu: cancelling still posted: " + JSON.stringify(posts));
    await open("land-rs-work");
    await item().click();
    await dialogUp().click();
    await p.waitForTimeout(500);
    if (posts.join() !== "/v1/tasks/land-rs-work/restart POST") fail("restartMenu: confirmed restart did not POST once: " + JSON.stringify(posts));
    posts.length = 0;

    // a refusal is a toast.
    refuse = true;
    await open("land-rs-idle");
    await item().click();
    await p.waitForFunction(() => /could not restart/.test(document.body.textContent), null, { timeout: slow(5000) })
      .catch(() => fail("restartMenu: a refused restart did not toast."));
    refuse = false;
    posts.length = 0;

    // parked: no process, so a card with no runner is not restarted: its menu offers resume, and that wakes the card.
    labels = await open("land-rs-park");
    if (labels.some(l => /^restart/.test(l)) || !labels.some(l => /^resume/.test(l))) fail("restartMenu: a parked card should offer resume and not restart: " + JSON.stringify(labels));
    // Two entries are called resume: the conversation picker (a submenu, marked with an arrow) and the parked
    // card's own, which has none. The sub-text that told them apart is gone.
    await p.locator("#cardmenu button", { hasText: /^resume/, hasNotText: /▸/ }).first().click();
    await p.waitForTimeout(500);
    if (posts.join() !== "/v1/tasks/land-rs-park/resume POST") fail("restartMenu: restarting a parked card did not resume it: " + JSON.stringify(posts));

    // paused: open() already refused a repeated label.
    // A paused card has no row on the stack (it sits behind the dead filter), so its board menu is asked for directly.
    await p.keyboard.press("Escape");
    await p.evaluate(() => cardMenu({ target: document.querySelector("#stack-list .stackrow"), clientX: 640, clientY: 120,
      preventDefault() {}, stopPropagation() {} }, "land-rs-paused"));
    await p.waitForSelector("#cardmenu button", { state: "visible", timeout: slow(5000) });
    const pausedOwn = await p.evaluate(() => [...document.querySelectorAll("#cardmenu > button")].map(b => b.firstChild.textContent.trim()));
    if (!pausedOwn.includes("resume")) fail("restartMenu: a paused card lost resume: " + JSON.stringify(pausedOwn));
    if (pausedOwn.some((l, i) => pausedOwn.indexOf(l) !== i)) fail("restartMenu: the paused card's menu has a repeated label: " + JSON.stringify(pausedOwn));
    if (process.env.RESTART_SHOT_DIR) await p.screenshot({ path: process.env.RESTART_SHOT_DIR + "/land-rs-paused.png" });
    await p.keyboard.press("Escape");
    // a card atrium cannot restart: left out.
    labels = await open("land-rs-bare");
    if (labels.some(l => /^restart/.test(l))) fail("restartMenu: restart is offered on a card atrium cannot restart.");
  } finally {
    await ctx.close();
    mock.tasksMode = was;
  }
  if (errors.length) fail("the restart menu page threw: " + errors.join(" | "));
}

// u-rooms-pill-setup-drift: the rooms pill warns when an attached room is set up wrongly, its tooltip names each room
// in one line, the rooms row says the same with a fix button for hooks and none for a build, and a room that did not
// answer is "not answering" and no warning. ROOMS_SETUP_SHOTS=<dir> saves the pill and the row.
async function roomsSetupSection(browser, base) {
  const shots = process.env.ROOMS_SETUP_SHOTS || "";
  const wasHub = mock.hubMode, wasSgg = mock.sggAttached;
  mock.hubMode = true;
  mock.sggAttached = true;
  const hooksIssue = { answering: true, hooks_missing: 3, hooks_stale: 1, issues: [{ kind: "hooks", text: "atrium claude hooks: 3 missing, 1 stale" }] };
  mock.ALPHA.setup = { answering: true, hooks_missing: 0, issues: [{ kind: "build", text: "runs a different atrium build than the hub (bbbbbbbb, hub aaaaaaaa)" }] };
  mock.SGG.setup = hooksIssue;
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  const errors = [];
  p.on("pageerror", e => errors.push(String(e)));
  const has = (label, ok, extra) => { if (!ok) fail("roomsSetup: " + label + (extra ? ": " + JSON.stringify(extra) : "")); };
  try {
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof hubRooms !== "undefined" && hubRooms.length === 2 &&
      !document.getElementById("rooms").hidden, null, { timeout: slow(15000) });
    const pill = () => p.evaluate(() => { const e = document.getElementById("rooms");
      return { warn: e.classList.contains("warn"), tip: e.dataset.tip }; });
    let s = await pill();
    has("the pill is not warning", s.warn, s);
    has("the tip does not name sgg's hooks", /sgg: atrium claude hooks: 3 missing, 1 stale/.test(s.tip), s);
    has("the tip does not name alpha's build", /alpha: runs a different atrium build/.test(s.tip), s);
    if (shots) {
      fs.mkdirSync(shots, { recursive: true });
      await p.screenshot({ path: path.join(shots, "pill-after.png"), clip: { x: 900, y: 0, width: 500, height: 60 } });
    }
    await p.evaluate(() => switchView("runners")); // the tab may sit in the `...` menu at this width
    await p.waitForSelector("#room-list .roomrow", { timeout: slow(10000) });
    const rows = () => p.evaluate(() => [...document.querySelectorAll("#room-list .roomrow")].map(r => ({
      name: r.querySelector(".roomname").textContent,
      lines: [...r.querySelectorAll(".setupline")].map(l => ({ kind: l.dataset.kind, text: l.textContent.replace(/\s+/g, " ").trim(),
        fix: !!l.querySelector("button.setupfix") })),
      text: r.textContent })));
    let r = await rows();
    const sgg = r.find(x => x.name === "sgg"), alpha = r.find(x => x.name === "alpha");
    has("sgg's row does not show the hooks problem with a fix", sgg && sgg.lines.length === 1 && sgg.lines[0].kind === "hooks" && sgg.lines[0].fix, sgg);
    has("alpha's row does not say the build differs, or it has a button", alpha && alpha.lines.length === 1 &&
      alpha.lines[0].kind === "build" && !alpha.lines[0].fix, alpha);
    if (shots) await (await p.$("#room-list")).screenshot({ path: path.join(shots, "row-after.png") });

    // The fix names the room and the pill clears with the row.
    mock.setupFixed.length = 0;
    await p.click('#room-list .setupfix[data-room="sgg"]');
    await p.waitForFunction(() => !document.querySelector('#room-list .setupfix'), null, { timeout: slow(10000) })
      .catch(() => fail("roomsSetup: the fix button stayed after the fix"));
    has("the fix did not post for sgg", mock.setupFixed.length === 1 && mock.setupFixed[0] === "sgg", mock.setupFixed);
    s = await pill();
    has("the tip still names sgg after the fix", !/sgg:/.test(s.tip), s);
    has("the pill stopped warning while alpha still has a build issue", s.warn, s);

    // A room that does not answer is not a setup problem.
    await p.evaluate(() => applyHubRooms([{ name: "alpha", setup: { answering: false, issues: [] } },
      { name: "sgg" }], null));
    await p.waitForTimeout(300);
    s = await pill();
    has("a room that did not answer made the pill warn", !s.warn && !/alpha:/.test(s.tip), s);
    await p.evaluate(() => renderRooms());
    await p.waitForFunction(() => /not answering/.test(document.getElementById("room-list").textContent), null, { timeout: slow(5000) })
      .catch(() => fail("roomsSetup: the row did not say not answering"));
  } finally {
    await ctx.close();
    delete mock.ALPHA.setup;
    delete mock.SGG.setup;
    mock.hubMode = wasHub;
    mock.sggAttached = wasSgg;
  }
  if (errors.length) fail("roomsSetup: the page threw: " + errors.join(" | "));
}

// u-034: the rooms dashboard's machine band and the all-rooms tile. Snapshots go through
// onRoomStats in the wire shape. Checks the two sparklines and the figures, a gap for a null
// sample, words and never a dash for a room with no machine or an empty one, cpu unavailable
// against not yet sampled, stale_since, the all-rooms rows, and no overflow at 390x844.
// ROOMS_MACHINE_SHOTS=<dir> saves the open menu at 1400x900.
async function roomsMachineSection(browser, base) {
  const skip = () => localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
  const shots = process.env.ROOMS_MACHINE_SHOTS || "";
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  const errors = [];
  p.on("pageerror", e => errors.push(String(e)));
  await p.addInitScript(skip);
  try {
    await p.goto(base + "/?demo=rooms", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof roomsDemoLive !== "undefined" && roomsDemoLive &&
      roomsDemoTicks > 0 && !document.getElementById("rooms").hidden, null, { timeout: slow(15000) });
    await p.evaluate(() => openRooms());
    await p.waitForTimeout(2300);
    await p.evaluate(() => clearInterval(roomsDemoTimer));
    await p.waitForTimeout(900);
    if (shots) {
      fs.mkdirSync(shots, { recursive: true });
      await p.screenshot({ path: path.join(shots, "u-034-after-1400.png") });
    }
    const feed = mode => p.evaluate(mode => {
      const now = Date.now();
      const iso = ms => new Date(ms).toISOString();
      const ramp = (a, b) => Array.from({ length: 60 }, (_, i) => Math.round(a + (b - a) * i / 59));
      const gappy = ramp(20, 60).map((v, i) => (i < 20 || (i > 30 && i < 34) ? null : v));
      const up = s => ({ started_at: iso(now - s * 1000) });
      const send = (room, machine, process) => onRoomStats({ v: 1, room, at: iso(now), process, machine });
      if (mode === "full") {
        send("sg3", { cpu_pct: 62, cpu_series_pct: gappy, mem_used_bytes: 21e9, mem_total_bytes: 32e9, mem_series_pct: ramp(50, 66) }, up(9999));
        send("sg4", { cpu_pct: 30, cpu_series_pct: ramp(10, 30), mem_used_bytes: 55e9, mem_total_bytes: 64e9, mem_series_pct: ramp(70, 86) }, up(9999));
        send("m1mini", { mem_used_bytes: 8e9, mem_total_bytes: 16e9, mem_series_pct: ramp(45, 50) }, up(9999));
      } else if (mode === "none") {
        for (const r of ["sg3", "sg4", "m1mini"]) send(r, undefined, up(9999));
      } else if (mode === "empty") {
        for (const r of ["sg3", "sg4"]) send(r, {}, up(9999));
        send("m1mini", undefined, up(9999));
      } else if (mode === "young") {
        send("sg3", { mem_used_bytes: 8e9, mem_total_bytes: 16e9 }, up(5));
        send("sg4", { cpu_pct: 30, cpu_series_pct: ramp(10, 30), mem_used_bytes: 55e9, mem_total_bytes: 64e9, mem_series_pct: ramp(70, 86), stale_since: iso(now - 600e3) }, up(9999));
        send("m1mini", { mem_used_bytes: 8e9, mem_total_bytes: 16e9 }, up(9999));
      }
      const m = document.getElementById("rooms-menu");
      const tile = n => m.querySelector(`.rtile[data-room="${n}"] .rd-band.machine`);
      const all = m.querySelector(".rtile.all");
      const rowsOf = t => [...t.querySelectorAll(".rd-rooms .rd-row")].map(r => r.textContent.replace(/\s+/g, " ").trim());
      return {
        hub: roomsHubBuild(),
        machine: Object.fromEntries(["sg3", "sg4", "m1mini"].map(n => [n, {
          text: tile(n).textContent.replace(/\s+/g, " ").trim(),
          sparks: [...tile(n).querySelectorAll("svg.rd-spark")].map(s => s.getAttribute("class").replace("rd-spark ", "")),
          polylines: [...tile(n).querySelectorAll("svg.rd-spark.cpu polyline")].length,
          dash: !!tile(n).querySelector(".rd-note .rd-dash"),
        }])),
        allRows: rowsOf(all),
        allDash: all.querySelector(".rd-rooms").textContent.includes("\u2014"),
        allH: all.getBoundingClientRect().height,
      };
    }, mode);
    const has = (label, ok, extra) => { if (!ok) fail("roomsMachine: " + label + (extra ? ": " + JSON.stringify(extra) : "")); };

    // All five fields: two sparklines, the figures, a gap where the samples are null.
    const full = await feed("full");
    if (process.env.SPARK_SHOTS) {
      fs.mkdirSync(process.env.SPARK_SHOTS, { recursive: true });
      for (const w of [140, 260]) {
        await p.evaluate(w => document.querySelectorAll("#rooms-menu .rtile .rd-band.machine").forEach(b => { b.style.width = w + "px"; }), w);
        const band = p.locator('#rooms-menu .rtile[data-room="sg3"] .rd-band.machine');
        await band.screenshot({ path: path.join(process.env.SPARK_SHOTS, "machine-band-" + w + ".png") });
        await p.evaluate(() => document.querySelectorAll("#rooms-menu .rtile .rd-band.machine").forEach(b => { b.style.width = ""; }));
      }
    }
    for (const w of [110, 140, 180, 260, 420]) {
      const bad = await p.evaluate(w => {
        const out = [];
        for (const band of document.querySelectorAll("#rooms-menu .rtile .rd-band.machine")) {
          band.style.width = w + "px";
          const edge = band.getBoundingClientRect().right;
          for (const sp of band.querySelectorAll(".rd-spark")) {
            const r = sp.getBoundingClientRect().right;
            if (r > edge + 0.5) out.push(sp.getAttribute("class") + " " + r.toFixed(1) + ">" + edge.toFixed(1));
          }
          for (const fig of band.querySelectorAll(".rd-rate > span")) {
            if (fig.scrollWidth > fig.clientWidth + 1 || fig.getBoundingClientRect().right > edge + 0.5) out.push("figure clipped: " + fig.textContent.trim());
          }
          for (const row of band.querySelectorAll(".rd-rate")) {
            if (row.scrollWidth > row.clientWidth + 1) out.push("row " + row.textContent.trim().slice(0, 12) + " " + row.scrollWidth + ">" + row.clientWidth);
          }
          band.style.width = "";
        }
        return out;
      }, w);
      if (bad.length) fail("roomsMachine: a sparkline runs past its box at " + w + "px: " + bad.join("; "));
    }
    const g3 = full.machine.sg3;
    has("sg3 did not draw a cpu and a mem sparkline", g3.sparks.join(",") === "cpu,mem", g3);
    has("sg3 cpu and mem figures", /cpu 62%/.test(g3.text) && /mem 66%/.test(g3.text) && /21\/32 GB used/.test(g3.text), g3.text);
    has("a null sample in the cpu series was not a gap (expected two runs)", g3.polylines === 2, g3);
    const gapPoints = await p.evaluate(() => [...document.querySelectorAll('.rtile[data-room="sg3"] svg.rd-spark.cpu polyline')]
      .map(l => l.getAttribute("points").split(" ").length));
    has("the gap runs do not hold the samples they should", gapPoints[0] === 11 && gapPoints[1] === 26, gapPoints);
    has("a plain null-free series is one polyline", await p.evaluate(() =>
      document.querySelectorAll('.rtile[data-room="sg4"] svg.rd-spark.cpu polyline').length) === 1);
    // macOS shape: memory, no CPU, up for a while: unavailable, in words, no dash.
    const mini = full.machine.m1mini;
    has("m1mini cpu was not said to be unavailable", /cpu: not available on this room/.test(mini.text) && /mem 50%/.test(mini.text) && /8\/16 GB used/.test(mini.text), mini.text);
    has("m1mini drew a cpu sparkline", mini.sparks.join(",") === "mem", mini);
    // All-rooms rows: busiest and tightest, from rooms that report. The hub build is unknown in
    // this fixture only if roomsHubBuild is empty, so the rows are checked by content.
    has("all-rooms busiest row", full.allRows.some(r => /busiest sg3 62%/.test(r)), full.allRows);
    // The demo's rooms are not all on the hub's build, so the warning row is drawn and the memory
    // line gives way to it: three rows and never four.
    has("all-rooms build warning row", full.allRows.some(r => /1 not the hub's build/.test(r)), full.allRows);
    has("all-rooms band grew past three rows", full.allRows.length === 3 && !full.allRows.some(r => /tightest/.test(r)), full.allRows);
    has("all-rooms rows hold a dash", !full.allDash, full.allRows);
    const onHub = await p.evaluate(() => {
      const h = hubRooms.map(r => Object.assign({}, r, { version: roomsHubBuild() }));
      const t = document.createElement("template");
      t.innerHTML = allRoomsTileHTML(h, false).trim();
      return [...t.content.querySelectorAll(".rd-rooms .rd-row")].map(r => r.textContent.replace(/\s+/g, " ").trim());
    });
    has("with every room on the hub's build the rows are live, busiest and tightest", onHub.length === 3 &&
      /busiest sg3 62%/.test(onHub[1]) && /tightest sg4 86% mem/.test(onHub[2]), onHub);

    // No machine at all: words, no dash, and the all-rooms tile drops its busiest and tightest rows.
    const none = await feed("none");
    for (const n of ["sg3", "sg4", "m1mini"]) {
      has(n + " with no machine did not say the build does not report it", /cpu and memory: this room's build does not report them/.test(none.machine[n].text), none.machine[n].text);
      has(n + " with no machine drew a dash or a sparkline", !none.machine[n].dash && !none.machine[n].sparks.length, none.machine[n]);
    }
    has("all-rooms drew busiest or tightest with no cpu or memory from any room", !none.allRows.some(r => /busiest|tightest/.test(r)), none.allRows);
    has("all-rooms tile holds a dash with no machine numbers", !none.allDash, none.allRows);

    // An empty machine object: waiting for the first reading.
    const empty = await feed("empty");
    has("an empty machine did not say it waits for the first reading", /waiting for the first reading/.test(empty.machine.sg3.text) && !empty.machine.sg3.dash, empty.machine.sg3);
    has("a room with no machine beside an empty one lost its words", /does not report/.test(empty.machine.m1mini.text));

    // Young room: cpu is still due. Stale sample: the time, and dimmed figures.
    const young = await feed("young");
    has("a room started seconds ago said cpu is unavailable", /cpu: waiting for the second reading/.test(young.machine.sg3.text), young.machine.sg3.text);
    has("stale_since was not drawn as a time", /stale since \d\d:\d\d/.test(young.machine.sg4.text), young.machine.sg4.text);
    has("stale figures are not dimmed", await p.evaluate(() =>
      Number(getComputedStyle(document.querySelector('.rtile[data-room="sg4"] .rd-stale')).opacity) < 0.9));
    has("a stale room was named busiest", !young.allRows.some(r => /busiest sg4/.test(r)), young.allRows);

    // The hub build unknown: the build row is dropped and nothing draws a dash. Known: it is a number.
    const hub = await p.evaluate(() => {
      const all = document.querySelector("#rooms-menu .rtile.all .rd-rooms").textContent;
      return { hub: roomsHubBuild(), row: /not the hub's build/.test(all), dash: all.includes("\u2014") };
    });
    has("the build row and the dash", hub.hub ? hub.row && !hub.dash : !hub.row && !hub.dash, hub);
    has("hub build and the none-fixture rows", none.allRows.length === 2 && /live/.test(none.allRows[0]) && /not the hub's build/.test(none.allRows[1]), none.allRows);
    const unknown = await p.evaluate(() => {
      const was = window.roomsDemoLive;
      roomsDemoLive = false;
      try {
        const h = allRoomsTileHTML(hubRooms, false);
        return { hub: roomsHubBuild(), row: /not the hub's build/.test(h), dash: /rd-rooms[\s\S]*\u2014/.test(h) };
      } finally { roomsDemoLive = was; }
    });
    has("with the hub build unknown the row was drawn or a dash showed", unknown.hub === "" && !unknown.row && !unknown.dash, unknown);

    // The all-rooms tile is no taller with the rows than without them.
    const tall = { full: full.allH, none: none.allH };
    has("the all-rooms tile grew with the new rows", tall.full <= tall.none + 8, tall);

    await feed("full");

    // Phone: the tiles stack and nothing is wider than the window.
    await p.setViewportSize({ width: 390, height: 844 });
    await p.evaluate(() => { const m = document.getElementById("rooms-menu"); m.hidden = true; openRooms(); });
    await feed("full");
    const phone = await p.evaluate(() => {
      const m = document.getElementById("rooms-menu");
      const wide = [...m.querySelectorAll(".rtile *")].filter(e => e.getBoundingClientRect().right > 390.5)
        .map(e => e.className && e.className.baseVal === undefined ? e.className : e.tagName).slice(0, 5);
      return { right: m.getBoundingClientRect().right, sw: m.scrollWidth, cw: m.clientWidth, wide,
        doc: document.documentElement.scrollWidth,
        sparks: m.querySelectorAll('.rtile[data-room="sg3"] svg.rd-spark').length };
    });
    has("at 390x844 the rooms menu overflows", phone.right <= 390 && phone.sw <= phone.cw + 1 && !phone.wide.length && phone.doc <= 390 && phone.sparks >= 2, phone);
    await feed("none");
    const phone2 = await p.evaluate(() => {
      const m = document.getElementById("rooms-menu");
      return { sw: m.scrollWidth, cw: m.clientWidth, doc: document.documentElement.scrollWidth };
    });
    has("at 390x844 the words overflow the menu", phone2.sw <= phone2.cw + 1 && phone2.doc <= 390, phone2);
    if (errors.length) fail("roomsMachine: the page threw: " + errors.join(" | "));
  } finally {
    await ctx.close();
  }
}

// ── a hub that is down is noticed once, and the board holds its other pollers ──
async function hubGateSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  let down = false, bad503 = 0, healthAsks = 0, readsWhileDown = 0;
  const logged = [];
  await p.route(/\/(v1|_hub)\//, async r => {
    const u = new URL(r.request().url());
    if (u.pathname === "/v1/health") {
      healthAsks++;
      if (down) return r.fulfill({ status: 503, body: "down" });
      return r.continue();
    }
    if (down && r.request().method() === "GET" && !/\/v1\/(events|stream)|\/attach/.test(u.pathname)) return r.fulfill({ status: 503, body: "hub is restarting" });
    return r.continue();
  });
  p.on("response", res => { if (res.status() === 503 && !/\/v1\/health/.test(res.url())) { bad503++; if (process.env.GATE_DEBUG) console.log("503", res.url()); } });
  p.on("console", m => { if (m.type() === "error" && /503/.test(m.text())) logged.push(m.text()); });
  try {
    await p.clock.install();
    await p.goto(base + "/raw", { waitUntil: "load" });
    await fakePause(p);
    await fakeTick(p, 1500);
    // The auth fields live in a form, so the browser stops warning about them.
    const loose = await p.evaluate(() => ["auth-pass", "auth-secret"].filter(id => { const e = document.getElementById(id); return !e || !e.form; }));
    if (loose.length) fail("hubGate: password fields outside a form: " + loose.join(", "));
    // The hub goes down, then a burst of every kind of poller reads at once.
    down = true; bad503 = 0; logged.length = 0;
    // Pollers fire on their own timers, so the first one finds out and the rest are behind it.
    await p.evaluate(() => { api("/v1/tasks").catch(() => {}); });
    await fakeWait(p, () => !!hubGateP, null, 5000);
    await p.evaluate(() => { for (const u of ["/v1/settings", "/v1/harnesses", "/v1/providers", "/v1/permissions", "/v1/themes", "/v1/shares"]) api(u).catch(() => {}); });
    await fakeTick(p, 300);
    const first = bad503;
    await p.evaluate(() => { for (const u of ["/v1/tasks", "/v1/settings", "/v1/harnesses", "/v1/providers", "/v1/prs", "/v1/usage/turn-limits"]) api(u).catch(() => {}); });
    await fakeTick(p, 2500);
    if (bad503 !== first) fail("hubGate: pollers kept asking a hub known to be down (" + first + " then " + bad503 + ")");
    if (first > 1) fail("hubGate: the first burst logged " + first + " failures, wanted 1");
    // One probe answers, and the held reads go out and finish.
    down = false;
    const done = await fakeRun(p, (_, done) => { api("/v1/settings").then(() => done(true), e => done(String(e && e.message))); });
    if (done !== true) fail("hubGate: reads did not resume once the hub answered: " + done);
  } finally {
    await ctx.close();
  }
  if (!bad) console.log("hubGate ok");
}

// The recogniser editor's default repo: read into the box from the row, and saved back with what was typed.
async function recogniserRepoSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const errors = [];
  const json = (route, status, body) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
  const zd = { id: "zd", label: "zendesk ticket", pattern: "^https://z/(?P<num>\\d+)$", rank: 50, enabled: true, room: "sgg",
    kind: "zendesk", title: "zendesk-{num}", tags: "zendesk", cwd: "", prompt: "read it", branch: "zendesk-{num}",
    window: "", theme: "", fetch: "", fetch_args: [], fetch_cwd: "", default_repo: "github.com/openziti/ziti" };
  const puts = [];
  await ctx.route(/\/v1\/recognisers(\/|\?|$)/, async route => {
    const req = route.request();
    if (req.method() === "PUT") { puts.push(JSON.parse(req.postData() || "{}")); return json(route, 200, puts[puts.length - 1]); }
    return json(route, 200, { recognisers: [zd] });
  });
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(e.message));
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof editRecogniser === "function" && typeof renderRecognisers === "function", null, { timeout: slow(15000) });
    await p.evaluate(async () => { await renderRecognisers(); editRecogniser("zd", "sgg"); });
    await p.waitForFunction(() => document.getElementById("recogniser").open, null, { timeout: slow(5000) })
      .catch(() => fail("recogniserRepo: the editor did not open"));
    if (await p.inputValue("#r-default-repo") !== "github.com/openziti/ziti") fail("recogniserRepo: the default repo was not read in");
    await p.fill("#r-default-repo", "github.com/openziti/zrok");
    await p.evaluate(() => saveRecogniser());
    await p.waitForFunction(() => !document.getElementById("recogniser").open, null, { timeout: slow(5000) })
      .catch(() => fail("recogniserRepo: the save did not close the editor"));
    if (!puts.length || puts[0].default_repo !== "github.com/openziti/zrok") fail("recogniserRepo: saved " + JSON.stringify(puts[0]));
    if (errors.length) fail("recogniserRepo: page errors: " + errors.join(" | "));
  } finally { await ctx.close(); }
  if (!bad) console.log("recogniserRepo ok");
}

// roomsMenuScroll: the rooms dropdown, scrolled to its end, shows the last tile whole above the window's bottom edge.
async function roomsMenuScrollSection(browser, base) {
  const shots = process.env.ROOMS_MENU_SHOTS || "";
  const skip = () => localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
  for (const [name, vw, vh] of [["desktop", 1280, 700], ["phone", 400, 700]]) {
    const ctx = await browser.newContext({ viewport: { width: vw, height: vh } });
    const p = await ctx.newPage();
    const errors = [];
    p.on("pageerror", e => errors.push(String(e)));
    await p.addInitScript(skip);
    try {
      await p.goto(base + "/?demo=rooms", { waitUntil: "domcontentloaded" });
      await p.waitForFunction(() => typeof roomsDemoLive !== "undefined" && roomsDemoLive &&
        roomsDemoTicks > 0 && !document.getElementById("rooms").hidden, null, { timeout: slow(15000) });
      await p.evaluate(() => clearInterval(roomsDemoTimer));
      await p.evaluate(() => { window.fullRooms = hubRooms; hubRooms = hubRooms.slice(0, 1); openRooms(); });
      await p.waitForFunction(() => { const mn = document.getElementById("rooms-menu"); return !mn.hidden && !!mn.style.maxHeight && !!mn.style.top; }, null, { timeout: slow(5000) });
      const m = await p.evaluate(() => {
        const menu = document.getElementById("rooms-menu");
        const more = [];
        for (let i = 0; i < 6; i++) more.push(...window.fullRooms.map(r => ({ ...r, name: r.name + "-" + i })));
        hubRooms = window.fullRooms.concat(more);
        refreshRoomsMenu();
        menu.scrollTop = menu.scrollHeight;
        const mr = menu.getBoundingClientRect();
        const last = [...menu.children].pop().getBoundingClientRect();
        return { scrolls: menu.scrollHeight > menu.clientHeight + 1, atEnd: menu.scrollTop + menu.clientHeight >= menu.scrollHeight - 1,
          menuBottom: mr.bottom, lastBottom: last.bottom, vh: innerHeight };
      });
      if (shots) {
        fs.mkdirSync(shots, { recursive: true });
        await p.screenshot({ path: path.join(shots, "rooms-menu-" + name + ".png") });
      }
      const tag = "roomsMenuScroll " + name + ": ";
      if (!m.scrolls) fail(tag + "the menu did not need to scroll, so it proves nothing: " + JSON.stringify(m));
      if (!m.atEnd) fail(tag + "the menu did not scroll to its end: " + JSON.stringify(m));
      if (m.lastBottom > m.menuBottom + 0.5 || m.menuBottom > m.vh - 4) fail(tag + "the last tile is cut off at the end of the scroll: " + JSON.stringify(m));
      if (errors.length) fail(tag + "page errors: " + errors.join(" | "));
    } finally { await ctx.close(); }
  }
}

const units = [
  { name: "restartGate", run: ({ browser, base }) => restartGateSection(browser, base) },
  { name: "restartStays", run: ({ browser, base }) => restartStaysSection(browser, base) },
  { name: "atriumDown", run: ({ browser, base }) => atriumDownSection(browser, base) },
  { name: "roomsDash", run: ({ browser, base }) => roomsDashSection(browser, base) },
  { name: "transportChips", run: ({ browser, base }) => transportChipsSection(browser, base) },
  { name: "pathBadges", run: ({ browser, base }) => pathBadgesSection(browser, base) },
  { name: "transportToggle", run: ({ browser, base }) => transportToggleSection(browser, base) },
  { name: "hubTransports", run: ({ browser, base }) => hubTransportsSection(browser, base) },
  { name: "zitiLogo", run: ({ browser, base }) => zitiLogoSection(browser, base) },
  { name: "zrokLogo", run: ({ browser, base }) => zrokLogoSection(browser, base) },
  { name: "pollsGone", run: ({ browser, base }) => pollsGoneSection(browser, base) },
  { name: "findingsProbe", run: ({ browser, base }) => findingsProbeSection(browser, base) },
  { name: "roomsMachine", run: ({ browser, base }) => roomsMachineSection(browser, base) },
  { name: "loginNeeded", run: ({ browser, base }) => loginNeededSection(browser, base) },
  { name: "restartMenu", run: ({ browser, base }) => restartMenuSection(browser, base) },
  { name: "roomsSetup", run: ({ browser, base }) => roomsSetupSection(browser, base) },
  { name: "hubGate", run: ({ browser, base }) => hubGateSection(browser, base) },
  { name: "recogniserRepo", run: ({ browser, base }) => recogniserRepoSection(browser, base) },
  { name: "roomsMenuScroll", run: ({ browser, base }) => roomsMenuScrollSection(browser, base) },
];
const sections = { restartGate: restartGateSection, restartStays: restartStaysSection, atriumDown: atriumDownSection, roomsDash: roomsDashSection, transportChips: transportChipsSection, pathBadges: pathBadgesSection, transportToggle: transportToggleSection, hubTransports: hubTransportsSection, zitiLogo: zitiLogoSection, zrokLogo: zrokLogoSection, pollsGone: pollsGoneSection, findingsProbe: findingsProbeSection, roomsMachine: roomsMachineSection, loginNeeded: loginNeededSection, restartMenu: restartMenuSection, roomsSetup: roomsSetupSection, hubGate: hubGateSection, recogniserRepo: recogniserRepoSection, roomsMenuScroll: roomsMenuScrollSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
