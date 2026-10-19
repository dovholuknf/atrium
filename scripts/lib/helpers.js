// Helpers more than one bucket of scripts/board-tests/ uses, or one a bucket and the harness both use. A helper only one
// bucket reads lives in that bucket's file. Moved here from the harness unchanged.

const path = require("path");
const { T1 } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");
const { slow, fakeWait } = require("../lib/browser.js");
const SCRIPTS = path.join(__dirname, "..");

// Doc screenshots go under build.claude/ so a run never rewrites a tracked file. REFRESH_DOC_IMAGES=1 writes the
// committed ones in the repo.
const docImgDir = (...tracked) => process.env.REFRESH_DOC_IMAGES === "1"
  ? path.join(SCRIPTS, "..", ...tracked)
  : path.join(SCRIPTS, "..", "build.claude", "doc-images", ...tracked.slice(1));

// A CLICK ON AN ALERT LANDS WHERE IT IS ABOUT. One card with a live terminal:
// that terminal attached. One card without one: its detail, or its request.
// Nothing in particular: nowhere, and an open dialog stays open. From a toast,
// the toast log, a plain desktop notification clicked while the board was
// unfocused, the service worker's message, a `?land=` address, and a
// popped-out window. The first case is the race that asked for this: a new
// card listed a moment before its terminal, which landed on the stack.
async function landContext(browser, unfocused) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  await ctx.addInitScript(unfocused => {
    localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
    if (!unfocused) return;
    // Nobody is looking and desktop notifications are allowed, so `notify`
    // takes its third branch. The notification is kept to be clicked.
    Document.prototype.hasFocus = () => false;
    window.__notes = [];
    window.Notification = function (title, o) { this.title = title; this.o = o; window.__notes.push(this); };
    window.Notification.permission = "granted";
    window.Notification.requestPermission = async () => "granted";
    window.Notification.prototype.close = function () {};
  }, !!unfocused);
  return ctx;
}

// Three Claude cards: one under the context threshold, one past it, and one
// the room has no size for.
const CTX_CARDS = [
  Object.assign({}, T1, { id: "cx-small", display_title: "small context", resume_id: "sess-small",
    context_size: { tokens: 90000, warn: false, threshold_k: 150 } }),
  Object.assign({}, T1, { id: "cx-big", display_title: "big context", resume_id: "sess-big",
    context_size: { tokens: 212000, warn: true, threshold_k: 150 } }),
  Object.assign({}, T1, { id: "cx-none", display_title: "no size yet" }),
];

const CTX_USAGE = {
  "cx-small": { context_now: 90000, model: "claude-opus-5-5",
    totals: { rows: 7, input: 4100, output: 18200, cache_write_5m: 0, cache_write_1h: 96000, cache_read: 512000,
      cost: 1.84 } },
  "cx-big": { context_now: 212000, model: "claude-opus-5-5",
    totals: { rows: 44, replies: 301, input: 20400, output: 96100, cache_write_5m: 0, cache_write_1h: 388000,
      cache_read: 6100000, cost: 7.62 },
    // 41 prompts of the card's own in 287 calls, and a refresh and two subagent rows on top.
    by_cause: { operator: { rows: 41, replies: 287 }, keepalive: { rows: 2, replies: 2 },
      subagent: { rows: 1, replies: 12 } } },
};

// ── the board is driven by its event stream, not by polling ─────────────────
//
// u-009. The board keeps one copy of the cards (js/cards.js) and paints every
// view from it. A `task` event carrying the whole row (`row: 1`) replaces that
// card with no request. One without it (an older room) pulls `/v1/tasks` at
// most once every 5s. A click that only changes how the list is drawn paints
// in the frame it was made in. `/v1/waiting` is worked out from the cards and
// never asked for. And the one clock left, the resync, stays quiet while the
// tab is hidden.
function countRequests(ctx) {
  const counts = new Map();
  ctx.on("request", r => {
    const u = new URL(r.url());
    if (u.pathname.startsWith("/vendor/") || u.pathname === "/" || u.pathname.startsWith("/v1/events")) return;
    counts.set(u.pathname, (counts.get(u.pathname) || 0) + 1);
    if (process.env.DEBUG_COUNTS) console.log("  req " + (Date.now() % 100000) + " " + u.pathname);
  });
  return counts;
}

function topCounts(counts) {
  return [...counts.entries()].sort((a, b) => b[1] - a[1]).map(([k, n]) => n + " " + k).join(", ");
}

async function hubReposNoOverflow(p, tag) {
  const r = await p.evaluate(() => { const l = document.getElementById("hubrepos-list");
    return { page: document.documentElement.scrollWidth - innerWidth, list: l.scrollWidth - l.clientWidth,
      wide: [...l.querySelectorAll(".hr-root *")].filter(e => { const b = e.getBoundingClientRect(); const lr = l.getBoundingClientRect();
        if (!b.width) return false; for (let n = e.parentElement; n && n !== l; n = n.parentElement) { const o = getComputedStyle(n).overflowX; if (o === "auto" || o === "scroll" || o === "hidden" || o === "clip") return false; }
        return b.right > lr.right + 1; }).length }; });
  if (r.page > 1 || r.list > 1 || r.wide) fail(tag + ": scrolls sideways " + JSON.stringify(r));
}

const ATTACH_TRIES = 5;
const ATTACH_WAIT_MS = 1000;

// A normal attach opens in about 0.1 s, so a socket still not open after 1 s is not coming: tear it down and attach again.
// `check(id)` runs in the page and is the unit's own condition. `attach: false` waits on an attach the unit already
// started. `fake` polls by moving the page clock, for a page on the fake clock. Every miss is logged for the CI ledger.
async function attachRetry(p, o) {
  const { unit, id, check, attach = true, fake = false } = o;
  const tries = o.tries || ATTACH_TRIES;
  const misses = [];
  const holds = async () => {
    if (fake) {
      try { await fakeWait(p, check, id, ATTACH_WAIT_MS, 10); return true; }
      catch (e) { if (/fakeWait timed out/.test(e.message)) return false; throw e; }
    }
    const end = Date.now() + slow(ATTACH_WAIT_MS);
    for (;;) {
      if (await p.evaluate(check, id)) return true;
      if (Date.now() > end) return false;
      await new Promise(r => setTimeout(r, 10));
    }
  };
  for (let n = 1; n <= tries; n++) {
    const t0 = Date.now();
    if (n > 1 || attach) await p.evaluate(x => attachTask(x), id);
    if (await holds()) {
      if (n > 1) console.log("ATTACHRETRY-RECOVERED " + JSON.stringify({ unit, id, try: n }));
      return;
    }
    const state = await p.evaluate(x => ({
      sock: typeof termSock !== "undefined" && termSock ? termSock.readyState : "none",
      task: typeof termTask !== "undefined" && termTask ? termTask.id : "",
      marks: window.__switchMarks || [],
      row: !![...document.querySelectorAll("#stack-list .stackrow")].find(e => e.dataset.id === x),
    }), id);
    const miss = { unit, id, try: n, ms: Date.now() - t0, termSock: state.sock, termTask: state.task, switchMarks: state.marks, rowInDom: state.row };
    misses.push(miss);
    console.log("ATTACHRETRY " + JSON.stringify(miss));
    await p.evaluate(() => { try { if (termSock) termSock.close(); } catch (e) {} closeTerm(); });
  }
  throw new Error(unit + ": the card terminal never attached in " + tries + " tries of " + ATTACH_WAIT_MS + "ms: " + misses.map(m => JSON.stringify(m)).join(" | "));
}

module.exports = { attachRetry, docImgDir, landContext, CTX_CARDS, CTX_USAGE, countRequests, topCounts, hubReposNoOverflow };
