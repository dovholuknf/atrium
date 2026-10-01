// The pure rules for ordering, grouping and filtering cards, loaded by the board and by the phone page so there is one copy.
// No DOM and no other script is needed, so the order this loads in only matters to what calls it.

// Defaults, shown in the settings dialog as the starting point and used when nothing has been written.
const DEFAULT_GROUP_BY = `// Return the group name for a card, or "" for no group.
// The whole task is available: worktree, runner, status, title, why, pid.
const path = (task.worktree || "").replace(/\\\\/g, "/");
if (!path) return "";

// .../<forge>/<org>/<repo>/... -> "org/repo"
const forge = path.match(/\\/(?:github|gitlab|bitbucket)[^/]*\\/([^/]+)\\/([^/]+)/i);
if (forge) return forge[1] + "/" + forge[2];

// Otherwise the last two segments, which is usually parent/leaf.
const parts = path.split("/").filter(Boolean);
if (parts.length >= 2) return parts.slice(-2).join("/");
return parts[0] || "";`;

const DEFAULT_GROUP_ORDER = `// Sort two group names. Return <0, 0, or >0.
// Alphabetical, with anything ungrouped last.
if (!a) return 1;
if (!b) return -1;
return a.localeCompare(b);`;

// A DOER IS AN AGENT-LAUNCHED SESSION, see the note in js/terminal-list.js. Matched exactly as the daemon's `hasOriginTag` does.
const DOER_TAG = "origin:agent";
function isDoer(t) {
  return !!(t && Array.isArray(t.tags) &&
    t.tags.some(x => String(x).trim().toLowerCase() === DOER_TAG));
}

// The project rule as a callable, compiled from the same source the settings dialog shows and cached.
let cardProjectRule;
function cardProjectOf(task) {
  if (cardProjectRule === undefined) {
    try { cardProjectRule = new Function("task", DEFAULT_GROUP_BY); } catch (e) { cardProjectRule = null; }
  }
  if (!cardProjectRule) return "";
  try { return String(cardProjectRule(task) ?? ""); } catch (e) { return ""; }
}

// Seconds since the card last did anything, as of `now`. `last_activity_at` is a fact about the card and is preferred.
// `idle_seconds` is a count taken when the row was read, so rows read at different times (a list a minute old beside a row an
// event just replaced) cannot be compared by it. It is the fallback for a card with no stamp, and a card with neither is
// the quietest there is.
function cardIdleSeconds(t, now) {
  const at = Date.parse(t && t.last_activity_at);
  // Not clamped: a stamp a little ahead of this clock is still ordered ahead of one a little behind it.
  if (!isNaN(at)) return ((now == null ? Date.now() : now) - at) / 1000;
  if (t && typeof t.idle_seconds === "number") return t.idle_seconds;
  return Infinity;
}

// Seconds the card has been waiting for the operator, as of `now`. The daemon makes `wait_seconds` the same way it makes
// `idle_seconds` (time since a stamp, at the moment the row is serialised), so it goes stale the same way. `waiting_since` is the
// stamp and is preferred. A card that is not waiting has neither and reads 0.
function cardWaitSeconds(t, now) {
  const at = Date.parse(t && t.waiting_since);
  if (!isNaN(at)) return ((now == null ? Date.now() : now) - at) / 1000;
  return t && typeof t.wait_seconds === "number" ? t.wait_seconds : 0;
}

// A count of seconds for display: never negative, and 0 where there is nothing to count.
function cardSecs(x) { return isFinite(x) ? Math.max(0, x) : 0; }

// The idle age to show a person or to decide on: the same number the sort placed the card by, as a plain count of seconds
// that is never negative or infinite. A card with no stamp at all reads 0.
function cardIdleAge(t, now) { return cardSecs(cardIdleSeconds(t, now)); }

// Most recently active first, the board's "last active" sort and the phone page's. One `now` for both cards of a comparison,
// so two cards with the same stamp tie exactly and the caller's tie break decides.
function cardActivityCmp(a, b) {
  const now = Date.now(), x = cardIdleSeconds(a, now), y = cardIdleSeconds(b, now);
  return x === y ? 0 : x - y;
}

// Two group names in the default order, alphabetical with anything ungrouped last.
let cardGroupRule;
function cardGroupCmp(a, b) {
  if (cardGroupRule === undefined) {
    try { cardGroupRule = new Function("a", "b", DEFAULT_GROUP_ORDER); } catch (e) { cardGroupRule = null; }
  }
  return cardGroupRule ? cardGroupRule(a, b) : String(a).localeCompare(String(b));
}

// Break sorting ties by creation time, then id, so rows stay in the same
// order even when polls return them in a different sequence. Both the stack
// and terminal strip use this.
function cardTieBreak(a, b) {
  return (a.created_at || "").localeCompare(b.created_at || "") ||
    (a.id || "").localeCompare(b.id || "");
}
