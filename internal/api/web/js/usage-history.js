// The usage tab's past pages, and the chart that lays tokens and percent used on one time axis.
//
// A page is the range stepped back UC.off times: a week by the weekly resets, a calendar day on 24h, and 1h, 6h
// and 7d by their own span. The percents are the readings cards reported (GET /v1/usage/limits?since=&until=,
// internal/api/limits.go). They are the account's, so ticking a room off changes the tokens and never them.

const UH_DAY = 86400000;
const UH_BACK = 30 * UH_DAY;   // store.UsageMaxBack: no page starts further back than the room answers
const UH_DOW = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const UH_MON = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const UH_ROOM_COLOURS = 4;
const UC_PLOT_COLOURS = 6;

// ---- pages ----

// The page `off` steps back from `now`: {since, until} in ms, until 0 when it is the live one. Pure, so a test
// can drive it.
function ucPage(range, off, now) {
  if (range === "week") {
    let s = ucWeekSince(now);
    for (let k = 0; k > off; k--) s = ucWeekSince(s - 1);
    return { since: s, until: off ? ucWeekNext(s) || s + 7 * UH_DAY : 0 };
  }
  if (range === "24h" && off < 0) {
    const d = new Date(now);
    d.setHours(0, 0, 0, 0);
    d.setDate(d.getDate() + off);
    const since = d.getTime();
    d.setDate(d.getDate() + 1);
    return { since, until: d.getTime() };
  }
  const span = UC_RANGES[range][0] * 1000, ms = UC_RANGES[range][1] * 1000;
  const end = Math.floor(now / ms) * ms + off * span;
  return { since: end - span, until: off ? end : 0 };
}

function ucDayName(t) { const d = new Date(t); return `${UH_DOW[d.getDay()]} ${UH_MON[d.getMonth()]} ${d.getDate()}`; }
function ucHM(t) { const d = new Date(t); return ulPad(d.getHours()) + ":" + ulPad(d.getMinutes()); }

function ucPageLabel(range, off, p) {
  if (!off) return range === "week" ? "this week" : "last " + range;
  if (range === "week") return (off === -1 ? "last week · " : "") + `${ucDayName(p.since)} – ${ucDayName(p.until)}`;
  if (range === "24h") return (off === -1 ? "yesterday · " : "") + ucDayName(p.since);
  return `${ucDayName(p.since)} ${ucHM(p.since)} – ${range === "7d" ? ucDayName(p.until) + " " : ""}${ucHM(p.until)}`;
}

function ucPaintPager() {
  const lab = document.getElementById("uc-pagelab");
  if (!lab) return;
  const now = UC.now || Date.now();
  lab.textContent = ucPageLabel(UC.range, UC.off, { since: UC.since, until: UC.until });
  document.getElementById("uc-next").disabled = !UC.off;
  document.getElementById("uc-now").disabled = !UC.off;
  document.getElementById("uc-prev").disabled = ucPage(UC.range, UC.off - 1, now).since < now - UH_BACK;
  const date = document.getElementById("uc-date");
  if (document.activeElement === date) return;
  const iso = t => { const d = new Date(t); return `${d.getFullYear()}-${ulPad(d.getMonth() + 1)}-${ulPad(d.getDate())}`; };
  date.max = iso(now);
  date.min = iso(now - UH_BACK);
  date.value = iso(!UC.off && UC.range !== "week" ? now : UC.since || now);
}

// A day picked: on the week range, the week holding it, on any other, that calendar day.
function ucJumpTo(value) {
  const [y, m, d] = String(value).split("-").map(Number);
  if (!y) return;
  const t = new Date(y, m - 1, d, 12).getTime(), now = UC.now || Date.now();
  if (UC.range === "week") {
    let off = 0;
    for (let s = ucWeekSince(now); s > t && off > -6; off--) s = ucWeekSince(s - 1);
    UC.off = off;
  } else {
    const day = x => { const z = new Date(x); z.setHours(0, 0, 0, 0); return z.getTime(); };
    UC.range = "24h";
    UC.off = Math.min(0, Math.round((day(t) - day(now)) / UH_DAY));
  }
  loadUsageTab();
}

// ---- percent used, from the readings ----

// A room's readings for the page, from a week before it so the percent in force when it opens is known.
async function ucFetchHist(room, since, until) {
  const iso = t => encodeURIComponent(new Date(t).toISOString());
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), 15000);
  try {
    const res = await plainFetch(`/v1/usage/limits?since=${iso(since - UL_WINDOW.weekly)}` + (until ? `&until=${iso(until)}` : ""),
      { headers: room ? { "X-Atrium-Room": room } : {}, signal: ctrl.signal });
    if (!res.ok) return [];
    const body = await res.json();
    return ((body && body.readings) || []).map(r => ({ at: Date.parse(r.at), room, card: String(r.card || ""), kind: r.kind,
      pct: r.pct, reset: Date.parse(r.resets_at) || 0 })).filter(r => r.kind in UL_WINDOW && isFinite(r.at) && typeof r.pct === "number");
  } catch (e) {
    return [];
  } finally {
    clearTimeout(timer);
  }
}

// The percent of `kind` in force over [from, to], as steps [[t, pct]]: at each time a reading lands or a window
// ends, the highest any card last said while its window still held. Pure, so a test can drive it.
function ucPctSteps(readings, kind, from, to) {
  const win = UL_WINDOW[kind];
  const rs = readings.filter(r => r.kind === kind && r.at < to).sort((a, b) => a.at - b.at);
  const ends = r => Math.min(r.reset || Infinity, r.at + win);
  const times = new Set([from]);
  for (const r of rs) {
    if (r.at > from) times.add(r.at);
    const e = ends(r);
    if (e > from && e < to) times.add(e);
  }
  const last = new Map(), out = [];
  let i = 0;
  for (const t of [...times].sort((a, b) => a - b)) {
    while (i < rs.length && rs[i].at <= t) { last.set(rs[i].room + "|" + rs[i].card, rs[i]); i++; }
    let v = 0;
    for (const r of last.values()) if (ends(r) > t) v = Math.max(v, r.pct);
    if (!out.length || out[out.length - 1][1] !== v) out.push([t, v]);
  }
  return out;
}

function ucPctAt(steps, t) {
  let v = 0;
  for (const s of steps) { if (s[0] > t) break; v = s[1]; }
  return v;
}

// ---- the rooms and lines to draw ----

function ucRoomClass(n) { return "ucroom" + Math.max(0, Object.keys(UC.rooms).indexOf(n)) % UH_ROOM_COLOURS; }

function ucPaintPick(ok) {
  const el = document.getElementById("uc-pick");
  if (!el) return;
  const chip = (on, data, swatch, label, small, tip, dis) => `<button type="button" class="ucchip${on ? " on" : ""}${dis ? " dis" : ""}" ${data}` +
    `${tip ? ` data-tip="${esc(tip)}"` : ""}><i class="${swatch}"></i>${esc(label)}${small ? ` <small>${esc(small)}</small>` : ""}</button>`;
  let html = "";
  const spent = ok.map(n => {
    let sum = 0;
    for (const b of UC.rooms[n].buckets.values()) sum += ucShown(b.total);
    return [n, sum];
  }).filter(([, sum]) => sum > 0);
  if (spent.length > 1) {
    html += `<span class="ucpickk">rooms</span><span class="ucranges ucchips">` +
      spent.map(([n, sum]) => chip(!UC.hide.has(n), `data-room="${esc(n)}"`, ucRoomClass(n), n || "this room", usageTokens(sum))).join("") + `</span>`;
  }
  const rs = UC.until ? UC.hist : UC.hist.concat(UL.readings);
  const hasW = rs.some(r => r.kind === "weekly"), has5 = rs.some(r => r.kind === "five_hour");
  const none = k => `No card reported a ${k} percent in this range, so there is no line to draw`;
  html += `<span class="ucpickk">lines</span><span class="ucranges ucchips">` + chip(UC.lines.tok, `data-line="tok"`, "ucl-tok", "tokens") +
    chip(hasW && UC.lines.pw, `data-line="pw"`, "ucl-wk", "week %", "", hasW ? "" : none("weekly"), !hasW) +
    chip(has5 && UC.lines.p5, `data-line="p5"`, "ucl-5h", "5h %", "", has5 ? "" : none("5h"), !has5) + `</span>`;
  if (UC.sub === "charts") {
    html += `<span class="ucpickk">cards</span><span class="ucranges ucchips">` + UC.plot.map((k, i) => {
      const [room, id] = k.split("|");
      return `<button type="button" class="ucchip on ucpc${i % UC_PLOT_COLOURS}" data-unplot="${esc(k)}" data-tip="take it off the chart">` +
        `<i></i>${esc(ucTitle(room, id))} <small>×</small></button>`;
    }).join("") + `<button type="button" class="ucchip" id="uc-addcard" data-tip="draw one or more cards on the chart">+ card</button></span>`;
  }
  html += `<span class="help" tabindex="0" data-tip="Rooms add or take away their cards' tokens in every chart. The percents are what cards reported for the account, so they do not change with the rooms. Cards draw their own running total on the chart, and a click on a card below does the same.">?</span>`;
  if (el.innerHTML !== html) el.innerHTML = html;
}

function ucTogglePlot(key) {
  const i = UC.plot.indexOf(key);
  if (i >= 0) UC.plot.splice(i, 1); else UC.plot.push(key);
  ucPaint();
}

function ucPaintCardPop() {
  const pop = document.getElementById("uc-cardpop");
  if (!pop || pop.hidden) return;
  const q = document.getElementById("uc-cardq").value.trim().toLowerCase();
  const rows = UC.cardList.map(c => Object.assign({ name: ucTitle(c.room, c.id) }, c))
    .filter(c => !q || c.name.toLowerCase().includes(q) || c.room.toLowerCase().includes(q)).slice(0, 80);
  const html = rows.map(c => `<label class="${UC.plot.includes(c.key) ? "on" : ""}"><input type="checkbox" data-plot="${esc(c.key)}"${UC.plot.includes(c.key) ? " checked" : ""}>` +
    `<span class="nm">${esc(c.name)}</span><span class="rm">${esc(c.room)}</span><b>${usageTokens(c.v)}</b></label>`).join("") ||
    `<div class="ucnote">no card matches</div>`;
  const list = document.getElementById("uc-cardlist");
  if (list.innerHTML !== html) list.innerHTML = html;
}

function ucOpenCardPop(btn) {
  const pop = document.getElementById("uc-cardpop");
  if (!pop.hidden) { pop.hidden = true; return; }
  const r = btn.getBoundingClientRect(), host = document.getElementById("usage").getBoundingClientRect();
  pop.style.left = (r.left - host.left) + "px";
  pop.style.top = (r.bottom - host.top + 4) + "px";
  pop.hidden = false;
  ucPaintCardPop();
  document.getElementById("uc-cardq").focus();
}

// The buckets of the shown rooms over the page: each {t, rooms: {name: tokens}, cards: Map(room|id -> tokens), sum}.
function ucCorBins(okRooms) {
  const ms = UC.bw * 1000, byT = new Map();
  for (const n of okRooms) {
    for (const [t, b] of UC.rooms[n].buckets) {
      if (t + ms <= UC.since || (UC.until && t >= UC.until)) continue;
      let e = byT.get(t);
      if (!e) byT.set(t, e = { t, rooms: {}, cards: new Map(), sum: 0 });
      const v = ucShown(b.total);
      e.rooms[n] = (e.rooms[n] || 0) + v;
      e.sum += v;
      for (const [id, s] of Object.entries(b.cards)) e.cards.set(ucKey(n, id), (e.cards.get(ucKey(n, id)) || 0) + ucShown(s));
    }
  }
  return [...byT.values()].sort((a, b) => a.t - b.t);
}

// ---- the chart ----

// Counted tokens since the page began, stacked by room under the total, against the 5h and weekly percent on a
// second axis. On the live week the tokens and the week run on to the reset at this pace. UC.cor keeps what the
// hover needs (ucCorAt, ucCorHover).
function ucCorrelate(okRooms) {
  const L = UC.lines, ms = UC.bw * 1000, live = !UC.until;
  const nowMs = live ? ucNow() : UC.until, from = UC.since;
  const proj = live && UC.range === "week";
  const end = proj ? Math.max(ucWeekNext(nowMs), nowMs) : nowMs;
  const span = Math.max(end - from, 1), W = 600, H = 100;
  const X = t => (Math.min(Math.max(t, from), end) - from) / span * W;
  const left = t => (X(t) / W * 100).toFixed(2) + "%";
  const bins = ucCorBins(okRooms);
  let run = 0;
  const cum = {};
  const pts = bins.map(e => {
    run += e.sum;
    for (const n of okRooms) cum[n] = (cum[n] || 0) + (e.rooms[n] || 0);
    return { t: e.t + ms, run, cum: Object.assign({}, cum), e };
  });
  let recent = 0;
  for (const e of bins) if (e.t >= nowMs - 6 * UL_HOUR) recent += e.sum;
  const rate = recent / Math.max(Math.min(6 * UL_HOUR, nowMs - from), 1);
  const projRun = proj ? run + rate * (end - nowMs) : run;
  const readings = live ? UC.hist.concat(UL.readings) : UC.hist;
  const has5 = readings.some(r => r.kind === "five_hour"), hasW = readings.some(r => r.kind === "weekly");
  const p5 = ucPctSteps(readings, "five_hour", from, nowMs), pw = ucPctSteps(readings, "weekly", from, nowMs);
  const pwNow = ucPctAt(pw, nowMs);
  let projPw = null;
  if (proj && hasW) {
    const f = ulFit(pw.filter(s => s[0] >= Math.max(from, nowMs - 24 * UL_HOUR)).map(([at, pct]) => ({ at, pct })), { now: nowMs, reset: end, pct: pwNow });
    if (f.state !== "none") projPw = Math.min(100, pwNow + f.rate * (end - nowMs) / UL_HOUR);
  }
  const plotRuns = UC.plot.map(k => { let r = 0; return pts.map(p => (r += p.e.cards.get(k) || 0)); });
  const topVal = L.tok ? projRun : Math.max(0, ...plotRuns.map(a => a.length ? a[a.length - 1] : 0));
  const step = ucNiceStep(topVal), top = step ? Math.ceil(topVal / step - 1e-9) * step : 1;
  const Y = v => H - 2 - Math.max(0, Math.min(top, v)) / top * (H - 4);
  const P = p => H - 2 - Math.max(0, Math.min(110, p)) / 110 * (H - 4);
  const ln = (cls, x1, y1, x2, y2, more) => `<line class="${cls}"${more || ""} x1="${x1.toFixed(2)}" x2="${x2.toFixed(2)}" y1="${y1.toFixed(2)}" y2="${y2.toFixed(2)}"></line>`;
  let svg = "", over = "", yax = "";
  if (L.tok || UC.plot.length) {
    for (let v = 0; step && v <= top + step / 1000; v += step) {
      svg += ln("ulgrid", 0, Y(v), W, Y(v));
      yax += `<span class="ucy ucyk" style="top:${(Y(v) / H * 100).toFixed(2)}%">${usageTokens(Math.round(v))}</span>`;
    }
  }
  const pctOn = (L.pw && hasW) || (L.p5 && has5);
  if (pctOn) {
    for (const p of [0, 25, 50, 75, 100]) {
      if (p === 100) svg += ln("ulcap", 0, P(p), W, P(p));
      else if (!L.tok) svg += ln("ulgrid", 0, P(p), W, P(p));
      yax += `<span class="ucyr${p === 100 ? " ucy100" : ""}" style="top:${(P(p) / H * 100).toFixed(2)}%">${p}%</span>`;
    }
  }
  // The 5h windows the readings name, shaded with their resets, when the range is short enough to tell them apart.
  const wins = [...new Set(readings.filter(r => r.kind === "five_hour" && r.reset).map(r => Math.round(r.reset / 60000) * 60000))].sort((a, b) => a - b);
  if (L.p5 && has5 && !ucWide()) {
    for (const r of wins) {
      const s = r - UL_WINDOW.five_hour;
      if (r <= from || s >= nowMs) continue;
      svg += `<rect class="uc5win" x="${X(s).toFixed(2)}" y="0" width="${(X(Math.min(r, nowMs)) - X(s)).toFixed(2)}" height="${H}"></rect>`;
      if (r < end && r <= nowMs) svg += ln("ulresetln", X(r), 0, X(r), H, ` data-reset="five_hour"`);
    }
  }
  const wk = ucWeekMarks(from + 1, end - 1, nowMs, W, H, X);
  svg += wk.svg;
  over += wk.over;
  if (proj) over += `<span class="ulresetlab ucend" data-reset="weekly" style="left:${left(end)}">week resets ${esc(ucWeekName(end))}</span>`;
  const hits = [];
  for (let i = 1; i < p5.length; i++) if (p5[i][1] >= 100 && p5[i - 1][1] < 100) hits.push(p5[i][0]);
  if (p5.length && p5[0][1] >= 100 && p5[0][0] > from) hits.unshift(p5[0][0]);
  for (const t of hits) {
    svg += ln("ulhit", X(t), 0, X(t), H);
    if (!ucWide()) over += `<span class="ulhitlab" style="left:${left(t)}">5h limit hit ${ucHM(t)}</span>`;
  }
  const xy = (t, v) => X(Math.min(t, nowMs)).toFixed(2) + " " + v.toFixed(2);
  if (L.tok && pts.length) {
    // The rooms stacked bottom up in their order, so the top edge of the stack is the total line.
    let below = pts.map(() => 0);
    for (const n of okRooms) {
      const above = pts.map((p, i) => below[i] + (p.cum[n] || 0));
      const up = [xy(from, Y(0))].concat(pts.map((p, i) => xy(p.t, Y(above[i]))));
      const down = [xy(from, Y(0))].concat(pts.map((p, i) => xy(p.t, Y(below[i])))).reverse();
      svg += `<path class="${ucRoomClass(n)}" d="M${up.concat(down).join(" L")} Z"></path>`;
      below = above;
    }
    svg += `<path class="uck-line" d="M${[xy(from, Y(0))].concat(pts.map(p => xy(p.t, Y(p.run)))).join(" L")}"></path>`;
  }
  plotRuns.forEach((run, i) => {
    if (pts.length) svg += `<path class="ucpcline ucpc${i % UC_PLOT_COLOURS}" d="M${[xy(from, Y(0))].concat(pts.map((p, j) => xy(p.t, Y(run[j])))).join(" L")}"></path>`;
  });
  const stepPath = (steps, cls) => {
    let d = `M${xy(from, P(steps[0][1]))}`;
    for (let i = 1; i < steps.length; i++) d += ` L${xy(steps[i][0], P(steps[i - 1][1]))} L${xy(steps[i][0], P(steps[i][1]))}`;
    return `<path class="${cls}" d="${d} L${xy(nowMs, P(steps[steps.length - 1][1]))}"></path>`;
  };
  if (L.p5 && has5) svg += stepPath(p5, "ucp5");
  if (L.pw && hasW) svg += stepPath(pw, "ucpw");
  if (live) {
    if (proj && L.tok && rate > 0) svg += `<path class="uck-line uck-proj" d="M${X(nowMs).toFixed(2)} ${Y(run).toFixed(2)} L${X(end).toFixed(2)} ${Y(projRun).toFixed(2)}"></path>`;
    if (L.pw && projPw != null) svg += `<path class="ucpw uck-proj" d="M${X(nowMs).toFixed(2)} ${P(pwNow).toFixed(2)} L${X(end).toFixed(2)} ${P(projPw).toFixed(2)}"></path>`;
    svg += ln("ucnowln", X(nowMs), 0, X(nowMs), H);
    over += `<span class="ucnowlab" style="left:${left(nowMs)}">now</span>`;
  }
  // The rate under the plot, a bar a bucket with the rooms stacked in it, on the plot's own time axis.
  let maxTok = 0, bars = "";
  for (const e of bins) maxTok = Math.max(maxTok, e.sum);
  for (const e of bins) {
    if (!(e.sum > 0)) continue;
    const x = X(e.t), w = Math.max(X(Math.min(e.t + ms, nowMs)) - x - .4, .4);
    let y = 48, segs = "";
    for (const n of okRooms) {
      const h = (e.rooms[n] || 0) / maxTok * 46;
      if (!(h > 0)) continue;
      y -= h;
      segs += `<rect class="${ucRoomClass(n)}" x="${x.toFixed(2)}" y="${y.toFixed(2)}" width="${w.toFixed(2)}" height="${h.toFixed(2)}"></rect>`;
    }
    bars += `<g data-t="${e.t}">${segs}</g>`;
  }
  const rateAx = maxTok ? `<span class="ucrl" style="top:0">${usageTokens(Math.round(maxTok / ms * UL_HOUR))}/h</span><span class="ucrl ucrl0" style="top:100%">0</span>` : "";
  const times = ucTicks(from, end, false).map(([t, lab]) => `<span style="left:${left(t)}">${lab}</span>`).join("");
  const peak5 = p5.reduce((a, s) => Math.max(a, s[1]), 0);
  UC.cor = { from, end, nowMs, ms, pts, run, rate, pwNow, projPw, p5, pw, has5, hasW, wins, Y, P };
  const axis = `<span data-n="corrun" data-tip="counted tokens over the range, from the rooms that are ticked">${usageTokens(run)} counted</span>` +
    (hasW ? `<span data-n="corweek" data-tip="the weekly percent cards last reported in the range, and on the live week where this pace leaves it at the reset">week ${pwNow}%${projPw != null ? ` · at this pace ${Math.round(projPw)}% at the reset` : ""}</span>` : "") +
    (has5 ? `<span data-n="cor5h" data-tip="the highest 5h percent cards reported in the range, and how many times it reached 100%">5h peak ${peak5}%${hits.length ? ` · hit ${hits.length}×` : ""}</span>` : "");
  return `<h4 class="uch">tokens and percent used <span class="help" tabindex="0" data-tip="Counted tokens since the range began, stacked by room, against the 5h and weekly percent your cards reported. Hover the chart to read them all at one time, with the cards that spent most then.">?</span></h4>` +
    `<div class="ucchart" data-chart="correlate"><div class="ucread" aria-live="off"></div>` +
    `<div class="ucstage"><div class="ucplot">${yax}<svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none" role="img" aria-label="tokens and percent used over time">${svg}</svg>${over}</div>` +
    `<div class="ucrate">${rateAx}<svg viewBox="0 0 ${W} 48" preserveAspectRatio="none" role="img" aria-label="tokens per hour by room">${bars}</svg></div>` +
    `<div class="uctimes">${times}</div>` +
    `<i class="uchov" hidden></i><i class="uchovdot dt" hidden></i><i class="uchovdot d5" hidden></i><i class="uchovdot dw" hidden></i><b class="uchovtag" hidden></b></div>` +
    `<div class="ucaxis">${axis}</div></div>`;
}

// ---- the hover ----

// What the chart holds at t (clamped to it): the total, the rate and top cards of that bucket, and both percents.
// Pure on UC.cor, so a test can drive it.
function ucCorAt(c, t) {
  t = Math.max(c.from, Math.min(c.end, t));
  const out = { t, run: 0, rate: 0, p5: null, pw: null, proj: t > c.nowMs, top: [], bucket: null };
  if (out.proj) {
    out.run = c.run + c.rate * (t - c.nowMs);
    out.rate = c.rate * UL_HOUR;
    if (c.projPw != null) out.pw = c.pwNow + (c.projPw - c.pwNow) * (t - c.nowMs) / Math.max(c.end - c.nowMs, 1);
    return out;
  }
  let prev = 0;
  for (const p of c.pts) {
    const s = p.t - c.ms;
    if (t < s) break;
    if (t <= p.t) {
      out.run = prev + p.e.sum * (t - s) / c.ms;
      out.rate = p.e.sum / Math.max(Math.min(c.ms, c.nowMs - s), 1) * UL_HOUR;
      out.top = [...p.e.cards].filter(x => x[1] > 0).sort((a, b) => b[1] - a[1]).slice(0, 3);
      out.bucket = s;
      break;
    }
    prev = p.run;
    out.run = prev;
  }
  if (c.has5) out.p5 = ucPctAt(c.p5, t);
  if (c.hasW) out.pw = ucPctAt(c.pw, t);
  return out;
}

// The bubble's two lines: when, tokens, both percents, the rate, and the cards that spent most in that bucket.
function ucCorText(c, a) {
  const L = UC.lines, d = new Date(a.t);
  const when = `${UH_DOW[d.getDay()]} ${ulPad(d.getMonth() + 1)}-${ulPad(d.getDate())} ${ucHM(a.t)}`;
  const parts = [when, L.tok ? `${ucNum(a.run)} tokens` : "", L.p5 && a.p5 != null ? `5h ${Math.round(a.p5)}%` : "",
    L.pw && a.pw != null ? `week ${Math.round(a.pw)}%` : "", `${ucNum(a.rate)}/h`, a.proj ? "projected" : ""].filter(Boolean);
  const line = parts.join("\n");
  const top = a.proj ? "" : UC.plot.map(k => {
    let v = 0;
    for (const p of c.pts) { if (p.t - c.ms > a.t) break; v += p.e.cards.get(k) || 0; }
    const [room, id] = k.split("|");
    return `${ucTitle(room, id)} ${ucNum(v)}`;
  }).join("\n");
  const win = L.p5 && a.p5 != null ? c.wins.find(r => r - UL_WINDOW.five_hour <= a.t && a.t < r) : null;
  return { line, top, read: parts.join(" · ") + (win ? ` · 5h window ${ucHM(win - UL_WINDOW.five_hour)}–${ucHM(win)}` : "") };
}

function ucCorHide(chart) {
  if (!chart) return;
  for (const e of chart.querySelectorAll(".uchov, .uchovdot, .uchovtag")) e.hidden = true;
  const rd = chart.querySelector(".ucread");
  if (rd) rd.textContent = "";
  for (const g of chart.querySelectorAll(".ucrate g.on")) g.classList.remove("on");
}

// x is a client x.
function ucCorHover(stage, x) {
  const c = UC.cor, chart = stage.closest(".ucchart");
  if (!c || !c.pts.length || !chart) return;
  const r = stage.getBoundingClientRect();
  if (!(r.width > 0) || !isFinite(x)) return;
  const f = Math.max(0, Math.min(1, (x - r.left) / r.width));
  const a = ucCorAt(c, c.from + f * (c.end - c.from)), L = UC.lines, txt = ucCorText(c, a);
  chart.querySelector(".ucread").textContent = txt.read;
  const pos = (f * 100).toFixed(2) + "%", plotH = stage.querySelector(".ucplot").offsetHeight;
  const hov = chart.querySelector(".uchov");
  hov.hidden = false;
  hov.style.left = pos;
  const ys = [];
  const dot = (cls, y) => {
    const e = chart.querySelector(".uchovdot." + cls);
    e.hidden = y == null;
    if (y == null) return;
    ys.push(y);
    e.style.left = pos;
    e.style.top = (y / 100 * plotH).toFixed(1) + "px";
  };
  dot("dt", L.tok ? c.Y(a.run) : null);
  dot("d5", L.p5 && a.p5 != null ? c.P(a.p5) : null);
  dot("dw", L.pw && a.pw != null ? c.P(a.pw) : null);
  const tag = chart.querySelector(".uchovtag");
  tag.hidden = false;
  tag.innerHTML = esc(txt.line) + (txt.top ? "\n<em>" + esc(txt.top) + "</em>" : "");
  tag.style.left = pos;
  tag.style.top = Math.max(0, Math.min(100, ...ys) / 100 * plotH - tag.offsetHeight - 8).toFixed(1) + "px";
  const px = f * r.width, w = tag.offsetWidth + 10;
  tag.classList.toggle("flip", px + w > r.width && px > r.width - px);
  for (const g of chart.querySelectorAll(".ucrate g[data-t]")) g.classList.toggle("on", a.bucket != null && g.dataset.t === String(a.bucket));
}

// ---- wiring, by delegation ----

document.addEventListener("click", ev => {
  const b = ev.target.closest && ev.target.closest("#uc-pager button");
  if (!b || b.disabled) return;
  if (b.id === "uc-prev") UC.off--;
  else if (b.id === "uc-next") UC.off = Math.min(0, UC.off + 1);
  else if (b.id === "uc-now") UC.off = 0;
  else return;
  loadUsageTab();
});

document.addEventListener("change", ev => {
  const i = ev.target;
  if (i && i.id === "uc-date") { ucJumpTo(i.value); return; }
  if (i && i.dataset && i.dataset.plot) ucTogglePlot(i.dataset.plot);
});

document.addEventListener("input", ev => { if (ev.target && ev.target.id === "uc-cardq") ucPaintCardPop(); });

document.addEventListener("click", ev => {
  const pop = document.getElementById("uc-cardpop");
  const add = ev.target.closest && ev.target.closest("#uc-addcard");
  if (add) { ucOpenCardPop(add); return; }
  if (pop && !pop.hidden && !(ev.target.closest && ev.target.closest("#uc-cardpop"))) pop.hidden = true;
  const c = ev.target.closest && ev.target.closest("#uc-pick .ucchip");
  if (!c || c.classList.contains("dis")) return;
  if (c.dataset.unplot) { ucTogglePlot(c.dataset.unplot); return; }
  if (c.dataset.room != null) { if (UC.hide.has(c.dataset.room)) UC.hide.delete(c.dataset.room); else UC.hide.add(c.dataset.room); }
  if (c.dataset.line) UC.lines[c.dataset.line] = !UC.lines[c.dataset.line];
  ucPaint();
});

document.addEventListener("pointermove", ev => {
  const st = ev.target.closest && ev.target.closest(".ucchart[data-chart=correlate] .ucstage");
  if (st) ucCorHover(st, ev.clientX);
});
document.addEventListener("pointerdown", ev => {
  const st = ev.target.closest && ev.target.closest(".ucchart[data-chart=correlate] .ucstage");
  if (st) { ucCorHover(st, ev.clientX); return; }
  for (const ch of document.querySelectorAll(".ucchart[data-chart=correlate]")) ucCorHide(ch);
});
document.addEventListener("pointerout", ev => {
  if (ev.pointerType === "touch") return;
  const st = ev.target.closest && ev.target.closest(".ucchart[data-chart=correlate] .ucstage");
  if (st && !st.contains(ev.relatedTarget)) ucCorHide(st.closest(".ucchart"));
});
