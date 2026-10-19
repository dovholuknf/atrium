// termrow: a Terminals tab row at a narrow list width. The chips on the right keep their size, the title gives way.
//
// Units: termRowChips.
// Alone: `node scripts/board-tests/termrow.js` runs just these units, the way the full run would.
const path = require("path");
const fs = require("fs");
const { slow } = require("../lib/browser.js");
const { fail } = require("../lib/results.js");

async function termRowChipsSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1100, height: 700 } });
  const p = await ctx.newPage();
  const errors = [];
  p.on("pageerror", e => errors.push(String(e)));
  try {
    await p.addInitScript(() => localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true })));
    await p.goto(base + "/?demo=rooms", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof termRoomChip === "function" && !document.getElementById("rooms").hidden,
      null, { timeout: slow(15000) });
    const got = await p.evaluate(() => {
      const list = document.createElement("div");
      list.id = "termrow-probe";
      list.className = "term-list";
      list.style.cssText = "position:fixed;left:10px;top:10px;width:260px;z-index:99999;display:block";
      list.innerHTML = `<div class="termscroll"><div class="card tab"><div class="card-line">
        <div class="title"><span class="pin">&#9734;</span><span class="tstack"
          ><span class="tname">ziti-hook minion: builds what ziti-edge-tunnel needs before it can run</span
          ><span class="tpath">github/dovholuknf/atrium:claude/ziti-hook-minion-builds-things</span></span></div>
        <div class="chips"><span class="chip warn">? 1</span><span class="chip room" style="--rhue:200">claude-sg4</span></div>
      </div></div></div>`;
      document.body.appendChild(list);
      const fit = el => ({ scroll: el.scrollWidth, client: el.clientWidth, width: el.getBoundingClientRect().width });
      const room = list.querySelector(".chip.room");
      const badge = list.querySelector(".chip.warn");
      return {
        room: fit(room), badge: fit(badge),
        name: fit(list.querySelector(".tname")), path: fit(list.querySelector(".tpath")),
        roomText: room.textContent,
        card: list.querySelector(".card").getBoundingClientRect().right,
        roomRight: room.getBoundingClientRect().right,
      };
    });
    const dir = process.env.TERMROW_SHOTS || "";
    if (dir) {
      fs.mkdirSync(dir, { recursive: true });
      await p.locator("#termrow-probe").screenshot({ path: path.join(dir, process.env.TERMROW_SHOT_NAME || "terminals-row.png") });
    }
    if (got.room.scroll > got.room.client) fail("the room chip is cut: " + JSON.stringify(got));
    if (got.room.client < 40) fail("the room chip shrank: " + JSON.stringify(got));
    if (got.badge.scroll > got.badge.client) fail("the questions badge is cut: " + JSON.stringify(got));
    if (got.name.scroll <= got.name.client) fail("the title did not ellipsize at a narrow list width: " + JSON.stringify(got));
    if (got.path.scroll <= got.path.client) fail("the path line did not ellipsize at a narrow list width: " + JSON.stringify(got));
    if (got.roomRight > got.card + 1) fail("the room chip runs past the card: " + JSON.stringify(got));
    if (errors.length) fail("termRowChips page errors: " + errors.join("; "));
  } finally {
    await ctx.close();
  }
}

const units = [
  { name: "termRowChips", run: ({ browser, base }) => termRowChipsSection(browser, base) },
];
const sections = { termRowChips: termRowChipsSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
