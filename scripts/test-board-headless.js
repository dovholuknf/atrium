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
// A card whose last turn nobody has seen, ending on two questions nobody has
// answered. Its own card so the rest of this file's cards stay unmarked. See
// docs/seen-design.md.
const SEEN = Object.assign({}, T1, {
  id: "seen1", display_title: "unread card",
  seen: { unseen: true, turn_ended_at: "2026-09-23T12:00:00.000Z", answered: false,
    open_questions: ["land sa21 first?", "build the tray?"] }
});
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
// `histMany` swaps in 250 runs, more than two pages, for the scroll test, and
// `histManyLive` puts one new run on top of them.
let histMany = false;
let histManyLive = false;
const histRun = n => ({
  id: "hm" + n, display_title: "run " + n, runner: "claude", status: "done",
  created_at: new Date(Date.UTC(2026, 8, 18, 9, 0, n)).toISOString(), why: "run number " + n,
  recap: "", worktree: "/tmp/run" + n, archived_at: ""
});
const HIST_MANY = Array.from({ length: 250 }, (_, i) => histRun(250 - i));
function histFeed() { return histManyLive ? [histRun(251)].concat(HIST_MANY) : HIST_MANY; }
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

// A pinned, live card filed into the operator's `active` group, the shape of the
// screenshot where the group said 0 and asked for a card to be filed into it.
const FILED = Object.assign({}, PIN, {
  id: "filed1", display_title: "filed card", status: "running", supervised: true,
  tags: ["active"], worktree: "/tmp/filed"
});
// A live, unpinned, ungrouped row beside it, for the drag case.
const LOOSE = Object.assign({}, T1, { id: "loose1", display_title: "loose card", supervised: true });

// The hide-strip, exercising the two independent "hide inactive" toggles. The
// two kinds read DIFFERENT inactive signals: an AGENT is inactive when it has no
// live connection (`supervised`), so a connected agent stays whether it is
// thinking or idle at a prompt; a SUBAGENT is inactive when it is not working
// right now (`workingNow`), so only an actively-computing subagent stays and an
// idle OR exited one hides.
//
// SUBAGENTS carry the `origin:agent` tag the launch cap counts; AGENTS do not (a
// human's own session). A dead UNPINNED session is not in the strip at all
// (nothing to switch to), so the dead rows are PINNED, drawn cold, which also
// proves a pinned session hides like any other once its toggle is on. The
// subagent side has three rows to separate its rule from the agents' rule:
// WORKING (stays), IDLE (supervised but not computing, hides - the difference
// from the agents rule) and DEAD (pinned, hides). The agent side has an
// IDLE-BUT-LIVE row (stays) and a DEAD row (pinned, cold, hides).
const SUBLIVE = {
  id: "sublive", status: "running", display_title: "idle subagent", runner: "claude",
  rank: 1, worktree: "/tmp/sublive", why: "", idle_seconds: 0, wait_seconds: 0,
  created_at: "2026-09-19T12:00:00Z", last_activity_at: "2026-09-19T12:00:00Z",
  tags: ["origin:agent"], supervised: true, offline: false, pinned: false, auto_approve: false
};
const SUBWORK = Object.assign({}, SUBLIVE, {
  id: "subwork", display_title: "working subagent", activity: { what: "thinking" }
});
// Runner gone, held by its pin, drawn cold: not working (and exited), so the
// subagents toggle hides it despite the pin.
const SUBDEAD = Object.assign({}, SUBLIVE, {
  id: "subdead", display_title: "dead subagent", status: "dead",
  supervised: false, pinned: true, pid: 0
});
const AGLIVE = Object.assign({}, SUBLIVE, {
  id: "aglive", display_title: "my terminal", tags: []
});
// A dead agent (no tag), pinned cold: the agents toggle hides it despite the pin.
const AGDEAD = Object.assign({}, SUBLIVE, {
  id: "agdead", display_title: "my dead terminal", tags: [],
  status: "dead", supervised: false, pinned: true, pid: 0
});

// Four live untagged cards served in ADDRESS order, which is not name order and
// not activity order, so a view that sorts on the wrong key is caught. The
// shape of the screenshot: named cards whose dim second line is the path.
const untaggedCard = (id, title, worktree, idle) => Object.assign({}, T1, {
  id, display_title: title, worktree, repo: worktree.split("/").pop(), branch: "b",
  idle_seconds: idle, supervised: true
});
const UNTAGGED_CARDS = [
  untaggedCard("u-sa67", "sa67 stages", "/w/claude/atrium/one", 30),
  untaggedCard("u-sa65", "sa65 merger", "/w/github/dovholuknf/atrium", 10),
  untaggedCard("u-night", "nightly-fail", "/w/github/openziti/ziti-sdk-csharp", 20),
  untaggedCard("u-disc", "discourse-6101", "/w/github/openziti/ziti", 40)
];
// Cards added under a page that is already open, a 503 while a hub has no room,
// an empty answer while it has none yet, and the `room~` tag a second room puts
// on every id.
let untaggedExtra = [];
let untaggedDown = false;
let untaggedEmpty = false;
let untaggedTag = "";
const NEWC = untaggedCard("u-new", "brand new", "/w/github/openziti/fresh", 5);

// The cards the alert-landing section clicks through to, by id. `land-new` is
// the race: listed before its terminal, and made supervised by the test a
// moment after the click. `landList` is what the list answers on top of T1, and
// `landPerms` the pending requests. See `landSection`.
const LAND = {};
function landCard(id, over) {
  LAND[id] = Object.assign({}, T1, { id, display_title: id.replace("-", " "),
    created_at: new Date().toISOString() }, over);
  return LAND[id];
}
let landList = [];
let landPerms = [];

let tasksMode = "first";   // first | hang | second | pinned | loop | worn | untagged | land
// One card per shipped terminal theme, filled in from the page's own table by
// the card-colours section, plus one with no theme that takes the repo default.
let wornTasks = [];
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
// The hub restart gate's mocked state: whether a pause is held, and how many
// times the board called each of its three endpoints.
let gatePaused = false;
// What `GET /_hub/restart` says is left of a countdown, for a window opened during one.
let gateCountdownLeft = 0;
// Which hub process `GET /_hub/restart` names, and whether the hub is away. Away
// drops every stream and hub call on the floor, the way a stopped hub refuses
// the connection, so the board's event stream keeps retrying. See
// restartStaysSection.
let gateBoot = "boot-a";
let hubAway = false;
// The board build `/v1/health` reports. A change makes the board reload.
let healthBuild = "test";
// Whether the service worker and its offline page are served, and whether the
// page itself answers 502 the way a share in front of a stopped atrium does.
// Off by default: every other section runs without a worker. See atriumDownSection.
let serveSW = false;
let page502 = false;
const gateCalls = { input: 0, pause: 0, resume: 0 };
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
// `auditMany` swaps in a feed far taller than the window, for the scroll test, and
// `auditManyLive` puts one more line on top of it.
let auditMany = false;
let auditManyLive = false;
const AUDIT_MANY = Array.from({ length: 150 }, (_, i) => ({
  id: "m" + (150 - i), at: new Date(Date.UTC(2026, 8, 19, 12, 0, 150 - i)).toISOString(),
  room: i % 2 ? "sgg" : "alpha", kind: "session-start", detail: "card " + (150 - i) + " started"
}));
const AUDIT_MANY_LIVE = { id: "m151", at: "2026-09-19T12:03:00Z", room: "sgg", kind: "session-exit",
  detail: "card 151 exited" };
function auditFeed() {
  if (auditMany) return auditManyLive ? [AUDIT_MANY_LIVE].concat(AUDIT_MANY) : AUDIT_MANY.slice();
  return auditLive ? [AUDIT_LIVE].concat(AUDIT_BASE) : AUDIT_BASE.slice();
}
function resetAudit() { auditLive = false; auditMany = false; auditManyLive = false; }

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
  return Object.assign({ global_auto: gautoOn, global_auto_seconds: 0, board_skin: skin, board_skins: SKINS },
    kaSettings);
}
// The cache keep-alive's room settings, and every write the board made to it or
// to a card's switch. See keepaliveSection.
let kaSettings = {};
let kaWrites = [];
// The global auto switch the mocked daemon holds, and whether a room-scoped
// settings read fails: the room the picker is scoped to has not re-attached yet,
// so the hub cannot reach it. See the gauto-never-blank test.
let gautoOn = false;
let roomSettingsDown = false;
// Every settings read answered, and how long each takes. A hub's answer takes
// over 100ms, which is what lets a load's several readers overlap.
let settingsReads = 0;
let settingsDelay = 0;
// The on/off switch rows: one runner and one fixture in each state, so the test
// can compare the pill's box across an on row and an off row, and flip each way.
// `switchFail` makes the next write refuse, which must put the pill back.
// `switchWrites` is every write the switches made, as "METHOD path enabled".
const HON = { id: "hon", label: "claude", cmd: "claude", args: [], enabled: true,
  found: "/usr/bin/claude", launch_mode: "pty" };
const HOFF = Object.assign({}, HON, { id: "hoff", label: "codex", cmd: "codex", enabled: false });
const FON = { id: "fon", label: "notes", harness: "claude", cwd: "/tmp/notes", enabled: true,
  resume: true, sort: 0 };
const FOFF = Object.assign({}, FON, { id: "foff", label: "scratch", enabled: false, sort: 1 });
let switchFail = false;
let switchWrites = [];
function resetSwitches() {
  HON.enabled = true; HOFF.enabled = false; FON.enabled = true; FOFF.enabled = false;
  switchFail = false; switchWrites = [];
}
// A PUT onto one of the rows above. The row takes the new state unless the test
// armed a refusal, which is answered the way the daemon answers a bad save.
function switchPut(req, res, rows) {
  let raw = "";
  req.on("data", c => { raw += c; });
  req.on("end", () => {
    let body = {};
    try { body = JSON.parse(raw || "{}"); } catch (e) {}
    const id = decodeURIComponent(req.url.split("?")[0].split("/").pop());
    switchWrites.push(req.method + " " + req.url.split("?")[0] + " " + body.enabled);
    if (switchFail) {
      switchFail = false;
      res.writeHead(400, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "the daemon refused the switch" }));
      return;
    }
    const row = rows.find(r => r.id === id);
    if (row) row.enabled = !!body.enabled;
    sendJSON(res, row || body);
  });
}

const hungResponses = [];   // held-open sockets, ended on teardown
const openStreams = [];
const hubStreams = [];       // hub event streams, used to push a `rooms` event

// A room the hub still counts as attached but that does not answer: every
// proxied read is held until this time, then refused. See idleRateSection.
let roomStallUntil = 0;
let stalledCount = 0;

const HTML = wholeBoard();

function sendJSON(res, obj) {
  const body = JSON.stringify(obj);
  res.writeHead(200, { "Content-Type": "application/json" });
  res.end(body);
}

const server = http.createServer((req, res) => {
  const url = req.url.split("?")[0];

  if (hubAway) { req.socket.destroy(); return; }

  if (roomStallUntil > Date.now() && url.startsWith("/v1/") && url !== "/v1/health" &&
      !url.startsWith("/v1/events")) {
    stalledCount++;
    setTimeout(() => { res.writeHead(503); res.end("{}"); }, roomStallUntil - Date.now());
    return;
  }

  if (serveSW && (url === "/sw.js" || url === "/down.html")) {
    fs.readFile(path.join(WEB_ROOT, url), (err, body) => {
      if (err) { res.writeHead(404); res.end(""); return; }
      res.writeHead(200, { "Content-Type": url.endsWith(".js") ? "application/javascript" : "text/html",
        "Cache-Control": "no-store" });
      res.end(body);
    });
    return;
  }
  if (url === "/" || url === "/index.html") {
    if (page502) { res.writeHead(502, { "Content-Type": "text/plain" }); res.end("bad gateway"); return; }
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
  // A card's keep-alive switch. Recorded, and answered the way the daemon does.
  if (url.startsWith("/v1/tasks/") && url.endsWith("/keepalive") && req.method === "POST") {
    let raw = "";
    req.on("data", c => { raw += c; });
    req.on("end", () => {
      let body = {};
      try { body = JSON.parse(raw || "{}"); } catch (e) {}
      kaWrites.push({ url, body });
      sendJSON(res, { task_id: url.split("/")[3], keepalive: { state: body.on ? "on" : "off" } });
    });
    return;
  }
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
        // A theme kept from the picker, saved on the worn list so a reload reads it back.
        const worn = tasksMode === "worn" && wornTasks.find(t => t.id === id);
        if (worn && typeof body.theme === "string") { worn.theme = body.theme; sendJSON(res, worn); return; }
        sendJSON(res, id === "pin1" ? PIN : T1);
      });
      return;
    }
    // Ahead of the solo modes, which an earlier section can leave set: the
    // group sections read this card's tags for its menu.
    if (id === "filed1") { sendJSON(res, FILED); return; }
    if (id.startsWith("land-") && !id.includes("/")) {
      if (LAND[id]) { sendJSON(res, LAND[id]); return; }
      res.writeHead(404, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "no such card" }));
      return;
    }
    if (soloMode === "noroom") {
      res.writeHead(503, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "no room is attached to this hub. the hub " +
        "serves the board and holds nothing, so until a room connects there is " +
        "nothing to show. run `atrium room join` on the machine your agents are on." }));
      return;
    }
    if (soloMode === "gone") {
      res.writeHead(404, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "no such card" }));
      return;
    }
    if (id === "loop1") { sendJSON(res, LOOP); return; }
    // The solo card under its room-tagged spelling, the form the hub serves
    // while more than one room is attached. See `popoutTagFlipSection`.
    if (id === "sgg~s1") { sendJSON(res, Object.assign({}, SOLO, { id: "sgg~s1" })); return; }
    sendJSON(res, id === "s1" ? SOLO : id === "pin1" ? PIN : (id === "t2" ? T2 : T1));
    return;
  }
  if (url === "/v1/tasks") {
    if (tasksMode === "hang") { hungResponses.push(res); return; }  // never answer
    // The pinned-cold strip: the terminated card while its pin holds it, and an
    // empty list once dismiss has unpinned it.
    if (tasksMode === "pinned") { sendJSON(res, { tasks: PIN.pinned ? [PIN] : [] }); return; }
    if (tasksMode === "filed") { sendJSON(res, { tasks: [FILED, LOOSE] }); return; }
    if (tasksMode === "seen") { sendJSON(res, { tasks: [T1, SEEN] }); return; }
    if (tasksMode === "land") { sendJSON(res, { tasks: [T1].concat(landList) }); return; }
    // Untagged cards in custom mode, for the sort and the new-card sections.
    if (tasksMode === "untagged") {
      if (untaggedDown) { res.writeHead(503); res.end("{}"); return; }
      if (untaggedEmpty) { sendJSON(res, { tasks: [] }); return; }
      const list = [FILED].concat(UNTAGGED_CARDS, untaggedExtra);
      sendJSON(res, { tasks: untaggedTag
        ? list.map(t => Object.assign({}, t, { id: untaggedTag + "~" + t.id })) : list });
      return;
    }
    // The hide strip: an alive idle subagent, an alive working subagent, a dead
    // (cold, pinned) subagent, an alive agent and a dead (cold, pinned) agent.
    if (tasksMode === "doers") {
      sendJSON(res, { tasks: [SUBLIVE, SUBWORK, SUBDEAD, AGLIVE, AGDEAD] }); return;
    }
    // The attach-loop repro. The cached LIST lags the live card: it carries the
    // loop card WITHOUT `supervised` (so a render finds the pane stale and tears
    // it down) while the single-card poll above still says supervised (so the
    // watchdog attaches again). `loopListSupervised` flips to the recovered
    // state, where the list agrees the card is attachable and nothing tears it
    // down. Pinned so the row is present either way, the way a real lagging
    // list keeps the row while dropping the live flag.
    if (tasksMode === "worn") { sendJSON(res, { tasks: wornTasks }); return; }
    if (tasksMode === "keepalive") { sendJSON(res, { tasks: KA_CARDS }); return; }
    if (tasksMode === "loop") {
      sendJSON(res, { tasks: [Object.assign({}, LOOP,
        { supervised: loopListSupervised, pinned: true })] });
      return;
    }
    sendJSON(res, { tasks: [tasksMode === "second" ? T2 : T1] });
    return;
  }
  // The runners page. Everything it reads besides runners and fixtures is empty.
  if (url === "/v1/harnesses") { sendJSON(res, { harnesses: [HON, HOFF] }); return; }
  if (url.startsWith("/v1/harnesses/") && req.method === "PUT") {
    switchPut(req, res, [HON, HOFF]); return;
  }
  if (url === "/v1/fixtures") { sendJSON(res, { fixtures: [FON, FOFF] }); return; }
  if (url.startsWith("/v1/fixtures/") && req.method === "PUT") {
    switchPut(req, res, [FON, FOFF]); return;
  }
  if (url === "/v1/hooks") { sendJSON(res, { missing: 0, hooks: [] }); return; }
  if (url === "/v1/sources") { sendJSON(res, { sources: [] }); return; }
  if (url === "/v1/recognisers") { sendJSON(res, { recognisers: [] }); return; }
  if (url === "/v1/actions") { sendJSON(res, { actions: [] }); return; }
  if (url === "/v1/providers") { sendJSON(res, { providers: [] }); return; }
  if (url === "/v1/dispatch") { sendJSON(res, { dispatches: [] }); return; }
  if (url === "/v1/history") {
    if (!histMany) { sendJSON(res, { tasks: [HIST], total: 1 }); return; }
    // Paged the way the store pages, so "show more" and the live re-read ask
    // for the same slices a real room answers.
    const q = new URL(req.url, "http://x").searchParams;
    const all = histFeed();
    const offset = +q.get("offset") || 0, limit = +q.get("limit") || 100;
    sendJSON(res, { tasks: all.slice(offset, offset + limit), total: all.length });
    return;
  }
  if (url === "/v1/waiting") { sendJSON(res, { tasks: [] }); return; }
  if (url === "/v1/permissions") { sendJSON(res, { permissions: tasksMode === "land" ? landPerms : [] }); return; }
  if (url === "/v1/shares") { sendJSON(res, { shares: [] }); return; }
  if (url === "/v1/rooms") { sendJSON(res, { rooms: [] }); return; }
  if (url === "/v1/health") {
    sendJSON(res, { build: healthBuild, settling: false, halted: false }); return;
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
        // The keep-alive's two settings, recorded so the test can read what the
        // switch and the clear button sent.
        if (keys.some(k => k.startsWith("cache_keepalive_"))) {
          kaWrites.push({ url, body });
          if ("cache_keepalive_default" in body) kaSettings.cache_keepalive_default = body.cache_keepalive_default;
          if (body.cache_keepalive_suspended === false) kaSettings.cache_keepalive_suspended = "";
          sendJSON(res, settingsBody(room));
          return;
        }
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
    if (room && roomSettingsDown) {
      res.writeHead(503, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "room " + room + " is not attached" }));
      return;
    }
    settingsReads++;
    if (settingsDelay) { setTimeout(() => sendJSON(res, settingsBody(room)), settingsDelay); return; }
    sendJSON(res, settingsBody(room));
    return;
  }
  if (url === "/v1/themes") { sendJSON(res, { themes: [] }); return; }
  // The plain-daemon stream and the hub's own spelling of it. On a hub the board
  // opens `/v1/events/hub`, and the room-picker test writes a `rooms` event onto
  // that one to make a room attach while the dropdown is open.
  if (url === "/v1/events" || url === "/v1/events/hub" || url.startsWith("/v1/events/room/")) {
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
  // The hub restart gate's board calls. The countdown itself is pushed onto the
  // open streams by the test, the way the hub pushes it. See restartGateSection.
  if (url.startsWith("/_hub/restart")) {
    if (!hubMode) { res.writeHead(404); res.end("not a hub"); return; }
    const sub = url.slice("/_hub/restart".length);
    if (sub === "/input") gateCalls.input++;
    if (sub === "/pause") { gateCalls.pause++; gatePaused = true; }
    if (sub === "/resume") { gateCalls.resume++; gatePaused = false; }
    sendJSON(res, { paused: gatePaused, waiting: false, countdown_left: gateCountdownLeft, boards: 1,
      boot: gateBoot });
    return;
  }
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

// Every worn card on the page, scored in the page. For each one: its computed
// background, and the contrast of its title, its path and every chip against
// what each sits on, with a chip's own wash composited over the card first.
// A room chip only appears with two rooms attached, so one is put on each
// card for the measurement and taken off again.
function measureWorn() {
  const parse = s => {
    let m = /^rgba?\(([\d.]+),\s*([\d.]+),\s*([\d.]+)(?:,\s*([\d.]+))?\)$/.exec(s);
    if (m) return { r: +m[1], g: +m[2], b: +m[3], a: m[4] === undefined ? 1 : +m[4] };
    m = /^color\(srgb ([\d.e-]+) ([\d.e-]+) ([\d.e-]+)(?: \/ ([\d.]+))?\)$/.exec(s);
    if (m) return { r: m[1] * 255, g: m[2] * 255, b: m[3] * 255, a: m[4] === undefined ? 1 : +m[4] };
    return null;
  };
  const over = (top, under) => ({
    r: top.r * top.a + under.r * (1 - top.a), g: top.g * top.a + under.g * (1 - top.a),
    b: top.b * top.a + under.b * (1 - top.a), a: 1
  });
  const lum = c => [c.r, c.g, c.b].map(v => v / 255)
    .map(s => s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4))
    .reduce((sum, v, i) => sum + v * [0.2126, 0.7152, 0.0722][i], 0);
  const ratio = (a, b) => {
    const la = lum(a), lb = lum(b);
    return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
  };
  const where = el => el.closest("#term-list") ? "terminals list"
    : el.closest("#stack-list") ? "stack" : "board";
  const out = [];
  for (const card of document.querySelectorAll(".worn[data-id]")) {
    const cs = getComputedStyle(card);
    const bg = parse(cs.backgroundColor);
    const row = { id: card.dataset.id, where: where(card), bg: cs.backgroundColor, on: card.classList.contains("on"),
      shadow: cs.boxShadow, scores: [] };
    out.push(row);
    if (!bg || bg.a < 1) { row.scores.push({ what: "background", ratio: 0 }); continue; }
    const score = (what, el, floor) => {
      if (!el) return;
      const s = getComputedStyle(el);
      let under = bg;
      const wash = parse(s.backgroundColor);
      if (wash && wash.a > 0) under = over(wash, bg);
      const fg = parse(s.color);
      row.scores.push({ what, floor, ratio: fg ? ratio(over(fg, under), under) : 0, color: s.color });
    };
    const titleEl = card.querySelector(".tname") || card.querySelector(".who > b") || card.querySelector(".title");
    score("title", titleEl, 4.5);
    score("path", card.querySelector(".tpath") || card.querySelector(".who > span"), 4.5);
    card.querySelectorAll(".chip").forEach(c => score("chip " + c.className.replace(/\s+/g, "."), c, 4.5));
    score("star", card.querySelector(".pin.on"), 3);
    const chips = card.querySelector(".chips") || card;
    const room = document.createElement("span");
    room.className = "chip room";
    room.style.setProperty("--rhue", "205");
    room.textContent = "sg4";
    chips.appendChild(room);
    score("room chip", room, 4.5);
    room.remove();
  }
  return out;
}

async function wornSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const wp = await ctx.newPage();
  const errors = [];
  wp.on("pageerror", e => errors.push(String(e)));
  await wp.addInitScript(() => {
    let all = {};
    try { all = JSON.parse(localStorage.getItem("atrium.skipconfirm") || "{}"); } catch (e) {}
    all["width-floor"] = true;
    localStorage.setItem("atrium.skipconfirm", JSON.stringify(all));
  });
  const was = tasksMode;
  try {
    await wp.goto(base, { waitUntil: "domcontentloaded" });
    await wp.waitForSelector("#stack-list .stackrow", { timeout: 15000 });

    // The setting sits in the gear's board pane, beside the other view settings.
    const inBoardPane = await wp.evaluate(() => {
      let el = document.getElementById("s-cardcolors");
      if (!el) return "missing";
      el = el.closest(".field");
      while (el && !(el.matches && el.matches("h3.s-section"))) el = el.previousElementSibling;
      return el ? el.textContent.trim() : "no heading";
    });
    if (inBoardPane !== "board") {
      fail("the card-colours checkbox is not in the gear's board pane (found under: " + inBoardPane + ").");
    }

    // A card per shipped theme, all waiting on you so they land in the stack's
    // default filter, half of them pinned so the star is on some. One more with
    // no theme and a repo, which takes the repo default.
    const names = await wp.evaluate(() => Object.keys(TERM_THEMES));
    wornTasks = names.map((n, i) => Object.assign({}, T1, {
      id: "w-" + n, display_title: "themed " + n, theme: n, supervised: true,
      worktree: "/tmp/worn/" + n, pinned: i % 2 === 0
    }));
    wornTasks.push(Object.assign({}, T1, {
      id: "w-default", display_title: "repo default", supervised: true,
      repo: "dovholuknf/atrium", worktree: "/tmp/worn/atrium"
    }));
    tasksMode = "worn";
    const expect = await wp.evaluate(() => {
      const out = {};
      for (const [n, th] of Object.entries(TERM_THEMES)) out["w-" + n] = th.background;
      out["w-default"] = TERM_THEMES["active-light"].background;
      return out;
    });
    const paintAll = async () => {
      await wp.evaluate(async () => { runRefresh(); await renderTermList(); });
      await wp.waitForFunction(n => document.querySelectorAll('#stack-list .stackrow[data-id^="w-"]').length >= n &&
        document.querySelectorAll('#term-list .card.tab[data-id^="w-"]').length >= n,
        wornTasks.length, { timeout: 15000 });
      await wp.click('.tab[data-view="board"]');
      await wp.waitForFunction(n =>
        document.querySelectorAll('.card:not(.tab)[data-id^="w-"]').length >= n, wornTasks.length, { timeout: 15000 });
      await wp.click('.tab[data-view="stack"]');
    };

    // OFF: the current look. No card is worn, and two cards in different
    // themes are painted the same.
    await paintAll();
    const off = await wp.evaluate(() => {
      const worn = document.querySelectorAll(".worn").length;
      const look = sel => [...document.querySelectorAll(sel)].map(el => {
        const s = getComputedStyle(el);
        return s.backgroundColor + "|" + s.backgroundImage;
      });
      const uniq = a => new Set(a).size;
      return {
        worn,
        // The board's own palette on a card is not the theme's. A board card
        // in a project group carries that group's hue, so the board is checked
        // for the rewrite rather than for one look.
        rewritten: document.querySelectorAll('[data-id^="w-"][style*="--card-0"]').length,
        stack: uniq(look('#stack-list .stackrow[data-id^="w-"]')),
        term: uniq(look('#term-list .card.tab[data-id^="w-"]:not(.on)'))
      };
    });
    if (off.worn || off.rewritten) {
      fail("with the setting off, " + off.worn + " cards are drawn worn and " + off.rewritten +
        " carry a rewritten palette.");
    }
    if (off.stack !== 1 || off.term !== 1) {
      fail("with the setting off, cards in different themes are painted differently " +
        "(distinct looks: stack " + off.stack + ", terminals " + off.term + ").");
    }

    // ON. The board's setting for the stack and the columns, and the terminals
    // list's own for the rows that are not selected.
    await wp.evaluate(() => { toggleCardColors(true); toggleTermWear("idle", true); });
    const stored = await wp.evaluate(() => localStorage.getItem("atrium.cardColors"));
    if (stored !== "1") fail("turning card colours on did not store it in this browser (got " + stored + ").");
    await paintAll();
    // Attach one, so the list has an `.on` card to tell apart.
    await wp.evaluate(async () => { termTask = { id: "w-dracula" }; await renderTermList(); });
    const rows = await wp.evaluate(measureWorn);
    await wp.evaluate(async () => { termTask = null; await renderTermList(); });

    const seen = { "terminals list": new Set(), stack: new Set(), board: new Set() };
    const low = [];
    const hex = s => {
      const m = /rgb\((\d+), (\d+), (\d+)\)/.exec(s || "");
      return m ? "#" + m.slice(1).map(v => (+v).toString(16).padStart(2, "0")).join("") : s;
    };
    for (const r of rows) {
      seen[r.where].add(r.id);
      const want = (expect[r.id] || "").toLowerCase();
      if (hex(r.bg) !== want) {
        fail(r.where + " card " + r.id + " is painted " + r.bg + ", not its theme's background " + want + ".");
      }
      for (const s of r.scores) {
        if (s.ratio < (s.floor || 4.5)) {
          low.push(r.where + " " + r.id + " " + s.what + " " + s.ratio.toFixed(2) + " (" + s.color + ")");
        }
      }
    }
    for (const [where, ids] of Object.entries(seen)) {
      if (ids.size !== wornTasks.length) {
        fail("with the setting on, the " + where + " drew " + ids.size + " worn cards of " + wornTasks.length + ".");
      }
    }
    if (low.length) {
      fail(low.length + " text colours on worn cards read under their floor:\n  " + low.slice(0, 30).join("\n  "));
    }
    const term = rows.filter(r => r.where === "terminals list");
    const on = term.filter(r => r.on);
    const framed = r => /inset/.test(r.shadow || "");
    if (on.length !== 1 || !framed(on[0])) {
      fail("the attached card does not carry its frame when every card is coloured (" +
        on.length + " attached, shadow " + (on[0] && on[0].shadow) + ").");
    }
    if (term.some(r => !r.on && framed(r))) fail("a card that is not attached carries the attached card's frame.");

    // Phone width: the same list, still worn.
    await wp.setViewportSize({ width: 390, height: 780 });
    await wp.click('.tab[data-view="terms"]');
    await wp.evaluate(() => renderTermList());
    const phone = await wp.evaluate(() => {
      const cards = [...document.querySelectorAll('#term-list .card.tab.worn[data-id^="w-"]')];
      const vis = cards.filter(c => c.getBoundingClientRect().width > 0);
      return { worn: cards.length, visible: vis.length,
        bg: vis[0] ? getComputedStyle(vis[0]).backgroundColor : "", id: vis[0] ? vis[0].dataset.id : "" };
    });
    if (!phone.visible || hex(phone.bg) !== (expect[phone.id] || "").toLowerCase()) {
      fail("at phone width the terminals list does not draw worn cards (" + JSON.stringify(phone) + ").");
    }

    // OFF again puts the current look back.
    // A view repaints when it is shown, so walk the three.
    await wp.setViewportSize({ width: 1400, height: 900 });
    await wp.evaluate(() => { toggleCardColors(false); toggleTermWear("idle", false); });
    for (const v of ["board", "stack", "terms"]) {
      await wp.click('.tab[data-view="' + v + '"]');
      await wp.waitForTimeout(300);
    }
    await wp.waitForFunction(() => !document.querySelector(".worn"), null, { timeout: 15000 }).catch(async () => {
      const left = await wp.evaluate(() => document.querySelectorAll(".worn").length);
      fail("turning card colours back off left " + left + " cards worn.");
    });
    if (errors.length) fail("the card-colours page threw uncaught errors: " + errors.join(" | "));
  } finally {
    tasksMode = was;
    await ctx.close();
  }
}

// The terminals list's three switches: the selected row, the rest, and an
// exited row. Each is flipped on its own and the rows read back, so a switch
// that moved another kind of row fails here.
async function termWearSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const wp = await ctx.newPage();
  const errors = [];
  wp.on("pageerror", e => errors.push(String(e)));
  await wp.addInitScript(() => {
    let all = {};
    try { all = JSON.parse(localStorage.getItem("atrium.skipconfirm") || "{}"); } catch (e) {}
    all["width-floor"] = true;
    localStorage.setItem("atrium.skipconfirm", JSON.stringify(all));
  });
  const was = tasksMode;
  const live = (id, theme) => Object.assign({}, T1, {
    id, display_title: "row " + id, theme, supervised: true, pinned: true, worktree: "/tmp/tw/" + id
  });
  const dead = (id, theme) => Object.assign(live(id, theme), { status: "dead", supervised: false, pid: 0 });
  try {
    wornTasks = [live("tw-atrium", "atrium"), live("tw-nord", "nord"), live("tw-light", "active-light"),
      dead("tw-dead", "dracula"), dead("tw-deadlight", "active-light")];
    tasksMode = "worn";
    await wp.goto(base, { waitUntil: "domcontentloaded" });
    await wp.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    await wp.click('.tab[data-view="terms"]');

    // Defaults, as the gear shows them: selected on, the other two off.
    const boxes = await wp.evaluate(() => {
      paintSettings();
      return ["selected", "idle", "exited"].map(k => {
        const el = document.getElementById("s-termwear-" + k);
        return el ? k + "=" + el.checked : k + " missing";
      }).join(" ");
    });
    if (boxes !== "selected=true idle=false exited=false") {
      fail("the terminals list's switches do not open at their defaults in the gear: " + boxes);
    }

    const read = () => wp.evaluate(async () => {
      termTask = { id: "tw-atrium" };
      await renderTermList();
      const out = {};
      for (const el of document.querySelectorAll('#term-list .card.tab[data-id^="tw-"]')) {
        const s = getComputedStyle(el), a = getComputedStyle(el, "::after");
        out[el.dataset.id] = {
          cls: el.className, bg: s.backgroundColor, img: s.backgroundImage, opacity: s.opacity, filter: s.filter,
          after: a.content !== "none" ? { opacity: a.opacity, bg: a.backgroundColor } : null
        };
      }
      out.shell = getComputedStyle(document.getElementById("term-list")).backgroundColor;
      return out;
    });
    const themeBg = await wp.evaluate(() => {
      const hex = h => { const n = parseInt(h.slice(1), 16); return `rgb(${n >> 16}, ${(n >> 8) & 255}, ${n & 255})`; };
      const o = {};
      for (const n of ["atrium", "nord", "active-light", "dracula"]) o[n] = hex(TERM_THEMES[n].background);
      return o;
    });
    const worn = r => / worn\b/.test(r.cls);

    // DEFAULT: the selected row in its terminal's background, nothing else worn,
    // the exited rows faded to grey the way they always were.
    let r = await read();
    if (r["tw-atrium"].bg !== themeBg.atrium || worn(r["tw-atrium"])) {
      fail("by default the selected row is not in its theme's background (" + r["tw-atrium"].bg + ").");
    }
    if (["tw-nord", "tw-light", "tw-dead", "tw-deadlight"].some(id => worn(r[id]))) {
      fail("by default a row that is not selected wears its theme.");
    }
    if (r["tw-dead"].opacity !== "0.3" || r["tw-dead"].after) {
      fail("by default an exited row is not faded (" + JSON.stringify(r["tw-dead"]) + ").");
    }
    const plain = r["tw-nord"].bg + "|" + r["tw-nord"].img;

    // SELECTED OFF: the attached row is the skin's card.
    await wp.evaluate(() => toggleTermWear("selected", false));
    r = await read();
    if (!/\bbare\b/.test(r["tw-atrium"].cls) || r["tw-atrium"].bg + "|" + r["tw-atrium"].img !== plain) {
      fail("with the selected row's theme off, it is still coloured (" + r["tw-atrium"].bg + " " +
        r["tw-atrium"].img + ", a plain row is " + plain + ").");
    }
    await wp.evaluate(() => toggleTermWear("selected", true));

    // IDLE ON: every live row in its own background, the dead ones still grey.
    await wp.evaluate(() => toggleTermWear("idle", true));
    r = await read();
    if (r["tw-nord"].bg !== themeBg.nord || r["tw-light"].bg !== themeBg["active-light"] || !worn(r["tw-nord"])) {
      fail("with idle rows on, a row that is not selected is not in its theme (" + r["tw-nord"].bg + ").");
    }
    if (r["tw-atrium"].bg !== themeBg.atrium) fail("with idle rows on, the selected row lost its theme.");
    // An exited row does not follow `idle`: with its own switch off it is the
    // skin's card, faded, so it cannot read as a live row in colour.
    if (worn(r["tw-dead"]) || r["tw-dead"].bg + "|" + r["tw-dead"].img !== plain ||
        r["tw-dead"].opacity !== "0.3" || r["tw-dead"].after) {
      fail("with idle rows on and exited rows off, an exited row is not the skin's card faded (" +
        JSON.stringify(r["tw-dead"]) + ").");
    }

    // EXITED ON: the dead rows in their theme under the same fade every exited
    // row gets, which is the pale, still-tinted look. No wash laid over them.
    // With idle rows on and off, since the two are independent.
    await wp.evaluate(() => toggleTermWear("exited", true));
    for (const idleOn of [true, false]) {
      await wp.evaluate(v => toggleTermWear("idle", v), idleOn);
      r = await read();
      for (const [id, theme] of [["tw-dead", "dracula"], ["tw-deadlight", "active-light"]]) {
        const d = r[id];
        if (!worn(d) || d.bg !== themeBg[theme] || d.opacity !== "0.3" || d.after) {
          fail("with exited rows on (idle " + idleOn + "), " + id + " is not its theme faded (" +
            JSON.stringify(d) + ").");
        }
      }
      if (!idleOn && worn(r["tw-nord"])) fail("turning exited rows on coloured a live row.");
    }
    const stored = await wp.evaluate(() => ["selected", "idle", "exited"]
      .map(k => localStorage.getItem("atrium.termWear." + k)).join(","));
    if (stored !== "1,0,1") fail("the terminals list's switches are not kept in this browser (got " + stored + ").");

    // For checking by eye: TERMWEAR_SHOTS=<dir> writes the list on a dark and
    // a light skin with the exited switch on and off, idle rows on.
    if (process.env.TERMWEAR_SHOTS) {
      await wp.evaluate(() => toggleTermWear("idle", true));
      for (const skin of ["midnight", "daylight", "paper", "noir"]) {
        await wp.evaluate(s => applySkin(s), skin);
        for (const on of [true, false]) {
          await wp.evaluate(v => { toggleTermWear("exited", v); termTask = { id: "tw-atrium" }; renderTermList(); }, on);
          await wp.waitForTimeout(400);
          await wp.locator("#term-list").screenshot({
            path: require("path").join(process.env.TERMWEAR_SHOTS, skin + "-exited-" + (on ? "on" : "off") + ".png")
          });
        }
      }
      await wp.evaluate(() => { toggleTermWear("idle", false); toggleTermWear("exited", true); });
    }

    // A browser that had the old all-cards setting on starts with idle rows on.
    const legacy = await browser.newContext();
    const lp = await legacy.newPage();
    await lp.addInitScript(() => localStorage.setItem("atrium.cardColors", "1"));
    await lp.goto(base, { waitUntil: "domcontentloaded" });
    const idle = await lp.evaluate(() => termWearOn.idle);
    await legacy.close();
    if (idle !== true) fail("a browser with the old card colours setting on did not start with idle rows worn.");

    if (errors.length) fail("the terminals list switches page threw uncaught errors: " + errors.join(" | "));
  } finally {
    tasksMode = was;
    await ctx.close();
  }
}

// The attached row bridges the divider into the terminal: a strip in the row's
// background with the row's border along its top and bottom, reaching the
// pane's edge. A pinned row filed into a group is drawn twice, and both copies
// are the attached row, so both get the frame and a bridge of their own.
async function bridgeSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const bp = await ctx.newPage();
  const errors = [];
  bp.on("pageerror", e => errors.push(String(e)));
  await bp.addInitScript(() => {
    localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
    localStorage.setItem("atrium.grouping", JSON.stringify({ on: true, mode: "custom", groups: ["active"] }));
  });
  const was = tasksMode;
  tasksMode = "filed";
  try {
    await bp.goto(base, { waitUntil: "domcontentloaded" });
    await bp.waitForTimeout(900);
    await bp.click('.tab[data-view="terms"]');
    await bp.waitForSelector('#term-list .tnest[data-group="active"] .card.tab[data-id="filed1"]',
      { state: "attached", timeout: 15000 });
    // Two ways a row is drawn: in the skin's colours with its theme on the
    // selected row alone, and with every row worn, which frames the selected one.
    for (const [skin, idle] of [["noir", false], ["noir", true], ["daylight", false], ["daylight", true]]) {
      const got = await bp.evaluate(async ([skin, idle]) => {
        applySkin(skin);
        toggleTermWear("idle", idle);
        termTask = { id: "filed1" };
        // Stand-in for an attached terminal. The bridge only asks that there is one.
        term = term || { stub: true };
        await renderTermList();
        await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));
        placeTabBridge();
        const pe = document.getElementById("term-pane"), ps = getComputedStyle(pe);
        const pane = { left: pe.getBoundingClientRect().left, bl: ps.borderLeftWidth,
          bw: ps.borderTopWidth + "/" + ps.borderBottomWidth, bc: ps.borderLeftColor };
        const bridges = [...document.querySelectorAll("#term-layout .tabbridge")].filter(b => !b.hidden)
          .map(b => {
            const r = b.getBoundingClientRect(), s = getComputedStyle(b);
            return { top: r.top, bottom: r.bottom, left: r.left, right: r.right,
              bw: s.borderTopWidth + "/" + s.borderBottomWidth,
              bc: s.borderTopColor + "/" + s.borderBottomColor, bg: s.backgroundColor };
          });
        const cards = [...document.querySelectorAll('#term-list .card.tab.on[data-id="filed1"]')].map(c => {
          const r = c.getBoundingClientRect(), s = getComputedStyle(c);
          // The frame the eye sees along the top and bottom: the border plus any
          // inset shadow laid inside it there.
          let top = 0, bottom = 0;
          for (const m of s.boxShadow.matchAll(/(-?[\d.]+)px (-?[\d.]+)px [\d.]+px(?: -?[\d.]+px)? inset/g)) {
            const y = parseFloat(m[2]);
            if (y > 0) top = Math.max(top, y); else bottom = Math.max(bottom, -y);
          }
          return { top: r.top, bottom: r.bottom, right: r.right, bc: s.borderTopColor, bg: s.backgroundColor,
            shadow: s.boxShadow, cls: c.className,
            fw: (parseFloat(s.borderTopWidth) + top) + "px/" + (parseFloat(s.borderBottomWidth) + bottom) + "px" };
        });
        return { pane, bridges, cards };
      }, [skin, idle]);
      const where = "(" + skin + ", idle rows " + (idle ? "worn" : "plain") + ")";
      if (process.env.BRIDGE_SHOTS) {
        await bp.screenshot({ path: require("path").join(process.env.BRIDGE_SHOTS,
          "bridge-" + skin + "-" + (idle ? "worn" : "plain") + ".png") });
      }
      if (got.cards.length !== 2) {
        fail("the pinned, filed attached row is not drawn twice as the selected row " + where + ": " +
          got.cards.length);
        continue;
      }
      const [a, b] = got.cards;
      if (a.bc !== b.bc || a.bg !== b.bg || a.shadow !== b.shadow) {
        fail("the two copies of the attached row are framed differently " + where + ": " + JSON.stringify(got.cards));
      }
      for (const [i, c] of got.cards.entries()) {
        const br = got.bridges.find(x => Math.abs(x.top - c.top) < 1 && Math.abs(x.bottom - c.bottom) < 1);
        if (!br) {
          fail("copy " + (i + 1) + " of the attached row has no bridge into the terminal " + where + ".");
          continue;
        }
        // The bridge's edge is as thick as the row's frame, or the frame steps where it crosses the divider.
        if (br.bw !== c.fw) {
          fail("copy " + (i + 1) + "'s bridge edge is " + br.bw + " and the row's frame " + c.fw + " " + where + ".");
        }
        if (br.bc !== c.bc + "/" + c.bc || /rgba\(0, 0, 0, 0\)/.test(br.bc)) {
          fail("copy " + (i + 1) + "'s bridge does not carry the row's border " + where + ": " + br.bw + " " +
            br.bc + ", the row's is " + c.bc + ".");
        }
        if (br.bg !== c.bg) fail("copy " + (i + 1) + "'s bridge is not the row's background " + where + ".");
        // The terminal's frame is as thick as the bridge running into it, and the
        // bridge covers the pane's left edge, or a line crosses where they meet.
        if (got.pane.bw !== br.bw || got.pane.bl !== br.bw.split("/")[0]) {
          fail("copy " + (i + 1) + "'s bridge edge is " + br.bw + " and the terminal's frame " + got.pane.bw +
            ", " + got.pane.bl + " on the left " + where + ".");
        }
        if (/ worn\b/.test(c.cls) && got.pane.bc !== c.bc) {
          fail("the terminal's frame is " + got.pane.bc + " and the worn row's " + c.bc + " " + where + ".");
        }
        if (br.right < got.pane.left + parseFloat(got.pane.bl)) {
          fail("copy " + (i + 1) + "'s bridge stops at " + br.right + ", inside the terminal's left frame, which " +
            "ends at " + (got.pane.left + parseFloat(got.pane.bl)) + " " + where + ".");
        }
        if (br.left > c.right || br.right < got.pane.left) {
          fail("copy " + (i + 1) + "'s bridge does not run from the row to the terminal's edge " + where + ": " +
            JSON.stringify(br) + ", row right " + c.right + ", pane left " + got.pane.left + ".");
        }
      }
    }
    if (errors.length) fail("the bridge page threw uncaught errors: " + errors.join(" | "));
  } finally {
    tasksMode = was;
    await ctx.close();
  }
}

// A group made with `+ new group` on the terminals list is removed from its
// heading's right-click menu. Its cards go back to where they sit without it,
// with their tags kept, and nothing is closed: no card is written to at all.
async function groupRemoveSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const gp = await ctx.newPage();
  const errors = [];
  gp.on("pageerror", e => errors.push(String(e)));
  const writes = [];
  gp.on("request", r => { if (r.method() !== "GET" && /\/v1\/tasks/.test(r.url())) writes.push(r.method() + " " + r.url()); });
  await gp.addInitScript(() => {
    localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
    localStorage.setItem("atrium.grouping",
      JSON.stringify({ on: true, mode: "custom", groups: ["active", "spare"] }));
  });
  const was = tasksMode;
  tasksMode = "filed";
  try {
    await gp.goto(base, { waitUntil: "domcontentloaded" });
    await gp.waitForTimeout(900);
    await gp.click('.tab[data-view="terms"]');
    await gp.waitForSelector('#term-list .tnest[data-group="active"] .card.tab[data-id="filed1"]',
      { state: "attached", timeout: 15000 });
    const heads = () => gp.evaluate(() =>
      [...document.querySelectorAll("#term-list .tgroup .tgname")].map(e => e.textContent.trim()));
    const remove = async name => {
      await gp.locator("#term-list .tgroup", { hasText: name }).first().click({ button: "right" });
      const item = gp.locator("#cardmenu button", { hasText: "remove from the view" });
      if (!(await item.count())) { fail("the " + name + " group's heading has no remove item on its menu."); return; }
      await item.click();
      await gp.waitForTimeout(400);
    };

    // The empty group: gone from the list and from the saved view.
    await remove("spare");
    let h = await heads();
    if (h.includes("spare")) fail("an empty group removed from the terminals list is still drawn: " + h.join(", "));

    // The group with a card in it: gone, and the card back where it sits
    // without the group, still pinned and still carrying its tag.
    await remove("active");
    h = await heads();
    const state = await gp.evaluate(() => ({
      groups: groupingPrefs().groups,
      inBucket: !!document.querySelector('#term-list .termbucket .card.tab[data-id="filed1"]'),
      copies: document.querySelectorAll('#term-list .card.tab[data-id="filed1"]').length,
      loose: !!document.querySelector('#term-list .card.tab[data-id="loose1"]')
    }));
    if (h.includes("active") || JSON.stringify(state.groups) !== "[]") {
      fail("removing a group from the terminals list left it (" + h.join(", ") + "; saved " +
        JSON.stringify(state.groups) + ").");
    }
    if (!state.inBucket || state.copies !== 1 || !state.loose) {
      fail("removing a group did not put its cards back where they sit without it: " + JSON.stringify(state));
    }
    if (writes.length) fail("removing a group wrote to a card: " + writes.join(" | "));
    // A heading that is not a group of yours has no menu.
    const stray = await gp.evaluate(() =>
      [...document.querySelectorAll("#term-list .tgroup")].filter(b => b.hasAttribute("oncontextmenu") &&
        !b.classList.contains("pinnedhead")).map(b => b.textContent.trim()));
    if (stray.length) fail("a heading that is not one of your groups takes the group menu: " + stray.join(", "));
    if (errors.length) fail("the group remove page threw uncaught errors: " + errors.join(" | "));
  } finally {
    tasksMode = was;
    await ctx.close();
  }
}

// A group's colour is `groupHue`, kept in this browser's grouping prefs. A
// recolour made from the terminals pane has to land on the stack, the board and
// the strip, on every skin and with card colours either way, survive a reload,
// and reach a second window without waiting for its poll. The strip's headings
// drew in the label grey whatever the hue was, which was the break.
async function groupColorSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const cp = await ctx.newPage();
  const errors = [];
  cp.on("pageerror", e => errors.push(String(e)));
  // Seeded once. Written on every load, it would undo the recolour the reload
  // is there to check.
  await cp.addInitScript(() => {
    localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
    if (!localStorage.getItem("atrium.grouping")) {
      localStorage.setItem("atrium.grouping",
        JSON.stringify({ on: true, mode: "custom", groups: ["active", "spare"] }));
    }
  });
  const was = tasksMode;
  tasksMode = "filed";
  const hues = page => page.evaluate(() => {
    const hue = sel => {
      const el = document.querySelector(sel);
      return el ? getComputedStyle(el).getPropertyValue("--ghue").trim() : "missing";
    };
    const tn = document.querySelector('#term-list .tgroup[data-ghead="active"] .tgname');
    return {
      stack: hue('.stackgroup[data-morph-key="stack:active"] .gname'),
      board: hue('.cardgroup.project[data-fold="proj:active"] .gname'),
      term: hue('#term-list .tgroup[data-ghead="active"]'),
      nest: hue('#term-list .tnest[data-group="active"]'),
      termColor: tn ? getComputedStyle(tn).color : "missing",
      label: getComputedStyle(document.querySelector("#term-list .tgroup.pinnedhead")).color
    };
  });
  const recolor = async (page, hue) => {
    await page.locator('#term-list .tgroup[data-ghead="active"]').click({ button: "right" });
    await page.locator("#cardmenu button", { hasText: "recolor" }).click();
    await page.locator(`#ask-body .swatch[data-hue="${hue}"]`).click();
    await page.waitForTimeout(500);
  };
  try {
    await cp.goto(base, { waitUntil: "domcontentloaded" });
    await cp.waitForTimeout(900);
    await cp.click('.tab[data-view="terms"]');
    await cp.waitForSelector('#term-list .tgroup[data-ghead="active"]', { timeout: 15000 });
    await recolor(cp, 140);
    // A hidden view repaints when it is shown, so each is visited.
    for (const skin of ["harbour", "daylight", "website"]) {
      for (const cc of [false, true]) {
        await cp.evaluate(([s, c]) => { applySkin(s); toggleCardColors(c); }, [skin, cc]);
        await cp.click('.tab[data-view="board"]');
        await cp.waitForTimeout(500);
        await cp.click('.tab[data-view="stack"]');
        await cp.waitForTimeout(500);
        await cp.click('.tab[data-view="terms"]');
        await cp.waitForTimeout(300);
        const h = await hues(cp);
        if (h.stack !== "140" || h.board !== "140" || h.term !== "140" || h.nest !== "140") {
          fail(`a group recoloured from the terminals pane did not land everywhere on ${skin}, ` +
            `card colours ${cc ? "on" : "off"}: ` + JSON.stringify(h));
        }
        if (h.termColor === h.label) {
          fail(`a terminals pane group heading is still drawn in the label colour on ${skin}: ` + JSON.stringify(h));
        }
      }
    }
    await cp.reload({ waitUntil: "domcontentloaded" });
    await cp.waitForSelector('#term-list .tgroup[data-ghead="active"]', { state: "attached", timeout: 15000 });
    await cp.waitForSelector('.stackgroup[data-morph-key="stack:active"]', { state: "attached", timeout: 15000 });
    const after = await hues(cp);
    if (after.term !== "140" || after.stack !== "140") fail("a group colour did not survive a reload: " + JSON.stringify(after));

    // A second window, on the same browser. Well inside `POLL_MS`, so only the
    // storage event can have carried it.
    const cp2 = await ctx.newPage();
    cp2.on("pageerror", e => errors.push(String(e)));
    await cp2.goto(base, { waitUntil: "domcontentloaded" });
    await cp2.waitForSelector('#term-list .tgroup[data-ghead="active"]', { state: "attached", timeout: 15000 });
    await cp2.waitForTimeout(600);
    await cp.click('.tab[data-view="terms"]');
    await cp.waitForTimeout(300);
    await recolor(cp, 250);
    await cp2.waitForTimeout(1200);
    const seen = await hues(cp2);
    await cp2.click('.tab[data-view="stack"]');
    await cp2.waitForTimeout(500);
    const other = Object.assign(await hues(cp2), { termBeforeSwitch: seen.term });
    if (seen.term !== "250" || other.stack !== "250") {
      fail("a group recoloured in one window did not reach a second window: " + JSON.stringify(other));
    }
    if (errors.length) fail("the group colour page threw uncaught errors: " + errors.join(" | "));
  } finally {
    tasksMode = was;
    await ctx.close();
  }
}

// Dragging a group heading on the terminals pane reorders your groups, and is
// never mistaken for dragging a row. Taking a card out of a group is a menu
// entry on every surface and a drop on `untagged` in the strip.
async function groupDragSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const dp = await ctx.newPage();
  const errors = [];
  dp.on("pageerror", e => errors.push(String(e)));
  const writes = [];
  dp.on("request", r => {
    if (r.method() === "PATCH" && /\/v1\/tasks\//.test(r.url())) writes.push(r.url() + " " + r.postData());
  });
  await dp.addInitScript(() => {
    localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
    if (!localStorage.getItem("atrium.grouping")) {
      localStorage.setItem("atrium.grouping",
        JSON.stringify({ on: true, mode: "custom", groups: ["active", "spare"] }));
    }
  });
  const was = tasksMode;
  tasksMode = "filed";
  const heads = () => dp.evaluate(() =>
    [...document.querySelectorAll("#term-list .tgroup")].map(b =>
      b.classList.contains("pinnedhead") ? "*pinned" : b.querySelector(".tgname").textContent.trim()));
  try {
    await dp.goto(base, { waitUntil: "domcontentloaded" });
    await dp.waitForTimeout(900);
    await dp.click('.tab[data-view="terms"]');
    await dp.waitForSelector('#term-list .tgroup[data-ghead="spare"]', { timeout: 15000 });

    // A heading dragged above another moves the whole group and writes the order.
    await dp.locator('#term-list .tgroup[data-ghead="spare"]').dragTo(
      dp.locator('#term-list .tgroup[data-ghead="active"]'), { targetPosition: { x: 10, y: 2 } });
    await dp.waitForTimeout(600);
    await dp.click('.tab[data-view="stack"]');
    await dp.waitForTimeout(500);
    await dp.click('.tab[data-view="terms"]');
    await dp.waitForTimeout(300);
    let st = await dp.evaluate(() => ({
      groups: groupingPrefs().groups,
      stack: [...document.querySelectorAll(".stackgroup")].map(d => d.dataset.morphKey),
      nestAfterHead: (() => {
        const h = document.querySelector('#term-list .tgroup[data-ghead="active"]');
        return !!(h && h.nextElementSibling && h.nextElementSibling.dataset.group === "active");
      })()
    }));
    if (JSON.stringify(st.groups) !== '["spare","active"]') {
      fail("dragging a group heading above another did not reorder the groups: " + JSON.stringify(st.groups));
    }
    if (st.stack.indexOf("stack:spare") > st.stack.indexOf("stack:active")) {
      fail("the stack did not follow a group reordered on the terminals pane: " + st.stack.join(", "));
    }
    if (!st.nestAfterHead) fail("a group's rows did not travel with its heading.");
    let h = await heads();
    if (h[0] !== "*pinned") fail("pinned is no longer on top after a group was dragged: " + h.join(", "));
    if (writes.length) fail("reordering groups wrote to a card: " + writes.join(" | "));

    // A row dropped on a heading is a row drag, and reorders nothing.
    await dp.locator('#term-list .card.tab[data-id="loose1"]').dragTo(
      dp.locator('#term-list .tgroup[data-ghead="spare"]'));
    await dp.waitForTimeout(500);
    st = await dp.evaluate(() => groupingPrefs().groups);
    if (JSON.stringify(st) !== '["spare","active"]') fail("a row dropped on a heading reordered the groups: " + JSON.stringify(st));
    // And a heading dropped on the pinned bucket pins nothing.
    await dp.locator('#term-list .tgroup[data-ghead="active"]').dragTo(dp.locator("#term-list .termbucket"));
    await dp.waitForTimeout(500);
    if (writes.some(w => /pinned/.test(w))) fail("a heading dropped on the pinned bucket pinned something: " + writes.join(" | "));
    writes.length = 0;

    // A row dragged from a group onto `untagged` leaves that group.
    await dp.locator('#term-list .tnest[data-group="active"] .card.tab[data-id="filed1"]').dragTo(
      dp.locator('#term-list .tnest[data-ungroup] .card.tab[data-id="loose1"]'));
    await dp.waitForTimeout(600);
    if (!writes.some(w => /filed1/.test(w) && /"tags":\[\]/.test(w))) {
      fail("a row dragged out of its group onto untagged did not lose the group's tag: " + writes.join(" | "));
    }
    writes.length = 0;

    // The same, from the card's menu.
    await dp.locator('#term-list .tnest[data-group="active"] .card.tab[data-id="filed1"]').click({ button: "right" });
    const out = dp.locator("#cardmenu button", { hasText: "out of active" });
    await out.waitFor({ timeout: 5000 }).catch(() => {});
    if (!(await out.count())) fail("a card in a group has no `out of` entry on its menu.");
    else {
      await out.click();
      await dp.waitForTimeout(500);
      if (!writes.some(w => /filed1/.test(w) && /"tags":\[\]/.test(w))) {
        fail("`out of active` did not take the tag off: " + writes.join(" | "));
      }
    }

    // Outside custom mode a heading does not drag and says where ordering lives.
    await dp.evaluate(() => { setGrouping({ mode: "recency" }); });
    await dp.waitForTimeout(600);
    const tag = await dp.evaluate(() => [...document.querySelectorAll("#term-list .tgroup:not(.pinnedhead)")]
      .map(b => ({ drag: b.draggable, grip: !!b.querySelector(".tgrip"), title: b.dataset.tip })));
    if (tag.some(t => t.drag || t.grip)) fail("a heading drags outside the custom grouping: " + JSON.stringify(tag));
    if (!tag.some(t => /custom grouping/.test(t.title))) {
      fail("a heading outside custom mode does not say where reordering lives: " + JSON.stringify(tag));
    }
    if (errors.length) fail("the group drag page threw uncaught errors: " + errors.join(" | "));
  } finally {
    tasksMode = was;
    await ctx.close();
  }
}

// The website skin is the one skin that carries rules beyond a palette: a
// gradient primary button, a frosted header and a glow behind the board. Every
// one is scoped to that skin, and the failure this guards is one leaking out, so
// harbour is measured before the skin is worn and again after, and must match
// both times, and a second skin must not pick any of it up either.
async function websiteSkinSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const wp = await ctx.newPage();
  const errors = [];
  wp.on("pageerror", e => errors.push(String(e)));
  try {
    await wp.goto(base, { waitUntil: "domcontentloaded" });
    await wp.waitForSelector("button.go.newagent", { state: "visible", timeout: 15000 });
    // Buttons carry `transition: all`, so a colour read straight after a skin
    // change is partway there.
    const look = skin => wp.evaluate(async skin => {
      applySkin(skin);
      await new Promise(r => setTimeout(r, 400));
      const go = getComputedStyle(document.querySelector("button.go.newagent"));
      const hd = getComputedStyle(document.querySelector("header"));
      const bd = getComputedStyle(document.body);
      const root = getComputedStyle(document.documentElement);
      const vars = ["--teal", "--blue", "--bg-0", "--bg-1", "--bg-2", "--card-0", "--stroke", "--head", "--body"];
      return {
        goBg: go.backgroundImage, goColor: go.color, goShadow: go.boxShadow, goBorder: go.borderTopColor,
        hdBg: hd.backgroundImage + " " + hd.backgroundColor, hdFilter: hd.backdropFilter,
        hdBorder: hd.borderBottomColor, bodyBg: bd.backgroundImage,
        palette: vars.map(v => v + "=" + root.getPropertyValue(v).trim()).join(" ")
      };
    }, skin);
    const harbour = await look("harbour");
    const site = await look("website");
    const noir = await look("noir");
    const harbourAgain = await look("harbour");

    if (JSON.stringify(harbourAgain) !== JSON.stringify(harbour)) {
      fail("wearing the website skin left harbour changed: " + JSON.stringify(harbour) + " then " +
        JSON.stringify(harbourAgain));
    }
    if (harbour.goBg !== "none" || /saturate/.test(harbour.hdFilter) || /radial/.test(harbour.bodyBg)) {
      fail("harbour wears a website effect: " + JSON.stringify(harbour));
    }
    if (noir.goBg !== "none" || /saturate/.test(noir.hdFilter) || /radial/.test(noir.bodyBg)) {
      fail("noir wears a website effect: " + JSON.stringify(noir));
    }
    // The skin itself: harbour's palette, plus the three effects.
    if (site.palette !== harbour.palette) {
      fail("the website skin's palette is not harbour's: " + site.palette + " vs " + harbour.palette);
    }
    if (!/linear-gradient/.test(site.goBg) || site.goColor !== "rgb(4, 18, 29)" || !/rgba\(0, 227, 176/.test(site.goShadow)) {
      fail("the website skin's primary button is not the gradient on its glow: " + JSON.stringify(site));
    }
    if (!/saturate\(1\.6\)|saturate\(160%\)/.test(site.hdFilter)) {
      fail("the website skin's header is not frosted: " + site.hdFilter);
    }
    if ((site.bodyBg.match(/radial-gradient/g) || []).length !== 2) {
      fail("the website skin has no glow behind the board: " + site.bodyBg);
    }
    // A disabled primary button still has to look like it cannot do anything.
    const disabled = await wp.evaluate(() => {
      applySkin("website");
      const b = document.createElement("button");
      b.className = "go"; b.disabled = true; b.textContent = "x";
      document.body.appendChild(b);
      const bg = getComputedStyle(b).backgroundImage;
      b.remove();
      return bg;
    });
    if (disabled !== "none") fail("a disabled primary button in the website skin keeps its gradient: " + disabled);

    // For checking by eye: WEBSITE_SHOTS=<dir> writes the board in the skin.
    if (process.env.WEBSITE_SHOTS) {
      for (const s of ["website", "harbour"]) {
        await wp.evaluate(s => applySkin(s), s);
        await wp.waitForTimeout(300);
        await wp.screenshot({ path: path.join(process.env.WEBSITE_SHOTS, "board-" + s + ".png") });
      }
    }
    if (errors.length) fail("the website skin page threw uncaught errors: " + errors.join(" | "));
  } finally {
    await ctx.close();
  }
}

// A load asks `/v1/settings` from several places, and on a hub those asks land
// inside the first one's round trip. They share it: one read per load, and every
// reader still gets the answer (the skin is worn).
async function settingsOnceSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const sp = await ctx.newPage();
  const wasHub = hubMode;
  hubMode = true;
  settingsDelay = 135;
  settingsReads = 0;
  try {
    await sp.goto(base, { waitUntil: "domcontentloaded" });
    await sp.waitForTimeout(2500);
    if (settingsReads !== 1) fail("a board load read /v1/settings " + settingsReads + " times, not once.");
    const skin = await sp.evaluate(() => document.documentElement.getAttribute("data-skin"));
    if (skin !== skinFor[""]) {
      fail("with the settings read shared, the skin did not land (got " + skin + ", want " + skinFor[""] + ").");
    }
    // A read made after the first has answered asks again: nothing finished is kept.
    const before = settingsReads;
    await sp.evaluate(() => api("/v1/settings"));
    if (settingsReads !== before + 1) fail("a settings read after the load did not reach the daemon.");
  } finally {
    hubMode = wasHub;
    settingsDelay = 0;
    await ctx.close();
  }
}

// The hub restart gate, the board's half. The hub's events are written onto the
// open streams the way the hub writes them, and the board's three calls are
// counted by the mock. See docs/hub-restart-gate.md.
async function restartGateSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const gp = await ctx.newPage();
  const errors = [];
  gp.on("pageerror", e => errors.push(String(e)));
  const wasHub = hubMode;
  hubMode = true;
  gatePaused = false;
  Object.assign(gateCalls, { input: 0, pause: 0, resume: 0 });
  const say = state => {
    const line = "event: hub-restart\ndata: " + JSON.stringify(state) + "\n\n";
    openStreams.forEach(r => { try { if (!r.destroyed) r.write(line); } catch (e) {} });
  };
  const countdownText = () => gp.evaluate(() => {
    const el = document.querySelector(".toast.hubgate b");
    return el ? el.textContent : "";
  });
  try {
    await gp.goto(base, { waitUntil: "domcontentloaded" });
    await gp.waitForFunction(() => typeof hubIsHub !== "undefined" && hubIsHub &&
      document.getElementById("conn").classList.contains("live"), null, { timeout: 15000 });

    // Input is reported, and throttled.
    await gp.mouse.click(700, 450);
    await gp.keyboard.press("Shift");
    await gp.waitForTimeout(300);
    if (gateCalls.input !== 1) fail("a click and a key inside the throttle reported input " +
      gateCalls.input + " times, not once.");

    // The countdown shows and counts.
    say({ state: "countdown", seconds: 5 });
    await gp.waitForFunction(() => !!document.querySelector(".toast.hubgate"), null, { timeout: 5000 })
      .catch(() => fail("a countdown from the hub drew no toast."));
    const first = await countdownText();
    if (!/atrium restarts in [45]s/.test(first)) {
      fail("the countdown toast said: " + first);
    }
    await gp.waitForTimeout(1300);
    const later = await countdownText();
    if (!/in [34]s/.test(later)) fail("the countdown did not count down: " + first + " then " + later);

    // A click on it pauses, and is not reported as input.
    const inputBefore = gateCalls.input;
    await gp.waitForTimeout(3100);
    await gp.click(".toast.hubgate");
    await gp.waitForTimeout(300);
    if (gateCalls.pause !== 1) fail("clicking the countdown called pause " + gateCalls.pause + " times.");
    if (gateCalls.input !== inputBefore) fail("clicking the countdown was also reported as input.");
    if (await gp.$(".toast.hubgate")) fail("the countdown toast stayed up after it was clicked.");

    // The hub says paused: a sticky toast with a resume button, which survives
    // the stack filling up.
    say({ state: "paused" });
    await gp.waitForFunction(() => {
      const el = document.querySelector(".toast.hubgate");
      return el && /restart on hold/.test(el.textContent);
    }, null, { timeout: 5000 }).catch(() => fail("a pause from the hub drew no paused toast."));
    await gp.evaluate(() => { for (let i = 0; i < 5; i++) toast("filler " + i, "pushing the stack"); });
    await gp.waitForTimeout(100);
    if (!(await gp.$(".toast.hubgate .hubgate-act"))) fail("the paused toast was pushed off the stack.");
    await gp.waitForTimeout(9500);
    if (!(await gp.$(".toast.hubgate"))) fail("the paused toast timed out like an ordinary one.");
    await gp.click(".toast.hubgate .hubgate-act");
    await gp.waitForTimeout(300);
    if (gateCalls.resume !== 1) fail("the resume button called resume " + gateCalls.resume + " times.");
    if (await gp.$(".toast.hubgate")) fail("the paused toast stayed up after resume.");

    // A window that opens while a pause is held shows it without an event.
    gatePaused = true;
    const late = await ctx.newPage();
    await late.goto(base, { waitUntil: "domcontentloaded" });
    await late.waitForFunction(() => {
      const el = document.querySelector(".toast.hubgate");
      return el && /restart on hold/.test(el.textContent);
    }, null, { timeout: 15000 }).catch(() => fail("a board opened during a pause did not show it."));
    await late.close();
    gatePaused = false;

    // THE COUNTDOWN STAYS until the hub says what comes next: not the toast cap,
    // not a removal, not its own clock running out, not a stream reopen.
    const hasCountdown = () => gp.evaluate(() =>
      [...document.querySelectorAll(".toast.hubgate")].some(el => /atrium restarts in/.test(el.textContent)));
    say({ state: "countdown", seconds: 3 });
    await gp.waitForFunction(() => !!document.querySelector(".toast.hubgate"), null, { timeout: 5000 })
      .catch(() => fail("the second countdown drew no toast."));
    await gp.evaluate(() => { for (let i = 0; i < 5; i++) toast("filler " + i, "pushing the stack"); });
    await gp.waitForTimeout(100);
    if (!(await hasCountdown())) fail("the countdown toast was pushed off the stack.");
    const plainCount = await gp.evaluate(() => document.querySelectorAll("#toasts .toast:not(.sticky)").length);
    if (plainCount !== 3) fail("with a countdown up the stack held " + plainCount + " ordinary toasts, not 3.");
    await gp.evaluate(() => document.querySelector(".toast.hubgate").remove());
    await gp.waitForTimeout(100);
    if (!(await hasCountdown())) fail("the countdown toast stayed gone after something removed it.");
    await gp.waitForTimeout(9500);
    if (!(await hasCountdown())) fail("the countdown toast went away on a timer.");
    // Every stream back, not only the one `#conn` watches. The hub's own stream
    // reopens on its own clock, and an event said before it is back is said to
    // nobody: the "cancelled countdown stayed on screen" flake.
    const streamsWere = openStreams.filter(r => !r.destroyed).length;
    openStreams.forEach(r => { try { r.destroy(); } catch (e) {} });
    await gp.waitForFunction(() => document.getElementById("conn").classList.contains("live"), null,
      { timeout: 15000 }).catch(() => fail("the stream did not come back."));
    for (const end = Date.now() + 15000;
      openStreams.filter(r => !r.destroyed).length < streamsWere && Date.now() < end;) {
      await gp.waitForTimeout(50);
    }
    await gp.waitForTimeout(500);
    if (!(await hasCountdown())) fail("the countdown toast went away when the stream reopened.");
    say({ state: "cancelled" });
    await gp.waitForFunction(() => !document.querySelector(".toast.hubgate"), null, { timeout: 5000 })
      .catch(() => fail("a cancelled countdown stayed on screen."));

    // On a phone the cap is one, and a paused toast is on top of it, not in it.
    await gp.setViewportSize({ width: 400, height: 800 });
    say({ state: "paused" });
    await gp.waitForFunction(() => !!document.querySelector(".toast.hubgate"), null, { timeout: 5000 })
      .catch(() => fail("a pause on a phone drew no toast."));
    await gp.evaluate(() => { for (let i = 0; i < 3; i++) toast("phone filler " + i, "pushing the stack"); });
    await gp.waitForTimeout(100);
    const phone = await gp.evaluate(() => ({
      paused: !!document.querySelector(".toast.hubgate .hubgate-act"),
      plain: document.querySelectorAll("#toasts .toast:not(.sticky)").length
    }));
    if (!phone.paused || phone.plain !== 1) {
      fail("on a phone the stack was " + JSON.stringify(phone) + ", not the paused toast and one other.");
    }
    say({ state: "resumed" });
    await gp.waitForFunction(() => !document.querySelector(".toast.hubgate"), null, { timeout: 5000 })
      .catch(() => fail("a resume did not take the paused toast down."));
    await gp.setViewportSize({ width: 1400, height: 900 });

    // A window opened during a countdown shows what is left of it.
    gateCountdownLeft = 30;
    const mid = await ctx.newPage();
    await mid.goto(base, { waitUntil: "domcontentloaded" });
    await mid.waitForFunction(() => {
      const el = document.querySelector(".toast.hubgate");
      return el && /atrium restarts in (29|30)s/.test(el.textContent);
    }, null, { timeout: 15000 }).catch(() => fail("a board opened during a countdown did not show it."));
    await mid.close();
    gateCountdownLeft = 0;

    // The cover is a wait card, and a wait card sets its own display, which
    // must not draw it while it is shut.
    if (await gp.evaluate(() => getComputedStyle(document.getElementById("hubrestart")).display !== "none")) {
      fail("the restarting cover is drawn while it is closed.");
    }

    // Restarting: a modal that Escape and closing every dialog both leave up.
    say({ state: "restarting" });
    await gp.waitForFunction(() => document.getElementById("hubrestart").open, null, { timeout: 5000 })
      .catch(() => fail("the hub restarting drew no modal."));
    // It says how long it has been, and it wears the skin: its card is the
    // palette's own card colour, whatever skin is on.
    await gp.waitForTimeout(1200);
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
    await gp.waitForTimeout(200);
    if (!(await gp.evaluate(() => document.getElementById("hubrestart").open))) {
      fail("the restarting modal came down while the hub was still away.");
    }
    // The hub goes and a new one answers: the stream reopens onto a hub with a
    // new name and the modal clears. See restartStaysSection for the old hub.
    gateBoot = "boot-a2";
    openStreams.forEach(r => { try { r.destroy(); } catch (e) {} });
    await gp.waitForFunction(() => !document.getElementById("hubrestart").open, null, { timeout: 15000 })
      .catch(() => fail("the restarting modal did not clear when the stream came back."));
    if (errors.length) fail("the restart gate page threw uncaught errors: " + errors.join(" | "));
  } finally {
    hubMode = wasHub;
    gatePaused = false;
    gateCountdownLeft = 0;
    gateBoot = "boot-a";
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
  const wasHub = hubMode;
  hubMode = true;
  gatePaused = false;
  gateBoot = "boot-a";
  healthBuild = "test";
  const say = state => {
    const line = "event: hub-restart\ndata: " + JSON.stringify(state) + "\n\n";
    openStreams.forEach(r => { try { if (!r.destroyed) r.write(line); } catch (e) {} });
  };
  const drop = () => openStreams.splice(0).forEach(r => { try { r.destroy(); } catch (e) {} });
  const coverUp = () => gp.evaluate(() => document.getElementById("hubrestart").open);
  const live = () => gp.waitForFunction(() => document.getElementById("conn").classList.contains("live"),
    null, { timeout: 15000 });
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
    await gp.goto(base, { waitUntil: "domcontentloaded" });
    await gp.waitForFunction(() => typeof hubIsHub !== "undefined" && hubIsHub &&
      document.getElementById("conn").classList.contains("live"), null, { timeout: 15000 });
    await gp.waitForTimeout(500);

    // Countdown to cover, with no frame between them.
    say({ state: "countdown", seconds: 1 });
    await gp.waitForFunction(() => !!document.querySelector(".toast.hubgate"), null, { timeout: 5000 })
      .catch(() => fail("a countdown drew no toast."));
    await watch();
    await gp.waitForTimeout(1500);
    if (!(await gp.$(".toast.hubgate"))) fail("the countdown toast went at 0 before the hub said restarting.");
    say({ state: "restarting" });
    await gp.waitForFunction(() => document.getElementById("hubrestart").open, null, { timeout: 5000 })
      .catch(() => fail("restarting drew no cover."));
    if (await gaps()) fail("between the countdown and the cover a frame showed neither: " + (await seen()));

    // The old hub's stream blips and comes back before the old hub goes. Same
    // hub, so the cover stays.
    drop();
    await live().catch(() => fail("the stream did not come back after a blip."));
    await gp.waitForTimeout(1000);
    if (!(await coverUp())) fail("the cover came down when the OLD hub's stream reopened.");

    // Nothing else on the board takes it down: a view switch, the terminals
    // pane redrawing, a refresh, every dialog being closed.
    await gp.evaluate(async () => {
      switchView("terms");
      if (typeof renderTerms === "function") await renderTerms();
      switchView("stack");
      refreshSoon();
      await closeOpenDialogs();
      document.getElementById("hubrestart").close();
    });
    await gp.waitForTimeout(1500);
    if (!(await coverUp())) fail("the cover came down on a view switch, a terminals redraw or a dialog close.");

    // The old hub goes. The cover holds while nothing answers.
    hubAway = true;
    drop();
    await gp.waitForTimeout(2500);
    if (!(await coverUp())) fail("the cover came down while the hub was away.");

    // The new hub answers on the same build: the cover clears, and not before.
    gateBoot = "boot-b";
    hubAway = false;
    await live().catch(() => fail("the stream did not come back from the new hub."));
    await gp.waitForFunction(() => !document.getElementById("hubrestart").open, null, { timeout: 10000 })
      .catch(() => fail("the cover did not clear once the new hub answered."));
    if ((await seen()) !== "tc-") fail("from the countdown to the new hub the screen went " + (await seen()) +
      ", not countdown, cover, clear.");
    await gp.evaluate(() => cancelAnimationFrame(window.__watch));

    // THE RELOAD PATH. The hub restarts onto a new board build, so the board
    // reloads. The cover is up when the reloaded page first paints and comes
    // down once that page has the new hub.
    say({ state: "countdown", seconds: 1 });
    await gp.waitForTimeout(1300);
    say({ state: "restarting" });
    await gp.waitForFunction(() => document.getElementById("hubrestart").open, null, { timeout: 5000 })
      .catch(() => fail("the second restarting drew no cover."));
    hubAway = true;
    drop();
    await gp.waitForTimeout(1500);
    const reloaded = gp.waitForEvent("domcontentloaded", { timeout: 20000 });
    gateBoot = "boot-c";
    healthBuild = "test2";
    hubAway = false;
    let sawCoverWithReload = false;
    await reloaded.then(async () => {
      sawCoverWithReload = await gp.evaluate(() => document.getElementById("hubrestart").open);
    }).catch(() => fail("a new build after the restart did not reload the board."));
    if (!sawCoverWithReload) fail("the board reloaded onto the new build with the cover down.");
    await gp.waitForFunction(() => !document.getElementById("hubrestart").open, null, { timeout: 15000 })
      .catch(() => fail("the cover did not clear after the reload settled."));

    // A window that missed `restarting` (its stream was reconnecting when the
    // hub said it) keeps the countdown at 0 while the hub is away, and the
    // cover takes over from it when a different hub answers.
    await live().catch(() => fail("the reloaded board never went live."));
    await gp.waitForTimeout(500);
    say({ state: "countdown", seconds: 1 });
    await gp.waitForFunction(() => !!document.querySelector(".toast.hubgate"), null, { timeout: 5000 })
      .catch(() => fail("the reloaded board drew no countdown."));
    await watch();
    await gp.waitForTimeout(1300);
    hubAway = true;
    drop();
    await gp.waitForTimeout(2500);
    if (!(await gp.$(".toast.hubgate"))) fail("a countdown that ran out went while the hub was away.");
    gateBoot = "boot-d";
    hubAway = false;
    await gp.waitForFunction(() => window.__seen.endsWith("c-"), null, { timeout: 15000 })
      .catch(() => {});
    if ((await seen()) !== "tc-") fail("over a missed restarting the screen went " + (await seen()) +
      ", not countdown, cover, clear.");
    if (errors.length) fail("the restart-stays page threw: " + errors.join(" | "));
  } finally {
    hubMode = wasHub;
    hubAway = false;
    gateBoot = "boot-a";
    healthBuild = "test";
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
  const wasHub = hubMode;
  hubMode = true;
  gateBoot = "boot-a";
  const drop = () => openStreams.splice(0).forEach(r => { try { r.destroy(); } catch (e) {} });
  const downUp = () => dp.evaluate(() => { const d = document.getElementById("atriumdown"); return !!(d && d.open); });
  const live = () => dp.waitForFunction(() => document.getElementById("conn").classList.contains("live"),
    null, { timeout: 15000 });
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
    await dp.goto(base, { waitUntil: "domcontentloaded" });
    await dp.waitForFunction(() => typeof hubIsHub !== "undefined" && hubIsHub &&
      document.getElementById("conn").classList.contains("live"), null, { timeout: 15000 });
    await dp.waitForTimeout(500);

    // A blip is not atrium being down.
    await watchDown();
    drop();
    await live().catch(() => fail("the stream did not come back after a blip."));
    await dp.waitForTimeout(6000);
    if (await dp.evaluate(() => window.__downSeen)) fail("a stream blip put the down cover up.");

    // Atrium stops with nothing said: nothing for five seconds, then the cover.
    hubAway = true;
    drop();
    await dp.waitForTimeout(3000);
    if (await downUp()) fail("the down cover went up before five seconds.");
    await dp.waitForFunction(() => document.getElementById("atriumdown").open, null, { timeout: 8000 })
      .catch(() => fail("atrium stopping with nothing said put no down cover up."));
    await dp.waitForTimeout(1200);
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
    await dp.waitForTimeout(300);
    if (!(await downUp())) fail("the down cover came down while atrium was still away.");

    // Atrium answers: the cover comes down on its own.
    hubAway = false;
    await dp.waitForFunction(() => !document.getElementById("atriumdown").open, null, { timeout: 15000 })
      .catch(() => fail("the down cover did not come down once atrium answered."));
    await live().catch(() => fail("the stream did not come back after atrium did."));

    // A planned restart is the restart cover's, however long the hub is away.
    const say = state => {
      const line = "event: hub-restart\ndata: " + JSON.stringify(state) + "\n\n";
      openStreams.forEach(r => { try { if (!r.destroyed) r.write(line); } catch (e) {} });
    };
    await dp.waitForTimeout(500);
    await watchDown();
    say({ state: "countdown", seconds: 1 });
    await dp.waitForTimeout(1300);
    say({ state: "restarting" });
    await dp.waitForFunction(() => document.getElementById("hubrestart").open, null, { timeout: 5000 })
      .catch(() => fail("restarting drew no restart cover."));
    hubAway = true;
    drop();
    await dp.waitForTimeout(8000);
    if (await dp.evaluate(() => window.__downSeen)) fail("a planned restart put the down cover up.");
    gateBoot = "boot-b";
    hubAway = false;
    await dp.waitForFunction(() => !document.getElementById("hubrestart").open, null, { timeout: 15000 })
      .catch(() => fail("the restart cover did not clear on the new hub."));
    if (await dp.evaluate(() => window.__downSeen)) fail("the down cover showed as the planned restart ended.");
    await dp.evaluate(() => cancelAnimationFrame(window.__downWatch));
  } finally {
    await ctx.close();
  }

  // THE RELOAD. A worker has to be installed and holding the offline page first,
  // which only a page with atrium up can do.
  serveSW = true;
  const sctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const sp = await sctx.newPage();
  sp.on("pageerror", e => errors.push(String(e)));
  try {
    await sp.goto(base, { waitUntil: "domcontentloaded" });
    const ready = await sp.waitForFunction(async () => {
      if (!navigator.serviceWorker || !navigator.serviceWorker.controller) return false;
      return !!(await caches.match("/down.html"));
    }, null, { timeout: 15000, polling: 250 }).then(() => true, () => false);
    if (!ready) {
      fail("the service worker did not take the page and keep down.html.");
    } else {
      await sp.waitForFunction(() => document.getElementById("conn").classList.contains("live"), null,
        { timeout: 15000 }).catch(() => {});
      const card0 = await sp.evaluate(() =>
        getComputedStyle(document.documentElement).getPropertyValue("--card-0").trim());
      hubAway = true;
      drop();
      await sp.reload({ waitUntil: "load", timeout: 15000 })
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
      await sp.waitForTimeout(1200);
      const clock = await sp.evaluate(() => document.getElementById("atriumdown-el").textContent).catch(() => "");
      if (!/^down for \d+s$/.test(clock)) fail("down.html's clock said " + JSON.stringify(clock));
      // Atrium comes back: the page reloads onto the board by itself.
      hubAway = false;
      await sp.waitForFunction(() => !!document.getElementById("hubrestart"), null, { timeout: 15000 })
        .catch(() => fail("down.html did not reload onto the board once atrium answered."));
      await sp.waitForFunction(() => !document.getElementById("atriumdown").open, null, { timeout: 15000 })
        .catch(() => fail("the board came back from down.html with the down cover stuck up."));

      // A share in front of a stopped atrium answers 502: the same page.
      page502 = true;
      await sp.reload({ waitUntil: "load", timeout: 15000 }).catch(() => {});
      const title = await sp.evaluate(() => document.title).catch(() => "");
      if (title !== "atrium is not running") fail("a 502 on reload showed " + JSON.stringify(title) + ", not down.html.");
      page502 = false;
      await sp.waitForFunction(() => !!document.getElementById("hubrestart"), null, { timeout: 15000 })
        .catch(() => fail("down.html over a 502 did not reload onto the board once the page answered."));

      // A reload during a PLANNED restart says restarting, not down, and the
      // board it reloads onto puts the restart cover back until the new hub.
      await sp.waitForFunction(() => document.getElementById("conn").classList.contains("live"), null,
        { timeout: 15000 }).catch(() => fail("the board after down.html never went live."));
      await sp.waitForTimeout(500);
      const say = state => {
        const line = "event: hub-restart\ndata: " + JSON.stringify(state) + "\n\n";
        openStreams.forEach(r => { try { if (!r.destroyed) r.write(line); } catch (e) {} });
      };
      say({ state: "countdown", seconds: 1 });
      await sp.waitForTimeout(1300);
      say({ state: "restarting" });
      await sp.waitForFunction(() => document.getElementById("hubrestart").open, null, { timeout: 5000 })
        .catch(() => fail("restarting drew no restart cover before the reload."));
      hubAway = true;
      drop();
      await sp.reload({ waitUntil: "load", timeout: 15000 }).catch(() => {});
      const planned = await sp.evaluate(() => ({
        title: document.title, text: document.body.textContent.replace(/\s+/g, " ")
      })).catch(() => ({ title: "", text: "" }));
      if (planned.title !== "atrium is restarting" || /not running/.test(planned.text)) {
        fail("a reload during a planned restart showed " + JSON.stringify(planned.title) + ": " + planned.text);
      }
      gateBoot = "boot-z";
      hubAway = false;
      const back = await sp.waitForFunction(() => {
        const d = document.getElementById("hubrestart");
        return d && d.open;
      }, null, { timeout: 15000, polling: 50 }).then(() => true, () => false);
      if (!back) fail("the board after a restart-time reload did not put the restart cover back.");
      await sp.waitForFunction(() => !document.getElementById("hubrestart").open &&
        !document.getElementById("atriumdown").open, null, { timeout: 15000 })
        .catch(() => fail("the restart cover did not clear after the restart-time reload."));
    }
    if (errors.length) fail("the atrium-down pages threw: " + errors.join(" | "));
  } finally {
    serveSW = false;
    page502 = false;
    hubAway = false;
    hubMode = wasHub;
    gateBoot = "boot-a";
    await sctx.close();
  }
}

// A TOAST STAYS FOR ITS WHOLE LIFE. An alert about something nobody answers (a
// card arriving, a stuck step, a share that stopped) was keyed by its subject,
// and the poll that raised it reaped every keyed toast not waiting or pending,
// so it popped and went at once. A pending one still goes when it is answered.
async function toastStaysSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const tp = await ctx.newPage();
  const errors = [];
  tp.on("pageerror", e => errors.push(String(e)));
  try {
    await tp.goto(base, { waitUntil: "domcontentloaded" });
    await tp.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    await tp.evaluate(() => {
      window.__reaps = 0;
      const real = reapToasts;
      window.reapToasts = keys => { window.__reaps++; return real(keys); };
      document.getElementById("toasts").innerHTML = "";
      // The shape `check("arrived")` raises for one new card.
      alerting.notify("new card is on the board", "a new card", "stack", "", "arrived9", "arrived9", "");
      // And a card waiting on you, which is answered.
      alerting.notify("ready card is ready", "its turn ended", "stack", "", "ready9", "ready9", "", "",
        { pending: true });
    });
    const has = title => tp.evaluate(t => [...document.querySelectorAll("#toasts .toast:not(.leaving)")]
      .some(el => el.querySelector("b").textContent === t), title);
    const born = Date.now();
    await tp.waitForFunction(() => window.__reaps >= 1, null, { timeout: 12000 })
      .catch(() => fail("no poll reaped toasts, so the test did not exercise the bug."));
    await tp.waitForTimeout(400);
    if (!(await has("new card is on the board"))) {
      fail("an arrival toast was taken down by the poll after it, " + (Date.now() - born) + "ms in.");
    }
    if (await has("ready card is ready")) fail("a toast for a card no longer waiting was not reaped.");
    await tp.waitForTimeout(Math.max(0, 8500 - (Date.now() - born)));
    if (!(await has("new card is on the board"))) fail("an arrival toast went before its 9 seconds.");
    await tp.waitForTimeout(1200);
    if (await has("new card is on the board")) fail("an arrival toast outlived its 9 seconds.");
    if (errors.length) fail("the toast page threw uncaught errors: " + errors.join(" | "));
  } finally {
    await ctx.close();
  }
}

// THE BOARD'S OWN TOOLTIP, NOT THE BROWSER'S. A native `title` draws a white box
// in the system font whatever the skin, so every one became a `data-tip` that
// `#tip` draws. Checked on a toolbar button, a chip on a stack card and a row on
// the terminals pane, in two skins, and then the rendered board is searched for
// any `title` left over. `scripts/check-titles.sh` guards the source, this
// guards what the source draws.
async function tooltipSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const tp = await ctx.newPage();
  const errors = [];
  tp.on("pageerror", e => errors.push(String(e)));
  // The first visible, non-empty `data-tip` under a selector, marked so the
  // pointer can be sent to it.
  const mark = sel => tp.evaluate(sel => {
    document.querySelectorAll("[data-tipprobe]").forEach(e => e.removeAttribute("data-tipprobe"));
    const el = [...document.querySelectorAll(sel)].find(e => {
      const r = e.getBoundingClientRect();
      return e.dataset.tip && r.width > 0 && r.height > 0;
    });
    if (!el) return null;
    el.setAttribute("data-tipprobe", "1");
    return el.dataset.tip;
  }, sel);
  const tipNow = () => tp.evaluate(() => {
    const t = document.getElementById("tip");
    const cs = getComputedStyle(t);
    const r = t.getBoundingClientRect();
    return { on: t.classList.contains("on"), text: t.textContent, bg: cs.backgroundColor, color: cs.color,
      font: cs.fontFamily, left: r.left, right: r.right, top: r.top, bottom: r.bottom,
      w: innerWidth, h: innerHeight };
  });
  // What the skin says the panel should wear, resolved by the browser the same way.
  const want = () => tp.evaluate(() => {
    const p = document.createElement("div");
    p.style.cssText = "background: var(--shell-0); color: var(--body); font-family: var(--sans)";
    document.body.appendChild(p);
    const cs = getComputedStyle(p);
    const out = { bg: cs.backgroundColor, color: cs.color, font: cs.fontFamily };
    p.remove();
    return out;
  });
  const hover = async (sel, what) => {
    const text = await mark(sel);
    if (!text) { fail("no " + what + " with a data-tip to hover (" + sel + ")."); return null; }
    await tp.mouse.move(2, 890);
    await tp.waitForTimeout(100);
    await tp.hover("[data-tipprobe]");
    await tp.waitForTimeout(150);
    if ((await tipNow()).on) fail("the tooltip on " + what + " came up before the hover delay.");
    await tp.waitForTimeout(600);
    const t = await tipNow();
    if (!t.on) fail("hovering " + what + " did not bring up the styled tooltip.");
    else if (t.text !== text) fail("the tooltip on " + what + " said " + JSON.stringify(t.text) +
      ", not its data-tip " + JSON.stringify(text));
    else if (t.left < 0 || t.top < 0 || t.right > t.w || t.bottom > t.h) {
      fail("the tooltip on " + what + " went off screen: " + JSON.stringify(t));
    }
    return t;
  };
  // The filed set has supervised cards, so the terminals pane has rows.
  const was = tasksMode;
  tasksMode = "filed";
  try {
    await tp.goto(base, { waitUntil: "domcontentloaded" });
    await tp.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    await tp.waitForTimeout(500);

    for (const skin of ["harbour", "daylight"]) {
      await tp.evaluate(s => applySkin(s), skin);
      await tp.waitForTimeout(300);
      const w = await want();
      const t = await hover("header #gear", "the header's gear button");
      if (t && (t.bg !== w.bg || t.color !== w.color || t.font !== w.font)) {
        fail("in " + skin + " the tooltip wore " + JSON.stringify({ bg: t.bg, color: t.color, font: t.font }) +
          ", not the skin's " + JSON.stringify(w));
      }
      // Leaving takes it down.
      await tp.mouse.move(700, 890);
      await tp.waitForTimeout(100);
      if ((await tipNow()).on) fail("in " + skin + " the tooltip stayed up after the pointer left.");
    }
    await tp.evaluate(() => applySkin("harbour"));

    await hover("#stack-list .stackrow [data-tip]", "a chip on a stack card");
    // A scroll that is not the terminal's takes it down.
    await tp.evaluate(() => window.dispatchEvent(new Event("scroll")));
    if ((await tipNow()).on) fail("a scroll left the tooltip up.");

    // A click on a button is not keyboard focus, and does not pin its tooltip up.
    await mark("header #sound");
    await tp.click("[data-tipprobe]");
    await tp.waitForTimeout(700);
    if ((await tipNow()).on) fail("clicking the sound button left its tooltip up.");
    await tp.click("[data-tipprobe]");
    // Keyboard focus shows it at once.
    await tp.mouse.move(700, 890);
    await tp.keyboard.press("Shift");
    await tp.evaluate(() => document.getElementById("gear").focus());
    await tp.waitForTimeout(50);
    const kb = await tipNow();
    if (!kb.on || kb.text !== "sound settings") fail("keyboard focus on the gear did not show its tooltip: " +
      JSON.stringify(kb));
    await tp.evaluate(() => document.activeElement.blur());

    await tp.click('.tab[data-view="terms"]');
    await tp.waitForSelector("#term-list .card.tab", { timeout: 15000 });
    await tp.waitForTimeout(300);
    await hover("#term-list .card.tab [data-tip]", "a row on the terminals pane");
    await tp.mouse.move(700, 890);

    // NOTHING DRAWN STILL CARRIES A NATIVE TITLE, on any of the main views.
    const left = [];
    for (const v of ["stack", "board", "terms", "history", "runners", "perms"]) {
      await tp.click('.tab[data-view="' + v + '"]').catch(() => {});
      await tp.waitForTimeout(400);
      left.push(...await tp.evaluate(v => [...document.querySelectorAll("[title]")]
        .map(e => v + ": <" + e.tagName.toLowerCase() + " class=\"" + e.className + "\" title=\"" +
          e.getAttribute("title") + "\">"), v));
    }
    if (left.length) fail("the rendered board still has native titles: " + [...new Set(left)].join(" | "));
    if (errors.length) fail("the tooltip page threw uncaught errors: " + errors.join(" | "));
  } finally {
    tasksMode = was;
    await ctx.close();
  }
}

// A POPPED-OUT CARD STAYS SPOKEN FOR ACROSS A ROOM-SET CHANGE. The hub tags a
// card `room~id` while more than one room is attached and serves it bare with
// one, and a restart re-attaches rooms one at a time. A popped-out window keeps
// the spelling in its hash while the board re-resolves to the current one, so
// the board held `sgg~s1` while the window claimed `s1`. The ownership ledger
// compared raw ids, so the board read the card as free and attached it, and the
// window's claim never matched the board's pane, so neither let go.
async function popoutTagFlipSection(browser, base) {
  // The restart section above leaves the card poll answering 404.
  soloMode = "ok";
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const bp = await ctx.newPage();
  const errors = [];
  bp.on("pageerror", e => errors.push(String(e)));
  if (process.env.DEBUG_HEADLESS) bp.on("console", m => console.error("[flip] " + m.text()));
  try {
    await bp.goto(base, { waitUntil: "domcontentloaded" });
    await bp.waitForTimeout(1000);
    await bp.click('.tab[data-view="terms"]');
    // A stand-in popped-out window on the shared bus, holding the card under
    // one spelling and answering every roll call with it.
    const standIn = spelling => bp.evaluate(s => {
      if (window.__flipSolo) window.__flipSolo.close();
      window.__flipSolo = new BroadcastChannel("atrium-solo");
      window.__flipSolo.onmessage = e => {
        if ((e.data || {}).type === "solo-who") window.__flipSolo.postMessage({ type: "solo-claim", task: s });
      };
      window.__flipSolo.postMessage({ type: "solo-claim", task: s });
    }, spelling);

    // Claimed bare, asked tagged, and the other way round.
    await standIn("s1");
    await bp.waitForTimeout(200);
    if (!(await bp.evaluate(() => poppedOut("sgg~s1")))) {
      fail("a card popped out as `s1` read as free once the board spelled it `sgg~s1`.");
    }
    await standIn("sgg~s1");
    await bp.evaluate(() => soloHeld.clear());
    await bp.evaluate(() => runRefresh());
    await bp.waitForTimeout(200);
    if (!(await bp.evaluate(() => poppedOut("s1")))) {
      fail("a card popped out as `sgg~s1` read as free once the board spelled it `s1`.");
    }

    // The restore after a restart: the board comes back waiting for its card
    // under the new spelling while the window holds the old one. It must not
    // attach.
    await bp.evaluate(() => soloHeld.clear());
    await standIn("s1");
    await bp.waitForTimeout(200);
    // Counted at `openTerm`, because the list render tears a pane down again
    // when the mocked list does not carry the card, which would hide the attach.
    await bp.evaluate(() => {
      window.__flipOpened = [];
      const real = openTerm;
      window.__flipRealOpen = real;
      openTerm = t => { window.__flipOpened.push(t.id); return real(t); };
      waitAndAttach("sgg~s1");
    });
    await bp.waitForTimeout(1500);
    const restored = await bp.evaluate(() => {
      openTerm = window.__flipRealOpen;
      return window.__flipOpened;
    });
    if (restored.length) fail("the board's restore attached " + restored.join(", ") + ", a card popped out as `s1`.");

    // The board already has the pane under the tagged spelling when the window
    // claims it bare. The popped-out window wins: the board lets go.
    await bp.evaluate(() => { if (window.__flipSolo) window.__flipSolo.close(); window.__flipSolo = null; });
    await bp.evaluate(() => soloHeld.clear());
    await bp.evaluate(async () => openTerm(await api("/v1/tasks/sgg~s1")));
    await bp.waitForFunction(() => termTask && termTask.id === "sgg~s1", null, { timeout: 5000 });
    await standIn("s1");
    let yielded = false;
    try {
      await bp.waitForFunction(() => !termTask, null, { timeout: 3000 });
      yielded = true;
    } catch (e) {}
    if (!yielded) {
      fail("the board kept its pane on `sgg~s1` after a window claimed `s1`: two views on one terminal.");
    }
    if (errors.length) fail("the tag-flip page threw: " + errors.join(" | "));
  } finally {
    await ctx.close();
  }
}

// AN IDLE BOARD IS NEAR IDLE, with a popped-out window and a second tab open.
//
// Nobody touches anything. A board polls every ten seconds and a popped-out
// window heartbeats its claim, so a healthy browser makes a handful of requests
// a second across all three documents and repaints about as often. A loop
// between documents (a claim answered with a claim, a storage write answered
// with a write) shows up as a rate many times that, and the browser's six
// connections per host saturate behind it. Then the room goes silent: see below.
async function idleRateSection(browser, base) {
  const wasHub = hubMode, wasSgg = sggAttached;
  hubMode = true;
  sggAttached = true;
  soloMode = "ok";
  tasksMode = "first";
  const secs = +(process.env.IDLE_RATE_SECONDS || 12);
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const errors = [];
  const counts = new Map();
  ctx.on("request", r => {
    const u = new URL(r.url());
    if (u.pathname.startsWith("/vendor/") || u.pathname === "/") return;
    let who = "sw";
    try { who = r.frame().page().__who || "?"; } catch (e) {}
    const k = who + " " + r.method() + " " + u.pathname;
    counts.set(k, (counts.get(k) || 0) + 1);
  });
  await ctx.addInitScript(() => {
    window.__atriumMut = 0;
    new MutationObserver(ms => { window.__atriumMut += ms.length; })
      .observe(document, { childList: true, subtree: true, attributes: true, characterData: true });
  });
  const open = async (who, hash) => {
    const p = await ctx.newPage();
    p.__who = who;
    p.on("pageerror", e => errors.push(who + ": " + e));
    if (process.env.DEBUG_HEADLESS) p.on("console", m => console.error("[" + who + "] " + m.text()));
    await p.goto(base + (hash || ""), { waitUntil: "domcontentloaded" });
    return p;
  };
  try {
    const board = await open("board");
    await board.waitForTimeout(1500);
    await board.click('.tab[data-view="terms"]').catch(() => {});
    const solo = await open("solo", "#term=sgg~s1");
    const tab2 = await open("tab2");
    await board.waitForTimeout(3000);
    // The spelling flip: sgg drops and comes back, so the board re-resolves
    // `sgg~s1` to `s1` and back while the window keeps the spelling it opened on.
    counts.clear();
    const pages = [board, solo, tab2];
    const mut0 = await Promise.all(pages.map(p => p.evaluate(() => window.__atriumMut)));
    for (let i = 0; i < secs; i++) {
      if (i === 2 || i === 6) {
        sggAttached = !sggAttached;
        hubStreams.forEach(r => { try { r.write("event: rooms\ndata: {}\n\n"); } catch (e) {} });
      }
      await board.waitForTimeout(1000);
    }
    const mut1 = await Promise.all(pages.map(p => p.evaluate(() => window.__atriumMut)));
    const total = [...counts.values()].reduce((a, b) => a + b, 0);
    const rate = total / secs;
    const muts = pages.map((p, i) => (mut1[i] - mut0[i]) / secs);
    if (process.env.IDLE_RATE_REPORT) {
      console.log("requests/s " + rate.toFixed(1) + ", mutations/s board " + muts[0].toFixed(0) +
        " solo " + muts[1].toFixed(0) + " tab2 " + muts[2].toFixed(0));
      [...counts.entries()].sort((a, b) => b[1] - a[1]).slice(0, 15)
        .forEach(([k, n]) => console.log("  " + n + "  " + k));
    }
    // Three documents, each polling a dozen endpoints every ten seconds, plus a
    // room flip that re-reads everything twice. Around four a second is healthy.
    if (rate > 12) {
      fail("an idle board, a second tab and a popped-out window made " + rate.toFixed(1) +
        " requests a second: " + [...counts.entries()].sort((a, b) => b[1] - a[1]).slice(0, 4)
          .map(([k, n]) => n + " " + k).join(", "));
    }
    // A ROOM THE HUB COUNTS AS ATTACHED THAT DOES NOT ANSWER, which is what a
    // hub-only restart left for minutes. Every proxied read is held. Unbounded,
    // the board's queue grew by a pass's worth every thirty seconds (7 by 33s,
    // 9 by 54s) and nothing left the browser. Bounded, a held read gives its
    // slot back and the queue stays within one pass.
    const stall = +(process.env.IDLE_RATE_STALL || 40);
    if (stall) {
      stalledCount = 0;
      roomStallUntil = Date.now() + stall * 1000;
      let most = 0, where = "";
      for (let i = 0; i < stall; i++) {
        await board.waitForTimeout(1000);
        const qs = await Promise.all(pages.map(p => p.evaluate(() => apiQueue.length)));
        qs.forEach((n, j) => { if (n > most) { most = n; where = pages[j].__who + " at " + (i + 1) + "s"; } });
        if (process.env.IDLE_RATE_REPORT && i % 5 === 4) {
          console.log("  stall t+" + (i + 1) + "s queued " + qs.join(" ") + ", reads held " + stalledCount);
        }
      }
      roomStallUntil = 0;
      if (most > 4) {
        fail("a room that stopped answering queued " + most + " board fetches (" + where +
          "): a held read is keeping its slot instead of timing out.");
      }
      await board.waitForTimeout(5000);
      const after = await Promise.all(pages.map(p => p.evaluate(() => apiInflight + apiQueue.length)));
      if (after.some(n => n > 0)) fail("the board still had fetches out 5s after the room answered: " + after.join(" "));
    }
    if (errors.length) fail("the idle-rate pages threw: " + errors.join(" | "));
  } finally {
    roomStallUntil = 0;
    await ctx.close();
    hubMode = wasHub;
    sggAttached = wasSgg;
  }
}

// A GROUP YOU OPEN STAYS OPEN, and two windows left alone write nothing.
//
// The board's `untagged` group starts shut in custom mode, so its entry in
// `atrium.folded` means OPEN, the reverse of every other project group. The
// `toggle` listener only knew the offline group was reversed, so opening
// `untagged` wrote the entry, the repaint drew it open and fired `toggle`, the
// listener read "open" as "take the entry out", the repaint shut it, and round
// again with a refresh on every turn: the board expanding and collapsing a group
// on its own, and a fetch pass per flip.
async function foldStillSection(browser, base) {
  const was = tasksMode;
  tasksMode = "filed";
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const errors = [];
  await ctx.addInitScript(() => {
    localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
    localStorage.setItem("atrium.grouping", JSON.stringify({ on: true, mode: "custom", groups: ["active"] }));
    window.__writes = [];
    const set = Storage.prototype.setItem;
    Storage.prototype.setItem = function (k, v) {
      if (this.getItem(k) !== String(v)) window.__writes.push(k);
      return set.call(this, k, v);
    };
    window.__toggles = 0;
    addEventListener("toggle", () => { window.__toggles++; }, true);
  });
  const open = async who => {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(who + ": " + e));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForTimeout(1200);
    await p.click('.tab[data-view="board"]');
    return p;
  };
  let live = 0;
  try {
    const a = await open("a");
    const b = await open("b");
    await a.waitForSelector('details.cardgroup[data-fold="proj:untagged"]', { state: "attached", timeout: 10000 });
    // Open it by hand, the way a click on its summary does.
    await a.evaluate(() => { document.querySelector('details.cardgroup[data-fold="proj:untagged"]').open = true; });
    // A live board: a card changes and the stream says so, every second, so
    // every pass repaints. A repaint is where the stored answer meets the screen.
    let tick = 0;
    live = setInterval(() => {
      LOOSE.display_title = "loose card " + (++tick);
      openStreams.forEach(r => { try { r.write("event: task\ndata: {}\n\n"); } catch (e) {} });
    }, 1000);
    await a.waitForTimeout(2500);
    const reset = p => p.evaluate(() => { window.__writes = []; window.__toggles = 0; });
    await reset(a); await reset(b);
    await a.waitForTimeout(+(process.env.FOLD_STILL_SECONDS || 10) * 1000);
    const got = await Promise.all([a, b].map(p => p.evaluate(() => ({
      writes: window.__writes, toggles: window.__toggles,
      open: !!(document.querySelector('details.cardgroup[data-fold="proj:untagged"]') || {}).open
    }))));
    if (process.env.DEBUG_HEADLESS) console.log("fold still: " + JSON.stringify(got));
    got.forEach((g, i) => {
      const who = i ? "the second window" : "the window it was opened in";
      if (g.writes.length) {
        fail(who + " wrote storage " + g.writes.length + " times while idle: " +
          [...new Set(g.writes)].join(", "));
      }
      if (g.toggles > 2) fail(who + " toggled a group " + g.toggles + " times while idle.");
    });
    if (!got[0].open) fail("the untagged group, opened by hand in custom mode, did not stay open.");

    // And shut again, which is its default, so the entry goes and stays gone.
    await a.evaluate(() => { document.querySelector('details.cardgroup[data-fold="proj:untagged"]').open = false; });
    await a.waitForTimeout(2500);
    await reset(a); await reset(b);
    await a.waitForTimeout(4000);
    const shut = await Promise.all([a, b].map(p => p.evaluate(() => ({
      writes: window.__writes.length,
      open: !!(document.querySelector('details.cardgroup[data-fold="proj:untagged"]') || {}).open
    }))));
    if (shut.some(s => s.open)) fail("the untagged group, shut again by hand, came back open: " + JSON.stringify(shut));
    if (shut.some(s => s.writes)) fail("shutting the untagged group left windows writing storage: " + JSON.stringify(shut));
    if (errors.length) fail("the fold-still pages threw: " + errors.join(" | "));
  } finally {
    clearInterval(live);
    LOOSE.display_title = "loose card";
    await ctx.close();
    tasksMode = was;
  }
}

// A context for the untagged sections: custom grouping with one group of yours,
// every stack column shown, and a record of every storage write that changed
// something.
async function untaggedContext(browser, opts) {
  const ctx = await browser.newContext(Object.assign({ viewport: { width: 1400, height: 900 } }, opts || {}));
  await ctx.addInitScript(() => {
    if (!sessionStorage.getItem("primed")) {
      sessionStorage.setItem("primed", "1");
      localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
      localStorage.setItem("atrium.grouping", JSON.stringify({ on: true, mode: "custom", groups: ["active"] }));
      localStorage.setItem("atrium.stack.show", "[]");
    }
    window.__writes = [];
    const set = Storage.prototype.setItem;
    Storage.prototype.setItem = function (k, v) {
      if (this.getItem(k) !== String(v)) window.__writes.push(k);
      return set.call(this, k, v);
    };
  });
  return ctx;
}

// The ids in the untagged group of each view, in the order they are drawn.
async function untaggedOrder(p, view) {
  await p.click('.tab[data-view="' + view + '"]');
  await p.waitForTimeout(900);
  return p.evaluate(view => {
    const sel = {
      stack: 'details.stackgroup[data-fold="proj:stack:untagged"] .stackrow[data-id]',
      board: 'details.cardgroup[data-fold="proj:untagged"] .card[data-id]',
      terms: '#term-list [data-ungroup="1"] .card.tab[data-id]'
    }[view];
    return [...document.querySelectorAll(sel)].map(e => e.dataset.id).join(",");
  }, view);
}

// UNTAGGED FOLLOWS THE SORT in all three views. Named groups keep the order you
// set, and `untagged` is the board's own heap, so it must read in the order the
// sort pill says. The terminals pane sorted it on the address line.
async function untaggedSortSection(browser, base) {
  const was = tasksMode;
  tasksMode = "untagged";
  const ctx = await untaggedContext(browser);
  const errors = [];
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    const byName = "u-disc,u-night,u-sa65,u-sa67";
    const byActivity = "u-sa65,u-night,u-sa67,u-disc";
    // Name.
    await p.evaluate(() => {
      localStorage.setItem("atrium.boardsort", "name");
      localStorage.setItem("atrium.termSort", "0");
      sortByActivity = false;
      setStackSort("name");
    });
    for (const v of ["stack", "board", "terms"]) {
      const got = await untaggedOrder(p, v);
      if (got !== byName) fail(`sorted by name, untagged on the ${v} read ${got}, expected ${byName}.`);
    }
    // Activity.
    await p.evaluate(() => {
      localStorage.setItem("atrium.boardsort", "activity");
      localStorage.setItem("atrium.termSort", "1");
      sortByActivity = true;
      setStackSort("activity");
    });
    for (const v of ["stack", "board", "terms"]) {
      const got = await untaggedOrder(p, v);
      if (got !== byActivity) fail(`sorted by activity, untagged on the ${v} read ${got}, expected ${byActivity}.`);
    }
    // And the stack's other pills, which have their own keys.
    const stackWant = { project: "u-sa67,u-sa65,u-disc,u-night", runner: byName };
    for (const [pill, want] of Object.entries(stackWant)) {
      await p.evaluate(k => setStackSort(k), pill);
      const got = await untaggedOrder(p, "stack");
      if (got !== want) fail(`sorted by ${pill}, untagged on the stack read ${got}, expected ${want}.`);
    }
    if (errors.length) fail("the untagged-sort page threw: " + errors.join(" | "));
  } finally {
    await ctx.close();
    tasksMode = was;
  }
}

// A CARD THIS WINDOW HAS NOT SEEN BEFORE says so: a pulse, then a `new` chip that
// clears on a click. Nothing is marked on a first load, a reload, or a hub
// restart that brings the same cards back, and nothing writes storage while idle.
async function newCardSection(browser, base) {
  const was = tasksMode;
  tasksMode = "untagged";
  untaggedExtra = [];
  const ctx = await untaggedContext(browser);
  const errors = [];
  const poke = () => openStreams.forEach(r => { try { r.write("event: task\ndata: {}\n\n"); } catch (e) {} });
  const marks = p => p.evaluate(() => ({
    arrived: [...document.querySelectorAll(".arrived[data-id]")].map(e => e.dataset.id),
    glow: [...document.querySelectorAll(".arrived.glow[data-id]")].map(e => e.dataset.id),
    chips: document.querySelectorAll(".chip.arrived").length
  }));
  let live = 0;
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    await p.waitForTimeout(800);
    let m = await marks(p);
    if (m.arrived.length) fail("a first load marked cards that were already there as new: " + m.arrived.join(","));

    // A card arrives.
    untaggedExtra = [NEWC];
    poke();
    await p.waitForSelector('#stack-list .stackrow.arrived.glow[data-id="u-new"]', { timeout: 10000 })
      .catch(() => fail("a new card on the stack did not pulse."));
    const anim = await p.evaluate(() => {
      const el = document.querySelector('#stack-list .stackrow[data-id="u-new"]');
      return el ? getComputedStyle(el).animationName : "";
    });
    if (anim !== "arrived") fail("the new stack row's animation is " + JSON.stringify(anim) + ", not arrived.");
    // NEWCARD_SHOT=dir saves what the pulse looks like, for a human to judge.
    if (process.env.NEWCARD_SHOT) {
      await p.waitForTimeout(300);
      await p.screenshot({ path: path.join(process.env.NEWCARD_SHOT, "newcard-stack.png") });
    }
    m = await marks(p);
    if (m.arrived.join(",") !== "u-new") fail("only the new card should be marked, got " + m.arrived.join(","));
    if (!m.chips) fail("the new card has no `new` chip on the stack.");
    for (const [v, sel] of [["board", '.card.arrived[data-id="u-new"] .chip.arrived'],
      ["terms", '#term-list .card.tab.arrived[data-id="u-new"] .chip.arrived']]) {
      await p.click('.tab[data-view="' + v + '"]');
      await p.waitForSelector(sel, { state: "attached", timeout: 8000 })
        .catch(() => fail("the new card has no `new` chip on the " + v + "."));
    }
    await p.click('.tab[data-view="stack"]');

    // Idle, with a repaint every second: nothing is written.
    await p.waitForTimeout(1500);
    await p.evaluate(() => { window.__writes = []; });
    let tick = 0;
    live = setInterval(() => { LOOSE.display_title = "loose " + (++tick); poke(); }, 1000);
    await p.waitForTimeout(5000);
    clearInterval(live); live = 0;
    const idle = await p.evaluate(() => window.__writes);
    if (idle.length) fail("the new-card cue wrote storage " + idle.length + " times while idle: " +
      [...new Set(idle)].join(", "));

    // A reload: nothing pulses. The card nobody has looked at keeps its chip,
    // and no other card gets one.
    await p.reload({ waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    await p.waitForTimeout(1200);
    m = await marks(p);
    if (m.glow.length) fail("a reload pulsed " + m.glow.join(","));
    if (m.arrived.join(",") !== "u-new") fail("after a reload the marked cards are " + m.arrived.join(",") +
      ", expected only the unread u-new.");

    // A click clears it, here and after another reload.
    await p.click('#stack-list .stackrow[data-id="u-new"] .who b');
    await p.keyboard.press("Escape");
    await p.waitForTimeout(300);
    m = await marks(p);
    if (m.arrived.length || m.chips) fail("a click did not clear the new card: " + JSON.stringify(m));
    const stored = await p.evaluate(() => JSON.parse(localStorage.getItem("atrium.newcards") || "{}")["u-new"]);
    if (stored !== 0) fail("the cleared card is stored as " + JSON.stringify(stored) + ", not 0.");

    // A hub restart: the rooms go, the hub answers 503 and then an empty list,
    // then the same cards come back under a second room's tag. Nothing is new.
    untaggedDown = true;
    openStreams.forEach(r => { try { r.destroy(); } catch (e) {} });
    await p.waitForTimeout(2500);
    untaggedDown = false; untaggedEmpty = true;
    poke();
    await p.waitForTimeout(2000);
    untaggedEmpty = false; untaggedTag = "sgg";
    poke();
    await p.waitForTimeout(3000);
    m = await marks(p);
    if (m.arrived.length) fail("a hub restart that brought the same cards back marked " + m.arrived.join(","));

    // And a reload in the middle of one, which starts on an empty board.
    untaggedEmpty = true;
    await p.reload({ waitUntil: "domcontentloaded" });
    await p.waitForTimeout(2000);
    untaggedEmpty = false;
    poke();
    await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    await p.waitForTimeout(1500);
    m = await marks(p);
    if (m.arrived.length) fail("a reload during a hub restart marked " + m.arrived.join(","));
    if (errors.length) fail("the new-card page threw: " + errors.join(" | "));
  } finally {
    clearInterval(live);
    LOOSE.display_title = "loose card";
    untaggedExtra = []; untaggedDown = false; untaggedEmpty = false; untaggedTag = "";
    await ctx.close();
  }

  // Reduced motion: the chip, and no animation.
  const rctx = await untaggedContext(browser, { reducedMotion: "reduce" });
  try {
    const p = await rctx.newPage();
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    await p.waitForTimeout(800);
    untaggedExtra = [NEWC];
    poke();
    await p.waitForSelector('#stack-list .stackrow.arrived[data-id="u-new"] .chip.arrived', { timeout: 10000 })
      .catch(() => fail("under reduced motion the new card has no chip."));
    const anim = await p.evaluate(() => {
      const el = document.querySelector('#stack-list .stackrow[data-id="u-new"]');
      return el ? getComputedStyle(el).animationName : "";
    });
    if (anim !== "none") fail("under reduced motion the new card still animates: " + anim);
  } finally {
    untaggedExtra = [];
    await rctx.close();
    tasksMode = was;
  }
}

// A THEME PREVIEW RECOLOURS THE CARD, not only the terminal: the attached row,
// its bridge, the pane's frame and the stack card follow the picker, for that
// card alone. Cancelling, detaching and switching cards put the saved colours
// back, nothing is written while previewing, and "use it" survives a reload.
async function themePreviewSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  const errors = [];
  p.on("pageerror", e => errors.push(String(e)));
  await p.addInitScript(() => {
    localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
    localStorage.setItem("atrium.cardColors", "1");
    window.__writes = [];
    const set = Storage.prototype.setItem;
    Storage.prototype.setItem = function (k, v) { window.__writes.push(k); return set.call(this, k, v); };
  });
  const patches = [];
  p.on("request", r => {
    if (r.method() === "PATCH" && /\/v1\/tasks\//.test(r.url())) patches.push(r.url() + " " + r.postData());
  });
  const was = tasksMode;
  const live = (id, theme) => Object.assign({}, T1, {
    id, display_title: "row " + id, theme, supervised: true, pinned: true, worktree: "/tmp/tp/" + id
  });
  try {
    wornTasks = [live("tp-a", "nord"), live("tp-b", "atrium")];
    tasksMode = "worn";
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    await p.click('.tab[data-view="terms"]');
    await p.waitForSelector('#term-list .card.tab[data-id="tp-a"]', { state: "attached", timeout: 15000 });
    // Let the view switch finish. `switchView` clears `on` from every `.tab`,
    // the rows included, and a late one would unselect the stand-in attach.
    await p.waitForTimeout(1000);
    const bg = await p.evaluate(() => {
      const hex = h => { const n = parseInt(h.slice(1), 16); return `rgb(${n >> 16}, ${(n >> 8) & 255}, ${n & 255})`; };
      const o = {};
      for (const n of ["nord", "atrium", "dracula", "gruvbox-dark"]) o[n] = hex(TERM_THEMES[n].background);
      return o;
    });
    // Attach a stand-in terminal on tp-a. The picker and the bridge only ask
    // that there is one with options to set.
    const attach = id => p.evaluate(async id => {
      termTask = lastTasks.find(t => t.id === id);
      term = { options: {}, dispose() {} };
      await renderTermList();
      await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));
      placeTabBridge();
    }, id);
    const read = () => p.evaluate(async () => {
      await new Promise(r => setTimeout(r, 400));
      await renderStack();
      const row = id => document.querySelector('#term-list .card.tab[data-id="' + id + '"]');
      const a = row("tp-a"), b = row("tp-b");
      const bridge = [...document.querySelectorAll("#term-layout .tabbridge")].find(x => !x.hidden);
      const pane = document.getElementById("term-pane");
      const stack = document.querySelector('#stack-list .stackrow[data-id="tp-a"]');
      return {
        a: a && getComputedStyle(a).backgroundColor, b: b && getComputedStyle(b).backgroundColor,
        aEdge: a && getComputedStyle(a).borderTopColor,
        bridge: bridge ? getComputedStyle(bridge).backgroundColor : null,
        frame: pane.style.getPropertyValue("--framec"),
        stack: stack ? stack.style.getPropertyValue("--card-0") : null,
        term: term && term.options.theme ? term.options.theme.background : null,
        wrap: document.getElementById("t-theme-wrap").hidden
      };
    });
    const themeHex = n => p.evaluate(n => TERM_THEMES[n].background.toLowerCase(), n);

    await attach("tp-a");
    let r = await read();
    if (r.a !== bg.nord) fail("before a preview the attached row is " + r.a + ", not nord's " + bg.nord + ".");
    const saved = r;

    // Preview dracula: everything that wears tp-a's theme follows it, tp-b does not.
    await p.evaluate(() => { window.__writes = []; pickTheme(); previewTheme("dracula"); });
    r = await read();
    if (r.term !== await themeHex("dracula")) fail("the preview did not reach the terminal: " + r.term);
    if (r.a !== bg.dracula) fail("previewing dracula, the attached row stayed " + r.a + ".");
    if (r.b !== saved.b) fail("previewing a theme on tp-a recoloured tp-b: " + r.b);
    if (!r.bridge || r.bridge !== r.a) fail("previewing, the bridge is " + r.bridge + " and the row " + r.a + ".");
    if (r.frame !== r.aEdge) fail("previewing, the pane frame is " + r.frame + " and the row's edge " + r.aEdge + ".");
    if (r.stack !== await themeHex("dracula")) fail("previewing, the stack card wears " + r.stack + ".");
    // Another pick moves it again.
    await p.evaluate(() => previewTheme("gruvbox-dark"));
    r = await read();
    if (r.a !== bg["gruvbox-dark"]) fail("a second pick did not move the row: " + r.a);

    // Cancel: the saved colours come back, and nothing was written.
    await p.evaluate(() => cancelTheme());
    r = await read();
    for (const k of ["a", "b", "bridge", "frame", "stack"]) {
      if (r[k] !== saved[k]) fail("after cancel, " + k + " is " + r[k] + ", saved was " + saved[k] + ".");
    }
    const writes = await p.evaluate(() => window.__writes);
    if (writes.length) fail("a preview wrote localStorage: " + [...new Set(writes)].join(", "));
    if (patches.length) fail("a preview wrote to the daemon: " + patches.join(" | "));

    // Detaching ends a preview.
    await p.evaluate(() => { pickTheme(); previewTheme("dracula"); });
    await p.evaluate(() => clearTermPane(false));
    r = await read();
    if (r.a !== saved.a || !r.wrap) fail("after detaching mid-preview the row is " + r.a + ", picker hidden " + r.wrap);
    if (await p.evaluate(() => themePreview)) fail("detaching left a preview held.");

    // So does switching cards.
    await attach("tp-a");
    await p.evaluate(() => { pickTheme(); previewTheme("dracula"); });
    await p.evaluate(async () => {
      clearTermPane(true);
      termTask = lastTasks.find(t => t.id === "tp-b");
      term = { options: {}, dispose() {} };
    });
    r = await read();
    if (r.a !== saved.a) fail("after switching to tp-b mid-preview, tp-a is " + r.a + ", not its saved " + saved.a);

    // Use it: saved once, and kept across a reload.
    await attach("tp-a");
    await p.evaluate(async () => {
      pickTheme();
      document.getElementById("t-theme").value = "dracula";
      previewTheme("dracula");
      await keepTheme();
    });
    r = await read();
    if (r.a !== bg.dracula) fail("after use it the row is " + r.a + ", not dracula.");
    if (patches.length !== 1 || !/tp-a .*"theme":"dracula"/.test(patches[0])) {
      fail("use it did not save the theme once: " + patches.join(" | "));
    }
    await p.reload({ waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    await p.click('.tab[data-view="terms"]');
    await p.waitForSelector('#term-list .card.tab[data-id="tp-a"]', { state: "attached", timeout: 15000 });
    // Let the view switch finish. `switchView` clears `on` from every `.tab`,
    // the rows included, and a late one would unselect the stand-in attach.
    await p.waitForTimeout(1000);
    await attach("tp-a");
    r = await read();
    if (r.a !== bg.dracula) fail("after use it and a reload the row is " + r.a + ", not dracula.");
    if (errors.length) fail("the theme preview page threw: " + errors.join(" | "));
  } finally {
    tasksMode = was;
    await ctx.close();
  }
}

// A CLICK ON AN ALERT LANDS WHERE IT IS ABOUT. One card with a live terminal:
// that terminal attached. One card without one: its detail, or its request.
// Nothing in particular: nowhere, and an open dialog stays open. From a toast,
// the toast log, a plain desktop notification clicked while the board was
// unfocused, the service worker's message, a `?land=` address, and a
// popped-out window. The first case is the race that asked for this: a new
// card listed a moment before its terminal, which landed on the stack.
async function landContext(browser, unfocused) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  await ctx.addInitScript(unfocused => {
    localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
    if (!unfocused) return;
    // Nobody is looking and desktop notifications are allowed, so `notify`
    // takes its third branch. The notification is kept to be clicked.
    Document.prototype.hasFocus = () => false;
    window.__notes = [];
    window.Notification = function (title, o) { this.title = title; this.o = o; window.__notes.push(this); };
    window.Notification.permission = "granted";
    window.Notification.requestPermission = async () => "granted";
    window.Notification.prototype.close = function () {};
  }, !!unfocused);
  return ctx;
}

// Records each attach at `openTerm`, which every landing on a terminal reaches.
function spyAttach(p) {
  return p.evaluate(() => {
    window.__opened = [];
    const real = openTerm;
    openTerm = t => { window.__opened.push(t.id); return real(t); };
  });
}

// Where the page is: the view showing, the last card attached, and whether the
// card detail is open and on what.
function landedAt(p) {
  return p.evaluate(() => ({
    view: VIEWS.find(v => !document.getElementById(v).hidden) || "",
    opened: (window.__opened || []).slice(-1)[0] || "",
    detail: document.getElementById("detail").open ? document.getElementById("d-title").textContent : "",
    dialogs: [...document.querySelectorAll("dialog[open]")].map(d => d.id)
  }));
}

async function landSection(browser, base) {
  const was = tasksMode;
  tasksMode = "land";
  const poke = () => openStreams.forEach(r => { try { r.write("event: task\ndata: {}\n\n"); } catch (e) {} });
  const reset = p => p.evaluate(() => {
    closeTerm(true);
    document.querySelectorAll("dialog[open]").forEach(d => d.close());
    document.querySelectorAll("#toasts .toast").forEach(t => t.remove());
    switchView("stack");
    window.__opened = [];
  });
  const clickToast = (p, text) => p.click(`#toasts .toast:has-text("${text}") b`);
  // Settles on a place, rather than asserting at a fixed moment: the race case
  // lands after a wait, and a place asserted early is the wrong place.
  const settle = async (p, want, ms) => {
    const until = Date.now() + (ms || 6000);
    let at;
    do {
      at = await landedAt(p);
      if (want(at)) return at;
      await p.waitForTimeout(200);
    } while (Date.now() < until);
    return at;
  };
  landCard("land-live", { supervised: true, created_at: "2026-09-19T12:00:00Z" });
  landCard("land-dead", { status: "dead", supervised: false, created_at: "2026-09-19T12:00:00Z" });
  landCard("land-pc", { status: "needs-permission", supervised: false, created_at: "2026-09-19T12:00:00Z" });
  landList = [LAND["land-live"], LAND["land-dead"]];
  landPerms = [];
  const errors = [];
  let ctx = await landContext(browser);
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    if (process.env.DEBUG_HEADLESS) p.on("console", m => console.error("[land] " + m.text()));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    await p.waitForTimeout(1200);
    await spyAttach(p);

    // 1. A new card whose terminal is not there yet, from its own toast.
    landCard("land-new", { supervised: false });
    landList = landList.concat(LAND["land-new"]);
    poke();
    await p.waitForSelector('#toasts .toast:has-text("land new is on the board")', { timeout: 10000 });
    await clickToast(p, "land new is on the board");
    setTimeout(() => { LAND["land-new"].supervised = true; }, 1500);
    let at = await settle(p, a => a.opened === "land-new");
    if (at.opened !== "land-new" || at.view !== "terms") {
      fail("a new card's toast, clicked before its terminal opened, landed on " + JSON.stringify(at) +
        ", not its terminal.");
    }

    // 2. The service worker's message, which is how a desktop notification
    // clicked with the board in the background arrives. Same race.
    await reset(p);
    landCard("land-new2", { supervised: false });
    landList = landList.concat(LAND["land-new2"]);
    await p.evaluate(() => navigator.serviceWorker.dispatchEvent(new MessageEvent("message",
      { data: { type: "goTo", view: "stack", taskFor: "land-new2", key: "" } })));
    setTimeout(() => { LAND["land-new2"].supervised = true; }, 1000);
    at = await settle(p, a => a.opened === "land-new2");
    if (at.opened !== "land-new2" || at.view !== "terms") {
      fail("a desktop notification for a new card landed on " + JSON.stringify(at) + ", not its terminal.");
    }

    // 3. The toast log.
    await reset(p);
    await p.evaluate(() => { toast("land live is ready", "", "stack", null, "land-live"); });
    await p.evaluate(() => document.querySelectorAll("#toasts .toast").forEach(t => t.remove()));
    await p.evaluate(() => openToastLog());
    await p.click('#toastlog-list .tlrow.clickable:has-text("land live is ready")');
    at = await settle(p, a => a.opened === "land-live");
    if (at.opened !== "land-live" || at.view !== "terms" || at.dialogs.includes("toastlog")) {
      fail("a toast log row for a live card landed on " + JSON.stringify(at) + ", not its terminal.");
    }

    // 4. A card with no terminal: its detail, not the stack.
    await reset(p);
    await p.evaluate(() => { toast("land dead has stopped", "", "stack", null, "land-dead"); });
    await clickToast(p, "land dead has stopped");
    at = await settle(p, a => !!a.detail);
    if (at.detail !== "land dead" || at.opened) {
      fail("an alert for a card with no terminal landed on " + JSON.stringify(at) + ", not its detail.");
    }

    // 5. A request on a card with no terminal: the request, flashed.
    await reset(p);
    landPerms = [{ id: "perm-land", task_id: "land-pc", agent: "land pc", tool: "Bash", command: "ls",
      requested_at: new Date().toISOString().replace("Z", "") }];
    await p.evaluate(() => runRefresh());
    await p.waitForTimeout(800);
    await p.evaluate(() => document.querySelectorAll("#toasts .toast").forEach(t => t.remove()));
    await p.evaluate(() => { toast("land pc needs permission", "Bash: ls", "perms", "perm-land", "land-pc"); });
    await clickToast(p, "land pc needs permission");
    await p.waitForTimeout(800);
    at = await landedAt(p);
    const flashed = await p.evaluate(() => {
      const el = document.querySelector('#perms-list .perm[data-id="perm-land"]');
      return !!el && el.classList.contains("flash");
    });
    if (at.view !== "perms" || !flashed || at.opened) {
      fail("a request on a card with no terminal landed on " + JSON.stringify(at) +
        (flashed ? "" : ", with the request not flashed") + ".");
    }
    landPerms = [];
    await p.evaluate(() => runRefresh());

    // 6. A toast about nothing: nowhere, and the dialog under it stays.
    await reset(p);
    await p.evaluate(() => { document.getElementById("toastlog").showModal(); toast("land nowhere", "copied"); });
    await clickToast(p, "land nowhere");
    await p.waitForTimeout(500);
    at = await landedAt(p);
    if (at.view !== "stack" || !at.dialogs.includes("toastlog")) {
      fail("a toast about nothing took the board to " + JSON.stringify(at) + ".");
    }

    // 7. A popped-out window's alert about a card that is not its own: the
    // board lands it. And one about its own card leaves the board alone.
    await reset(p);
    const solo = await ctx.newPage();
    solo.on("pageerror", e => errors.push(String(e)));
    await solo.goto(base + "/#term=s1", { waitUntil: "domcontentloaded" });
    await solo.waitForFunction(() => typeof soloID !== "undefined" && soloID === "s1", null, { timeout: 10000 });
    await solo.evaluate(() => { toast("land live is ready", "", "stack", null, "land-live"); });
    await clickToast(solo, "land live is ready");
    at = await settle(p, a => a.opened === "land-live");
    if (at.opened !== "land-live") {
      fail("a popped-out window's alert for another card did not land it on the board: " + JSON.stringify(at));
    }
    await reset(p);
    await solo.evaluate(() => { toast("solo card is ready", "", "stack", null, "s1"); });
    await clickToast(solo, "solo card is ready");
    await p.waitForTimeout(800);
    at = await landedAt(p);
    if (at.opened) fail("a popped-out window's alert for its own card attached the board to " + at.opened);
    await solo.close();

    // 8. A board opened by a desktop notification with none open.
    await p.goto(base + "/?land=land-live&view=stack", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof openTerm === "function", null, { timeout: 10000 });
    await spyAttach(p);
    at = await settle(p, a => a.view === "terms");
    const left = await p.evaluate(() => location.search);
    const term = await p.evaluate(() => termTask && termTask.id);
    if (at.view !== "terms" || term !== "land-live") {
      fail("a board opened at ?land=land-live landed on " + JSON.stringify(at) + " holding " + term + ".");
    }
    if (left) fail("the landing query stayed on the address: " + left);
  } finally {
    await ctx.close();
  }

  // 9. A plain desktop notification, raised with the board unfocused and
  // clicked: the terminal, and a line in the toast log that lands there too.
  ctx = await landContext(browser, true);
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    await p.waitForTimeout(1200);
    await spyAttach(p);
    landCard("land-new3", { supervised: false });
    landList = landList.concat(LAND["land-new3"]);
    poke();
    await p.waitForFunction(() => window.__notes.some(n => n.title === "land new3 is on the board"), null,
      { timeout: 10000 }).catch(() => fail("no desktop notification for a new card with the board unfocused."));
    await p.evaluate(() => window.__notes.find(n => n.title === "land new3 is on the board").onclick());
    setTimeout(() => { LAND["land-new3"].supervised = true; }, 1000);
    const at = await settle(p, a => a.opened === "land-new3");
    if (at.opened !== "land-new3" || at.view !== "terms") {
      fail("a desktop notification for a new card, clicked, landed on " + JSON.stringify(at) + ".");
    }
    const logged = await p.evaluate(() => toastLog().find(t => t.title === "land new3 is on the board"));
    if (!logged || logged.taskFor !== "land-new3") {
      fail("the desktop notification's log line does not carry its card: " + JSON.stringify(logged));
    }
  } finally {
    await ctx.close();
  }
  if (errors.length) fail("the landing pages threw: " + errors.join(" | "));
  landList = []; landPerms = [];
  tasksMode = was;
}

// ── selecting the attached card again is a focus, not a re-attach ─────────
// Backlog-2 item 20. Every way back onto the card the pane already has live
// (the row, a toast, the switcher, a `#term=` window) goes through `openTerm`,
// and it used to tear the pane down and dial the same socket again, which makes
// the daemon replay the whole scrollback. The mocked attach socket replays a
// line on every open, the way the daemon does, so a re-attach shows up both as
// a second socket and as writes into xterm.
async function reselectSection(browser, base) {
  const was = tasksMode;
  tasksMode = "land";
  landCard("land-live", { supervised: true, created_at: "2026-09-19T12:00:00Z" });
  landCard("land-other", { supervised: true, created_at: "2026-09-19T12:00:00Z" });
  landList = [LAND["land-live"], LAND["land-other"]];
  landPerms = [];
  const errors = [];
  const ctx = await landContext(browser);
  await ctx.addInitScript(() => {
    window.__attaches = [];
    const Real = window.WebSocket;
    window.WebSocket = function (url, protocols) {
      if (!/\/attach(\?|$)/.test(url)) return new Real(url, protocols);
      window.__attaches.push(url);
      const s = { url, readyState: 0, binaryType: "arraybuffer",
        onopen: null, onclose: null, onmessage: null, onerror: null,
        send() {}, close() { this.readyState = 3; } };
      setTimeout(() => {
        s.readyState = 1;
        if (s.onopen) s.onopen({});
        if (s.onmessage) s.onmessage({ data: "replayed history\r\n" });
      }, 0);
      return s;
    };
    Object.assign(window.WebSocket, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });
  });
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    await p.evaluate(() => attachTask("land-live"));
    await p.waitForFunction(() => termSock && termSock.readyState === 1 && termTask && termTask.id === "land-live", null,
      { timeout: 10000 });
    await p.waitForTimeout(300);
    // From here on nothing may reach xterm or dial a socket.
    await p.evaluate(() => {
      window.__writes = 0;
      window.__term = term;
      const real = Terminal.prototype.write;
      Terminal.prototype.write = function () { window.__writes++; return real.apply(this, arguments); };
      window.__attaches = [];
    });
    const check = async how => {
      await p.waitForTimeout(400);
      const got = await p.evaluate(() => ({ socks: window.__attaches.length, writes: window.__writes,
        same: term === window.__term, card: termTask && termTask.id,
        view: document.getElementById("terms").hidden ? "not terms" : "terms" }));
      if (got.socks || got.writes || !got.same || got.card !== "land-live" || got.view !== "terms") {
        fail("selecting the attached card again by " + how + " re-attached it: " + JSON.stringify(got) +
          ". A second select of a live card must focus it, not open a socket or replay history.");
      }
    };
    await p.waitForSelector('#term-list .card.tab[data-id="land-live"]', { timeout: 10000 });
    await p.click('#term-list .card.tab[data-id="land-live"]');
    await check("its row");
    await p.evaluate(() => landOnAlert("land-live"));
    await check("an alert");
    await p.evaluate(() => { switchView("stack"); return attachTask("land-live"); });
    await check("attach from another view");
    await p.evaluate(() => openTerm(termTask));
    await check("openTerm directly");

    // The control: another card does attach, and so does the same card once its
    // socket is gone.
    await p.evaluate(() => attachTask("land-other"));
    await p.waitForTimeout(400);
    let socks = await p.evaluate(() => window.__attaches.length);
    if (socks !== 1) fail("selecting a different card opened " + socks + " sockets, not 1.");
    await p.evaluate(() => { window.__attaches = []; termSock.readyState = 3; return attachTask("land-other"); });
    await p.waitForTimeout(400);
    socks = await p.evaluate(() => window.__attaches.length);
    if (socks !== 1) fail("selecting a card whose socket had closed opened " + socks + " sockets, not 1.");
  } finally {
    await ctx.close();
  }
  if (errors.length) fail("the reselect page threw: " + errors.join(" | "));
  landList = []; landPerms = [];
  tasksMode = was;
}

// ── over a terminal the toasts hang from the top right ─────────────────────
// Backlog-2 item 18. A toast in the bottom right sat on the terminal's input
// line and status bar, where clint was typing. On the terminals view (and in a
// popped-out window) the stack anchors below the pane's bar, clear of the
// paste indicator, and every other view keeps bottom right. A switch with
// toasts up moves the same elements and does not replay their entrance.
async function toastsTopSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const errors = [];
  // Where the stack is, and each toast's box, oldest first as in the DOM.
  const where = pg => pg.evaluate(() => {
    const host = document.getElementById("toasts");
    const boxes = [...host.querySelectorAll(".toast:not(.leaving)")].map(el => {
      const r = el.getBoundingClientRect();
      return { title: el.querySelector("b").textContent, top: r.top, bottom: r.bottom, left: r.left, right: r.right };
    });
    const bar = document.querySelector("#term-pane .term-bar").getBoundingClientRect();
    const pane = document.getElementById("term-pane").getBoundingClientRect();
    return { top: host.classList.contains("top"), boxes, barBottom: bar.bottom, paneRight: pane.right,
      h: innerHeight, starts: window.__starts || 0 };
  });
  const overlaps = (a, b) => a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom;
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    await p.evaluate(() => {
      switchView("stack");
      document.getElementById("toasts").innerHTML = "";
      window.__starts = 0;
      document.getElementById("toasts").addEventListener("animationstart", e => {
        if (e.animationName === "toastin") window.__starts++;
      });
      toast("older toast", "first");
      toast("newer toast", "second");
    });
    await p.waitForTimeout(400);

    // 1. Off the terminals view: bottom right, as before.
    let w = await where(p);
    if (w.top || !w.boxes.length || w.boxes.some(b => b.bottom < w.h - 200)) {
      fail("off the terminals view the toasts are not bottom right: " + JSON.stringify(w));
    }
    const born = w.starts;

    // 2. Onto the terminals view with them up: top right, under the bar, the
    // same two toasts, not animated in again, newest on top.
    await p.evaluate(() => {
      document.querySelectorAll("#toasts .toast").forEach(el => { el.__mark = 1; });
      switchView("terms");
    });
    await p.waitForTimeout(400);
    w = await where(p);
    const kept = await p.evaluate(() => [...document.querySelectorAll("#toasts .toast")].every(el => el.__mark));
    if (!w.top || w.boxes.length !== 2 || w.boxes.some(b => b.top < w.barBottom || b.bottom > w.h / 2)) {
      fail("on the terminals view the toasts do not hang below the terminal's bar: " + JSON.stringify(w));
    }
    if (w.boxes.some(b => b.right > w.paneRight + 1)) {
      fail("on the terminals view a toast pokes past the terminal's right edge: " + JSON.stringify(w));
    }
    if (!kept) fail("switching to the terminals view rebuilt the toasts instead of moving them.");
    if (w.starts !== born) fail("switching to the terminals view replayed the toasts' entrance.");
    const older = w.boxes.find(b => b.title === "older toast"), newer = w.boxes.find(b => b.title === "newer toast");
    if (!older || !newer || newer.top > older.top) {
      fail("anchored top, the newest toast is not the top one: " + JSON.stringify(w.boxes));
    }

    // 3. A paste on its way: the indicator is not under a toast.
    await p.evaluate(() => { pasteShow({ n: 300 * 1024 }); toast("during a paste", "third"); });
    await p.waitForTimeout(400);
    w = await where(p);
    const paste = await p.evaluate(() => {
      const r = document.getElementById("t-pasting").getBoundingClientRect();
      return { top: r.top, bottom: r.bottom, left: r.left, right: r.right, h: r.height };
    });
    if (!paste.h) fail("the paste indicator did not show, so the test did not exercise it.");
    if (w.boxes.some(b => overlaps(b, paste))) {
      fail("a toast covers the paste indicator: " + JSON.stringify({ paste, boxes: w.boxes }));
    }
    await p.evaluate(() => pasteEnd());

    // 4. And back off it: bottom right again, still not re-animated.
    const before = (await where(p)).starts;
    await p.evaluate(() => switchView("stack"));
    await p.waitForTimeout(400);
    w = await where(p);
    if (w.top || w.boxes.some(b => b.bottom < w.h - 400)) {
      fail("back off the terminals view the toasts did not return bottom right: " + JSON.stringify(w));
    }
    if (w.starts !== before) fail("switching away from the terminals view replayed the toasts' entrance.");

    // 5. Every skin: the anchor is layout, so none of them may move it.
    await p.evaluate(() => switchView("terms"));
    for (const skin of ["harbour", "daylight", "website", "noir", "paper"]) {
      await p.evaluate(s => applySkin(s), skin);
      await p.waitForTimeout(150);
      w = await where(p);
      if (!w.top || w.boxes.some(b => b.top < w.barBottom || b.bottom > w.h / 2)) {
        fail("in " + skin + " the terminals view's toasts are not top right: " + JSON.stringify(w));
      }
    }

    // 6. A popped-out window is a terminal too.
    const solo = await ctx.newPage();
    solo.on("pageerror", e => errors.push(String(e)));
    await solo.goto(base + "/#term=s1", { waitUntil: "domcontentloaded" });
    await solo.waitForFunction(() => typeof soloID !== "undefined" && soloID === "s1", null, { timeout: 10000 });
    await solo.evaluate(() => toast("solo toast", "in a popped-out window"));
    await solo.waitForTimeout(400);
    w = await where(solo);
    if (!w.top || !w.boxes.length || w.boxes.some(b => b.top < w.barBottom || b.bottom > w.h / 2)) {
      fail("in a popped-out window the toasts are not top right: " + JSON.stringify(w));
    }
    await solo.close();
  } finally {
    await ctx.close();
  }
  if (errors.length) fail("the toasts-top page threw: " + errors.join(" | "));
}

// ── say immediately, or when the turn is done ─────────────────────────────
// Backlog-2 item 10. Beside send on both composers (say something, and the
// note) sits an "immediately" button: send posts `when: "done"`, which waits
// for the turn to end, and immediately posts `when: "immediate"`. The held `!`
// chip names what is holding a message (the line, the turn, or a dialog) and
// counts them, `! 2`. The runner form carries the mid-turn setting.
async function sayWhenSection(browser, base) {
  const errors = [];
  const ctx = await browser.newContext();
  const posted = [];
  await ctx.route("**/v1/tasks/*/message", async route => {
    posted.push({ kind: "message", body: JSON.parse(route.request().postData() || "{}") });
    await route.fulfill({ contentType: "application/json",
      body: JSON.stringify({ delivered: "queued", when: posted[posted.length - 1].body.when }) });
  });
  await ctx.route("**/v1/tasks/*/note/send", async route => {
    posted.push({ kind: "note", body: JSON.parse(route.request().postData() || "{}") });
    await route.fulfill({ contentType: "application/json", body: JSON.stringify({ delivered: "queued" }) });
  });
  await ctx.route("**/v1/tasks/*/messages", route =>
    route.fulfill({ contentType: "application/json", body: JSON.stringify({ messages: [] }) }));
  await ctx.route("**/v1/tasks/*", route => route.request().method() === "PATCH"
    ? route.fulfill({ contentType: "application/json", body: "{}" }) : route.fallback());
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });

    // The buttons sit beside send, in the same toolbar, and wear a styled tip.
    const layout = await p.evaluate(() => {
      const beside = (a, b) => {
        const x = document.getElementById(a), y = document.getElementById(b);
        return !!(x && y && x.parentElement === y.parentElement);
      };
      const now = document.getElementById("d-say-now");
      return { say: beside("d-say-send", "d-say-now"), note: beside("d-note-send", "d-note-now"),
        label: now && now.textContent.trim(), tip: !!(now && now.dataset.tip),
        form: !!document.getElementById("h-midturn") };
    });
    if (!layout.say) fail("the say box has no immediately button beside send: " + JSON.stringify(layout));
    if (!layout.note) fail("the note has no immediately button beside send it: " + JSON.stringify(layout));
    if (layout.label !== "immediately" || !layout.tip) {
      fail("the immediately button is not labelled or has no styled tip: " + JSON.stringify(layout));
    }
    if (!layout.form) fail("the runner form has no mid-turn input checkbox.");

    // Send waits for the turn, immediately does not, and each says so.
    await p.evaluate(async () => {
      current = { id: "t1", status: "running", note: "" };
      const box = document.getElementById("d-say");
      box.value = "stop now";
      await sayToCurrent("immediate");
      box.value = "rebase after";
      await sayToCurrent("done");
    });
    const sayWhens = posted.filter(x => x.kind === "message").map(x => x.body.when);
    if (sayWhens.join(",") !== "immediate,done") {
      fail("the say buttons posted when " + JSON.stringify(sayWhens) + ", not immediate then done.");
    }
    const hint = await p.evaluate(() => document.getElementById("d-say-how").textContent);
    if (!/turn to end/.test(hint)) fail("a done say did not say it waits for the turn: " + JSON.stringify(hint));
    // Enter is send, ctrl-enter is immediately.
    await p.evaluate(() => {
      const box = document.getElementById("d-say");
      box.value = "via ctrl-enter";
      box.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", ctrlKey: true, bubbles: true }));
    });
    await p.waitForTimeout(300);
    const last = posted.filter(x => x.kind === "message").pop();
    if (!last || last.body.text !== "via ctrl-enter" || last.body.when !== "immediate") {
      fail("ctrl-enter in the say box did not send immediately: " + JSON.stringify(last));
    }

    await p.evaluate(async () => {
      document.getElementById("d-note").value = "three things";
      await sendNote("immediate");
    });
    const note = posted.filter(x => x.kind === "note").pop();
    if (!note || note.body.when !== "immediate") {
      fail("the note's immediately button did not post when immediate: " + JSON.stringify(note));
    }

    // The form carries the setting on save.
    const saved = await p.evaluate(() => {
      document.getElementById("h-midturn").checked = true;
      return harnessFromForm().mid_turn_input;
    });
    if (saved !== true) fail("the runner form does not send mid_turn_input: " + JSON.stringify(saved));

    // The chip names the condition, and counts.
    const chips = await p.evaluate(() => {
      const one = (act) => {
        const d = document.createElement("div");
        d.innerHTML = termHeldChip({ activity: Object.assign({ held_peer: "sg4/doer", held_seconds: 90 }, act) });
        const s = d.querySelector(".chip.held");
        return s ? { text: s.textContent, tip: s.dataset.tip } : null;
      };
      return { turn: one({ held_for: "turn", held_count: 2 }), line: one({ held_for: "line" }),
        dialog: one({ held_for: "dialog" }), old: one({}) };
    });
    if (!chips.turn || chips.turn.text !== "! 2" || !/turn to end/.test(chips.turn.tip) ||
        /input line/.test(chips.turn.tip)) {
      fail("a message held for the turn did not draw `! 2` naming the turn: " + JSON.stringify(chips.turn));
    }
    if (!chips.line || chips.line.text !== "!" || !/input line/.test(chips.line.tip)) {
      fail("a message held by the line did not name the line: " + JSON.stringify(chips.line));
    }
    if (!chips.dialog || !/dialog/.test(chips.dialog.tip)) {
      fail("a message held by a dialog did not name the dialog: " + JSON.stringify(chips.dialog));
    }
    if (!chips.old || !/input line/.test(chips.old.tip)) {
      fail("a room older than held_for lost the line wording: " + JSON.stringify(chips.old));
    }
  } finally {
    await ctx.close();
  }
  if (errors.length) fail("the say-when page threw: " + errors.join(" | "));
}

// ── any paste still in flight after 20ms shows the spinner ────────────────
// Test plan BB. What starts it is a paste gesture, not a size: a one-line paste
// held on the socket shows it, one that drains and echoes inside 20ms never
// flashes, and a typed key or escape sequence never shows it however long it is.
async function pasteSpinnerSection(browser, base) {
  const was = tasksMode;
  tasksMode = "land";
  landCard("land-live", { supervised: true, created_at: "2026-09-19T12:00:00Z" });
  landList = [LAND["land-live"]];
  landPerms = [];
  const errors = [];
  const ctx = await landContext(browser);
  await ctx.addInitScript(() => {
    const Real = window.WebSocket;
    window.WebSocket = function (url, protocols) {
      if (!/\/attach(\?|$)/.test(url)) return new Real(url, protocols);
      const s = { url, readyState: 0, binaryType: "arraybuffer", bufferedAmount: 0,
        onopen: null, onclose: null, onmessage: null, onerror: null,
        send() {}, close() { this.readyState = 3; } };
      setTimeout(() => { s.readyState = 1; if (s.onopen) s.onopen({}); }, 0);
      return s;
    };
    Object.assign(window.WebSocket, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });
  });
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    await p.evaluate(() => attachTask("land-live"));
    await p.waitForFunction(() => termSock && termSock.readyState === 1 && termTask && termTask.id === "land-live", null,
      { timeout: 10000 });
    await p.waitForTimeout(300);
    await p.evaluate(() => {
      const real = pasteShow;
      pasteShow = f => { window.__shown++; real(f); };
    });

    // 1. A one-line paste held on the socket for 50ms shows, then goes on the echo.
    const heldGot = await p.evaluate(() => new Promise(done => {
      const vis = () => { const el = document.getElementById("t-pasting"); return !!(el && !el.hidden); };
      pasteEnd();
      window.__shown = 0;
      termSock.bufferedAmount = 5;
      sendPasteText("one line");
      const got = {};
      setTimeout(() => { got.during = vis(); got.text = (document.getElementById("t-pasting") || {}).textContent; }, 40);
      setTimeout(() => { termSock.bufferedAmount = 0; termSock.onmessage({ data: "one line" }); }, 50);
      setTimeout(() => { got.after = vis(); got.shown = window.__shown; done(got); }, 120);
    }));
    if (!heldGot.during || heldGot.shown !== 1) {
      fail("a one-line paste held on the socket for 50ms did not show the spinner: " + JSON.stringify(heldGot));
    }
    if (heldGot.during && !/pasting \d+B/.test(heldGot.text || "")) {
      fail("a small paste's spinner does not name its size in bytes: " + JSON.stringify(heldGot.text));
    }
    if (heldGot.after) fail("the spinner stayed up after the paste drained and echoed: " + JSON.stringify(heldGot));

    // 2. A paste that drains and echoes inside 20ms never flashes.
    const quick = await p.evaluate(() => new Promise(done => {
      pasteEnd();
      window.__shown = 0;
      termSock.bufferedAmount = 0;
      sendPasteText("x");
      setTimeout(() => termSock.onmessage({ data: "x" }), 5);
      setTimeout(() => done({ shown: window.__shown, flight: !!pasteFlight }), 150);
    }));
    if (quick.shown || quick.flight) {
      fail("a paste that landed inside 20ms flashed the spinner: " + JSON.stringify(quick));
    }

    // 3. Typed input never shows it: a key, an escape sequence, or a long burst.
    const typed = await p.evaluate(() => new Promise(done => {
      pasteEnd();
      window.__shown = 0;
      termSock.bufferedAmount = 5;
      term.input("a", true);
      term.input("\x1b[A", true);
      term.input("x".repeat(3000), true);
      setTimeout(() => {
        const got = { shown: window.__shown, flight: !!pasteFlight };
        termSock.bufferedAmount = 0;
        done(got);
      }, 150);
    }));
    if (typed.shown || typed.flight) fail("typed input started the paste spinner: " + JSON.stringify(typed));
  } finally {
    await ctx.close();
  }
  if (errors.length) fail("the paste spinner page threw: " + errors.join(" | "));
  landList = []; landPerms = [];
  tasksMode = was;
}

// ── copy on select answers the pointer, not the find bar ──────────────────
// Test plan BJ. The search addon shows a match by selecting it, so copy on
// select used to copy every find keystroke, step and re-search. Typing in the
// find bar, stepping, and output arriving while it is open leave the clipboard
// alone; a drag, a double-click and a triple-click still copy.
async function copySelectSection(browser, base) {
  const was = tasksMode;
  tasksMode = "land";
  landCard("land-live", { supervised: true, created_at: "2026-09-19T12:00:00Z" });
  landList = [LAND["land-live"]];
  landPerms = [];
  const errors = [];
  const ctx = await landContext(browser);
  await ctx.addInitScript(() => {
    const Real = window.WebSocket;
    window.WebSocket = function (url, protocols) {
      if (!/\/attach(\?|$)/.test(url)) return new Real(url, protocols);
      const s = { url, readyState: 0, binaryType: "arraybuffer", bufferedAmount: 0,
        onopen: null, onclose: null, onmessage: null, onerror: null,
        send() {}, close() { this.readyState = 3; } };
      setTimeout(() => { s.readyState = 1; if (s.onopen) s.onopen({}); }, 0);
      return s;
    };
    Object.assign(window.WebSocket, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });
    window.__clip = [];
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: {
      writeText: t => { window.__clip.push(t); return Promise.resolve(); },
      readText: () => Promise.resolve(""), read: () => Promise.resolve([]) } });
  });
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
    await p.evaluate(() => attachTask("land-live"));
    await p.waitForFunction(() => termSock && termSock.readyState === 1 && termTask && termTask.id === "land-live", null,
      { timeout: 10000 });
    await p.waitForTimeout(300);
    await p.evaluate(() => {
      copyOnSelect = true;
      termSock.onmessage({ data: "alpha beta gamma\r\nalpha delta\r\nalpha epsilon\r\n" });
    });
    await p.waitForTimeout(200);

    // 1. Typing, stepping both ways, and output while the bar is open.
    await p.evaluate(() => openFind());
    await p.click("#t-find-q");
    await p.keyboard.type("alpha", { delay: 20 });
    await p.evaluate(() => { runFind(true); runFind(true); runFind(true, true); });
    await p.evaluate(() => termSock.onmessage({ data: "alpha zeta\r\n" }));
    await p.waitForTimeout(500);
    const find = await p.evaluate(() => ({ sel: term.getSelection(), clip: window.__clip.slice() }));
    if (find.sel !== "alpha") fail("the find bar did not select its match, so this proves nothing: " + JSON.stringify(find));
    if (find.clip.length) fail("copy on select copied from the find bar: " + JSON.stringify(find.clip));
    await p.evaluate(() => closeFind());

    // 2. A drag across the first row copies what it selected.
    const box = await p.locator("#t-screen .xterm-screen").boundingBox();
    const cell = await p.evaluate(() => {
      const d = term._core._renderService.dimensions.css.cell;
      return { w: d.width, h: d.height };
    });
    const y = box.y + cell.h / 2;
    await p.mouse.move(box.x + 1, y);
    await p.mouse.down();
    await p.mouse.move(box.x + cell.w * 5, y, { steps: 5 });
    await p.mouse.move(box.x + cell.w * 10.5, y, { steps: 5 });
    await p.mouse.up();
    await p.waitForTimeout(100);
    const drag = await p.evaluate(() => window.__clip.slice());
    if (drag.length !== 1 || !/^alpha be/.test(drag[0] || "")) fail("a drag did not copy its selection: " + JSON.stringify(drag));

    // 3. A double-click copies the word, a triple-click the line.
    await p.evaluate(() => { term.clearSelection(); window.__clip = []; });
    await p.mouse.dblclick(box.x + cell.w * 7.5, y + cell.h);
    await p.waitForTimeout(100);
    const word = await p.evaluate(() => window.__clip.slice());
    if (!word.includes("delta")) fail("a double-click did not copy the word: " + JSON.stringify(word));
    await p.evaluate(() => { term.clearSelection(); window.__clip = []; });
    await p.mouse.click(box.x + cell.w * 2.5, y + cell.h * 2, { clickCount: 3 });
    await p.waitForTimeout(100);
    const line = await p.evaluate(() => window.__clip.slice());
    if (!line.some(t => /^alpha epsilon\s*$/.test(t))) fail("a triple-click did not copy the line: " + JSON.stringify(line));

    // 4. A plain click that changes nothing copies nothing.
    await p.evaluate(() => { term.clearSelection(); window.__clip = []; });
    await p.mouse.click(box.x + cell.w * 3, y);
    await p.waitForTimeout(100);
    const still = await p.evaluate(() => window.__clip.slice());
    if (still.length) fail("a plain click copied: " + JSON.stringify(still));
  } finally {
    await ctx.close();
  }
  if (errors.length) fail("the copy on select page threw: " + errors.join(" | "));
  landList = []; landPerms = [];
  tasksMode = was;
}

// ── a second press fires nothing (backlog-2 item 19) ─────────────────────
// Every request-firing button goes through `busyWhile` or `oneAtATime` in
// js/core.js. Each case presses twice (or calls twice) while the first request
// is held open, and asserts exactly one request reached the wire. The launch
// dialog also shows its spinner while held, closes on success, and on a refusal
// stays open with the reason in it and the button live again.
async function busyGuardSection(browser, base) {
  const errors = [];
  const ctx = await browser.newContext();
  const hits = {};
  let launchFails = false;
  const hold = (key, reply, ms) => async route => {
    hits[key] = (hits[key] || 0) + 1;
    await new Promise(r => setTimeout(r, ms || 300));
    await route.fulfill(reply());
  };
  const json = body => ({ contentType: "application/json", body: JSON.stringify(body) });
  await ctx.addInitScript(() => {
    localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true, "kill-runner": true }));
  });
  await ctx.route("**/v1/launch", hold("launch", () => launchFails
    ? { status: 409, contentType: "application/json", body: JSON.stringify({ error: "the card is busy" }) }
    : json({ id: "t1", supervised: false })));
  await ctx.route("**/v1/tasks/*/kill", hold("kill", () => json({})));
  await ctx.route("**/v1/permissions/*/decide", hold("decide", () => json({})));
  await ctx.route("**/v1/tasks/*/message", hold("message", () => json({ delivered: "queued", when: "done" })));
  await ctx.route("**/v1/tasks/*/note/send", hold("note", () => json({ delivered: "queued" })));
  await ctx.route("**/v1/tasks/*/messages", route => route.fulfill(json({ messages: [] })));
  await ctx.route("**/v1/settings", route => route.request().method() === "POST"
    ? hold("settings", () => json({ global_auto: false }))(route) : route.fallback());
  await ctx.route("**/__busy/*", route =>
    hold("html:" + route.request().url().split("/__busy/")[1], () => json({}), 100)(route));
  const once = (key, what) => {
    if (hits[key] !== 1) fail(what + " sent " + (hits[key] || 0) + " requests for two presses, want 1.");
  };
  try {
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });

    // 1. The launch dialog: two clicks and an Enter, one request, a spinner while held.
    const openIt = () => p.evaluate(() => {
      launchTarget = { harness: "claude", resume: "conv-1", task_id: "t1" };
      document.getElementById("l-resume-field").hidden = false;
      document.getElementById("l-resume-on").checked = true;
      document.getElementById("launch").showModal();
    });
    await openIt();
    const during = await p.evaluate(() => new Promise(done => {
      const go = document.getElementById("l-go");
      go.click();
      go.click();
      document.getElementById("l-cwd").dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
      doLaunch();
      setTimeout(() => done({ busy: go.getAttribute("aria-busy"), disabled: go.disabled,
        spin: !!go.querySelector(".busy-spin"), label: go.textContent }), 100);
    }));
    await p.waitForTimeout(600);
    once("launch", "the launch dialog's launch button");
    if (during.busy !== "true" || !during.disabled || !during.spin || !/starting/.test(during.label)) {
      fail("a held launch did not show it was working: " + JSON.stringify(during));
    }
    const after = await p.evaluate(() => ({ open: document.getElementById("launch").open,
      label: document.getElementById("l-go").textContent, disabled: document.getElementById("l-go").disabled }));
    if (after.open) fail("a launch that succeeded left its dialog open.");
    if (after.label !== "launch" || after.disabled) fail("the launch button did not come back: " + JSON.stringify(after));

    // 2. A refused launch keeps the dialog, says why in it, and gives the button back.
    launchFails = true;
    await openIt();
    await p.evaluate(() => document.getElementById("l-go").click());
    await p.waitForTimeout(600);
    const refused = await p.evaluate(() => {
      const why = document.querySelector("#launch .busy-why");
      return { open: document.getElementById("launch").open, why: why && why.textContent,
        disabled: document.getElementById("l-go").disabled, label: document.getElementById("l-go").textContent };
    });
    if (!refused.open || refused.why !== "the card is busy" || refused.disabled || refused.label !== "launch") {
      fail("a refused launch did not stay open with the reason and a live button: " + JSON.stringify(refused));
    }
    await p.evaluate(() => document.getElementById("launch").close());
    await p.waitForTimeout(50);
    if (await p.evaluate(() => !!document.querySelector("#launch .busy-why"))) {
      fail("the refusal line outlived its dialog, so the next open wears the last failure.");
    }
    launchFails = false;

    // 3. The card menu's resume, twice on one card.
    hits.launch = 0;
    await p.evaluate(() => Promise.all([
      resumeNow("t1", { runner: "claude", worktree: "D:/w" }, "", "conv-1"),
      resumeNow("t1", { runner: "claude", worktree: "D:/w" }, "", "conv-1")
    ]));
    once("launch", "resume from the card menu");

    // 4. Terminate, twice on one card.
    await p.evaluate(() => Promise.all([killById("t1"), killById("t1")]));
    once("kill", "terminate");

    // 5. A permission card's approve, double clicked, wearing the spinner while held.
    const perm = await p.evaluate(() => new Promise(done => {
      const el = permCard({ id: "pq1", tool: "Bash", command: "ls", requested_at: new Date().toISOString() });
      document.body.appendChild(el);
      const b = el.querySelector(".actions button");
      b.click();
      b.click();
      setTimeout(() => done({ busy: b.getAttribute("aria-busy") }), 100);
    }));
    await p.waitForTimeout(600);
    once("decide", "a permission card's approve");
    if (perm.busy !== "true") fail("a permission answer in flight did not show it: " + JSON.stringify(perm));

    // 6. The say box: send pressed twice and Enter once.
    await p.evaluate(() => {
      current = { id: "t1", status: "running", note: "" };
      document.getElementById("d-say").value = "run the tests";
      const b = document.getElementById("d-say-send");
      b.click();
      b.click();
      sayToCurrent("done");
    });
    await p.waitForTimeout(600);
    once("message", "the say box's send");

    // 7. The note's send it, twice.
    await p.evaluate(() => {
      document.getElementById("d-note").value = "three things";
      return Promise.all([sendNote("done"), sendNote("done")]);
    });
    once("note", "the note's send it");

    // 8. The approve-everything switch, turned off twice.
    await p.evaluate(() => { globalAuto = true; return Promise.all([toggleGlobalAuto(), toggleGlobalAuto()]); });
    once("settings", "the approve-everything switch");

    // 9. Every save, remove and run button wired in the page, each pressed twice
    // and let answer before the next, since a held button also holds its row.
    // The handler is stubbed to one request, so what is counted is the wiring.
    const wired = await p.evaluate(async () => {
      const buttons = [...document.querySelectorAll("button[onclick^='busyWhile(this, ']")];
      const names = [];
      for (const b of buttons) {
        const m = /^busyWhile\(this, (\w+)/.exec(b.getAttribute("onclick"));
        if (!m) continue;
        names.push(m[1]);
        window[m[1]] = () => fetch("/__busy/" + m[1], { method: "POST" });
      }
      for (const b of buttons) {
        b.click();
        b.click();
        await new Promise(r => setTimeout(r, 200));
      }
      return names;
    });
    const distinct = [...new Set(wired)];
    if (distinct.length < 25) fail("only " + distinct.length + " buttons go through busyWhile: " + distinct.join(","));
    for (const n of ["saveHarness", "saveSource", "saveTheme", "keepTheme", "keepSkin", "deleteHarness",
      "doDispatch", "saveProvider", "runSourceNow"]) {
      if (!distinct.includes(n)) fail(n + " does not go through busyWhile.");
    }
    for (const n of distinct) {
      const want = wired.filter(x => x === n).length;
      if (hits["html:" + n] !== want) {
        fail(n + " sent " + (hits["html:" + n] || 0) + " requests for " + want + " button(s) pressed twice each.");
      }
    }
  } finally {
    await ctx.close();
  }
  if (errors.length) fail("the busy guard page threw: " + errors.join(" | "));
}

// The cache keep-alive's cards: one stopped at break-even, one being kept warm,
// one on with nothing spent yet, and one with no switch at all (not Claude).
const KA_CARDS = [
  Object.assign({}, T1, { id: "ka-stop", display_title: "stopped card", keepalive: {
    state: "stopped:break-even", state_at: "2026-09-27T18:00:00Z", refreshes: 5, spent: 0.3, budget: 0.3,
    warm_until: "2026-09-27T17:42:00Z" } }),
  Object.assign({}, T1, { id: "ka-warm", display_title: "warm card", keepalive: {
    state: "on", state_at: "2026-09-27T10:00:00Z", refreshes: 3, spent: 0.18, budget: 0.3,
    warm_until: "2026-09-27T19:00:00Z" } }),
  Object.assign({}, T1, { id: "ka-quiet", display_title: "quiet card", keepalive: {
    state: "on", state_at: "2026-09-27T10:00:00Z", refreshes: 0, spent: 0, budget: 0.3 } }),
  Object.assign({}, T1, { id: "ka-none", display_title: "shell card", runner: "shell" }),
];

// THE CACHE KEEP-ALIVE ON THE BOARD. The switch in the gear's settings sets the
// default for new cards and saves it, a suspension shows with a clear button,
// each card draws the right chip, the card menu's switch posts to the card, and
// a break-even stop pushed on the stream lands in the toast log. See
// js/keepalive.js and docs/cache-keepalive-design.md.
async function keepaliveSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const kp = await ctx.newPage();
  const errors = [];
  kp.on("pageerror", e => errors.push(String(e)));
  const was = tasksMode;
  tasksMode = "keepalive";
  kaWrites = [];
  kaSettings = {
    cache_keepalive_default: true, cache_keepalive_suspended: "two refreshes in a row on two cards missed the cache",
    cache_keepalive_week_usd: 1.25, cache_keepalive_week_refreshes: 21,
  };
  try {
    await kp.goto(base, { waitUntil: "domcontentloaded" });
    await kp.waitForFunction(() => typeof paintKeepaliveSettings === "function" &&
      typeof loadHousekeeping === "function", null, { timeout: 15000 });
    await kp.evaluate(() => loadHousekeeping());

    // The board switch: in the gear's settings, under its own heading, checked
    // because the default is on.
    const sw = await kp.evaluate(() => {
      const el = document.getElementById("s-keepalive");
      if (!el) return { missing: true };
      let h = el.closest(".field");
      while (h && !(h.matches && h.matches("h3.s-section"))) h = h.previousElementSibling;
      return { checked: el.checked, heading: h ? h.textContent.trim() : "",
        dialog: el.closest("dialog") ? el.closest("dialog").id : "",
        spend: document.getElementById("s-keepalive-spend").textContent,
        suspendedShown: !document.getElementById("s-keepalive-suspended").hidden,
        why: document.getElementById("s-keepalive-why").textContent };
    });
    if (sw.missing) fail("the keep-alive switch is not in the settings.");
    if (sw.dialog !== "settings") fail("the keep-alive switch is not in the gear's settings dialog: " + sw.dialog);
    if (sw.heading !== "cache keep-alive") fail("the keep-alive switch sits under " + JSON.stringify(sw.heading));
    if (!sw.checked) fail("the keep-alive switch is not checked when the default is on.");
    if (!/21 refreshes, \$1\.25/.test(sw.spend)) fail("the week's spend reads " + JSON.stringify(sw.spend));
    if (!sw.suspendedShown || !/missed the cache/.test(sw.why)) {
      fail("a suspended room does not say so: " + JSON.stringify(sw));
    }

    // Unchecking it saves the default, and only the default.
    await kp.evaluate(() => {
      const el = document.getElementById("s-keepalive");
      el.checked = false;
      el.dispatchEvent(new Event("change"));
    });
    await kp.waitForFunction(() => true, null, { timeout: 500 }).catch(() => {});
    await new Promise(r => setTimeout(r, 300));
    const saved = kaWrites.find(w => w.url === "/v1/settings" && "cache_keepalive_default" in w.body);
    if (!saved || saved.body.cache_keepalive_default !== false || Object.keys(saved.body).length !== 1) {
      fail("unchecking the switch did not save {cache_keepalive_default:false}: " + JSON.stringify(kaWrites));
    }

    // Clearing the suspension sends false and hides the notice.
    await kp.evaluate(() => clearKeepaliveSuspension());
    const cleared = kaWrites.find(w => w.body.cache_keepalive_suspended === false);
    if (!cleared) fail("clearing the suspension did not post cache_keepalive_suspended:false.");
    const stillShown = await kp.evaluate(() => !document.getElementById("s-keepalive-suspended").hidden);
    if (stillShown) fail("the suspension notice stayed up after it was cleared.");

    // The chips, from the card renderer the board uses.
    const chips = await kp.evaluate(cards => cards.map(t => {
      const box = document.createElement("div");
      box.innerHTML = cardHTML(t);
      const c = box.querySelector(".chip.keepalive");
      return { id: t.id, text: c ? c.textContent.trim() : "", stopped: c ? c.classList.contains("stopped") : false,
        tip: c ? c.getAttribute("data-tip") : "" };
    }), KA_CARDS);
    const by = Object.fromEntries(chips.map(c => [c.id, c]));
    if (!by["ka-stop"].stopped || !/cold/.test(by["ka-stop"].text)) {
      fail("a card stopped at break-even does not draw the stopped chip: " + JSON.stringify(by["ka-stop"]));
    }
    if (!/break-even/.test(by["ka-stop"].tip) || !/5 refreshes, \$0\.30 of a \$0\.30 budget/.test(by["ka-stop"].tip)) {
      fail("the stopped chip's tooltip does not carry the spend: " + JSON.stringify(by["ka-stop"].tip));
    }
    if (!/warm/.test(by["ka-warm"].text) || !/kept warm 3x, \$0\.18 of \$0\.30/.test(by["ka-warm"].tip)) {
      fail("a card being kept warm does not say so: " + JSON.stringify(by["ka-warm"]));
    }
    if (by["ka-quiet"].text) fail("a card with nothing spent drew a keep-alive chip.");
    if (by["ka-none"].text) fail("a card with no switch drew a keep-alive chip.");

    // The card menu's switch: offered on a Claude card, reads its state, and
    // posts to that card. Not offered on a card with no switch.
    const menu = await kp.evaluate(async cards => {
      const warm = keepaliveMenuItem(cards[1], () => {});
      const none = keepaliveMenuItem(cards[3], () => {});
      const stop = keepaliveMenuItem(cards[0], () => {});
      await warm.act();
      await stop.act();
      return { warmLabel: warm.label, warmOn: warm.on, none, stopOn: stop.on, stopHelp: stop.help };
    }, KA_CARDS);
    if (menu.none !== null) fail("a card with no switch was offered one in its menu.");
    if (menu.warmLabel !== "keep its cache warm" || menu.warmOn !== true) {
      fail("the card menu's switch reads wrong: " + JSON.stringify(menu));
    }
    if (menu.stopOn !== false || !/break-even/.test(menu.stopHelp)) {
      fail("a stopped card's menu switch does not read off with its reason: " + JSON.stringify(menu));
    }
    const offPost = kaWrites.find(w => w.url === "/v1/tasks/ka-warm/keepalive");
    const onPost = kaWrites.find(w => w.url === "/v1/tasks/ka-stop/keepalive");
    if (!offPost || offPost.body.on !== false) fail("turning a warm card off did not post on:false.");
    if (!onPost || onPost.body.on !== true) fail("turning a stopped card back on did not post on:true.");

    // A break-even stop pushed on the stream is a toast, and so in the toast log.
    await kp.evaluate(() => localStorage.removeItem("atrium.toastlog"));
    const payload = JSON.stringify({ task_id: "ka-stop", state: "stopped:break-even",
      toast: "keep-alive stopped on stopped card at break-even after 5 refreshes, $0.30" });
    openStreams.forEach(r => { try { r.write("event: keepalive\ndata: " + payload + "\n\n"); } catch (e) {} });
    await kp.waitForFunction(() => {
      try { return JSON.parse(localStorage.getItem("atrium.toastlog") || "[]")
        .some(t => /break-even after 5 refreshes/.test(t.body || "")); } catch (e) { return false; }
    }, null, { timeout: 5000 }).catch(() => fail("a break-even stop on the stream did not reach the toast log."));
  } finally {
    await ctx.close();
    tasksMode = was;
    kaSettings = {};
  }
  if (errors.length) fail("the keep-alive page threw: " + errors.join(" | "));
}

// ── the board skin follows the room-picker scope ────────────────────────
// THREE SCOPES, THREE SKINS, HELD AT ONCE. The ALL view wears the hub's skin,
// and each of two rooms wears its own, so scoping ALL -> alpha -> sgg reads
// three different skins back. Then: a save in one scope reaches only that
// scope and leaks into no other, switching scope re-applies each
// independently, and a room attaching does not clobber the ALL skin. A fresh
// context keeps this test's per-scope localStorage out of the others'.
// Both rooms live, so all three scopes are pickable.
async function skinScopeSection(browser, base) {
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
  // wears waits for the board to wear a skin (null is the default) and names
  // the step that did not, with what it wore instead. Returns whether it did.
  const wears = (want, what) => skin.waitForFunction(w =>
    document.documentElement.getAttribute("data-skin") === w, want, { timeout: 15000 })
    .then(() => true, async () => {
      fail(what + ": the board wore " + JSON.stringify(await dataSkin()) + ", wanted " +
        JSON.stringify(want) + ". The mock holds " + JSON.stringify(skinFor));
      return false;
    });
  // THE LOAD'S OWN SETTINGS READS HAVE TO LAND BEFORE A SAVE. The remembered
  // skin paints at once, so the right skin on screen does not mean the board has
  // finished reading. A save made then was painted over by a read answered
  // before it, and the wait for the new skin ran out: the flaky main-flow
  // timeout. Counted per request, so this waits on the reads, not a clock.
  // A reload abandons the old page's reads without always saying so, so the set
  // starts over when the page does.
  const settingsOut = new Set();
  const isSettings = r => new URL(r.url()).pathname === "/v1/settings";
  skin.on("request", r => { if (isSettings(r)) settingsOut.add(r); });
  skin.on("requestfinished", r => settingsOut.delete(r));
  skin.on("requestfailed", r => settingsOut.delete(r));
  skin.on("framenavigated", f => { if (f === skin.mainFrame()) settingsOut.clear(); });
  const settled = async what => {
    for (const end = Date.now() + 15000; settingsOut.size && Date.now() < end;) {
      await skin.waitForTimeout(50);
    }
    if (settingsOut.size) fail(what + ": the settings reads never finished.");
    // One more turn of the page, so the last answer has been painted.
    await skin.evaluate(() => new Promise(r => setTimeout(r, 0)));
  };
  // scopeTo reloads the board into a scope (null for ALL) and waits for the
  // skin that scope wears to land and the reads behind it to finish.
  const scopeTo = async (room, want) => {
    await Promise.all([
      skin.waitForNavigation({ waitUntil: "domcontentloaded" }),
      skin.evaluate(r => pickRoom(r), room)
    ]);
    const ok = await wears(want, "scoped to " + (room || "ALL"));
    await settled("scoped to " + (room || "ALL"));
    return ok;
  };
  try {
    // ALL scope: the hub's own skin, not the alphabetically-first room's.
    await skin.goto(base, { waitUntil: "domcontentloaded" });
    await wears("noir", "the ALL view on load");

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
    await wears("vapor", "a skin saved from the ALL view");
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
    await wears("sandstone", "a skin saved while scoped to alpha");
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
    await wears(null, "a skin saved while scoped to sgg");
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
    await wears(null, "scoped back to sgg");

    // Back to ALL: the hub skin is what it was, and a room attaching or
    // leaving does not change it. This is the bug clint hit on a deploy: a
    // room connecting swapped his theme to its own skin and its leaving
    // reverted it, though he never changed scope off ALL.
    //
    // To catch it, the answer a re-read WOULD give is moved out from under the
    // settled skin: skinFor[""] is changed to a different skin, so any re-fetch
    // of the ALL scope now returns `ember`. A room attaching or detaching must
    // still leave the applied `vapor` alone, because the skin has settled and
    // ALL is not the scope of the room that changed. The old code re-read the
    // skin on every attached-set flip and would repaint to `ember` here.
    await scopeTo(null, "vapor");
    const allWas = skinFor[""];
    skinFor[""] = "ember";
    // Past loadHubRooms' 2s throttle, so the `rooms` event below actually runs
    // its body rather than being coalesced away. Then a room leaves and one
    // attaches: two changes to the attached set, neither of which is the ALL
    // scope the operator is on, so the settled skin must not move.
    await skin.waitForTimeout(2200);
    sggAttached = false;
    hubStreams.forEach(r => { try { r.write("event: rooms\ndata: {}\n\n"); } catch (e) {} });
    await skin.waitForTimeout(2200);
    sggAttached = true;
    hubStreams.forEach(r => { try { r.write("event: rooms\ndata: {}\n\n"); } catch (e) {} });
    await skin.waitForTimeout(500);
    if ((await dataSkin()) !== "vapor") {
      fail("a room attaching or leaving clobbered the settled ALL skin: it " +
        "became " + JSON.stringify(await dataSkin()) + ", wanted the applied vapor.");
    }
    skinFor[""] = allWas;
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
}

// ── a persisted skin heals when a room attaches, with no reload ──────────
// The board loads against a hub that has no room to borrow settings from
// yet, the window right after a hub restart. The ALL-view `/v1/settings`
// read is a 409, so the load-time skin read fails and the board is on the
// default dark. This is exactly what clint saw: a deploy restarts the hub,
// he reloads before a room is back, and the paper skin never paints. When a
// room attaches the read succeeds, and the skin must heal there rather than
// waiting for another manual reload.
async function skinHealSection(browser, base) {
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
      document.documentElement.getAttribute("data-skin") === "paper", null, { timeout: 15000 });
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
}

// ── the history view paints rows ──────────────────────────────────────
async function historySection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 720 } });
  const page = await ctx.newPage();
  await page.addInitScript(() => {
    let all = {};
    try { all = JSON.parse(localStorage.getItem("atrium.skipconfirm") || "{}"); } catch (e) {}
    all["width-floor"] = true;
    localStorage.setItem("atrium.skipconfirm", JSON.stringify(all));
  });
  try {
  await page.goto(base, { waitUntil: "domcontentloaded" });
  await page.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
  await page.click('.tab[data-view="history"]');
  await page.waitForSelector("#history-list .row.line", { timeout: 15000 });
  const histRows = await page.locator("#history-list .row.line").count();
  if (histRows < 1) fail("the history view painted no rows from /v1/history.");

  // ── a long history scrolls in its own box, filters stay put ───────────
  // main clips, so a view that is not a scroll box of its own can never show
  // the rows below the window. Two pages loaded, then checked at desktop and
  // phone widths.
  histMany = true;
  await page.evaluate(() => renderHistory(false));
  await page.waitForFunction(() =>
    document.querySelectorAll("#history-list .row.line").length === 100, null,
    { timeout: 15000 }).catch(() => fail("the history view did not draw the long list."));
  await page.evaluate(() => moreHistory());
  await page.waitForFunction(() =>
    document.querySelectorAll("#history-list .row.line").length === 200, null,
    { timeout: 15000 }).catch(() => fail("show more did not add the second history page."));
  for (const vp of [{ width: 1280, height: 800 }, { width: 390, height: 780 }]) {
    await page.setViewportSize(vp);
    const sc = await page.evaluate(() => {
      const list = document.getElementById("history-list");
      const bar = document.querySelector("#history > .toolbar");
      list.scrollTop = 0;
      const before = bar.getBoundingClientRect().top;
      const tall = { sh: list.scrollHeight, ch: list.clientHeight };
      list.scrollTop = 600;
      return Object.assign(tall, {
        moved: list.scrollTop,
        barMoved: bar.getBoundingClientRect().top - before,
        barOnScreen: bar.getBoundingClientRect().bottom <= window.innerHeight
      });
    });
    if (!(sc.sh > sc.ch) || sc.moved <= 0) {
      fail("the history list does not scroll at " + vp.width + "px: " + JSON.stringify(sc));
    }
    if (sc.barMoved !== 0 || !sc.barOnScreen) {
      fail("the history search bar moved with the list at " + vp.width + "px: " + JSON.stringify(sc));
    }
  }
  await page.setViewportSize({ width: 1280, height: 800 });

  // ── a live repaint keeps the pages and the reader's row ───────────────
  // A board event repaints the open view. It re-reads both pages rather than
  // cutting back to one, and a new run on top does not move the row a reader
  // who has scrolled down is on.
  const hHeld = await page.evaluate(() => {
    const list = document.getElementById("history-list");
    list.scrollTop = 3000;
    const top = list.getBoundingClientRect().top;
    const row = [...list.querySelectorAll(".row.line")]
      .find(r => r.getBoundingClientRect().bottom > top);
    return { scrollTop: list.scrollTop, id: row.dataset.id,
      offset: row.getBoundingClientRect().top - top };
  });
  histManyLive = true;
  await page.evaluate(() => repaintLists());
  await page.waitForFunction(() =>
    document.querySelector("#history-list .row.line").dataset.id === "hm251", null,
    { timeout: 15000 }).catch(() => fail("the live history repaint did not draw the new run."));
  const hLate = await page.evaluate((id) => {
    const list = document.getElementById("history-list");
    const row = list.querySelector('.row.line[data-id="' + id + '"]');
    return { rows: list.querySelectorAll(".row.line").length, scrollTop: list.scrollTop,
      offset: row ? row.getBoundingClientRect().top - list.getBoundingClientRect().top : null };
  }, hHeld.id);
  if (hLate.rows < 200) {
    fail("a live history repaint cut the list back to one page: " + JSON.stringify(hLate));
  }
  if (hLate.scrollTop < hHeld.scrollTop || hLate.offset === null ||
      Math.abs(hLate.offset - hHeld.offset) > 1) {
    fail("a live history repaint moved the reader: " + JSON.stringify({ hHeld, hLate }));
  }

  // ── a new search starts at the top ────────────────────────────────────
  await page.evaluate(() => renderHistory(false));
  await page.waitForFunction(() =>
    document.querySelectorAll("#history-list .row.line").length === 100, null,
    { timeout: 15000 }).catch(() => fail("a fresh history load did not go back to one page."));
  const hTop = await page.evaluate(() => document.getElementById("history-list").scrollTop);
  if (hTop !== 0) fail("a fresh history load kept the old scroll: " + hTop);
  histMany = false; histManyLive = false;
  await page.evaluate(() => renderHistory(false));
  } finally {
    await ctx.close();
  }
}

// Where in this file a throw came from. A bare "Timeout 30000ms exceeded" names
// no wait, so a failure nobody can run alone could not even be found.
function threwAt(e) {
  const at = String((e && e.stack) || "").split("\n")
    .find(l => /test-board-headless\.js:\d+/.test(l));
  return at ? " (at " + at.trim().replace(/^at /, "") + ")" : "";
}

async function main() {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const base = "http://127.0.0.1:" + server.address().port;

  const browser = await chromium.launch();
  // HEADLESS_ONLY=termWear,bridge runs just those sections, for working on one.
  if (process.env.HEADLESS_ONLY) {
    const only = { termWear: termWearSection, bridge: bridgeSection, settingsOnce: settingsOnceSection,
      groupRemove: groupRemoveSection, worn: wornSection, restartGate: restartGateSection, restartStays: restartStaysSection, atriumDown: atriumDownSection,
      toastStays: toastStaysSection, groupColor: groupColorSection,
      groupDrag: groupDragSection, tooltip: tooltipSection, popoutTagFlip: popoutTagFlipSection, idleRate: idleRateSection, foldStill: foldStillSection,
      untaggedSort: untaggedSortSection, newCard: newCardSection, themePreview: themePreviewSection, land: landSection, reselect: reselectSection,
      toastsTop: toastsTopSection, sayWhen: sayWhenSection, pasteSpinner: pasteSpinnerSection,
      copySelect: copySelectSection, busyGuard: busyGuardSection, keepalive: keepaliveSection,
      skinScope: skinScopeSection, skinHeal: skinHealSection,
      history: historySection };
    try {
      for (const n of process.env.HEADLESS_ONLY.split(",")) await only[n](browser, base);
    } catch (e) { fail("the headless run threw: " + (e && e.message ? e.message : e) + threwAt(e)); }
    await browser.close();
    openStreams.forEach(r => { try { r.destroy(); } catch (e) {} });
    await new Promise(r => server.close(r));
    if (bad) process.exit(1);
    console.log("the sections asked for passed: " + process.env.HEADLESS_ONLY);
    return;
  }
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
  // The width-floor notice is a modal, and a narrow terminal pane in this run
  // would raise it over every later click. It is tested on its own below.
  await page.addInitScript(() => {
    let all = {};
    try { all = JSON.parse(localStorage.getItem("atrium.skipconfirm") || "{}"); } catch (e) {}
    all["width-floor"] = true;
    localStorage.setItem("atrium.skipconfirm", JSON.stringify(all));
  });

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

    // ── an unread turn and its open questions are marked ──────────────────
    // A dot on the card whose last turn nobody saw, `? 2` for its two open
    // questions, and neither on the card with no seen state. On the stack and
    // on the terminal strip, which is where the operator is while workers run.
    tasksMode = "seen";
    await page.evaluate(() => runRefresh());
    await page.waitForSelector('#stack-list .stackrow[data-id="seen1"] .chip.unseen',
      { state: "attached", timeout: 15000 });
    const seenMarks = await page.evaluate(async () => {
      await renderTermList();
      const row = document.querySelector('#stack-list .stackrow[data-id="seen1"]');
      const plain = document.querySelector('#stack-list .stackrow[data-id="t1"]');
      const tab = document.querySelector('#term-list .card.tab[data-id="seen1"]');
      return {
        q: row && (row.querySelector(".chip.questions") || {}).textContent,
        plain: plain ? plain.querySelectorAll(".chip.unseen, .chip.questions").length : -1,
        tab: tab ? !!tab.querySelector(".chip.unseen") : null,
      };
    });
    if (!seenMarks.q || seenMarks.q.replace(/\s+/g, " ").trim() !== "? 2") {
      fail("the card with two open questions did not draw `? 2`: " + JSON.stringify(seenMarks));
    }
    if (seenMarks.plain !== 0) fail("a card with no seen state drew a seen mark");
    if (seenMarks.tab === false) fail("the terminal strip row did not draw the unread dot");
    tasksMode = "first";
    await page.evaluate(() => runRefresh());

    // ── the history view paints rows ──────────────────────────────────────
    await historySection(browser, base);

    // ── changing runners pane goes back to the top ────────────────────────
    // `#runners` is the scroll box, not main. A short window so the page has
    // something to scroll.
    await page.setViewportSize({ width: 1280, height: 320 });
    const rp = await page.evaluate(() => {
      switchView("runners");
      const host = document.getElementById("runners");
      const btns = [...host.querySelectorAll(".pane-nav button")];
      if (btns.length < 2) return { error: "no pane nav" };
      btns[0].click();
      host.scrollTop = 150;
      const scrolled = host.scrollTop;
      btns[1].click();
      return { scrolled, after: host.scrollTop };
    });
    if (rp.error || rp.scrolled <= 0) {
      fail("the runners page did not scroll in its own box: " + JSON.stringify(rp));
    } else if (rp.after !== 0) {
      fail("changing runners pane left the page scrolled: " + JSON.stringify(rp));
    }
    await page.evaluate(() => switchView("history"));
    await page.setViewportSize({ width: 1280, height: 720 });

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
      }, null, { timeout: 15000 });
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

    // ── a pinned card filed into one of your groups is drawn in it ───────────
    // Custom grouping with one group, `active`, and a pinned card carrying that
    // tag. The group must count it and draw it, not say 0 over an empty-state
    // hint. Then the drag case: a row moved by hand into the group's nest, and a
    // repaint whose markup did not change, must still put the rows back where the
    // markup says, since that stale DOM is what drew the card under `active 0`.
    tasksMode = "filed";
    await page.evaluate(() => {
      localStorage.setItem("atrium.grouping",
        JSON.stringify({ on: true, mode: "custom", groups: ["active"] }));
      renderTermList();
    });
    await page.waitForSelector('#term-list .tnest[data-group="active"] .card.tab[data-id="filed1"]',
      { state: "attached", timeout: 15000 }).catch(() => {});
    const filedState = () => page.evaluate(() => {
      const nest = document.querySelector('#term-list .tnest[data-group="active"]');
      const head = nest && nest.previousElementSibling;
      const bucket = document.querySelector("#term-list .termbucket");
      return {
        inGroup: !!(nest && nest.querySelector('.card.tab[data-id="filed1"]')),
        count: head ? head.querySelector(".tgcount").textContent.trim() : "",
        hint: !!(nest && nest.querySelector(".groupempty")),
        inBucket: !!(bucket && bucket.querySelector('.card.tab[data-id="filed1"]')),
        strays: nest ? nest.querySelectorAll('.card.tab[data-id="loose1"]').length : -1
      };
    });
    let fs1 = await filedState();
    if (!fs1.inGroup || fs1.count !== "1" || fs1.hint) {
      fail("a pinned card filed into a custom group is not drawn in it: " + JSON.stringify(fs1));
    }
    if (!fs1.inBucket) fail("a pinned card filed into a group left the pinned bucket.");
    await page.evaluate(() => {
      const nest = document.querySelector('#term-list .tnest[data-group="active"]');
      const stray = document.querySelector('#term-list .card.tab[data-id="loose1"]');
      if (nest && stray) nest.appendChild(stray);
      repaintTermList();
    });
    await page.waitForTimeout(300);
    fs1 = await filedState();
    if (fs1.strays !== 0) {
      fail("a row dragged into a group stayed there after a repaint with unchanged markup.");
    }
    await page.evaluate(() => localStorage.removeItem("atrium.grouping"));
    tasksMode = "first";

    // ── the two independent "hide inactive" toggles (agents and subagents) ────
    // The strip carries a WORKING SUBAGENT, an IDLE (supervised, not computing)
    // SUBAGENT and a DEAD (cold, pinned) subagent, plus an IDLE-BUT-LIVE AGENT and
    // a DEAD (cold, pinned) agent. The control is one segmented pill with two
    // segments that toggle independently. The two segments read DIFFERENT inactive
    // signals: the subagents segment keeps only the WORKING subagent and hides the
    // idle AND the dead one (not-working-right-now), while the agents segment keeps
    // any LIVE agent - the idle one included - and hides only the dead one (no
    // live connection). The idle subagent hiding while the idle agent stays is the
    // whole point: same idle state, different rule. Pinning exempts nothing: the
    // dead rows are pinned and still hide. And the agents toggle hides exactly the
    // agent rows drawn grey (`.cold`), never more or fewer. The on/off combinations are
    // each asserted, so the two toggles are proven independent. Driven through the
    // board's own functions so the device-scoped persistence for BOTH keys is
    // exercised, not faked.
    tasksMode = "doers";
    const hideState = () => page.evaluate(() => {
      const has = id => !!document.querySelector(`#term-list .card.tab[data-id="${id}"]`);
      const seg = which => {
        const btns = [...document.querySelectorAll("#term-list .termhide button")];
        return btns.find(b => new RegExp("^" + which + "\\b").test(b.textContent.trim())) || null;
      };
      const a = seg("agents"), s = seg("subagents");
      return {
        idleSub: has("sublive"), workingSub: has("subwork"), deadSub: has("subdead"),
        liveAgent: has("aglive"), deadAgent: has("agdead"),
        // Agent (non-doer) rows drawn grey right now. With the agents toggle on
        // this must be empty: grey and hidden are one predicate.
        coldAgents: [...document.querySelectorAll("#term-list .card.tab.cold")]
          .map(c => c.dataset.id).filter(id => id === "aglive" || id === "agdead"),
        // The pinned bucket's heading count and its empty line, and every other
        // heading's count, for the shown/total checks.
        pinnedCount: (document.querySelector("#term-list .pinnedhead .tgcount") || {})
          .textContent || "",
        pinnedEmpty: ((document.querySelector("#term-list .termbucket .bucketdrop") || {})
          .textContent || "").trim(),
        groupCounts: [...document.querySelectorAll(
          "#term-list .tgroup:not(.pinnedhead) .tgcount")].map(c => c.textContent.trim()),
        agentMode: hideAgentsMode(), subMode: hideSubagentsMode(),
        agentLit: !!(a && a.classList.contains("on")),
        subLit: !!(s && s.classList.contains("on")),
        agentLabel: a ? a.textContent.trim() : "",
        subLabel: s ? s.textContent.trim() : "",
        subTitle: s ? s.dataset.tip || "" : "",
        onePill: document.querySelectorAll("#term-list .termhide").length === 1,
        segCount: document.querySelectorAll("#term-list .termhide button").length
      };
    });

    // DEFAULT, with neither key ever set: the subagents side is ON and the agents
    // side OFF. So the idle and the dead (pinned) subagent are hidden out of the
    // box (count 2), the working one stays, and both agent rows stay (the
    // idle-but-live one and the dead one). This is the "subagents default on,
    // agents default off" contract. A stale value is written under the legacy `atrium.hidedoers`
    // key to prove it does NOT override the new default: the toggle now reads the
    // renamed `atrium.hidesubagents` key, so a prior test click under the old name
    // is inert and the intended default shows.
    await page.evaluate(async () => {
      try {
        localStorage.setItem(termDeviceKey("atrium.hidedoers"), "none");
        localStorage.removeItem(termDeviceKey("atrium.hidesubagents"));
        localStorage.removeItem(termDeviceKey("atrium.hideagents"));
      } catch (e) {}
      await renderTermList();
    });
    await page.waitForSelector('#term-list .card.tab[data-id="subwork"]',
      { state: "attached", timeout: 15000 });
    const hDef = await hideState();
    if (hDef.subMode !== "on" || hDef.agentMode !== "none") {
      fail("the hide defaults are not subagents-on / agents-off when unset: " +
        JSON.stringify(hDef));
    }
    if (hDef.idleSub || hDef.deadSub) {
      fail("the subagents side did not default on: an idle or dead (pinned) " +
        "subagent was still shown: " + JSON.stringify(hDef));
    }
    if (!hDef.workingSub || !hDef.liveAgent || !hDef.deadAgent) {
      fail("the default state hid a row it should not have (the working subagent, " +
        "or an agent): " + JSON.stringify(hDef));
    }
    if (!hDef.subLit || hDef.agentLit || !/^subagents \(2\)$/.test(hDef.subLabel)) {
      fail("the default did not light the subagents segment alone with a count of " +
        "2 (idle, dead): " + JSON.stringify(hDef));
    }
    // The pinned heading counts shown out of total: the dead pinned subagent is
    // hidden, the dead pinned agent is not, so `1/2`.
    if (hDef.pinnedCount !== "1/2") {
      fail("the pinned heading does not read shown/total (1/2) with a pinned row " +
        "hidden: " + JSON.stringify(hDef));
    }
    // The subagents segment says, in its tooltip, that a subagent is an
    // atrium-launched session, so the word is not left to guess at.
    if (!/atrium/i.test(hDef.subTitle) || !/origin:agent/.test(hDef.subTitle)) {
      fail("the subagents segment tooltip does not explain these are atrium " +
        "(origin:agent) subagents: " + JSON.stringify(hDef.subTitle));
    }

    // NEITHER on: every row is drawn, both segments unlit, and it is ONE pill of
    // two segments rather than two loose buttons.
    await page.evaluate(async () => {
      setHideSubagents("none"); setHideAgents("none");
      await renderTermList();
    });
    const hNone = await hideState();
    if (!hNone.idleSub || !hNone.workingSub || !hNone.deadSub ||
        !hNone.liveAgent || !hNone.deadAgent) {
      fail("with neither toggle on the strip did not draw all five rows: " +
        JSON.stringify(hNone));
    }
    // The dead pinned agent is drawn grey, and it is the only grey agent: this is
    // the set the agents toggle must take out below.
    if (hNone.coldAgents.join() !== "agdead") {
      fail("with neither toggle on the grey agent rows are not exactly the dead " +
        "one: " + JSON.stringify(hNone.coldAgents));
    }
    // Nothing hidden, so every heading reads its plain count.
    if (hNone.pinnedCount !== "2" || hNone.groupCounts.some(c => c.includes("/"))) {
      fail("with nothing hidden a heading still reads shown/total: " +
        JSON.stringify(hNone));
    }
    if (hNone.agentMode !== "none" || hNone.subMode !== "none" ||
        hNone.agentLit || hNone.subLit) {
      fail("the hide pill was not in the unlit `none`/`none` state: " +
        JSON.stringify(hNone));
    }
    if (!hNone.onePill || hNone.segCount !== 2) {
      fail("the hide control is not one pill with exactly two segments: " +
        JSON.stringify(hNone));
    }
    if (!/^agents$/.test(hNone.agentLabel) || !/^subagents$/.test(hNone.subLabel)) {
      fail("the hide segments are not labelled 'agents' and 'subagents': " +
        JSON.stringify(hNone));
    }

    // SUBAGENTS on, agents off: the idle subagent goes (not working right now), and
    // so does the dead one, pin or no pin. The working one stays. Both agent rows
    // stay (the agents toggle is off). The subagents segment lights with its
    // hidden count of 2; the agents segment stays unlit. Independence on one side,
    // plus the idle-subagent-hides-but-idle-agent-stays proof.
    await page.evaluate(async () => { setHideSubagents("on"); await renderTermList(); });
    const hSub = await hideState();
    if (hSub.idleSub) {
      fail("the subagents toggle left an idle subagent in the strip: an unpinned " +
        "subagent that is not working right now must hide.");
    }
    if (hSub.deadSub) {
      fail("the subagents toggle left a PINNED subagent that exited in the strip: " +
        "a pin does not exempt a row from hide inactive.");
    }
    if (!hSub.workingSub) {
      fail("the subagents toggle hid the WORKING subagent: an actively-computing " +
        "subagent must stay.");
    }
    if (!hSub.liveAgent || !hSub.deadAgent) {
      fail("the subagents toggle also hid an AGENT row: the two toggles are not " +
        "independent: " + JSON.stringify(hSub));
    }
    if (hSub.subMode !== "on" || !hSub.subLit || hSub.agentLit ||
        hSub.agentMode !== "none") {
      fail("the subagents toggle did not light its own segment alone: " +
        JSON.stringify(hSub));
    }
    if (!/^subagents \(2\)$/.test(hSub.subLabel)) {
      fail("the lit subagents segment did not show its hidden count of 2 (idle, " +
        "dead) in parens: " + JSON.stringify(hSub));
    }

    // AGENTS on too: now BOTH are on. The dead agent goes although it is pinned,
    // the idle-but-live agent stays (its own rule is liveness, not working-now),
    // the working subagent still stays, and both inactive subagents stay hidden.
    // No grey agent row is left. Both segments are lit at once, which
    // agent|shell (one-of-two) cannot do.
    await page.evaluate(async () => { setHideAgents("on"); await renderTermList(); });
    const hBoth = await hideState();
    if (hBoth.idleSub || hBoth.deadSub) {
      fail("with both toggles on an inactive subagent survived: " + JSON.stringify(hBoth));
    }
    if (hBoth.deadAgent || hBoth.coldAgents.length) {
      fail("with both toggles on a grey (cold, pinned) agent was still listed: " +
        JSON.stringify(hBoth));
    }
    if (!hBoth.workingSub || !hBoth.liveAgent) {
      fail("with both toggles on the working subagent or the live agent was " +
        "hidden: " + JSON.stringify(hBoth));
    }
    if (!hBoth.agentLit || !hBoth.subLit ||
        hBoth.agentMode !== "on" || hBoth.subMode !== "on") {
      fail("both segments are not lit with both toggles on: " + JSON.stringify(hBoth));
    }
    if (!/^agents \(1\)$/.test(hBoth.agentLabel) ||
        !/^subagents \(2\)$/.test(hBoth.subLabel)) {
      fail("the two lit segments did not show hidden counts of 1 (dead agent) " +
        "and 2 (idle, dead subagent): " + JSON.stringify(hBoth));
    }
    // Both pinned rows hidden: the bucket reads `0/2` and says why it is empty
    // rather than offering a first drag. The idle subagent is unpinned, so some
    // group heading below also reads shown/total.
    if (hBoth.pinnedCount !== "0/2" || !/^2 hidden by hide inactive$/.test(hBoth.pinnedEmpty)) {
      fail("with every pinned row hidden the bucket does not read 0/2 and say so: " +
        JSON.stringify(hBoth));
    }
    // Grouped by age every unpinned row lands in one bucket: three rows, the idle
    // subagent hidden, so `2/3`.
    const byAge = await page.evaluate(async () => {
      const prev = localStorage.getItem(GROUPING_KEY);
      localStorage.setItem(GROUPING_KEY, JSON.stringify({ on: true, mode: "recency", by: "" }));
      await renderTermList();
      const counts = [...document.querySelectorAll(
        "#term-list .tgroup:not(.pinnedhead) .tgcount")].map(c => c.textContent.trim());
      if (prev === null) localStorage.removeItem(GROUPING_KEY);
      else localStorage.setItem(GROUPING_KEY, prev);
      await renderTermList();
      return counts;
    });
    if (byAge.join() !== "2/3") {
      fail("grouped by age with the idle subagent hidden, the one group heading " +
        "does not read 2/3: " + JSON.stringify(byAge));
    }

    // AGENTS on, subagents off: the other independence check. Turning the
    // subagents side back off restores the idle subagent while the agents toggle
    // stays lit. So the agents toggle held its state across the subagents toggle
    // flipping, which is the two-keys-persist-independently proof.
    await page.evaluate(async () => { toggleHideSubagents(); await renderTermList(); });
    const hAgent = await hideState();
    if (!hAgent.idleSub || !hAgent.workingSub || !hAgent.deadSub) {
      fail("turning the subagents toggle off did not restore the subagent rows: " +
        JSON.stringify(hAgent));
    }
    if (hAgent.deadAgent || hAgent.coldAgents.length) {
      fail("the agents toggle left a grey (cold, pinned) agent in the strip: " +
        JSON.stringify(hAgent));
    }
    if (!hAgent.liveAgent) {
      fail("the agents-only state hid the alive agent: " + JSON.stringify(hAgent));
    }
    if (hAgent.agentMode !== "on" || !hAgent.agentLit ||
        hAgent.subMode !== "none" || hAgent.subLit) {
      fail("the agents-only state did not light the agents segment alone: " +
        JSON.stringify(hAgent));
    }

    // Both off again: every row comes back, so hiding is a view, not a deletion.
    await page.evaluate(async () => { toggleHideAgents(); await renderTermList(); });
    const hBack = await hideState();
    if (!hBack.idleSub || !hBack.workingSub || !hBack.deadSub ||
        !hBack.liveAgent || !hBack.deadAgent ||
        hBack.agentMode !== "none" || hBack.subMode !== "none") {
      fail("turning both toggles off did not restore every row and reset both " +
        "segments: " + JSON.stringify(hBack));
    }

    // ── the controls tray ────────────────────────────────────────────────────
    // The sort/hide/group controls are a panel of their own at the top of the
    // list, in flow, and the rows scroll in `.termscroll` beneath it, so no card
    // passes under the controls. It rolls up to one summary line (the default)
    // and down to the controls, and the choice is kept per device.
    // On the terminals view, so the geometry below is measured on a laid-out
    // list, and with the hide toggles back at their defaults.
    await page.click('.tab[data-view="terms"]');
    await page.evaluate(async () => {
      localStorage.removeItem(termDeviceKey("atrium.termtray"));
      localStorage.removeItem(termDeviceKey("atrium.hidesubagents"));
      localStorage.removeItem(termDeviceKey("atrium.hideagents"));
      await renderTermList();
    });
    const trayShut = await page.evaluate(() => {
      const tray = document.querySelector("#term-list .termtray");
      const scroll = document.querySelector("#term-list .termscroll");
      const body = tray && tray.querySelector(".traybody");
      return {
        tray: !!tray, scroll: !!scroll,
        trayInScroll: !!(tray && scroll && scroll.contains(tray)),
        trayFirst: !!(tray && scroll && (tray.compareDocumentPosition(scroll) &
          Node.DOCUMENT_POSITION_FOLLOWING)),
        trayPos: tray ? getComputedStyle(tray).position : "",
        listOverflow: getComputedStyle(document.getElementById("term-list")).overflowY,
        scrollOverflow: scroll ? getComputedStyle(scroll).overflowY : "",
        open: !!(tray && tray.classList.contains("open")),
        inert: !!(body && body.inert),
        bodyH: body ? body.getBoundingClientRect().height : -1,
        summary: ((tray && tray.querySelector(".traysum")) || {}).textContent || "",
        widthBtns: tray ? tray.querySelectorAll(".traybar .tlcycle").length : 0
      };
    });
    if (!trayShut.tray || !trayShut.scroll || trayShut.trayInScroll || !trayShut.trayFirst) {
      fail("the controls are not a tray above a separate scrolling box of rows: " +
        JSON.stringify(trayShut));
    }
    if (trayShut.trayPos === "sticky" || trayShut.listOverflow !== "hidden" ||
        trayShut.scrollOverflow !== "auto") {
      fail("the list still scrolls under its controls (sticky, or the list itself " +
        "scrolls): " + JSON.stringify(trayShut));
    }
    if (trayShut.open || !trayShut.inert || trayShut.bodyH > 1) {
      fail("the tray is not folded by default (open, not inert, or its body has " +
        "height): " + JSON.stringify(trayShut));
    }
    // The subagents toggle defaults on and hides the idle and the dead subagent.
    if (!/^sorted by (name|activity) · (by \w+|ungrouped) · hiding inactive subagents \(2\)$/
        .test(trayShut.summary)) {
      fail("the folded tray does not summarise sort, grouping and hiding in one " +
        "line: " + JSON.stringify(trayShut.summary));
    }
    if (trayShut.widthBtns < 1) {
      fail("the list's width buttons are not on the folded tray's bar: " +
        JSON.stringify(trayShut));
    }

    // Open it: the key is written for this device, the body is live and has
    // height once the roll has run, and the three rows are there, labelled.
    await page.evaluate(() => toggleTermTray());
    await page.waitForTimeout(450);
    const trayOpen = await page.evaluate(() => {
      const tray = document.querySelector("#term-list .termtray");
      const body = tray.querySelector(".traybody");
      const pills = [...tray.querySelectorAll(".trayseg button")];
      return {
        key: localStorage.getItem(termDeviceKey("atrium.termtray")),
        open: tray.classList.contains("open"), inert: body.inert,
        bodyH: body.getBoundingClientRect().height,
        labels: [...tray.querySelectorAll(".trayrow > .barlabel")].map(l => l.textContent.trim()),
        sortOn: [...tray.querySelectorAll(".trayrow:first-child .trayseg button.on")]
          .map(b => b.textContent.trim()),
        heights: [...new Set(pills.map(b => Math.round(b.getBoundingClientRect().height)))],
        groupPills: tray.querySelectorAll("#term-group button").length
      };
    });
    if (trayOpen.key !== "open" || !trayOpen.open || trayOpen.inert || trayOpen.bodyH < 60) {
      fail("opening the tray did not roll it down and remember it: " + JSON.stringify(trayOpen));
    }
    if (trayOpen.labels.join("|") !== "sort|hide inactive|group") {
      fail("the open tray's rows are not sort, hide inactive and group: " +
        JSON.stringify(trayOpen.labels));
    }
    if (trayOpen.sortOn.length !== 1) {
      fail("the sort row does not light exactly one of name|activity: " +
        JSON.stringify(trayOpen));
    }
    if (trayOpen.heights.length !== 1 || trayOpen.groupPills < 6) {
      fail("the tray's pills are not one consistent size, or the six group modes " +
        "are missing: " + JSON.stringify(trayOpen));
    }

    // Remembered: a fresh paint of the list (what a reload does) reads the key
    // and comes up open.
    const trayKept = await page.evaluate(async () => {
      const host = document.getElementById("term-list");
      host.innerHTML = ""; host.__paintedFrom = null;
      await renderTermList();
      return document.querySelector("#term-list .termtray").classList.contains("open");
    });
    if (!trayKept) fail("the tray did not come back open from its stored state.");

    // `+ new group` is not an orphan: in `by group` mode it spans the whole row
    // under the six modes rather than wrapping in as a seventh pill.
    const plus = await page.evaluate(async () => {
      const prev = localStorage.getItem(GROUPING_KEY);
      localStorage.setItem(GROUPING_KEY, JSON.stringify({ on: true, mode: "custom", by: "" }));
      await renderTermList();
      const seg = document.getElementById("term-group");
      const btn = seg && seg.querySelector(".groupplus");
      const out = btn ? {
        found: true,
        full: Math.abs(btn.getBoundingClientRect().width - seg.getBoundingClientRect().width) <= 2,
        summary: document.querySelector("#term-list .traysum").textContent
      } : { found: false };
      if (prev === null) localStorage.removeItem(GROUPING_KEY);
      else localStorage.setItem(GROUPING_KEY, prev);
      await renderTermList();
      return out;
    });
    if (!plus.found || !plus.full) {
      fail("`+ new group` is missing or does not span the group row: " + JSON.stringify(plus));
    }
    if (plus.found && !/ · by group · /.test(plus.summary)) {
      fail("the tray summary does not name `by group` grouping: " + JSON.stringify(plus));
    }

    // NOTHING PASSES UNDER IT. Squeeze the list so the rows overflow, scroll the
    // row box to the bottom, and every row's visible part is below the tray: the
    // scroll box starts where the tray ends, and it is the scroll box that clips.
    const under = await page.evaluate(async () => {
      const host = document.getElementById("term-list");
      host.style.maxHeight = "220px";
      await renderTermList();
      const scroll = host.querySelector(".termscroll");
      scroll.scrollTop = scroll.scrollHeight;
      const tray = host.querySelector(".termtray").getBoundingClientRect();
      const sr = scroll.getBoundingClientRect();
      const out = {
        scrolled: scroll.scrollTop > 0,
        scrollBelowTray: sr.top >= tray.bottom - 0.5
      };
      host.style.maxHeight = "";
      return out;
    });
    if (!under.scrolled || !under.scrollBelowTray) {
      fail("the rows do not scroll in their own box below the tray: " + JSON.stringify(under));
    }

    // In `mini` the tray keeps only the width buttons, so the way back is there.
    const mini = await page.evaluate(async () => {
      setTermListMode("mini");
      const tray = document.querySelector("#term-list .termtray");
      const vis = el => !!el && getComputedStyle(el).display !== "none";
      const out = {
        toggle: vis(tray.querySelector(".traytoggle")),
        body: vis(tray.querySelector(".traybody")),
        widen: [...tray.querySelectorAll(".tlcycle")].some(vis)
      };
      setTermListMode("full");
      localStorage.removeItem(termDeviceKey("atrium.termtray"));
      await renderTermList();
      return out;
    });
    if (mini.toggle || mini.body || !mini.widen) {
      fail("in mini the tray is not reduced to the width buttons: " + JSON.stringify(mini));
    }

    await page.evaluate(() => {
      try {
        localStorage.removeItem(termDeviceKey("atrium.hidedoers"));
        localStorage.removeItem(termDeviceKey("atrium.hidesubagents"));
        localStorage.removeItem(termDeviceKey("atrium.hideagents"));
      } catch (e) {}
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
      const groups = document.querySelector("#term-list .termtray");
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
        // The controls tray is above the first card, at the top of the list,
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
      fail("the controls tray is not at the top of the terminals list at 390px: " +
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
    // what a real attach runs, and it is what sets `has-term`. Inside the open
    // flyout the controls tray is folded to its one line until tapped, and the
    // rows scroll in their own box below it.
    await page.evaluate(() =>
      paintPaneBg({ background: "#101828", foreground: "#e6e6e6", cursor: "#4ea1ff" }));
    const decoupled = await page.evaluate(async () => {
      localStorage.removeItem(termDeviceKey("atrium.termtray"));
      await renderTermList();
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
      const trayBodyH = () => {
        const b = document.querySelector("#term-list .termbody .termtray .traybody");
        return b ? b.getBoundingClientRect().height : -1;
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
      // The tray is on the flyout, folded, above a scroll box of its own; a tap
      // rolls it down.
      const tray = document.querySelector("#term-list .termbody .termtray");
      const trayShown = !!tray && getComputedStyle(tray).display !== "none";
      const scroll = document.querySelector("#term-list .termbody .termscroll");
      const scrollBelow = !!(tray && scroll &&
        scroll.getBoundingClientRect().top >= tray.getBoundingClientRect().bottom - 0.5);
      const headBefore = trayBodyH();
      toggleTermTray();
      await new Promise(r => setTimeout(r, 450));
      const headAfter = trayBodyH();
      toggleTermTray();
      return {
        dropShown, collapsedBody, openBody, openPos,
        paneStable: Math.abs(paneOpen - paneCollapsed) <= 2,
        trayShown, scrollBelow,
        headHiddenByDefault: headBefore >= 0 && headBefore <= 1,
        headShownAfterToggle: headAfter > 60,
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
    if (!decoupled.trayShown || !decoupled.scrollBelow) {
      fail("the phone flyout does not show the tray above its own scrolling rows: " +
        JSON.stringify(decoupled));
    }
    if (!decoupled.headHiddenByDefault || !decoupled.headShownAfterToggle) {
      fail("the tray does not fold the controls on a phone " +
        "(hidden-by-default " + decoupled.headHiddenByDefault + ", shown-after-toggle " +
        decoupled.headShownAfterToggle + ").");
    }
    await page.evaluate(() => {
      setTermListOpen(false); paintPaneBg(null);
      localStorage.removeItem(termDeviceKey("atrium.termtray"));
    });

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
    await page.waitForFunction(() => window.__attachAttempts > 0, null, { timeout: 20000 });
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

    // ── the terminal fills the pane down to the footer, no dead band ────────
    // THE BUG THIS SECTION EXISTS FOR. xterm draws whole rows, so the grid is
    // `rows * cellHeight` and rarely the exact height of `#t-screen`. The
    // leftover, up to one line, used to fall BELOW the last row as a band of the
    // host's background between the terminal and the help bar: dead vertical
    // space above the footer. `sizeTermHost` sizes `.xterm` to the grid and
    // `#t-screen`'s `justify-content: flex-end` parks it on the footer, so the
    // grid's bottom meets the help bar's top and the leftover joins the air under
    // the bar instead. A real terminal is built (attach socket mocked to open and
    // idle) at a height whose remainder is non-zero, then the grid is measured
    // against the footer.
    // A FRESH PAGE, isolated from the timers the sections above left running (the
    // capped attach-retry from the loop repro would otherwise tear this pane down
    // mid-measure). The attach socket is mocked at load to open and idle, so
    // `openTerm` builds a real Terminal that fits and stays.
    const fillPage = await browser.newPage();
    const fillErrors = [];
    fillPage.on("pageerror", e => fillErrors.push(String(e)));
    if (process.env.DEBUG_HEADLESS) {
      fillPage.on("console", m => console.error("[fill] " + m.type() + ": " + m.text()));
    }
    await fillPage.addInitScript(() => {
      window.__realWS = window.WebSocket;
      window.WebSocket = function (url, protocols) {
        if (/\/attach(\?|$)/.test(url)) {
          const s = { url, readyState: 0, binaryType: "arraybuffer",
            onopen: null, onclose: null, onmessage: null, onerror: null,
            send() {}, close() { this.readyState = 3; } };
          setTimeout(() => { s.readyState = 1; if (s.onopen) s.onopen({}); }, 0);
          return s;
        }
        return new window.__realWS(url, protocols);
      };
    });
    try {
      // A height whose leftover against the cell size is a visible fraction of a
      // row, which is where the dead band used to show.
      await fillPage.setViewportSize({ width: 1280, height: 900 });
      await fillPage.goto(base, { waitUntil: "domcontentloaded" });
      // Let the board's first poll fire and settle before attaching.
      await fillPage.waitForTimeout(900);
      await fillPage.click('.tab[data-view="terms"]');
      // Build, fill, and measure in ONE step. The mocked attach opens but never
      // replays, so a poll or watchdog that ran between steps would read the card
      // as not-truly-attached and clear the pane; `closeTerm` and `clearTermPane`
      // are frozen for the duration so nothing tears the terminal down under the
      // measurement. This page is thrown away right after, so the freeze leaks
      // nowhere.
      const fill = await fillPage.evaluate(async () => {
        const t = await api("/v1/tasks/t1");
        openTerm(t);
        if (typeof term === "undefined" || !term) return { ok: false, why: "no terminal" };
        try { closeTerm = () => {}; } catch (e) {}
        try { clearTermPane = () => {}; } catch (e) {}
        paintPaneBg({ background: "#1C5A2B", foreground: "#e6f0e6", cursor: "#9be29b" });
        for (let i = 0; i < 120; i++) term.write("line " + i + " of terminal output\r\n");
        term.write(">> a live prompt on the last row");
        const frame = () => new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));
        await frame();
        term.scrollToBottom();
        await frame();
        const r = s => { const el = document.querySelector(s); return el ? el.getBoundingClientRect() : null; };
        const host = r("#t-screen"), help = r(".term-help");
        const grid = r("#t-screen .xterm-screen"), vp = r("#t-screen .xterm-viewport");
        if (!host || !help || !grid || !vp) {
          return { ok: false, why: "missing element " +
            JSON.stringify({ host: !!host, help: !!help, grid: !!grid, vp: !!vp }) };
        }
        return {
          ok: true,
          // The drawn grid's bottom sits on the help bar's top: no host-coloured
          // band between the last terminal row and the footer.
          gridToFooter: Math.round(help.top - grid.bottom),
          // The scrollable viewport reaches the footer too, so nothing shows the
          // host background below it.
          viewportToFooter: Math.round(help.top - vp.bottom),
          // The host still fills the pane down to the footer (flex:1 intact).
          hostToFooter: Math.round(help.top - host.bottom)
        };
      });
      if (!fill.ok) {
        fail("could not measure the attached terminal against the footer (" +
          (fill.why || "unknown") + ").");
      } else {
        // A pixel or two of sub-pixel rounding is fine; a whole line is the bug.
        if (Math.abs(fill.gridToFooter) > 3) {
          fail("the terminal grid does not sit on the footer (gap " + fill.gridToFooter +
            "px): a remainder band of dead space is back above the help bar.");
        }
        if (Math.abs(fill.viewportToFooter) > 3) {
          fail("the terminal viewport does not reach the footer (gap " +
            fill.viewportToFooter + "px).");
        }
        if (Math.abs(fill.hostToFooter) > 3) {
          fail("the terminal host does not fill the pane down to the footer (gap " +
            fill.hostToFooter + "px): the pane layout regressed.");
        }
      }
      if (fillErrors.length) {
        fail("the terminal-fill page threw uncaught errors: " + fillErrors.join(" | "));
      }
    } finally {
      await fillPage.close();
    }

    // ── a pane under the width floor says so once, and links to the setting ──
    // A narrow window opens a runner terminal. It draws the floor's 120 columns
    // in a sideways scroll, and the first time it raises a modal whose link opens
    // the settings cog on the board pane with the floor field highlighted. Once
    // "do not show this again" is ticked, it stays quiet in this browser.
    const floorPage = await browser.newPage();
    const floorErrors = [];
    floorPage.on("pageerror", e => floorErrors.push(String(e)));
    await floorPage.addInitScript(() => {
      window.__realWS = window.WebSocket;
      window.WebSocket = function (url, protocols) {
        if (/\/attach(\?|$)/.test(url)) {
          const s = { url, readyState: 0, binaryType: "arraybuffer",
            onopen: null, onclose: null, onmessage: null, onerror: null,
            send() {}, close() { this.readyState = 3; } };
          setTimeout(() => { s.readyState = 1; if (s.onopen) s.onopen({}); }, 0);
          return s;
        }
        return new window.__realWS(url, protocols);
      };
    });
    try {
      await floorPage.setViewportSize({ width: 700, height: 800 });
      await floorPage.goto(base, { waitUntil: "domcontentloaded" });
      await floorPage.waitForTimeout(900);
      await floorPage.click('.tab[data-view="terms"]');
      await floorPage.evaluate(async () => {
        const t = await api("/v1/tasks/t1");
        try { closeTerm = () => {}; } catch (e) {}
        try { clearTermPane = () => {}; } catch (e) {}
        openTerm(t);
      });
      await floorPage.waitForSelector("#ask[open]", { timeout: 5000 }).catch(() => {});
      const seen = await floorPage.evaluate(() => ({
        open: document.getElementById("ask").open,
        body: document.getElementById("ask-body").textContent,
        remember: document.querySelector("#ask-remember span").textContent,
        cols: term && term.cols,
        fit: termFitCols,
        wide: document.getElementById("t-screen").classList.contains("wide"),
      }));
      if (!seen.open || !/120 columns/.test(seen.body) || !/scrolls sideways/.test(seen.body)) {
        fail("a pane under the width floor did not raise the notice: " + JSON.stringify(seen));
      }
      if (seen.remember !== "do not show this again") {
        fail("the width-floor notice's checkbox reads " + JSON.stringify(seen.remember) + ".");
      }
      if (seen.cols !== 120 || !(seen.fit < 120) || !seen.wide) {
        fail("a pane under the floor did not draw 120 columns in a sideways scroll: " + JSON.stringify(seen));
      }
      await floorPage.click("#ask-body a");
      await floorPage.waitForTimeout(300);
      const cog = await floorPage.evaluate(() => {
        const pane = document.querySelector('#settings .pane[data-name="board"]');
        const field = document.getElementById("s-mincols-field");
        return {
          settings: document.getElementById("settings").open,
          ask: document.getElementById("ask").open,
          board: !!pane && !pane.hidden && pane.contains(field),
          flash: field.classList.contains("flash"),
          reset: document.getElementById("s-mincols-reset").hidden,
        };
      });
      if (!cog.settings || cog.ask || !cog.board || !cog.flash) {
        fail("the notice's link did not open the settings cog on the board pane with the floor " +
          "highlighted: " + JSON.stringify(cog));
      }
      if (!cog.reset) fail("the reset link shows while the floor is the default.");
      // Tick "do not show this again", and it stays quiet in this browser.
      await floorPage.evaluate(() => {
        document.getElementById("settings").close();
        floorNoticed = false;
        noteUnderFloor();
      });
      await floorPage.check("#ask-remember-on");
      await floorPage.click("#ask-actions button");
      const quiet = await floorPage.evaluate(() => {
        floorNoticed = false;
        noteUnderFloor();
        return { skipped: !!skippedConfirms()["width-floor"], open: document.getElementById("ask").open };
      });
      if (!quiet.skipped || quiet.open) {
        fail("\"do not show this again\" did not keep the width-floor notice away: " + JSON.stringify(quiet));
      }
      if (floorErrors.length) fail("the width-floor page threw: " + floorErrors.join(" | "));
    } finally {
      await floorPage.close();
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
    await page.waitForFunction(() => poppedOut("s1"), null, { timeout: 15000 });

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
        () => poppedOut("s1") && (Date.now() - (soloHeld.get("s1") || 0) < 5000), null,
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
      await page.waitForFunction(() => !poppedOut("s1"), null, { timeout: 15000 });
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
      }, null, { timeout: 15000 });

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
        /solo card/.test(document.title), null, { timeout: 20000 });

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
      }, null, { timeout: 15000 });
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
      }, null, { timeout: 15000 });
      await hub.evaluate(() => openRooms());
      await hub.waitForFunction(() => {
        const m = document.getElementById("rooms-menu");
        return m && !m.hidden;
      }, null, { timeout: 15000 });

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
      }, null, { timeout: 15000 });

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
      }, null, { timeout: 15000 });
      await hub.evaluate(() => switchView("audit"));
      await hub.waitForFunction(() => {
        const rows = document.querySelectorAll("#audit-list .aud-row");
        return rows.length >= 2;
      }, null, { timeout: 15000 });
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
      }, null, { timeout: 15000 }).catch(() => fail(
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
        document.querySelectorAll("#audit-list .aud-row").length === 4, null,
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
      }, null, { timeout: 15000 }).catch(() => fail(
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
        document.querySelectorAll("#audit-list .aud-row").length === 4, null,
        { timeout: 15000 }).catch(() => fail(
          "clearing the kind filter did not restore the full audit feed."));
      auditLive = true;
      hubStreams.forEach(r => { try { r.write("event: audit\ndata: {}\n\n"); } catch (e) {} });
      await hub.waitForFunction(() => {
        const rows = [...document.querySelectorAll("#audit-list .aud-row")];
        return rows.length === 5 &&
          rows[0].querySelector(".aud-kind").textContent === "session-exit";
      }, null, { timeout: 15000 }).catch(() => fail(
        "the audit pane did not pick up a live event on the `audit` delta."));

      // ── a long feed scrolls in its own box, filters stay put ──────────────
      // main clips, so a pane that is not a scroll box of its own can never show
      // the rows below the window. The list scrolls and the filters above it do
      // not move. Checked at desktop and phone widths.
      auditMany = true;
      hubStreams.forEach(r => { try { r.write("event: audit\ndata: {}\n\n"); } catch (e) {} });
      await hub.waitForFunction(() =>
        document.querySelectorAll("#audit-list .aud-row").length === 150, null,
        { timeout: 15000 }).catch(() => fail("the audit pane did not draw the long feed."));
      for (const vp of [{ width: 1280, height: 800 }, { width: 390, height: 780 }]) {
        await hub.setViewportSize(vp);
        const sc = await hub.evaluate(() => {
          const list = document.getElementById("audit-list");
          const filters = document.querySelector("#audit > .filters");
          list.scrollTop = 0;
          const before = filters.getBoundingClientRect().top;
          const tall = { sh: list.scrollHeight, ch: list.clientHeight };
          list.scrollTop = 600;
          return Object.assign(tall, {
            moved: list.scrollTop,
            filtersMoved: filters.getBoundingClientRect().top - before,
            filtersOnScreen: filters.getBoundingClientRect().bottom <= window.innerHeight
          });
        });
        if (!(sc.sh > sc.ch) || sc.moved <= 0) {
          fail("the audit list does not scroll at " + vp.width + "px: " + JSON.stringify(sc));
        }
        if (sc.filtersMoved !== 0 || !sc.filtersOnScreen) {
          fail("the audit filters moved with the list at " + vp.width + "px: " + JSON.stringify(sc));
        }
      }
      await hub.setViewportSize({ width: 1280, height: 800 });

      // ── a live delta does not yank a reader who has scrolled down ─────────
      // A new line lands on top. The row the reader was looking at stays where it
      // was, which means scrollTop grows by about one row rather than resetting.
      const held = await hub.evaluate(() => {
        const list = document.getElementById("audit-list");
        list.scrollTop = 600;
        const top = list.getBoundingClientRect().top;
        const row = [...list.querySelectorAll(".aud-row")]
          .find(r => r.getBoundingClientRect().bottom > top);
        return { scrollTop: list.scrollTop, id: row.dataset.id,
          offset: row.getBoundingClientRect().top - top };
      });
      auditManyLive = true;
      hubStreams.forEach(r => { try { r.write("event: audit\ndata: {}\n\n"); } catch (e) {} });
      await hub.waitForFunction(() =>
        document.querySelectorAll("#audit-list .aud-row").length === 151, null,
        { timeout: 15000 }).catch(() => fail("the audit pane did not pick up the live line on the long feed."));
      const late = await hub.evaluate((id) => {
        const list = document.getElementById("audit-list");
        const row = list.querySelector('.aud-row[data-id="' + id + '"]');
        return { scrollTop: list.scrollTop,
          offset: row.getBoundingClientRect().top - list.getBoundingClientRect().top };
      }, held.id);
      if (late.scrollTop <= 0 || late.scrollTop < held.scrollTop) {
        fail("a live audit delta reset the reader's scroll: " + JSON.stringify({ held, late }));
      }
      if (Math.abs(late.offset - held.offset) > 1) {
        fail("a live audit delta moved the row the reader was on: " + JSON.stringify({ held, late }));
      }

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
    await skinScopeSection(browser, base);

    // ── a persisted skin heals when a room attaches, with no reload ──────────
    await skinHealSection(browser, base);

    // ── the global auto button is never blank ────────────────────────────────
    // `#gauto` has no class and no text in the markup, and only a settings read
    // that landed ever painted it. A deploy restarts the hub, the board reloads
    // or reconnects before a room is back, the read fails, and the header shows
    // an empty grey pill with no dot and no word. Three ways in: the read fails
    // at load, the read fails in a room scope, and the read fails on a stream
    // reopen after it had worked. Each must still say something, and each must
    // heal to the real answer without a reload.
    const gautoPaint = pg => pg.evaluate(() => {
      const b = document.getElementById("gauto");
      return b ? { cls: b.className, text: b.textContent.trim(), title: b.dataset.tip } : null;
    });
    const gautoDrawn = (what, g) => {
      if (!g || !/(^|\s)gauto(\s|$)/.test(g.cls) || !g.text) {
        fail(what + ": #gauto was blank, " + JSON.stringify(g) + ". It needs the gauto class and a word.");
        return false;
      }
      return true;
    };
    const gautoHeals = async (what, pg, want) => {
      try {
        await pg.waitForFunction(w => {
          const b = document.getElementById("gauto");
          return b && b.textContent.trim() === w &&
            !b.classList.contains("unknown") && !b.classList.contains("stale");
        }, want, { timeout: 15000 });
      } catch (e) {
        fail(what + ": #gauto did not heal to " + JSON.stringify(want) + " once settings answered, it is " +
          JSON.stringify(await gautoPaint(pg)));
      }
    };
    const pushRooms = () =>
      hubStreams.forEach(r => { try { r.write("event: rooms\ndata: {}\n\n"); } catch (e) {} });

    // (1) the read fails at load: the hub has no room to borrow settings from.
    hubMode = true;
    hubHasRoom = false;
    sggAttached = false;
    const ga1Ctx = await browser.newContext();
    const ga1 = await ga1Ctx.newPage();
    const ga1Errors = [];
    ga1.on("pageerror", e => ga1Errors.push(String(e)));
    try {
      await ga1.goto(base, { waitUntil: "domcontentloaded" });
      await ga1.waitForTimeout(1500);
      const g = await gautoPaint(ga1);
      if (gautoDrawn("settings failing at load", g) && !/unknown/.test(g.cls)) {
        fail("settings failing at load: #gauto claims a state nobody read, " + JSON.stringify(g));
      }
      // Past loadHubRooms' 2s throttle, a room attaches and the read succeeds.
      await ga1.waitForTimeout(2200);
      hubHasRoom = true;
      pushRooms();
      await gautoHeals("settings failing at load", ga1, "asking");
      if (ga1Errors.length) fail("the gauto load page threw: " + ga1Errors.join(" | "));
    } finally {
      await ga1.close();
      await ga1Ctx.close();
      hubHasRoom = true;
    }

    // (2) a room scope whose room has not re-attached: the scoped read fails.
    roomSettingsDown = true;
    const ga2Ctx = await browser.newContext();
    await ga2Ctx.addInitScript(() => { try { localStorage.setItem("atrium.room", "alpha"); } catch (e) {} });
    const ga2 = await ga2Ctx.newPage();
    const ga2Errors = [];
    ga2.on("pageerror", e => ga2Errors.push(String(e)));
    try {
      await ga2.goto(base, { waitUntil: "domcontentloaded" });
      await ga2.waitForTimeout(1500);
      gautoDrawn("settings failing in a room scope", await gautoPaint(ga2));
      // The room comes back. The poll re-reads while the switch is unknown.
      roomSettingsDown = false;
      await gautoHeals("settings failing in a room scope", ga2, "asking");
      if (ga2Errors.length) fail("the gauto room page threw: " + ga2Errors.join(" | "));
    } finally {
      await ga2.close();
      await ga2Ctx.close();
      roomSettingsDown = false;
    }

    // (3) a stream reopen after a good read: approving everything, then the hub
    // restarts. The reopen's read fails, so the last answer must read as stale
    // rather than as fresh, and the button must still say something.
    gautoOn = true;
    const ga3Ctx = await browser.newContext();
    const ga3 = await ga3Ctx.newPage();
    const ga3Errors = [];
    ga3.on("pageerror", e => ga3Errors.push(String(e)));
    try {
      await ga3.goto(base, { waitUntil: "domcontentloaded" });
      await gautoHeals("before the stream reopen", ga3, "approving everything");
      hubHasRoom = false;
      const reopened = ga3.waitForRequest(r => r.url().includes("/v1/events"), { timeout: 15000 });
      const settingsAfter = reopened.then(() => ga3.waitForResponse(r =>
        r.url().split("?")[0].endsWith("/v1/settings") && r.status() === 409, { timeout: 15000 }));
      openStreams.splice(0).forEach(r => { try { r.end(); } catch (e) {} });
      hubStreams.splice(0);
      await settingsAfter;
      await ga3.waitForTimeout(300);
      const g = await gautoPaint(ga3);
      if (gautoDrawn("settings failing on a stream reopen", g) && !/stale/.test(g.cls)) {
        fail("settings failing on a stream reopen: #gauto shows its old answer as fresh, " + JSON.stringify(g));
      }
      await ga3.waitForTimeout(2200);
      hubHasRoom = true;
      pushRooms();
      await gautoHeals("settings failing on a stream reopen", ga3, "approving everything");
      if (ga3Errors.length) fail("the gauto reopen page threw: " + ga3Errors.join(" | "));
    } catch (e) {
      fail("the gauto stream-reopen test did not run through: " + e.message);
    } finally {
      await ga3.close();
      await ga3Ctx.close();
      gautoOn = false;
      hubMode = false;
      hubHasRoom = true;
    }

    // ── the on/off switch: runners and fixtures ──────────────────────────────
    // Every enable/disable on the runners page is the row's own on/off pill. No
    // separate enable button; a click writes the row with `enabled` flipped; a
    // refusal puts the pill back and says why; the pill is one box on an on row
    // and an off row, so whatever follows it starts in one column; and at phone
    // width it is a thumb's forty pixels tall.
    resetSwitches();
    const swCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
    const sw = await swCtx.newPage();
    const swErrors = [];
    sw.on("pageerror", e => swErrors.push(String(e)));
    // The pill's box and the left edge of the element after it, so two rows can
    // be compared, plus what a screen reader and a hover would be told.
    const pill = (list, id) => sw.evaluate(([l, i]) => {
      const b = document.querySelector(`#${l} .chip.toggle[data-id="${i}"]`);
      if (!b) return null;
      const r = b.getBoundingClientRect();
      const next = b.nextElementSibling ? b.nextElementSibling.getBoundingClientRect().left : 0;
      return { w: r.width, h: r.height, next: next - r.left, tag: b.tagName,
        role: b.getAttribute("role"), checked: b.getAttribute("aria-checked"),
        title: b.dataset.tip, text: b.textContent.trim() };
    }, [list, id]);
    const checked = (list, id, want) => sw.waitForFunction(([l, i, w]) => {
      const b = document.querySelector(`#${l} .chip.toggle[data-id="${i}"]`);
      return b && b.getAttribute("aria-checked") === w;
    }, [list, id, want], { timeout: 15000 });
    const wrote = (path, method) => sw.waitForResponse(r =>
      r.url().split("?")[0].endsWith(path) && r.request().method() === method, { timeout: 15000 });
    // A refusal is told the way the page tells every error: the ask dialog.
    const toldAndClosed = async (words) => {
      await sw.waitForSelector("#ask[open]", { timeout: 15000 });
      const said = await sw.evaluate(() => document.getElementById("ask").textContent);
      if (!said.includes(words)) {
        fail("a refused switch did not show the refusal; the dialog said " + JSON.stringify(said.trim()));
      }
      await sw.evaluate(() => document.getElementById("ask").close());
    };
    const sameBox = (what, a, b) => {
      if (!a || !b) { fail(what + ": a switch was not drawn."); return; }
      if (Math.abs(a.w - b.w) > 0.5) {
        fail(what + ": the on pill is " + a.w + "px wide and the off pill " + b.w +
          "px, so the columns after it do not line up.");
      }
      if (Math.abs(a.next - b.next) > 0.5) {
        fail(what + ": the column after the pill starts " + a.next + "px in on the on row and " +
          b.next + "px in on the off row.");
      }
    };
    try {
      await sw.goto(base, { waitUntil: "domcontentloaded" });
      await sw.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
      await sw.evaluate(() => goRunners("runners"));
      await sw.waitForSelector('#harness-list .chip.toggle[data-id="hoff"]', { timeout: 15000 });

      const buttons = await sw.evaluate(() =>
        [...document.querySelectorAll("#harness-list button, #fixture-list button")]
          .map(b => b.textContent.trim()));
      if (buttons.some(w => w === "enable" || w === "disable")) {
        fail("a runner or fixture row still has its own enable/disable button: " +
          JSON.stringify(buttons));
      }

      const on = await pill("harness-list", "hon");
      const off = await pill("harness-list", "hoff");
      sameBox("runners", on, off);
      if (on && (on.tag !== "BUTTON" || on.role !== "switch" || on.checked !== "true" ||
          on.text !== "on" || on.title !== "on. click to disable this runner")) {
        fail("the runner switch is not a keyboard-reachable role=switch saying what a " +
          "click does: " + JSON.stringify(on));
      }
      if (off && (off.checked !== "false" || off.text !== "off" ||
          off.title !== "off. click to enable this runner")) {
        fail("the off runner switch reads wrong: " + JSON.stringify(off));
      }

      // Off to on writes that runner, with enabled true, and the pill stays on.
      let put = wrote("/v1/harnesses/hoff", "PUT");
      await sw.click('#harness-list .chip.toggle[data-id="hoff"]');
      let sent = JSON.parse((await put).request().postData() || "{}");
      if (sent.enabled !== true || sent.id !== "hoff") {
        fail("turning a runner on sent " + JSON.stringify(sent) + ", not the row with enabled true.");
      }
      await checked("harness-list", "hoff", "true").catch(() =>
        fail("a runner switched on did not stay on after the repaint."));

      // A refusal puts the pill back and says why.
      switchFail = true;
      put = wrote("/v1/harnesses/hon", "PUT");
      await sw.click('#harness-list .chip.toggle[data-id="hon"]');
      await put;
      await toldAndClosed("the daemon refused the switch");
      const back = await pill("harness-list", "hon");
      if (!back || back.checked !== "true" || back.text !== "on") {
        fail("a refused switch did not go back to on: " + JSON.stringify(back));
      }

      // Fixtures: the same box, and a key press flips it.
      await sw.evaluate(() => goRunners("fixtures"));
      await sw.waitForSelector('#fixture-list .chip.toggle[data-id="foff"]', { timeout: 15000 });
      sameBox("fixtures", await pill("fixture-list", "fon"), await pill("fixture-list", "foff"));
      put = wrote("/v1/fixtures/foff", "PUT");
      await sw.focus('#fixture-list .chip.toggle[data-id="foff"]');
      await sw.keyboard.press("Space");
      sent = JSON.parse((await put).request().postData() || "{}");
      if (sent.enabled !== true) fail("space on a fixture switch sent " + JSON.stringify(sent));
      await checked("fixture-list", "foff", "true").catch(() =>
        fail("a fixture switched on from the keyboard did not stay on."));

      // Phone width: a thumb's forty pixels, still one box both ways.
      resetSwitches();
      await sw.setViewportSize({ width: 390, height: 800 });
      await sw.evaluate(() => goRunners("runners"));
      await sw.evaluate(() => renderRunners());
      await checked("harness-list", "hoff", "false");
      const pOn = await pill("harness-list", "hon");
      const pOff = await pill("harness-list", "hoff");
      sameBox("runners at phone width", pOn, pOff);
      if (pOn && pOn.h < 40) {
        fail("at phone width the switch is " + pOn.h + "px tall, under the board's 40px tap target.");
      }
      if (swErrors.length) fail("the switch page threw uncaught errors: " + swErrors.join(" | "));
    } finally {
      await sw.close();
      await swCtx.close();
      resetSwitches();
    }

    // ── cards wear their terminal colours ───────────────────────────────────
    // The board setting that draws every card in its own terminal theme, with
    // the terminals list's idle-rows switch beside it. Off, nothing changes.
    // On, every card in the terminals list, the stack and the board columns
    // takes its theme's background, the attached card keeps a
    // frame the others do not have, and every title, path and chip reads at
    // WCAG AA (4.5:1) against the surface it is on, on every shipped theme.
    // Measured off computed styles, so what is scored is what the browser
    // painted, not what the code meant to paint.
    await wornSection(browser, base);
    // ── the terminals list's three theme switches ───────────────────────────
    await termWearSection(browser, base);
    // ── the attached row's bridge into the terminal, and the terminal's frame ─
    await bridgeSection(browser, base);
    // ── a load reads settings once ──────────────────────────────────────────
    await settingsOnceSection(browser, base);
    // ── the website skin's effects stay inside the website skin ─────────────
    await websiteSkinSection(browser, base);
    // ── the hub restart gate: countdown, pause, resume and the modal ────────
    await restartGateSection(browser, base);
    await restartStaysSection(browser, base);
    await atriumDownSection(browser, base);
    await toastStaysSection(browser, base);
    // ── the styled tooltip, and no native title anywhere on the board ───────
    await tooltipSection(browser, base);
    // ── group colours on every surface, and dragging group headings ─────────
    await groupColorSection(browser, base);
    await groupDragSection(browser, base);
    // ── a popped-out card stays spoken for across a room-set change ─────────
    await popoutTagFlipSection(browser, base);
    // ── an idle board stays idle, and a silent room cannot fill the fetch cap ─
    await idleRateSection(browser, base);
    // ── a group opened by hand stays put, and idle windows write nothing ───
    await foldStillSection(browser, base);
    // ── untagged follows the sort pill in all three views ──────────────────
    await untaggedSortSection(browser, base);
    // ── a card this window has not seen says so, once ──────────────────────
    await newCardSection(browser, base);
    // ── a theme preview recolours the card everywhere it shows ─────────────
    await themePreviewSection(browser, base);
    // ── a click on an alert lands where the alert is about ─────────────────
    await landSection(browser, base);
    await reselectSection(browser, base);
    // ── over a terminal the toasts hang from the top right ─────────────────
    await toastsTopSection(browser, base);
    // ── say immediately, or when the turn is done ──────────────────────────
    await sayWhenSection(browser, base);
    // ── any paste still in flight after 20ms shows the spinner ─────────────
    await pasteSpinnerSection(browser, base);
    // ── copy on select answers the pointer, not the find bar ───────────────
    await copySelectSection(browser, base);
    // ── a second press fires nothing ──────────────────────────────────────
    await busyGuardSection(browser, base);
    // ── keep-alive chips, the card switch, and the break-even toast ────────
    await keepaliveSection(browser, base);
  } catch (e) {
    fail("the headless run threw: " + (e && e.message ? e.message : e) + threwAt(e));
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
    "the two independent hide-inactive toggles drop inactive agents (dead, no live " +
    "connection) and inactive subagents (idle, waiting, or exited - not working " +
    "right now) each on their own (both/either/neither, subagents on by default), " +
    "so an idle subagent hides while an idle-but-live agent stays, a pinned row " +
    "hides like any other, the agents toggle hides exactly the grey rows, the pinned and " +
    "group headings read shown/total while rows are hidden, counting each " +
    "kind's hidden rows in parens, " +
    "the controls are a tray above the rows' own scroll box that folds to a one-line " +
    "summary, remembers its state, keeps even pills and a full-width `+ new group`, " +
    "the board paints its lists, a hung fetch does not blank it, the " +
    "board's roll call re-hears a live popped-out window (and drops one that " +
    "went away), a popped-out window rides out a hub restart and recovers, the " +
    "open room picker live-updates a newly-attached room from disconnected to " +
    "live, a long history scrolls under a fixed search bar at desktop and phone width and a live repaint " +
    "keeps its pages and the reader's row, changing runners pane goes back to the top, " +
    "the audit pane paints newest-first, filters by room and by kind, and " +
    "picks up a live event on the `audit` delta with no reload, a long feed scrolls in its own box under " +
    "fixed filters at desktop and phone width and a live line does not move the row a scrolled reader is on, " +
    "and the board " +
    "skin follows the room-picker scope (ALL wears the " +
    "hub's, each room its own, a save lands in the current scope, a room " +
    "attaching leaves the ALL skin alone), a persisted skin heals when a " +
    "room attaches after a load that could not read settings, with no reload, " +
    "the global auto button is never blank (a failed read at load, in a room scope, or on a " +
    "stream reopen says unknown or stale, and heals when settings answer), " +
    "a desktop notification fired while no window has focus is recorded in " +
    "the toast log without also drawing a toast, and the runner and fixture " +
    "rows switch on and off from their own pill (no enable button, the right write, " +
    "a refusal puts it back, one box on and off, 40px tall at phone width), " +
    "a card whose last turn is unread wears a dot and its open questions `? N`, " +
    "the website skin wears harbour's palette with a gradient button, a frosted header and a glow " +
    "while harbour and noir wear none of it, " +
    "and the hub restart gate counts down in a toast, pauses on a click without reporting it as input, " +
    "holds a sticky paused toast with resume, keeps the countdown and paused toasts through the toast cap, " +
    "a removal, a timer and a stream reopen, and covers the board until the stream comes back, " +
    "and a toast nobody answers stays its full nine seconds while a pending one goes when it is answered.");
}

main().catch(e => { console.error(e); process.exit(1); });
