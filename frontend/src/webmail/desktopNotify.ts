// Desktop notifications (Web Notifications API): fire while a Beskos tab is open.
// Permission is per-browser, so preferences live on the device (localStorage), not in the
// synced settings — enabling it on the laptop shouldn't enable it on the phone.
//
// Noise rule: only notify when the tab is NOT visible (document.hidden). If the person is
// looking at Beskos, they already see what arrives — an OS notification would be redundant.

import { isTauri, onNotifOpen, tauriPermission, tauriRequestPermission, tauriShow } from "./tauriNotify";

export type NotifDomain = "email" | "chat" | "meet" | "cal" | "files" | "tasks";
const DOMAINS: NotifDomain[] = ["email", "chat", "meet", "cal", "files", "tasks"];
const LS_PREFS = "mailin.notif.prefs";

export function notifSupported(): boolean {
  if (isTauri()) return true; // native app: uses the OS plugin (WKWebView has no Web Notifications)
  return typeof window !== "undefined" && "Notification" in window;
}

export function notifPermission(): NotificationPermission {
  if (isTauri()) return tauriPermission();
  return typeof window !== "undefined" && "Notification" in window ? Notification.permission : "denied";
}

// Requests permission. Must be called from a user gesture (click).
export async function requestNotifPermission(): Promise<NotificationPermission> {
  if (isTauri()) return tauriRequestPermission();
  if (!("Notification" in window)) return "denied";
  try { return await Notification.requestPermission(); }
  catch { return "denied"; }
}

export function notifPrefs(): Record<NotifDomain, boolean> {
  const def = { email: true, chat: true, meet: true, cal: true, files: true, tasks: true };
  try {
    const raw = localStorage.getItem(LS_PREFS);
    if (!raw) return def;
    return { ...def, ...JSON.parse(raw) };
  } catch { return def; }
}

export function setNotifPref(domain: NotifDomain, on: boolean): void {
  try { localStorage.setItem(LS_PREFS, JSON.stringify({ ...notifPrefs(), [domain]: on })); }
  catch { /* private mode: stays only in the current browser session */ }
}

export function isDomain(v: string): v is NotifDomain {
  return (DOMAINS as string[]).includes(v);
}

// Shows a notification, preferring the SERVICE WORKER (registration.showNotification), which is
// the reliable path on Chrome/Edge/Firefox desktop — the `new Notification()` constructor is
// deprecated and missing in several browsers. Falls back to the constructor only if there is no SW.
async function show(title: string, body?: string, tag?: string, link?: string): Promise<boolean> {
  if (notifPermission() !== "granted") return false;
  if (isTauri()) return tauriShow(title, body, link); // native OS notification (desktop/mobile)
  // `data.link`: this is where the service worker gets where to navigate on click (same contract
  // as push). Without this, the click always landed on the inbox, whatever the notification was.
  const opts: NotificationOptions = { body, tag, icon: "/beskos-mark.svg", badge: "/beskos-mark.svg", data: { link: link || "/mail" } };
  // Preferred path: an ACTIVE service worker. Right after register(), the SW is still installing
  // and reg.showNotification() fails silently — so we wait for serviceWorker.ready (with a short
  // timeout so it doesn't hang) before using the registration.
  if ("serviceWorker" in navigator) {
    try {
      if (!(await navigator.serviceWorker.getRegistration())) await navigator.serviceWorker.register("/sw.js");
      const reg = await Promise.race<ServiceWorkerRegistration | undefined>([
        navigator.serviceWorker.ready,
        new Promise((res) => setTimeout(() => res(undefined), 1500)),
      ]);
      if (reg?.showNotification) { await reg.showNotification(title, opts); return true; }
    } catch { /* falls back to the constructor below */ }
  }
  // Fallback: the Notification constructor (less reliable, but good enough in several browsers).
  // Here the click reaches this tab (not the service worker), so we navigate ourselves.
  try {
    const n = new Notification(title, opts);
    n.onclick = () => { window.focus(); n.close(); if (link) navigateTo?.(link); };
    return true;
  } catch { return false; }
}

// How to navigate when the click is handled IN THIS TAB (Notification constructor fallback). The
// App registers the router's `navigate` — without this we'd have to reload the whole page.
let navigateTo: ((link: string) => void) | null = null;
export function setNotifNavigate(fn: ((link: string) => void) | null): void {
  navigateTo = fn;
}

// onDesktopNotifClick wires a click on a NATIVE notification (desktop app) to the app's navigation.
export { onNotifOpen as onDesktopNotifClick };

// TEST notification — ignores the visibility rule and the per-app preferences. If there is no
// permission yet, requests it first (the button click is the gesture the browser requires).
export async function testNotification(title: string, body: string): Promise<boolean> {
  if (!notifSupported()) return false;
  if (notifPermission() !== "granted") {
    const p = await requestNotifPermission();
    if (p !== "granted") return false;
  }
  return show(title, body, "mailin-test");
}

// Shows an OS notification if: the browser supports it, permission is granted, the domain is
// enabled, and the tab isn't visible. `tag` deduplicates (one notification per conversation/event
// instead of stacking). `link` is the route of the ELEMENT that triggered the notice — clicking
// the notification opens it.
export function showDesktopNotification(domain: NotifDomain, opts: {
  title: string; body?: string; tag?: string; link?: string;
}): void {
  if (notifPermission() !== "granted") return;
  if (!notifPrefs()[domain]) return;
  // In the native app the window can be minimized but "visible"; document.hidden is unreliable in
  // WKWebView. On the web we keep the rule of only notifying with the tab hidden (avoids redundant noise).
  if (!isTauri() && !document.hidden) return;
  // Web: the click is handled in the service worker (notificationclick), which reads the link from `data`.
  // Desktop: Rust waits for the click and emits `notif-open` with the same link.
  void show(opts.title, opts.body, opts.tag, opts.link);
}
