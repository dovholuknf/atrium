// ── expose the board 3: the goal-first surface ───────────
//
// The second redo, a different bet from menu 2. Menu 2 lists the three
// transports and lets you turn any on. This one hides them: it asks who needs
// to reach the board and turns the answer into the right transport, so the
// operator never chooses zrok-vs-ziti and never reads the word "transport".
// One path is on screen at a time, which is the "one-click common case" taken
// as far as it goes.
//
// It reuses menu 2's action helpers where they take no DOM (exp2Start,
// exp2Stop, loadExpose2Overlays) and the shared board-login state (exp2Auth,
// exp2AuthState). Everything it reads from the page uses x3- ids, so no id
// collides with menu 2 or the classic panel. Deletable in one pass.

// The three answers, each mapped to the transport that serves it. `rowId`
// matches menu 2's EXP2_TRANSPORTS so exp2Start can be reused unchanged.
const EXP3_GOALS = [
  { id: "phone", tile: "My phone, from anywhere",
    sub: "a public URL, guarded by a login",
    rowId: "zrok-public", kind: "zrok", mode: "public" },
  { id: "machine", tile: "Another machine I run",
    sub: "a private zrok link, no public URL",
    rowId: "zrok-private", kind: "zrok", mode: "private" },
  { id: "ziti", tile: "A device on my OpenZiti network",
    sub: "bind the board to a service",
    rowId: "openziti", kind: "ziti" }
];

const EXP3_GOAL_KEY = "atrium.expose3.goal";
function exp3Goal() {
  try { return localStorage.getItem(EXP3_GOAL_KEY) || ""; } catch (e) { return ""; }
}
function exp3PickGoal(id) {
  try { localStorage.setItem(EXP3_GOAL_KEY, id); } catch (e) {}
  paintExpose3();
}

function paintExpose3() {
  const host = document.getElementById("expose3");
  if (!host) return;
  const chosen = exp3Goal();
  const tiles = EXP3_GOALS.map(g => {
    const o = (overlays || []).find(v => v.kind === g.kind) || {};
    const running = !!o.running && (g.kind !== "zrok" || (o.config || {}).mode === g.mode);
    return `<button class="x3-goal ${chosen === g.id ? "on" : ""}" onclick="exp3PickGoal('${g.id}')">
      <span class="x3-goal-t">${esc(g.tile)}</span>
      <span class="x3-goal-s">${esc(g.sub)}</span>
      ${running ? `<span class="x3-goal-live">reachable now</span>` : ""}
    </button>`;
  }).join("");

  const goal = EXP3_GOALS.find(g => g.id === chosen);
  const panel = goal ? exp3Panel(goal) : "";
  setHTML(host, `<p class="x3-q">Who needs to reach this board?</p>
    <div class="x3-goals">${tiles}</div>${panel}`);
}

// The focused panel for one answer.
function exp3Panel(g) {
  const o = (overlays || []).find(v => v.kind === g.kind) || { kind: g.kind };
  const ready = !!o.ready;
  const running = !!o.running && (g.kind !== "zrok" || (o.config || {}).mode === g.mode);

  let inner;
  if (!ready) {
    inner = exp3Setup(g);
  } else if (g.id === "phone") {
    inner = exp3Phone(g, o, running);
  } else if (g.id === "machine") {
    inner = exp3Machine(g, o, running);
  } else {
    inner = exp3Ziti(g, o, running);
  }
  return `<div class="x3-panel ${running ? "live" : ""}">${inner}</div>`;
}

// The enablement step, shown only if the transport this answer needs is not set
// up. Same endpoint as menu 2 and the classic panel; own x3- ids.
function exp3Setup(g) {
  const isZrok = g.kind === "zrok";
  const lead = isZrok
    ? "This uses zrok, and this machine is not enabled for it yet. Paste your zrok account "
      + "token once and atrium enables it."
    : "This uses your OpenZiti network, and atrium has no identity on it yet. Paste the one-use "
      + "enrollment token your network administrator gave you.";
  const tokLabel = isZrok ? "account token" : "enrollment token";
  const tokPlace = isZrok ? "paste your zrok account token" : "paste the enrollment JWT";
  const tokInput = isZrok
    ? `<input type="password" id="x3-${g.kind}-token" spellcheck="false" autocomplete="off"
         placeholder="${esc(tokPlace)}">`
    : `<textarea id="x3-${g.kind}-token" rows="3" spellcheck="false" placeholder="${esc(tokPlace)}"></textarea>`;
  return `<p class="x3-lead">${esc(lead)}</p>
    <div class="x3-field">
      <label class="eyebrow" for="x3-${g.kind}-token">${esc(tokLabel)}</label>
      ${tokInput}
    </div>
    <div class="x3-field">
      <label class="eyebrow" for="x3-${g.kind}-name">${isZrok ? "what to call this machine" : "call this identity"}</label>
      <input type="text" id="x3-${g.kind}-name" spellcheck="false"
        placeholder="${isZrok ? "left to zrok's default when empty" : "atrium"}">
    </div>
    <div class="x3-actions">
      <button class="go x3-go" onclick="exp3SetupRun('${g.kind}')">${
        isZrok ? "enable this machine" : "enroll this machine"}</button>
      <span class="grow"></span>
      <span class="hintline" id="x3-${g.kind}-said"></span>
    </div>`;
}

async function exp3SetupRun(kind) {
  const token = (document.getElementById(`x3-${kind}-token`) || {}).value || "";
  const name = (document.getElementById(`x3-${kind}-name`) || {}).value || "";
  const said = document.getElementById(`x3-${kind}-said`);
  if (!token.trim()) { if (said) said.textContent = "paste the token first"; return; }
  try {
    const res = await api(`/v1/overlays/${kind}/setup`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: token.trim(), name: name.trim() })
    });
    if (res.overlays) overlays = res.overlays;
    if (!res.ok && said) { said.textContent = res.error || "that did not work"; said.className = "hintline bad"; }
    await loadExpose2Overlays();
  } catch (e) {
    if (said) { said.textContent = e.message; said.className = "hintline bad"; }
  }
}

// The phone path: a public share, which is the one that needs board
// authentication. So the login lives here, folded into this answer, and the
// publish button is refused until it is set.
function exp3Phone(g, o, running) {
  const auth = (typeof exp2AuthState === "function") ? exp2AuthState() : { on: false, line: "" };
  const authBlock = exp3AuthBlock(auth);
  const c = o.config || {};
  const lock = running ? " disabled" : "";
  const name = `<div class="x3-field">
    <label class="eyebrow" for="x3-zrok-name">share name (optional)</label>
    <input type="text" id="x3-zrok-name"${lock} spellcheck="false" value="${esc(c.name || "")}"
      placeholder="atrium-xxxxxxxx, picked for you" onchange="exp3SaveZrok()">
    <span class="hintline">Becomes the hostname people type. Empty, atrium picks and keeps one,
      so the link survives a restart.</span>
  </div>`;
  const addr = (running && o.address) ? exp3Addr(o.address) : "";
  const note = running ? `<div class="x3-note">While this is up, <code>atrium stop</code> needs
    <code>--shutdown-token</code>.</div>` : "";
  const btn = running
    ? `<button class="no x3-go" onclick="exp3Stop('zrok')">stop publishing</button>`
    : `<button class="go x3-go" onclick="exp3Start('zrok-public')">publish to my phone</button>`;
  return `<p class="x3-lead">A public URL anyone can open, so it must sit behind a login. Open it
    on the phone, sign in, drive the board.</p>
    ${authBlock}${name}${addr}${note}
    <div class="x3-actions">${btn}<span class="grow"></span></div>`;
}

// Board authentication, as a small block inside the phone path. Warn-weight
// while unset, since the button below is refused until it is answered.
function exp3AuthBlock(auth) {
  const a = exp2Auth || {};
  const mode = !a.enabled ? "off" : (a.issuer && !a.basic) ? "oidc" : "basic";
  const opt = (v, label) => `<option value="${v}" ${v === mode ? "selected" : ""}>${esc(label)}</option>`;
  const title = auth.on ? "board authentication is set" : "board authentication is required first";
  const sel = `<div class="x3-field">
    <label class="eyebrow" for="x3-auth-mode">how people prove who they are</label>
    <div class="picker"><select id="x3-auth-mode" onchange="exp3SaveAuth()">
      ${opt("off", "no login (publishing is refused)")}
      ${opt("basic", "a name and a password")}
      ${opt("oidc", "an OIDC provider")}
    </select></div>
  </div>`;
  const basic = mode === "basic" ? `<div class="x3-field">
      <label class="eyebrow" for="x3-auth-user">name</label>
      <input type="text" id="x3-auth-user" spellcheck="false" autocomplete="off"
        value="${esc(a.user || "")}" placeholder="who types the password" onchange="exp3SaveAuth()">
    </div>
    <div class="x3-field">
      <label class="eyebrow" for="x3-auth-pass">password</label>
      <input type="password" id="x3-auth-pass" autocomplete="new-password"
        placeholder="typing here sets it" onchange="exp3SaveAuth()">
      <span class="hintline">${a.has_password ? "a password is set. typing replaces it, empty keeps it."
        : "no password set yet."}</span>
    </div>` : "";
  const oidc = mode === "oidc" ? `<div class="x3-field">
      <label class="eyebrow" for="x3-auth-issuer">provider</label>
      <input type="text" id="x3-auth-issuer" spellcheck="false" value="${esc(a.issuer || "")}"
        placeholder="https://keycloak.example/realms/yours" onchange="exp3SaveAuth()">
    </div>
    <div class="x3-field">
      <label class="eyebrow" for="x3-auth-allow">who may in</label>
      <input type="text" id="x3-auth-allow" spellcheck="false" value="${esc((a.allow || []).join(", "))}"
        placeholder="you@example.com" onchange="exp3SaveAuth()">
      <span class="hintline">Comma separated. Empty means nobody, on purpose.</span>
    </div>` : "";
  return `<div class="x3-auth ${auth.on ? "" : "stop"}">
    <div class="x3-auth-t">${esc(title)}</div>
    ${sel}${basic}${oidc}
    <span class="hintline" id="x3-auth-said"></span>
  </div>`;
}

async function exp3SaveAuth() {
  const val = id => (document.getElementById(id) || {}).value || "";
  const mode = val("x3-auth-mode");
  const said = document.getElementById("x3-auth-said");
  const a = exp2Auth || {};
  const body = {
    enabled: mode !== "off",
    basic: mode === "basic",
    user: val("x3-auth-user").trim(),
    password: val("x3-auth-pass"),
    issuer: mode === "oidc" ? val("x3-auth-issuer").trim() : a.issuer || "",
    client_id: a.client_id || "",
    redirect: a.redirect || "",
    allow: mode === "oidc" ? val("x3-auth-allow").split(",").map(s => s.trim()).filter(Boolean) : a.allow || [],
    client_secret: ""
  };
  try {
    await api("/v1/auth", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    if (said) { said.textContent = "saved."; said.className = "hintline ok"; }
    if (typeof loadExpose2 === "function") await loadExpose2();
    paintExpose3();
  } catch (e) {
    if (said) { said.textContent = e.message; said.className = "hintline bad"; }
  }
}

// The machine path: a private zrok share. No login needed, zrok is the gate.
function exp3Machine(g, o, running) {
  const c = o.config || {};
  const lock = running ? " disabled" : "";
  const tok = `<div class="x3-field">
    <label class="eyebrow" for="x3-zrok-token-priv">share token (optional)</label>
    <input type="text" id="x3-zrok-token-priv"${lock} spellcheck="false" value="${esc(c.share_token || "")}"
      placeholder="reuses a reserved private share" onchange="exp3SaveZrok()">
    <span class="hintline">Left empty, zrok makes one. Reusing a reserved token keeps the address
      across a restart.</span>
  </div>`;
  const addr = (running && o.address) ? exp3Addr(o.address) : "";
  const note = running ? `<div class="x3-note">On the other machine, run
    <code>zrok access private ${esc(o.address || "&lt;token&gt;")}</code>. While this is up,
    <code>atrium stop</code> needs <code>--shutdown-token</code>.</div>` : "";
  const btn = running
    ? `<button class="no x3-go" onclick="exp3Stop('zrok')">stop the link</button>`
    : `<button class="go x3-go" onclick="exp3Start('zrok-private')">create the private link</button>`;
  return `<p class="x3-lead">A private link. The far machine needs zrok and the share token, not
    just a URL, so there is no login to set.</p>
    ${tok}${addr}${note}
    <div class="x3-actions">${btn}<span class="grow"></span></div>`;
}

// The ziti path: bind the board to a service on the overlay.
function exp3Ziti(g, o, running) {
  const c = o.config || {};
  const lock = running ? " disabled" : "";
  const svc = `<div class="x3-field">
    <label class="eyebrow" for="x3-ziti-service">service to bind</label>
    <div class="picker">
      <input type="text" id="x3-ziti-service"${lock} spellcheck="false" value="${esc(c.service || "")}"
        placeholder="atrium" onchange="exp3SaveZiti()">
      <button onclick="exp3ZitiServices()"${lock}>what can I host?</button>
    </div>
    <span class="hintline" id="x3-ziti-svc-said">The service this board answers. It must exist,
      with a bind policy this identity satisfies.</span>
  </div>`;
  const addr = (running && o.address) ? exp3Addr(o.address) : "";
  const note = running ? `<div class="x3-note">The device runs the OpenZiti tunneler, already
    enrolled, and opens the service. While this is up, <code>atrium stop</code> needs
    <code>--shutdown-token</code>.</div>` : "";
  const btn = running
    ? `<button class="no x3-go" onclick="exp3Stop('ziti')">stop binding</button>`
    : `<button class="go x3-go" onclick="exp3Start('openziti')">bind the board</button>`;
  return `<p class="x3-lead">The board is bound to a service on your OpenZiti network. A device on
    that network reaches it through the tunneler.</p>
    ${svc}${addr}${note}
    <div class="x3-actions">${btn}<span class="grow"></span></div>`;
}

function exp3Addr(address) {
  return `<div class="x3-addr">
    <code>${esc(address)}</code>
    <button onclick='copyText(this, ${JSON.stringify(address).replace(/'/g, "&#39;")})'>copy</button>
  </div>`;
}

// Config writes: merge onto stored config so a field this surface does not show
// is not blanked. Same endpoints as menu 2.
async function exp3SaveZrok() {
  const o = (overlays || []).find(v => v.kind === "zrok") || {};
  const body = Object.assign({}, o.config || {});
  const name = document.getElementById("x3-zrok-name");
  const tok = document.getElementById("x3-zrok-token-priv");
  if (name) body.name = name.value;
  if (tok) body.share_token = tok.value;
  await exp3PutConfig("zrok", body);
}
async function exp3SaveZiti() {
  const o = (overlays || []).find(v => v.kind === "ziti") || {};
  const body = Object.assign({}, o.config || {});
  const svc = document.getElementById("x3-ziti-service");
  if (svc) body.service = svc.value;
  await exp3PutConfig("ziti", body);
}
async function exp3PutConfig(kind, body) {
  try {
    await api(`/v1/overlays/${kind}`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    await loadExpose2Overlays();
  } catch (e) { toast("that did not stick", e.message); await loadExpose2Overlays(); }
}

// Start and stop reuse menu 2's DOM-free helpers, but menu 2's start sets the
// zrok mode from the row id, which is exactly what is wanted here too.
async function exp3Start(rowId) {
  if (typeof exp2Start === "function") return exp2Start(rowId);
}
async function exp3Stop(kind) {
  if (typeof exp2Stop === "function") return exp2Stop(kind);
}

async function exp3ZitiServices() {
  const said = document.getElementById("x3-ziti-svc-said");
  if (said) said.textContent = "asking the network...";
  let cap;
  try { cap = await api("/v1/overlays/ziti/services"); }
  catch (e) { if (said) said.textContent = e.message || String(e); return; }
  if (cap.err) { if (said) said.textContent = cap.err; return; }
  const svcs = cap.services || [];
  if (!svcs.length) { if (said) said.textContent = "this identity can bind no services."; return; }
  const rows = svcs.map(s => s.bind
    ? `<span class="chip attach" title="use this one" onclick="exp3UseZitiService('${esc(s.name)}')">${esc(s.name)}</span>`
    : `<span class="chip" title="reachable but not bindable">${esc(s.name)} (dial only)</span>`).join(" ");
  if (said) said.innerHTML = `${cap.bindable} of ${svcs.length} can be hosted.<br>${rows}`;
}
function exp3UseZitiService(name) {
  const el = document.getElementById("x3-ziti-service");
  if (el) el.value = name;
  exp3SaveZiti();
}
