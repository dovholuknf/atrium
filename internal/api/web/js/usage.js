// A card's token use on record, in its details and nowhere else.
//
// The room reads every Claude turn off the runner's own transcript and keeps a
// row per turn with what started it. This draws the totals, the context now,
// and the rows newest first. Nothing here is on the card face, the terminals
// list or a toast: it is history, asked for by opening it. See
// internal/daemon/usage.go.

const USAGE_CAUSES = {
  operator: "you",
  say: "a say",
  "restart-wake": "restart wake",
  keepalive: "keep-alive",
  resume: "resume",
  subagent: "subagent",
  backfill: "backfilled",
  unknown: "unknown",
};

// Tokens, short, with millions, since a card holds a few of them.
function usageTokens(n) {
  n = Number(n) || 0;
  if (n >= 1e6) return `${(n / 1e6).toFixed(n >= 1e7 ? 0 : 1)}M`;
  if (n >= 1000) return `${Math.round(n / 1000)}k`;
  return String(n);
}

// Rows that are not a turn of the card's own: a keep-alive refresh fork, and
// what the card's Claude subagents spent. Counted on their own cause lines,
// never as turns.
const USAGE_NOT_TURNS = new Set(["keepalive", "subagent"]);

// What a cause line counts: prompts and the API calls they took, refreshes, or
// a subagent's calls. A "prompt" is a row: what lies between two Stop hooks.
function usageCount(cause, t) {
  const n = (k, one, many) => `${k} ${k === 1 ? one : many}`;
  if (cause === "keepalive") return n(t.rows || 0, "refresh", "refreshes");
  if (cause === "subagent") return n(t.replies || 0, "call", "calls");
  return `${n(t.rows || 0, "prompt", "prompts")} · ${n(t.replies || 0, "call", "calls")}`;
}

// The card's own prompts and the API calls they took, over every cause but the
// keep-alive refreshes and the subagents. Shared with the hover details.
function usageOwn(by) {
  let prompts = 0, calls = 0;
  for (const [c, bt] of Object.entries(by || {})) {
    if (USAGE_NOT_TURNS.has(c)) continue;
    prompts += bt.rows || 0;
    calls += bt.replies || 0;
  }
  return { prompts, calls };
}

const USAGE_TIPS = {
  prompts: "the card's own prompts: each is one stretch of work between two Stop hooks. keep-alive refreshes and " +
    "subagents are on their own lines",
  calls: "API calls (replies) the card's own prompts took. one prompt is usually many calls, one per tool round",
  input: "input tokens NOT served from the cache, summed over every call. with prompt caching most input is a " +
    "cache read, so this is small. it is not the card's whole input",
  output: "tokens the model wrote",
  write5m: "input tokens written to the 5 minute prompt cache (1.25x the input price)",
  write1h: "input tokens written to the 1 hour prompt cache (2x the input price)",
  read: "input tokens read back from the prompt cache (0.1x the input price, less on some models)",
  cause_backfill: "turns recorded afterwards from the session's transcript, by `atrium usage backfill`",
  cause_keepalive: "refreshes that keep the card's cache warm while it is idle",
  peak: "the busiest bucket in the range, in tokens per minute, from the room's recorded turns",
  cardTotal: "this card's tokens in the range, from the room's recorded turns",
  causeCount: "prompts and API calls (or refreshes) recorded in the range for this cause",
  causeCounted: "counted tokens in the range for this cause: uncached in, out and cache writes. cache reads are the dim column",
  causeAll: "all five kinds of tokens in the range for this cause, cache reads included",
  cumTotal: "the running total over the range: counted tokens, or all five kinds with cache reads shown. it matches tokens by kind",
  cumProj: "the last hour's pace carried on to the coming midnight. it does not see the future, so a quiet or busy evening changes it",
  scope: "covers the whole card: every session it has run, including before a /clear, and keep-alive refreshes " +
    "and subagents",
};

// Offered on a card that has a Claude conversation behind it. Folded shut on
// every open, like the files.
function paintUsageField() {
  const field = document.getElementById("d-usage-field");
  if (!field) return;
  field.hidden = !(current && current.resume_id);
  document.getElementById("d-usage").hidden = true;
  document.getElementById("d-usage-toggle").classList.remove("open");
}

function toggleUsage() {
  const box = document.getElementById("d-usage");
  const open = box.hidden;
  box.hidden = !open;
  document.getElementById("d-usage-toggle").classList.toggle("open", open);
  if (open && current) loadUsage(current.id);
}

async function loadUsage(id) {
  const sum = document.getElementById("d-usage-sum");
  const list = document.getElementById("d-usage-list");
  const note = document.getElementById("d-usage-note");
  const causeBox = document.getElementById("d-usage-causes");
  sum.textContent = "reading…";
  list.innerHTML = "";
  causeBox.innerHTML = "";
  note.textContent = "";
  let v;
  try {
    v = await api(`/v1/tasks/${encodeURIComponent(id)}/usage`);
  } catch (e) {
    sum.textContent = "";
    note.textContent = "could not read it: " + e.message;
    return;
  }
  if (!current || current.id !== id) return;
  if (typeof paintCardUsageChart === "function") paintCardUsageChart(current);
  const t = v.totals || {};
  const by = v.by_cause || {};
  const own = usageOwn(by);
  const cell = (label, value, tip) =>
    `<span class="usagecell"${tip ? ` data-tip="${esc(tip)}"` : ""}><b>${esc(value)}</b> ${esc(label)}</span>`;
  sum.innerHTML = [
    cell("context now", usageTokens(v.context_now), v.model || ""),
    cell("prompts", String(own.prompts), USAGE_TIPS.prompts),
    cell("calls", String(own.calls), USAGE_TIPS.calls),
    cell("uncached in", usageTokens(t.input), USAGE_TIPS.input),
    cell("out", usageTokens(t.output), USAGE_TIPS.output),
    cell("cache write 5m", usageTokens(t.cache_write_5m), USAGE_TIPS.write5m),
    cell("cache write 1h", usageTokens(t.cache_write_1h), USAGE_TIPS.write1h),
    cell("cache read", usageTokens(t.cache_read), USAGE_TIPS.read),
  ].join("");
  const tokensOf = s => (s.input || 0) + (s.output || 0) + (s.cache_write_5m || 0) + (s.cache_write_1h || 0) +
    (s.cache_read || 0);
  const causes = Object.keys(by).sort((a, b) => tokensOf(by[b]) - tokensOf(by[a]));
  causeBox.innerHTML = causes.map(c => {
    const bt = by[c];
    return `<span>${esc(USAGE_CAUSES[c] || c)}</span><span>${esc(usageCount(c, bt))}</span>` +
      `<span>${usageTokens((bt.cache_write_5m || 0) + (bt.cache_write_1h || 0))} written</span>` +
      `<span>${usageTokens(bt.cache_read)} read</span>`;
  }).join("");
  const rows = v.rows || [];
  if (!rows.length) {
    note.textContent = "nothing recorded yet. a row is written when a turn ends.";
    return;
  }
  const tipped = (label, tip) => `<span data-tip="${esc(tip)}">${esc(label)}</span>`;
  const head = `<div class="urow uhead"><span>ended</span><span>cause</span>` +
    tipped("uncached in", USAGE_TIPS.input) + tipped("out", USAGE_TIPS.output) +
    tipped("cache write 5m", USAGE_TIPS.write5m) + tipped("cache write 1h", USAGE_TIPS.write1h) +
    tipped("cache read", USAGE_TIPS.read) + `<span>context</span></div>`;
  list.innerHTML = head + rows.map(r => {
    const cause = (USAGE_CAUSES[r.cause] || r.cause) + (r.after_resume && r.cause !== "resume" ? " · resumed" : "");
    const tip = `${r.replies} API call${r.replies === 1 ? "" : "s"}, ${r.model || "model unknown"}, from ${
      firstSeen(r.started_at)}`;
    return `<div class="urow${r.after_resume ? " resumed" : ""}" data-tip="${esc(tip)}">` +
      `<span>${esc(firstSeen(r.ended_at))}</span><span>${esc(cause)}</span>` +
      `<span>${usageTokens(r.input)}</span><span>${usageTokens(r.output)}</span>` +
      `<span>${usageTokens(r.cache_write_5m)}</span><span>${usageTokens(r.cache_write_1h)}</span>` +
      `<span>${usageTokens(r.cache_read)}</span><span>${usageTokens(r.context)}</span></div>`;
  }).join("");
  note.textContent = t.rows > rows.length ? `the newest ${rows.length} of ${t.rows} rows.` : "";
}
