// ── the hub's transports, a row in the gear ─────────────────────────────
//
// The board, the room link and the CI hook each bind a ziti service on the hub. `GET /_hub/transports` answers
// {transports: [{name, label, on, state, last_error, service, identity, seeded, locked, switchable, no_service}]}.
// A zrok share (no_service) has no service or identity, so its row is a state and a switch. `PUT` takes
// {name, on, service, identity}: 403 from any machine but the hub's own, and then the row is read-only and says where
// to set it. 409 is a refusal, shown as the hub said it.
//
// Only on a hub. Any answer but ok hides the row. Read when the gear opens, after a PUT and on a stream reopen.
// There is no timer.

let htReadOnly = false;

function htEl(id) { return document.getElementById(id); }

function htText(parent, tag, text, cls) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  n.textContent = text;
  parent.appendChild(n);
  return n;
}

function htStateText(t) {
  if (t.state === "failing" && t.last_error) return "failing: " + t.last_error;
  return t.state;
}

function htPaint(list) {
  const box = htEl("s-ht-list");
  box.textContent = "";
  for (const t of list) {
    const row = document.createElement("div");
    row.className = "ht-item";
    row.dataset.name = t.name;
    htText(row, "strong", t.label || t.name);
    htText(row, "span", htStateText(t), "ht-state ht-" + t.state);
    const bare = !!t.no_service;
    const svc = document.createElement("input");
    svc.type = "text";
    svc.className = "ht-service";
    svc.value = t.service || "";
    svc.placeholder = t.default_service || "service";
    svc.spellcheck = false;
    svc.disabled = htReadOnly || !t.switchable;
    if (!bare) row.appendChild(svc);
    const id = document.createElement("input");
    id.type = "text";
    id.className = "ht-identity";
    id.value = t.identity || "";
    id.placeholder = t.identity_used || "identity file";
    id.spellcheck = false;
    id.disabled = htReadOnly || !t.switchable;
    if (!bare) row.appendChild(id);
    if (!htReadOnly && t.switchable) {
      if (!bare) {
        const apply = document.createElement("button");
        apply.type = "button";
        apply.className = "ht-apply";
        apply.textContent = "apply";
        apply.onclick = () => putHubTransport({ name: t.name, service: svc.value, identity: id.value });
        row.appendChild(apply);
      }
      const sw = document.createElement("button");
      sw.type = "button";
      sw.className = "ht-switch";
      sw.textContent = t.on ? "turn off" : "turn on";
      sw.onclick = () => putHubTransport(bare ? { name: t.name, on: !t.on }
        : { name: t.name, on: !t.on, service: svc.value, identity: id.value });
      row.appendChild(sw);
    }
    if (t.locked) htText(row, "div", t.locked, "hintline");
    if (Array.isArray(t.seeded) && t.seeded.length) {
      htText(row, "div", "set by flag: " + t.seeded.join(", ") + ". the first change here replaces it.", "hintline");
    }
    box.appendChild(row);
  }
  htEl("s-ht-ro").hidden = !htReadOnly;
}

async function loadHubTransports() {
  const row = htEl("s-ht-row");
  if (!row) return;
  if (typeof isGuest === "function" && isGuest()) { row.hidden = true; return; }
  let r;
  try { r = await plainFetch("/_hub/transports"); } catch (e) { return; }
  if (!r.ok) { row.hidden = true; return; }
  let v;
  try { v = await r.json(); } catch (e) { return; }
  row.hidden = false;
  htPaint(Array.isArray(v.transports) ? v.transports : []);
}

async function putHubTransport(ask) {
  const msg = htEl("s-ht-msg");
  msg.textContent = "saving";
  try {
    const r = await plainFetch("/_hub/transports", { method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(ask) });
    if (r.status === 403) {
      htReadOnly = true;
      msg.textContent = "";
      await loadHubTransports();
      return false;
    }
    if (!r.ok) {
      let why = "";
      try { why = (await r.json()).error; } catch (e) { /* not json */ }
      msg.textContent = why || ("the hub refused it: " + r.status);
      return false;
    }
    msg.textContent = "saved";
    await loadHubTransports();
    return true;
  } catch (e) { msg.textContent = "could not reach the hub"; return false; }
}
