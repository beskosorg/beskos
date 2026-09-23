import { backend } from "./backend";

// Client-side Web Push: registers the service worker, subscribes to the browser's push service with
// the server's public VAPID key, and sends the subscription to the backend. Best-effort — if the
// server does not have push configured (VAPID disabled), it does nothing and open-tab notifications suffice.

export function pushSupported(): boolean {
  return "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
}

// The VAPID key comes in base64url; PushManager wants a Uint8Array.
function urlB64ToUint8Array(base64: string): Uint8Array {
  const padding = "=".repeat((4 - (base64.length % 4)) % 4);
  const b64 = (base64 + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(b64);
  const buf = new ArrayBuffer(raw.length);
  const out = new Uint8Array(buf);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

async function registerSW(): Promise<ServiceWorkerRegistration | null> {
  if (!pushSupported()) return null;
  try { return await navigator.serviceWorker.register("/sw.js"); }
  catch { return null; }
}

// Subscribes (idempotent): reuses the browser's existing subscription if there already is one. Only
// proceeds if the server has VAPID enabled. Returns true if it ended up subscribed.
export async function enableWebPush(): Promise<boolean> {
  if (!pushSupported() || Notification.permission !== "granted") return false;
  let vapid: { enabled: boolean; publicKey?: string };
  try { vapid = await backend.pushVapid(); } catch { return false; }
  if (!vapid.enabled || !vapid.publicKey) return false;

  const reg = await registerSW();
  if (!reg) return false;
  try {
    const existing = await reg.pushManager.getSubscription();
    const sub = existing ?? await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlB64ToUint8Array(vapid.publicKey) as BufferSource,
    });
    await backend.pushSubscribe(sub.toJSON());
    return true;
  } catch { return false; }
}

// Cancels the subscription on this browser (and tells the server to delete the row).
export async function disableWebPush(): Promise<void> {
  if (!pushSupported()) return;
  try {
    const reg = await navigator.serviceWorker.getRegistration();
    const sub = await reg?.pushManager.getSubscription();
    if (sub) {
      await backend.pushUnsubscribe(sub.endpoint).catch(() => { /* proceed anyway */ });
      await sub.unsubscribe();
    }
  } catch { /* nothing to do */ }
}

// Forwards a notification click (coming from the service worker) to the app's navigation.
export function onNotificationClick(navigate: (link: string) => void): () => void {
  if (!("serviceWorker" in navigator)) return () => {};
  const handler = (e: MessageEvent) => {
    if (e.data?.type === "notif-click" && e.data.link) navigate(e.data.link);
  };
  navigator.serviceWorker.addEventListener("message", handler);
  return () => navigator.serviceWorker.removeEventListener("message", handler);
}
