// The changes sheet: what a card's worktree changed, read on the phone, and a line of it quoted into the composer. Read only:
// nothing here edits, stages or discards. Design: docs/rnd/changes-view-design.md and docs/backlog/ui/u-new-m-code-review.md.
//
// Two ways in. The chip on a reply that edited files ("3 files edited", from the `edited` count /replies carries, with no git
// number on it) opens THAT turn: one `?turn=<reply at>` call, made when the chip is tapped. The card's "changes" button opens
// the card's own, uncommitted by default with a toggle for since base. Both draw the same two screens, the file list and one
// file's hunks, and both read through `GET /v1/tasks/{id}/changes`, whose answer says what it cut and what it may have missed.
//
// BACK is history, as the file viewer's is: the chip pushes one entry (the list), a file pushes a second (its hunks), "All files"
// is history.back(), and back from the list returns to the thread, which was never touched, at the scroll it had. The sheet
// leaves the composer showing, so a tapped line becomes a chip in the box the operator writes in.
//
// Refreshed when it opens and when the card's turn ends, never on a timer. While the card is running the sheet says so.
(function () {
  "use strict";

  const U = window.mUtil;
  const HUNK_SHOW = 40;   // a hunk longer than this is folded with a "show full hunk" under it
  const PARTIAL = "may miss changes made by commands (sed, generate, checkout)";

  let els = null;
  let view = null;       // { card, kind: "turn" | "card", at, against, data, err, screen, file, seq, sel: Set, sig }

  const el = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; };
  const base = p => String(p).split("/").pop();
  const hhmm = at => { const d = new Date(at); return isNaN(d) ? "" : String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0"); };
  const plural = (n, w) => n + " " + w + (n === 1 ? "" : "s");

  // ── reading ──────────────────────────────────────────────────────────────
  function path(v) {
    return "/v1/tasks/" + encodeURIComponent(v.card) + "/changes" +
      (v.kind === "turn" ? "?turn=" + encodeURIComponent(v.at) : "?against=" + v.against);
  }

  async function load() {
    const v = view;
    if (!v) return;
    const mine = ++v.seq;
    v.loading = true;
    draw();
    try {
      const data = await window.mNet.api(path(v));
      if (view !== v || mine !== v.seq) return;
      v.data = data && typeof data === "object" ? data : { files: [] };
      v.err = "";
    } catch (e) {
      if (view !== v || mine !== v.seq) return;
      // The 404 for a directory that is not a git worktree has its own sentence, and it is shown, never an empty diff.
      v.data = null;
      v.err = e && e.message ? String(e.message) : "could not read the changes";
      v.errStatus = e && e.status || 0;
    }
    v.loading = false;
    // A file that was open may be gone from the new answer.
    if (v.screen === "file" && !fileOf(v)) v.screen = "list";
    draw();
  }

  const fileOf = v => v && v.data && (v.data.files || []).find(f => f.path === v.file) || null;

  // ── the unified diff text, as rows ───────────────────────────────────────
  // Hunks are `@@ -a,b +c,d @@ section` and then lines led by a space, + or -. Anything before the first hunk header (the
  // diff --git, index, --- and +++ lines) is not a hunk and is skipped. The number shown is the new one on + and context lines
  // and the old one on - lines.
  function parseHunks(text) {
    const out = [];
    let h = null, o = 0, n = 0;
    String(text || "").replace(/\r/g, "").split("\n").forEach(line => {
      const m = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@\s?(.*)$/.exec(line);
      if (m) { h = { head: line, section: m[3] || "", rows: [] }; out.push(h); o = +m[1]; n = +m[2]; return; }
      if (!h || line === "") return;
      const k = line[0];
      if (k === "\\") return;                       // "\ No newline at end of file"
      if (k === "+") h.rows.push({ k: "+", no: n++, text: line.slice(1) });
      else if (k === "-") h.rows.push({ k: "-", no: o++, text: line.slice(1) });
      else if (k === " ") h.rows.push({ k: " ", no: n, text: line.slice(1) }), o++, n++;
    });
    return out.filter(x => x.rows.length);
  }

  // The part of a changed line that is the change, for a - line directly followed by one + line: what differs between the common
  // start and the common end is marked.
  function pairMarks(rows) {
    for (let i = 0; i + 1 < rows.length; i++) {
      const a = rows[i], b = rows[i + 1];
      if (a.k !== "-" || b.k !== "+" || (rows[i + 2] && rows[i + 2].k === "+") || (rows[i - 1] && rows[i - 1].k === "-") || a.text === b.text) continue;
      let s = 0;
      while (s < a.text.length && s < b.text.length && a.text[s] === b.text[s]) s++;
      let e = 0;
      while (e < a.text.length - s && e < b.text.length - s && a.text[a.text.length - 1 - e] === b.text[b.text.length - 1 - e]) e++;
      a.mark = [s, a.text.length - e];
      b.mark = [s, b.text.length - e];
    }
  }

  function textEl(row) {
    const code = el("code", "cg-t");
    if (row.mark && row.mark[1] > row.mark[0]) {
      code.append(row.text.slice(0, row.mark[0]));
      code.appendChild(el("mark", "", row.text.slice(row.mark[0], row.mark[1])));
      code.append(row.text.slice(row.mark[1]));
    } else code.textContent = row.text === "" ? " " : row.text;
    return code;
  }

  // ── the screens ──────────────────────────────────────────────────────────
  function cmtWhere(v) { return v.kind === "turn" ? "turn " + hhmm(v.at) : v.against === "base" ? "since base" : "uncommitted"; }
  const headSha = v => String((v.data && v.data.head) || "").slice(0, 7) || "unknown";
  const keyOf = (p, k, no) => p + "\u0000" + k + "\u0000" + no;

  function note(text, cls) { return el("p", "cg-note" + (cls ? " " + cls : ""), text); }

  function titleOf(v) { return v.kind === "turn" ? "Turn " + hhmm(v.at) : "Changes"; }

  function totals(files) {
    return files.reduce((t, f) => ({ a: t.a + (f.added || 0), r: t.r + (f.removed || 0) }), { a: 0, r: 0 });
  }

  function draw() {
    if (!els || !view) return;
    const v = view;
    els.title.textContent = v.screen === "file" ? base(v.file) : titleOf(v);
    const body = el("div", "cg-body");
    const card = window.mStore.card(v.card);
    if (v.loading && !v.data) body.appendChild(note("loading"));
    else if (v.err) body.appendChild(note(v.err, "cg-err"));
    else if (v.screen === "file" && fileOf(v)) drawFile(body, v, fileOf(v), card);
    else drawList(body, v, card);
    els.body.replaceChildren(body);
  }

  function drawList(body, v, card) {
    const d = v.data || { files: [] };
    const files = d.files || [];
    const t = totals(files);
    const head = el("div", "cg-head");
    head.appendChild(el("div", "cg-sub", plural(files.length, "file") + (v.kind === "turn" ? " edited" : " changed") + (files.length ? " +" + t.a + " -" + t.r : "")));
    if (v.kind === "card") {
      const tog = el("div", "cg-toggle");
      [["head", "uncommitted"], ["base", "since base"]].forEach(([val, label]) => {
        const b = el("button", "", label);
        b.type = "button";
        b.setAttribute("aria-pressed", String(v.against === val));
        b.addEventListener("click", () => { if (v.against !== val) { v.against = val; v.data = null; load(); } });
        tog.appendChild(b);
      });
      head.appendChild(tog);
    }
    body.appendChild(head);
    // Said once, at the top, and not on the chip: the agent is still going, or a turn's list may be missing command changes.
    if (card && card.status === "running") body.appendChild(note("the agent is still working"));
    if (v.kind === "turn" && d.partial) body.appendChild(note(PARTIAL, "cg-grey"));
    if (d.note) body.appendChild(note(d.note, "cg-grey"));
    if (d.outside > 0) body.appendChild(note(plural(d.outside, "edit") + " outside this card's folder " + (d.outside === 1 ? "is" : "are") + " not shown", "cg-grey"));
    if (d.cut && d.cut.files > 0) body.appendChild(note(plural(d.cut.files, "more file") + " not listed" + (d.cut.why ? " (" + d.cut.why + ")" : ""), "cg-grey"));
    if (d.cut && d.cut.hunks > 0) body.appendChild(note(plural(d.cut.hunks, "file") + " show counts only" + (d.cut.why ? " (" + d.cut.why + ")" : ""), "cg-grey"));
    if (!files.length) { body.appendChild(note("no changes")); return; }
    const max = Math.max(1, ...files.map(f => (f.added || 0) + (f.removed || 0)));
    const list = el("div", "cg-files");
    files.forEach(f => {
      const row = el("button", "cg-file");
      row.type = "button";
      row.dataset.path = f.path;
      row.appendChild(el("span", "cg-path", f.path));
      row.appendChild(el("span", "cg-stat", f.status === "binary" ? "binary" : "+" + (f.added || 0) + " -" + (f.removed || 0)));
      row.appendChild(el("span", "cg-status", f.status + (f.old_path ? " from " + f.old_path : "")));
      const bar = el("span", "cg-bar");
      const w = ((f.added || 0) + (f.removed || 0)) / max * 100;
      const add = el("i", "cg-add"), del = el("i", "cg-del");
      const tot = (f.added || 0) + (f.removed || 0);
      add.style.width = (tot ? w * (f.added || 0) / tot : 0) + "%";
      del.style.width = (tot ? w * (f.removed || 0) / tot : f.status === "binary" ? 8 : 0) + "%";
      bar.append(add, del);
      row.appendChild(bar);
      row.addEventListener("click", () => openFile(f.path));
      list.appendChild(row);
    });
    body.appendChild(list);
  }

  function drawFile(body, v, f) {
    const head = el("div", "cg-head");
    head.appendChild(el("div", "cg-fpath", f.path));
    const bk = el("button", "cg-all", "‹ All files");
    bk.type = "button";
    bk.addEventListener("click", () => history.back());
    head.appendChild(bk);
    body.appendChild(head);
    // `cumulative` belongs to this file only: its numbers hold more than this turn.
    if (v.kind === "turn" && f.cumulative) body.appendChild(note("all uncommitted edits to this file, not only this turn", "cg-grey"));
    const openBtn = label => {
      const b = el("button", "cg-open", label);
      b.type = "button";
      b.addEventListener("click", () => { if (window.mViewer) window.mViewer.open(v.card, f.path); });
      return b;
    };
    const exists = f.status !== "deleted";
    if (f.status === "binary") {
      body.appendChild(note("binary, not shown"));
      if (exists) body.appendChild(openBtn("open"));
      return;
    }
    const hunks = parseHunks(f.hunks);
    if (f.hunks_cut || (!hunks.length && ((f.added || 0) + (f.removed || 0)) > 0)) {
      body.appendChild(note("+" + (f.added || 0) + " -" + (f.removed || 0) + ", too large to show here"));
      if (exists) body.appendChild(openBtn("open the file"));
      return;
    }
    if (!hunks.length) { body.appendChild(note("nothing to show for this file")); return; }
    body.appendChild(note("tap a line to quote it", "cg-grey"));
    hunks.forEach(h => body.appendChild(hunkEl(v, f, h)));
  }

  function hunkEl(v, f, h) {
    const box = el("div", "cg-hunk");
    const top = el("div", "cg-hh");
    top.append(el("span", "", "hunk"), el("span", "cg-hr", h.head.replace(/\s+@@.*$/, " @@")));
    box.appendChild(top);
    pairMarks(h.rows);
    const rows = el("div", "cg-rows");
    const rowEl = r => {
      const row = el("div", "cg-row cg-" + (r.k === "+" ? "add" : r.k === "-" ? "del" : "ctx"));
      row.setAttribute("role", "button");
      row.tabIndex = 0;
      row.dataset.key = keyOf(f.path, r.k, r.no);
      row.append(el("span", "cg-ln", String(r.no)), el("span", "cg-sg", r.k === " " ? "" : r.k), textEl(r));
      if (v.sel.has(row.dataset.key)) row.classList.add("on");
      row.addEventListener("click", () => comment(v, f, r, row));
      return row;
    };
    const fold = h.rows.length > HUNK_SHOW;
    h.rows.slice(0, fold ? HUNK_SHOW : h.rows.length).forEach(r => rows.appendChild(rowEl(r)));
    box.appendChild(rows);
    if (fold) {
      const more = el("button", "cg-more", "Show full hunk (" + (h.rows.length - HUNK_SHOW) + " more lines)");
      more.type = "button";
      more.addEventListener("click", () => { h.rows.slice(HUNK_SHOW).forEach(r => rows.appendChild(rowEl(r))); more.remove(); });
      box.appendChild(more);
    }
    return box;
  }

  // A tapped line goes to the composer as a chip, and the line shows it. Tapping it again takes the chip off.
  function comment(v, f, r, row) {
    if (!window.mCompose || !window.mCompose.addComment) return;
    const ok = window.mCompose.addComment({ path: f.path, line: r.no, kind: r.k, text: r.text, where: cmtWhere(v), head: headSha(v) });
    if (!ok) return;
    const k = row.dataset.key;
    if (v.sel.has(k)) { v.sel.delete(k); row.classList.remove("on"); }
    else { v.sel.add(k); row.classList.add("on"); }
  }

  // ── history ──────────────────────────────────────────────────────────────
  function show() {
    els.sheet.hidden = false;
    document.body.classList.add("changes-open");
    fit();
  }

  function hide() {
    if (view) view.seq++;
    view = null;
    if (!els) return;
    els.sheet.hidden = true;
    els.body.textContent = "";
    document.body.classList.remove("changes-open");
  }

  function begin(v) {
    view = Object.assign({ seq: 0, screen: "list", file: "", sel: new Set(), data: null, err: "", loading: false, sig: "" }, v);
    const c = window.mStore.card(v.card);
    view.sig = sigOf(c);
    try { history.pushState({ mcard: v.card, mchg: "list" }, "", location.href); } catch (e) {}
    show();
    els.scroll.scrollTop = 0;
    load();
  }

  // The chip on a reply: that turn's files, in one call made now.
  function openTurn(card, at) { if (els && card && at) begin({ card, kind: "turn", at }); }
  // The card's own changes, uncommitted unless asked for since base.
  function openCard(card) { if (els && card) begin({ card, kind: "card", against: "head" }); }

  function openFile(p) {
    if (!view) return;
    view.screen = "file";
    view.file = p;
    try { history.pushState({ mcard: view.card, mchg: "file", mfile: p }, "", location.href); } catch (e) {}
    els.scroll.scrollTop = 0;
    draw();
  }

  // The sheet ends where the composer begins, so the composer stays in reach for the chips.
  function fit() {
    const c = document.getElementById("m-compose");
    if (els && c) els.sheet.style.bottom = c.offsetHeight + "px";
  }

  const sigOf = c => c ? (c.status || "") + "|" + ((c.seen && c.seen.turn_ended_at) || "") : "";

  // The card's Stop: its turn ended, so what it changed is read again. Not before, and never on a timer.
  function onCards() {
    if (!els || els.sheet.hidden || !view) return;
    const c = window.mStore.card(view.card);
    const sig = sigOf(c);
    if (sig === view.sig) return;
    const was = view.sig;
    view.sig = sig;
    if (c && c.status !== "running") load();
    else if (was !== sig) draw();
  }

  function init() {
    els = { sheet: document.getElementById("m-changes"), title: document.getElementById("m-changes-title"),
      scroll: document.getElementById("m-changes-scroll"), body: document.getElementById("m-changes-body"),
      back: document.getElementById("m-changes-back") };
    if (!els.sheet) { els = null; return; }
    els.back.addEventListener("click", () => history.back());
    window.addEventListener("popstate", e => {
      if (els.sheet.hidden || !view) return;
      const s = e.state || {};
      // The file viewer opened over this sheet keeps its mchg, so a state with one stays.
      if (!s.mchg) { if (!s.mview) hide(); return; }
      if (s.mchg === "list" && view.screen !== "list") { view.screen = "list"; draw(); }
      else if (s.mchg === "file" && s.mfile && (view.screen !== "file" || view.file !== s.mfile)) { view.screen = "file"; view.file = s.mfile; draw(); }
    });
    // A chip taken off in the composer, or the message sent, clears the line it marked.
    window.addEventListener("m-comment-removed", e => {
      if (!view) return;
      const k = e.detail && e.detail.key;
      if (k && view.sel.delete(k)) els.body.querySelectorAll(".cg-row").forEach(r => { if (r.dataset.key === k) r.classList.remove("on"); });
    });
    window.addEventListener("m-send", e => {
      if (!view || !e.detail || e.detail.state !== "pending") return;
      view.sel.clear();
      els.body.querySelectorAll(".cg-row.on").forEach(r => r.classList.remove("on"));
    });
    const compose = document.getElementById("m-compose");
    if (compose && window.ResizeObserver) new ResizeObserver(fit).observe(compose);
    window.addEventListener("resize", fit);
    window.mStore.on("cards", onCards);
    if (window.mCard && window.mCard.bindPinch) window.mCard.bindPinch(els.scroll);
  }

  window.mChanges = { init, openTurn, openCard, reset: () => { if (els && !els.sheet.hidden) hide(); }, isOpen: () => !!els && !els.sheet.hidden };
})();
