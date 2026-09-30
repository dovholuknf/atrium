// ── a card has an address made of names ──────────────────────────────────
//
// `/alias/<alias>` opens a card's terminal on its own, the way `#term=<id>` does. `/room/<room>/<name>` is the
// qualified form that never clashes, and `/room/<room>` is the board scoped to that room. See
// docs/rnd/card-urls-design.md.
//
// THE PAGE DOES NOT RESOLVE A NAME ITSELF. It asks the hub's handle-addressed route, `GET /v1/tasks/<alias>` or
// `GET /v1/tasks/<name>@<room>`, once per open and never on a timer. What comes back is a card, a 404 with what
// would have worked, or a 409 naming every card the alias reaches.

// What this address is: `alias`, `card` (a room and a name), `room` (the board scoped), or null.
function cardUrlShape(path) {
  const parts = String(path == null ? location.pathname : path).replace(/\/$/, "").split("/");
  const dec = s => { try { return decodeURIComponent(s); } catch (e) { return s; } };
  if (parts[0] !== "") return null;
  if (parts[1] === "alias" && parts.length === 3 && parts[2]) {
    return { kind: "alias", alias: cardUrlName(dec(parts[2])) };
  }
  if (parts[1] === "room" && parts.length === 3 && parts[2]) return { kind: "room", room: dec(parts[2]) };
  if (parts[1] === "room" && parts.length === 4 && parts[2] && parts[3]) {
    // A wire name compares case-sensitively, so only the alias form is lowered.
    return { kind: "card", room: dec(parts[2]), name: dec(parts[3]).replace(/^@/, "") };
  }
  return null;
}

// The way the resolver compares a name: case does not matter and a leading `@` is dropped.
function cardUrlName(s) { return String(s || "").replace(/^@/, "").toLowerCase(); }

// Whether the address names one card, which is what makes a window a terminal.
function cardUrlIsCard() {
  const s = cardUrlShape();
  return !!s && s.kind !== "room";
}

// `/room/<room>` scopes the board the way picking the room in the header does. Written before anything asks for the
// room, so there is no reload. A card remembered from another room goes with it, as `pickRoom` does.
function cardUrlScope() {
  const s = cardUrlShape();
  if (!s || s.kind !== "room" || typeof ROOM_KEY === "undefined") return;
  try {
    if (localStorage.getItem(ROOM_KEY) === s.room) return;
    localStorage.setItem(ROOM_KEY, s.room);
    localStorage.removeItem("atrium.term");
  } catch (e) {}
}

// The path a card is linked by, or "" when it has neither an alias nor a handle and `#term=` is all there is.
//
// `/alias/` unless another live card holds that alias, or this card is done and a live one does. Then it is
// `/room/<room>/<name>`. `all` is the list to check the clash against, which a window showing one card does not
// have, so it defaults to what the board knows.
function cardUrlPath(t, all) {
  if (!t) return "";
  const alias = cardUrlName(t.alias);
  const wire = String(t.wire_name || "");
  const handle = wire.slice(wire.lastIndexOf("/") + 1);
  const room = t.room || roomOf(t.id) || "";
  const enc = encodeURIComponent;
  if (alias && !cardUrlClashes(t, alias, all)) return "/alias/" + enc(alias);
  // The handle first: a done card whose alias a live card took is reached by its handle, since the resolver lets a live alias win.
  const name = handle || alias;
  if (room && name) return "/room/" + enc(room) + "/" + enc(name);
  return alias ? "/alias/" + enc(alias) : "";
}

function cardUrlClashes(t, alias, all) {
  const list = all || (typeof cardList === "function" ? cardList() : []);
  return list.some(o => o && !sameCard(o.id, t.id) && cardUrlName(o.alias) === alias &&
    o.status !== "dead" && o.status !== "done");
}

// The address a card is opened from: its readable path, else the fragment.
function cardUrlFor(t, all) {
  return cardUrlPath(t, all) || "/#term=" + encodeURIComponent(t.id);
}

// ── what this browser opened last, per path ──────────────────────────────

function cardUrlKey(shape) {
  return "atrium.cardurl." + (shape.kind === "alias" ? "alias:" + shape.alias : shape.room + "/" + shape.name);
}

function cardUrlLast(shape) {
  try { return localStorage.getItem(cardUrlKey(shape)) || ""; } catch (e) { return ""; }
}

function cardUrlRemember(shape, id) {
  try { localStorage.setItem(cardUrlKey(shape), bareId(id)); } catch (e) {}
}

// Writes what a window on a card's readable path opened, so a bookmark of that path can say when it changes.
function cardUrlNote(path, t) {
  const s = path && cardUrlShape(path);
  if (s && s.kind !== "room" && t) cardUrlRemember(s, t.id);
}

// ── the lookup ───────────────────────────────────────────────────────────

function cardUrlLookupPath(shape) {
  const enc = encodeURIComponent;
  return "/v1/tasks/" + (shape.kind === "alias" ? enc(shape.alias) : enc(shape.name) + "@" + enc(shape.room));
}

// One line off a 409's candidate list, `alias@room (room~id)`.
function cardUrlCandidate(s) {
  const m = /^(.*)@(\S+) \((.+)\)$/.exec(String(s));
  return m ? { name: m[1], room: m[2], id: m[3] } : null;
}

// Opens the card an address names. Resolves `{ id, task, notes }` when there is a card to open, and null after
// drawing the reason there is not.
async function cardUrlOpen(shape) {
  let task = null;
  try {
    task = await soloFetchCard("", cardUrlLookupPath(shape));
  } catch (e) {
    if (e.status === 409) return cardUrlClash(shape, e.body || {});
    if (e.status === 404) cardUrlMiss(shape, e.body || {}, e.message);
    else cardUrlSay("could not look that up", [{ text: e.message }]);
    return null;
  }
  const notes = [];
  const last = cardUrlLast(shape);
  if (last && !sameCard(last, task.id)) notes.push(await cardUrlChanged(shape, last, task));
  if (task.status === "done") {
    const recap = String(task.recap || "").trim().split("\n").filter(Boolean).pop();
    notes.push({ text: "this card is done" + (recap ? ". " + recap : "") });
  }
  cardUrlRemember(shape, task.id);
  return { id: task.id, task, notes };
}

// The address opens a different card than it did before. Says when this one started and what became of the old one,
// with a link to it by `#term=` while it still exists.
async function cardUrlChanged(shape, last, task) {
  const name = shape.kind === "alias" ? shape.alias : shape.name;
  let was = "is gone";
  let link = "";
  try {
    const old = await api("/v1/tasks/" + encodeURIComponent(last));
    was = old && old.status === "done" ? "is done" : "is still running";
    link = "/#term=" + encodeURIComponent(old && old.id ? old.id : last);
  } catch (e) {}
  const since = task.created_at ? ", launched " + new Date(task.created_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) : "";
  return { text: "@" + name + " is a different card now" + since + ". the one this address opened before " + was,
    link: link ? { href: link, label: "open the old one" } : null };
}

// No card by that name. Says what would have worked, each as a readable link.
function cardUrlMiss(shape, body, message) {
  const name = shape.kind === "alias" ? shape.alias : shape.name;
  const rooms = typeof hubRooms !== "undefined" ? hubRooms.map(r => r.name) : [];
  if (shape.kind === "card" && body.would_work === undefined && /no room called/.test(message || "")) {
    cardUrlSay("room " + shape.room + " is not attached to this hub",
      rooms.map(r => ({ text: r, href: "/room/" + encodeURIComponent(r) })));
    return;
  }
  const one = rooms.length === 1 ? rooms[0] : "";
  const rows = (body.would_work || []).map(w => {
    const m = /^(\S+?)(?:@(\S+))?(?: \(@(\S+)\))?$/.exec(w);
    if (!m) return { text: w };
    const room = m[2] || one;
    const enc = encodeURIComponent;
    let href = "";
    if (m[3]) href = cardUrlPath({ alias: m[3], room }) || "/alias/" + enc(m[3]);
    else if (room) href = "/room/" + enc(room) + "/" + enc(m[1]);
    return { text: w, href };
  });
  cardUrlSay("no card called " + name + (shape.kind === "card" ? " on room " + shape.room : ""), rows);
}

// An alias two rooms hold. The card this browser opened last time wins when it is one of them, with a line naming
// the other. Otherwise the chooser, and picking one remembers it for this path.
async function cardUrlClash(shape, body) {
  const cands = (body.candidates || []).map(cardUrlCandidate).filter(Boolean);
  const link = c => "/room/" + encodeURIComponent(c.room) + "/" + encodeURIComponent(c.name);
  const last = cardUrlLast(shape);
  const mine = last && cands.find(c => sameCard(c.id, last));
  if (mine) {
    let task = null;
    try { task = await soloFetchCard(mine.id); } catch (e) { task = null; }
    if (task) {
      const others = cands.filter(c => c !== mine);
      return { id: task.id, task, notes: [{
        text: "@" + shape.alias + " is also on " + others.map(c => c.room).join(", "),
        link: others.length ? { href: link(others[0]), label: "open that one" } : null }] };
    }
  }
  const rows = [];
  for (const c of cands) {
    let t = null;
    try { t = await api("/v1/tasks/" + encodeURIComponent(c.id)); } catch (e) {}
    rows.push({ text: c.room + "  " + c.name + (t ? "  " + cardUrlDoing(t) : ""), href: link(c), id: c.id,
      at: t && t.created_at || "", done: !!t && t.status === "done" });
  }
  rows.sort((a, b) => (a.done - b.done) || String(b.at).localeCompare(String(a.at)));
  cardUrlSay("@" + shape.alias + " is on more than one room", rows, shape);
  return null;
}

function cardUrlDoing(t) {
  if (t.status === "done") return "done";
  const a = t.activity && t.activity.what;
  return a ? a : String(t.status || "");
}

// ── the page's words ─────────────────────────────────────────────────────

function cardUrlBox(id) {
  let el = document.getElementById(id);
  if (!el) {
    el = document.createElement("div");
    el.id = id;
    document.body.appendChild(el);
  }
  el.textContent = "";
  el.hidden = false;
  return el;
}

function cardUrlLine(parent, row, onPick) {
  const p = document.createElement(row.href ? "a" : "span");
  p.className = "cu-row";
  p.textContent = row.text;
  if (row.href) {
    p.href = row.href;
    if (onPick) p.addEventListener("click", () => onPick(row));
  }
  parent.appendChild(p);
}

// No terminal, a sentence, and the links that would work. `shape` is given for the chooser, which remembers the pick.
function cardUrlSay(title, rows, shape) {
  document.title = "atrium: " + title;
  termWait("");
  const box = cardUrlBox("cardurl");
  const h = document.createElement("strong");
  h.textContent = title;
  box.appendChild(h);
  const list = document.createElement("div");
  list.className = "cu-list";
  for (const r of rows) cardUrlLine(list, r, shape ? row => cardUrlRemember(shape, row.id) : null);
  box.appendChild(list);
}

// One line over the pane, which can be dismissed.
function cardUrlNotice(notes) {
  if (!notes || !notes.length) return;
  const box = cardUrlBox("cardurl-note");
  for (const n of notes) {
    const line = document.createElement("div");
    line.className = "cu-note";
    line.textContent = n.text;
    if (n.link) {
      const a = document.createElement("a");
      a.href = n.link.href;
      a.textContent = " " + n.link.label;
      line.appendChild(a);
    }
    box.appendChild(line);
  }
  const x = document.createElement("button");
  x.className = "cu-x";
  x.textContent = "×";
  x.setAttribute("aria-label", "dismiss");
  x.onclick = () => { box.hidden = true; };
  box.appendChild(x);
}
