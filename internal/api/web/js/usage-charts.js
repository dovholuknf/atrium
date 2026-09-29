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
};

function ucKey(room, id) { return room + "|" + id; }

function ucSums() {
  return { rows: 0, input: 0, output: 0, cache_write_5m: 0, cache_write_1h: 0, cache_read: 0, cost: 0 };
}

function ucAdd(a, b) {
  for (const k of Object.keys(a)) a[k] += Number(b && b[k]) || 0;
  return a;
}

function ucTokens(s) {
  return UC_KINDS.reduce((n, k) => n + (Number(s && s[k[0]]) || 0), 0);
}

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

async function ucFetchRoom(room, since, bw, card) {
  const q = `/v1/usage?since=${encodeURIComponent(new Date(since).toISOString())}&bucket=${bw}` +
    (card ? "&card=" + encodeURIComponent(card) : "");
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
  const seq = ++UC.loading;
  UC.inflight++;
  const rooms = {};
  for (const r of absent) rooms[r] = { state: "absent", why: "not attached", buckets: new Map() };
  const asked = UC.card ? [UC.card.room] : ask;
  const got = await Promise.all(asked.map(r =>
    ucFetchRoom(r, UC.since, bw, UC.card ? UC.card.id : "").then(x => [r, x])));
  UC.inflight--;
  if (seq !== UC.loading) return;
  for (const [r, x] of got) {
    rooms[r] = { state: x.state, why: x.why || "", buckets: x.series ? ucIngest(x.series, bw) : new Map() };
  }
  UC.rooms = rooms;
  const held = UC.pending.splice(0);
  for (const e of held) ucApply(e);
  ucPaint();
}

// The newest bucket grows by the row, in the room it came from.
function onUsageEvent(e) {
  let d;
  try { d = JSON.parse(e.data); } catch (err) { return; }
  if (!d || !d.task_id) return;
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
    cache_write_1h: d.cache_write_1h, cache_read: d.cache_read, cost: d.cost,
  };
  ucAdd(b.total, row);
  ucAdd(b.cards[id] || (b.cards[id] = ucSums()), row);
  ucAdd(b.causes[d.cause || "unknown"] || (b.causes[d.cause || "unknown"] = ucSums()), row);
}

function ucSchedulePaint() {
  if (UC.paintTimer) return;
  UC.paintTimer = setTimeout(() => { UC.paintTimer = 0; if (isViewing("usage")) ucPaint(); }, 500);
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
        c.per.set(t, (c.per.get(t) || 0) + (Number(s.cost) || 0));
      }
      for (const [c, s] of Object.entries(b.causes)) ucAdd(causes[c] || (causes[c] = ucSums()), s);
    }
  }
  const series = [...merged.values()].sort((a, b) => a.t - b.t);
  const total = ucSums();
  for (const m of series) ucAdd(total, m.total);
  document.getElementById("uc-count").textContent = total.rows ? `${total.rows} turns · ${usageMoney(total.cost)} est.` : "";
  let html = "";
  for (const n of missing) {
    html += `<div class="ucmissing" data-room="${esc(n)}"><b>${esc(n || "this room")}</b> ${esc(UC.rooms[n].why)}. ` +
      `Its usage is not in these charts.</div>`;
  }
  if (!series.length) {
    html += `<div class="empty">${okRooms.length ? "No turn ended in this range." : "Nothing to draw."}</div>`;
    body.innerHTML = html;
    return;
  }
  html += ucLegend();
  html += `<h4 class="uch">burn rate <span class="ucnote">tokens per minute, stacked by kind</span></h4>` +
    ucBurn(series) +
    `<h4 class="uch">by card <span class="ucnote">top ${UC_TOP_CARDS} by est. cost, the rest as others. ` +
    `Click one to filter</span></h4>` + ucCards(cardRows, series) +
    `<h4 class="uch">tokens by kind</h4>` + ucSplit(total) +
    `<h4 class="uch">cumulative est. cost</h4>` + ucCumulative(series) + ucCauseTable(causes);
  body.innerHTML = html;
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
function ucAxis(series) {
  const ms = UC.bw * 1000;
  const first = Math.floor(UC.since / ms) * ms;
  const last = Math.floor(Date.now() / ms) * ms;
  return { first, n: Math.max(1, Math.round((last - first) / ms) + 1), ms };
}

function ucBurn(series) {
  const ax = ucAxis(series);
  const per = UC.bw / 60;
  const byT = new Map(series.map(s => [s.t, s]));
  let peak = 1;
  for (const s of series) peak = Math.max(peak, ucTokens(s.total) / per);
  const W = 600, H = 150, bw = W / ax.n;
  let bars = "";
  for (let i = 0; i < ax.n; i++) {
    const s = byT.get(ax.first + i * ax.ms);
    if (!s) continue;
    let y = H;
    let segs = "";
    for (const k of UC_KINDS) {
      const h = (Number(s.total[k[0]]) || 0) / per / peak * (H - 4);
      if (h <= 0) continue;
      y -= h;
      segs += `<rect class="${k[3]}" x="${(i * bw).toFixed(2)}" y="${y.toFixed(2)}" width="${Math.max(bw - 0.6, 0.4).toFixed(2)}" height="${h.toFixed(2)}"></rect>`;
    }
    bars += `<g data-t="${s.t}">${segs}</g>`;
  }
  return `<div class="ucchart" data-chart="burn"><svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none" role="img" ` +
    `aria-label="tokens per minute over time">${bars}</svg>` +
    `<div class="ucaxis"><span>${ucFmtWhen(ax.first)}</span><span>peak ${usageTokens(Math.round(peak))}/min</span>` +
    `<span>${ucFmtWhen(ax.first + (ax.n - 1) * ax.ms)}</span></div>` +
    `<div class="ucread" aria-live="off">hover a bar</div></div>`;
}

function ucCards(cardRows, series) {
  const ax = ucAxis(series);
  const rows = [...cardRows.values()].sort((a, b) => b.sums.cost - a.sums.cost);
  const top = rows.slice(0, UC_TOP_CARDS);
  const rest = rows.slice(UC_TOP_CARDS);
  let peak = 0;
  const cell = (label, room, id, per, cost, other) => {
    let m = 0;
    for (const v of per.values()) m = Math.max(m, v);
    peak = Math.max(peak, m);
    return { label, room, id, per, cost, other };
  };
  const cells = top.map(c => cell(ucTitle(c.room, c.id), c.room, c.id, c.per, c.sums.cost, false));
  if (rest.length) {
    const per = new Map();
    let cost = 0;
    for (const c of rest) {
      cost += c.sums.cost;
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
      bars += `<rect class="uck-cost" x="${(i * bw).toFixed(2)}" y="${(H - h).toFixed(2)}" width="${Math.max(bw - 0.4, 0.4).toFixed(2)}" height="${h.toFixed(2)}"></rect>`;
    }
    const attrs = c.other ? "" : ` data-room="${esc(c.room)}" data-id="${esc(c.id)}"`;
    html += `<div class="ucmini${c.other ? " other" : ""}"${attrs} data-key="${esc(ucKey(c.room, c.id))}">` +
      `<div class="ucminit"><span class="uctitle">${c.room && UC.rooms && Object.keys(UC.rooms).length > 1 ? `<span class="ucroom">${esc(c.room)}</span> ` : ""}${esc(c.label)}</span>` +
      `<b>${usageMoney(c.cost)}</b></div>` +
      `<svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none">${bars}</svg></div>`;
  }
  return html + `</div>`;
}

function ucSplit(total) {
  const sum = ucTokens(total) || 1;
  let bar = "", legend = "";
  for (const k of UC_KINDS) {
    const v = Number(total[k[0]]) || 0;
    if (v > 0) bar += `<span class="${k[3]}" style="width:${(v / sum * 100).toFixed(2)}%" data-tip="${k[1]}: ${usageTokens(v)}"></span>`;
    legend += `<span class="uctip" data-tip="${esc(USAGE_TIPS[k[2]])}"><i class="${k[3]}"></i>${k[1]} <b>${usageTokens(v)}</b></span>`;
  }
  return `<div class="ucsplit">${bar}</div><div class="uclegend">${legend}<span>est. <b>${usageMoney(total.cost)}</b></span></div>`;
}

function ucCumulative(series) {
  const ax = ucAxis(series);
  const total = series.reduce((n, s) => n + s.total.cost, 0) || 1;
  const byT = new Map(series.map(s => [s.t, s.total.cost]));
  const W = 600, H = 90;
  let run = 0, pts = [];
  for (let i = 0; i < ax.n; i++) {
    run += byT.get(ax.first + i * ax.ms) || 0;
    pts.push(`${(i / Math.max(ax.n - 1, 1) * W).toFixed(1)},${(H - 3 - run / total * (H - 6)).toFixed(1)}`);
  }
  return `<div class="ucchart"><svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none" role="img" aria-label="cumulative cost">` +
    `<polyline class="uck-line" points="${pts.join(" ")}"></polyline></svg>` +
    `<div class="ucaxis"><span>$0.00</span><span>${usageMoney(series.reduce((n, s) => n + s.total.cost, 0))} est.</span></div></div>`;
}

function ucCauseTable(causes) {
  const rows = Object.entries(causes).sort((a, b) => b[1].cost - a[1].cost);
  if (!rows.length) return "";
  return `<h4 class="uch">est. cost by cause</h4><div class="uccauses">` + rows.map(([c, s]) =>
    `<span>${esc(USAGE_CAUSES[c] || c)}</span><span>${esc(usageCount(c, s))}</span><span>${usageMoney(s.cost)}</span>`).join("") + `</div>`;
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
  for (const b of m.values()) peak = Math.max(peak, ucTokens(b.total));
  const W = 300, H = 40, bw = W / n;
  let bars = "";
  for (const b of m.values()) {
    const i = Math.round((b.t - first) / 900000);
    if (i < 0 || i >= n) continue;
    let y = H;
    for (const k of UC_KINDS) {
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

// ---- wiring, by delegation ----

document.addEventListener("click", ev => {
  const rb = ev.target.closest && ev.target.closest("#uc-ranges button");
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
  const nodes = [...UC_KINDS];
  let b = null;
  for (const n of Object.keys(UC.rooms)) {
    const x = UC.rooms[n].buckets && UC.rooms[n].buckets.get(t);
    if (x) { b = b || ucSums(); ucAdd(b, x.total); }
  }
  if (!b) return;
  chart.querySelector(".ucread").textContent = ucFmtWhen(t) + " · " +
    nodes.map(k => `${k[1]} ${usageTokens(b[k[0]])}`).join(" · ") + " · est. " + usageMoney(b.cost);
});
