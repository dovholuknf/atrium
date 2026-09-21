// ── expose the board 2: the redo surface ─────────────────
//
// A coexisting alternate to the classic overlay panel (js/overlays.js), added
// so clint can open both in the settings nav and judge them against the real
// board. It reinvents no persistence: every read and write here goes to the
// same daemon endpoints the classic panel uses (/v1/overlays, /v1/auth). What
// is new is only the SHAPE.
//
// The shape, decided in the interview (docs/interview-log.md, 2026-09-20):
//   - one collapsed row per thing you can turn on
//   - board authentication first, then the three transports
//   - a row is one line until you open it, so an untouched panel is a short
//     column and the common case is one press
//   - zrok public cannot be started until board authentication is set
//
// It reuses the shared `overlays` global that loadOverlays() keeps fresh, so
// acting on either surface keeps both in step. It keeps its own copy of the
// board login (exp2Auth), fetched here rather than shared, because the classic
// loadAuth() writes straight into the DOM and keeps no state to borrow.
//
// EVERYTHING here is prefixed xb / exp2, and its markup is one section in
// index.html, so the whole surface is deletable in one pass if it loses.

// The three transports, in the order the design lists them. Each names the
// overlay kind it drives and, for zrok, which mode a share of this row runs in.
// The two zrok rows share one enablement and one listener: zrok's share mode is
// singular, so starting one stops the other, which the rows say rather than
// hide.
const EXP2_TRANSPORTS = [
  { id: "zrok-private", kind: "zrok", mode: "private", name: "zrok private",
    blurb: "reach it from another machine you control that also runs zrok" },
  { id: "zrok-public", kind: "zrok", mode: "public", name: "zrok public",
    blurb: "a public URL anyone can open, so it needs board authentication" },
  { id: "openziti", kind: "ziti", name: "openziti",
    blurb: "bind the board to a service on your OpenZiti overlay" }
];

// The board login, read from the same /v1/auth the classic "who may open it"
// section writes. Null until the first fetch.
let exp2Auth = null;

// Which rows are open. Kept in this browser, since it is about this screen, and
// keyed by row id so opening one to change a field survives a repaint.
const EXP2_OPEN_KEY = "atrium.expose2.open";
function exp2IsOpen(id) {
  try { return !!JSON.parse(localStorage.getItem(EXP2_OPEN_KEY) || "{}")[id]; }
  catch (e) { return false; }
}
function exp2Toggle(id) {
  let all = {};
  try { all = JSON.parse(localStorage.getItem(EXP2_OPEN_KEY) || "{}"); } catch (e) {}
  all[id] = !all[id];
  localStorage.setItem(EXP2_OPEN_KEY, JSON.stringify(all));
  paintExpose2();
}

// The gear opened, or the config changed. Fetch the board login, then paint.
// Overlays arrive on the shared global from loadOverlays(), which paintSettings
// calls alongside this.
async function loadExpose2() {
  try { exp2Auth = await api("/v1/auth"); }
  catch (e) { exp2Auth = null; }
  paintExpose2();
}

// Whether the board login is set at all, and how it reads in one line.
function exp2AuthState() {
  const a = exp2Auth;
  if (!a || !a.enabled) return { on: false, word: "no login", line: "nobody is asked to sign in" };
  if (a.basic && a.issuer) return { on: true, word: "login set", line: "a name and password, and an OIDC provider" };
  if (a.basic) return { on: true, word: "login set", line: "a name and a password" };
  if (a.issuer) return { on: true, word: "login set", line: "an OIDC provider" };
  return { on: false, word: "no login", line: "sign-in is on but nothing is configured to check" };
}

function paintExpose2() {
  const host = document.getElementById("expose2");
  if (!host) return;
  const auth = exp2AuthState();
  const rows = [exp2AuthRow(auth)]
    .concat(EXP2_TRANSPORTS.map(t => exp2Row(t, auth)))
    .join("");
  setHTML(host, rows);
}

// One row's head, shared by the login row and the transport rows: icon tile,
// name, one line, a state word, a caret.
function exp2Head(id, mark, kind, name, blurb, state) {
  return `<div class="xb-head" onclick="exp2Toggle('${esc(id)}')">
    <span class="xb-mark" data-kind="${esc(kind)}">${mark}</span>
    <span class="grow">
      <span class="xb-name">${esc(name)}</span>
      <span class="xb-blurb">${esc(blurb)}</span>
    </span>
    <span class="xb-state ${state.cls || ""}">${esc(state.word)}</span>
    <span class="xb-caret">&#8250;</span>
  </div>`;
}

// The board authentication row. First, because the interview put it there: it
// is shared across every transport and it is what zrok public waits on.
function exp2AuthRow(auth) {
  const open = exp2IsOpen("board-auth");
  const key = `<span aria-hidden="true">&#128273;</span>`;
  const state = auth.on
    ? { cls: "on", word: "login set" }
    : { cls: "", word: "no login" };
  const head = exp2Head("board-auth", key, "auth", "board authentication",
    auth.line, state);
  const body = open ? `<div class="xb-body"><div class="xb-body-in">
    ${exp2AuthBody()}
  </div></div>` : "";
  return `<div class="xb-row ${open ? "open" : ""} ${auth.on ? "" : ""}">${head}${body}</div>`;
}

// The board login, as three choices rather than a wall of fields: off, a name
// and a password, or an OIDC provider. The fields for the chosen one appear
// under it. Writes the same /v1/auth body the classic saveAuth does, so the two
// surfaces set exactly the same thing.
function exp2AuthBody() {
  const a = exp2Auth || {};
  const mode = !a.enabled ? "off" : (a.issuer && !a.basic) ? "oidc" : "basic";
  const opt = (v, label) =>
    `<option value="${v}" ${v === mode ? "selected" : ""}>${esc(label)}</option>`;
  const sel = `<div class="xb-field">
    <label class="eyebrow" for="xb-auth-mode">how people prove who they are</label>
    <div class="picker"><select id="xb-auth-mode" onchange="exp2SaveAuth()">
      ${opt("off", "no login (a public share is refused)")}
      ${opt("basic", "a name and a password")}
      ${opt("oidc", "an OIDC provider")}
    </select></div>
  </div>`;

  const basic = mode === "basic" ? `
    <div class="xb-field">
      <label class="eyebrow" for="xb-auth-user">name</label>
      <input type="text" id="xb-auth-user" spellcheck="false" autocomplete="off"
        value="${esc(a.user || "")}" placeholder="who types the password"
        onchange="exp2SaveAuth()">
    </div>
    <div class="xb-field">
      <label class="eyebrow" for="xb-auth-pass">password</label>
      <input type="password" id="xb-auth-pass" autocomplete="new-password"
        placeholder="typing here sets it" onchange="exp2SaveAuth()">
      <span class="hintline">${a.has_password
        ? "a password is set. typing replaces it, empty keeps it."
        : "no password set yet."}</span>
    </div>` : "";

  const oidc = mode === "oidc" ? `
    <div class="xb-field">
      <label class="eyebrow" for="xb-auth-issuer">provider</label>
      <input type="text" id="xb-auth-issuer" spellcheck="false"
        value="${esc(a.issuer || "")}" placeholder="https://keycloak.example/realms/yours"
        onchange="exp2SaveAuth()">
      <span class="hintline">The realm URL, not the admin console.</span>
    </div>
    <div class="xb-field">
      <label class="eyebrow" for="xb-auth-client">client id</label>
      <input type="text" id="xb-auth-client" spellcheck="false"
        value="${esc(a.client_id || "")}" placeholder="atrium" onchange="exp2SaveAuth()">
    </div>
    <div class="xb-field">
      <label class="eyebrow" for="xb-auth-redirect">where the provider sends people back</label>
      <input type="text" id="xb-auth-redirect" spellcheck="false"
        value="${esc(a.redirect || "")}" placeholder="https://your-board-address/auth/callback"
        onchange="exp2SaveAuth()">
    </div>
    <div class="xb-field">
      <label class="eyebrow" for="xb-auth-allow">who may in</label>
      <input type="text" id="xb-auth-allow" spellcheck="false"
        value="${esc((a.allow || []).join(", "))}"
        placeholder="you@example.com, someone-else@example.com" onchange="exp2SaveAuth()">
      <span class="hintline">Comma separated. Empty means nobody, on purpose.</span>
    </div>` : "";

  const said = `<span class="hintline" id="xb-auth-said"></span>`;
  return sel + basic + oidc + said;
}

// Reads the login form back and writes it, the same shape as classic saveAuth.
// The mode select decides which half is sent: "basic" turns basic on and clears
// nothing else, "oidc" sends the provider fields, "off" disables sign-in.
async function exp2SaveAuth() {
  const val = id => (document.getElementById(id) || {}).value || "";
  const mode = val("xb-auth-mode");
  const said = document.getElementById("xb-auth-said");
  const body = {
    enabled: mode !== "off",
    basic: mode === "basic",
    user: val("xb-auth-user").trim(),
    password: val("xb-auth-pass"),
    issuer: mode === "oidc" ? val("xb-auth-issuer").trim() : (exp2Auth || {}).issuer || "",
    client_id: mode === "oidc" ? val("xb-auth-client").trim() : (exp2Auth || {}).client_id || "",
    redirect: mode === "oidc" ? val("xb-auth-redirect").trim() : (exp2Auth || {}).redirect || "",
    allow: mode === "oidc"
      ? val("xb-auth-allow").split(",").map(s => s.trim()).filter(Boolean)
      : (exp2Auth || {}).allow || [],
    client_secret: ""
  };
  try {
    await api("/v1/auth", {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
    if (said) { said.textContent = "saved."; said.className = "hintline ok"; }
    loadExpose2();
  } catch (e) {
    if (said) { said.textContent = e.message; said.className = "hintline bad"; }
  }
}

// One transport row. The overlay it drives comes off the shared global; if it
// is not there yet (the daemon has not answered), the row still draws with a
// "needs setting up" state rather than vanishing.
function exp2Row(t, auth) {
  const o = (overlays || []).find(v => v.kind === t.kind) || { kind: t.kind };
  const mark = (typeof OVERLAY_MARKS === "object" && OVERLAY_MARKS[t.kind]) || "";
  const open = exp2IsOpen(t.id);
  // Running THIS row: the overlay is up and, for zrok, in this row's mode.
  const running = !!o.running && (t.kind !== "zrok" || (o.config || {}).mode === t.mode);
  const ready = !!o.ready;
  const state = running
    ? { cls: "on", word: "sharing" }
    : ready
      ? { cls: "", word: "ready" }
      : { cls: "warn", word: "set up" };

  const head = exp2Head(t.id, mark, t.kind, t.name, t.blurb, state);
  const body = open ? `<div class="xb-body"><div class="xb-body-in">
    ${exp2Body(t, o, ready, running, auth)}
  </div></div>` : "";
  return `<div class="xb-row ${open ? "open" : ""} ${running ? "live" : ""}">${head}${body}</div>`;
}

// The open body of a transport row. Three cases in the order you hit them:
// nothing enabled yet, ready to share, sharing now.
function exp2Body(t, o, ready, running, auth) {
  if (!ready) return exp2Setup(t, o);

  const addr = (running && o.address) ? `<div class="xb-addr">
      <code>${esc(o.address)}</code>
      <button onclick='copyText(this, ${JSON.stringify(o.address).replace(/'/g, "&#39;")})'>copy</button>
    </div>` : "";

  // The one gate the redo shows before the daemon does: a public share with no
  // board authentication. The daemon is still the authority and its refusal is
  // surfaced on start, but saying it here means the button is not a surprise.
  const gate = (t.id === "zrok-public" && !auth.on)
    ? `<div class="xb-note stop">Board authentication is not set, so a public URL would be the
        open internet. Open <b>board authentication</b> above and set a login first.</div>`
    : "";

  const fields = exp2Fields(t, o, running);

  const note = running
    ? `<div class="xb-note">While this is up, <code>atrium stop</code> needs
        <code>--shutdown-token</code>: a share makes every request look local.</div>`
    : "";

  const startLabel = t.kind === "zrok"
    ? `start ${t.mode} share`
    : "start sharing";
  const btn = running
    ? `<button class="no" onclick="exp2Stop('${esc(t.kind)}')">stop sharing</button>`
    : `<button class="go" onclick="exp2Start('${esc(t.id)}')">${esc(startLabel)}</button>`;

  return `${gate}${fields}${addr}${note}
    <div class="xb-actions">${btn}<span class="grow"></span></div>`;
}

// The enablement step, before a transport can share at all. zrok needs an
// account token, ziti an enrollment JWT. Same shape: paste, name, one button.
// Posts to the same /v1/overlays/{kind}/setup the classic panel uses.
function exp2Setup(t, o) {
  const isZrok = t.kind === "zrok";
  const title = isZrok ? "this machine is not enabled for zrok yet"
    : "no OpenZiti identity yet";
  const what = isZrok
    ? "zrok needs an environment before it can share. That comes from your account token, "
      + "which you get once and reuse on every machine."
    : "An identity proves this machine to your network. It comes from a one-use enrollment "
      + "token your network administrator issues.";
  const tokLabel = isZrok ? "account token" : "enrollment token";
  const tokPlace = isZrok ? "paste your zrok account token" : "paste the enrollment JWT";
  const tokInput = isZrok
    ? `<input type="password" id="xb-${esc(t.kind)}-token" spellcheck="false" autocomplete="off"
         placeholder="${esc(tokPlace)}">`
    : `<textarea id="xb-${esc(t.kind)}-token" rows="3" spellcheck="false"
         placeholder="${esc(tokPlace)}"></textarea>`;
  const nameLabel = isZrok ? "what to call this machine" : "call this identity";
  const namePlace = isZrok ? "left to zrok's default when empty" : "atrium";
  return `<div class="xb-note">${esc(title)}</div>
    <p class="xb-lead" style="margin-top:10px">${esc(what)}</p>
    <div class="xb-field">
      <label class="eyebrow" for="xb-${esc(t.kind)}-token">${esc(tokLabel)}</label>
      ${tokInput}
    </div>
    <div class="xb-field">
      <label class="eyebrow" for="xb-${esc(t.kind)}-name">${esc(nameLabel)}</label>
      <input type="text" id="xb-${esc(t.kind)}-name" spellcheck="false" placeholder="${esc(namePlace)}">
    </div>
    <div class="xb-actions">
      <button class="go" onclick="exp2SetupRun('${esc(t.kind)}')">${
        isZrok ? "enable this machine" : "enroll this machine"}</button>
      <span class="grow"></span>
      <span class="hintline" id="xb-${esc(t.kind)}-said"></span>
    </div>`;
}

async function exp2SetupRun(kind) {
  const token = (document.getElementById(`xb-${kind}-token`) || {}).value || "";
  const name = (document.getElementById(`xb-${kind}-name`) || {}).value || "";
  const said = document.getElementById(`xb-${kind}-said`);
  if (!token.trim()) { if (said) said.textContent = "paste the token first"; return; }
  try {
    const res = await api(`/v1/overlays/${kind}/setup`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: token.trim(), name: name.trim() })
    });
    if (res.overlays) overlays = res.overlays;
    if (!res.ok && said) { said.textContent = res.error || "that did not work"; said.className = "hintline bad"; }
    paintExpose2();
    if (typeof paintOverlays === "function") paintOverlays();
  } catch (e) {
    if (said) { said.textContent = e.message; said.className = "hintline bad"; }
  }
}

// The per-transport inputs shown once it is ready. Kept to what each one asks
// for in the design, and disabled while a share is up, because the daemon
// refuses to change a running share and a field that takes an edit then reports
// a refusal has taught nothing twice.
function exp2Fields(t, o, running) {
  const lock = running ? " disabled" : "";
  const c = o.config || {};
  if (t.id === "zrok-private") {
    return `<div class="xb-field">
      <label class="eyebrow" for="xb-zrok-token-priv">share token (optional)</label>
      <input type="text" id="xb-zrok-token-priv"${lock} spellcheck="false"
        value="${esc(c.share_token || "")}" placeholder="reuses a reserved private share"
        onchange="exp2SaveConfig('zrok')">
      <span class="hintline">Left empty, zrok makes one. Reusing a reserved token keeps the
        address across a restart.</span>
    </div>`;
  }
  if (t.id === "zrok-public") {
    return `<div class="xb-field">
      <label class="eyebrow" for="xb-zrok-name">share name</label>
      <input type="text" id="xb-zrok-name"${lock} spellcheck="false"
        value="${esc(c.name || "")}" placeholder="atrium-xxxxxxxx, picked for you"
        onchange="exp2SaveConfig('zrok')">
      <span class="hintline">Becomes the hostname people type. Empty, atrium invents one and
        keeps it. Either way it is reserved, so the link survives a restart.</span>
    </div>`;
  }
  // openziti
  return `<div class="xb-field">
      <label class="eyebrow" for="xb-ziti-service">service to bind</label>
      <div class="picker">
        <input type="text" id="xb-ziti-service"${lock} spellcheck="false"
          value="${esc(c.service || "")}" placeholder="atrium" onchange="exp2SaveConfig('ziti')">
        <button onclick="exp2ZitiServices()"${lock}>what can I host?</button>
      </div>
      <span class="hintline" id="xb-ziti-svc-said">The service this board answers. It has to
        exist, with a bind policy this identity satisfies.</span>
    </div>`;
}

// Asks the network what this identity may bind, and offers the bindable ones as
// chips that fill the box. Same endpoint as the classic panel's zitiServices.
async function exp2ZitiServices() {
  const said = document.getElementById("xb-ziti-svc-said");
  if (said) said.textContent = "asking the network...";
  let cap;
  try { cap = await api("/v1/overlays/ziti/services"); }
  catch (e) { if (said) said.textContent = e.message || String(e); return; }
  if (cap.err) { if (said) said.textContent = cap.err; return; }
  const svcs = cap.services || [];
  if (!svcs.length) { if (said) said.textContent = "this identity can bind no services."; return; }
  const rows = svcs.map(s => s.bind
    ? `<span class="chip attach" title="use this one"
         onclick="exp2UseZitiService('${esc(s.name)}')">${esc(s.name)}</span>`
    : `<span class="chip" title="reachable but not bindable by this identity">${esc(s.name)} (dial only)</span>`
  ).join(" ");
  if (said) said.innerHTML = `${cap.bindable} of ${svcs.length} can be hosted.<br>${rows}`;
}

function exp2UseZitiService(name) {
  const el = document.getElementById("xb-ziti-service");
  if (el) el.value = name;
  exp2SaveConfig("ziti");
}

// Writes one overlay's config back, reading only the fields this surface owns,
// merged onto what is already stored so a field the redo does not show is not
// blanked. For zrok, mode is left to the start path, which sets it from the row
// that was pressed.
async function exp2SaveConfig(kind) {
  const o = (overlays || []).find(v => v.kind === kind) || {};
  const body = Object.assign({}, o.config || {});
  if (kind === "zrok") {
    const name = document.getElementById("xb-zrok-name");
    const tok = document.getElementById("xb-zrok-token-priv");
    if (name) body.name = name.value;
    if (tok) body.share_token = tok.value;
  } else if (kind === "ziti") {
    const svc = document.getElementById("xb-ziti-service");
    if (svc) body.service = svc.value;
  }
  try {
    await api(`/v1/overlays/${kind}`, {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
    await loadExpose2Overlays();
  } catch (e) {
    toast("that did not stick", e.message);
    await loadExpose2Overlays();
  }
}

// Sets the zrok mode for the pressed row (or nothing for ziti), then starts.
// The public confirm and the shutdown-token consequence are the daemon's to
// enforce; the redo confirms the public case here for the same reason the
// classic panel does, since a public URL with no login is the one press worth
// stopping to think about.
async function exp2Start(rowId) {
  const t = EXP2_TRANSPORTS.find(x => x.id === rowId);
  if (!t) return;
  if (t.id === "zrok-public") {
    const ok = await confirmUser("share this publicly?",
      "A public zrok share is a link anyone who has it can open. Whoever opens it sees every "
      + "card and can answer permission requests. Set board authentication first if you have not.",
      "share it publicly");
    if (!ok) return;
  }
  // For zrok, set the mode of this row before starting: the daemon runs one
  // share at a time, so starting public means public.
  if (t.kind === "zrok") {
    const o = (overlays || []).find(v => v.kind === "zrok") || {};
    const body = Object.assign({}, o.config || {}, {
      public: t.mode === "public", private: t.mode === "private"
    });
    try {
      await api(`/v1/overlays/zrok`, {
        method: "PUT", headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body)
      });
    } catch (e) { toast("could not set the share mode", e.message); return; }
  }
  try {
    const res = await api(`/v1/overlays/${t.kind}/start`, { method: "POST" });
    if (res.overlays) overlays = res.overlays;
  } catch (e) { toast("could not start it", e.message); }
  await loadExpose2Overlays();
  // The address lands on the share's output a moment after it starts.
  setTimeout(loadExpose2Overlays, 1500);
  setTimeout(loadExpose2Overlays, 4000);
}

async function exp2Stop(kind) {
  try {
    const res = await api(`/v1/overlays/${kind}/stop`, { method: "POST" });
    if (res.overlays) overlays = res.overlays;
  } catch (e) { toast("could not stop it", e.message); }
  await loadExpose2Overlays();
}

// Refetches the shared overlay list and repaints both surfaces, so acting on
// the redo updates the classic panel behind it too.
async function loadExpose2Overlays() {
  try { overlays = (await api("/v1/overlays")).overlays || overlays; } catch (e) {}
  paintExpose2();
  if (typeof paintOverlays === "function") paintOverlays();
}
