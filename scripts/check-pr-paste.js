// A pasted pull request makes its worktree when launch is pressed, in a real browser with every endpoint mocked.
//
//   NODE_PATH=<dir with playwright> node scripts/check-pr-paste.js [shots-dir]
//
// It serves the concatenated board from board-source.js, the way scripts/audit-phone-u001.js does. No atrium process
// is involved. It checks that launch asks the provider for the PR's worktree first, with the host, org, repo and
// number the recogniser captured, that the launch goes to the returned path on the room the hub placed it on, that a
// refusal reads as the daemon's own sentence with nothing launched, and that a directory typed over is left alone.
// With a shots dir it writes before-paste.png, after-worktree.png and after-refused.png, which is how the dialog is
// shown.

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

const ROW = { id: "pr1", org_repo: "openziti/tlsuv", number: 378, state: "ready", title: "fix the handshake",
  url: PR, created_at: "2026-10-04T10:00:00Z", ready_at: "2026-10-04T10:05:00Z", walker_task: "", cost_usd: 1.2,
  counts: {} };

let seen, refuse, created, prErr, walkerCard, cardStatus;
const reset = () => {
  seen = { worktree: [], launch: [], prs: [], walker: [] };
  refuse = ""; created = true; prErr = ""; walkerCard = ""; cardStatus = "working";
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
  if (req.method === "POST" && p === "/v1/providers/gh/pr-worktree") {
    seen.worktree.push(JSON.parse(body));
    if (refuse) return json({ error: refuse }, 400);
    return json({ path: WORKTREE, existed: false, branch: "fix-handshake" }, 200, { "X-Atrium-Placed-Room": "beta" });
  }
  if (req.method === "POST" && p === "/v1/launch") {
    seen.launch.push({ body: JSON.parse(body), room: req.headers["x-atrium-room"] || "" });
    return json({ id: "t1", status: "running", supervised: false });
  }
  if (req.method === "POST" && p === "/v1/prs") {
    seen.prs.push({ body: JSON.parse(body), room: req.headers["x-atrium-room"] || "", at: seen.launch.length });
    if (prErr) return json({ error: prErr }, 400);
    return json({ pr: Object.assign({}, ROW, { state: "queued", walker_task: walkerCard }), created }, created ? 201 : 200);
  }
  if (req.method === "POST" && p === "/v1/prs/pr1/walker") {
    seen.walker.push({ body: JSON.parse(body), room: req.headers["x-atrium-room"] || "" });
    return json({ pr: Object.assign({}, ROW, { walker_task: JSON.parse(body).task }) });
  }
  if (req.method === "GET" && p === "/v1/tasks/old1") {
    return json({ id: "old1", status: cardStatus, supervised: true, title: "the first walker" });
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

    // The paste itself: the dialog is filled in and says the directory is not there yet.
    await open();
    await shot("before-paste.png");
    if (seen.worktree.length) bad("recognising a url made a worktree. only launch should");

    // Launch: the worktree first, then the card on its path and on the room the hub placed it on.
    await press();
    await page.waitForFunction(() => !document.getElementById("launch").open);
    const w = seen.worktree[0];
    if (!w || w.host !== "github.com" || w.org !== "openziti" || w.repo !== "tlsuv" || w.number !== 378) {
      bad("pr-worktree asked for " + JSON.stringify(w));
    }
    const l = seen.launch[0];
    if (!l || l.body.cwd !== WORKTREE) bad("launch cwd was " + (l && l.body.cwd) + ", want " + WORKTREE);
    if (!l || l.room !== "beta") bad("launch went to room '" + (l && l.room) + "', want the placed room beta");

    // The paste starts the review: POST /v1/prs on the placed room, before the card, and the card is tagged and set
    // as the row's walker.
    const rv = seen.prs[0];
    if (!rv || rv.body.url !== PR) bad("the review was asked for " + JSON.stringify(rv));
    if (!rv || rv.room !== "beta") bad("the review went to room '" + (rv && rv.room) + "', want beta");
    if (!rv || rv.at !== 0) bad("the review did not start before the card");
    const tags = (l && l.body.tags) || [];
    if (!tags.includes("pr") || !tags.includes("pr:openziti/tlsuv#378")) bad("the card's tags were " + tags);
    const wk = seen.walker[0];
    if (!wk || wk.body.action !== "set" || wk.body.task !== "t1" || wk.room !== "beta") {
      bad("the walker was set with " + JSON.stringify(wk));
    }
    // The walk drawer finds the row by the card.
    const found = await page.evaluate(() => { const r = walkPrOf("t1"); return r && r.id; });
    if (found !== "pr1") bad("walkPrOf(t1) found " + found + ", want pr1");
    // Without a row the pulls view is shown as it is, which is the before picture.
    if (found !== "pr1") await page.evaluate(() => { pulls.rows = []; });
    else await page.evaluate(() => pullsApplyRow({ id: "pr1", org_repo: "openziti/tlsuv", number: 378, state: "ready",
      title: "fix the handshake", url: "x", created_at: "2026-10-04T10:00:00Z", ready_at: "2026-10-04T10:05:00Z",
      walker_task: "t1", cost_usd: 1.2, counts: {} }));
    await page.evaluate(() => { pulls.loaded = true; switchView("pulls"); pullsPaint(); });
    await page.waitForSelector(".pull button, .pull .pull-btn", { timeout: 3000 }).catch(() => {});
    if (SHOTS) {
      fs.mkdirSync(SHOTS, { recursive: true });
      await page.screenshot({ path: path.join(SHOTS, found === "pr1" ? "card-with-walk.png" : "pulls-no-row.png") });
    }
    await page.evaluate(() => switchView("board"));

    // A review that will not start is the dialog's note, and the card still launches.
    reset();
    prErr = "no forge for that host";
    await open();
    await press();
    await page.waitForFunction(() => !document.getElementById("launch").open);
    if (!seen.launch[0]) bad("a refused review stopped the card");
    if (seen.walker.length) bad("a walker was set with no row");
    if ((seen.launch[0].body.tags || []).includes("pr")) bad("a card with no row was tagged pr");

    // A second paste of a PR with a live card attaches that card and starts no second one.
    reset();
    created = false; walkerCard = "old1";
    await open();
    await press();
    await page.waitForFunction(() => !document.getElementById("launch").open);
    if (seen.launch.length) bad("a second paste launched a second card");
    if (!(await page.evaluate(() => /already under review/.test(document.body.innerText)))) bad("no toast said so");

    // The same, but the old card is done: a new one is launched and becomes the walker.
    reset();
    created = false; walkerCard = "old1"; cardStatus = "done";
    await open();
    await press();
    await page.waitForFunction(() => !document.getElementById("launch").open);
    if (!seen.launch.length || !seen.walker.length) bad("a dead walker was not replaced");

    // A refusal is the daemon's sentence, in the dialog, and nothing starts.
    reset();
    refuse = "gh is not logged in. run gh auth login on this machine";
    await open();
    await press();
    await page.waitForFunction(() => /gh auth login/.test(document.getElementById("launch").innerText));
    if (seen.launch.length) bad("a refused worktree still launched");
    await shot("after-refused.png");

    // A directory typed over is the operator's, and no worktree is made for it.
    reset();
    await open();
    await page.fill("#l-cwd", "D:/somewhere/else");
    await press();
    await page.waitForFunction(() => !document.getElementById("launch").open);
    if (seen.worktree.length) bad("a directory typed over still made a worktree");
    if (seen.prs.length) bad("a directory typed over still started a review");
    if (!seen.launch[0] || seen.launch[0].body.cwd !== "D:/somewhere/else") bad("the typed directory was not launched");

    // The dialog with the worktree made, before the card starts.
    reset();
    await open();
    await page.evaluate(() => { window.__made = makePastedWorktree(pastedPR()); });
    await page.waitForFunction(() => /made the worktree/.test(document.getElementById("l-link-note").textContent));
    await shot("after-worktree.png");

    // The dialog after the paste, with the review started and the card not yet launched.
    reset();
    await open();
    await page.evaluate(async () => { window.__made = await makePastedWorktree(pastedPR()); await startPastedReview(pastedPR(), "beta"); });
    await shot("after-paste.png");
  } catch (e) { bad(String((e && e.stack) || e)); }
  await browser.close();
  server.close();
  if (fail) process.exit(1);
  console.log("pr paste: launch makes the worktree and starts the review on the placed room, the card walks it, a second paste attaches, and refusals stay in the dialog.");
})();
