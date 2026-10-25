// Fixtures for the items tab, shaped like the design's section 7 views, for building the board before the hub has L2 to L5. ONE
// FILE, SWAPPABLE: delete it once the hub answers (js/items-data.js reads `window.itemsMock` only when the URL asks, `?itemsmock=1`,
// for that load alone, and a fixed note says so).
//
// The rows are the three mockups of docs/rnd/img/item-lifecycle/ (list, detail, blocked). Those mockups give two item rows
// different totals from the same item's detail, so the list totals here are the list mockup's and the detail's are the sum of
// its steps. A real hub answers one set. State lives in this page only: an answer unblocks the item, a cancel finishes it.
(function () {
  "use strict";

  const now = () => Date.now();
  const ago = m => new Date(now() - m * 60000).toISOString();
  const k = n => n * 1000;
  const tok = (i, cw, cr, o) => ({ in: k(i), cw: k(cw), cr: k(cr), out: k(o) });
  const sum = t => t ? t.in + t.cw + t.cr + t.out : 0;
  const clock = iso => { const d = new Date(iso); return String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0"); };

  const PRICES = [{ k: "sonnet-5-5 input / output", v: "$2 / $10" }, { k: "cache write 1h / read", v: "$4 / $0.10" }];
  const TABLE = "v3, list prices";

  function blank() {
    return ["designed", "approved", "building", "tested", "reviewed", "landed", "deployed", "pushed"].map(name => ({
      name, state: "pending", from: null, to: null, when: "", cards: [], sha: "", strength: "", fact: "", reason: "", usd: null, tokens: null }));
  }
  // steps(done, spec): the first `done` steps are done with the given overrides, then the rest as given in `rest`.
  function steps(over) {
    const s = blank();
    for (const [i, o] of Object.entries(over)) Object.assign(s[i], o);
    return s;
  }
  const who = (name, role, coord) => ({ name, role, coord: !!coord });
  const done = (from, to, o) => Object.assign({ state: "done", from: ago(from), to: ago(to) }, o);

  // A row for the list: the steps are only as detailed as the list needs.
  function row(o) {
    return Object.assign({ owner_kind: "session", owner: "orchestrator", created_by: "orchestrator", room: "sg4", body: "", ask: null,
      coordination: [], prices: PRICES, price_table: TABLE, revision: 2, generation: 1, note: "" }, o);
  }

  const seed = () => [
    row({ id: "rd-item-lifecycle", title: "Items as the unit of work: step timeline, tokens and cost", priority: "high", dept: "rnd", state: "blocked",
      owner_kind: "human", owner: "clint", card: "c-designer-3", cards: [{ name: "designer-3", id: "c-designer-3", room: "sg4" }],
      created_at: ago(130), updated_at: ago(12),
      body: "One backlog item shown from designed to pushed: its steps, the cards on it and what each cost.",
      ask: { who: "approval", kind: "approval", sha: "e3a91c0", text: "The design is ready. Approve it with the open questions in section 11, or send it back?", since: ago(12),
        options: [] },
      steps: steps({ 0: done(130, 25, { cards: [who("designer-3", "designer")], sha: "e3a91c0", strength: "reported", fact: "docs/rnd/item-lifecycle-design.md",
          usd: 1.84, tokens: tok(38, 11, 330, 33) }),
        1: { state: "blocked", from: ago(25), cards: [who("clint", "human")], fact: "waiting on your approval", usd: 0, tokens: null } }),
      tokens: tok(38, 11, 330, 33), usd: 1.84, coord_usd: 0.21 }),

    row({ id: "f-ptyhost-reconnect", title: "Pty host: reattach terminals after a room restart", priority: "high", dept: "fabric", state: "blocked",
      owner_kind: "human", owner: "clint", card: "c-builder-ptyhost",
      cards: [{ name: "builder-ptyhost", id: "c-builder-ptyhost", room: "sg4" }, { name: "reviewer-1", id: "c-reviewer-1", room: "sg4" }],
      created_at: ago(302), updated_at: ago(41),
      body: "After a room restart the pty host should hand the same terminals back. Today the room starts a fresh host when the old pipe is gone.",
      ask: { who: "builder-ptyhost", since: ago(41), brief: "Windows named pipes cannot be reopened by name after the host exits. Fall back to a fresh host (loses scrollback), or keep a stub pipe alive?",
        text: "Windows named pipes cannot be reopened by name after the host exits. Fall back to a fresh host (terminals lose their scrollback), or keep a tiny stub process that holds the pipe open (one more process to supervise)?",
        options: ["Fresh host, accept the loss", "Keep a stub pipe", "Ask me later"] },
      coordination: [{ name: "relay-1", note: "relay turns about it", usd: 0.31 }, { name: "orchestrator", note: "turns about it", usd: 0 }],
      steps: steps({ 0: done(258, 190, { cards: [who("designer-4", "designer")], sha: "a47be19", strength: "reported", fact: "docs/rnd/ptyhost-reattach.md",
          usd: 0.52, tokens: tok(14, 2, 150, 11) }),
        1: done(190, 184, { cards: [who("clint", "human")], strength: "observed", fact: "approved at a47be19", usd: 0, tokens: null }),
        2: { state: "blocked", from: ago(184), cards: [who("builder-ptyhost", "builder"), who("relay-1", "minion relay", true)], sha: "5d0c2f8",
          strength: "observed", fact: "2 commits so far on claude/ptyhost-reconnect", usd: 1.94, tokens: tok(52, 8, 910, 47) },
        3: { fact: "the room runs the touched tests at the building tip" }, 4: { fact: "a reviewer takes the branch" },
        5: { fact: "fast-forward onto claude/main" }, 6: { fact: "rooms and build" }, 7: { fact: "origin/main" } }),
      tokens: tok(120, 0, 2100, 81), usd: 7.40, coord_usd: 0.66 }),

    row({ id: "f-room-handoff", title: "Room handoff: move a card between rooms with its folder", priority: "high", dept: "fabric", state: "running",
      card: "c-builder-handoff", cards: [{ name: "designer-1", id: "c-designer-1", room: "sg4" }, { name: "builder-handoff", id: "c-builder-handoff", room: "sg4" }],
      created_at: ago(1560), updated_at: ago(40),
      steps: steps({ 0: done(1560, 1300, { cards: [who("designer-1", "designer")], usd: 1.1, tokens: tok(20, 3, 300, 18) }),
        1: done(1300, 1290, { cards: [who("clint", "human")], usd: 0 }),
        2: { state: "active", from: ago(184), cards: [who("builder-handoff", "builder")], usd: 5.02, tokens: tok(76, 12, 1400, 56) } }),
      tokens: tok(96, 0, 1700, 74), usd: 6.12, coord_usd: 0.48 }),

    row({ id: "u-chat-tab", title: "Chat tab: terminals list beside the phone conversation view", priority: "medium", dept: "ui", state: "running",
      card: "c-builder-chat", cards: [{ name: "builder-chat", id: "c-builder-chat", room: "sg4" }, { name: "reviewer-2", id: "c-reviewer-2", room: "sg4" }],
      created_at: ago(400), updated_at: ago(6),
      steps: steps({ 0: done(400, 380, { usd: 0.4 }), 1: done(380, 372, { usd: 0 }), 2: done(372, 90, { usd: 1.6 }), 3: done(90, 70, { usd: 0.3 }),
        4: done(70, 6, { usd: 0.77 }), 5: { state: "active", from: ago(6), usd: null } }),
      tokens: tok(51, 0, 890, 45), usd: 3.07, coord_usd: 0.19 }),

    row({ id: "r-usage-export", title: "Usage export per card and turn for the hub", priority: "medium", dept: "rnd", state: "running",
      card: "c-builder-usage", cards: [{ name: "builder-usage", id: "c-builder-usage", room: "sg4" }],
      created_at: ago(1925), updated_at: ago(30),
      steps: steps({ 0: done(1520, 1500, {}), 1: done(1500, 1490, {}), 2: done(1490, 300, {}), 3: done(300, 280, {}), 4: done(280, 200, {}), 5: done(200, 190, {}),
        6: { state: "active", from: ago(190), note: "deployed to 2 of 3 rooms" } }),
      tokens: tok(33, 0, 575, 32), usd: 2.11, coord_usd: 0.12 }),

    row({ id: "f-hub-forge-push", title: "Hub forge: push claude/main to origin from the hub", priority: "medium", dept: "fabric", state: "queued",
      cards: [], created_at: ago(20), updated_at: ago(20), steps: steps({ 0: { state: "active", from: ago(20) } }),
      tokens: null, usd: 0, coord_usd: null }),

    row({ id: "u-items-tab", title: "Items tab on the hub board: grouped list, answer, cancel", priority: "high", dept: "ui", state: "finished",
      card: "c-builder-items", cards: [{ name: "builder-items", id: "c-builder-items", room: "sg4" }],
      created_at: ago(1200), updated_at: ago(60), note: "done: c19f7aa",
      body: "Show every item the hub holds, grouped by state, with an answer box for the ones blocked on the human and a cancel for the ones running. Title and body come from the backlog item.",
      coordination: [{ name: "relay-2", note: "relay turns about it", usd: 0.31 }, { name: "orchestrator", note: "turns about it", usd: 0.27 }],
      steps: steps({ 0: done(318, 219, { cards: [who("designer-3", "designer")], sha: "e3a91c0", strength: "reported", fact: "docs/rnd/item-lifecycle-design.md",
          usd: 0.71, tokens: tok(18, 3, 201, 14) }),
        1: done(219, 202, { cards: [who("clint", "human")], strength: "observed", fact: "approved at e3a91c0, no questions open", usd: 0, tokens: null }),
        2: done(202, 60, { cards: [who("builder-items", "builder"), who("relay-2", "minion relay", true)], sha: "7b02d4e", strength: "observed",
          fact: "4 commits on claude/items-tab", usd: 2.38, tokens: tok(41, 9, 622, 40) }),
        3: done(60, 49, { cards: [who("builder-items", "builder")], sha: "7b02d4e", strength: "observed", fact: "scripts/test-touched.sh passed, 0 commits since",
          usd: 0.42, tokens: tok(9, 2, 141, 6) }),
        4: done(49, 15, { cards: [who("reviewer-2", "reviewer")], sha: "7b02d4e", strength: "observed", fact: "doc-ok, 2 findings, both fixed",
          usd: 0.96, tokens: tok(22, 6, 338, 19) }),
        5: done(15, 8, { cards: [who("merger-1", "merger")], sha: "c19f7aa", strength: "observed", fact: "claude/main fast-forwarded",
          usd: 0.11, tokens: tok(3, 1, 40, 2) }),
        6: { state: "active", from: ago(8), note: "deployed to 2 of 3 rooms", when: "rooms 2 of 3 · since " + clock(ago(8)), sha: "c19f7aa", strength: "observed",
          fact: "sg3 and m1mini on build 0.9.4-c19f7aa, sg4 pending" },
        7: { fact: "origin/main has not reached c19f7aa" } }),
      tokens: tok(60, 0, 1000, 48), usd: 3.55, coord_usd: 0.25 }),
  ];

  // The detail mockup is a running item at deploy and the list mockup has the same title finished, so the one the detail shows
  // is kept out of the list (`detailOnly`) and the list carries the finished rows below, which are the list mockup's.
  const items = seed();
  const u = items.find(r => r.id === "u-items-tab");
  u.state = "running";
  u.detailOnly = true;

  const finished = [
    row({ id: "u-items-tab-v1", title: "Items tab on the hub board: grouped list, answer, cancel", priority: "medium", dept: "ui", state: "finished",
      card: "c-builder-items-1", cards: [{ name: "builder-items", id: "c-builder-items-1", room: "sg4" }], created_at: ago(900), updated_at: ago(60),
      steps: steps({}).map(s => Object.assign(s, { state: "done" })), tokens: tok(60, 0, 1000, 48), usd: 3.55, coord_usd: 0.25 }),
    row({ id: "r-new-item-deps", title: "Items can depend on other items", priority: "medium", dept: "rnd", state: "finished", card: "c-builder-deps",
      cards: [{ name: "builder-deps", id: "c-builder-deps", room: "sg4" }], created_at: ago(2400), updated_at: ago(1440),
      steps: steps({}).map(s => Object.assign(s, { state: "done" })), tokens: tok(40, 0, 650, 30), usd: 2.30, coord_usd: 0.14 }),
  ];
  // The list mockup's first finished row is u-items-tab itself, so the list carries the finished one under another id with the
  // backlog id shown: one title, two ids, which a real hub never answers.
  finished[0].id = "u-items-tab-done"; finished[0].backlog = "u-items-tab";
  finished[1].backlog = "r-new-item-deps";
  const rows = items.concat(finished);

  const shown = () => rows.filter(r => !r.detailOnly);
  const stats = () => ({
    needs_you: shown().filter(r => r.state === "blocked" && r.owner_kind === "human").length, who: "clint",
    in_flight: shown().filter(r => r.state === "running" || r.state === "blocked").length, working: 3,
    spend_today: 14.6, spend_coord: 1.4, landed_week: 9, landed_median: "7h 20m", landed_cost: 3.10 });

  // The efficacy page's fixture: the efficacy-1400.png mockup as the hub's EffReport (internal/link/efficacy_report.go) would answer it.
  // `observedOnly` drops the reported inputs, so the bugs row falls to a dash and the rest say observed.
  function efficacy(q) {
    const week = !q || q.by !== "day";
    const obs = !!(q && q.observedOnly);
    const series = {
      clean_pass: [.4, .5, .45, .55, .5, .6, .57, .71], first_pass: [.6, .7, .65, .7, .8, .75, .77, .86], send_backs: [.9, .8, .85, .7, .75, .6, .65, .45],
      red_after: [.1, .2, .1, .15, .1, .08, .08, .3], bugs: [.3, .3, .2, .35, .3, .3, .25, .3], time_to_land: [28, 25, 26, 23, 25, 21, 21, 18].map(h => h * 1000),
      cost: [1.6, 1.8, 1.6, 1.9, 1.8, 1.6, 1.8, 3.1], waste: [.5, .4, .8, .3, .5, .3, .4, 1.9] };
    const unit = { clean_pass: "pct", first_pass: "pct", send_backs: "per_item", red_after: "count", bugs: "per_item", time_to_land: "seconds", cost: "usd", waste: "usd" };
    const cur = { clean_pass: [5, 7], first_pass: [6, 7], send_backs: [0, 7], red_after: [1, 9], bugs: [2, 7], time_to_land: [0, 9], cost: [0, 9], waste: [2, 0] };
    const parts = { send_backs: { design: .3, review: .4, not_accepted: .2 }, bugs: { bugs: 2, reverts: 0 }, time_to_land: { wait: 7500 },
      cost: { coord: .5, share: .16 }, waste: { share: .09 } };
    const periods = series.clean_pass.map((_, i) => {
      const last = i === 7;
      const figures = {};
      for (const key of Object.keys(series)) {
        const reported = key === "bugs" && !obs;
        const hide = key === "bugs" && obs;
        const [num, den] = last ? cur[key] : [0, 6 + (i % 2)];
        figures[key] = { key, unit: unit[key], value: hide ? null : series[key][i], num: last ? num : 0, den: last ? den : den, greyed: false,
          source: reported ? "reported" : "observed", reported_n: reported && last ? 2 : 0, parts: last ? parts[key] : undefined };
      }
      return { key: "p" + i, start: ago((7 - i) * 10080), end: ago((6 - i) * 10080), partial: last, landed: last ? 9 : 6 + (i % 2), too_few: false, figures };
    });
    const trend = (verdict, dir, delta, was) => ({ verdict, dir, delta, text: "", was });
    const trends = {
      clean_pass: trend("better", "up", .14), first_pass: trend("better", "up", .09), send_backs: trend("better", "down", -.4),
      red_after: trend("worse", "up", 1, { num: 0, den: 6 }), bugs: obs ? trend("none", "", null) : trend("same", "flat", 0),
      time_to_land: trend("better", "down", -4200), cost: trend("same", "up", .12, { value: 2.98 }), waste: trend("worse", "up", 1.3, { value: .6 }) };
    const row = (id, title, dept, tone, text, rank, sb, secs, usd, reported) => ({ id, title, dept, landed_at: ago(rank * 300), mark: { kind: tone, text, tone, rank },
      send_backs: sb, total_seconds: secs, usd, reported: !!reported });
    return {
      by: week ? "week" : "day", observed_only: obs, now: ago(0), zone: "UTC", periods, compare: periods[6], trends,
      wrong: { red: 1, bugs: obs ? 0 : 2, reverts: 0, total: obs ? 1 : 3, was: 0, reported: obs ? 0 : 2, source: obs ? "observed" : "reported" },
      send_backs_by_step: { designed: 3, building: 2, tested: 0, reviewed: 4, landed: 0 },
      time: { median: 26400, work: 14700, wait: 7500, ci: 4200, p90_step: "reviewed", p90: 13200, long_wait_step: "approved", long_wait: 19200, long_wait_item: "u-items-tab" },
      abandoned: [{ id: "f-room-ssh-wizard", title: "", reason: "dropped", usd: 1.10 }, { id: "u-pulls-sort", title: "", reason: "dropped", usd: .80 }], abandoned_total: 1.90,
      landed: [
        row("u-items-tab", "Items tab on the hub board: grouped list, answer, cancel", "ui", "good", "clean pass", 1, "0 send-backs", 18600, 2.40),
        row("f-link-restart", "Hub proxy: pass the room link through a restart", "fabric", "watch", "watching, 4 days left", 2, "1 review send-back", 34800, 4.90),
        row("u-chat-tab", "Chat tab: conversation fills the pane", "ui", "bad", "CI red after landing, 22m", 3, "2 send-backs", 21900, 2.80),
        row("u-backlog-owner", "Backlog filter by owner", "ui", "bad", "bug filed 2d after landing", 4, "first test failed", 12600, 1.20, true),
        row("f-usage-cursor", "Usage export since cursor", "fabric", "good", "clean pass", 5, "0 send-backs", 15300, 1.70)].filter(r => !(obs && r.reported)),
      estimated: true, notes: obs ? ["Reported inputs are left out."] : ["Reported inputs: bug links until a bug is filed through --item."] };
  }

  const copy = r => JSON.parse(JSON.stringify(r));

  window.itemsMock = {
    async efficacy(q) { return efficacy(q); },
    async list() { return { items: rows.filter(r => !r.detailOnly).map(copy), stats: stats() }; },
    async detail(id) {
      const r = rows.find(x => x.id === id);
      if (!r) throw new Error("no such item");
      const d = copy(r);
      // The detail's own totals are its steps', since that is what it draws.
      d.tokens = d.steps.reduce((a, s) => s.tokens ? { in: a.in + s.tokens.in, cw: a.cw + s.tokens.cw, cr: a.cr + s.tokens.cr, out: a.out + s.tokens.out } : a,
        { in: 0, cw: 0, cr: 0, out: 0 });
      if (!sum(d.tokens)) d.tokens = null;
      d.usd = d.steps.reduce((a, s) => a + (s.usd || 0), 0);
      d.coord_usd = d.coordination.reduce((a, c) => a + c.usd, 0);
      return d;
    },
    async act(id, body) {
      const r = rows.find(x => x.id === id);
      if (!r) throw new Error("no such item");
      if (body.do === "unblock") { r.state = "running"; r.ask = null; r.steps.forEach(s => { if (s.state === "blocked") s.state = "active"; }); }
      else if (body.do === "approve") {
        const a = r.steps[1], b = r.steps[2];
        a.state = "done"; a.to = ago(0); a.strength = "observed"; a.fact = "approved at " + ((r.ask && r.ask.sha) || "the design sha");
        b.state = "active"; b.from = ago(0);
        r.state = "running"; r.ask = null;
      } else if (body.do === "send_back") {
        if (!body.note) throw new Error("a design is sent back with a note, which is typed to the designer");
        const a = r.steps[1], d = r.steps[0];
        a.state = "pending"; a.from = null; a.fact = "";
        d.state = "active"; d.to = null; d.fact = "sent back: " + body.note;
        r.state = "running"; r.ask = null;
      }
      else if (body.do === "cancel") { r.state = "finished"; r.note = "cancelled by owner"; }
      r.updated_at = ago(0);
      return { ok: true };
    }
  };
})();
