// The efficacy page of the items tab: how WELL the process worked, as docs/rnd/item-lifecycle-design.md section 12 draws it
// (docs/rnd/img/item-lifecycle/efficacy-1400.png and efficacy-2400.png). A page of the items tab, reached from its `efficacy` pill and
// left by its crumb, drawn into #items-list by `itemsPaint`.
//
// THIS FILE ONLY DRAWS. The data is `itemsData.efficacy` (js/items-data.js), the hub's `GET /_hub/work/efficacy`, a read of what the hub's
// last collect derived. Four rules it keeps, each from the design:
//   - A DASH IS NOT A ZERO. A figure with no value is drawn "-", and a bar for a period with none is a stub, never an empty bar.
//   - EVERY FIGURE SAYS OBSERVED OR REPORTED, and "observed only" asks the hub to drop the reported inputs from all of them.
//   - THE ARROW IS GOOD OR BAD, not up or down: teal when better, orange when worse, neutral when under the noise floor. A week with
//     fewer than 5 landed items says "too few to trend", and a rate over fewer than 5 is greyed with its count.
//   - DOLLARS ARE ESTIMATES at list price, and say so.
let effData = null;            // what the hub answered for the query below
let effBy = "week";
let effObservedOnly = false;
let effSort = "worst";
let effHtmlMemo = "";

const EFF_ROWS = [
  { key: "clean_pass", name: "clean-pass rate" },
  { key: "first_pass", name: "first-pass test rate" },
  { key: "send_backs", name: "send-backs per item" },
  { key: "red_after", name: "red after landing" },
  { key: "bugs", name: "escaped bugs per item" },
  { key: "time_to_land", name: "median time to land" },
  { key: "cost", name: "cost per landed item" },
  { key: "waste", name: "waste" },
];
const EFF_SORTS = { worst: "worst first", newest: "newest first", cost: "costliest first" };
const EFF_STEPS = ["designed", "building", "tested", "reviewed", "landed"];

// "7h 20m", "2h 05m", "22m", "1d 2h": seconds as the page says a span.
function effDur(sec) {
  const m = Math.round(Math.max(0, sec) / 60);
  if (m < 1) return "0m";
  if (m < 60) return m + "m";
  if (m < 1440) return Math.floor(m / 60) + "h " + String(m % 60).padStart(2, "0") + "m";
  return Math.floor(m / 1440) + "d " + Math.floor((m % 1440) / 60) + "h";
}

const effUsd = v => v == null ? "?" : "$" + v.toFixed(2);
const effDash = '<span class="ef-dash">&ndash;</span>';

// The figure's own number, as the large monospace value says it. A null value is a dash.
function effValue(f) {
  if (!f || f.value == null) return effDash;
  switch (f.key) {
    case "red_after": return f.num + " of " + f.den;
    case "clean_pass": case "first_pass": return Math.round(f.value * 100) + "%";
    case "send_backs": case "bugs": return f.value.toFixed(1);
    case "time_to_land": return effDur(f.value);
    default: return effUsd(f.value);
  }
}

function effSigned(n, text) { return (n > 0 ? "+" : n < 0 ? "-" : "") + text; }

// The trend's words and colour. `was` figures are said the way the mockup says them: "was 0 of 6".
function effTrend(key, t, f) {
  const arrow = t.verdict === "too_few" || t.verdict === "none" ? "" : t.dir === "up" ? "&#9650; " : t.dir === "down" ? "&#9660; " : "&#9644; ";
  const cls = t.verdict === "better" ? "g" : t.verdict === "worse" ? "b" : "n";
  const span = effBy === "day" ? "day" : "week";
  let text;
  if (t.verdict === "too_few") text = "too few to trend";
  else if (t.verdict === "none" || t.delta == null) text = "nothing to compare";
  else if (t.verdict === "same") text = "same (under noise floor)";
  else {
    const was = t.was || {};
    const d = t.delta;
    if (f.unit === "pct") text = effSigned(d, Math.abs(Math.round(d * 100))) + " pts vs last " + span;
    else if (key === "red_after") text = "was " + (was.num || 0) + " of " + (was.den || 0);
    else if (f.unit === "per_item") text = effSigned(d, Math.abs(d).toFixed(1)) + " vs last " + span;
    else if (f.unit === "seconds") text = effSigned(d, effDur(Math.abs(d))) + " vs last " + span;
    else if (key === "waste") text = "was " + effUsd(was.value);
    else text = effSigned(d, Math.round(Math.abs(d) * 100 / (was.value || 1)) + "%") + ", estimated";
  }
  return '<span class="ef-tr ' + cls + '">' + arrow + esc(text) + "</span>";
}

// The line under a row's name: the count, the parts, and how much of it leaned on a reported input.
function effSub(key, f, d) {
  if (!f || f.value == null) return key === "clean_pass" ? "none with an answer yet: the 7 days must end, and a test must have been seen" : "none yet";
  const p = f.parts || {};
  const rep = f.reported_n ? " &middot; " + f.reported_n + " of " + f.den + " reported" : "";
  switch (key) {
    case "clean_pass": case "first_pass": return f.num + " of " + f.den + rep;
    case "send_backs": return "design " + p.design.toFixed(1) + " &middot; review " + p.review.toFixed(1) + " &middot; not accepted " + p.not_accepted.toFixed(1) + rep;
    case "red_after": return "a red CI naming the item, within 7 days of landing";
    case "bugs": return p.bugs + " " + (p.bugs === 1 ? "bug" : "bugs") + " within 7 days, " + p.reverts + " " + (p.reverts === 1 ? "revert" : "reverts") + rep;
    case "time_to_land": return "of which waiting on you " + effDur(p.wait || 0);
    case "cost": return "steps " + effUsd(f.value) + " &middot; + coordination " + effUsd(p.coord) + " (" + Math.round((p.share || 0) * 100) + "%)";
    default: return f.num + " " + (f.num === 1 ? "item" : "items") + " dropped &middot; " + Math.round((p.share || 0) * 100) + "% of spend";
  }
}

// One bar per period, the current one open. A period with no value is a stub, so a dash is never drawn as a low number.
function effStrip(key, periods) {
  const vals = periods.map(p => p.figures[key] && p.figures[key].value);
  const max = Math.max(1e-9, ...vals.map(v => v == null ? 0 : v));
  return '<div class="ef-strip" data-key="' + key + '">' + periods.map((p, i) => {
    const v = vals[i];
    const cls = (p.partial ? "o" : "") + (v == null ? " none" : "");
    const h = v == null ? 6 : Math.max(10, Math.round(v * 100 / max));
    return '<i class="' + cls.trim() + '" style="height:' + h + '%"></i>';
  }).join("") + "</div>";
}

const effTag = f => '<span class="it-obs' + (f && f.source === "reported" ? " r" : "") + '">' + esc(f && f.source || "observed") + "</span>";

function effRow(r, d) {
  const f = d.periods[d.periods.length - 1].figures[r.key];
  const t = d.trends[r.key];
  return '<div class="ef-rr' + (f && f.greyed ? " grey" : "") + '" data-row="' + r.key + '"><div><div class="ef-rn">' + esc(r.name) + '</div><div class="ef-rs">' + effSub(r.key, f, d) +
    '</div></div><div class="ef-rv">' + effValue(f) + "</div><div>" + effTrend(r.key, t, f || {}) + "</div>" + effStrip(r.key, d.periods) + "<div>" + effTag(f) + "</div></div>";
}

function effTile(label, value, small, trend, hot, extra) {
  return '<div class="it-stat' + (hot ? " hot" : "") + '"><div class="it-k">' + esc(label) + '</div><div class="it-v">' + value + (small ? "<small>" + small + "</small>" : "") + "</div>" +
    '<div class="ef-tile-tr">' + trend + (extra || "") + "</div></div>";
}

function effTiles(d) {
  const cur = d.periods[d.periods.length - 1].figures, tr = d.trends;
  const cp = cur.clean_pass, sb = cur.send_backs, cost = cur.cost;
  const sp = sb.parts || {};
  const w = d.wrong;
  const span = effBy === "day" ? "day" : "week";
  const wrongTrend = w.total === w.was ? '<span class="ef-tr n">&#9644; same as last ' + span + "</span>"
    : '<span class="ef-tr ' + (w.total > w.was ? "b" : "g") + '">' + (w.total > w.was ? "&#9660;" : "&#9650;") + " was " + w.was + " last " + span + "</span>";
  return '<div class="it-stats ef-tiles">' +
    effTile("clean-pass rate", effValue(cp), cp.value == null ? "none yet" : cp.num + " of " + cp.den, effTrend("clean_pass", tr.clean_pass, cp)) +
    effTile("send-backs per item", effValue(sb), sb.value == null ? "none yet" : "design " + Math.round(sp.design * sb.den) + " &middot; review " + Math.round(sp.review * sb.den) +
      " &middot; not accepted " + Math.round(sp.not_accepted * sb.den), effTrend("send_backs", tr.send_backs, sb)) +
    effTile("went wrong after landing", w.total, w.red + " red CI &middot; " + w.bugs + " " + (w.bugs === 1 ? "bug" : "bugs") + " &middot; " + w.reverts + " " + (w.reverts === 1 ? "revert" : "reverts"),
      wrongTrend, w.total > 0, w.source === "reported" ? " " + effTag({ source: "reported" }) : "") +
    effTile("cost per landed item", effValue(cost), cost.value == null ? "" : "+ " + effUsd((cost.parts || {}).coord) + " coordination",
      effTrend("cost", tr.cost, cost).replace("ef-tr n", "ef-tr n").replace(/^<span class="ef-tr (\w)">/, '<span class="ef-tr $1">estimated, list price &middot; ')) + "</div>";
}

function effStepsPanel(d) {
  const by = d.send_backs_by_step || {};
  const max = Math.max(0, ...EFF_STEPS.map(s => by[s] || 0));
  return '<div class="ef-panel"><h3>Send-backs by step &middot; this ' + effBy + "</h3>" + EFF_STEPS.map(s => {
    const n = by[s] || 0;
    const hot = max > 0 && n * 4 >= max * 3;
    return '<div class="it-bar ef-sbar"><span>' + s + '</span><div><i class="' + (hot ? "hot" : "") + '" style="width:' + (max ? n * 100 / max : 0) + '%"></i></div><b>' + n + "</b></div>";
  }).join("") + '<div class="ef-note">Where the process failed first.' + (max > 0 ? " " + EFF_STEPS.filter(s => (by[s] || 0) === max).map(s => s.replace(/^./, c => c.toUpperCase())).join(" and ") + " is the step to fix." : "") + "</div></div>";
}

function effTimePanel(d) {
  const t = d.time;
  const parts = [["work", t.work, "var(--teal)"], ["waiting on you", t.wait, "var(--wait)"], ["CI and builds", t.ci, "var(--path)"]];
  const sum = parts.reduce((a, p) => a + p[1], 0) || 1;
  if (!t.median) return '<div class="ef-panel"><h3>Where the time went &middot; median landed item</h3><div class="it-big">' + effDash + '</div><div class="ef-note">No item landed in this ' + effBy + " yet.</div></div>";
  return '<div class="ef-panel"><h3>Where the time went &middot; median landed item</h3><div class="it-big">' + effDur(t.median) + '</div><div class="ef-split">' +
    parts.map(p => '<i class="ef-seg" style="width:' + p[1] * 100 / sum + '%;background:' + p[2] + '"></i>').join("") + '</div><div class="it-leg">' +
    parts.map(p => '<span><i style="background:' + p[2] + '"></i>' + p[0] + " " + effDur(p[1]) + "</span>").join("") + "</div>" +
    '<div class="it-kv ef-kv1"><span>slowest step (p90)</span><b>' + (t.p90_step ? esc(t.p90_step) + " " + effDur(t.p90) : "&ndash;") + "</b></div>" +
    '<div class="it-kv"><span>longest wait on you</span><b>' + (t.long_wait ? esc(t.long_wait_step) + " " + effDur(t.long_wait) : "&ndash;") + "</b></div></div>";
}

function effAbandonedPanel(d) {
  const cur = d.periods[d.periods.length - 1].figures.waste || {};
  const share = (cur.parts || {}).share;
  return '<div class="ef-panel"><h3>Abandoned or dropped &middot; this ' + effBy + "</h3>" +
    (d.abandoned.length ? d.abandoned.map(a => '<div class="it-kv"><span data-tip="' + esc(a.reason || "") + '">' + esc(a.id) + "</span><b>" + effUsd(a.usd) + "</b></div>").join("") +
      '<div class="it-kv"><span>total' + (share != null ? ", " + Math.round(share * 100) + "% of spend" : "") + "</span><b>" + effUsd(d.abandoned_total) + "</b></div>"
      : '<div class="ef-note">Nothing was given up on.</div>') + "</div>";
}

function effLandedRow(r) {
  return '<div class="ef-ir" data-item="' + esc(r.id) + '"><div><div class="ef-ttl">' + esc(r.title) + '</div><div class="it-meta"><span class="it-id">' + esc(r.id) + "</span>" +
    (r.dept ? '<span class="it-tag">' + esc(r.dept) + "</span>" : "") + '</div></div><div><span class="ef-mk ' + esc(r.mark.tone) + '">' + esc(r.mark.text) + "</span>" +
    (r.reported ? ' <span class="it-obs r">reported</span>' : "") + '</div><div class="ef-age">' + esc(r.send_backs) + '</div><div class="ef-age">' +
    (r.total_seconds ? effDur(r.total_seconds) : "&ndash;") + '</div><div class="ef-usd">' + (r.usd == null ? "?" : effUsd(r.usd)) + "</div></div>";
}

function effSorted(rows) {
  const r = rows.slice();
  if (effSort === "newest") r.sort((a, b) => (b.landed_at || "").localeCompare(a.landed_at || ""));
  else if (effSort === "cost") r.sort((a, b) => (b.usd || 0) - (a.usd || 0));
  return r;
}

function effSubtitle(d) {
  const now = new Date(d.now);
  if (effBy === "day") return "How well the process works: what it got right and wrong on landed items. Today so far, against yesterday to the same time.";
  const day = now.toLocaleDateString("en-US", { weekday: "long" });
  return "How well the process works: what it got right and wrong on landed items. This week, Monday to " + day + ", against last week to the same day.";
}

function efficacyHtml() {
  const d = effData;
  const head = '<div class="it-crumb"><a href="#" data-act="eff-back">Items</a> &rsaquo; efficacy</div>';
  if (!d) return '<div class="ef-page">' + head + '<p class="pane-lead">Loading the efficacy&hellip;</p></div>';
  const cur = d.periods[d.periods.length - 1];
  const controls = '<div class="ef-seg" role="group" aria-label="period"><button type="button" class="' + (effBy === "day" ? "on" : "") + '" data-act="eff-by" data-by="day">day</button>' +
    '<button type="button" class="' + (effBy === "week" ? "on" : "") + '" data-act="eff-by" data-by="week">week</button></div>' +
    '<button type="button" class="it-pill ' + (effObservedOnly ? "on" : "on") + ' ef-obs' + (effObservedOnly ? " strict" : "") + '" data-act="eff-obs" aria-pressed="' + effObservedOnly + '">observed only: ' + (effObservedOnly ? "on" : "off") + "</button>" +
    '<button type="button" class="it-pill" data-act="eff-sort" data-tip="change the order of the landed items">sort: ' + EFF_SORTS[effSort] + "</button>";
  const few = cur.too_few ? '<div class="ef-toofew" data-role="too-few">Too few to trend: ' + cur.landed + " " + (cur.landed === 1 ? "item" : "items") + " landed this " + effBy + ", and 5 are needed for an arrow.</div>" : "";
  return '<div class="ef-page">' + head + '<div class="ef-top"><div class="ef-title"><h1>Efficacy</h1><div class="it-sub">' + esc(effSubtitle(d)) + "</div></div>" + controls + "</div>" + few +
    effTiles(d) +
    '<div class="ef-two"><div class="ef-panel ef-rollups"><h3>Roll-ups &middot; this ' + effBy + ", trend, last " + (d.periods.length) + " " + effBy + "s (open bar is this " + effBy + " so far)</h3>" +
    EFF_ROWS.map(r => effRow(r, d)).join("") + '<div class="ef-note">Arrow colour is better or worse, not up or down. A rate over fewer than 5 landed items is greyed and left out of the arrow. ' +
    esc((d.notes || [])[0] || "") + "</div></div>" +
    "<div>" + effStepsPanel(d) + effTimePanel(d) + effAbandonedPanel(d) + "</div></div>" +
    '<div class="ef-panel ef-landed"><h3>Landed this ' + effBy + ", " + EFF_SORTS[effSort] + " &middot; what each one got right and wrong</h3>" +
    (d.landed.length ? effSorted(d.landed).map(effLandedRow).join("") : '<div class="ef-note">Nothing has landed this ' + effBy + ".</div>") + "</div></div>";
}

async function effLoad() {
  try {
    effData = await itemsData.efficacy({ by: effBy, observedOnly: effObservedOnly });
  } catch (e) {
    effData = null;
    itemsNote("The efficacy is not answering right now: " + e.message);
  }
  if (itemsPage === "efficacy") itemsPaint();
}

// The strip of one item's own efficacy, for the side of its page. Nothing before the hub has derived it.
function itemEfficacyHtml(d) {
  const e = d.efficacy;
  if (!e || !e.outcome) return "";
  const o = e.outcome, x = e.detail || {};
  const kv = (l, v) => '<div class="it-kv"><span>' + l + "</span><b>" + v + "</b></div>";
  const sb = (x.send_backs || {}), n = (sb.design || 0) + (sb.review || 0) + (sb.not_accepted || 0);
  const mark = e.mark ? '<span class="ef-mk ' + esc(e.mark.tone) + '">' + esc(e.mark.text) + "</span>" : "";
  const fp = o.first_pass ? (o.first_pass === "pass" ? "passed" : "failed") + (o.first_pass_source === "reported" ? " (reported)" : "") : "&ndash;";
  return '<div class="ef-item" data-role="efficacy"><h3>Efficacy ' + (e.reported ? '<span class="it-obs r">reported inputs</span>' : '<span class="it-obs">observed</span>') + "</h3>" +
    (mark ? '<div class="ef-itemmark">' + mark + "</div>" : "") +
    kv("send-backs", o.landed_at || n ? n + (n ? " (design " + (sb.design || 0) + ", review " + (sb.review || 0) + ", not accepted " + (sb.not_accepted || 0) + ")" : "") : "&ndash;") +
    kv("first-pass test", fp) +
    kv("CI red after landing", !o.landed_at ? "&ndash;" : x.red_episodes ? x.red_episodes + " for " + effDur(o.ci_red_seconds) + (x.red_open ? ", still red" : "") : x.ci_known ? "none" : "not read") +
    kv("bugs within 7 days", !o.landed_at ? "&ndash;" : (o.escaped_bugs || 0) + (o.reverted ? ", " + o.reverted + " reverted" : "")) +
    kv("waiting on you", o.landed_at ? effDur(o.wait_human_seconds) + " of " + effDur(x.total_seconds || 0) : "&ndash;") + "</div>";
}
