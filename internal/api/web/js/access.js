// Accessing Atrium: the one settings screen for every way in. It draws into #access from an `accessApi` object, so
// the board binds it to the hub and a mock can bind it to nothing. One sign-in block, then a list of entries (any
// number of each kind), then the rooms waiting for approval.

const ACCESS_KINDS = {
  ziti: { mark: "ziti", label: "OpenZiti" },
  zrok: { mark: "zrok", label: "zrok" },
  address: { mark: "addr", label: "my own address" }
};

const accessUI = { state: { auth: {}, entries: [], waiting: [] }, transports: [], readOnly: false, editing: "", adding: "", form: {}, msg: "" };

const accessApi = {
  async call(method, path, body) {
    const r = await fetch("/v1/access" + path, {
      method, headers: { "Content-Type": "application/json" }, body: body ? JSON.stringify(body) : undefined
    });
    if (!r.ok) throw new Error((await r.text()) || r.statusText);
    return r.status === 204 ? null : r.json();
  },
  load() { return this.call("GET", ""); },
  saveAuth(a) { return this.call("PUT", "/auth", a); },
  add(kind, form) { return this.call("POST", "/entries", { kind, ...form }); },
  revoke(id) { return this.call("DELETE", "/entries/" + encodeURIComponent(id)); },
  openUp(id, account) { return this.call("POST", "/entries/" + encodeURIComponent(id) + "/open", { account }); },
  approve(room) { return this.call("POST", "/rooms/" + encodeURIComponent(room) + "/approve"); }
};

const acEsc = s => String(s == null ? "" : s).replace(/[&<>"']/g, c =>
  ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

async function accessRefresh(msg) {
  try { accessUI.state = await accessApi.load(); accessUI.msg = msg || ""; }
  catch (e) { accessUI.msg = "could not read the list: " + e.message; }
  paintAccess();
}

async function accessDo(fn, done) {
  try { await fn(); accessUI.adding = ""; accessUI.form = {}; await accessRefresh(done); }
  catch (e) { accessUI.msg = e.message; paintAccess(); }
}

function accessField(name) {
  const el = document.getElementById("ac-f-" + name);
  return el ? el.value : "";
}

function accessStartAdd(kind, mode) {
  accessUI.adding = kind; accessUI.form = mode ? { mode } : {}; accessUI.menu = false; accessUI.fresh = true; paintAccess();
}
function accessToggleMenu() { accessUI.menu = !accessUI.menu; paintAccess(); }

// The marks are the real logos: the OpenZiti disc and the zrok rocket come from rooms.js, a lock or a globe marks
// zrok private or public, and a plain link glyph stands for an address of one's own.
const AC_SVG = {
  lock: `<svg viewBox="0 0 12 12" aria-hidden="true"><rect x="2" y="5.2" width="8" height="5.6" rx="1.3" fill="currentColor"/>` +
    `<path d="M3.8 5.2V3.8a2.2 2.2 0 0 1 4.4 0v1.4" fill="none" stroke="currentColor" stroke-width="1.3"/></svg>`,
  globe: `<svg viewBox="0 0 12 12" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.1"><circle cx="6" cy="6" r="4.8"/>` +
    `<ellipse cx="6" cy="6" rx="2.1" ry="4.8"/><path d="M1.2 6h9.6"/></svg>`,
  addr: `<svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" ` +
    `stroke-linejoin="round"><path d="M10 14a4.5 4.5 0 0 0 6.4 0l3-3a4.5 4.5 0 0 0-6.4-6.4l-1.1 1.1"/>` +
    `<path d="M14 10a4.5 4.5 0 0 0-6.4 0l-3 3a4.5 4.5 0 0 0 6.4 6.4l1.1-1.1"/></svg>`
};

function accessIcon(kind, mode) {
  const logo = kind === "ziti" ? zitiLogo() : kind === "zrok" ? zrokLogo() : AC_SVG.addr;
  const badge = kind === "zrok" && mode ? `<i class="ac-badge">${mode === "public" ? AC_SVG.globe : AC_SVG.lock}</i>` : "";
  return `<span class="ac-ico" data-kind="${kind}">${logo}${badge}</span>`;
}

function accessMenu() {
  const items = [["ziti", "", "OpenZiti", "Join a network, upload a .jwt or import a cert"],
    ["zrok", "private", "zrok private", "Closed, only accounts you grant"],
    ["zrok", "public", "zrok public", "A reserved URL behind your sign-in"],
    ["address", "", "My own address", "Your interface, hostname or proxy"]];
  const off = accessUI.readOnly ? [] : accessUI.transports.filter(t => t.switchable && !t.on);
  return `<div class="ac-menu"><button class="go" aria-haspopup="menu" aria-expanded="${!!accessUI.menu}"
      onclick="accessToggleMenu()">Access Atrium <b>+</b></button><span class="hintline" id="ac-msg">${acEsc(accessUI.msg)}</span>${accessUI.menu ? `<div class="ac-drop" role="menu">${
    items.map(([k, m, t, s]) => `<button role="menuitem" data-tip="${acEsc(s)}" onclick="accessStartAdd('${k}','${m}')">` +
      `${accessIcon(k, m)}<span class="ac-opt">${t}<small>${acEsc(s)}</small></span></button>`).join("")
  }${off.map(t => `<button role="menuitem" data-tip="Bind it on the hub now" onclick="accessTurnOn('${acEsc(t.name)}')">` +
      `${accessIcon(t.name.includes("zrok") ? "zrok" : "ziti", "")}<span class="ac-opt">${acEsc(t.label || t.name)}<small>Off now, bind it on the hub</small></span></button>`).join("")
  }</div>` : ""}</div>`;
}
function accessTurnOn(name) { accessUI.menu = false; accessUI.adding = ""; accessUI.form = {}; paintAccess(); putHubTransport({ name, on: true }); }
function accessPick(name, v) { accessUI.form[name] = v; paintAccess(); }

function accessExposes(kind, f) {
  return (kind === "zrok" && f.mode === "public") || (kind === "address" && f.mode === "oidc");
}

function accessNoAuth(on) { accessUI.form.noauth = on; accessUI.form.noauthSure = false; paintAccess(); }
function accessNoAuthSure(yes) {
  accessUI.form.noauth = yes; accessUI.form.noauthSure = yes; paintAccess();
}

function accessSubmitAdd() {
  const f = { ...accessUI.form, name: accessField("name") };
  for (const n of ["controller", "token", "address", "serves", "account", "cert", "key"]) {
    const v = accessField(n); if (v) f[n] = v;
  }
  if (accessUI.adding === "ziti" && f.mode === "token") f.ack = !!(document.getElementById("ac-f-ack") || {}).checked;
  if (accessGate(accessUI.adding, f)) return;
  const provider = accessField("provider").trim();
  accessDo(async () => {
    if (provider) {
      await accessApi.saveAuth({ provider, client: accessField("client"),
        allow: accessField("allow").split(/[\s,]+/).filter(Boolean) });
    }
    await accessApi.add(accessUI.adding, f);
  }, "added " + (f.name || accessUI.adding));
}

function accessRevoke(id) { accessDo(() => accessApi.revoke(id), "revoked"); }
function accessOpenUp(id) {
  const a = prompt("zrok account to let in");
  if (a) accessDo(() => accessApi.openUp(id, a.trim()), "opened to " + a.trim());
}
function accessApprove(room) { accessDo(() => accessApi.approve(room), room + " approved"); }

function accessSignIn(a, f, canSkip) {
  const tip = "Atrium holds no password. Anyone not on the allow list is told so and sees nothing of the board.";
  const warn = canSkip && f.noauth && !f.noauthSure ? `<div class="ac-warn"><b>Are you sure?</b> Anyone who learns this URL
      can open the board and drive every card on it.
      <div class="ac-warnbtns"><button onclick="accessNoAuthSure(true)">Yes, no sign-in</button>
      <button class="go" onclick="accessNoAuth(false)">Keep sign-in</button></div></div>` : "";
  const skip = canSkip ? `<label class="ac-check"><input type="checkbox" id="ac-noauth" ${f.noauth ? "checked" : ""}
      onchange="accessNoAuth(this.checked)"> This is a private URL, I don't need sign-in</label>${warn}` : "";
  return `<div class="ac-signin"><h5 class="ac-title">Sign-in
      <span class="help" tabindex="0" data-actip="${acEsc(tip)}">?</span></h5>
    <div class="ac-grid">
      <label for="ac-f-provider">provider</label>
      <input type="text" id="ac-f-provider" value="${acEsc(a.provider)}" oninput="accessRegate()"
        placeholder="https://accounts.example.com">
      <label for="ac-f-client">client id</label>
      <input type="text" id="ac-f-client" value="${acEsc(a.client)}">
      <label for="ac-f-allow">allow list</label>
      <textarea id="ac-f-allow" rows="2" placeholder="one email per line">${acEsc((a.allow || []).join("\n"))}</textarea>
    </div>${skip}</div>`;
}

// The reason "add" is disabled, or "" when it may go. An exposing flow needs a sign-in provider, or for an own
// address the private-URL box confirmed.
function accessGate(kind, f) {
  if (!accessExposes(kind, f)) return "";
  if (accessField("provider").trim() || f.noauthSure) return "";
  return kind === "zrok" ? "Fill in the sign-in provider to add." : "Fill in the sign-in provider, or confirm it is a private URL.";
}
function accessRegate() {
  const why = accessGate(accessUI.adding, accessUI.form);
  document.getElementById("ac-why").textContent = why;
  document.getElementById("ac-add").disabled = !!why;
}

function accessEntry(e) {
  const k = ACCESS_KINDS[e.kind] || { mark: e.kind, label: e.kind };
  const live = e.state === "live";
  const extra = e.kind === "zrok" && e.mode === "private"
    ? `<button onclick="accessOpenUp('${acEsc(e.id)}')">open it up</button>` : "";
  const desc = [k.label + (e.mode ? " " + e.mode : ""), "serves " + (e.serves || "both"), e.identity, e.url]
    .filter(Boolean).map(acEsc).join(" · ");
  return `<div class="ac-row ${live ? "live" : ""}">
    ${accessIcon(e.kind, e.mode)}
    <span class="grow"><span class="ac-name">${acEsc(e.name)}</span>
      <span class="ac-sub ac-desc" data-tip="${desc}">${desc}</span>
      ${e.prompt ? `<span class="ac-sub">Open <a href="${acEsc(e.prompt.url)}" target="_blank" rel="noopener">${acEsc(e.prompt.url)}</a>
        and enter <b>${acEsc(e.prompt.code)}</b> to finish signing in.</span>` : ""}</span>
    <span class="ac-chip ${live ? "on" : ""}" data-tip="${acEsc(e.state || "off")}">${acEsc(e.state || "off")}</span>
    <span class="ac-actions">${extra}<button onclick="accessRevoke('${acEsc(e.id)}')">revoke</button></span></div>`;
}

// A hub transport (the board, the room link or the CI hook over ziti, or a zrok share) in the same list as the
// entries. One that is off is not configured, so it waits in the Access Atrium menu instead.
function accessTransport(t) {
  const live = t.state === "bound";
  const kind = t.name.includes("zrok") ? "zrok" : "ziti";
  const bare = !!t.no_service;
  const desc = [t.service && "service " + t.service, t.identity, t.detail].filter(Boolean).map(acEsc).join(" · ");
  const can = !accessUI.readOnly && t.switchable;
  const open = accessUI.editing === t.name;
  const state = t.state === "failing" && t.last_error ? "failing: " + t.last_error : t.state;
  const edit = open ? `<div class="ac-grid ac-edit">
      <label for="ac-f-svc">service</label><input id="ac-f-svc" value="${acEsc(t.service)}" placeholder="${acEsc(t.default_service || "service")}" spellcheck="false">
      <label for="ac-f-idn">identity</label><input id="ac-f-idn" value="${acEsc(t.identity)}" placeholder="${acEsc(t.identity_used || "identity file")}" spellcheck="false">
      <span></span><span class="ac-foot"><button onclick="accessEditTransport('')">cancel</button>
      <button class="go" onclick="accessApplyTransport('${acEsc(t.name)}')">apply</button></span></div>` : "";
  return `<div class="ac-row ${live ? "live" : ""}" data-transport="${acEsc(t.name)}">
    ${accessIcon(kind, "")}
    <span class="grow"><span class="ac-name">${acEsc(t.label || t.name)}</span>
      <span class="ac-sub ac-desc" data-tip="${desc}">${desc || "hub transport"}</span>
      ${t.locked ? `<span class="ac-sub">${acEsc(t.locked)}</span>` : ""}
      ${!t.locked && (t.seeded || []).length ? `<span class="ac-sub">set by flag: ${acEsc(t.seeded.join(", "))}. the first change here replaces it.</span>` : ""}</span>
    <span class="ac-chip ${live ? "on" : t.state === "failing" ? "warn" : ""}" data-tip="${acEsc(state)}">${acEsc(state)}</span>
    <span class="ac-actions">${can && !bare ? `<button onclick="accessEditTransport('${acEsc(t.name)}')">edit</button>` : ""}
      ${can ? `<button onclick="putHubTransport({name:'${acEsc(t.name)}',on:false})">turn off</button>` : ""}</span>${edit}</div>`;
}
function accessEditTransport(name) { accessUI.editing = name; paintAccess(); }
function accessApplyTransport(name) {
  putHubTransport({ name, on: true, service: accessField("svc"), identity: accessField("idn") });
}

// A labelled row of the form grid. The hint rides in a hover ? beside the label, never as grey text under a control.
function accessRow(id, label, control, tip) {
  return `<label for="ac-f-${id}">${label}${tip ? ` <span class="help" tabindex="0" data-actip="${acEsc(tip)}">?</span>` : ""}</label>${control}`;
}
function accessSelect(id, name, cur, opts) {
  return `<select id="ac-f-${id}" onchange="accessPick('${name}', this.value)">${opts.map(([v, t]) =>
    `<option value="${v}" ${cur === v ? "selected" : ""}>${t}</option>`).join("")}</select>`;
}
function accessFile(id, label, accept, tip) {
  const got = accessUI.form["file_" + id];
  return `<label for="ac-file-${id}">${label} <span class="help" tabindex="0" data-actip="${acEsc(tip)}">?</span></label>
    <span class="ac-file"><input type="file" id="ac-file-${id}" accept="${accept}" onchange="accessRead('${id}', this)">
    <span class="ac-sub">${got ? acEsc(got) : "no file chosen"}</span></span>`;
}
function accessRead(id, input) {
  const file = input.files[0];
  if (!file) return;
  file.text().then(txt => { accessUI.form[id] = txt; accessUI.form["file_" + id] = file.name; paintAccess(); });
}

function accessForm() {
  const kind = accessUI.adding, f = accessUI.form;
  if (!kind) return "";
  let rows = accessRow("name", "name", `<input id="ac-f-name" placeholder="phone, office, lab">`) +
    accessRow("serves", "serves", accessSelect("serves", "serves", f.serves || "both",
      [["both", "the board and the room link"], ["board", "the board"], ["link", "the room link"]]));
  let after = "";
  if (kind === "ziti") {
    const how = f.how || "url", mode = f.mode || "cert";
    rows += accessRow("how", "join", accessSelect("how", "how", how,
      [["url", "by URL, sign in at the network's IdP"], ["jwt", "with a .jwt file"], ["import", "with a cert and key"]]),
      "A URL signs in at the network's IdP. A .jwt is enrolled by atrium. A cert and key import is like Desktop Edge for Windows.");
    if (how === "url") {
      rows += accessRow("mode", "enroll", accessSelect("mode", "mode", mode,
        [["cert", "to a cert (recommended)"], ["token", "to a token"]]),
        "A cert identity starts on its own. A token identity needs an IdP sign-in on every start.");
      if (mode === "token") rows += `<span></span><div class="ac-warn"><b>Read this.</b> A token identity cannot start on its
          own. Every time this hub or a room starts it asks somebody to sign in at the IdP first, and nothing comes up
          until they do. <label><input type="checkbox" id="ac-f-ack"> I understand</label></div>`;
      rows += accessRow("controller", "controller", `<input id="ac-f-controller" placeholder="https://ctrl.example.com:1280">`);
    } else if (how === "jwt") {
      rows += accessFile("token", ".jwt file", ".jwt,.txt", "The enrollment token from your controller. Atrium enrolls it.");
    } else {
      rows += accessFile("cert", "certificate", ".pem,.crt,.cer", "The identity certificate, as a PEM file.") +
        accessFile("key", "key", ".pem,.key", "The private key that goes with the certificate.");
    }
  } else if (kind === "zrok") {
    const pub = f.mode === "public";
    rows += accessRow("token", "zrok token", `<input id="ac-f-token" placeholder="only if this machine has no zrok environment yet">`,
      pub ? "A reserved atrium- URL on the public edge. It always sits behind a sign-in."
        : "Created closed and granted to this hub's account. \"open it up\" lets others in.");
    if (!pub) rows += accessRow("reach", "reach", accessSelect("reach", "reach", f.reach || "account",
      [["account", "just my account"], ["anyone", "any zrok user"]]),
      "Who may open the share. Either way they still sign in at the guard when one is set.");
    if (pub) after = accessSignIn(accessUI.state.auth || {}, f, false);
  } else {
    const mode = f.mode || "mtls";
    rows += accessRow("address", "address", `<input id="ac-f-address" placeholder="host:port, or a hostname behind your reverse proxy">`) +
      accessRow("mode", "guard", accessSelect("mode", "mode", mode,
        [["mtls", "mutual TLS"], ["oidc", "sign-in, for a board behind your proxy or VPN"]]),
        "Mutual TLS is today's direct link. A sign-in guards a board you put behind your own proxy or VPN.");
    if (mode === "oidc") after = accessSignIn(accessUI.state.auth || {}, f, true);
  }
  const gate = accessGate(kind, f);
  return `<div class="ac-add"><h4 class="ac-name">${accessIcon(kind, f.mode)} Add ${acEsc((ACCESS_KINDS[kind] || {}).label)}${kind === "zrok" ? " " + (f.mode || "private") : ""}</h4>
    <div class="ac-grid">${rows}${after}</div>
    <div class="ac-foot"><span class="ac-status warn" id="ac-why">${acEsc(gate)}</span>
      <button onclick="accessStartAdd('')">cancel</button>
      <button class="go" id="ac-add" ${gate ? "disabled" : ""} onclick="accessSubmitAdd()">add</button></div></div>`;
}
function paintAccess() {
  const host = document.getElementById("access");
  if (!host) return;
  const typed = {};
  if (!accessUI.fresh) host.querySelectorAll("[id^=ac-f-]").forEach(el => { typed[el.id] = el.type === "checkbox" ? el.checked : el.value; });
  accessUI.fresh = false;
  const s = accessUI.state;
  const on = accessUI.transports.filter(t => t.on || t.state !== "off");
  const list = on.map(accessTransport).join("") + (s.entries || []).map(accessEntry).join("") ||
    `<div class="ac-empty">Nothing yet. Atrium works with no overlay at all, on this machine.</div>`;
  const wait = (s.waiting || []).length ? `<h4 class="ac-sec">Rooms waiting for approval</h4>` +
    s.waiting.map(r => `<div class="ac-row"><span class="grow"><span class="ac-name">${acEsc(r.name)}</span>
      <span class="ac-sub">reached the hub over ${acEsc(r.via)}. It sees nothing until you approve it.</span></span>
      <button class="go" onclick="accessApprove('${acEsc(r.name)}')">approve</button></div>`).join("") : "";
  host.innerHTML = accessMenu() + accessForm() +
    `<h4 class="ac-sec">Configured access modes</h4>${list}
     ${wait}
     ${accessUI.readOnly ? `<span class="hintline">The hub's own transports are set on the hub's machine.</span>` : ""}`;
  for (const id in typed) {
    const el = document.getElementById(id);
    if (el) { if (el.type === "checkbox") el.checked = typed[id]; else el.value = typed[id]; }
  }
  if (accessUI.adding) accessRegate();
  clearTimeout(accessUI.poll);
  if ((s.entries || []).some(e => e.state === "signing in")) accessUI.poll = setTimeout(() => accessRefresh(accessUI.msg), 3000);
}
