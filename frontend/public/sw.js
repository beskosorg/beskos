/* Beskos service worker: receives Web Push and shows the notification even with every tab
   closed. Kept minimal on purpose — push and click only. */

// Activates immediately (without waiting for old tabs to close) and takes control — so
// registration.showNotification() is usable right after the first registration.
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => event.waitUntil(self.clients.claim()));

self.addEventListener("push", (event) => {
  let data = {};
  try { data = event.data ? event.data.json() : {}; } catch { data = {}; }
  const title = data.title || "Beskos";
  const options = {
    body: data.body || "",
    icon: "/favicon.svg",
    badge: "/favicon.svg",
    tag: (data.kind || "mailin") + ":" + (data.link || ""),
    data: { link: data.link || "/mail" },
  };
  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const link = (event.notification.data && event.notification.data.link) || "/mail";
  event.waitUntil((async () => {
    const all = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    // Is a Beskos tab already open? Focus it and tell it to navigate, instead of opening another.
    for (const c of all) {
      if (c.url.includes(self.location.origin)) {
        await c.focus();
        c.postMessage({ type: "notif-click", link });
        return;
      }
    }
    await self.clients.openWindow(link);
  })());
});
