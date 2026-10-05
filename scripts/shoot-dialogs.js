#!/usr/bin/env node
// Screenshots of the board's dialogs, headless, against a mocked daemon.
//
//   node scripts/shoot-dialogs.js <out dir> [skin,skin] [shot,shot]
//
// No atrium process: the page is `wholeBoard()` and every /v1 read is answered
// here with a card that fills every field, so a dialog is drawn the way it looks
// in use rather than empty. One PNG per dialog per skin, named skin-dialog.png.

const http = require("http");
const fs = require("fs");
const path = require("path");
const { wholeBoard, web } = require("./board-source.js");

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) { ({ chromium } = require("playwright")); }

const out = process.argv[2] || "shots";
const skins = (process.argv[3] || "harbour,noir").split(",");
const only = process.argv[4] ? process.argv[4].split(",") : null;
fs.mkdirSync(out, { recursive: true });

const CARD = {
  id: "t1", status: "needs-input", display_title: "sa56 every dialog sleek", runner: "claude",
  rank: 1, worktree: "D:/worktrees/claude/atrium/sleek-dialogs", why: "clint wants every dialog to look designed",
  idle_seconds: 340, wait_seconds: 120, pid: 48064, supervised: true, resume_id: "sess-1",
  created_at: "2026-09-28T09:12:00Z", last_activity_at: "2026-09-28T12:00:00Z",
  tags: ["design", "board"], offline: false, pinned: true, auto_approve: false,
  ask: "Card details first, or the rooms edit-agents dialog first?", asks_open: 1,
  recap: "Restyled the shared dialog chrome and card details. Checks pass.", recap_at: "2026-09-28T11:40:00Z",
  sound: "", icon: "✦"
};
const EVENTS = [
  { kind: "status-changed", at: "2026-09-28T11:02:10Z", payload: { from: "running", to: "needs-input" } },
  { kind: "perm-requested", at: "2026-09-28T10:58:02Z", payload: { id: "p1", tool: "Bash", command: "bash scripts/check-board.sh" } },
  { kind: "perm-decided", at: "2026-09-28T10:58:03Z", payload: { id: "p1", decision: "approve", by: "auto" } },
  { kind: "perm-requested", at: "2026-09-28T10:41:40Z", payload: { id: "p2", tool: "Bash", command: "git push --force origin main" } },
  { kind: "perm-decided", at: "2026-09-28T10:41:41Z", payload: { id: "p2", decision: "deny", reason: "never rule: force push", by: "rule" } },
  { kind: "message", at: "2026-09-28T10:30:00Z", payload: { text: "restyle card details first" } },
  { kind: "perm-requested", at: "2026-09-28T10:22:12Z", payload: { id: "p3", tool: "Edit", command: "internal/api/web/css/dialogs.css" } },
  { kind: "perm-decided", at: "2026-09-28T10:22:14Z", payload: { id: "p3", decision: "approve", by: "you" } },
];
const HARNESS = { id: "claude", label: "claude", cmd: "claude", args: ["--dangerously-skip-permissions"],
  resume_args: ["--resume", "{id}"], prompt_args: ["{prompt}"], model_args: ["--model", "{model}"],
  effort_args: [], cwd: "", env: { CLAUDE_CODE_NO_FLICKER: "1" }, launch_mode: "pty", enabled: true,
  found: "C:/Users/claude/.local/bin/claude.exe", rules_source: "", package: "", notes: "the main one" };
const FIXTURE = { id: "fx1", label: "notes", harness: "claude", cwd: "D:/git/notes", enabled: true,
  resume: true, resume_mode: "latest", sort: 0 };
const SKINS = ["harbour", "website", "graphite", "glacier", "abyss", "oxide", "moss", "plum", "ember", "vapor",
  "sandstone", "noir", "slate", "cobalt", "dusk", "fern", "clay", "mint", "frost"];

const HTML = wholeBoard();
const json = (res, o) => { res.writeHead(200, { "Content-Type": "application/json" }); res.end(JSON.stringify(o)); };
const server = http.createServer((req, res) => {
  const url = req.url.split("?")[0];
  if (url === "/" || url === "/index.html") { res.writeHead(200, { "Content-Type": "text/html" }); res.end(HTML); return; }
  if (url.startsWith("/vendor/") && !url.includes("..")) {
    fs.readFile(path.join(web, url), (err, body) => {
      if (err) { res.writeHead(404); res.end(""); return; }
      res.writeHead(200, { "Content-Type": url.endsWith(".css") ? "text/css" : "application/javascript" });
      res.end(body);
    });
    return;
  }
  if (url.startsWith("/v1/events")) {
    res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" });
    res.write(": open\n\n");
    return;
  }
  if (url === "/v1/tasks") return json(res, { tasks: [CARD] });
  if (/^\/v1\/tasks\/[^/]+\/events$/.test(url)) return json(res, { events: EVENTS });
  if (/^\/v1\/tasks\/[^/]+\/usage$/.test(url)) return json(res, { context_now: 90000, model: "claude-opus-5-5",
    totals: { rows: 7, input: 4100, output: 18200, cache_write_5m: 0, cache_write_1h: 96000, cache_read: 512000, cost: 1.84 } });
  if (/^\/v1\/tasks\/[^/]+\/messages$/.test(url)) return json(res, { messages: [] });
  if (/^\/v1\/tasks\/[^/]+\/review$/.test(url)) return json(res, { total: 14, unattended: 3, blocked: 1, groups: [
    { tool: "Bash", count: 9, unattended: 3, blocked: 1, entries: [
      { decision: "approve", repeats: 4, command: "bash scripts/check-board.sh", unattended: true },
      { decision: "approve", repeats: 1, command: "git status", by: "you" },
      { decision: "block", repeats: 1, command: "git push --force", by: "you" }] },
    { tool: "Edit", count: 5, unattended: 0, blocked: 0, entries: [
      { decision: "approve", repeats: 5, command: "internal/api/web/css/dialogs.css", by: "you" }] }] });
  if (/^\/v1\/tasks\/[^/]+\/asks$/.test(url)) return json(res, { asks: [] });
  if (/^\/v1\/tasks\/[^/]+$/.test(url)) return json(res, CARD);
  if (url === "/v1/harnesses") return json(res, { harnesses: [HARNESS] });
  if (url === "/v1/fixtures") return json(res, { fixtures: [FIXTURE] });
  if (url === "/v1/hooks") return json(res, { missing: 2, exists: true, path: "C:/Users/claude/.claude/settings.json",
    hooks: [
      { hook: "SessionStart", event: "SessionStart", why: "a card knows which conversation it is", installed: true, found: "atrium hook session-start" },
      { hook: "UserPromptSubmit", event: "UserPromptSubmit", why: "the card says working the moment you send", installed: true, found: "atrium hook prompt" },
      { hook: "PreToolUse", event: "PreToolUse", why: "permissions come to the board instead of the terminal", installed: false },
      { hook: "Stop", event: "Stop", why: "the card says done, and the bell rings", installed: true, stale: true, found: "old/atrium hook stop", want: "atrium hook stop" },
      { hook: "Notification", event: "Notification", why: "a question to you lands on the card", installed: false, optional: true }] });
  if (url === "/v1/sources") return json(res, { sources: [] });
  if (url === "/v1/recognisers") return json(res, { recognisers: [] });
  if (url === "/v1/actions") return json(res, { actions: [{ id: "a1", label: "run the tests", prompt: "run the tests", exit: false }] });
  if (url === "/v1/providers") return json(res, { providers: [{ name: "github", enabled: true, worktrees: true }] });
  if (/^\/v1\/providers\/[^/]+\/repos$/.test(url)) return json(res, { orgs: ["dovholuknf", "openziti"], repos: [
    { org: "dovholuknf", repo: "atrium", path: "D:/git/github/dovholuknf/atrium", present: true },
    { org: "openziti", repo: "ziti", path: "D:/git/github/openziti/ziti", present: true },
    { org: "openziti", repo: "zrok", path: "D:/git/github/openziti/zrok", present: false }] });
  if (/^\/v1\/providers\/[^/]+\/worktrees$/.test(url)) return json(res, { worktrees: [
    { branch: "claude/sleek-dialogs", path: "D:/worktrees/claude/atrium/sleek-dialogs" },
    { branch: "claude/main", path: "D:/worktrees/claude/atrium/main" }] });
  if (url === "/v1/browse") return json(res, { path: "D:/git/github", parent: "D:/git", entries: [
    { name: "dovholuknf", path: "D:/git/github/dovholuknf" },
    { name: "openziti", path: "D:/git/github/openziti" },
    { name: "notes", path: "D:/git/github/notes", repo: true }] });
  if (url === "/v1/dispatch") return json(res, { dispatches: [] });
  if (url === "/v1/history") return json(res, { tasks: [], total: 0 });
  if (url === "/v1/waiting") return json(res, { tasks: [] });
  if (url === "/v1/permissions") return json(res, { permissions: [] });
  if (url === "/v1/shares") return json(res, { shares: [] });
  if (url === "/v1/rooms") return json(res, { rooms: [] });
  if (url === "/v1/rooms/join") return json(res, { address: "http://192.168.1.20:7778", local: "127.0.0.1:7778",
    heartbeat_seconds: 20, stale_seconds: 90, forget_seconds: 86400 });
  if (url === "/v1/themes") return json(res, { themes: [] });
  if (url === "/v1/health") return json(res, { build: "shots", settling: false, halted: false });
  if (url === "/v1/settings") return json(res, { board_skin: "harbour", board_skins: SKINS, global_auto: false });
  res.writeHead(404); res.end("");
});

// Each shot opens one dialog on a freshly loaded board. `open` runs in the page.
const SHOTS = {
  detail: () => openTask("t1"),
  harness: async () => { await renderRunners(); await renderFixtures(); await editHarness("claude"); },
  fixture: async () => { await renderRunners(); await renderFixtures(); await editFixture("fx1"); },
  launch: () => openLaunch(),
  "launch-more": async () => { await openLaunch(); document.getElementById("l-more").open = true; },
  pickrepo: () => openPickRepo("l-cwd"),
  pickwhere: async () => { await openPickRepo("l-cwd"); await openWhere("dovholuknf", "atrium", "D:/git/github/dovholuknf/atrium"); },
  browse: () => openBrowse(),
  dispatch: () => openDispatch("lab"),
  // The gear paints the mocked settings, which name harbour, so the skin being shot is put back after.
  settings: async () => {
    document.getElementById("gear").click();
    await new Promise(r => setTimeout(r, 400));
    applySkin(window.shootSkin);
  },
  "settings-notify": async () => {
    document.getElementById("gear").click();
    showSettingsPane("notifications");
    await new Promise(r => setTimeout(r, 400));
    applySkin(window.shootSkin);
  },
  roomcfg: () => openRoomCog(""),
  action: () => editAction("a1"),
  source: () => editSource(""),
  recogniser: () => editRecogniser(""),
  provider: async () => { await openPickRepo("l-cwd"); document.getElementById("pickrepo").close(); editProvider("github"); },
  ask: () => { askUser({ title: "this is holding 1 request",
    body: "<b>sa56 every dialog sleek</b> has an agent waiting on it. Moving it to <b>shelved</b> answers it with block.",
    rememberKey: "shots", buttons: [{ label: "cancel", value: null }, { label: "move it to shelved", value: true, style: "go" }] }); },
  review: async () => { await openTask("t1"); await openReview(); },
  roomjoin: () => openRoomJoin(),
  hooks: () => openHooks(),
  "runner-setup": async () => { await renderRunners(); openRunnerSetup("claude"); },
  steps: () => showHookSteps(),
  theme: () => openThemeEditor(),
  toastlog: () => { logNotification("sa56 needs you", "Card details first, or edit agents?", "t1");
    logNotification("permission", "Bash: bash scripts/check-board.sh"); openToastLog(); },
  switcher: () => openSwitcher(),
  sharedlg: () => {
    SHARE_STEPS = SHARE_STEPS_BY_MODE.public;
    document.getElementById("share-title").textContent = "sharing sa56 every dialog sleek";
    sharePaintSteps(2, false);
    shareButtons([]);
    document.getElementById("sharedlg").showModal();
  },
  "sharedlg-link": () => {
    SHARE_STEPS = SHARE_STEPS_BY_MODE.public;
    document.getElementById("share-title").textContent = "sharing sa56 every dialog sleek";
    document.getElementById("sharedlg").showModal();
    showShareLink({ id: "t1" }, { mode: "public", address: "https://k3v9q2m7x1.share.zrok.io" });
  },
};

(async () => {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const base = `http://127.0.0.1:${server.address().port}/`;
  const browser = await chromium.launch();
  const names = Object.keys(SHOTS).filter(n => !only || only.includes(n));
  for (const skin of skins) {
    for (const name of names) {
      const ctx = await browser.newContext({ viewport: { width: +process.env.SHOOT_W || 1440, height: +process.env.SHOOT_H || 1000 } });
      const p = await ctx.newPage();
      const errs = [];
      p.on("pageerror", e => errs.push(String(e)));
      if (process.env.SHOOT_DEBUG) await p.addInitScript(() => { window.SHOOT_DEBUG = true; });
      await p.goto(base, { waitUntil: "domcontentloaded" });
      await p.waitForTimeout(900);
      await p.evaluate(s => { window.shootSkin = s; applySkin(s); }, skin);
      await p.evaluate(`(${SHOTS[name].toString()})()`).catch(e => errs.push(String(e)));
      await p.waitForTimeout(600);
      // A dialog focuses its first control, and a focused button shows its tooltip over the shot.
      await p.evaluate(() => { document.activeElement && document.activeElement.blur(); document.getElementById("tip").classList.remove("on"); });
      await p.mouse.move(2, (+process.env.SHOOT_H || 1000) - 2);
      await p.waitForTimeout(200);
      const file = path.join(out, `${skin}-${name}.png`);
      await p.screenshot({ path: file });
      // And the far end of a body that scrolls, so a long form is seen whole.
      const scrolled = await p.evaluate(() => {
        const d = [...document.querySelectorAll("dialog[open]")].pop();
        if (!d) return false;
        let moved = false;
        for (const b of [d, ...d.querySelectorAll(".dlg-body, .dlg-body *")]) {
          if (b.scrollHeight > b.clientHeight + 4 && /auto|scroll/.test(getComputedStyle(b).overflowY)) {
            b.scrollTop = b.scrollHeight;
            moved = true;
          }
        }
        if (!moved && window.SHOOT_DEBUG) return [d, d.querySelector(".dlg-body")].map(e => e && [e.scrollHeight, e.clientHeight, getComputedStyle(e).overflowY].join(" ")).join(" | ");
        return moved;
      });
      if (typeof scrolled === "string") console.log("  scroll: " + scrolled);
      if (scrolled) {
        await p.waitForTimeout(200);
        await p.screenshot({ path: path.join(out, `${skin}-${name}-end.png`) });
      }
      console.log(file + (errs.length ? "  errors: " + errs.join(" | ") : ""));
      await ctx.close();
    }
  }
  await browser.close();
  server.close();
})();
