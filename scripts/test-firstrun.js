// First run on a real hub: a hub with no room shows the connect panel, joining this machine makes a room, and the checklist, the
// launch dialog, the adopt dialog and the harden wizard run against the real API with no page error.
//
//   node scripts/test-firstrun.js        needs build.claude/atrium(.exe) (make build) and playwright, and says SKIPPED without them
//
// The hub runs in a throwaway home under build.claude/, serves the board from this checkout, and waits on state, never on time.
const fs = require("fs");
const net = require("net");
const path = require("path");
const { spawn } = require("child_process");

const root = path.join(__dirname, "..");
const bin = [path.join(root, "build.claude", "atrium.exe"), path.join(root, "build.claude", "atrium")].find(p => fs.existsSync(p));
let chromium;
try { ({ chromium } = require("playwright")); } catch (e) { try { ({ chromium } = require(path.join(process.env.PLAYWRIGHT_DIR || "", "playwright"))); } catch (e2) {} }
if (!bin || !chromium) { console.log("SKIPPED: test-firstrun needs build.claude/atrium and playwright (" + (bin ? "no playwright" : "no binary") + ")"); process.exit(0); }

const freePort = () => new Promise(ok => { const s = net.createServer(); s.listen(0, "127.0.0.1", () => { const p = s.address().port; s.close(() => ok(p)); }); });
const until = async (what, fn, ms) => {
  const end = Date.now() + (ms || 20000);
  for (;;) {
    try { const v = await fn(); if (v) return v; } catch (e) {}
    if (Date.now() > end) throw new Error("timed out waiting for " + what);
    await new Promise(r => setTimeout(r, 100));
  }
};
let failed = 0;
const check = (name, ok, extra) => { if (!ok) { failed++; console.error("FAIL: " + name + (extra ? ": " + extra : "")); } else console.log("ok   " + name); };

(async () => {
  const dir = path.join(root, "build.claude", "firstrun-test");
  fs.rmSync(dir, { recursive: true, force: true });
  const home = path.join(dir, "home"), proj = path.join(dir, "proj", "billing-api"), other = path.join(dir, "proj", "side-project");
  for (const d of [home, proj, other, path.join(dir, "bin")]) fs.mkdirSync(d, { recursive: true });
  const enc = p => p.replace(/[^A-Za-z0-9]/g, "-");
  const sessionsDir = path.join(home, ".claude", "projects", enc(proj.replace(/\\/g, "/")));
  fs.mkdirSync(sessionsDir, { recursive: true });
  fs.writeFileSync(path.join(sessionsDir, "11111111-aaaa-bbbb-cccc-000000000001.jsonl"),
    JSON.stringify({ type: "user", cwd: proj.replace(/\\/g, "/"), message: { role: "user", content: "tidy the invoice module" } }) + "\n");
  const fake = path.join(dir, "bin", process.platform === "win32" ? "claude.cmd" : "claude");
  fs.writeFileSync(fake, process.platform === "win32" ? "@echo off\r\necho 2.0.0\r\n" : "#!/bin/sh\necho 2.0.0\n", { mode: 0o755 });

  const ports = [await freePort(), await freePort(), await freePort(), await freePort()];
  const env = Object.assign({}, process.env, {
    HOME: home, USERPROFILE: home, APPDATA: path.join(home, "AppData", "Roaming"), LOCALAPPDATA: path.join(home, "AppData", "Local"),
    PATH: path.join(dir, "bin") + path.delimiter + process.env.PATH,
  });
  const hub = spawn(bin, ["run", "--isolated", "--no-hooks", "--no-open", "--no-room", "--addr", "127.0.0.1:" + ports[0], "--link", "127.0.0.1:" + ports[1],
    "--agent", "127.0.0.1:" + ports[2], "--http", "127.0.0.1:" + ports[3], "--atrium-dir", path.join(dir, "hub"), "--dir", path.join(dir, "room"),
    "--db", path.join(dir, "room", "atrium.db"), "--board", path.join(root, "internal", "api", "web")], { env, stdio: "ignore", windowsHide: true });
  const base = "http://127.0.0.1:" + ports[0];
  let browser;
  try {
    await until("the hub to answer", async () => (await fetch(base + "/_hub/rooms")).ok);
    const facts = await (await fetch(base + "/_hub/firstrun")).json();
    check("the hub reports who the room would run as, with no domain", !!facts.user && !facts.user.includes("\\"), JSON.stringify(facts));

    browser = await chromium.launch();
    const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
    const page = await ctx.newPage();
    const errors = [];
    page.on("pageerror", e => errors.push(e.message));
    page.on("console", m => { if (m.type() === "error" && !/503|Service Unavailable/.test(m.text())) errors.push(m.text()); });
    const shot = n => process.env.FIRSTRUN_SHOTS ? page.screenshot({ path: path.join(process.env.FIRSTRUN_SHOTS, n + ".png") }) : null;
    const vis = (sel, ms) => page.waitForSelector(sel, { state: "visible", timeout: ms || 20000 });

    await page.goto(base + "/");
    await vis("#fr-pane .fr-card h2");
    check("a hub with no room shows the connect panel", (await page.textContent("#fr-pane h2")) === "connect this machine");
    check("the room name is prefilled and the user is read only", (await page.inputValue("#fr-room")) === facts.room && (await page.getAttribute("#fr-human", "readonly")) !== null);
    check("no tip text is written inline", (await page.$$eval("#fr-pane .help", e => e.every(x => x.hasAttribute("data-tip") && x.textContent.trim() === "?"))));

    await page.click('[data-act="other"]');
    await until("a join line", () => page.$eval("#fr-cmdtext", e => /^atrium room join \S+/.test(e.value)));
    check("another machine gets a join line to copy", true);
    await page.click('[data-act="other"]');

    await page.click('[data-act="join"]');
    await vis('[data-act="continue"]', 60000);
    const rooms = await (await fetch(base + "/_hub/rooms")).json();
    check("joining this machine attaches a room under the typed name", rooms.rooms.length === 1 && rooms.rooms[0].name === facts.room, JSON.stringify(rooms));
    const joined = await page.textContent("#fr-pane .fr-found");
    check("the joined card reads the agent from the room", /claude/.test(joined), joined);

    await shot("joined");
    await page.click('[data-act="continue"]');
    await vis('[data-step="launch"]');
    const done = await page.$$eval(".fr-step.done", e => e.length);
    check("the checklist opens with the hub and the room done", done === 2, String(done));

    await shot("checklist");
    await page.click('#fr-pane [data-act="launch"]');
    await vis("#na #fr-run");
    check("start an agent opens the new-agent dialog on a folder, in place", (await page.evaluate(() => naS.kind)) === "folder");
    await page.fill("#na-cwd", proj.replace(/\\/g, "/"));
    await until("the launch button to name the folder", async () => /launch in billing-api/.test(await page.textContent("#na-go")));
    check("the folder chips offer the folder a session lives in", (await page.$$eval("#fr-folders [data-path]", e => e.length)) >= 1);
    await shot("launch");
    await page.click('#fr-run [data-v="copy"]');
    check("in a copy can be picked", await page.$eval('#fr-run [data-v="copy"]', e => e.classList.contains("on")));
    await page.click("#na-x");

    await page.click('#fr-pane [data-act="adopt"]');
    await vis("#fr-adopt .fr-sess");
    check("adopt lists the session found on this machine", /tidy the invoice module/.test(await page.textContent("#fr-adopt")));
    await shot("adopt");
    await page.fill("#fr-scan-in", other.replace(/\\/g, "/"));
    await page.click('[data-ad="scan"]');
    await until("the scan answer", async () => /no Claude sessions in/.test(await page.textContent("#fr-adopt")));
    check("scanning a folder with none says so and what to do", true);
    await page.click('#fr-adopt [data-ad="close"] >> nth=-1');

    await page.click('#fr-pane [data-act="harden"]');
    await vis("#fr-harden .fr-opt");
    await page.click('[data-perm="ask"]');
    await page.click('#fr-harden [data-h="next"]');
    await page.fill("#fr-addf", proj.replace(/\\/g, "/"));
    await page.click('[data-h="addf"]');
    await page.click('#fr-harden [data-h="next"]');
    await vis("[data-gd]");
    await shot("wizard-guard");
    await page.click('#fr-harden [data-h="next"]');
    await vis("[data-reach]");
    await page.click('#fr-harden [data-h="next"]');
    await vis(".fr-review");
    await page.click('#fr-harden [data-h="apply"]');
    await page.waitForFunction(() => /setup hardened/.test(document.getElementById("fr-harden").textContent), null, { timeout: 30000 });
    const s = await (await fetch(base + "/v1/settings")).json();
    check("the wizard saved the open folder", String(s.open_paths || "").includes(proj.replace(/\\/g, "/")), JSON.stringify(s.open_paths));
    check("the wizard left permissions asking", !s.global_auto);
    const hooks = await (await fetch(base + "/v1/hooks")).json();
    check("the wizard installed the guard hook", hooks.missing === 0, JSON.stringify(hooks.missing));
    await shot("wizard-done");
    await page.click('#fr-harden [data-h="undo"]');
    await until("the undo to restore open folders", async () => !String((await (await fetch(base + "/v1/settings")).json()).open_paths || "").includes("billing-api"));
    check("undo puts the open folders back", true);

    check("no page error", errors.length === 0, errors.join(" | "));
  } catch (e) {
    failed++;
    console.error("FAIL: " + e.message);
  } finally {
    if (browser) await browser.close();
    hub.kill();
    // The room the board made is its own daemon, so the hub's death leaves it running.
    try {
      const d = JSON.parse(fs.readFileSync(path.join(dir, "room", "daemon.json"), "utf8"));
      await fetch(d.board + "/v1/shutdown", { method: "POST" });
    } catch (e) {}
  }
  process.exit(failed ? 1 : 0);
})();
