// The mocked daemon the headless board suite drives: one http server answering every endpoint the board reads, the cards
// it answers with, and the mutable state a section sets to change an answer.
//
// ALL OF THAT STATE IS ONE OBJECT, `mock`. A section sets `mock.tasksMode = "worn"` where it used to set a module-level
// `tasksMode`, and `mock.snapshot()` / `mock.reset(snapshot)` put every field back to how it was when the snapshot was
// taken. That covers the loose `let`s and the objects a section mutates in place (the cards PIN, LOOSE, ALPHA, SGG, the
// switch rows HON, HOFF, FON, FOFF, LAND, the Set of dismissed questions, the write logs), so a unit that leaves one
// changed cannot reach the next unit. A new piece of state a section changes goes on `mock` as `mock.name = initial`,
// never as a module-level `let` or a `const` mutated in place: the listing in test-board-headless.js fails on a `let`.
//
// What /v1/tasks answers depends on cards declared next to the sections that use them, so the harness hands the function
// over: `hooks.tasks(reading)`.

const http = require("http");
const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const { wholeBoard } = require("../board-source.js");

const hooks = { tasks: () => [] };

const mock = {};
Object.defineProperties(mock, {
  // A deep copy of every field. Cloned, so a snapshot keeps its own values however the run changes the fields after.
  snapshot: { value() { return structuredClone(Object.fromEntries(Object.entries(this))); } },
  // Every field back to the snapshot's. Each is cloned again, so one snapshot can be reset to over and over.
  reset: { value(snap) { for (const k of Object.keys(snap)) this[k] = structuredClone(snap[k]); } },
});

// The board's xterm bundle, served off disk so a real Terminal is built. The
// page loads these as `<script src="/vendor/...">`, which board-source leaves
// external, and the attach-loop repro needs `openTerm` to build a real terminal
// and open a socket rather than bail on a missing library.
const WEB_ROOT = path.join(__dirname, "..", "..", "internal", "api", "web");

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
// docs/runtime/seen-design.md.
const SEEN = Object.assign({}, T1, {
  id: "seen1", display_title: "unread card",
  seen: { unseen: true, turn_ended_at: "2026-09-23T12:00:00.000Z", answered: false,
    open_questions: ["land sa21 first?", "build the tray?"] }
});
// Cards for the `? N` click: two open questions and a held peer message. A
// dismissed card answers `answered: true` with no questions, the way the room's
// view does once the set is answered. See `questionsClickSection`.
const QC_AT = "2026-09-29T09:00:00.000Z";
const QC_BASE = Object.assign({}, T1, {
  id: "qc1", display_title: "asking card", supervised: true, pinned: true,
  seen: { unseen: false, turn_ended_at: QC_AT, questions_at: QC_AT, answered: false,
    open_questions: ["land sa21 first?", "build the tray?"] },
  activity: { held_peer: true, held_count: 2, held_seconds: 30, held_for: "line", held_turn: "asked" },
});
mock.qDismissMode = "ok";       // ok | stale | fail
mock.qHeldQuiet = false;
mock.qDismissed = new Set();
mock.qDismissWrites = [];
function qclickCards() {
  return [Object.assign({}, QC_BASE, { activity: Object.assign({}, QC_BASE.activity, { held_quiet: mock.qHeldQuiet }) },
    mock.qDismissed.has("qc1")
    ? { seen: { unseen: false, turn_ended_at: QC_AT, questions_at: QC_AT, answered: true,
        answered_via: "dismissed" } }
    : {})];
}
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
mock.histMany = false;
mock.histManyLive = false;
const histRun = n => ({
  id: "hm" + n, display_title: "run " + n, runner: "claude", status: "done",
  created_at: new Date(Date.UTC(2026, 8, 18, 9, 0, n)).toISOString(), why: "run number " + n,
  recap: "", worktree: "/tmp/run" + n, archived_at: ""
});
const HIST_MANY = Array.from({ length: 250 }, (_, i) => histRun(250 - i));
function histFeed() { return mock.histManyLive ? [histRun(251)].concat(HIST_MANY) : HIST_MANY; }
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
mock.PIN = {
  id: "pin1", status: "dead", display_title: "terminated card", runner: "claude",
  rank: 1, worktree: "/tmp/pinned", why: "", idle_seconds: 0, wait_seconds: 0,
  created_at: "2026-09-19T12:00:00Z", last_activity_at: "2026-09-19T12:00:00Z",
  tags: [], supervised: false, offline: false, pinned: true, pid: 0, auto_approve: false
};
function resetPin() { mock.PIN.pinned = true; }

// A pinned, live card filed into the operator's `active` group, the shape of the
// screenshot where the group said 0 and asked for a card to be filed into it.
const FILED = Object.assign({}, mock.PIN, {
  id: "filed1", display_title: "filed card", status: "running", supervised: true,
  tags: ["active"], worktree: "/tmp/filed"
});
// A live, unpinned, ungrouped row beside it, for the drag case.
mock.LOOSE = Object.assign({}, T1, { id: "loose1", display_title: "loose card", supervised: true });

// The terminals strip, exercising the paused, dead and subagents chips. AGLIVE is a live agent (a human's own
// session, no `atrium:subagent` tag) with three subagents launched from it (`spawned_by_id`): SUBLIVE idle, SUBWORK
// working and SUBASK asking, which shows through the fold. SHELVED1 is a stopped card. A dead UNPINNED session is not
// in the strip at all (nothing to switch to), so the dead rows are PINNED, drawn cold: SUBDEAD and AGDEAD, which the
// dead chip counts and lists.
const SUBLIVE = {
  id: "sublive", status: "running", display_title: "idle subagent", runner: "claude",
  rank: 1, worktree: "/tmp/sublive", why: "", idle_seconds: 0, wait_seconds: 0,
  created_at: "2026-09-19T12:00:00Z", last_activity_at: "2026-09-19T12:00:00Z",
  tags: ["origin:agent", "atrium:subagent"], supervised: true, offline: false, pinned: false, auto_approve: false,
  spawned_by_id: "aglive"
};
const SUBWORK = Object.assign({}, SUBLIVE, {
  id: "subwork", display_title: "working subagent", activity: { what: "thinking" }
});
// A supervised subagent that is waiting on the operator: it shows through a folded parent.
const SUBASK = Object.assign({}, SUBLIVE, {
  id: "subask", display_title: "asking subagent", status: "needs-permission"
});
// Runner gone, held by its pin, drawn cold: behind the dead chip.
const SUBDEAD = Object.assign({}, SUBLIVE, {
  id: "subdead", display_title: "dead subagent", status: "dead",
  supervised: false, pinned: true, pid: 0, spawned_by_id: ""
});
const AGLIVE = Object.assign({}, SUBLIVE, {
  id: "aglive", display_title: "my terminal", tags: [], spawned_by_id: ""
});
// A dead agent (no tag), pinned cold: behind the dead chip.
const AGDEAD = Object.assign({}, SUBLIVE, {
  id: "agdead", display_title: "my dead terminal", tags: [],
  status: "dead", supervised: false, pinned: true, pid: 0, spawned_by_id: ""
});
// A stopped card: behind the paused chip.
const SHELVED1 = Object.assign({}, SUBLIVE, {
  id: "paused1", display_title: "paused card", status: "paused", tags: [],
  supervised: false, pinned: false, spawned_by_id: ""
});

// Four live untagged cards served in ADDRESS order, which is not name order and
// not activity order, so a view that sorts on the wrong key is caught. The
// shape of the screenshot: named cards whose dim second line is the path.
const untaggedCard = (id, title, worktree, idle) => Object.assign({}, T1, {
  id, display_title: title, worktree, repo: worktree.split("/").pop(), branch: "b",
  idle_seconds: idle, supervised: true,
  // last_activity_at is the stamp the sort goes by, so it says the same as idle_seconds
  last_activity_at: new Date(Date.now() - idle * 1000).toISOString()
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
mock.untaggedExtra = [];
mock.untaggedDown = false;
mock.untaggedEmpty = false;
mock.untaggedTag = "";
const NEWC = untaggedCard("u-new", "brand new", "/w/github/openziti/fresh", 5);

// The cards the alert-landing section clicks through to, by id. `land-new` is
// the race: listed before its terminal, and made supervised by the test a
// moment after the click. `landList` is what the list answers on top of T1, and
// `landPerms` the pending requests. See `landSection`.
mock.LAND = {};
function landCard(id, over) {
  mock.LAND[id] = Object.assign({}, T1, { id, display_title: id.replace("-", " "),
    created_at: new Date().toISOString() }, over);
  return mock.LAND[id];
}
mock.landList = [];
mock.landPerms = [];
// What the typing readout's endpoint answers, and every request it got.
mock.typingAnswer = { line: "", count: 0, since_ms: -1, open: true, reason: "nothing typed here yet" };
mock.typingPolls = [];
// Every PATCH that set a card's alias. See `aliasSection`.
mock.aliasWrites = [];

mock.filesDeleted = [];  // what the mock's /_hub/files delete was asked for
mock.boardDocs = false;    // whether the mock answers /_hub/docs, as a hub with documents does
mock.tasksMode = "first";   // first | hang | second | pinned | loop | worn | untagged | land
// One card per shipped terminal theme, filled in from the page's own table by
// the card-colours section, plus one with no theme that takes the repo default.
mock.wornTasks = [];
// Whether the cached list agrees the loop card is attachable. Off during the
// loop repro (the list lags the live card), on once it has recovered.
mock.loopListSupervised = false;
// How the mocked hub answers a card-scoped poll (GET /v1/tasks/<id>), which is
// what a popped-out window opens on. `noroom` is the transient hub-restart state
// (503, "no room is attached"), `gone` is a genuine missing card (404), and `ok`
// returns the card.
mock.soloMode = "ok";       // ok | noroom | gone
// Whether the mock answers the `/_hub/*` probes as a hub. Off by default so the
// board runs as a plain daemon for the tests above; the room-picker test turns
// it on and loads a fresh page. `sggAttached` is the one room that flips from
// disconnected to live under the open dropdown.
mock.hubMode = false;
mock.setupFixed = [];
mock.sggAttached = false;
// Whether the hub has a room to borrow `/v1/settings` from. False is the window
// right after a hub restart: no room has re-attached, so the ALL-view read is
// answered with the same 409 the real hub gives, and the board's load-time skin
// read fails. Flipping it true and pushing a `rooms` event is a room attaching,
// which is when the skin must heal without a reload. See the skin-heals test.
mock.hubHasRoom = true;
// The hub restart gate's mocked state: whether a pause is held, and how many
// times the board called each of its three endpoints.
mock.gatePaused = false;
// What `GET /_hub/restart` says is left of a countdown, for a window opened during one.
mock.gateCountdownLeft = 0;
// Which hub process `GET /_hub/restart` names, and whether the hub is away. Away
// drops every stream and hub call on the floor, the way a stopped hub refuses
// the connection, so the board's event stream keeps retrying. See
// restartStaysSection.
mock.gateBoot = "boot-a";
mock.hubAway = false;
mock.gateRestartMode = "";
mock.gateAsked = 0;
mock.gateSlowMs = 1500;
// The board build `/v1/health` reports. A change makes the board reload.
mock.healthBuild = "test";
// Whether the service worker and its offline page are served, and whether the
// page itself answers 502 the way a share in front of a stopped atrium does.
// Off by default: every other section runs without a worker. See atriumDownSection.
mock.serveSW = false;
mock.page502 = false;
// The reconnect delay the event stream asks for, in ms. 0 says nothing and leaves the browser's own 3s, which is what
// the daemon does. A section that drops the stream on purpose sets it so the reconnect is not waited out.
mock.sseRetry = 0;
mock.gateCalls = { input: 0, pause: 0, resume: 0 };
mock.ALPHA = { name: "alpha", host: "alpha-host" };
mock.SGG = { name: "sgg", host: "sgg-host" };

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
mock.auditLive = false;
const AUDIT_LIVE = { id: "a5", at: "2026-09-19T12:10:00Z", room: "sgg", kind: "session-exit",
  detail: "second card exited with code 0 after 3s" };
// `auditMany` swaps in a feed far taller than the window, for the scroll test, and
// `auditManyLive` puts one more line on top of it.
mock.auditMany = false;
mock.auditManyLive = false;
const AUDIT_MANY = Array.from({ length: 150 }, (_, i) => ({
  id: "m" + (150 - i), at: new Date(Date.UTC(2026, 8, 19, 12, 0, 150 - i)).toISOString(),
  room: i % 2 ? "sgg" : "alpha", kind: "session-start", detail: "card " + (150 - i) + " started"
}));
const AUDIT_MANY_LIVE = { id: "m151", at: "2026-09-19T12:03:00Z", room: "sgg", kind: "session-exit",
  detail: "card 151 exited" };
function auditFeed() {
  if (mock.auditMany) return mock.auditManyLive ? [AUDIT_MANY_LIVE].concat(AUDIT_MANY) : AUDIT_MANY.slice();
  return mock.auditLive ? [AUDIT_LIVE].concat(AUDIT_BASE) : AUDIT_BASE.slice();
}
function resetAudit() { mock.auditLive = false; mock.auditMany = false; mock.auditManyLive = false; }

// The board skin follows the room-picker scope: the ALL view (no X-Atrium-Room
// header) wears the HUB's own skin, and each room wears its own. `skinFor` is
// the mocked hub-plus-rooms state, keyed by scope with "" for the ALL/hub view.
// A skin-only save in the ALL view lands on the hub (skinFor[""]); one made
// while scoped to a room lands on that room, and never on the hub. The list is
// the same across scopes; only the selected one differs.
const SKINS = ["harbour", "moss", "noir", "ember", "vapor", "sandstone"];
mock.skinFor = { "": "noir", alpha: "moss", sgg: "ember" };
function resetSkins() { mock.skinFor = { "": "noir", alpha: "moss", sgg: "ember" }; }
function settingsBody(room) {
  const skin = mock.skinFor[room] != null ? mock.skinFor[room] : mock.skinFor[""];
  return Object.assign({ global_auto: mock.gautoOn, global_auto_seconds: 0, board_skin: skin, board_skins: SKINS,
    card_colors: mock.cardColorsMock, idle_park_after_now: mock.idleParkNow, idle_park_after_default: 7200,
    idle_park_after_min: 1800 }, mock.kaSettings);
}
// The idle park timer the mocked room holds, seconds or "off", and every write the board made to it.
mock.idleParkNow = 7200;
mock.idleParkWrites = [];
// The card colours the mocked daemon holds: the seed a fresh daemon writes, then whatever the board saved.
mock.cardColorsMock = { default: "", repos: {
  "github/openziti-test-kitchen/appetizer": "active-work", "github/dovholuknf/atrium": "active-light",
  "github/openziti/desktop-edge-win": "dracula", "github/netfoundry/docusaurus-shared": "matrix",
  "github/dovholuknf/dotfiles": "tangent", "github/openziti/sdk-golang": "terracotta",
  "github/netfoundry/sterling": "orange-coral", "github/openziti/ziti": "teal-dusk",
  "github/openziti/ziti-console": "deep-amethyst", "github/openziti/ziti-doc": "imperial-purple",
  "github/openziti/ziti-openwrt": "ocean-deep", "github/openziti/ziti-sdk-c": "neon-grape",
  "github/openziti/ziti-sdk-csharp": "nord", "github/openziti/ziti-sdk-py": "deep-ocean",
  "github/openziti/ziti-tunnel-sdk-c": "gruvbox-dark", "github/openziti-test-kitchen/ziti-tv": "mauve-purple",
  "github/openziti/zrok": "electric-purple" } };
// The cache keep-alive's room settings, and every write the board made to it or
// to a card's switch. See keepaliveSection.
mock.kaSettings = {};
mock.kaWrites = [];
// The global auto switch the mocked daemon holds, and whether a room-scoped
// settings read fails: the room the picker is scoped to has not re-attached yet,
// so the hub cannot reach it. See the gauto-never-blank test.
mock.gautoOn = false;
mock.roomSettingsDown = false;
// Every settings read answered, and how long each takes. A hub's answer takes
// over 100ms, which is what lets a load's several readers overlap.
mock.settingsReads = 0;
mock.settingsDelay = 0;
mock.settingsHold = null;
// The on/off switch rows: one runner and one fixture in each state, so the test
// can compare the pill's box across an on row and an off row, and flip each way.
// `switchFail` makes the next write refuse, which must put the pill back.
// `switchWrites` is every write the switches made, as "METHOD path enabled".
mock.HON = { id: "hon", label: "claude", cmd: "claude", args: [], enabled: true,
  found: "/usr/bin/claude", launch_mode: "pty" };
mock.HOFF = Object.assign({}, mock.HON, { id: "hoff", label: "codex", cmd: "codex", enabled: false });
mock.FON = { id: "fon", label: "notes", harness: "claude", cwd: "/tmp/notes", enabled: true,
  resume: true, sort: 0 };
mock.FOFF = Object.assign({}, mock.FON, { id: "foff", label: "scratch", enabled: false, sort: 1 });
mock.switchFail = false;
mock.switchWrites = [];
function resetSwitches() {
  mock.HON.enabled = true; mock.HOFF.enabled = false; mock.FON.enabled = true; mock.FOFF.enabled = false;
  mock.switchFail = false; mock.switchWrites = [];
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
    mock.switchWrites.push(req.method + " " + req.url.split("?")[0] + " " + body.enabled);
    if (mock.switchFail) {
      mock.switchFail = false;
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
mock.roomStallUntil = 0;
mock.stalledCount = 0;
// The reads a stall is holding, so a stall timed on a page's fake clock can end them when it ends.
const stallHeld = [];

const HTML = wholeBoard();

// The file endpoints the walk drawer uses, answering from a directory per card and the way
// internal/api/filetext.go does: a write quotes the hash it read, and a stale one is a 409 that hands back what
// is there now. `walkDirs` is card id -> a TEMPORARY COPY, never a real review folder. See `walkSection`.
mock.walkDirs = {};
const walkHash = b => crypto.createHash("sha256").update(b).digest("hex");
function walkRoute(req, res, url) {
  const m = /^\/v1\/tasks\/([^/]+)\/files\/(list|text)$/.exec(url);
  if (!m || !mock.walkDirs[m[1]]) return false;
  const dir = mock.walkDirs[m[1]];
  const rel = new URL(req.url, "http://x").searchParams.get("path") || "";
  const full = path.resolve(dir, rel);
  const deny = () => { res.writeHead(403, { "Content-Type": "application/json" }); res.end('{"error":"outside"}'); };
  if (full !== path.resolve(dir) && !full.startsWith(path.resolve(dir) + path.sep)) { deny(); return true; }
  if (m[2] === "list") {
    let ents;
    try { ents = fs.readdirSync(full, { withFileTypes: true }); } catch (e) { deny(); return true; }
    const entries = ents.map(d => {
      const st = fs.statSync(path.join(full, d.name));
      return { name: d.name, path: rel ? rel.replace(/\/$/, "") + "/" + d.name : d.name, dir: d.isDirectory(),
        size: d.isDirectory() ? undefined : st.size, mtime: st.mtime.toISOString() };
    }).sort((a, b) => (a.dir === b.dir ? (a.name < b.name ? -1 : 1) : a.dir ? -1 : 1));
    sendJSON(res, { path: rel, parent: "", entries });
    return true;
  }
  if (req.method === "PUT") {
    let raw = "";
    req.on("data", c => { raw += c; });
    req.on("end", () => {
      let body = {};
      try { body = JSON.parse(raw || "{}"); } catch (e) {}
      if (!String(body.hash || "").trim()) {
        res.writeHead(400, { "Content-Type": "application/json" });
        res.end('{"error":"a write has to say what it was based on"}');
        return;
      }
      let cur;
      try { cur = fs.readFileSync(full); } catch (e) { deny(); return; }
      if (walkHash(cur) !== body.hash) {
        res.writeHead(409, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: "that file changed while you were editing it. nothing was written.",
          text: cur.toString("utf8").replace(/\r\n/g, "\n"), hash: walkHash(cur) }));
        return;
      }
      let out = String(body.text || "");
      if (body.eol === "\r\n") out = out.replace(/\r\n/g, "\n").replace(/\n/g, "\r\n");
      fs.writeFileSync(full, out);
      sendJSON(res, { ok: true, hash: walkHash(Buffer.from(out)) });
    });
    return true;
  }
  let raw;
  try { raw = fs.readFileSync(full); } catch (e) { deny(); return true; }
  if (raw.length > (2 << 20)) {
    res.writeHead(400, { "Content-Type": "application/json" });
    res.end('{"error":"that file is too large to edit here. download it instead"}');
    return true;
  }
  sendJSON(res, { path: full.replace(/\\/g, "/"), text: raw.toString("utf8").replace(/\r\n/g, "\n"),
    hash: walkHash(raw), eol: raw.includes("\r\n") ? "\r\n" : "\n" });
  return true;
}

// THE UNIFIED CARD (docs/rnd/unified-card.md): one card per state, and 111 finished ones for the cap.
const UC_AT = new Date(Date.UTC(2026, 8, 19, 12, 0, 0)).toISOString();
const ucCard = (id, status, more) => Object.assign({
  id, status, display_title: id, runner: "claude", rank: 1, worktree: "/tmp/" + id, why: "", idle_seconds: 30, wait_seconds: 0,
  waiting_since: UC_AT, created_at: UC_AT, last_activity_at: UC_AT, tags: [], supervised: false, offline: false, pinned: false,
  auto_approve: false, pid: 0
}, more || {});
const UC_CARDS = [
  ucCard("uc-ready", "needs-input", { supervised: true, pid: 11 }),
  ucCard("uc-agent", "needs-input", { supervised: true, pid: 12, tags: ["origin:agent"] }),
  ucCard("uc-perm", "needs-permission", { supervised: true, pid: 13 }),
  ucCard("uc-work", "running", { supervised: true, pid: 14, activity: { what: "thinking" } }),
  ucCard("uc-quiet", "running", { supervised: true, pid: 15 }),
  ucCard("uc-gone", "needs-input", { supervised: false }),
  ucCard("uc-parked", "needs-input", { supervised: false, parked_at: UC_AT }),
  ucCard("uc-paused", "paused"),
  ucCard("uc-done", "done"),
  ucCard("uc-dead", "dead"),
];
const UC_MANY = Array.from({ length: 111 }, (_, i) => ucCard("ud" + i, "done", {
  pinned: i < 17, last_activity_at: new Date(Date.UTC(2026, 8, 19, 12, 0, 0) - i * 3600e3).toISOString() }));

function sendJSON(res, obj) {
  const body = JSON.stringify(obj);
  res.writeHead(200, { "Content-Type": "application/json" });
  res.end(body);
}

const server = http.createServer((req, res) => {
  const url = req.url.split("?")[0];

  if (mock.hubAway) { req.socket.destroy(); return; }

  if (mock.roomStallUntil > Date.now() && url.startsWith("/v1/") && url !== "/v1/health" &&
      !url.startsWith("/v1/events")) {
    mock.stalledCount++;
    const answer = () => { clearTimeout(t); if (!res.headersSent) { res.writeHead(503); res.end("{}"); } };
    const t = setTimeout(answer, mock.roomStallUntil - Date.now());
    stallHeld.push(answer);
    return;
  }

  if (url === "/working.gif") {
    res.writeHead(200, { "Content-Type": "image/gif" });
    res.end(fs.readFileSync(path.join(WEB_ROOT, "working.gif")));
    return;
  }
  if (mock.serveSW && (url === "/sw.js" || url === "/down.html")) {
    fs.readFile(path.join(WEB_ROOT, url), (err, body) => {
      if (err) { res.writeHead(404); res.end(""); return; }
      res.writeHead(200, { "Content-Type": url.endsWith(".js") ? "application/javascript" : "text/html",
        "Cache-Control": "no-store" });
      res.end(body);
    });
    return;
  }
  // THE PAGE AS A BROWSER GETS IT, one <script src> per file. HTML above is every script inlined as one, where a
  // function declaration hoists across files, so a file calling one that loads later works here and throws live.
  // Only `bootClean` asks for this. `/raw/<anything>` is the same page, for a pop-out or card path.
  if (url === "/raw" || url.startsWith("/raw/")) {
    res.writeHead(200, { "Content-Type": "text/html" });
    res.end(fs.readFileSync(path.join(WEB_ROOT, "index.html")));
    return;
  }
  if (url === "/read.html" || url === "/read.js") {
    res.writeHead(200, { "Content-Type": url.endsWith(".js") ? "application/javascript" : "text/html" });
    res.end(fs.readFileSync(path.join(WEB_ROOT, url)));
    return;
  }
  if (/^\/(js|css)\/[A-Za-z0-9_.-]+\.(js|css)$/.test(url)) {
    return fs.readFile(path.join(WEB_ROOT, url), (err, body) => {
      if (err) { res.writeHead(404); res.end(""); return; }
      res.writeHead(200, { "Content-Type": url.endsWith(".js") ? "application/javascript" : "text/css" });
      res.end(body);
    });
  }
  if (url === "/" || url === "/index.html") {
    if (mock.page502) { res.writeHead(502, { "Content-Type": "text/plain" }); res.end("bad gateway"); return; }
    res.writeHead(200, { "Content-Type": "text/html" });
    res.end(HTML);
    return;
  }
  // The xterm bundle off disk, so a real terminal is built. Confined to
  // /vendor/ under the web root.
  // The phone page itself, which the chat tab embeds (`chatTab`).
  if (url === "/m/" || url === "/m") {
    res.writeHead(200, { "Content-Type": "text/html" });
    res.end(fs.readFileSync(path.join(WEB_ROOT, "m", "index.html")));
    return;
  }
  if ((url.startsWith("/vendor/") || url.startsWith("/m/")) && !url.includes("..")) {
    const file = path.join(WEB_ROOT, url);
    fs.readFile(file, (err, body) => {
      if (err) { res.writeHead(404); res.end(""); return; }
      const type = url.endsWith(".css") ? "text/css" : "application/javascript"; // /m/ is the phone page's composer files
      res.writeHead(200, { "Content-Type": type });
      res.end(body);
    });
    return;
  }
  // A card-scoped poll, the one a popped-out window opens on. The hub answers a
  // no-room restart with 503 and a genuine missing card with 404, and the solo
  // window has to tell those apart. Placed before the list route, which is the
  // exact path "/v1/tasks" with no trailing id.
  // The typing gate readout's poll. Counted, so the section can prove it is not
  // polled while the readout is off. See `typingSection`.
  if (url.startsWith("/v1/tasks/") && url.endsWith("/typing")) {
    mock.typingPolls.push(req.url);
    sendJSON(res, mock.typingAnswer);
    return;
  }
  // A card's keep-alive switch. Recorded, and answered the way the daemon does.
  if (url.startsWith("/v1/tasks/") && url.endsWith("/keepalive") && req.method === "POST") {
    let raw = "";
    req.on("data", c => { raw += c; });
    req.on("end", () => {
      let body = {};
      try { body = JSON.parse(raw || "{}"); } catch (e) {}
      mock.kaWrites.push({ url, body });
      sendJSON(res, { task_id: url.split("/")[3], keepalive: { state: body.on ? "on" : "off" } });
    });
    return;
  }
  // A `? N` chip's dismiss. Recorded, answered as the daemon does: the card's
  // open questions go away unless the mock is told the set was stale.
  if (url.startsWith("/v1/tasks/") && url.endsWith("/questions/dismiss") && req.method === "POST") {
    let raw = "";
    req.on("data", c => { raw += c; });
    req.on("end", () => {
      let body = {};
      try { body = JSON.parse(raw || "{}"); } catch (e) {}
      const id = url.split("/")[3];
      mock.qDismissWrites.push({ url, body });
      if (mock.qDismissMode === "fail") {
        res.writeHead(500, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: "the store said no" }));
        return;
      }
      if (mock.qDismissMode === "nothing") { sendJSON(res, { dismissed: false, stale: false }); return; }
      if (mock.qDismissMode === "stale") { sendJSON(res, { dismissed: false, stale: true }); return; }
      mock.qDismissed.add(id);
      sendJSON(res, { dismissed: true, stale: false });
    });
    return;
  }
  if (walkRoute(req, res, url)) return;
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
        if (id === "pin1" && typeof body.pinned === "boolean") mock.PIN.pinned = body.pinned;
        if ("alias" in body) mock.aliasWrites.push({ id, body });
        // A theme kept from the picker, saved on the worn list so a reload reads it back.
        const worn = mock.tasksMode === "worn" && mock.wornTasks.find(t => t.id === id);
        if (worn && typeof body.theme === "string") { worn.theme = body.theme; sendJSON(res, worn); return; }
        sendJSON(res, id === "pin1" ? mock.PIN : T1);
      });
      return;
    }
    // Ahead of the solo modes, which an earlier section can leave set: the
    // group sections read this card's tags for its menu.
    if (id === "filed1") { sendJSON(res, FILED); return; }
    if (id.startsWith("ll-") && mock.tasksMode === "worn") {
      const one = mock.wornTasks.find(t => t.id === id);
      if (one) { sendJSON(res, one); return; }
    }
    if ((id.startsWith("land-") || id.startsWith("cc-")) && !id.includes("/")) {
      if (mock.LAND[id]) { sendJSON(res, mock.LAND[id]); return; }
      res.writeHead(404, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "no such card" }));
      return;
    }
    if (mock.soloMode === "noroom") {
      res.writeHead(503, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "no room is attached to this hub. the hub " +
        "serves the board and holds nothing, so until a room connects there is " +
        "nothing to show. run `atrium room join` on the machine your agents are on." }));
      return;
    }
    if (mock.soloMode === "gone") {
      res.writeHead(404, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "no such card" }));
      return;
    }
    if (id === "loop1") { sendJSON(res, LOOP); return; }
    // The solo card under its room-tagged spelling, the form the hub serves
    // while more than one room is attached. See `popoutTagFlipSection`.
    if (id === "sgg~s1") { sendJSON(res, Object.assign({}, SOLO, { id: "sgg~s1" })); return; }
    sendJSON(res, id === "s1" ? SOLO : id === "pin1" ? mock.PIN : (id === "t2" ? T2 : T1));
    return;
  }
  if (url === "/v1/tasks") {
    const got = hooks.tasks(true);
    if (got === "hang") { hungResponses.push(res); return; }
    if (got === "down") { res.writeHead(503); res.end("{}"); return; }
    sendJSON(res, { tasks: got });
    return;
  }
  // The runners page. Everything it reads besides runners and fixtures is empty.
  if (url === "/v1/harnesses") { sendJSON(res, { harnesses: [mock.HON, mock.HOFF] }); return; }
  if (url.startsWith("/v1/harnesses/") && req.method === "PUT") {
    switchPut(req, res, [mock.HON, mock.HOFF]); return;
  }
  if (url === "/v1/fixtures") { sendJSON(res, { fixtures: [mock.FON, mock.FOFF] }); return; }
  if (url.startsWith("/v1/fixtures/") && req.method === "PUT") {
    switchPut(req, res, [mock.FON, mock.FOFF]); return;
  }
  if (url === "/v1/hooks") { sendJSON(res, { missing: 0, hooks: [] }); return; }
  // The rooms pill's fix: the room it wrote to, and the setup the hub then reads clean.
  if (url === "/v1/hooks/install" && req.method === "POST") {
    mock.setupFixed.push(req.headers["x-atrium-room"] || "");
    delete mock.SGG.setup;
    sendJSON(res, { report: { missing: 0, hooks: [] }, backup: "C:/Users/x/.claude/settings.json.atrium-1.bak", changed: true });
    return;
  }
  if (url.startsWith("/_hub/setup-check") && req.method === "POST") { sendJSON(res, { room: "sgg", setup: null }); return; }
  if (url === "/v1/sources") { sendJSON(res, { sources: [] }); return; }
  if (url === "/v1/recognisers") { sendJSON(res, { recognisers: [] }); return; }
  if (url === "/v1/actions") { sendJSON(res, { actions: [] }); return; }
  if (url === "/v1/providers") { sendJSON(res, { providers: [] }); return; }
  if (url === "/v1/dispatch") { sendJSON(res, { dispatches: [] }); return; }
  if (url === "/v1/history") {
    if (!mock.histMany) { sendJSON(res, { tasks: [HIST], total: 1 }); return; }
    // Paged the way the store pages, so "show more" and the live re-read ask
    // for the same slices a real room answers.
    const q = new URL(req.url, "http://x").searchParams;
    const all = histFeed();
    const offset = +q.get("offset") || 0, limit = +q.get("limit") || 100;
    sendJSON(res, { tasks: all.slice(offset, offset + limit), total: all.length });
    return;
  }
  if (url === "/v1/waiting") { mock.waitingReads++; sendJSON(res, { tasks: [] }); return; }
  if (url === "/v1/permissions") { sendJSON(res, { permissions: mock.tasksMode === "land" ? mock.landPerms : [] }); return; }
  if (url === "/v1/shares") { sendJSON(res, { shares: [] }); return; }
  if (url === "/v1/rooms") { sendJSON(res, { rooms: [] }); return; }
  if (url === "/v1/usage" && req.method === "POST") {
    let raw = "";
    req.on("data", c => { raw += c; });
    req.on("end", () => {
      try { for (const [k, n] of Object.entries(JSON.parse(raw).counts || {})) boardUsageSeen.set(k, (boardUsageSeen.get(k) || 0) + n); } catch (e) {}
      res.writeHead(204); res.end();
    });
    return;
  }
  if (url === "/v1/usage/board") {
    const controls = [...boardUsageSeen].map(([control, n]) => ({ control, n, last: "2026-10-10" })).sort((x, y) => y.n - x.n);
    sendJSON(res, { controls }); return;
  }
  if (url === "/v1/health") {
    sendJSON(res, { build: mock.healthBuild, settling: false, halted: false }); return;
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
          mock.kaWrites.push({ url, body });
          if ("cache_keepalive_default" in body) mock.kaSettings.cache_keepalive_default = body.cache_keepalive_default;
          if (body.cache_keepalive_suspended === false) mock.kaSettings.cache_keepalive_suspended = "";
          sendJSON(res, settingsBody(room));
          return;
        }
        if ("idle_park_after" in body) {
          mock.idleParkWrites.push({ room, body });
          mock.idleParkNow = body.idle_park_after === "off" ? "off" : Number(body.idle_park_after);
          sendJSON(res, settingsBody(room));
          return;
        }
        // A skin-only save lands on the current scope: the hub for ALL, the room
        // when scoped. Anything else in the ALL view still needs a room (409),
        // which is the refusal the scoped skin does NOT get any more.
        if (keys.length === 1 && keys[0] === "card_colors") {
          mock.cardColorsMock = body.card_colors;
          sendJSON(res, settingsBody(room));
          return;
        }
        if (keys.length === 1 && keys[0] === "board_skin") {
          mock.skinFor[room] = body.board_skin;
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
    if (!room && !mock.hubHasRoom) {
      res.writeHead(409, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "pick a room first" }));
      return;
    }
    if (room && mock.roomSettingsDown) {
      res.writeHead(503, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "room " + room + " is not attached" }));
      return;
    }
    mock.settingsReads++;
    if (mock.settingsHold) { mock.settingsHold.then(() => sendJSON(res, settingsBody(room))); return; }
    if (mock.settingsDelay) { setTimeout(() => sendJSON(res, settingsBody(room)), mock.settingsDelay); return; }
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
    res.write((mock.sseRetry ? "retry: " + mock.sseRetry + "\n" : "") + ": open\n\n");
    openStreams.push(res);
    if (url === "/v1/events/hub") hubStreams.push(res);
    return;
  }
  // The hub probes. Off by default (a plain daemon 404s them); the room-picker
  // test turns `hubMode` on so the board runs as a hub with two rooms.
  if (url.startsWith("/_hub/docs")) {
    // Hub documents, for the board's documents tab. Off by default: a room's own board has none.
    if (!mock.boardDocs) { res.writeHead(404); res.end("not a hub"); return; }
    const v = { n: 1, at: "2026-09-30T12:00:00Z", by: "operator", origin: "local", size: 14, kind: "markdown", mime: "text/markdown", name: "board.md", sha: "x", missing: false, purged: false };
    if (url === "/_hub/docs/settings") { sendJSON(res, { operator: true, enabled: true, caps: {}, usage: {}, largest: [] }); return; }
    if (url.startsWith("/_hub/docs/board-doc/raw")) { res.writeHead(200, { "Content-Type": "application/octet-stream" }); res.end("# Board doc\n\nhello"); return; }
    if (url === "/_hub/docs/board-doc") { sendJSON(res, { slug: "board-doc", title: "Board doc", created: v.at, deleted: null, versions: [v] }); return; }
    if (url.startsWith("/_hub/docs")) { sendJSON(res, { docs: [{ slug: "board-doc", title: "Board doc", created: v.at, updated: v.at, versions: 1, latest: v, deleted: null }], usage: { bytes: 14, cap: 1000000 } }); return; }
  }
  if (url.startsWith("/_hub/files")) {
    // The hub file drop, for the board's files tab. It rides the documents flag: a hub with one has the other.
    if (!mock.boardDocs) { res.writeHead(404); res.end("not a hub"); return; }
    if (url === "/_hub/files" && req.method === "GET") {
      const now = Date.now();
      sendJSON(res, { files: [
        { name: "report.pdf", size: 2048, origin: "card", by: "card-a", room: "box", note: "the numbers", at: new Date(now - 3600000).toISOString(), expires: new Date(now + 5 * 86400000).toISOString() },
        { name: "old log.txt", size: 512, origin: "local", by: "", room: "", note: "", at: new Date(now - 86400000).toISOString(), expires: new Date(now + 3600000 * 5).toISOString() },
      ], usage: { bytes: 2560, cap: 1000000 } });
      return;
    }
    if (url.endsWith("/delete") && req.method === "POST") { mock.filesDeleted.push(decodeURIComponent(url.split("/")[3])); sendJSON(res, { ok: true }); return; }
    res.writeHead(404); res.end("no such files route"); return;
  }
  if (url === "/_hub/rooms") {
    if (!mock.hubMode) { res.writeHead(404); res.end("not a hub"); return; }
    if (!mock.hubHasRoom) { sendJSON(res, { rooms: [] }); return; }
    sendJSON(res, { rooms: mock.sggAttached ? [mock.ALPHA, mock.SGG] : [mock.ALPHA] });
    return;
  }
  if (url === "/_hub/inventory") {
    if (!mock.hubMode) { res.writeHead(404); res.end("not a hub"); return; }
    const alpha = Object.assign({ transport: "direct", attached: true,
      first_seen: "2026-09-19T06:00:00Z", last_seen: "2026-09-19T12:00:00Z" }, mock.ALPHA);
    // sgg has dialled in before, so it shows disconnected until it attaches: the
    // picker only lists an ever-connected room, never one that is only inventory.
    const sgg = Object.assign({ transport: "direct", attached: mock.sggAttached,
      first_seen: "2026-09-19T06:00:00Z", last_seen: "2026-09-19T11:00:00Z" }, mock.SGG);
    sendJSON(res, { rooms: [alpha, sgg] });
    return;
  }
  if (url === "/_hub/health") { res.writeHead(404); res.end("not a hub"); return; }
  // The hub restart gate's board calls. The countdown itself is pushed onto the
  // open streams by the test, the way the hub pushes it. See restartGateSection.
  if (url.startsWith("/_hub/restart")) {
    if (!mock.hubMode) { res.writeHead(404); res.end("not a hub"); return; }
    const sub = url.slice("/_hub/restart".length);
    if (sub === "/input") mock.gateCalls.input++;
    if (sub === "/pause") { mock.gateCalls.pause++; mock.gatePaused = true; }
    if (sub === "/resume") { mock.gateCalls.resume++; mock.gatePaused = false; }
    const answer = { paused: mock.gatePaused, waiting: false, countdown_left: mock.gateCountdownLeft, boards: 1,
      boot: mock.gateBoot };
    if (sub === "") mock.gateAsked++;
    // `gateRestartMode` shapes only the board's `GET /_hub/restart`: `fail` answers 503, and
    // `slow` holds the answer back, naming the hub as it was when it was asked.
    if (sub === "" && mock.gateRestartMode === "fail") { res.writeHead(503); res.end("{}"); return; }
    if (sub === "" && mock.gateRestartMode === "slow") { setTimeout(() => sendJSON(res, answer), mock.gateSlowMs); return; }
    sendJSON(res, answer);
    return;
  }
  // The hub's backlog and director reports, read-only on the board. Open items only when `open=1`, as the hub does.
  // See js/backlog.js.
  if (url.startsWith("/_hub/backlog") || url.startsWith("/_hub/reports")) {
    if (!mock.hubMode) { res.writeHead(404); res.end("not a hub"); return; }
    const q = new URL(req.url, "http://x").searchParams;
    if (url.startsWith("/_hub/backlog")) {
      let items = [
        { id: "f-003", dept: "fabric", title: "An inventory of resources", status: "built", priority: "low" },
        { id: "f-new-x", dept: "fabric", title: "Open work", status: "open" },
        { id: "r-037", dept: "runtime", title: "Finished work", status: "done" }
      ];
      if (q.get("open") === "1") items = items.filter(b => b.status !== "done" && b.status !== "dropped");
      if (q.get("dept")) items = items.filter(b => b.dept === q.get("dept"));
      sendJSON(res, { items });
      return;
    }
    sendJSON(res, { reports: [{ id: "rp_1", to_dept: "", from_by: "fabric", from_room: "sg4", subject: "f-003 built",
      body: "done and tested", at: "2026-09-30T12:00:00Z", read_at: null, read_by: null }] });
    return;
  }
  // The items a human owns that are blocked or finished. See js/backlog.js.
  if (url.startsWith("/_hub/items")) {
    if (!mock.hubMode) { res.writeHead(404); res.end("not a hub"); return; }
    sendJSON(res, { items: [{ item_id: "it-aa11", room: "sg4", card: "c1", state: "blocked", revision: 2,
      owner_kind: "human", updated_at: "2026-10-13T12:00:00Z" }] });
    return;
  }
  // The operational audit feed, newest first and filterable by room and kind the
  // same way the hub serves it, so the pane's filters can be driven against a
  // real response. See js/audit.js.
  if (url === "/_hub/audit") {
    if (!mock.hubMode) { res.writeHead(404); res.end("not a hub"); return; }
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

// THE MOCK BROADCASTS ITS CHANGES, the way the daemon does.
//
// The board no longer re-reads the cards on a clock: it reads them at load, on
// a resync, and when the stream says something changed. Every change on a real
// daemon is broadcast, so a mock whose answer changes without saying so is a
// daemon that does not exist. This watches what `/v1/tasks` and
// `/v1/permissions` would answer and, when either moves, says so on every open
// stream: a sweep `task-removed` (no id, so the board reads the list again) or a
// `permission`. `mockBroadcast` is off in the sections that count requests
// against a stream the test drives itself.
mock.mockBroadcast = true;
// Counted so `eventDriven` can prove the board never asks for it.
mock.waitingReads = 0;
mock.mockSaid = { tasks: "", perms: "" };
function mockFingerprint(v) { try { return JSON.stringify(v); } catch (e) { return String(v); } }
function mockSay(kind, data) {
  openStreams.forEach(r => { try { r.write("event: " + kind + "\ndata: " + data + "\n\n"); } catch (e) {} });
}
const mockWatch = setInterval(() => {
  const tasks = mockFingerprint(hooks.tasks(false));
  const perms = mockFingerprint(mock.tasksMode === "land" ? mock.landPerms : []);
  if (mock.mockBroadcast && mock.mockSaid.tasks && tasks !== mock.mockSaid.tasks) mockSay("task-removed", "{}");
  if (mock.mockBroadcast && mock.mockSaid.perms && perms !== mock.mockSaid.perms) mockSay("permission", "{}");
  mock.mockSaid = { tasks, perms };
}, 200);
mockWatch.unref();

/* The board counts its own controls (js/usage-board.js) and the ... menu lists them, most used first, with the ones
   never used at the bottom. */
const boardUsageSeen = new Map();

module.exports = { mock, hooks, FILED, T1, SEEN, qclickCards, UNTAGGED_CARDS, AGLIVE, SUBLIVE, SUBWORK, SUBASK, SUBDEAD, AGDEAD, SHELVED1, UC_CARDS, UC_MANY, LOOP, T2, SKINS, openStreams, hubStreams, stallHeld, NEWC, landCard, settingsBody, resetSkins, boardUsageSeen, QC_AT, mockSay, WEB_ROOT, HTML, server, resetPin, resetAudit, resetSwitches, hungResponses };
