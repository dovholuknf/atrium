// The board in a real browser, at mobile widths, asserting the two things a
// parser cannot see: that nothing breaks out of the viewport sideways, and that
// no header control renders with nothing in it.
//
// It renders the board the way `board-source.js` assembles it (one file, CSS and
// script inlined), off a file:// URL, with the network stubbed so no atrium has
// to be running. Every fetch answers an empty board and the event stream is a
// no-op, so what is measured is the LAYOUT at each width, not any data.
//
// ── running it ──────────────────────────────────────────
//
//   npm i                       # installs @playwright/test (declared)
//   npx playwright install chromium
//   node scripts/test-board-headless.js
//
// It is a lint, not a build step: with no browser available it SKIPS rather than
// fails, the same way check-board.sh skips when node is missing. A real overflow
// or a blank control exits non-zero.

const fs = require("fs");
const os = require("os");
const path = require("path");
const { wholeBoard } = require("./board-source.js");

// The widths that matter: a phone, a tablet, and the 1150 breakpoint where the
// header controls collapse.
const VIEWPORTS = [
  { name: "phone", width: 390, height: 844 },
  { name: "tablet", width: 768, height: 1024 },
  { name: "breakpoint", width: 1150, height: 800 },
];

// The tabs to walk. Read off the page at runtime would be better, but clicking
// each .tab in turn covers it without hard-coding view ids here.

// Stub the network before any board script runs. Empty everything, so each view
// draws its empty state rather than waiting on a socket.
function initStub() {
  const okJson = (obj) =>
    new Response(JSON.stringify(obj), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  window.EventSource = class {
    constructor() {}
    addEventListener() {}
    removeEventListener() {}
    close() {}
  };
  window.WebSocket = class {
    constructor() {}
    addEventListener() {}
    send() {}
    close() {}
  };
  const answer = (url) => {
    const u = String(url);
    if (u.includes("/_hub/rooms")) return okJson({ rooms: [] });
    if (u.includes("/_hub/inventory")) return okJson({ rooms: [] });
    if (u.includes("/v1/tasks")) return okJson({ tasks: [] });
    if (u.includes("/v1/health")) return okJson({ build: "test" });
    if (u.includes("/v1/permissions")) return okJson({ permissions: [] });
    if (u.includes("/v1/fixtures")) return okJson({ fixtures: [] });
    if (u.includes("/v1/rules")) return okJson({ rules: [] });
    if (u.includes("/v1/history")) return okJson({ events: [] });
    return okJson({});
  };
  window.fetch = (url) => Promise.resolve(answer(url));
}

// Overflow, measured at the document. Inner scrollers (the nav, the kanban, the
// terminal strip) are overflow-x containers on purpose, so their scrollWidth is
// their own business. What must never happen is the DOCUMENT scrolling sideways:
// that is content the viewport cannot reach.
async function overflowOf(page) {
  return page.evaluate(() => {
    const de = document.documentElement;
    return { scrollWidth: de.scrollWidth, innerWidth: window.innerWidth };
  });
}

// Every header control that is on screen has to say what it is: a visible button
// with neither text nor a pseudo-element label is the empty pill this pass exists
// to kill.
async function blankHeaderControls(page) {
  return page.evaluate(() => {
    const bad = [];
    const controls = document.querySelectorAll(
      "header .tab, header button, header .rooms, header .conn");
    for (const el of controls) {
      if (el.hidden || el.offsetParent === null) continue;
      const r = el.getBoundingClientRect();
      if (r.width === 0 || r.height === 0) continue;
      const text = (el.textContent || "").trim();
      const before = getComputedStyle(el, "::before").content;
      const after = getComputedStyle(el, "::after").content;
      const has = (c) => c && c !== "none" && c !== "normal" && c !== '""';
      if (!text && !has(before) && !has(after)) {
        bad.push(el.id || el.className || el.tagName.toLowerCase());
      }
    }
    return bad;
  });
}

async function main() {
  let chromium;
  try {
    ({ chromium } = require("@playwright/test"));
  } catch (e) {
    console.log("playwright is not installed, so the headless board test is skipped.");
    console.log("install it to run this check:  npm i && npx playwright install chromium");
    return 0;
  }

  const html = wholeBoard();
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "atrium-board-"));
  const file = path.join(tmp, "board.html");
  fs.writeFileSync(file, html);
  const url = "file://" + file.replace(/\\/g, "/");

  let browser;
  try {
    browser = await chromium.launch();
  } catch (e) {
    console.log("no chromium for playwright, so the headless board test is skipped.");
    console.log("install it:  npx playwright install chromium");
    fs.rmSync(tmp, { recursive: true, force: true });
    return 0;
  }

  let bad = 0;
  const fail = (m) => { console.error("FAIL: " + m); bad++; };

  for (const vp of VIEWPORTS) {
    const page = await browser.newPage({ viewport: { width: vp.width, height: vp.height } });
    await page.addInitScript(initStub);
    await page.goto(url);
    await page.waitForTimeout(400);

    // Each tab, in turn. The board renders one view at a time, so overflow is
    // measured per view rather than once.
    const tabs = await page.$$("header nav .tab");
    const stops = [null, ...tabs.keys()];
    for (const i of stops) {
      if (i !== null) {
        const t = (await page.$$("header nav .tab"))[i];
        if (t) { await t.click(); await page.waitForTimeout(250); }
      }
      const view = i === null ? "(initial)" :
        ((await page.$$("header nav .tab"))[i] &&
          (await (await page.$$("header nav .tab"))[i].textContent()).trim()) || String(i);
      const o = await overflowOf(page);
      if (o.scrollWidth > o.innerWidth + 1) {
        fail(`${vp.name} ${vp.width}px, view ${view}: document scrolls sideways ` +
          `(${o.scrollWidth} > ${o.innerWidth}). content the viewport cannot reach.`);
      }
    }

    const blank = await blankHeaderControls(page);
    if (blank.length) {
      fail(`${vp.name} ${vp.width}px: header controls render with no label ` +
        `(text or pseudo): ${blank.join(", ")}.`);
    }

    await page.close();
  }

  await browser.close();
  fs.rmSync(tmp, { recursive: true, force: true });

  if (bad) {
    console.error(`\n${bad} responsive assertion${bad === 1 ? "" : "s"} broken.`);
    return 1;
  }
  console.log("the board fits every tested width, and no header control is blank.");
  return 0;
}

main().then((code) => process.exit(code)).catch((e) => {
  console.error(e);
  process.exit(1);
});
