// The usage tab's limits: the 5h and weekly readings, when each resets, and when each would run out.
//
// The percentages are the ones cards' statuslines report (telemetry.five_hour and .weekly). The room keeps
// them (GET /v1/usage/limits?since=, internal/api/limits.go) so a reload draws the same picture; after that
// they arrive on the card updates the board already gets. NOTHING here polls, and nothing here notifies: the
// flameout shows on the tab and nowhere else. See docs/rnd/usage-tab-design.md, sections 3.3 and 3.3.1.
//
// Atrium does not know which account a card runs under. A row is "highest seen in the last hour", never a
// sum, and two distinct reset times are two accounts and so two rows.

const UL_HOUR = 3600000;
const UL_WINDOW = { five_hour: 5 * UL_HOUR, weekly: 7 * 24 * UL_HOUR };
const UL_KEEP = { five_hour: 8 * UL_HOUR, weekly: 7 * 24 * UL_HOUR };
const UL_LABEL = { five_hour: "5h", weekly: "week" };
const UL_MIN_POINTS = 3;
const UL_MIN_SPAN = 15 * 60000;
const UL_DANGER = 30 * 60000;
const UL_ROUGH = 3;             // residual (rms, in points of percentage) past which the line is "rough"
const UL_BAND_FROM = 21600;     // the 5h band shows at ranges of this many seconds or more

const UL = {
  readings: [],   // {at ms, room, card, kind, pct, reset ms (0 when absent)}
  last: new Map(), // room|card|kind -> the newest reading, so an unchanged statusline adds nothing
  fetched: new Map(), // room -> when its readings were last asked for
  html: "",       // the last section painted, so a live update that changes nothing paints nothing
};

const UL_TIP = "When you would hit 100% if you keep spending as you have since the window started, from the " +
  "percentages your cards reported. It sees spend outside atrium, it does not see the future, and it is only as " +
  "fresh as the last card that reported. It is a straight line through a bursty curve, so it is shown to the " +
  "nearest 5 minutes and says rough when the readings scatter.";
const UL_TIP_TOKENS = " Too few readings for a line, so this scales the counted tokens one percentage point took " +
  "by the burn of the last 30 minutes. It assumes the mix of tokens stays as it was, and it only sees what atrium " +
  "counted, so it can only undercount.";

// ---- the fit: readings in, projection out. Pure, so a test can drive it. ----

// points: [{at (ms), pct}] already limited to the current window.
// o: {now (ms), reset (ms, 0 when unknown), pct (the reading shown), tokens: {between, recent} or null}
//   tokens.between is the counted tokens from the first reading to the last, tokens.recent over the last 30 minutes.
// Returns {state: "at" | "notwindow" | "none", method: "fit" | "tokens", at, rough, before, danger, rate}
//   at: the projected 100% in ms, rounded to 5 minutes. before: ms from that to the reset (when it is known).
function ulFit(points, o) {
  const now = o.now, pct = o.pct, reset = o.reset || 0;
  const none = method => ({ state: "none", method });
  const pts = (points || []).filter(p => isFinite(p.at) && typeof p.pct === "number").sort((a, b) => a.at - b.at);
  if (pct >= 100) return finish({ method: "fit", rate: 0, rough: false, at: now });
  let method = "fit", rate = 0, rough = false;
  const span = pts.length ? pts[pts.length - 1].at - pts[0].at : 0;
  if (pts.length >= UL_MIN_POINTS && span >= UL_MIN_SPAN) {
    const t0 = pts[0].at;
    const xs = pts.map(p => (p.at - t0) / UL_HOUR), n = pts.length;
    const mx = xs.reduce((a, b) => a + b, 0) / n, my = pts.reduce((a, p) => a + p.pct, 0) / n;
    let sxx = 0, sxy = 0;
    for (let i = 0; i < n; i++) { sxx += (xs[i] - mx) ** 2; sxy += (xs[i] - mx) * (pts[i].pct - my); }
    rate = sxx ? sxy / sxx : 0;
    const icpt = my - rate * mx;
    let ss = 0;
    for (let i = 0; i < n; i++) ss += (pts[i].pct - (icpt + rate * xs[i])) ** 2;
    rough = Math.sqrt(ss / n) > UL_ROUGH;
  } else {
    method = "tokens";
    const tk = o.tokens;
    if (pts.length < 2 || !tk) return none(method);
    const gained = pts[pts.length - 1].pct - pts[0].pct;
    if (!(gained > 0) || !(tk.between > 0) || !(tk.recent > 0)) return none(method);
    rate = (tk.recent / 0.5) / (tk.between / gained);   // points per hour
  }
  if (!(rate > 0.01)) return none(method);
  return finish({ method, rate, rough, at: now + (100 - pct) / rate * UL_HOUR });

  function finish(r) {
    const at = r.at === now ? now : Math.round(r.at / 300000) * 300000;
    const out = { state: "at", method: r.method, rate: r.rate, rough: r.rough, at, before: 0, danger: false };
    if (reset) {
      out.before = reset - at;
      if (at > reset) { out.state = "notwindow"; return out; }
    }
    out.danger = at - now <= UL_DANGER;
    return out;
  }
}

// ---- readings ----

function ulKey(r) { return r.room + "|" + r.card + "|" + r.kind; }

function ulAdd(r) {
  if (!r || !(r.kind in UL_WINDOW) || typeof r.pct !== "number" || !isFinite(r.at)) return false;
  const k = ulKey(r), prev = UL.last.get(k);
  if (prev && prev.at >= r.at) {
    if (prev.at === r.at) return false;                              // the same reading twice
    if (prev.pct === r.pct && prev.reset === r.reset) return false;
  } else if (prev && prev.pct === r.pct && prev.reset === r.reset) {   // unchanged, and newer
    prev.seen = r.at;
    return false;
  }
  r.seen = r.at;
  UL.readings.push(r);
  if (!prev || prev.at <= r.at) UL.last.set(k, r);
  return true;
}

function ulPrune(now) {
  UL.readings = UL.readings.filter(r => now - r.at <= UL_KEEP[r.kind]);
  const gone = [];
  for (const [k, r] of UL.last) if (now - r.at > UL_KEEP[r.kind]) gone.push(k);
  for (const k of gone) UL.last.delete(k);
}

// What cards carry now: telemetry.five_hour and .weekly, each a {pct, resets_at}, `seconds` old.
function ulOnCards() {
  const now = Date.now();
  let moved = false;
  for (const t of (typeof lastTasks !== "undefined" ? lastTasks : [])) {
    const c = t && t.telemetry;
    if (!c) continue;
    const room = t.room || roomOf(t.id) || roomNow() || "";
    const at = now - (Number(c.seconds) || 0) * 1000;
    for (const kind of Object.keys(UL_WINDOW)) {
      const lim = c[kind === "five_hour" ? "five_hour" : "weekly"];
      if (!lim || typeof lim.pct !== "number") continue;
      if (ulAdd({ at, room, card: bareId(t.id), kind, pct: Math.round(lim.pct), reset: Date.parse(lim.resets_at) || 0 })) moved = true;
    }
  }
  ulPrune(now);
  if (moved && typeof isViewing === "function" && isViewing("usage")) ulRefresh();
}

// A room's readings, asked for on the tab's first paint. A 404 is an older room: live telemetry only, no error.
async function ulLoad(rooms) {
  const now = Date.now();
  let got = false;
  await Promise.all(rooms.map(async room => {
    if (now - (UL.fetched.get(room) || 0) < 60000) return;
    UL.fetched.set(room, now);
    for (const [kind, back] of [["five_hour", UL_KEEP.five_hour], ["weekly", UL_KEEP.weekly]]) {
      const q = `/v1/usage/limits?since=${encodeURIComponent(new Date(now - back).toISOString())}`;
      try {
        const res = await plainFetch(q, { headers: room ? { "X-Atrium-Room": room } : {} });
        if (!res.ok) continue;
        const body = await res.json();
        for (const r of (body && body.readings) || []) {
          if (r.kind !== kind) continue;
          if (ulAdd({ at: Date.parse(r.at), room, card: String(r.card || ""), kind, pct: r.pct, reset: Date.parse(r.resets_at) || 0 })) got = true;
        }
      } catch (e) { /* live telemetry only */ }
    }
  }));
  if (got && typeof isViewing === "function" && isViewing("usage")) ulRefresh();
}

// The rows: one per distinct reset time per kind, from the last hour's readings. A group whose window is
// already over is not shown as if it were now.
function ulGroups(kind, now) {
  const by = new Map();
  for (const r of UL.readings) {
    if (r.kind !== kind || now - r.seen > UL_HOUR) continue;
    if (r.reset && r.reset < now) continue;
    const k = r.reset ? Math.round(r.reset / 60000) : 0;
    let g = by.get(k);
    if (!g) by.set(k, g = { kind, reset: r.reset, key: k, best: r, cards: new Set() });
    g.cards.add(r.card);
    if (r.pct > g.best.pct || (r.pct === g.best.pct && r.seen > g.best.seen)) g.best = r;
    if (r.reset > g.reset) g.reset = r.reset;
  }
  return [...by.values()].sort((a, b) => (a.reset || 1e18) - (b.reset || 1e18));
}

// The readings that count for a group's line: its own window, pooled over its cards.
function ulPoints(g, now) {
  const from = g.kind === "five_hour"
    ? (g.reset ? g.reset : now) - UL_WINDOW.five_hour
    : Math.max((g.reset ? g.reset : now) - UL_WINDOW.weekly, now - 24 * UL_HOUR);
  return UL.readings.filter(r => r.kind === g.kind && (r.reset ? Math.round(r.reset / 60000) : 0) === g.key && r.at >= from)
    .map(r => ({ at: r.at, pct: r.pct }));
}

// Counted tokens in [from, to], from the buckets the tab holds, each weighted by how much of it overlaps.
// Null when the range does not reach back that far or one card is filtered, since then it is not the account's burn.
function ulTokensIn(from, to) {
  if (typeof UC === "undefined" || UC.card || from < UC.since) return null;
  const ms = UC.bw * 1000;
  let n = 0;
  for (const name of Object.keys(UC.rooms)) {
    const r = UC.rooms[name];
    if (r.state !== "ok") continue;
    for (const [t, b] of r.buckets) {
      const o = Math.min(to, t + ms) - Math.max(from, t);
      if (o > 0) n += ucCounted(b.total) * o / ms;
    }
  }
  return n;
}

// ---- drawing ----

const ulPad = n => String(n).padStart(2, "0");
function ulClock(ms, now) {
  const d = new Date(ms), n = new Date(now);
  const hm = ulPad(d.getHours()) + ":" + ulPad(d.getMinutes());
  return d.toDateString() === n.toDateString() ? hm : d.toLocaleDateString([], { weekday: "short" }) + " " + hm;
}
function ulDur(ms) {
  const m = Math.max(0, Math.round(ms / 60000));
  if (m < 60) return m + "m";
  if (m < 1440) return Math.floor(m / 60) + "h" + (m % 60 ? " " + (m % 60) + "m" : "");
  const h = Math.floor((m % 1440) / 60);
  return Math.floor(m / 1440) + "d" + (h ? " " + h + "h" : "");
}
function ulCardName(r) {
  const t = (typeof lastTasks !== "undefined" ? lastTasks : []).find(x =>
    bareId(x.id) === r.card && (!r.room || (x.room || roomOf(x.id) || roomNow()) === r.room));
  return (t && t.display_title) || (r.card.length > 12 ? r.card.slice(0, 8) : r.card);
}

// The second line of a row: the projection, and the mark when the 5h limit runs out before it resets.
function ulProjection(g, now) {
  const pts = ulPoints(g, now);
  let tokens = null;
  if (pts.length && (pts.length < UL_MIN_POINTS || pts[pts.length - 1].at - pts[0].at < UL_MIN_SPAN)) {
    const sorted = pts.slice().sort((a, b) => a.at - b.at);
    const between = ulTokensIn(sorted[0].at, sorted[sorted.length - 1].at);
    const recent = ulTokensIn(now - 30 * 60000, now);
    if (between != null && recent != null) tokens = { between, recent };
  }
  const f = ulFit(pts, { now, reset: g.reset, pct: g.best.pct, tokens });
  const tip = UL_TIP + (f.method === "tokens" ? UL_TIP_TOKENS : "");
  const tail = (f.method === "tokens" ? " (estimated from token burn)" : "") + (f.rough ? " · rough" : "");
  if (f.state === "none") return `<div class="ulproj" data-tip="${esc(tip)}">no pace to project</div>`;
  if (f.state === "notwindow") return `<div class="ulproj" data-tip="${esc(tip)}">at this pace: not this window${esc(tail)}</div>`;
  const when = f.at === now ? "at the limit now" : `100% at ${ulClock(f.at, now)}`;
  const rel = g.reset ? `, ${ulDur(f.before)} before the reset` : "";
  const flame = g.kind === "five_hour" && g.reset;
  const mark = flame ? `<b class="ulflame ${f.danger ? "uldanger" : "ulwarn"}">▲ flameout before reset</b>` : "";
  return `<div class="ulproj${flame ? (f.danger ? " uldanger" : " ulwarn") : ""}" data-tip="${esc(tip)}">at this pace: ${esc(when + rel + tail)}${mark}</div>`;
}

function ulRow(g, now) {
  const b = g.best;
  const pct = Math.max(0, Math.min(100, Math.round(b.pct)));
  const others = g.cards.size - 1;
  const from = `from ${esc(ulCardName(b))}${others > 0 ? ` +${others}` : ""} · ${ulDur(now - b.seen)} ago`;
  const reset = g.reset ? `resets ${ulClock(g.reset, now)} (in ${ulDur(g.reset - now)})` : "";
  const tip = `highest seen: ${ulCardName(b)} reported ${pct}% of its ${UL_LABEL[g.kind]} limit ${ulDur(now - b.seen)} ago. ` +
    `atrium does not know which account a card runs under, so this is what a card last said, never a sum.`;
  return `<div class="ulrow" data-kind="${g.kind}" data-reset="${g.reset || ""}" data-tip="${esc(tip)}">` +
    `<span class="ulk">${UL_LABEL[g.kind]}</span>` +
    `<div class="ucsplit ulbar"><span class="ulfill" style="width:${pct}%"></span></div>` +
    `<b class="ulpct">${pct}%</b><span class="ulreset">${reset}</span><span class="ulfrom">${from}</span>` +
    ulProjection(g, now) + `</div>`;
}

function ulDash(kind) {
  return `<div class="ulrow ulnone" data-kind="${kind}"><span class="ulk">${UL_LABEL[kind]}</span>` +
    `<div class="ucsplit ulbar"></div><b class="ulpct" data-tip="no card reported ${UL_LABEL[kind]} limit in the last hour">–</b></div>`;
}

function ulSection(now) {
  now = now || Date.now();
  let rows = "";
  for (const kind of Object.keys(UL_WINDOW)) {
    const gs = ulGroups(kind, now);
    rows += gs.length ? gs.map(g => ulRow(g, now)).join("") : ulDash(kind);
  }
  return (UL.html = `<h4 class="uch">limits <span class="ucnote">highest seen, last hour</span></h4>` +
    `<div class="ullimits" id="ul-limits">${rows}</div>`);
}

// The 5h window's start to now, behind the bars, when the range is 6h or longer. W and H are the chart's viewBox.
function ulBand(ax, W, H, now) {
  now = now || Date.now();
  const none = { svg: "", label: "" };
  if (typeof UC === "undefined" || UC_RANGES[UC.range][0] < UL_BAND_FROM) return none;
  const gs = ulGroups("five_hour", now).filter(g => g.reset);
  if (!gs.length) return none;
  const span = ax.n * ax.ms;
  let out = "", label = "";
  for (const g of gs) {
    const x0 = Math.max(0, (g.reset - UL_WINDOW.five_hour - ax.first) / span * W);
    const x1 = Math.min(W, (now - ax.first) / span * W);
    if (!(x1 > x0)) continue;
    out += `<rect class="ulband" x="${x0.toFixed(2)}" y="0" width="${(x1 - x0).toFixed(2)}" height="${H}"></rect>` +
      `<line class="ulbandedge" x1="${x0.toFixed(2)}" x2="${x0.toFixed(2)}" y1="0" y2="${H}"></line>`;
    if (!label) label = `<span class="ulbandlab" style="left:${(x0 / W * 100).toFixed(2)}%">5h window</span>`;
  }
  return out ? { svg: `<g class="ulbands">${out}</g>`, label } : none;
}

// A live reading: only the section and the band are redrawn, and only when they changed.
function ulRefresh() {
  const box = document.getElementById("ul-limits");
  if (!box) return;
  const before = UL.html;
  const html = ulSection();
  if (html === before) return;
  const tmp = document.createElement("div");
  tmp.innerHTML = html;
  const fresh = tmp.querySelector("#ul-limits");
  if (fresh) box.innerHTML = fresh.innerHTML;
  ulPaintBand();
  if (typeof ucPaintCum === "function") ucPaintCum();
}

// The band, redrawn in the chart that is there.
function ulPaintBand() {
  const chart = document.querySelector("#uc-body .ucchart[data-chart=burn]");
  if (!chart) return;
  const svg = chart.querySelector("svg");
  const vb = svg.viewBox.baseVal;
  const band = ulBand(ucAxis(), vb.width, vb.height);
  const old = svg.querySelector(".ulbands");
  if (old) old.remove();
  const lab = chart.querySelector(".ulbandlab");
  if (lab) lab.remove();
  if (band.svg) svg.insertAdjacentHTML("afterbegin", band.svg);
  if (band.label) chart.insertAdjacentHTML("afterbegin", band.label);
}
