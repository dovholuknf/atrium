// Service worker for atrium.
//
// It exists for one reason: a plain `new Notification(...)` cannot carry
// buttons. Only a notification shown through a service worker registration can,
// and only the worker can act on a button press. That also means the buttons
// still work when no atrium tab is open at all, because the worker runs without
// one.
//
// Chrome on Windows renders at most two action buttons, so the pair is
// approve and block. Anything richer, editing the command or setting a rule,
// needs the page, so clicking the notification body opens it.

self.addEventListener("install", () => self.skipWaiting())
self.addEventListener("activate", event => event.waitUntil(self.clients.claim()))

// The page hands over what to show, since the worker has no view of state.
const sleep = ms => new Promise(r => setTimeout(r, ms))

// Takes a notification down once its time is up. waitUntil keeps the worker
// alive while this runs, but a browser may still shut the worker down first,
// so the page reaps expired notifications as well.
async function expire(tag, ms) {
  await sleep(ms)
  const list = await self.registration.getNotifications({ tag })
  list.forEach(n => n.close())
}

self.addEventListener("message", event => {
  const m = event.data || {}
  if (m.type !== "notify") return
  const expireMs = Number(m.expireMs) || 0
  if (expireMs > 0) event.waitUntil(expire(m.tag || "atrium", expireMs))
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
      origin: m.origin || self.location.origin, icon: m.icon
    },
    actions: m.permId
      ? [{ action: "approve", title: "approve" }, { action: "block", title: "block" }]
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
