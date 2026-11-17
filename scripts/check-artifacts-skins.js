// The artifacts pane's tabs, toolbar and code block, measured in every board skin with real computed colours.
//   node scripts/check-artifacts-skins.js [--report] [--shots <dir>]
// It draws the pane's own markup under the board's own stylesheets, so a skin or a rule that turns a tab pale fails here.
"use strict";
const fs = require("fs");
const path = require("path");
const web = path.join(__dirname, "..", "internal", "api", "web");
let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) { console.log("check-artifacts-skins: playwright missing, skipped"); process.exit(0); }

// the terminal theme the pane sits in sets these on .term-pane, light text on a dark terminal and the reverse
const TERMS = [["#d4d4d4", "#1e1e1e", "#264f78"], ["#2a2a2a", "#fafafa", "#b5d5ff"], ["#f2f2f2", "#1b5e38", "#2f7d52"]];
const TEXT = 4.5, GRAPHIC = 3.0;
const argv = process.argv.slice(2);
const report = argv.includes("--report");
const shots = argv.includes("--shots") ? argv[argv.indexOf("--shots") + 1] : "";

const themes = fs.readFileSync(path.join(web, "css/themes.css"), "utf8");
const skins = ["(default)", ...[...themes.matchAll(/^\s{0,2}:root\[data-skin="([^"]+)"\]\s*\{/gm)].map(m => m[1])];
const index = fs.readFileSync(path.join(web, "index.html"), "utf8");
const sheets = [...index.matchAll(/<link rel="stylesheet" href="(\/(?:css|m)\/[^"]+)"/g)].map(m => m[1]);
const readStyle = fs.readFileSync(path.join(web, "read.html"), "utf8").match(/<style>([\s\S]*?)<\/style>/)[1];
const icons = new Function("return " + fs.readFileSync(path.join(web, "js/artifacts.js"), "utf8").match(/const ART_ICONS = (\{[\s\S]*?\n\});/)[1])();
const ic = k => `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">${icons[k]}</svg>`;
const btn = (k, attrs, word) => `<button type="button" class="ab${word ? " w" : ""}" ${attrs || ""}>${ic(k)}${word || ""}</button>`;

const page = `<!doctype html><html><head><meta charset="utf-8">${sheets.map(s => `<link rel="stylesheet" href="${s}">`).join("")}
<style>${readStyle}</style><style>body{margin:0;height:420px;position:relative}.term-pane{position:absolute;inset:0}.term-arts{--arts-top:0px}*{transition:none!important}</style></head>
<body><div class="term-pane"><div class="term-files term-arts viewing" id="t-arts-panel">
<div class="filebar"><span class="arthead">artifacts <span class="artcount">4</span></span>
<span class="seg artkinds"><button type="button" class="on">all<i class="pillcount">4</i></button><button type="button">files<i class="pillcount">2</i></button><button type="button">images<i class="pillcount">1</i></button></span>
<span class="fgrow"></span></div>
<div class="fsplit"><div class="fside"><div class="filelist"><div class="frow agroup"><span>files</span><span class="n">2</span></div>
<div class="frow cur"><span class="fn">one.go</span><span class="aw">a/one.go</span><span class="fsize">3 B</span><span class="fwhen">12:00</span></div></div></div>
<div class="fview" id="t-aview"><div class="filebar abar" id="t-aview-bar">${btn("back", 'data-v="back"')}
<span class="awhere"><code>one.go</code><span class="by">3 B, 12:00</span></span>
<span class="abgroup">${btn("prev", "disabled")}${btn("next")}</span><span class="absep"></span>
<span class="abgroup">${btn("rendered", 'aria-pressed="true"', "rendered")}${btn("raw", 'aria-pressed="false"', "raw")}</span><span class="absep"></span>
<span class="abgroup">${btn("get")}${btn("copy")}${btn("close")}</span></div>
<div class="md"><pre id="code">package main
func main() {}</pre></div></div></div></div></div></body></html>`;

const probes = [
  ["tab, off", "#t-arts-panel .artkinds button:not(.on)", "text", TEXT],
  ["tab, off, border", "#t-arts-panel .artkinds button:not(.on)", "border", GRAPHIC],
  ["tab count, off", "#t-arts-panel .artkinds button:not(.on) .pillcount", "text", TEXT],
  ["tab, on", "#t-arts-panel .artkinds button.on", "text", TEXT],
  ["tab, on, border", "#t-arts-panel .artkinds button.on", "border", GRAPHIC],
  ["tab count, on", "#t-arts-panel .artkinds button.on .pillcount", "text", TEXT],
  ["artifacts count", ".artcount", "text", TEXT],
  ["toolbar file name", "#t-aview-bar .awhere code", "text", TEXT],
  ["toolbar file date", "#t-aview-bar .by", "text", TEXT],
  ["toolbar back icon", '#t-aview-bar [data-v="back"]', "icon", GRAPHIC],
  ["toolbar back, border", '#t-aview-bar [data-v="back"]', "border", GRAPHIC],
  ["toolbar next icon", "#t-aview-bar .abgroup .ab:nth-child(2)", "icon", GRAPHIC],
  ["toolbar raw (off)", '#t-aview-bar [aria-pressed="false"]', "text", TEXT],
  ["toolbar rendered (on)", '#t-aview-bar [aria-pressed="true"]', "text", TEXT],
  ["toolbar download icon", "#t-aview-bar .abgroup:last-child .ab:nth-child(1)", "icon", GRAPHIC],
  ["toolbar delete icon", "#t-aview-bar .abgroup:last-child .ab:nth-child(3)", "icon", GRAPHIC],
  ["list group heading", ".agroup", "text", TEXT],
  ["list file name", ".frow.cur .fn", "text", TEXT],
  ["code block", "#code", "text", TEXT],
];

// Runs in the page: the flattened colour of a probe's foreground and of the surface it sits on, as 0-255 triples.
function measure(arg) {
  const [sel, kind] = arg;
  const cv = document.createElement("canvas").getContext("2d", { willReadFrequently: true });
  const rgba = css => { cv.clearRect(0, 0, 1, 1); cv.fillStyle = "#000"; cv.fillStyle = css; cv.fillRect(0, 0, 1, 1);
    const d = cv.getImageData(0, 0, 1, 1).data; return [d[0], d[1], d[2], d[3] / 255]; };
  const over = (top, under) => [0, 1, 2].map(i => top[i] * top[3] + under[i] * (1 - top[3]));
  const el = document.querySelector(sel);
  const root = getComputedStyle(document.documentElement);
  const stops = ["--shell0-rgb", "--shell1-rgb"].map(v => rgba("rgb(" + root.getPropertyValue(v) + ")"));
  const chain = [];
  for (let e = el.parentElement; e && e.id !== "t-arts-panel"; e = e.parentElement) chain.unshift(e);
  const layers = [...chain, el].map(e => rgba(getComputedStyle(e).backgroundColor)).filter(c => c[3] > 0);
  const cs = getComputedStyle(el);
  let fg = rgba(kind === "border" ? cs.borderTopColor : cs.color);
  let op = 1;
  for (let e = el; e && e !== document.body; e = e.parentElement) op *= Number(getComputedStyle(e).opacity);
  fg = [fg[0], fg[1], fg[2], fg[3] * op];
  const out = stops.map(base => {
    let bg = base.slice(0, 3);
    for (const l of layers) bg = over([l[0], l[1], l[2], l[3]], [...bg, 1]);
    let outer = base.slice(0, 3);
    for (const e of chain) { const c = rgba(getComputedStyle(e).backgroundColor); if (c[3] > 0) outer = over(c, [...outer, 1]); }
    return { bg: outer, fg: over(fg, [...bg, 1]), own: bg };
  });
  return out;
}

const lum = c => { const f = v => { v /= 255; return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4); }; return 0.2126 * f(c[0]) + 0.7152 * f(c[1]) + 0.0722 * f(c[2]); };
const ratio = (a, b) => { const x = lum(a), y = lum(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); };

(async () => {
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1000, height: 420 } });
  await ctx.route("http://fx/**", route => {
    const p = new URL(route.request().url()).pathname;
    if (p === "/") return route.fulfill({ contentType: "text/html", body: page });
    const f = path.join(web, p);
    if (p === "/user.css" || !fs.existsSync(f) || !f.startsWith(web)) return route.fulfill({ status: 404, body: "" });
    return route.fulfill({ contentType: p.endsWith(".css") ? "text/css" : "application/octet-stream", body: fs.readFileSync(f) });
  });
  const wp = await ctx.newPage();
  await wp.goto("http://fx/", { waitUntil: "load" });
  await wp.evaluate(() => document.fonts.ready);
  const bad = [];
  const rows = [];
  for (const [skin, term] of skins.flatMap(s => TERMS.map(t => [s, t]))) {
    await wp.evaluate(t => { const st = document.querySelector(".term-pane").style; st.setProperty("--term-fg", t[0]); st.setProperty("--term-bg", t[1]); st.setProperty("--term-line", t[2]); }, term);
    await wp.evaluate(s => { if (s === "(default)") document.documentElement.removeAttribute("data-skin"); else document.documentElement.setAttribute("data-skin", s); }, skin);
    await wp.waitForFunction(s => document.documentElement.getAttribute("data-skin") === (s === "(default)" ? null : s), skin);
    for (const [what, sel, kind, floor] of probes) {
      // the border and icon of a probe are drawn against the surface under it, and a tab's own fill counts as that surface
      const stops = await wp.evaluate(measure, [sel, kind]);
      const worst = Math.min(...stops.map(s => ratio(s.fg, kind === "border" ? s.bg : s.own)));
      rows.push(`${skin}${TERMS.indexOf(term) ? " (term " + TERMS.indexOf(term) + ")" : ""}\t${what}\t${worst.toFixed(2)}`);
      if (worst < floor) bad.push(`${skin}${TERMS.indexOf(term) ? " (term " + TERMS.indexOf(term) + ")" : ""}: ${what} is ${worst.toFixed(2)}:1, wants ${floor}:1`);
    }
    if (shots) {
      fs.mkdirSync(shots, { recursive: true });
      await wp.screenshot({ path: path.join(shots, skin.replace(/[^a-z0-9]+/gi, "") + (TERMS.indexOf(term) ? "-term" + TERMS.indexOf(term) : "") + ".png") });
    }
  }
  await browser.close();
  if (report) console.log(rows.join("\n"));
  if (bad.length) { console.log(bad.join("\n")); console.log(`FAILED: ${bad.length} artifacts pane pairs under their floor`); process.exit(1); }
  console.log(`check-artifacts-skins ok: ${skins.length} skins x ${TERMS.length} terminal themes, ${probes.length} pairs each`);
})().catch(e => { console.error(e); process.exit(1); });
