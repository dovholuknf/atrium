// ── a link being opened, drawn as the card it is becoming ───────────────────
//
// A PASTED LINK IS A CARD FROM THE MOMENT IT IS PASTED. Opening one takes as long as a forge read, maybe a clone, a
// fetch and a worktree, and the card itself is only made at the end of that on the room (internal/api/open.go). Until
// then this draws it: a card at the top of `running` on the board and at the top of the terminals list, carrying the
// link's name and the step the room says it is on (`open-progress`, see internal/api/openprogress.go). It replaced a
// line floating over the board, which said the step and nothing about what it was for, and lingered.
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
  linkOpenings.set(id, { url, name: name || "", step: "opening the link", error: "" });
  linkOpeningPaint();
  return id;
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
  o.step = step;
  linkOpeningPaint();
}

function linkOpeningFail(id, why) {
  const o = linkOpenings.get(id);
  if (!o) return;
  o.error = why || "the open failed";
  linkOpeningPaint();
}

// The open answered with its card, or was dropped.
function linkOpeningEnd(id) {
  if (!linkOpenings.delete(id)) return;
  linkOpeningPaint();
}

function linkOpeningHTML(id, o) {
  const name = o.name || o.url;
  const line = o.error
    ? `<div class="linkopen-say warn">${esc(o.error)}</div>
       <div class="linkopen-acts">
         <button type="button" data-op-act="edit">edit</button>
         <button type="button" data-op-act="again">again</button>
         <button type="button" data-op-act="dismiss">dismiss</button>
       </div>`
    : `<div class="linkopen-say"><span class="linkopen-spin" aria-hidden="true"></span>${esc(o.step)}…</div>`;
  return `<div class="card linkopen${o.error ? " failed" : ""}" data-linkopen="${esc(id)}" role="status">
    <div class="card-line"><div class="title">${esc(name)}</div></div>
    ${o.name ? `<div class="linkopen-url">${esc(o.url)}</div>` : ""}
    ${line}
  </div>`;
}

// Drawn into whatever the board and the list drew last, so both renderers call this after their own write.
function linkOpeningPaint() {
  document.querySelectorAll("[data-linkopen]").forEach(el => el.remove());
  if (!linkOpenings.size) return;
  const html = [...linkOpenings].map(([id, o]) => linkOpeningHTML(id, o)).join("");
  const col = document.querySelector('#board section[data-column="running"] .cards') ||
    document.querySelector("#board section .cards");
  if (col) col.insertAdjacentHTML("afterbegin", html);
  const list = document.querySelector("#term-list .termscroll") || document.getElementById("term-list");
  if (list) list.insertAdjacentHTML("afterbegin", html);
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
  linkOpeningEnd(id);
  if (act === "edit") quickPasteGo(o.url, true);
  else if (act === "again") quickPasteGo(o.url, false);
});
