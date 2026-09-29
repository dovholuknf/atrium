// The rooms dashboard: one tile per room in the header's room menu.
//
// ── what this file is for ────────────────────────────────────────────────────
//
// The room chip opens the same menu it always did, and a click on a tile still
// focuses the board on that room (`pickRoom`). What changed is that each row
// grew into a tile with three bands: the agents, the tokens and the machine.
// The "all rooms" tile sits on top as the board-wide total.
//
// ── where the numbers come from ─────────────────────────────────────────────
//
// TWO SOURCES AND NO THIRD. The agent counts and the limit bars come from the
// board's own card list (`lastTasks`), which every view already keeps current,
// so they can never disagree with the columns next to them. Everything else
// comes from a `room-stats` event, one snapshot per room, pushed by the room.
// Nothing here asks for anything: no fetch, no poll, no timer. The board is
// event-driven, and this is one more thing driven by an event.
//
// A FIELD A ROOM HAS NOT SHIPPED YET IS ABSENT, and its band shows a dash. A
// dash is a true statement about what is known. A zero would be a lie.
//
// ── and the preview ─────────────────────────────────────────────────────────
//
// `?demo=rooms` runs the fixture at the bottom of this file. It is the stand-in
// for the pushed event and nothing else: it builds snapshots in the event's
// exact shape and hands them to `onRoomStats`, the same function the stream's
// listener calls. See `ROOMS_DEMO` in js/rooms.js for how it is gated.

// roomStats is the latest snapshot per room name. Replaced whole on every
// event, because a snapshot is the room's complete answer at one moment and
// merging two of them would draw a moment that never happened.
const roomStats = {};

// A limit report older than this is not drawn. The bar is what a card last
// said, and an hour-old percentage on a five-hour window is history.
const LIMIT_FRESH_SECONDS = 3600;

// onRoomStats takes one `room-stats` snapshot, from the stream or the preview.
//
// A snapshot of a version this board does not know is dropped rather than
// guessed at. Only the tile it is about is redrawn, and the all-rooms tile,
// since that is the sum it feeds.
function onRoomStats(snap) {
  if (!snap || typeof snap !== "object" || !snap.room) return;
  if (snap.v !== undefined && snap.v !== 1) return;
  roomStats[snap.room] = snap;
  paintRoomTile(snap.room);
}

// paintRoomTile swaps one tile in the OPEN menu. A closed menu draws nothing:
// it paints from `roomStats` when it opens, so nothing is lost by waiting.
function paintRoomTile(name) {
  const menu = document.getElementById("rooms-menu");
  if (!menu || menu.hidden) return;
  const r = hubRooms.find(x => x.name === name);
  const tile = r && [...menu.querySelectorAll(".rtile[data-room]")]
    .find(el => el.dataset.room === name);
  if (tile) swapTile(tile, roomTileHTML(r, roomStats[name], roomNow() === name));
  const all = menu.querySelector(".rtile.all");
  if (all) swapTile(all, allRoomsTileHTML(hubRooms, roomNow() === ""));
}

// swapTile replaces a tile and fades the numbers that moved.
//
// A number that changed carries its old text in `data-was`, which the stylesheet
// fades out over the new one as it fades in. A number that did not change is
// left alone, so a steady board stays still.
function swapTile(el, html) {
  const was = {};
  el.querySelectorAll("[data-v]").forEach(n => { was[n.dataset.v] = n.textContent; });
  const t = document.createElement("template");
  t.innerHTML = html.trim();
  const next = t.content.firstElementChild;
  if (!next) return;
  next.querySelectorAll("[data-v]").forEach(n => {
    const old = was[n.dataset.v];
    if (old === undefined || old === n.textContent) return;
    n.dataset.was = old;
    n.classList.add("rd-fade");
  });
  el.replaceWith(next);
}

// ── the numbers ─────────────────────────────────────────────────────────────

// roomCards is the cards on one room, or on every room when `name` is null.
//
// On a board scoped to one room the cards carry no room of their own, because
// every one of them is that room's. In the preview the made-up rooms have
// made-up cards, kept apart from `lastTasks` so the real columns never show them.
function roomCards(name) {
  const all = roomsDemoLive ? roomsDemoCards
    : (typeof lastTasks !== "undefined" && Array.isArray(lastTasks) ? lastTasks : []);
  if (name == null) return all;
  const scoped = roomNow() === name;
  return all.filter(t => (t.room || (scoped ? name : "")) === name);
}

// Whether a waiting card is asking something, rather than only done with its turn.
function cardAsks(t) {
  const s = t.seen;
  return !!(s && !s.answered && Array.isArray(s.open_questions) && s.open_questions.length);
}

// agentCounts reads the four agent numbers off a list of cards.
//
// NEEDS YOU is permissions plus questions, the two things only a person can
// clear. A card that finished its turn and asked nothing is idle.
function agentCounts(cards) {
  const out = { running: 0, idle: 0, needs_you: 0, done_24h: 0 };
  const dayAgo = Date.now() - 86400000;
  for (const t of cards) {
    if (t.status === "running") out.running++;
    else if (t.status === "needs-permission") out.needs_you++;
    else if (t.status === "needs-input") { if (cardAsks(t)) out.needs_you++; else out.idle++; }
    else if (t.status === "done" && Date.parse(t.last_activity_at || "") >= dayAgo) out.done_24h++;
  }
  return out;
}

// highestSeen is the highest limit any card reported in the last hour.
//
// ATRIUM DOES NOT KNOW WHICH ACCOUNT A ROOM RUNS UNDER, so this is never an
// account's state. It is what a card said, labelled `highest seen`, with the
// card, the age and the reset in its hint. Across rooms it is the highest of
// them, never a sum: two cards at 60% of one window are not at 120%.
function highestSeen(cards, key) {
  let best = null;
  for (const t of cards) {
    const c = t.telemetry;
    const lim = c && c[key];
    if (!lim || typeof lim.pct !== "number") continue;
    const age = Number(c.seconds) || 0;
    if (age > LIMIT_FRESH_SECONDS) continue;
    if (best && lim.pct <= best.pct) continue;
    best = { pct: lim.pct, resets_at: lim.resets_at || "", seconds: age,
      card: t.display_title || String(t.id || "").slice(0, 8), room: t.room || "" };
  }
  return best;
}

// Tokens that cost: in, out and cache writes. Cache reads are shown apart,
// because they are most of every total and would drown the rest.
function counted(o) {
  return o ? (o.in || 0) + (o.out || 0) + (o.cache_write || 0) : 0;
}

// Short numbers for a tile: 41k, 18.2M, 1.3B.
function fmtShort(n) {
  n = Number(n) || 0;
  const one = (v, u) => (v >= 100 ? Math.round(v) : Math.round(v * 10) / 10) + u;
  if (n >= 1e9) return one(n / 1e9, "B");
  if (n >= 1e6) return one(n / 1e6, "M");
  if (n >= 1e4) return Math.round(n / 1e3) + "k";
  if (n >= 1e3) return one(n / 1e3, "k");
  return String(Math.round(n));
}

// Bytes as whole gigabytes, which is the grain a disk or a machine is read at.
function fmtGB(b) {
  const g = (Number(b) || 0) / 1e9;
  return (g >= 10 ? Math.round(g) : Math.round(g * 10) / 10) + " GB";
}

// The build the hub runs, for the `not the hub's build` chip. Only the preview
// knows it today. Until a hub says, no room is flagged.
function roomsHubBuild() {
  return roomsDemoLive ? ROOMS_DEMO_HUB_BUILD : "";
}

// ── the pieces ──────────────────────────────────────────────────────────────

const RD_DASH = `<span class="rd-dash">&mdash;</span>`;

// sparkSVG is a series as an inline SVG line, oldest point on the left.
//
// Redrawn from the array every time, never animated: a polyline is a few
// hundred bytes and the browser draws it in no time. `hi` pins the top, so a
// CPU line is out of 100 and not out of its own peak.
function sparkSVG(series, cls, hi) {
  if (!Array.isArray(series) || series.length < 2) return `<span class="rd-spark ${cls} none"></span>`;
  const n = series.length;
  const top = hi || Math.max(1, ...series.map(v => Number(v) || 0));
  const pts = series.map((v, i) =>
    `${(i * 100 / (n - 1)).toFixed(1)},${(23 - Math.min(1, (Number(v) || 0) / top) * 21).toFixed(1)}`).join(" ");
  return `<svg class="rd-spark ${cls}" viewBox="0 0 100 24" preserveAspectRatio="none" aria-hidden="true"
    ><polygon class="area" points="0,24 ${pts} 100,24"/><polyline points="${pts}"/></svg>`;
}

// limitBar is one `highest seen` bar, or a dash when no card has said.
function limitBar(label, lim) {
  if (!lim) {
    return `<div class="rd-lim" data-tip="no card reported a ${label} limit in the last hour"
      ><span class="k">${label}</span>${RD_DASH}</div>`;
  }
  const pct = Math.max(0, Math.min(100, Math.round(lim.pct)));
  const heat = pct >= 85 ? " hot" : pct >= 60 ? " warm" : "";
  const when = lim.seconds > 5 ? ago(lim.seconds) + " ago" : "just now";
  const reset = lim.resets_at ? `, resets ${resetsIn(lim.resets_at)}` : "";
  const tip = `highest seen: ${lim.card}${lim.room ? " on " + lim.room : ""} reported ${pct}% ` +
    `of its ${label} limit ${when}${reset}. atrium does not know which account a room runs under, ` +
    `so this is what a card last said and not the account's state.`;
  return `<div class="rd-lim${heat}" data-tip="${esc(tip)}"
    ><span class="k">${label}</span><span class="rd-bar"><i style="width:${pct}%"></i></span
    ><span class="v" data-v="${label}">${pct}%</span></div>`;
}

// limitBars is the pair, under one `highest seen` label.
function limitBars(cards) {
  return `<div class="rd-limits"><div class="rd-sub">limits, highest seen</div>` +
    limitBar("5h", highestSeen(cards, "five_hour")) +
    limitBar("week", highestSeen(cards, "weekly")) + `</div>`;
}

// agentsBand is the first band, from cards alone.
function agentsBand(key, q, a, runners) {
  const need = a.needs_you
    ? `<span class="rd-need on" role="button" tabindex="0"
        data-tip="${a.needs_you} waiting on you: open ${q ? "this room's" : "the"} stack on what is waiting"
        onclick="event.stopPropagation();roomNeedsYou('${q}')"
        ><b data-v="${key}.need" data-n="${a.needs_you}">${a.needs_you}</b> needs you</span>`
    : `<span class="rd-need"><b data-v="${key}.need" data-n="0">0</b> needs you</span>`;
  let kinds = "";
  if (runners === undefined) kinds = "";
  else if (!Array.isArray(runners) || !runners.length) kinds = `<div class="rd-row rd-kinds">${RD_DASH}</div>`;
  else {
    kinds = `<div class="rd-row rd-kinds">` + runners.map(r =>
      `<span class="${r.resolves ? "" : "off"}" data-tip="${esc(r.kind)}${r.resolves
        ? "" : ": its binary does not resolve on this machine"}">${esc(r.kind)}</span>`)
      .join(`<i>&middot;</i>`) + `</div>`;
  }
  return `<div class="rd-band agents"><div class="rd-h">agents</div>
    <div class="rd-row"><span><b class="rd-big" data-v="${key}.run" data-n="${a.running}">${a.running}</b> running</span>
      <span><b data-v="${key}.idle" data-n="${a.idle}">${a.idle}</b> idle</span></div>
    <div class="rd-row">${need}
      <span><b data-v="${key}.done" data-n="${a.done_24h}">${a.done_24h}</b> done 24h</span></div>
    ${kinds}</div>`;
}

// tokensBand is the rate, its hour, the 24h total and the limit bars.
function tokensBand(key, tok, cards) {
  const tip = "in, out and cache writes. cache reads are counted apart, since they are most of any total.";
  const body = !tok ? `<div class="rd-row">${RD_DASH}</div>` : `
    <div class="rd-row rd-rate" data-tip="per minute over the last 5 minutes: ${tip}">
      <span><b class="rd-big" data-v="${key}.rate" data-n="${tok.per_min_5m || 0}">${fmtShort(tok.per_min_5m)}</b>/min</span>
      ${sparkSVG(tok.series_per_min, "tok")}</div>
    <div class="rd-row" data-tip="the last 24 hours: ${tip}">
      <span>24h <b data-v="${key}.24h" data-n="${counted(tok.last_24h)}">${tok.last_24h ? fmtShort(counted(tok.last_24h)) : "&mdash;"}</b></span>
      <span class="rd-dim">cache <span data-v="${key}.cache" data-n="${(tok.last_24h || {}).cache_read || 0}">${tok.last_24h ? fmtShort(tok.last_24h.cache_read) : "&mdash;"}</span></span></div>`;
  return `<div class="rd-band tokens"><div class="rd-h">tokens</div>${body}${limitBars(cards)}</div>`;
}

// machineBand is the CPU and its line, memory, disk and worktrees.
function machineBand(key, m, d) {
  const cpu = m && typeof m.cpu_pct === "number"
    ? `<div class="rd-row rd-rate${m.cpu_pct > 90 ? " hot" : ""}">
        <span>cpu <b class="rd-big" data-v="${key}.cpu">${Math.round(m.cpu_pct)}%</b></span>
        ${sparkSVG(m.cpu_series_pct, "cpu", 100)}</div>`
    : `<div class="rd-row"><span>cpu ${RD_DASH}</span></div>`;
  const mem = m && m.mem_total_bytes
    ? `<div class="rd-row"><span>mem <b data-v="${key}.mem">${fmtGB(m.mem_used_bytes).replace(" GB", "")}/${fmtGB(m.mem_total_bytes)}</b></span></div>`
    : `<div class="rd-row"><span>mem ${RD_DASH}</span></div>`;
  const disk = d && d.total_bytes
    ? `<div class="rd-row" data-tip="free on ${esc(d.path || "the volume holding the worktrees")}${d.db_bytes
        ? ", of which atrium's database is " + fmtGB(d.db_bytes) : ""}"
        ><span>disk <b data-v="${key}.disk">${fmtGB(d.free_bytes).replace(" GB", "")}/${fmtGB(d.total_bytes)}</b> free</span></div>`
    : `<div class="rd-row"><span>disk ${RD_DASH}</span></div>`;
  const wt = d && typeof d.worktrees === "number"
    ? `<div class="rd-row" data-tip="card worktrees atrium knows about"><span><b data-v="${key}.wt">${d.worktrees}</b> worktrees</span></div>`
    : "";
  return `<div class="rd-band machine"><div class="rd-h">machine</div>${cpu}${mem}${disk}${wt}</div>`;
}

// ── the tiles ───────────────────────────────────────────────────────────────

// roomTileHTML is one room as a tile.
//
// KNOWS NOTHING ABOUT THE MENU. It takes the room record and its snapshot and
// returns markup, so a rooms page can lay the same tiles out later. `on` marks
// the room being looked at.
//
// The tile is the click that focuses the board on the room. The cog and the
// needs-you count stop their click, so reaching for either is never also a
// switch. A div with a button role and not a button, because it holds other
// controls and a button inside a button is not markup a browser honours.
function roomTileHTML(room, snap, on) {
  const name = esc(room.name);
  const q = name.replace(/'/g, "&#39;");
  const key = "r." + name;
  const cards = roomCards(room.name);
  const a = agentCounts(cards);
  const s = snap || {};
  const m = s.machine;
  const hot = a.needs_you > 0 || (m && m.cpu_pct > 90);

  const meta = [];
  if (room.os) meta.push(esc(room.os));
  const started = s.process && Date.parse(s.process.started_at || "");
  if (started) meta.push(`${esc(ago(Math.max(0, (Date.now() - started) / 1000)))} up`);
  if (room.version) meta.push(`build ${esc(room.version)}`);
  const hub = roomsHubBuild();
  const notHub = hub && room.version && room.version !== hub
    ? `<span class="chip warn rd-build" data-tip="${esc(`this room runs build ${room.version}, the hub runs ${hub}`)}"
        >not the hub's build</span>` : "";

  return `<div class="rtile${on ? " on" : ""}${hot ? " hot" : ""}" data-room="${name}" role="button" tabindex="0"
    onclick="pickRoom('${q}')" onkeydown="rtileKey(event,'${q}')">
    <div class="rt-head">
      <span class="dot live"></span><strong>${name}</strong>
      <span class="rt-meta" data-tip="${meta.join(" &middot; ")}">${meta.join(" &middot; ")}</span>
      ${notHub}${typeof transportBadge === "function" ? transportBadge(room.transport) : ""}
      <span class="grow"></span>
      <span class="roomcog" role="button" aria-label="settings for ${name}" data-tip="settings for ${name}"
        onclick="event.stopPropagation();openRoomCog('${q}')">&#9881;</span>
    </div>
    <div class="rt-bands">
      ${agentsBand(key, q, a, s.runners)}
      ${tokensBand(key, s.tokens, cards)}
      ${machineBand(key, m, s.disk)}
    </div></div>`;
}

// allRoomsTileHTML is the board-wide total, drawn above the rooms.
//
// Agents and tokens are summed across the latest snapshot per live room. The
// limit bars are the highest across rooms, never summed. The third band says
// how many rooms there are and how many are not on the hub's build.
function allRoomsTileHTML(rooms, on) {
  const cards = roomCards(null);
  const a = agentCounts(cards);
  const snaps = rooms.map(r => roomStats[r.name]).filter(s => s && s.tokens);
  let tok = null;
  if (snaps.length) {
    const len = Math.max(...snaps.map(s => (s.tokens.series_per_min || []).length));
    const series = new Array(len).fill(0);
    // Summed from the newest end, so rooms with shorter series still line up on now.
    for (const s of snaps) {
      const sp = s.tokens.series_per_min || [];
      for (let i = 0; i < sp.length; i++) series[len - sp.length + i] += Number(sp[i]) || 0;
    }
    const sum = f => snaps.reduce((n, s) => n + ((s.tokens.last_24h || {})[f] || 0), 0);
    tok = {
      per_min_5m: snaps.reduce((n, s) => n + (s.tokens.per_min_5m || 0), 0),
      series_per_min: series,
      last_24h: { in: sum("in"), out: sum("out"), cache_read: sum("cache_read"), cache_write: sum("cache_write") },
    };
  }
  const hub = roomsHubBuild();
  const off = hub ? rooms.filter(r => r.version && r.version !== hub).length : 0;
  const away = (typeof hubInventory !== "undefined" ? hubInventory : []).filter(r =>
    !r.attached && r.first_seen && r.transport !== "local").length;
  let busiest = null;
  for (const r of rooms) {
    const m = (roomStats[r.name] || {}).machine;
    if (m && typeof m.cpu_pct === "number" && (!busiest || m.cpu_pct > busiest.cpu)) busiest = { name: r.name, cpu: m.cpu_pct };
  }
  return `<div class="rtile all${on ? " on" : ""}${a.needs_you ? " hot" : ""}" role="button" tabindex="0"
    onclick="pickRoom('')" onkeydown="rtileKey(event,'')">
    <div class="rt-head">
      <span class="dot ghost"></span><strong>all rooms</strong>
      <span class="rt-meta">the board-wide total</span>
    </div>
    <div class="rt-bands">
      ${agentsBand("all", "", a, undefined)}
      ${tokensBand("all", tok, cards)}
      <div class="rd-band rd-rooms"><div class="rd-h">rooms</div>
        <div class="rd-row"><span><b class="rd-big" data-v="all.live" data-n="${rooms.length}">${rooms.length}</b> live</span>
          <span><b data-v="all.away" data-n="${away}">${away}</b> away</span></div>
        <div class="rd-row${off ? " rd-warn" : ""}"><span><b data-v="all.offbuild" data-n="${off}">${hub ? off : "&mdash;"}</b> not the hub's build</span></div>
        <div class="rd-row"><span>busiest ${busiest
          ? `<b data-v="all.busy">${esc(busiest.name)} ${Math.round(busiest.cpu)}%</b>` : RD_DASH}</span></div>
      </div>
    </div></div>`;
}

// Enter and space on a focused tile do what a click does.
function rtileKey(e, name) {
  if (e.target !== e.currentTarget) return;
  if (e.key !== "Enter" && e.key !== " ") return;
  e.preventDefault();
  pickRoom(name);
}

// roomNeedsYou opens a room's stack on what is waiting, from its needs-you count.
//
// The stack's filter is set to the two waiting columns, then the board goes to
// that room, or straight to the stack if it is already there. The all-rooms
// tile passes no name, which is the whole board.
function roomNeedsYou(name) {
  const menu = document.getElementById("rooms-menu");
  // The preview writes nothing real, so it only switches.
  if (!roomsDemoLive) {
    try {
      localStorage.setItem(STACK_SHOW_KEY, JSON.stringify(["needs-permission", "needs-input"]));
      localStorage.setItem("atrium.view", "stack");
    } catch (e) {}
  }
  if (roomNow() === name) {
    if (menu) menu.hidden = true;
    if (typeof paintStackShow === "function") { paintStackShow(); paintStack(); }
    if (typeof switchView === "function") switchView("stack");
    return;
  }
  pickRoom(name);
}

// ── the preview: `?demo=rooms` ──────────────────────────────────────────────
//
// Everything below runs only when `roomsDemoLive` is set, which `startRooms`
// does once, for a board page opened with `?demo=rooms`. It is a pretend hub
// with four made-up rooms, made-up cards on three of them, and a generator that
// emits one snapshot per room every two seconds through `onRoomStats`.
//
// THE ONE TIMER IS THE STAND-IN FOR THE EVENT. The real board has no timer
// here: the room pushes `room-stats` and the stream's listener calls the same
// function this does.
//
// Time is compressed. Each tick shifts the series by one point, which in the
// real event is one minute, so the lines move while you watch.

const ROOMS_DEMO_HUB_BUILD = "a91c2de";
const ROOMS_DEMO_SET = [
  { name: "sg3", host: "sg3", os: "windows", transport: "direct", version: "3b578f0",
    up: 4 * 3600 + 12 * 60, cpu: 71, mem: 32e9, used: 21e9, disk: 512e9, free: 392e9, rate: 41000,
    path: "D:\\git", worktrees: 38, cards: { running: 7, idle: 2, asks: 1, done: 12 },
    runners: [["claude", true], ["codex", true], ["ollama", false]] },
  { name: "sg4", host: "sg4", os: "windows", transport: "ziti", version: ROOMS_DEMO_HUB_BUILD,
    up: 26 * 3600 + 3 * 60, cpu: 88, mem: 64e9, used: 49e9, disk: 1000e9, free: 318e9, rate: 72000,
    path: "E:\\worktrees", worktrees: 61, cards: { running: 11, idle: 1, perms: 1, done: 23 },
    runners: [["claude", true], ["codex", true], ["gemini", true]] },
  // THE SECOND WAVE HAS NOT SHIPPED HERE, so there is no `machine` and the
  // machine band shows the dash path.
  { name: "m1mini", host: "m1mini.local", os: "darwin", transport: "zrok", version: ROOMS_DEMO_HUB_BUILD,
    up: 3 * 86400 + 5 * 3600, noMachine: true, disk: 256e9, free: 88e9, rate: 9000,
    path: "/Users/claude/git", worktrees: 14, cards: { running: 2, idle: 3, done: 5 },
    runners: [["claude", true], ["ollama", true]] },
];
const ROOMS_DEMO_AWAY = { name: "lab-pc", host: "lab-pc", os: "linux", transport: "direct",
  version: "71d0e44", away: 3 * 3600 + 20 * 60 };

let roomsDemoCards = [];
let roomsDemoTimer = null;
let roomsDemoTicks = 0;
const roomsDemoForgot = new Set();
const roomsDemoState = {};

// A small random walk, held inside a band.
function demoWalk(v, step, lo, hi) {
  return Math.max(lo, Math.min(hi, v + (Math.random() * 2 - 1) * step));
}

// roomsDemoStart builds the rooms, their cards and their series, and starts
// the ticking stand-in for the event.
function roomsDemoStart() {
  if (roomsDemoTimer) return;
  const now = Date.now();
  const iso = ms => new Date(ms).toISOString();
  for (const r of ROOMS_DEMO_SET) {
    const rate = [], cpu = [];
    let x = r.rate, c = r.cpu || 0;
    for (let i = 0; i < 60; i++) {
      x = demoWalk(x, r.rate * 0.12, r.rate * 0.35, r.rate * 1.8);
      rate.push(Math.round(x));
      c = demoWalk(c, 6, 8, 99);
      cpu.push(Math.round(c));
    }
    roomsDemoState[r.name] = { rate, cpu, used: r.used || 0, free: r.free,
      day: { in: r.rate * 260, out: r.rate * 34, cache_read: r.rate * 1500, cache_write: r.rate * 70 } };
    const add = (n, status, over) => {
      for (let i = 0; i < (n || 0); i++) {
        roomsDemoCards.push(Object.assign({
          id: `demo-${r.name}-${status}-${i}`, room: r.name, status, display_title: `${r.name} ${status} ${i + 1}`,
          last_activity_at: iso(now - (i + 1) * 37 * 60000),
        }, over || {}));
      }
    };
    add(r.cards.running, "running");
    add(r.cards.idle, "needs-input");
    add(r.cards.asks, "needs-input", { seen: { answered: false, open_questions: ["land it?"] } });
    add(r.cards.perms, "needs-permission");
    add(r.cards.done, "done");
  }
  // Limit reports on a few cards, the way a statusline posts them: sg3's cards
  // run near the five-hour line, m1mini reports only the week.
  const tele = (id, five, week, age) => {
    const t = roomsDemoCards.find(c => c.id === id);
    if (!t) return;
    t.telemetry = { seconds: age, five_hour: five == null ? undefined : { pct: five, resets_at: iso(now + 97 * 60000) },
      weekly: week == null ? undefined : { pct: week, resets_at: iso(now + 3 * 86400000) } };
  };
  tele("demo-sg3-running-0", 64, 31, 40);
  tele("demo-sg3-running-3", 58, 30, 300);
  tele("demo-sg4-running-1", 82, 47, 90);
  tele("demo-m1mini-running-0", null, 22, 600);
  roomsDemoTick();
  roomsDemoTimer = setInterval(roomsDemoTick, 2000);
}

// roomsDemoTick moves everything a little and emits one snapshot per live room.
function roomsDemoTick() {
  roomsDemoTicks++;
  const now = Date.now();
  const end = new Date(Math.floor(now / 60000) * 60000).toISOString();
  for (const r of ROOMS_DEMO_SET) {
    if (roomsDemoForgot.has(r.name)) continue;
    const st = roomsDemoState[r.name];
    const lastRate = st.rate[st.rate.length - 1];
    st.rate.push(Math.round(demoWalk(lastRate, r.rate * 0.15, r.rate * 0.35, r.rate * 1.8)));
    st.rate.shift();
    // sg4 runs hot, so the pulse shows. The others wander in the middle.
    const lastCpu = st.cpu[st.cpu.length - 1];
    st.cpu.push(Math.round(demoWalk(lastCpu, 7, r.name === "sg4" ? 78 : 8, 99)));
    st.cpu.shift();
    st.used = demoWalk(st.used, (r.mem || 0) * 0.01, (r.mem || 0) * 0.4, (r.mem || 0) * 0.92);
    st.free = Math.max(1e9, st.free - Math.random() * 2e8);
    const perMin = Math.round(st.rate.slice(-5).reduce((n, v) => n + v, 0) / 5);
    st.day.in += perMin * 0.78; st.day.out += perMin * 0.1; st.day.cache_write += perMin * 0.12;
    st.day.cache_read += perMin * 5.8;
    const round = o => Object.fromEntries(Object.entries(o).map(([k, v]) => [k, Math.round(v)]));
    const day = round(st.day);
    const snap = {
      v: 1,
      room: r.name,
      at: new Date(now).toISOString(),
      tokens: {
        per_min_5m: perMin,
        hour: round({ in: perMin * 47, out: perMin * 6, cache_read: perMin * 350, cache_write: perMin * 7 }),
        last_24h: day,
        by_cause_24h: round({ operator: day.in * 0.7, peer: day.in * 0.26, keepalive: day.in * 0.04 }),
        series_per_min: st.rate.slice(),
        series_end: end,
      },
      process: { cpu_pct: Math.round(demoWalk(3, 2, 0.5, 9) * 10) / 10, rss_bytes: 212000000, heap_bytes: 88000000,
        goroutines: 412, started_at: new Date(now - r.up * 1000).toISOString() },
      disk: { path: r.path, free_bytes: Math.round(st.free), total_bytes: r.disk, db_bytes: 740000000,
        worktrees: r.worktrees },
      runners: r.runners.map(([kind, resolves]) => ({ kind, resolves })),
    };
    if (!r.noMachine) {
      snap.machine = { cpu_pct: st.cpu[st.cpu.length - 1], mem_used_bytes: Math.round(st.used),
        mem_total_bytes: r.mem, cpu_series_pct: st.cpu.slice() };
    }
    roomsDemoDrift(r.name);
    onRoomStats(snap);
  }
}

// roomsDemoDrift now and then moves one card, so the agent counts change too.
function roomsDemoDrift(room) {
  if (Math.random() > 0.3) return;
  const cards = roomsDemoCards.filter(t => t.room === room && t.status !== "done");
  const t = cards[Math.floor(Math.random() * cards.length)];
  if (!t) return;
  if (t.status === "running") {
    t.status = "needs-input";
    t.seen = Math.random() < 0.3 ? { answered: false, open_questions: ["merge now?"] } : undefined;
  } else {
    t.status = "running";
    t.seen = undefined;
  }
  t.last_activity_at = new Date().toISOString();
  for (const tt of roomsDemoCards) {
    if (tt.telemetry && tt.telemetry.five_hour && tt.room === room) {
      tt.telemetry.five_hour.pct = Math.min(100, tt.telemetry.five_hour.pct + (Math.random() < 0.5 ? 1 : 0));
    }
  }
}

// roomsDemoHub is `loadHubRooms` for the pretend hub: the rooms from memory,
// the chip and the open menu repainted, and nothing asked of anybody.
function roomsDemoHub() {
  const since = new Date(Date.now() - 3600000).toISOString();
  hubIsHub = true;
  hubRooms = ROOMS_DEMO_SET.filter(r => !roomsDemoForgot.has(r.name)).map(r => ({
    name: r.name, host: r.host, os: r.os, transport: r.transport, version: r.version, since }))
    .sort((a, b) => String(a.name).localeCompare(String(b.name)));
  const away = roomsDemoForgot.has(ROOMS_DEMO_AWAY.name) ? [] : [Object.assign({}, ROOMS_DEMO_AWAY, {
    attached: false, first_seen: "2026-09-01T09:00:00Z",
    last_seen: new Date(Date.now() - ROOMS_DEMO_AWAY.away * 1000).toISOString() })];
  hubInventory = hubRooms.map(r => Object.assign({ attached: true, first_seen: since }, r)).concat(away)
    .sort((a, b) => String(a.name).localeCompare(String(b.name)));
  attachedRoomKey = hubRooms.map(r => r.name).join("\n");
  paintRooms();
  refreshRoomsMenu();
  return true;
}

// roomsDemoForget is the preview's `x`: the room leaves the pretend hub.
function roomsDemoForget(name) {
  roomsDemoForgot.add(name);
}
