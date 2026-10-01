// The usage tab: what every card has spent over time, drawn by hand as inline SVG.
//
// The room sums its session_usage rows into buckets (GET /v1/usage, see
// internal/api/usage.go) and says so again over the event stream as each row is
// written (the `usage` event). Nothing here is recorded anywhere. There is no
// chart library on purpose: the board works offline and has no build step.
//
// TWO ROOMS CAN HOLD THE SAME CARD ID. Every per-card key here is the room, a
// bar and the bare id, never the id or the title alone, so a live event for one
// room's card cannot land on the other's. A room that is not attached, too old
// to have the endpoint, or silent is NAMED in the tab. It is never a zero.
//
// Colours are skin variables, in the stylesheet (.uck-*), so a skin change
// recolours a chart already drawn.

const UC_RANGES = { "1h": [3600, 60], "6h": [21600, 300], "24h": [86400, 900], "7d": [604800, 3600] };
const UC_KINDS = [
  ["input", "uncached in", "input", "uck-in"],
  ["output", "out", "output", "uck-out"],
  ["cache_read", "cache read", "read", "uck-read"],
  ["cache_write_5m", "cache write 5m", "write5m", "uck-w5"],
  ["cache_write_1h", "cache write 1h", "write1h", "uck-w1"],
];
const UC_TOP_CARDS = 12;
const UC_USAGE_BUILD = "39a8da1"; // the first room build with /v1/usage

const UC = {
  range: "24h",
  room: "",
  card: null,      // {room, id} or null
  rooms: {},       // name -> {state: ok|old|down|absent, why, buckets: Map(startMs -> bucket)}
  since: 0,        // ms
  bw: 900,         // seconds
  loading: 0,      // load sequence, so a slow answer never paints over a newer one
  inflight: 0,
  pending: [],     // events that arrived while a load was in flight
  paintTimer: 0,
  cacheReads: false, // the daemon setting usage_cache_reads: cache reads drawn in the charts
  cacheKnown: false, // whether the daemon has been asked once
  live: false,       // the paint now is from a live usage event, so changes are animated
  seen: new Map(),   // number key -> text as last painted, to find what changed
  bars: new Set(),   // burn bars already drawn, so only a new one grows
  kaCards: new Set(), // cards a live keep-alive row was seen on
};

function ucKey(room, id) { return room + "|" + id; }

function ucSums() {
  return { rows: 0, replies: 0, input: 0, output: 0, cache_write_5m: 0, cache_write_1h: 0, cache_read: 0, cost: 0 };
}

function ucAdd(a, b) {
  for (const k of Object.keys(a)) a[k] += Number(b && b[k]) || 0;
  return a;
}

function ucTokens(s) {
  return UC_KINDS.reduce((n, k) => n + (Number(s && s[k[0]]) || 0), 0);
}

// Counted tokens: uncached in, out and both cache writes. Cache reads are reported
// apart. The one place the rule lives; the rooms dashboard calls it too.
function ucCounted(s) {
  return UC_KINDS.reduce((n, k) => k[0] === "cache_read" ? n : n + (Number(s && s[k[0]]) || 0), 0);
}

// What the charts add up: counted tokens, or all five kinds with the toggle on.
function ucShown(s) { return UC.cacheReads ? ucTokens(s) : ucCounted(s); }

// The kinds a chart stacks.
function ucKindsShown() { return UC.cacheReads ? UC_KINDS : UC_KINDS.filter(k => k[0] !== "cache_read"); }

function ucTitle(room, id) {
  const t = (typeof lastTasks !== "undefined" ? lastTasks : []).find(x =>
    bareId(x.id) === id && (!room || (x.room || roomOf(x.id) || roomNow()) === room));
  return (t && t.display_title) || id.slice(0, 8);
}

// Which rooms to ask, and which to name as not askable.
function ucTargets() {
  const one = roomNow();
  if (one) return { ask: [one], absent: [] };
  if (UC.room) return { ask: [UC.room], absent: [] };
  if (typeof hubIsHub === "undefined" || !hubIsHub) return { ask: [""], absent: [] };
  const attached = hubRooms.map(r => r.name);
  const absent = (hubInventory || []).filter(r =>
    !attached.includes(r.name) && r.transport !== "local").map(r => r.name);
  return { ask: attached, absent };
}

async function ucFetchRoom(room, since, bw, card, group) {
  const q = `/v1/usage?since=${encodeURIComponent(new Date(since).toISOString())}&bucket=${bw}` +
    (card ? "&card=" + encodeURIComponent(card) : "") + (group ? "&group=" + group : "");
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), 15000);
  try {
    const res = await plainFetch(q, { headers: room ? { "X-Atrium-Room": room } : {}, signal: ctrl.signal });
    if (res.status === 404) return { state: "old", why: "too old to keep usage" };
    if (!res.ok) return { state: "down", why: "did not answer" };
    return { state: "ok", series: await res.json() };
  } catch (e) {
    return { state: "down", why: "did not answer" };
  } finally {
    clearTimeout(timer);
  }
}

function ucIngest(series, bw) {
  const m = new Map();
  const ms = bw * 1000;
  for (const b of (series && series.buckets) || []) {
    const t = Math.floor(Date.parse(b.t) / ms) * ms;
    let cur = m.get(t);
    if (!cur) m.set(t, cur = { t, total: ucSums(), cards: {}, causes: {} });
    ucAdd(cur.total, b.total);
    ucIngestGroups(cur, b);
    for (const [id, s] of Object.entries(b.cards || {})) ucAdd(cur.cards[id] || (cur.cards[id] = ucSums()), s);
    for (const [c, s] of Object.entries(b.causes || {})) ucAdd(cur.causes[c] || (cur.causes[c] = ucSums()), s);
  }
  return m;
}

async function loadUsageTab() {
  const [span, bw] = UC_RANGES[UC.range];
  UC.bw = bw;
  UC.since = Math.floor((Date.now() - span * 1000) / 1000) * 1000;
  const { ask, absent } = ucTargets();
  ucAskSetting();
  const seq = ++UC.loading;
  UC.inflight++;
  UC.lastRead = Date.now();
  const rooms = {};
  for (const r of absent) rooms[r] = { state: "absent", why: "not attached", buckets: new Map() };
  const asked = UC.card ? [UC.card.room] : ask;
  const got = await Promise.all(asked.map(r =>
    ucFetchRoom(r, UC.since, bw, UC.card ? UC.card.id : "", ucGroupParam()).then(x => [r, x])));
  UC.inflight--;
  if (seq !== UC.loading) return;
  for (const [r, x] of got) {
    rooms[r] = { state: x.state, why: x.why || "", buckets: x.series ? ucIngest(x.series, bw) : new Map(), noGroups: ucNoGroups(x.series) };
  }
  UC.rooms = rooms;
  if (typeof ulLoad === "function") ulLoad(asked);
  const held = UC.pending.splice(0);
  for (const e of held) ucApply(e);
  ucLoadItems(asked, UC.since);
  ucPaint();
}

// The newest bucket grows by the row, in the room it came from.
function onUsageEvent(e) {
  let d;
  try { d = JSON.parse(e.data); } catch (err) { return; }
  if (!d || !d.task_id) return;
  ucGroupStale();
  if (UC.inflight) { UC.pending.push(d); return; }
  if (!isViewing("usage")) return;
  ucApply(d);
  ucSchedulePaint();
}

function ucEventRoom(d) {
  return d.room || roomNow() ||
    (typeof hubIsHub !== "undefined" && hubIsHub && hubRooms.length === 1 ? hubRooms[0].name : "");
}

function ucApply(d) {
  const room = ucEventRoom(d);
  const id = d.room ? String(d.task_id).replace(d.room + "~", "") : bareId(d.task_id);
  if (UC.card && (UC.card.room !== room || UC.card.id !== id)) return;
  const r = UC.rooms[room];
  if (!r || r.state !== "ok") return;
  const ms = UC.bw * 1000;
  const at = Date.parse(d.ended_at) || Date.now();
  if (at < UC.since) return;
  const t = Math.floor(at / ms) * ms;
  let b = r.buckets.get(t);
  if (!b) r.buckets.set(t, b = { t, total: ucSums(), cards: {}, causes: {} });
  const row = {
    rows: 1, input: d.input, output: d.output, cache_write_5m: d.cache_write_5m,
    cache_write_1h: d.cache_write_1h, cache_read: d.cache_read,
  };
  ucAdd(b.total, row);
  ucAdd(b.cards[id] || (b.cards[id] = ucSums()), row);
  ucAdd(b.causes[d.cause || "unknown"] || (b.causes[d.cause || "unknown"] = ucSums()), row);
  if (d.cause === "keepalive") UC.kaCards.add(ucKey(room, id));
}

function ucSchedulePaint() {
  if (UC.paintTimer) return;
  UC.paintTimer = setTimeout(() => {
    UC.paintTimer = 0;
    if (!isViewing("usage")) return;
    UC.live = true;
    try { ucPaint(); } finally { UC.live = false; }
  }, 500);
}

// The stream came back, so what was missed while it was down is asked for.
function onUsageStreamOpen() {
  if (isViewing("usage")) loadUsageTab();
}

// ---- painting ----

function ucPaint() {
  const body = document.getElementById("uc-body");
  if (!body) return;
  for (const b of document.querySelectorAll("#uc-ranges button")) b.classList.toggle("on", b.dataset.range === UC.range);
  const cr = document.getElementById("uc-cache");
  if (cr) {
    cr.classList.toggle("on", UC.cacheReads);
    cr.setAttribute("aria-pressed", String(UC.cacheReads));
    const lab = document.getElementById("uc-cache-label");
    if (lab) lab.textContent = UC.cacheReads ? "cache reads shown" : "cache reads hidden";
    cr.setAttribute("data-tip", UC.cacheReads
      ? "Cache reads are drawn in the charts and counted in every total. Click to count them apart, since they are most of the input and swamp the rest"
      : "Cache reads are counted apart and left out of the charts and totals, since they are most of the input and swamp the rest. Click to draw them");
  }
  ucPaintChips();
  const names = Object.keys(UC.rooms);
  const okRooms = names.filter(n => UC.rooms[n].state === "ok");
  const missing = names.filter(n => UC.rooms[n].state !== "ok");
  const merged = new Map();
  const cardRows = new Map();   // key -> {room, id, sums}
  const causes = {};
  for (const n of okRooms) {
    for (const [t, b] of UC.rooms[n].buckets) {
      let m = merged.get(t);
      if (!m) merged.set(t, m = { t, total: ucSums(), rooms: {} });
      ucAdd(m.total, b.total);
      for (const [id, s] of Object.entries(b.cards)) {
        const k = ucKey(n, id);
        let c = cardRows.get(k);
        if (!c) cardRows.set(k, c = { room: n, id, sums: ucSums(), per: new Map() });
        ucAdd(c.sums, s);
        c.per.set(t, (c.per.get(t) || 0) + ucShown(s));
      }
      for (const [c, s] of Object.entries(b.causes)) ucAdd(causes[c] || (causes[c] = ucSums()), s);
    }
  }
  const series = [...merged.values()].sort((a, b) => a.t - b.t);
  const total = ucSums();
  for (const m of series) ucAdd(total, m.total);
  const cnt = document.getElementById("uc-count");
  cnt.textContent = total.rows ? `${total.rows} turns` : "";
  cnt.setAttribute("data-tip", "rows the room recorded in this range, one per prompt, keep-alive refresh or subagent turn");
  let html = "";
  for (const n of missing) {
    html += UC.rooms[n].state === "old" ? ucOldLine(n) :
      `<div class="ucmissing" data-room="${esc(n)}"><b>${esc(n || "this room")}</b> ${esc(UC.rooms[n].why)}. ` +
      `Its usage is not in these charts.</div>`;
  }
  if (!series.length) {
    html += `<div class="empty">${okRooms.length ? "No turn ended in this range." : "Nothing to draw."}</div>` + ulSection();
    body.innerHTML = html;
    ucAfterPaint(body);
    return;
  }
  html += ucLegend();
  html += `<h4 class="uch">burn rate <span class="ucnote">${UC.cacheReads ? "" : "counted "}tokens per minute, stacked by kind</span></h4>` +
    ucBurn(series) +
    ucCardsHead() + ucGroups(series) + ucCards(ucGroupFilter(cardRows), series) +
    `<h4 class="uch">tokens by kind</h4>` + ucSplit(total, causes, cardRows) +
    (UC.cumSeries = series, ucCumulative(series)) +
    `<h4 class="uch">tokens by cause</h4>` + ucCauseTable(causes) + ulSection() + ucItems();
  body.innerHTML = html;
  ucAfterPaint(body);
}

function ucPaintChips() {
  const sel = document.getElementById("uc-room");
  if (sel) {
    const show = typeof hubIsHub !== "undefined" && hubIsHub && !roomNow();
    sel.hidden = !show;
    if (show) {
      const opts = [""].concat(hubRooms.map(r => r.name));
      sel.innerHTML = opts.map(n => `<option value="${esc(n)}"${n === UC.room ? " selected" : ""}>${n ? esc(n) : "all rooms"}</option>`).join("");
    }
  }
  const chip = document.getElementById("uc-card");
  if (chip) {
    chip.hidden = !UC.card;
    if (UC.card) {
      chip.innerHTML = `${UC.card.room ? `<span class="ucroom">${esc(UC.card.room)}</span> ` : ""}` +
        `<b>${esc(ucTitle(UC.card.room, UC.card.id))}</b> <button data-clear="1" data-tip="Show every card" aria-label="Show every card">×</button>`;
    }
  }
}

function ucLegend() {
  return `<div class="uclegend">` + UC_KINDS.map(k =>
    `<span class="uctip" data-tip="${esc(USAGE_TIPS[k[2]])}"><i class="${k[3]}"></i>${k[1]}</span>`).join("") + `</div>`;
}

function ucFmtWhen(ms) {
  const d = new Date(ms);
  const hm = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  return UC.range === "7d" ? d.toLocaleDateString([], { month: "short", day: "numeric" }) + " " + hm : hm;
}

// The whole range on one axis, so a quiet stretch reads as a gap.
// The clock. A test sets UC.now so a fixture never depends on the real time of day.
function ucNow() { return UC.now || Date.now(); }

function ucAxis(series, now) {
  const ms = UC.bw * 1000;
  const first = Math.floor(UC.since / ms) * ms;
  const last = Math.floor((now || ucNow()) / ms) * ms;
  return { first, n: Math.max(1, Math.round((last - first) / ms) + 1), ms };
}

function ucBurn(series) {
  const ax = ucAxis(series);
  const per = UC.bw / 60;
  const byT = new Map(series.map(s => [s.t, s]));
  let peak = 1;
  for (const s of series) peak = Math.max(peak, ucShown(s.total) / per);
  const W = 600, H = 150, bw = W / ax.n;
  let bars = "";
  for (let i = 0; i < ax.n; i++) {
    const s = byT.get(ax.first + i * ax.ms);
    if (!s) continue;
    let y = H;
    let segs = "";
    for (const k of ucKindsShown()) {
      const h = (Number(s.total[k[0]]) || 0) / per / peak * (H - 4);
      if (h <= 0) continue;
      y -= h;
      segs += `<rect class="${k[3]}" x="${(i * bw).toFixed(2)}" y="${y.toFixed(2)}" width="${Math.max(bw - 0.6, 0.4).toFixed(2)}" height="${h.toFixed(2)}"></rect>`;
    }
    bars += `<g data-t="${s.t}">${segs}</g>`;
  }
  const band = ulBand(ax, W, H);
  return `<div class="ucchart" data-chart="burn">${band.label}<svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none" role="img" ` +
    `aria-label="tokens per minute over time">${band.svg}${bars}</svg>` +
    `<div class="ucaxis"><span>${ucFmtWhen(ax.first)}</span><span data-n="peak" data-tip="${esc(USAGE_TIPS.peak)}">peak ${usageTokens(Math.round(peak))}/min</span>` +
    `<span>${ucFmtWhen(ax.first + (ax.n - 1) * ax.ms)}</span></div>` +
    `<div class="ucread" aria-live="off">hover a bar</div></div>`;
}

// The next local midnight after `now`. A day that is 23 or 25 hours long is the Date's business, not ours.
function ucMidnight(now) {
  const d = new Date(now);
  d.setHours(24, 0, 0, 0);
  return d.getTime();
}

// The limit the cumulative chart is read against: the 5h window on the day ranges, the weekly one on 7d. The
// card that reported the highest percentage sets it. Its tokens per point come from the counted tokens the
// chart holds for that window, so 100% is the tokens one point took times a hundred. Null with no reading,
// and null when the chart does not hold the whole account's window: a 1h range, a card filter, cache reads shown.
const UC_LIMIT_NAME = { five_hour: "5h", weekly: "weekly" };
function ucLimitFor(series, now) {
  if (typeof ulGroups !== "function" || UC.cacheReads) return null;
  const kind = UC.range === "7d" ? "weekly" : "five_hour";
  const gs = ulGroups(kind, now).filter(g => g.reset > now && g.best.pct > 0);
  if (!gs.length) return null;
  const g = gs.reduce((a, b) => b.best.pct > a.best.pct ? b : a);
  const start = g.reset - UL_WINDOW[kind], ms = UC.bw * 1000;
  if (ulTokensIn(start, now) == null) return null;
  let tok = 0;
  for (const s of series) {
    const o = Math.min(now, s.t + ms) - Math.max(start, s.t);
    if (o > 0) tok += ucShown(s.total) * o / ms;
  }
  return tok > 0 ? { kind, name: UC_LIMIT_NAME[kind], reset: g.reset, start, pct: g.best.pct, perPct: tok / g.best.pct } : null;
}

// Where the time axis puts its labels: hours on the day ranges, local midnights on the week.
function ucTicks(first, end) {
  const out = [], d = new Date(first);
  if (UC.range === "7d") {
    d.setHours(0, 0, 0, 0);
    while (d.getTime() < first) d.setDate(d.getDate() + 1);
    for (; d.getTime() <= end; d.setDate(d.getDate() + 1)) out.push([d.getTime(), d.toLocaleDateString([], { weekday: "short", day: "numeric" })]);
    return out;
  }
  const step = { "1h": 10, "6h": 60, "24h": 180 }[UC.range] || 180;
  if (step < 60) d.setMinutes(Math.floor(d.getMinutes() / step) * step, 0, 0); else d.setMinutes(0, 0, 0);
  const ok = t => step < 60 || t.getHours() % (step / 60) === 0;
  while (d.getTime() < first || !ok(d)) d.setTime(d.getTime() + (step < 60 ? step : 60) * 60000);
  for (; d.getTime() <= end; d.setTime(d.getTime() + step * 60000)) out.push([d.getTime(), ulPad(d.getHours()) + ":" + ulPad(d.getMinutes())]);
  return out;
}

// The running total of what the charts add up, oldest on the left, following the toggle. On 24h and 7d a
// dashed line carries on from now to the coming midnight at the pace of the last hour. It says "at this
// pace" because it does not see the future, and it is left out when the last hour was empty.
// When a card has reported a limit the y axis is percent of that limit, with the 100% line, the time the
// projection crosses it and the resets, and the line then runs on to the reset of the window. UC.cum keeps
// what the hover needs.
function ucCumulative(series, now) {
  now = now || ucNow();
  const ax = ucAxis(series, now);
  const byT = new Map(series.map(s => [s.t, s]));
  const pts = [];
  let run = 0;
  for (let i = 0; i < ax.n; i++) {
    const s = byT.get(ax.first + i * ax.ms);
    if (!s) continue;
    run += ucShown(s.total);
    pts.push([ax.first + (i + 1) * ax.ms, run]);
  }
  const nowMs = Math.min(now, ax.first + ax.n * ax.ms);
  // The pace: whole buckets from the one holding an hour ago up to now, over the time they cover.
  const from = Math.floor((now - 3600000) / ax.ms) * ax.ms;
  let hour = 0;
  for (const s of series) if (s.t >= from) hour += ucShown(s.total);
  const rate = hour / Math.max(now - from, 1);
  const lim = ucLimitFor(series, now);
  const midnight = ucMidnight(now);
  const project = lim ? hour > 0 && lim.reset > now : (UC.range === "24h" || UC.range === "7d") && hour > 0 && midnight > now;
  const end = project ? (lim ? lim.reset : midnight) : (lim ? Math.max(nowMs, lim.reset) : nowMs);
  const span = Math.max(end - ax.first, 1);
  const W = 600, H = 100;
  const X = t => (Math.min(t, end) - ax.first) / span * W;
  const proj = project ? run + rate * (end - now) : run;
  let Y, ymax = 0, base = 0, pctRate = 0;
  if (lim) {
    base = run - lim.pct * lim.perPct;
    pctRate = rate / lim.perPct;                       // points per ms
    ymax = Math.max(120, Math.ceil((lim.pct + 10) / 10) * 10);
    Y = v => H - 2 - Math.max(0, Math.min(ymax, (v - base) / lim.perPct)) / ymax * (H - 4);
  } else {
    Y = v => H - 2 - v / (proj || 1) * (H - 4);
  }
  // In percent the line is the window's own, so it starts where the window did and not at the range's edge.
  let d = lim ? `M${X(lim.start).toFixed(2)} ${Y(base).toFixed(2)}` : `M0 ${Y(0).toFixed(2)}`;
  for (const [t, v] of pts) if (!lim || t > lim.start) d += ` L${X(Math.min(t, nowMs)).toFixed(2)} ${Y(v).toFixed(2)}`;
  let svg = "", over = "", yax = "", phrase = "";
  const left = t => ((t - ax.first) / span * 100).toFixed(2) + "%";
  if (lim) {
    for (const p of [0, 25, 50, 75, 100].concat(ymax > 100 ? [ymax] : [])) {
      const y = Y(base + p * lim.perPct);
      svg += `<line class="${p === 100 ? "ulcap" : "ulgrid"}" x1="0" x2="${W}" y1="${y.toFixed(2)}" y2="${y.toFixed(2)}"></line>`;
      yax += `<span class="ucy${p === 100 ? " ucy100" : ""}" data-pct="${p}" style="top:${(y / H * 100).toFixed(2)}%">${p}%<small> ${usageTokens(Math.round(p * lim.perPct))}</small></span>`;
    }
    // The resets of both windows: the coming one is named, the ones behind it are the same mark unnamed.
    for (const kind of Object.keys(UL_WINDOW)) {
      if (kind === "five_hour" && UC.range === "7d") continue;   // a mark every 5 hours across a week is a comb
      for (const g of ulGroups(kind, now)) {
        if (!g.reset) continue;
        for (let t = g.reset, k = 0; t >= ax.first; t -= UL_WINDOW[kind], k++) {
          if (t > end) continue;
          const x = X(t).toFixed(2);
          svg += `<line class="ulresetln" data-reset="${kind}" data-at="${t}" x1="${x}" x2="${x}" y1="0" y2="${H}"></line>`;
          if (k === 0 && t > now) over += `<span class="ulresetlab" data-reset="${kind}" style="left:${left(t)}">${UC_LIMIT_NAME[kind]} reset ${esc(ulClock(t, now))}</span>`;
        }
      }
    }
  }
  svg += `<path class="uck-line" d="${d}"></path>`;
  if (project) {
    let tEnd = end;
    if (lim && pctRate > 0) tEnd = Math.min(end, now + (ymax - lim.pct) / pctRate);
    svg += `<path class="uck-line uck-proj" d="M${X(nowMs).toFixed(2)} ${Y(run).toFixed(2)} L${X(tEnd).toFixed(2)} ${Y(run + rate * (tEnd - now)).toFixed(2)}"></path>`;
    if (lim) {
      const cross = lim.pct >= 100 ? now : now + (100 - lim.pct) / pctRate;
      if (pctRate > 0 && cross <= lim.reset) {
        phrase = `<span data-n="cumproj" data-cross="${Math.round(cross)}" class="ucwarn" data-tip="${esc(USAGE_TIPS.cumProj)}">hits ${lim.name} limit ${esc(ulClock(cross, now))}</span>`;
        svg += `<line class="ulcross" x1="${X(cross).toFixed(2)}" x2="${X(cross).toFixed(2)}" y1="0" y2="${H}"></line>`;
        over += `<span class="ulcrosslab" style="left:${left(cross)}">${esc(ulClock(cross, now))}</span>`;
      } else {
        const atReset = lim.pct + pctRate * (lim.reset - now);
        phrase = `<span data-n="cumproj" data-tip="${esc(USAGE_TIPS.cumProj)}">at this pace: ${Math.round(atReset)}% of the ${lim.name} limit at reset</span>`;
      }
    } else {
      phrase = `<span data-n="cumproj" data-tip="${esc(USAGE_TIPS.cumProj)}">at this pace: ${usageTokens(Math.round(proj))} by midnight</span>`;
    }
  }
  svg += `<line class="ucnowln" x1="${X(nowMs).toFixed(2)}" x2="${X(nowMs).toFixed(2)}" y1="0" y2="${H}"></line>`;
  over += `<span class="ucnowlab" style="left:${left(nowMs)}">now</span>`;
  const times = ucTicks(ax.first, end).map(([t, lab]) => `<span style="left:${left(t)}">${lab}</span>`).join("");
  UC.cum = { first: ax.first, end, nowMs, now, pts, run, rate, lim, base };
  return `<h4 class="uch">cumulative tokens <span class="ucnote">${UC.cacheReads ? "" : "counted, "}${lim ? `since the ${lim.name} window began, as percent of its limit` : "running total over the range"}</span></h4>` +
    `<div class="ucchart${lim ? " uclim" : ""}" data-chart="cumulative"><div class="ucplot">${yax}<svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none" role="img" ` +
    `aria-label="running total of tokens over time">${svg}</svg>${over}<i class="uchov" hidden></i><div class="uctimes">${times}</div></div>` +
    `<div class="ucaxis"><span>0</span>${phrase}<span data-n="cumtotal" data-tip="${esc(USAGE_TIPS.cumTotal)}">${usageTokens(lim ? Math.round(lim.pct * lim.perPct) : run)}</span></div>` +
    `<div class="ucread" aria-live="off">hover the line</div></div>`;
}

// A live reading moved the limit: the cumulative chart is drawn again in place.
function ucPaintCum() {
  const chart = document.querySelector("#uc-body .ucchart[data-chart=cumulative]");
  if (!chart || !UC.cumSeries) return;
  const tmp = document.createElement("div");
  tmp.innerHTML = ucCumulative(UC.cumSeries);
  const fresh = tmp.querySelector(".ucchart[data-chart=cumulative]");
  if (fresh) chart.replaceWith(fresh);
}

function ucCards(cardRows, series) {
  const ax = ucAxis(series);
  const rows = [...cardRows.values()].sort((a, b) => ucShown(b.sums) - ucShown(a.sums));
  const top = rows.slice(0, UC_TOP_CARDS);
  const rest = rows.slice(UC_TOP_CARDS);
  let peak = 0;
  const cell = (label, room, id, per, cost, other) => {
    let m = 0;
    for (const v of per.values()) m = Math.max(m, v);
    peak = Math.max(peak, m);
    return { label, room, id, per, cost, other };
  };
  const cells = top.map(c => cell(ucTitle(c.room, c.id), c.room, c.id, c.per, ucShown(c.sums), false));
  if (rest.length) {
    const per = new Map();
    let cost = 0;
    for (const c of rest) {
      cost += ucShown(c.sums);
      for (const [t, v] of c.per) per.set(t, (per.get(t) || 0) + v);
    }
    cells.push(cell(`${rest.length} others`, "", "", per, cost, true));
  }
  const W = 200, H = 40, bw = W / ax.n;
  let html = `<div class="ucminis">`;
  for (const c of cells) {
    let m = 0;
    for (const v of c.per.values()) m = Math.max(m, v);
    let bars = "";
    for (const [t, v] of c.per) {
      const i = Math.round((t - ax.first) / ax.ms);
      if (i < 0 || i >= ax.n || !(v > 0)) continue;
      const h = v / (m || 1) * (H - 2);
      bars += `<rect class="uck-bar" x="${(i * bw).toFixed(2)}" y="${(H - h).toFixed(2)}" width="${Math.max(bw - 0.4, 0.4).toFixed(2)}" height="${h.toFixed(2)}"></rect>`;
    }
    const attrs = c.other ? "" : ` data-room="${esc(c.room)}" data-id="${esc(c.id)}"`;
    html += `<div class="ucmini${c.other ? " other" : ""}"${attrs} data-key="${esc(ucKey(c.room, c.id))}">` +
      `<div class="ucminit"><span class="uctitle">${c.room && UC.rooms && Object.keys(UC.rooms).length > 1 ? `<span class="ucroom">${esc(c.room)}</span> ` : ""}${esc(c.label)}</span>` +
      `<b data-n="card:${esc(ucKey(c.room, c.id))}" data-tip="${esc(USAGE_TIPS.cardTotal)}">${usageTokens(c.cost)}</b></div>` +
      `<svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none">${bars}</svg></div>`;
  }
  return html + `</div>`;
}

function ucSplit(total, causes, cardRows) {
  const sum = ucShown(total) || 1;
  let bar = "", legend = "";
  for (const k of ucKindsShown()) {
    const v = Number(total[k[0]]) || 0;
    if (v > 0) bar += `<span class="${k[3]}" style="width:${(v / sum * 100).toFixed(2)}%" data-tip="${k[1]}: ${usageTokens(v)}"></span>`;
    legend += `<span class="uctip" data-tip="${esc(USAGE_TIPS[k[2]])}"><i class="${k[3]}"></i>${k[1]} <b data-n="kind:${k[0]}">${usageTokens(v)}</b></span>`;
  }
  let line = "";
  if (!UC.cacheReads) {
    const read = Number(total.cache_read) || 0;
    const all = read + ucCounted(total);
    const pct = all ? Math.round(read / all * 100) : 0;
    line = `<div class="uclegend uccacheline" data-tip="${esc(ucCacheTip())}">cache reads <b data-n="cacheread">${usageTokens(read)}</b> · not in these charts · ` +
      `${pct}% of input was served from the cache${ucKeepalivePhrase(causes, cardRows)}</div>`;
  }
  return `<div class="ucsplit">${bar}</div><div class="uclegend">${legend}</div>${line}`;
}

function ucCauseTable(causes) {
  const rows = Object.entries(causes).sort((a, b) => ucShown(b[1]) - ucShown(a[1]));
  if (!rows.length) return "";
  const dim = !UC.cacheReads;
  return `<div class="uccauses${dim ? " withread" : ""}">` + rows.map(([c, s]) =>
    `<span${USAGE_TIPS["cause_" + c] ? ` data-tip="${esc(USAGE_TIPS["cause_" + c])}"` : ""}>${esc(USAGE_CAUSES[c] || c)}</span>` +
    `<span data-n="ccount:${esc(c)}" data-tip="${esc(USAGE_TIPS.causeCount)}">${esc(usageCount(c, s))}</span>` +
    `<span data-n="ctok:${esc(c)}" data-tip="${esc(UC.cacheReads ? USAGE_TIPS.causeAll : USAGE_TIPS.causeCounted)}">${usageTokens(ucShown(s))}</span>` +
    (dim ? `<span class="uccread" data-n="cread:${esc(c)}" data-tip="${esc(USAGE_TIPS.read)}">${usageTokens(s.cache_read)} cache read</span>` : "")).join("") + `</div>`;
}

// ---- the too-old line, the cache and keep-alive line, and the polish rules ----

// The build of a room, as the hub lists it (the tab reads what the rooms pane already holds).
function ucRoomBuild(name) {
  const all = [].concat(typeof hubRooms !== "undefined" ? hubRooms : [],
    typeof hubInventory !== "undefined" && hubInventory ? hubInventory : []);
  const r = all.find(x => x.name === name && x.version);
  if (!r) return "";
  const v = String(r.version);
  const m = v.match(/\b[0-9a-f]{7,40}\b/);
  return m ? m[0].slice(0, 7) : v.slice(0, 12);
}

// A 404 from /v1/usage: the room's build predates the endpoint.
function ucOldLine(name) {
  const build = ucRoomBuild(name);
  return `<div class="ucmissing" data-room="${esc(name)}"><b>${esc(name || "this room")}</b> ` +
    `${build ? `build ${esc(build)} ` : ""}predates usage (needs ${UC_USAGE_BUILD} or later). ` +
    `Update the room to see its usage here.</div>`;
}

// The keep-alive phrase: what the refreshes counted, and how many cards they kept warm.
// The cards are those with a keep-alive row in this range: seen live, or in range and
// carrying refreshes on the card (the bucket read has no card by cause).
function ucKeepalivePhrase(causes, cardRows) {
  const ka = causes && causes.keepalive;
  if (!ka || !(ka.rows > 0)) return "";
  const warm = new Set([...UC.kaCards].filter(k => cardRows && cardRows.has(k)));
  for (const [k, c] of (cardRows || new Map())) {
    const t = (typeof lastTasks !== "undefined" ? lastTasks : []).find(x =>
      bareId(x.id) === c.id && (!c.room || (x.room || roomOf(x.id) || roomNow()) === c.room));
    if (t && t.keepalive && t.keepalive.refreshes > 0) warm.add(k);
  }
  const n = warm.size;
  return ` · <span data-n="kaphrase">keep-alive spent ${usageTokens(ucCounted(ka))} counted and kept ${n} card${n === 1 ? "" : "s"} warm</span>`;
}

// The hint. The board holds no break-even figure (the daemon keeps each card's budget in
// its own keepalive rows and sends the board only refreshes and state), so it stays a definition.
function ucCacheTip() {
  return "cache reads are input tokens read back from the prompt cache, apart from the counted tokens. " +
    "the percentage is cache read over cache read plus uncached in plus cache write. " +
    "keep-alive is the refreshes that keep a card's cache warm while it is idle, counted the same way";
}

// After each paint: fade the numbers that changed and grow the bars that are new, on a live
// event only. A load, a range change or a toggle is a redraw, never an animation.
function ucAfterPaint(body) {
  const live = UC.live;
  const seen = new Map();
  for (const el of body.querySelectorAll("[data-n]")) {
    const k = el.getAttribute("data-n"), v = el.textContent;
    seen.set(k, v);
    if (live && UC.seen.has(k) && UC.seen.get(k) !== v) el.classList.add("ucfade");
  }
  UC.seen = seen;
  const bars = new Set();
  for (const g of body.querySelectorAll(".ucchart[data-chart=burn] g[data-t]")) {
    const k = UC.range + "|" + UC.room + "|" + g.dataset.t;
    bars.add(k);
    if (live && UC.bars.size && !UC.bars.has(k)) g.classList.add("ucgrow");
  }
  UC.bars = bars;
}

// ---- the card's own small chart, in its details ----

async function paintCardUsageChart(t) {
  const box = document.getElementById("d-usage-chart");
  if (!box || !t) return;
  const room = t.room || roomOf(t.id) || roomNow();
  const id = bareId(t.id);
  const since = Math.floor((Date.now() - 86400000) / 1000) * 1000;
  const got = await ucFetchRoom(room, since, 900, id);
  if (!current || current.id !== t.id) return;
  if (got.state !== "ok") { box.innerHTML = `<div class="ucnote">24h chart: ${esc(got.why)}</div>`; return; }
  const m = ucIngest(got.series, 900);
  const first = Math.floor(since / 900000) * 900000;
  const n = 97;
  let peak = 1;
  for (const b of m.values()) peak = Math.max(peak, ucShown(b.total));
  const W = 300, H = 40, bw = W / n;
  let bars = "";
  for (const b of m.values()) {
    const i = Math.round((b.t - first) / 900000);
    if (i < 0 || i >= n) continue;
    let y = H;
    for (const k of ucKindsShown()) {
      const h = (Number(b.total[k[0]]) || 0) / peak * (H - 2);
      if (h <= 0) continue;
      y -= h;
      bars += `<rect class="${k[3]}" x="${(i * bw).toFixed(2)}" y="${y.toFixed(2)}" width="${Math.max(bw - 0.3, 0.3).toFixed(2)}" height="${h.toFixed(2)}"></rect>`;
    }
  }
  box.innerHTML = m.size
    ? `<svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none">${bars}</svg>` +
      `<div class="ucnote">last 24h · <a href="#" data-openusage="1">open in the usage tab</a></div>`
    : `<div class="ucnote">no turn in the last 24h</div>`;
  const a = box.querySelector("[data-openusage]");
  if (a) a.onclick = ev => {
    ev.preventDefault();
    const dlg = document.getElementById("detail");
    if (dlg && dlg.close) dlg.close();
    UC.card = { room, id };
    switchView("usage");
  };
}

// ---- the cache reads toggle: a daemon setting, asked for once and told by the settings event ----

function ucHaveSetting(s) {
  if (!s || typeof s.usage_cache_reads !== "boolean") return;
  UC.cacheKnown = true;
  if (s.usage_cache_reads === UC.cacheReads) return;
  UC.cacheReads = s.usage_cache_reads;
  if (typeof isViewing === "function" && isViewing("usage")) ucPaint();
}

async function ucAskSetting() {
  if (UC.cacheKnown) return;
  UC.cacheKnown = true;
  try { ucHaveSetting(await api("/v1/settings")); } catch (e) { /* stays off */ }
}

async function ucSetCacheReads(on) {
  UC.cacheReads = on;
  ucPaint();
  try {
    ucHaveSetting(await api("/v1/settings", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ usage_cache_reads: on }),
    }));
  } catch (e) { /* shown here anyway, and the daemon is asked again on the next open */ UC.cacheKnown = false; }
}

// ---- wiring, by delegation ----

document.addEventListener("click", ev => {
  const rb = ev.target.closest && ev.target.closest("#uc-ranges button");
  const cb = ev.target.closest && ev.target.closest("#uc-cache");
  if (cb) { ucSetCacheReads(!UC.cacheReads); return; }
  if (rb) { UC.range = rb.dataset.range; loadUsageTab(); return; }
  if (ev.target.closest && ev.target.closest("#uc-card [data-clear]")) { UC.card = null; loadUsageTab(); return; }
  const mini = ev.target.closest && ev.target.closest(".ucmini[data-id]");
  if (mini) { UC.card = { room: mini.dataset.room, id: mini.dataset.id }; loadUsageTab(); }
});

document.addEventListener("change", ev => {
  if (ev.target && ev.target.id === "uc-room") { UC.room = ev.target.value; UC.card = null; loadUsageTab(); }
});

document.addEventListener("mousemove", ev => {
  const g = ev.target.closest && ev.target.closest(".ucchart[data-chart=burn] g[data-t]");
  if (!g) return;
  const chart = g.closest(".ucchart");
  const t = Number(g.dataset.t);
  const nodes = [...UC_KINDS]; // all five, whatever the toggle says
  let b = null;
  for (const n of Object.keys(UC.rooms)) {
    const x = UC.rooms[n].buckets && UC.rooms[n].buckets.get(t);
    if (x) { b = b || ucSums(); ucAdd(b, x.total); }
  }
  if (!b) return;
  chart.querySelector(".ucread").textContent = ucFmtWhen(t) + " · " +
    nodes.map(k => `${k[1]} ${usageTokens(b[k[0]])}`).join(" · ");
});

// Hover on the cumulative line: the time under the pointer, the total there and the percent of the limit.
document.addEventListener("mousemove", ev => {
  const plot = ev.target.closest && ev.target.closest(".ucchart[data-chart=cumulative] .ucplot");
  const c = UC.cum;
  if (!plot || !c) return;
  const r = plot.getBoundingClientRect();
  if (!(r.width > 0)) return;
  const f = Math.max(0, Math.min(1, (ev.clientX - r.left) / r.width));
  const t = c.first + f * (c.end - c.first);
  let v = c.run;
  if (t <= c.nowMs) {
    let a = [c.first, 0];
    v = 0;
    for (const p of c.pts) {
      if (t <= p[0]) { v = a[1] + (p[1] - a[1]) * (t - a[0]) / Math.max(p[0] - a[0], 1); break; }
      a = p;
      v = p[1];
    }
  } else {
    v = c.run + c.rate * (t - c.nowMs);
  }
  if (c.lim && t < c.lim.start) {
    plot.parentNode.querySelector(".ucread").textContent = ulClock(t, c.now) + ` · before the ${c.lim.name} window`;
    return;
  }
  const pct = c.lim ? Math.max(0, (v - c.base) / c.lim.perPct) : null;
  plot.parentNode.querySelector(".ucread").textContent = ulClock(t, c.now) + " · " + usageTokens(Math.round(v)) + " tokens" +
    (pct == null ? "" : ` · ${Math.round(pct)}% of the ${c.lim.name} limit`) + (t > c.nowMs ? " · projected" : "");
  const hov = plot.querySelector(".uchov");
  hov.hidden = false;
  hov.style.left = (f * 100).toFixed(2) + "%";
});
