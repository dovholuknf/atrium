// homes: the card's home on the board, the room's card root, the Disk panel, the new-home launch.
//
// Units: homesChips, homesRoot, homesDisk, homesLaunch, homesAllRooms.
// `units` is what the harness runs, in suite order. `sections` is for HEADLESS_ONLY, which names a section and not a unit.
// Alone: `node scripts/board-tests/homes.js` runs just these units, the way the full run would.
const path = require("path");
const fs = require("fs");
const { slow } = require("../lib/browser.js");
const { fail } = require("../lib/results.js");

async function open(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1100, height: 700 } });
  const p = await ctx.newPage();
  const errors = [];
  p.on("pageerror", e => errors.push(String(e)));
  await p.addInitScript(() => localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true })));
  await p.goto(base + "/?demo=rooms", { waitUntil: "domcontentloaded" });
  await p.waitForFunction(() => typeof homePathChips === "function" && !document.getElementById("rooms").hidden,
    null, { timeout: slow(15000) });
  return { ctx, p, errors };
}

async function shot(p, name, sel) {
  const dir = process.env.HOMES_SHOTS || "";
  if (!dir) return;
  fs.mkdirSync(dir, { recursive: true });
  const file = path.join(dir, name + ".png");
  if (sel) await p.locator(sel).first().screenshot({ path: file });
  else await p.screenshot({ path: file });
}

async function homesChipsSection(browser, base) {
  const { ctx, p, errors } = await open(browser, base);
  try {
    const got = await p.evaluate(() => {
      const draw = h => {
        homesLoaded(h ? [h] : []);
        const box = document.createElement("div");
        box.className = "homes-probe";
        box.style.cssText = "position:fixed;left:10px;top:10px;z-index:99999;background:var(--bg-1);padding:8px";
        box.innerHTML = homePathChips({ id: "t1" });
        document.body.appendChild(box);
        return { text: box.textContent.trim(), size: (box.querySelector(".homesize") || {}).textContent || "", old: !!box.querySelector(".homesize.old") };
      };
      const now = Date.now();
      const base = { id: "h1", card: "t1", path: "/srv/cards/x-abc123", state: "kept", bytes: 5e9, files: 12 };
      return {
        none: draw(null).text,
        fresh: draw(Object.assign({ measured_at: new Date(now - 60e3).toISOString() }, base)),
        old: draw(Object.assign({ measured_at: new Date(now - 3 * 3600e3).toISOString() }, base)),
        never: draw(base),
      };
    });
    if (got.none !== "") fail("a card with no home drew home chips: " + got.none);
    if (!/x-abc123/.test(got.fresh.text) || !/GB/.test(got.fresh.size)) fail("a home shows no path or size: " + JSON.stringify(got.fresh));
    if (/ago/.test(got.fresh.size) || got.fresh.old) fail("a fresh size showed its age: " + JSON.stringify(got.fresh));
    if (!/ago/.test(got.old.size) || !got.old.old) fail("a size over an hour old hid its age: " + JSON.stringify(got.old));
    if (!/not measured/.test(got.never.size)) fail("an unmeasured home claimed a size: " + JSON.stringify(got.never));
    await p.evaluate(() => {
      document.querySelectorAll(".homes-probe").forEach(b => b.remove());
      const now = Date.now();
      const base = { id: "h1", card: "t1", path: "/srv/cards/fix-the-thing-a1b2c3", state: "kept", bytes: 5.2e9, files: 1840 };
      const wrap = document.createElement("div");
      wrap.id = "homes-shot";
      wrap.style.cssText = "position:fixed;left:10px;top:10px;width:760px;z-index:99999;background:var(--bg-1);padding:12px";
      const row = (label, h) => { homesLoaded([h]); return '<div style="margin:6px 0;color:var(--dim)">' + label + '</div><div class="chips">' + homePathChips({ id: "t1" }) + "</div>"; };
      wrap.innerHTML = row("Terminals tab chips, measured a minute ago", Object.assign({ measured_at: new Date(now - 60e3).toISOString() }, base)) +
        row("card detail chips, measured three hours ago", Object.assign({ measured_at: new Date(now - 3 * 3600e3).toISOString() }, base));
      document.body.appendChild(wrap);
    });
    await shot(p, "home-chips", "#homes-shot");
    if (errors.length) fail("homesChips page errors: " + errors.join("; "));
  } finally {
    await ctx.close();
  }
}

async function homesRootSection(browser, base) {
  const { ctx, p, errors } = await open(browser, base);
  try {
    const got = await p.evaluate(() => {
      const row = d => { const h = document.createElement("div"); h.innerHTML = cardRootRow("k", d); const r = h.querySelector(".rd-cardroot"); return r ? r.dataset.low : "none"; };
      const T = 1000e9;
      return {
        roomy: row({ card_root: "/srv/c", homes: 3, homes_bytes: 1e9, root_free_bytes: 500e9, root_total_bytes: T }),
        pct: row({ card_root: "/srv/c", homes: 3, homes_bytes: 1e9, root_free_bytes: 90e9, root_total_bytes: T }),
        bytes: row({ card_root: "/srv/c", homes: 3, homes_bytes: 1e9, root_free_bytes: 15e9, root_total_bytes: 100e9 }),
        unset: row({ homes: 0 }),
      };
    });
    for (const [name, d] of [["room-tile", { card_root: "/srv/cards", homes: 14, homes_bytes: 41e9, root_free_bytes: 500e9, root_total_bytes: 1000e9 }],
      ["room-tile-orange", { card_root: "/srv/cards", homes: 14, homes_bytes: 41e9, root_free_bytes: 15e9, root_total_bytes: 1000e9 }]]) {
      await p.evaluate(d => {
        document.getElementById("homes-shot") && document.getElementById("homes-shot").remove();
        const wrap = document.createElement("div");
        wrap.id = "homes-shot";
        wrap.style.cssText = "position:fixed;left:10px;top:10px;width:300px;z-index:99999;background:var(--bg-1);padding:12px";
        wrap.innerHTML = '<div class="rd-band machine"><div class="rd-h">machine</div>' +
          '<div class="rd-row"><span>disk <b>' + fmtGB(d.root_free_bytes).replace(" GB", "") + "/" + fmtGB(d.root_total_bytes) + "</b> free</span></div>" + cardRootRow("k", d) + "</div>";
        document.body.appendChild(wrap);
      }, d);
      await shot(p, name, "#homes-shot");
    }
    if (got.roomy !== "0") fail("a roomy card root drew orange: " + JSON.stringify(got));
    if (got.pct !== "1") fail("under 10 percent free was not orange: " + JSON.stringify(got));
    if (got.bytes !== "1") fail("under 20 GB free was not orange: " + JSON.stringify(got));
    if (got.unset !== "none") fail("a room with no card root drew a row: " + JSON.stringify(got));
    if (errors.length) fail("homesRoot page errors: " + errors.join("; "));
  } finally {
    await ctx.close();
  }
}

async function homesDiskSection(browser, base) {
  const { ctx, p, errors } = await open(browser, base);
  try {
    const posted = [];
    await p.route("**/v1/homes**", route => {
      const r = route.request();
      if (r.method() === "POST") posted.push({ url: new URL(r.url()).pathname, body: r.postData() });
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(r.method() === "POST" ? { removed: true, freed: 2e9 } : { homes: [] }) });
    });
    const sent = await p.evaluate(async (posted) => {
      diskView = { root: "/srv/cards", free_bytes: 5e9, total_bytes: 1e12, bytes: 3e9, homes: [
        { id: "h1", card: "t1", title: "one", path: "/srv/cards/one-aaaaaa", state: "kept", reason: "dirty", bytes: 2e9, measured_at: new Date().toISOString(), running: false },
        { id: "h2", card: "t2", title: "two", path: "/srv/cards/two-bbbbbb", state: "live", bytes: 1e9, measured_at: new Date().toISOString(), running: true },
        { id: "h3", card: "", title: "", path: "/srv/cards/gone-cccccc", state: "kept", reason: "its card was deleted", bytes: 4e8, measured_at: new Date().toISOString(), orphaned: true, running: false },
      ], not_atriums: ["/srv/cards/stray"], outside: [] };
      const view = JSON.stringify(diskView);
      paintDiskPanel();
      const buttons = [...document.querySelectorAll("#rc-disk .homerm")].map(b => b.dataset.home);
      window.askUser = async () => true;
      await removeHomeFromDisk(document.querySelector("#rc-disk .homerm"));
      return { buttons, calls: [], view };
    }, posted);
    sent.calls = posted;
    await p.evaluate(view => {
      diskView = JSON.parse(view);
      const out = document.getElementById("rc-disk");
      const wrap = document.createElement("div");
      wrap.id = "homes-shot";
      wrap.style.cssText = "position:fixed;left:10px;top:10px;width:820px;z-index:99999;background:var(--bg-1);padding:12px";
      const h = document.createElement("div");
      h.className = "dlg-sec-h";
      h.textContent = "Disk";
      wrap.append(h, out);
      document.body.appendChild(wrap);
      paintDiskPanel();
    }, sent.view);
    await shot(p, "disk-panel", "#homes-shot");
    if (sent.buttons.join() !== "h1,h3") fail("remove offered on the wrong homes: " + sent.buttons);
    const post = sent.calls.find(c => c.url === "/v1/homes/remove");
    if (!post) fail("remove posted nothing: " + JSON.stringify(sent.calls));
    const body = JSON.parse(post.body);
    if (Object.keys(body).join() !== "id" || body.id !== "h1") fail("remove posted more than the id: " + post.body);
    if (/srv\/cards/.test(post.body)) fail("remove posted a path: " + post.body);
    if (errors.length) fail("homesDisk page errors: " + errors.join("; "));
  } finally {
    await ctx.close();
  }
}

async function homesLaunchSection(browser, base) {
  const { ctx, p, errors } = await open(browser, base);
  try {
    const launched = [];
    await p.route("**/v1/launch", route => {
      launched.push(route.request().postData());
      route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ error: "stop" }) });
    });
    const got = await p.evaluate(async () => {
      openLaunch("claude", "", "");
      const shown = !document.getElementById("l-home-field").hidden;
      const on = document.getElementById("l-home-on");
      on.checked = true;
      on.dispatchEvent(new Event("change"));
      document.getElementById("l-home-repo").value = "github.com/o/r";
      document.getElementById("l-home-branch").value = "feat/x";
      document.getElementById("l-cwd").value = "/typed/dir";
      try { await launchNow(false); } catch (e) {}
      return { shown, cwdOff: document.getElementById("l-cwd").disabled };
    });
    got.body = launched.length ? JSON.parse(launched[0]) : null;
    await p.evaluate(() => {
      const d = document.getElementById("launch");
      if (!d.open) d.show();
      const on = document.getElementById("l-home-on");
      if (!on.checked) { on.checked = true; on.dispatchEvent(new Event("change")); }
      document.getElementById("l-home-field").scrollIntoView({ block: "center" });
    });
    await shot(p, "new-card-form", "#launch");
    if (!got.shown) fail("the new-home tick is not offered on a fresh launch");
    if (!got.body) fail("the launch posted nothing");
    if (got.body.home !== true || got.body.repo !== "github.com/o/r" || got.body.branch !== "feat/x") fail("the home launch lacks home, repo or branch: " + JSON.stringify(got.body));
    if (got.body.cwd) fail("a home launch posted a cwd: " + JSON.stringify(got.body));
    if (!got.cwdOff) fail("the directory box stayed live under a new home");
    if (errors.length) fail("homesLaunch page errors: " + errors.join("; "));
  } finally {
    await ctx.close();
  }
}

// The all-rooms view of a hub with more than one room asks for neither the homes nor the turn limits: the hub answers
// both with a 409 asking for a room. Scoped to one room, both are asked.
async function homesAllRoomsSection(browser, base) {
  const { ctx, p, errors } = await open(browser, base);
  try {
    const asked = { homes: 0, limits: 0 };
    await ctx.route(/\/v1\/homes(\?|$)/, route => { asked.homes++; route.fulfill({ json: { homes: [] } }); });
    await ctx.route(/\/v1\/usage\/turn-limits/, route => { asked.limits++; route.fulfill({ json: { day: "", findings: [] } }); });
    const ask = () => p.evaluate(async () => { turnLimits.at = 0; await loadHomes(true); await renderTurnLimits(); });
    const where = await p.evaluate(() => ({ hub: hubIsHub, rooms: hubRooms.length, room: roomNow() }));
    if (!where.hub || where.rooms < 2 || where.room) fail("homesAllRooms: the demo is not the all-rooms view of a hub: " + JSON.stringify(where));
    asked.homes = asked.limits = 0;
    await ask();
    if (asked.homes || asked.limits) fail("homesAllRooms: the all-rooms view asked for what the hub refuses: " + JSON.stringify(asked));
    await p.evaluate(() => sessionStorage.setItem(ROOMS_DEMO_KEY, hubRooms[0].name));
    await ask();
    if (!asked.homes || !asked.limits) fail("homesAllRooms: a board scoped to one room did not ask: " + JSON.stringify(asked));
    if (errors.length) fail("homesAllRooms page errors: " + errors.join("; "));
  } finally {
    await ctx.close();
  }
}

const units = [
  { name: "homesChips", run: ({ browser, base }) => homesChipsSection(browser, base) },
  { name: "homesRoot", run: ({ browser, base }) => homesRootSection(browser, base) },
  { name: "homesDisk", run: ({ browser, base }) => homesDiskSection(browser, base) },
  { name: "homesLaunch", run: ({ browser, base }) => homesLaunchSection(browser, base) },
  { name: "homesAllRooms", run: ({ browser, base }) => homesAllRoomsSection(browser, base) },
];
const sections = { homesChips: homesChipsSection, homesRoot: homesRootSection, homesDisk: homesDiskSection, homesLaunch: homesLaunchSection, homesAllRooms: homesAllRoomsSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
