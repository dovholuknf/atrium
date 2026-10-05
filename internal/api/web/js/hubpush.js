// ── the hub's Web Push, in the settings gear (u-new-web-push-build) ─────────
//
// The operator's half of phone alerts: the switch, the contact the push service can reach, the list of subscribed
// devices with remove, a test to every device, and a new key. A phone subscribes itself from the board's bell
// (m/js/push.js). The routes are in internal/link/pushapi.go and are the machine's own operator only, so a guest, a
// hub that is too old and a board reached over an overlay get a 404 or 403 and draw no row.

function hpEl(id) { return document.getElementById(id); }

// The hub answers a refusal as {"error": "..."}. Say the sentence, or the status when it is something else.
async function hpWhy(r) {
  let t = "";
  try { t = await r.text(); } catch (e) {}
  try { const j = JSON.parse(t); if (j && j.error) return j.error; } catch (e) {}
  return t || String(r.status);
}

function hpSend(path, method, body) {
  return plainFetch("/_hub/push" + path, { method, headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body) });
}

function hpPaint(p, keepEdits) {
  hpEl("s-hp-enabled").checked = !!p.enabled;
  if (!keepEdits) hpEl("s-hp-contact").value = p.contact || "";
  const subs = p.subscriptions || [];
  hpEl("s-hp-test").disabled = !p.enabled || subs.length === 0;
  hpEl("s-hp-count").textContent = subs.length + " of " + (p.max || 8) + " devices" +
    (p.enabled ? "" : ". Push is off, so none is sent to");
  const list = hpEl("s-hp-list");
  list.textContent = "";
  for (const s of subs) {
    const li = document.createElement("li");
    li.className = "hp-sub";
    const what = document.createElement("div");
    what.className = "hp-what";
    const name = document.createElement("b");
    name.textContent = s.label || "a device";
    what.appendChild(name);
    const more = document.createElement("span");
    more.className = "hp-more";
    more.textContent = " from " + (s.origin || "an unknown page") + ", through " + (s.service || "a push service") +
      ", added " + hnAgo(s.created_at) + (s.failures ? ", " + s.failures + " failed in a row" : "");
    what.appendChild(more);
    if (s.disabled_reason) {
      const off = document.createElement("div");
      off.className = "hp-off";
      off.textContent = "switched off: " + s.disabled_reason;
      what.appendChild(off);
    }
    li.appendChild(what);
    const rm = document.createElement("button");
    rm.className = "hp-remove";
    rm.textContent = "remove";
    rm.setAttribute("aria-label", "remove " + (s.label || "this device"));
    rm.onclick = () => removeHubPush(s.id);
    li.appendChild(rm);
    list.appendChild(li);
  }
}

// Read on open and after each change. There is no timer.
async function loadHubPush(keepEdits) {
  const row = hpEl("s-hp-row");
  if (!row) return;
  if (typeof isGuest === "function" && isGuest()) { row.hidden = true; return; }
  let p;
  try {
    const r = await hpSend("", "GET");
    if (!r.ok) { row.hidden = true; return; }
    p = await r.json();
  } catch (e) { return; }
  row.hidden = false;
  hpPaint(p, keepEdits);
}

async function hpChange(path, method, body, done) {
  const msg = hpEl("s-hp-msg");
  msg.textContent = "working";
  try {
    const r = await hpSend(path, method, body);
    if (!r.ok) { msg.textContent = "the hub refused it: " + await hpWhy(r); await loadHubPush(true); return; }
    const a = await r.json();
    msg.textContent = done ? done(a) : "saved";
  } catch (e) { msg.textContent = "could not reach the hub"; return; }
  await loadHubPush(true);
}

function setHubPush(on) { hpChange("", "PUT", { enabled: !!on }, () => on ? "on" : "off"); }

function saveHubPushContact() {
  hpChange("", "PUT", { contact: hpEl("s-hp-contact").value.trim() }, () => "contact saved");
}

function testHubPush() {
  hpChange("/test", "POST", undefined, a => a.sent === 0 ? "no device to send to" :
    a.ok + " of " + a.sent + " sent" + (a.error ? ". " + a.error : ""));
}

function removeHubPush(id) { hpChange("/subscriptions/" + encodeURIComponent(id), "DELETE", undefined, () => "removed"); }

function rotateHubPush() {
  if (!confirm("Make a new key? Every phone is removed, and each has to turn alerts on again.")) return;
  hpChange("", "PUT", { rotate: true }, () => "new key made, every device removed");
}
