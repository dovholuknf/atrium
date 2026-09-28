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
  unknown: "unknown",
};

// Tokens, short, with millions, since a card holds a few of them.
function usageTokens(n) {
  n = Number(n) || 0;
  if (n >= 1e6) return `${(n / 1e6).toFixed(n >= 1e7 ? 0 : 1)}M`;
  if (n >= 1000) return `${Math.round(n / 1000)}k`;
  return String(n);
}

function usageMoney(n) {
  return "$" + (Number(n) || 0).toFixed(2);
}

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
  sum.textContent = "reading…";
  list.innerHTML = "";
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
  const t = v.totals || {};
  const cell = (label, value, tip) =>
    `<span class="usagecell"${tip ? ` data-tip="${esc(tip)}"` : ""}><b>${esc(value)}</b> ${esc(label)}</span>`;
  sum.innerHTML = [
    cell("context now", usageTokens(v.context_now), v.model || ""),
    cell("turns", String(t.rows || 0)),
    cell("in", usageTokens(t.input)),
    cell("out", usageTokens(t.output)),
    cell("write 5m", usageTokens(t.cache_write_5m)),
    cell("write 1h", usageTokens(t.cache_write_1h)),
    cell("read", usageTokens(t.cache_read)),
    cell("est.", usageMoney(t.cost), "priced on the models atrium has prices for; others count as $0"),
  ].join("");
  const by = v.by_cause || {};
  const causes = Object.keys(by).sort((a, b) => (by[b].cost || 0) - (by[a].cost || 0));
  const rows = v.rows || [];
  if (!rows.length) {
    note.textContent = "nothing recorded yet. a row is written when a turn ends.";
    return;
  }
  const head = `<div class="urow uhead"><span>ended</span><span>cause</span><span>in</span><span>out</span>` +
    `<span>write 5m</span><span>write 1h</span><span>read</span><span>context</span><span>est.</span></div>`;
  list.innerHTML = head + rows.map(r => {
    const cause = (USAGE_CAUSES[r.cause] || r.cause) + (r.after_resume && r.cause !== "resume" ? " · resumed" : "");
    const tip = `${r.replies} request${r.replies === 1 ? "" : "s"}, ${r.model || "model unknown"}, from ${
      firstSeen(r.started_at)}`;
    return `<div class="urow${r.after_resume ? " resumed" : ""}" data-tip="${esc(tip)}">` +
      `<span>${esc(firstSeen(r.ended_at))}</span><span>${esc(cause)}</span>` +
      `<span>${usageTokens(r.input)}</span><span>${usageTokens(r.output)}</span>` +
      `<span>${usageTokens(r.cache_write_5m)}</span><span>${usageTokens(r.cache_write_1h)}</span>` +
      `<span>${usageTokens(r.cache_read)}</span><span>${usageTokens(r.context)}</span>` +
      `<span>${usageMoney(r.cost)}</span></div>`;
  }).join("");
  note.textContent = (causes.length
    ? "by cause: " + causes.map(c => `${USAGE_CAUSES[c] || c} ${usageMoney(by[c].cost)}`).join(", ") + ". "
    : "") + (t.rows > rows.length ? `the newest ${rows.length} of ${t.rows} turns.` : "");
}
