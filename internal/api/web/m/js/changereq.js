// Change requests on the phone (/m): a sheet with two screens, the list and one request's page, like the desktop board's
// Requests view (js/changereq.js) and the same data, through js/changereq-core.js (which answers from js/changereq-mock.js only
// when asked). docs/fabric/hub-forge-design.md section 6. Stage 5 is MANUAL: a request into `main` waits for the orchestrator or
// clint to merge on the hub's side, and this sheet only RECORDS that it happened. It has no merge button and no word that says
// it merges.
//
// A DOOR ONLY WHERE THERE IS A HUB THAT ANSWERS. The "requests" button in the header stays hidden until the list answers once;
// a room's own board, or a hub too old for the routes, never shows it, and a hub that goes away later shows its own sentence
// inside the sheet. History is the sheet's back: opening it pushes the list, a request pushes a second entry, back pops one.
// EVERYTHING FROM THE HUB IS TEXT: title, why, note and branch names are set with textContent, never as markup.
(function () {
  "use strict";

  const C = window.crCore;
  if (!C) return;
  const U = () => window.mUtil || { ago: () => "", ts: x => Date.parse(x) || 0 };

  let els = null;
  let avail = null;
  const st = { tab: "open", reqs: [], loaded: false, note: "", sel: "", detail: {}, screen: "list", form: null, act: "", draft: { sha: "", note: "" }, err: "", busy: false, repos: null };

  const el = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; };
  const btn = (label, fn, cls) => { const b = el("button", "crm-btn" + (cls ? " " + cls : ""), label); b.type = "button"; if (fn) b.addEventListener("click", fn); return b; };
  const age = at => { const a = U().ago(Date.now() - U().ts(at)); return a === "now" ? "just now" : a ? a + " ago" : ""; };
  const hash = s => { let h = 2166136261 >>> 0; for (const ch of String(s)) h = Math.imul(h ^ ch.charCodeAt(0), 16777619) >>> 0; return h; };
  const accent = s => "crm-a" + (hash(s) % 5);
  const avatar = room => el("span", "crm-av " + accent("room" + room), String(room || "?").slice(0, 2));
  const pill = (label, tone) => { const p = el("span", "crm-pill", label); p.style.setProperty("--c", "var(--" + tone + ")"); return p; };
  const openCount = () => st.reqs.filter(C.isOpen).length;

  // ---- reading -----------------------------------------------------------------------------------------------------
  async function load() {
    const r = await C.api.list({ state: "all" });
    if (r.ok && r.body && Array.isArray(r.body.requests)) { st.reqs = r.body.requests; st.note = ""; st.loaded = true; }
    else {
      st.loaded = false;
      st.note = r.offline ? "The hub is not answering, so there is nothing to show until it is back."
        : r.status === 404 ? "This hub does not have change requests yet. It may be too old." : "The hub would not list change requests: " + C.errText(r);
    }
    badge();
    return st.loaded;
  }
  async function detail(id) {
    const r = await C.api.get(id);
    st.detail[id] = r.ok && r.body && r.body.id === id ? r.body : { error: r.offline ? "the hub is not answering" : C.errText(r) };
  }
  function badge() {
    const b = document.getElementById("m-req-btn");
    if (!b) return;
    const n = st.reqs.filter(C.needsOperator).length;
    b.textContent = n ? "requests " : "requests";
    if (n) { const em = el("em", "", String(n)); em.setAttribute("aria-label", n + " need the orchestrator or clint"); b.appendChild(em); }
  }

  // ---- the sheet ---------------------------------------------------------------------------------------------------
  function build() {
    if (els) return els;
    const sheet = el("section", "crsheet");
    sheet.id = "m-requests"; sheet.hidden = true; sheet.setAttribute("aria-label", "change requests");
    try { const v = parseFloat(localStorage.getItem("atrium.mfs")); if (v) sheet.style.setProperty("--m-fs", v + "px"); } catch (e) {}
    const top = el("div", "crm-top");
    const back = el("button", "crm-back"); back.type = "button"; back.setAttribute("aria-label", "back");
    back.innerHTML = '<svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 5l-7 7 7 7"/></svg>';
    const title = el("span", "crm-title");
    top.append(back, title);
    const scroll = el("div", "crm-scroll"), body = el("div", "crm-body");
    scroll.appendChild(body);
    sheet.append(top, scroll);
    document.body.appendChild(sheet);
    back.addEventListener("click", close);
    els = { sheet, title, scroll, body };
    return els;
  }

  function go(s, replace) {
    build();
    try { (replace ? history.replaceState : history.pushState).call(history, { mcr: s }, "", location.href); } catch (e) {}
    render(s);
  }
  function render(s) {
    build();
    st.screen = s.v; if (s.id) st.sel = s.id;
    els.sheet.hidden = false;
    document.body.classList.add("crm-open");
    draw(true);
  }
  function hide() { if (!els) return; els.sheet.hidden = true; els.body.textContent = ""; document.body.classList.remove("crm-open"); st.form = null; st.err = ""; }
  function close() {
    if (!els || els.sheet.hidden) return;
    // The open form is a screen of its own with no history entry: back from it is the list again.
    if (st.form) { st.form = null; draw(true); return; }
    if (history.state && history.state.mcr) history.back(); else hide();
  }

  async function openList() {
    if (!st.loaded) await load();
    go({ v: "list" });
    if (!st.loaded) return;
  }
  async function openPage(id) {
    st.act = ""; st.err = ""; st.draft = { sha: "", note: "" }; st.form = null;
    go({ v: "page", id });
    if (!st.detail[id]) { await detail(id); if (st.sel === id && st.screen === "page") draw(false); }
  }
  async function refresh(id) { await load(); if (id) await detail(id); draw(false); }

  // ---- drawing -----------------------------------------------------------------------------------------------------
  function draw(top) {
    if (!els || els.sheet.hidden) return;
    const body = el("div", "crm-view");
    let title = "Change requests";
    if (st.form) { title = "Open a request"; body.appendChild(drawForm()); }
    else if (st.screen === "page") { const r = st.reqs.find(x => x.id === st.sel); title = r ? r.id : "Request"; body.appendChild(r ? drawPage(r) : note("This request is not on the hub any more.")); }
    else body.appendChild(drawList());
    els.title.textContent = title;
    const keep = els.scroll.scrollTop;
    els.body.replaceChildren(body);
    els.scroll.scrollTop = top ? 0 : keep;
  }
  const note = (t, cls) => el("p", "crm-note" + (cls ? " " + cls : ""), t);

  function drawRow(r) {
    const b = el("button", "crm-li " + accent("room" + C.sourceRoom(r)));
    b.type = "button"; b.dataset.id = r.id;
    const t = el("span", "crm-lit");
    t.append(el("span", "crm-lt", r.title), el("span", "crm-lm", r.source.branch + " → " + r.target.branch + " · " + age(r.created_at)));
    b.append(avatar(C.sourceRoom(r)), t);
    if (C.needsOperator(r)) b.appendChild(el("span", "crm-needs", "needs you")); else b.appendChild(pill(C.STATE[r.state] || r.state, C.STATE_TONE[r.state] || "dim"));
    b.addEventListener("click", () => openPage(r.id));
    return b;
  }

  function drawList() {
    const root = el("div", "crm-list");
    if (st.note && !st.loaded) { root.append(note(st.note, "crm-warn"), btn("Try again", async () => { await load(); draw(false); })); return root; }
    const bar = el("div", "crm-bar");
    if (!C.state.readOnly) bar.appendChild(btn("Open a request", startForm, "go"));
    if (bar.childNodes.length) root.appendChild(bar);
    if (!st.reqs.length) {
      const e = el("div", "crm-empty");
      e.append(el("h2", "", "Nothing is waiting to be merged."), note("A room asks for its finished branch to go into main or another branch, and the request lands here with its change record."));
      if (C.state.readOnly) e.appendChild(note(C.readOnlyLine, "crm-ro"));
      root.appendChild(e);
      return root;
    }
    // One card per open request into main, above everything else.
    st.reqs.filter(C.needsOperator).forEach(r => {
      const h = el("article", "crm-hero " + accent("room" + C.sourceRoom(r)));
      const lane = el("div", "crm-lane2");
      lane.append(avatar(C.sourceRoom(r)), el("span", "crm-br", r.source.branch), el("span", "crm-arrow", "→"), el("span", "crm-br", "main"));
      const foot = el("div", "crm-hfoot");
      foot.append(el("span", "crm-quiet", r.id + " · " + age(r.created_at)), btn("Open", () => openPage(r.id)));
      h.append(el("span", "crm-k", "Needs the orchestrator or clint"), el("h3", "", r.title), lane, foot);
      root.appendChild(h);
    });
    const seg = el("div", "crm-seg"); seg.setAttribute("role", "group"); seg.setAttribute("aria-label", "which requests");
    [["open", "Open", openCount()], ["closed", "Closed", st.reqs.length - openCount()]].forEach(([k, label, n]) => {
      const b = el("button", "", label + " "); b.type = "button"; b.setAttribute("aria-pressed", String(st.tab === k)); b.appendChild(el("b", "", String(n)));
      b.addEventListener("click", () => { st.tab = k; draw(false); });
      seg.appendChild(b);
    });
    root.appendChild(seg);
    const rs = st.reqs.filter(r => st.tab === "open" ? C.isOpen(r) : !C.isOpen(r));
    if (!rs.length) root.appendChild(note(st.tab === "open" ? "Nothing is open." : "Nothing has been closed yet."));
    C.byTarget(rs).forEach(g => {
      const h = el("div", "crm-tg"); h.append(el("span", "", "into " + g.target), el("b", "", String(g.reqs.length)));
      root.appendChild(h);
      g.reqs.forEach(r => root.appendChild(drawRow(r)));
    });
    return root;
  }

  const kv = rows => { const d = el("dl", "crm-kv"); rows.forEach(([k, v]) => { d.append(el("dt", "", k), el("dd", "", v)); }); return d; };
  const panel = (h, ...kids) => { const p = el("section", "crm-panel"); p.appendChild(el("h3", "", h)); kids.forEach(k => p.appendChild(k)); return p; };

  function drawPage(r) {
    const d = st.detail[r.id], p = d && d.pushed;
    const root = el("div", "crm-page " + accent("room" + C.sourceRoom(r)));
    root.appendChild(el("div", "crm-crumb", C.repoShort(r.repo) + " · " + age(r.created_at)));
    const lane = el("div", "crm-lane");
    const from = el("div", "crm-end"); from.append(el("small", "", "from"));
    const who = el("span", "crm-who"); who.append(avatar(C.sourceRoom(r)), el("span", "crm-br", r.source.branch)); from.appendChild(who);
    const to = el("div", "crm-end"); to.append(el("small", "", "into"), el("span", "crm-br", r.target.branch));
    lane.append(from, el("span", "crm-down", "↓"), to);
    root.appendChild(lane);
    const badges = el("div", "crm-badges");
    badges.appendChild(pill(C.STATE[r.state] || r.state, C.STATE_TONE[r.state] || "dim"));
    if (p) badges.appendChild(pill(C.PUSHED_SHORT[p.state] || p.state, C.PUSHED_TONE[p.state] || "dim"));
    root.append(badges, el("h2", "crm-ttl", r.title));
    if (!C.isOpen(r)) {
      const e = el("section", "crm-ended " + r.state);
      const by = r.closed_by ? (r.closed_by.card === "operator" ? "the operator" : ((r.closed_by.room || "") + " " + (r.closed_by.card || "")).trim()) : "";
      e.appendChild(el("b", "", r.state === "merged" ? "Recorded as merged" + (r.merged_sha ? " at " + C.sha7(r.merged_sha) : "") : r.state === "withdrawn" ? "Withdrawn" : "Closed"));
      e.appendChild(el("span", "", (by ? "by " + by + " " : "") + age(r.closed_at)));
      if (r.note) e.appendChild(el("q", "crm-quote", r.note));
      root.appendChild(e);
    } else root.appendChild(drawGate(r));
    root.appendChild(panel("Why", r.why ? el("p", "crm-why", r.why) : note("No reason was given.")));
    // On the hub: the push log's answer, or why there is none.
    let hub;
    if (d && d.error) hub = [note("The hub did not say where this branch is: " + d.error + ".")];
    else if (!d) hub = [note("Reading the push log.")];
    else if (p.state === "not-pushed") hub = [el("p", "crm-line", C.pushedLine(p, r.source.branch)), note(C.notPushedHow(r.source.branch))];
    else hub = [el("p", "crm-line", C.pushedLine(p, r.source.branch)), kv([["hub head", C.sha7(p.hub_sha)]].concat(r.source.sha ? [["request", C.sha7(r.source.sha)]] : [], p.room ? [["pushed by", p.room + (p.card ? " " + p.card : "")]] : [], p.at ? [["pushed", age(p.at)]] : []))];
    root.appendChild(panel("On the hub", ...hub));
    const lines = r.change ? C.recordLines(r) : null;
    root.appendChild(panel("Change record", !r.change ? note("No change record is attached to this request.") : el("p", "crm-quiet", "Attached: " + r.change),
      ...(r.change ? [lines ? kv(Object.entries(lines).concat([["Pushed", d && d.pushed ? C.pushedLine(d.pushed, r.source.branch) : "reading the push log"]])) : note("The hub does not serve the record's four lines to the board yet. The Pushed line is above.")] : [])));
    const tl = el("ol", "crm-tl");
    C.timeline(r, p).forEach(e => {
      const li = el("li", e.kind);
      const t = el("span", "", ""); if (e.who) t.appendChild(el("b", "", e.who + " ")); t.appendChild(document.createTextNode(e.text + " · " + age(e.at)));
      li.appendChild(t); if (e.note) li.appendChild(el("q", "crm-quote", e.note));
      tl.appendChild(li);
    });
    root.appendChild(panel("History", tl));
    return root;
  }

  function drawGate(r) {
    const main = C.needsOperator(r);
    const g = el("section", "crm-gate" + (main ? " main" : ""));
    g.appendChild(el("b", "", main ? "Needs the orchestrator or clint" : "Waiting to be merged"));
    g.appendChild(el("span", "", main ? "A request into main is merged on the hub's side for now. Nothing on the board does it." : "The board does not merge. Merge it on the hub's side, then record it here."));
    const steps = el("ol", "crm-steps");
    ["Merge it on the hub's side", "Re-sign and push", "Record the sha here"].forEach((t, i) => { const li = el("li"); li.append(el("b", "", String(i + 1)), document.createTextNode(t)); steps.appendChild(li); });
    g.appendChild(steps);
    if (C.state.readOnly) { g.appendChild(el("p", "crm-ro", C.readOnlyLine)); return g; }
    const f = el("div", "crm-field");
    const lab = el("label", "", "Merged at (sha)"); lab.htmlFor = "crm-sha";
    const row = el("div", "crm-row");
    const inp = el("input", "crm-in mono"); inp.id = "crm-sha"; inp.value = st.draft.sha; inp.placeholder = "full sha, 40 hex"; inp.maxLength = 64; inp.autocomplete = "off"; inp.spellcheck = false; inp.setAttribute("aria-describedby", "crm-shahint");
    inp.setAttribute("autocapitalize", "off");
    const go = btn("Record that it was merged", () => doAct(r.id, { do: "merged", sha: st.draft.sha.trim() }), "go");
    const hint = el("small", "crm-hint", "Only records it. The board does not merge anything."); hint.id = "crm-shahint";
    const sync = () => { const v = inp.value.trim(), ok = C.isSha(v); go.disabled = !ok || st.busy; hint.classList.toggle("bad", !!v && !ok); hint.textContent = v && !ok ? C.SHA_HINT : "Only records it. The board does not merge anything."; };
    inp.addEventListener("input", () => { st.draft.sha = inp.value; sync(); });
    sync();
    row.append(inp, go); f.append(lab, row, hint); g.appendChild(f);
    if (st.err) { const e = el("p", "crm-err", st.err); e.setAttribute("role", "alert"); g.appendChild(e); }
    const acts = el("div", "crm-acts");
    acts.appendChild(btn("Withdraw", () => doAct(r.id, { do: "withdraw" })));
    if (st.act !== "close") acts.appendChild(btn("Close with a note", () => { st.act = "close"; draw(false); const n = document.getElementById("crm-note"); if (n) n.focus(); }));
    g.appendChild(acts);
    if (st.act === "close") {
      const cf = el("div", "crm-field"), l2 = el("label", "", "Why close it (optional)"); l2.htmlFor = "crm-note";
      const n = el("input", "crm-in"); n.id = "crm-note"; n.value = st.draft.note; n.maxLength = 300; n.autocomplete = "off";
      n.addEventListener("input", () => { st.draft.note = n.value; });
      const r2 = el("div", "crm-row"); r2.append(n, btn("Close request", () => doAct(r.id, { do: "close", note: st.draft.note.trim() })), btn("Cancel", () => { st.act = ""; draw(false); }, "quiet"));
      cf.append(l2, r2); g.appendChild(cf);
    }
    return g;
  }

  async function doAct(id, body) {
    if (st.busy) return;
    st.busy = true; st.err = ""; draw(false);
    const r = await C.api.act(id, body);
    st.busy = false;
    if (r.ok) { st.act = ""; st.draft = { sha: "", note: "" }; delete st.detail[id]; await refresh(id); return; }
    st.err = C.state.readOnly ? C.readOnlyLine : r.offline ? "The hub is not answering. Nothing was changed." : C.errText(r);
    draw(false);
  }

  // ---- opening one -------------------------------------------------------------------------------------------------
  async function startForm() {
    if (!st.repos) {
      try { const r = await fetch("/_hub/git/repos"); const b = r.ok ? await r.json() : null; st.repos = (b && b.repos) || []; } catch (e) { st.repos = []; }
    }
    const first = st.repos[0];
    st.form = { repo: first ? (first.host || "github") + "/" + first.owner + "/" + first.repo : "github/", branch: "", target: "main", title: "", why: "", err: "" };
    draw(true);
  }
  function drawForm() {
    const f = st.form, root = el("div", "crm-form");
    root.appendChild(note("Asks for a branch on the hub to go into another. The board does not merge it: the orchestrator or clint does, on the hub's side."));
    const keyOf = r => (r.host || "github") + "/" + r.owner + "/" + r.repo;
    const branches = () => { const r = (st.repos || []).find(x => keyOf(x) === f.repo); return r ? (r.branches || []).map(b => b.name) : []; };
    const field = (id, label, node) => { const l = el("label", "", label); l.htmlFor = id; node.id = id; root.append(l, node); return node; };
    const bind = (n, k) => { n.addEventListener("input", () => { f[k] = n.value; }); return n; };
    const dl = el("datalist"); dl.id = "crm-dl"; branches().concat(["main"]).forEach(b => { const o = el("option"); o.value = b; dl.appendChild(o); });
    if ((st.repos || []).length > 1) {
      const sel = el("select", "crm-in"); st.repos.forEach(r => { const o = el("option", "", keyOf(r)); o.selected = keyOf(r) === f.repo; sel.appendChild(o); });
      sel.addEventListener("change", () => { f.repo = sel.value; draw(false); });
      field("crm-f-repo", "Repo", sel);
    } else field("crm-f-repo", "Repo", bind(Object.assign(el("input", "crm-in mono"), { value: f.repo }), "repo"));
    const br = bind(Object.assign(el("input", "crm-in mono"), { value: f.branch }), "branch"); br.setAttribute("list", "crm-dl"); br.autocomplete = "off"; br.spellcheck = false;
    field("crm-f-branch", "Branch to send", br);
    const tg = bind(Object.assign(el("input", "crm-in mono"), { value: f.target }), "target"); tg.setAttribute("list", "crm-dl"); tg.autocomplete = "off"; tg.spellcheck = false;
    field("crm-f-target", "Into", tg);
    field("crm-f-title", "Title", bind(Object.assign(el("input", "crm-in"), { value: f.title, maxLength: 200 }), "title"));
    const why = bind(Object.assign(el("textarea", "crm-in"), { value: f.why, rows: 4, maxLength: 2000 }), "why");
    field("crm-f-why", "Why", why);
    root.appendChild(dl);
    if (f.err) { const e = el("p", "crm-err", f.err); e.setAttribute("role", "alert"); root.appendChild(e); }
    const acts = el("div", "crm-acts");
    acts.append(btn("Open request", submitForm, "go"), btn("Cancel", () => { st.form = null; draw(true); }, "quiet"));
    root.appendChild(acts);
    return root;
  }
  async function submitForm() {
    const f = st.form, title = f.title.trim(), branch = f.branch.trim(), target = f.target.trim();
    if (!branch || !target || !title) { f.err = "A request needs the branch to send, where it goes and a title."; draw(false); return; }
    const r = await C.api.create({ repo: C.canonRepo(f.repo), source: { branch }, target: { branch: target }, title, why: f.why.trim() });
    if (r.ok || (r.status === 409 && r.body && r.body.id)) {
      st.form = null; await load();
      const id = r.body.id; st.tab = "open"; delete st.detail[id];
      await detail(id); st.sel = id; st.screen = "page"; try { history.pushState({ mcr: { v: "page", id } }, "", location.href); } catch (e) {} draw(true);
      if (r.status === 409) { st.err = "An open request for this branch and target already exists: it is shown here."; draw(false); }
      return;
    }
    f.err = C.state.readOnly ? C.readOnlyLine : r.offline ? "The hub is not answering. Nothing was opened." : r.status === 404 ? "The hub has no branch called " + branch + ". " + C.notPushedHow(branch) : C.errText(r);
    draw(false);
  }

  // ---- the door ----------------------------------------------------------------------------------------------------
  async function init() {
    const door = document.getElementById("m-req-btn");
    window.addEventListener("popstate", e => {
      if (!els || els.sheet.hidden) { if (e.state && e.state.mcr && avail) render(e.state.mcr); return; }
      if (e.state && e.state.mcr) render(e.state.mcr); else hide();
    });
    C.onReadOnly(() => draw(false));
    if (door) door.addEventListener("click", openList);
    avail = await load();
    if (door) door.hidden = !avail;
  }
  // The hub says `change-request` on /v1/events; the store passes it here. Read again once the list has been read at all.
  let evT = 0;
  function onEvent() {
    if (!st.loaded) return;
    clearTimeout(evT);
    evT = setTimeout(async () => { await load(); if (st.screen === "page" && st.sel) await detail(st.sel); draw(false); }, 300);
  }
  window.mRequests = { init, openList, openPage, onEvent, available: () => avail === true, state: st };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init); else init();
})();
