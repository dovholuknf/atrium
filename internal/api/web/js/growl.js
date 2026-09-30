// ── the persistent growler, the board's half ─────────────────────────────────
//
// Something waiting on a human that stays until it is handled, dismissed, or its reason ends. The hub holds the
// state and says the WHOLE open set on every `growls` event, so this file keeps no state of its own that an event
// does not replace. See docs/rnd/persistent-growler-design.md.
//
// NOTHING IS FETCHED. The hub sends one `growls` event when each stream opens, so the set arrives on load and on a
// reopen without asking. A board whose stream never sends one (a room served alone) draws nothing and behaves as it
// did before this file.
//
// It lives in the toast host, pinned above the toasts by `order`, and is `sticky` so the toast cap neither counts
// nor evicts it. That is the `hubToast` exemption, and it keeps `raiseToasts` putting it inside an open modal.

// The set as the hub last said it, open and snoozed rows, in the order the hub sorted them.
let growlSet = [];
// Whether an event has arrived yet. The first one seeds: a page load is not a pile of new growlers.
let growlHeard = false;
let growlOpen = false;
let growlSnoozing = "";
let growlPermAfter = 120;
// What was typed into each reply field, so a redraw from an event does not eat it.
const growlDrafts = new Map();
// The undo toast for each dismissed growler, so one that comes back by another route takes it down.
const growlUndo = new Map();

const GROWL_NAME = {
  permission: ["permission", "permissions"], halt: ["halt", "halts"], blocked: ["blocked", "blocked"],
  question: ["question", "questions"], "deploy-hold": ["deploy-hold", "deploy-holds"]
};
const GROWL_URGENCY = { permission: 1, halt: 2, blocked: 3, question: 4, "deploy-hold": 5 };
const GROWL_SNOOZES = [["15 min", 15], ["1 h", 60], ["until tomorrow 09:00", 0]];

function growlUrgency(g) { return g.urgency || GROWL_URGENCY[g.reason] || 9; }

function growlDrawn() { return growlSet.filter(g => g.state === "open"); }

// Whether a permission, or any subject, has an open growler. The nag and keyed toasts ask.
function growlHas(subject) {
  return !!subject && growlSet.some(g => g.state === "open" && g.subject && sameCard(g.subject, subject));
}

// The hub's event. `remind` is an array of ids on a reminder tick, which U2 turns into sound.
function onGrowlsEvent(e) {
  let d;
  try { d = JSON.parse(e.data); } catch (err) { return; }
  if (!d || !Array.isArray(d.growls)) return;
  if (d.perm_after_seconds) growlPermAfter = d.perm_after_seconds;
  growlApply(d.growls, !growlHeard);
  growlHeard = true;
}

function growlApply(next, seed) {
  const was = new Map(growlSet.map(g => [g.id, g]));
  const now = new Map(next.map(g => [g.id, g]));
  if (!seed) {
    next.forEach(g => {
      const w = was.get(g.id);
      if (g.state === "open" && (!w || w.state !== "open")) growlLog("growler", g);
      if (g.state === "snoozed" && w && w.state === "open") growlLog("growler snoozed", g);
    });
    growlSet.forEach(g => {
      if (g.state === "open" && !now.has(g.id)) growlLog("growler ended", g);
    });
  }
  growlSet = next;
  growlUndo.forEach((el, id) => {
    const g = now.get(id);
    if (g && g.state === "open" && el.dismiss) { el.dismiss(); growlUndo.delete(id); }
  });
  growlDraw();
}

function growlLog(what, g) {
  if (typeof recordToLog === "function") recordToLog(what + ": " + g.title, g.body || "", "", null, null);
}

// The card in the form the board's own list uses: `room~id` once two rooms are attached, bare before.
function growlCard(g) {
  if (typeof cardRows !== "undefined" && g.card && cardRows.has(g.card)) return g.card;
  if (typeof cardRows !== "undefined" && g.card_id && cardRows.has(g.card_id)) return g.card_id;
  return g.card || g.card_id || "";
}

function growlPermID(g) {
  const p = typeof permsLocal !== "undefined" ? permsLocal.find(x => sameCard(x.id, g.subject)) : null;
  return p ? p.id : g.subject;
}

function growlCounts(rest) {
  const n = {};
  rest.forEach(g => { n[g.reason] = (n[g.reason] || 0) + 1; });
  return Object.keys(GROWL_URGENCY).filter(k => n[k])
    .map(k => `${n[k]} ${GROWL_NAME[k][n[k] === 1 ? 0 : 1]}`).join(", ");
}

function growlFirstLine(s) { return String(s || "").split("\n")[0]; }

// The primary action of a row, as a button. Permission carries its own pair in the full face.
function growlPrimary(g) {
  switch (g.reason) {
    case "permission": return `<button data-do="approve"${g.subject ? "" : " disabled"}>approve once</button>`;
    case "halt": return `<button data-do="room">open the room</button>`;
    case "deploy-hold": return `<button data-do="lift">lift</button>`;
    default: return `<button data-do="open">open</button>`;
  }
}

function growlFull(g) {
  const off = g.room_offline ? `<span class="gr-off">room offline</span>` : "";
  const perm = g.reason === "permission";
  const text = perm
    ? `<code class="gr-cmd">${esc(growlFirstLine(g.body))}</code>`
    : `<div class="gr-line">${esc(growlFirstLine(g.body))}</div>`;
  const dead = perm && !g.subject ? " disabled" : "";
  let acts = "";
  if (perm) acts += `<button data-do="approve"${dead}>approve once</button><button data-do="block"${dead}>block</button>`;
  else if (g.reason === "halt") acts += `<button data-do="room">open the room</button>`;
  else if (g.reason === "deploy-hold") acts += `<button data-do="lift">lift</button>`;
  if (g.reason === "blocked" || g.reason === "question") {
    acts += `<input class="gr-reply" placeholder="reply" aria-label="reply"><button data-do="reply">send</button>`;
  }
  acts += `<button data-do="open">open</button><button data-do="snooze">snooze</button>` +
    `<button data-do="dismiss" data-tip="stops this alert. the ? chip on the card stays">dismiss this</button>`;
  const snooze = growlSnoozing === g.id
    ? `<div class="gr-snooze">${GROWL_SNOOZES.map(s => `<button data-snooze="${s[1]}">${s[0]}</button>`).join("")}</div>` : "";
  return `<div class="gr-full" data-id="${esc(g.id)}" data-reason="${esc(g.reason)}">` +
    `<div class="gr-head"><b>${esc(g.title)}</b>${off}</div>${text}<div class="gr-acts">${acts}</div>${snooze}</div>`;
}

function growlRow(g) {
  return `<div class="gr-row" data-id="${esc(g.id)}" data-reason="${esc(g.reason)}"><span class="gr-t">${esc(g.title)}</span>` +
    `${growlPrimary(g)}<button data-do="dismiss" data-tip="stops this alert. the ? chip on the card stays">&times;</button></div>`;
}

function growlDraw() {
  const host = document.getElementById("toasts");
  if (!host) return;
  let el = document.getElementById("growl");
  const rows = growlDrawn();
  // A phone has no stack yet. The state and the log lines above still run.
  if (!rows.length || innerWidth <= PHONE) {
    if (el) el.remove();
    growlOpen = false;
    return;
  }
  if (!el) {
    el = document.createElement("div");
    el.id = "growl";
    el.className = "sticky";
    el.addEventListener("click", growlClick);
    el.addEventListener("input", e => {
      const row = e.target.closest(".gr-full");
      if (row && e.target.classList.contains("gr-reply")) growlDrafts.set(row.dataset.id, e.target.value);
    });
    el.addEventListener("keydown", e => {
      if (e.key === "Enter" && e.target.classList.contains("gr-reply")) growlAct(e.target.closest(".gr-full"), "reply");
    });
    host.appendChild(el);
  }
  const focused = document.activeElement && document.activeElement.classList.contains("gr-reply") &&
    el.contains(document.activeElement) ? document.activeElement.closest(".gr-full").dataset.id : "";
  el.classList.toggle("open", growlOpen && rows.length > 1);
  if (growlOpen && rows.length > 1) {
    el.innerHTML = `<div class="gr-list">${rows.map(growlRow).join("")}</div>` +
      `<button class="gr-strip" data-do="fold">fold</button>`;
  } else {
    growlOpen = false;
    const rest = rows.slice(1);
    el.innerHTML = growlFull(rows[0]) + (rest.length
      ? `<button class="gr-strip" data-do="expand">+${rest.length} more: ${growlCounts(rest)}</button>` : "");
    const box = el.querySelector(".gr-reply");
    if (box) {
      box.value = growlDrafts.get(rows[0].id) || "";
      if (focused === rows[0].id) box.focus();
    }
  }
  // The cap is half the window, and the list scrolls inside it.
  el.style.setProperty("--gr-max", Math.floor(innerHeight / 2) + "px");
  if (typeof raiseToasts === "function") raiseToasts();
}
addEventListener("resize", () => { if (growlSet.length) growlDraw(); });

function growlClick(e) {
  const btn = e.target.closest("button");
  if (!btn) return;
  const row = btn.closest(".gr-full, .gr-row");
  if (btn.dataset.snooze !== undefined) { growlSnoozeFor(row, Number(btn.dataset.snooze)); return; }
  const what = btn.dataset.do;
  if (what === "expand" || what === "fold") { growlOpen = what === "expand"; growlSnoozing = ""; growlDraw(); return; }
  if (row) growlAct(row, what);
}

function growlByRow(row) { return growlSet.find(g => g.id === row.dataset.id); }

async function growlAct(row, what) {
  const g = row && growlByRow(row);
  if (!g) return;
  switch (what) {
    case "approve": return growlDecide(g, "approve", "");
    case "block": {
      const reason = await askText("why not?",
        "Handed straight back to the agent as the reason it was refused. " +
        "Telling it what to do instead is more useful than a wall.",
        "", "use a temp directory instead");
      if (reason === null) return;
      return growlDecide(g, "block", reason);
    }
    case "open": return growlOpenIt(g);
    case "room": if (await closeOpenDialogs()) openRooms(); return;
    case "lift": return growlLift(g);
    case "reply": return growlReply(g, row);
    case "snooze": growlSnoozing = growlSnoozing === g.id ? "" : g.id; growlDraw(); return;
    case "dismiss": return growlPost(g, { do: "dismiss" }, true);
  }
}

// Approve and block answer the request and leave the growler alone. It ends because the request left.
async function growlDecide(g, decision, reason) {
  if (!g.subject) return;
  try {
    await api(`/v1/permissions/${growlPermID(g)}/decide`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ decision, reason, forever: false, prefix: "", kind: "command", command: "" })
    });
  } catch (e) {
    toast("too late", e.message);
  }
  if (typeof permsSoon === "function") permsSoon();
}

async function growlOpenIt(g) {
  if (!await closeOpenDialogs()) return;
  const perm = g.reason === "permission";
  landOnAlert(growlCard(g), perm ? "perms" : "", perm ? growlPermID(g) : "");
}

async function growlLift(g) {
  if (!await confirmUser("lift the deploy hold?",
    `The hold on <b>${esc(g.room)}</b> ends and its agents carry on.`, "lift it")) return;
  try {
    await api("/v1/hold", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "lift", outcome: "operator", by: "operator" })
    });
  } catch (e) {
    toast("could not lift the hold", e.message);
  }
}

// Through the operator message path, which queues. Never typed into a terminal a human may be typing in.
async function growlReply(g, row) {
  const box = row.querySelector(".gr-reply");
  const text = box ? box.value.trim() : "";
  const id = growlCard(g);
  if (!text || !id) return;
  try {
    await api(`/v1/tasks/${id}/message`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text, when: "done" })
    });
    growlDrafts.delete(g.id);
    if (box) box.value = "";
    toast("sent", `${g.title}: ${text.slice(0, 80)}`);
  } catch (e) {
    toast("not sent", e.message || String(e));
  }
}

function growlSnoozeFor(row, minutes) {
  const g = row && growlByRow(row);
  if (!g) return;
  if (!minutes) {
    const t = new Date();
    t.setDate(t.getDate() + 1);
    t.setHours(9, 0, 0, 0);
    minutes = Math.ceil((t - Date.now()) / 60000);
  }
  growlSnoozing = "";
  growlPost(g, { do: "snooze", minutes: Math.min(10080, Math.max(1, minutes)) }, false);
}

// One call for dismiss, snooze and undismiss. A 409 means the reason ended first, and the row goes.
async function growlPost(g, body, offerUndo) {
  let res;
  try {
    res = await plainFetch("/_hub/growls/" + encodeURIComponent(g.id), {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(Object.assign({ via: "board", tab: typeof hubTabId === "string" ? hubTabId : "" }, body))
    });
  } catch (e) {
    toast("could not reach atrium", e.message || String(e));
    return null;
  }
  let d = {};
  try { d = await res.json(); } catch (e) {}
  if (res.status === 409) {
    toast("already handled", g.title);
    growlSet = growlSet.filter(x => x.id !== g.id);
    growlDraw();
    return null;
  }
  if (!res.ok) {
    toast("could not do that", d.error || "atrium answered " + res.status);
    return null;
  }
  if (d.growl) growlSet = growlSet.map(x => x.id === g.id ? d.growl : x);
  growlDraw();
  if (offerUndo && body.do === "dismiss") growlOfferUndo(g);
  return d.growl || null;
}

function growlOfferUndo(g) {
  if (growlDrawn().some(x => x.id === g.id)) return;
  const el = toast("dismissed: " + g.title, "");
  if (!el || !el.querySelector) return;
  const b = document.createElement("button");
  b.className = "gr-undo";
  b.textContent = "undo";
  b.addEventListener("click", async () => {
    growlUndo.delete(g.id);
    if (el.dismiss) el.dismiss();
    const back = await growlPost(g, { do: "undismiss" }, false);
    if (!back) return;
    if (!growlSet.some(x => x.id === back.id)) growlSet.push(back);
    growlSet.sort((a, c) => growlUrgency(a) - growlUrgency(c) || String(a.raised_at).localeCompare(String(c.raised_at)));
    growlDraw();
  });
  const body = el.querySelector(".body");
  if (body) body.appendChild(b);
  growlUndo.set(g.id, el);
}

// A keyed toast about a subject with an open growler says the same thing in two boxes. It is still logged, so
// "what was I told" keeps one answer. Wraps the wrapper in toast-log.js, so this file loads after it.
const growlRawToast = toast;
// eslint-disable-next-line no-func-assign
toast = function (title, body, goTo, key, taskFor) {
  if (key && growlHas(key)) {
    if (typeof recordToLog === "function") recordToLog(title, body, goTo, key, taskFor);
    return phoneToastStub;
  }
  return growlRawToast(title, body, goTo, key, taskFor);
};
