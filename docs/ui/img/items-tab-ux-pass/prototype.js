// A MOCKUP of the proposed Items tab, drawn over the live tab by shoot.js from the same fixture. Not shipped code: it shows what the
// design in docs/ui/items-tab-ux-pass.md asks for, and its grouping (foldItems) is the logic phase 1 would move into js/items-data.js.
(function () {
  const rows = window.__fx.rows, work = window.__fx.work;
  const tasks = typeof lastTasks === "undefined" ? [] : lastTasks;
  const liveIds = new Set(tasks.filter(t => !t.offline).map(t => t.id));
  const offlineIds = new Set(tasks.filter(t => t.offline).map(t => t.id));
  const STEP = ["designed", "approved", "building", "tested", "reviewed", "landed", "deployed", "pushed"];
  const VERB = { designed: "designing", approved: "waiting for approval", building: "building", tested: "testing", reviewed: "in review", landed: "landing", deployed: "deploying", pushed: "waiting to push" };
  // The steps a CARD does. approved is the human's, deployed and pushed have no card (lifecycle design section 3).
  const CARD_STEP = new Set(["designed", "building", "tested", "reviewed", "landed"]);
  const handle = a => { const t = tasks.find(x => x.id === a.card); return (t && t.wire_name) || (a.card || "").replace(/^c-/, ""); };

  // ONE ROW PER BACKLOG ID. The newest live assignment drives the row's state and its question.
  function foldItems() {
    const by = {};
    for (const r of rows) (by[r.work_id] = by[r.work_id] || []).push(r);
    return Object.entries(by).map(([id, as]) => {
      const w = work.find(x => x.backlog_id === id) || { steps: [], title: id };
      as.sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at));
      const open = as.filter(a => a.state !== "finished");
      const live = open.filter(a => a.card && liveIds.has(a.card));
      const unknown = open.filter(a => a.card && offlineIds.has(a.card));
      const driver = live[0] || unknown[0] || open[0] || as[0];
      const apr = w.steps.find(s => s.step === "approved" && s.state === "active" && s.party === "human");
      // The current step is the first that is stale, active or blocked, else the first not reached. All eight done is past `pushed`.
      let si = w.steps.findIndex(s => s.state === "active" || s.state === "blocked" || s.state === "stale");
      if (si < 0) si = w.steps.findIndex(s => s.state === "pending");
      const past = si < 0;
      if (past) si = 7;
      const cancelled = as.every(a => a.state === "finished") && as.some(a => /cancelled/i.test(a.note || ""));
      // The ONE function the header count and the rows share: real text addressed to the human, or a design waiting on them.
      const question = apr ? "The design is ready. Approve it, or send it back with a note for the designer."
        : (driver.state === "blocked" && driver.owner_kind === "human" && driver.note ? driver.note : "");
      let group;
      if (cancelled || past) group = "finished";                       // finished is PAST pushed, or cancelled. A landed item is not finished.
      else if (question) group = "needs";
      else if (as.every(a => a.state === "queued")) group = "queued";  // nothing has ever started
      else if (CARD_STEP.has(STEP[si]) && !live.length && !unknown.length) group = "idle";  // a card's step with no card on it
      else group = "working";
      return { id, title: w.title, as, live, unknown, open, driver, question, group, steps: w.steps, si, since: driver.updated_at, cancelled,
        apr: !!apr };
    });
  }

  const e = s => esc(s);
  function bar(it) {
    const fin = it.group === "finished";
    return '<div class="x-bar" role="img" aria-label="step ' + (it.si + 1) + ' of 8, ' + e(STEP[it.si]) + '">' + it.steps.map((s, i) => {
      let cls = s.state === "done" ? "d" : s.state === "stale" ? "k" : "";
      if (i === it.si && !fin) cls = it.group === "needs" ? "w" : it.group === "idle" ? "i" : s.state === "stale" ? "k c" : "c";
      return '<span class="' + cls + '" data-tip="' + e(s.step + (s.state === "stale" ? " (stale: commits since)" : "")) + '"></span>';
    }).join("") + "</div>";
  }
  function stepLine(it) {
    if (it.group === "finished") return "<b>" + (it.cancelled ? "cancelled" : "pushed") + "</b> <em>" + e(itemAge(it.since)) + " ago</em>";
    const name = STEP[it.si], st = it.steps[it.si].state;
    const verb = it.apr ? "waiting for your approval" : it.group === "needs" ? "asked a question at " + name : it.group === "idle" ? "idle at " + name + ", nothing running"
      : st === "stale" ? name + " is stale, commits since" : (VERB[name] || name);
    return "<b>step " + (it.si + 1) + " of 8 · " + e(verb) + "</b> <em>" + e(itemDur(it.since)) + "</em>";
  }
  function cards(it, openIt) {
    const real = it.as.filter(a => a.card);
    if (!real.length) return "";
    const chip = a => (typeof itemChip === "function" ? itemChip(handle(a), a.card) : "");
    const off = real.filter(a => !it.live.includes(a));
    const head = (it.live.length ? it.live.length + " live" : "") + (it.unknown.length ? (it.live.length ? ", " : "") + it.unknown.length + " unknown" : "") +
      (off.length - it.unknown.length > 0 ? (it.live.length || it.unknown.length ? ", " : "") + (off.length - it.unknown.length) + " ended" : "");
    return '<details class="x-cards"' + (openIt ? " open" : "") + "><summary>" + e(head) + "</summary><div>" +
      it.live.map(a => '<span class="x-c">' + chip(a) + '<i>live · ' + e(itemDur(a.updated_at)) + "</i></span>").join("") +
      off.map(a => '<span class="x-c off">' + chip(a) + "<i>" + (offlineIds.has(a.card) ? "room " + e(a.room) + " offline" : a.state === "finished" ? "done" : "ended, never reported") + "</i></span>").join("") +
      "</div></details>";
  }
  function askLine(it) {
    const d = it.driver, who = handle(d) || d.owner;
    return '<div class="x-who">' + e(who) + (it.apr ? " is waiting on your approval" : " asked") + ", " + e(itemDur(d.updated_at)) + " ago</div>";
  }
  function row(it, first) {
    let h = '<article class="x-row g-' + it.group + '" data-backlog="' + e(it.id) + '"><header><div class="x-title">' + e(it.title) + '</div><span class="x-id">' + e(it.id) + "</span></header>" +
      bar(it) + '<div class="x-step">' + stepLine(it) + "</div>";
    if (it.group === "needs") h += '<div class="x-ask">' + askLine(it) + "<p>" + e(it.question) + '</p><div class="x-btns">' +
      (it.apr ? '<button class="it-btn go">approve</button><button class="it-btn">send back</button>' :
        '<button class="it-btn go">answer</button><button class="it-btn">open terminal</button>') + "</div></div>";
    else if (it.group === "idle") h += '<div class="x-idle"><p>Nothing is running on this. ' + (it.driver.state === "blocked" ? "The last card stopped blocked and recorded no question." : "The last card ended without finishing.") +
      '</p><div class="x-btns"><button class="it-btn">cancel item</button><button class="it-btn">relaunch</button></div></div>';
    else if (it.group === "working" && it.driver.state === "blocked") h += '<div class="x-note blk">' + askLine(it).replace("x-who", "x-who q") + "<p>" + e(it.driver.note || "no question recorded") + "</p>" +
      "<span>Blocked, waiting on " + e(it.driver.owner) + ", not on you.</span></div>";
    else if (it.group === "working" && !it.live.length && it.unknown.length) h += '<div class="x-note">Room ' + e(it.unknown[0].room) + " is offline, so what this card is doing is unknown. Nothing to do until it is back.</div>";
    else if (it.group === "working" && !it.live.length) h += '<div class="x-note">No card is needed for this step. Atrium is waiting for ' + (it.si === 7 ? "origin to see the commit" : "every room to report the build") + ".</div>";
    return h + cards(it, first) + "</article>";
  }

  const items = foldItems();
  const of = g => items.filter(i => i.group === g);
  const sec = (g, label, hint, list, open) => list.length ? '<section class="x-sec s-' + g + '"><h2>' + label + ' <span class="x-n">' + list.length + "</span><small>" + hint + "</small></h2>" +
    list.map((i, n) => row(i, open && n === 0)).join("") + "</section>" : "";
  const needs = of("needs");
  const chips = [["needs you", needs.length, "hot"], ["working", of("working").length, ""], ["idle", of("idle").length, "mute"], ["queued", of("queued").length, ""]]
    .map(c => '<span class="x-chip ' + c[2] + '"><b>' + c[1] + "</b> " + c[0] + "</span>").join("");
  const box = document.getElementById("items-list");
  box.innerHTML = '<div class="x-wrap"><div class="x-top"><h1>Items</h1><div class="x-chips" data-needs="' + needs.length + '">' + chips + "</div>" +
    '<div class="x-legend"><span><i class="d"></i>done</span><span><i class="c"></i>current step</span><span><i class="w"></i>waiting on you</span><span><i class="k"></i>stale step (commits since)</span>' +
    '<span><i class="i"></i>idle: nothing running</span><span><i class="p"></i>not reached</span><em>hover a segment for its step name</em></div></div>' +
    '<div class="x-cols"><div class="x-col a">' + sec("needs", "Needs you", "a question or a design for you, nothing else", needs) +
    sec("idle", "Idle", "a card's step with no card on it", of("idle"), true) + '</div><div class="x-col b">' +
    sec("working", "Working", "a live card, or a step no card does (deploy, push)", of("working"), true) + sec("queued", "Queued", "nothing has started", of("queued")) +
    sec("finished", "Finished", "pushed, or cancelled", of("finished")) + "</div></div></div>";
  window.__items = items;
})();
