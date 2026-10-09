// ── going to a card opens what hides it ─────────────────────────────────────
//
// A card can sit inside several folds at once: its board column, the status group in that column, the project group
// under it, the stack's group, the terminals list's headings down to its folder, the pinned bucket, and the row of the
// card that launched it. A fold hides the row completely, so going to a card that is inside one landed on a list with
// nothing selected in it and nowhere to look.
//
// `revealCard` opens EVERY fold the card is inside, nested ones included, and then scrolls it into view. It is called
// where a card is selected or jumped to (`attachTask`, `openTask`), which the switcher, a notification or toast, a card
// link and the peek all go through. It writes the same two stores a click on the fold writes (`atrium.folded` and
// `atrium.termfolded`), so what it opened stays open after a reload and in the other windows.
//
// Those stores record what is NOT as it comes. A group that comes open has an entry when it is shut, so revealing it
// removes the entry. The groups that come shut (the offline group, and `untagged` in custom mode) have an entry when
// they are open, so revealing those ADDS one. See the toggle listener in js/board.js.

// The card and every card above it, by who launched whom, nearest first.
function revealChain(t) {
  const out = [t];
  const seen = new Set([t.id]);
  for (let cur = t; cur && cur.spawned_by_id;) {
    const up = lastTasks.find(x => x.id === cur.spawned_by_id || bareId(x.id) === bareId(cur.spawned_by_id));
    if (!up || seen.has(up.id)) break;
    seen.add(up.id);
    out.push(up);
    cur = up;
  }
  return out;
}

// What is in the way of this card, as edits to the two stores. Returns true when anything changed.
function revealFolds(t) {
  const list = foldedColumns();
  const termSet = termFolded();
  let changed = false;
  const open = key => { const i = list.indexOf(key); if (i >= 0) { list.splice(i, 1); changed = true; } };
  const openInverted = key => { if (!list.includes(key)) { list.push(key); changed = true; } };
  const openTermFold = key => { if (termSet.delete(key)) changed = true; };
  const chain = revealChain(t);
  const top = chain[chain.length - 1];

  // The rows of the cards that launched it.
  chain.slice(1).forEach(up => open(termKidsKey(up)));

  const col = COLUMNS.find(c => c.statuses.includes(t.status));
  const bg = typeof grouper === "function" ? grouper("board") : null;
  if (col) {
    open(col.id);
    if (col.statuses.length > 1) open("group:" + t.status);
    if (t.offline) openInverted("offline:" + col.id);
  }
  if (t.offline) openInverted("offline:stack");
  const projects = (g, prefix, c) => {
    if (t.pinned && prefix === "proj:") { open("proj:pinned"); return; }
    let names = [];
    try { names = g ? g.of(c) || [] : []; } catch (e) { names = []; }
    names.filter(Boolean).forEach(name => {
      const shutByDefault = prefix === "proj:" && name === UNTAGGED && groupingPrefs(g.view).mode === "custom";
      if (shutByDefault) openInverted(prefix + name); else open(prefix + name);
    });
  };
  projects(bg, "proj:", t);
  projects(typeof grouper === "function" ? grouper("stack") : null, "proj:stack:", t);

  // The terminals list: its pinned bucket, then the headings above the row. A card launched by another is drawn under
  // its parent, so the headings are the top card's.
  if (t.pinned || top.pinned) openTermFold(PINNED_FOLD);
  const p = typeof groupingPrefs === "function" ? groupingPrefs("terms") : null;
  const tg = typeof grouper === "function" ? grouper("terms") : null;
  if (p && p.on) {
    if (!tg || (p.mode === "project" && !String(p.by || "").trim())) {
      const segs = termPathOf(top).segs;
      if (!segs.length) openTermFold("uncategorized");
      segs.forEach((_, i) => openTermFold(segs.slice(0, i + 1).join("/")));
    } else {
      let names = [];
      try { names = tg.of(top) || []; } catch (e) { names = []; }
      if (!names.length) names = [""];
      names.forEach(n => openTermFold("g:" + (String(n ?? "") || "uncategorized")));
    }
  }

  if (!changed) return false;
  try {
    localStorage.setItem("atrium.folded", JSON.stringify(list));
    localStorage.setItem(TERM_FOLDED, JSON.stringify([...termSet]));
  } catch (e) {}
  return true;
}

// Scrolled to once the lists have painted what was opened. The first row of the card that is drawn and on screen.
function revealScroll(id) {
  const sel = `[data-id="${CSS.escape(id)}"]`;
  const row = [...document.querySelectorAll("#board " + sel + ", #stack-list " + sel + ", #term-list " + sel)]
    .find(el => el.offsetParent !== null);
  if (row) row.scrollIntoView({ block: "nearest" });
}

function revealCard(id) {
  const t = typeof lastTasks !== "undefined" && lastTasks.find(x => x.id === id);
  if (!t) return false;
  let changed = false;
  try { changed = revealFolds(t); } catch (e) { return false; }
  if (changed && typeof repaintLists === "function") repaintLists();
  // Two frames: the repaint of the list may itself wait a frame, and the row is not there to scroll to until it has.
  requestAnimationFrame(() => requestAnimationFrame(() => revealScroll(id)));
  return changed;
}
