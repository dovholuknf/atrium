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

  const pane = document.querySelector("#term-layout .term-pane");
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
  showMenu(e, [
    { label: "rename…", act: () => renameTask(id, t.display_title) },
    { label: "attach", act: () => attachTask(id) },
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
    t.pid > 0 ? { label: "terminate", danger: true, act: () => killById(id) } : null
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
function termPathOf(t) {
  const label = terminalLabel(t) || t.display_title || "";
  const cut = label.lastIndexOf(":");
  const where = cut > 0 ? label.slice(0, cut) : "";
  const leaf = cut > 0 ? label.slice(cut + 1) : label;
  return { segs: where.split("/").filter(Boolean), leaf: leaf || label };
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
  const shown = deep ? leaf : full;
  return `
    <div class="card tab ${termTask && t.id === termTask.id ? "on" : ""}"
         data-id="${t.id}"
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
            >${t.pinned ? "&#9733;" : "&#9734;"}</span>${runnerMark(t.runner)}<span
            class="tname" title="${esc(full)}">${esc(shown)}</span><span
            class="tshort" title="${esc(full)}"
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
  return termNodeHTML(termTree(list), 0, "", termFolded());
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
  return `<button class="tgroup" onclick="toggleTermGroup('${esc(path)}')"
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
  for (const kid of node.kids.values()) {
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

async function renderTermList() {
  // Cheap and idempotent, and it means the mode survives a reload without
  // finding a second place to call it from.
  applyTermList();
  let all = [];
  try { all = (await api("/v1/tasks")).tasks || []; } catch (e) { return; }

  // Terminals, and only terminals.
  //
  // A pinned card without one used to sit here offering `resume`, on the
  // reasoning that a fixture vanishing the moment it stops is the opposite of
  // a fixture. That reasoning was about the CARD, and this list is not a list
  // of cards: it is the switcher for the pane beside it, and a row that
  // cannot be switched to is a row that does nothing.
  //
  // Restarting a stopped session is what the stack and the board are for, and
  // both do it better, with the whole card in front of you.
  const tasks = all.filter(t => t.supervised);
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
  if (termTask && !tasks.some(t => t.id === termTask.id)) clearTermPane();

  if (sortByActivity) {
    // Anything waiting on a human first, then by how recently it moved.
    tasks.sort((a, b) => {
      const w = (isWaiting(b) ? 1 : 0) - (isWaiting(a) ? 1 : 0);
      if (w) return w;
      return (a.idle_seconds || 0) - (b.idle_seconds || 0);
    });
  }
  // Pinned above everything, in either sort. A fixture you have to hunt for
  // is not one, and this is the whole point of pinning it.
  tasks.sort((a, b) => (b.pinned ? 1 : 0) - (a.pinned ? 1 : 0));

  const host = document.getElementById("term-list");
  const toggle = `<div class="termhead">
      <button class="termsort" onclick="toggleTermSort()"
        title="newest activity first, and anything waiting on you above that">
        ${sortByActivity ? "sorted by activity" : "sorted by name"}</button>
      <span class="grow"></span>
      ${termListButtons()}
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
    ? toggle + termGroupsHTML(tasks)
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

