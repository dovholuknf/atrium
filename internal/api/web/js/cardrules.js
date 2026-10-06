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

// THE NAMED RULES, one per entry in the settings dialog's "group by" picker. Plain code, never a stored expression: the board
// stores only the NAME the operator picked. Each takes a task and returns the group name, "" for no group. `project` is the
// default rule above and `repo` is the repo alone, without the org.
const GROUP_RULES = {
  repo: t => {
    const parts = String(t.worktree || "").replace(/\\/g, "/").split("/").filter(Boolean);
    const at = parts.findIndex(x => /^(github|gitlab|bitbucket)/i.test(x));
    return at >= 0 && parts[at + 2] ? parts[at + 2] : (parts[parts.length - 1] || "");
  },
  room: t => String(t.room || ""),
  status: t => String(t.status || ""),
  runner: t => String(t.runner || ""),
  // The part of a tag before its first colon (`origin:agent` is `origin`). A card lands under each prefix it carries.
  prefix: t => [...new Set((t.tags || []).filter(x => String(x).includes(":")).map(x => String(x).split(":")[0]))]
};

// The labels the picker shows, in order. `code` is the operator's own expression.
const GROUP_CHOICES = [
  ["project", "project", "org/repo read from the worktree path"],
  ["repo", "repo", "the repo name alone, without the org"],
  ["room", "room", "the room the card runs on"],
  ["status", "status", "working, waiting, and so on"],
  ["runner", "runner", "which agent runs the card"],
  ["prefix", "tag prefix", "the part of a tag before its colon, so origin:agent files under origin"],
  ["window", "pile", "the pile the launcher put the card in, else its project"],
  ["tag", "tag", "every tag on the card, a card with several appears under each"],
  ["custom", "your groups", "the named buckets you made with the + button"],
  ["recency", "age", "today, yesterday, this week, this month, dormant"],
  ["code", "your own code", "a JavaScript rule you type"]
];

// The ways to order the groups, `code` being the comparator you type.
const GROUP_ORDERS = [
  ["name", "name", "alphabetical, anything ungrouped last"],
  ["recent", "most recent activity", "the group holding the card that did something most recently first"],
  ["count", "card count", "the biggest group first"],
  ["code", "your own code", "a JavaScript comparator you type"]
];

// A comparator over group names for a named order, given each group's cards. Ties and ungrouped fall back to the name.
function groupOrderCmp(order, cardsOf) {
  const byName = (a, b) => !a ? 1 : !b ? -1 : a.localeCompare(b);
  if (order === "count") return (a, b) => (cardsOf(b).length - cardsOf(a).length) || byName(a, b);
  if (order === "recent") {
    const now = Date.now();
    const age = n => Math.min(Infinity, ...cardsOf(n).map(t => cardIdleSeconds(t, now)));
    return (a, b) => {
      const x = age(a), y = age(b);
      return x === y ? byName(a, b) : x - y;
    };
  }
  return byName;
}

// A DOER IS AN AGENT-LAUNCHED SESSION, see the note in js/terminal-list.js. Matched exactly as the daemon's `hasOriginTag` does.
const DOER_TAG = "origin:agent";
function isDoer(t) {
  return !!(t && Array.isArray(t.tags) &&
    t.tags.some(x => String(x).trim().toLowerCase() === DOER_TAG));
}

// A SUBAGENT IS A WORKER, the card the launch cap counts: tagged `atrium:subagent` (SubagentTag). A resident an agent launched
// (a director, the orchestrator) has `origin:agent` and not this, so it is a doer and not a subagent. The quiet rule and the
// terminals "subagents" hide toggle both key on this one tag, so "subagent" means one thing on the board.
const SUBAGENT_TAG = "atrium:subagent";
function isSubagent(t) {
  return !!(t && Array.isArray(t.tags) &&
    t.tags.some(x => String(x).trim().toLowerCase() === SUBAGENT_TAG));
}

// AN AGENT-LAUNCHED CARD THAT ENDED A TURN WITH NO OPEN QUESTION IS IDLE, not waiting for the operator: its launcher is who
// it answers to. This is the one test every surface shares (board, /m, counts, bell). A question, a permission ask (its
// own status) or an operator-launched card (no origin:agent tag) is still waiting.
function cardHasOpenQuestion(t) {
  const s = t && t.seen;
  const open = !!s && s.answered === false && ((Array.isArray(s.open_questions) && s.open_questions.length > 0) || !!s.questions_unparsed);
  return open || ((t && t.asks_open) || 0) > 0;
}
function agentIdle(t) {
  return !!t && t.status === "needs-input" && isDoer(t) && !cardHasOpenQuestion(t);
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
