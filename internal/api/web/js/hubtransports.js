// ── the hub's transports, a row in the gear ─────────────────────────────
//
// The board, the room link and the CI hook each bind a ziti service on the hub. `GET /_hub/transports` answers
// {transports: [{name, label, on, state, last_error, service, identity, seeded, locked, switchable, no_service, detail}]}.
// A zrok share (no_service) has no service or identity, so its row is a state, a switch and its detail line: the
// masked share token and the zrok env. `PUT` takes
// {name, on, service, identity}: 403 from any machine but the hub's own, and then the row is read-only and says where
// to set it. 409 is a refusal, shown as the hub said it.
//
// Only on a hub. Any answer but ok leaves the list empty. Read when the gear opens, after a PUT and on a stream reopen.
// There is no timer.

function htMsg(text) { accessUI.msg = text; const m = htEl("ac-msg"); if (m) m.textContent = text; }

function htEl(id) { return document.getElementById(id); }

async function loadHubTransports() {
  if (typeof isGuest === "function" && isGuest()) { accessUI.transports = []; paintAccess(); return; }
  let r;
  try { r = await plainFetch("/_hub/transports"); } catch (e) { return; }
  if (!r.ok) { accessUI.transports = []; paintAccess(); return; }
  let v;
  try { v = await r.json(); } catch (e) { return; }
  accessUI.transports = Array.isArray(v.transports) ? v.transports : [];
  accessUI.editing = "";
  paintAccess();
}

async function putHubTransport(ask) {
  htMsg("saving");
  try {
    const r = await plainFetch("/_hub/transports", { method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(ask) });
    if (r.status === 403) {
      accessUI.readOnly = true;
      htMsg("");
      await loadHubTransports();
      return false;
    }
    if (!r.ok) {
      let why = "";
      try { why = (await r.json()).error; } catch (e) { /* not json */ }
      htMsg(why || ("the hub refused it: " + r.status));
      return false;
    }
    htMsg("saved");
    await loadHubTransports();
    return true;
  } catch (e) { htMsg("could not reach the hub"); return false; }
}
