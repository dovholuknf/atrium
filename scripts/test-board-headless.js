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
// The card a popped-out (solo) window is opened onto. Supervised, so it reads
// like a real live session rather than a dead one.
const SOLO = Object.assign({}, T1, {
  id: "s1", display_title: "solo card", supervised: true
});
const HIST = {
  id: "h1", display_title: "old run", runner: "claude", status: "done",
  created_at: "2026-09-18T09:00:00Z", why: "did a thing", recap: "",
  worktree: "/tmp/old", archived_at: ""
};

let tasksMode = "first";   // first | hang | second
// How the mocked hub answers a card-scoped poll (GET /v1/tasks/<id>), which is
// what a popped-out window opens on. `noroom` is the transient hub-restart state
// (503, "no room is attached"), `gone` is a genuine missing card (404), and `ok`
// returns the card.
let soloMode = "ok";       // ok | noroom | gone
// Whether the mock answers the `/_hub/*` probes as a hub. Off by default so the
// board runs as a plain daemon for the tests above; the room-picker test turns
// it on and loads a fresh page. `sggAttached` is the one room that flips from
// disconnected to live under the open dropdown.
let hubMode = false;
let sggAttached = false;
const ALPHA = { name: "alpha", host: "alpha-host" };
const SGG = { name: "sgg", host: "sgg-host" };

// The board skin follows the room-picker scope: the ALL view (no X-Atrium-Room
// header) wears the HUB's own skin, and each room wears its own. `skinFor` is
// the mocked hub-plus-rooms state, keyed by scope with "" for the ALL/hub view.
// A skin-only save in the ALL view lands on the hub (skinFor[""]); one made
// while scoped to a room lands on that room, and never on the hub. The list is
// the same across scopes; only the selected one differs.
const SKINS = ["harbour", "moss", "noir", "ember", "vapor"];
let skinFor = { "": "noir", alpha: "moss", sgg: "ember" };
function resetSkins() { skinFor = { "": "noir", alpha: "moss", sgg: "ember" }; }
function settingsBody(room) {
  const skin = skinFor[room] != null ? skinFor[room] : skinFor[""];
  return { global_auto: false, global_auto_seconds: 0, board_skin: skin, board_skins: SKINS };
}
const hungResponses = [];   // held-open sockets, ended on teardown
const openStreams = [];
const hubStreams = [];       // hub event streams, used to push a `rooms` event

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
  // A card-scoped poll, the one a popped-out window opens on. The hub answers a
  // no-room restart with 503 and a genuine missing card with 404, and the solo
  // window has to tell those apart. Placed before the list route, which is the
  // exact path "/v1/tasks" with no trailing id.
  if (url.startsWith("/v1/tasks/")) {
    const id = url.slice("/v1/tasks/".length);
    if (soloMode === "noroom") {
      res.writeHead(503, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "no room is attached to this hub. the hub " +
        "serves the board and holds nothing, so until a room connects there is " +
        "nothing to show. run `atrium2 join` on the machine your agents are on." }));
      return;
    }
    if (soloMode === "gone") {
      res.writeHead(404, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "no such card" }));
      return;
    }
    sendJSON(res, id === "s1" ? SOLO : (id === "t2" ? T2 : T1));
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
    // The scope is the room header the board's fetch wrapper adds, or "" for the
    // ALL view. The hub answers the ALL view with its own skin and a room-scoped
    // request with that room's, which is the whole of the feature.
    const room = req.headers["x-atrium-room"] || "";
    if (req.method === "POST" || req.method === "PUT") {
      let raw = "";
      req.on("data", c => { raw += c; });
      req.on("end", () => {
        let body = {};
        try { body = JSON.parse(raw || "{}"); } catch (e) {}
        const keys = Object.keys(body);
        // A skin-only save lands on the current scope: the hub for ALL, the room
        // when scoped. Anything else in the ALL view still needs a room (409),
        // which is the refusal the scoped skin does NOT get any more.
        if (keys.length === 1 && keys[0] === "board_skin") {
          skinFor[room] = body.board_skin;
          sendJSON(res, settingsBody(room));
          return;
        }
        if (!room) {
          res.writeHead(409, { "Content-Type": "application/json" });
          res.end(JSON.stringify({ error: "pick a room first" }));
          return;
        }
        sendJSON(res, settingsBody(room));
      });
      return;
    }
    sendJSON(res, settingsBody(room));
    return;
  }
  if (url === "/v1/themes") { sendJSON(res, { themes: [] }); return; }
  // The plain-daemon stream and the hub's own spelling of it. On a hub the board
  // opens `/v1/events/hub`, and the room-picker test writes a `rooms` event onto
  // that one to make a room attach while the dropdown is open.
  if (url === "/v1/events" || url === "/v1/events/hub") {
    // An event stream that stays open and says nothing. The board polls for its
    // data, so an idle stream is enough to keep it out of the reconnect state.
    res.writeHead(200, {
      "Content-Type": "text/event-stream", "Cache-Control": "no-cache",
      "Connection": "keep-alive"
    });
    res.write(": open\n\n");
    openStreams.push(res);
    if (url === "/v1/events/hub") hubStreams.push(res);
    return;
  }
  // The hub probes. Off by default (a plain daemon 404s them); the room-picker
  // test turns `hubMode` on so the board runs as a hub with two rooms.
  if (url === "/_hub/rooms") {
    if (!hubMode) { res.writeHead(404); res.end("not a hub"); return; }
    sendJSON(res, { rooms: sggAttached ? [ALPHA, SGG] : [ALPHA] });
    return;
  }
  if (url === "/_hub/inventory") {
    if (!hubMode) { res.writeHead(404); res.end("not a hub"); return; }
    const alpha = Object.assign({ transport: "direct", attached: true,
      first_seen: "2026-09-19T06:00:00Z", last_seen: "2026-09-19T12:00:00Z" }, ALPHA);
    // sgg has dialled in before, so it shows disconnected until it attaches: the
    // picker only lists an ever-connected room, never one that is only inventory.
    const sgg = Object.assign({ transport: "direct", attached: sggAttached,
      first_seen: "2026-09-19T06:00:00Z", last_seen: "2026-09-19T11:00:00Z" }, SGG);
    sendJSON(res, { rooms: [alpha, sgg] });
    return;
  }
  if (url === "/_hub/health") { res.writeHead(404); res.end("not a hub"); return; }
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

    // ── a live popped-out window is re-heard on the board's roll call ───────
    // The board asks `solo-who` on every poll now, not just at boot. A window
    // still open answers and re-stamps its claim, so a claim that lapsed while
    // the window's own poll was stalled through a hub restart is refreshed by
    // the board rather than left to expire. That is what keeps `poppedOut` true
    // and stops the pane taking the terminal back into a second view. Driven on
    // the board `page` with a stand-in solo window on the shared bus: pages here
    // are separate browser contexts, so this channel reaches only this board.
    await page.evaluate(() => {
      window.__fakeSolo = new BroadcastChannel("atrium-solo");
      window.__fakeSoloAnswers = true;
      window.__fakeSolo.onmessage = e => {
        const m = e.data || {};
        if (m.type === "solo-who" && window.__fakeSoloAnswers) {
          window.__fakeSolo.postMessage({ type: "solo-claim", task: "s1" });
        }
      };
      // The claim it posts on the way in, the way a solo window claims first.
      window.__fakeSolo.postMessage({ type: "solo-claim", task: "s1" });
    });
    // The board heard the claim: the card reads as popped out.
    await page.waitForFunction(() => poppedOut("s1"), { timeout: 15000 });

    // The window's OWN poll lapses past soloClaimFor without re-claiming, which
    // a reconnect/backoff through a hub restart causes. Simulated by ageing the
    // stored claim past the TTL, which is what the wall clock would do.
    await page.evaluate(() => soloHeld.set("s1", Date.now() - 20000));

    // The board's roll call, on its regular refresh. The still-open window
    // answers and its claim is re-stamped, so the card stays popped out. Every
    // attach path gates on `poppedOut`, so a card that stays popped out is a
    // pane that does NOT double-open its terminal.
    await page.evaluate(() => runRefresh());
    let reheard = false;
    try {
      await page.waitForFunction(
        () => poppedOut("s1") && (Date.now() - (soloHeld.get("s1") || 0) < 5000),
        { timeout: 15000 });
      reheard = true;
    } catch (e) {}
    if (!reheard) {
      fail("the board's roll call did not re-hear a live popped-out window: its " +
        "claim lapsed and poppedOut('s1') went false, so the pane would " +
        "double-open the terminal.");
    }

    // ── a window that truly went away frees its card ────────────────────────
    // The heartbeat MUST still expire. A genuinely-closed window stops answering
    // the roll call, so its claim ages out and the card is freed rather than
    // held forever. Silence the stand-in, age the claim, ring the roll call, and
    // the card is no longer popped out.
    await page.evaluate(() => { window.__fakeSoloAnswers = false; });
    await page.evaluate(() => soloHeld.set("s1", Date.now() - 20000));
    await page.evaluate(() => runRefresh());
    let dropped = false;
    try {
      await page.waitForFunction(() => !poppedOut("s1"), { timeout: 15000 });
      dropped = true;
    } catch (e) {}
    if (!dropped) {
      fail("a popped-out window that stopped answering the roll call kept its " +
        "card claimed. A closed window's claim must expire so its card is freed.");
    }
    await page.evaluate(() => { try { window.__fakeSolo.close(); } catch (e) {} });

    // ── a popped-out window rides out a hub restart, then recovers ──────────
    // A hub-only deploy leaves the hub with no room for about a second, and it
    // answers a card poll with a 503 "no room is attached" in that window. The
    // solo window must treat that as a reconnect, not a dead card: no blocking
    // "nothing to attach to" modal, a non-blocking reconnecting line instead,
    // and it must paint the card on its own once the room is back.
    soloMode = "noroom";
    const solo = await browser.newPage();
    const soloErrors = [];
    solo.on("pageerror", e => soloErrors.push(String(e)));
    if (process.env.DEBUG_HEADLESS) {
      solo.on("console", m => console.error("[solo] " + m.type() + ": " + m.text()));
    }
    try {
      await solo.goto(base + "#term=s1", { waitUntil: "domcontentloaded" });

      // The reconnecting line comes up, non-blocking.
      await solo.waitForFunction(() => {
        const b = document.getElementById("t-wait");
        return b && !b.hidden && /reconnect/i.test(
          (document.getElementById("t-wait-say") || {}).textContent || "");
      }, { timeout: 15000 });

      // And the dead-end modal is NOT up while the hub is a moment from
      // answering. That modal is the bug: a transient restart used to pop it.
      const stuckEarly = await solo.evaluate(() => {
        const d = document.getElementById("ask");
        return !!(d && d.open &&
          (document.getElementById("ask-title") || {}).textContent === "nothing to attach to");
      });
      if (stuckEarly) {
        fail("the solo window popped the dead-end 'nothing to attach to' modal " +
          "during a hub restart. A no-room 503 must be a reconnect, not a dead card.");
      }

      // The room reattaches. The window must paint the card with no click: its
      // title carries the card's name once soloFetchCard returns.
      soloMode = "ok";
      await solo.waitForFunction(() =>
        /solo card/.test(document.title), { timeout: 20000 });

      // The reconnecting line comes down, and the dead-end modal never appeared.
      const afterRecover = await solo.evaluate(() => {
        const wait = document.getElementById("t-wait");
        const d = document.getElementById("ask");
        return {
          waiting: !!(wait && !wait.hidden),
          deadEnd: !!(d && d.open &&
            (document.getElementById("ask-title") || {}).textContent === "nothing to attach to")
        };
      });
      if (afterRecover.waiting) {
        fail("the solo window kept its reconnecting line up after the room " +
          "returned. Recovery must clear it and paint the card.");
      }
      if (afterRecover.deadEnd) {
        fail("the solo window showed the dead-end modal even after recovering.");
      }
    } finally {
      await solo.close();
    }

    // ── a genuinely missing card still dead-ends ────────────────────────────
    // The fix must not swallow a real 404. A bad link, the card the hub says
    // does not exist, still gets the "nothing to attach to" modal.
    soloMode = "gone";
    const bad404 = await browser.newPage();
    try {
      await bad404.goto(base + "#term=s1", { waitUntil: "domcontentloaded" });
      await bad404.waitForFunction(() => {
        const d = document.getElementById("ask");
        return !!(d && d.open &&
          (document.getElementById("ask-title") || {}).textContent === "nothing to attach to");
      }, { timeout: 15000 });
    } catch (e) {
      fail("a genuine 404 did not show the dead-end 'nothing to attach to' modal: " +
        (e && e.message ? e.message : e));
    } finally {
      await bad404.close();
    }

    // ── the OPEN room picker live-updates when a room attaches ──────────────
    // With the dropdown left open, a room coming online must flip in place from
    // disconnected to live on the `rooms` event, with no reopen. This is the
    // whole of the picker-live fix: the chip's counter was reactive, the open
    // menu was a snapshot from when it opened.
    hubMode = true;
    const hub = await browser.newPage();
    const hubErrors = [];
    hub.on("pageerror", e => hubErrors.push(String(e)));
    if (process.env.DEBUG_HEADLESS) {
      hub.on("console", m => console.error("[hub] " + m.type() + ": " + m.text()));
    }
    try {
      await hub.goto(base, { waitUntil: "domcontentloaded" });

      // The chip shows once the hub probe answers, then the menu is opened the
      // way the chip's onclick does. Called rather than clicked so an overlay in
      // the header layout cannot make the open flaky: this test is about what the
      // OPEN menu does, not about the click that opens it.
      await hub.waitForFunction(() => {
        const el = document.getElementById("rooms");
        return el && !el.hidden;
      }, { timeout: 15000 });
      await hub.evaluate(() => openRooms());
      await hub.waitForFunction(() => {
        const m = document.getElementById("rooms-menu");
        return m && !m.hidden;
      }, { timeout: 15000 });

      // sgg starts disconnected in the open menu, and its placement is recorded
      // so the live update can be proven not to move it. The row is found by its
      // name in the `<strong>`, not the whole button text: the host runs on right
      // after the name with no separator, so a word-boundary match on the text
      // would miss the live row once sgg carries a host.
      const before = await hub.evaluate(() => {
        const btns = [...document.querySelectorAll("#rooms-menu button")];
        const sgg = btns.find(b => {
          const s = b.querySelector("strong");
          return s && s.textContent === "sgg";
        });
        const menu = document.getElementById("rooms-menu");
        return {
          found: !!sgg,
          disconnected: !!sgg && /disconnect/i.test(sgg.textContent),
          live: !!(sgg && sgg.querySelector(".dot.live")),
          top: menu.style.top, left: menu.style.left
        };
      });
      if (!before.found || !before.disconnected || before.live) {
        fail("the open picker did not list sgg as disconnected to begin with: " +
          JSON.stringify(before));
      }

      // Attach sgg, clear the loadHubRooms throttle, then push the `rooms` event
      // the hub sends on a membership change. Nothing reopens the menu.
      sggAttached = true;
      await hub.waitForTimeout(2100);
      hubStreams.forEach(r => { try { r.write("event: rooms\ndata: {}\n\n"); } catch (e) {} });

      // The open menu repaints in place: sgg is now live, not disconnected, and
      // the menu never closed to do it.
      await hub.waitForFunction(() => {
        const menu = document.getElementById("rooms-menu");
        if (!menu || menu.hidden) return false;
        const sgg = [...menu.querySelectorAll("button")].find(b => {
          const s = b.querySelector("strong");
          return s && s.textContent === "sgg";
        });
        return !!(sgg && sgg.querySelector(".dot.live") &&
          !/disconnect/i.test(sgg.textContent));
      }, { timeout: 15000 });

      // The menu held its placement: only the rows changed under the user.
      const after = await hub.evaluate(() => {
        const menu = document.getElementById("rooms-menu");
        return { hidden: menu.hidden, top: menu.style.top, left: menu.style.left };
      });
      if (after.hidden) {
        fail("the picker closed instead of updating in place on the rooms event.");
      }
      if (after.top !== before.top || after.left !== before.left) {
        fail("the picker jumped on the live update (top/left changed): " +
          JSON.stringify({ before, after }));
      }
      if (hubErrors.length) {
        fail("the hub page threw uncaught errors: " + hubErrors.join(" | "));
      }
    } finally {
      await hub.close();
      hubMode = false;
      sggAttached = false;
    }

    // ── the board skin follows the room-picker scope ────────────────────────
    // The ALL view wears the hub's skin; scoping to a room wears that room's;
    // switching scope re-skins (a scope change is a reload, so bootSkin re-reads
    // the right one); a skin saved from ALL lands on the hub, not the old 409;
    // and a room attaching does not clobber the ALL skin. A fresh context keeps
    // this test's per-scope localStorage out of the others'.
    hubMode = true;
    resetSkins();
    const skinCtx = await browser.newContext();
    const skin = await skinCtx.newPage();
    const skinErrors = [];
    skin.on("pageerror", e => skinErrors.push(String(e)));
    if (process.env.DEBUG_HEADLESS) {
      skin.on("console", m => console.error("[skin] " + m.type() + ": " + m.text()));
    }
    const dataSkin = () =>
      skin.evaluate(() => document.documentElement.getAttribute("data-skin"));
    try {
      // ALL scope: the hub's skin, not the alphabetically-first room's.
      await skin.goto(base, { waitUntil: "domcontentloaded" });
      await skin.waitForFunction(() =>
        document.documentElement.getAttribute("data-skin") === "noir", { timeout: 15000 });

      // A skin saved from the ALL view lands on the hub (skinFor[""]), and the
      // board keeps wearing it. A 409 would have made saveSkin revert the paint.
      await skin.evaluate(() => saveSkin("vapor"));
      await skin.waitForFunction(() =>
        document.documentElement.getAttribute("data-skin") === "vapor", { timeout: 15000 });
      if (skinFor[""] !== "vapor") {
        fail("a skin saved from the ALL view did not reach the hub: skinFor[''] is " +
          JSON.stringify(skinFor[""]) + ", wanted vapor.");
      }

      // Scope to a room: a reload re-skins to that room's own skin.
      await Promise.all([
        skin.waitForNavigation({ waitUntil: "domcontentloaded" }),
        skin.evaluate(() => pickRoom("alpha"))
      ]);
      await skin.waitForFunction(() =>
        document.documentElement.getAttribute("data-skin") === "moss", { timeout: 15000 });

      // A skin saved while scoped to a room lands on that room, and leaves the
      // hub's ALL skin alone.
      await skin.evaluate(() => saveSkin("ember"));
      await skin.waitForFunction(() =>
        document.documentElement.getAttribute("data-skin") === "ember", { timeout: 15000 });
      if (skinFor.alpha !== "ember") {
        fail("a skin saved while scoped to alpha did not reach the room: skinFor.alpha is " +
          JSON.stringify(skinFor.alpha) + ", wanted ember.");
      }
      if (skinFor[""] !== "vapor") {
        fail("saving alpha's skin clobbered the hub's ALL skin: skinFor[''] is " +
          JSON.stringify(skinFor[""]) + ", wanted the untouched vapor.");
      }

      // Back to ALL: the hub skin is what it was, and a second room attaching
      // does not change it. This is the borrow-race bug clint hit on a deploy.
      await Promise.all([
        skin.waitForNavigation({ waitUntil: "domcontentloaded" }),
        skin.evaluate(() => pickRoom(null))
      ]);
      await skin.waitForFunction(() =>
        document.documentElement.getAttribute("data-skin") === "vapor", { timeout: 15000 });
      sggAttached = true;
      hubStreams.forEach(r => { try { r.write("event: rooms\ndata: {}\n\n"); } catch (e) {} });
      await skin.waitForTimeout(500);
      if ((await dataSkin()) !== "vapor") {
        fail("a room attaching clobbered the ALL skin: it became " +
          JSON.stringify(await dataSkin()) + ", wanted the hub's vapor.");
      }
      if (skinErrors.length) {
        fail("the skin page threw uncaught errors: " + skinErrors.join(" | "));
      }
    } finally {
      await skin.close();
      await skinCtx.close();
      hubMode = false;
      sggAttached = false;
      resetSkins();
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
  console.log("the board paints its lists, a hung fetch does not blank it, the " +
    "board's roll call re-hears a live popped-out window (and drops one that " +
    "went away), a popped-out window rides out a hub restart and recovers, the " +
    "open room picker live-updates a newly-attached room from disconnected to " +
    "live, and the board skin follows the room-picker scope (ALL wears the " +
    "hub's, each room its own, a save lands in the current scope, a room " +
    "attaching leaves the ALL skin alone).");
}

main().catch(e => { console.error(e); process.exit(1); });
