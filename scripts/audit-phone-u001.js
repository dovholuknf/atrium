// u-001 phase 1: drive every screen of the board and of /m at two phone sizes, by touch, and write down what does
// not fit. Screenshots go to build.claude/u001-shots/ (JPEG, out of the repo) and the measurements to a JSON
// report. The few shots the audit cites are copied into docs/backlog/ui/img/u-001/ by hand. Nothing here changes
// the board: it is the evidence behind docs/backlog/ui/u-001-audit.md.
//
//   NODE_PATH=<dir with playwright> node scripts/audit-phone-u001.js [report.json]
//
// U001_SHOTS moves the shots. U001_ONLY=card-menu,questions runs only the named steps (the page still loads),
// and U001_WIDTH=390 only that width, which is how one gap is re-driven without the whole four minutes.
//
// It serves the concatenated board from board-source.js and the /m page off disk, with every endpoint mocked, the
// same way scripts/test-board-headless.js does. No atrium process is involved.

const http = require("http");
const fs = require("fs");
const path = require("path");
const { wholeBoard } = require("./board-source.js");

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) {
  try { ({ chromium } = require("playwright")); } catch (e2) { console.log("playwright is not installed"); process.exit(0); }
}

const WEB = path.join(__dirname, "..", "internal", "api", "web");
const SHOTS = process.env.U001_SHOTS || path.join(__dirname, "..", "build.claude", "u001-shots");
const REPORT = process.argv[2] || "";
const ONLY = process.env.U001_ONLY ? new Set(process.env.U001_ONLY.split(",")) : null;
const VIEWS = [{ width: 390, height: 844 }, { width: 412, height: 915 }]
  .filter(v => !process.env.U001_WIDTH || String(v.width) === process.env.U001_WIDTH);
// A soft keyboard takes about 40% of a portrait phone. Chromium cannot draw one, so the viewport is shortened
// by that much, which is what `interactive-widget=resizes-content` does to the layout viewport on Android.
const KB = 0.4;

const iso = msAgo => new Date(Date.now() - msAgo).toISOString();
const MIN = 60000;

// ── fixtures: every status, a long title, a long path, questions, a permission, a room chip ───────────────────
const card = (id, over) => Object.assign({
  id, status: "running", display_title: id, runner: "claude", rank: 1, worktree: "/home/clint/git/github/openziti/" + id,
  why: "", idle_seconds: 30, wait_seconds: 0, created_at: iso(3 * 3600000), last_activity_at: iso(MIN), tags: [],
  supervised: true, offline: false, pinned: false, auto_approve: false, seen: {}, activity: {}
}, over || {});
const CARDS = [
  card("perm1", { status: "needs-permission", display_title: "u-028 composer", alias: "composer", wait_seconds: 240,
    waiting_since: iso(4 * MIN), why: "wants to run go test ./internal/api/...", tags: ["ui", "wave-1"] }),
  card("ask1", { status: "needs-input", display_title: "r-021 resume after clear, the long title that never ends on a phone",
    wait_seconds: 900, waiting_since: iso(15 * MIN), worktree: "/home/clint/worktrees/claude/atrium/r-021-resume-after-clear",
    seen: { unseen: true, turn_ended_at: iso(15 * MIN), questions_at: iso(15 * MIN), answered: false,
      open_questions: ["land sa21 first, or wait for the review?", "keep the old flag for one release?"] },
    tags: ["runtime", "origin:agent"], pinned: true }),
  card("work1", { status: "running", display_title: "f-017 push trigger", activity: { what: "tool", tool: "Bash", seconds: 12 },
    tags: ["fabric"] }),
  card("unread1", { status: "needs-input", display_title: "t-003 reading view", wait_seconds: 120, waiting_since: iso(2 * MIN),
    seen: { unseen: true, turn_ended_at: iso(2 * MIN) } }),
  card("idle1", { status: "done", display_title: "docs sweep", supervised: false, idle_seconds: 7200,
    recap: "Swept the docs for em-dashes and fixed 14 files." }),
  card("shelf1", { status: "shelved", display_title: "parked experiment", supervised: false }),
  card("codex1", { status: "running", runner: "codex", display_title: "codex review", tags: ["review"] })
];
const PERMS = [
  { id: "p1", perm_id: "p1", task_id: "perm1", agent: "composer", tool: "Bash", requested_at: iso(4 * MIN),
    command: "cd /home/clint/worktrees/claude/atrium/u-028 && go test ./internal/api/... -run TestCompose -count=1 2>&1 | tail -40" },
  { id: "p2", perm_id: "p2", task_id: "ask1", agent: "r-021", tool: "Edit", requested_at: iso(2 * MIN),
    command: "internal/daemon/session.go",
    details: "--- a/internal/daemon/session.go\n+++ b/internal/daemon/session.go\n@@ -40,7 +40,9 @@ func (d *Daemon) sessionOf(t *store.Task) string {\n-\treturn t.ResumeID\n+\tif s := d.ctx.lastStarted(t.ID); s != \"\" {\n+\t\treturn s\n+\t}\n+\treturn t.ResumeID\n }\n" }
];
const HARNESSES = [
  { id: "h1", label: "claude", cmd: "claude", args: ["--dangerously-skip-permissions"], enabled: true, found: "/usr/bin/claude", launch_mode: "pty" },
  { id: "h2", label: "codex", cmd: "codex", args: [], enabled: false, found: "", launch_mode: "pty" }
];
const FIXTURES = [
  { id: "f1", label: "notes", harness: "claude", cwd: "/home/clint/notes", enabled: true, resume: true, sort: 0 },
  { id: "f2", label: "scratch", harness: "claude", cwd: "/tmp/scratch", enabled: false, sort: 1 }
];
const SOURCES = [{ id: "s1", label: "my PRs", argv: ["gh", "pr", "list", "--json", "url,title"], interval_seconds: 300, enabled: true,
  last_run_at: iso(3 * MIN), last_ok: true, failures: 0 }];
const PROVIDERS = [{ id: "pv1", name: "github", kind: "git", root: "/home/clint/git/github", layout: "org/repo", worktrees: true,
  worktree_root: "/home/clint/worktrees", repos: [{ org: "openziti", repo: "ziti", present: true }] }];
const RECOGNISERS = [{ id: "r1", label: "pull request", pattern: "https://github.com/(?P<org>[^/]+)/(?P<repo>[^/]+)/pull/(?P<num>\\d+)",
  cwd: "/home/clint/worktrees/{org}/{repo}/pr-{num}", title: "PR {num}", prompt: "review PR {num}", sort: 0 }];
const ACTIONS = [{ id: "a1", label: "run the tests", prompt: "run the tests and report", and_exit: false, tags: [], runners: [] }];
const HISTORY = [0, 1, 2, 3, 4, 5].map(n => ({ id: "h" + n, display_title: "old run " + n, runner: "claude", status: "done",
  created_at: iso((n + 1) * 86400000), why: "did thing " + n, recap: n % 2 ? "" : "Wrote the recap for run " + n, worktree: "/tmp/old" + n,
  archived_at: "" }));
const AUDIT = [
  { id: "a4", at: iso(4 * MIN), room: "alpha", kind: "permission-requested", detail: "Bash: go test ./internal/api/..." },
  { id: "a3", at: iso(9 * MIN), room: "sgg", kind: "room-attached", detail: "sgg running v2" },
  { id: "a2", at: iso(20 * MIN), room: "sgg", kind: "session-start", detail: "second card started on claude" },
  { id: "a1", at: iso(60 * MIN), kind: "hub-started", detail: "the hub came up" }
];
function usageBody() {
  const out = [];
  const now = Math.floor(Date.now() / 900000) * 900000;
  for (let i = 24; i > 0; i--) {
    const n = 2000 + (i * 7919 % 5000);
    const b = { rows: 3, replies: 3, input: n, output: n / 4, cache_write_5m: n / 2, cache_write_1h: 0, cache_read: n * 20, cost: 0 };
    out.push({ t: new Date(now - i * 900000).toISOString(), total: b, cards: { perm1: b }, causes: { operator: b } });
  }
  return { buckets: out };
}
let hub = false;

function readBody(req) {
  return new Promise(r => { let s = ""; req.on("data", c => { s += c; }); req.on("end", () => r(s)); });
}
const unknown = new Set();
const streams = [];
// Every POST, so a step can say what a tap actually sent (a questions chip that dismisses on a tap, say).
const posts = [];
const HTML = wholeBoard();
const TYPES = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".png": "image/png", ".svg": "image/svg+xml",
  ".webmanifest": "application/manifest+json", ".gif": "image/gif", ".woff2": "font/woff2" };

const server = http.createServer(async (req, res) => {
  const u = new URL(req.url, "http://x");
  const p = u.pathname;
  const json = (o, code) => { res.writeHead(code || 200, { "Content-Type": "application/json" }); res.end(JSON.stringify(o)); };
  if (p === "/" || p === "/index.html") { res.writeHead(200, { "Content-Type": "text/html" }); return res.end(HTML); }
  if (p === "/m" || p === "/m/") { res.writeHead(200, { "Content-Type": "text/html" }); return res.end(fs.readFileSync(path.join(WEB, "m", "index.html"))); }
  if ((p.startsWith("/vendor/") || p.startsWith("/m/") || p.startsWith("/css/") || p === "/working.gif") && !p.includes("..")) {
    const f = path.join(WEB, p);
    if (fs.existsSync(f) && fs.statSync(f).isFile()) {
      res.writeHead(200, { "Content-Type": TYPES[path.extname(f)] || "application/octet-stream" });
      return res.end(fs.readFileSync(f));
    }
    res.writeHead(404); return res.end("");
  }
  if (p.startsWith("/v1/events")) {
    res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" });
    res.write(": open\n\n");
    streams.push(res);
    req.on("close", () => { const i = streams.indexOf(res); if (i >= 0) streams.splice(i, 1); });
    return;
  }
  if (req.method !== "GET") { await readBody(req); posts.push(p); return json({ ok: true, queued: false, typed: true, dismissed: true }); }
  if (p === "/v1/tasks") return json({ tasks: CARDS });
  if (p === "/v1/permissions") return json({ permissions: PERMS });
  if (p === "/v1/permissions/history") return json({ decisions: [] });
  if (p === "/v1/rules") return json({ rules: [{ id: "r1", tool: "Bash", pattern: "go build", decision: "approve", reason: "", created_at: iso(86400000) }] });
  if (p === "/v1/harnesses") return json({ harnesses: HARNESSES });
  if (p === "/v1/fixtures") return json({ fixtures: FIXTURES });
  if (p === "/v1/sources") return json({ sources: SOURCES });
  if (p === "/v1/providers") return json({ providers: PROVIDERS });
  if (p === "/v1/recognisers") return json({ recognisers: RECOGNISERS });
  if (p === "/v1/actions") return json({ actions: ACTIONS });
  if (p === "/v1/dispatch") return json({ dispatches: [] });
  if (p === "/v1/hooks") return json({ missing: 1, hooks: [{ event: "PreToolUse", wired: true, why: "the gate" }, { event: "Stop", wired: false, why: "reach an idle session" }] });
  if (p === "/v1/history") return json({ tasks: HISTORY, total: HISTORY.length });
  if (p === "/v1/waiting") return json({ tasks: [] });
  if (p === "/v1/shares") return json({ shares: [] });
  if (p === "/v1/rooms") return json({ rooms: [] });
  if (p === "/v1/themes") return json({ themes: [] });
  if (p === "/v1/overlays") return json({ overlays: [] });
  if (p === "/v1/health") return json({ build: "audit", settling: false, halted: false });
  if (p === "/v1/usage") return json(usageBody());
  if (p === "/v1/usage/items") return json({ items: [], unlinked: 0 });
  if (p === "/v1/usage/limits") return json({ limits: [] });
  if (p === "/v1/settings") return json({ global_auto: false, global_auto_seconds: 0, board_skin: "harbour",
    board_skins: ["harbour", "moss", "noir", "ember", "vapor", "sandstone", "website"], scrollback_lines: 5000 });
  if (p === "/v1/browse") return json({ path: "/home/clint", parent: "/home", entries: [{ name: "git", path: "/home/clint/git", dir: true }, { name: "worktrees", path: "/home/clint/worktrees", dir: true }] });
  let m = p.match(/^\/v1\/tasks\/([^/]+)\/replies$/);
  if (m) return json({ source: "transcript", replies: [
    { at: iso(16 * MIN), text: "I read the resume path. `sessionOf` returns **ResumeID** after a `/clear`, which points at the old conversation.\n\n```\nfunc (d *Daemon) sessionOf(t *store.Task) string { return t.ResumeID }\n```" },
    { at: iso(15 * MIN), text: "Two questions before I change it:\n\n1. land sa21 first, or wait for the review?\n2. keep the old flag for one release?" }] });
  m = p.match(/^\/v1\/tasks\/([^/]+)$/);
  if (m) { const t = CARDS.find(c => c.id === decodeURIComponent(m[1])); return t ? json(t) : json({ error: "no such card" }, 404); }
  if (/^\/v1\/tasks\/[^/]+\/(sessions|messages|events)$/.test(p)) return json({ sessions: [], messages: [], events: [] });
  if (/^\/v1\/tasks\/[^/]+\/files\/list$/.test(p)) return json({ path: "", parent: "", entries: [{ name: "README.md", path: "README.md", dir: false, size: 1200, mtime: iso(MIN) }] });
  if (/^\/v1\/tasks\/[^/]+\/typing$/.test(p)) return json({ line: "", count: 0, since_ms: -1, open: true, reason: "" });
  if (p === "/_hub/rooms") return hub ? json({ rooms: [{ name: "alpha", host: "alpha-host" }, { name: "sgg", host: "sgg-host" }] }) : json({ error: "not a hub" }, 404);
  if (p === "/_hub/inventory") return hub ? json({ rooms: [
    { name: "alpha", host: "alpha-host", transport: "direct", attached: true, first_seen: iso(86400000), last_seen: iso(1000) },
    { name: "sgg", host: "sgg-host", transport: "direct", attached: true, first_seen: iso(86400000), last_seen: iso(1000) }] }) : json({}, 404);
  if (p === "/_hub/audit") return hub ? json({ events: AUDIT }) : json({}, 404);
  if (p === "/_hub/restart") return hub ? json({ paused: false, waiting: false, countdown_left: 0, boards: 1, boot: "a" }) : json({}, 404);
  if (p.startsWith("/_hub/")) return hub ? json({}) : json({}, 404);
  unknown.add(p);
  json({});
});

// A fake attach socket: opens, says the pty size, and paints a claude-like screen with a question on it.
function fakeSock() {
  const Real = window.WebSocket;
  window.__sent = [];
  window.WebSocket = function (url, protocols) {
    if (!/\/attach(\?|$)/.test(url)) return new Real(url, protocols);
    const s = { url, readyState: 0, binaryType: "arraybuffer", bufferedAmount: 0, onopen: null, onclose: null, onmessage: null,
      onerror: null, send(d) { window.__sent.push(String(d)); }, close() { this.readyState = 3; } };
    setTimeout(() => {
      s.readyState = 1; if (s.onopen) s.onopen({});
      const say = d => s.onmessage && s.onmessage({ data: d });
      say('{"t":"size","cols":120,"rows":36}');
      let screen = "\x1b[2J\x1b[H\x1b[1m> resume after clear\x1b[0m\r\n\r\n";
      for (let i = 0; i < 20; i++) screen += "  " + ("line " + i + " of the reply, reading sessionOf and ResumeID ").repeat(2) + "\r\n";
      screen += "\r\n\x1b[36m╭──────────────────────────────────────────────────────────────╮\x1b[0m\r\n";
      screen += "\x1b[36m│\x1b[0m Do you want to run go test ./internal/api/...?                 \x1b[36m│\x1b[0m\r\n";
      screen += "\x1b[36m│\x1b[0m \x1b[1m❯ 1. Yes\x1b[0m                                                     \x1b[36m│\x1b[0m\r\n";
      screen += "\x1b[36m│\x1b[0m   2. Yes, and don't ask again for go test                     \x1b[36m│\x1b[0m\r\n";
      screen += "\x1b[36m│\x1b[0m   3. No, and tell Claude what to do differently (esc)        \x1b[36m│\x1b[0m\r\n";
      screen += "\x1b[36m╰──────────────────────────────────────────────────────────────╯\x1b[0m\r\n";
      say(screen);
    }, 0);
    return s;
  };
  Object.assign(window.WebSocket, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });
}

// ── the measurements, run in the page ─────────────────────────────────────────────────────────────────────────
// Everything visible and interactive inside `scope` (or the page). A target is small under 44px on either side.
// Inline links inside running text are exempt, as WCAG 2.5.8 exempts them. Occluded controls (under a dialog or
// a sheet) are skipped by asking elementFromPoint who is on top at the centre.
function scan(scope) {
  const W = innerWidth, H = innerHeight;
  const root = scope ? document.querySelector(scope) : document.body;
  if (!root) return { error: "no " + scope };
  const shown = el => {
    const r = el.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) return false;
    for (let e = el; e && e !== document.documentElement; e = e.parentElement) {
      const cs = getComputedStyle(e);
      if (cs.display === "none" || cs.visibility === "hidden" || +cs.opacity === 0) return false;
      if (e.tagName === "DETAILS" && !e.open && e !== el && !(el.tagName === "SUMMARY" && el.parentElement === e)) return false;
    }
    return true;
  };
  const label = el => {
    let s = el.tagName.toLowerCase();
    if (el.id) s += "#" + el.id;
    else if (el.className && typeof el.className === "string") s += "." + el.className.trim().split(/\s+/).slice(0, 2).join(".");
    const t = (el.getAttribute("aria-label") || el.textContent || el.value || el.placeholder || "").replace(/\s+/g, " ").trim().slice(0, 28);
    return t ? s + " \"" + t + "\"" : s;
  };
  const scroller = el => {
    for (let e = el.parentElement; e && e !== document.body; e = e.parentElement) {
      const ox = getComputedStyle(e).overflowX;
      if ((ox === "auto" || ox === "scroll" || ox === "hidden") && e.scrollWidth > e.clientWidth + 1) return e;
    }
    return null;
  };
  const onTop = el => {
    const r = el.getBoundingClientRect();
    const x = Math.min(Math.max(r.left + r.width / 2, 0), W - 1), y = Math.min(Math.max(r.top + r.height / 2, 0), H - 1);
    if (r.bottom < 0 || r.top > H) return true; // below the fold: reachable by scrolling, so counted
    const hit = document.elementFromPoint(x, y);
    return !hit || hit === el || el.contains(hit) || hit.contains(el);
  };
  const SEL = "button, a[href], input:not([type=hidden]), select, textarea, summary, [role=button], [role=tab], nav .tab, [onclick]";
  const small = [], offEdge = [], sideways = [];
  const seen = new Set();
  for (const el of root.querySelectorAll(SEL)) {
    if (seen.has(el)) continue;
    if (el.closest("[onclick]") !== el && el.matches("[onclick]") === false && el.closest("button")) continue;
    if (!shown(el)) continue;
    if (el.matches("[onclick]") && !el.matches("button, a, input, select, textarea, summary, .tab, [role=button]")) {
      // a row that is clickable as a whole is a big target by definition
      const r = el.getBoundingClientRect();
      if (r.width >= 200) continue;
    }
    seen.add(el);
    const r = el.getBoundingClientRect();
    const inline = el.tagName === "A" && getComputedStyle(el).display === "inline" && el.parentElement &&
      el.parentElement.textContent.trim().length > el.textContent.trim().length + 20;
    if (!onTop(el)) continue;
    const sc = scroller(el);
    if (r.right > W + 1 || r.left < -1) {
      (sc ? sideways : offEdge).push(label(el) + " @" + Math.round(r.left) + ".." + Math.round(r.right));
    }
    if (!inline && (r.width < 44 || r.height < 44)) small.push({ l: label(el), w: Math.round(r.width), h: Math.round(r.height) });
  }
  // Text that is cut: a box narrower than what it holds, with its overflow hidden.
  const clipped = [];
  for (const el of root.querySelectorAll("*")) {
    if (!el.firstChild || ![...el.childNodes].some(n => n.nodeType === 3 && n.textContent.trim())) continue;
    if (el.scrollWidth <= el.clientWidth + 1 || !el.clientWidth) continue;
    const cs = getComputedStyle(el);
    if (cs.overflowX === "visible" || cs.overflowX === "auto" || cs.overflowX === "scroll") continue;
    if (!shown(el) || !onTop(el)) continue;
    clipped.push(label(el) + (cs.textOverflow === "ellipsis" ? " (ellipsis)" : " (hard cut)"));
  }
  // Text too small to read at arm's length.
  let tiny = 0; const tinySamples = [];
  for (const el of root.querySelectorAll("*")) {
    if (![...el.childNodes].some(n => n.nodeType === 3 && n.textContent.trim())) continue;
    const fs = parseFloat(getComputedStyle(el).fontSize);
    if (fs < 11 && shown(el)) { tiny++; if (tinySamples.length < 6) tinySamples.push(label(el) + " " + fs + "px"); }
  }
  const dialogs = [...document.querySelectorAll("dialog[open]")].map(d => {
    const r = d.getBoundingClientRect();
    return { id: d.id, w: Math.round(r.width), h: Math.round(r.height), sh: d.scrollHeight, top: Math.round(r.top),
      bottom: Math.round(r.bottom), over: r.bottom > H + 1 || r.top < -1 || r.right > W + 1 };
  });
  return {
    W, H, docSideways: document.documentElement.scrollWidth > W + 1 ? document.documentElement.scrollWidth : 0,
    small: small.slice(0, 60), smallCount: small.length, offEdge, sideways: sideways.slice(0, 20), clipped: clipped.slice(0, 25),
    clippedCount: clipped.length, tiny, tinySamples, dialogs
  };
}

// Controls that appear only on hover. Every :hover rule that raises opacity, visibility or display, turned back into
// its resting selector and asked of the page: an element that exists, has a box, and is invisible at rest on a
// phone is an affordance a thumb never finds.
function hoverOnly() {
  const out = [];
  const walk = rules => {
    for (const r of rules) {
      if (r.cssRules && !r.selectorText) {
        if (r.media && !matchMedia(r.media.mediaText).matches) continue;
        walk(r.cssRules); continue;
      }
      if (!r.selectorText || !/:hover/.test(r.selectorText)) continue;
      const st = r.style;
      const reveals = (st.opacity && +st.opacity > 0) || st.visibility === "visible" || (st.display && st.display !== "none");
      if (!reveals) continue;
      for (const sel of r.selectorText.split(",")) {
        if (!/:hover/.test(sel)) continue;
        const rest = sel.replace(/:hover/g, "").replace(/:focus-within|:focus-visible|:focus/g, "").trim();
        let els = [];
        try { els = [...document.querySelectorAll(rest)]; } catch (e) { continue; }
        const hidden = els.filter(el => {
          const cs = getComputedStyle(el);
          const par = el.parentElement && el.parentElement.getBoundingClientRect();
          return par && par.width > 0 && (+cs.opacity === 0 || cs.visibility === "hidden" || cs.display === "none") &&
            getComputedStyle(el.parentElement).display !== "none";
        });
        if (hidden.length) out.push({ sel: sel.trim(), n: hidden.length, sample: (hidden[0].getAttribute("aria-label") || hidden[0].textContent || "").trim().slice(0, 30) });
      }
    }
  };
  for (const sh of document.styleSheets) { try { walk(sh.cssRules); } catch (e) {} }
  return out;
}

// ── the driver ────────────────────────────────────────────────────────────────────────────────────────────────
const results = {};
let vpNow = null;
async function shoot(p, name, scope, note) {
  const key = name;
  const w = vpNow.width;
  await p.waitForTimeout(350);
  let data;
  try { data = await p.evaluate(scan, scope || ""); } catch (e) { data = { error: String(e) }; }
  if (note) data.note = note;
  results[key] = results[key] || {};
  results[key][w] = data;
  fs.mkdirSync(SHOTS, { recursive: true });
  await p.screenshot({ path: path.join(SHOTS, name + "-" + w + ".jpg"), type: "jpeg", quality: 80 });
  console.log("shot " + name + " " + w + (data.error ? " ERROR " + data.error : " small=" + data.smallCount + " off=" + data.offEdge.length +
    " clip=" + data.clippedCount + (data.docSideways ? " SIDEWAYS=" + data.docSideways : "")));
}
function record(name, obj) { results[name] = results[name] || {}; results[name][vpNow.width] = Object.assign(results[name][vpNow.width] || {}, obj); }

async function newPage(browser, vp, opts) {
  const ctx = await browser.newContext({ viewport: vp, hasTouch: true, isMobile: true, deviceScaleFactor: 1 });
  await ctx.addInitScript(() => {
    localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
    localStorage.setItem("atrium.walked", "1");
  });
  await ctx.addInitScript(fakeSock);
  const p = await ctx.newPage();
  p.__errors = [];
  p.on("pageerror", e => p.__errors.push(String(e)));
  return { ctx, p };
}
// A real touch at the centre of the first match, scrolled into view first. page.tap waits for the element to be
// stable, and a row whose clock repaints every second never is.
async function touch(p, sel) {
  const at = await p.evaluate(s => {
    const e = document.querySelector(s);
    if (!e) return { missing: s, rows: [...document.querySelectorAll("[data-id]")].slice(0, 12).map(x => x.className.split(" ")[0] + ":" + x.dataset.id) };
    e.scrollIntoView({ block: "center" });
    const r = e.getBoundingClientRect();
    return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
  }, sel);
  if (at.missing) throw new Error("no " + sel + " among " + at.rows.join(" "));
  await p.touchscreen.tap(at.x, at.y);
}
// The stack-all step toggles the show pills, which can leave the ready card filtered out. Tap pills until it is back.
async function showAsk(p) {
  for (let i = 0; i < 8; i++) {
    if (await p.$('#stack-list .stackrow[data-id="ask1"]')) return;
    await p.evaluate(i => { const b = [...document.querySelectorAll("#stack-seg button")]; const r = b.find(x => /^ready/.test(x.textContent.trim()));
      (i === 0 && r ? r : b[i % b.length]).click(); }, i);
    await p.waitForTimeout(300);
  }
}
async function step(name, fn) {
  if (ONLY && !ONLY.has(name)) return;
  try { await fn(); } catch (e) { console.log("STEP FAILED " + name + " " + vpNow.width + ": " + (e.message || e).split("\n")[0]); record(name, { stepError: String(e.message || e).split("\n")[0] }); }
}
// With the soft keyboard up: the viewport shortened, the field focused. Does the field stay on screen, and can the
// dialog's own buttons still be reached?
async function keyboard(p, name, fieldSel, actionSel) {
  const full = vpNow;
  await p.tap(fieldSel).catch(() => p.focus(fieldSel));
  await p.setViewportSize({ width: full.width, height: Math.round(full.height * (1 - KB)) });
  // A phone scrolls the focused field into view when its keyboard comes up. A shortened viewport does not, so
  // that is done here, and what is left to measure is whether the action can still be reached with it.
  await p.evaluate(() => document.activeElement && document.activeElement.scrollIntoView &&
    document.activeElement.scrollIntoView({ block: "nearest" }));
  await p.waitForTimeout(400);
  const kb = await p.evaluate(([f, a]) => {
    const H = innerHeight;
    const box = s => { const e = s && document.querySelector(s); if (!e) return null; const r = e.getBoundingClientRect(); return { top: Math.round(r.top), bottom: Math.round(r.bottom), vis: r.bottom > 0 && r.top < H && r.height > 0 }; };
    return { H, field: box(f), action: box(a), focused: document.activeElement && (document.activeElement.id || document.activeElement.tagName) };
  }, [fieldSel, actionSel || ""]);
  await shoot(p, name + "-kb", "", "keyboard up: " + JSON.stringify(kb));
  record(name + "-kb", { kb });
  await p.setViewportSize(full);
  await p.evaluate(() => document.activeElement && document.activeElement.blur && document.activeElement.blur());
}

async function board(browser, vp) {
  hub = false;
  const { ctx, p } = await newPage(browser, vp);
  await p.goto(BASE + "/", { waitUntil: "domcontentloaded" });
  await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
  await p.waitForTimeout(800);
  results.hoverOnly = results.hoverOnly || {};
  results.hoverOnly[vp.width] = await p.evaluate(hoverOnly);

  await step("stack", async () => {
    await shoot(p, "stack");
    await p.evaluate(() => { const s = document.querySelector('#stack-seg button:not(.on)'); document.querySelectorAll('#stack-seg button:not(.on)').forEach(b => b.click()); });
    await p.waitForTimeout(300);
    await shoot(p, "stack-all");
    await p.evaluate(() => window.scrollTo(0, 99999) || (document.querySelector("main").scrollTop = 99999));
    await shoot(p, "stack-all-scrolled");
    await p.evaluate(() => { window.scrollTo(0, 0); document.querySelector("main").scrollTop = 0; });
    await keyboard(p, "stack-search", "#stack-q", "#stack-list .stackrow");
  });
  await step("header-open", async () => {
    await p.tap("#hdr-toggle");
    await shoot(p, "header-open");
    await p.tap("#hdr-toggle");
  });
  await step("card-menu", async () => {
    // The row repaints every second (its wait clock), which Playwright reads as never stable, so the tap is forced.
    await p.evaluate(() => { window.scrollTo(0, 0); document.querySelector("main").scrollTop = 0; });
    await showAsk(p);
    await touch(p, '#stack-list .stackrow[data-id="ask1"] .who b');
    await p.waitForSelector("#cardmenu.on", { timeout: 5000 });
    await p.waitForTimeout(400);
    const m = await p.evaluate(() => {
      const e = document.getElementById("cardmenu"), r = e.getBoundingClientRect();
      const items = [...e.querySelectorAll(":scope > button")].map(b => Math.round(b.getBoundingClientRect().height));
      return { top: Math.round(r.top), bottom: Math.round(r.bottom), left: Math.round(r.left), right: Math.round(r.right),
        sh: e.scrollHeight, ch: e.clientHeight, H: innerHeight, items: items.length, itemH: [Math.min(...items), Math.max(...items)],
        flyouts: [...e.querySelectorAll(":scope > button")].filter(b => b.querySelector(".sub")).map(b => b.firstChild.textContent.trim()) };
    });
    await shoot(p, "card-menu", "#cardmenu", "menu " + JSON.stringify(m));
    record("card-menu", { menu: m });
    // A row with a flyout opens it on pointerenter and closes it on pointerleave after a grace period. A tap is
    // both, so ask whether the flyout is still open a moment after the finger lifts.
    const fly = await p.$("#cardmenu > button:has(.sub)");
    if (fly) {
      await fly.tap({ force: true });
      const at = [150, 900];
      const open = [];
      for (const ms of at) { await p.waitForTimeout(ms - (open.length ? at[0] : 0)); open.push(await p.evaluate(() => !!document.querySelector("#cardmenu button.subopen"))); }
      record("card-menu", { flyoutAfterTap: { label: m.flyouts[0], at, open, menuStillOn: await p.evaluate(() => document.getElementById("cardmenu").classList.contains("on")) } });
      await shoot(p, "card-menu-flyout", "#cardmenu");
    }
    await p.evaluate(() => closeCardMenu());
  });
  await step("questions", async () => {
    // Where the chip is drawn at all, and what a TAP on it does: the question text lives only in its tooltip.
    await showAsk(p);
    const where = await p.evaluate(() => [...document.querySelectorAll(".chip.questions")].map(c => {
      const r = c.getBoundingClientRect();
      return { in: (c.closest("[id]") || {}).id, card: c.dataset.id, w: Math.round(r.width), h: Math.round(r.height) };
    }));
    const row = await p.evaluate(() => { const r = document.querySelector('#stack-list .stackrow[data-id="ask1"] .chips'); return r ? r.innerHTML.replace(/\s+/g, " ").slice(0, 400) : null; });
    record("questions", { where, stackChips: row });
    const q = await p.$('.chip.questions[data-id="ask1"]');
    if (q && await q.isVisible()) {
      const before = posts.length;
      await q.tap({ force: true });
      await p.waitForTimeout(500);
      record("questions", { tapPosted: posts.slice(before) });
      await shoot(p, "questions-chip");
    }
  });
  await step("detail", async () => {
    await p.evaluate(() => openTask("ask1"));
    await p.waitForSelector("#detail[open]", { timeout: 5000 });
    await shoot(p, "detail");
    await p.evaluate(() => { const d = document.querySelector("#detail .dlg-body") || document.getElementById("detail"); d.scrollTop = 99999; });
    await shoot(p, "detail-scrolled");
    await p.evaluate(() => document.getElementById("detail").close());
  });
  await step("board", async () => {
    await p.tap('nav .tab[data-view="board"]');
    await p.waitForTimeout(500);
    await shoot(p, "board");
    await p.evaluate(() => { document.querySelector("main").scrollTop = 1200; window.scrollTo(0, 1200); });
    await shoot(p, "board-scrolled");
    await p.evaluate(() => { document.querySelector("main").scrollTop = 0; window.scrollTo(0, 0); });
  });
  await step("perms", async () => {
    await p.tap('nav .tab[data-view="perms"]');
    await p.waitForSelector("#perms-list .row.perm", { timeout: 5000 });
    await shoot(p, "perms");
    await p.evaluate(() => { document.querySelector("#perms-list .row.perm:nth-child(2)").scrollIntoView(); });
    await shoot(p, "perms-diff");
    await p.evaluate(() => { document.getElementById("rules-d").open = true; document.getElementById("hist-d").open = true; document.getElementById("rules-d").scrollIntoView(); });
    await shoot(p, "perms-rules");
    await p.evaluate(() => { window.scrollTo(0, 0); document.querySelector("main").scrollTop = 0; });
    await keyboard(p, "perms-cmd", "#perms-list .row.perm textarea.cmd", '#perms-list .row.perm [data-do="approve"]');
    // block with a reason: the block button asks why
    const blk = await p.$('#perms-list .row.perm [data-do="block"]');
    if (blk) { await blk.tap(); await p.waitForTimeout(400); await shoot(p, "perms-block"); await p.keyboard.press("Escape"); }
  });
  await step("terms-list", async () => {
    await p.tap('nav .tab[data-view="terms"]');
    await p.waitForTimeout(600);
    await shoot(p, "terms");
  });
  await step("terminal", async () => {
    await p.evaluate(() => attachTask("ask1"));
    await p.waitForFunction(() => typeof termSock !== "undefined" && termSock && termSock.readyState === 1, null, { timeout: 8000 });
    await p.waitForTimeout(900);
    await shoot(p, "terminal");
    await p.tap("#t-tray-handle").catch(() => p.evaluate(() => phoneTraySet(true)));
    await p.waitForTimeout(400);
    await shoot(p, "terminal-tray");
    const pick = await p.$("#t-pick");
    if (pick && await pick.isVisible()) { await pick.tap(); await p.waitForTimeout(400); await shoot(p, "terminal-picker"); await pick.tap().catch(() => {}); }
    await p.evaluate(() => phoneTraySet(false)).catch(() => {});
    await p.waitForTimeout(300);
    const perm = await p.evaluate(() => { const e = document.getElementById("term-perm"); return e && !e.hidden; });
    record("terminal", { termPermShown: perm });
    if (perm) {
      // The permission strip over an attached terminal: the one place a phone answers without leaving the session.
      await p.evaluate(() => document.getElementById("term-perm").scrollIntoView({ block: "center" }));
      const box = await p.evaluate(() => {
        const e = document.getElementById("term-perm"), r = e.getBoundingClientRect();
        return { top: Math.round(r.top), bottom: Math.round(r.bottom), h: Math.round(r.height), H: innerHeight,
          buttons: [...e.querySelectorAll("button")].map(b => { const q = b.getBoundingClientRect(); return b.textContent.trim() + " " + Math.round(q.width) + "x" + Math.round(q.height) + " @" + Math.round(q.top); }) };
      });
      record("terminal-perm", { box });
      await shoot(p, "terminal-perm", "#term-perm", "strip " + JSON.stringify(box));
      // block once asks why in a dialog: the reason field with the keyboard up
      await p.locator('#term-perm button.no').tap({ force: true });
      await p.waitForSelector("#ask[open] #ask-input", { timeout: 4000 });
      await shoot(p, "terminal-perm-block");
      await keyboard(p, "terminal-perm-block", "#ask-input", "#ask-actions button:last-child");
      await p.keyboard.press("Escape");
      await p.evaluate(() => document.querySelectorAll("dialog[open]").forEach(d => d.close()));
    }
    // the compose box and the keyboard
    const comp = await p.$("#t-compose textarea");
    if (comp && await comp.isVisible()) await keyboard(p, "terminal-compose", "#t-compose textarea", "#t-keys");
    else {
      const xt = await p.$("#t-screen .xterm-helper-textarea");
      if (xt) await keyboard(p, "terminal-keys", "#t-screen .xterm-helper-textarea", "#t-keys");
    }
    await p.evaluate(() => toggleTermFull()).catch(() => {});
    await p.waitForTimeout(400);
    await shoot(p, "terminal-full");
    await p.evaluate(() => toggleTermFull()).catch(() => {});
    // the files panel and the cog, both from the tray
    await p.evaluate(() => { phoneTraySet(true); openTermFiles(); });
    await p.waitForTimeout(500);
    await shoot(p, "terminal-files");
    // A file row's actions (edit here, open there, get) show on :hover or :focus-within only. Tap the name, which
    // takes no focus, and ask whether they became visible.
    const facts = async () => p.evaluate(() => { const f = document.querySelector("#t-files-list .frow:not(.dir) .facts");
      return f ? getComputedStyle(f).visibility : "none"; });
    const atRest = await facts();
    const nm = await p.$("#t-files-list .frow:not(.dir) .fname");
    if (nm) { await touch(p, "#t-files-list .frow:not(.dir) .fname"); await p.waitForTimeout(300); }
    record("terminal-files", { factsAtRest: atRest, factsAfterNameTap: await facts() });
    if (nm) await shoot(p, "terminal-files-tapped");
    await p.evaluate(() => { openTermFiles(); });
    await p.evaluate(() => termSettings({ currentTarget: document.getElementById("t-cog"), target: document.getElementById("t-cog"), stopPropagation() {}, preventDefault() {} })).catch(() => {});
    await p.waitForTimeout(400);
    await shoot(p, "terminal-cog");
    await p.keyboard.press("Escape");
    await p.evaluate(() => phoneTraySet(false)).catch(() => {});
  });
  await step("history", async () => {
    await p.evaluate(() => switchView("history"));
    await p.waitForTimeout(600);
    await shoot(p, "history");
    await keyboard(p, "history-search", "#h-q", "#history-list");
  });
  await step("usage", async () => {
    await p.evaluate(() => switchView("usage"));
    await p.waitForTimeout(1200);
    await shoot(p, "usage");
    await p.evaluate(() => { document.querySelector("main").scrollTop = 800; window.scrollTo(0, 800); });
    await shoot(p, "usage-scrolled");
  });
  await step("runners", async () => {
    await p.evaluate(() => switchView("runners"));
    await p.waitForTimeout(600);
    const panes = await p.$$eval("#runners .pane-nav button", b => b.map(x => x.textContent.trim()));
    record("runners", { panes });
    for (let i = 0; i < panes.length; i++) {
      await p.evaluate(i => document.querySelectorAll("#runners .pane-nav button")[i].click(), i);
      await p.waitForTimeout(350);
      await shoot(p, "runners-" + panes[i].replace(/[^a-z0-9]+/gi, "-").toLowerCase());
    }
  });
  await step("runner-edit", async () => {
    await p.evaluate(() => editHarness(null));
    await p.waitForSelector("#harness[open]", { timeout: 4000 });
    await shoot(p, "runner-edit");
    const f = await p.$("#harness[open] input[type=text]");
    if (f) await keyboard(p, "runner-edit", "#harness[open] input[type=text]", "#harness[open] button.go");
    await p.evaluate(() => document.getElementById("harness").close());
  });
  await step("launch", async () => {
    await p.evaluate(() => openLaunch());
    await p.waitForSelector("#launch[open]", { timeout: 5000 });
    await shoot(p, "launch");
    await p.evaluate(() => { const b = document.querySelector("#launch .dlg-body"); if (b) b.scrollTop = 99999; });
    await shoot(p, "launch-scrolled");
    await p.evaluate(() => { const b = document.querySelector("#launch .dlg-body"); if (b) b.scrollTop = 0; });
    await keyboard(p, "launch-cwd", "#l-cwd", "#l-go");
    // The first instruction sits under the "more" fold.
    await p.evaluate(() => { const m = document.getElementById("l-more"); if (m) m.open = true; });
    await p.waitForTimeout(300);
    const pr = await p.$("#l-prompt");
    if (pr && await pr.isVisible()) await keyboard(p, "launch-prompt", "#l-prompt", "#l-go");
    else record("launch-prompt-kb", { stepError: "#l-prompt not visible with #l-more open" });
    await p.evaluate(() => openBrowse()).catch(() => {});
    await p.waitForTimeout(500);
    if (await p.$("#browse[open]")) { await shoot(p, "launch-browse"); await p.evaluate(() => document.getElementById("browse").close()); }
    await p.evaluate(() => document.getElementById("launch").close());
  });
  await step("settings", async () => {
    await p.tap("#hdr-toggle");
    await p.tap("#gear");
    await p.waitForSelector("#settings[open]", { timeout: 5000 });
    await p.tap("#hdr-toggle").catch(() => {});
    const panes = await p.$$eval("#settings .pane-nav button", b => b.map(x => x.textContent.trim()));
    record("settings", { panes });
    await shoot(p, "settings");
    for (let i = 0; i < panes.length; i++) {
      await p.evaluate(i => document.querySelectorAll("#settings .pane-nav button")[i].click(), i);
      await p.waitForTimeout(350);
      await shoot(p, "settings-" + panes[i].replace(/[^a-z0-9]+/gi, "-").toLowerCase());
    }
    // With the keyboard: the first visible text or number field on any pane, and whether the dialog's close is
    // still on screen.
    let done = false;
    for (let i = 0; i < panes.length && !done; i++) {
      await p.evaluate(i => document.querySelectorAll("#settings .pane-nav button")[i].click(), i);
      await p.waitForTimeout(250);
      const sel = await p.evaluate(() => {
        const f = [...document.querySelectorAll("#settings input[type=text], #settings input[type=number], #settings textarea")]
          .find(e => e.getBoundingClientRect().height > 0);
        if (!f) return "";
        if (!f.id) f.id = "u001-settings-field";
        return "#" + f.id;
      });
      if (sel) { record("settings", { kbPane: panes[i], kbField: sel }); await keyboard(p, "settings", sel, "#settings .dlg-foot button, #settings .dlg-head button"); done = true; }
    }
    await p.evaluate(() => document.getElementById("settings").close());
  });
  await step("toastlog", async () => {
    await p.evaluate(() => openToastLog());
    await p.waitForTimeout(400);
    await shoot(p, "toastlog");
    await p.keyboard.press("Escape");
    await p.evaluate(() => { const d = document.getElementById("toastlog"); if (d.open) d.close(); });
  });
  await step("switcher", async () => {
    await p.evaluate(() => openSwitcher());
    await p.waitForTimeout(400);
    await shoot(p, "switcher");
    await p.evaluate(() => { const d = document.getElementById("switcher"); if (d.open) d.close(); });
  });
  record("board-errors", { errors: p.__errors.slice(0, 10) });
  await ctx.close();
}

async function hubBoard(browser, vp) {
  hub = true;
  const { ctx, p } = await newPage(browser, vp);
  await p.goto(BASE + "/", { waitUntil: "domcontentloaded" });
  await p.waitForSelector("#stack-list .stackrow", { timeout: 15000 });
  await p.waitForTimeout(1200);
  await step("hub-stack", async () => { await shoot(p, "hub-stack"); });
  await step("rooms-menu", async () => {
    await p.tap("#hdr-toggle");
    await p.waitForTimeout(300);
    await shoot(p, "hub-header-open");
    await p.evaluate(() => openRooms());
    await p.waitForTimeout(700);
    await shoot(p, "rooms-menu");
    await p.evaluate(() => { const m = document.getElementById("rooms-menu"); if (m) m.hidden = true; });
    await p.tap("#hdr-toggle").catch(() => {});
  });
  await step("audit", async () => {
    await p.evaluate(() => switchView("audit"));
    await p.waitForTimeout(800);
    await shoot(p, "audit");
  });
  record("hub-errors", { errors: p.__errors.slice(0, 10) });
  await ctx.close();
  // The rooms dashboard as its own demo, which draws tiles with sparklines.
  const d = await newPage(browser, vp);
  await d.p.goto(BASE + "/?demo=rooms", { waitUntil: "domcontentloaded" });
  await step("rooms-demo", async () => {
    await d.p.waitForFunction(() => typeof roomsDemoLive !== "undefined" && roomsDemoLive && roomsDemoTicks > 0, null, { timeout: 15000 });
    await d.p.evaluate(() => openRooms());
    await d.p.waitForTimeout(2500);
    await shoot(d.p, "rooms-dash");
    await d.p.evaluate(() => { const m = document.getElementById("rooms-menu"); const s = m && (m.querySelector(".rd-scroll") || m); if (s) s.scrollTop = 99999; });
    await shoot(d.p, "rooms-dash-scrolled");
  });
  await d.ctx.close();
  hub = false;
}

async function popout(browser, vp) {
  const { ctx, p } = await newPage(browser, vp);
  await p.goto(BASE + "/#term=ask1", { waitUntil: "domcontentloaded" });
  await step("popout", async () => {
    await p.waitForFunction(() => typeof termSock !== "undefined" && termSock && termSock.readyState === 1, null, { timeout: 10000 });
    await p.waitForTimeout(1000);
    await shoot(p, "popout");
    await p.tap("#t-tray-handle").catch(() => p.evaluate(() => phoneTraySet(true)));
    await p.waitForTimeout(400);
    await shoot(p, "popout-tray");
  });
  await ctx.close();
}

async function mPage(browser, vp) {
  hub = false;
  const { ctx, p } = await newPage(browser, vp);
  await p.goto(BASE + "/m/", { waitUntil: "domcontentloaded" });
  await step("m-home", async () => {
    await p.waitForSelector("#m-list .row", { timeout: 10000 });
    await p.waitForTimeout(900);
    await shoot(p, "m-home");
    await p.tap("#m-seg-all");
    await p.waitForTimeout(500);
    await shoot(p, "m-all");
    await p.tap("#m-seg-needs");
  });
  await step("m-perm-row", async () => {
    await p.waitForSelector('#m-list .row[data-id="perm1"]', { timeout: 10000 });
    const row = await p.$('#m-list .row[data-id="perm1"]');
    if (row) { await row.tap(); await p.waitForSelector("#m-card.on", { timeout: 5000 }); await p.waitForTimeout(700); await shoot(p, "m-card-perm"); }
    // "Deny with a reason" opens a textarea under the buttons. Not the plain Deny, which sends at once.
    const why = await p.$("#m-card .mp-btn.why");
    if (why) {
      await why.tap(); await p.waitForTimeout(400);
      await shoot(p, "m-card-perm-deny");
      await keyboard(p, "m-card-perm-deny", "#m-card .mp-reason-box", "#m-card .mp-btn.deny.go");
    }
    await p.goBack().catch(() => {});
    await p.waitForTimeout(500);
  });
  await step("m-card", async () => {
    await p.evaluate(() => { if (location.hash) history.replaceState(null, "", location.pathname); });
    await p.goto(BASE + "/m/", { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#m-list .row", { timeout: 10000 });
    await p.waitForTimeout(600);
    await touch(p, '#m-list .row[data-id="ask1"]');
    await p.waitForSelector("#m-card.on", { timeout: 5000 });
    await p.waitForTimeout(900);
    await shoot(p, "m-card");
    await p.evaluate(() => { const s = document.getElementById("m-card-scroll"); if (s) s.scrollTop = 99999; });
    await shoot(p, "m-card-scrolled");
    const ta = await p.$("#m-card .mc-box");
    if (ta) await keyboard(p, "m-compose", "#m-card .mc-box", "#m-card .mc-send");
    await p.goBack().catch(() => {});
  });
  record("m-errors", { errors: p.__errors.slice(0, 10) });
  await ctx.close();
}

let BASE = "";
async function main() {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  BASE = "http://127.0.0.1:" + server.address().port;
  const browser = await chromium.launch();
  try {
    // hoverOnly() found nothing on a phone. Asked once of a desktop context too, so an empty answer is known to
    // mean "no hover-only reveal under pointer: coarse" rather than "the probe never matches anything".
    if (!ONLY || ONLY.has("hover-desktop")) {
      const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
      await ctx.addInitScript(() => { localStorage.setItem("atrium.walked", "1"); });
      const p = await ctx.newPage();
      await p.goto(BASE + "/", { waitUntil: "domcontentloaded" });
      await p.waitForSelector("#stack-list .stackrow, #board .card", { timeout: 15000 }).catch(() => {});
      await p.waitForTimeout(800);
      results.hoverOnlyDesktop = await p.evaluate(hoverOnly);
      await ctx.close();
    }
    for (const vp of VIEWS) {
      vpNow = vp;
      await board(browser, vp);
      await hubBoard(browser, vp);
      await popout(browser, vp);
      await mPage(browser, vp);
    }
  } finally {
    await browser.close();
    streams.forEach(s => { try { s.destroy(); } catch (e) {} });
    server.close();
  }
  results.unknownEndpoints = [...unknown];
  if (REPORT) {
    // A partial run (U001_ONLY or U001_WIDTH) folds into the report already there rather than replacing it.
    let out = results;
    if ((ONLY || process.env.U001_WIDTH) && fs.existsSync(REPORT)) {
      out = JSON.parse(fs.readFileSync(REPORT, "utf8"));
      for (const [k, v] of Object.entries(results)) {
        out[k] = v && typeof v === "object" && !Array.isArray(v) && out[k] && typeof out[k] === "object" && !Array.isArray(out[k])
          ? Object.assign(out[k], v) : v;
      }
    }
    fs.writeFileSync(REPORT, JSON.stringify(out, null, 1));
  }
  console.log("done. unknown endpoints: " + [...unknown].join(", "));
}
main().catch(e => { console.error(e); process.exit(1); });
