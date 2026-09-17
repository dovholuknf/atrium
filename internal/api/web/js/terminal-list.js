// ── how much room the session list gets ─────────────────────────────────────
//
// Three modes and a width, because the list is competing with the one thing
// on this page anybody came for. A terminal is measured in columns: every
// pixel the list takes is characters the session does not have, and eighty
// columns is not a preference.
//
// `full` is the list as it was. `mini` keeps the mark, eight characters and
// the left edge, which between them still say which session and whether it
// wants you. `off` gives the terminal the whole pane and leaves a strip to
// hover, and in that mode the list floats OVER the terminal rather than
// pushing it, so passing the pointer near the left edge never re-fits xterm.
//
// All of it is localStorage. It is a fact about this screen, and the same
// board on a laptop wants a different answer.
const TERMLIST_MIN = 150, TERMLIST_MAX = 520;
let termListMode = localStorage.getItem("atrium.termlist.mode") || "full";
let termListW = Number(localStorage.getItem("atrium.termlist.w")) || 260;
if (!["full", "mini", "off"].includes(termListMode)) termListMode = "full";

function applyTermList() {
  const lay = document.getElementById("term-layout");
  if (!lay) return;
  lay.classList.toggle("tl-mini", termListMode === "mini");
  lay.classList.toggle("tl-off", termListMode === "off");
  lay.style.setProperty("--termw", clampTermW(termListW) + "px");
}

// Puts the bridge across the gutter, level with the attached card.
//
// Measured rather than calculated. The gap is a CSS variable, the handle is
// nine pixels, the list is a width somebody dragged and the card is wherever
// the scroll left it: four numbers that all change independently, and the only
// one that is ever right is the one the browser just laid out.
//
// Cheap enough to call on every scroll: two `getBoundingClientRect` calls and
// four style writes, no layout of its own.
function placeTabBridge() {
  const bridge = document.getElementById("tab-bridge");
  if (!bridge) return;
  const lay = document.getElementById("term-layout");
  const card = document.querySelector("#term-list .card.on");
  // Nothing attached, or the list is not beside the terminal to begin with.
  // In `off` the list floats OVER the pane, so there is no gutter to cross.
  if (!lay || !card || termListMode === "off" || !term) { bridge.hidden = true; return; }

  const l = lay.getBoundingClientRect();
  const c = card.getBoundingClientRect();
  const list = document.getElementById("term-list");
  const lr = list.getBoundingClientRect();

  // NOTHING TO MEASURE IS NOT THE SAME AS NOTHING TO DRAW.
  //
  // Every rectangle on a `display: none` subtree is zero, and read as
  // geometry that says the card is scrolled out of sight and the gutter has
  // no width, so this hid the bridge for a reason that had nothing to do with
  // the bridge. A window resized while you are on another view is enough to
  // reach it, and coming back does not undo it.
  //
  // So a measurement taken on a hidden layout is DISCARDED rather than acted
  // on. The state is left exactly as it was, and the next placement on a
  // visible layout is the one that decides.
  if (!l.width && !l.height) return;

  // Scrolled out of sight behind its own header, or past the bottom. Drawing
  // it anyway would leave a bar of colour floating against the terminal with
  // nothing on the other end of it.
  if (c.bottom <= lr.top + 2 || c.top >= lr.bottom - 2) { bridge.hidden = true; return; }

  const pane = document.getElementById("term-pane");
  const p = pane ? pane.getBoundingClientRect() : { left: lr.right };
  // From just inside the card's right edge to just inside the pane's left
  // one, so it overlaps both and no seam shows at either end.
  const from = c.right - 2, to = p.left + 1;
  if (to <= from) { bridge.hidden = true; return; }

  bridge.hidden = false;
  bridge.style.left = (from - l.left) + "px";
  bridge.style.width = (to - from) + "px";
  bridge.style.top = (Math.max(c.top, lr.top) - l.top) + "px";
  bridge.style.height = (Math.min(c.bottom, lr.bottom) - Math.max(c.top, lr.top)) + "px";
  // The colours are the card's, whatever theme it is wearing.
  bridge.style.setProperty("--tabbg", getComputedStyle(card).backgroundColor);
  bridge.style.setProperty("--tabc", getComputedStyle(card).borderTopColor);
}

function clampTermW(px) {
  return Math.max(TERMLIST_MIN, Math.min(TERMLIST_MAX, Math.round(px || 0)));
}

// TWO BUTTONS, NOT ONE CYCLE.
//
// It was one button that went full to mini to off and round again, which is
// three states behind a control that only ever appeared to make things
// smaller. Halfway along, in `mini`, there was no way to go back: the button
// only shrank, and the way out was to shrink it AGAIN to `off` and find the
// rail. Nobody works that out. They collapse it once, lose the list, and stop
// using the mode.
//
// A pair says what it does, both directions are always reachable, and the end
// of the range is a disabled button rather than a wrap-around that undoes what
// you just did.
const TERM_LIST_MODES = ["off", "mini", "full"];

function setTermListMode(mode) {
  if (!TERM_LIST_MODES.includes(mode) || mode === termListMode) return;
  termListMode = mode;
  try { localStorage.setItem("atrium.termlist.mode", mode); } catch (e) {}
  applyTermList();
  renderTermList();
  // The pane just changed width, and xterm only knows its size because
  // something measured it. Deferred a frame so the measurement happens after
  // the layout it is measuring.
  requestAnimationFrame(onTermResize);
}

function stepTermList(by) {
  const at = TERM_LIST_MODES.indexOf(termListMode);
  const to = TERM_LIST_MODES[at + by];
  if (to) setTermListMode(to);
}

// Called by the rail, which is the way back from `off`.
function showTermList() { setTermListMode("mini"); }

// Only the directions that go anywhere.
//
// A disabled button at each end says "you cannot", which is a sentence nobody
// needed: at full width there is obviously no wider, and the greyed control
// is just something else in a header that has three things in it. What is
// missing IS the answer.
function termListButtons() {
  const at = TERM_LIST_MODES.indexOf(termListMode);
  let out = "";
  if (at > 0) {
    out += `<button class="termsort tlcycle" onclick="stepTermList(-1)"
      title="${termListMode === "full"
        ? "shrink it to just the names"
        : "hide it, and hover the edge to peek"}">&laquo;</button>`;
  }
  if (at < TERM_LIST_MODES.length - 1) {
    out += `<button class="termsort tlcycle" onclick="stepTermList(1)"
      title="${termListMode === "off"
        ? "put the list back"
        : "show the whole name"}">&raquo;</button>`;
  }
  return out;
}

// ── dragging the width ──────────────────────────────────────────────────────
//
// Pointer events with capture rather than mousemove on the document. Capture
// means the drag keeps following the pointer over the terminal, over an
// iframe, and past the edge of the window, which a listener on the element
// alone does not.
let gripFrom = 0, gripWas = 0;
function startGrip(e) {
  if (termListMode === "off") return;
  e.preventDefault();
  // THE ELEMENT IS HELD IN A LOCAL, not read off the event inside the
  // closures. `currentTarget` is only set while an event is being dispatched
  // and is null after that, so `done` threw on its first line: nothing was
  // removed, `gripping` was never taken off the body, and the pointermove
  // handler stayed bound to the grip for the life of the page. Every later
  // pass of the pointer near the handle then resized from a `gripFrom` set
  // during a drag that ended long ago, which is why the list grew a little
  // wider every time you moved across the cards.
  const el = e.currentTarget;
  gripFrom = e.clientX;
  gripWas = clampTermW(termListW);
  el.setPointerCapture(e.pointerId);
  el.classList.add("dragging");
  document.body.classList.add("gripping");
  const move = ev => setTermW(gripWas + (ev.clientX - gripFrom));
  const done = () => {
    el.classList.remove("dragging");
    document.body.classList.remove("gripping");
    el.removeEventListener("pointermove", move);
    el.removeEventListener("pointerup", done);
    el.removeEventListener("pointercancel", done);
    saveTermW();
  };
  el.addEventListener("pointermove", move);
  el.addEventListener("pointerup", done);
  el.addEventListener("pointercancel", done);
}

function gripKey(e) {
  const step = e.shiftKey ? 40 : 10;
  if (e.key === "ArrowLeft") { e.preventDefault(); setTermW(termListW - step); saveTermW(); }
  else if (e.key === "ArrowRight") { e.preventDefault(); setTermW(termListW + step); saveTermW(); }
  else return;
}

// The width is set on the element as it moves and written down only when the
// drag ends. A localStorage write per pointermove is a write per frame.
function setTermW(px) {
  termListW = clampTermW(px);
  const lay = document.getElementById("term-layout");
  if (lay) lay.style.setProperty("--termw", termListW + "px");
  // Live, so the terminal reflows under the handle rather than jumping when
  // it is let go. xterm's fit is cheap next to the repaint it triggers, and
  // this only runs while a pointer is down.
  onTermResize();
}

function saveTermW() {
  try { localStorage.setItem("atrium.termlist.w", String(termListW)); } catch (e) {}
}

// The name at eight characters, for the collapsed list.
//
// Built from the repo and the branch rather than by cutting the full address,
// which starts with the host and the org: every entry would be truncated in
// the part they all share. `github/dovholuknf/atrium:main` becomes `atrium:m`,
// which is not the whole story and is not meant to be. The full name is the
// title attribute, and the mode exists for the case where you already know
// what is in your own list.
// THE BRANCH IS NOT THE NAME. It was in here as a fallback, and it won every
// time, because a card carries a branch far more often than it carries a repo:
// every session was labelled `main`, which is the one word that cannot tell two
// of them apart. What identifies a session in a list of four is the DIRECTORY.
//
// So: the repo if the card knows one, the last segment of the working
// directory if not, and only then whatever the label ends with. The branch is
// in the tooltip, where a tie-break belongs.
function shortLabel(task) {
  if (!task) return "";
  const leaf = s => {
    const segs = String(s || "").replace(/\\/g, "/").split("/").filter(Boolean);
    return segs.length ? segs[segs.length - 1] : "";
  };
  // `atrium:main` is one segment of a label, and the half before the colon is
  // the half worth keeping.
  const base = String(task.repo || "").trim()
    || leaf(task.worktree)
    || leaf(terminalLabel(task) || task.display_title);
  return base.split(":")[0].slice(0, 10);
}

// The list of things worth attaching to, so switching is a click.
// Right click a session in the list. Renaming is the one people reach for
// most: a launched runner is named after its directory and a timestamp, which
// says nothing about what it is doing.
async function termMenu(e, id) {
  e.preventDefault();
  e.stopPropagation();
  const t = await api(`/v1/tasks/${id}`);
  // The runners, for `new agent here`. Same bargain as the board's card menu:
  // read once and held, because the menu is drawn on a click and a round trip
  // would open it a beat late every time.
  await loadHarnesses();
  showMenu(e, [
    // FIRST, because on a card whose runner has gone it is the only entry that
    // does anything, and it was not here at all.
    //
    // `attach` was the thing people reached for instead, and on a stopped card
    // it attaches to nothing: the pane says `nothing attached` and the socket
    // sits in its five minute reconnect saying "waiting for it to come back",
    // for a runner that is not coming. So attach is now offered only while
    // there is one, which is the rule the board's card menu already follows.
    //
    // Same two entries as the board, and the same reasoning: placement comes
    // off `lastPlace` rather than being asked, and the conversation is the
    // card's own unless you ask to choose.
    t.worktree && canResume(t) && !cannotResume(t) ? {
      label: "resume",
      help: "Starts a runner in this card's directory and picks the " +
        "conversation back up where it stopped. It opens where this card was " +
        "last open.",
      sub: [
        {
          label: "the last conversation",
          act: () => resumeCard(id, t, lastPlace(id), t.resume_id || "")
        },
        { label: "choose…", act: () => resumeCard(id, t, lastPlace(id)) }
      ]
    } : null,
    { label: "rename…", act: () => renameTask(id, t.display_title) },
    // Beside rename because they are the same kind of act: both write an
    // override, both survive the runner reconnecting and reporting for itself.
    // Rename decides what the row says, this decides where it sits.
    //
    // Only where there is a path to place it against. A card with no worktree
    // is not in the tree at all, so there is nothing for this to move.
    t.worktree
      ? { label: "which repo…", note: t.display_repo || "guessing",
          act: () => setTaskRepo(id, t.display_repo) }
      : null,
    // Only while there IS one. Attaching to a card whose runner has exited
    // draws an empty pane and then waits five minutes for something that has
    // already gone. `resume` above is what that click meant.
    t.supervised ? { label: "attach", act: () => attachTask(id) } : null,
    // THE SAME ENTRY THE BOARD'S CARD MENU HAS, because it is the same
    // question asked from the other surface. Starting a second agent beside
    // this one is most wanted while you are looking at the first one, which is
    // here, and the strip was the one place it could not be reached from.
    t.worktree ? { label: "new agent here", sub: newAgentSub(id, t) } : null,
    // The point of the whole feature, offered where you are when you need it:
    // the agent has stopped answering and you want to look at the directory
    // it is sitting in. Only for a card atrium owns a runner for, since a
    // window mode session has no terminal here to sit beside.
    t.supervised
      ? { label: t.shell ? "go to its shell" : "open a shell here", act: () => openShellFor(id) }
      : null,
    t.shell ? { label: "close its shell", act: () => closeShellFor(id) } : null,
    { sep: true },
    { label: "what did it do?", act: () => { current = t; openReview(); } },
    { label: "details…", act: () => openTask(id) },
    { sep: true },
    // NOT `pid > 0` ALONE. The pid is a reconnect hint that is never cleared,
    // so a card whose runner exited weeks ago still carries the number it had.
    // The menu offered `terminate` on a codex card already sitting in `dead`,
    // the kill signalled a pid belonging to nothing, and the server answered
    // 200, so pressing it reported neither success nor failure and looked like
    // the board had ignored the click.
    //
    // `dead` is the one status that means the process is known to have gone.
    // `done` is not: a card is filed when its work is finished and its runner
    // may still be sitting at a prompt, which is exactly when you want this.
    (t.supervised || t.pid > 0) && t.status !== "dead"
      ? { label: "terminate", danger: true, act: () => killById(id) } : null
  ].filter(Boolean));
}

// Attach to a card and land on its shell, from anywhere that offers it.
//
// Two steps rather than one, and in this order: attach first so the pane is on
// the right card, then switch. Asking for the shell before attaching would
// leave the toggle describing a card the pane is not showing.
// Ends a card's shell from outside it.
//
// `exit` in the shell does the same thing and is what you reach for when you
// are looking at it. This is for a shell open on a card you are not: it holds a
// process and its working directory, and the idle sweep is half an hour away.
//
// If the pane is showing that shell, it falls back to the runner on its own:
// the socket closes and `connectTerm` handles it, the same as `exit`.
async function closeShellFor(id) {
  try {
    await api(`/v1/tasks/${id}/shell`, { method: "DELETE" });
  } catch (e) {
    toast("could not close it", e.message);
    return;
  }
  toast("shell closed", "the agent's terminal is untouched");
  refresh();
}

async function openShellFor(id) {
  await attachTask(id);
  // `attachTask` raises a popped-out window instead of attaching here, and in
  // that case this pane is showing something else entirely. Switching would
  // move the wrong terminal.
  if (!termTask || termTask.id !== id) return;
  await showTerminal("shell");
}

// The name is an override, so it survives the runner reconnecting and
// reporting its directory again.
async function renameTask(id, currentName) {
  const name = await askText("rename this session",
    "Shown on the card and in the terminal list. Kept when the runner " +
    "reconnects and reports its own name again.", currentName || "");
  if (name === null) return;
  await patchTask(id, { overrides: { title: name.trim() } });
  refresh();
}

// WHICH REPOSITORY THIS IS, when the answer read off the path is wrong.
//
// The daemon resolves the repo in three tiers: this override, then whatever the
// launcher recorded, then a guess read out of the worktree. The guess handles
// the ordinary case and cannot handle a checkout kept somewhere that does not
// look like `<forge>/<org>/<repo>`, so this is the way out of that.
//
// It decides WHERE THE ROW SITS, not what it is called. The strip's headings
// are built by anchoring on the repo inside the worktree path, so a card with
// the wrong one lands under the wrong heading, or under three headings named
// after directories below the checkout. Renaming is the other entry and answers
// a different question.
//
// Empty clears it, which is what `SetOverrides` does with an empty value, and
// then the guess applies again. That is the undo.
async function setTaskRepo(id, current) {
  const repo = await askText("which repository is this",
    "Decides where this session sits in the terminal list, which groups by " +
    "forge, org and repo read out of the worktree path. Atrium guesses this " +
    "when nothing told it. Leave it empty to go back to the guess.",
    current || "");
  if (repo === null) return;
  await patchTask(id, { overrides: { repo: repo.trim() } });
  refresh();
}

// WHERE A SESSION LIVES, as the path its heading is built from.
//
// `terminalLabel` already composes `github/dovholuknf/atrium:atrium-backlog`
// out of the worktree. Everything before the colon is the place and the branch
// is the leaf, so the tree is a split rather than anything new to derive.
//
// A card with no path at all still has to land somewhere, and the empty array
// is that somewhere: it sorts before every heading and draws as loose rows at
// the top of the strip. A session started in a directory atrium cannot ask git
// about is not an error and should not be filed under a heading invented for
// it.
// Where a row nests, and what it is called once it is there.
//
// THE TWO HALVES COME FROM THE TASK, NOT FROM SPLITTING THE LABEL. This split
// on the last colon of the joined label, which is right until a field holds a
// colon of its own. One did: a card whose branch was recorded as
// `main:desktop-edge-win` produced a label with two colons, and the split
// handed back `desktop-edge-win:main` as a directory. The card grew a heading
// of its own next to the repo it belongs under.
function termPathOf(t) {
  const p = terminalParts(t);
  const label = terminalLabel(t) || t.display_title || "";
  return { segs: p.where.split("/").filter(Boolean), leaf: p.leaf || label };
}

// THE NAME SOMEBODY GAVE THIS CARD, WHEN THE PATH DOES NOT ALREADY SAY IT.
//
// `terminalLabel` builds a label out of the worktree, the repo and the branch,
// and never looks at what the card is called. That is right for placing a row
// in the tree and wrong for naming it, because every session ever started in
// one checkout derives the SAME string. Five cards on `D:/git/.../atrium` all
// read `github/dovholuknf/atrium:main`, and the one that was waiting on the
// operator, titled `spike: mcp-gateway in, beside, or rooms`, was drawn as an
// anonymous `main` at the bottom of the strip.
//
// `display_title` differs from `title` when an override was written, which is
// `atrium join --title` and `atrium launch --title` as well as a rename.
//
// SO THE OVERRIDE DOES NOT REPLACE THE LABEL, IT IS ADDED TO IT. Most cards
// here carry one and most of those are the repo name again: `zrok-research`
// against `github/openziti/zrok:zrok-research`. Swapping the label for it would
// shorten ten rows to something the heading above them already said, and the
// pinned group has no headings at all, so those rows would lose the org and the
// repo entirely.
//
// Contained rather than equal, because the useful cases are exactly the ones
// that say something new. `ziti-openwrt` is already in its own path and adds
// nothing. `spike: mcp-gateway in, beside, or rooms` is not and is the whole
// reason this exists.
function termExtraName(t) {
  const shown = String((t && t.display_title) || "").trim();
  const base = String((t && t.title) || "").trim();
  if (!shown || shown === base) return "";
  const path = terminalLabel(t) || "";
  return path.toLowerCase().includes(shown.toLowerCase()) ? "" : shown;
}

// What a row reads as, for deciding whether two of them read the same.
function termRowName(t) {
  const extra = termExtraName(t);
  const path = terminalLabel(t) || (t && t.display_title) || "";
  return extra ? path + " " + extra : path;
}

// TWO ROWS THAT READ THE SAME ARE TWO ROWS YOU CANNOT CHOOSE BETWEEN.
//
// Naming by hand fixes the cards somebody has named and nothing else. Three
// sessions in one checkout that nobody renamed still derive one string, and
// the strip offers three identical rows, one of which is live, one of which
// has exited and one of which belongs to another runner entirely.
//
// So a row whose name is not its own says what makes it different. The runner
// when that tells them apart, which is the common case and the readable one,
// and the tail of the card id when it does not, which is ugly and is still
// better than two rows that cannot be told apart at all.
//
// Held per render rather than computed per row: the question is about the
// whole drawn list and a row cannot answer it alone. Keyed by card id so the
// answer survives the tree walk, which visits rows in an order this does not
// know about.
let termSuffix = new Map();

function termNoteDuplicates(tasks) {
  const by = new Map();
  for (const t of tasks) {
    const k = termRowName(t);
    if (!k) continue;
    if (!by.has(k)) by.set(k, []);
    by.get(k).push(t);
  }
  const out = new Map();
  for (const group of by.values()) {
    if (group.length < 2) continue;
    // Only when the runner is a different answer for every one of them. Two
    // claude sessions both labelled `(claude)` is the same problem with more
    // characters in it.
    const runners = new Set(group.map(t => String(t.runner || "").trim()));
    const byRunner = runners.size === group.length && !runners.has("");
    for (const t of group) {
      out.set(t.id, byRunner
        ? ` (${String(t.runner).trim()})`
        : ` (${String(t.id).slice(-4)})`);
    }
  }
  termSuffix = out;
}

// The strip as a tree of headings.
//
// ELEVEN ENTRIES REPEATED `github/` ELEVEN TIMES, and the characters that told
// them apart were the last few, so the list was widest where it said least.
// Entries were already being cut at the FRONT to keep the leaf, which is the
// symptom of a shared prefix drawn once per row. A heading holds it once.
//
// EVERY FORGE AND EVERY ORG GETS ITS OWN HEADING, even when it holds one
// thing.
//
// A chain of only children used to be folded into one heading, so `github`
// containing only `dovholuknf` read as `github/dovholuknf`. It was done to keep
// the heading count down and it made the list inconsistent in the way that
// matters: `openziti` was drawn as an org and `netfoundry` was drawn as text on
// a row, so the same kind of thing appeared in two shapes and one of them
// looked like the grouping had failed to recognise it. The operator read it
// exactly that way.
//
// The heading count is kept down by the OTHER rule instead, in `termNodeHTML`:
// a level holding a single ROW folds into that row as `project:branch`. That
// one removes headings that group nothing, where this one removed headings
// that name something.
function termTree(list) {
  const root = { name: "", rows: [], kids: new Map() };
  for (const t of list) {
    const { segs } = termPathOf(t);
    let node = root;
    for (const seg of segs) {
      if (!node.kids.has(seg)) node.kids.set(seg, { name: seg, rows: [], kids: new Map() });
      node = node.kids.get(seg);
    }
    node.rows.push(t);
  }
  return root;
}

// The runner's mark, moving while the runner is.
//
// The strip is where you look to decide which session to go to, and every row
// in it looked the same whether the agent was grinding through a build or sat
// at a prompt. The board answers that with a chip that spells the state out in
// words, which is right in a column and does not fit in a strip a few hundred
// pixels wide. So the mark that is already there carries it.
//
// CSS RATHER THAN THE ANIMATED IMAGE THIS WAS ASKED FOR, and the reasons are
// worth stating because a gif is the obvious answer:
//
//   - There is no build step here and nothing is served that is not in the
//     repository, so a gif is a binary to vendor, embed and cache-bust.
//   - It could not take the session's colour. The mark wears `--tabc`, which
//     is that terminal's own theme, and a gif is the pixels it was saved as.
//   - `prefers-reduced-motion` cannot stop a gif. It stops this, by name, in
//     the block that already exists for the board's live chips.
//   - The board already animates exactly this state three ways, keyed on the
//     same field. A fourth spelling of "working" would be a second answer.
//
// The state is `t.activity.what`, which is what the board's `activityChip`
// reads, and the guards are the same: nothing is drawn as working for a card
// that is waiting on you, finished, or shelved. Activity is held in memory and
// outlives the status change that filed the card, so a card in `done` can
// still be carrying a stale `thinking`.
function termRunnerMark(t) {
  const mark = runnerMark(t.runner);
  const a = t.activity;
  const working = a && a.what && a.what !== "idle" &&
    !isWaiting(t) && t.status !== "shelved" && !staleActivity(t);
  if (!working) return mark;
  // Inserted into the class list the shared builder produced, rather than the
  // builder growing a parameter. `runnerMark` is the board's and is called
  // from three places that do not want this.
  return mark.replace('class="rmark"', `class="rmark working ${esc(a.what)}"`);
}

// One entry in the switcher.
//
// A BOARD CARD, the same shape and the same class, because the switcher had
// grown its own card with its own borders and hover and the two drifted until
// one session looked like two different things depending on the view.
//
// `deep` is whether a heading above already says where this is. Under one the
// row draws its LEAF alone, which is the whole point of the headings: the
// shared prefix moves up and stops being drawn once per row.
//
function termRow(t, deep) {
  // THE ENTRY WEARS ITS OWN TERMINAL'S COLOURS, so the list reads as a row of
  // tabs onto the panes behind it rather than as a list of names. Amber for
  // waiting said something the board already says better and cost the one
  // signal that is actually per session.
  const th = themeFor(t);
  const style = `--tabc:${th.cursor || th.foreground};--tabbg:${th.background}`;
  const full = terminalLabel(t) || t.display_title;
  const leaf = termPathOf(t).leaf;
  // Both empty for most rows. See termExtraName and termNoteDuplicates.
  const extra = termExtraName(t);
  const tail = termSuffix.get(t.id) || "";
  const shown = (deep ? leaf : full) + tail;
  // The hover keeps the whole address whatever the row had room to draw.
  const hover = full + tail + (extra ? ": " + extra : "");
  return `
    <div class="card tab ${termTask && t.id === termTask.id ? "on" : ""}${
           // COLD, NOT GONE. Only ever a pinned row, since an unpinned one
           // without a runner is not drawn at all. Greyed rather than removed
           // so the bucket keeps the shape you gave it, and it still answers
           // a click: there is nothing to attach to, so it offers to start
           // the session again where it was.
           t.supervised ? "" : " cold"}"
         data-id="${t.id}"
         title="${t.supervised ? "" : "this one has exited. click to start it again here"}"
         style="${style}"
         onclick="${t.supervised
           ? `attachTask('${t.id}')`
           : `resumePinned('${t.id}')`}"
         oncontextmenu="termMenu(event, '${t.id}')">
      <div class="card-line">
        <div class="title">
          <span class="pin ${t.pinned ? "on" : ""}"
            title="${t.pinned ? "always here. click to unpin" : "keep this here"}"
            onclick="event.stopPropagation();togglePin('${t.id}', ${!t.pinned})"
            >${t.pinned ? "&#9733;" : "&#9734;"}</span>${termRunnerMark(t)}<span
            class="tname" title="${esc(hover)}">${esc(shown)}</span>${
              // WHAT THIS SESSION IS FOR, after where it lives.
              //
              // A SIBLING OF `.tname`, NOT A CHILD OF IT. `.tname` is
              // `direction: rtl` so that a name too long for the strip keeps
              // its tail, and an inline box added inside an rtl one is laid
              // out at the LEFT, which would put the note in front of the
              // address it is annotating.
              extra
                ? `<span class="tnote" title="${esc(hover)}">${esc(extra)}</span>`
                : ""
            }<span
            class="tshort" title="${esc(hover)}"
            >${esc(shortLabel(t))}</span>
        </div>
        ${poppedOut(t.id)
          // Says where it is rather than letting you click and wonder why
          // nothing happened. Clicking raises that window.
          ? `<div class="chips"><span class="chip accent"
               title="this session is showing in a window of its own. click to raise it"
               >&#8599;</span></div>` : ""}
      </div>
    </div>`;
}

// The whole strip, everything under where it lives.
//
// PINNING STOPS BEING A DIVIDER HERE, which is the decision this had to
// settle. Grouping and pinning both wanted to be the top level split and they
// cannot both be, and the obvious answer is the wrong one.
//
// The obvious answer is what the stack does: pinned as one group above all the
// others, since pinning outranks the sort there. Run against the real board it
// falls apart. Eight of ten terminals here are pinned, so the pinned block is
// almost the whole list drawn flat, and the repeated `github/` prefixes that
// this exists to remove are all still in it. A split that puts most rows on
// one side is not a split.
//
// So pinning keeps its meaning and loses its heading. It still sorts first,
// now WITHIN each group, and the star says which rows it applies to. That star
// was invisible until two days ago, which is probably why the divider was
// carrying the whole signal.
//
// A HEADING IS NOT A CONTROL HERE. It labels and nothing else: no click
// target, no accordion. `docs/dispatch-queue.md` group F has an open complaint
// that a group heading on the BOARD toggles when you click the white space
// around it, and three levels of heading in a narrow strip would multiply
// whatever is decided there. A label cannot have that problem.
function termGroupsHTML(list) {
  const folded = termFolded();
  // THE BOARD'S GROUPING, ON THE STRIP. One setting, two surfaces: `grouper`
  // and `groupingPrefs` are the board's and are read here rather than copied,
  // so changing how the board groups changes how this groups and there is no
  // second answer to keep in step.
  //
  // PROJECT IS THE EXCEPTION AND STAYS THE TREE. The board draws a project as
  // one flat heading, `org/repo`, which is right for a column and wrong for a
  // strip a few hundred pixels wide: eleven entries repeating `github/` is the
  // complaint `termTree` exists to answer, and it answers it by lifting the
  // shared prefix into nested headings. Same grouping, drawn for the space it
  // is in. Every other mode has no path to nest, so it draws flat.
  const p = typeof groupingPrefs === "function" ? groupingPrefs() : null;
  const g = typeof grouper === "function" ? grouper() : null;
  // OFF MEANS OFF, and it did not.
  //
  // `grouper()` answers null for two different reasons: grouping is switched
  // OFF, or the mode is one this strip draws its own way. Both arrived here as
  // "no grouper", and the fallback is the path tree, so pressing `off` swapped
  // one set of headings for another set of headings. The one thing it could not
  // do was stop grouping.
  //
  // `p.on` is the question that was being skipped. Off is a flat list in the
  // order it was handed over, which is what sorting by activity means when
  // nothing is allowed to reorder it into buckets.
  if (p && !p.on) return list.map(t => termRow(t, false)).join("");
  if (!g || !p || (p.mode === "project" && !String(p.by || "").trim())) {
    return termNodeHTML(termTree(list), 0, "", folded);
  }
  return termFlatGroupsHTML(list, g, folded);
}

// One heading per group, in the grouper's own order.
//
// A card can land in SEVERAL groups, which is what `many` means and what
// grouping by tag does. That is why the rows are collected into a map rather
// than the list being partitioned: partitioning has to decide where a card
// with three tags goes, and the answer is all three.
//
// The heading keys are prefixed, so folding a group called `dotfiles` here
// does not also fold a path segment called `dotfiles` in the tree. They are
// different groupings of the same sessions and a fold is an opinion about one
// of them.
function termFlatGroupsHTML(list, g, folded) {
  const by = new Map();
  for (const t of list) {
    let names = [];
    try { names = g.of(t) || []; } catch (e) { names = []; }
    if (!names.length) names = [""];
    for (const n of names) {
      const key = String(n ?? "");
      if (!by.has(key)) by.set(key, []);
      by.get(key).push(t);
    }
  }
  const names = [...by.keys()].sort((a, b) => {
    try { return g.cmp(a, b); } catch (e) { return a.localeCompare(b); }
  });
  return names.map(name => {
    // A group with no name is the absence of an answer rather than one, and
    // the tree calls that `uncategorized`. Same word, so the two modes do not
    // disagree about what nothing is called.
    const shown = name || "uncategorized";
    const at = "g:" + shown;
    const off = folded.has(at);
    const head = termHeading(shown, at, by.get(name).length, off);
    if (off) return head;
    return head + `<div class="tnest">${
      by.get(name).map(t => termRow(t, false)).join("")}</div>`;
  }).join("");
}

// WHICH GROUPS ARE FOLDED, kept in this browser and keyed by the path itself.
//
// Not on the card and not on the daemon: this is a property of how somebody is
// reading the list right now, and two windows on one board are allowed to
// disagree about what is folded.
//
// Keyed by path rather than by position, so folding `github/openziti` and then
// starting a session in a new org leaves the fold where it was instead of
// sliding onto whatever moved into that slot.
const TERM_FOLDED = "atrium.termfolded";

function termFolded() {
  try { return new Set(JSON.parse(localStorage.getItem(TERM_FOLDED) || "[]")); }
  catch (e) { return new Set(); }
}

function toggleTermGroup(path) {
  const folded = termFolded();
  if (folded.has(path)) folded.delete(path);
  else folded.add(path);
  try { localStorage.setItem(TERM_FOLDED, JSON.stringify([...folded])); } catch (e) {}
  renderTermList();
}

// A group heading, which IS a control now.
//
// THE ROW IS NOT THE TARGET, and that is deliberate. `docs/dispatch-queue.md`
// group F carries a complaint about the board's group headings: the whole
// width toggles the accordion and nothing says so, so clicking what looks like
// empty space next to a name collapses the thing you were reading. The button
// here is the caret and the name and stops there, and the rule that fills the
// rest of the width is decoration outside it.
//
// The count is on the heading because a folded group has to say what is inside
// it or folding loses information rather than hiding it.
function termHeading(name, path, count, folded) {
  // The apostrophe, on top of the HTML escaping. A group name is free text now
  // that these headings can come from tags and from operator-written code, and
  // HTML escaping is not JavaScript escaping: `it's mine` closes the quoted
  // argument and the rest of the attribute parses as nonsense. Same fix, same
  // reason, as `tagChips` in `stack.js`.
  const arg = esc(path).replace(/'/g, "&#39;");
  return `<button class="tgroup" onclick="toggleTermGroup('${arg}')"
      title="${folded ? "show" : "hide"} ${esc(name)}"
      ><span class="tcaret">${folded ? "&#9656;" : "&#9662;"}</span
      ><span class="tgname">${esc(name)}</span
      ><span class="tgcount">${count}</span></button>`;
}

// Rows at this level first, then the levels under it.
//
// The root has no name and draws no heading, which is what puts a card with no
// path at the top as loose rows rather than under a heading invented for it.
//
// A LEVEL HOLDING ONE ROW IS NOT A LEVEL. It draws as `project:branch` on the
// row instead of a heading with a single thing under it.
//
// This is the difference between the design working and not. The first version
// drew eleven headings for ten rows on the real board, which is the failure the
// whole item warns about: three levels of accordion in a strip a few hundred
// pixels wide spending more height on headings than on sessions. Folding the
// single-row levels took it to five headings for the same ten rows, and the
// ones that remain are the ones that group something.
// NESTED CONTAINERS, NOT A FLAT LIST WITH AN INDENT CLASS.
//
// The first version drew headings with a `data-depth` and left the rows at the
// left edge, so a session three levels down sat in line with one at the top and
// the tree was in the markup and nowhere on the screen. Indenting rows by depth
// would have fixed the alignment and nothing else.
//
// A container per level gets both for free: the indent is the container's, and
// a border down its left edge draws the vertical guide that says which rows
// belong to which heading, the way `tree` does with line drawing characters.
// The lines are continuous because the box is, rather than being reassembled
// per row out of glyphs that have to agree about depth.
//
// It also makes folding a container with nothing in it rather than a filter
// over a flat list.
function termNodeHTML(node, depth, path, folded) {
  const at = node.name ? (path ? path + "/" + node.name : node.name) : "";
  const isFolded = at !== "" && folded.has(at);
  let out = node.name ? termHeading(node.name, at, termCount(node), isFolded) : "";
  if (isFolded) return out;

  // AT THE ROOT, LOOSE ROWS GET A HEADING OF THEIR OWN.
  //
  // A session in a directory atrium cannot read a forge and an org out of sits
  // under no heading, and at the top of the list that read as a session that
  // had escaped the grouping rather than one the grouping has nothing to say
  // about. `uncategorized` says which it is.
  //
  // Named and moved to the BOTTOM, since a catch-all above everything is the
  // first thing read and the least interesting. And only when there is
  // something to contrast it with: with no groups at all it is the whole list,
  // and a heading over everything names nothing, which is the same rule the
  // pinned divider followed.
  const loose = node.name ? "" : (() => {
    if (!node.rows.length) return "";
    if (!node.kids.size) return node.rows.map(t => termRow(t, false)).join("");
    const isOff = folded.has("uncategorized");
    const head = termHeading("uncategorized", "uncategorized", node.rows.length, isOff);
    if (isOff) return head;
    return head + `<div class="tnest">${node.rows.map(t => termRow(t, true)).join("")}</div>`;
  })();

  // EVERY LEVEL IS A HEADING, including one holding a single row.
  //
  // This folded a level with one row into that row, so a repo with one branch
  // came out as `ziti-openwrt:firmware-upgrade-recovery-docs` on a row while
  // `desktop-edge-win` beside it, which happened to have two, was a heading.
  // Two repos, two shapes, decided by something that is not about them.
  //
  // It was done to keep the height down and that is the wrong thing to spend
  // consistency on. The same instinct folded orgs a moment earlier and made
  // the same mess: the shape of the list should say host, then org, then repo,
  // every time, so somebody can read it without working out which rule applied
  // to which row.
  let inner = node.name ? node.rows.map(t => termRow(t, true)).join("") : "";
  // HEADINGS ARE ALWAYS IN NAME ORDER, whatever the rows under them are sorted
  // by.
  //
  // The children arrive in a Map, so iterating it draws them in the order the
  // rows happened to build them in, which is the row sort. Under `by activity`
  // that order changes every poll, so `ziti` and `ziti-sdk-csharp` swapped
  // places while nothing about either had changed. A heading names a place,
  // and a place does not become more urgent, so it has no business moving.
  // Rows inside a heading still follow whichever sort is on: that is the thing
  // the sort was asked about.
  for (const kid of [...node.kids.values()].sort((a, b) => a.name.localeCompare(b.name))) {
    inner += termNodeHTML(kid, depth + 1, at, folded);
  }
  // The root has no heading, so it has nothing to indent under and no guide to
  // draw. Wrapping it would push the whole strip right for no reason.
  return node.name ? out + `<div class="tnest">${inner}</div>` : out + inner + loose;
}

// How many sessions are under a heading, at any depth. A folded group has to
// say what is inside it, or folding loses information rather than hiding it.
function termCount(node) {
  let n = node.rows.length;
  for (const kid of node.kids.values()) n += termCount(kid);
  return n;
}

// `termPinBreak` stood here. It drew `pinned` and `the rest` across the strip,
// and `termGroupsHTML` replaced it: once the list has headings for where a
// session lives, a second kind of heading for whether it is pinned is two
// systems competing for the same top level. Pinned is a group now. The stack
// still has its own `.pinbreak`, which is the same idea in a list that has no
// other headings in it.

// Sort the strip in place before grouping or rendering. Keeping this separate
// lets test-sort-order.js check stability across different input orders.
function termOrder(tasks) {
  if (sortByActivity) {
    // Waiting sessions come first, then recent activity. Use the shared tiebreak
    // so equal values do not cause rows to move between polls.
    tasks.sort((a, b) => {
      const w = (isWaiting(b) ? 1 : 0) - (isWaiting(a) ? 1 : 0);
      if (w) return w;
      return (a.idle_seconds || 0) - (b.idle_seconds || 0) || cardTieBreak(a, b);
    });
  } else {
    // Sort by the displayed label. Grouping preserves this order within headings.
    const named = t => terminalLabel(t) || t.display_title || "";
    tasks.sort((a, b) => named(a).localeCompare(named(b)) || cardTieBreak(a, b));
  }
  // Lift pinned rows with a stable second pass to preserve the order within
  // pinned and unpinned groups.
  //
  // WITHIN the pinned set the order is the one somebody dragged, and neither
  // sort above gets a say. That is the difference between a bucket and a
  // filter: activity and name both move on their own, so a set arranged by
  // hand and then sorted by either is a set that rearranges itself while you
  // are not looking. `pin_order` is zero on every card until the first drag,
  // which leaves them tied and falling through to the sort underneath, so an
  // untouched board looks exactly as it did.
  tasks.sort((a, b) => (b.pinned ? 1 : 0) - (a.pinned ? 1 : 0) ||
    (a.pinned && b.pinned ? (a.pin_order || 0) - (b.pin_order || 0) : 0));
  return tasks;
}

async function renderTermList() {
  // Cheap and idempotent, and it means the mode survives a reload without
  // finding a second place to call it from.
  applyTermList();
  let all = [];
  try { all = (await api("/v1/tasks")).tasks || []; } catch (e) { return; }
  // The header's count, from the list that just loaded. The terminals view is
  // the one you are most likely to be on while something is working, and it is
  // the view that does not call `renderBoard`.
  if (typeof paintWorking === "function") paintWorking(all);
  // AND THE BOARD'S COPY OF THE LIST. `renderBoard` and `renderStack` both set
  // this and only one of the three runs per poll, so on the terminals view it
  // was whatever the last visit to another view left behind. The alert for a
  // card arriving reads it, and a stale list means a card that turned up while
  // you were watching a terminal is announced whenever you next look at the
  // board, or not at all.
  lastTasks = all;

  // Terminals, AND the pinned bucket whether or not it is running.
  //
  // These are two different lists sharing one strip, and the rule that tells
  // them apart is pinning. An unpinned row is a live terminal and nothing
  // else: it appears when a runner starts and goes when it exits, because
  // there is nothing left to switch to. A PINNED row is somewhere you keep a
  // session, and it stays through the exit, drawn cold.
  //
  // This reverses a decision recorded here, that a row which cannot be
  // switched to is a row that does nothing. The reasoning held while pinning
  // only meant "sort me first". It stops holding once pinning means "this is
  // mine and I put it here": a bucket you arranged that empties itself when
  // you quit a session is not a bucket, and putting the row back by hand is
  // the work that pinning it was supposed to save. A cold row still does
  // something, it just is not attaching: it holds its place, and clicking it
  // starts the session again in the same directory, onto the same card.
  const tasks = all.filter(t => t.supervised || t.pinned);
  // The badge counts what is actually attachable, since it is a count of live
  // terminals rather than of rows.
  badge("c-term", tasks.filter(t => t.supervised).length);

  // The attached session is gone, so the pane showing it is stale.
  //
  // Driven off the poll rather than off the socket closing. A socket close is
  // one event that can be missed: a tab in the background, a daemon that went
  // away, a close the browser never delivered. This asks the daemon what is
  // actually running, which is the same question the list is already asking.
  // clearTermPane rather than closeTerm, since closeTerm refreshes and this is
  // running inside a refresh.
  //
  // ATTACHABLE, not merely present. A pinned row outlives its runner now, so
  // testing that the id is still in the list would leave the pane holding a
  // dead terminal for as long as the row stayed pinned, which is forever.
  if (termTask && !tasks.some(t => t.id === termTask.id && t.supervised)) clearTermPane();

  termOrder(tasks);
  // Before anything is drawn, and over the rows that will BE drawn: a card
  // filtered out above cannot be confused with one on screen.
  termNoteDuplicates(tasks);
  const pinnedTasks = tasks.filter(t => t.pinned);
  pinnedNow = pinnedTasks.map(t => t.id);

  const host = document.getElementById("term-list");
  const toggle = `<div class="termhead">
      <button class="termsort" onclick="toggleTermSort()"
        title="newest activity first, and anything waiting on you above that">
        ${sortByActivity ? "sorted by activity" : "sorted by name"}</button>
      <span class="grow"></span>
      ${termListButtons()}
    </div>
    <!-- THE SAME CONTROL THE BOARD AND THE STACK HAVE, filled in by
         \`paintGroupSegs\` from the same list. Grouping is a way of reading the
         same sessions rather than a property of one screen, so the control
         belongs wherever you are when you decide you want it, and there is one
         setting behind all three.
         Its own row, because five buttons do not fit beside the sort toggle in
         a strip this narrow. Hidden in \`mini\`, where there is no room for any
         of it. -->
    <div class="termhead termgroups">
      <span class="barlabel">group</span>
      <div class="seg groupseg" id="term-group"></div>
    </div>`;

  setHTML(host, tasks.length
    // A BOARD CARD, the same shape and the same class. The switcher had grown
    // its own card with its own borders, its own hover and its own chips, and
    // the two drifted until the same session looked like two different things
    // depending on which view you were in.
    //
    // What it does NOT carry is the status chip and the duration beside it.
    // This list is a switcher: the question it answers is which session, and
    // a pill that rewrites itself every few seconds in the corner of your eye
    // answers a question nobody asked it. The board is where "how long has
    // this been sitting" belongs, and it says it better.
    ? toggle + termBucketHTML(pinnedTasks, termFolded().has(PINNED_FOLD)) +
      termGroupsHTML(tasks.filter(t => !t.pinned))
    : `<div class="panel"><div class="empty">
         no terminals. start one from the board, or attach to a running session.
       </div></div>`);

  // After the cards exist, since it is placed against one of them. A frame
  // later, so the measurement happens on the layout that was just written
  // rather than on the one being replaced.
  requestAnimationFrame(placeTabBridge);
  // Scrolling the list moves the attached card under a bridge that is not
  // moving with it. Wired here rather than at boot because the host is
  // replaced wholesale, and guarded so a refresh does not stack listeners the
  // way the terminal's paste handler did.
  if (!host.dataset.bridged) {
    host.dataset.bridged = "1";
    host.addEventListener("scroll", placeTabBridge, { passive: true });
  }
  wireTermDrag(host);
  // After the host is replaced, since `setHTML` above threw away the div these
  // buttons live in. Guarded because the strip draws an empty state with no
  // header at all, and the board's painter writes into whichever of its three
  // hosts it finds.
  if (typeof paintGroupSegs === "function") paintGroupSegs();
}

// The fold key for the pinned bucket.
//
// Held in the same store as the tree's folds, which is keyed by path. `*` is
// not a legal path segment anywhere the tree builds keys from, so this cannot
// collide with a group called `pinned`.
const PINNED_FOLD = "*pinned";

// The pinned ids in the order the strip last drew them.
//
// Needed because a FOLDED bucket draws no rows, so a drop onto it cannot read
// the order off the DOM the way an open one does. Without this the drop would
// send a one-id list and renumber that card to the front of a bucket it was
// meant to land at the back of.
let pinnedNow = [];

// The pinned bucket: a heading, and the rows in the order somebody put them.
//
// FLAT, AND NOT THROUGH THE TREE, which is the one thing that makes it a
// bucket. The tree exists to lift a shared `github/org/` prefix off a column
// of rows that all repeat it, and it is right for the rows underneath. It is
// wrong here: an order arranged by hand and then sorted into folders is not
// the order any more, and the whole promise of the bucket is that a row stays
// where it was put.
//
// So this is the opposite trade from the rows below, taken knowingly. These
// rows draw their FULL label, prefix and all, because there is no heading
// above them to carry it. That is more text per row and it is the price of the
// row not moving.
//
// The heading is drawn even when the bucket is empty, because an empty bucket
// still has to be a place you can drag the first row into. It says so.
//
// IT FOLDS, like every other heading in this strip. It was written as a label
// on the reasoning that the bucket is where you put things so you can see
// them, so a control that hides it only exists to undo the feature. That was
// wrong in the case that actually happens: eight pinned rows is most of the
// strip, and folding them is how you get at the ones underneath without
// unpinning anything. The count on the heading is what makes a folded group
// honest, and it is already there.
//
// A FOLDED BUCKET STILL TAKES A DROP. It is a fold, not a lid: a row dragged
// onto it goes on the end, which is the same thing dropping past the last row
// does when it is open.
function termBucketHTML(pinned, folded) {
  const rows = folded ? "" : pinned.map(t => termRow(t, false)).join("");
  const empty = folded ? "" :
    `<div class="empty bucketdrop">drag a terminal here to keep it</div>`;
  return `<div class="termbucket${folded ? " folded" : ""}" data-bucket="1">
      <button class="tgroup pinnedhead" onclick="toggleTermGroup('${PINNED_FOLD}')"
        title="${folded ? "show" : "hide"} the pinned terminals"
        ><span class="tcaret">${folded ? "&#9656;" : "&#9662;"}</span
        ><span class="tgname">pinned</span
        ><span class="tgcount">${pinned.length}</span></button>
      ${rows || empty}
    </div>`;
}

// Where a drag started, held because `dataTransfer` cannot be read during
// `dragover` and the drop indicator has to be drawn while the pointer moves.
let termDragID = "";

// Dragging a row into the bucket, or around inside it.
//
// TWO INTENTIONS, ONE GESTURE, told apart by where the row came from. A row
// dragged in from below is being pinned, and where it lands is also where it
// goes in the order. A row dragged within the bucket is only being moved. Both
// end in the same write, so they are one handler.
//
// The DOM is reordered on the drop and the order read back off it, rather than
// computed from the model and re-rendered. The list is rebuilt from the daemon
// on the next poll anyway, so computing it twice is two chances to disagree,
// and reading the rows is what the operator can actually see.
//
// Wired after every render, guarded on the host, because `renderTermList`
// replaces the whole strip and an unguarded listener per render is the leak
// the terminal's paste handler already taught this file about.
function wireTermDrag(host) {
  host.querySelectorAll(".card.tab").forEach(el => {
    el.draggable = true;
    el.ondragstart = e => {
      termDragID = el.dataset.id;
      e.dataTransfer.effectAllowed = "move";
      // Firefox refuses to start a drag with nothing set.
      e.dataTransfer.setData("text/plain", termDragID);
    };
    el.ondragend = () => {
      const aborted = !!termDragID;
      termDragID = "";
      host.querySelectorAll(".dropover").forEach(x => x.classList.remove("dropover"));
      // AN ABANDONED DRAG PUTS THE STRIP BACK. `dragover` moves the row as the
      // pointer travels, so letting go outside the bucket leaves the list
      // showing an order that was never written. The poll would correct it
      // within five seconds, which is five seconds of the strip claiming
      // something untrue about where things are.
      //
      // Told apart from a completed drag by the id: `ondrop` runs first and
      // clears it, so anything still set here never reached a drop.
      if (aborted) renderTermList();
    };
  });

  const bucket = host.querySelector(".termbucket");
  if (!bucket) return;

  bucket.ondragover = e => {
    if (!termDragID) return;
    // Without this the browser refuses the drop and the gesture ends in the
    // page's own "no" cursor.
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    bucket.classList.add("dropover");
    const over = e.target.closest(".card.tab");
    const moving = host.querySelector(`.card.tab[data-id="${termDragID}"]`);
    if (!moving || !over || over === moving) return;
    // Above or below the row under the pointer, decided by which half of it
    // the pointer is in. Snapping to one side would make the last position in
    // the bucket unreachable.
    const box = over.getBoundingClientRect();
    const after = e.clientY > (box.top + box.height / 2);
    bucket.insertBefore(moving, after ? over.nextSibling : over);
  };
  bucket.ondragleave = e => {
    if (!bucket.contains(e.relatedTarget)) bucket.classList.remove("dropover");
  };
  bucket.ondrop = async e => {
    e.preventDefault();
    bucket.classList.remove("dropover");
    const id = termDragID;
    termDragID = "";
    if (!id) return;
    const moving = host.querySelector(`.card.tab[data-id="${id}"]`);
    // Dropped on the bucket but never dragged over a row in it, which is what
    // dropping onto the heading or onto an empty bucket looks like. It goes on
    // the end.
    if (moving && !bucket.contains(moving)) bucket.appendChild(moving);
    // Off the DOM when the bucket is open, because the rows are what the
    // operator can see and computing the same answer twice is two chances to
    // disagree. A FOLDED bucket has no rows to read, so the last drawn order
    // stands and the dropped card goes on the end.
    const ids = bucket.classList.contains("folded")
      ? pinnedNow.filter(x => x !== id).concat(id)
      : [...bucket.querySelectorAll(".card.tab")].map(x => x.dataset.id);
    try {
      // Pinned FIRST, and only then ordered. A card that was dragged in from
      // below is not in the bucket as far as the daemon is concerned, so an
      // order written before the pin would be an order over a set it is not
      // in yet, and the next poll would drop it back out.
      await patchTask(id, { pinned: true });
      await api("/v1/tasks/pin-order", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ids })
      });
    } catch (err) {
      toast("that order did not stick", err.message);
    }
    refresh();
  };
}

// How long to keep trying to attach to a runner that is not there YET.
//
// Launching into its own window opens the window as soon as `/v1/launch`
// answers, and the runner is registered with the supervisor a moment later. The
// attach in between is refused, the socket closes without ever opening, and the
// close handler below tore the pane down: a window whose whole purpose is one
// terminal, showing "nothing attached".
//
// A socket that NEVER OPENED and one that closed after being open are different
// events and take different paths.
//
// The second case then splits again, and this is the part that matters when
// atrium restarts itself. A socket that was open and closed means either the
// RUNNER went away or the DAEMON did, and those want opposite behavior: a
// runner that exited should tear the pane down, and a daemon that is coming
// back should be waited for. Nothing in the close event says which.
//
// So the daemon is asked. `/v1/health` answering means the daemon is up and
// this was the runner; a failed request means the daemon is the thing missing.
// One extra request, only on a disconnect, and it is the difference between a
// terminal that reconnects itself after `restart_atrium` and one that clears
// itself two seconds before the daemon comes back.
const attachRetryFor = 5 * 60 * 1000;
const attachRetryEvery = 700;
// How long a refused attach stays silent before it says it is waiting.
//
// `/v1/launch` returns before the supervisor has registered the runner, and
// that gap is well under a second. Anything past this is a wait worth naming.
const attachRaceFor = 4000;
let attachSince = 0;
// Said once per outage rather than once per attempt. At 700ms a five minute
// wait is four hundred lines of the same sentence.
let attachSaidGone = false;

// Draw the terminal on the GPU rather than in the DOM.
//
// xterm has no renderer of its own beyond a fallback, and the fallback is the
// DOM: a `<span>` per styled run per row, rebuilt every frame, plus a
// generated stylesheet of 256 color rules. It is correct and it is enormously
// expensive. One board tab was holding a quarter of a CPU and had accumulated
// two and a half hours of CPU time doing nothing but drawing terminals.
//
// AFTER `term.open`, which is not optional: the addon needs a canvas in the
// document to get a context from, and loading it earlier fails silently and
// leaves you on the DOM renderer wondering why nothing changed.
//
// Three ways this can not happen, all of them ending on the DOM renderer,
// which is slow rather than broken:
//
//   - No WebGL at all: a remote session, a blocklisted driver, a machine with
//     no GPU worth using.
//   - The context is lost later. A driver reset or a laptop switching GPUs
//     does this, and the addon cannot recover, so it is disposed and xterm
//     falls back on its own.
//   - The addon is missing entirely, which is what a build with a stale
//     `vendor/` looks like.
//
// None of them is worth a message to the operator. A terminal that renders is
// the requirement; which path drew it is not.
function useWebgl(t) {
  if (typeof WebglAddon === "undefined") return;
  let addon;
  try {
    addon = new WebglAddon.WebglAddon();
  } catch (e) {
    console.warn("no webgl renderer, falling back to the DOM one:", e);
    return;
  }
  addon.onContextLoss(() => {
    console.warn("webgl context lost, falling back to the DOM renderer");
    addon.dispose();
  });
  try {
    t.loadAddon(addon);
  } catch (e) {
    console.warn("webgl renderer would not load:", e);
    addon.dispose();
  }
}

