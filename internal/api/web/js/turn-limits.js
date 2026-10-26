// A turn cause over its daily limit, on the Terminals tab: one compact line per firing cause, dismissable for the day.
//
// GET /v1/usage/turn-limits (internal/api/turnlimits.go) decides which kinds of atrium text passed the daily limit
// (setting turn_cause_limits). This only draws what it answers, and every value in it came from the server, so it is
// written with textContent and never as markup. The usage tab's turns section (js/usage-turns.js) reports and decides
// nothing, and stays that way.

const TURN_LIMIT_POLL_MS = 60000;
const TURN_LIMIT_FAIL_MS = 600000;
const TURN_LIMIT_DISMISS_KEY = "atrium.turnlimits.dismissed";
let turnLimits = { at: 0, day: "", findings: [] };

// Which kinds were dismissed, for which UTC day. Yesterday's dismissals are no longer anything.
function turnLimitDismissed(day) {
  try {
    const d = JSON.parse(localStorage.getItem(TURN_LIMIT_DISMISS_KEY) || "{}");
    return d.day === day && Array.isArray(d.kinds) ? d.kinds : [];
  } catch (e) { return []; }
}

function dismissTurnLimit(kind) {
  const kinds = turnLimitDismissed(turnLimits.day).concat(kind);
  try { localStorage.setItem(TURN_LIMIT_DISMISS_KEY, JSON.stringify({ day: turnLimits.day, kinds })); } catch (e) {}
  paintTurnLimits();
}

// The words of one finding: which cause, how much today, the limit, and what would remove it.
function turnLimitText(f) {
  const limit = [];
  if (f.limit_turns > 0 && (f.over === "turns" || f.over === "both")) limit.push(f.limit_turns + " turns");
  if (f.limit_tokens > 0 && (f.over === "tokens" || f.over === "both")) limit.push(usageTokens(f.limit_tokens) + " tokens");
  const way = f.item
    ? "to remove it: " + f.item
    : "no item removes it. to raise the limit set turn_cause_limits in settings";
  return f.kind + ": " + f.turns + " turn" + (f.turns === 1 ? "" : "s") + ", " + usageTokens(f.tokens) +
    " tokens today, limit " + limit.join(" or ") + ". " + way;
}

function paintTurnLimits() {
  const host = document.getElementById("term-limits");
  if (!host) return;
  const hide = turnLimitDismissed(turnLimits.day);
  const shown = (turnLimits.findings || []).filter(f => !hide.includes(f.kind));
  host.replaceChildren();
  host.hidden = shown.length === 0;
  for (const f of shown) {
    const row = document.createElement("div");
    row.className = "tl-limit";
    const text = document.createElement("span");
    text.className = "tl-limit-text";
    text.textContent = turnLimitText(f);
    const btn = document.createElement("button");
    btn.className = "tl-limit-dismiss";
    btn.textContent = "dismiss";
    btn.setAttribute("data-tip", "hide this until tomorrow (UTC)");
    btn.addEventListener("click", () => dismissTurnLimit(f.kind));
    row.append(text, btn);
    host.append(row);
  }
}

async function renderTurnLimits() {
  if (Date.now() - turnLimits.at < TURN_LIMIT_POLL_MS) return;
  if (typeof roomUnnamed === "function" && roomUnnamed()) return;
  turnLimits.at = Date.now();
  try {
    const r = await api("/v1/usage/turn-limits");
    turnLimits = { at: turnLimits.at, day: r.day || "", findings: r.findings || [] };
  } catch (e) {
    // An old room has no such route, and a room that does not answer is not a finding.
    // Asked again in ten minutes rather than one: a 409 (the room is ambiguous or not answering) does not clear in a
    // minute, and each retry is a failed request in the console.
    turnLimits = { at: Date.now() + TURN_LIMIT_FAIL_MS - TURN_LIMIT_POLL_MS, day: "", findings: [] };
  }
  paintTurnLimits();
}