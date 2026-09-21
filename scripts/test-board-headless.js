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
const fs = require("fs");
const path = require("path");
const { wholeBoard } = require("./board-source.js");

// The board's xterm bundle, served off disk so a real Terminal is built. The
// page loads these as `<script src="/vendor/...">`, which board-source leaves
// external, and the attach-loop repro needs `openTerm` to build a real terminal
// and open a socket rather than bail on a missing library.
const WEB_ROOT = path.join(__dirname, "..", "internal", "api", "web");

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
// The card the attach-loop repro drives. Supervised on the single-card poll a
// watchdog reads, so it always looks like it is "back" and worth attaching. Its
// attach socket is mocked in the browser to close before it opens, which is the
// exact failure that seized the screen.
const LOOP = Object.assign({}, T1, {
  id: "loop1", display_title: "loop card", supervised: true
});
const HIST = {
  id: "h1", display_title: "old run", runner: "claude", status: "done",
  created_at: "2026-09-18T09:00:00Z", why: "did a thing", recap: "",
  worktree: "/tmp/old", archived_at: ""
};
// A pinned terminal whose runner was terminated: not supervised, still pinned,
// so the strip draws it cold. This is the row that used to linger with no way to
// remove it, since terminate is gone once the process is. `pinned` is mutable so
// a PATCH can unpin it and the next /v1/tasks answer drops it, which is what
// proves dismiss does not just hide it for one render.
const PIN = {
  id: "pin1", status: "dead", display_title: "terminated card", runner: "claude",
  rank: 1, worktree: "/tmp/pinned", why: "", idle_seconds: 0, wait_seconds: 0,
  created_at: "2026-09-19T12:00:00Z", last_activity_at: "2026-09-19T12:00:00Z",
  tags: [], supervised: false, offline: false, pinned: true, pid: 0, auto_approve: false
};
function resetPin() { PIN.pinned = true; }

// The hide-doers strip. Four live rows exercising the 3-way control: an
// agent-launched doer working RIGHT NOW (the `origin:agent` tag the launch cap
// counts, plus a live `activity`), an agent-launched doer that is IDLE (the tag,
// no activity), a human's own terminal with no tag, and a PINNED doer. `active`
// hiding drops the working doer only; `inactive` hiding drops the idle doer
// only; the human and the pinned doer stay in every mode, since pinning is the
// operator keeping one and the human's own is never a doer. All supervised so
// they draw as live rows rather than cold, and so `staleActivity` is false and
// the working doer reads as working.
const DOER = {
  id: "doer1", status: "running", display_title: "idle doer", runner: "claude",
  rank: 1, worktree: "/tmp/doer", why: "", idle_seconds: 0, wait_seconds: 0,
  created_at: "2026-09-19T12:00:00Z", last_activity_at: "2026-09-19T12:00:00Z",
  tags: ["origin:agent"], supervised: true, offline: false, pinned: false, auto_approve: false
};
const ADOER = Object.assign({}, DOER, {
  id: "adoer", display_title: "working doer", activity: { what: "thinking" }
});
const HUMANT = Object.assign({}, DOER, {
  id: "humant", display_title: "my terminal", tags: []
});
const PINDOER = Object.assign({}, DOER, {
  id: "pindoer", display_title: "pinned doer", pinned: true
});

let tasksMode = "first";   // first | hang | second | pinned | loop
// Whether the cached list agrees the loop card is attachable. Off during the
// loop repro (the list lags the live card), on once it has recovered.
let loopListSupervised = false;
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
// Whether the hub has a room to borrow `/v1/settings` from. False is the window
// right after a hub restart: no room has re-attached, so the ALL-view read is
// answered with the same 409 the real hub gives, and the board's load-time skin
// read fails. Flipping it true and pushing a `rooms` event is a room attaching,
// which is when the skin must heal without a reload. See the skin-heals test.
let hubHasRoom = true;
const ALPHA = { name: "alpha", host: "alpha-host" };
const SGG = { name: "sgg", host: "sgg-host" };

// The operational audit feed the hub serves, newest first, across two rooms and
// a hub-level line so the room and kind filters have something to sort. `auditLive`
// arms one extra event that the live-delta test makes appear without a reload.
const AUDIT_BASE = [
  { id: "a4", at: "2026-09-19T12:06:00Z", room: "alpha", kind: "permission-requested",
    detail: "Bash: ls" },
  { id: "a3", at: "2026-09-19T12:05:00Z", room: "sgg", kind: "room-attached",
    detail: "sgg running v2" },
  { id: "a2", at: "2026-09-19T12:02:00Z", room: "sgg", kind: "session-start",
    detail: "second card started on claude" },
  { id: "a1", at: "2026-09-19T12:00:00Z", kind: "hub-started", detail: "the hub came up" }
];
let auditLive = false;
const AUDIT_LIVE = { id: "a5", at: "2026-09-19T12:10:00Z", room: "sgg", kind: "session-exit",
  detail: "second card exited with code 0 after 3s" };
function auditFeed() { return auditLive ? [AUDIT_LIVE].concat(AUDIT_BASE) : AUDIT_BASE.slice(); }
function resetAudit() { auditLive = false; }

// The board skin follows the room-picker scope: the ALL view (no X-Atrium-Room
// header) wears the HUB's own skin, and each room wears its own. `skinFor` is
// the mocked hub-plus-rooms state, keyed by scope with "" for the ALL/hub view.
// A skin-only save in the ALL view lands on the hub (skinFor[""]); one made
// while scoped to a room lands on that room, and never on the hub. The list is
// the same across scopes; only the selected one differs.
const SKINS = ["harbour", "moss", "noir", "ember", "vapor", "sandstone"];
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
  // The xterm bundle off disk, so a real terminal is built. Confined to
  // /vendor/ under the web root.
  if (url.startsWith("/vendor/") && !url.includes("..")) {
    const file = path.join(WEB_ROOT, url);
    fs.readFile(file, (err, body) => {
      if (err) { res.writeHead(404); res.end(""); return; }
      const type = url.endsWith(".css") ? "text/css" : "application/javascript";
      res.writeHead(200, { "Content-Type": type });
      res.end(body);
    });
    return;
  }
  // A card-scoped poll, the one a popped-out window opens on. The hub answers a
  // no-room restart with 503 and a genuine missing card with 404, and the solo
  // window has to tell those apart. Placed before the list route, which is the
  // exact path "/v1/tasks" with no trailing id.
  if (url.startsWith("/v1/tasks/")) {
    const id = url.slice("/v1/tasks/".length);
    // The unpin behind dismiss: togglePin PATCHes the card, and the mutated pin
    // state is what the next /v1/tasks answer reads to drop the row for good.
    if (req.method === "PATCH") {
      let raw = "";
      req.on("data", c => { raw += c; });
      req.on("end", () => {
        let body = {};
        try { body = JSON.parse(raw || "{}"); } catch (e) {}
        if (id === "pin1" && typeof body.pinned === "boolean") PIN.pinned = body.pinned;
        sendJSON(res, id === "pin1" ? PIN : T1);
      });
      return;
    }
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
    if (id === "loop1") { sendJSON(res, LOOP); return; }
    sendJSON(res, id === "s1" ? SOLO : id === "pin1" ? PIN : (id === "t2" ? T2 : T1));
    return;
  }
  if (url === "/v1/tasks") {
    if (tasksMode === "hang") { hungResponses.push(res); return; }  // never answer
    // The pinned-cold strip: the terminated card while its pin holds it, and an
    // empty list once dismiss has unpinned it.
    if (tasksMode === "pinned") { sendJSON(res, { tasks: PIN.pinned ? [PIN] : [] }); return; }
    // The hide-doers strip: an idle doer, a working doer, a human's terminal,
    // and a pinned doer.
    if (tasksMode === "doers") { sendJSON(res, { tasks: [DOER, ADOER, HUMANT, PINDOER] }); return; }
    // The attach-loop repro. The cached LIST lags the live card: it carries the
    // loop card WITHOUT `supervised` (so a render finds the pane stale and tears
    // it down) while the single-card poll above still says supervised (so the
    // watchdog attaches again). `loopListSupervised` flips to the recovered
    // state, where the list agrees the card is attachable and nothing tears it
    // down. Pinned so the row is present either way, the way a real lagging
    // list keeps the row while dropping the live flag.
    if (tasksMode === "loop") {
      sendJSON(res, { tasks: [Object.assign({}, LOOP,
        { supervised: loopListSupervised, pinned: true })] });
      return;
    }
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
    // A hub with no room to borrow from cannot answer the ALL view, the same 409
    // the real proxy gives until a room re-attaches. A room-scoped read still
    // goes straight to that room and is unaffected.
    if (!room && !hubHasRoom) {
      res.writeHead(409, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "pick a room first" }));
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
    if (!hubHasRoom) { sendJSON(res, { rooms: [] }); return; }
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
  // The operational audit feed, newest first and filterable by room and kind the
  // same way the hub serves it, so the pane's filters can be driven against a
  // real response. See js/audit.js.
  if (url === "/_hub/audit") {
    if (!hubMode) { res.writeHead(404); res.end("not a hub"); return; }
    const q = new URL(req.url, "http://x").searchParams;
    const room = (q.get("room") || "").toLowerCase();
    const kind = q.get("kind") || "";
    let events = auditFeed();
    if (room) events = events.filter(e => (e.room || "").toLowerCase() === room);
    if (kind) events = events.filter(e => e.kind === kind);
    sendJSON(res, { events });
    return;
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

    // ── a terminated terminal can be dismissed from its right-click menu ─────
    // A pinned card whose runner was terminated stays in the terminal strip,
    // drawn cold. Its menu used to offer no way to remove it, since terminate is
    // gone once the process is. The dismiss entry unpins the card, which is the
    // only thing holding a cold row in the strip, so the row leaves and does not
    // come back on the next render. `renderTermList` is driven directly: the
    // strip lives in the DOM on every view, and this is about what it draws, not
    // about the tab that reveals it.
    tasksMode = "pinned";
    resetPin();
    await page.evaluate(() => renderTermList());
    // Attached, not visible: the terminals view is hidden while the test sits on
    // another tab, and this is about what the strip draws, not whether it shows.
    await page.waitForSelector('#term-list .card.tab[data-id="pin1"].cold',
      { state: "attached", timeout: 15000 });

    // Right-click it. The menu must carry a dismiss entry: a dead terminal's
    // right-click is never allowed to be a dead end.
    await page.evaluate(() => {
      const el = document.querySelector('#term-list .card.tab[data-id="pin1"]');
      el.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, clientX: 40, clientY: 40 }));
    });
    const hasDismiss = () => page.evaluate(() => {
      const m = document.getElementById("cardmenu");
      if (!m || !m.classList.contains("on")) return false;
      return [...m.querySelectorAll(":scope > button")]
        .some(b => /^dismiss\b/.test(b.textContent.trim()));
    });
    try {
      await page.waitForFunction(() => {
        const m = document.getElementById("cardmenu");
        return m && m.classList.contains("on") &&
          [...m.querySelectorAll(":scope > button")].some(b => /^dismiss\b/.test(b.textContent.trim()));
      }, { timeout: 15000 });
    } catch (e) {
      fail("a terminated pinned terminal's right-click menu offered no dismiss " +
        "action, so the operator has no way to remove it.");
    }

    // Press dismiss and let the unpin PATCH land, then render the poll the
    // operator would see next: the row is gone.
    if (await hasDismiss()) {
      const patched = page.waitForResponse(r =>
        r.url().endsWith("/v1/tasks/pin1") && r.request().method() === "PATCH",
        { timeout: 15000 });
      await page.evaluate(() => {
        const b = [...document.getElementById("cardmenu").querySelectorAll(":scope > button")]
          .find(x => /^dismiss\b/.test(x.textContent.trim()));
        b.click();
      });
      await patched;
      await page.evaluate(() => renderTermList());
      const gone = await page.evaluate(() =>
        !document.querySelector('#term-list .card.tab[data-id="pin1"]'));
      if (!gone) {
        fail("dismiss did not remove the terminated terminal from the strip.");
      }
      // A further render, the next poll, keeps it gone: the pin that held it is
      // cleared, not the row hidden once.
      await page.evaluate(() => renderTermList());
      const back = await page.evaluate(() =>
        !!document.querySelector('#term-list .card.tab[data-id="pin1"]'));
      if (back) {
        fail("a dismissed terminal came back on the next render: unpinning must " +
          "drop it from the strip for good, not hide it once.");
      }
    }
    tasksMode = "first";

    // ── the 2-state hide-doers control ──────────────────────────────────────
    // The strip carries an idle doer and a working doer (both `origin:agent`), a
    // human's own terminal, and a pinned doer. `none` shows all four and the
    // control reads "subagents shown". Toggled to `inactive` it drops the idle
    // doer only: the working doer, the human row and the pinned doer all stay,
    // and the control reads "hide inactive subagents". A working doer is never
    // hideable. Driven through the board's own functions so the device-scoped
    // persistence and the split are exercised, not faked.
    tasksMode = "doers";
    const doerState = () => page.evaluate(() => {
      const btn = document.querySelector("#term-list .termdoers");
      const has = id => !!document.querySelector(`#term-list .card.tab[data-id="${id}"]`);
      return {
        idle: has("doer1"), working: has("adoer"),
        human: has("humant"), pinned: has("pindoer"),
        mode: hideDoersMode(),
        label: btn ? btn.textContent.trim() : "",
        lit: !!(btn && btn.classList.contains("on"))
      };
    });
    await page.evaluate(() => {
      try { localStorage.removeItem(termDeviceKey("atrium.hidedoers")); } catch (e) {}
      setHideDoers("none");
      renderTermList();
    });
    await page.waitForSelector('#term-list .card.tab[data-id="doer1"]',
      { state: "attached", timeout: 15000 });
    const dNone = await doerState();
    if (!dNone.idle || !dNone.working || !dNone.human || !dNone.pinned) {
      fail("with hide-doers at `none` the strip did not draw all four rows: " +
        JSON.stringify(dNone));
    }
    if (dNone.mode !== "none" || dNone.lit) {
      fail("the doers control was not in the unlit `none` state to begin with: " +
        JSON.stringify(dNone));
    }
    if (!/^subagents shown$/.test(dNone.label)) {
      fail("the doers control at `none` did not read exactly 'subagents shown': " +
        JSON.stringify(dNone));
    }

    // `inactive`: the idle doer goes, the working doer stays (never hideable),
    // and so do the human row and the pinned doer. The control lights up and
    // reads exactly "hide inactive subagents".
    await page.evaluate(() => { setHideDoers("inactive"); renderTermList(); });
    const dInactive = await doerState();
    if (dInactive.idle) {
      fail("hide-doers `inactive` left the idle agent-launched doer in the strip.");
    }
    if (!dInactive.working) {
      fail("hide-doers `inactive` hid the WORKING doer: only the idle ones must go.");
    }
    if (!dInactive.human || !dInactive.pinned) {
      fail("hide-doers `inactive` hid the human's terminal or a PINNED doer: " +
        JSON.stringify(dInactive));
    }
    if (dInactive.mode !== "inactive" || !dInactive.lit ||
        !/^hide inactive subagents$/.test(dInactive.label)) {
      fail("hide-doers `inactive` did not persist, light, and read exactly " +
        "'hide inactive subagents': " + JSON.stringify(dInactive));
    }

    // Toggling once more returns to `none` and every row comes back, so hiding
    // is a view, not a deletion.
    await page.evaluate(() => { toggleHideDoers(); });
    const dBack = await doerState();
    if (!dBack.idle || !dBack.working || dBack.mode !== "none" ||
        !/^subagents shown$/.test(dBack.label)) {
      fail("toggling the doers control off `inactive` did not return to `none` " +
        "with every row restored and the label reset: " + JSON.stringify(dBack));
    }

    // The control cluster is a sticky header: it stays pinned to the top of the
    // list rather than scrolling away with the cards. Asserted structurally,
    // since a headless run has no tall list to scroll: the two control rows live
    // inside one `.termstick`, and it is `position: sticky` pinned to `top: 0`.
    await page.evaluate(() => { setHideDoers("none"); renderTermList(); });
    const sticky = await page.evaluate(() => {
      const st = document.querySelector("#term-list .termstick");
      if (!st) return { ok: false, why: "no .termstick wrapper" };
      const cs = getComputedStyle(st);
      const rows = st.querySelectorAll(".termhead").length;
      const group = !!st.querySelector("#term-group");
      return { ok: true, position: cs.position, top: cs.top, rows, group };
    });
    if (!sticky.ok) {
      fail("the terminals control cluster is not wrapped for pinning: " + sticky.why);
    } else if (sticky.position !== "sticky" || sticky.top !== "0px") {
      fail("the terminals control cluster is not a sticky header pinned to the top: " +
        JSON.stringify(sticky));
    } else if (sticky.rows < 2 || !sticky.group) {
      fail("the sticky header is missing a control row (sort/doers or the group row): " +
        JSON.stringify(sticky));
    }

    await page.evaluate(() => {
      try { localStorage.removeItem(termDeviceKey("atrium.hidedoers")); } catch (e) {}
    });
    tasksMode = "first";

    // ── the terminals tab is not blank at phone width ───────────────────────
    // THE BUG THIS SECTION EXISTS FOR. The terminals list used to lay itself out
    // as a horizontal strip on a phone, on the idea that it was a thin band of
    // cards above the terminal. It stopped being a flat row of cards long ago:
    // it grew a sort header, a `group` toolbar, a pinned bucket and a nested
    // tree of headings, and laid out sideways those stack ACROSS the screen. The
    // two header bars and the toolbar alone are wider than a phone, so every
    // card was pushed off the right edge and the tab rendered blank with the
    // toolbar stranded mid-screen. This asserts, at 390px, that the list fills
    // the view, the group toolbar sits at the top of it (not floating below a
    // gap), and the cards are on-screen and stacked down the page.
    await page.setViewportSize({ width: 390, height: 780 });
    tasksMode = "pinned";
    resetPin();
    await page.click('.tab[data-view="terms"]');
    await page.evaluate(() => renderTermList());
    await page.waitForSelector('#term-list .card.tab[data-id="pin1"]',
      { state: "visible", timeout: 15000 });
    const phoneTerm = await page.evaluate(() => {
      const vw = window.innerWidth;
      const list = document.getElementById("term-list");
      const lr = list.getBoundingClientRect();
      const groups = document.querySelector("#term-list .termgroups");
      const gr = groups ? groups.getBoundingClientRect() : null;
      const cards = [...document.querySelectorAll("#term-list .card.tab")]
        .map(c => c.getBoundingClientRect());
      const first = cards[0] || null;
      return {
        vw,
        listFills: lr.height,
        // Every card's right edge is within the viewport, so none is pushed off
        // the side the way the horizontal strip pushed all of them.
        cardsOnScreen: cards.length > 0 &&
          cards.every(r => r.left >= -1 && r.right <= vw + 1),
        // The group toolbar is above the first card, at the top of the list,
        // rather than stranded in the blank space the missing cards left.
        groupsAboveCards: !!(gr && first && gr.top <= first.top + 1),
        // The document itself does not scroll sideways.
        docScroll: document.documentElement.scrollWidth <= vw + 1
      };
    });
    if (!phoneTerm.cardsOnScreen) {
      fail("the terminals list drew cards off-screen at 390px: the list is laid " +
        "out sideways and the cards are pushed past the right edge, which is the " +
        "blank-tab bug.");
    }
    if (!phoneTerm.groupsAboveCards) {
      fail("the group toolbar is not at the top of the terminals list at 390px: " +
        "it is stranded in the blank space the off-screen cards left behind.");
    }
    if (phoneTerm.listFills < 200) {
      fail("the terminals list did not fill the view at 390px (height " +
        Math.round(phoneTerm.listFills) + "px): it collapsed instead of taking the pane.");
    }
    if (!phoneTerm.docScroll) {
      fail("the terminals tab made the document scroll sideways at 390px.");
    }

    // ── attached, the terminal is decoupled from the switcher ───────────────
    // With a terminal attached the phone collapses the list to a one-row trigger
    // and the terminal takes the rest. Opening the switcher must NOT resize the
    // terminal: the sessions float OVER it (position: absolute), the way the
    // desktop `off` flyout floats the list over the pane, so the terminal is a
    // stable surface with no shared split to drag. `paintPaneBg` with a theme is
    // what a real attach runs, and it is what sets `has-term`. The filters
    // button folds the sort and grouping controls away until asked for.
    await page.evaluate(() =>
      paintPaneBg({ background: "#101828", foreground: "#e6e6e6", cursor: "#4ea1ff" }));
    const decoupled = await page.evaluate(() => {
      const layout = document.getElementById("term-layout");
      const paneH = () => document.getElementById("term-pane").getBoundingClientRect().height;
      const drop = document.querySelector("#term-list .termdrop");
      const dropShown = drop && getComputedStyle(drop).display !== "none";
      const bodyDisp = () => {
        const b = document.querySelector("#term-list .termbody");
        return b ? getComputedStyle(b).display : "missing";
      };
      const bodyPos = () => {
        const b = document.querySelector("#term-list .termbody");
        return b ? getComputedStyle(b).position : "missing";
      };
      const headShown = () => {
        const h = document.querySelector("#term-list .termbody .termhead");
        return h ? getComputedStyle(h).display !== "none" : false;
      };
      // Collapsed to begin with: the body hidden, the terminal at full height.
      setTermListOpen(false);
      const collapsedBody = bodyDisp();
      const paneCollapsed = paneH();
      // Open the switcher: the body appears as an overlay, and the terminal keeps
      // its height rather than shrinking under a split.
      setTermListOpen(true);
      const openBody = bodyDisp();
      const openPos = bodyPos();
      const paneOpen = paneH();
      // Filters fold away until the button is on.
      const headBefore = headShown();
      toggleTermFilters();
      const headAfter = headShown();
      toggleTermFilters();
      return {
        dropShown, collapsedBody, openBody, openPos,
        paneStable: Math.abs(paneOpen - paneCollapsed) <= 2,
        headHiddenByDefault: !headBefore, headShownAfterToggle: headAfter,
        gripHidden: getComputedStyle(document.getElementById("term-grip")).display === "none"
      };
    });
    if (!decoupled.dropShown) {
      fail("the phone switcher trigger is not shown with a terminal attached.");
    }
    if (decoupled.collapsedBody !== "none") {
      fail("the phone switcher did not collapse with a terminal attached: the list " +
        "body was " + decoupled.collapsedBody + ", not hidden behind the trigger.");
    }
    if (decoupled.openBody === "none" || decoupled.openPos !== "absolute") {
      fail("opening the phone switcher did not float the list over the terminal " +
        "(body display " + decoupled.openBody + ", position " + decoupled.openPos + ").");
    }
    if (!decoupled.paneStable) {
      fail("opening the phone switcher resized the terminal: the two must be " +
        "decoupled, with the list floating over a stable terminal, not tied by a split.");
    }
    if (!decoupled.gripHidden) {
      fail("the width grip is shown on a phone: there is no tied split to drag there.");
    }
    if (!decoupled.headHiddenByDefault || !decoupled.headShownAfterToggle) {
      fail("the filters toggle does not fold the sort/grouping controls on a phone " +
        "(hidden-by-default " + decoupled.headHiddenByDefault + ", shown-after-toggle " +
        decoupled.headShownAfterToggle + ").");
    }
    await page.evaluate(() => { setTermListOpen(false); paintPaneBg(null); });

    // Put the width, the view and the data back for the sections below.
    await page.setViewportSize({ width: 1280, height: 800 });
    tasksMode = "first";
    await page.click('.tab[data-view="stack"]');
    await page.waitForSelector('#stack-list .stackrow', { timeout: 15000 });

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

    // ── a failed attach does not spin the board (the screen-seize loop) ──────
    // THE BUG THIS ACCEPTANCE TEST EXISTS FOR. A supervised card whose attach
    // socket closes before it opens, while the cached task list lags and reads
    // the card as not-attachable, drove openTerm -> switchView -> refresh ->
    // renderTermList -> clearTermPane -> waitAndAttach -> openTerm forever:
    // hundreds of passes a second, a new terminal (and WebGL context) each one,
    // flickering the screen until the tab ran out of contexts. This proves the
    // attempts are BOUNDED, the board does not lock up, and once the attach
    // succeeds it attaches once and stops.
    await page.click('.tab[data-view="terms"]');

    // The attach socket, mocked to CLOSE BEFORE IT OPENS. Only the attach uses
    // `new WebSocket`, so this replaces exactly that and counts each attempt.
    // `__attachSucceeds` flips it to a socket that opens and stays, which is the
    // recovery half. `__openTermCount` counts the re-entry that built a new
    // terminal each pass, which is what exhausted the WebGL contexts.
    await page.evaluate(() => {
      window.__attachAttempts = 0;
      window.__openTermCount = 0;
      window.__attachSucceeds = false;
      window.__realWS = window.WebSocket;
      window.WebSocket = function (url, protocols) {
        if (/\/attach(\?|$)/.test(url)) {
          window.__attachAttempts++;
          const sock = {
            url, readyState: 0, binaryType: "arraybuffer",
            onopen: null, onclose: null, onmessage: null, onerror: null,
            send() {}, close() { this.readyState = 3; }
          };
          if (window.__attachSucceeds) {
            setTimeout(() => { sock.readyState = 1; if (sock.onopen) sock.onopen({}); }, 0);
          } else {
            setTimeout(() => { sock.readyState = 3; if (sock.onclose) sock.onclose({ reason: "" }); }, 0);
          }
          return sock;
        }
        return new window.__realWS(url, protocols);
      };
      // Reassigning the global property is what a bare `openTerm(...)` call
      // resolves to on this page, so every re-entry is counted.
      const realOpen = window.openTerm;
      window.openTerm = function (task) { window.__openTermCount++; return realOpen(task); };
    });

    // Drive the exact chain: the cached list lags (loop card not attachable) and
    // the single-card poll says supervised, so the watchdog keeps attaching.
    tasksMode = "loop";
    loopListSupervised = false;
    await page.evaluate(async () => {
      const t = await api("/v1/tasks/loop1");
      openTerm(t);
    });

    // A second of real time. On the broken code the counters run into the
    // hundreds here; the fix bounds them.
    await page.waitForTimeout(1200);
    const opens = await page.evaluate(() => window.__openTermCount);
    const attempts = await page.evaluate(() => window.__attachAttempts);
    if (opens > 8) {
      fail("a failed attach re-entered openTerm " + opens + " times in a second: the " +
        "render/watchdog loop is spinning the board. It must be bounded.");
    }
    if (attempts > 12) {
      fail("a failed attach opened " + attempts + " sockets in a second: the retry is not " +
        "backing off. It must be a debounced, capped timer.");
    }

    // Not wedged: a refresh still completes and the page still answers.
    const alive = await page.evaluate(() => {
      try { runRefresh(); return true; } catch (e) { return false; }
    });
    if (!alive) fail("the board was wedged after the attach loop: runRefresh threw.");

    // Recover: the attach starts succeeding and the list catches up. It must
    // attach ONCE more and then stop, not keep churning.
    loopListSupervised = true;
    await page.evaluate(() => {
      window.__attachSucceeds = true;
      window.__openTermCount = 0;
      window.__attachAttempts = 0;
    });
    // Wait past the capped backoff for a pending retry to fire and open.
    await page.waitForFunction(() => window.__attachAttempts > 0, { timeout: 20000 });
    await page.waitForTimeout(1500);
    const opensAfter = await page.evaluate(() => window.__openTermCount);
    const attemptsAfter = await page.evaluate(() => window.__attachAttempts);
    if (opensAfter > 2) {
      fail("after the attach recovered it re-opened the terminal " + opensAfter + " times: a " +
        "successful attach must attach once and stop.");
    }
    if (attemptsAfter > 3) {
      fail("after the attach recovered it kept opening sockets (" + attemptsAfter + "): it must " +
        "settle once it is connected.");
    }

    // Put the socket, the loop card and the view back for the sections below.
    await page.evaluate(() => {
      try { closeTerm(); } catch (e) {}
      window.WebSocket = window.__realWS;
    });
    tasksMode = "first";
    loopListSupervised = false;
    await page.click('.tab[data-view="stack"]');
    await page.waitForSelector('#stack-list .stackrow', { timeout: 15000 });

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

    // ── a desktop notification lands in the toast log ───────────────────────
    // THE BUG THIS SECTION EXISTS FOR. When no atrium window has focus, notify
    // fires ONE desktop notification and no toast. The log was fed only by the
    // toast, so an alert that rang and popped while the board was unfocused left
    // no trace: clint saw and heard two and could find neither. This drives the
    // nobody-looking branch and asserts the alert is recorded even though no
    // toast is shown, and that a toast is NOT also shown (either/or, not both).
    const toastRecord = await page.evaluate(() => {
      // The nobody-looking branch: this window is not focused and no sibling
      // claims focus, desktop is allowed, and sound is on. The OS notification
      // is stubbed so the headless run does not try to raise a real one, and it
      // is counted so the desktop path is provable, not merely inferred from
      // the log.
      localStorage.setItem("atrium.toastlog", "[]");
      localStorage.removeItem("atrium.toastlog.seen");
      window.focusIsHere = () => false;
      window.focusIsElsewhere = () => "";
      window.desktopAllowed = () => true;
      window.__notified = 0;
      window.showNotification = () => { window.__notified++; return null; };
      document.getElementById("toasts").innerHTML = "";
      alerting.set({ muted: false, desktop: true });
      alerting.notify("clint: a held message", "a peer is waiting on this session",
        "stack", "", "held-1", null, "", "", {});
      return {
        notified: window.__notified,
        toasts: document.querySelectorAll("#toasts .toast").length,
        log: JSON.parse(localStorage.getItem("atrium.toastlog") || "[]")
      };
    });
    if (toastRecord.notified !== 1) {
      fail("the nobody-looking alert did not take the desktop path (showNotification " +
        "called " + toastRecord.notified + " times): the test did not exercise the bug.");
    }
    if (toastRecord.toasts !== 0) {
      fail("the nobody-looking alert also drew a toast: the either-toast-or-desktop " +
        "behavior was broken, they must never both fire.");
    }
    if (toastRecord.log.length !== 1 ||
        toastRecord.log[0].title !== "clint: a held message") {
      fail("a desktop-only notification was not recorded in the toast log: " +
        JSON.stringify(toastRecord.log) + ". A desktop alert fired while the board " +
        "is unfocused must still be findable afterward.");
    }
    // Restore the stubs so nothing below inherits them, and clear the log.
    await page.evaluate(() => {
      try { localStorage.setItem("atrium.toastlog", "[]"); } catch (e) {}
    });

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

      // ── the audit pane is hub-only and paints from /_hub/audit ────────────
      // The tab is hidden on a plain daemon and revealed once the hub probe
      // answers. Switching to it fetches the feed and draws one row per event,
      // newest first. See js/audit.js.
      await hub.waitForFunction(() => {
        const tab = document.querySelector('.tab[data-view="audit"]');
        return tab && !tab.hidden;
      }, { timeout: 15000 });
      await hub.evaluate(() => switchView("audit"));
      await hub.waitForFunction(() => {
        const rows = document.querySelectorAll("#audit-list .aud-row");
        return rows.length >= 2;
      }, { timeout: 15000 });
      const audit = await hub.evaluate(() => {
        const rows = [...document.querySelectorAll("#audit-list .aud-row")];
        const first = rows[0];
        return {
          count: rows.length,
          firstKind: first && first.querySelector(".aud-kind")
            ? first.querySelector(".aud-kind").textContent : "",
          hubLine: rows.some(r => r.querySelector(".aud-room.aud-hub"))
        };
      });
      // Newest first: the 12:06 permission line is above the 12:00 hub-started.
      if (audit.firstKind !== "permission-requested") {
        fail("the audit pane did not draw newest first: " + JSON.stringify(audit));
      }
      // A hub-level line (no room) is drawn as `hub`.
      if (!audit.hubLine) {
        fail("the audit pane did not mark the hub-level line: " + JSON.stringify(audit));
      }
      if (hubErrors.length) {
        fail("the hub page threw uncaught errors after the audit pane: " +
          hubErrors.join(" | "));
      }

      // ── the audit pane filters by room ────────────────────────────────────
      // Picking a room narrows the feed to that machine. The two sgg lines stay
      // and the alpha and hub lines go, and the request carries the filter, so
      // this is the hub filtering rather than the pane hiding rows.
      await hub.evaluate(() => {
        const sel = document.getElementById("audit-room");
        // The option is normally seeded from the hub's room list; add it if the
        // probe has not filled the dropdown yet, so this tests the filter and not
        // the timing of when the list arrived.
        if (![...sel.options].some(o => o.value === "sgg")) {
          const o = document.createElement("option");
          o.value = "sgg"; o.textContent = "sgg"; sel.appendChild(o);
        }
        sel.value = "sgg";
        sel.dispatchEvent(new Event("change"));
      });
      await hub.waitForFunction(() => {
        const rows = [...document.querySelectorAll("#audit-list .aud-row")];
        return rows.length === 2 && rows.every(r => {
          const rm = r.querySelector(".aud-room");
          return rm && rm.textContent === "sgg";
        });
      }, { timeout: 15000 }).catch(() => fail(
        "the audit pane did not filter to the sgg room."));

      // ── the audit pane filters by kind ────────────────────────────────────
      // Clear the room, then pick a kind: only the hub-started line remains. Done
      // in two sequenced steps so the room-clear fetch settles before the kind
      // fetch fires, rather than racing it.
      await hub.evaluate(() => {
        const sel = document.getElementById("audit-room");
        sel.value = "";
        sel.dispatchEvent(new Event("change"));
      });
      await hub.waitForFunction(() =>
        document.querySelectorAll("#audit-list .aud-row").length === 4,
        { timeout: 15000 }).catch(() => fail(
          "clearing the room filter did not restore the full audit feed."));
      await hub.evaluate(() => {
        const sel = document.getElementById("audit-kind");
        sel.value = "hub-started";
        sel.dispatchEvent(new Event("change"));
      });
      await hub.waitForFunction(() => {
        const rows = [...document.querySelectorAll("#audit-list .aud-row")];
        return rows.length === 1 &&
          rows[0].querySelector(".aud-kind").textContent === "hub-started";
      }, { timeout: 15000 }).catch(() => fail(
        "the audit pane did not filter to the hub-started kind."));

      // ── a new event arrives live, no reload ───────────────────────────────
      // Clear the kind filter, then a fresh event lands on the hub and it emits
      // an `audit` delta. The open pane re-fetches on that delta alone and the
      // new session-exit line appears at the top, without the page reloading.
      await hub.evaluate(() => {
        const sel = document.getElementById("audit-kind");
        sel.value = "";
        sel.dispatchEvent(new Event("change"));
      });
      await hub.waitForFunction(() =>
        document.querySelectorAll("#audit-list .aud-row").length === 4,
        { timeout: 15000 }).catch(() => fail(
          "clearing the kind filter did not restore the full audit feed."));
      auditLive = true;
      hubStreams.forEach(r => { try { r.write("event: audit\ndata: {}\n\n"); } catch (e) {} });
      await hub.waitForFunction(() => {
        const rows = [...document.querySelectorAll("#audit-list .aud-row")];
        return rows.length === 5 &&
          rows[0].querySelector(".aud-kind").textContent === "session-exit";
      }, { timeout: 15000 }).catch(() => fail(
        "the audit pane did not pick up a live event on the `audit` delta."));

      if (hubErrors.length) {
        fail("the hub page threw uncaught errors after the audit filters: " +
          hubErrors.join(" | "));
      }
    } finally {
      resetAudit();
      await hub.close();
      hubMode = false;
      sggAttached = false;
    }

    // ── the board skin follows the room-picker scope ────────────────────────
    // THREE SCOPES, THREE SKINS, HELD AT ONCE. The ALL view wears the hub's skin,
    // and each of two rooms wears its own, so scoping ALL -> alpha -> sgg reads
    // three different skins back. Then: a save in one scope reaches only that
    // scope and leaks into no other, switching scope re-applies each
    // independently, and a room attaching does not clobber the ALL skin. A fresh
    // context keeps this test's per-scope localStorage out of the others'.
    // Both rooms live, so all three scopes are pickable.
    hubMode = true;
    sggAttached = true;
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
    // scopeTo reloads the board into a scope (null for ALL) and waits for the
    // skin that scope wears to land.
    const scopeTo = async (room, want) => {
      await Promise.all([
        skin.waitForNavigation({ waitUntil: "domcontentloaded" }),
        skin.evaluate(r => pickRoom(r), room)
      ]);
      await skin.waitForFunction(w =>
        document.documentElement.getAttribute("data-skin") === w, want, { timeout: 15000 });
    };
    try {
      // ALL scope: the hub's own skin, not the alphabetically-first room's.
      await skin.goto(base, { waitUntil: "domcontentloaded" });
      await skin.waitForFunction(() =>
        document.documentElement.getAttribute("data-skin") === "noir", { timeout: 15000 });

      // Scope to each room in turn: three scopes, three different skins, at once.
      // The hub holds noir, alpha holds moss, sgg holds ember, and no two agree.
      await scopeTo("alpha", "moss");
      await scopeTo("sgg", "ember");
      const held = { "": skinFor[""], alpha: skinFor.alpha, sgg: skinFor.sgg };
      const distinct = new Set(Object.values(held));
      if (distinct.size !== 3) {
        fail("the three scopes did not hold three different skins at once: " +
          JSON.stringify(held));
      }
      if (held[""] !== "noir" || held.alpha !== "moss" || held.sgg !== "ember") {
        fail("the three scopes wore the wrong skins: " + JSON.stringify(held));
      }

      // A skin saved from the ALL view lands on the hub (skinFor[""]) and leaves
      // both rooms alone. A 409 would have made saveSkin revert the paint.
      await scopeTo(null, "noir");
      await skin.evaluate(() => saveSkin("vapor"));
      await skin.waitForFunction(() =>
        document.documentElement.getAttribute("data-skin") === "vapor", { timeout: 15000 });
      if (skinFor[""] !== "vapor") {
        fail("a skin saved from the ALL view did not reach the hub: skinFor[''] is " +
          JSON.stringify(skinFor[""]) + ", wanted vapor.");
      }
      if (skinFor.alpha !== "moss" || skinFor.sgg !== "ember") {
        fail("saving the ALL skin leaked into a room: " + JSON.stringify(skinFor));
      }

      // A skin saved while scoped to alpha lands on alpha alone, and leaves the
      // hub's ALL skin and sgg's untouched.
      await scopeTo("alpha", "moss");
      await skin.evaluate(() => saveSkin("sandstone"));
      await skin.waitForFunction(() =>
        document.documentElement.getAttribute("data-skin") === "sandstone", { timeout: 15000 });
      if (skinFor.alpha !== "sandstone") {
        fail("a skin saved while scoped to alpha did not reach the room: skinFor.alpha is " +
          JSON.stringify(skinFor.alpha) + ", wanted sandstone.");
      }
      if (skinFor[""] !== "vapor" || skinFor.sgg !== "ember") {
        fail("saving alpha's skin leaked into another scope: " + JSON.stringify(skinFor));
      }

      // A skin saved while scoped to sgg lands on sgg alone.
      await scopeTo("sgg", "ember");
      await skin.evaluate(() => saveSkin("harbour"));
      await skin.waitForFunction(() =>
        document.documentElement.getAttribute("data-skin") === null, { timeout: 15000 });
      if (skinFor.sgg !== "harbour") {
        fail("a skin saved while scoped to sgg did not reach the room: skinFor.sgg is " +
          JSON.stringify(skinFor.sgg) + ", wanted harbour.");
      }
      if (skinFor[""] !== "vapor" || skinFor.alpha !== "sandstone") {
        fail("saving sgg's skin leaked into another scope: " + JSON.stringify(skinFor));
      }

      // Switching scope re-applies each saved skin independently: ALL is vapor,
      // alpha is sandstone, sgg is the default harbour (drawn by removing the
      // attribute), and each is read fresh on its own reload.
      await scopeTo(null, "vapor");
      await scopeTo("alpha", "sandstone");
      await Promise.all([
        skin.waitForNavigation({ waitUntil: "domcontentloaded" }),
        skin.evaluate(() => pickRoom("sgg"))
      ]);
      await skin.waitForFunction(() =>
        document.documentElement.getAttribute("data-skin") === null, { timeout: 15000 });

      // Back to ALL: the hub skin is what it was, and a room attaching does not
      // change it. This is the borrow-race bug clint hit on a deploy.
      await scopeTo(null, "vapor");
      sggAttached = false;
      hubStreams.forEach(r => { try { r.write("event: rooms\ndata: {}\n\n"); } catch (e) {} });
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

    // ── a persisted skin heals when a room attaches, with no reload ──────────
    // The board loads against a hub that has no room to borrow settings from
    // yet, the window right after a hub restart. The ALL-view `/v1/settings`
    // read is a 409, so the load-time skin read fails and the board is on the
    // default dark. This is exactly what clint saw: a deploy restarts the hub,
    // he reloads before a room is back, and the paper skin never paints. When a
    // room attaches the read succeeds, and the skin must heal there rather than
    // waiting for another manual reload.
    hubMode = true;
    hubHasRoom = false;
    sggAttached = false;
    // The hub wears paper, a light skin, which is the one clint set and did not
    // see paint. resetSkins in the finally puts the default back.
    skinFor = { "": "paper", alpha: "moss", sgg: "ember" };
    const healCtx = await browser.newContext();
    const heal = await healCtx.newPage();
    const healErrors = [];
    heal.on("pageerror", e => healErrors.push(String(e)));
    if (process.env.DEBUG_HEADLESS) {
      heal.on("console", m => console.error("[heal] " + m.type() + ": " + m.text()));
    }
    try {
      await heal.goto(base, { waitUntil: "domcontentloaded" });
      // No room to borrow from: the load-time read 409s and the board is dark.
      await heal.waitForTimeout(1500);
      const dark = await heal.evaluate(() =>
        document.documentElement.getAttribute("data-skin"));
      if (dark !== null) {
        fail("with the hub unable to answer settings, the board should be on the " +
          "default, but data-skin was " + JSON.stringify(dark));
      }
      // Past loadHubRooms' 2s throttle, then a room attaches: the read now
      // succeeds and the skin heals to the hub's paper without a reload.
      await heal.waitForTimeout(2200);
      hubHasRoom = true;
      hubStreams.forEach(r => { try { r.write("event: rooms\ndata: {}\n\n"); } catch (e) {} });
      await heal.waitForFunction(() =>
        document.documentElement.getAttribute("data-skin") === "paper", { timeout: 15000 });
      if (healErrors.length) {
        fail("the skin-heal page threw uncaught errors: " + healErrors.join(" | "));
      }
    } finally {
      await heal.close();
      await healCtx.close();
      hubMode = false;
      hubHasRoom = true;
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
  console.log("a terminated pinned terminal can be dismissed from its right-click " +
    "menu and stays gone on the next render, " +
    "the 3-way hide-doers control drops the active or the inactive agent-launched " +
    "doers while keeping the human's own and pinned rows and showing a hidden count, " +
    "the control cluster is a sticky header pinned to the top of the list, " +
    "the board paints its lists, a hung fetch does not blank it, the " +
    "board's roll call re-hears a live popped-out window (and drops one that " +
    "went away), a popped-out window rides out a hub restart and recovers, the " +
    "open room picker live-updates a newly-attached room from disconnected to " +
    "live, the audit pane paints newest-first, filters by room and by kind, and " +
    "picks up a live event on the `audit` delta with no reload, and the board " +
    "skin follows the room-picker scope (ALL wears the " +
    "hub's, each room its own, a save lands in the current scope, a room " +
    "attaching leaves the ALL skin alone), a persisted skin heals when a " +
    "room attaches after a load that could not read settings, with no reload, " +
    "and a desktop notification fired while no window has focus is recorded in " +
    "the toast log without also drawing a toast.");
}

main().catch(e => { console.error(e); process.exit(1); });
