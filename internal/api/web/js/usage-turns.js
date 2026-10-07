// The usage tab's turns atrium caused: what each kind of text atrium typed or queued into a session cost in the
// turn after it.
//
// GET /v1/usage/turns (internal/api/turncost.go) answers rows by day, card and kind, the costliest turns and a
// count still waiting for their turn to end. Atrium is the only party that knows why a turn happened, and the room
// joins that to the transcript at the Stop (internal/daemon/turncost.go). Nothing here is stored or decided: it sums
// and sorts what came, and says which are estimates. Loaded after js/usage-groups.js, which calls into it.
//
// NO THRESHOLDS AND NO FINDINGS. It reports what happened. What counts as too much is somebody else's call.

Object.assign(UC, {
  turns: null,       // null (not asked) | {state: ok|old|down, rows, top, pending}
  turnsSeq: 0,
});

async function ucFetchTurns(room, since) {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), 15000);
  try {
    const res = await plainFetch(`/v1/usage/turns?since=${encodeURIComponent(new Date(since).toISOString())}`,
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

async function ucLoadTurns(asked, since) {
  const seq = ++UC.turnsSeq;
  const got = await Promise.all(asked.map(r => ucFetchTurns(r, since).then(x => [r, x])));
  if (seq !== UC.turnsSeq) return;
  const oks = got.filter(([, x]) => x.state === "ok");
  if (!oks.length) {
    UC.turns = { state: got.every(([, x]) => x.state === "old") ? "old" : "down", rows: [], top: [], pending: 0 };
  } else {
    const t = { state: "ok", rows: [], top: [], pending: 0 };
    for (const [r, x] of oks) {
      const room = oks.length > 1 ? r : "";
      t.pending += Number(x.data && x.data.pending) || 0;
      for (const row of (x.data && x.data.rows) || []) t.rows.push(Object.assign({ room }, row));
      for (const d of (x.data && x.data.top) || []) t.top.push(Object.assign({ room }, d));
    }
    t.top.sort((a, b) => ucTurnTokens(b) - ucTurnTokens(a));
    t.top = t.top.slice(0, 10);
    UC.turns = t;
  }
  if (isViewing("usage")) ucPaint();
}

// Tokens counted the way the rest of the tab counts them, cache reads apart.
function ucTurnTokens(r) {
  return (Number(r.input) || 0) + (Number(r.output) || 0) + (Number(r.cache_write) || 0) +
    (Number(r.cache_write_5m) || 0) + (Number(r.cache_write_1h) || 0);
}

function ucUsd(v) {
  v = Number(v) || 0;
  if (v === 0) return "$0";
  return v < 0.01 ? "<$0.01" : "$" + v.toFixed(2);
}

function ucSumKinds(rows, keyOf) {
  const out = new Map();
  for (const r of rows) {
    const k = keyOf(r);
    let s = out.get(k);
    if (!s) out.set(k, s = { key: k, label: r.name || r.task_id || k, turns: 0, tokens: 0, read: 0, cost: 0, noise: 0, kinds: new Map() });
    s.turns += r.turns;
    s.tokens += ucTurnTokens(r);
    s.read += Number(r.cache_read) || 0;
    s.cost += Number(r.cost) || 0;
    s.noise += r.noise_turns || 0;
    const kk = s.kinds.get(r.kind) || { tokens: 0, cost: 0 };
    kk.tokens += ucTurnTokens(r);
    kk.cost += Number(r.cost) || 0;
    s.kinds.set(r.kind, kk);
  }
  return [...out.values()];
}

function ucKindChips(s) {
  return [...s.kinds.entries()].sort((a, b) => b[1].tokens - a[1].tokens)
    .map(([k, v]) => `${esc(k)} ${usageTokens(v.tokens)}`).join(" · ");
}

function ucTurnTable(head, list, labelOf) {
  if (!list.length) return "";
  return `<h4 class="uch">${head}</h4><div class="ucturns">` +
    `<span class="ucnote">&nbsp;</span><span class="ucnote">turns</span><span class="ucnote">tokens</span><span class="ucnote">est.</span><span class="ucnote">by kind</span>` +
    list.map(s => `<span>${labelOf(s)}</span><span>${s.turns}</span><span>${usageTokens(s.tokens)}</span><span>${ucUsd(s.cost)}</span>` +
      `<span class="ucnote">${ucKindChips(s)}</span>`).join("") + `</div>`;
}

function ucTurns() {
  const t = UC.turns;
  if (!t) return "";
  const head = `<h4 class="uch">turns atrium caused <span class="ucnote">last ${esc(UC.range)}, by what atrium typed or queued into the session. Dollars are estimates from list prices</span></h4>`;
  if (t.state === "old") return head + `<div class="ucnote" data-role="turns-old">this room is too old to cost its turns by cause.</div>`;
  if (t.state !== "ok") return head + `<div class="ucnote" data-role="turns-old">the room did not answer for turn costs.</div>`;
  if (!t.rows.length) {
    return head + `<div class="ucnote" data-role="turns-none">no turn has been costed in this range yet${t.pending ? `, ${t.pending} still waiting for their turn to end` : ""}.</div>`;
  }
  const byKind = ucSumKinds(t.rows, r => r.kind).sort((a, b) => b.tokens - a.tokens);
  const byCard = ucSumKinds(t.rows, r => r.room + "|" + r.task_id).sort((a, b) => b.tokens - a.tokens).slice(0, 12);
  const byDay = ucSumKinds(t.rows, r => r.day).sort((a, b) => a.key < b.key ? 1 : -1).slice(0, 14);
  let html = head;
  html += `<div class="ucturns ucturns-kind" data-role="turns-kind">` +
    `<span class="ucnote">kind</span><span class="ucnote">turns</span><span class="ucnote">tokens</span><span class="ucnote">est.</span><span class="ucnote">noise</span>` +
    byKind.map(s => `<span>${esc(s.key)}</span><span>${s.turns}</span><span data-tip="${esc(usageTokens(s.read) + " cache read, counted apart")}">${usageTokens(s.tokens)}</span>` +
      `<span>${ucUsd(s.cost)}</span><span>${s.noise ? s.noise + " turn" + (s.noise === 1 ? "" : "s") : ""}</span>`).join("") + `</div>`;
  html += ucTurnTable("per card", byCard, s => esc(s.label));
  html += ucTurnTable("per day", byDay, s => esc(s.key));
  // The costliest turns an atrium text started, not the operator's.
  if (t.top.length) {
    html += `<h4 class="uch">costliest ${t.top.length} <span class="ucnote">turns atrium caused, by tokens counted</span></h4><div class="ucturns ucturns-top" data-role="turns-top">` +
      t.top.map(d => `<span>${esc(d.name || d.task_id)}${d.room ? ` <span class="ucroom">${esc(d.room)}</span>` : ""}</span>` +
        `<span>${esc(d.kind)}${d.from ? ` <span class="ucnote">from ${esc(d.from)}</span>` : ""}</span>` +
        `<span class="ucnote">${esc(d.reply || "")}</span>` +
        `<span>${usageTokens(ucTurnTokens(d))}</span><span>${ucUsd(d.cost)}</span>`).join("") + `</div>`;
  }
  // Turns whose reply was only an ack, or nothing.
  let n = 0, tok = 0, cost = 0, none = 0, ack = 0;
  for (const r of t.rows) {
    if (r.kind === "operator") continue;
    n += r.noise_turns || 0; tok += Number(r.noise_tokens) || 0; cost += Number(r.noise_cost) || 0;
    none += r.none_turns || 0; ack += r.ack_turns || 0;
  }
  html += `<div class="uclegend" data-role="turns-noise"><span data-tip="turns an atrium text started whose reply was only an acknowledgement, or no words and no tool at all">` +
    `noise: ${n} turn${n === 1 ? "" : "s"}, ${usageTokens(tok)} tokens, ${ucUsd(cost)} est. (${none} no reply, ${ack} ack)</span>` +
    (t.pending ? ` <span>· ${t.pending} delivered, turn not ended yet</span>` : "") + `</div>`;
  return html;
}
