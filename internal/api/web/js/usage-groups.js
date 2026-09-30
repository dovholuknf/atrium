// The usage tab, grouped by department or director, and tokens per accepted item.
//
// GET /v1/usage?group=dept|launcher adds `groups` ({key: sums}) to every bucket
// (internal/api/usage.go). The key "" is a card with no department, or one the
// operator launched; "@before" is a row written before the room kept either.
// GET /v1/usage/items answers what each accepted work item cost
// (internal/api/usageitems.go). The room owns the link rule. Nothing here is
// stored. Loaded after js/usage-charts.js, which calls into it.
//
// The answer holds group sums only, never which card is in which group, so a
// click on a group filters the card list by what the board knows of each card
// now (lastTasks). A card the room has pruned drops out of a filtered list.

const UC_GROUPS = [["card", "card"], ["dept", "department"], ["launcher", "director"]];
const UC_REREAD_MS = 5000;

Object.assign(UC, {
  group: "card",     // card | dept | launcher
  groupKey: null,    // the group clicked, or null
  stale: false,      // a live usage event arrived while grouped
  lastRead: 0,
  rereadTimer: 0,
  items: null,       // null (not asked) | {state: ok|old|down, rows, unlinked}
  itemsSeq: 0,
});

function ucGroupParam() { return UC.group === "card" ? "" : UC.group; }

// A room with rows but no `groups` in any bucket is an older build that ignored the param.
function ucNoGroups(series) {
  if (UC.group === "card") return false;
  const bs = (series && series.buckets) || [];
  return bs.length > 0 && !bs.some(b => b.groups);
}

function ucIngestGroups(cur, b) {
  if (!b.groups) return;
  if (!cur.groups) cur.groups = {};
  for (const [k, s] of Object.entries(b.groups)) ucAdd(cur.groups[k] || (cur.groups[k] = ucSums()), s);
}

function ucGroupLabel(key) {
  if (key === "@before") return "before grouping";
  if (key === "") return UC.group === "dept" ? "no department" : "clint";
  return key;
}

// ---- the heading with its select ----

function ucCardsHead() {
  return `<h4 class="uch">by card <select id="uc-group" aria-label="group by" data-tip="Group the spend by card, by department (the card's dept: tag) or by director (the card that launched it), kept as of each turn">` +
    UC_GROUPS.map(g => `<option value="${g[0]}"${g[0] === UC.group ? " selected" : ""}>${g[1]}</option>`).join("") + `</select> ` +
    `<span class="ucnote">top ${UC_TOP_CARDS} by ${UC.cacheReads ? "" : "counted "}tokens, the rest as others. Click one to filter</span></h4>`;
}

// ---- the group tiles ----

function ucGroupRows() {
  const rows = new Map();
  const old = [];
  for (const n of Object.keys(UC.rooms)) {
    const r = UC.rooms[n];
    if (r.state !== "ok") continue;
    if (r.noGroups) old.push(n);
    for (const [t, b] of r.buckets) {
      for (const [k, s] of Object.entries(b.groups || {})) {
        let g = rows.get(k);
        if (!g) rows.set(k, g = { key: k, sums: ucSums(), per: new Map() });
        ucAdd(g.sums, s);
        g.per.set(t, (g.per.get(t) || 0) + ucShown(s));
      }
    }
  }
  return { rows, old };
}

function ucGroups(series) {
  if (UC.group === "card") return "";
  const { rows, old } = ucGroupRows();
  let html = "";
  for (const n of old) {
    html += `<div class="ucnote" data-role="nogroups">${esc(n || "this room")} does not group usage yet (an older build). Update it to see the groups.</div>`;
  }
  const list = [...rows.values()].sort((a, b) => ucShown(b.sums) - ucShown(a.sums));
  if (!list.length) return html;
  const ax = ucAxis(series);
  const W = 200, H = 40, bw = W / ax.n;
  html += `<div class="ucminis" data-role="groups">`;
  for (const g of list) {
    let m = 0;
    for (const v of g.per.values()) m = Math.max(m, v);
    let bars = "";
    for (const [t, v] of g.per) {
      const i = Math.round((t - ax.first) / ax.ms);
      if (i < 0 || i >= ax.n || !(v > 0)) continue;
      const h = v / (m || 1) * (H - 2);
      bars += `<rect class="uck-bar" x="${(i * bw).toFixed(2)}" y="${(H - h).toFixed(2)}" width="${Math.max(bw - 0.4, 0.4).toFixed(2)}" height="${h.toFixed(2)}"></rect>`;
    }
    const sel = UC.groupKey === g.key;
    html += `<div class="ucmini${sel ? " sel" : ""}" data-group="${esc(g.key)}" data-tip="${esc(sel ? "Click again to show every group" : "Filter the card list to this group")}">` +
      `<div class="ucminit"><span class="uctitle">${esc(ucGroupLabel(g.key))}</span>` +
      `<b data-n="group:${esc(g.key)}" data-tip="${esc(USAGE_TIPS.cardTotal)}">${usageTokens(ucShown(g.sums))}</b></div>` +
      `<svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none">${bars}</svg></div>`;
  }
  return html + `</div><div class="ucnote" style="margin:6px 0">${UC.groupKey === null ? "cards, all groups" : `cards in ${esc(ucGroupLabel(UC.groupKey))}`}</div>`;
}

// Where a card files now, by what the board holds of it: the same rule as the room's.
function ucCardGroup(room, id) {
  const all = typeof lastTasks !== "undefined" ? lastTasks : [];
  const t = all.find(x => bareId(x.id) === id && (!room || (x.room || roomOf(x.id) || roomNow()) === room));
  if (!t) return null;
  const tags = t.tags || [];
  if (UC.group === "dept") {
    const d = tags.find(x => /^dept:./i.test(String(x).trim()));
    return d ? String(d).trim().slice(5).trim() : "";
  }
  const name = c => c.alias || c.wire_name || "";
  if (tags.includes("atrium:director")) return name(t);
  if (!t.spawned_by && !t.spawned_by_id) return "";
  const p = t.spawned_by_id && all.find(x => x.id === t.spawned_by_id || bareId(x.id) === bareId(t.spawned_by_id));
  return p ? name(p) : (t.spawned_by || "");
}

function ucGroupFilter(cardRows) {
  if (UC.group === "card" || UC.groupKey === null) return cardRows;
  const out = new Map();
  for (const [k, c] of cardRows) if (ucCardGroup(c.room, c.id) === UC.groupKey) out.set(k, c);
  return out;
}

// ---- live events while grouped: stale, then at most one re-read per 5s, trailing ----

function ucGroupStale() {
  if (UC.group === "card" || typeof isViewing !== "function" || !isViewing("usage")) return;
  UC.stale = true;
  if (UC.rereadTimer) return;
  const wait = Math.max(500, UC_REREAD_MS - (Date.now() - UC.lastRead));
  UC.rereadTimer = setTimeout(() => {
    UC.rereadTimer = 0;
    if (!UC.stale || UC.group === "card" || !isViewing("usage")) return;
    UC.stale = false;
    loadUsageTab();
  }, wait);
}

// ---- tokens per accepted item ----

async function ucFetchItems(room, since) {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), 15000);
  try {
    const res = await plainFetch(`/v1/usage/items?since=${encodeURIComponent(new Date(since).toISOString())}`,
      { headers: room ? { "X-Atrium-Room": room } : {}, signal: ctrl.signal });
    if (res.status === 404) return { state: "old" };
    if (!res.ok) return { state: "down" };
    return { state: "ok", data: await res.json() };
  } catch (e) {
    return { state: "down" };
  } finally {
    clearTimeout(timer);
  }
}

async function ucLoadItems(asked, since) {
  const seq = ++UC.itemsSeq;
  const got = await Promise.all(asked.map(r => ucFetchItems(r, since).then(x => [r, x])));
  if (seq !== UC.itemsSeq) return;
  const oks = got.filter(([, x]) => x.state === "ok");
  if (!oks.length) {
    UC.items = { state: got.every(([, x]) => x.state === "old") ? "old" : "down", rows: [], unlinked: 0 };
  } else {
    const rows = [];
    let unlinked = 0;
    for (const [r, x] of oks) {
      unlinked += Number(x.data && x.data.unlinked) || 0;
      for (const it of (x.data && x.data.items) || []) rows.push(Object.assign({ room: oks.length > 1 ? r : "" }, it));
    }
    UC.items = { state: "ok", rows, unlinked };
  }
  if (isViewing("usage")) ucPaint();
}

function ucMedian(vals) {
  if (!vals.length) return 0;
  const v = vals.slice().sort((a, b) => a - b), m = v.length >> 1;
  return v.length % 2 ? v[m] : (v[m - 1] + v[m]) / 2;
}

function ucItems() {
  const it = UC.items;
  if (!it) return "";
  const head = `<h4 class="uch">per accepted item <span class="ucnote">last ${esc(UC.range)}, what the work that landed cost</span></h4>`;
  if (it.state === "old") return head + `<div class="ucnote" data-role="items-old">this room is too old to total tokens per item.</div>`;
  if (it.state !== "ok") return head + `<div class="ucnote" data-role="items-old">the room did not answer for items.</div>`;
  const rows = it.rows.slice().sort((a, b) => b.counted - a.counted);
  let html = head;
  if (UC.group === "dept") {
    html += `<div class="ucnote" data-role="items-nodept">items carry no department, so they are not grouped here.</div>`;
  }
  if (rows.length) {
    html += `<div class="ucitems">` + rows.map(r => {
      const shared = (r.split || 1) > 1;
      const tip = shared ? ` data-tip="${esc(`the cost is shared: a card of this item also counts for ${r.split} accepted items, so its tokens are divided evenly between them`)}"` : "";
      return `<span${tip}><span class="ucroom">${esc(r.item)}</span> ${r.room ? `<span class="ucroom">${esc(r.room)}</span> ` : ""}${esc(r.title || "")}${shared ? ` <span class="ucnote">shared ÷${r.split}</span>` : ""}</span>` +
        `<span${tip}>${usageTokens(r.counted)}</span>` +
        `<span${tip}>${r.cards} card${r.cards === 1 ? "" : "s"}</span>` +
        `<span class="uccread"${tip}>${usageTokens(r.cache_read)} cache read</span>`;
    }).join("") + `</div>`;
  }
  const med = ucMedian(rows.map(r => Number(r.counted) || 0));
  html += `<div class="uclegend" data-role="items-foot"><span data-tip="the middle of the counted tokens of the items above, cache reads apart">` +
    (rows.length ? `median ${usageTokens(med)} per item · ${rows.length} item${rows.length === 1 ? "" : "s"} accepted` : "no accepted item with a card on record") + `</span>` +
    (it.unlinked > 0 ? ` <span data-tip="left out of the median, so it does not flatter">· ${it.unlinked} accepted item${it.unlinked === 1 ? " has" : "s have"} no card on record</span>` : "") + `</div>`;
  return html;
}

// ---- wiring ----

document.addEventListener("change", ev => {
  if (ev.target && ev.target.id === "uc-group") {
    UC.group = ev.target.value;
    UC.groupKey = null;
    UC.stale = false;
    loadUsageTab();
  }
});

document.addEventListener("click", ev => {
  const g = ev.target.closest && ev.target.closest(".ucmini[data-group]");
  if (!g) return;
  UC.groupKey = UC.groupKey === g.dataset.group ? null : g.dataset.group;
  ucPaint();
});
