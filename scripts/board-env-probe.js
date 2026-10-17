// What the headless browser on this machine looks like, for reading a board unit that passes on one machine and fails on
// another. Prints the browser, the screen facts a layout or contrast check depends on, and the font Chromium actually
// uses for each of the board's font stacks (asked of Chromium itself through the DevTools protocol, not guessed from
// the names). Run by scripts/ci.sh before the board and kept in the run's artifacts as board-env.txt.
//
//   node scripts/board-env-probe.js
//
// Never fails: a probe that cannot run says so and exits 0, because it must not be the reason a run is red.

const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");

const web = path.join(__dirname, "..", "internal", "api", "web", "css");

// The board's own stacks, read from its stylesheets, so this follows them when they change.
function stacks() {
  const out = {};
  for (const f of fs.readdirSync(web).filter(n => n.endsWith(".css"))) {
    for (const m of fs.readFileSync(path.join(web, f), "utf8").matchAll(/--(sans|mono)\s*:\s*([^;]+);/g)) {
      if (!out[m[1]]) out[m[1]] = m[2].trim();
    }
  }
  return out;
}

function sh(cmd, args) {
  try { return execFileSync(cmd, args, { encoding: "utf8", timeout: 10000 }).trim(); } catch (e) { return "(" + cmd + " not available)"; }
}

async function main() {
  const { chromium } = require("./lib/browser.js");
  const browser = await chromium.launch();
  console.log("board environment probe");
  console.log("  chromium:       " + browser.version());
  console.log("  platform:       " + process.platform + " " + process.arch + ", node " + process.version);
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const page = await ctx.newPage();
  const css = stacks();
  await page.setContent(
    "<body style='margin:0'>" +
    Object.entries(css).map(([k, v]) => "<p id='" + k + "' style='display:inline-block;font:16px " + v + "'>The quick brown fox 0123456789 mmmmmm</p>").join("") +
    ["sans-serif", "serif", "monospace", "system-ui", "ui-monospace"].map(g => "<p id='g-" + g + "' style='display:inline-block;font:16px " + g + "'>The quick brown fox 0123456789 mmmmmm</p>").join("") +
    "</body>");
  const facts = await page.evaluate(() => ({
    ua: navigator.userAgent,
    dpr: devicePixelRatio,
    dark: matchMedia("(prefers-color-scheme: dark)").matches,
    reduced: matchMedia("(prefers-reduced-motion: reduce)").matches,
    forced: matchMedia("(forced-colors: active)").matches,
    tz: Intl.DateTimeFormat().resolvedOptions().timeZone,
    lang: navigator.language,
    widths: Object.fromEntries([...document.querySelectorAll("p")].map(p => [p.id, +p.getBoundingClientRect().width.toFixed(1)])),
  }));
  console.log("  user agent:     " + facts.ua);
  console.log("  dpr / scheme:   " + facts.dpr + ", prefers dark " + facts.dark + ", reduced motion " + facts.reduced + ", forced colors " + facts.forced);
  console.log("  zone / lang:    " + facts.tz + ", " + facts.lang);
  const cdp = await ctx.newCDPSession(page);
  await cdp.send("DOM.enable");
  await cdp.send("CSS.enable");
  const { root } = await cdp.send("DOM.getDocument", { depth: -1 });
  for (const id of [...Object.keys(css), "g-sans-serif", "g-serif", "g-monospace", "g-system-ui", "g-ui-monospace"]) {
    const { nodeId } = await cdp.send("DOM.querySelector", { nodeId: root.nodeId, selector: "#" + id });
    const { fonts } = await cdp.send("CSS.getPlatformFontsForNode", { nodeId });
    const label = css[id] ? "--" + id + " (" + css[id] + ")" : id.slice(2);
    console.log("  font " + label + ":");
    console.log("      used: " + (fonts.map(f => f.familyName + (f.isCustomFont ? " [web font]" : "") + " x" + f.glyphCount).join(", ") || "(none)") +
      "   line width " + facts.widths[id] + "px");
  }
  await browser.close();
  if (process.platform === "linux") {
    console.log("  fc-match sans-serif: " + sh("fc-match", ["sans-serif"]));
    console.log("  fc-match monospace:  " + sh("fc-match", ["monospace"]));
    console.log("  fonts installed:     " + sh("sh", ["-c", "fc-list : family | sort -u | wc -l"]));
  }
}

main().catch(e => { console.log("board environment probe could not run: " + (e && e.message || e)); }).then(() => process.exit(0));
