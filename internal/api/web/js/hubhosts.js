// ── the hub's host names, a row in the gear ──────────────────────────────────
//
// The names the hub answers besides its own, so a board shared over zrok works without setting $ATRIUM_HOSTS and
// restarting the hub by hand. `GET /_hub/hosts` answers {hosts, ignored: [{name, why}], env}. `PUT` takes
// {hosts} and answers 403 from any machine but the hub's own, and then the row is read-only and says where to
// set it, the way the notify row does (js/hubnotify.js).
//
// Only on a hub. Any answer but ok hides the row. Read when the gear opens, after a PUT and on a stream reopen.
// There is no timer.

let hhReadOnly = false;
let hhHosts = [];

function hhEl(id) { return document.getElementById(id); }

function hhLine(parent, text, cls) {
  const d = document.createElement("div");
  if (cls) d.className = cls;
  d.textContent = text;
  parent.appendChild(d);
  return d;
}

function hhPaint(v) {
  hhHosts = Array.isArray(v.hosts) ? v.hosts.slice() : [];
  const ignored = Array.isArray(v.ignored) ? v.ignored : [];
  const env = Array.isArray(v.env) ? v.env : [];
  const list = hhEl("s-hh-list");
  list.textContent = "";
  if (!hhHosts.length) hhLine(list, "none set", "hintline");
  for (const h of hhHosts) {
    const row = document.createElement("div");
    row.className = "hh-item";
    const name = document.createElement("code");
    name.textContent = h;
    row.appendChild(name);
    const why = ignored.find(i => i && i.name === h);
    if (why) hhLine(row, "ignored: " + why.why, "hh-why");
    if (!hhReadOnly) {
      const b = document.createElement("button");
      b.type = "button";
      b.textContent = "remove";
      b.dataset.remove = h;
      b.onclick = () => removeHubHost(h);
      row.appendChild(b);
    }
    list.appendChild(row);
  }
  // An entry the hub ignored but does not list among the hosts is still shown, with why.
  const extra = ignored.filter(i => i && !hhHosts.includes(i.name));
  const ig = hhEl("s-hh-ignored");
  ig.textContent = "";
  ig.hidden = !extra.length;
  for (const i of extra) hhLine(ig, "ignored " + i.name + ": " + i.why, "hh-why");
  const en = hhEl("s-hh-env");
  en.textContent = "";
  en.hidden = !env.length;
  if (env.length) {
    hhLine(en, "from $ATRIUM_HOSTS, read-only:", "hintline");
    for (const e of env) hhLine(en, e, "hh-env");
  }
  hhEl("s-hh-add").hidden = hhReadOnly;
  hhEl("s-hh-ro").hidden = !hhReadOnly;
}

// Read on open, after a save and on a stream reopen.
async function loadHubHosts() {
  const row = hhEl("s-hh-row");
  if (!row) return;
  if (typeof isGuest === "function" && isGuest()) { row.hidden = true; return; }
  let r;
  try { r = await plainFetch("/_hub/hosts"); } catch (e) { return; }
  if (!r.ok) { row.hidden = true; return; }
  let v;
  try { v = await r.json(); } catch (e) { return; }
  row.hidden = false;
  hhPaint(v);
}

async function putHubHosts(hosts) {
  const msg = hhEl("s-hh-msg");
  msg.textContent = "saving";
  try {
    const r = await plainFetch("/_hub/hosts", { method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ hosts }) });
    if (r.status === 403) {
      hhReadOnly = true;
      msg.textContent = "";
      await loadHubHosts();
      return false;
    }
    if (!r.ok) { msg.textContent = "the hub refused it: " + ((await r.text()) || r.status); return false; }
    msg.textContent = "saved";
    await loadHubHosts();
    return true;
  } catch (e) { msg.textContent = "could not reach the hub"; return false; }
}

async function addHubHost() {
  const box = hhEl("s-hh-name");
  const name = box.value.trim();
  if (!name || hhHosts.includes(name)) return;
  if (await putHubHosts(hhHosts.concat([name]))) box.value = "";
}

function removeHubHost(name) { return putHubHosts(hhHosts.filter(h => h !== name)); }
