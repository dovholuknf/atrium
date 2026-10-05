// The home: "needs you", the cards waiting on the operator, oldest wait first, and a switch to every card.
//
// A card needs the operator for one or more REASONS, worked out here from the card JSON and the pending permissions:
//   permission  a request is frozen waiting for an answer      (perms, or status needs-permission)
//   questions   its last turn ended on questions nobody answered (seen.open_questions, asks_open)
//   report      a card the operator launched has reported and he has not touched it since (reported_at, human_at)
//   held        a message for the card is held and not just waiting for the turn (activity.held_peer)
//   unseen      a finished turn nobody has read                  (seen.unseen, see js/seen.js)
//   ready       it is waiting for his next prompt                (status needs-input)
// The row shows the most pressing one and says how many more there are. Its age is the OLDEST wait among them.
(function () {
  "use strict";

  const U = window.mUtil;
  const MODE_KEY = "atrium.m.mode";
  const LEAVE_MS = 320;

  const ICON = {
    permission: '<svg viewBox="0 0 24 24"><path d="M12 3l7 3v5c0 4.5-3 8-7 10-4-2-7-5.5-7-10V6z"/><path d="M9.5 12l2 2 3.5-4"/></svg>',
    questions: '<svg viewBox="0 0 24 24"><path d="M9.2 9a3 3 0 115 2c-.9.7-2.2 1.3-2.2 3"/><circle cx="12" cy="17.6" r=".6"/></svg>',
    report: '<svg viewBox="0 0 24 24"><path d="M7 3h7l4 4v14H7z"/><path d="M14 3v4h4M10 12h5M10 16h5"/></svg>',
    held: '<svg viewBox="0 0 24 24"><rect x="3.5" y="6" width="17" height="12" rx="2"/><path d="M4 8l8 6 8-6"/></svg>',
    unseen: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="4.5" class="fill"/></svg>',
    ready: '<svg viewBox="0 0 24 24"><path d="M6 12h11M13 7l5 5-5 5"/></svg>',
    working: '<svg viewBox="0 0 24 24"><path d="M12 4a8 8 0 108 8" class="spin"/></svg>',
    idle: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3" class="fill"/></svg>',
  };
  // Most pressing first. A frozen agent costs time right now, a finished turn can wait a little.
  const ORDER = ["permission", "questions", "report", "held", "unseen", "ready"];

  function permsFor(card, perms) {
    const bare = window.mNet.bareId(card.id);
    return perms.filter(p => window.mNet.bareId(p.task_id) === bare);
  }

  // What the pending request would run, short enough for a row.
  function permText(p) {
    const cmd = String(p.command || "").split("\n")[0].trim();
    const short = cmd.length > 64 ? cmd.slice(0, 63) + "…" : cmd;
    if (!short) return "wants to use " + U.esc(p.tool || "a tool");
    if (!p.tool || p.tool === "Bash") return "wants to run <code>" + U.esc(short) + "</code>";
    return "wants to use " + U.esc(p.tool) + " <code>" + U.esc(short) + "</code>";
  }

  // The reasons this card needs the operator, each {kind, text (html), since (ms, 0 when unknown)}.
  function reasons(t, perms, now) {
    if (!t || t.archived_at || t.offline) return [];
    if (t.status === "dead" || t.status === "shelved" || t.status === "backlog") return [];
    now = now || Date.now();
    const out = [];
    const mine = permsFor(t, perms || []);
    if (mine.length || t.status === "needs-permission") {
      const first = mine.slice().sort((a, b) => U.ts(a.requested_at) - U.ts(b.requested_at))[0];
      const since = first ? U.ts(first.requested_at) : U.ts(t.waiting_since);
      out.push({
        kind: "permission", since,
        text: mine.length > 1 ? "wants " + mine.length + " permissions" :
          first ? permText(first) : "is waiting for a permission",
      });
    }
    const s = t.seen || {};
    const qs = s.answered === false ? (s.open_questions || []) : [];
    const unparsed = s.answered === false && !!s.questions_unparsed;
    const nq = qs.length || t.asks_open || 0;
    if (nq || unparsed) {
      out.push({
        kind: "questions", since: U.ts(s.questions_at) || U.ts(s.turn_ended_at) || U.ts(t.ask_at),
        text: nq ? "asks " + nq + (nq === 1 ? " question" : " questions") : "asked questions",
      });
    }
    // A report is for the operator only on a card he launched, and only until he has touched it since.
    if (t.spawned_by === "@human" && t.reported_at && U.ts(t.reported_at) > U.ts(t.human_at)) {
      out.push({ kind: "report", since: U.ts(t.reported_at), text: "report waiting" });
    }
    const a = t.activity || {};
    if (a.held_peer && !a.held_quiet && a.held_count > 0) {
      const n = a.held_count;
      out.push({
        kind: "held", since: a.held_seconds ? now - a.held_seconds * 1000 : 0,
        text: n === 1 ? "1 message held for it" : n + " messages held for it",
      });
    }
    if (s.unseen && s.turn_ended_at && !agentIdle(t)) {
      const at = U.ts(s.turn_ended_at);
      const age = U.ago(now - at);
      out.push({ kind: "unseen", since: at,
        text: (age === "now" ? "finished just now" : "finished " + age + " ago") + ", not read" });
    }
    if (t.status === "needs-input" && !out.length && !agentIdle(t)) {
      out.push({
        kind: "ready", since: U.ts(t.waiting_since),
        text: t.waiting_reason === "started" ? "started, waiting for its first prompt" : "waiting for you",
      });
    }
    out.sort((x, y) => ORDER.indexOf(x.kind) - ORDER.indexOf(y.kind));
    return out;
  }

  function needsList(cards, perms, now) {
    const rows = [];
    cards.forEach(t => {
      const rs = reasons(t, perms, now);
      if (!rs.length) return;
      const known = rs.map(r => r.since).filter(Boolean);
      const since = known.length ? Math.min.apply(null, known) : U.ts(t.last_activity_at) || 0;
      rows.push({ card: t, reasons: rs, since });
    });
    // OLDEST WAIT FIRST. A card with no known age goes last rather than jumping the queue.
    rows.sort((a, b) => (a.since || Infinity) - (b.since || Infinity) || String(a.card.id).localeCompare(String(b.card.id)));
    return rows;
  }

  // The device's view of the list: order, grouping and filters, remembered in localStorage. Sorting, grouping and the
  // subagent test are the board's own rules from js/cardrules.js.
  const OPTS_KEY = "atrium.m.homeopts";
  const OPT_DEFAULTS = { order: "newest", orderNeeds: "oldest", group: "none", needsMe: false, hideDone: false, hideSubs: false };
  function loadOpts() {
    try {
      const o = JSON.parse(localStorage.getItem(OPTS_KEY) || "{}");
      return {
        order: o.order === "oldest" ? "oldest" : "newest",
        orderNeeds: o.orderNeeds === "newest" ? "newest" : "oldest",
        group: o.group === "room" || o.group === "project" ? o.group : "none",
        needsMe: o.needsMe === true, hideDone: o.hideDone === true, hideSubs: o.hideSubs === true,
      };
    } catch (e) { return Object.assign({}, OPT_DEFAULTS); }
  }
  let opts = loadOpts();
  function saveOpts() { try { localStorage.setItem(OPTS_KEY, JSON.stringify(opts)); } catch (e) {} }

  // Newest or oldest by last activity, with the board's tie break. Oldest is the whole newest order reversed.
  function sortRows(rows) {
    rows.sort((a, b) => cardActivityCmp(a.card, b.card) || cardTieBreak(a.card, b.card));
    if (opts.order === "oldest") rows.reverse();
    return rows;
  }

  // Each list has its own order and the control shows the one on screen. The all list goes by last activity, newest first
  // unless asked. The needs list is a queue of answers owed and goes by when each wait began, oldest first unless asked.
  const orderKey = () => mode === "needs" ? "orderNeeds" : "order";

  function isFinished(t) { return t.status === "done" || t.status === "dead"; }

  // The filters that apply to a row. Needs-me only is how the needs list is already made, so it changes the all list.
  // Hide subagents never hides a live card that has an alias: somebody named it, so it is not an anonymous helper.
  // The board's isDoer stays the board's. Here a director and the orchestrator are not subagents either, whatever they are
  // tagged, since they are who the operator talks to.
  const NOT_SUB = ["atrium:director", "atrium:orchestrator", "orchestrators", "atrium:hold-notices", "atrium:context-ceiling"];
  function isSub(t) {
    if (!isSubagent(t)) return false;
    if (t.alias && !isFinished(t)) return false;
    return !(t.tags || []).some(x => NOT_SUB.indexOf(String(x).trim().toLowerCase()) >= 0);
  }

  // Whether the filters keep a row. `revealed` is the tap on "hidden by filters", which shows what they hide until the
  // filters change.
  let revealed = false;
  function keepRow(r, forAll) {
    const t = r.card;
    if (forAll && opts.needsMe && !r.reasons.length) return false;
    if (opts.hideDone && isFinished(t)) return false;
    if (opts.hideSubs && isSub(t)) return false;
    return true;
  }

  function groupName(t) {
    if (opts.group === "room") return window.mNet.roomOf(t.id) || t.room || "";
    return cardProjectOf(t);
  }

  // The rows as [{key, label, rows}] sections. No label means no heading.
  function sections(rows, forAll) {
    if (opts.group !== "none") {
      const by = new Map();
      rows.forEach(r => {
        const g = groupName(r.card);
        if (!by.has(g)) by.set(g, []);
        by.get(g).push(r);
      });
      return [...by.keys()].sort(cardGroupCmp).map(g => ({ key: (opts.group === "room" ? "r:" : "p:") + g,
        label: g || (opts.group === "room" ? "no room" : "no project"), rows: by.get(g) }));
    }
    if (!forAll) return [{ key: "", label: "", rows }];
    return GROUPS.map(([g, label]) => ({ key: g, label, rows: rows.filter(r => groupOf(r.card) === g) })).filter(s => s.rows.length);
  }

  // All mode: working, waiting, idle.
  function groupOf(t) {
    if (t.status === "running") return "working";
    if (agentIdle(t)) return "idle";
    if (t.status === "needs-input" || t.status === "needs-permission") return "waiting";
    return "idle";
  }
  const GROUPS = [["working", "Working"], ["waiting", "Waiting"], ["idle", "Idle"]];

  function allList(cards, perms, now) {
    const rows = [];
    cards.forEach(t => { if (!t.archived_at) rows.push({ card: t, reasons: reasons(t, perms, now) }); });
    return rows;
  }

  // ── the view ─────────────────────────────────────────────────────────────
  let mode = "needs";
  try { if (localStorage.getItem(MODE_KEY) === "all") mode = "all"; } catch (e) {}
  const els = {};
  const nodes = new Map(); // key -> element, so a row is reused and never redrawn from nothing

  function q(id) { return document.getElementById(id); }

  function rowHTML(item, forAll, many) {
    const t = item.card;
    const nm = U.cardName(t);
    const r = item.reasons[0];
    const kind = r ? r.kind : forAll ? groupOf(t) : "idle";
    const extra = !openingState(t.id) && item.reasons.length > 1 ? '<span class="more">+' + (item.reasons.length - 1) + "</span>" : "";
    let why;
    const op = openingState(t.id);
    if (op) why = op.state === "slow" ? '<span class="opening slow">' + U.esc(op.why) + "</span>"
      : '<span class="opening"><span class="m-spin" aria-hidden="true"></span>' + openingText + "…</span>";
    else if (r) why = r.text;
    else why = U.esc(U.activityText(t) || U.statusLabel(t));
    const at = item.since || (forAll ? U.ts(t.last_activity_at) : 0);
    const room = many ? window.mNet.roomOf(t.id) || t.room || "" : "";
    return '<button type="button" class="row k-' + kind + '" data-id="' + U.esc(t.id) + '">' +
      '<span class="glyph">' + (ICON[kind] || ICON.idle) + "</span>" +
      '<span class="body"><span class="name"><b>' + U.esc(nm.main) + "</b>" +
      (nm.sub ? "<i>" + U.esc(nm.sub) + "</i>" : "") + '</span><span class="why">' + why + extra + "</span></span>" +
      '<span class="meta">' + (at ? '<span class="age">' + U.ago(Date.now() - at) + "</span>" : "") +
      (room ? '<span class="room">' + U.esc(room) + "</span>" : "") + "</span></button>";
  }

  function makeWrap(key, cls) {
    const w = document.createElement("div");
    w.className = "rw " + cls;
    w.dataset.key = key;
    const inner = document.createElement("div");
    inner.className = "rw-in";
    w.appendChild(inner);
    return w;
  }

  function reconcile(entries) {
    const host = els.list;
    const keep = new Set(entries.map(e => e.key));
    // Leavers slide shut where they are.
    nodes.forEach((el, key) => {
      if (keep.has(key) || el.classList.contains("leaving")) return;
      el.classList.add("leaving");
      const done = () => { el.remove(); if (nodes.get(key) === el) nodes.delete(key); };
      if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) done();
      else setTimeout(done, LEAVE_MS);
    });
    let anchor = null;
    entries.forEach(e => {
      let el = nodes.get(e.key);
      if (el && el.classList.contains("leaving")) {
        // It came back before it finished leaving.
        el.classList.remove("leaving");
      }
      if (!el) {
        el = makeWrap(e.key, e.head ? "head" : "card");
        nodes.set(e.key, el);
        el.classList.add("entering");
        el.addEventListener("animationend", () => el.classList.remove("entering"), { once: true });
      }
      const inner = el.firstChild;
      if (inner.dataset.sig !== e.sig) { inner.innerHTML = e.html; inner.dataset.sig = e.sig; }
      // Placed after the previous entry, and only moved when it is not already there.
      const want = anchor ? anchor.nextSibling : host.firstChild;
      if (el !== want) host.insertBefore(el, want);
      anchor = el;
    });
  }

  function render() {
    if (!els.list) return;
    const net = window.mNet, store = window.mStore;
    const cards = store.cards(), perms = store.perms(), now = Date.now();
    const many = net.manyRooms();
    const needs = needsList(cards, perms, now);
    const all = cards.filter(c => !c.archived_at).length;

    els.h1.textContent = mode === "needs" ? "Needs you" : "Every session";
    els.segNeeds.innerHTML = "needs you <em>" + needs.length + "</em>";
    els.segAll.innerHTML = "all <em>" + all + "</em>";
    els.segNeeds.setAttribute("aria-selected", String(mode === "needs"));
    els.segAll.setAttribute("aria-selected", String(mode === "all"));
    els.seg.dataset.mode = mode;
    paintOpts();

    const loaded = net.loaded();
    els.skel.hidden = loaded;
    const entries = [];
    let hidden = 0;
    if (loaded) {
      const forAll = mode === "all";
      const listed = forAll ? allList(cards, perms, now) : needs;
      const passing = listed.filter(r => keepRow(r, forAll));
      hidden = listed.length - passing.length;
      const kept = revealed ? listed : passing;
      const rows = forAll ? sortRows(kept) : (opts.orderNeeds === "newest" ? kept.slice().reverse() : kept);
      sections(rows, forAll).forEach(s => {
        if (s.label) {
          const html = '<h2 class="grp">' + U.esc(s.label) + " <em>" + s.rows.length + "</em></h2>";
          entries.push({ key: "h:" + s.key, html, sig: html, head: true });
        }
        s.rows.forEach(n => {
          const html = rowHTML(n, forAll, many);
          entries.push({ key: "c:" + n.card.id, html, sig: html });
        });
      });
    }
    reconcile(entries);

    // The designed empty states.
    const none = loaded && entries.length === 0;
    els.empty.hidden = !(none && mode === "needs");
    els.none.hidden = !(none && mode === "all" && all === 0);
    paintChips(hidden);
    if (none && mode === "needs") {
      els.emptyAll.hidden = all === 0;
      els.emptyAll.textContent = all === 1 ? "1 session is working or resting" : all + " sessions are working or resting";
    }
    document.title = (needs.length ? "(" + needs.length + ") " : "") + "atrium";
  }

  // The view control: one button that opens a panel of order, grouping and filters.
  function paintOpts() {
    if (!els.opts) return;
    els.optsBtn.setAttribute("aria-expanded", String(!els.opts.hidden));
    const on = (opts.order !== "newest" ? 1 : 0) + (opts.orderNeeds !== "oldest" ? 1 : 0) + (opts.group !== "none" ? 1 : 0) + (opts.needsMe ? 1 : 0) + (opts.hideDone ? 1 : 0) + (opts.hideSubs ? 1 : 0);
    els.optsBtn.dataset.on = String(on);
    els.opts.querySelectorAll("[data-opt]").forEach(b => {
      const v = b.dataset.val;
      const key = b.dataset.opt === "order" ? orderKey() : b.dataset.opt;
      const cur = v === undefined ? opts[key] : opts[key] === v;
      b.setAttribute("aria-pressed", String(cur));
    });
  }

  // The two things that can make the list look short: the room this page is scoped to, and the filters.
  function paintChips(hidden) {
    const room = window.mNet.room();
    els.room.hidden = !room;
    if (room) els.roomName.textContent = "room: " + room;
    els.hidden.hidden = !hidden;
    if (hidden) {
      els.hidden.textContent = revealed ? "showing " + hidden + " hidden by filters. hide them again" : hidden + " hidden by filters. show";
    }
  }

  function setOpt(k, v) {
    revealed = false;
    opts[k === "order" ? orderKey() : k] = v;
    saveOpts();
    // A different order or grouping is a different arrangement, not a move, so rows are not animated across it.
    nodes.forEach(el => el.remove());
    nodes.clear();
    render();
  }

  // Another window of this browser changed the list's order, grouping, filters or its needs/all switch.
  prefLive([OPTS_KEY, MODE_KEY], () => {
    opts = loadOpts();
    try { mode = localStorage.getItem(MODE_KEY) === "all" ? "all" : "needs"; } catch (e) {}
    nodes.forEach(el => el.remove());
    nodes.clear();
    paintOpts();
    render();
  });

  function setMode(m) {
    if (m === mode) return;
    mode = m;
    try { localStorage.setItem(MODE_KEY, m); } catch (e) {}
    // The two lists are different sets, so they are not animated into each other.
    nodes.forEach(el => el.remove());
    nodes.clear();
    window.scrollTo(0, 0);
    render();
  }

  function init() {
    els.h1 = q("m-h1");
    els.seg = q("m-seg");
    els.segNeeds = q("m-seg-needs");
    els.segAll = q("m-seg-all");
    els.list = q("m-list");
    els.skel = q("m-skel");
    els.empty = q("m-empty");
    els.emptyAll = q("m-empty-all");
    els.none = q("m-none");
    els.room = q("m-room-chip");
    els.roomName = q("m-room-name");
    els.hidden = q("m-hidden");
    q("m-room-all").addEventListener("click", () => window.mNet.clearRoom());
    els.hidden.addEventListener("click", () => { revealed = !revealed; render(); });
    els.opts = q("m-opts");
    els.optsBtn = q("m-opts-btn");
    els.optsBtn.addEventListener("click", () => { els.opts.hidden = !els.opts.hidden; paintOpts(); });
    els.opts.addEventListener("click", e => {
      const b = e.target.closest("[data-opt]");
      if (!b) return;
      const k = b.dataset.opt;
      setOpt(k, b.dataset.val === undefined ? !opts[k] : b.dataset.val);
    });
    els.segNeeds.addEventListener("click", () => setMode("needs"));
    els.segAll.addEventListener("click", () => setMode("all"));
    els.list.addEventListener("click", e => {
      const b = e.target.closest(".row");
      if (b && window.mCard) window.mCard.open(b.dataset.id);
    });
    window.mStore.on("cards", render);
    window.mStore.on("perms", render);
    window.mNet.on("rooms", render);
    // Ages are the one thing that moves with the clock. Repainted, never fetched.
    setInterval(() => { if (document.visibilityState === "visible") render(); }, 30000);
    render();
  }

  window.mHome = { init, render, reasons, needsList, allList, mode: () => mode, setMode, opts: () => Object.assign({}, opts) };
})();
