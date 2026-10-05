// Service worker for atrium.
//
// A plain `new Notification(...)` cannot carry buttons. Only a notification
// shown through a service worker registration can, and only the worker can act
// on a press, so the buttons work with no atrium tab open.
//
// Chrome on Windows renders at most two action buttons: approve and block.
// Editing the command or setting a rule needs the page, so clicking the body
// opens it.

// THE PAGE FOR WHEN ATRIUM IS DOWN. A reload or a fresh visit with atrium not
// running got the browser's own "can't reach this page", which says nothing
// about atrium and never comes back by itself. So the worker keeps `down.html`
// and answers with it when opening a page fails. That page stands on its own and
// reloads onto the board once atrium answers. See js/down.js for the open board.
const DOWN_CACHE = "atrium-down"
const DOWN_PAGE = "/down.html"

// Fetched past the HTTP cache, so the copy kept is the one atrium serves now.
function keepDownPage() {
  return caches.open(DOWN_CACHE)
    .then(c => c.add(new Request(DOWN_PAGE, { cache: "reload" })))
    .catch(() => {})
}

self.addEventListener("install", event => event.waitUntil(keepDownPage().then(() => self.skipWaiting())))
// Activation claims open pages, and sweeps: a worker killed with notifications
// on screen leaves overdue ones behind.
self.addEventListener("activate", event => event.waitUntil(
  Promise.all([self.clients.claim(), sweepExpired()])))

// Only page loads, and only when they fail. Everything else is left to the
// browser, and a page that loads is passed through untouched. A load that works
// refreshes the kept copy, so a new build's `down.html` replaces the old one
// without waiting for this file to change.
//
// A 502 or 504 is a failure too: that is a proxy in front of atrium, a share or
// an overlay, saying atrium behind it did not answer.
self.addEventListener("fetch", event => {
  if (event.request.mode !== "navigate") return
  const down = async () => (await caches.match(DOWN_PAGE, { cacheName: DOWN_CACHE })) || Response.error()
  event.respondWith(fetch(event.request).then(res => {
    if (res.status === 502 || res.status === 504) return down()
    event.waitUntil(keepDownPage())
    return res
  }, down))
})

// The page hands over what to show, since the worker has no view of state.
const sleep = ms => new Promise(r => setTimeout(r, ms))

// A service worker can be killed at any time, and `waitUntil` only delays that.
// A worker killed mid-sleep takes its timer with it, and on Windows the
// notification then stays in the action centre until dismissed by hand.
//
// So each notification carries the wall-clock time it should be gone by, and
// every event that wakes the worker sweeps whatever is overdue: a new
// notification, a click, activation.

// sweepExpired closes every notification whose time has passed.
async function sweepExpired() {
  const now = Date.now()
  const list = await self.registration.getNotifications()
  for (const n of list) {
    const at = n.data && n.data.expireAt
    if (at && now >= at) n.close()
  }
}

// expireLater closes on time when the worker survives that long, rather than
// waiting for the next event to sweep.
async function expireLater(ms) {
  await sleep(ms)
  await sweepExpired()
}

// expire closes ONE notification by tag, for the ones nothing else retires.
//
// The sweep above only knows about `expireAt`, which the page sets and the
// page's own poll clears. A notification the worker raises by itself has
// neither: no page put it there and no poll will take it away. There is
// exactly one of those, the "too late" below, and it used to call a function
// by this name that was never written. So the failure was: press approve on a
// notification for a request somebody had already answered, and instead of
// being told you were too late, get a ReferenceError inside a service worker
// and nothing on screen at all. Which is the same silence the "too late"
// message exists to prevent, reached by a different route.
async function expire(tag, ms) {
  await sleep(ms)
  const list = await self.registration.getNotifications({ tag })
  for (const n of list) n.close()
}

self.addEventListener("message", event => {
  const m = event.data || {}
  if (m.type !== "notify") return
  event.waitUntil(sweepExpired())

  const expireMs = Number(m.expireMs) || 0
  if (expireMs > 0) event.waitUntil(expireLater(expireMs))
  event.waitUntil(self.registration.showNotification(m.title, {
    body: m.body || "",
    icon: m.icon,
    badge: m.icon,
    tag: m.tag || "atrium",
    renotify: true,
    requireInteraction: !!m.sticky,
    silent: true,
    data: {
      permId: m.permId || "", goTo: m.goTo || "",
      // Where a click on the body lands: the card, and the request on it. See
      // `landOnAlert` in js/toasts.js.
      taskFor: m.taskFor || "", key: m.key || "",
      // The card's readable path, so a window on it is found. See `soloCard`.
      path: m.path || "",
      // What this notification is about: a request, or the card that went
      // ready. Carried so an open page can take it down once that has been
      // answered, which permId alone could not do for anything but a
      // permission.
      subject: m.subject || m.permId || "",
      origin: m.origin || self.location.origin, icon: m.icon,
      // Wall clock, not a duration, so a sweep can decide correctly no matter
      // how long the worker was dead in between.
      expireAt: expireMs > 0 ? Date.now() + expireMs : 0
    },
    actions: m.permId
      // "once", matching the board, since neither button sets a standing rule.
      // A rule needs the scope line, which only the page has.
      ? [{ action: "approve", title: "approve once" }, { action: "block", title: "block once" }]
      : []
  }))
})

async function decide(origin, permId, decision) {
  const res = await fetch(`${origin}/v1/permissions/${permId}/decide`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      decision,
      reason: decision === "block" ? "blocked from a notification" : ""
    })
  })
  if (res.ok) return
  // A request answered elsewhere comes back as a conflict carrying what the
  // answer was. Pressing a button and seeing nothing happen is worse than
  // being told you are too late.
  let msg = `could not answer that: ${res.status}`
  try {
    const body = await res.json()
    if (body && body.error) msg = body.error
  } catch (e) {}
  throw new Error(msg)
}

// The bare card id, as js/notify.js `bareId` reads it: a `room~` tag is how
// the hub spells a card while more than one room is attached.
const bareId = id => { const s = String(id || ""); const i = s.indexOf("~"); return i > 0 ? s.slice(i + 1) : s }

// The card a popped-out window is showing, off its `#term=` address.
function soloCard(url) {
  const i = url.indexOf("#term=")
  return i < 0 ? "" : decodeURIComponent(url.slice(i + "#term=".length))
}

// The path of a window's address, where a card's readable path lives. See js/cardurl.js.
function pathOf(url) {
  try { return new URL(url).pathname.replace(/\/$/, "") } catch (e) { return "" }
}

// Whether a window is one terminal: a `#term=` address or a card's readable path.
// `/room/<room>` alone is a scoped board.
function isSolo(url) {
  if (soloCard(url)) return true
  const p = pathOf(url).replace(/\/$/, "").split("/")
  return (p[1] === "alias" && p.length === 3) || (p[1] === "room" && p.length === 4)
}

// Bring an existing window forward rather than piling up new ones, and land
// the click where `landOnAlert` says.
//
// A card popped out into a window of its own lands on that window: only a
// click can bring a window forward, and this is the click. Otherwise a board,
// never a popped-out window, since one of those cannot become a board. With no
// board open, one is opened carrying where to land.
async function openBoard(origin, goTo, taskFor, key, path) {
  const all = await self.clients.matchAll({ type: "window", includeUncontrolled: true })
  const mine = all.filter(c => c.url.startsWith(origin))
  const solo = taskFor && mine.find(c => (soloCard(c.url) && bareId(soloCard(c.url)) === bareId(taskFor)) ||
    (path && isSolo(c.url) && pathOf(c.url) === path.replace(/\/$/, "")))
  if (solo) {
    await solo.focus()
    return
  }
  const board = mine.find(c => !isSolo(c.url))
  if (board) {
    await board.focus()
    board.postMessage({ type: "goTo", view: goTo || "", taskFor: taskFor || "", key: key || "" })
    return
  }
  const q = new URLSearchParams()
  if (taskFor) q.set("land", taskFor)
  if (goTo) q.set("view", goTo)
  if (key) q.set("key", key)
  const s = q.toString()
  await self.clients.openWindow(origin + "/" + (s ? "?" + s : ""))
}

// ── web push, for a phone with no atrium page open ───────────────────────────
//
// The hub sends one small JSON payload, four plain fields and no more: `title` is the card's name, `body` is one phrase
// for why it wants you, `tag` is the card id so a newer alert for the same card replaces the older, and `path` is the
// card's `/m` path. Never the command a permission wants to run, a question or a recap. See docs/rnd/web-push-design.md.
//
// EVERY PUSH SHOWS A NOTIFICATION. iOS revokes a subscription that receives a push and shows nothing, so a payload
// that cannot be read still shows the generic line. The browser has already decrypted the payload by the time this
// runs, and `userVisibleOnly` is what promised the browser this.
//
// NO ACTION BUTTONS. An approval from a lock screen would be given without seeing the command, which the payload does
// not carry and must not. A tap opens the card on `/m` and the answer is given there.
const PUSH_ICON = "/m/icons/icon-192.png"
const PUSH_TITLE_MAX = 60

// A path the worker will open from a push: the phone page and nothing else. A payload is the hub's, but what this
// opens is a window on this origin, so it is held to the one place a push is for. No scheme, no host, no `//`.
function pushPath(p) {
  const s = String(p || "")
  if (!/^\/m(\/|$)/.test(s) || s.startsWith("//") || s.includes("\\") || /[\u0000-\u001f]/.test(s)) return "/m/"
  return s
}

// What the payload says, read defensively: a notification renders no HTML, so everything here is plain text.
function readPush(event) {
  let p = {}
  try { p = event.data ? event.data.json() : {} } catch (e) { p = {} }
  if (!p || typeof p !== "object") p = {}
  const text = (v, max) => String(v == null ? "" : v).slice(0, max)
  return {
    title: text(p.title, PUSH_TITLE_MAX) || "atrium",
    body: text(p.body, 200) || "a card wants you",
    tag: text(p.tag, 128) || "atrium",
    path: pushPath(p.path)
  }
}

self.addEventListener("push", event => {
  const m = readPush(event)
  event.waitUntil((async () => {
    // Tells an open phone page a push arrived, which is how its "did the test push come" check is answered.
    const all = await self.clients.matchAll({ type: "window", includeUncontrolled: true })
    for (const c of all) c.postMessage({ type: "push-seen", tag: m.tag })
    await self.registration.showNotification(m.title, {
      body: m.body,
      icon: PUSH_ICON,
      badge: PUSH_ICON,
      tag: m.tag,
      // A newer alert for the same card replaces the older and buzzes again.
      renotify: true,
      data: { push: true, path: m.path, origin: self.location.origin }
    })
  })())
})

// The browser rotated or dropped the subscription behind the page's back. Subscribe again with the same key and hand
// the hub the new one, so a phone does not go quiet without anybody being told. It cannot ask for permission, and a
// failure here is left for the page to find the next time it opens.
self.addEventListener("pushsubscriptionchange", event => {
  event.waitUntil((async () => {
    const old = event.oldSubscription
    const key = (old && old.options && old.options.applicationServerKey) ||
      (event.newSubscription && event.newSubscription.options && event.newSubscription.options.applicationServerKey)
    const sub = event.newSubscription ||
      (key && await self.registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key }))
    if (!sub) return
    const j = sub.toJSON()
    await fetch("/_hub/push/subscriptions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ endpoint: j.endpoint, keys: j.keys, label: "this phone, renewed" })
    })
    if (old && old.endpoint && old.endpoint !== j.endpoint) {
      await fetch("/_hub/push/subscriptions", {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ endpoint: old.endpoint })
      })
    }
  })().catch(() => {}))
})

// A tap on a push opens the card on the phone page: the window already on `/m` is taken there, else a new one opens.
async function openPushPath(origin, path) {
  const url = origin + pushPath(path)
  const all = await self.clients.matchAll({ type: "window", includeUncontrolled: true })
  const mine = all.filter(c => c.url.startsWith(origin + "/m"))
  const here = mine.find(c => c.focused) || mine[0]
  if (here) {
    try {
      const c = await here.navigate(url)
      if (c) await c.focus()
      else await here.focus()
      return
    } catch (e) {
      // An uncontrolled window cannot be navigated. Fall through to a new one.
    }
  }
  await self.clients.openWindow(url)
}

self.addEventListener("notificationclick", event => {
  const data = event.notification.data || {}
  const origin = data.origin || self.location.origin
  event.notification.close()
  event.waitUntil(sweepExpired())

  if (data.push) {
    event.waitUntil(openPushPath(origin, data.path))
    return
  }

  if (event.action && data.permId) {
    const tag = "atrium-conflict-" + data.permId
    event.waitUntil(
      decide(origin, data.permId, event.action).catch(async err => {
        // The request may have been answered elsewhere already, or the daemon
        // may be down. Say so rather than failing silently.
        await self.registration.showNotification("atrium: too late", {
          body: String(err.message || err),
          icon: data.icon,
          badge: data.icon,
          tag,
          renotify: true
        })
        // Nothing else clears this one, so it clears itself.
        await expire(tag, 10000)
      }))
    return
  }
  event.waitUntil(openBoard(origin, data.goTo, data.taskFor, data.key || data.permId, data.path))
})
