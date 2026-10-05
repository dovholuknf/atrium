// The phone page's "phone alerts" switch: web push, so a locked phone hears that a card needs it.
//
// It lives in the notifications sheet, under the bell. What it does, in order, on a tap:
//   1. asks for notification permission, which no browser lets a page do on its own, so it is the tap that asks
//   2. reads the hub's public key (`GET /_hub/push/key`, 404 while the operator has push off)
//   3. registers the service worker and subscribes the browser to its push service with that key
//   4. hands the hub the subscription (`POST /_hub/push/subscriptions`), whose answer may be a refusal in words
// A second tap undoes it: `DELETE /_hub/push/subscriptions` with the endpoint in the body, which is the proof this
// browser owns it, and then the browser's own unsubscribe. See docs/rnd/web-push-design.md.
//
// FIVE THINGS THE ROW CAN SAY, and the reason is always on the row, never a silent switch that does nothing:
//   off          nothing is subscribed. The switch is there.
//   on           this browser is subscribed and the hub knows.
//   refused      permission was denied, the browser refused to subscribe, or the hub said no. The sentence says which.
//   unavailable  this page cannot do push at all: not a secure context, no PushManager, an iPhone tab that is not on
//                the home screen, or the hub has push off.
//   working      between the tap and the answer.
(function () {
  "use strict";

  const SEEN_WAIT_MS = 30000;
  const KEY_URL = "/_hub/push/key";
  const SUBS_URL = "/_hub/push/subscriptions";

  let els = null;
  let state = "off";
  let note = "";
  let seenTimer = 0;
  let seen = false;

  const q = id => document.getElementById(id);

  // The hub's key is base64url, and the browser wants bytes.
  function keyBytes(b64) {
    const s = String(b64).replace(/-/g, "+").replace(/_/g, "/");
    const bin = atob(s + "=".repeat((4 - (s.length % 4)) % 4));
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  }

  const isIOS = () => /iPad|iPhone|iPod/.test(navigator.userAgent) ||
    (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
  const standalone = () => !!(navigator.standalone || (window.matchMedia && window.matchMedia("(display-mode: standalone)").matches));

  // A name for this device, which the hub shows in the desktop gear and in the growler line for a new subscriber.
  // The owner's own name for it wins, kept in this browser and sent with every subscribe.
  const NAME_KEY = "atrium.push.label";
  function savedName() {
    try { return (localStorage.getItem(NAME_KEY) || "").trim(); } catch (e) { return ""; }
  }
  function deviceLabel() {
    return savedName() || guessLabel();
  }
  function guessLabel() {
    const ua = navigator.userAgent;
    const os = /iPhone/.test(ua) ? "iPhone" : /iPad/.test(ua) ? "iPad" : /Android/.test(ua) ? "Android phone"
      : /Windows/.test(ua) ? "Windows" : /Mac/.test(ua) ? "Mac" : /Linux/.test(ua) ? "Linux" : "phone";
    const br = navigator.brave ? "Brave" : /Edg\//.test(ua) ? "Edge" : /Firefox\//.test(ua) ? "Firefox"
      : /CriOS|Chrome\//.test(ua) ? "Chrome" : /Safari\//.test(ua) ? "Safari" : "";
    return br ? os + " (" + br + ")" : os;
  }

  // Why this page can never do push, or "" when it can. These are checked before anything is asked of anybody.
  function cannot() {
    if (!window.isSecureContext) {
      return "push needs a secure page. open atrium over https or on this machine's own address, and it is offered";
    }
    if (isIOS() && !standalone()) {
      return "on an iPhone push works only for a web app on the home screen. tap Share, then Add to Home Screen, and open it from there";
    }
    if (!("serviceWorker" in navigator) || !("PushManager" in window) || !("Notification" in window)) {
      return "this browser has no web push";
    }
    return "";
  }

  const BRAVE_NOTE = "the browser would not subscribe. in Brave, turn on Settings, Privacy, Use Google services for push messaging";

  function paint() {
    if (!els) return;
    els.row.dataset.state = state;
    const on = state === "on";
    els.btn.textContent = state === "working" ? "working" : on ? "turn off" : "turn on";
    els.btn.setAttribute("aria-pressed", String(on));
    els.btn.disabled = state === "working" || state === "unavailable";
    els.btn.hidden = state === "unavailable";
    const line = {
      off: "this phone is not told when a card needs you and the page is closed",
      on: "a card that needs you buzzes this phone, locked or not",
      refused: "",
      unavailable: "",
      working: "one moment",
    }[state];
    els.note.textContent = note || line || "";
    els.note.classList.toggle("bad", state === "refused" || state === "unavailable");
    // The name is only worth editing while this phone is subscribed, since that is when the hub shows it.
    els.nameRow.hidden = !on;
    if (on && document.activeElement !== els.name) els.name.value = deviceLabel();
  }

  // The hub's subscribe is also a rename: the same endpoint is the same device, and it sends no second test push.
  async function rename() {
    const v = els.name.value.trim().slice(0, 60);
    try {
      if (v && v !== guessLabel()) localStorage.setItem(NAME_KEY, v);
      else localStorage.removeItem(NAME_KEY);
    } catch (e) {}
    els.name.value = deviceLabel();
    if (state !== "on") return;
    let sub = null;
    try { sub = await current(); } catch (e) {}
    if (!sub) return;
    const j = sub.toJSON();
    try {
      await window.mNet.api(SUBS_URL, { method: "POST", body: JSON.stringify({
        endpoint: j.endpoint, keys: j.keys, label: deviceLabel(), origin: location.origin }) });
      note = "named " + deviceLabel();
    } catch (e) {
      note = "the hub did not take the name: " + ((e && e.message) || e);
    }
    paint();
  }

  function set(s, n) { state = s; note = n || ""; paint(); }

  // The registration this page uses, made if it is not there. The board registers the same worker, so it is one
  // registration for the origin and not two.
  async function registration() {
    let reg = await navigator.serviceWorker.getRegistration("/");
    if (!reg) reg = await navigator.serviceWorker.register("/sw.js");
    await navigator.serviceWorker.ready;
    return reg;
  }

  async function current() {
    const reg = await navigator.serviceWorker.getRegistration("/");
    return reg && reg.pushManager ? reg.pushManager.getSubscription() : null;
  }

  // What is true right now, asked when the sheet opens. A subscription the browser holds and the hub has pushed out
  // is not told apart here: the hub removes a dead one when the push service says it is gone, and a page that finds
  // none subscribed simply offers the switch again.
  async function refresh() {
    if (!els) return;
    const why = cannot();
    if (why) { set("unavailable", why); return; }
    if (state === "working") return;
    let sub = null;
    try { sub = await current(); } catch (e) {}
    if (sub) { set("on"); return; }
    if (Notification.permission === "denied") {
      set("refused", "notifications are blocked for this site. allow them in the browser's site settings, then turn this on");
      return;
    }
    // Asks the hub whether push is on, so the switch is not offered for something that cannot work.
    try {
      await window.mNet.api(KEY_URL, { headers: { Accept: "application/json" } });
      set("off");
    } catch (e) {
      if (e && e.status === 404) set("unavailable", "push is not turned on at this hub. the operator turns it on in the desktop gear");
      else set("off");
    }
  }

  async function subscribe() {
    const why = cannot();
    if (why) { set("unavailable", why); return; }
    set("working");
    let perm;
    try {
      // First, and inside the tap: a page cannot ask on its own, and a browser that was asked once and said no
      // answers "denied" at once, without a prompt.
      perm = await Notification.requestPermission();
    } catch (e) { perm = "denied"; }
    if (perm !== "granted") {
      set("refused", perm === "denied"
        ? "notifications are blocked for this site. allow them in the browser's site settings, then turn this on"
        : "permission was not given, so nothing was turned on");
      return;
    }
    let key;
    try {
      const r = await window.mNet.api(KEY_URL, { headers: { Accept: "application/json" } });
      key = r && r.key;
      if (!key) throw new Error("the hub sent no key");
    } catch (e) {
      set(e && e.status === 404 ? "unavailable" : "refused",
        e && e.status === 404 ? "push is not turned on at this hub. the operator turns it on in the desktop gear"
          : "could not reach the hub for its key: " + (e && e.message || e));
      return;
    }
    let sub;
    try {
      const reg = await registration();
      sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(key) });
    } catch (e) {
      set("refused", BRAVE_NOTE + (e && e.message ? " (" + e.message + ")" : ""));
      return;
    }
    const j = sub.toJSON();
    try {
      await window.mNet.api(SUBS_URL, { method: "POST", body: JSON.stringify({
        endpoint: j.endpoint, keys: j.keys, label: deviceLabel(), origin: location.origin }) });
    } catch (e) {
      // The hub said no, in its own words. The browser's half is undone so nothing is left subscribed that the hub
      // will never send to.
      try { await sub.unsubscribe(); } catch (e2) {}
      set("refused", (e && e.message) || "the hub refused this device");
      return;
    }
    set("on");
    waitForTestPush();
  }

  // The hub sends one test push on subscribe. If it has not arrived in 30 seconds the push service is not
  // delivering to this browser, and Brave's setting is the first suspect.
  function waitForTestPush() {
    seen = false;
    clearTimeout(seenTimer);
    seenTimer = setTimeout(() => {
      if (!seen && state === "on") {
        note = "on, but the test push did not arrive. in Brave, turn on Settings, Privacy, Use Google services for push messaging";
        paint();
      }
    }, SEEN_WAIT_MS);
  }

  async function unsubscribe() {
    set("working");
    let sub = null;
    try { sub = await current(); } catch (e) {}
    if (sub) {
      // The hub first, while the endpoint is still in hand: the endpoint is the proof this browser owns the row.
      try {
        await window.mNet.api(SUBS_URL, { method: "DELETE", body: JSON.stringify({ endpoint: sub.endpoint }) });
      } catch (e) {
        // A row the hub no longer has is as good as removed. Anything else is said, and the browser's half still goes,
        // so this phone stops listening whatever the hub does.
        if (!e || e.status !== 404) note = "this phone is off, but the hub did not confirm: " + ((e && e.message) || e);
      }
      try { await sub.unsubscribe(); } catch (e) {}
    }
    clearTimeout(seenTimer);
    const n = note;
    set("off");
    if (n) { note = n; paint(); }
  }

  function init() {
    const row = q("m-push");
    if (!row) return;
    els = { row, btn: q("m-push-btn"), note: q("m-push-note"), nameRow: q("m-push-name-row"), name: q("m-push-name") };
    els.name.addEventListener("change", rename);
    els.name.addEventListener("keydown", e => { if (e.key === "Enter") { e.preventDefault(); els.name.blur(); } });
    els.btn.addEventListener("click", () => { if (state === "on") unsubscribe(); else subscribe(); });
    const bell = q("m-bell");
    if (bell) bell.addEventListener("click", refresh);
    if ("serviceWorker" in navigator) {
      navigator.serviceWorker.addEventListener("message", e => {
        if (e.data && e.data.type === "push-seen") seen = true;
      });
    }
    paint();
    refresh();
  }

  window.mPush = { init, refresh, subscribe, unsubscribe, state: () => state, deviceLabel };
})();
