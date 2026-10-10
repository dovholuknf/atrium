// HELD NOTICES MADE VISIBLE. A card that holds its notices (atrium:hold-notices, or the orchestrator) is not typed
// at: what its workers report waits on its card and is read when it asks. Nothing on the board said so, and two
// finished interviewers sat unread for a whole evening while the operator believed atrium was broken.
//
// The count comes from the row (`held_notices`, from stored events, see internal/daemon/fyi.go). This draws it as an
// icon and a number wherever a card's chips are drawn, and a click opens the notices: who, what and when. Opening them
// is the read, so the count clears: the same `notices-read` an `atrium_task` with `notices` posts.

const HELD_NOTICES_ICON = "&#128235;&#xFE0E;";

function heldNoticesOf(t) { return Number(t && t.held_notices) || 0; }

// The chip, or "" when nothing is held. A click on it opens the list and never the card behind it.
function heldNoticesChip(t) {
  const n = heldNoticesOf(t);
  if (!n) return "";
  const tip = `${n} held ${n === 1 ? "notice" : "notices"} on this card, not typed to it. click to read ` +
    (n === 1 ? "it" : "them") + (t.oldest_held_at ? ". the oldest was held " + hnWhen(t.oldest_held_at) : "");
  return `<span class="chip hnotices" role="button" tabindex="0" data-hn="${esc(t.id)}" aria-label="${esc(tip)}"
    data-tip="${esc(tip)}">${HELD_NOTICES_ICON}<b>${n}</b></span>`;
}

function hnWhen(iso) {
  const ms = Date.parse(iso);
  if (isNaN(ms)) return "";
  const secs = Math.max(0, (Date.now() - ms) / 1000);
  return (typeof ago === "function" ? ago(secs) : Math.round(secs) + "s") + " ago";
}

let hnSeq = 0;

function closeHeldNotices() {
  const el = document.getElementById("hn-dialog");
  if (el) el.remove();
}

// The list, then the read. `through` is the newest notice shown and never now, so one held while the list was open
// stays counted.
async function openHeldNotices(id) {
  closeHeldNotices();
  const card = (typeof peekCard === "function" ? peekCard(id) : null) || { id };
  const name = card.display_title || card.title || "this card";
  const el = document.createElement("div");
  el.id = "hn-dialog";
  el.className = "hn-back";
  el.innerHTML = `<div class="hn-box" role="dialog" aria-label="held notices"><div class="hn-head"><b>held notices</b>
    <span class="hn-for">${esc(name)}</span><button type="button" class="hn-x" aria-label="close">&times;</button></div>
    <div class="hn-list"><div class="hn-none">reading them</div></div></div>`;
  document.body.appendChild(el);
  el.addEventListener("click", e => { if (e.target === el || e.target.closest(".hn-x")) closeHeldNotices(); });
  const seq = ++hnSeq;
  let list = [];
  try {
    const r = await fetch("/v1/tasks/" + encodeURIComponent(id) + "/notices");
    if (!r.ok) throw new Error("answered " + r.status);
    list = (await r.json()).notices || [];
  } catch (e) {
    if (seq === hnSeq) el.querySelector(".hn-list").innerHTML = `<div class="hn-none">could not read them: ${esc(e.message)}</div>`;
    return;
  }
  if (seq !== hnSeq) return;
  el.querySelector(".hn-list").innerHTML = list.length
    ? list.map(n => `<div class="hn-item"><div class="hn-meta"><b class="hn-who">${esc(n.about || "atrium")}</b>
        <span class="hn-src">${esc(n.source || "")}</span>
        <span class="hn-at" title="${esc(n.at || "")}">${esc(hnWhen(n.at))}</span></div>
        <div class="hn-text">${esc(n.text || "")}</div></div>`).join("")
    : `<div class="hn-none">nothing is held. it has all been read.</div>`;
  if (!list.length) return;
  try {
    await fetch("/v1/tasks/" + encodeURIComponent(id) + "/notices-read", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ through: list[list.length - 1].at }),
    });
  } catch (e) { return; }
  // The room publishes the card too. This is only so the count does not wait on that event.
  for (const t of [typeof lastTasks !== "undefined" ? lastTasks.find(x => x.id === id) : null,
    typeof termTask !== "undefined" && termTask && termTask.id === id ? termTask : null]) {
    if (t) { t.held_notices = 0; delete t.oldest_held_at; }
  }
  if (typeof repaintLists === "function") repaintLists(undefined, true);
}

// Delegated, so every surface that draws the chip gets it, and a repaint cannot lose it. A drag that selected text
// never reaches here: see js/dragclick.js.
document.addEventListener("click", e => {
  const c = e.target && e.target.closest && e.target.closest(".chip.hnotices");
  if (!c) return;
  e.stopPropagation();
  e.preventDefault();
  openHeldNotices(c.dataset.hn);
}, true);
document.addEventListener("keydown", e => {
  if (e.key === "Escape") closeHeldNotices();
  else if ((e.key === "Enter" || e.key === " ") && e.target && e.target.matches && e.target.matches(".chip.hnotices")) {
    e.preventDefault();
    openHeldNotices(e.target.dataset.hn);
  }
});
