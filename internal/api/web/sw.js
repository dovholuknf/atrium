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
self.addEventListener("message", event => {
  const m = event.data || {}
  if (m.type !== "notify") return
  event.waitUntil(self.registration.showNotification(m.title, {
    body: m.body || "",
    icon: m.icon,
    badge: m.icon,
    tag: m.tag || "atrium",
    renotify: true,
    requireInteraction: !!m.sticky,
    silent: true,
    data: { permId: m.permId || "", goTo: m.goTo || "", origin: m.origin || self.location.origin },
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
  if (!res.ok) throw new Error(`decide failed: ${res.status}`)
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
    event.waitUntil(
      decide(origin, data.permId, event.action).catch(err =>
        // The request may have been answered elsewhere already, or the daemon
        // may be down. Say so rather than failing silently.
        self.registration.showNotification("atrium could not answer that", {
          body: String(err.message || err),
          icon: data.icon,
          tag: "atrium-error"
        })))
    return
  }
  event.waitUntil(openBoard(origin, data.goTo))
})
