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

// A FORGE'S SHORT LABEL AND ITS HOST NAME ARE ONE HOST, and the short label is the canonical form (the hub's git paths and
// hubrepos.js already say `github`). A checkout kept under `.../github.com/org/repo` and one under `.../github/org/repo` are the
// same forge, so they group, filter and colour as one. Twin of store.ForgeLabel in Go: the two lists have to agree.
// Only the heading changes. A URL to the forge still says github.com.
const FORGE_HOST_LABELS = { "github.com": "github", "bitbucket.org": "bitbucket", "gitlab.com": "gitlab" };
function forgeHostLabel(h) {
  const s = String(h == null ? "" : h);
  return FORGE_HOST_LABELS[s.toLowerCase()] || s;
}

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

// THE GROUP SUBMENUS ("add to group", "remove from group") list names A to Z, whatever order the view keeps them in,
// and a group made in the last ten minutes wears a "new" tag. A group is only a name in the browser's `groups` list, so
// when it was made is kept beside it in `made`, a name to epoch milliseconds. A group with no entry there (made before
// this was recorded, or typed into the settings by hand) is not new.
const GROUP_NEW_MS = 10 * 60 * 1000;
function groupMenuNames(names) {
  return (names || []).slice().sort((a, b) => a.localeCompare(b));
}
function groupIsNew(name, made, now) {
  const at = made && made[name];
  return typeof at === "number" && (now === undefined ? Date.now() : now) - at < GROUP_NEW_MS;
}
// The group name a person typed, as the tag it becomes: trimmed and lower case.
function groupNameOf(raw) { return String(raw || "").trim().toLowerCase(); }

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

// AN INTERVIEW IS NEVER NESTED. A card tagged `atrium:interview` shows in its own tag group, a top row, whoever launched it:
// the human has to find it, and a fold under its launcher hid it. THE ONE RULE every list that nests a card under the card
// that launched it asks (terminals `termNest`, stack and board `subagentRollup`).
const INTERVIEW_TAG = "atrium:interview";
function isInterview(t) {
  return !!(t && Array.isArray(t.tags) &&
    t.tags.some(x => String(x).trim().toLowerCase() === INTERVIEW_TAG));
}
// A MINION IS THE THROWAWAY SESSION AN ITEM GETS, tagged `atrium:minion` by the orchestrator's launch (no Go stamps it). It wins
// over `atrium:director`, which its launch also carries, and an item worker without this tag is not one. Matched exactly.
const MINION_TAG = "atrium:minion";
function isMinion(t) {
  return !!(t && Array.isArray(t.tags) &&
    t.tags.some(x => String(x).trim().toLowerCase() === MINION_TAG));
}
// The debug toggle that shows hidden minions. Nothing registers one yet, so they are hidden.
let minionShownFn = null;
function setMinionShown(fn) { minionShownFn = typeof fn === "function" ? fn : null; }
function minionShown() { return !!(minionShownFn && minionShownFn()); }
// A card HAS SOMETHING FOR THE HUMAN: waiting on them, an open question, a permission, or stuck. Not one whose room is silent.
function cardNeedsYou(t) {
  if (!t || t.offline) return false;
  return cardWaiting(t) || cardHasOpenQuestion(t) || t.status === "needs-permission" ||
    (typeof isStuck === "function" && isStuck(t));
}
// ONE ANSWER TO "IS THIS CARD LEFT OUT OF EVERY LIST RIGHT NOW". A view filter only: the card list still holds it, so counts
// and alerts see it. A minion that needs you, or is the attached one, is not hidden.
function cardHidden(t) {
  if (!isMinion(t) || minionShown() || cardNeedsYou(t)) return false;
  return !(typeof termTask !== "undefined" && termTask && termTask.id === t.id);
}
function nestsUnderLauncher(t) { return isSubagent(t) && !isInterview(t); }

// THE CARD THAT LAUNCHED `t`, from `cards`, or null. Every list that nests a child under its launcher asks this one
// function (terminals `termNest`, stack and board `subagentRollup`, reveal, the usage groups), so a child on another room
// nests exactly like a same-room one. A launcher on the same room is named by `spawned_by_id`, matched on the bare id.
// One on ANOTHER room is only known by its handle, `spawned_by` = "name@room" (the hub qualifies it), so that is read as
// the launcher's wire name (or alias) on the card whose id is tagged `room~`. A bare `spawned_by` is a launcher on the child's
// own room, which the room did not resolve to an id. `byBare` is an optional prebuilt
// bare-id index, so a list does not rescan itself for every child.
function launcherOf(t, cards, byBare) {
  if (!t) return null;
  let p = null;
  if (t.spawned_by_id) {
    const want = bareId(t.spawned_by_id);
    p = byBare ? byBare.get(want) : (cards || []).find(x => bareId(x.id) === want);
  }
  if (!p && t.spawned_by) {
    const at = String(t.spawned_by).lastIndexOf("@");
    const name = at > 0 ? t.spawned_by.slice(0, at) : String(t.spawned_by);
    const room = at > 0 ? t.spawned_by.slice(at + 1) : roomOf(t.id);
    if (name && (room || at <= 0)) {
      const local = s => { const i = String(s).lastIndexOf("/"); return i >= 0 ? String(s).slice(i + 1) : String(s); };
      p = (cards || []).find(x => x !== t && roomOf(x.id) === room &&
        (x.wire_name === name || (x.wire_name && local(x.wire_name) === local(name)) || (x.alias && x.alias === name)));
    }
  }
  return p && p !== t ? p : null;
}

// A CARD'S OPEN QUESTION, from the parsed list, an unparsed one, or a question put to the operator (asks_open).
function cardHasOpenQuestion(t) {
  const s = t && t.seen;
  const open = !!s && s.answered === false && ((Array.isArray(s.open_questions) && s.open_questions.length > 0) || !!s.questions_unparsed);
  return open || ((t && t.asks_open) || 0) > 0;
}

// GREY MEANS NOT RUNNING, AND NOTHING ELSE. A card with a live runner that is waiting for the operator is orange, whatever its
// origin tag says: an agent-launched interview is waiting on a person like any other. The one rule every surface shares
// (stack, terminals, phone, counts, bell) is `runnerGone`, and a card is grey exactly when it is over or its runner is gone.
//
// A runner is gone when the status still says it is alive (running, needs-input, needs-permission) and nothing vouches for it:
// atrium does not hold its terminal (`supervised`, api.go Supervised), it is not being launched (`starting`), and no process id
// was reported for the reaper to ask the OS about (`pid`, daemon/reaper.go only files a card dead whose pid is gone, and
// exempts a waiting card with no pid, because it is quiet and not necessarily gone). A joined terminal has a pid, so it is live.
// A running card with no pid is given the no-contact window before it counts as gone, since hooks may still be reaching it.
const NO_CONTACT_AFTER_SECONDS = 20 * 60;
const LIVE_STATUSES = ["running", "needs-input", "needs-permission"];
function runnerGone(t) {
  if (!t || !LIVE_STATUSES.includes(t.status)) return false;
  // `supervised` absent is unknown, not gone: the daemon always sends it, so only an explicit false counts.
  if (t.supervised !== false || t.starting || t.offline || t.pid > 0) return false;
  if (t.parked_at) return true;
  return t.status !== "running" || cardIdleAge(t) > NO_CONTACT_AFTER_SECONDS;
}
// PAUSED IS NOT DEAD. A DEAD card has no agent and cannot be revived. A PAUSED card has no agent running either, but it
// CAN be resumed: the operator paused it (status `paused`) or it sat idle and was parked (`parked_at`, no process). A
// paused card is never "ready": ready means a live runner is waiting on you.
function cardPaused(t) {
  if (!t || t.offline) return false;
  if (t.status === "paused") return true;
  return !!t.parked_at && !t.supervised;
}
// What a card's state is called, where the stored status would mislead: a parked card keeps the status it had, which
// reads "ready", and nothing is running to be ready.
function cardStatusLabel(t) {
  return cardPaused(t) ? "paused" : statusLabel(t && t.status);
}

// Waiting on the operator: a waiting status and a runner there to answer.
function cardWaiting(t) {
  return !!t && (t.status === "needs-input" || t.status === "needs-permission") && !runnerGone(t);
}

// A runner is up: being launched, or alive by its status with nothing saying it went.
function cardLive(t) {
  return !!t && (!!t.starting || (LIVE_STATUSES.includes(t.status) && !runnerGone(t)));
}
// IS THIS SESSION WORKING RIGHT NOW: a live `activity.what` that is not idle, on a card that is not waiting on the operator,
// not paused, and whose activity is not stale. The board, the stack and the terminals all ask this one function.
function workingNow(t) {
  const a = t && t.activity;
  return !!(a && a.what && a.what !== "idle" && !a.looks_idle &&
    !cardWaiting(t) && t.status !== "paused" && !staleActivity(t));
}
// ONE WORD FOR HOW A CARD IS DRAWN, EVERYWHERE (docs/rnd/unified-card.md). Grey is "off" and nothing else.
//   unreachable  its room is not answering, so nothing says it stopped
//   waiting      a live runner is waiting on the operator, whoever launched it
//   working      a live runner is mid-turn
//   live         a live runner that is neither
//   off          no runner: gone, parked, paused, done, dead, an inbox card
function cardState(t) {
  if (!t) return "off";
  if (t.offline) return "unreachable";
  if (cardWaiting(t)) return "waiting";
  if (cardLive(t)) return workingNow(t) ? "working" : "live";
  return "off";
}

// WHICH BOARD COLUMN A CARD IS IN (docs/rnd/unified-card-decisions.md). The board's columns follow the state, not the stored
// status: `needs you` holds a live runner waiting on the operator, `working` a live runner doing or ready to do something,
// and `done` every card with no agent running (finished, dead, runner gone, parked, paused). A ready card whose runner is
// gone is done, never needs you. The inbox is the stored status `backlog`. A card on a room that is not answering is
// `unreachable`, which says nothing about its runner, so it is filed by the status it last had.
function boardColumnOf(t) {
  if (!t || t.status === "backlog") return "backlog";
  const s = cardState(t);
  if (s === "waiting") return "needs-you";
  if (s === "working" || s === "live") return "working";
  if (s === "unreachable") {
    return t.status === "needs-input" || t.status === "needs-permission" ? "needs-you" : t.status === "running" ? "working" : "done";
  }
  return "done";
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
