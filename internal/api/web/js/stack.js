// ── the stack ───────────────────────────────────────────
// One list of every card, ordered. The board says what is where, which is
// worth having, but a column of thirty cards is not a reading order and a
// waiting agent in one lane says nothing about a waiting agent in another.
//
// Waiting first by default, then by how long since anything happened, which is
// the order you would work through them in.
// Each entry is one axis, not one direction. Pressing the active pill reverses
// it, so "newest activity" and "quietest" were the same pill twice, and
// "needs me" was the `wants me` filter with extra steps.
// The two clocks are named for what each measures rather than for the order it
// puts them in. They are close enough to be confused: on a card that is ready,
// the turn ended and the activity stopped at the same moment, so both read the
// same number. They come apart on everything else. A card that is running has
// been active recently and is waiting for nobody, and one frozen on a
// permission for an hour was last active an hour ago and has wanted you for
// that whole hour.
//
// `first` and `last` say which end of the axis is at the top, in words. The
// label alone cannot: "last active" reads as most recent first, and reversed
// it is the exact opposite under an unchanged label.
// Every axis below is coarse enough that cards tie on it constantly, and what
// they fall back to is `cardTieBreak` in core.js, applied where the sort is
// run rather than written into each axis.
const STACK_SORTS = {
  activity: {
    label: "last active",
    first: "most recently active first", last: "quietest first",
    cmp: (a, b) => (a.idle_seconds || 0) - (b.idle_seconds || 0)
  },
  waited: {
    label: "waiting on you",
    first: "questions first, then waiting longest", last: "newest first",
    // A question outranks any amount of waiting.
    //
    // Sorting by duration alone put a card that asked you something two
    // minutes ago below twenty that had merely stopped overnight, which is the
    // wrong way round for a list called "waiting on you": those twenty are not
    // waiting on anything, they ran out of things to do. The clock only
    // decides between cards of the same kind.
    // A question you owe an answer to outranks one another session owes,
    // which outranks any amount of waiting. The middle rank is a card stopped
    // on a peer: it still belongs above twenty that merely finished, because
    // a peer that never answers looks exactly like a session nobody noticed.
    cmp: (a, b) => askRank(a) - askRank(b) ||
      (b.wait_seconds || 0) - (a.wait_seconds || 0)
  },
  status: {
    label: "status",
    first: "blocked, then ready, then working", last: "over and put down first",
    cmp: (a, b) => {
      const rank = t => t.status === "needs-permission" ? 0
        : t.status === "needs-input" ? 1
        : t.status === "running" ? 2
        : t.status === "shelved" ? 3 : 4;
      return rank(a) - rank(b) || (a.idle_seconds || 0) - (b.idle_seconds || 0);
    }
  },
  name: {
    label: "name", first: "a to z", last: "z to a",
    cmp: (a, b) => (a.display_title || "").localeCompare(b.display_title || "")
  },
  project: {
    label: "project", first: "a to z", last: "z to a",
    cmp: (a, b) => (a.worktree || "").localeCompare(b.worktree || "")
  },
  runner: {
    label: "runner", first: "a to z", last: "z to a",
    cmp: (a, b) => (a.runner || "").localeCompare(b.runner || "")
  },
  // Untagged last, so sorting by tag surfaces what you have filed rather than
  // what you have not.
  tag: {
    label: "tag", first: "a to z, untagged last", last: "untagged first, then z to a",
    cmp: (a, b) => {
      const at = (a.tags || []).join(" "), bt = (b.tags || []).join(" ");
      if (!at && !bt) return 0;
      if (!at) return 1;
      if (!bt) return -1;
      return at.localeCompare(bt);
    }
  }
};
// Which columns the stack is showing, as column ids from COLUMNS.
//
// Ready alone by default. The stack exists to answer "what has stopped and is
// waiting on me", and showing everything by default buried that in a list of
// things that need nothing. Everything else is one click away.
//
// Additive rather than exclusive: the columns are already the set of answers
// to "what kind of attention does this want", so wanting two of them at once
// is ordinary and used to need a separate word invented for each combination.
const STACK_SHOW_KEY = "atrium.stack.show";

function stackShow() {
  try {
    const v = JSON.parse(localStorage.getItem(STACK_SHOW_KEY) || "null");
    if (Array.isArray(v)) return new Set(v);
  } catch (e) {}
  return new Set(["needs-input"]);
}

function setStackShow(next) {
  localStorage.setItem(STACK_SHOW_KEY, JSON.stringify([...next]));
  paintStackShow();
  paintStack();
}

// Clicking a pill adds or removes that column. Turning the last one off shows
// everything rather than nothing: an empty list with no explanation reads as
// broken, and "none of them" is not a thing anybody wants to look at.
function toggleStackShow(id) {
  const on = stackShow();
  if (on.has(id)) on.delete(id); else on.add(id);
  setStackShow(on);
}

// WHICH COLUMNS GET A PILL HERE, which is not all of them.
//
// `inbox` and `needs permission` are board words. On the board they are
// columns you can see, so their names are explained by where they sit and by
// the help bubble above them. Here they are two pills in a row of pills, with
// nothing around them to say what either means, and `inbox` in particular
// means nothing at all unless you already know a source filled it.
//
// Both are still reachable: `all` shows every card whatever state it is in,
// which is what somebody looking for one of these is doing anyway.
const STACK_PILL_SKIP = ["backlog", "needs-permission"];

function paintStackShow() {
  const on = stackShow();
  const all = on.size === 0;
  // `all` FIRST. It is the answer most of the time and the way back from any
  // filter, and it was last in a row long enough that finding it meant reading
  // to the end.
  document.getElementById("stack-seg").innerHTML =
    `<button class="${all ? "on" : ""}" data-v=""
       onclick="setStackShow(new Set())"
       title="every card, whatever state it is in"
       >all <i class="pillcount"></i></button>` +
    COLUMNS.filter(c => !STACK_PILL_SKIP.includes(c.id)).map(c =>
      `<button class="${on.has(c.id) ? "on" : ""}" data-v="${esc(c.id)}"
        onclick="toggleStackShow('${esc(c.id)}')"
        title="${esc(c.label)}: click to add or remove"
        >${esc(c.label)} <i class="pillcount"></i></button>`).join("");
}

// Activity by default. This is the "what has been going on" view, and the
// board is where you go to see what is where.
let stackSort = "activity";
let stackDesc = false;
let allStack = [];

function setStackSort(key) {
  if (stackSort === key) stackDesc = !stackDesc;
  else { stackSort = key; stackDesc = false; }
  paintStackSort();
  paintStack();
}

// The arrows were the wrong way round: reversing put the largest value first
// and drew an up arrow. That is the reading everybody has, so it made a
// correctly sorted list look broken, which is worse than no arrow at all.
//
// Each sort also says which end it is showing, because the quantity alone does
// not. "last active" reads as most recent first, and reversed it is the exact
// opposite while the label is unchanged.
function paintStackSort() {
  document.getElementById("stack-sort").innerHTML =
    Object.entries(STACK_SORTS).map(([k, s]) => {
      const on = k === stackSort;
      // The cmp puts the first-listed end first, so reversing shows the other.
      const end = on ? (stackDesc ? s.last : s.first) : "";
      return `<button class="${on ? "on" : ""}" data-sort="${k}"
        onclick="setStackSort('${k}')"
        title="${on ? end + ". click again to reverse" : "sort by " + s.label}"
        >${s.label}${on ? (stackDesc ? " &#8595;" : " &#8593;") : ""}
         <i class="pillcount"></i></button>`;
    }).join("");
}

// Writes a count into the pills of one segment, keyed by the pill's value.
//
// Set on the element rather than rebuilt into the markup, so the pill the
// pointer is on does not get replaced under it every poll.
function pillCounts(segID, counts) {
  const seg = document.getElementById(segID);
  if (!seg) return;
  seg.querySelectorAll("button").forEach(b => {
    const slot = b.querySelector(".pillcount");
    if (!slot) return;
    const key = b.dataset.v !== undefined ? b.dataset.v : b.dataset.sort;
    const n = counts[key];
    slot.textContent = n === undefined ? "" : String(n);
    slot.classList.toggle("zero", n === 0);
  });
}

async function renderStack() {
  try { allStack = (await api("/v1/tasks")).tasks || []; } catch (e) { return; }
  lastTasks = allStack;
  paintStack();
}

function paintStack() {
  const q = (document.getElementById("stack-q").value || "").toLowerCase();
  const show = stackShow();
  // Which statuses the chosen columns hold, resolved from COLUMNS so the two
  // views cannot disagree about what a column contains.
  const wanted = new Set();
  COLUMNS.forEach(c => { if (show.has(c.id)) c.statuses.forEach(s => wanted.add(s)); });

  // `#tag` means that tag exactly, so filtering to `#prs` does not also drag
  // in a card whose path happens to contain the letters. Anything else is a
  // plain substring across everything a card carries, tags included.
  const matchesText = t => {
    if (!q) return true;
    if (q.startsWith("#")) {
      const want = q.slice(1).trim();
      return want ? (t.tags || []).includes(want) : true;
    }
    return (t.display_title + " " + (t.worktree || "") + " " + (t.runner || "") + " " +
      (t.why || "") + " " + (t.ask || "") + " " +
      (t.tags || []).join(" ")).toLowerCase().includes(q);
  };

  const list = allStack.filter(t => {
    // No column chosen means every card, which is what the `everything` pill
    // sets. See toggleStackShow: an empty list with no explanation reads as
    // broken.
    if (wanted.size && !wanted.has(t.status)) return false;
    return matchesText(t);
  });

  // What each pill would ADD, counted against the same search text. A count
  // that described the current list would be the same number on every pill.
  const searched = allStack.filter(matchesText);
  const counts = { "": searched.length };
  COLUMNS.forEach(c => {
    counts[c.id] = searched.filter(t => c.statuses.includes(t.status)).length;
  });
  pillCounts("stack-seg", counts);
  // Only on the sort that has an empty state worth knowing about. A count on
  // name or runner would be the same total written four more times.
  pillCounts("stack-sort", { waited: searched.filter(isWaiting).length });

  document.getElementById("stack-match").textContent =
    list.length === allStack.length ? "" : `${list.length} of ${allStack.length}`;

  const s = STACK_SORTS[stackSort] || STACK_SORTS.activity;
  // The tiebreak is applied here rather than written into each cmp, so every
  // axis gets it and a new one cannot be added without it.
  list.sort((a, b) => s.cmp(a, b) || cardTieBreak(a, b));
  if (stackDesc) list.reverse();
  // Pinned to the top, after the reverse so it stays there in either
  // direction. Sorted is what the pills asked for; pinned is what you asked
  // for once and meant permanently.
  list.sort((a, b) => (b.pinned ? 1 : 0) - (a.pinned ? 1 : 0));

  if (!list.length) {
    setHTML(document.getElementById("stack-list"),
      `<div class="panel"><div class="empty">${allStack.length
        ? "nothing matches that filter" : "no agents yet"}</div></div>`);
    return;
  }

  // The same grouping the board uses, from the same setting, so turning it on
  // in one place turns it on everywhere. The chosen sort still orders the rows
  // inside each group.
  const g = grouper();
  setHTML(document.getElementById("stack-list"), g
    ? stackGroupsHTML(list, g)
    : `<div class="panel">` + stackRows(list) + `</div>`);

  if (groupingFault) {
    toast("grouping code", groupingFault);
    groupingFault = "";
  }
}

// The stack split into project groups, each its own panel so the color reads
// down the edge the way it does on the board.
function stackGroupsHTML(list, g) {
  const byName = new Map();
  for (const t of list) {
    for (const name of g.of(t)) {
      if (!byName.has(name)) byName.set(name, []);
      byName.get(name).push(t);
    }
  }
  if (byName.size < 2 && byName.has("")) {
    return `<div class="panel">` + stackRows(list) + `</div>`;
  }

  return [...byName.keys()].sort(g.cmp).map(name => {
    const mine = byName.get(name);
    if (!name) {
      return `<div class="panel">` + stackRows(mine) + `</div>`;
    }
    const key = "stack:" + name;
    const shut = foldedColumns().includes("proj:" + key) ? "" : " open";
    return `<details class="stackgroup"${shut} style="--ghue:${groupHue(name)}"
      data-morph-key="${esc(key)}">
      <summary onclick="rememberProject(event, '${esc(key).replace(/'/g, "&#39;")}')"
        oncontextmenu="groupMenu(event, '${esc(name).replace(/'/g, "&#39;")}')">
        <span class="gname" title="${esc(name)} &mdash; right click to recolor">${esc(name)}</span>
        <span class="gn">${mine.length}</span>
      </summary>
      <div class="panel">${stackRows(mine)}</div>
    </details>`;
  }).join("");
}

// The big number follows the sort.
//
// It used to show how long a card had been waiting when it was waiting, and
// how long it had been quiet otherwise. Two quantities in one column, which
// meant a list sorted correctly by one of them could still read as out of
// order, because half the rows were showing the other. On a ready card the two
// track each other closely enough to hide it most of the time, which is worse
// than obvious: the list looks wrong occasionally and for no visible reason.
//
// Sorting by a quantity now shows that quantity, so the column is always
// monotonic under the active sort. Sorting by something that is not a duration
// falls back to the old rule, since there is no sorted quantity to agree with.
function bigNumber(t) {
  if (stackSort === "activity") return t.idle_seconds;
  if (stackSort === "waited") return t.wait_seconds;
  return isWaiting(t) ? t.wait_seconds : t.idle_seconds;
}

function bigNumberMeans(t) {
  if (stackSort === "activity") return "how long since it last did anything";
  if (stackSort === "waited") {
    return isWaiting(t)
      ? "how long it has been waiting for you"
      : "not waiting for anything";
  }
  return isWaiting(t)
    ? "how long it has been waiting for you"
    : "how long since it last did anything";
}

// The MOMENT the big number is counting from.
//
// The date column used to be `created_at`, when atrium first saw the card, and
// it was the only column on the row measuring something the list was not
// ordered by. Sorted by last active, it read `today, today, yesterday, today`,
// which is correct about two different facts and looks like a broken sort.
//
// So it follows the same rule `bigNumber` does, and the two always agree: one
// says how long ago, the other says when, and the order is by that same
// moment. Nothing on the row is measuring something else.
function bigNumberAt(t) {
  if (stackSort === "activity") return t.last_activity_at;
  if (stackSort === "waited") return t.waiting_since || t.last_activity_at;
  return isWaiting(t) ? (t.waiting_since || t.last_activity_at) : t.last_activity_at;
}

function bigNumberAtMeans(t) {
  if (stackSort === "waited" && !isWaiting(t)) return "not waiting for anything";
  return "when: " + bigNumberMeans(t).replace(/^how long (since|it has been) /, "");
}

// One card as a row. The same facts the board shows, in an order rather than a
// column, with the age reading as what it is for the status it is in.
//
// The state and its duration are one chip, matching the board. Two chips said
// it twice and sometimes disagreed with themselves: a ready card read
// "idle 30m" beside "waiting 31m", two clocks that start together, and a dead
// one read "dead" beside "dead 1h".
// The rows, with a line under the pinned ones.
//
// PINNING OUTRANKS THE SORT and nothing on screen said so. That is a real
// answer to a real question, and it is drawn as one list ordered by one rule,
// so the reading is that the sort is broken: three cards at 16m, 2m and 19s
// sit above one at 4h17m, and the only thing saying why is a star twelve
// pixels wide at the far left.
//
// It is two lists, so BOTH SIDES ARE NAMED. Only a divider was, which left the
// block above it as the unlabelled half and made the label read as a caption
// for the whole thing.
//
// The sort's name came off it. It said "the rest, most recently active", and
// the sort control at the top of the screen already says which sort is on:
// repeating it here made the line look like it was ABOUT the sort, which is the
// opposite of what it is for. The line is about pinning outranking the sort.
//
// Only when there is something on both sides: a heading over an empty set, or
// over everything, names nothing.
function stackRows(list) {
  const pinned = list.filter(t => t.pinned).length;
  if (!pinned || pinned === list.length) return list.map(stackRow).join("");
  return `<div class="pinbreak"><span>pinned</span></div>` + list.map((t, i) =>
    (i === pinned ? `<div class="pinbreak"><span>the rest</span></div>` : "")
    + stackRow(t)).join("");
}

function stackRow(t) {
  const w = isWaiting(t);
  const dark = isOutOfContact(t);
  return `<div class="row stackrow ${w ? "attn" : ""}${t.auto_approve ? " autoon" : ""}${
      t.pinned ? " pinned" : ""}"
    data-id="${t.id}"
    onclick="cardMenu(event, '${t.id}')"
    oncontextmenu="cardMenu(event, '${t.id}')">
    ${pinStar(t)}
    ${runnerMark(t.runner)}
    <div class="wait ${w ? "" : "quiet"}" title="${esc(bigNumberMeans(t))}"
      >${ago(bigNumber(t))}</div>
    <div class="who">
      <b>${w ? '<span class="pulse"></span>' : ""}${esc(t.display_title)}</b>
      <span>${esc(t.worktree || "no directory")}</span>
      ${askLine(t)}
      ${t.why ? `<span class="why">${esc(t.why)}</span>` : ""}
    </div>
    <div class="chips">
      ${tagChips(t)}
      ${modelChip(t)}
      ${noteChip(t)}
      ${originChip(t)}
      ${recapChip(t)}
      ${activityChip(t)}
      ${contextChip(t)}
      ${dark ? `<span class="chip nocontact">no contact</span>` : ""}
      ${t.auto_approve ? `<span class="chip auto">auto</span>` : ""}
      ${stateChip(t, w)}
      ${t.status === "backlog" ? `<span class="chip attach"
        title="start this, with what the source already knew filled in"
        onclick="event.stopPropagation();startOffered('${t.id}')">start</span>` : ""}
      ${t.supervised ? `<span class="chip attach"
        onclick="event.stopPropagation();attachTask('${t.id}')">attach</span>
      <span class="chip attach icon"
        title="open this terminal in its own window"
        onclick="event.stopPropagation();popOutTask('${t.id}')">${popIcon()}</span>` : ""}
    </div>
    <i class="since" title="${esc(bigNumberAtMeans(t))}"
      >${esc(firstSeen(bigNumberAt(t)))}</i>
  </div>`;
}

// The state, and its duration only when that is not already the big number on
// the left.
//
// Under the default sort those are the same measurement: `bigNumber` returns
// `wait_seconds` for a waiting card and `idle_seconds` otherwise, which is
// exactly what this chip was printing, so every row said one number twice.
//
// Not dropped outright, because the two come apart under the other sorts.
// Sorting by activity makes the big number "how long since it last did
// anything" for every row including the waiting ones, and then the wait is a
// second fact that nothing else on the row carries. Compared rather than
// assumed, so this stays right if either rule changes.
function stateChip(t, w) {
  const dur = w ? t.wait_seconds : t.idle_seconds;
  const said = bigNumber(t) === dur;
  // A question asked says so, in place of the status. `ready` is true of both
  // kinds and is the less useful half: an agent that stopped will wait
  // forever without costing anything, and a question has somebody's work
  // behind it.
  const label = wasAsked(t) ? "asked you"
    : askedAPeer(t) ? "asked a peer" : statusLabel(t.status);
  const asking = wasAsked(t) || askedAPeer(t);
  return `<span class="chip ${asking ? "warn asked" : w ? "warn" : over(t) ? "" : "accent"}"
    title="${esc(wasAsked(t) ? "it put a question to you and is waiting"
      : askedAPeer(t) ? "it put a question to " + t.ask_peer + " and is waiting on that session"
      : t.status)}"
    >${esc(label)}${said ? "" : " " + ago(dur)}</span>`;
}

// The conventional "opens in a new window" mark: a box with an arrow leaving
// it. Drawn rather than typed, because the characters that mean this (U+29C9,
// U+2197) either render as a box in the board's mono stack or read as a plain
// arrow, and this sits next to `attach` where the difference is the whole
// message.
// Two sheets, drawn rather than typed.
//
// The clipboard EMOJI was the first attempt and it is the wrong tool: an emoji
// is a full-colour glyph with its own metrics and its own idea of a baseline,
// so it came out orange next to grey text, taller than the line it sat on, and
// nothing about it could be themed. This is one stroke weight in
// `currentColor`, which means it is whatever colour the chip is.
function copyIcon() {
  // 14px against a 12px line. An icon drawn at the text's own size reads as
  // smaller than the text, because letters fill their box and a line drawing
  // does not.
  return `<svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"
    fill="none" stroke="currentColor" stroke-width="1.4"
    stroke-linecap="round" stroke-linejoin="round">
    <rect x="5.5" y="5.5" width="9" height="9.5" rx="1.5"/>
    <path d="M10.5 5.5V3a1.5 1.5 0 0 0-1.5-1.5H3A1.5 1.5 0 0 0 1.5 3v6A1.5 1.5 0 0 0 3 10.5h2.5"/>
  </svg>`;
}

function popIcon() {
  return `<svg viewBox="0 0 16 16" width="11" height="11" aria-hidden="true"
    fill="none" stroke="currentColor" stroke-width="1.6"
    stroke-linecap="round" stroke-linejoin="round">
    <path d="M13 9.5V13a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h3.5"/>
    <path d="M9.5 2H14v4.5"/><path d="M14 2 7.5 8.5"/>
  </svg>`;
}

// Shared with paintStack's filter, and used by stackRow.
function over(t) { return ["done", "dead"].includes(t.status); }

// Keeping a card where it can be found.
//
// A pinned card sorts above everything on the stack and stays in the terminal
// switcher whether or not it has a terminal right now. Some sessions are
// permanent fixtures, and hunting for one in activity order is the wrong
// shape for something you reach for every day.
async function togglePin(id, on) {
  try {
    await patchTask(id, { pinned: on });
  } catch (e) {
    toast("that did not stick", e.message);
    return;
  }
  refresh();
}

// The star on a card, wherever cards are drawn.
function pinStar(t) {
  return `<span class="pin ${t.pinned ? "on" : ""}"
    title="${t.pinned ? "pinned. click to unpin" : "pin this to the top and to terminals"}"
    onclick="event.stopPropagation();togglePin('${t.id}', ${!t.pinned})"
    >${t.pinned ? "&#9733;" : "&#9734;"}</span>`;
}

// A pinned card with no terminal, clicked. There is nothing to attach to, so
// this offers to start one the same way the detail dialog does.
async function resumePinned(id) {
  let t;
  try { t = await api(`/v1/tasks/${id}`); } catch (e) { return; }
  current = t;
  // Onto the same card. A fixture that came back as a second card with the
  // same name would defeat the pin, since the pinned one would stay cold
  // forever while its replacement did the work.
  openLaunch(t.runner || "claude", t.resume_id || "", t.worktree || "", t.id);
}

// What the operator called this card. Colored from the tag's own name by the
// same hash the groups use, so a tag looks the same wherever it appears and
// picks up its group color for free.
//
// Clicking one filters to it, since seeing a tag and wanting only that is the
// same motion.
function tagChips(t) {
  return (t.tags || []).map(tag =>
    `<span class="chip tag" style="--ghue:${groupHue(tag)}"
       title="show only ${esc(tag)}"
       onclick="event.stopPropagation();filterByTag('${esc(tag).replace(/'/g, "&#39;")}')"
       >${esc(tag)}</span>`).join("");
}

// What the session said it did, as a chip on a finished card.
//
// Only on finished cards, because that is where the question comes up. A card
// in `done` with a recap has been written up. One without is either still
// worth writing up or was never worth starting, and those are different enough
// that the board should not make them look the same.
//
// The whole recap is in the tooltip rather than on the card. It is two or
// three sentences, which is too much for a chip and exactly right for a hover.
function recapChip(t) {
  if (t.status !== "done" && t.status !== "dead") return "";
  if (!t.recap) {
    // Not on a dead card. A session that was killed did not decline to write
    // itself up, it never got the chance, and marking that as a gap would put
    // a reproach on every card that crashed.
    if (t.status !== "done") return "";
    return `<span class="chip norecap"
      title="this finished without saying what it did. only a human moving the card produces that,
             or a session that ended before it could say. nothing is wrong, there is just no account
             of it."
      >no recap</span>`;
  }
  return `<span class="chip recap" title="${esc(t.recap)}">recap</span>`;
}


// A note written and held, said on the card.
//
// The text goes in the tooltip because a note is a sentence or three and a card
// is one line. What the card has to carry is that there IS one.
//
// NO SEND BUTTON HERE. Sending is the deliberate half of what a note is for,
// and a one-click send on a hover target beside `attach` is how a paragraph
// arrives mid-turn by accident.
function noteChip(t) {
  const note = (t.note || "").trim();
  if (!note) return "";
  return `<span class="chip note"
    title="${esc("held, not sent:\n\n" + note + "\n\nopen the card to send it")}"
    >note</span>`;
}

// Which model this session is running on, when it is not the runner's default.
//
// Only shown when one was chosen, so every card that existed before this looks
// exactly as it did. The point is that two cards in one directory, on one
// runner, on two models are otherwise indistinguishable, and the difference is
// the whole reason somebody chose.
//
// The name as typed, never interpreted. Atrium does not know what a model is
// any more than it knows what a source is: it is text handed to the runner in
// the shape that runner declared, and rendered back here unchanged.
function modelChip(t) {
  const model = (t.model || "").trim();
  if (!model) return "";
  return `<span class="chip model"
    title="${esc("this session was started on " + model + ". chosen once when it launched, " +
      "and kept across a restart")}"
    >${esc(model)}</span>`;
}

// Where this work came from, when it came from somewhere.
//
// The link is the whole reverse direction worth having. Ticket state on the
// board would be polling backwards and would sit next to the status column
// disagreeing with it; one click gets the truth from the system that owns it.
//
// The label prefers the external identifier over the source name, because
// "openziti/ziti#4211" says everything "github" says and more. The source is
// the fallback for an item whose identifier is not worth reading.
//
// Opened in a new tab and stopped from bubbling, so clicking the link does not
// also open the card behind it.
function originChip(t) {
  if (!t.source && !t.external_id && !t.url) return "";
  const label = t.external_id || t.source || "link";
  const title = [t.source, t.external_id, t.url].filter(Boolean).join(" ");
  const href = inertURL(t.url);
  if (!href) {
    // Either there is no url, or there is one that is not safe to make
    // clickable. Both render as text: the identifier is still worth showing
    // and the value is still visible on hover.
    return `<span class="chip origin" title="${esc(title)}">${esc(label)}</span>`;
  }
  return `<a class="chip origin" href="${esc(href)}" target="_blank" rel="noopener noreferrer"
      title="${esc(title)}" onclick="event.stopPropagation()">${esc(label)}</a>`;
}

// A url that is safe to put in an href, or empty.
//
// Escaping protects the ATTRIBUTE and does nothing about the SCHEME.
// `javascript:alert(1)` survives HTML escaping intact, and clicking it runs in
// the board's own context, where the settings, the grouping expression and
// every card live.
//
// This matters here specifically because intake data comes from outside. A
// source is a script reading GitHub or Zendesk, and its output becomes a link
// on a card. That is data from an external system rendered as something
// clickable, which is the stored-XSS shape however single-user the board is.
//
// An allow list, not a deny list. `javascript:` and `data:` are the two
// everybody thinks of and neither is the point: the point is that the set of
// schemes a browser will execute is not a set this code gets to enumerate.
function inertURL(raw) {
  const value = String(raw || "").trim();
  if (!value) return "";
  try {
    // Resolved against the page, so `/x` and `./x` work and a bare word does
    // not silently become a scheme.
    const u = new URL(value, location.href);
    return (u.protocol === "http:" || u.protocol === "https:") ? u.href : "";
  } catch (e) {
    return "";
  }
}

// Types a tag into the stack's search box, which already filters on it.
function filterByTag(tag) {
  switchView("stack");
  const q = document.getElementById("stack-q");
  if (!q) return;
  q.value = "#" + tag;
  paintStack();
}

// Which harness a row is, as a mark rather than a word.
//
// Abstract shapes, not the vendors' logos: a bad copy of somebody's trademark
// is worse than a shape that is merely consistent. The colors are the ones
// the runner chip already uses, so the two agree wherever both appear.
const RUNNER_MARKS = {
  // A seven-spoke burst.
  claude: `<path d="M12 3v18M4.5 6.5l15 11M19.5 6.5l-15 11M3 12h18"
    stroke="currentColor" stroke-width="2.4" stroke-linecap="round"/>`,
  // A ring, for the loop a codex session runs in.
  codex: `<circle cx="12" cy="12" r="7.5" fill="none"
    stroke="currentColor" stroke-width="2.4"/>
    <circle cx="12" cy="12" r="2.4" fill="currentColor"/>`,
  // A head and two ears, read as a llama at this size.
  ollama: `<rect x="8" y="9" width="8" height="12" rx="3" fill="currentColor"/>
    <rect x="8" y="3" width="2.6" height="5" rx="1.3" fill="currentColor"/>
    <rect x="13.4" y="3" width="2.6" height="5" rx="1.3" fill="currentColor"/>`,
  // A prompt.
  shell: `<path d="M5 7l5 5-5 5M12.5 17H19"
    stroke="currentColor" stroke-width="2.4" fill="none"
    stroke-linecap="round" stroke-linejoin="round"/>`
};

function runnerMark(runner) {
  const key = (runner || "").toLowerCase();
  const body = RUNNER_MARKS[key];
  // An unknown harness gets its first letter. Adding a shape for it would be
  // inventing a meaning atrium does not have.
  const inner = body
    || `<text x="12" y="16.5" text-anchor="middle" font-size="13"
         font-family="var(--sans)" font-weight="700"
         fill="currentColor">${esc((runner || "?").slice(0, 1).toUpperCase())}</text>`;
  return `<span class="rmark" data-runner="${esc(key)}" title="${esc(runner || "unknown runner")}">
    <svg viewBox="0 0 24 24" aria-hidden="true">${inner}</svg></span>`;
}

// Pending cards are built once and then left alone. Rebuilding markup on a
// poll is what silently reverted a typed pattern and applied the wrong rule,
// so this never re-renders a card that already exists: it only adds new ones
// and removes answered ones.
// How long an agent has been frozen, rather than the wall-clock time it asked.
//
// "asked 14:22:01" needs you to know what time it is now and do the
// subtraction, which is exactly the work you cannot be bothered to do on a
// phone at three in the morning. This one says the thing being decided: how
// long something has been stuck.
//
// Atrium refuses approval timeouts, so this is not a countdown and there is
// nothing running out. It is a fact about how long you have taken.
function waitedFor(requestedAt) {
  const t = Date.parse(requestedAt || "");
  if (!t) return "asked just now";
  const secs = Math.max(0, Math.round((Date.now() - t) / 1000));
  if (secs < 45) return "asked just now";
  return "frozen for " + ago(secs);
}

function permCard(p) {
  const el = document.createElement("div");
  el.className = "row perm";
  el.dataset.id = p.id;
  // Where the answer goes. A request from a room is answered by that room's own
  // daemon, so the address of it is kept on the card rather than being worked
  // out again at the moment of the click, when the poll that produced this may
  // have been replaced by one that no longer mentions it.
  el.dataset.room = p.room || "";
  el.dataset.perm = p.perm_id || p.id;
  el.style.cssText = "cursor:default;align-items:flex-start;flex-wrap:wrap";
  // One input only: the command. Editing it changes what actually runs.
  // The scope for always and never is chosen from the buttons underneath, so
  // there is never a second box to mistake for this one. The box grows to hold
  // the whole command, because two visible lines of a long one is not enough to
  // decide on.
  el.innerHTML = `
    <div class="who">
      <b>${esc(p.tool)}</b>
      <span>${p.agent ? `<span class="asker">${esc(p.agent)}</span> &middot; ` : ""}${
        waitedFor(p.requested_at)}</span>
      <!-- WHICH MACHINE, said before the command. The command is what gets
           read and approved, and a recursive delete of a build directory
           means one thing on this machine and another on a cloud instance. A
           queue that merges two machines and does not say which is a queue
           you cannot safely press approve in. -->
      ${p.room ? `<div class="hintline">
        <span class="chip warn">on ${esc(p.room)}</span>
        ${p.room_stale ? `<span class="chip warn">that room has gone quiet</span>` : ""}
        This runs on <b>${esc(p.room)}</b>, not here. Your answer is handed to that
        machine and it releases its own agent, so <b>always</b> and <b>never</b> write
        the rule in <b>${esc(p.room)}</b>'s rule list rather than in this one.</div>` : ""}
      <label class="eyebrow" for="cmd-${p.id}">command</label>
      <span class="hintline">this is what runs. edit it to run something else</span>
      <textarea class="cmd edit" id="cmd-${p.id}" rows="1" spellcheck="false"
        oninput="autosize(this)"
        title="whatever is in here is what runs once you approve"></textarea>
      ${p.details ? `<details class="change" open>
        <summary>what changes</summary>
        <pre class="diff">${diffHTML(p.details)}</pre>
      </details>` : ""}
      <div class="scope">
        <span class="lbl">stop asking about, if you press always or never</span>
        <code class="patview" id="pat-${p.id}"></code>
        <div class="hints">
          ${hintsFor(p).map(h =>
            `<button class="hint ${h.kind === "path" ? "path" : ""}" data-v="${esc(h.value)}"
              data-kind="${esc(h.kind || "command")}" title="${esc(h.why)}">${
              esc((h.icon ? h.icon + " " : "") + h.label)}</button>`).join("")}
          <button class="hint custom" data-custom="1" title="type your own pattern">custom&hellip;</button>
        </div>
      </div>
    </div>
    <!-- Two pairs, each reading the same way: what happens, then how long it
         lasts. "block" against "approve once" made the first look like the
         standing answer and the second like the exception. -->
    <div class="actions">
      <button class="go" data-do="approve"
        title="let this one through. the next matching request asks again">approve once</button>
      <button class="no" data-do="block"
        title="refuse this one. the next matching request asks again">block once</button>
      <button class="go" data-do="approve" data-forever="1"
        title="approve, and never ask again about the scope shown">always</button>
      <button class="no" data-do="block" data-forever="1"
        title="block, and never ask again about the scope shown">never</button>
    </div>`;

  // Values are set as properties, not markup, so nothing re-serialises them.
  const cmd = el.querySelector(".cmd.edit");
  cmd.value = p.command;
  cmd.defaultValue = p.command;
  // NOT autosized here, and that is the fix rather than an omission.
  //
  // `el` is still detached at this point, and a detached element measures
  // `scrollHeight` as 0, so `autosize` wrote `height: 2px` inline and left it
  // there. What you saw was `min-height` doing the whole job: every command
  // clipped to two lines, forever, with the inline height beating any rule
  // that would have grown it. A six line command showed its first two.
  //
  // It cost nothing on a desktop, where two lines is most commands, and it
  // made the phone unusable, where two lines is none of them. `sizeCommands`
  // runs once the card is in the document.

  const view = el.querySelector(".patview");
  // The kind decides how the pattern is read: a command shape, or a directory.
  // Kept on the card beside the pattern so decide sends both and cannot send a
  // directory as a command prefix.
  const setScope = (value, btn, kind) => {
    el.dataset.pattern = value;
    el.dataset.kind = kind || "command";
    view.textContent = value;
    view.classList.toggle("path", el.dataset.kind === "path");
    el.querySelectorAll(".hint").forEach(x => x.classList.toggle("on", x === btn));
  };
  // The default scope is the command shape, not a folder. A rule is made from a
  // request that was read, and widening it to a whole directory grants more
  // than was agreed to.
  const hints = [...el.querySelectorAll(".hint:not(.custom)")]
    .find(b => b.dataset.kind !== "path");
  setScope(prefixOf(p.tool, p.command), hints || null, "command");

  // Retyping the command re-derives the scope, unless you picked one yourself.
  cmd.addEventListener("input", () => {
    if (el.dataset.chosen === "1") return;
    setScope(prefixOf(p.tool, cmd.value), null, "command");
  });
  el.querySelectorAll(".hint").forEach(b => b.addEventListener("click", async () => {
    if (b.dataset.custom) {
      const choice = await askUser({
        title: "stop asking about",
        body: "Two ways to say it.<br><br>" +
          "<b>A command shape</b> matches the start of the command, or the whole " +
          "thing when it contains <code>*</code> or <code>?</code>.<br><br>" +
          "<b>A folder</b> matches any command that touches anything inside it, " +
          "whatever the command is and however the path is quoted.",
        buttons: [
          { label: "a command shape", value: "command", style: "go" },
          { label: "a folder", value: "path" }
        ]
      });
      if (choice === null) return;
      const v = await askText(
        choice === "path" ? "which folder" : "which command shape",
        choice === "path"
          ? "Anything under this folder stops asking. Name the narrowest one that covers your work."
          : "Plain text matches the <b>start</b> of the command. " +
            "Add <code>*</code> or <code>?</code> to match against the whole thing.",
        el.dataset.pattern || "");
      if (v === null || !v.trim()) return;
      el.dataset.chosen = "1";
      setScope(v.trim(), b, choice);
      return;
    }
    el.dataset.chosen = "1";
    setScope(b.dataset.v, b, b.dataset.kind);
  }));
  // Wrapped so a fault in one handler cannot leave a button looking dead, and
  // latched so a slow answer cannot be sent four times. Silence is the worst
  // possible feedback for a click, and a second click on a request already
  // being answered can only ever produce a conflict.
  el.querySelectorAll(".actions button").forEach(b => b.addEventListener("click", async () => {
    if (el.dataset.deciding === "1") return;
    el.dataset.deciding = "1";
    el.querySelectorAll(".actions button").forEach(x => { x.disabled = true; });
    try {
      await decide(p.id, b.dataset.do, b.dataset.forever === "1");
    } catch (err) {
      console.error(err);
      toast("that did not work", String(err && err.message || err));
      el.dataset.deciding = "";
      el.querySelectorAll(".actions button").forEach(x => { x.disabled = false; });
    }
  }));
  return el;
}

async function renderPerms() {
  // ONE QUEUE, BOTH MACHINES. A request on another machine that is only visible
  // on that machine's own board is a frozen agent nobody is going to find, and
  // a second panel for remote ones would be a second place to keep right and a
  // second place to forget to look.
  //
  // Ordered oldest first across both, like the stack: whoever has waited
  // longest is the one to answer, and which machine they are on has nothing to
  // do with it. The seconds are comparable because each was measured by the
  // daemon that owns the clock the request was recorded against.
  const [permissions, remote] = await Promise.all([
    api("/v1/permissions").then(r => r.permissions || []),
    remoteRequests()
  ]);
  const list = permissions.concat(remote).sort((a, b) =>
    (Date.parse(a.requested_at) || 0) - (Date.parse(b.requested_at) || 0));
  const host = document.getElementById("perms-list");

  let panel = host.querySelector(".panel.perms");
  if (!panel) {
    host.innerHTML = `<div class="panel perms"></div>`;
    panel = host.querySelector(".panel.perms");
  }

  const want = new Set(list.map(p => p.id));
  // Drop answered ones.
  panel.querySelectorAll(".perm").forEach(el => {
    if (!want.has(el.dataset.id)) el.remove();
  });
  // Add new ones. Anything already on screen keeps whatever you typed in it.
  const have = new Set([...panel.querySelectorAll(".perm")].map(el => el.dataset.id));
  list.forEach(p => { if (!have.has(p.id)) panel.appendChild(permCard(p)); });

  let empty = panel.querySelector(".empty");
  if (!list.length && !empty) {
    panel.innerHTML = `<div class="empty">no pending permissions</div>`;
  } else if (list.length && empty) {
    empty.remove();
  }

  sizeCommands();

  renderRules();
  renderHistory();
}

// Grows every command box to hold its whole command.
//
// A textarea can only be measured where it is drawn, so this cannot live in
// `permCard`, which builds a detached card, and it cannot run once and be
// done either. THREE THINGS make a box the wrong size, and all three are
// ordinary on a phone:
//
//   1. The card was just added, so nothing has measured it yet.
//   2. The perms view was hidden when it was added. A hidden element measures
//      as zero exactly like a detached one, so landing on the stack tab and
//      then going to perms is enough to do it.
//   3. The window changed width. Rotating a phone rewraps every command, and
//      the phone breakpoint changes the font size on the way past 480, which
//      rewraps them again.
//
// Cheap enough to be unconditional: it is one pass over a queue that is
// normally empty and never long.
function sizeCommands() {
  if (document.getElementById("perms").hidden) return;
  document.querySelectorAll("#perms-list .cmd.edit").forEach(autosize);
}

// Rotating a phone is a resize, and so is the desk monitor a popped-out window
// gets dragged onto.
addEventListener("resize", sizeCommands);

// Everything already answered, newest first. Auto-approved requests never
// appear in the pending queue, so this is the only place they are visible.
let allHistory = [];

async function renderHistory() {
  try {
    allHistory = (await api("/v1/permissions/history?limit=300")).permissions || [];
  } catch (e) { return; }
  const auto = allHistory.filter(p => p.decided_by && p.decided_by !== "you").length;
  document.getElementById("hist-count").innerHTML =
    `${allHistory.length}${auto ? ` &middot; ${auto} by rule` : ""}`;
  paintHistory();
}

function paintHistory() {
  const q = (document.getElementById("hist-q").value || "").toLowerCase();
  const only = document.querySelector("#hist-seg .on").dataset.v;
  const byRule = p => p.decided_by && p.decided_by !== "you";
  const list = allHistory.filter(p => {
    if (only === "rule" && !byRule(p)) return false;
    if (only === "you" && byRule(p)) return false;
    if ((only === "approve" || only === "block") && p.decision !== only) return false;
    // The agent is searchable too, so "show me everything the deploy session
    // did" is one filter rather than a scan.
    return !q || (p.tool + " " + p.command + " " + (p.decided_by || "") + " " +
      (p.agent || "")).toLowerCase().includes(q);
  });

  document.getElementById("hist-match").textContent =
    list.length === allHistory.length ? "" : `${list.length} of ${allHistory.length}`;

  sortHistory(list);

  // One line per decision. The command takes the slack and truncates, with the
  // whole thing on hover, so a screen shows many rather than four.
  //
  // A rule name is a link to the rule. Reading "a rule allowed this" and then
  // scrolling a list of a hundred and thirty to find which one is the work
  // this saves.
  setHTML(document.getElementById("history-list"), list.length
    ? `<div class="panel">` + list.map(p => `
        <div class="row line">
          <span class="chip ${p.decision === "approve" ? "accent" : "warn"}">${p.decision}</span>
          <span class="stamp">${when(p.decided_at)}</span>
          <span class="asker ell" title="${esc(p.agent || "")}">${esc(p.agent || "?")}</span>
          <span class="tool">${esc(p.tool)}</span>
          <code class="grow ell" title="${esc(p.command)}">${esc(p.command)}</code>
          <span class="by" title="${esc(p.reason || "")}">${p.decided_by === "auto"
            ? `<span class="auto" title="auto mode answered this, nobody was asked">auto</span>`
            : byRule(p)
              ? `rule ${ruleLink(p.decided_by)}`
              : p.rule_created
                ? `you, made rule ${ruleLink(p.rule_created)}`
                : "you"}</span>
        </div>`).join("") + `</div>`
    : `<div class="panel"><div class="empty">${allHistory.length
        ? "nothing matches that filter" : "nothing decided yet"}</div></div>`);
}

// A rule pattern rendered as a link to the rule itself.
function ruleLink(pattern) {
  return `<code class="rulelink" title="go to this rule"
    onclick="event.stopPropagation();gotoRule('${esc(pattern).replace(/'/g, "&#39;")}')"
    >${esc(pattern)}</code>`;
}

// Opens the standing rules, finds the rule and flashes it.
//
// The rules list is collapsed by default and filtered by a search box, so
// getting to one rule out of a hundred and thirty means opening, typing and
// scanning. This does all three.
async function gotoRule(pattern) {
  const rules = document.getElementById("rules-d");
  if (rules && !rules.open) rules.open = true;
  const q = document.getElementById("rules-q");
  if (q) { q.value = pattern; paintRules(); }
  // A frame for the list to repaint before hunting for the row.
  setTimeout(() => {
    const row = [...document.querySelectorAll("#rules-list .row.line code")]
      .find(c => c.textContent.trim().replace(/^\u{1F4C1}\s*/u, "") === pattern);
    const target = row && row.closest(".row");
    if (!target) { toast("no rule matches", pattern + " is no longer in the list"); return; }
    target.scrollIntoView({ behavior: "smooth", block: "center" });
    target.classList.remove("flash");
    void target.offsetWidth;
    target.classList.add("flash");
  }, 60);
}

