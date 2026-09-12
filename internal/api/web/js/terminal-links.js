// ── clicking a path in the terminal ─────────────────────
//
// A path that scrolled past is a path you retype. The terminal knows where it
// is and the daemon knows what the card's directory holds, so between them a
// filename can be a link, and clicking it opens the file.
//
// THE HARD PART IS NOT UNDERLINING EVERYTHING. A terminal is full of things
// shaped like a filename and not one: `v2.1.263`, `zrok.io`, `foo.bar()`,
// `Opus 5`. Every rule that separates them by looking is wrong somewhere, and
// wrong here means a page of noise with a line under half the words.
//
// So nothing here decides. It finds candidates and asks `files/probe`, which
// answers which of them exist inside the card, and the rule becomes "it is a
// link if it is a file". The answer is CACHED PER CARD, hit and miss alike,
// because the same word turns up on a hundred lines and the point of asking
// is that it is asked once.
//
// Where a click goes is the other half, and it is not `files/open`. That
// starts an editor ON THE DAEMON'S MACHINE, which over a share is a window on
// a screen nobody is sitting in front of. These open atrium's own viewer,
// which opens where the click happened, and that is the whole reason the
// board is worth using from another machine.

// What the daemon has already answered, for one card. A miss is remembered as
// firmly as a hit: `the` is not a file, and it should be asked about once
// rather than once per line it appears on.
let probeCache = { id: "", known: new Map() };

// The cache for a card, emptied if the card changed. Switching cards changes
// which directory a word is measured against, so nothing carries over.
function probeKnown(id) {
  if (probeCache.id !== id) probeCache = { id, known: new Map() };
  return probeCache.known;
}

// The daemon's own bound, repeated here so a wide line is cut before it is
// sent rather than silently truncated on arrival.
const probeBatch = 64;

// How far a logical line may be chased across wrapped rows. A path is a
// reason to join rows together; a screenful of unbroken output is not.
const probeWrapRows = 64;

function useFileLinks(t, task) {
  if (!task || !task.worktree) return;
  const id = task.id;
  try {
    t.registerLinkProvider({
      provideLinks: (y, done) => provideFileLinks(t, id, y, done)
    });
  } catch (e) {
    // Not worth a word to the operator. What they lose is an underline.
    console.warn("path links would not register:", e);
  }
}

// Every path on one row, once the daemon has agreed they are paths.
//
// xterm asks per row and asks on hover, which is what makes a batch the right
// shape: the answer for the whole row is fetched in one request the first time
// the pointer crosses it, and never again.
async function provideFileLinks(t, id, y, done) {
  // The pane may have moved to another card while this was in flight, and a
  // link that opens a file from the previous card is worse than no link.
  if (!termTask || termTask.id !== id) { done(undefined); return; }
  const line = logicalLine(t, y);
  if (!line) { done(undefined); return; }

  const known = probeKnown(id);
  const spans = [], ask = [];
  for (const c of fileCandidates(line.text)) {
    const start = cellAt(line, c.start), end = cellAt(line, c.end - 1);
    // Only what is on the row being asked about. The line is joined across
    // wraps so a path broken over two rows is still whole, but a joined line
    // can be thousands of characters and probing all of it would ask about
    // words nobody is pointing at.
    if (y < start.y || y > end.y) continue;
    spans.push({ token: c.token, range: { start, end } });
    if (!known.has(c.token) && !ask.includes(c.token)) ask.push(c.token);
  }
  if (ask.length) await askProbe(id, ask);
  if (!termTask || termTask.id !== id) { done(undefined); return; }

  const links = [];
  for (const s of spans) {
    const hit = probeKnown(id).get(s.token);
    if (hit) links.push(fileLink(id, s.range, hit));
  }
  done(links.length ? links : undefined);
}

// Asks the daemon, and remembers both answers.
//
// A failure is silent and cached as nothing, so the terminal behaves exactly
// as it did before any of this existed. A decoration that cannot be drawn is
// not something to interrupt somebody about.
async function askProbe(id, paths) {
  const batch = paths.slice(0, probeBatch);
  let found = [];
  try {
    const r = await api(`/v1/tasks/${id}/files/probe`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ paths: batch })
    });
    found = r.found || [];
  } catch (e) {
    return;
  }
  const hits = new Map(found.map(h => [h.path, h]));
  const known = probeKnown(id);
  // EVERY path asked about is written down, not just the ones that came back.
  // Remembering only the hits means every word that is not a file is asked
  // about again on the next hover, which is most of the words.
  for (const p of batch) known.set(p, hits.get(p) || null);
}

// One link, with somewhere to go and something to say on the way.
function fileLink(id, range, hit) {
  return {
    range,
    text: hit.rel,
    activate: (ev) => {
      ev.preventDefault();
      hideTip();
      openFromTerminal(id, hit);
    },
    hover: (ev) => tipSoon(() => placeTip(pointerAnchor(ev), hit.dir
      ? hit.rel + "/\nopens the file browser here"
      : hit.rel + "  " + bytes(hit.size) + "\nopens in atrium's own editor, in this browser")),
    leave: () => hideTip()
  };
}

// Where a clicked path opens, WHICH IS HERE.
//
// `files/open` is deliberately not reachable from this. It runs a command on
// the daemon's machine, so over a share it opens a window beside the agent
// and not beside the person who clicked. The text editor in this page and the
// file browser in this pane are the two things that open where you are.
function openFromTerminal(id, hit) {
  if (!termTask || termTask.id !== id) return;
  // THE DRAWER FIRST, for a file as well as for a directory.
  //
  // The editor is a panel INSIDE the drawer, so opening it while the drawer
  // was hidden put the file on screen nowhere: it was read, the box was
  // filled in, and nothing appeared. It stayed invisible until something else
  // opened the drawer, and then a file clicked minutes earlier turned up. So
  // the click read as doing nothing and the next click read as opening the
  // wrong file.
  setTermFiles(true);
  if (hit.dir) {
    loadFiles(hit.rel, termFilesCtx());
    return;
  }
  // The directory the file is in, drawn behind the editor. Closing the editor
  // then leaves the drawer somewhere related to what was clicked, rather than
  // wherever it happened to be last.
  const cut = hit.rel.lastIndexOf("/");
  loadFiles(cut > 0 ? hit.rel.slice(0, cut) : "", termFilesCtx());
  openEditor(id, hit.rel);
}

// placeTip measures whatever it is explaining, and a link has nothing to
// measure: the terminal is drawn on a canvas, so there is no element under the
// underline. The pointer stands in for one.
function pointerAnchor(ev) {
  const x = ev.clientX, y = ev.clientY;
  return {
    getBoundingClientRect: () => ({
      left: x, right: x, top: y - 8, bottom: y + 8, width: 0, height: 16
    })
  };
}

// One logical line, joined back together across the rows it wrapped onto.
//
// A long path is exactly the thing that wraps, and half of one probes as
// nothing. Rows are recorded with the offset each starts at, so a position in
// the joined text can be turned back into a cell.
//
// `translateToString(false)` is asked for without trimming ON PURPOSE: it pads
// every row out to the terminal's width, so an offset in the joined text maps
// to a column by arithmetic, and a token continued on the next row is joined
// with nothing between it.
function logicalLine(t, y) {
  const buf = t.buffer.active;
  let top = y;
  for (let n = 0; n < probeWrapRows && top > 1; n++) {
    const l = buf.getLine(top - 1);
    if (!l || !l.isWrapped) break;
    top--;
  }
  const rows = [];
  let text = "";
  for (let at = top; at < top + probeWrapRows; at++) {
    const l = buf.getLine(at - 1);
    if (!l) break;
    if (at !== top && !l.isWrapped) break;
    const s = l.translateToString(false);
    rows.push({ y: at, at: text.length, len: s.length });
    text += s;
  }
  return rows.length ? { rows, text } : null;
}

// An offset in the joined text, as the cell it is in. xterm counts columns
// from one and both ends of a range are inclusive.
function cellAt(line, off) {
  for (const r of line.rows) {
    if (off < r.at + r.len) return { x: off - r.at + 1, y: r.y };
  }
  const last = line.rows[line.rows.length - 1];
  return { x: Math.max(1, last.len), y: last.y };
}

// Every word on a line that is worth asking the daemon about.
//
// Tokens are cut at whitespace and at the brackets and quotes that wrap a path
// in prose, in a shell line or in a stack trace. What survives is TRIMMED
// rather than rejected, because the characters around a path are not part of
// it: `"internal/api/api.go",` is a path with three characters of punctuation
// stuck to it, and probing it whole finds nothing.
function fileCandidates(text) {
  const out = [];
  const re = /[^\s"'`()\[\]{}<>,;|]+/g;
  let m;
  while ((m = re.exec(text))) {
    const lead = (m[0].match(/^[('"`\[{<*]+/) || [""])[0].length;
    const token = trimCandidate(m[0].slice(lead));
    if (!token || !worthProbing(token)) continue;
    out.push({ token, start: m.index + lead, end: m.index + lead + token.length });
  }
  return out;
}

// The punctuation stuck to the end of a path, taken off.
function trimCandidate(tok) {
  tok = tok.replace(/[)'"`\]}>]+$/, "");
  // `internal/api/api.go:248:1`, which is how a compiler, a vet run and a test
  // failure all say where they are. The line number is not part of the name,
  // and leaving it on means none of those ever link.
  //
  // The TRAILING COLON is matched here rather than left to the sentence
  // punctuation below, because the two cannot be applied in the other order:
  // `api.go:248:1:` ends in a colon, so stripping punctuation first leaves
  // `api.go:248:1` and the line number is then past its last chance to go.
  tok = tok.replace(/:\d+(:\d+)?:?$/, "");
  // Sentence punctuation last, since a full stop can follow a line number.
  return tok.replace(/[.,:;!?]+$/, "");
}

// A cheap refusal before the daemon is asked. NOT a judgement about what looks
// like a path: that is the daemon's job and the reason it has one. This only
// drops what could not be a file in this card under any reading, so that a
// line of English costs one small request instead of one per word.
function worthProbing(tok) {
  if (tok.length < 2 || tok.length > 512) return false;
  // A flag.
  if (tok[0] === "-") return false;
  // A URL. Its host resembles a filename and is not one, which is most of
  // what this endpoint exists to refuse, but there is no need to ask.
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(tok)) return false;
  // A version, a duration, a hash, a count. Nothing to open.
  if (!/[a-zA-Z_]/.test(tok)) return false;
  // Characters a path cannot hold on either platform.
  if (/[*?<>|]/.test(tok)) return false;
  return true;
}
// ── finding something in the scrollback ─────────────────────────────────────
//
// xterm's own search addon rather than anything written here. It walks the
// terminal BUFFER, which is the only place the text exists: the WebGL renderer
// draws to a canvas, so nothing on screen is in the DOM and the browser's
// ctrl-f has nothing to match. Decorations paint every other hit down the
// scrollbar, which is what makes "17 of 240" mean something.
let termSearch = null;
let findCase = false, findRe = false;

function useSearch(t) {
  termSearch = null;
  // Loudly, because everything downstream of this degrades to "finds nothing"
  // rather than to an error. The vendored bundle sets `self.SearchAddon`, so
  // an undefined global means the script tag did not load, which is a 404 or a
  // build that dropped the file, not a search problem.
  if (typeof SearchAddon === "undefined") {
    console.error("the search addon did not load. /vendor/xterm-addon-search.js " +
      "defines SearchAddon and nothing did. ctrl-f will find nothing.");
    return;
  }
  try {
    termSearch = new SearchAddon.SearchAddon();
    t.loadAddon(termSearch);
    // The count comes back on an event rather than from the call, because the
    // addon searches lazily and a buffer of two hundred thousand lines is not
    // walked before it has to be.
    if (termSearch.onDidChangeResults) {
      termSearch.onDidChangeResults(r => paintFindCount(r));
    }
  } catch (e) {
    console.warn("the search addon would not load:", e);
    termSearch = null;
  }
}

// ── a URL in the output, clickable ──────────────────────
//
// The board prints its own address at people and they could not click it. Over
// a share the address is the one thing you most want out of the terminal and
// the least want to retype, so it was copied by eye into a bar.
//
// xterm's own addon rather than a fourth matcher here. Its regular expression
// is the one part of this nobody should be writing twice, and the file-path
// provider beside it is deliberately the opposite kind of thing: that one asks
// the daemon because looking cannot tell a filename from a version string,
// while a URL announces itself with a scheme and needs no permission to
// recognise.
//
// REGISTERED FIRST, which is the contract between the two. xterm collects
// every provider's answer for a row and then throws away links that overlap
// something an EARLIER provider claimed, so registration order decides who
// owns a run of text both matched. Reversing it lets the path matcher claim
// the inside of a URL, and the failure is a link that opens the wrong thing
// rather than a link that is missing.
function useWebLinks(t) {
  // Loudly, for the same reason the search addon is: what a missing bundle
  // looks like is a URL that is simply not a link, which is also what this
  // feature looked like before it existed. One sentence tells the two apart.
  if (typeof WebLinksAddon === "undefined") {
    console.error("the web links addon did not load. /vendor/xterm-addon-web-links.js " +
      "defines WebLinksAddon and nothing did. URLs in the terminal will not be clickable.");
    return;
  }
  try {
    t.loadAddon(new WebLinksAddon.WebLinksAddon(openTermURL));
  } catch (e) {
    console.warn("the web links addon would not load:", e);
  }
}

// A URL opens in a new tab, and that is all it does.
//
// NOT atrium's file viewer, which is where a clicked PATH goes. A URL is not a
// file in the card and the daemon has no business being asked about it.
//
// `noreferrer` because the board can be published, and the address of a
// published board is not something to hand to whatever an agent printed a link
// to. It implies `noopener`, and `noopener` is named as well so that reading
// the line does not require knowing that.
function openTermURL(ev, uri) {
  ev.preventDefault();
  const a = document.createElement("a");
  a.href = uri;
  a.target = "_blank";
  a.rel = "noreferrer noopener";
  // Attached before it is clicked. A detached anchor is ignored by some
  // browsers, and the failure is a click that does nothing at all.
  document.body.appendChild(a);
  a.click();
  a.remove();
}

// The colours are the theme's. A hit highlighted in board-blue on a dark green
// terminal is the same mistake the rest of the chrome just stopped making.
//
// `incremental` is what TYPING wants, not what stepping wants, and it was the
// wrong way round. It means "search again from the start of the current match",
// which is right while the word is still being typed, because it keeps the
// first hit from running away as the pattern gets longer. Applied to Enter it
// does the opposite of stepping: the search restarts at the match you are
// already on and finds it again, so pressing next repeatedly never leaves the
// first hit.
function findOpts(typing) {
  const th = themeFor(termTask);
  return {
    caseSensitive: findCase,
    regex: findRe,
    decorations: {
      matchBackground: th.selectionBackground || "#173A57",
      matchBorder: th.cursor || "#00E3B0",
      matchOverviewRuler: th.cursor || "#00E3B0",
      activeMatchBackground: th.cursor || "#00E3B0",
      activeMatchBorder: th.cursor || "#00E3B0",
      activeMatchColorOverviewRuler: th.cursor || "#00E3B0"
    },
    incremental: !!typing
  };
}

function openFind() {
  const bar = document.getElementById("t-find");
  const q = document.getElementById("t-find-q");
  if (!bar || !q) return;
  bar.hidden = false;
  // Selected rather than cleared, so pressing ctrl-f again and typing replaces
  // the last search, and pressing it to look at the old one still works.
  q.focus();
  q.select();
  if (q.value) runFind(false);
}

function closeFind() {
  const bar = document.getElementById("t-find");
  if (bar) bar.hidden = true;
  // THE SEARCH HAS TO BE FORGOTTEN, not just undecorated, and this is the
  // whole of the terminal-jumps-to-the-bottom bug.
  //
  // The addon caches the last term and re-runs the search on EVERY chunk of
  // output: `onWriteParsed` schedules `findPrevious` two hundred milliseconds
  // later. Re-selecting a match calls the public `scrollLines` to bring it into
  // view, so a search somebody ran once kept dragging the viewport to the same
  // place forever, every time a runner printed and on every re-render a focus
  // or a blur caused.
  //
  // It looked like a focus bug for hours. It is not: focus merely re-renders,
  // and the re-render is what re-selected the stale match. Nothing in atrium
  // scrolls, and both of xterm's own scroll-on-input paths are off.
  //
  // `clearDecorations()` with no argument drops `_cachedSearchTerm`, which is
  // what stops the rescheduling. The argument means KEEP the term, so do not
  // pass one here however tempting the name looks.
  if (termSearch) {
    termSearch.clearDecorations();
    if (termSearch.clearActiveDecoration) termSearch.clearActiveDecoration();
  }
  // And the selection with it, since a selection left on a match is the other
  // half of what makes the old result look live.
  if (term) term.clearSelection();
  // Back to the terminal, or the next thing typed goes into a box nobody can
  // see. WITHOUT SCROLLING: closing the find bar used to jump to the bottom,
  // so searching for something above the fold and pressing escape lost the
  // result you had just found. See `focusTerm`.
  focusTerm();
}

// `step` means move to another hit. Typing searches from where you are without
// moving on, so the first match found while typing does not run away as the
// word gets longer.
function runFind(step, back) {
  const q = document.getElementById("t-find-q");
  if (!q) return;
  // SAID, not returned silently. Without the addon there is no search at all,
  // and the box still accepts typing and still looks like it is working, which
  // is indistinguishable from a search that matches nothing. That ambiguity
  // cost an evening: "it finds nothing" and "it is not searching" look the
  // same from the outside and have nothing in common as bugs.
  if (!termSearch) {
    paintFindCount(null, "no search addon loaded");
    return;
  }
  const text = q.value;
  if (!text) {
    termSearch.clearDecorations();
    paintFindCount(null);
    return;
  }
  try {
    const found = step && back
      ? termSearch.findPrevious(text, findOpts(false))
      : termSearch.findNext(text, findOpts(!step));
    // The addon answers false for "no match" and reports the count on its own
    // event. Both are needed: the event does not fire when the search found
    // nothing at all, so a miss would otherwise leave the previous count on
    // screen and read as a hit.
    if (!found) paintFindCount(null, "no matches");
  } catch (e) {
    // A half-typed regular expression is the ordinary case here, not an error
    // worth a dialog: `(foo` is what `(foo|bar)` looks like on the way in.
    //
    // ANYTHING ELSE IS SAID OUT LOUD. This catch used to swallow every failure
    // into an empty count, so a search that threw on its first call looked
    // exactly like a search that matched nothing, forever, with nothing in the
    // console either.
    if (findRe) {
      paintFindCount(null, "bad pattern");
      return;
    }
    console.error("the terminal search threw:", e);
    paintFindCount(null, "search failed, see the console");
  }
}

function toggleFind(which) {
  if (which === "case") findCase = !findCase;
  if (which === "re") findRe = !findRe;
  const el = document.getElementById(which === "case" ? "t-find-case" : "t-find-re");
  if (el) el.classList.toggle("on", which === "case" ? findCase : findRe);
  runFind(false);
}

function paintFindCount(r, msg) {
  const el = document.getElementById("t-find-n");
  if (!el) return;
  if (msg) { el.textContent = msg; return; }
  if (!r || !r.resultCount) { el.textContent = ""; return; }
  // `resultIndex` is -1 until something is selected, which is the state while
  // a search is still running over a long buffer.
  el.textContent = (r.resultIndex >= 0 ? (r.resultIndex + 1) + " of " : "") + r.resultCount;
}

// Ctrl-F when the terminal does not have focus.
//
// xterm's key handler only sees keystrokes aimed at the terminal, and the
// common case is exactly the other one: you have just clicked a card in the
// switcher, or scrolled with the mouse, and the focus is anywhere but the
// grid. Without this, find worked only if you had typed into the terminal
// first, which is indistinguishable from not working.
addEventListener("keydown", e => {
  if (!(e.ctrlKey || e.metaKey) || e.code !== "KeyF") return;
  const terms = document.getElementById("terms");
  if (!term || !terms || terms.hidden) return;
  // Somebody typing in a real text box wants their browser's find, or wants
  // nothing. The one exception is the find box itself, where ctrl-f means
  // "select what is in here and start over".
  const el = document.activeElement;
  const tag = el ? el.tagName : "";
  if ((tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") && el.id !== "t-find-q") return;
  e.preventDefault();
  openFind();
});

// Reload when the daemon starts serving a board this page is not.
//
// A restart replaces what is served and touches nothing already running, so a
// window open since before a fix is still executing the old JavaScript. On the
// board that is a reload away and obvious. On a POPPED-OUT TERMINAL it is
// neither: those stay open for days, and the symptom is a fix that works in
// every window opened afterwards and not in the one you are looking at.
//
// The build id is a hash of the board the binary carries, so this fires when
// the page and the daemon genuinely differ and never on an ordinary restart of
// the same build.
//
// Reloading a popped-out terminal costs the scrollback and nothing else: the
// card id is in the URL, the runner is untouched, and it reattaches on load.
let boardBuild = "";
function checkBuild(build) {
  if (!build) return;
  if (!boardBuild) { boardBuild = build; return; }
  if (build === boardBuild) return;
  boardBuild = build;
  // This is THE reload that matters for coming back to where you were, so
  // where you are is written down here rather than trusted to have been
  // written earlier.
  rememberWhereYouAre();
  location.reload();
}

// Is the daemon there at all.
//
// The one question that separates "the runner exited" from "atrium is
// restarting", and neither the websocket close code nor its reason can answer
// it: a daemon shutting down and a runner quitting both close normally.
async function daemonAnswers() {
  try {
    const res = await fetch("/v1/health", { cache: "no-store" });
    if (!res.ok) return false;
    // The reconnect path is the one that matters most for this: a popped-out
    // window asks here on every retry, so a daemon that came back on a new
    // build is noticed at the moment it comes back rather than on the next
    // five second poll.
    try { checkBuild((await res.clone().json()).build); } catch (e) {}
    return true;
  } catch (e) {
    return false;
  }
}

// What the pane says while it is not connected to anything.
//
// One function, called from every path that waits, so the message cannot be
// left up by one of them and cleared by another.
function termWait(say) {
  const box = document.getElementById("t-wait");
  const line = document.getElementById("t-wait-say");
  if (!box || !line) return;
  if (!say) { box.hidden = true; return; }
  line.textContent = say;
  box.hidden = false;
}

// The session's name, for a sentence about it. Falls back to something rather
// than to nothing: "reconnecting to" with a blank after it reads as truncated.
function waitName(t) {
  return (t && (shortLabel(t) || t.display_title)) || "the session";
}

// WHICH OF THE ATTACHED CARD'S TWO TERMINALS THE PANE IS SHOWING.
//
// `runner` or `shell`. Reset to `runner` whenever the pane attaches to a
// different card, because a shell belongs to the card it was opened in and
// carrying the choice across would attach to the wrong thing or to nothing.
let termKind = "runner";
// Which card `termKind` is an answer about. See `openTerm`.
let termKindFor = "";

// The toggle, shown only when there is a choice to make.
//
// A card with no runner atrium owns has no terminal here at all, so offering
// `agent` and `shell` on it would be two buttons where one leads nowhere. The
// pair appears with the runner, which is also the only case where a wedged
// agent is a thing that can happen.
function paintTermKind() {
  const wrap = document.getElementById("t-kind");
  if (!wrap) return;
  wrap.hidden = !(termTask && termTask.supervised);
  const agent = document.getElementById("t-kind-agent");
  const shell = document.getElementById("t-kind-shell");
  if (agent) agent.classList.toggle("on", termKind !== "shell");
  if (shell) shell.classList.toggle("on", termKind === "shell");
}

// Switch the pane between the runner's terminal and the card's shell.
//
// ASKING FOR THE SHELL IS WHAT CREATES IT. The board does not track which
// cards have one: a shell outlives a reload and is closed by an idle sweep the
// browser knows nothing about, so anything the page remembered would be wrong
// within the hour. `POST .../shell` is idempotent, so pressing this twice is
// the same as pressing it once.
async function showTerminal(kind) {
  if (!termTask) return;
  if (kind === termKind) return;
  if (kind === "shell") {
    try {
      await api(`/v1/tasks/${termTask.id}/shell`, { method: "POST" });
    } catch (e) {
      // The failures here are readable ones: the directory is gone, the
      // configured shell is not installed. This is the whole reason opening a
      // shell is a POST rather than something the websocket does on the way
      // up: a close code cannot say either of those.
      toast("no shell here", e.message);
      return;
    }
  }
  const task = termTask;
  termKind = kind;
  termKindFor = task.id;
  paintTermKind();
  // Tear the socket down and dial the other terminal. Not two sockets: one
  // pane shows one thing, and both writing into one screen is the situation
  // popping out exists to avoid.
  closeTerm(true);
  openTerm(task);
}

function connectTerm(taskID) {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  // ONE SOCKET PER PANE, ALWAYS.
  //
  // Reconnecting is scheduled from several branches of `onclose`, and there is
  // nothing stopping this being entered while an earlier socket is still open
  // or still connecting. The old one is not closed by being replaced: it keeps
  // receiving, keeps writing into the same terminal, and every byte from the
  // runner lands twice. It reads as a broken keyboard rather than as two
  // sockets, because the doubling shows up in what you type.
  //
  // Closed rather than trusted to have gone. `close()` on an already-closed
  // socket does nothing.
  if (termSock) { try { termSock.close(); } catch (e) {} }
  // Absent means the runner, which is what the daemon assumes too, so the
  // parameter is only ever added rather than always sent. That keeps the
  // ordinary URL identical to what it was before shells existed.
  const kind = termKind === "shell" ? "?kind=shell" : "";
  termSock = new WebSocket(`${proto}//${location.host}/v1/tasks/${taskID}/attach${kind}`);
  termSock.binaryType = "arraybuffer";
  // Per socket, not per pane: a reconnect that succeeds must not leave the
  // next disconnect thinking it never opened.
  let opened = false;
  if (!attachSince) attachSince = Date.now();

  termSock.onopen = () => {
    opened = true;
    attachSince = 0;
    // Back. The announcement has been spent, so the next close is judged on
    // its own: leaving it armed would make a session ended half an hour from
    // now look like the tail of this restart.
    restartAt = 0;
    termWait("");
    if (attachSaidGone && term) {
      term.write("\r\n\x1b[38;5;79m[atrium] reconnected\x1b[0m\r\n");
    }
    attachSaidGone = false;
    sendResize();
    focusTerm();
    // THE BRIDGE, once there is a terminal for it to reach.
    //
    // It had three callers, the strip render, a resize and the pane teardown,
    // and none of them was this. `placeTabBridge` gives up when `term` is
    // null, and on the restore path the strip renders before the socket
    // opens, so the one call that fired measured a terminal that did not
    // exist yet. The row drew itself attached and the span across the gutter
    // was missing, which the stylesheet's own comment says reads as broken.
    //
    // A frame later for the same reason the strip defers it: the pane is
    // still settling into the layout that was just written.
    requestAnimationFrame(placeTabBridge);
  };
  const sock = termSock;
  // GUARDED BY IDENTITY, the same way `onclose` is.
  //
  // A socket that has stopped being the current one can still deliver a
  // message: closing is asynchronous, and anything already in flight arrives
  // afterwards. Writing it would put a runner's output into a terminal that is
  // now showing something else, or into the same one twice.
  termSock.onmessage = e => {
    if (termSock !== sock || !term) return;
    // The scroll goes in the WRITE CALLBACK, not after the call. `term.write`
    // is asynchronous: it queues the bytes and parses them later, so scrolling
    // on the next line scrolls a buffer that has not grown yet, which is the
    // same off-by-one-echo mistake `sendInput` was making on its own.
    noteScrollAct("output");
    if (typeof e.data === "string") { term.write(e.data, followScroll); return; }
    term.write(new Uint8Array(e.data), followScroll);
  };
  termSock.onclose = async ev => {
    if (!term) return;
    // A close belonging to a socket that is no longer the one attached.
    //
    // This is the race behind "I clicked a session and it said nothing
    // attached, then I clicked again and it was there". Switching sessions
    // calls `closeTerm(true)`, which closes the old socket and builds the new
    // one immediately. The OLD socket's close event lands a moment later, and
    // nothing here distinguished it, so it ran the teardown path and cleared
    // the terminal that had just been attached.
    //
    // Compared by identity rather than by a flag, so it covers every way a
    // socket stops being the current one, including ways nobody has thought
    // of yet.
    if (termSock !== sock) return;

    // The daemon's own word for what just happened, or an empty string when
    // the close did not come from it at all: a connection that dropped carries
    // no reason. See `whyClosed` in internal/daemon/attach.go.
    const why = (ev && ev.reason) || "";

    // THE DAEMON SAID IT IS RESTARTING, on the close frame itself.
    //
    // Believed the same way the `going-down` event is, and for the same
    // reason: it is the daemon saying so rather than this page inferring it
    // from a socket that went quiet. The two carry the same news by two routes
    // and either one arriving is enough, so this fills in the announcement
    // when the event was the one that got lost.
    if (why === "restarting" && !restartComing()) restartAt = Date.now();

    // A SHELL CLOSING IS NOT A SESSION GOING AWAY, and every branch below this
    // one assumes it is.
    //
    // Typing `exit` in a shell ends it, the daemon forgets it, and the next
    // attach with `?kind=shell` has nothing to attach to. Falling through meant
    // retrying that for five minutes, and in a popped-out window the retry
    // branch is unconditional, so the window sat saying it was waiting for a
    // session to come back that had never left. The runner was up the whole
    // time, one button away.
    //
    // So: go back to the runner, say so, and stop. Deliberately NOT reopening
    // the shell. A shell is opened because somebody pressed a button, and one
    // that respawns itself when closed is a shell you cannot get rid of.
    // A RESTART WAS ANNOUNCED, so this close is expected and is not the end of
    // anything. Ahead of every other branch because it is the one fact here
    // rather than an inference: the daemon said so before it touched a single
    // runner. See `armRestart`.
    if (restartComing()) {
      if (!attachSince) attachSince = Date.now();
      if (Date.now() - attachSince < attachRetryFor) {
        termWait("atrium is restarting. reconnecting to " + waitName(termTask) + "…");
        setTimeout(() => { if (term) connectTerm(taskID); }, attachRetryEvery);
        return;
      }
    }

    // NOT A RESTART, AND THE DAEMON SAID WHY IT CLOSED.
    //
    // Everything below this treats a close as something to wait out, which is
    // right for a dropped connection and wrong for a session that ended
    // because somebody typed `exit`. The retry then hammers an attach for a
    // card whose runner is deliberately gone: five minutes of failed
    // connections in the console and a pane that will not settle.
    if (why === "runner exited") {
      attachSince = 0;
      attachSaidGone = false;
      termWait("");
      // THE TEARDOWN HAS TO KNOW THIS TOO, and it is several calls away from
      // here: this branch closes the pane, and the pane's teardown is what
      // decides whether to spend ninety seconds waiting for the session to
      // come back. Without the word being written down, it waited on every
      // exit. See `noteAttachEnded`.
      noteAttachEnded(taskID, why);
      // A window that IS this session says so and tears itself down.
      //
      // THE `markTermDead` CALL IS THE POINT, and its absence was the bug. This
      // branch used to write the line below and return, so the entire
      // teardown, the countdown, and the close were unreachable: `markTermDead`
      // was defined, was the only caller of `offerSoloClose`, and was called
      // from nowhere at all. Three separate attempts fixed things downstream of
      // a function that never ran.
      //
      // The scrollback is untouched by it, which is what the operator wants
      // left: the exit, and for claude the resume id.
      if (termOnly()) {
        term.write("\r\n\x1b[38;5;244m[atrium] this session has ended\x1b[0m\r\n");
        markTermDead();
        return;
      }
      term.write("\r\n\x1b[38;5;244m[atrium] detached\x1b[0m\r\n");
      setTimeout(() => closeTerm(), 900);
      return;
    }

    // The same answer whether it closed under you or was already gone when a
    // reload asked for it. Both are "there is no shell", and neither is worth
    // waiting on: the button makes another one.
    if (termKind === "shell") {
      termKind = "runner";
      paintTermKind();
      term.write("\r\n\x1b[38;5;244m[atrium] " +
        (opened ? "shell closed. back to the agent"
                : "that shell is gone. back to the agent") +
        "\x1b[0m\r\n");
      attachSince = 0;
      termWait("");
      setTimeout(() => { if (term && termTask) connectTerm(termTask.id); }, 120);
      return;
    }

    if (!attachSince) attachSince = Date.now();
    const within = Date.now() - attachSince < attachRetryFor;

    // Never opened: keep trying until it works or the window runs out.
    //
    // Deliberately NOT clever about why. An earlier version asked the daemon
    // whether it was up, and declared the card dead when it answered while
    // still refusing the attach. That reasoning breaks the exact case this
    // exists for: a daemon comes back BEFORE its fixtures do, so there is
    // always a stretch where `/v1/health` answers and the card has no runner
    // yet. It would have cleared the pane a second before the terminal
    // appeared.
    //
    // The page is addressed by card id and that id does not change. Waiting
    // for it to start answering is the whole job, and five minutes of patience
    // costs nothing next to clearing a window that was about to work.
    if (!opened && within) {
      // One line, once, and only after long enough that this is not the
      // ordinary launch race. Silence for five minutes reads as broken.
      if (!attachSaidGone && Date.now() - attachSince > attachRaceFor) {
        attachSaidGone = true;
        term.write("\r\n\x1b[38;5;244m[atrium] waiting for atrium\x1b[0m\r\n");
      }
      if (Date.now() - attachSince > attachRaceFor) {
        // NOT CALLED A RESTART UNLESS ONE WAS ANNOUNCED. This branch is every
        // attach that never opened, which is usually a fixture that has not
        // started yet, and naming it a restart put a sentence on screen that
        // the operator could see was false. The branch below this one, which
        // has actually asked whether the daemon is there, is the one entitled
        // to say it.
        termWait(restartComing()
          ? "atrium is restarting. reconnecting to " + waitName(termTask) + "…"
          : "reconnecting to " + waitName(termTask) + "…");
      }
      setTimeout(() => { if (term) connectTerm(taskID); }, attachRetryEvery);
      return;
    }

    // Was open, and now is not. Ask who left.
    if (within && !(await daemonAnswers())) {
      if (!attachSaidGone) {
        attachSaidGone = true;
        term.write("\r\n\x1b[38;5;244m[atrium] atrium went away. reconnecting" +
          "\x1b[0m\r\n");
      }
      termWait("atrium is restarting. reconnecting to " + waitName(termTask) + "…");
      setTimeout(() => { if (term) connectTerm(taskID); }, attachRetryEvery);
      return;
    }

    // A WINDOW THAT IS ONE TERMINAL KEEPS TRYING, whatever left.
    //
    // The branch above asks the daemon who went away and gets a useful answer
    // most of the time. It gets the WRONG answer during a restart, because of
    // the order a wind-down happens in: supervised runners are stopped while
    // the HTTP listener is still up. So the runner dies, `daemonAnswers()`
    // says yes, and this reads as "the runner exited" when it is the first
    // second of a restart. On the board that costs a cleared pane and the
    // waiter picks it up again. In a popped-out window it took the window's
    // entire reason for existing, and the only way back was F5.
    //
    // There is nothing to weigh here. This window IS that card: if the card
    // comes back it should reattach, and if it is really gone the scrollback
    // holds the exit and the resume id, and the window can be closed. So it
    // retries for the whole window rather than deciding.
    if (termOnly() && within) {
      if (!attachSaidGone) {
        attachSaidGone = true;
        term.write("\r\n\x1b[38;5;244m[atrium] waiting for this session to come back" +
          "\x1b[0m\r\n");
      }
      termWait("waiting for " + waitName(termTask) + " to come back…");
      setTimeout(() => { if (term) connectTerm(taskID); }, attachRetryEvery);
      return;
    }

    attachSince = 0;
    termWait("");
    if (attachSaidGone) {
      attachSaidGone = false;
      term.write("\r\n\x1b[38;5;203m[atrium] gave up waiting for atrium\x1b[0m\r\n");
    }
    term.write("\r\n\x1b[38;5;244m[atrium] detached\x1b[0m\r\n");
    // The socket also closes when the runner exits, leaving a live-looking
    // header over a dead terminal, with the session list saying there is
    // nothing to attach to. Keeping the scrollback around left a dead terminal
    // on screen next to a list saying there was nothing there, which reads as
    // stale rather than as history. The resume id lives on the card, so
    // clearing costs nothing.
    setTimeout(() => closeTerm(), 900);
  };
  // Nothing written here. Every failed attempt during a restart fires this,
  // and `onclose` follows immediately with the one message worth reading.
  termSock.onerror = () => {};

  // ONE KEYSTROKE LISTENER PER TERMINAL, NOT PER SOCKET.
  //
  // `onData` registers a listener and returns a disposable. This function runs
  // again on every reconnect, and the terminal object survives a reconnect, so
  // without disposing the previous one each attempt left another listener
  // behind and every keypress was sent once per listener. It comes out as
  // `eexxiitt` in the terminal, which reads like a broken keyboard rather than
  // like a leak.
  //
  // Every reconnect path hits this: the restart retry, the popped-out window's
  // wait, and falling back to the agent when a shell closes.
  if (termData) { try { termData.dispose(); } catch (e) {} }
  // `sendInput` rather than `send`: it is the one place every kind of input
  // goes through, and it is what puts the view back at the bottom.
  // Watched before it is sent, so path completion knows what has been typed.
  // See `noteTyped`: it tracks atrium's OWN keystrokes and gives up the moment
  // anything could have moved the cursor.
  // NOT EVERYTHING ON `onData` IS SOMETHING SOMEBODY TYPED, and believing it
  // was is what made a scrolled-up terminal jump to the bottom on a click.
  //
  // A terminal user interface asks to be told about focus and about the mouse,
  // and Claude Code asks for both. xterm answers by SENDING ESCAPE SEQUENCES
  // AS INPUT: `ESC [ I` when the terminal is focused, `ESC [ O` when it is
  // blurred, and a coordinate report on every click. They arrive here exactly
  // like a keystroke, because to the application they are the same channel.
  //
  // They still have to be SENT, or the runner stops knowing where the pointer
  // is and whether it has focus. What they must not do is count as the
  // operator typing, which has two consequences that cost hours:
  //
  //   `sendInput` scrolls to the bottom on input, so clicking anywhere in a
  //   terminal you had scrolled up threw your position away.
  //
  //   `noteTyped` abandons its buffer on anything that is not a printable
  //   character, so every focus change silently switched off path completion.
  termData = term.onData(d => {
    const report = isAutoReport(d);
    if (!report) noteTyped(d);
    sendInput(d, report);
  });
  // Resize is handled once, globally, by the observers near onTermResize.
  // Adding a listener here leaked one per connect.
}

function send(msg) {
  if (termSock && termSock.readyState === WebSocket.OPEN) termSock.send(JSON.stringify(msg));
}

// Input to the runner, whatever produced it: a keystroke, a paste, a path
// after an upload.
//
// ONE FRAME, however big it is. DO NOT CHUNK THIS.
//
// Chunking is the obvious answer and it is wrong, and the way it is wrong is
// worth writing down because it looks like an improvement. A paste that
// arrived as four hundred 16KB frames was read by Claude Code as four hundred
// separate pastes: it detects a paste from a burst of input, so every chunk
// boundary became a new one and a single file came out as
// `[Pasted text #1 +18 lines][Pasted text #2 +10 lines]` a hundred and
// forty-six times over. The pty has no framing, so the receiver has nothing to
// reassemble from except timing, and timing is what splitting destroys.
//
// The size limit that started all this belongs on the daemon, and that is
// where it was fixed: `coder/websocket` reads 32KB per message by default and
// CLOSES THE CONNECTION when one is bigger, which is why pasting a text file
// said `detached`. See `internal/daemon/attach.go`.
// Everything the operator sends to the runner goes through here.
//
// ANY INPUT PUTS YOU BACK AT THE BOTTOM. You cannot see what you are sending
// from three screens up, and the alternative is typing into a terminal that
// looks like it is ignoring you.
//
// HERE rather than in the keystroke handler, and rather than on xterm's own
// `scrollOnUserInput`. Both of those only cover keys xterm itself handled, and
// the paths that matter most do not go through xterm at all: a paste is
// intercepted before xterm sees it so the clipboard's files are reachable, and
// a dropped file writes its path in the same way. Those were exactly the
// inputs that left the view where it was.
// ── completing a path in the terminal ───────────────────
//
// Tab belongs to whatever is running in the pty. There is no way to add
// completion to somebody else's input line by asking nicely, so this works from
// the outside, and the whole design is about giving up early rather than being
// clever.
//
// WHAT CANNOT BE KNOWN: where the cursor is, or what the line currently holds.
// The runner redraws, wraps, recalls history and rewrites the line whenever it
// likes. Anything that inserts text on an assumption about cursor position will
// eventually corrupt what somebody typed, which is far worse than no
// completion.
//
// WHAT CAN BE KNOWN EXACTLY: what atrium itself has sent. Every keystroke goes
// through one place. `typed` is a record of what left since the last Enter, not
// an inference about the screen.
//
// So the rule is: track what we sent, ABANDON the moment anything ambiguous
// happens, and offer nothing while abandoned. Silence beats wrong text.
let typed = "";
// Whether the tracked buffer is still believable. Anything that could have
// moved the cursor or rewritten the line sets this false until the next Enter.
let typedSure = true;

// noteTyped follows the keystrokes atrium sends.
//
// The list of things that abandon tracking is the load-bearing part. An arrow
// key moves the cursor, so the tail of the buffer is no longer the token under
// it. A control sequence can do anything. Anything multi-character is a paste
// or an escape sequence rather than a keystroke.
function noteTyped(d) {
  if (d === "\r" || d === "\n") {
    // A new line, and a fresh start. This is the only thing that restores
    // confidence, which is why an abandoned buffer stays abandoned until the
    // operator submits something.
    typed = "";
    typedSure = true;
    return;
  }
  if (d === "\x7f" || d === "\b") {
    // Backspace is the one edit that can be followed exactly.
    typed = typed.slice(0, -1);
    return;
  }
  // A TAB WE PASSED THROUGH IS NOT A REASON TO STOP TRACKING, and treating it
  // as one is what made this feature disable itself.
  //
  // Tab arrives here as `\t`, a control character, so the rule below abandoned
  // on it. Since Tab is the TRIGGER, one press that completed nothing turned
  // completion off until the next Enter, and every press after that kept it
  // off. In practice that was almost every abandon.
  //
  // It is safe to ignore because a Tab does not move the cursor. If the runner
  // does something with it the buffer goes stale, and the next completion
  // finds a directory that does not match and stays silent, which is the same
  // outcome as not offering.
  if (d === "\t") return;
  if (d.length !== 1 || d < " ") {
    // An escape sequence, a control character, or a paste. Any of them can put
    // the cursor somewhere this has no way to know about.
    //
    // The buffer is CLEARED as well as doubted. Leaving it meant the next
    // keystrokes appended onto text that no longer describes the line, so the
    // tracker held a plausible-looking string that was missing whatever
    // happened during the abandon.
    typedSure = false;
    typed = "";
    return;
  }
  // THREE WAYS BACK, not just Enter, because a tracker that can only be
  // re-armed by submitting a line spends most of its life switched off.
  //
  // A SPACE starts a fresh token. A token is bounded by whitespace, so
  // whatever follows one is a whole token this will have seen every character
  // of, whatever happened before. The buffer is cleared with it.
  if (d === " " && !typedSure) {
    typedSure = true;
    typed = "";
    return;
  }
  // A SEPARATOR re-arms without clearing, which is a weaker claim and a
  // deliberate one. `/` and `\` are only typed while writing a path, so the
  // characters before them in this token were typed in sequence and are very
  // likely what is on the line. That is not a guarantee, which is why the
  // buffer is kept rather than trusted: a prefix that turns out to be wrong
  // names a directory that does not exist, the daemon refuses it, and nothing
  // is offered. Silence is still the failure mode.
  //
  // Without this, `cat \worktrees\` could never re-arm mid-path, which is
  // exactly when somebody wants Tab.
  if ((d === "/" || d === "\\") && !typedSure) {
    typedSure = true;
  }
  typed += d;
}

// pathToken is the token being typed, when it looks like a path.
//
// A `/` or a `\` in it is what makes it a path rather than a word, which is the
// operator's own suggestion and is the right test: on Windows both separators
// turn up and a token may mix them.
//
// Empty when there is nothing to complete or when tracking was abandoned.
function pathToken() {
  if (!typedSure) return "";
  const tail = typed.split(/[\s"']/).pop() || "";
  if (!tail || !/[\\/]/.test(tail)) return "";
  return tail;
}

// completePath is Tab, when atrium is confident enough to answer it.
//
// Returns true when it handled the key. FALSE MEANS PASS TAB THROUGH, which is
// what keeps the runner's own completion working everywhere this does not
// apply, and it is the answer in every uncertain case.
async function completePath() {
  const token = pathToken();
  if (!token) return false;

  // Split at the last separator: everything before it is the directory to
  // list, and what follows is the prefix to match.
  const cut = Math.max(token.lastIndexOf("/"), token.lastIndexOf("\\"));
  let dir = token.slice(0, cut) || "/";
  const prefix = token.slice(cut + 1);
  // `d:\wo` cuts to `d:`, which is a drive and not a directory. The daemon
  // refuses it, so without this every from-scratch Windows path failed at the
  // first separator and Tab looked dead.
  if (/^[a-zA-Z]:$/.test(dir)) dir += "/";

  let entries;
  try {
    const r = await api("/v1/browse?path=" + encodeURIComponent(dir));
    entries = (r.entries || []).filter(e =>
      e.name.toLowerCase().startsWith(prefix.toLowerCase()));
  } catch (e) {
    // OUTSIDE THE BROWSE ROOTS, which is the ordinary case rather than an
    // error, and it is why this feature looked like it did nothing at all.
    //
    // The picker is deliberately bounded to the home directory plus every
    // directory a card already uses, because unbounded it is a recursive
    // listing of the whole machine the moment the board is published. Nothing
    // about completion is worth widening that.
    //
    // But a bound on what may be LISTED is not a bound on what may be
    // OFFERED. The roots themselves are already public to this board, so a
    // token that resolves to nothing listable falls back to matching the
    // roots, which is what makes `cd d:\wo` useful from an empty line.
    entries = await rootsMatching(token);
    if (!entries.length) return false;
    if (entries.length === 1) {
      sendCompletion(token, entries[0]);
      return true;
    }
    // Named for what they ARE rather than as generic matches. These are not
    // the contents of a directory: they are the places this board already
    // knows about, which is why they can be offered when the directory itself
    // cannot be listed.
    showCompletions(entries, "places atrium knows");
    return true;
  }
  if (!entries.length) return false;

  if (entries.length === 1) {
    sendCompletion(prefix, entries[0]);
    return true;
  }
  // The longest prefix every candidate shares. Completing that much is what a
  // shell does and it is the half of Tab that needs no interface.
  let common = entries[0].name;
  for (const e of entries) {
    while (common && !e.name.toLowerCase().startsWith(common.toLowerCase())) {
      common = common.slice(0, -1);
    }
  }
  if (common.length > prefix.length) {
    sendCompletion(prefix, { name: common, dir: false });
    return true;
  }
  // Nothing more is unambiguous, so show what is there. Drawn rather than
  // sent: printing a list into the terminal would be atrium writing into a
  // runner's screen, which it has no business doing.
  showCompletions(entries);
  return true;
}

// rootsMatching offers the browse roots that start with what has been typed.
//
// `/v1/browse` with no path answers with the roots, which is how the picker
// opens. Matched case-insensitively and on either separator, because a Windows
// path gets typed with backslashes and the daemon reports forward ones.
//
// The NAME is the whole remaining path rather than one segment, so completing
// a root jumps straight to it. That is the right behaviour here: a root is a
// place the operator already works, and offering it one directory at a time
// through directories that are not themselves listable would offer nothing.
async function rootsMatching(token) {
  let roots;
  try {
    roots = (await api("/v1/browse")).entries || [];
  } catch (e) {
    return [];
  }
  const want = token.replace(/\\/g, "/").toLowerCase();
  return roots
    .filter(r => (r.path || "").toLowerCase().startsWith(want))
    .map(r => ({ name: r.path, dir: true }));
}

// sendCompletion types only the characters that are MISSING.
//
// Never a whole line, never a backspace-and-retype. Both assume the line is
// what atrium thinks it is, and that assumption is the one thing this design
// refuses to make.
function sendCompletion(prefix, entry) {
  let add = entry.name.slice(prefix.length);
  if (entry.dir) add += "/";
  if (!add) return;
  noteTyped(add.length === 1 ? add : "");
  if (add.length !== 1) {
    // Sent as one frame, which noteTyped cannot follow character by character,
    // so the buffer is caught up by hand rather than abandoned.
    typed += add;
  }
  sendInput(add);
}

// showCompletions puts the candidates on screen, over the terminal.
//
// TWO THINGS MAKE THIS READABLE and the first version had neither.
//
// It says what it is. A list appearing over a terminal with no heading is a
// thing that just happened TO you, and the first reaction is to work out what
// broke rather than to read it.
//
// And it shows what DIFFERS. Twenty three roots under `D:/worktrees/` rendered
// as twenty three full paths is a wall in which every line begins with the
// same forty characters, so the eye has nothing to sort on. The shared part is
// said once, in the heading, and each row carries only its own tail.
function showCompletions(entries, what) {
  const host = document.querySelector(".term-pane") || document.body;
  let el = document.getElementById("t-complete");
  if (!el) {
    el = document.createElement("div");
    el.id = "t-complete";
    host.appendChild(el);
  }
  const shown = entries.slice(0, 60);
  const shared = sharedPrefix(shown.map(e => e.name));
  const more = entries.length > shown.length
    ? ` <span class="candmore">and ${entries.length - shown.length} more</span>` : "";

  setHTML(el,
    `<div class="candhead">${esc(entries.length)} ${esc(what || "matches")}${
      shared ? ` under <code>${esc(shared)}</code>` : ""}${more}
      <span class="candhint">keep typing to narrow, or press escape</span></div>` +
    shown.map(e =>
      `<span class="cand">${esc(e.name.slice(shared.length))}${e.dir ? "/" : ""}</span>`
    ).join(""));
  el.hidden = false;
  clearTimeout(el._t);
  // Dismissed on a timer as well as on the next keystroke, because it sits
  // over a terminal and a list that outstays its welcome is in the way.
  el._t = setTimeout(() => { el.hidden = true; }, 8000);
}

// sharedPrefix is the longest run every candidate starts with, cut back to a
// separator so the heading names a DIRECTORY rather than half a word.
//
// Without the cut back, `posture-bug` and `posture-check-proof` would share
// `posture-` and the heading would end mid-name, which reads as a truncation
// bug rather than as a common parent.
function sharedPrefix(names) {
  if (names.length < 2) return "";
  let p = names[0];
  for (const n of names) {
    while (p && !n.startsWith(p)) p = p.slice(0, -1);
  }
  const cut = Math.max(p.lastIndexOf("/"), p.lastIndexOf("\\"));
  return cut < 0 ? "" : p.slice(0, cut + 1);
}

function hideCompletions() {
  const el = document.getElementById("t-complete");
  if (el) el.hidden = true;
}

// followScrollUntil is when to stop dragging the view down with the output.
//
// SCROLLING WHEN THE INPUT LEAVES IS NOT ENOUGH, and believing it was is why
// this read as fixed while still being wrong. The scroll below happens at the
// moment the bytes are SENT. The runner has not seen them yet. What moves the
// bottom of the buffer is the echo that comes back and the prompt redrawing
// around it, and by then the scroll has already been and gone. A twenty line
// paste lands with the view about twenty lines short, which is worse than not
// scrolling at all, because it looks like it worked.
//
// So the view follows the output for a moment afterwards, and then stops. It
// has to stop: a terminal that scrolls to the bottom on every write can never
// be scrolled up while a runner is producing output, which is the whole reason
// xterm does not do this by default.
let followScrollUntil = 0;

// followScrollFor is how long that moment is.
//
// It covers the round trip and the redraw, not the work. A runner that thinks
// for a minute and then prints is not still being followed, and should not be.
const followScrollFor = 1200;

// `quiet` sends the bytes without treating them as something somebody typed.
//
// For the reports xterm generates on its own: focus, blur and the mouse. They
// go to the runner like any other input and they must not move the view. See
// `isAutoReport`, and the note on the `onData` handler for what this cost.
function sendInput(text, quiet) {
  send({ t: "in", d: String(text == null ? "" : text) });
  if (quiet) return;
  if (term) term.scrollToBottom();
  followScrollUntil = Date.now() + followScrollFor;
}

// Did xterm generate this by itself, rather than a person producing it.
//
// Three shapes, and all three arrive on the same channel as a keystroke:
//
//	ESC [ I            the terminal gained focus
//	ESC [ O            it lost focus
//	ESC [ < b ; x ; y M/m   a mouse report, SGR encoding, one per click
//	ESC [ M b x y      the older X10 mouse encoding
//
// Conservative on purpose. Anything not matched here is treated as typing,
// which is the direction that merely scrolls when it should not have, rather
// than swallowing a keystroke somebody meant.
function isAutoReport(d) {
  if (d === "\x1b[I" || d === "\x1b[O") return true;
  if (/^\x1b\[<\d+;\d+;\d+[Mm]$/.test(d)) return true;
  if (/^\x1b\[M/.test(d)) return true;
  return false;
}

// followScroll keeps the view at the bottom while the echo of what was just
// sent arrives. Called after every write, and a no-op for all the ones that are
// not answering something the operator did.
function followScroll() {
  if (!term || Date.now() > followScrollUntil) return;
  term.scrollToBottom();
}

function sendResize() {
  if (!term) return;
  send({ t: "resize", cols: term.cols, rows: term.rows });
}

// A FIT MUST NOT MOVE YOU. Scrolling up and then changing window focus used to
// put you back at the bottom.
//
// Nothing here scrolled on purpose. `fit()` works out rows and columns and
// hands them to xterm's `resize()`, and xterm clamps the viewport to the bottom
// whenever the row count changes. Anything producing a layout event runs this,
// and a focus change is one of them: a scrollbar appearing, the window manager,
// the board's own chrome settling. So a position the operator set by hand was
// thrown away by an update nobody asked for, which is the same complaint as the
// board repainting and losing the scroll, on the other surface.
//
// Two guards, and the first one covers most of it. A fit that changes nothing
// is not a resize: focus moving does not usually change the size at all, so the
// cheapest fix is to stop telling xterm about a size it already has.
//
// When the size DID change, where you were is restored, measured as DISTANCE
// FROM THE BOTTOM rather than as an absolute line. A width change reflows the
// buffer, so the line you were reading has a different index afterwards and
// there is no exact answer. The rows between you and the end are few and the
// rows above are many, so anchoring to the end is the approximation that lands
// closest to where you were looking.
//
// Being at the bottom already is left alone, because following new output is
// what anybody wants from there. `sendInput` handles the other deliberate
// exception: after you TYPE, `followScroll` keeps the view down for a moment.
// The rule the operator gave is exactly that, said from this side: stay put
// until I type.
// HOW BIG THE TEXT IN THE TERMINAL IS, and nothing else on the page.
//
// Browser zoom was the only tool and it scales the tabs, the card strip, the
// bar and every chip with it. The operator's words: "scale the font IN the
// terminal separate from the browser so that it doesn't scale the ui elements
// with the text".
//
// PER CARD, AND REMEMBERED IN THIS BROWSER. It is not on the daemon.
//
// The first version of this died with the pane, on the reasoning that it
// answers "I cannot read this right now" rather than "this is how I like
// terminals". That reasoning was fine and the implementation of it was not:
// `openTerm` builds a terminal every time you SWITCH to another session, so
// clicking a second card and coming back put the size straight back to
// fourteen. The operator's words: "changing terminals shouldn't clear it".
//
// So it is stored the way a popped-out window's size already is, keyed by
// card, in `localStorage`. Per card rather than one number for the board,
// because the original ask was per terminal and it is a real difference: one
// agent drawing wide tables and another printing a log want different answers.
//
// NOT ON THE DAEMON, which is the line that is still worth holding. This is a
// property of the screen somebody is reading, not of the work, so it has no
// business travelling to another machine or being served to a guest holding a
// share of one session.
const TERM_FONT_DEFAULT = 14;
const TERM_FONT_MIN = 8;
const TERM_FONT_MAX = 32;
let termFontSize = TERM_FONT_DEFAULT;

const termFontKey = (id) => "atrium.termfont." + id;

// What this card's terminal was last read at, or the default.
//
// Guarded like `readSize` is, because a stored zero or a NaN left by an older
// shape would open a terminal at a size with no glyphs in it, which looks
// exactly like a terminal that failed to attach.
function readTermFont(id) {
  try {
    const v = Number(localStorage.getItem(termFontKey(id)));
    if (v >= TERM_FONT_MIN && v <= TERM_FONT_MAX) return Math.round(v);
  } catch (e) {}
  return TERM_FONT_DEFAULT;
}

// A FONT CHANGE IS A RESIZE, which is the part that makes this more than a
// setting. xterm measures in CELLS, so changing the size changes how many rows
// and columns fit, and that number goes to the runner.
//
// So it goes through `onTermResize` rather than fitting on its own. That
// function already does the two things this path needs and both took a while
// to get right: it ignores a fit that changed nothing, since reaching xterm's
// `resize` with the same numbers snaps a scrolled-up terminal to the bottom,
// and it restores the viewport afterwards and HOLDS it while the runner
// repaints. `check-terminal.js` rules 9 through 12 exist because of those.
// Reimplementing the fit here would be a second copy of that to get wrong.
//
// It does resize the pty for every other viewer, since `setViewport` agrees on
// the smallest attached viewport. Left that way deliberately: a terminal has
// one size, an exemption would mean the agent drawing into a width nobody is
// looking at, and the second-view refusal means it is one viewer nearly always.
function setTermFont(px) {
  if (!term) return;
  const size = Math.max(TERM_FONT_MIN, Math.min(TERM_FONT_MAX, Math.round(px || 0)));
  if (size === termFontSize) return;
  termFontSize = size;
  term.options.fontSize = size;
  // Written against the card rather than against the pane, so switching to
  // another session and back finds it again. See the note above `readTermFont`
  // for why that is not the same as making it a preference.
  if (termTask && termTask.id) {
    try { localStorage.setItem(termFontKey(termTask.id), String(size)); } catch (e) {}
  }
  onTermResize();
}

function onTermResize() {
  // The bridge is placed either way. It spans the gap between the list and
  // the pane, and that gap moves whenever anything else does, attached
  // terminal or not.
  placeTabBridge();
  if (!termFit || !term) return;

  const before = term.buffer.active;
  const wasAtBottom = before.viewportY >= before.baseY;
  const fromBottom = before.baseY - before.viewportY;
  const wasCols = term.cols, wasRows = term.rows;

  termFit.fit();

  const changed = term.cols !== wasCols || term.rows !== wasRows;
  noteScrollAct(changed ? "fit(changed)" : "fit(same)");
  // TEMPORARY, and paired with `watchScroll`. See the note there: the point is
  // to say WHO moved the viewport rather than to keep narrowing the field.
  if (scrollDebugOn()) {
    console.debug("[atrium] fit:", {
      changed, wasCols, wasRows, cols: term.cols, rows: term.rows,
      wasAtBottom, fromBottom,
      viewportY: before.viewportY, baseY: before.baseY
    });
  }
  if (!changed) return;
  sendResize();
  if (wasAtBottom) return;
  holdScrollAt(fromBottom);
}

// HOLDING A POSITION AGAINST THE RUNNER, not just against the renderer.
//
// A single restore after the fit was not enough and the reason is not in the
// browser. Telling the runner its new size makes it REPAINT: a terminal user
// interface redraws on being told its size, and it draws with absolute cursor
// moves that put the view at the end. That output arrives over the websocket
// milliseconds later, after any `requestAnimationFrame`, and drags the viewport
// back down.
//
// So the position is re-asserted for as long as that repaint takes, rather
// than once. `followScroll` is the same idea pointing the other way: after you
// type, the view is pinned to the bottom for a moment because output is coming.
// This pins it where you left it for a moment, because output is coming.
//
// It stops at the first sign the operator moved, so it cannot fight a scroll,
// and it ends on its own so it can never hold a terminal that has been
// legitimately scrolled by something else.
const holdScrollFor = 600;
let holdUntil = 0, holdFrom = 0, holdTimer = 0;


// Focus the terminal WITHOUT the browser scrolling to it.
//
// `term.focus()` focuses xterm's hidden textarea, which sits at the cursor, and
// a browser scrolls a focused element into view. `preventScroll` is the whole
// answer for the focuses atrium performs itself, and closing the find bar is
// the one that surprised the operator: search for something above the fold,
// press escape, and the result you were reading is gone.
function focusTerm() {
  if (!term) return;
  const ta = term.textarea;
  if (ta && ta.focus) {
    ta.focus({ preventScroll: true });
    return;
  }
  term.focus();
}

