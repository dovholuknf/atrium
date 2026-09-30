// The phone page's data: the cards and the pending permissions, kept current from the same API and event stream the
// board reads. Nothing here loads the board's scripts, so a desktop change cannot break the phone.
//
// `window.mStore` is the contract with compose.js and perms.js: cards, perms and on, plus the growlers that
// growl.js reads. Everything else this page needs from the network lives on `window.mNet`.
//
// EVENT DRIVEN. The stream says what changed and only that is re-read. The one poll is a safety resync of 60s while
// the page is visible, for what a stream that dropped without closing cannot say.
(function () {
  "use strict";

  const ROOM_KEY = "atrium.room"; // js/rooms.js, the room the operator scoped the board to
  const RESYNC_MS = (window.__mResyncMs) || 60000;
  const BACKOFF = [1000, 2000, 4000, 8000, 15000, 30000];

  const cards = new Map();
  let perms = [];
  let live = false;
  let loaded = false;
  let hub = false;
  let rooms = [];
  // The hub's growlers, whole set each time. Nothing is fetched: the hub says the set when a stream opens and on
  // every change, and a page with no hub never hears one.
  let growls = [];
  let growlsRemind = [];
  const subs = { cards: new Set(), perms: new Set(), live: new Set(), rooms: new Set(), growls: new Set() };

  function roomNow() {
    try { return localStorage.getItem(ROOM_KEY) || ""; } catch (e) { return ""; }
  }

  // The board's fetch wrapper puts the room it is scoped to in one header, and this page reads the same choice.
  function api(path, opts) {
    opts = Object.assign({}, opts);
    const room = roomNow();
    const headers = new Headers(opts.headers || {});
    if (room && !headers.get("X-Atrium-Room")) headers.set("X-Atrium-Room", room);
    if (opts.body && !headers.get("Content-Type")) headers.set("Content-Type", "application/json");
    opts.headers = headers;
    return fetch(path, opts).then(async res => {
      if (res.status === 401) throw new Error("sign in again");
      if (!res.ok && res.status !== 204) {
        let msg = "";
        try { msg = await res.text(); } catch (e) {}
        try { msg = JSON.parse(msg).error || msg; } catch (e) {}
        const err = new Error(String(msg).trim() || res.statusText || ("HTTP " + res.status));
        err.status = res.status;
        throw err;
      }
      if (res.status === 204) return null;
      const ct = res.headers.get("Content-Type") || "";
      return ct.indexOf("json") >= 0 ? res.json() : res.text();
    });
  }

  // Copied from js/notify.js (bareId, roomOf): the aggregate view tags an id as `room~id` only while more than one
  // room is attached.
  function bareId(id) {
    const s = String(id);
    const i = s.indexOf("~");
    return i > 0 ? s.slice(i + 1) : s;
  }
  function roomOf(id) {
    const s = String(id);
    const i = s.indexOf("~");
    return i > 0 ? s.slice(0, i) : "";
  }

  // Changes are announced once per turn of the event loop, however many arrived together.
  const dirty = { cards: false, perms: false, live: false, rooms: false, growls: false };
  let flushing = false;
  function mark(kind) {
    dirty[kind] = true;
    if (flushing) return;
    flushing = true;
    queueMicrotask(() => {
      flushing = false;
      for (const k of Object.keys(dirty)) {
        if (!dirty[k]) continue;
        dirty[k] = false;
        subs[k].forEach(fn => { try { fn(); } catch (e) { console.error(e); } });
      }
    });
  }

  // ── reads ────────────────────────────────────────────────────────────────
  let tasksTimer = 0, tasksBusy = false, tasksAgain = false;
  function tasksSoon(ms) {
    clearTimeout(tasksTimer);
    tasksTimer = setTimeout(loadTasks, ms == null ? 150 : ms);
  }
  async function loadTasks() {
    if (tasksBusy) { tasksAgain = true; return; }
    tasksBusy = true;
    try {
      const r = await api("/v1/tasks");
      const next = new Map();
      ((r && r.tasks) || []).forEach(t => { if (t && t.id) next.set(t.id, t); });
      cards.clear();
      next.forEach((v, k) => cards.set(k, v));
      loaded = true;
      mark("cards");
    } catch (e) { /* keep what is held. the next event or resync tries again */ }
    tasksBusy = false;
    if (tasksAgain) { tasksAgain = false; tasksSoon(); }
  }

  let permsTimer = 0, permsBusy = false, permsAgain = false;
  function permsSoon(ms) {
    clearTimeout(permsTimer);
    permsTimer = setTimeout(loadPerms, ms == null ? 150 : ms);
  }
  async function loadPerms() {
    if (permsBusy) { permsAgain = true; return; }
    permsBusy = true;
    try {
      const r = await api("/v1/permissions");
      perms = (r && r.permissions) || [];
      mark("perms");
    } catch (e) {}
    permsBusy = false;
    if (permsAgain) { permsAgain = false; permsSoon(); }
  }

  async function loadRooms() {
    try {
      const res = await fetch("/_hub/rooms", { headers: { Accept: "application/json" } });
      if (!res.ok) throw new Error("not a hub");
      const got = await res.json();
      hub = true;
      rooms = ((got && got.rooms) || []).map(r => r.name).filter(Boolean).sort();
    } catch (e) {
      hub = false;
      rooms = [];
    }
    mark("rooms");
  }

  // ── the skin ─────────────────────────────────────────────────────────────
  // The remembered one is applied by the page head before the first paint. This is the daemon's answer, which wins,
  // the same rule as `applyResolvedSkin` in js/notes-files.js.
  async function loadSkin() {
    try {
      const s = await api("/v1/settings");
      const names = (s && s.board_skins) || [];
      const now = (s && s.board_skin) || names[0] || "";
      const root = document.documentElement;
      const key = "atrium.skin" + (roomNow() ? "." + roomNow() : "");
      if (now && names.length && now !== names[0]) {
        root.setAttribute("data-skin", now);
        try { localStorage.setItem(key, now); } catch (e) {}
      } else {
        root.removeAttribute("data-skin");
        try { localStorage.removeItem(key); } catch (e) {}
      }
      window.dispatchEvent(new Event("m-skin"));
    } catch (e) {}
  }

  // ── the stream ───────────────────────────────────────────────────────────
  let es = null, attempt = 0, retryTimer = 0;
  function eventsURL() {
    if (!hub) return "/v1/events";
    const room = roomNow();
    return room ? "/v1/events/room/" + encodeURIComponent(room) : "/v1/events/hub";
  }
  function setLive(v) {
    if (live === v) return;
    live = v;
    mark("live");
  }
  function onTask(e) {
    let d = null;
    try { d = JSON.parse(e.data); } catch (err) {}
    // A row the daemon marks whole (`row: 1`) is put in place. Anything else is a reason to read again, because a
    // partial row over a whole one would strip the asks and the seen state from a card.
    if (d && d.row === 1 && d.id) {
      cards.set(d.id, d);
      mark("cards");
      return;
    }
    tasksSoon();
  }
  function onRemoved(e) {
    let d = {};
    try { d = JSON.parse(e.data) || {}; } catch (err) {}
    if (d.id) {
      if (cards.delete(d.id)) mark("cards");
      return;
    }
    tasksSoon();
  }
  function connect() {
    clearTimeout(retryTimer);
    if (es) { try { es.close(); } catch (e) {} es = null; }
    const src = new EventSource(eventsURL());
    es = src;
    src.onopen = () => {
      attempt = 0;
      setLive(true);
      // The stream coming back is the first sign the daemon is back, and whatever this page held was decided by a
      // daemon that may not be the one running now.
      tasksSoon(0);
      permsSoon(0);
      loadSkin();
    };
    src.onerror = () => {
      setLive(false);
      // Reopened here, with a backoff, rather than left to the browser's fixed retry, so the live dot tells the truth
      // about a drop and a long outage does not hammer the hub.
      try { src.close(); } catch (e) {}
      if (es === src) es = null;
      const wait = BACKOFF[Math.min(attempt, BACKOFF.length - 1)] * (0.8 + Math.random() * 0.4);
      attempt++;
      clearTimeout(retryTimer);
      retryTimer = setTimeout(connect, wait);
    };
    src.addEventListener("task", onTask);
    src.addEventListener("task-removed", onRemoved);
    src.addEventListener("permission", () => permsSoon());
    src.addEventListener("rooms", () => { loadRooms(); tasksSoon(); });
    src.addEventListener("growls", e => {
      let d = null;
      try { d = JSON.parse(e.data); } catch (err) { return; }
      if (!d || !Array.isArray(d.growls)) return;
      growls = d.growls;
      growlsRemind = Array.isArray(d.remind) ? d.remind : [];
      mark("growls");
    });
  }

  // The safety resync, and a page coming back to the front reads everything again.
  setInterval(() => {
    if (document.visibilityState !== "visible") return;
    tasksSoon(0);
    permsSoon(0);
  }, RESYNC_MS);
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState !== "visible") return;
    if (!es) { attempt = 0; connect(); }
    tasksSoon(0);
    permsSoon(0);
  });
  window.addEventListener("online", () => { if (!live) { attempt = 0; connect(); } });

  function on(kind, fn) {
    const set = subs[kind];
    if (!set) throw new Error("mStore.on: unknown kind " + kind);
    set.add(fn);
    return () => set.delete(fn);
  }

  window.mStore = {
    cards() { return Array.from(cards.values()); },
    card(id) {
      if (cards.has(id)) return cards.get(id);
      // The same card under a different tag: one room attached is bare, two is `room~id`.
      const bare = bareId(id);
      for (const [k, v] of cards) if (bareId(k) === bare) return v;
      return null;
    },
    perms() { return perms.slice(); },
    // Open and snoozed rows in the hub's order, and the ids a reminder tick named.
    growls() { return growls.slice(); },
    remind() { return growlsRemind.slice(); },
    // After the hub answered a dismiss or an undo, so the strip moves before the next event confirms it.
    setGrowl(row) {
      if (!row || !row.id) return;
      const i = growls.findIndex(g => g.id === row.id);
      if (i >= 0) growls[i] = row;
      else growls.push(row);
      growls.sort((a, c) => (a.urgency || 9) - (c.urgency || 9) || String(a.raised_at).localeCompare(String(c.raised_at)));
      mark("growls");
    },
    dropGrowl(id) {
      const n = growls.length;
      growls = growls.filter(g => g.id !== id);
      if (growls.length !== n) mark("growls");
    },
    on(kind, fn) { return on(kind, fn); },
  };

  window.mNet = {
    api,
    live() { return live; },
    loaded() { return loaded; },
    hub() { return hub; },
    rooms() { return rooms.slice(); },
    room: roomNow,
    roomOf,
    bareId,
    on(kind, fn) { return on(kind, fn); },
    // The board's own room chip is the reference: with several rooms attached and none chosen, tags are shown.
    manyRooms() { return hub && (roomNow() ? false : rooms.length > 1); },
    start() {
      loadSkin();
      loadRooms().then(connect, connect);
    },
  };
})();
