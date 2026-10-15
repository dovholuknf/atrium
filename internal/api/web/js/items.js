// The items tab: every item on the hub, grouped by what it needs. HUB-ONLY, like the backlog: `/_hub/items` is a 404 on a
// plain daemon, so the tab shows once the hub probe in rooms.js has found a hub, through `paintItemsTab`.
//
// Groups, in this order: blocked (waiting on a person) first, then running, then queued, then finished, folded shut.
// A cancelled item is a finished one whose note says so. Inside a group the newest change is first, which is the order
// the hub answers in.
//
// A blocked row carries its question and an answer box, so answering is no extra click. The board acts as the human
// owner: it sends `by: board`, which the hub takes from its own machine only (internal/link/items.go, ownerMayAct).
// `cancel` is behind a confirm on every item that is not finished.
//
// The count on the tab is the items blocked on the human. Live by the hub's `item` event, and polled every 5s while the
// tab is on screen as a backstop, since an event is not guaranteed to reach a board that was asleep.
// See docs/changes/items-view.md.
let itemsList = [];
let itemsOpen = null;          // the id whose detail is open
let itemsDetail = null;        // what GET /_hub/items/<id> answered for it
const itemsFolded = { finished: true };
const itemsDraft = {};         // an answer typed and not yet sent, by id, so a repaint does not eat it
let itemsTimer = 0;

const ITEM_GROUPS = [
  { state: "blocked", label: "blocked on a person" },
  { state: "running", label: "running" },
  { state: "queued", label: "queued" },
  { state: "finished", label: "finished and cancelled" },
];

function paintItemsTab() {
  const tab = document.querySelector('.tab[data-view="items"]');
  if (!tab) return;
  tab.hidden = typeof hubIsHub === "undefined" ? true : !hubIsHub;
  if (!tab.hidden) loadItemsBadge();
}

async function itemsFetch(path, init) {
  const fetcher = typeof plainFetch === "function" ? plainFetch : window.fetch.bind(window);
  const res = await fetcher(path, init);
  let body = null;
  try { body = await res.json(); } catch (e) { /* an empty body is fine */ }
  if (!res.ok) throw new Error((body && body.error) || res.status + "");
  return body;
}

function itemsVisible() {
  const v = document.getElementById("items");
  return !!v && !v.hidden && !document.hidden;
}

function itemBlockedOnHuman(it) { return it.state === "blocked" && it.owner_kind === "human"; }

function itemsBadge(n) {
  const el = document.getElementById("c-items");
  if (!el) return;
  el.textContent = n || "";
  el.classList.toggle("zero", !n);
}

// The count alone, for when the tab is not the one on screen.
async function loadItemsBadge() {
  try {
    const body = await itemsFetch("/_hub/items?owner_kind=human&state=blocked");
    itemsBadge(((body && body.items) || []).length);
  } catch (e) { /* the badge keeps what it had */ }
}

async function loadItems() {
  const box = document.getElementById("items-list");
  if (!box) return;
  try {
    const body = await itemsFetch("/_hub/items");
    itemsList = (body && body.items) || [];
    itemsBadge(itemsList.filter(itemBlockedOnHuman).length);
    itemsPaint();
  } catch (e) {
    box.innerHTML = '<p class="pane-lead">The items are not answering right now.</p>';
  }
  if (itemsOpen) loadItemDetail(itemsOpen, true);
}

function itemsMs(iso) {
  const t = iso ? Date.parse(iso) : NaN;
  return isNaN(t) ? null : t;
}

// How long it has sat in this state, from the last change.
function itemAge(iso) {
  const t = itemsMs(iso);
  if (t == null) return "";
  const s = Math.max(0, (Date.now() - t) / 1000);
  if (s < 45) return "just now";
  if (s < 3600) return Math.round(s / 60) + "m";
  if (s < 86400) return Math.round(s / 3600) + "h";
  return Math.round(s / 86400) + "d";
}

function itemCancelled(it) { return it.state === "finished" && /cancelled/i.test(it.note || ""); }
function itemStateWord(it) { return itemCancelled(it) ? "cancelled" : it.state; }

function itemCard(it) {
  if (typeof lastTasks === "undefined") return null;
  return lastTasks.find(x => x.id === it.card && (!x.room || !it.room || x.room === it.room)) || null;
}

// The board has no title for an item, so the card that runs it supplies one.
function itemTitle(it) {
  const t = itemCard(it);
  return (t && (t.display_title || t.title)) || it.card || it.item_id;
}

function itemOwnerText(it) {
  if (it.owner_kind === "human") return "you";
  if (it.owner_kind === "role") return it.owner + " role";
  return it.owner || "";
}

function itemRow(it) {
  const id = esc(it.item_id);
  const blocked = it.state === "blocked";
  const worker = (itemCard(it) && itemCard(it).wire_name) || it.card;
  const open = itemsOpen === it.item_id;
  let h = '<div class="it-row st-' + esc(itemStateWord(it)) + (open ? " open" : "") + '" data-item="' + id + '">' +
    '<div class="it-main" role="button" tabindex="0" aria-expanded="' + open + '" data-act="open" data-id="' + id + '">' +
    '<span class="it-title">' + esc(itemTitle(it)) + "</span>" +
    '<span class="it-state">' + esc(itemStateWord(it)) + "</span>" +
    '<span class="it-owner" data-tip="owner of the item">' + esc(itemOwnerText(it)) + "</span>" +
    '<button type="button" class="it-card" data-act="card" data-id="' + id + '" data-card="' + esc(it.card) +
    '" data-tip="open this card\'s terminal">' + esc(worker) + "@" + esc(it.room) + "</button>" +
    '<span class="it-age" data-tip="' + esc(it.updated_at || "") + '">' + esc(itemAge(it.updated_at)) + "</span>" +
    '<span class="it-id">' + id + "</span>" +
    (it.state !== "finished" ? '<button type="button" class="it-cancel" data-act="cancel" data-id="' + id +
      '" aria-label="cancel item ' + id + '">cancel</button>' : "") +
    "</div>";
  if (blocked) {
    h += '<div class="it-ask"><div class="it-q">' + esc(it.note || "(no reason given)") + "</div>" +
      '<div class="it-answer"><input type="text" class="it-answer-box" data-id="' + id + '" placeholder="answer" ' +
      'aria-label="answer for ' + id + '" value="' + esc(itemsDraft[it.item_id] || "") + '">' +
      '<button type="button" class="it-send" data-act="answer" data-id="' + id + '">answer</button></div></div>';
  }
  if (open) h += itemDetailHtml(it);
  return h + "</div>";
}

function itemDetailHtml(it) {
  const d = itemsDetail && itemsDetail.item_id === it.item_id ? itemsDetail : null;
  const hist = d && d.history ? d.history : [];
  const line = (k, v) => v ? '<dt>' + k + "</dt><dd>" + v + "</dd>" : "";
  const result = it.state === "finished" ? "result" : it.state === "blocked" ? "asked" : "last note";
  return '<div class="it-detail" data-detail="' + esc(it.item_id) + '"><dl>' +
    line(result, esc(it.note || "")) +
    line("created by", esc(it.created_by || "")) +
    line("created", esc(it.created_at || "")) +
    line("changed", esc(it.updated_at || "")) +
    line("card", '<button type="button" class="it-card" data-act="card" data-card="' + esc(it.card) + '">' +
      esc(it.card) + "@" + esc(it.room) + "</button>") +
    line("revision", String(it.revision)) +
    "</dl>" +
    '<ol class="it-history">' + (hist.length ? hist.map(e => '<li><b>' + esc(e.state) + "</b> r" + e.revision +
      ' <time data-tip="' + esc(e.at) + '">' + esc(itemAge(e.at)) + " ago</time></li>").join("")
      : "<li>" + (d ? "no history" : "loading") + "</li>") + "</ol></div>";
}

function itemsPaint() {
  const box = document.getElementById("items-list");
  if (!box) return;
  // What is being typed survives the repaint: the answer box, with the caret where it was.
  const had = document.activeElement && document.activeElement.classList && document.activeElement.classList.contains("it-answer-box")
    ? { id: document.activeElement.dataset.id, at: document.activeElement.selectionStart } : null;
  for (const el of box.querySelectorAll(".it-answer-box")) itemsDraft[el.dataset.id] = el.value;
  if (!itemsList.length) { box.innerHTML = '<p class="pane-lead">No items.</p>'; return; }
  box.innerHTML = ITEM_GROUPS.map(g => {
    const rows = itemsList.filter(it => it.state === g.state);
    if (!rows.length) return "";
    const folded = !!itemsFolded[g.state];
    return '<section class="it-group" data-state="' + g.state + '"><button type="button" class="it-head" data-act="fold" ' +
      'data-state="' + g.state + '" aria-expanded="' + !folded + '"><span>' + g.label + '</span><span class="it-n">' +
      rows.length + "</span></button>" + (folded ? "" : rows.map(itemRow).join("")) + "</section>";
  }).join("");
  if (had) {
    const el = box.querySelector('.it-answer-box[data-id="' + CSS.escape(had.id) + '"]');
    if (el) { el.focus(); try { el.setSelectionRange(had.at, had.at); } catch (e) { /* not a text field */ } }
  }
}

async function loadItemDetail(id, quiet) {
  try {
    const d = await itemsFetch("/_hub/items/" + encodeURIComponent(id));
    if (itemsOpen !== id) return;
    itemsDetail = d;
    if (!quiet) itemsPaint();
    else if (document.querySelector('.it-detail[data-detail="' + CSS.escape(id) + '"]')) itemsPaint();
  } catch (e) { /* the row still shows what the list had */ }
}

function itemsNote(text) {
  const n = document.getElementById("items-note");
  if (n) n.textContent = text || "";
}

async function itemsAct(id, body) {
  itemsNote("");
  try {
    await itemsFetch("/_hub/items/" + encodeURIComponent(id), {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(Object.assign({ by: "board" }, body))
    });
    return true;
  } catch (e) {
    itemsNote("Could not " + body.do + " " + id + ": " + e.message);
    return false;
  }
}

async function itemsAnswer(id) {
  const box = document.querySelector('.it-answer-box[data-id="' + CSS.escape(id) + '"]');
  const answer = box ? box.value.trim() : "";
  if (!answer) { itemsNote("Type the answer first."); if (box) box.focus(); return; }
  if (await itemsAct(id, { do: "unblock", answer })) {
    delete itemsDraft[id];
    // The room moves the item to running only once the worker's turn starts, so say what happened rather than wait.
    itemsNote("Sent the answer for " + id + ". It shows as running once the worker starts its turn.");
  }
  loadItems();
}

async function itemsCancel(id) {
  const ok = await askUser({
    title: "cancel " + id + "?",
    body: "The item is finished as <b>cancelled</b> and its worker is asked to leave. This cannot be undone.",
    buttons: [{ label: "keep it", value: null }, { label: "cancel the item", value: true, style: "go" }]
  });
  if (!ok) return;
  await itemsAct(id, { do: "cancel" });
  loadItems();
}

async function itemsOpenCard(cardId) {
  if (!cardId) return;
  if (typeof attachTask !== "function") return;
  if (typeof switchView === "function") switchView("terms");
  await attachTask(cardId);
}

function itemsToggle(id) {
  itemsOpen = itemsOpen === id ? null : id;
  itemsDetail = null;
  itemsPaint();
  if (itemsOpen) loadItemDetail(itemsOpen, false);
}

// Called from switchView when the tab opens, and by the poll.
function startItems() {
  loadItems();
  clearInterval(itemsTimer);
  itemsTimer = setInterval(() => {
    if (itemsVisible()) loadItems();
    else { clearInterval(itemsTimer); itemsTimer = 0; }
  }, 5000);
}

function onItemEvent() {
  if (itemsVisible()) loadItems(); else loadItemsBadge();
}

document.addEventListener("DOMContentLoaded", () => {
  const box = document.getElementById("items-list");
  if (!box) return;
  box.addEventListener("click", ev => {
    const b = ev.target.closest("[data-act]");
    if (!b) return;
    const act = b.dataset.act;
    if (act === "fold") { itemsFolded[b.dataset.state] = !itemsFolded[b.dataset.state]; itemsPaint(); }
    else if (act === "answer") itemsAnswer(b.dataset.id);
    else if (act === "cancel") itemsCancel(b.dataset.id);
    else if (act === "card") itemsOpenCard(b.dataset.card);
    else if (act === "open") itemsToggle(b.dataset.id);
  });
  box.addEventListener("keydown", ev => {
    if (ev.target.classList.contains("it-answer-box") && ev.key === "Enter") { ev.preventDefault(); itemsAnswer(ev.target.dataset.id); }
    else if (ev.target.classList.contains("it-main") && (ev.key === "Enter" || ev.key === " ")) { ev.preventDefault(); itemsToggle(ev.target.dataset.id); }
  });
  box.addEventListener("input", ev => {
    if (ev.target.classList.contains("it-answer-box")) itemsDraft[ev.target.dataset.id] = ev.target.value;
  });
});
