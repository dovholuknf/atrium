// The walk drawer: a fixed list of items an agent produced, a rail beside the terminal, and one item at a time.
//
// docs/rnd/review-tab-design.md, stage 1. The drawer sits BESIDE the one xterm in the terms view and never builds a
// second one, so every guard `openTerm` has still applies and a popped-out `#term=` window gets the drawer too.
//
// TWO HALVES, deliberately. The `dock*` functions are the drawer and the rail: open and close, the poll, the
// selection, `j` `k` `g`, the progress bar. A TENANT supplies the rest: how to tell a card is one, where its items
// come from, how one is drawn, and what a key does. The walk is the first tenant (`walkTenant`, further down).
// Open Questions (u-004) is meant to be the second, with its own source, renderer and respond action, and must not
// need this half rewritten. Do not build it here.
//
// WHERE STATE LIVES. Walk state is a `Walk:` line in each finding file, never the store. The walker reads the
// same file, the line survives a renumbering, and it sits under Evidence so it is never copied into a comment.
// Every write goes through `PUT files/text` carrying the hash it read, which is the daemon's refusal of a stale
// write. Nothing here posts to GitHub.
//
// A REVIEW THE PULLS VIEW KNOWS IS READ AND WRITTEN THROUGH `/v1/prs/{id}`. When the attached card is the walker of
// a pulls row, the findings come from `GET findings`, an edit is `PUT findings/{key}` with the hash it read, and a
// mark is `POST findings/{key}/walk`, which the daemon keeps in walk.txt. The card's own files are only read for a
// findings folder no pulls row names. Same drawer either way.

// ── the drawer and the rail ─────────────────────────────

const dock = {
  tenant: null,     // who is using the drawer
  taskId: "",       // the card it is open on
  open: false,
  items: [],
  cur: "",          // the current item's key, not its index: an index moves when a file is inserted
  timer: 0,
  gen: 0,           // bumped on every open and reset, so a slow answer for a card that has gone is dropped
  want: "",         // a card whose drawer the operator left open, so a reattach puts it back
  loading: false
};

// A card is a tenant's when its probe says so. Called from `openTerm` for every attach.
async function walkProbe(task) {
  dockReset();
  const btn = document.getElementById("t-walk");
  if (!btn || !task) return;
  const gen = dock.gen;
  let yes = false;
  try { yes = await walkTenant.probe(task); } catch (e) { yes = false; }
  if (gen !== dock.gen || !termTask || termTask.id !== task.id) return;
  btn.hidden = !yes;
  if (!yes) return;
  dock.tenant = walkTenant;
  dock.taskId = task.id;
  if (dock.want === task.id) dockOpen();
  else dockLoadQuiet();
}

// The pane is going, or is being pointed at another card. Closes the drawer without forgetting that it was
// wanted, so the reattach that follows a dropped socket puts it back.
function dockReset() {
  dock.gen++;
  clearTimeout(dock.timer);
  dock.timer = 0;
  dock.open = false;
  dock.items = [];
  dock.cur = "";
  dock.taskId = "";
  dock.tenant = null;
  dock.editing = null;
  dock.conflict = null;
  const drawer = document.getElementById("walk-drawer");
  if (drawer) drawer.hidden = true;
  const btn = document.getElementById("t-walk");
  if (btn) { btn.hidden = true; btn.classList.remove("go"); }
  const mini = document.getElementById("t-walk-bar");
  if (mini) mini.innerHTML = "";
  walkTenant.reset();
}
function walkReset() { dockReset(); }

function toggleWalk() {
  if (dock.open) { dock.want = ""; dockClose(); }
  else dockOpen();
}

async function dockOpen() {
  const drawer = document.getElementById("walk-drawer");
  if (!drawer || !dock.tenant || !termTask) return;
  dock.open = true;
  dock.want = dock.taskId;
  drawer.hidden = false;
  walkApplyWidth();
  const btn = document.getElementById("t-walk");
  if (btn) btn.classList.add("go");
  if (typeof onTermResize === "function") onTermResize();
  await dock.tenant.opened(dock);
  await dockLoad();
  drawer.focus({ preventScroll: true });
}

function dockClose() {
  const drawer = document.getElementById("walk-drawer");
  dock.open = false;
  clearTimeout(dock.timer);
  dock.timer = 0;
  if (drawer) drawer.hidden = true;
  const btn = document.getElementById("t-walk");
  if (btn) btn.classList.remove("go");
  if (typeof onTermResize === "function") onTermResize();
  if (typeof term !== "undefined" && term) term.focus();
}
function walkClose() { dock.want = ""; dockClose(); }

// NO POLL. The list is read again when something says it may have changed: an event for this card (`dockKick`,
// from `onTaskEvent`), the tab becoming visible, and the 60s resync (`dockResync`). Findings are files an agent
// writes during a turn and no event says one landed, so the card's own events stand in. A burst is one read.
const DOCK_KICK_MS = 1000;
function dockKick(id) {
  if (!dock.tenant || (id && id !== dock.taskId) || dock.timer) return;
  dock.timer = setTimeout(() => { dock.timer = 0; dockResync(); }, DOCK_KICK_MS);
}
function dockResync() {
  if (!dock.tenant || document.hidden) return;
  // A read in flight may have listed the folder before the change: read once more when it lands.
  if (dock.loading) { dock.again = true; return; }
  dockLoad();
}
document.addEventListener("visibilitychange", () => { if (dock.open) dockResync(); });

// The button carries the progress even while the drawer is shut, so the list is read once when a card attaches.
async function dockLoadQuiet() {
  const gen = dock.gen;
  await dockLoad();
  if (gen !== dock.gen) return;
}

async function dockLoad() {
  const t = dock.tenant;
  if (!t || dock.loading) return;
  const gen = dock.gen;
  dock.loading = true;
  let got;
  try {
    got = await t.load(termTask, dock);
  } catch (e) {
    dock.loading = false;
    return;
  }
  dock.loading = false;
  if (dock.again) { dock.again = false; dockResync(); }
  if (gen !== dock.gen) return;
  const was = dock.cur;
  const wasIdx = dock.items.findIndex(i => i.key === was);
  dock.items = got.items;
  let idx = dock.items.findIndex(i => i.key === was);
  if (idx < 0 && dock.items.length) idx = Math.min(Math.max(wasIdx, 0), dock.items.length - 1);
  dock.cur = idx >= 0 ? dock.items[idx].key : "";
  dockPaint();
}

function dockSelect(key, focus) {
  if (dock.editing || dock.conflict) return;
  if (dock.cur !== key) dock.tenant.selected(dock, key);
  dock.cur = key;
  dockPaint();
  const row = document.querySelector('#walk-rail .wk-row.cur');
  if (row) row.scrollIntoView({ block: "nearest" });
  if (focus) document.getElementById("walk-drawer").focus({ preventScroll: true });
}
function dockStep(d) {
  const i = dock.items.findIndex(x => x.key === dock.cur);
  const n = Math.max(0, Math.min(dock.items.length - 1, i + d));
  if (dock.items[n]) dockSelect(dock.items[n].key);
}
function dockCurrent() { return dock.items.find(i => i.key === dock.cur) || null; }

function dockPaint() {
  const t = dock.tenant;
  const drawer = document.getElementById("walk-drawer");
  if (!t) return;
  const segs = t.segments(dock.items);
  const barHtml = segs.map(s => `<i class="${s.cls}${s.done ? " done" : ""}"></i>`).join("");
  const mini = document.getElementById("t-walk-bar");
  if (mini) setHTML(mini, barHtml);
  if (!dock.open || !drawer) return;
  const head = t.header(dock);
  document.getElementById("walk-title").textContent = head.title;
  document.getElementById("walk-note").textContent = head.note;
  setHTML(document.getElementById("walk-bar"), barHtml);
  document.getElementById("walk-count").textContent = t.summary(dock.items);
  setHTML(document.getElementById("walk-rail"), dock.items.map((it, i) => t.railRow(it, i, dock)).join(""));
  // Held while a compose box is open: repainting it would throw away what was typed.
  if (!dock.editing) {
    const pane = document.getElementById("walk-item");
    const cur = dockCurrent();
    if (!cur) setHTML(pane, `<div class="empty">${esc(t.empty)}</div>`);
    else setHTML(pane, t.pane(cur, dock));
  }
}

// ── the tenant: findings in a review folder ────────────

const walkSevs = {
  blocking: "high", high: "high", critical: "high",
  med: "med", medium: "med",
  low: "low",
  nit: "nit"
};
const walkNameRe = /^(\d+)-([a-z]+)-(.+)-L(\d+)\.txt$/i;

// The label line: `MED controller/share.go line 165: committed = true`.
const walkLabelRe = /^(\S+)\s+(\S+)\s+line\s+(\d+):\s?(.*)$/;

function walkParseName(name) {
  const m = walkNameRe.exec(name);
  if (!m) return null;
  const sev = walkSevs[m[2].toLowerCase()];
  if (!sev) return null;
  return { num: m[1], sevWord: m[2], sev, file: m[3], line: Number(m[4]) };
}

function walkNow() { return new Date().toISOString().replace(/\.\d+Z$/, "Z"); }

// One finding file, read into the parts the drawer draws and the parts a write must put back untouched.
function walkParseFinding(name, text, hash, eol, meta) {
  const lines = text.split("\n");
  let ev = -1;
  for (let i = 3; i < lines.length; i++) if (lines[i].trim() === "Evidence") { ev = i; break; }
  const end = ev < 0 ? lines.length : ev;
  const label = (lines[1] || "").trim();
  const lm = walkLabelRe.exec(label);
  const link = /^https?:\/\//.test((lines[2] || "").trim()) ? lines[2].trim() : "";
  const bullets = lines.slice(3, end);
  while (bullets.length && !bullets[0].trim()) bullets.shift();
  while (bullets.length && !bullets[bullets.length - 1].trim()) bullets.pop();
  const comment = bullets.length ? [label, ""].concat(bullets).join("\n") : label;
  const evLines = ev < 0 ? [] : lines.slice(ev + 1);
  while (evLines.length && !evLines[evLines.length - 1].trim()) evLines.pop();
  const field = re => { for (const l of evLines) { const m = re.exec(l); if (m) return m[1].trim(); } return ""; };
  const nm = walkParseName(name) || { num: "", sev: "nit", sevWord: "", file: "", line: 0 };
  const path = lm ? lm[2] : nm.file;
  const code = lm ? lm[4].trim() : "";
  const id = field(/^Id:\s*(.+)$/);
  const wl = field(/^Walk:\s*(.+)$/);
  const wm = /^(accepted|posted|skipped)\s*(\S*)\s*(.*)$/.exec(wl);
  return {
    name, text, hash, eol: eol || "\n", mtime: (meta && meta.mtime) || "", size: (meta && meta.size) || 0,
    lines, ev, prUrl: (lines[0] || "").trim(), label, link, comment, evLines,
    num: nm.num, sev: nm.sev, sevText: lm ? lm[1].toUpperCase() : nm.sevWord.toUpperCase(),
    path, line: lm ? Number(lm[3]) : nm.line, code,
    id, leak: evLines.some(l => /^Leak:/.test(l)),
    state: wm ? walkUiState(wm[1] === "posted" ? "done" : wm[1]) : "", stateAt: wm ? wm[2] : "", stateUrl: wm ? wm[3].trim() : "",
    key: "", isNew: false, flash: null
  };
}

// Which finding this is, across a rename. The `Id:` line when there is one, else the path and the code on the
// label line. The fallback is a display heuristic and never an identity: two findings on repeated code in one
// file collide on it, which `walkUniqueKeys` breaks by order.
function walkKeyOf(it) { return it.id ? "id:" + it.id : "pc:" + it.path + "\u0001" + it.code; }
function walkUniqueKeys(items) {
  const seen = new Map();
  for (const it of items) {
    const k = walkKeyOf(it);
    const n = seen.get(k) || 0;
    seen.set(k, n + 1);
    it.key = n ? k + "\u0001#" + n : k;
  }
}

// Setting or clearing the Walk line, on a text that may be stale. Pure, so a refused write can be redone on what
// the daemon handed back.
function walkSetWalkLine(text, line) {
  const lines = text.split("\n");
  const trail = lines.length && lines[lines.length - 1] === "";
  if (trail) lines.pop();
  let ev = -1;
  for (let i = 3; i < lines.length; i++) if (lines[i].trim() === "Evidence") { ev = i; break; }
  const out = lines.filter((l, i) => !(ev >= 0 && i > ev && /^Walk:/.test(l)));
  if (line) {
    if (ev < 0) out.push("", "Evidence");
    out.push(line);
  }
  return out.join("\n") + "\n";
}

// The comment part replaced, everything else (the PR line, the link line, Evidence) kept as it was.
function walkRebuild(text, comment) {
  const lines = text.split("\n");
  let ev = -1;
  for (let i = 3; i < lines.length; i++) if (lines[i].trim() === "Evidence") { ev = i; break; }
  const body = comment.replace(/\r\n/g, "\n").split("\n");
  while (body.length && !body[0].trim()) body.shift();
  while (body.length && !body[body.length - 1].trim()) body.pop();
  const label = body.shift() || "";
  while (body.length && !body[0].trim()) body.shift();
  const out = [lines[0] || "", label, lines[2] || ""];
  if (body.length) out.push("", ...body);
  if (ev >= 0) out.push("", ...lines.slice(ev));
  else out.push("");
  return out.join("\n");
}

// pr.diff into `path -> hunks`, each hunk a run of NEW-side lines. Removed lines are dropped: the drawer shows the
// code at the head, and a number that only the old side has is no use to a comment.
function walkParseDiff(text) {
  const files = new Map();
  const src = text.split("\n");
  if (src.length && src[src.length - 1] === "") src.pop();
  let cur = null, hunk = null, n = 0, header = false;
  for (const l of src) {
    if (l.startsWith("diff --git ")) { cur = null; hunk = null; header = true; continue; }
    if (header && l.startsWith("+++ ")) {
      let p = l.slice(4).trim();
      if (p === "/dev/null") { cur = null; continue; }
      if (p.startsWith("b/")) p = p.slice(2);
      cur = { path: p, hunks: [] };
      files.set(p, cur);
      continue;
    }
    const h = /^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(l);
    if (h) {
      header = false;
      if (!cur) { hunk = null; continue; }
      hunk = { lines: [] };
      cur.hunks.push(hunk);
      n = Number(h[1]);
      continue;
    }
    if (!hunk || header) continue;
    const c = l[0];
    if (c === "+") hunk.lines.push({ t: "+", n: n++, text: l.slice(1) });
    else if (c === " " || l === "") hunk.lines.push({ t: " ", n: n++, text: l.slice(1) });
    // "-" and "\ No newline at end of file" carry no new-side line.
  }
  return files;
}

function walkDiffFile(files, path) {
  if (!files) return null;
  if (files.has(path)) return files.get(path);
  const base = path.split("/").pop();
  for (const [p, f] of files) if (p.endsWith("/" + path) || p.split("/").pop() === base) return f;
  return null;
}

function walkNorm(s) { return String(s || "").replace(/\s+/g, " ").trim(); }

// Where finding `it` sits in the diff, and what is wrong with it, as text a person can read.
function walkLocate(it, files) {
  if (!files) return { warns: [], hunk: null };
  const f = walkDiffFile(files, it.path);
  if (!f) return { warns: [`${it.path} is not in pr.diff`], hunk: null };
  for (const h of f.hunks) {
    const idx = h.lines.findIndex(l => l.n === it.line);
    if (idx < 0) continue;
    const l = h.lines[idx];
    const warns = [];
    const a = walkNorm(l.text), b = walkNorm(it.code);
    if (b && a !== b && !a.includes(b) && !b.includes(a)) warns.push("line text differs from the diff");
    if (l.t !== "+") warns.push("unchanged line, GitHub will not take a comment here");
    return { warns, hunk: h, idx };
  }
  return { warns: ["unchanged line, GitHub will not take a comment here"], hunk: null };
}

// The pulls row whose walker is this card, from the rows the pulls view holds.
function walkPrOf(taskId) {
  if (typeof pulls === "undefined" || !taskId) return null;
  return pulls.rows.find(r => r.walker_task === taskId) || null;
}

// The API's walk words and the drawer's: done is posted.
// accepted is clint agreeing it should be raised, dismissed is `skipped` on the wire. The wire words stay.
function walkUiState(s) {
  return s === "done" ? "posted" : s === "skipped" ? "dismissed" : s === "accepted" || s === "deferred" ? s : "";
}

// One finding of `GET /v1/prs/{id}/findings` as a drawer item. Its hunk becomes a one-file diff, so the code view
// is the one the file source draws.
function walkPrItem(f) {
  const it = walkParseFinding(f.file, f.text || "", f.hash, "\n", null);
  it.key = f.key;
  it.num = String(f.position).padStart(2, "0");
  it.sev = f.sev;
  it.path = f.path || it.path;
  it.line = f.line || 0;
  it.link = f.link || "";
  it.leak = !!f.leak;
  const w = f.walk || {};
  it.state = walkUiState(w.state); it.stateAt = w.at || ""; it.stateUrl = w.url || "";
  it.diff = f.hunk ? walkParseDiff(`diff --git a/${it.path} b/${it.path}\n+++ b/${it.path}\n${f.hunk}`) : null;
  return it;
}

const walkTenant = {
  name: "walk",
  empty: "no findings in this folder",
  diff: null,          // Map from walkParseDiff, or null
  diffNote: "",        // why there is none
  cache: new Map(),    // file name -> item, so an unchanged mtime costs nothing
  ctx: { key: "", above: 3, below: 3 },
  evOpen: new Set(),   // keys whose Evidence is unfolded
  prId: "",            // the pulls row this card walks, "" for a bare findings folder

  reset() {
    this.prId = "";
    this.diff = null; this.diffNote = ""; this.cache = new Map();
    this.ctx = { key: "", above: 3, below: 3 };
  },

  async probe(task) {
    const row = walkPrOf(task.id);
    if (row) { this.prId = row.id; return true; }
    if (!task.worktree) return false;
    const res = await api(`/v1/tasks/${task.id}/files/list?path=findings`);
    return (res.entries || []).some(e => !e.dir && walkParseName(e.name));
  },

  async opened(d) {
    if (this.prId) { this.diff = null; this.diffNote = ""; return; }
    // The diff is read when the drawer opens, and not on the poll: it is the head as reviewed, and does not move.
    this.diff = null; this.diffNote = "";
    try {
      const r = await api(`/v1/tasks/${d.taskId}/files/text?path=pr.diff`);
      this.diff = walkParseDiff(r.text);
    } catch (e) {
      this.diffNote = e && e.status === 400
        ? "pr.diff is too large to show here, so only the label's line is shown"
        : "no pr.diff in this folder, so only the label's line is shown. step 7 should copy it here";
    }
  },

  // The findings of a pulls row. The text is parsed here the way a file is, and what the daemon already worked
  // out (key, place, walk state, the hunk) is laid over it.
  async loadPr(d) {
    const out = await api(`/v1/prs/${encodeURIComponent(this.prId)}/findings`);
    if (out && out.pr && typeof pullsApplyRow === "function") { pullsApplyRow(out.pr); pullsChanged(); }
    const next = (out.findings || []).map(f => walkPrItem(f));
    const prev = new Map(d.items.map(i => [i.key, i]));
    for (const it of next) {
      const before = prev.get(it.key);
      if (before) it.isNew = before.isNew;
      else if (d.items.length) it.isNew = true;
    }
    return { items: next };
  },

  async load(task, d) {
    if (this.prId) return this.loadPr(d);
    const res = await api(`/v1/tasks/${task.id}/files/list?path=findings`);
    const entries = (res.entries || []).filter(e => !e.dir && walkParseName(e.name))
      .sort((a, b) => a.name < b.name ? -1 : a.name > b.name ? 1 : 0);
    const prev = d.items;
    const prevByKey = new Map(prev.map(i => [i.key, i]));
    const next = [];
    const cache = new Map();
    for (const e of entries) {
      let it = this.cache.get(e.name);
      if (!it || it.mtime !== (e.mtime || "") || it.size !== (e.size || 0)) {
        const r = await api(`/v1/tasks/${task.id}/files/text?path=${encodeURIComponent("findings/" + e.name)}`);
        const fresh = walkParseFinding(e.name, r.text, r.hash, r.eol, e);
        // A file we wrote ourselves, seen back: nothing changed that clint did not do.
        const mine = it && it.wrote === r.hash;
        fresh.old = it && !mine ? it : null;
        it = fresh;
      }
      cache.set(e.name, it);
      next.push(it);
    }
    this.cache = cache;
    walkUniqueKeys(next);
    for (const it of next) {
      const before = prevByKey.get(it.key);
      // `new`: a key that was not there. A rename keeps its key, so it is not new, and it keeps its place.
      if (before) it.isNew = before.isNew;
      else if (prev.length) it.isNew = true;
      if (it.old) {
        const oldSet = new Map();
        for (const l of it.old.comment.split("\n")) oldSet.set(l, (oldSet.get(l) || 0) + 1);
        const changed = new Set();
        it.comment.split("\n").forEach((l, i) => {
          const n = oldSet.get(l) || 0;
          if (n > 0) oldSet.set(l, n - 1); else changed.add(i);
        });
        if (changed.size) {
          it.flash = { lines: changed, until: Date.now() + 2000 };
          const key = it.key;
          setTimeout(() => {
            const cur = dock.items.find(x => x.key === key);
            if (cur && cur.flash && Date.now() >= cur.flash.until - 50) { cur.flash = null; if (dock.open) dockPaint(); }
          }, 2100);
        }
        it.old = null;
      } else {
        const before2 = prevByKey.get(it.key);
        if (before2 && before2.flash && before2.flash.until > Date.now()) it.flash = before2.flash;
      }
    }
    return { items: next };
  },

  selected(d, key) {
    this.ctx = { key, above: 3, below: 3 };
    const it = d.items.find(i => i.key === key);
    if (it) it.isNew = false;
  },

  header(d) {
    const first = d.items[0];
    let title = "findings";
    if (first) {
      const m = /github\.com\/([^/]+\/[^/]+)\/pull\/(\d+)/.exec(first.prUrl);
      if (m) title = `${m[1]} #${m[2]}`;
    }
    const wt = (termTask && termTask.worktree) || "";
    const sha = /-([0-9a-f]{7,40})$/.exec(wt.replace(/[\\/]+$/, "").split(/[\\/]/).pop());
    return { title, note: sha ? "head " + sha[1] : "" };
  },

  summary(items) {
    const n = s => items.filter(i => i.state === s).length;
    const a = n("accepted"), p = n("posted"), d = n("dismissed"), f = n("deferred");
    return `${p + d} of ${items.length}` + (a || p || d || f
      ? `: ${a} accepted, ${p} posted, ${d} dismissed` + (f ? `, ${f} deferred` : "") : "");
  },

  segments(items) {
    return items.map(i => ({ cls: "s-" + i.sev + (i.leak ? " leak" : ""), done: i.state === "posted" || i.state === "dismissed" }));
  },

  railRow(it, i, d) {
    const glyph = it.state === "posted" ? "✓" : it.state === "accepted" ? "●" : it.state === "dismissed" ? "─" : it.state === "deferred" ? "…" : "";
    const prev = d.items[i - 1];
    return `<button type="button" class="wk-row s-${it.sev}${it.key === d.cur ? " cur" : ""}${it.state ? " " + it.state : ""}${
        prev && prev.sev !== it.sev ? " brk" : ""}" data-i="${i}" role="option"
        aria-selected="${it.key === d.cur}" data-tip="${esc(it.label)}">
      <span class="wk-g">${glyph}</span><span class="wk-n">${esc(it.num)}</span><span class="wk-sev s-${it.sev}">${esc(it.sevText)}</span><span class="wk-p">${
        esc(it.path.split("/").pop())}:${it.line}</span>${
        it.leak ? `<span class="wk-lk" data-tip="a leak">◆</span>` : ""}${
        it.isNew ? `<span class="wk-new">new</span>` : ""}</button>`;
  },

  // The finding: header, code at the head, comment as raw markdown, Evidence folded, the keys.
  pane(it, d) {
    if (d.conflict && d.conflict.key === it.key) return this.compare(it, d.conflict);
    const files = it.diff !== undefined ? it.diff : this.diff;
    const loc = walkLocate(it, files);
    const warns = loc.warns.slice();
    if (!files && this.diffNote) warns.push(this.diffNote);
    const state = it.state
      ? `<span class="wk-state ${it.state}">${it.state} ${esc(it.stateAt)}${it.stateUrl ? " " + esc(it.stateUrl) : ""}</span>` : "";
    return `
      <div class="wk-fhead">
        <span class="wk-num">${esc(it.num)}</span>
        <span class="wk-sev s-${it.sev}">${esc(it.sevText)}</span>${it.leak ? `<span class="wk-leakchip">LEAK</span>` : ""}
        <span class="wk-where">${esc(it.path)} · line ${it.line}</span>${state}
        <span class="grow"></span>
        ${it.link ? `<button type="button" data-act="o" data-tip="open the line on GitHub (o)">⧉ open</button>` : ""}
      </div>
      ${warns.length ? `<div class="wk-warns">${warns.map(w => `<div class="wk-warn">${esc(w)}</div>`).join("")}</div>` : ""}
      ${this.codeHtml(it, loc)}
      <div class="wk-comment" data-tip="exactly what c copies">${this.commentHtml(it)}</div>
      ${this.evidenceHtml(it)}
      <div class="wk-actions">
        <button type="button" data-act="a" data-tip="ask the walker about this one, then finish the sentence (a). A asks the canned question">a ask</button>
        <button type="button" data-act="e" data-tip="edit the comment (e)">e edit</button>
        <button type="button" data-act="c" data-tip="copy the comment (c). C copies then opens the line">c copy</button>
        <button type="button" data-act="o" data-tip="open the line on GitHub (o)"${it.link ? "" : " disabled"}>o open</button>
        <button type="button" data-act="y" data-tip="accept: it should be raised, not posted yet (y)">y accept</button>
        <button type="button" data-act="d" data-tip="mark posted, with the comment link if you have it (d)">d posted</button>
        <button type="button" data-act="s" data-tip="dismiss: do not raise it (s)">s dismiss</button>${this.prId
          ? `<button type="button" data-act="f" data-tip="come back to it later (f)">f defer</button>` : ""}
        <button type="button" data-act="u" data-tip="set it back to open (u)"${it.state ? "" : " disabled"}>u undo</button>
      </div>`;
  },

  codeHtml(it, loc) {
    if (!loc.hunk) {
      return `<div class="wk-code"><div class="wk-l at"><span class="wk-ln">${it.line}</span><span class="wk-mk">▸</span><span class="wk-tx">${
        esc(it.code)}</span></div></div>`;
    }
    const ctx = this.ctx.key === it.key ? this.ctx : { above: 3, below: 3 };
    const ls = loc.hunk.lines;
    const from = Math.max(0, loc.idx - ctx.above), to = Math.min(ls.length - 1, loc.idx + ctx.below);
    const rows = [];
    for (let i = from; i <= to; i++) {
      const l = ls[i];
      rows.push(`<div class="wk-l${l.t === "+" ? " add" : ""}${i === loc.idx ? " at" : ""}"><span class="wk-ln">${l.n}</span><span class="wk-mk">${
        l.t === "+" ? "+" : " "}${i === loc.idx ? "▸" : ""}</span><span class="wk-tx">${esc(l.text) || "&nbsp;"}</span></div>`);
    }
    const above = from, below = ls.length - 1 - to;
    return `<div class="wk-code">${
      above > 0 ? `<button type="button" class="wk-more" data-more="above">⋯ ${above} more above</button>` : ""}${
      rows.join("")}${
      below > 0 ? `<button type="button" class="wk-more" data-more="below">⋯ ${below} more below</button>` : ""}</div>`;
  },

  commentHtml(it) {
    const flash = it.flash && it.flash.until > Date.now() ? it.flash.lines : null;
    return it.comment.split("\n").map((l, i) => `<div class="wk-cl${flash && flash.has(i) ? " flash" : ""}${
      i === 0 ? " lab" : ""}">${esc(l).replace(/`([^`]+)`/g, "<code>`$1`</code>") || "&nbsp;"}</div>`).join("");
  },

  evidenceHtml(it) {
    if (!it.evLines.length) return "";
    const f = re => { for (const l of it.evLines) { const m = re.exec(l); if (m) return m[1]; } return ""; };
    const cause = f(/^Cause:\s*([^(.]*)/).trim();
    const test = f(/^Test status:\s*(\S+)/).replace(/[.,]$/, "");
    const found = f(/^Found:\s*([a-z]+)/i);
    const bits = [cause, test ? (/^none/i.test(test) ? "no test" : "test: " + test) : "", found.toLowerCase()].filter(Boolean);
    const open = this.evOpen.has(it.key);
    return `<div class="wk-ev${open ? " open" : ""}"><button type="button" class="wk-evh" data-ev="1" aria-expanded="${open}">${
      open ? "▾" : "▸"} Evidence <span>${esc(bits.join(" · "))}</span></button>${
      open ? `<pre>${esc(it.evLines.join("\n"))}</pre>` : ""}</div>`;
  },

  // A write that was refused: yours against what is on disk, and a way through either side.
  compare(it, c) {
    return `
      <div class="wk-fhead"><span class="wk-num">${esc(it.num)}</span><span class="wk-where">this file changed while you were editing it</span></div>
      ${c.said ? `<div class="wk-warns"><div class="wk-warn">${esc(c.said)}</div></div>` : ""}
      <div class="wk-cmp">
        <div><div class="wk-cmph">yours</div><pre id="walk-cmp-mine">${esc(c.mine)}</pre></div>
        <div><div class="wk-cmph">on disk</div><pre id="walk-cmp-disk">${esc(c.diskComment)}</pre></div>
      </div>
      <div class="wk-actions">
        <button type="button" class="go" data-act="keep" data-tip="write your comment over what is there now">keep mine</button>
        <button type="button" data-act="take" data-tip="drop your edit and use what is on disk">take theirs</button>
      </div>`;
  }
};

// ── keys and actions ────────────────────────────────────

function walkKeydown(e) {
  if (e.ctrlKey || e.metaKey || e.altKey) return;
  const t = e.target;
  if (t && (t.tagName === "TEXTAREA" || t.tagName === "INPUT" || t.tagName === "SELECT")) return;
  const k = e.key;
  const key = k === "Enter" ? "C" : k;
  if (k === "Enter" && t && (t.tagName === "BUTTON" || t.tagName === "A")) return;
  if (!"aAecCopsdyfujkg".includes(key) || key.length !== 1) return;
  if (dock.editing || dock.conflict) return;
  e.preventDefault();
  e.stopPropagation();
  walkAct(key);
}

async function walkAct(k) {
  if (k === "j") return dockStep(1);
  if (k === "k") return dockStep(-1);
  if (k === "g") {
    const it = dock.items.find(i => !i.state);
    if (it) dockSelect(it.key);
    return;
  }
  const it = dockCurrent();
  if (!it) return;
  if (k === "keep") return walkKeepMine(it);
  if (k === "take") return walkTakeTheirs(it);
  if (dock.editing || dock.conflict) return;
  const at = `${it.num} ${it.path.split("/").pop()}:${it.line}`;
  switch (k) {
    case "a": return walkAsk(`about ${at}, `, false);
    case "A": return walkAsk(`about ${at}, do we care? do we know?`, true);
    case "e": return walkEdit(it);
    case "c": return void walkCopy(it.comment, "copied the comment");
    case "C":
      if (await walkCopy(it.comment, "copied the comment")) walkOpenLink(it);
      return;
    case "o": return walkOpenLink(it);
    case "p": case "d": return walkPosted(it);
    case "y": return void walkWriteState(it, `Walk: accepted ${walkNow()}`, "accepted");
    case "s": return void walkWriteState(it, `Walk: skipped ${walkNow()}`, "skipped");
    case "f":
      if (!walkTenant.prId) return;
      return void walkWriteState(it, null, "deferred");
    case "u":
      if (!it.state) { toast("nothing to undo", "this finding is still open"); return; }
      return void walkWriteState(it, null, "open");
  }
}

// A keystroke in clint's own terminal, not a message. `POST /message` types and presses Enter and refuses a
// part-written line, which is right for someone else talking and wrong here. One frame, so it is not a chunked
// paste. `a` leaves the line unfinished and the cursor at its end, `A` submits.
function walkAsk(text, submit) {
  if (typeof termSock === "undefined" || !termSock || termSock.readyState !== WebSocket.OPEN) {
    toast("the terminal is not connected", "there is nobody to ask yet");
    return;
  }
  sendInput(text);
  if (submit) setTimeout(() => sendInput("\r"), 60);
  if (term) term.focus();
}

async function walkCopy(text, said) {
  try {
    await navigator.clipboard.writeText(text);
  } catch (e) {
    // Reading or writing the clipboard is a permission that a share origin has not been asked for. A selection
    // and the browser's own copy command need none.
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.style.cssText = "position:fixed;opacity:0;top:0;left:0";
    document.body.appendChild(ta);
    ta.select();
    let ok = false;
    try { ok = document.execCommand("copy"); } catch (e2) {}
    ta.remove();
    if (!ok) { toast("could not copy it", e && e.message ? e.message : "the clipboard refused"); return false; }
  }
  toast(said || "copied", text.split("\n")[0]);
  return true;
}

function walkOpenLink(it) {
  if (!it.link) { toast("no link", "the third line of this file is not a link"); return; }
  // The one way this board opens a link into a reused tab (u-006, defined in terminal-links.js). Not a
  // window.open of our own.
  openLinkReused(it.link);
}

async function walkPosted(it) {
  const url = await askUser({
    title: `mark ${it.num} posted`,
    body: "the URL of the comment on GitHub, if you have it. enter to skip.",
    input: true, placeholder: "https://github.com/...",
    buttons: [{ label: "cancel", value: null }, { label: "mark posted", value: true, style: "go" }]
  });
  if (url === null) return;
  const u = String(url).trim();
  return void walkWriteState(it, `Walk: posted ${walkNow()}${u ? " " + u : ""}`, "done", u);
}

// The one write of a finding's text. A pulls row's finding is named by its key, a file by its name.
async function walkPut(taskId, name, text, hash, eol, it) {
  if (walkTenant.prId && it) {
    return api(`/v1/prs/${encodeURIComponent(walkTenant.prId)}/findings/${encodeURIComponent(it.key)}`, {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text, hash, eol })
    });
  }
  return api(`/v1/tasks/${taskId}/files/text?path=${encodeURIComponent("findings/" + name)}`, {
    method: "PUT", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ text, hash, eol })
  });
}

// Takes the new text into the item and the cache without waiting for the poll. The mtime is left alone on
// purpose, so the poll still re-reads it, and `wrote` lets that read know the change was ours.
function walkAdopt(it, text, hash, key) {
  const fresh = walkParseFinding(it.name, text, hash, it.eol, { mtime: it.mtime, size: it.size });
  fresh.wrote = hash;
  fresh.key = it.key;
  fresh.isNew = it.isNew;
  if (walkTenant.prId) {
    // The walk state is not in the text here, and the daemon may have given an edited finding a new key.
    fresh.key = key || it.key;
    if (dock.cur === it.key) dock.cur = fresh.key;
    fresh.diff = it.diff;
    fresh.num = it.num;
    fresh.link = it.link;
    fresh.state = it.state; fresh.stateAt = it.stateAt; fresh.stateUrl = it.stateUrl;
  }
  const i = dock.items.indexOf(it);
  if (i >= 0) dock.items[i] = fresh;
  if (!walkTenant.prId) walkTenant.cache.set(it.name, fresh);
  return fresh;
}

// accepted, posted, dismissed, undo. Only the Walk line changes, so when the file has moved on it is safe to say the same
// thing again on what the daemon hands back, once. An edit is not like that and goes through the compare.
async function walkWriteState(it, line, state, url) {
  if (walkTenant.prId) return walkMark(it, state, url);
  let text = it.text, hash = it.hash;
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      const out = walkSetWalkLine(text, line);
      const res = await walkPut(dock.taskId, it.name, out, hash, it.eol);
      walkAdopt(it, out, res.hash);
      dockPaint();
      return true;
    } catch (e) {
      if (e.status === 409 && e.body && e.body.hash && attempt === 0) { text = e.body.text; hash = e.body.hash; continue; }
      toast("could not write it", e.message);
      return false;
    }
  }
  return false;
}

// A mark on a pulls row's finding: the daemon replaces that finding's line in walk.txt and answers the counts, which
// the row in the pulls view takes at once.
async function walkMark(it, state, url) {
  try {
    const body = { state };
    if (url) body.url = url;
    const out = await pullsPost(`/v1/prs/${encodeURIComponent(walkTenant.prId)}/findings/${encodeURIComponent(it.key)}/walk`, body);
    const w = (out && out.walk) || { state, at: "", url: "" };
    it.state = walkUiState(w.state); it.stateAt = w.at || ""; it.stateUrl = w.url || "";
    const row = pulls.rows.find(r => r.id === walkTenant.prId);
    if (row && out && out.counts) { row.walk = out.counts; pullsChanged(); }
    dockPaint();
    return true;
  } catch (e) {
    toast("could not mark it", e.message);
    return false;
  }
}

function walkEdit(it) {
  dock.editing = { key: it.key, text: it.text, hash: it.hash };
  const pane = document.getElementById("walk-item");
  // setHTML remembers what it last painted, and this is not that.
  pane.__paintedFrom = null;
  pane.innerHTML = `
    <div class="wk-fhead"><span class="wk-num">${esc(it.num)}</span><span class="wk-where">editing the comment</span></div>
    <textarea id="walk-edit-text" class="wk-edit" spellcheck="false" aria-label="the comment"></textarea>
    <div class="wk-actions">
      <button type="button" class="go" data-act="save" data-tip="write it (ctrl-s)">save</button>
      <button type="button" data-act="cancel" data-tip="leave the file alone (escape)">cancel</button>
    </div>`;
  const ta = document.getElementById("walk-edit-text");
  ta.value = it.comment;
  ta.focus();
}

function walkEditDone() {
  dock.editing = null;
  dockPaint();
  document.getElementById("walk-drawer").focus({ preventScroll: true });
}

async function walkSave() {
  const ed = dock.editing;
  const it = ed && dock.items.find(i => i.key === ed.key);
  if (!it) { walkEditDone(); return; }
  const mine = document.getElementById("walk-edit-text").value;
  const out = walkRebuild(ed.text, mine);
  try {
    const res = await walkPut(dock.taskId, it.name, out, ed.hash, it.eol, it);
    walkAdopt(it, out, res.hash, res.key);
    toast("saved", it.name);
    walkEditDone();
  } catch (e) {
    if (e.status === 409 && e.body && e.body.hash) {
      // Nothing was written. Both sides are kept on screen and it is clint's call which one stands.
      const disk = walkParseFinding(it.name, e.body.text, e.body.hash, it.eol);
      dock.editing = null;
      dock.conflict = { key: it.key, mine, disk: e.body.text, diskHash: e.body.hash, diskComment: disk.comment,
        said: walkTenant.prId ? e.message : "" };
      dockPaint();
      return;
    }
    toast("could not save it", e.message);
  }
}

async function walkKeepMine(it) {
  const c = dock.conflict;
  if (!c) return;
  const out = walkRebuild(c.disk, c.mine);
  try {
    const res = await walkPut(dock.taskId, it.name, out, c.diskHash, it.eol, it);
    walkAdopt(it, out, res.hash, res.key);
    dock.conflict = null;
    toast("kept yours", it.name);
    dockPaint();
  } catch (e) {
    if (e.status === 409 && e.body && e.body.hash) {
      // It moved again while the compare was open. Show the newer disk side rather than overwrite it blind.
      const disk = walkParseFinding(it.name, e.body.text, e.body.hash, it.eol);
      dock.conflict = Object.assign({}, c, { disk: e.body.text, diskHash: e.body.hash, diskComment: disk.comment });
      dockPaint();
      return;
    }
    toast("could not save it", e.message);
  }
}

function walkTakeTheirs(it) {
  const c = dock.conflict;
  if (!c) return;
  dock.conflict = null;
  // The daemon's copy becomes ours, so the next write quotes its hash.
  walkAdopt(it, c.disk, c.diskHash);
  dockPaint();
}

// "walk done" types the words into the walker's terminal and submits them, which is the existing brief's way to
// end a walk. A leak that is neither posted nor dismissed asks once first: rule 5 lets clint leave a leak out of the
// comments, and does not let the tab do it for clint.
async function walkDone() {
  const open = dock.items.filter(i => i.leak && i.state !== "posted" && i.state !== "dismissed").map(i => "`" + i.num + "`");
  if (open.length) {
    const who = open.length === 1 ? open[0] + " is a leak and is not posted"
      : open.slice(0, -1).join(", ") + " and " + open[open.length - 1] + " are leaks and are not posted";
    const ok = await askUser({
      title: "finish anyway?",
      body: esc(who).replace(/`([^`]+)`/g, "<code>$1</code>") + ", finish anyway?",
      buttons: [{ label: "not yet", value: null }, { label: "finish anyway", value: true, style: "go" }]
    });
    if (!ok) return;
  }
  walkAsk("walk done", true);
}

// ── the drawer's own wiring ─────────────────────────────

function walkApplyWidth() {
  const d = document.getElementById("walk-drawer");
  if (!d) return;
  let w = 0;
  try { w = Number(localStorage.getItem("atrium.walk.w")) || 0; } catch (e) {}
  if (window.matchMedia("(max-width: 900px)").matches) w = 0;
  d.style.width = w ? w + "px" : "";
}

function walkGripDown(e) {
  const grip = e.currentTarget;
  const d = document.getElementById("walk-drawer");
  if (!d) return;
  const x0 = e.clientX, w0 = d.getBoundingClientRect().width;
  grip.setPointerCapture(e.pointerId);
  const move = ev => {
    const w = Math.max(320, Math.min(w0 + ev.clientX - x0, window.innerWidth - 360));
    d.style.width = w + "px";
  };
  const up = () => {
    grip.removeEventListener("pointermove", move);
    grip.removeEventListener("pointerup", up);
    grip.removeEventListener("pointercancel", up);
    try { localStorage.setItem("atrium.walk.w", String(Math.round(d.getBoundingClientRect().width))); } catch (e2) {}
    if (typeof onTermResize === "function") onTermResize();
  };
  grip.addEventListener("pointermove", move);
  grip.addEventListener("pointerup", up);
  grip.addEventListener("pointercancel", up);
}

(function walkWire() {
  const drawer = document.getElementById("walk-drawer");
  if (!drawer) return;
  drawer.addEventListener("keydown", walkKeydown);
  drawer.addEventListener("click", e => {
    const el = e.target.closest("[data-act],[data-i],[data-more],[data-ev]");
    if (!el || !drawer.contains(el)) return;
    if (el.dataset.i !== undefined) {
      const it = dock.items[Number(el.dataset.i)];
      if (it) dockSelect(it.key, true);
      return;
    }
    if (el.dataset.more) {
      const c = walkTenant.ctx;
      if (c.key !== dock.cur) { c.key = dock.cur; c.above = 3; c.below = 3; }
      c[el.dataset.more] += 5;
      dockPaint();
      return;
    }
    if (el.dataset.ev) {
      if (walkTenant.evOpen.has(dock.cur)) walkTenant.evOpen.delete(dock.cur);
      else walkTenant.evOpen.add(dock.cur);
      dockPaint();
      return;
    }
    const a = el.dataset.act;
    if (a === "save") return void walkSave();
    if (a === "cancel") return walkEditDone();
    walkAct(a);
  });
  // ctrl-s saves and escape leaves, the same reflexes the file editor has.
  drawer.addEventListener("keydown", e => {
    if (e.target && e.target.id === "walk-edit-text") {
      if ((e.ctrlKey || e.metaKey) && e.key === "s") { e.preventDefault(); e.stopPropagation(); walkSave(); }
      else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); walkEditDone(); }
      return;
    }
  });
})();
