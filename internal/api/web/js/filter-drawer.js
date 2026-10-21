// THE FILTER DRAWER: one control on the stack, board and terminals tabs, three configurations (docs/backlog/ui/u-011-interview.md).
//
// A line under the title bar says what the tab is set to, holds the search box and carries a `filters` handle. A
// click on the rest of the line rolls a drawer down over the content: SHOW chips, SORT BY, GROUP. It is an overlay, so
// it never resizes the list or a terminal under it.
//
// THIS FILE OWNS THE SHAPE AND THE BEHAVIOUR, AND NOTHING ABOUT WHAT A TAB FILTERS. Each tab keeps painting its own
// pills into the rows named in FD_TABS (stack.js, board.js, terminal-list.js) and tells the line what to say with
// `fdStatus`. One component, so a rule changed here changes on all three.
//
// THE RULES, and why each is there:
//   - It starts closed, on every load and every tab switch. Whether it was open is not remembered.
//   - Only a click or a pull opens it. NO KEY DOES: a click that arrives with no pointer behind it (`detail` 0) is a key
//     or a script and is ignored. The way to the search box from the keyboard is ctrl-/, which leaves the drawer shut.
//   - A click inside it applies at once and leaves it open.
//   - A click outside closes it and is SWALLOWED, so it does not also open the card it landed on.
//   - Esc closes it and puts focus back where it was. In the search box Esc clears the text, then leaves the box.
//   - ctrl-/ does nothing while a terminal has focus, so it never takes a key from xterm.
//   - On a phone the drawer follows a finger dragged down from the line, and only from the line.
//   - When the whole row of controls fits on the line beside the search box, the drawer is not there: the panel sits on the
//     line in place of the summary, with no caret. It comes back when the row would wrap (`fdFitInline`).
const FD_TABS = {
  stack: {
    host: "fd-stack", q: "stack-q", match: "stack-match",
    placeholder: "name, path, runner or tag. #tag for one tag",
    rows: [["show", "stack-seg", "seg multi"], ["sort by", "stack-sort", "seg"], ["group", "stack-group", "seg groupseg"], ["label", "stack-label", "seg"]],
    repaint: () => paintStack()
  },
  board: {
    host: "board-bar", q: "board-q", match: "board-match",
    placeholder: "name, path, runner or tag. #tag for one tag",
    rows: [["show", "board-seg", "seg multi"], ["sort", "board-sort", "seg"], ["group", "board-group", "seg groupseg"], ["label", "board-label", "seg"]],
    repaint: () => repaintLists()
  },
  terms: {
    host: "fd-terms", q: "term-q", match: "term-match",
    placeholder: "name, path or tag. #tag for one tag",
    rows: [["show", "term-show", "seg multi"], ["sort by", "term-sort", "seg"], ["group", "term-drawer-group", "seg groupseg"], ["label", "term-label", "seg"]],
    repaint: () => renderTermList()
  }
};
FD_TABS.backlog = {
  host: "fd-backlog", q: "backlog-q", match: "backlog-match",
  placeholder: "id, title, dept or body. #dept for one dept",
  rows: [["show", "backlog-show", "seg multi"], ["priority", "backlog-prios", "seg multi"], ["dept", "backlog-depts", "seg multi"],
    ["sort by", "backlog-sort", "seg"], ["group", "backlog-group", "seg"]],
  repaint: () => paintBacklog()
};
const FD_NARROW = "(max-width: 520px)";
// How far a drag has to carry the drawer, as a fraction of its height, for it to stay open when let go.
const FD_SNAP = 1 / 3;
// THE ONE SWITCH for the terminals tab. true: the drawer is an accordion like stack and board, pushing the terminal down,
// with a real fit and a pty resize on open and on close. false: the drawer is an overlay over the terminal's top rows and
// the terminal keeps its row count (no fit, no resize), the path docs/backlog/ui/filter-drawer-strip-notes.md describes.
// A var so a test can flip it; nothing else reads it but `fdOverlay`.
var FD_TERMS_PUSH = true;
function fdOverlay(tab) { return tab === "terms" && !FD_TERMS_PUSH; }

const fdState = {};   // tab -> { open, prev, status }
let fdSwallow = 0;    // when a swallowed pointerdown happened, so its click is eaten and a stray flag times out
let fdDraggedAt = 0;  // when a drag last ended, so the click a lift can fire does not toggle it back

function fdHost(tab) { return document.getElementById(FD_TABS[tab].host); }
function fdEl(tab, sel) { const h = fdHost(tab); return h ? h.querySelector(sel) : null; }
function fdIsOpen(tab) { return !!(fdState[tab] && fdState[tab].open); }
function fdAnyOpen() { return Object.keys(FD_TABS).find(fdIsOpen) || ""; }
function fdSearchKey(tab) { return "atrium." + tab + ".q"; }

// The search text for a tab, lower cased, which is what every filter compares against.
function fdSearchText(tab) {
  const el = document.getElementById(FD_TABS[tab].q);
  return el ? String(el.value || "").toLowerCase() : "";
}

// Sets the text from code (a tag chip's `filterByTag`) and keeps it for the session like typing does.
function fdSetSearch(tab, text) {
  const el = document.getElementById(FD_TABS[tab].q);
  if (!el) return;
  el.value = text;
  try { sessionStorage.setItem(fdSearchKey(tab), text); } catch (e) {}
}

function fdMount(tab) {
  const cfg = FD_TABS[tab];
  const host = fdHost(tab);
  if (!host || host.dataset.mounted) return;
  host.dataset.mounted = "1";
  host.classList.add("fdrawer");
  host.dataset.tab = tab;
  // All three tabs are the strip (the header's colours, a chevron after the summary). Stack and board are an accordion
  // that pushes the content down; the terminals tab is too unless FD_TERMS_PUSH is off, which makes its panel an overlay
  // so the terminal keeps its row count while it is open.
  host.classList.add("fstrip");
  host.classList.toggle("foverlay", fdOverlay(tab));
  const btn = `<button type="button" class="filtersbtn" tabindex="-1" aria-expanded="false" aria-controls="${cfg.host}-panel" aria-label="filters"
        ><svg class="fchev" viewBox="0 0 20 20" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M4.5 7.5 10 13l5.5-5.5"/></svg></button>`;
  const qm = `<input type="search" class="fq" id="${cfg.q}" aria-label="search this list" placeholder="${esc(cfg.placeholder)}"
        spellcheck="false" autocomplete="off">
      <span class="matchcount" id="${cfg.match}"></span>`;
  // The board's groups fold per lane, so it is the one tab with a way to fold every lane at once.
  const foldAll = tab === "board"
    ? `<span class="foldall"><button type="button" tabindex="-1" id="board-collapse-all" data-tip="fold every group in every lane" onclick="foldAllGroups(true)">collapse all</button><button type="button" tabindex="-1" id="board-expand-all" data-tip="open every group in every lane" onclick="foldAllGroups(false)">expand all</button></span>`
    : "";
  host.innerHTML = `<div class="fline">
      <span class="fsum"></span>
      ${foldAll + btn + qm}
    </div>
    <div class="fwrap"><div class="fpanel" id="${cfg.host}-panel" tabindex="-1" inert role="group" aria-label="filters">
      ${cfg.rows.map(([label, id, cls]) => `<div class="frow"><span class="barlabel">${esc(label)}</span><div class="${cls}" id="${id}"></div></div>`).join("")}
      <button type="button" class="fhandle" aria-label="close the filters" data-tip="close the filters"><i></i></button>
    </div></div>`;
  fdState[tab] = { open: false, prev: null, status: null };

  const q = document.getElementById(cfg.q);
  try { q.value = sessionStorage.getItem(fdSearchKey(tab)) || ""; } catch (e) {}
  q.addEventListener("input", () => {
    try { sessionStorage.setItem(fdSearchKey(tab), q.value); } catch (e) {}
    cfg.repaint();
  });
  q.addEventListener("keydown", e => {
    if (e.key !== "Escape") return;
    if (fdIsOpen(tab)) return;   // the document handler closes the drawer first
    e.preventDefault();
    e.stopPropagation();
    if (q.value) { q.value = ""; q.dispatchEvent(new Event("input")); } else q.blur();
  });

  const line = host.querySelector(".fline");
  line.addEventListener("pointerdown", () => { fdState[tab].prev = document.activeElement; }, true);
  line.addEventListener("click", e => {
    if (e.target.closest(".fq, .foldall, .fpanel") || host.classList.contains("inline")) return;
    if (e.detail === 0) return;   // no key opens it
    if (Date.now() - fdDraggedAt < 400) return;
    fdSet(tab, !fdIsOpen(tab));
  });
  // Closing is open to the keyboard too: only opening is not (the line keeps its `detail` test).
  host.querySelector(".fhandle").addEventListener("click", () => fdSet(tab, false, true));
  fdWirePanel(tab, host.querySelector(".fpanel"));
  fdWireDrag(tab, host);
  if (typeof ResizeObserver === "function") new ResizeObserver(() => fdFitInline(tab)).observe(host);
}

// THE ROW ON THE LINE, WHEN IT FITS. The panel is moved onto the line, in place of the summary, and measured with its rows
// held to one line: if what it holds is no wider than the room it was given it stays there and the drawer is gone, and
// if not it goes back under the line. The measure is skipped while nothing it depends on (the line's width, the pills'
// words and counts) has changed, so a poll does not move the panel about. An open drawer is left alone until it is shut.
function fdFitInline(tab, force) {
  const host = fdHost(tab);
  const st = fdState[tab];
  if (!host || !st || st.open || !host.offsetParent) return;
  const line = host.querySelector(".fline");
  const panel = host.querySelector(".fpanel");
  const narrow = window.matchMedia(FD_NARROW).matches;
  const sig = line.clientWidth + "|" + narrow + "|" + panel.textContent;
  if (st.fit === sig && !force) return;
  st.fit = sig;
  const was = host.classList.contains("inline");
  let fits = false;
  if (!narrow) {
    host.classList.add("inline");
    line.querySelector(".fsum").after(panel);
    fits = panel.scrollWidth <= panel.clientWidth + 1;
  }
  panel.inert = !fits;
  if (!fits) {
    host.classList.remove("inline");
    host.querySelector(".fwrap").appendChild(panel);
  }
  if (fits !== was && tab === "terms" && !fdOverlay(tab)) fdRefitTerms(host);
}
window.addEventListener("resize", () => Object.keys(FD_TABS).forEach(t => fdFitInline(t)));

function fdSet(tab, open, restore) {
  const host = fdHost(tab);
  const st = fdState[tab];
  if (!host || !st || st.open === !!open) return;
  if (open && host.classList.contains("inline")) return;
  if (open) fdCloseAll(tab);
  st.open = !!open;
  const panel = host.querySelector(".fpanel");
  host.classList.toggle("foverlay", fdOverlay(tab));
  host.classList.toggle("open", st.open);
  host.classList.remove("fdragging");
  if (tab === "terms" && !fdOverlay(tab)) fdRefitTerms(host);
  panel.style.transform = "";
  panel.inert = !st.open;
  host.querySelector(".filtersbtn").setAttribute("aria-expanded", st.open ? "true" : "false");
  if (st.open) {
    // Into the drawer, so Tab walks its rows. Esc puts it back.
    panel.focus({ preventScroll: true });
  } else {
    const was = st.prev;
    st.prev = null;
    const inHost = document.activeElement && host.contains(document.activeElement);
    if (restore !== false && inHost) {
      const inSearch = document.activeElement === document.getElementById(FD_TABS[tab].q);
      if (!inSearch) {
        if (was && was !== document.body && document.contains(was)) was.focus({ preventScroll: true });
        else document.activeElement.blur();
      }
    }
  }
}

// The pane moves with the drawer, so the terminal needs a fit and the pty a resize. The ResizeObserver on the screen does
// most of it frame by frame; this is the explicit one, now and when the slide ends, so the last size is always sent.
function fdRefitTerms(host) {
  const again = () => { if (typeof onTermResize === "function") onTermResize(); };
  again();
  const wrap = host.querySelector(".fwrap");
  let done = false;
  const end = () => { if (done) return; done = true; wrap.removeEventListener("transitionend", end); again(); };
  wrap.addEventListener("transitionend", end);
  setTimeout(end, 260);
}

function fdCloseAll(except) {
  Object.keys(FD_TABS).forEach(t => { if (t !== except && fdIsOpen(t)) fdSet(t, false, false); });
}

// What the line says. `summary` is the settings in words, `match` the `12 of 40` text. Called by each tab's own painter, so the line is right whenever the list is.
function fdStatus(tab, s) {
  const host = fdHost(tab);
  if (!host) return;
  const st = fdState[tab] || (fdState[tab] = { open: false, prev: null, status: null });
  st.status = s;
  const sum = host.querySelector(".fsum");
  if (sum.textContent !== s.summary) sum.textContent = s.summary || "";
  const m = host.querySelector(".matchcount");
  const mt = s.match || "";
  if (m.textContent !== mt) m.textContent = mt;
  fdFitSummary(host);
  fdPaintLabel(tab);
  fdFitInline(tab);
}

// On a narrow screen the summary is dropped when it does not fit, so the search box keeps the line.
function fdFitSummary(host) {
  const sum = host.querySelector(".fsum");
  if (!sum) return;
  sum.hidden = false;
  if (window.matchMedia(FD_NARROW).matches && sum.scrollWidth > sum.clientWidth + 1) sum.hidden = true;
}
window.addEventListener("resize", () => Object.keys(FD_TABS).forEach(t => { const h = fdHost(t); if (h) fdFitSummary(h); }));

// ── the drawer's own keyboard ───────────────────────────────────────────────
//
// One tab stop per row (the lit chip, or the first), arrows along it. The pills are rebuilt by their painters on every
// click, so the stops are set again whenever a row's children change, and a pressed chip that was replaced under focus
// gets it back.
function fdWirePanel(tab, panel) {
  let lastFocus = null;
  const rove = () => {
    panel.querySelectorAll(".seg").forEach(seg => {
      const btns = [...seg.querySelectorAll("button")];
      const stop = btns.find(b => b.classList.contains("on")) || btns[0];
      btns.forEach(b => b.tabIndex = b === stop ? 0 : -1);
    });
    if (lastFocus && fdIsOpen(tab) && (!document.activeElement || document.activeElement === document.body || document.activeElement === panel)) {
      const seg = panel.querySelector("#" + CSS.escape(lastFocus.seg));
      const again = seg && seg.querySelectorAll("button")[lastFocus.i];
      if (again) again.focus({ preventScroll: true });
    }
  };
  new MutationObserver(rove).observe(panel, { childList: true, subtree: true });
  rove();
  panel.addEventListener("focusin", e => {
    const b = e.target.closest && e.target.closest(".seg button");
    if (!b) { lastFocus = null; return; }
    lastFocus = { seg: b.parentElement.id, i: [...b.parentElement.querySelectorAll("button")].indexOf(b) };
  });
  panel.addEventListener("keydown", e => {
    const keys = { ArrowLeft: -1, ArrowRight: 1, Home: -2, End: 2 };
    if (!(e.key in keys) || e.ctrlKey || e.altKey || e.metaKey) return;
    const b = e.target.closest && e.target.closest(".seg button");
    if (!b) return;
    const btns = [...b.parentElement.querySelectorAll("button")];
    const i = btns.indexOf(b);
    const k = keys[e.key];
    const to = k === -2 ? 0 : k === 2 ? btns.length - 1 : Math.max(0, Math.min(btns.length - 1, i + k));
    e.preventDefault();
    btns[to].focus();
  });
}

// ── closing: outside click, Esc ─────────────────────────────────────────────
//
// The pointerdown closes it and arms the swallow; the click that follows is stopped before anything below sees it.
// A dialog opened from the drawer (`+ new group`) is neither inside nor outside, so it is left alone.
//
// ONLY A PRIMARY PRESS THAT CAN STILL BECOME A CLICK ARMS IT. A right-click opens a menu and fires no click, a touch that
// turns into a scroll is cancelled by the browser, and a press that is carried away (a card drag) comes up elsewhere: each
// would leave the flag armed for the next real click, which is the first item of the menu or the second tap on a card.
let fdDown = null;   // where the swallowed press began
document.addEventListener("pointerdown", e => {
  const tab = fdAnyOpen();
  if (!tab) return;
  const t = e.target;
  if (!t || !t.closest) return;
  if (fdHost(tab).contains(t) || t.closest("dialog, #tip")) return;
  fdSwallow = e.button === 0 ? Date.now() : 0;
  fdDown = fdSwallow ? { x: e.clientX, y: e.clientY } : null;
  fdSet(tab, false, false);
}, true);
function fdDisarm() { fdSwallow = 0; fdDown = null; }
document.addEventListener("pointerup", e => {
  if (!fdSwallow || !fdDown) return;
  if (Math.hypot(e.clientX - fdDown.x, e.clientY - fdDown.y) > 8) fdDisarm();
}, true);
["pointercancel", "contextmenu", "dragstart"].forEach(t => document.addEventListener(t, fdDisarm, true));
document.addEventListener("click", e => {
  if (!fdSwallow) return;
  const fresh = Date.now() - fdSwallow < 1500;
  fdDisarm();
  if (!fresh) return;
  e.stopPropagation();
  e.preventDefault();
}, true);

document.addEventListener("keydown", e => {
  if (e.key === "Escape" && !e.defaultPrevented) {
    const tab = fdAnyOpen();
    if (!tab || document.querySelector("dialog[open]")) return;
    const a = document.activeElement;
    if (a && a !== document.body && !fdHost(tab).contains(a)) return;
    e.preventDefault();
    e.stopPropagation();
    fdSet(tab, false);
    return;
  }
  // ctrl-/ to the search box, drawer shut. Never from a terminal, and not with another modifier on it.
  if (e.ctrlKey && !e.altKey && !e.metaKey && !e.shiftKey && (e.key === "/" || e.code === "Slash")) {
    const a = document.activeElement;
    if (a && a.closest && a.closest(".xterm, #t-screen")) return;
    if (document.querySelector("dialog[open]")) return;
    const tab = Object.keys(FD_TABS).find(t => typeof isViewing === "function" && isViewing(t === "board" ? "board" : t));
    if (!tab) return;
    const q = document.getElementById(FD_TABS[tab].q);
    if (!q || !q.offsetParent) return;
    e.preventDefault();
    e.stopPropagation();
    q.focus();
    q.select();
  }
}, true);

// ── the phone's pull ────────────────────────────────────────────────────────
//
// A drag that starts on the line, and only there, brings the drawer down under the finger. Let go past a third of its
// height and it snaps open, short of that it snaps back. A swipe up on the open drawer closes it. The line has
// `overscroll-behavior: none` and `touch-action: none`, so the page's pull to refresh stays out of it.
function fdWireDrag(tab, host) {
  const line = host.querySelector(".fline");
  const panel = host.querySelector(".fpanel");
  const wrap = host.querySelector(".fwrap");
  const stripNow = () => !host.classList.contains("foverlay");
  let d = null;
  line.addEventListener("touchstart", e => {
    if (e.touches.length !== 1 || (e.target.closest && e.target.closest(".fq")) || fdIsOpen(tab)) { d = null; return; }
    const t = e.touches[0];
    d = { x: t.clientX, y: t.clientY, h: 0, on: false };
  }, { passive: true });
  line.addEventListener("touchmove", e => {
    if (!d) return;
    const t = e.touches[0];
    const dy = t.clientY - d.y, dx = t.clientX - d.x;
    if (!d.on) {
      if (Math.abs(dy) < 6 && Math.abs(dx) < 6) return;
      if (dy < 0 || Math.abs(dx) > Math.abs(dy)) { d = null; return; }
      d.on = true;
      fdCloseAll(tab);
      fdState[tab].prev = document.activeElement;
      host.classList.add("fdragging");
      d.h = stripNow() ? Math.min(panel.scrollHeight, parseFloat(getComputedStyle(panel).maxHeight) || Infinity) : panel.offsetHeight;
    }
    if (e.cancelable) e.preventDefault();
    const y = Math.max(0, Math.min(d.h, dy));
    d.y1 = y;
    if (stripNow()) wrap.style.gridTemplateRows = y + "px";
    else panel.style.transform = "translateY(" + (y - d.h) + "px)";
  }, { passive: false });
  const end = () => {
    if (!d || !d.on) { d = null; return; }
    const open = d.y1 >= d.h * FD_SNAP;
    d = null;
    fdDraggedAt = Date.now();
    host.classList.remove("fdragging");
    panel.style.transform = "";
    wrap.style.gridTemplateRows = "";
    if (open) fdSet(tab, true);
  };
  line.addEventListener("touchend", end, { passive: true });
  line.addEventListener("touchcancel", end, { passive: true });

  // A swipe up on the open drawer closes it, unless it is the drawer's own scrolling.
  let s = null;
  panel.addEventListener("touchstart", e => {
    const t = e.touches[0];
    s = { x: t.clientX, y: t.clientY, atEnd: panel.scrollTop + panel.clientHeight >= panel.scrollHeight - 1 };
  }, { passive: true });
  panel.addEventListener("touchend", e => {
    if (!s || !fdIsOpen(tab)) { s = null; return; }
    const t = e.changedTouches[0];
    const dy = t.clientY - s.y, dx = t.clientX - s.x;
    const was = s;
    s = null;
    if (dy < -30 && Math.abs(dy) > Math.abs(dx) && was.atEnd) fdSet(tab, false, false);
  }, { passive: true });
}

// A tab switch closes everything: the drawer is never remembered open.
function fdReset() { fdCloseAll(); }

// ── what the three tabs share ───────────────────────────────────────────────

// `#tag` means that tag exactly, so filtering to `#prs` does not also drag in a card whose path happens to contain the
// letters. Anything else is a plain substring across everything a card carries, tags included. `q` is lower case.
function cardMatchesText(t, q) {
  if (!q) return true;
  if (q.startsWith("#")) {
    const want = q.slice(1).trim();
    return want ? (t.tags || []).includes(want) : true;
  }
  return (t.display_title + " " + (t.alias || "") + " " + (t.worktree || "") + " " + (t.runner || "") + " " +
    (t.why || "") + " " + (t.ask || "") + " " +
    (t.tags || []).join(" ")).toLowerCase().includes(q);
}

// THE ATTACHED CARD IS NEVER FILTERED OUT. The card whose terminal is open stays on all three tabs whatever is typed in
// the search box and whatever chip is off: the stack's and the board's show chips, a board column's chip, the subagents
// rollup, and on the terminals tab the search, a folded parent and the kind chips. A filter that pulled it out would
// take the pane's own row away from under the operator. Compared by bare id, since a room-set change retags every id.
function fdAttached(t) {
  return typeof termTask !== "undefined" && !!termTask && !!t && bareId(t.id) === bareId(termTask.id);
}

// THE SUBAGENTS CHIP, off by default on all three tabs: a subagent is not a row (or a card) of its own, and its parent
// shows a `3 subagents` badge instead. A subagent that needs you still shows, and one whose parent is not on the board
// has nowhere to roll up to, so it stays a row. Returns `rolled`, the ids that are folded away, and `badge`, how many
// each parent that stays on screen is carrying (a rolled subagent's own subagents count towards the nearest parent
// that is still drawn). The attached card is never rolled (`fdAttached`). `rollable` is how many there are, which is the chip's count whether it is on or off.
function subagentRollup(cards, on) {
  const byBare = new Map(cards.map(t => [bareId(t.id), t]));
  const parentOf = t => {
    if (!nestsUnderLauncher(t)) return null;
    return launcherOf(t, cards, byBare);
  };
  // The attached card is not rollable: it stays a row of its own, and is not counted in its parent's badge.
  const rollable = new Set(cards.filter(t => parentOf(t) && !isWaiting(t) && !fdAttached(t)).map(t => t.id));
  const rolled = new Set();
  const badge = new Map();
  if (!on) {
    rollable.forEach(id => rolled.add(id));
    for (const t of cards) {
      if (!rolled.has(t.id)) continue;
      const seen = new Set([t]);
      let up = parentOf(t);
      while (up && rolled.has(up.id) && !seen.has(up)) { seen.add(up); up = parentOf(up); }
      if (up && !rolled.has(up.id)) badge.set(up.id, (badge.get(up.id) || 0) + 1);
    }
  }
  return { rolled, badge, rollable: rollable.size };
}
// The words on a parent: `1 subagent`, `3 subagents`.
function subsBadgeHTML(n) {
  return n ? `<span class="chip subs" data-tip="${n} subagent${n === 1 ? "" : "s"} rolled up under this one. turn on the subagents filter to list them"
    >${n} subagent${n === 1 ? "" : "s"}</span>` : "";
}

// How a tab's grouping reads in the line, in the words of the group pills (settings.js `paintGroupSegs`).
const FD_GROUP_WORDS = { project: "by project", window: "by pile", tag: "by tag", custom: "by group", recency: "by age", code: "by code" };
function groupWords(p) {
  if (!p || !p.on) return "ungrouped";
  const mode = usesCode(p) ? "code" : (p.mode || "project");
  return FD_GROUP_WORDS[mode] || "by " + mode;
}

// THE LABEL: what a card's main line says. `title` is the card's title, `alias` is its alias with the title falling back
// for a card that has none. Kept per tab beside the group (`groupingPrefs`), so each tab has its own.
const FD_LABELS = [
  ["title", "title", "each card's main line shows its title"],
  ["alias", "alias", "each card's main line shows its alias, or its title when it has none. the title moves to the tooltip"]
];
function fdLabelMode(tab) {
  return typeof groupingPrefs === "function" && groupingPrefs(tab).label === "alias" ? "alias" : "title";
}
function fdPaintLabel(tab) {
  const el = document.getElementById({ stack: "stack-label", board: "board-label", terms: "term-label" }[tab]);
  if (!el) return;
  const mode = fdLabelMode(tab);
  setHTML(el, FD_LABELS.map(([v, label, tip]) =>
    `<button class="${v === mode ? "on" : ""}" data-label="${v}" onclick="setCardLabel('${v}', '${tab}')" data-tip="${esc(tip)}">${esc(label)}</button>`).join(""));
}
function setCardLabel(mode, tab) {
  if (fdLabelMode(tab) === mode) return;
  setGrouping({ label: mode }, tab);
  fdPaintLabel(tab);
  FD_TABS[tab].repaint();
}
// `main` is the line a card leads with on `tab`, and `other` the title it displaced, which the row's tooltip carries.
function cardLead(t, tab) {
  const title = String((t && t.display_title) || "").trim();
  const alias = fdLabelMode(tab) === "alias" ? String((t && t.alias) || "").trim() : "";
  return alias ? { main: alias, other: title } : { main: title, other: "" };
}
function cardLeadTip(t, tab) {
  const l = cardLead(t, tab);
  return l.other ? ` data-tip="${esc(l.other)}"` : "";
}
prefLive("atrium.grouping.*", () => Object.keys(FD_TABS).forEach(t => { fdPaintLabel(t); FD_TABS[t].repaint(); }));

// One chip's state in a stored list, for the three tabs' show rows.
function fdChip(label, on, onclick, tip, v) {
  return `<button class="${on ? "on" : ""}" data-v="${esc(v)}" onclick="${onclick}" data-tip="${esc(tip)}"
    >${esc(label)} <i class="pillcount"></i></button>`;
}

Object.keys(FD_TABS).forEach(fdMount);
