// Service worker for atrium.
//
// A plain `new Notification(...)` cannot carry buttons. Only a notification
// shown through a service worker registration can, and only the worker can act
// on a press, so the buttons work with no atrium tab open.
//
// Chrome on Windows renders at most two action buttons: approve and block.
// Editing the command or setting a rule needs the page, so clicking the body
// opens it.

self.addEventListener("install", () => self.skipWaiting())
// Activation claims open pages, and sweeps: a worker killed with notifications
// on screen leaves overdue ones behind.
self.addEventListener("activate", event => event.waitUntil(
  Promise.all([self.clients.claim(), sweepExpired()])))

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

// Bring an existing tab forward rather than piling up new ones.
async function openBoard(origin, goTo) {
  const all = await self.clients.matchAll({ type: "window", includeUncontrolled: true })
  const mine = all.find(c => c.url.startsWith(origin))
  if (mine) {
    await mine.focus()
    mine.postMessage({ type: "goTo", view: goTo || "perms" })
    return
  }
  await self.clients.openWindow(origin + "/")
}

self.addEventListener("notificationclick", event => {
  const data = event.notification.data || {}
  const origin = data.origin || self.location.origin
  event.notification.close()
  event.waitUntil(sweepExpired())

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
  event.waitUntil(openBoard(origin, data.goTo))
})
