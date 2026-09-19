// The board, in a real browser, must not blank on a hung fetch.
//
// The unit test (test-refresh-storm.js) proves the debounce, single-flight,
// abort, cap and watchdog as logic. This proves the two things only a browser
// can: that the live-style board actually PAINTS its lists from the daemon's
// answers, and that a fetch which hangs forever does not leave the board frozen
// on a blank pane. The watchdog is what makes the second true, so this is its
// end-to-end check.
//
// It needs no atrium and touches no atrium: it serves the concatenated board
// from board-source.js off an ephemeral localhost port, answers every endpoint
// with a mock, and drives a headless Chromium. If Playwright or its browser is
// not installed it SKIPS rather than fails, the same way check-board.sh treats
// node itself: this is a check, not a build step.

const http = require("http");
const { wholeBoard } = require("./board-source.js");

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

// The mocked daemon. Its answers are mutable so the test can make /v1/tasks hang
// and then recover with a different card.
// status needs-input so the card lands in the stack view's default filter,
// which opens showing that column. See stack.js stackShow().
const T1 = {
  id: "t1", status: "needs-input", display_title: "first card", runner: "claude",
  rank: 1, worktree: "/tmp/one", why: "", idle_seconds: 0, wait_seconds: 0,
  created_at: "2026-09-19T12:00:00Z", last_activity_at: "2026-09-19T12:00:00Z",
  tags: [], supervised: false, offline: false, pinned: false, auto_approve: false
};
const T2 = Object.assign({}, T1, { id: "t2", display_title: "second card" });
const HIST = {
  id: "h1", display_title: "old run", runner: "claude", status: "done",
  created_at: "2026-09-18T09:00:00Z", why: "did a thing", recap: "",
  worktree: "/tmp/old", archived_at: ""
};

let tasksMode = "first";   // first | hang | second
const hungResponses = [];   // held-open sockets, ended on teardown
const openStreams = [];

const HTML = wholeBoard();

function sendJSON(res, obj) {
  const body = JSON.stringify(obj);
  res.writeHead(200, { "Content-Type": "application/json" });
  res.end(body);
}

const server = http.createServer((req, res) => {
  const url = req.url.split("?")[0];

  if (url === "/" || url === "/index.html") {
    res.writeHead(200, { "Content-Type": "text/html" });
    res.end(HTML);
    return;
  }
  if (url === "/v1/tasks") {
    if (tasksMode === "hang") { hungResponses.push(res); return; }  // never answer
    sendJSON(res, { tasks: [tasksMode === "second" ? T2 : T1] });
    return;
  }
  if (url === "/v1/history") { sendJSON(res, { tasks: [HIST], total: 1 }); return; }
  if (url === "/v1/waiting") { sendJSON(res, { tasks: [] }); return; }
  if (url === "/v1/permissions") { sendJSON(res, { permissions: [] }); return; }
  if (url === "/v1/shares") { sendJSON(res, { shares: [] }); return; }
  if (url === "/v1/rooms") { sendJSON(res, { rooms: [] }); return; }
  if (url === "/v1/health") {
    sendJSON(res, { build: "test", settling: false, halted: false }); return;
  }
  if (url === "/v1/settings") {
    sendJSON(res, { global_auto: false, global_auto_seconds: 0 }); return;
  }
  if (url === "/v1/themes") { sendJSON(res, { themes: [] }); return; }
  if (url === "/v1/events") {
    // An event stream that stays open and says nothing. The board polls for its
    // data, so an idle stream is enough to keep it out of the reconnect state.
    res.writeHead(200, {
      "Content-Type": "text/event-stream", "Cache-Control": "no-cache",
      "Connection": "keep-alive"
    });
    res.write(": open\n\n");
    openStreams.push(res);
    return;
  }
  // A hub probe that says "not a hub", so the board runs as a plain daemon.
  if (url === "/_hub/rooms" || url === "/_hub/health" || url === "/_hub/inventory") {
    res.writeHead(404); res.end("not a hub"); return;
  }
  // Everything else (sw.js, icons, favicon): a clean 404.
  res.writeHead(404); res.end("");
});

let bad = 0;
function fail(msg) { console.error("FAIL: " + msg); bad++; }

async function main() {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const base = "http://127.0.0.1:" + server.address().port;

  const browser = await chromium.launch();
  const page = await browser.newPage();
  const consoleErrors = [];
  page.on("pageerror", e => consoleErrors.push(String(e)));
  if (process.env.DEBUG_HEADLESS) {
    page.on("console", m => console.error("[console] " + m.type() + ": " + m.text()));
    page.on("requestfailed", r =>
      console.error("[reqfail] " + r.url() + " " + (r.failure() || {}).errorText));
  }

  // Shorten the watchdog so the unwedge is provable in seconds, not the 30 a
  // real board waits. A plain board never sets this.
  await page.addInitScript(() => { window.__atriumRunTimeout = 2000; });

  try {
    await page.goto(base, { waitUntil: "domcontentloaded" });

    // ── the task list paints (not blank) ──────────────────────────────────
    // The board opens on the stack view, which paints from /v1/tasks.
    await page.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    const firstId = await page.getAttribute("#stack-list .stackrow", "data-id");
    if (firstId !== "t1") {
      fail("the stack painted a row with data-id " + firstId + ", not the card " +
        "the daemon returned. The board is not rendering the task list.");
    }

    // The guest page must not be showing: a 200 on /v1/tasks is a board, not a
    // one-terminal link.
    const guest = await page.evaluate(() =>
      document.body.classList.contains("guestonly"));
    if (guest) fail("the board fell into guest mode against a 200 /v1/tasks.");

    // ── the history view paints rows ──────────────────────────────────────
    await page.click('.tab[data-view="history"]');
    await page.waitForSelector("#history-list .row.line", { timeout: 15000 });
    const histRows = await page.locator("#history-list .row.line").count();
    if (histRows < 1) fail("the history view painted no rows from /v1/history.");

    // ── a hung fetch does not blank the board, and a later refresh repaints ─
    // Back on the stack, make /v1/tasks hang, then drive one refresh through the
    // single-flight guard. The pass wedges on the hung fetch.
    await page.click('.tab[data-view="stack"]');
    await page.waitForSelector('#stack-list .stackrow[data-id="t1"]', { timeout: 15000 });
    tasksMode = "hang";
    await page.evaluate(() => runRefresh());

    // While it hangs, the board keeps its last paint rather than clearing.
    await page.waitForTimeout(800);
    const duringHang = await page.locator("#stack-list .stackrow").count();
    if (duringHang < 1) {
      fail("the board blanked its task list while a fetch was in flight. A hung " +
        "refresh must leave the last paint standing, not clear it.");
    }

    // Recover the endpoint with a DIFFERENT card. The watchdog must free the
    // single-flight latch past __atriumRunTimeout and the requeued pass must
    // repaint with the new data. If the board were wedged, t2 never appears.
    tasksMode = "second";
    if (process.env.DEBUG_HEADLESS) {
      for (let i = 0; i < 12; i++) {
        await page.waitForTimeout(500);
        const st = await page.evaluate(() => ({
          ids: [...document.querySelectorAll("#stack-list .stackrow")]
            .map(e => e.dataset.id),
          inFlight: typeof refreshInFlight !== "undefined" ? refreshInFlight : "?",
          dirty: typeof refreshDirty !== "undefined" ? refreshDirty : "?",
          streak: typeof apiFailStreak !== "undefined" ? apiFailStreak : "?"
        }));
        console.error("[hang " + (i * 500) + "ms] " + JSON.stringify(st));
      }
    }
    await page.waitForSelector('#stack-list .stackrow[data-id="t2"]', { timeout: 15000 });

    if (consoleErrors.length) {
      fail("the page threw uncaught errors: " + consoleErrors.join(" | "));
    }
  } catch (e) {
    fail("the headless run threw: " + (e && e.message ? e.message : e));
    if (process.env.DEBUG_HEADLESS) {
      try {
        const diag = await page.evaluate(() => ({
          bodyClass: document.body.className,
          stackHidden: document.getElementById("stack") &&
            document.getElementById("stack").hidden,
          stackListHTML: (document.getElementById("stack-list") || {}).innerHTML,
          onView: (document.querySelector(".tab.on") || {}).dataset,
          hasRunRefresh: typeof runRefresh,
          lastTasks: typeof lastTasks !== "undefined" ? lastTasks : "undef"
        }));
        console.error("[diag] " + JSON.stringify(diag).slice(0, 2000));
      } catch (d) { console.error("[diag failed] " + d.message); }
    }
  } finally {
    hungResponses.forEach(r => { try { r.destroy(); } catch (e) {} });
    openStreams.forEach(r => { try { r.destroy(); } catch (e) {} });
    await browser.close();
    await new Promise(r => server.close(r));
  }

  if (bad) process.exit(1);
  console.log("the board paints its lists, and a hung fetch does not blank it.");
}

main().catch(e => { console.error(e); process.exit(1); });
