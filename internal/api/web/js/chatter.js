// The chatter tab: every say from one session to another, from every room, newest first.
//
// One read of GET /v1/says when the tab opens or a filter changes, and one more for each press of `more`. Nothing polls
// and no event repaints it, so a say that lands while it is open shows on the next open. Through the hub the rows come
// merged across rooms, each tagged with its room and with `room~id` on both cards, and a `cursor` that carries where
// each room is. A plain room sends neither, and `more` falls back to the id of the last row read. A room that did not
// answer is named in `missing`. Each room keeps 30 days and 2000 says, so this is that much history and no more.
// See docs/rnd/say-log-view-notes.md.

const chatterState = { rows: [], cursor: "", more: false, missing: [], gen: 0, loaded: false };

function chatterFilters() {
  const agent = document.getElementById("chatter-agent")?.value.trim() || "";
  const dir = document.getElementById("chatter-dir")?.value || "";
  const room = document.getElementById("chatter-room")?.value || "";
  const back = Number(document.getElementById("chatter-since")?.value || 0);
  const p = new URLSearchParams();
  if (agent) p.set(dir || "agent", agent);
  if (room) p.set("room", room);
  if (back) p.set("since", new Date(Date.now() - back * 1000).toISOString());
  return p;
}

// The card a say's end names, when the board has it. `tag` is `room~id` through the hub and a bare id in a scoped view.
function chatterCard(tag) {
  if (!tag) return null;
  const all = typeof lastTasks !== "undefined" ? lastTasks : [];
  return all.find(t => t.id === tag) || all.find(t => bareId(t.id) === bareId(tag)) || null;
}

function chatterWho(wire, tag) {
  const card = chatterCard(tag);
  let el;
  if (card) {
    el = document.createElement("button");
    el.type = "button";
    el.className = "chat-who link";
    el.dataset.tip = "open its card";
    el.onclick = e => { e.stopPropagation(); card.supervised ? attachTask(card.id) : openTask(card.id); };
  } else {
    el = document.createElement("span");
    el.className = "chat-who";
  }
  el.textContent = wire || "unknown";
  return el;
}

function chatterRow(s) {
  const row = document.createElement("div");
  row.className = "chatter-row";
  const when = document.createElement("span");
  when.className = "chatter-when";
  const at = new Date(s.sent_at);
  when.textContent = isNaN(at) ? "" : at.toLocaleString();
  const room = document.createElement("span");
  room.className = "aud-room";
  room.textContent = s.room || "";
  if (!s.room) room.hidden = true;
  const state = document.createElement("span");
  state.className = "chatter-state";
  state.textContent = s.state + (s.state === "delivered" && s.channel ? " by " + s.channel : "");
  const arrow = document.createElement("span");
  arrow.className = "chatter-arrow";
  arrow.textContent = "to";
  const toggle = document.createElement("button");
  toggle.type = "button";
  toggle.className = "chat-toggle chatter-preview";
  toggle.textContent = s.preview || "(empty)";
  toggle.setAttribute("aria-expanded", "false");
  const body = document.createElement("div");
  body.className = "chat-text chatter-text";
  body.hidden = true;
  body.textContent = (s.text || s.preview || "") +
    (s.text && Array.from(s.text).length < s.chars ? `\n[cut: the say was ${s.chars} characters]` : "");
  toggle.onclick = body.onclick = () => {
    body.hidden = !body.hidden;
    toggle.classList.toggle("open", !body.hidden);
    toggle.setAttribute("aria-expanded", String(!body.hidden));
  };
  row.append(when, room, chatterWho(s.from, s.from_task), arrow, chatterWho(s.to || s.to_input, s.to_task), state, toggle, body);
  return row;
}

function chatterPaint() {
  const list = document.getElementById("chatter-list");
  if (!list) return;
  list.textContent = "";
  const note = (cls, text) => {
    const p = document.createElement("p");
    p.className = cls;
    p.textContent = text;
    list.appendChild(p);
  };
  if (chatterState.missing.length) note("pane-lead chatter-missing", "Not answering, so not shown: " + chatterState.missing.join(", "));
  if (!chatterState.rows.length) {
    if (chatterState.loaded) note("pane-lead", "Nothing said matches.");
    return;
  }
  for (const s of chatterState.rows) list.appendChild(chatterRow(s));
  if (chatterState.more) {
    const more = document.createElement("button");
    more.type = "button";
    more.className = "chat-more";
    more.textContent = "more";
    more.onclick = () => loadChatter(true);
    list.appendChild(more);
  }
}

// The agent names offered while typing, from the cards the board has and the names already read.
function chatterDrawChoices() {
  const dl = document.getElementById("chatter-agents");
  if (dl) {
    const names = new Set();
    for (const t of typeof lastTasks !== "undefined" ? lastTasks : []) if (t.wire_name) names.add(t.wire_name);
    for (const s of chatterState.rows) { if (s.from) names.add(s.from); if (s.to) names.add(s.to); }
    const key = [...names].sort().join("\n");
    if (dl.dataset.names !== key) {
      dl.dataset.names = key;
      dl.replaceChildren(...[...names].sort().map(n => Object.assign(document.createElement("option"), { value: n })));
    }
  }
  const sel = document.getElementById("chatter-room");
  if (sel && document.activeElement !== sel) {
    const rooms = (typeof hubRooms !== "undefined" ? hubRooms : []).map(r => r.name);
    const key = rooms.join("\n");
    if (sel.dataset.names !== key) {
      const keep = sel.value;
      sel.dataset.names = key;
      sel.replaceChildren(Object.assign(document.createElement("option"), { value: "", textContent: "all rooms" }),
        ...rooms.map(n => Object.assign(document.createElement("option"), { value: n, textContent: n })));
      sel.value = rooms.includes(keep) ? keep : "";
    }
    sel.hidden = !rooms.length;
    sel.previousElementSibling && (sel.previousElementSibling.hidden = !rooms.length);
  }
}

// loadChatter reads the first page, or with `more` the page after the last one read. A reply that arrives after a newer
// request was made is dropped.
async function loadChatter(more) {
  const list = document.getElementById("chatter-list");
  if (!list) return;
  const gen = ++chatterState.gen;  const p = chatterFilters();
  if (more) {
    const last = chatterState.rows[chatterState.rows.length - 1];
    const before = chatterState.cursor || (last && last.id) || "";
    if (before) p.set("before", before);
  }
  let page;
  try {
    page = await api("/v1/says" + (p.toString() ? "?" + p.toString() : ""));
  } catch (e) {
    if (gen !== chatterState.gen) return;
    chatterState.loaded = false;
    list.innerHTML = '<p class="pane-lead">The chatter is not answering right now. Open the tab again to retry.</p>';
    return;
  }
  if (gen !== chatterState.gen || !page) return;
  const rows = page.says || [];
  chatterState.rows = more ? chatterState.rows.concat(rows) : rows;
  chatterState.cursor = page.cursor || "";
  chatterState.more = !!page.more && rows.length > 0;
  chatterState.missing = page.missing || [];
  chatterState.loaded = true;
  chatterDrawChoices();
  chatterPaint();
}

document.addEventListener("DOMContentLoaded", () => {
  const refilter = () => {
    const list = document.getElementById("chatter-list");
    if (list) list.scrollTop = 0;
    loadChatter(false);
  };
  for (const id of ["chatter-dir", "chatter-room", "chatter-since"]) {
    document.getElementById(id)?.addEventListener("change", refilter);
  }
  // A typed name is read when it is chosen or left, not on every key.
  document.getElementById("chatter-agent")?.addEventListener("change", refilter);
  document.getElementById("chatter-agent")?.addEventListener("keydown", e => { if (e.key === "Enter") refilter(); });
  chatterDrawChoices();
});
