import type { EmailAddress } from "../jmap/types";

const HUES = ["#B44D3A", "#116466", "#8257C7", "#2E7D5B", "#C0692F", "#3B6FB0", "#A23E6E", "#5B6470"];

export function displayName(addrs: EmailAddress[] | null): string {
  const a = addrs?.[0];
  if (!a) return "—";
  return a.name || a.email;
}

export function initials(name: string): string {
  const parts = name.replace(/["']/g, "").trim().split(/[\s@.]+/).filter(Boolean);
  return ((parts[0]?.[0] ?? "?") + (parts[1]?.[0] ?? "")).toUpperCase();
}

// Company logo from the domain: the site's own favicon (no external provider/data broker).
// Sanitizes the domain to avoid path/scheme injection; returns "" if invalid.
export function faviconOf(domain: string): string {
  const d = domain.trim().toLowerCase().replace(/^https?:\/\//, "").replace(/\/.*$/, "");
  if (!/^[a-z0-9.-]+\.[a-z]{2,}$/.test(d)) return "";
  return `https://${d}/favicon.ico`;
}

export function hueFor(key: string): string {
  let h = 0;
  for (let i = 0; i < key.length; i++) h = (h * 31 + key.charCodeAt(i)) >>> 0;
  return HUES[h % HUES.length];
}

// Formats the relative/short date per the locale (law §4.5 — Intl, not by hand).
// Full spelled-out date, with timezone — what is wanted in "show details" and when printing.
// E.g.: "Thursday, July 16, 2026 at 07:49 (GMT-3)".
export function fullDateTime(iso: string, locale: string): string {
  return new Date(iso).toLocaleString(locale, {
    weekday: "long", day: "2-digit", month: "long", year: "numeric",
    hour: "2-digit", minute: "2-digit", timeZoneName: "short",
  });
}

// "1 hour ago", "3 days ago" — the relative label shown next to the time, like in Gmail.
export function relativeTime(iso: string, locale: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  const min = Math.round(diff / 60000);
  if (Math.abs(min) < 60) return rtf.format(-min, "minute");
  const hr = Math.round(min / 60);
  if (Math.abs(hr) < 24) return rtf.format(-hr, "hour");
  return rtf.format(-Math.round(hr / 24), "day");
}

export function shortTime(iso: string, locale: string): string {
  const d = new Date(iso);
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  if (sameDay) return d.toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit" });
  return d.toLocaleDateString(locale, { day: "2-digit", month: "short" });
}
