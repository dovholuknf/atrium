// Web push on the phone page, in a real browser: the subscribe control on /m, and the push handler in sw.js.
//
//   NODE_PATH=<dir with playwright> node scripts/check-web-push.js [shots-dir]
//
// It serves the real files of internal/api/web off an ephemeral localhost port, which is a secure context, and mocks
// the hub's three push routes (`GET /_hub/push/key`, `POST` and `DELETE /_hub/push/subscriptions`) and the endpoints
// the phone page reads. No atrium process is involved and no push service is reached: the browser's `PushManager`
// is replaced by a fake that hands back a subscription with an endpoint and two keys, because a headless browser has
// no push service to subscribe to. The service worker is the real file, and a push is delivered to it through the
// devtools protocol, the way a push service would.
//
// WEB=<dir> serves another copy of the web tree, which is how the "before" shot is taken from claude/main.
// With a shots dir it writes <prefix>-off.png, <prefix>-on.png and <prefix>-refused.png of the notifications sheet,
// where <prefix> is SHOT_PREFIX, "after" by default.

const http = require("http");
const fs = require("fs");
const path = require("path");

let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) {
  try { ({ chromium } = require("playwright")); } catch (e2) { console.log("playwright is not installed"); process.exit(0); }
}

const WEB = process.env.WEB || path.join(__dirname, "..", "internal", "api", "web");
const SHOTS = process.argv[2] || "";
const PREFIX = process.env.SHOT_PREFIX || "after";
const TYPES = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".png": "image/png",
  ".svg": "image/svg+xml", ".webmanifest": "application/manifest+json", ".gif": "image/gif", ".woff2": "font/woff2" };

const iso = msAgo => new Date(Date.now() - msAgo).toISOString();
const MIN = 60000;
const CARDS = [
  { id: "c1", status: "needs-permission", display_title: "u-028 composer", alias: "composer", wait_seconds: 240, rank: 1,
    waiting_since: iso(4 * MIN), why: "wants to run go test", tags: [], created_at: iso(3600000), last_activity_at: iso(MIN),
    supervised: true, seen: {}, activity: {} },
];

// What the hub's key route and the subscription routes answer, changed by a step.
let hub;
const reset = () => { hub = { key: "BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj6I5Fyt3tcvqpCM", keyStatus: 200,
  subStatus: 201, subError: "", posts: [], deletes: [] }; };
reset();

const readBody = req => new Promise(r => { let s = ""; req.on("data", c => { s += c; }); req.on("end", () => r(s)); });

const server = http.createServer(async (req, res) => {
  const u = new URL(req.url, "http://x");
  const p = u.pathname;
  const json = (o, code) => { res.writeHead(code || 200, { "Content-Type": "application/json" }); res.end(JSON.stringify(o)); };
  if (p === "/m" || p === "/m/") { res.writeHead(200, { "Content-Type": "text/html" }); return res.end(fs.readFileSync(path.join(WEB, "m", "index.html"))); }
  if ((p.startsWith("/vendor/") || p.startsWith("/m/") || p.startsWith("/css/") || p.startsWith("/js/") || p === "/sw.js" ||
    p === "/down.html" || p === "/working.gif") && !p.includes("..")) {
    const f = path.join(WEB, p);
    if (fs.existsSync(f) && fs.statSync(f).isFile()) {
      res.writeHead(200, { "Content-Type": TYPES[path.extname(f)] || "application/octet-stream", "Cache-Control": "no-store" });
      return res.end(fs.readFileSync(f));
    }
    res.writeHead(404); return res.end("");
  }
  if (p.startsWith("/v1/events")) {
    res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" });
    return res.write(": open\n\n");
  }
  const body = await readBody(req);
  if (p === "/_hub/push/key" && req.method === "GET") {
    return hub.keyStatus === 200 ? json({ key: hub.key }) : json({ error: "push is off" }, hub.keyStatus);
  }
  if (p === "/_hub/push/subscriptions" && req.method === "POST") {
    hub.posts.push(JSON.parse(body));
    return hub.subStatus < 300 ? json({ id: "s1" }, hub.subStatus) : json({ error: hub.subError }, hub.subStatus);
  }
  if (p === "/_hub/push/subscriptions" && req.method === "DELETE") {
    hub.deletes.push(JSON.parse(body));
    return json({ ok: true });
  }
  if (req.method !== "GET") return json({ ok: true });
  if (p === "/v1/tasks") return json({ tasks: CARDS });
  if (p === "/v1/permissions") return json({ permissions: [] });
  if (p === "/v1/health") return json({ build: "check", settling: false, halted: false });
  if (p === "/v1/settings") return json({ board_skin: "harbour", board_skins: ["harbour"] });
  if (p === "/_hub/rooms") return json({ error: "not a hub" }, 404);
  return json({});
});

let failed = false;
const bad = msg => { console.error("FAIL: " + msg); failed = true; };
const eq = (got, want, what) => { if (JSON.stringify(got) !== JSON.stringify(want)) bad(what + ": got " + JSON.stringify(got) + ", wanted " + JSON.stringify(want)); };

// The browser's push service, faked. Runs before the page's own scripts. `window.__push` is what a step sets.
function fakePush() {
  const mock = window.__push = { permission: "default", asked: 0, subscribeError: "", sub: null, unsubscribed: 0 };
  Object.defineProperty(Notification, "permission", { get: () => mock.permission, configurable: true });
  Notification.requestPermission = async () => {
    mock.asked++;
    if (mock.permission === "default") mock.permission = mock.grant || "granted";
    return mock.permission;
  };
  const b64 = s => btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  PushManager.prototype.getSubscription = async function () { return mock.sub; };
  PushManager.prototype.subscribe = async function (opts) {
    if (mock.subscribeError) throw new Error(mock.subscribeError);
    mock.options = { userVisibleOnly: opts.userVisibleOnly, keyLength: opts.applicationServerKey.length, keyFirst: opts.applicationServerKey[0] };
    const sub = mock.sub = {
      endpoint: "https://fcm.googleapis.com/fcm/send/fake-endpoint-1",
      toJSON() { return { endpoint: this.endpoint, keys: { p256dh: b64("fake-p256dh-key"), auth: b64("fake-auth") } }; },
      async unsubscribe() { mock.unsubscribed++; mock.sub = null; return true; },
    };
    return sub;
  };
}

// sw.js run in a vm against a fake worker scope. `shown` is every showNotification call, replaced by tag the way a
// browser replaces a notification, `clients` is what a tap finds, and `fire` runs a listener and waits for its
// waitUntil promises.
async function swChecks() {
  const vm = require("vm");
  const origin = "http://phone.example";
  const listeners = {};
  const shown = [];
  const opened = [];
  const messages = [];
  const windows = [];
  const fakeClient = (url, extra) => Object.assign({ url, focused: false, postMessage: m => messages.push(m),
    async navigate(u) { opened.push({ navigate: u }); return this; }, async focus() { opened.push({ focus: this.url }); } }, extra);
  const self = {
    location: { origin },
    addEventListener: (t, fn) => { listeners[t] = fn; },
    skipWaiting: async () => {},
    clients: { claim: async () => {}, matchAll: async () => windows,
      openWindow: async u => { opened.push({ openWindow: u }); } },
    registration: {
      getNotifications: async () => shown.slice(),
      showNotification: async (title, o) => {
        const i = shown.findIndex(n => n.tag === o.tag);
        const n = Object.assign({ title, close() {} }, o);
        if (i >= 0) shown.splice(i, 1);
        shown.push(n);
      },
    },
  };
  const ctx = vm.createContext({ self, caches: { open: async () => ({ add: async () => {} }), match: async () => null },
    Request: function () {}, Response: { error: () => ({}) }, fetch: async () => ({ ok: true }), URL, URLSearchParams, Date,
    setTimeout, Promise, JSON, String, Number, Math, console });
  vm.runInContext(fs.readFileSync(path.join(WEB, "sw.js"), "utf8"), ctx, { filename: "sw.js" });
  const fire = async (type, ev) => {
    const waits = [];
    ev.waitUntil = p => waits.push(p);
    listeners[type](ev);
    await Promise.all(waits);
  };
  const push = payload => fire("push", { data: payload === undefined ? null
    : { json: () => JSON.parse(payload) } });

  if (!listeners.push) { bad("sw.js has no push handler"); return; }

  windows.push(fakeClient(origin + "/m/"));
  await push(JSON.stringify({ title: "composer", body: "wants permission", tag: "alpha~c1", path: "/m/alias/composer" }));
  eq(shown.length, 1, "one notification for one push");
  const n = shown[0] || {};
  eq([n.title, n.body, n.tag], ["composer", "wants permission", "alpha~c1"], "the notification's text and tag");
  eq(n.data && n.data.path, "/m/alias/composer", "the tap's path");
  eq((n.actions || []).length, 0, "no action buttons on a push");
  eq(n.silent, undefined, "a push is not silent, so the phone buzzes");
  eq(messages, [{ type: "push-seen", tag: "alpha~c1" }], "an open phone page is told a push arrived");

  await push(JSON.stringify({ title: "composer", body: "asked you something", tag: "alpha~c1", path: "/m/alias/composer" }));
  eq(shown.length, 1, "the same tag replaces and does not stack");
  eq(shown[0] && shown[0].body, "asked you something", "the replacement is the newer text");

  // Each still shows, and none is trusted: a path off /m, a long title, a payload that is not JSON, and none at all.
  await push(JSON.stringify({ title: "x".repeat(200), body: "b", tag: "t2", path: "https://evil.example/m/" }));
  eq(((shown.find(x => x.tag === "t2") || {}).title || "").length, 60, "a long title is cut to 60");
  eq((shown.find(x => x.tag === "t2") || {}).data.path, "/m/", "an absolute address is not followed");
  for (const bad2 of ["//evil.example/m/x", "/m/..\\x", "/other/page", "/mx", "/m/\u0001"]) {
    await push(JSON.stringify({ title: "t", tag: "p" + bad2.length, path: bad2 }));
    const got = shown.find(x => x.tag === "p" + bad2.length);
    eq(got && got.data.path, "/m/", "the path " + JSON.stringify(bad2) + " is turned into /m/");
  }
  await fire("push", { data: { json: () => { throw new Error("not json"); } } });
  await fire("push", {});
  const generic = shown.find(x => x.tag === "atrium") || {};
  eq([generic.title, generic.body], ["atrium", "a card wants you"], "an unreadable payload still shows the generic line");

  // A tap: the phone window already open is taken to the card, and with none a new one opens.
  const click = path => fire("notificationclick", { notification: { data: { push: true, path, origin }, close() {} } });
  await click("/m/alias/composer");
  eq(opened, [{ navigate: origin + "/m/alias/composer" }, { focus: origin + "/m/" }], "a tap navigates the open /m window");
  opened.length = 0;
  windows.length = 0;
  windows.push(fakeClient(origin + "/"));
  await click("/m/room/alpha/composer");
  eq(opened, [{ openWindow: origin + "/m/room/alpha/composer" }], "with no /m window a tap opens one on the card");
  opened.length = 0;
  await click("https://evil.example/");
  eq(opened, [{ openWindow: origin + "/m/" }], "a tap never opens an address off /m");
}

(async () => {
  await new Promise(r => server.listen(0, "127.0.0.1", r));
  const base = "http://127.0.0.1:" + server.address().port;
  const browser = await chromium.launch();
  const view = { width: 390, height: 844 };

  // A fresh page with a seeded notification log, so the sheet is drawn the way it is with something in it.
  async function open(setup, ctxOpts) {
    reset();
    const ctx = await browser.newContext(Object.assign({ viewport: view, hasTouch: true, isMobile: true }, ctxOpts || {}));
    await ctx.addInitScript(fakePush);
    await ctx.addInitScript(() => {
      try {
        localStorage.setItem("atrium.toastlog", JSON.stringify([
          { sig: "a", title: "composer is waiting for you", body: "", goTo: "", taskFor: "c1", key: "", at: Date.now() - 4 * 60000, n: 1 }]));
        localStorage.setItem("atrium.toastlog.seen", String(Date.now()));
      } catch (e) {}
    });
    const page = await ctx.newPage();
    page.on("pageerror", e => bad("page error: " + e.message));
    if (process.env.DEBUG) page.on("console", m => console.log("page console: " + m.text()));
    if (setup) await setup(page);
    await page.goto(base + "/m/");
    await page.waitForFunction(() => window.mBell && (window.mPush ? document.getElementById("m-push") : true));
    await page.click("#m-bell");
    await page.waitForFunction(() => !document.getElementById("m-log").hidden);
    return { ctx, page };
  }
  const row = page => page.evaluate(() => {
    const r = document.getElementById("m-push");
    return { state: r.dataset.state, button: document.getElementById("m-push-btn").textContent.trim(),
      hiddenBtn: document.getElementById("m-push-btn").hidden, note: document.getElementById("m-push-note").textContent.trim() };
  });
  const settle = (page, state) => page.waitForFunction(s => document.getElementById("m-push").dataset.state === s, state, { timeout: 5000 })
    .catch(async () => bad("the row never reached " + state + ": " + JSON.stringify(await row(page))));
  const shot = async (page, name) => {
    if (!SHOTS) return;
    fs.mkdirSync(SHOTS, { recursive: true });
    await page.screenshot({ path: path.join(SHOTS, PREFIX + "-" + name + ".png") });
  };

  try {
    // ── the page before there is any control: the "before" shot is taken on a tree that has none ───────────────
    const has = fs.existsSync(path.join(WEB, "m", "js", "push.js"));
    if (!has) {
      const { ctx, page } = await open();
      await shot(page, "off");
      await ctx.close();
      console.log("this tree has no push control: wrote the before shot only");
      await browser.close(); server.close();
      return;
    }

    // ── off, then on: the tap asks permission, reads the key, subscribes, and tells the hub ─────────────────────
    {
      const { ctx, page } = await open();
      await settle(page, "off");
      const r0 = await row(page);
      eq(r0.button, "turn on", "the switch while off");
      await shot(page, "off");
      await page.tap("#m-push-btn");
      await settle(page, "on");
      const m = await page.evaluate(() => window.__push);
      eq(m.asked, 1, "permission asked once, from the tap");
      eq(m.options.userVisibleOnly, true, "userVisibleOnly");
      eq(m.options.keyLength, 65, "the application server key is the 65 byte P-256 point");
      eq(m.options.keyFirst, 4, "the key is an uncompressed point");
      eq(hub.posts.length, 1, "one subscription posted");
      const sent = hub.posts[0] || {};
      eq(sent.endpoint, "https://fcm.googleapis.com/fcm/send/fake-endpoint-1", "the endpoint posted");
      eq(Object.keys(sent.keys || {}).sort(), ["auth", "p256dh"], "the keys posted");
      if (!sent.label) bad("no device label posted");
      eq((await row(page)).button, "turn off", "the switch while on");
      await shot(page, "on");

      // ── off again: the hub is told with the endpoint as proof, and the browser unsubscribes ──────────────────
      await page.tap("#m-push-btn");
      await settle(page, "off");
      eq(hub.deletes, [{ endpoint: "https://fcm.googleapis.com/fcm/send/fake-endpoint-1" }], "the delete carries the endpoint");
      eq((await page.evaluate(() => window.__push.unsubscribed)), 1, "the browser unsubscribed");
      await ctx.close();
    }

    // ── a subscription that is already there reads as on when the sheet opens ────────────────────────────────────
    {
      const { ctx, page } = await open(async p => p.addInitScript(() => {
        const m = window.__push;
        m.permission = "granted";
        m.sub = { endpoint: "https://fcm.googleapis.com/fcm/send/old", toJSON() { return { endpoint: this.endpoint, keys: {} }; },
          async unsubscribed() {}, async unsubscribe() { m.unsubscribed++; m.sub = null; return true; } };
      }));
      // The fake is installed by the context's init script, so a page init script that runs after it can set state.
      // A subscription lives under a registration, which the phone page makes when it subscribes.
      await page.evaluate(async () => { await navigator.serviceWorker.register("/sw.js"); await navigator.serviceWorker.ready; });
      await page.reload();
      await page.click("#m-bell");
      await settle(page, "on");
      await ctx.close();
    }

    // ── refused: permission denied, with nothing asked of the hub ────────────────────────────────────────────────
    {
      const { ctx, page } = await open();
      await page.evaluate(() => { window.__push.permission = "default"; window.__push.grant = "denied"; });
      await page.tap("#m-push-btn");
      await settle(page, "refused");
      const r = await row(page);
      if (!/blocked for this site/.test(r.note)) bad("a denied permission does not say so: " + r.note);
      eq(hub.posts.length, 0, "nothing posted when permission is denied");
      await shot(page, "refused");
      await ctx.close();
    }

    // ── refused by the browser: no subscribe, and Brave's setting is named ───────────────────────────────────────
    {
      const { ctx, page } = await open();
      await page.evaluate(() => { window.__push.subscribeError = "Registration failed - push service error"; });
      await page.tap("#m-push-btn");
      await settle(page, "refused");
      const r = await row(page);
      if (!/Use Google services for push messaging/.test(r.note)) bad("a failed subscribe does not name Brave's setting: " + r.note);
      eq(hub.posts.length, 0, "nothing posted when the browser will not subscribe");
      await ctx.close();
    }

    // ── refused by the hub: its sentence is shown whole, and the browser's half is undone ───────────────────────
    {
      const { ctx, page } = await open();
      hub.subStatus = 409;
      hub.subError = "8 devices already get alerts. Remove one in the desktop gear first.";
      await page.tap("#m-push-btn");
      await settle(page, "refused");
      eq((await row(page)).note, hub.subError, "the hub's refusal, word for word");
      eq((await page.evaluate(() => window.__push.unsubscribed)), 1, "the browser's subscription is dropped when the hub refuses");
      await ctx.close();
    }

    // ── the hub has push off: the switch is not offered, and the row says why ───────────────────────────────────
    {
      reset();
      const ctx = await browser.newContext({ viewport: view, hasTouch: true, isMobile: true });
      await ctx.addInitScript(fakePush);
      const page = await ctx.newPage();
      hub.keyStatus = 404;
      await page.goto(base + "/m/");
      await page.waitForFunction(() => window.mPush);
      await settle(page, "unavailable");
      const r = await row(page);
      if (!r.hiddenBtn) bad("the switch is offered while the hub has push off");
      if (!/not turned on at this hub/.test(r.note)) bad("no reason on the row: " + r.note);
      await ctx.close();
    }

    // ── an iPhone tab: no push, and the home-screen route is spelled out ─────────────────────────────────────────
    {
      const { ctx, page } = await open(null, { userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1" });
      await settle(page, "unavailable");
      if (!/Add to Home Screen/.test((await row(page)).note)) bad("an iPhone tab is not told about the home screen");
      await ctx.close();
    }

    // ── the service worker's own code, run in a stand-in for its scope: a push shows a notification, and a tap
    //    opens the card's /m path. Headless Chromium denies notification permission to a worker whatever the
    //    context grants, so the file is run against a fake `self` and the notifications it raises are read back.
    await swChecks();
  } catch (e) {
    bad("the run threw: " + (e && e.stack || e));
  }
  await browser.close();
  server.close();
  if (failed) { console.error("web push check FAILED"); process.exit(1); }
  console.log("web push check passed");
})();
