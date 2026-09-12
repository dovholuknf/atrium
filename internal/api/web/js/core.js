// A phone, in pixels.
//
// THIS NUMBER IS DECLARED TWICE, HERE AND AS `@media (max-width: 480px)`, AND
// THE TWO HAVE TO AGREE. There is no way to have one: a media query cannot
// read a constant and `matchMedia` would put the literal back on this side
// anyway. What can be done is to notice when they drift, so
// `scripts/check-phone.js` reads both out of this file and compares them.
//
// Two of them apart is not a crash. It is a toast cap that applies at a width
// where the layout it was written for has not started yet, which looks like
// nothing at all until somebody counts toasts on a phone.
const PHONE = 480;

// The columns, in the order you should read them: what is blocking an agent
// right now, then what is waiting on you, then what is moving, then what is
// over, then what you put down.
//
// `statuses` is what a column holds. Only `finished` holds more than one, and
// it groups them: done and dead are both over, and separating them across the
// board put two columns of things needing nothing next to each other.
//
// `takes` is the status a card filed into the column by hand lands in, since a
// column holding two statuses has to pick one. `accepts: false` means the
// column is not offered as a destination at all.
//
// `backlog` is the inbox now. It used to be absent from this list, because
// nothing ever created a card in it and an empty column on every screen is a
// column you learn to skip past. Intake creates cards in it: work that is real
// and has no session yet.
//
// It stays hidden while it is empty, which is the same judgement as before
// rather than a reversal of it. Nobody who has not wired up a source should
// have a permanently empty column in front of them.
const COLUMNS = [
  {
    // Last in the DOM, first here, because reading order is what this array
    // decides and an inbox is where you start. See docs/intake-design.md.
    //
    // `accepts: false` for the usual reason plus one of its own: filing a
    // card back into the inbox would claim it had never been started, and the
    // session it already had would still be running.
    id: "backlog", label: "inbox", statuses: ["backlog"],
    accepts: false, hideWhenEmpty: true,
    why: "Work a source found that nobody has started.\n\n" +
      "There is no runner behind these, so nothing here is costing an agent " +
      "anything. Press start to give one a runner.\n\n" +
      "Hidden until you wire up a source."
  },
  // `accepts: false` keeps these two out of the move-to menu. They are
  // reports, not buckets: a card is in them because an agent said so. Filing
  // one into needs-permission would claim a request that does not exist, and
  // into needs-input would claim an agent is waiting when it is not.
  {
    id: "needs-permission", label: "needs permission", statuses: ["needs-permission"],
    accepts: false,
    why: "An agent is frozen waiting for your answer.\n\n" +
      "There is no timeout and nothing else will answer for it. First column " +
      "because it is the one costing time right now."
  },
  {
    // `ready` rather than `needs input`. The status stored underneath is
    // still `needs-input`, and this is only what it is called.
    //
    // The word comes from the gwt session ledger, which has been in daily use
    // far longer than this board and calls the same moment `done`: the turn
    // ended and it is your move. `done` was not available here, because a
    // board also has to say "this work is finished, stop showing it to me",
    // which is a claim only a human makes and which no ledger tracking
    // sessions has a word for. Two vocabularies for one event is worse than
    // either, so this takes the third word that means what both do.
    id: "needs-input", label: "ready", statuses: ["needs-input"],
    accepts: false,
    why: "The turn ended and it is your move.\n\n" +
      "Nothing here is frozen mid-tool, which is why it sits below " +
      "permissions."
  },
  {
    // The counterpart to `ready`, and the ledger's `thinking`. Kept as
    // `running` because a card here may be running a build or a test rather
    // than a model, and the live activity badge already says `thinking` when
    // that is what it is actually doing.
    id: "running", label: "running", statuses: ["running"],
    why: "Working, or believed to be.\n\n" +
      "With a known process id, atrium asks the operating system every twenty " +
      "seconds, so a card here is alive and its idle time only means it is " +
      "sitting at a prompt.\n\n" +
      "Without one there is nothing to ask, so silence is the only signal. " +
      "Those are marked NO CONTACT and eventually treated as gone, and come " +
      "back the moment the session says anything."
  },
  {
    id: "finished", label: "finished", statuses: ["done", "dead"], takes: "done",
    why: "Over, in two kinds.\n\n" +
      "done: the session ended and its directory is still there, so the " +
      "conversation can be picked back up.\n" +
      "dead: the process AND the directory are gone, so there is nothing to " +
      "come back to.\n\n" +
      "Clear deletes a group and its history."
  },
  {
    id: "shelved", label: "shelved", statuses: ["shelved"], takes: "shelved",
    why: "Put down on purpose, to come back to.\n\n" +
      "Requests from a shelved session are refused until you unshelve it, and " +
      "clear never takes one."
  }
];

// What a status is called, as opposed to what it is stored as.
//
// The stored names are the schema's and they are not changing: they are in
// CHECK constraints, in the rules, in the API and in every card's history. This
// is the one place that decides what a human reads, so the column heading, the
// stack chip, the terminal switcher and the card dialog cannot drift apart and
// call one state three things.
const STATUS_LABEL = {
  "needs-input": "ready",
  "needs-permission": "needs permission",
  // Stored as `backlog` since the first migration. Called `offered` because
  // that is what it means now: something found this and nobody has picked it
  // up. Backlog is a word for a list you are behind on, and an inbox nobody
  // has looked at is not the same feeling as one you are behind on.
  "backlog": "offered"
};
const statusLabel = (s) => STATUS_LABEL[s] || s;

// What every list of cards falls back to once its sort has run out of opinion.
//
// Every axis anything here sorts on is coarser than the list it sorts. A dozen
// cards share a runner, a whole project shares a worktree, and after a quiet
// night most of them read the same idle second. Cards that tie kept whatever
// order the last poll handed them, which is not an order at all: the daemon is
// free to answer two identical questions in two sequences, so a repaint that
// changed nothing still moved rows, and the one you were reaching for was
// somewhere else by the time you got there.
//
// Oldest first, then the id. The id says nothing to a reader, but it is on
// every card and never changes, so two cards raised in the same second still
// land in the same order on every repaint.
//
// Here rather than in the stack or the strip because both have the bug and it
// is the same bug, and a second copy of this would be the half that goes stale.
function cardTieBreak(a, b) {
  return (a.created_at || "").localeCompare(b.created_at || "") ||
    (a.id || "").localeCompare(b.id || "");
}

const KEY_EVENTS = ["created", "prompted", "perm-decided", "status-changed"];
const detail = document.getElementById("detail");
const reviewDlg = document.getElementById("review");
let current = null;

// Fits a box to its content: a textarea cannot say "as tall as what is in me",
// so the height is cleared, measured, then set. The CSS cap turns a very long
// command into a scroll rather than a card that runs off the page.
//
// Called after a value is set and on every keystroke, since editing adds and
// removes lines.
function autosize(el) {
  if (!el) return;
  el.style.height = "auto";
  el.style.height = (el.scrollHeight + 2) + "px";
}

// A session that ran out, on a page that never navigates.
//
// The published board asks who you are and that session expires. Every request
// this page makes is a fetch, so the redirect the guard sends a browser is
// never followed by anything: the board simply fills up with failures while
// the answer, a login, is one navigation away.
//
// Sent to renew rather than to sign in. A provider that still knows this
// browser answers without showing anybody anything, so an expiry in the middle
// of a working day costs a page load and no typing. `back` is the page as it
// stands, so a renewal puts somebody back where they were.
//
// ONCE. A board mid-poll has several requests in flight and they all come back
// unauthorized together, and each one would otherwise start its own navigation.
let signingInAgain = false;
function signInAgain() {
  if (signingInAgain) return;
  signingInAgain = true;
  location.assign("/auth/renew?back=" +
    encodeURIComponent(location.pathname + location.search));
}

const api = async (path, opts) => {
  const res = await fetch(path, opts);
  if (res.status === 401) signInAgain();
  if (!res.ok && res.status !== 204) {
    // THE BODY, WHATEVER SHAPE IT IS IN.
    //
    // Most of the daemon answers a failure as `{"error": "..."}`, and this read
    // that field and fell back to `res.statusText` when the parse failed. Every
    // refusal written with `http.Error` is a plain sentence, so it failed the
    // parse and arrived here as the word `Forbidden` with the sentence thrown
    // away. `guestHandler` is where that showed: a lent session refusing an
    // upload says what the link is and what it is for, and the board printed
    // "that did not go up: Forbidden".
    let msg = "";
    try { msg = await res.text(); } catch (e) {}
    try { msg = JSON.parse(msg).error || msg; } catch (e) {}
    throw new Error(String(msg).trim() || res.statusText);
  }
  return res.status === 204 ? null : res.json();
};

const esc = (s) => (s == null ? "" : String(s)).replace(/[&<>"]/g,
  c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));

// ── the atrium mark ─────────────────────────────────────────────────────────
//
// An A for atrium: two legs and a crossbar. It is drawn rather than fetched,
// so there is no image file anywhere and nothing to load before it appears.
//
// ONE DRAWING, TWO PLACES. The tab wears it and every desktop notification
// wears it. A second copy of these dozen lines is how the two end up nearly
// the same, which reads worse than either being wrong: a mark that shifts
// between the tab and the notification looks like a rendering fault.
//
// The coordinates are written for a 128 box and scaled, because a notification
// wants a large mark and a tab wants a small one. The line width scales with
// them, which is the reason to scale at all: a 12px stroke on a 16px icon is a
// smear.
function drawAtriumA(g, size) {
  const s = size / 128;
  const grad = g.createLinearGradient(24 * s, 96 * s, 104 * s, 32 * s);
  grad.addColorStop(0, "#00E3B0");
  grad.addColorStop(1, "#28C2FF");
  g.strokeStyle = grad;
  g.lineWidth = 12 * s;
  g.lineCap = "round";
  g.lineJoin = "round";
  g.beginPath();
  g.moveTo(30 * s, 100 * s);
  g.lineTo(64 * s, 28 * s);
  g.lineTo(98 * s, 100 * s);
  g.moveTo(44 * s, 72 * s);
  g.lineTo(84 * s, 72 * s);
  g.stroke();
}

// The mark on its own navy field, as a PNG data URL.
//
// The field is part of the mark. A tab strip is somebody else's background,
// and the gradient on whatever white or grey a browser is using that day is a
// shape with no weight to it.
function atriumMarkURL(size) {
  const c = document.createElement("canvas");
  c.width = c.height = size;
  const g = c.getContext("2d");
  g.fillStyle = "#0B1B2E";
  g.fillRect(0, 0, size, size);
  drawAtriumA(g, size);
  return c.toDataURL("image/png");
}

