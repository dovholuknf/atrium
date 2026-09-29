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
  if (/^\/v1\/tasks\/[^/]+\/review$/.test(url)) return json(res, { events: EVENTS });
  if (/^\/v1\/tasks\/[^/]+\/asks$/.test(url)) return json(res, { asks: [] });
  if (/^\/v1\/tasks\/[^/]+$/.test(url)) return json(res, CARD);
  if (url === "/v1/harnesses") return json(res, { harnesses: [HARNESS] });
  if (url === "/v1/fixtures") return json(res, { fixtures: [FIXTURE] });
  if (url === "/v1/hooks") return json(res, { missing: 0, hooks: [] });
  if (url === "/v1/sources") return json(res, { sources: [] });
  if (url === "/v1/recognisers") return json(res, { recognisers: [] });
  if (url === "/v1/actions") return json(res, { actions: [{ id: "a1", label: "run the tests", prompt: "run the tests", exit: false }] });
  if (url === "/v1/providers") return json(res, { providers: [] });
  if (url === "/v1/dispatch") return json(res, { dispatches: [] });
  if (url === "/v1/history") return json(res, { tasks: [], total: 0 });
  if (url === "/v1/waiting") return json(res, { tasks: [] });
  if (url === "/v1/permissions") return json(res, { permissions: [] });
  if (url === "/v1/shares") return json(res, { shares: [] });
  if (url === "/v1/rooms") return json(res, { rooms: [] });
  if (url === "/v1/themes") return json(res, { themes: [] });
  if (url === "/v1/health") return json(res, { build: "shots", settling: false, halted: false });
  if (url === "/v1/settings") return json(res, { board_skin: "harbour", board_skins: SKINS, global_auto: false });
  res.writeHead(404); res.end("");
});

// Each shot opens one dialog on a freshly loaded board. `open` runs in the page.
const SHOTS = {
  detail: () => openTask("t1"),
  harness: () => editHarness("claude"),
  fixture: () => editFixture("fx1"),
};

(async () => {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const base = `http://127.0.0.1:${server.address().port}/`;
  const browser = await chromium.launch();
  const names = Object.keys(SHOTS).filter(n => !only || only.includes(n));
  for (const skin of skins) {
    for (const name of names) {
      const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
      const p = await ctx.newPage();
      const errs = [];
      p.on("pageerror", e => errs.push(String(e)));
      if (process.env.SHOOT_DEBUG) await p.addInitScript(() => { window.SHOOT_DEBUG = true; });
      await p.goto(base, { waitUntil: "domcontentloaded" });
      await p.waitForTimeout(900);
      await p.evaluate(s => applySkin(s), skin);
      await p.evaluate(`(${SHOTS[name].toString()})()`).catch(e => errs.push(String(e)));
      await p.waitForTimeout(600);
      // A dialog focuses its first control, and a focused button shows its tooltip over the shot.
      await p.evaluate(() => { document.activeElement && document.activeElement.blur(); document.getElementById("tip").classList.remove("on"); });
      await p.mouse.move(2, 998);
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
