// The mic and its key type the words into any runner's terminal, not only Claude's, in a real browser.
//
//   NODE_PATH=<dir with playwright> node scripts/check-dictate-runner.js
//
// It serves the concatenated board from board-source.js, the way scripts/check-pr-paste.js does. The attached card is a
// codex card on a runner with no bracketed paste, so nothing Claude only can be what the mic waits for. The recorder is
// stubbed to answer a clip, and the input the board would send to the terminal is captured.

const http = require("http");
const { wholeBoard } = require("./board-source.js");

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) {
  try { ({ chromium } = require("playwright")); } catch (e2) { console.log("playwright is not installed"); process.exit(0); }
}

const HTML = wholeBoard();
const server = http.createServer((req, res) => {
  const p = new URL(req.url, "http://x").pathname;
  if (p === "/" || p === "/index.html") { res.writeHead(200, { "Content-Type": "text/html" }); return res.end(HTML); }
  if (p.startsWith("/v1/events")) {
    res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" });
    return res.write(": open\n\n");
  }
  res.writeHead(200, { "Content-Type": "application/json" });
  if (p === "/v1/tasks") return res.end(JSON.stringify({ tasks: [] }));
  if (p === "/v1/health") return res.end(JSON.stringify({ build: "check", settling: false, halted: false }));
  if (p === "/v1/settings") return res.end(JSON.stringify({ board_skin: "harbour", board_skins: ["harbour"], scrollback_lines: 5000 }));
  res.end("{}");
});

let fail = false;
const bad = msg => { console.error("FAIL: " + msg); fail = true; };

(async () => {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  try {
    await page.goto("http://127.0.0.1:" + server.address().port + "/");
    await page.waitForFunction(() => typeof dictFinish === "function" && !!document.querySelector(".dict-mic"));
    const shown = await page.evaluate(() => getComputedStyle(document.querySelector(".dict-dock")).display);
    if (shown === "none") bad("the mic is hidden with no Claude card attached");
    const sent = await page.evaluate(async () => {
      const out = [];
      termTask = { id: "codex1", harness: "codex", provider: "codex", supervised: true };
      termCaps = {};
      window.sendInput = (d, quiet, pasted) => out.push([d, !!pasted]);
      window.atriumVoice = { stop: async () => "run the tests send it", timing: () => null };
      dictState = "listening";
      dictCard = "codex1";
      await dictFinish(false);
      await new Promise(r => setTimeout(r, 400)); // wait: the Enter follows the words after a fixed 250ms
      return out;
    });
    if (sent.length !== 2 || sent[0][0] !== "run the tests" || !sent[0][1] || sent[1][0] !== "\r") {
      bad("the words were not typed and sent like a typed line: " + JSON.stringify(sent));
    }
  } catch (e) { bad(String((e && e.stack) || e)); }
  await browser.close();
  server.close();
  if (fail) process.exit(1);
  console.log("dictate runner: the mic types the words and Enter into a codex card, with no Claude-only gate.");
})();
