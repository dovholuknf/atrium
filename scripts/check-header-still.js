// The header is opaque and the sound button's nudge pulse ends, in a real browser with every endpoint mocked.
//
//   NODE_PATH=<dir with playwright> node scripts/check-header-still.js
//
// A blurred translucent sticky header is re-blurred on every frame anything behind it moves, and a looping pulse on the
// sound button inside it kept the compositor at 60 fps until the first click (item u-new-board-gpu-cpu-fix). So on the
// default skin, a light skin and the website skin this fails if the header has a backdrop-filter or a see-through
// background, and fails if the blocked sound button's animation is not exactly three iterations, still running at the
// start, finished after them, and not left running once they are done.

const http = require("http");
const fs = require("fs");
const path = require("path");
const { wholeBoard } = require("./board-source.js");

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) {
  try { ({ chromium } = require("playwright")); } catch (e2) { console.log("playwright is not installed"); process.exit(0); }
}

const WEB = path.join(__dirname, "..", "internal", "api", "web");
const HTML = wholeBoard();
const TYPES = { ".js": "text/javascript", ".css": "text/css", ".png": "image/png", ".svg": "image/svg+xml",
  ".gif": "image/gif", ".woff2": "font/woff2" };

const server = http.createServer((req, res) => {
  const p = new URL(req.url, "http://x").pathname;
  const json = o => { res.writeHead(200, { "Content-Type": "application/json" }); res.end(JSON.stringify(o)); };
  if (p === "/" || p === "/index.html") { res.writeHead(200, { "Content-Type": "text/html" }); return res.end(HTML); }
  if ((p.startsWith("/vendor/") || p.startsWith("/css/") || p === "/working.gif") && !p.includes("..")) {
    const f = path.join(WEB, p);
    if (fs.existsSync(f) && fs.statSync(f).isFile()) {
      res.writeHead(200, { "Content-Type": TYPES[path.extname(f)] || "application/octet-stream" });
      return res.end(fs.readFileSync(f));
    }
    res.writeHead(404); return res.end("");
  }
  if (p.startsWith("/v1/events")) { res.writeHead(200, { "Content-Type": "text/event-stream" }); return res.write(": open\n\n"); }
  if (p === "/v1/tasks") return json({ tasks: [] });
  if (p === "/v1/health") return json({ build: "check", settling: false, halted: false });
  if (p === "/v1/settings") return json({ global_auto: false, board_skin: "harbour", board_skins: ["harbour"], scrollback_lines: 5000 });
  if (p === "/v1/permissions") return json({ permissions: [] });
  if (p === "/v1/harnesses") return json({ harnesses: [] });
  return json({});
});

let failed = 0;
const check = (ok, what) => { console.log((ok ? "ok   " : "FAIL ") + what); if (!ok) failed++; };

(async () => {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const browser = await chromium.launch();
  const page = await browser.newPage();
  await page.goto("http://127.0.0.1:" + server.address().port + "/");
  await page.waitForTimeout(1000);

  for (const skin of ["", "daylight", "website"]) {
    const got = await page.evaluate(s => {
      if (s) document.documentElement.setAttribute("data-skin", s); else document.documentElement.removeAttribute("data-skin");
      const cs = getComputedStyle(document.querySelector("header"));
      return { filter: cs.backdropFilter, bg: cs.backgroundColor, image: cs.backgroundImage };
    }, skin);
    const name = skin || "default";
    check(got.filter === "none" || got.filter === "" || got.filter === undefined, name + ": header has no backdrop-filter (" + got.filter + ")");
    check(/^rgb\(/.test(got.bg) && got.image === "none", name + ": header background is solid (" + got.bg + ")");
  }

  // The pulse. Restart it so the check does not depend on how long the page has been up.
  const t = await page.evaluate(async () => {
    const el = document.getElementById("sound");
    el.classList.remove("muted"); el.classList.remove("blocked"); void el.offsetWidth; el.classList.add("blocked");
    const a = el.getAnimations().find(x => x.animationName === "nudge");
    if (!a) return { found: false };
    const timing = a.effect.getComputedTiming();
    const start = a.playState;
    const t0 = performance.now();
    const ended = await Promise.race([a.finished.then(() => true), new Promise(r => setTimeout(() => r(false), 9000))]);
    if (!ended) return { found: true, iterations: timing.iterations, start, ended, after: -1, opacity: "" };
    return { found: true, iterations: timing.iterations, start, ms: performance.now() - t0,
      after: el.getAnimations().filter(x => x.playState === "running").length, opacity: getComputedStyle(el).opacity };
  });
  check(t.found, "the blocked sound button runs the nudge animation");
  if (t.found) {
    check(t.ended !== false, "it ends within 9 seconds");
    check(t.iterations === 3, "it runs 3 iterations (" + t.iterations + ")");
    check(t.start === "running", "it is running at the start (" + t.start + ")");
    check(t.after === 0, "no animation is still running on it once it ends (" + t.after + ")");
    check(t.opacity === "1", "it rests at full opacity (" + t.opacity + ")");
  }

  const css = fs.readFileSync(path.join(WEB, "css", "dialogs.css"), "utf8");
  check(!/nudge[^;}]*infinite/.test(css), "dialogs.css does not loop the nudge");

  await browser.close();
  server.close();
  process.exit(failed ? 1 : 0);
})();
