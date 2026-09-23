// Adapter for Tauri's NATIVE notifications (desktop/mobile). macOS's WKWebView does not support
// the Web Notifications API — so, in the packaged app, web notifications do not work and the app
// does not even show up in the OS notification settings. Tauri's native plugin solves both: it
// uses the OS notification center and registers the app's bundle there.
//
// Dynamic import: the `@tauri-apps/plugin-notification` package only exists/works inside Tauri;
// on the web it is never loaded (isTauri() is false), so the web build has no runtime dependency on it.

// isTauri detects the native runtime. Tauri 2 injects window.isTauri; __TAURI_INTERNALS__ is the backup.
export function isTauri(): boolean {
  if (typeof window === "undefined") return false;
  const w = window as unknown as { isTauri?: boolean; __TAURI_INTERNALS__?: unknown };
  return w.isTauri === true || typeof w.__TAURI_INTERNALS__ !== "undefined";
}

// Synchronous cache of the permission: the UI (SettingsView) reads the state synchronously, but
// the plugin is async. We initialize it at startup (initTauriNotify) and update it after requesting permission.
let cachedPerm: NotificationPermission = "default";
export function tauriPermission(): NotificationPermission { return cachedPerm; }

type NotifPlugin = typeof import("@tauri-apps/plugin-notification");
async function plugin(): Promise<NotifPlugin> {
  return import("@tauri-apps/plugin-notification");
}

// initTauriNotify reads the current permission into the cache (call once at startup, inside Tauri).
export async function initTauriNotify(): Promise<void> {
  try {
    const p = await plugin();
    cachedPerm = (await p.isPermissionGranted()) ? "granted" : "default";
  } catch { /* outside Tauri or plugin unavailable */ }
}

export async function tauriRequestPermission(): Promise<NotificationPermission> {
  try {
    const p = await plugin();
    if (await p.isPermissionGranted()) { cachedPerm = "granted"; return "granted"; }
    const res = await p.requestPermission(); // "granted" | "denied" | "default"
    cachedPerm = res === "granted" ? "granted" : "denied";
    return cachedPerm;
  } catch { return "denied"; }
}

// onTauriEvent listens for an event emitted by the native side (e.g. "window-hidden", when closing
// the window hides it in the tray). On the web it is a no-op. Returns the function that cancels the
// listener — the registration is asynchronous, so we track "cancelled" to avoid orphaned listeners.
export function onTauriEvent(name: string, fn: () => void): () => void {
  if (!isTauri()) return () => {};
  let off: (() => void) | null = null;
  let cancelled = false;
  void (async () => {
    try {
      const { listen } = await import("@tauri-apps/api/event");
      const un = await listen(name, () => fn());
      if (cancelled) un(); else off = un;
    } catch { /* API unavailable: ignore */ }
  })();
  return () => { cancelled = true; off?.(); };
}

// Shows the native notification. `link` is the route to open when the person clicks the alert.
//
// The normal path is the `notify_show` (Rust) command: it is the only one that knows how to wait
// for the click and hand it back to the app — the notification plugin delivers the alert and
// forgets about it on desktop. If the command does not exist (old app version, or mobile, where
// the module is desktop-only), it falls back to the plugin: the notification still shows up, it
// just does not open anything when clicked.
export async function tauriShow(title: string, body?: string, link?: string): Promise<boolean> {
  try {
    const p = await plugin();
    if (!(await p.isPermissionGranted())) return false;
    try {
      const { invoke } = await import("@tauri-apps/api/core");
      await invoke("notify_show", { title, body: body ?? "", link: link ?? "" });
      return true;
    } catch { /* no native command: fall through to the plugin */ }
    p.sendNotification({ title, body });
    return true;
  } catch { return false; }
}

// onNotifOpen listens for a click on a native notification (`notif-open` event, emitted by Rust
// with the link) and hands the link to the navigator. On the web it is a no-op — there the click
// arrives through the service worker.
export function onNotifOpen(fn: (link: string) => void): () => void {
  if (!isTauri()) return () => {};
  let off: (() => void) | null = null;
  let cancelled = false;
  void (async () => {
    try {
      const { listen } = await import("@tauri-apps/api/event");
      const un = await listen<string>("notif-open", (e) => { if (e.payload) fn(e.payload); });
      if (cancelled) un(); else off = un;
    } catch { /* API unavailable: ignore */ }
  })();
  return () => { cancelled = true; off?.(); };
}
