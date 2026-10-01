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

// Seconds since the card last did anything: the list's `idle_seconds`, else worked out from `last_activity_at`.
function cardIdleSeconds(t) {
  if (t && typeof t.idle_seconds === "number") return t.idle_seconds;
  const at = Date.parse(t && t.last_activity_at);
  return isNaN(at) ? Infinity : Math.max(0, (Date.now() - at) / 1000);
}

// Most recently active first, the board's "last active" sort.
function cardActivityCmp(a, b) { return cardIdleSeconds(a) - cardIdleSeconds(b); }

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
