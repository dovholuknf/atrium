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
// The growler "next" brought up, by id. The open set is turned until it is first, so a later event that replaces the
// set keeps it in front while it lasts. Empty is the hub's own order.
let growlFront = "";
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
const GROWL_SNOOZES = [["15 min", 15], ["1 h", 60], ["tomorrow 9am", 0]];

function growlUrgency(g) { return g.urgency || GROWL_URGENCY[g.reason] || 9; }

// THE GROWLER IS A SETTING, AND IT IS OFF UNLESS SWITCHED ON. Per browser, in localStorage beside the other view
// prefs, read on every ask so every window follows a change with no message between them (the `storage` listener below
// redraws). OFF draws nothing: no floating growler, no stack, no undo bar, no favicon dot or title flash, and no
// growler tone. The set is still tracked and every raise, snooze and end is still logged, so the bell keeps the
// alert and its count. Desktop notifications still follow the notify setting, and a permission request is carried
// by the ordinary permission path (its keyed toast, tone and nag, the perms tab badge and the bell).
const GROWLER_KEY = "atrium.growler";
function growlOn() {
  try { return localStorage.getItem(GROWLER_KEY) === "1"; } catch (e) { return false; }
}
function setGrowler(on) {
  try {
    if (on) localStorage.setItem(GROWLER_KEY, "1");
    else localStorage.removeItem(GROWLER_KEY);
  } catch (e) {}
  growlSwitched();
}
// The switch moved here or in another window: put the stack and its leftovers right, and repaint the checkbox.
function growlSwitched() {
  if (!growlOn()) {
    growlUndo.forEach(el => { if (el && el.dismiss) el.dismiss(); });
    growlUndo.clear();
    growlPhoneUndo = null;
  }
  growlDraw();
  growlAttention();
  const c = document.getElementById("s-growler");
  if (c) c.checked = growlOn();
}
addEventListener("storage", e => { if (e.key === GROWLER_KEY || e.key === null) growlSwitched(); });
addEventListener("DOMContentLoaded", () => {
  const c = document.getElementById("s-growler");
  if (c) c.checked = growlOn();
});

// THE BOARD DRAWS EVERY GROWLER, including a popped-out card's, because a growler is state like the card's badges.
// A pop-out draws only its own card's. Neither switch hides one. See the design, section 7.
function growlMine(g) { return !inPopout() || sameCard(growlCard(g), popoutCard()); }
function growlDrawn() {
  if (!growlOn()) return [];
  return growlSet.filter(g => g.state === "open" && growlMine(g) && !growlQuiet.has(g.id));
}

// NOT DRAWN, BECAUSE YOU ARE ALREADY ON IT. A question or a blocked card raised while its own terminal is attached,
// the tab visible and the window focused repeats what is on screen, so it is not drawn, not rung and not notified.
// It is still logged, once, as a raise always is. The ids are the growlers hidden that way, kept until the growler
// leaves the set or a reminder finds you looking elsewhere, when it draws like any other. Permissions, halts and
// deploy holds are never quiet: a permission blocks the card, and the other two are not about one terminal.
const growlQuiet = new Set();
function growlAsks(g) { return g.reason === "question" || g.reason === "blocked"; }
// `readySilenced` is the same rule the ready alert follows, across windows: this window focused and showing the
// card, or another focused window that said it is. A pop-out focused on its own card covers the board.
function growlOnIt(g) { return growlAsks(g) && readySilenced(growlCard(g)); }

// A HIDDEN GROWLER IS RE-CHECKED, since "you are on it" ends without any event of the hub's: the window blurs, the tab
// hides, another card is attached, the terminals view is left, a pop-out blurs or closes, a focus claim expires. A
// quiet id whose rule no longer holds is let out and drawn, and nothing rings: it was announced when it was raised,
// to the window you were reading. Asked on the events a window has and on a one second tick for the rest, which
// costs nothing while no growler is hidden.
function growlRecheck() {
  if (!growlQuiet.size) return;
  let back = false;
  growlQuiet.forEach(id => {
    const g = growlSet.find(x => x.id === id);
    if (!g || g.state !== "open" || !growlOnIt(g)) { growlQuiet.delete(id); back = true; }
  });
  if (back) { growlDraw(); growlAttention(); }
}
["focus", "blur"].forEach(t => addEventListener(t, () => setTimeout(growlRecheck, 0)));
document.addEventListener("visibilitychange", () => setTimeout(growlRecheck, 0));
if ("BroadcastChannel" in window) new BroadcastChannel("atrium-solo").addEventListener("message", () => setTimeout(growlRecheck, 0));
setInterval(growlRecheck, 1000);

// Whether this window has an open growler drawn for a subject. The nag and keyed toasts ask.
function growlHas(subject) {
  return !!subject && growlDrawn().some(g => g.subject && sameCard(g.subject, subject));
}

// A popped-out card's reminders belong to its pop-out, so its switch and mute hold them back and the board does not
// ring in their place. This is the board's view of that: the card is popped out and its window is quiet, read off
// the two per-card keys in the same localStorage.
function growlMutedInPopout(g) {
  if (inPopout()) return false;
  const card = growlCard(g);
  if (!card || !poppedOut(card)) return false;
  const id = bareId(card);
  try {
    return localStorage.getItem(NOTIFY_OFF_CARD_KEY + id) === "1" ||
      !!JSON.parse(localStorage.getItem(SOUND_CARD_KEY + id) || "{}").muted;
  } catch (e) { return false; }
}
addEventListener("storage", e => {
  if (!e.key || !(e.key.startsWith(NOTIFY_OFF_CARD_KEY) || e.key.startsWith(SOUND_CARD_KEY))) return;
  if (growlSet.length) { growlDraw(); growlAttention(); }
});

// The hub's event. `remind` is an array of ids on a reminder tick, which U2 turns into sound.
function onGrowlsEvent(e) {
  let d;
  try { d = JSON.parse(e.data); } catch (err) { return; }
  if (!d || !Array.isArray(d.growls)) return;
  if (d.perm_after_seconds) growlPermAfter = d.perm_after_seconds;
  growlApply(d.growls, !growlHeard, Array.isArray(d.remind) ? d.remind : []);
  growlHeard = true;
}

function growlApply(next, seed, remind) {
  const was = new Map(growlSet.map(g => [g.id, g]));
  const now = new Map(next.map(g => [g.id, g]));
  const raised = [];
  if (!seed) {
    next.forEach(g => {
      const w = was.get(g.id);
      if (g.state === "open" && (!w || w.state !== "open")) {
        growlLog("growler", g);
        if (growlOnIt(g)) growlQuiet.add(g.id); else { growlQuiet.delete(g.id); raised.push(g); }
      }
      if (g.state === "snoozed" && w && w.state === "open") growlLog("growler snoozed", g);
    });
    growlSet.forEach(g => {
      if (g.state === "open" && !now.has(g.id)) growlLog("growler ended", g);
    });
  }
  growlSet = next;
  // An open bell shows each growler's control, so it follows the set.
  const bell = document.getElementById("toastlog");
  if (!seed && bell && bell.open && typeof openToastLog === "function") openToastLog();
  growlQuiet.forEach(id => { const g = now.get(id); if (!g || g.state !== "open") growlQuiet.delete(id); });
  growlUndo.forEach((el, id) => {
    const g = now.get(id);
    if (g && g.state === "open" && el.dismiss) { el.dismiss(); growlUndo.delete(id); }
  });
  if (growlPhoneUndo && (now.get(growlPhoneUndo.g.id) || {}).state === "open") growlPhoneUndo = null;
  growlDraw();
  growlReapNotes();
  growlAttention();
  if (!seed) growlAttend(raised, remind);
}

// EVERY OPEN WINDOW HEARS EVERY EVENT, and they share one localStorage and one toast log. A line or an alert
// that every window made would be said once per window, so the first to claim a key says it. The key carries the
// growler's `raised_at`, which the hub resets when a snooze ends, so a growler raised again is a new key.
const GROWL_ONCE_KEY = "atrium.growl.once";
function growlOnce(key) {
  try {
    const now = Date.now();
    const seen = JSON.parse(localStorage.getItem(GROWL_ONCE_KEY) || "{}");
    Object.keys(seen).forEach(k => { if (now - seen[k] > 60000) delete seen[k]; });
    if (seen[key]) return false;
    seen[key] = now;
    localStorage.setItem(GROWL_ONCE_KEY, JSON.stringify(seen));
  } catch (e) {}
  return true;
}

function growlLog(what, g, extra) {
  if (!growlOnce(what + "|" + g.id + "|" + g.raised_at + "|" + (extra || ""))) return;
  if (typeof recordToLog === "function") recordToLog(what + ": " + g.title, g.body || "", "", null, null, g.id);
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
  const off = (g.room_offline ? `<span class="gr-off">room offline</span>` : "") +
    (growlMutedInPopout(g) ? `<span class="gr-off">muted in its window</span>` : "");
  const perm = g.reason === "permission";
  // A question is read whole, in a box that scrolls. Anything else keeps its one line.
  const asks = g.reason === "blocked" || g.reason === "question";
  const split = asks ? replies.split(g.body) : { text: g.body, choices: [] };
  const text = perm
    ? `<code class="gr-cmd">${esc(growlFirstLine(g.body))}</code>`
    : asks ? `<div class="gr-body">${growlBodyHTML(split.text)}</div>`
      : `<div class="gr-line">${esc(growlFirstLine(g.body))}</div>`;
  const offered = asks ? replies.of({ replies: g.replies, ask: g.body, fixed: g.fixed }) : [];
  const choices = offered.length
    ? replies.buttons(offered, { cls: "gr-choices", disabled: growlChoiceSent.get(g.id) === g.body }).outerHTML : "";
  const big = growlCompose === g.id;
  const dead = perm && !g.subject ? " disabled" : "";
  let acts = "";
  if (perm) acts += `<button data-do="approve"${dead}>approve once</button><button data-do="block"${dead}>block</button>`;
  else if (g.reason === "halt") acts += `<button data-do="room">open the room</button>`;
  else if (g.reason === "deploy-hold") acts += `<button data-do="lift">lift</button>`;
  let reply = "";
  if (asks) {
    reply = `<div class="gr-replybox${big ? " big" : ""}"><textarea class="gr-reply" rows="1" placeholder="reply" ` +
      `aria-label="reply"></textarea><div class="gr-replyacts"><button data-do="reply">send</button>` +
      `<button data-do="compose" aria-label="${big ? "fold the reply box" : "open a bigger reply box"}" ` +
      `data-tip="${big ? "fold it. your text stays" : "a bigger box to write in"}">${big ? "&#10514;" : "&#10530;"}</button></div></div>`;
  }
  acts += `<button data-do="open">open</button><button data-do="snooze">remind me</button>` +
    `<button data-do="dismiss" data-tip="stops this alert. the ? chip on the card stays">dismiss this</button>`;
  const snooze = growlSnoozing === g.id
    ? `<div class="gr-snooze">${GROWL_SNOOZES.map(s => `<button data-snooze="${s[1]}">${s[0]}</button>`).join("")}</div>` : "";
  return `<div class="gr-full" data-id="${esc(g.id)}" data-reason="${esc(g.reason)}">` +
    `<div class="gr-head"><b>${esc(g.title)}</b>${off}</div>${text}${choices}${reply}<div class="gr-acts">${acts}</div>${snooze}</div>`;
}

// The growlers whose choice is in flight or answered, by the body they answered, so a redraw keeps the
// buttons disabled and a new question on the same growler gets live ones.
const growlChoiceSent = new Map();

// The body as paragraphs, lists and code blocks, every character escaped first. No inline markup and no links: a
// question is model output and some of it echoes what a tool read.
function growlBodyHTML(text) {
  const lines = String(text || "").replace(/\r/g, "").split("\n");
  const out = [];
  const num = /^\s*(\d+)[.)]\s+(.*)$/, dot = /^\s*[-*]\s+(.*)$/, fence = /^\s*```/;
  for (let i = 0; i < lines.length;) {
    const l = lines[i];
    if (fence.test(l)) {
      const code = [];
      for (i++; i < lines.length && !fence.test(lines[i]); i++) code.push(lines[i]);
      i++;
      out.push(`<pre><code>${esc(code.join("\n"))}</code></pre>`);
    } else if (!l.trim()) {
      i++;
    } else if (num.test(l) || dot.test(l)) {
      const ordered = num.test(l), re = ordered ? num : dot, items = [];
      const start = ordered ? Number(num.exec(l)[1]) : 0;
      for (; i < lines.length && re.test(lines[i]); i++) items.push(`<li>${esc(re.exec(lines[i])[ordered ? 2 : 1])}</li>`);
      out.push(ordered ? `<ol start="${start}">${items.join("")}</ol>` : `<ul>${items.join("")}</ul>`);
    } else {
      const para = [];
      for (; i < lines.length && lines[i].trim() && !fence.test(lines[i]) && !num.test(lines[i]) && !dot.test(lines[i]); i++) {
        para.push(esc(lines[i]));
      }
      out.push(`<p>${para.join("<br>")}</p>`);
    }
  }
  return out.join("");
}

// Which growler's reply box is opened to full size. The text is the draft either way.
let growlCompose = "";

// A reply box that grows with what is typed, up to the cap in the stylesheet, and scrolls past it.
function growlGrow(box) {
  box.style.height = "auto";
  box.style.height = box.scrollHeight + "px";
}

function growlRow(g) {
  return `<div class="gr-row" data-id="${esc(g.id)}" data-reason="${esc(g.reason)}"><span class="gr-t">${esc(g.title)}</span>` +
    `${growlPrimary(g)}<button data-do="dismiss" data-tip="stops this alert. the ? chip on the card stays">&times;</button></div>`;
}

// One set of listeners for both faces. The drafts and the focus survive a redraw from an event.
function growlWire(el) {
  el.addEventListener("click", growlClick);
  el.addEventListener("input", e => {
    const row = e.target.closest(".gr-full");
    if (row && e.target.classList.contains("gr-reply")) {
      growlDrafts.set(row.dataset.id, e.target.value);
      growlGrow(e.target);
    }
  });
  // Enter sends and Shift+Enter is a newline. Escape folds a full-size box, keeping the text.
  el.addEventListener("keydown", e => {
    if (!e.target.classList.contains("gr-reply") || e.isComposing) return;
    const row = e.target.closest(".gr-full");
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); growlAct(row, "reply"); }
    if (e.key === "Escape" && growlCompose === row.dataset.id) {
      e.preventDefault();
      e.stopPropagation();
      growlCompose = "";
      growlDraw();
    }
  });
}

// THE DRAWN NODES ARE KEPT WHEN THEIR MARKUP IS THE SAME. Every `growls` event, every claim from a pop-out and every
// resize asks for a draw, and replacing a node drops its `:hover` and whatever is typed in it. So each part is
// compared with the markup it was made from, and only a part that changed is swapped. Answers the nodes it made.
const growlSigs = new WeakMap();
function growlReconcile(el, parts) {
  const made = [];
  parts.forEach((html, i) => {
    const old = el.children[i];
    if (old && growlSigs.get(old) === html) return;
    const t = document.createElement("template");
    t.innerHTML = html;
    const n = t.content.firstElementChild;
    growlSigs.set(n, html);
    if (old) el.replaceChild(n, old);
    else el.appendChild(n);
    made.push(n);
  });
  while (el.children.length > parts.length) el.lastElementChild.remove();
  return made;
}

// What is focused in a reply box and where the caret is, so a redraw from an event does not move it.
let growlCaret = [0, 0];

// Only for nodes just made: an untouched node already holds its draft, and setting it again would move the caret.
function growlRestore(nodes, focused) {
  nodes.flatMap(n => n.matches(".gr-full") ? [n] : [...n.querySelectorAll(".gr-full")]).forEach(full => {
    const box = full.querySelector(".gr-reply");
    if (!box) return;
    box.value = growlDrafts.get(full.dataset.id) || "";
    growlGrow(box);
    if (focused === full.dataset.id) {
      box.focus();
      try { box.setSelectionRange(growlCaret[0], growlCaret[1]); } catch (e) {}
    }
  });
}

function growlFocused(el) {
  const a = document.activeElement;
  if (!(a && a.classList.contains("gr-reply") && el.contains(a))) return "";
  growlCaret = [a.selectionStart, a.selectionEnd];
  return a.closest(".gr-full").dataset.id;
}

function growlDraw() {
  const rows = growlDrawn();
  const phone = innerWidth <= PHONE;
  const desk = document.getElementById("growl");
  const hand = document.getElementById("growl-phone");
  if (phone) { if (desk) desk.remove(); } else if (hand) { hand.hidden = true; hand.innerHTML = ""; }
  if (phone) { growlDrawPhone(rows, hand); return; }
  const host = document.getElementById("toasts");
  if (!host) return;
  let el = desk;
  if (rows.length < 2) growlFront = "";
  if (!rows.length) {
    if (el) el.remove();
    growlOpen = false;
    return;
  }
  if (!el) {
    el = document.createElement("div");
    el.id = "growl";
    el.className = "sticky";
    growlWire(el);
    host.appendChild(el);
  }
  const focused = growlFocused(el);
  el.classList.toggle("open", growlOpen && rows.length > 1);
  let parts;
  if (growlOpen && rows.length > 1) {
    parts = [`<div class="gr-list">${rows.map(growlRow).join("")}</div>`,
      `<button class="gr-strip" data-do="fold">show less</button>`];
  } else {
    growlOpen = false;
    // THE STRIP SAYS WHAT IT DOES. "next" brings the following growler up as the full one, and the first goes to the
    // back of the line. "show all" is the stacked list, offered only when there is more than one other to see.
    const turn = Math.max(0, rows.findIndex(g => g.id === growlFront));
    const line = rows.slice(turn).concat(rows.slice(0, turn));
    const rest = line.slice(1);
    parts = [growlFull(line[0])];
    if (rest.length) {
      parts.push(`<button class="gr-strip" data-do="next" data-tip="${esc(growlCounts(rest))} waiting">next: ${esc(rest[0].title)}</button>`);
      if (rest.length > 1) parts.push(`<button class="gr-strip" data-do="expand">show all ${rows.length}: ${growlCounts(rows)}</button>`);
    }
  }
  growlRestore(growlReconcile(el, parts), focused);
  // The cap is half the window, and the list scrolls inside it.
  el.style.setProperty("--gr-max", Math.floor(innerHeight / 2) + "px");
  if (typeof raiseToasts === "function") raiseToasts();
}

// ON A PHONE the growler is one line pinned under the header, the one exception to the bell nudge, because the
// bell's count is the signal that did not work. It is an ordinary flex item of the body, between the header and
// `main`, so it takes its height out of `main` and never lies over the key bar or the composer. Tapping it opens
// the stack in place, at most half the window.
function growlDrawPhone(rows, el) {
  if (!el) return;
  const undo = growlOn() && growlPhoneUndo && Date.now() < growlPhoneUndo.until ? growlPhoneUndo.g : null;
  if (!rows.length && !undo) { el.hidden = true; el.innerHTML = ""; growlOpen = false; return; }
  if (!el.dataset.wired) { growlWire(el); el.dataset.wired = "1"; }
  const focused = growlFocused(el);
  el.hidden = false;
  el.classList.toggle("open", growlOpen && rows.length > 0);
  const parts = [];
  if (undo) parts.push(`<div class="gp-undo"><span>dismissed: ${esc(undo.title)}</span><button data-do="undo">undo</button></div>`);
  if (rows.length && growlOpen) {
    parts.push(`<button class="gp-line" data-do="fold"><b>show less</b></button>`);
    rows.forEach(g => parts.push(growlFull(g)));
  } else if (rows.length) {
    const top = rows[0];
    parts.push(`<button class="gp-line" data-do="expand"><b>${esc(top.title)}</b>` +
      (rows.length > 1 ? `<span class="gp-n">+${rows.length - 1}</span>` : "") + `</button>`);
  }
  growlRestore(growlReconcile(el, parts), focused);
  el.style.setProperty("--gr-max", Math.floor(innerHeight / 2) + "px");
}
// The dismissed growler a phone offers to bring back, since a phone draws no toasts.
let growlPhoneUndo = null;
addEventListener("resize", () => { if (growlSet.length) growlDraw(); });

function growlClick(e) {
  const btn = e.target.closest("button");
  if (!btn) return;
  const row = btn.closest(".gr-full, .gr-row");
  if (btn.dataset.snooze !== undefined) { growlSnoozeFor(row, Number(btn.dataset.snooze)); return; }
  if (btn.dataset.choice !== undefined) {
    const g = row && growlByRow(row);
    if (g) growlSendChoice(g, row, btn.dataset.choice);
    return;
  }
  const what = btn.dataset.do;
  if (what === "undo") {
    const u = growlPhoneUndo;
    growlPhoneUndo = null;
    if (u) growlUndismiss(u.g);
    growlDraw();
    return;
  }
  if (what === "next") {
    const rows = growlDrawn(), at = Math.max(0, rows.findIndex(g => g.id === growlFront));
    if (rows.length > 1) growlFront = rows[(at + 1) % rows.length].id;
    growlSnoozing = "";
    growlDraw();
    return;
  }
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
    case "compose": {
      // The text is the draft either way, and the box is rebuilt from it at the other size.
      const box = row.querySelector(".gr-reply");
      if (box) growlDrafts.set(g.id, box.value);
      growlCompose = growlCompose === g.id ? "" : g.id;
      growlDraw();
      const back = document.querySelector(`.gr-full[data-id="${CSS.escape(g.id)}"] .gr-reply`);
      if (back) { growlCaret = [back.value.length, back.value.length]; back.focus(); back.setSelectionRange(growlCaret[0], growlCaret[1]); }
      return;
    }
    case "snooze": growlSnoozing = growlSnoozing === g.id ? "" : g.id; growlDraw(); return;
    case "dismiss": return growlPost(g, { do: "dismiss" }, true);
  }
}

// Names the row's room to the hub, the way the launch dialog does: an explicit `X-Atrium-Room` wins over the
// board's own scope in the fetch wrapper (js/rooms.js). The hub routes `/v1/permissions` and `/v1/hold` by this
// header and not by an id, so on the merged view a write without it goes to the wrong room or to none.
function growlHeaders(g) {
  const h = { "Content-Type": "application/json" };
  if (typeof hubIsHub !== "undefined" && hubIsHub && g.room) h["X-Atrium-Room"] = g.room;
  return h;
}

// Approve and block answer the request and leave the growler alone. It ends because the request left.
// The room's own id, since the hub does not untag a permission path.
async function growlDecide(g, decision, reason) {
  if (!g.subject) return;
  try {
    await api(`/v1/permissions/${encodeURIComponent(g.subject)}/decide`, {
      method: "POST", headers: growlHeaders(g),
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
  await landOnAlert(growlCard(g), perm ? "perms" : "", perm ? growlPermID(g) : "");
  // Open is an answer to the alert, so it goes the way "dismiss this" does, with the same undo.
  return growlPost(g, { do: "dismiss" }, true);
}

async function growlLift(g) {
  if (!await confirmUser("lift the deploy hold?",
    `The hold on <b>${esc(g.room)}</b> ends and its agents carry on.`, "lift it")) return;
  try {
    await api("/v1/hold", {
      method: "POST", headers: growlHeaders(g),
      body: JSON.stringify({ action: "lift", outcome: "operator", by: "operator" })
    });
  } catch (e) {
    toast("could not lift the hold", e.message);
  }
}

// Through the operator message path, which queues. Never typed into a terminal a human may be typing in.
function growlReply(g, row) {
  const box = row.querySelector(".gr-reply");
  return growlSendReply(g, box ? box.value.trim() : "", box);
}

// A choice is one answer: every button of the question is disabled until the send answers, and lit again
// only if it failed, so a double press cannot send twice.
async function growlSendChoice(g, row, text) {
  if (growlChoiceSent.get(g.id) === g.body) return;
  growlChoiceSent.set(g.id, g.body);
  const btns = [...row.querySelectorAll(".gr-choices button")];
  btns.forEach(b => { b.disabled = true; });
  const ok = await growlSendReply(g, text, null);
  if (!ok) {
    growlChoiceSent.delete(g.id);
    btns.forEach(b => { b.disabled = false; });
  }
}

// A reply typed, or a choice pressed. Both are the same message. True when it was sent.
async function growlSendReply(g, text, box) {
  const id = growlCard(g);
  if (!text || !id) return false;
  try {
    await api(`/v1/tasks/${id}/message`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text, when: "done" })
    });
    growlDrafts.delete(g.id);
    if (box) { box.value = ""; growlGrow(box); }
    toast("sent", `${g.title}: ${text.slice(0, 80)}`);
    return true;
  } catch (e) {
    toast("not sent", e.message || String(e));
    return false;
  }
}

function growlSnoozeFor(row, minutes) {
  const g = row && growlByRow(row);
  if (!g) return;
  growlSnoozing = "";
  growlRemind(g, minutes);
}

// "Remind me" is the hub's snooze: the row stays in the log and raises once more when it is due. 0 is tomorrow 9am.
function growlRemind(g, minutes) {
  if (!minutes) {
    const t = new Date();
    t.setDate(t.getDate() + 1);
    t.setHours(9, 0, 0, 0);
    minutes = Math.ceil((t - Date.now()) / 60000);
  }
  return growlPost(g, { do: "snooze", minutes: Math.min(10080, Math.max(1, minutes)) }, false);
}

// How long until a reminded row is due, for the bell.
function growlRemindIn(g) {
  const m = Math.max(1, Math.round((Date.parse(g.until) - Date.now()) / 60000));
  if (!(m > 0)) return "reminding";
  return "reminding in " + (m < 90 ? m + " min" : m < 2880 ? Math.round(m / 60) + " h" : Math.round(m / 1440) + " days");
}

// One call for dismiss, snooze and undismiss. A 409 means the reason ended first, and the row goes.
async function growlPost(g, body, offerUndo) {
  let res;
  try {
    res = await plainFetch("/_hub/growls/" + encodeURIComponent(g.id), {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(Object.assign({ via: innerWidth <= PHONE ? "phone" : "board", tab: typeof hubTabId === "string" ? hubTabId : "" }, body))
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

// Undo for a dismissal: the hub answers with the row open again and its original raised_at, which goes back in the
// set at once. The next `growls` event confirms it.
async function growlUndismiss(g) {
  const back = await growlPost(g, { do: "undismiss" }, false);
  if (!back) return;
  if (!growlSet.some(x => x.id === back.id)) growlSet.push(back);
  growlSet.sort((a, c) => growlUrgency(a) - growlUrgency(c) || String(a.raised_at).localeCompare(String(c.raised_at)));
  growlDraw();
}

function growlOfferUndo(g) {
  if (!growlOn()) return;
  if (growlDrawn().some(x => x.id === g.id)) return;
  // A phone draws no toasts, so its undo is a line in the strip for as long as a toast would live.
  if (innerWidth <= PHONE) {
    const mine = { g, until: Date.now() + 10000 };
    growlPhoneUndo = mine;
    growlDraw();
    setTimeout(() => { if (growlPhoneUndo === mine) { growlPhoneUndo = null; growlDraw(); } }, 10000);
    return;
  }
  const el = toast("dismissed: " + g.title, "");
  if (!el || !el.querySelector) return;
  const b = document.createElement("button");
  b.className = "gr-undo";
  b.textContent = "undo";
  b.addEventListener("click", async () => {
    growlUndo.delete(g.id);
    if (el.dismiss) el.dismiss();
    growlUndismiss(g);
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

// ── asking for attention while the tab is not in front ───────────────────────
// All of it follows the hub's `growls` event, so every screen agrees on when a reminder happened. The growler on
// screen is state and is never held back by mute or the master switch. Sound and desktop notifications are, by
// going through `alerting.play` and `notify`, which ask `notifyHeld` and the mute themselves.

function growlIsOpen(id) { return growlSet.some(g => g.id === id && g.state === "open"); }

// A raise or a reminder, said by one window. The one looking at the board plays the tone, and so does the one
// picked when nobody is looking, which also raises the desktop notification. A window that knows another has the
// focus says nothing.
function growlAttend(raised, remind) {
  const said = new Map();
  (remind || []).forEach(id => {
    const g = growlSet.find(x => x.id === id && x.state === "open");
    if (g) said.set(id, [g, "remind"]);
  });
  raised.forEach(g => { if (!said.has(g.id)) said.set(g.id, [g, "raise"]); });
  // A reminder that finds you elsewhere brings a hidden growler back, so it is there to be answered.
  said.forEach(([g]) => {
    if (growlQuiet.has(g.id) && !growlOnIt(g)) { growlQuiet.delete(g.id); growlDraw(); growlAttention(); }
  });
  said.forEach(([g, kind]) => growlSay(g, kind));
}

function growlSay(g, kind) {
  // The pop-out rings for its own card and for no other. The board does not ring for a card that is popped out,
  // so the pop-out's switch and mute are what hold it back, and it rings even while the board has the focus, since
  // nobody else will. Closing the pop-out hands it back to the board.
  if (inPopout() ? !growlMine(g) : poppedOut(growlCard(g))) return;
  if (!inPopout() && focusIsElsewhere()) return;
  if (growlOnIt(g)) return;
  const extra = kind === "remind" ? "r" + (g.reminders || 0) : "";
  if (!growlOnce("say|" + kind + "|" + g.id + "|" + g.raised_at + "|" + extra)) return;
  if (kind === "remind") growlLog("growler reminder", g, extra);
  const perm = g.reason === "permission";
  const card = typeof cardRows !== "undefined" ? cardRows.get(growlCard(g)) : null;
  // The tone is the growler's own, so it goes with the growler. A permission still rings through the ordinary path.
  if (growlOn()) alerting.play(perm ? "permission" : "waiting", card ? soundForAlert(card) : "");
  alerting.notify(g.title, growlFirstLine(g.body), perm ? "perms" : "", "", "growl:" + g.id,
    growlCard(g), card ? card.icon || "" : "", "", {
      growl: { id: g.id, key: perm ? growlPermID(g) : "", onShown: n => growlNotes.set(g.id, n) }
    });
}

// The notifications this page made itself, for the browsers with no service worker.
const growlNotes = new Map();

// Closes every growler notification whose growler is no longer open, in this browser. Each browser gets the
// event, so each takes down its own.
function growlReapNotes() {
  growlNotes.forEach((n, id) => { if (!growlIsOpen(id)) { try { n.close(); } catch (e) {} growlNotes.delete(id); } });
  if (typeof swReg === "undefined" || !swReg || !swReg.getNotifications) return;
  swReg.getNotifications().then(list => list.forEach(n => {
    const s = (n.data || {}).subject || "";
    if (s.indexOf("growl:") === 0 && !growlIsOpen(s.slice(6))) n.close();
  })).catch(() => {});
}

// The poll's own sweep closes any notification whose subject is not pending, which a growler's never is.
// Its ids are added to what counts as pending, and the event above does the closing.
const growlRawReap = reapNotifications;
// eslint-disable-next-line no-func-assign
reapNotifications = function (liveKeys) {
  const keys = new Set(liveKeys);
  growlSet.forEach(g => { if (g.state === "open") keys.add("growl:" + g.id); });
  return growlRawReap(keys);
};

// TAB TITLE AND FAVICON. The title alternates between what `retitle` wrote and the top growler while the tab is
// not focused. It is a label tick and asks for nothing. The favicon wears an amber dot with the count for as long
// as any growler is open, focused or not.
let growlTick = 0;
let growlTitleBase = "";
let growlTitleAlt = false;
let growlMark = "";

function growlStopTitle() {
  clearInterval(growlTick);
  growlTick = 0;
  if (growlTitleAlt) document.title = growlTitleBase;
  growlTitleAlt = false;
}

function growlTitleStep() {
  const top = growlDrawn()[0];
  if (!top || document.hasFocus()) { growlStopTitle(); return; }
  if (growlTitleAlt) {
    document.title = growlTitleBase;
    growlTitleAlt = false;
  } else {
    growlTitleBase = document.title;
    document.title = "! " + top.title;
    growlTitleAlt = true;
  }
}

function growlMarkURL(n) {
  const size = 64;
  const c = document.createElement("canvas");
  c.width = c.height = size;
  const g = c.getContext("2d");
  g.fillStyle = "#0B1B2E";
  g.fillRect(0, 0, size, size);
  drawAtriumA(g, size);
  const warn = getComputedStyle(document.documentElement).getPropertyValue("--warn").trim() || "#e0a53a";
  g.fillStyle = warn;
  g.beginPath();
  g.arc(size * 0.7, size * 0.3, size * 0.3, 0, Math.PI * 2);
  g.fill();
  g.fillStyle = "#0B1B2E";
  g.font = "bold " + Math.round(size * 0.4) + "px sans-serif";
  g.textAlign = "center";
  g.textBaseline = "middle";
  g.fillText(n > 9 ? "9+" : String(n), size * 0.7, size * 0.32);
  return c.toDataURL("image/png");
}

function growlAttention() {
  const n = growlDrawn().length;
  const link = document.getElementById("favicon");
  if (link) {
    try {
      if (n) {
        if (growlMark !== String(n)) { link.href = growlMarkURL(n); growlMark = String(n); }
      } else if (growlMark) {
        growlMark = "";
        wearTheMark();
      }
    } catch (e) {}
  }
  // A pop-out whose own switch is off keeps its title still.
  if (n && !document.hasFocus() && !(inPopout() && popoutIsOff())) {
    if (!growlTick) { growlTick = setInterval(growlTitleStep, 1500); growlTitleStep(); }
  } else {
    growlStopTitle();
  }
}
addEventListener("focus", growlAttention);
document.addEventListener("visibilitychange", growlAttention);
