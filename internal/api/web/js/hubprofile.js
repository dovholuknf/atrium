// ── who the hub commits as, a row in the gear ───────────────────────────────
//
// `GET /_hub/profile` answers the operator's profile, the hub machine's git config --global user.name and user.email
// until one is written. `POST` takes the profile whole and answers 403 from any machine but the hub's own, and then
// the row is read-only. Only on a hub: a 404 hides the row. Read when the gear opens and on a stream reopen.

let hcProfile = {};

function hcEl(id) { return document.getElementById(id); }

async function loadHubProfile() {
  const row = hcEl("s-hc-row");
  if (!row) return;
  if (typeof isGuest === "function" && isGuest()) { row.hidden = true; return; }
  let r;
  try { r = await plainFetch("/_hub/profile"); } catch (e) { return; }
  if (r.status === 404) { row.hidden = true; return; }
  row.hidden = false;
  hcProfile = {};
  if (r.ok) { try { hcProfile = (await r.json()) || {}; } catch (e) { hcProfile = {}; } }
  hcEl("s-hc-msg").textContent = r.ok ? "" : "set your name and email, or git config --global user.name and user.email on the hub's machine";
  hcEl("s-hc-name").value = hcProfile.name || "";
  hcEl("s-hc-email").value = hcProfile.email || "";
}

async function saveHubProfile() {
  const msg = hcEl("s-hc-msg");
  const body = Object.assign({}, hcProfile, { name: hcEl("s-hc-name").value.trim(), email: hcEl("s-hc-email").value.trim() });
  if (!body.name || !body.email) { msg.textContent = "a name and an email, both"; return; }
  msg.textContent = "saving";
  try {
    const r = await plainFetch("/_hub/profile", { method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body) });
    if (r.status === 403) {
      for (const id of ["s-hc-name", "s-hc-email", "s-hc-save"]) hcEl(id).disabled = true;
      msg.textContent = "Set on the hub's machine.";
      return;
    }
    if (!r.ok) { msg.textContent = "the hub refused it: " + ((await r.text()) || r.status); return; }
    hcProfile = body;
    msg.textContent = "saved";
  } catch (e) { msg.textContent = "could not reach the hub"; }
}
