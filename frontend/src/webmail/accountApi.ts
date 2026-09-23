// Account/user endpoints: presence, addresses, aliases, profile, outbox, templates, branding,
// notifications, push. Extracted from backend.ts (law 1.4 -- 600 lines).
import { req } from "./backend";

export interface Branding { name: string; color: string; logo: string; logo_white_dark?: boolean }
export interface Notif { id: string; kind: string; title: string; body: string; link: string; read_at: string | null; created_at: string; args?: Record<string, string> }

export interface EmailAlias {
  id: string; address: string; domain?: string;
  owner_id: string; owner_email?: string; owner_name?: string; created_at?: string;
}
export interface MailConnection {
  host: string; username: string;
  imap: { host: string; port: number; security: string };
  smtp: { host: string; port: number; security: string }[];
  jmap: { url: string }; caldav: { url: string };
}
export interface OutboxItem { id: string; email_id: string; from_email: string; rcpts: string[]; subject: string; send_at: string; status: string; created_at: string }
export interface MailTemplate { id: string; name: string; subject: string; body_html: string; created_at: string; updated_at: string }

export const accountApi = {
  setMyStatus: (emoji: string, text: string) => req<{ emoji: string; text: string }>("/api/me/status", { method: "PUT", body: JSON.stringify({ emoji, text }) }),
  userStatus: (email: string) => req<{ emoji: string; text: string }>(`/api/user-status?email=${encodeURIComponent(email)}`),
  // Connection data for external clients (IMAP/SMTP) -- what gets pasted into Outlook or the site.
  mailConnection: () => req<MailConnection>("/api/me/mail-connection"),
  // Sender addresses: the user's own, and the aliases that deliver into their mailbox.
  myAddresses: () => req<{ primary: string; aliases: string[] }>("/api/me/addresses"),
  // Organization aliases (admin panel).
  aliases: () => req<{ aliases: EmailAlias[] }>("/api/aliases"),
  createAlias: (local: string, domainId: string, ownerId: string) =>
    req<EmailAlias>("/api/aliases", { method: "POST", body: JSON.stringify({ local, domain_id: domainId, owner_id: ownerId }) }),
  deleteAlias: (id: string) => req<{ status: string }>(`/api/aliases/${id}`, { method: "DELETE" }),
  me: () => req<{ id?: string; presence?: string; email?: string; name?: string; role?: string; mfa_setup_required?: boolean; mfa_required_method?: string; avatar?: string }>("/api/me"),
  myProfile: () => req<{ email: string; name: string; job_title: string; phone: string; avatar: string }>("/api/me/profile"),
  updateMyProfile: (p: { name: string; job_title: string; phone: string; avatar: string }) =>
    req<{ status: string }>("/api/me/profile", { method: "PUT", body: JSON.stringify(p) }),
  changeMyPassword: (current: string, newPw: string) =>
    req<{ status: string }>("/api/me/password", { method: "POST", body: JSON.stringify({ current, "new": newPw }) }),
  setPresence: (presence: "active" | "away" | "dnd") => req<{ presence: string }>("/api/me/presence", { method: "PUT", body: JSON.stringify({ presence }) }),
  // Snooze: records the wake time; the worker returns the message to the inbox (the client already
  // moved it to Snoozed).
  snoozeEmail: (emailId: string, wakeAt: string) => req<{ status: string }>("/api/mail/snooze", { method: "POST", body: JSON.stringify({ email_id: emailId, wake_at: wakeAt }) }),
  // Server-side outbox: schedules the real send (undo-send / scheduled). The worker submits via JMAP.
  outboxCreate: (b: { email_id: string; identity_id: string; from: string; rcpts: string[]; subject: string; send_at: string }) =>
    req<{ id: string }>("/api/mail/outbox", { method: "POST", body: JSON.stringify(b) }),
  outboxList: () => req<OutboxItem[]>("/api/mail/outbox"),
  outboxCancel: (id: string) => req<{ status: string }>(`/api/mail/outbox/${id}`, { method: "DELETE" }),
  // Canned responses owned by the user.
  templatesList: () => req<MailTemplate[]>("/api/mail/templates"),
  templateCreate: (b: { name: string; subject: string; body_html: string }) => req<MailTemplate>("/api/mail/templates", { method: "POST", body: JSON.stringify(b) }),
  templateUpdate: (id: string, b: { name: string; subject: string; body_html: string }) => req<MailTemplate>(`/api/mail/templates/${id}`, { method: "PATCH", body: JSON.stringify(b) }),
  templateDelete: (id: string) => req<{ status: string }>(`/api/mail/templates/${id}`, { method: "DELETE" }),
  brandingGet: () => req<Branding>("/api/branding"),
  brandingUpdate: (b: { color: string; logo: string; logo_white_dark?: boolean }) => req<{ status: string }>("/api/branding", { method: "PATCH", body: JSON.stringify(b) }),
  brandingResolve: (email: string) => req<Branding>(`/api/public/branding?email=${encodeURIComponent(email)}`),
  notifications: () => req<{ items: Notif[]; unread: number }>("/api/notifications"),
  notificationRead: (id: string) => req<{ status: string }>(`/api/notifications/${id}/read`, { method: "POST" }),
  notificationsReadAll: () => req<{ status: string }>("/api/notifications/read-all", { method: "POST" }),
  notificationsReadKind: (kind: string) => req<{ status: string }>("/api/notifications/read-kind", { method: "POST", body: JSON.stringify({ kind }) }),

  pushVapid: () => req<{ enabled: boolean; publicKey?: string }>("/api/push/vapid"),
  pushSubscribe: (sub: unknown) => req<{ status: string }>("/api/push/subscribe", { method: "POST", body: JSON.stringify(sub) }),
  pushUnsubscribe: (endpoint: string) => req<{ status: string }>("/api/push/unsubscribe", { method: "POST", body: JSON.stringify({ endpoint }) }),
};
