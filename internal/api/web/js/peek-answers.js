// Answering a card's open questions from its peek. Each question opens a flyout with a box for the answer.
// Answers are DRAFTS until sent: they are saved on the card (`answer_drafts`, PATCH /v1/tasks/{id}), not in this
// browser, so the phone and a second window see the same ones, and a reload, a blur or the agent's next turn loses
// nothing. A card holding drafts wears a mail chip (js/seen.js, `answerChip`).
//
// The stored shape is a JSON array of groups, one per turn that asked: {at, qs, a}. `at` is that turn's
// `seen.questions_at`, `qs` its questions as it asked them, `a` the answer to each, "" for none. A group whose `at`
// is not the card's current one is an earlier turn, and its answers are sent marked as such.
// Sending is a say to the card (POST /v1/tasks/{id}/message), so it is queued and never typed over a human.

const QA_SAVE_MS = 350;
// A question in the peek shows three lines, and the rest behind "click to expand". Which ones are open is kept
// here, by card and question, so a repaint of the block does not shut them.
const qaOpenSet = new Set();

let qaFly = null;
// What the flyout is on: the card, and the group and question it shows. `groups` is the working copy.
let qaOn = null, qaTimer = null;

function qaParse(t) {
  try {
    const v = JSON.parse((t && t.answer_drafts) || "[]");
    return Array.isArray(v) ? v.filter(g => g && Array.isArray(g.qs) && Array.isArray(g.a)) : [];
  } catch (e) { return []; }
}

function qaOpenQs(t) {
  const s = (t && t.seen) || {};
  return s.answered === false && Array.isArray(s.open_questions) ? s.open_questions : [];
}

// The card's groups: earlier turns that hold an answer, then the current turn's, made up when nothing is stored.
function qaGroups(t) {
  const at = (t.seen && t.seen.questions_at) || "";
  const qs = qaOpenQs(t);
  const stored = qaParse(t);
  const out = stored.filter(g => g.at !== at || !qs.length).map(g => ({ ...g, current: false }));
  if (qs.length) {
    const own = stored.find(g => g.at === at);
    const a = qs.map((_, i) => (own && typeof own.a[i] === "string" ? own.a[i] : ""));
    out.push({ at, qs, a, current: true });
  }
  return out.filter(g => g.current || g.a.some(x => x.trim()));
}

function qaAnswered(groups) { return groups.reduce((n, g) => n + g.a.filter(x => x.trim()).length, 0); }
// What there is to answer: all of the current turn's questions, and only the answered ones of an earlier turn.
function qaTotal(groups) { return groups.reduce((n, g) => n + (g.current ? g.qs.length : g.a.filter(x => x.trim()).length), 0); }
function qaDraftCount(t) { return t ? qaParse(t).reduce((n, g) => n + g.a.filter(x => String(x).trim()).length, 0) : 0; }

function qaClock(iso) {
  const d = new Date(iso);
  return isNaN(d) ? "" : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
}
// A question in as few words as will still say which one: its first four, without the trailing mark.
function qaShort(q) {
  return String(q).replace(/^[\s\W_]+/, "").split(/\s+/).slice(0, 4).join(" ").replace(/[\s?:.,;!]+$/, "");
}

// The one message the answers become. `re 10:12 Q2 (autocompact source): use the hub value` for an earlier turn's
// question, without the `re` for the current turn's. An unanswered question is left out.
function qaCompose(groups) {
  const lines = [];
  for (const g of groups) {
    const tag = g.current ? "" : "re " + (qaClock(g.at) || "earlier") + " ";
    g.qs.forEach((q, i) => {
      const a = String(g.a[i] || "").replace(/\s+/g, " ").trim();
      if (a) lines.push(`${tag}Q${i + 1} (${qaShort(q)}): ${a}`);
    });
  }
  return lines.join("\n");
}

function qaCard(id) { return typeof peekCard === "function" ? peekCard(id) : null; }

async function qaSaveNow(id, groups) {
  clearTimeout(qaTimer);
  qaTimer = null;
  const keep = groups.filter(g => g.a.some(x => x.trim())).map(g => ({ at: g.at, qs: g.qs, a: g.a }));
  const raw = keep.length ? JSON.stringify(keep) : "";
  const t = qaCard(id);
  if (t) { if (raw) t.answer_drafts = raw; else delete t.answer_drafts; }
  try {
    await api(`/v1/tasks/${encodeURIComponent(id)}`, {
      method: "PATCH", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ answer_drafts: raw }),
    });
  } catch (e) {
    toast("could not save the draft", e.message);
    return;
  }
  if (typeof refreshSoon === "function") refreshSoon();
}

function qaSaveSoon() {
  if (!qaOn) return;
  const { id, groups } = qaOn;
  clearTimeout(qaTimer);
  qaTimer = setTimeout(() => qaSaveNow(id, groups), QA_SAVE_MS);
}

// Writes out a draft still waiting on its timer. Called on anything that could end the page's life of it.
function qaFlush() {
  if (qaTimer && qaOn) qaSaveNow(qaOn.id, qaOn.groups);
}

// ── the rows in the peek ────────────────────────────────────────────────────

// The open questions, as the peek draws them: each a button that opens the flyout, an earlier turn's drafts under
// theirs, and the send button once there is something to send.
function peekQuestionsHtml(t) {
  const groups = qaOn && qaOn.id === t.id ? qaOn.groups : qaGroups(t);
  const done = qaAnswered(groups), total = qaTotal(groups);
  const q = (g, gi, i) => {
    const a = String(g.a[i] || "").trim();
    return `<button type="button" class="qa-q${a ? " has" : ""}" data-gi="${gi}" data-qi="${i}">` +
      `<i class="qa-n">${a ? "&#10003;" : i + 1}</i>` +
      `<span class="qa-t"><span class="qa-qx${qaOpenSet.has(`${t.id}:${gi}:${i}`) ? " open" : ""}">${esc(g.qs[i])}</span>` +
      `${a ? `<em>${esc(a)}</em>` : ""}</span></button>` +
      `<button type="button" class="qa-exp" data-gi="${gi}" data-qi="${i}" hidden></button>`;
  };
  let html = "";
  groups.forEach((g, gi) => {
    if (!g.current) html += `<div class="qa-old">earlier turn${qaClock(g.at) ? ", " + esc(qaClock(g.at)) : ""}</div>`;
    g.qs.forEach((_, i) => { if (g.current || String(g.a[i] || "").trim()) html += q(g, gi, i); });
  });
  if (done) {
    const all = done >= total;
    html += `<div class="qa-bar"><button type="button" class="qa-send${all ? " ready" : ""}" data-qa-send>` +
      `${all ? "send to agent now" : "send answers to agent"}${all ? "" : ` &middot; ${done} of ${total}`}</button>` +
      `<button type="button" class="qa-drop" data-qa-drop data-tip="throw the drafts away. nothing is sent">discard</button></div>`;
  }
  return `<div class="peek-row peek-qrow"><div class="peek-qa" data-id="${esc(t.id)}">${html}</div></div>`;
}

// Draws the block again wherever a peek or drawer shows this card.
function qaRepaint(id) {
  const t = qaCard(id);
  if (!t) return;
  document.querySelectorAll(".peek-qa").forEach(el => {
    if (el.dataset.id !== id) return;
    const row = el.closest(".peek-qrow");
    if (row) row.outerHTML = peekQuestionsHtml(t);
  });
  qaClamp(document);
}

// Offers "click to expand" under each question that runs past its three lines, and "show less" under an open one.
// Only measurable while shown, so it runs again whenever a drawer opens.
function qaClamp(root) {
  for (const x of (root || document).querySelectorAll(".peek-qa .qa-qx")) {
    const btn = x.closest(".qa-q"), exp = btn && btn.nextElementSibling;
    if (!exp || !exp.classList.contains("qa-exp")) continue;
    const open = x.classList.contains("open");
    exp.hidden = !open && !(x.scrollHeight > x.clientHeight + 1);
    exp.textContent = open ? "show less" : "click to expand";
  }
}

// ── the flyout ──────────────────────────────────────────────────────────────

function qaFlyEl() {
  if (qaFly) return qaFly;
  qaFly = document.createElement("div");
  qaFly.className = "qa-fly";
  qaFly.setAttribute("role", "dialog");
  qaFly.setAttribute("aria-label", "answer a question");
  document.body.appendChild(qaFly);
  qaFly.addEventListener("input", e => {
    if (!qaOn || !e.target.matches("textarea")) return;
    qaOn.groups[qaOn.gi].a[qaOn.qi] = e.target.value;
    qaFlyProgress();
    qaRepaint(qaOn.id);
    qaSaveSoon();
  });
  qaFly.addEventListener("keydown", e => {
    if (e.key === "Escape") { e.stopPropagation(); qaCloseFly(); return; }
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); qaFlyEnter(); }
  });
  qaFly.addEventListener("click", e => {
    const b = e.target.closest("button");
    if (!b) return;
    if (b.dataset.step) qaFlyStep(Number(b.dataset.step));
    else if (b.hasAttribute("data-qa-send")) qaSend();
    else if (b.hasAttribute("data-qa-close")) qaCloseFly();
    else if (b.dataset.dot) qaFlyGo(Number(b.dataset.dot));
  });
  return qaFly;
}

// Every question in the working copy, flat, in the order the flyout steps through them.
function qaFlat() {
  const out = [];
  qaOn.groups.forEach((g, gi) => g.qs.forEach((_, qi) => { if (g.current || String(g.a[qi] || "").trim()) out.push([gi, qi]); }));
  return out;
}

function qaFlyDraw() {
  const g = qaOn.groups[qaOn.gi], flat = qaFlat();
  const at = flat.findIndex(([gi, qi]) => gi === qaOn.gi && qi === qaOn.qi);
  const dots = flat.map(([gi, qi], n) => {
    const has = String(qaOn.groups[gi].a[qi] || "").trim();
    return `<button type="button" class="qa-dot${n === at ? " on" : ""}${has ? " has" : ""}" data-dot="${n}" ` +
      `aria-label="question ${n + 1}"></button>`;
  }).join("");
  const el = qaFlyEl();
  el.innerHTML = `<div class="qa-fh"><span class="qa-fk">${g.current ? "question" : "earlier question"} ` +
    `<b>${qaOn.qi + 1}</b>${g.qs.length > 1 ? ` of ${g.qs.length}` : ""}</span><span class="qa-dots">${dots}</span>` +
    `<button type="button" class="qa-x" data-qa-close aria-label="close">&times;</button></div>` +
    `<div class="qa-fq">${esc(g.qs[qaOn.qi])}</div>` +
    `<textarea class="qa-box" rows="5" spellcheck="true" placeholder="your answer. it is kept as a draft until you send it"></textarea>` +
    `<div class="qa-ff"><span class="qa-prog"></span><span class="qa-grow"></span>` +
    (at > 0 ? `<button type="button" class="qa-b" data-step="-1">back</button>` : "") +
    (at < flat.length - 1 ? `<button type="button" class="qa-b go" data-step="1">next</button>` : "") +
    `<button type="button" class="qa-b send" data-qa-send hidden>send to agent now</button></div>` +
    `<div class="qa-hint">${/Mac|iPhone|iPad/.test(navigator.platform || "") ? "&#8984;" : "ctrl"} + enter <span>for the next one</span></div>`;
  const box = el.querySelector("textarea");
  box.value = g.a[qaOn.qi] || "";
  qaFlyProgress();
  qaFlyPlace();
  el.classList.add("on");
  box.focus();
  box.setSelectionRange(box.value.length, box.value.length);
}

// "2 of 3 answered", and the send button once every question has one.
function qaReady() { const t = qaTotal(qaOn.groups); return t > 0 && qaAnswered(qaOn.groups) >= t; }

function qaFlyProgress() {
  if (!qaFly || !qaOn) return;
  const done = qaAnswered(qaOn.groups), total = qaTotal(qaOn.groups);
  const p = qaFly.querySelector(".qa-prog");
  if (p) p.textContent = `${done} of ${total} answered`;
  const s = qaFly.querySelector(".qa-b.send");
  if (s) s.hidden = !qaReady();
  const h = qaFly.querySelector(".qa-hint span");
  if (h) h.textContent = s.hidden ? "for the next one" : "to send to the agent";
  const n = qaFly.querySelector(".qa-b.go");
  if (n && s) n.classList.toggle("quiet", !s.hidden);
  const dot = qaFly.querySelectorAll(".qa-dot");
  const flat = qaFlat();
  dot.forEach((d, i) => {
    const [gi, qi] = flat[i];
    d.classList.toggle("has", !!String(qaOn.groups[gi].a[qi] || "").trim());
  });
}

// Beside the peek, on whichever side has room, else under it. Anything narrower is a sheet along the bottom (css).
function qaFlyPlace() {
  const el = qaFlyEl();
  const tray = document.getElementById("toastlog");
  const inTray = !!(tray && tray.open);
  // A modal dialog is in the top layer and makes the rest of the page inert, so the flyout goes inside it.
  if (inTray && el.parentNode !== tray) tray.appendChild(el);
  else if (!inTray && el.parentNode !== document.body) document.body.appendChild(el);
  if (inTray) {
    qaFlyAtTray(el, tray);
    return;
  }
  el.classList.remove("intray");
  el.style.clipPath = "";
  const host = inTray ? tray : (peekEl && peekEl.classList.contains("on") ? peekEl : document.getElementById("t-drawer"));
  const r = host ? host.getBoundingClientRect() : { left: innerWidth / 2 - 180, right: innerWidth / 2 + 180, top: 80, bottom: 200 };
  el.style.width = Math.min(360, innerWidth - 16) + "px";
  popFit(el, r, { axis: "x", gap: 10 });
}

// In the tray the drawer is the tray's own: flush against its left edge, its top on the question that was clicked and
// clamped inside the tray's height, wiped out from that edge. The tray closing closes it.
function qaFlyAtTray(el, tray) {
  if (!tray.dataset.qaDrawer) {
    tray.dataset.qaDrawer = "1";
    tray.addEventListener("close", () => { if (qaOn) qaCloseFly(); });
  }
  const t = tray.getBoundingClientRect();
  const w = Math.max(220, Math.min(360, Math.round(t.left) - 8));
  el.classList.add("intray");
  el.style.width = w + "px";
  el.style.left = Math.round(t.left - w) + "px";
  const q = qaOn && tray.querySelector(`.peek-qa[data-id="${CSS.escape(qaOn.id)}"] .qa-q[data-gi="${qaOn.gi}"][data-qi="${qaOn.qi}"]`);
  const want = q ? q.getBoundingClientRect().top : t.top;
  const h = el.offsetHeight;
  el.style.top = Math.round(Math.min(Math.max(want, t.top), Math.max(t.top, t.bottom - h))) + "px";
}

function qaFlyGo(n) {
  const flat = qaFlat();
  if (!qaOn || n < 0 || n >= flat.length) return;
  [qaOn.gi, qaOn.qi] = flat[n];
  qaFlyDraw();
  qaRepaint(qaOn.id);
}

function qaFlyStep(d, wrap) {
  const flat = qaFlat();
  const at = flat.findIndex(([gi, qi]) => gi === qaOn.gi && qi === qaOn.qi);
  let n = at + d;
  if (n >= flat.length) { if (!wrap) return; qaFlush(); qaCloseFly(); return; }
  qaFlyGo(Math.max(0, n));
}

// Ctrl/cmd+enter: send once every question has an answer, else on to the next one. On the last it goes to the first
// question still unanswered, never out of the flyout.
function qaFlyEnter() {
  if (!qaOn) return;
  if (qaReady()) { qaSend(); return; }
  const flat = qaFlat();
  const at = flat.findIndex(([gi, qi]) => gi === qaOn.gi && qi === qaOn.qi);
  if (at < flat.length - 1) { qaFlyGo(at + 1); return; }
  const first = flat.findIndex(([gi, qi]) => !String(qaOn.groups[gi].a[qi] || "").trim());
  if (first >= 0 && first !== at) qaFlyGo(first);
}

function qaOpenFly(id, gi, qi) {
  const t = qaCard(id);
  if (!t) return;
  if (!qaOn || qaOn.id !== id) qaOn = { id, groups: qaGroups(t), gi, qi };
  else { qaOn.gi = gi; qaOn.qi = qi; }
  // Pinned: a flyout open over a hover-opened peek must not go when the pointer wanders off to it.
  if (typeof peekMode !== "undefined" && peekMode === "hover") {
    peekMode = "menu";
    if (peekEl) peekEl.classList.add("pinned");
  }
  qaFlyDraw();
}

function qaCloseFly() {
  qaFlush();
  if (qaFly) qaFly.classList.remove("on");
  const id = qaOn && qaOn.id;
  qaOn = null;
  if (id) qaRepaint(id);
  if (typeof trayQuestionsMoved === "function") trayQuestionsMoved();
}

// Called by closePeek: the flyout goes with the peek, and its draft is written out.
function qaPeekClosed() { if (qaOn) qaCloseFly(); }

async function qaSend() {
  if (!qaOn) return;
  const { id, groups } = qaOn;
  const text = qaCompose(groups);
  if (!text) return;
  clearTimeout(qaTimer);
  try {
    await api(`/v1/tasks/${encodeURIComponent(id)}/message`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text }),
    });
  } catch (e) {
    toast("did not send the answers", e.message + ". they are still saved as drafts");
    return;
  }
  qaOn.groups = [];
  await qaSaveNow(id, []);
  qaCloseFly();
  toast("answers sent", "they reach the agent when its input is clear");
}

// ── the clicks in the peek ──────────────────────────────────────────────────

document.addEventListener("click", async e => {
  const b = e.target.closest && e.target.closest(".peek-qa button");
  if (!b) return;
  const id = b.closest(".peek-qa").dataset.id;
  if (b.classList.contains("qa-exp")) {
    e.stopPropagation();
    const key = `${id}:${b.dataset.gi}:${b.dataset.qi}`;
    if (!qaOpenSet.delete(key)) qaOpenSet.add(key);
    const x = b.previousElementSibling.querySelector(".qa-qx");
    x.classList.toggle("open", qaOpenSet.has(key));
    qaClamp(b.parentElement);
    // The popup grows to the text and is fitted again rather than scrolling.
    if (typeof peekRefit === "function") peekRefit();
  } else if (b.classList.contains("qa-q")) {
    e.stopPropagation();
    qaOpenFly(id, Number(b.dataset.gi), Number(b.dataset.qi));
  } else if (b.hasAttribute("data-qa-send")) {
    e.stopPropagation();
    const t = qaCard(id);
    if (!t) return;
    qaOn = { id, groups: qaGroups(t), gi: 0, qi: 0 };
    await qaSend();
  } else if (b.hasAttribute("data-qa-drop")) {
    e.stopPropagation();
    qaOn = null;
    await qaSaveNow(id, []);
    qaRepaint(id);
  }
});

window.addEventListener("pagehide", qaFlush);
document.addEventListener("visibilitychange", () => { if (document.visibilityState === "hidden") qaFlush(); });

// An envelope drawn rather than typed. The old U+2709 glyph came from whichever fallback font had it, and its ink
// sat low in the line box, so it never centred against the count beside it. A box centres the same everywhere.
const MAIL_SVG = `<svg viewBox="0 0 12 9" aria-hidden="true"><rect x=".65" y=".65" width="10.7" height="7.7" rx="1"/>` +
  `<path d="M1 1.4 6 5.4 11 1.4"/></svg>`;

// THE MAIL CHIP: a card holding drafted answers that were not sent. Click opens its peek, which offers to send them.
function answerChip(t) {
  const n = qaDraftCount(t);
  if (!n) return "";
  const tip = `${n} drafted answer${n === 1 ? "" : "s"} waiting on you to send. click to open them`;
  return `<span class="chip warn mail" role="button" tabindex="0" data-tip="${esc(tip)}" data-id="${esc(t.id)}" ` +
    `onclick="event.stopPropagation();openPeek(this.dataset.id,this,'menu')" ` +
    `onkeydown="if(event.key==='Enter'||event.key===' '){event.preventDefault();event.stopPropagation();openPeek(this.dataset.id,this,'menu')}"` +
    `>${MAIL_SVG}${n > 1 ? `<span class="mail-n">${n}</span>` : ""}</span>`;
}
