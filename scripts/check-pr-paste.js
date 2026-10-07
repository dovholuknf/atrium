// A pasted pull request opens in one call when launch is pressed, in a real browser with every endpoint mocked.
//
//   NODE_PATH=<dir with playwright> node scripts/check-pr-paste.js [shots-dir]
//
// It serves the concatenated board from board-source.js, the way scripts/audit-phone-u001.js does. No atrium process
// is involved. It checks that launch sends the link to POST /v1/open with what the dialog says (so an edited prompt is
// the one used), that the browser makes no worktree, row, launch or walker call of its own, that the opened card is
// attached, that a second paste with a live card attaches that card and says so, that a refusal reads as the room's
// own sentence in the dialog with nothing started, and that a directory typed over is an ordinary launch.
// With a shots dir it writes before-paste.png and after-refused.png, which is how the dialog is shown.

const http = require("http");
const fs = require("fs");
const path = require("path");
const { wholeBoard } = require("./board-source.js");

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) {
  try { ({ chromium } = require("playwright")); } catch (e2) { console.log("playwright is not installed"); process.exit(0); }
}

const WEB = path.join(__dirname, "..", "internal", "api", "web");
const SHOTS = process.argv[2] || "";
const HTML = wholeBoard();
const TYPES = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".png": "image/png",
  ".svg": "image/svg+xml", ".gif": "image/gif", ".woff2": "font/woff2" };

const PR = "https://github.com/openziti/tlsuv/pull/378";
const RESOLVED = {
  recogniser: "github-pull-request", label: "github pull request", url: PR, kind: "github",
  vars: { host: "github.com", org: "openziti", repo: "tlsuv", num: "378" },
  title: "openziti/tlsuv#378 fix the handshake", tags: ["pull-request", "tlsuv"],
  cwd: "D:/worktrees/github/openziti/tlsuv/pr-378", prompt: "Review the pull request at " + PR,
  repo: "tlsuv", org: "openziti", host: "github.com", branch: "fix-handshake", window: "pull-requests",
  cwd_exists: false,
  problem: "D:/worktrees/github/openziti/tlsuv/pr-378 is not here yet. make the worktree, then start it",
};
const WORKTREE = "D:/worktrees/github/openziti/tlsuv/fix-handshake";

let seen, refuse, created;
const reset = () => {
  seen = { open: [], launch: [], chain: [], tasks: [] };
  refuse = ""; created = true;
};
reset();

const server = http.createServer(async (req, res) => {
  const u = new URL(req.url, "http://x");
  const p = u.pathname;
  const json = (o, code, headers) => {
    res.writeHead(code || 200, Object.assign({ "Content-Type": "application/json" }, headers || {}));
    res.end(JSON.stringify(o));
  };
  if (p === "/" || p === "/index.html") { res.writeHead(200, { "Content-Type": "text/html" }); return res.end(HTML); }
  if ((p.startsWith("/vendor/") || p.startsWith("/css/") || p === "/working.gif") && !p.includes("..")) {
    const f = path.join(WEB, p);
    if (fs.existsSync(f) && fs.statSync(f).isFile()) {
      res.writeHead(200, { "Content-Type": TYPES[path.extname(f)] || "application/octet-stream" });
      return res.end(fs.readFileSync(f));
    }
    res.writeHead(404); return res.end("");
  }
  if (p.startsWith("/v1/events")) {
    res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" });
    return res.write(": open\n\n");
  }
  let body = "";
  for await (const c of req) body += c;
  if (req.method === "POST" && p === "/v1/recognise") return json(RESOLVED);
  if (req.method === "POST" && p === "/v1/open") {
    seen.open.push({ body: JSON.parse(body), room: req.headers["x-atrium-room"] || "" });
    if (refuse) return json({ error: refuse, code: "worktree_failed", step: "worktree" }, 400);
    if (!created) return json({ key: "github.com/openziti/tlsuv/378", card: "old1", pr: "pr1", worktree: WORKTREE, created: false });
    return json({ key: "github.com/openziti/tlsuv/378", card: "t1", pr: "pr1", worktree: WORKTREE, created: true }, 201);
  }
  if (req.method === "POST" && p === "/v1/launch") {
    seen.launch.push({ body: JSON.parse(body), room: req.headers["x-atrium-room"] || "" });
    return json({ id: "t2", status: "running", supervised: false });
  }
  // The chain the browser used to run. The open verb does it on the room now, so none of these may come from here.
  if (req.method === "POST" && (p.endsWith("/pr-worktree") || p === "/v1/prs" || /^\/v1\/prs\/[^/]+\/walker$/.test(p))) {
    seen.chain.push(p);
    return json({ error: "the browser should not call this" }, 500);
  }
  if (req.method === "GET" && (p === "/v1/tasks/t1" || p === "/v1/tasks/old1")) {
    seen.tasks.push(p);
    return json({ id: p.split("/").pop(), status: "running", supervised: false, title: "the walker" });
  }
  if (req.method !== "GET") return json({ ok: true });
  if (p === "/v1/harnesses") {
    return json({ harnesses: [{ id: "claude", label: "claude", kind: "claude", enabled: true, found: true, cmd: "claude" }] });
  }
  if (p === "/v1/providers") {
    return json({ providers: [{ name: "gh", kind: "github", host: "github.com", worktrees: true }] });
  }
  if (p === "/v1/tasks") return json({ tasks: [] });
  if (p === "/v1/health") return json({ build: "check", settling: false, halted: false });
  if (p === "/v1/settings") {
    return json({ global_auto: false, board_skin: "harbour", board_skins: ["harbour"], scrollback_lines: 5000 });
  }
  if (p === "/_hub/rooms" || p === "/_hub/inventory") return json({ error: "not a hub" }, 404);
  return json({});
});

let fail = false;
const bad = msg => { console.error("FAIL: " + msg); fail = true; };

(async () => {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  const open = async () => {
    await page.evaluate(() => { document.querySelectorAll("dialog[open]").forEach(d => d.close()); switchView("board"); });
    // The paste is the way in: a link pasted on the board, outside any box.
    await page.evaluate(url => {
      const dt = new DataTransfer();
      dt.setData("text", url);
      document.body.dispatchEvent(new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true }));
    }, PR);
    await page.waitForFunction(() => /not here yet/.test(document.getElementById("l-link-note").textContent));
  };
  const press = async () => { await page.evaluate(() => doLaunch().catch(() => {})); };
  const shot = async name => {
    if (!SHOTS) return;
    fs.mkdirSync(SHOTS, { recursive: true });
    await page.locator("#launch").screenshot({ path: path.join(SHOTS, name) });
  };
  try {
    await page.goto("http://127.0.0.1:" + server.address().port + "/");
    await page.waitForFunction(() => typeof openLaunch === "function");

    // The paste itself: the dialog is filled in, and nothing is opened until launch.
    await open();
    await shot("before-paste.png");
    if (seen.open.length) bad("recognising a url opened it. only launch should");

    // Launch: one call with the link and what the dialog says, an edited prompt included, and the card attached.
    await page.fill("#l-prompt", "only look at the handshake");
    await press();
    await page.waitForFunction(() => !document.getElementById("launch").open);
    const o = seen.open[0];
    if (!o || o.body.url !== PR || o.body.prompt !== "only look at the handshake" ||
        o.body.title !== RESOLVED.title || o.body.harness !== "claude") bad("the open was " + JSON.stringify(o));
    if (seen.open.length !== 1) bad("launch opened " + seen.open.length + " times");
    if (seen.chain.length || seen.launch.length) bad("the browser ran its own chain: " + seen.chain.concat(seen.launch.map(() => "/v1/launch")));
    if (seen.tasks[0] !== "/v1/tasks/t1") bad("the opened card was not read to attach: " + seen.tasks);
    await page.waitForFunction(() => typeof termTask !== "undefined" && termTask && termTask.id === "t1", null, { timeout: 5000 })
      .catch(() => bad("the opened card was not attached"));

    // A second paste of a PR with a live card attaches that card and says so.
    reset();
    created = false;
    await open();
    await press();
    await page.waitForFunction(() => !document.getElementById("launch").open);
    if (seen.launch.length) bad("a second paste launched a card of its own");
    if (seen.tasks[0] !== "/v1/tasks/old1") bad("the live card was not attached: " + seen.tasks);
    if (!(await page.evaluate(() => /already under review/.test(document.body.innerText)))) bad("no toast said so");

    // A refusal is the room's sentence, in the dialog, and nothing starts.
    reset();
    refuse = "gh is not logged in. run gh auth login on this machine";
    await page.evaluate(() => switchView("board"));
    await open();
    await press();
    await page.waitForFunction(() => /gh auth login/.test(document.getElementById("launch").innerText));
    if (seen.launch.length || seen.tasks.length) bad("a refused open still started or attached something");
    await shot("after-refused.png");

    // A directory typed over is the operator's: an ordinary launch there, and the link is not opened.
    reset();
    await open();
    await page.fill("#l-cwd", "D:/somewhere/else");
    await press();
    await page.waitForFunction(() => !document.getElementById("launch").open);
    if (seen.open.length) bad("a directory typed over still opened the link");
    if (!seen.launch[0] || seen.launch[0].body.cwd !== "D:/somewhere/else") bad("the typed directory was not launched");
  } catch (e) { bad(String((e && e.stack) || e)); }
  await browser.close();
  server.close();
  if (fail) process.exit(1);
  console.log("pr paste: launch opens the link in one call with the dialog's prompt, attaches its card, a second paste attaches the live card, and refusals stay in the dialog.");
})();
