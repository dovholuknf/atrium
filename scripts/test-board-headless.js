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

// Stub the network before any board script runs. Two shapes: an empty board, and
// a populated one carrying the widths that break a narrow layout: a long session
// name, a deep worktree path, a long room name, and a permission with a command
// too long for a phone. What is measured is the LAYOUT those produce, not data.
function initStub(populated) {
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

  const now = Math.floor(Date.now() / 1000);
  const TASKS = populated ? [
    { id: "t1", rank: 1, display_title: "doer1: board flap storm", runner: "claude",
      status: "working", worktree: "D:/worktrees/claude/atrium/flap-storm",
      branch: "claude/flap-storm", display_repo: "atrium", pid: 42216, supervised: true },
    { id: "t2", rank: 2, runner: "codex", status: "needs-permission", wait_seconds: 180,
      display_title: "a very long session name that must truncate rather than push the narrow column past the edge",
      worktree: "D:/git/github/openziti/desktop-edge-win/fix-app-version",
      branch: "promote-2.11.3.1-and-beta", display_repo: "desktop-edge-win", pid: 991, supervised: true },
    { id: "t3", rank: 3, display_title: "idle one", runner: "claude", status: "idle",
      worktree: "D:/git/github/dovholuknf/atrium", branch: "main", display_repo: "atrium",
      pid: 1200, supervised: true, pinned: true },
  ] : [];
  const ROOMS = populated ? [
    { name: "claude-sg4", attached: true, host: "sg4", transport: "link" },
    { name: "a-really-long-room-name-that-should-not-overflow", attached: false,
      first_seen: now - 9000, last_seen: now - 60, transport: "link" },
  ] : [];
  const PERMS = populated ? [
    { id: "p1", task_id: "t2", tool: "Bash", cwd: "D:/git/github/openziti/desktop-edge-win",
      command: "bash scripts/check-board.sh 2>&1 | grep -Ei 'storm|flap' && echo 'a command long enough to wrap several lines on a phone screen and then some more'" },
  ] : [];

  const answer = (url) => {
    const u = String(url);
    if (u.includes("/_hub/rooms")) return okJson({ rooms: ROOMS.filter((r) => r.attached) });
    if (u.includes("/_hub/inventory")) return okJson({ rooms: ROOMS });
    if (u.includes("/v1/tasks")) return okJson({ tasks: TASKS });
    if (u.includes("/v1/health")) return okJson({ build: "test" });
    if (u.includes("/v1/permissions/history")) return okJson({ events: [] });
    if (u.includes("/v1/permissions")) return okJson({ permissions: PERMS });
    if (u.includes("/v1/waiting")) return okJson({ waiting: TASKS.filter((t) => t.status === "needs-permission") });
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

  for (const populated of [false, true]) {
    const shape = populated ? "populated" : "empty";
    for (const vp of VIEWPORTS) {
      const page = await browser.newPage({ viewport: { width: vp.width, height: vp.height } });
      await page.addInitScript(initStub, populated);
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
          fail(`${shape} ${vp.name} ${vp.width}px, view ${view}: document scrolls sideways ` +
            `(${o.scrollWidth} > ${o.innerWidth}). content the viewport cannot reach.`);
        }
      }

      const blank = await blankHeaderControls(page);
      if (blank.length) {
        fail(`${shape} ${vp.name} ${vp.width}px: header controls render with no label ` +
          `(text or pseudo): ${blank.join(", ")}.`);
      }

      // Tap targets. A thumb needs about forty pixels, and this is a touch width.
      // Two pixels of slack for sub-pixel rounding.
      if (vp.width <= 900) {
        const small = await page.evaluate(() => {
          const out = [];
          const els = document.querySelectorAll(
            "header .tab, header button, header .rooms, header .conn");
          for (const el of els) {
            if (el.hidden || el.offsetParent === null) continue;
            const r = el.getBoundingClientRect();
            if (r.width === 0 || r.height === 0) continue;
            if (r.height < 38) out.push((el.id || el.className || el.tagName) +
              ":" + Math.round(r.height));
          }
          return out;
        });
        if (small.length) {
          fail(`${shape} ${vp.name} ${vp.width}px: header tap targets under 38px: ` +
            `${small.join(", ")}.`);
        }
      }

      // The room picker is a floating menu placed by script. Open it and check it
      // stays on screen, since a long room name is exactly what pushes it off.
      if (populated) {
        const roomChip = await page.$("#rooms");
        if (roomChip) {
          await roomChip.click();
          await page.waitForTimeout(150);
          const menu = await page.evaluate(() => {
            const m = document.getElementById("rooms-menu");
            if (!m || m.hidden) return null;
            const r = m.getBoundingClientRect();
            return { left: r.left, right: r.right, iw: window.innerWidth };
          });
          if (menu && (menu.left < 0 || menu.right > menu.iw + 1)) {
            fail(`${shape} ${vp.name} ${vp.width}px: the room picker menu runs off ` +
              `screen (left ${Math.round(menu.left)}, right ${Math.round(menu.right)}, ` +
              `width ${menu.iw}).`);
          }
          await page.keyboard.press("Escape").catch(() => {});
        }

        // The new-agent dialog, opened by script so the check reaches it even at
        // widths where the button that opens it is hidden. A dialog is sized to
        // the viewport, so what this catches is a field or a row inside it that
        // is not.
        const dlg = await page.evaluate(() => {
          try { if (typeof openLaunch === "function") openLaunch(); } catch (e) {}
          const d = document.querySelector("dialog[open]");
          if (!d) return null;
          const r = d.getBoundingClientRect();
          return { left: r.left, right: r.right, iw: window.innerWidth, sw: d.scrollWidth, cw: d.clientWidth };
        });
        if (dlg) {
          if (dlg.left < -1 || dlg.right > dlg.iw + 1) {
            fail(`${shape} ${vp.name} ${vp.width}px: the new-agent dialog runs off screen ` +
              `(left ${Math.round(dlg.left)}, right ${Math.round(dlg.right)}, width ${dlg.iw}).`);
          }
          if (dlg.sw > dlg.cw + 1) {
            fail(`${shape} ${vp.name} ${vp.width}px: the new-agent dialog scrolls sideways ` +
              `inside itself (${dlg.sw} > ${dlg.cw}). a field or row is wider than the dialog.`);
          }
          await page.keyboard.press("Escape").catch(() => {});
        }
      }

      await page.close();
    }
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
