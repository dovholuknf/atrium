// A MOCKUP of the proposed Items tab, drawn over the live tab by shoot.js from the same fixture. Not shipped code: it shows what the
// design in docs/ui/items-tab-ux-pass.md asks for, and its grouping (foldItems) is the logic phase 1 would move into js/items-data.js.
(function () {
  const rows = window.__fx.rows, work = window.__fx.work;
  const liveIds = new Set((typeof lastTasks === "undefined" ? [] : lastTasks).map(t => t.id));
  const STEP_DONE = ["designed", "approved", "building", "tested", "reviewed", "landed", "deployed", "pushed"];
  const VERB = { designed: "designing", approved: "waiting for approval", building: "building", tested: "testing", reviewed: "in review", landed: "landing", deployed: "deploying", pushed: "pushing" };

  // ONE ROW PER BACKLOG ID. The newest live assignment drives the row's state and its question.
  function foldItems() {
    const by = {};
    for (const r of rows) (by[r.work_id] = by[r.work_id] || []).push(r);
    return Object.entries(by).map(([id, as]) => {
      const w = work.find(x => x.backlog_id === id) || { steps: [], title: id };
      as.sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at));
      const live = as.filter(a => a.state !== "finished" && a.card && liveIds.has(a.card));
      const open = as.filter(a => a.state !== "finished");
      const driver = live[0] || open[0] || as[0];
      const apr = w.steps.find(s => s.step === "approved" && s.state === "active" && s.party === "human");
      // The ONE function the header count and the rows share: a question is real text addressed to the human, or a design waiting on them.
      const question = apr ? "The design is ready. Approve it, or send it back with a note for the designer."
        : (driver.state === "blocked" && driver.owner_kind === "human" && driver.note ? driver.note : "");
      let group;
      if (as.every(a => a.state === "finished")) group = "finished";
      else if (question) group = "needs";
      else if (!live.length && open.some(a => a.state !== "queued")) group = "stalled";
      else if (open.every(a => a.state === "queued")) group = "queued";
      else group = "working";
      const si = Math.max(0, w.steps.findIndex(s => s.state === "active" || s.state === "blocked"));
      return { id, title: w.title, as, live, open, driver, question, group, steps: w.steps, si, since: driver.updated_at,
        cancelled: as.every(a => a.state === "finished") && as.some(a => /cancelled/i.test(a.note || "")) };
    });
  }

  const e = s => esc(s);
  function bar(it) {
    const fin = it.group === "finished";
    return '<div class="x-bar" role="img" aria-label="step ' + (it.si + 1) + ' of 8, ' + e(STEP_DONE[it.si]) + '">' + it.steps.map((s, i) => {
      const cls = s.state === "done" ? "d" : i === it.si && !fin ? (it.group === "needs" ? "w" : it.group === "stalled" ? "s" : "c") : "";
      return '<span class="' + cls + '" data-tip="' + e(s.step) + '"></span>';
    }).join("") + "</div>";
  }
  function stepLine(it) {
    if (it.group === "finished") return '<b>' + (it.cancelled ? "cancelled" : "landed") + "</b> <em>" + e(itemAge(it.since)) + " ago</em>";
    const name = STEP_DONE[it.si];
    const verb = it.group === "needs" && name === "approved" ? "waiting for your approval" : it.group === "needs" ? "asked a question at " + name : it.group === "stalled" ? "stalled at " + name : (VERB[name] || name);
    return "<b>step " + (it.si + 1) + " of 8 · " + e(verb) + "</b> <em>" + e(itemDur(it.since)) + "</em>";
  }
  function cards(it, openIt) {
    const live = it.live, gone = it.open.filter(a => !live.includes(a)), fin = it.as.filter(a => a.state === "finished");
    const chip = a => (typeof itemChip === "function" ? itemChip(a.card.replace(/^c-/, ""), a.card || "") : "");
    const head = live.length ? live.length + " live" + (it.as.length - live.length ? ", " + (it.as.length - live.length) + " ended" : "") : it.as.length + " ended";
    return '<details class="x-cards"' + (openIt ? " open" : "") + "><summary>" + e(head) + "</summary><div>" +
      live.map(a => '<span class="x-c">' + chip(a) + '<i>live · ' + e(itemDur(a.updated_at)) + "</i></span>").join("") +
      gone.concat(fin).map(a => '<span class="x-c off">' + (a.card ? chip(a) : '<span class="it-card gone">no card yet</span>') + "<i>" + (a.state === "finished" ? "done" : "ended, never reported") + "</i></span>").join("") + "</div></details>";
  }
  function row(it, first) {
    const hot = it.group === "needs";
    let h = '<article class="x-row g-' + it.group + '" data-backlog="' + e(it.id) + '"><header><div class="x-title">' + e(it.title) + '</div><span class="x-id">' + e(it.id) + "</span></header>" +
      bar(it) + '<div class="x-step">' + stepLine(it) + "</div>";
    if (hot) h += '<div class="x-ask"><p>' + e(it.question) + '</p><div class="x-btns">' +
      (/design is ready/.test(it.question) ? '<button class="it-btn go">approve</button><button class="it-btn">send back</button>' :
        '<button class="it-btn go">answer</button><button class="it-btn">open terminal</button>') + "</div></div>";
    else if (it.group === "stalled") h += '<div class="x-stall"><p>Nothing is running on this. ' + (it.driver.state === "blocked" ? "The last card stopped blocked and recorded no question." : "The last card ended without finishing.") +
      '</p><div class="x-btns"><button class="it-btn">cancel item</button><button class="it-btn">relaunch</button></div></div>';
    else if (it.driver.state === "blocked" && it.driver.owner_kind !== "human") h += '<div class="x-note">Blocked, waiting on ' + e(it.driver.owner) + ", not on you.</div>";
    return h + cards(it, first) + "</article>";
  }

  const items = foldItems();
  const of = g => items.filter(i => i.group === g);
  const sec = (g, label, hint, list, open) => list.length ? '<section class="x-sec s-' + g + '"><h2>' + label + ' <span class="x-n">' + list.length + "</span><small>" + hint + "</small></h2>" +
    list.map((i, n) => row(i, open && n === 0)).join("") + "</section>" : "";
  const needs = of("needs");
  const chips = [["needs you", needs.length, "hot"], ["working", of("working").length, ""], ["stalled", of("stalled").length, "warn"], ["queued", of("queued").length, ""]]
    .map(c => '<span class="x-chip ' + c[2] + '"><b>' + c[1] + "</b> " + c[0] + "</span>").join("");
  const box = document.getElementById("items-list");
  box.innerHTML = '<div class="x-wrap"><div class="x-top"><h1>Items</h1><div class="x-chips" data-needs="' + needs.length + '">' + chips + "</div>" +
    '<div class="x-legend"><span><i class="d"></i>done</span><span><i class="c"></i>current step</span><span><i class="w"></i>waiting on you</span><span><i class="s"></i>stalled</span><span><i class="p"></i>not reached</span><em>hover a segment for its step name</em></div></div>' +
    '<div class="x-cols"><div class="x-col a">' + sec("needs", "Needs you", "a question or a design for you, nothing else", needs) +
    sec("stalled", "Stalled", "unfinished, and nothing is running on it", of("stalled"), true) + '</div><div class="x-col b">' +
    sec("working", "Working", "a live card is on it", of("working"), true) + sec("queued", "Queued", "no card yet", of("queued")) + sec("finished", "Finished", "landed or cancelled", of("finished")) + "</div></div></div>";
  window.__items = items;
})();
