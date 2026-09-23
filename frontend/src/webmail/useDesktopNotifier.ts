import { useEffect, useRef } from "react";
import type { MailService } from "../jmap/mail";
import type { Notif } from "./backend";
import { showDesktopNotification, isDomain, type NotifDomain } from "./desktopNotify";
import { isTauri } from "./tauriNotify";
import { playSound } from "./sounds";
import { notifText } from "./notifText";
import i18n from "../i18n";

// Maps the `kind` of an in-app notification to the desktop notification domain. Without a domain
// there is no OS alert — that is why the calendar (invites, RSVP, bookings) and Meet's "someone is
// at the door" also fall in here.
function domainOfKind(kind: string): NotifDomain | null {
  if (kind.startsWith("chat")) return "chat";
  if (kind.startsWith("meet")) return "meet"; // meeting_invite and meet_knock
  if (kind.startsWith("event") || kind === "booking_new") return "cal";
  if (kind.startsWith("file")) return "files";
  if (kind.startsWith("task")) return "tasks";
  return null;
}

// Triggers desktop notifications from two sources:
//  1. E-mail: JMAP push does not carry the content, so on a state change we fetch the most
//     recent message from the inbox and compare it with the last one seen (only what is new gets notified).
//  2. Other domains (chat mention, meeting, file, task): diff against the in-app notification
//     list — every new, unread row becomes an OS notification.
// In both cases, the first read only records the baseline (it does not notify about what already existed).
export function useDesktopNotifier(opts: {
  service: MailService;
  inboxId: string | null;
  notifItems: Notif[] | undefined;
}): (() => void) {
  const { service, inboxId, notifItems } = opts;

  // --- E-mail ---
  const lastEmailId = useRef<string | null>(null);
  const emailReady = useRef(false);
  const checkEmail = useRef<() => void>(() => {});
  checkEmail.current = async () => {
    if (!inboxId) return;
    try {
      const page = await service.listEmails(inboxId, 0, 1);
      const top = page.items[0];
      if (!top) return;
      if (!emailReady.current) { lastEmailId.current = top.id; emailReady.current = true; return; }
      if (top.id !== lastEmailId.current) {
        lastEmailId.current = top.id;
        const seen = top.keywords?.$seen;
        if (!seen) {
          const from = top.from?.[0]?.name || top.from?.[0]?.email || "";
          showDesktopNotification("email", {
            title: from || top.subject || "Novo e-mail",
            body: from ? (top.subject || "") : top.preview,
            tag: `mail-${top.id}`,
            // Opens the MESSAGE, not just the mailbox: /mail/mail/<folder>/<id> is the reader's route.
            link: `/mail/mail/inbox/${top.id}`,
          });
          // DESKTOP APP ONLY. On the web e-mail stays silent: someone with the webmail open all day
          // gets dozens of messages, and a sound for each one leads to muting the sound entirely —
          // including the alerts that matter. In the native app the alert comes from the system, which is expected.
          if (isTauri()) playSound("email");
        }
      }
    } catch { /* network: ignore, polling recovers */ }
  };

  // --- In-app notifications (chat/meet/files/tasks) ---
  const seenNotif = useRef<Set<string>>(new Set());
  const notifReady = useRef(false);
  useEffect(() => {
    if (!notifItems) return;
    if (!notifReady.current) {
      notifItems.forEach((n) => seenNotif.current.add(n.id));
      notifReady.current = true;
      return;
    }
    for (const n of notifItems) {
      if (seenNotif.current.has(n.id)) continue;
      seenNotif.current.add(n.id);
      if (n.read_at) continue;
      const dom = domainOfKind(n.kind);
      if (!dom || !isDomain(dom)) continue;
      const txt = notifText(n, i18n.t.bind(i18n), i18n.language);
      // `n.link` points to the element (the conversation, the file, the event) — the same target the
      // bell uses. Clicking the OS notification opens exactly what clicking the bell opens.
      showDesktopNotification(dom, {
        title: txt.title, body: txt.body, tag: `notif-${n.id}`, link: n.link,
      });
      // The sound plays REGARDLESS of the tab being visible, unlike the OS notification. That is
      // precisely what it is for: a second monitor counts as "visible" to the browser, but whoever
      // is looking at the other screen sees nothing — they hear it.
      playSound("notify");
    }
  }, [notifItems]);

  // Returns the e-mail trigger for the App to call inside the JMAP push callback.
  return () => checkEmail.current();
}
