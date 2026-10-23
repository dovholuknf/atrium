// ── a link being opened, drawn as the card it is becoming ───────────────────
//
// A PASTED LINK IS A CARD FROM THE MOMENT IT IS PASTED. Opening one takes as long as a forge read, maybe a clone, a
// fetch and a worktree, and the card itself is only made at the end of that on the room (internal/api/open.go). Until
// then this draws it: a card floating in the lower left over whatever view is up, carrying the link's name and the step
// the room says it is on (`open-progress`, see internal/api/openprogress.go). It used to sit at the top of the card
// list, which pushed every card down while the open ran and back up when it ended.
//
// A FAILED OPEN STAYS AS THE CARD, with the reason on it, instead of throwing the launch dialog up. `edit` opens the
// dialog filled in (the shift-enter path), `again` repeats the open, `dismiss` drops it. Nothing here is stored: it is
// the page's own request in flight, and a reload has no request to draw.
//
// Keyed by the progress id the open sends, so a room's step lands on the card it is about.
const linkOpenings = new Map();

function linkOpeningId() {
  return "op-" + Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
}

// A card for url. `name` is what the link was read as when that is known yet.
function linkOpeningBegin(id, url, name) {
  linkOpenings.set(id, { url, name: name || "", step: "opening the link", since: Date.now(), error: "" });
  linkOpeningPaint();
  linkOpeningTick();
  return id;
}

// A step that runs long says for how long, so a slow one reads as working and not as stuck. The timer runs only while
// an opening is in flight.
let linkOpeningTimer = 0;

function linkOpeningElapsed(o) {
  const s = Math.floor((Date.now() - o.since) / 1000);
  return s > 3 ? " " + s + "s" : "";
}

function linkOpeningTick() {
  if (!linkOpenings.size) {
    clearInterval(linkOpeningTimer);
    linkOpeningTimer = 0;
    return;
  }
  if (!linkOpeningTimer) linkOpeningTimer = setInterval(() => {
    if (!linkOpenings.size) { linkOpeningTick(); return; }
    linkOpeningPaint();
  }, 1000);
}

function linkOpeningName(id, name) {
  const o = linkOpenings.get(id);
  if (!o || !name) return;
  o.name = name;
  linkOpeningPaint();
}

function linkOpeningStep(id, step) {
  const o = linkOpenings.get(id);
  if (!o || o.error || !step) return;
  if (o.step !== step) o.since = Date.now();
  o.step = step;
  linkOpeningPaint();
}

// The open has done what it can without somebody saying so, and waits on the card for start or cancel. `start` runs once.
function linkOpeningConfirm(id, text, start) {
  const o = linkOpenings.get(id);
  if (!o) return;
  o.confirm = { text, start };
  linkOpeningPaint();
}

function linkOpeningFail(id, why, heldCard) {
  const o = linkOpenings.get(id);
  if (!o) return;
  o.error = why || "the open failed";
  // A link another card holds says which: `go to card` is then the first button, the default.
  o.card = heldCard || "";
  linkOpeningPaint();
  toast(o.name || o.url, o.error);
}

// The open answered with its card, or was dropped.
function linkOpeningEnd(id) {
  if (!linkOpenings.delete(id)) return;
  linkOpeningPaint();
  linkOpeningTick();
}

function linkOpeningHTML(id, o) {
  const name = o.name || o.url;
  const line = o.confirm
    ? `<div class="linkopen-say warn">${esc(o.confirm.text)}</div>
       <div class="linkopen-acts">
         <button type="button" class="primary" data-op-act="start">start</button>
         <button type="button" data-op-act="cancel">cancel</button>
       </div>`
    : o.error
    ? `<div class="linkopen-say warn">${esc(o.error)}</div>
       <div class="linkopen-acts">
         ${o.card ? `<button type="button" class="primary" data-op-act="goto">go to card</button>` : ""}
         <button type="button" data-op-act="edit">edit</button>
         <button type="button" data-op-act="again">again</button>
         <button type="button" data-op-act="dismiss">dismiss</button>
       </div>`
    : `<div class="linkopen-say"><span class="linkopen-spin" aria-hidden="true"></span>${esc(o.step)}…${linkOpeningElapsed(o)}</div>`;
  return `<div class="card linkopen${o.error ? " failed" : ""}" data-linkopen="${esc(id)}" role="status">
    <div class="card-line"><div class="title">${esc(name)}</div></div>
    ${o.name ? `<div class="linkopen-url">${esc(o.url)}</div>` : ""}
    ${line}
  </div>`;
}

// One dock on the body, outside every list, so no card moves when an opening comes or goes.
function linkOpeningPaint() {
  let dock = document.getElementById("linkopen-dock");
  if (!linkOpenings.size) { if (dock) dock.remove(); return; }
  if (!dock) {
    dock = document.createElement("div");
    dock.id = "linkopen-dock";
    document.body.appendChild(dock);
  }
  setHTML(dock, [...linkOpenings].map(([id, o]) => linkOpeningHTML(id, o)).join(""));
}

document.addEventListener("click", e => {
  const b = e.target && e.target.closest ? e.target.closest("[data-op-act]") : null;
  if (!b) return;
  const card = b.closest("[data-linkopen]");
  const id = card && card.dataset.linkopen;
  const o = id && linkOpenings.get(id);
  if (!o) return;
  e.stopPropagation();
  const act = b.dataset.opAct;
  if (act === "start") {
    const go = o.confirm && o.confirm.start;
    o.confirm = null;
    o.step = "starting";
    linkOpeningPaint();
    if (go) go();
    return;
  }
  linkOpeningEnd(id);
  if (act === "edit") quickPasteGo(o.url, true);
  else if (act === "again") quickPasteGo(o.url, false);
  else if (act === "goto") linkOpeningGoto(o.card);
});

// The link is held by a live card, and the person almost always wanted that card: bring it up, in its own window when
// it is popped out. The same landing as the answer of a link the holder has live (see launchGo in fixtures.js).
async function linkOpeningGoto(cardId) {
  const card = cardId ? await api("/v1/tasks/" + encodeURIComponent(cardId)).catch(() => null) : null;
  if (!card) { switchView("board"); return; }
  if (poppedOut(card.id)) { popOutTask(card.id); return; }
  switchView("terms");
  openTerm(card);
}
