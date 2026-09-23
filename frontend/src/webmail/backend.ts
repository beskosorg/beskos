// Typed client for OUR API (control plane) for the email-adjacent features: AI, Kanban, calendar,
// labels. Uses the webmail session cookie (POST /api/webmail/login). Same-origin via proxy.
// Email itself stays direct JMAP against Stalwart (see jmap/).
//
// This file used to hold every resource group inline and grew past the 600-line limit (law 1.4).
// It now keeps only the shared low-level helpers (translateError, ApiError, req) and the merged
// `backend` object; the resource-specific functions live in sibling `*Api.ts` files and are
// re-exported here so existing `import { backend, ... } from "./backend"` call sites keep working.

import i18n from "../i18n";
import { apiUrl, authHeaders, mediaUrl, notifyUnauthorized } from "../apiBase";

// Translates the backend error by its stable KEY (error_code) via i18n (server.<code>). Never
// shows the raw backend text in the UI (law §4.6).
export function translateError(code: string | undefined, serverMsg: string | undefined, status: number): string {
  if (code) return i18n.t(`server.${code}`, { defaultValue: serverMsg || i18n.t("errors.http", { status }) });
  return serverMsg || i18n.t("errors.http", { status });
}

// ApiError carries the HTTP STATUS alongside the already-translated message. Without this, callers
// only had the translated text to classify the error (e.g. telling "wrong password" apart from
// "backend is down"), and regexing a string that changes with the locale is fragile -- a translated
// 401 does not contain "401".
export class ApiError extends Error {
  status: number;
  code?: string;
  constructor(status: number, message: string, code?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

// Exported so a feature can keep its own API module (see notesApi.ts) instead of growing this
// file, which is long past the 600 lines the project allows.
export async function req<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch(apiUrl(path), {
    ...opts,
    credentials: "include",
    headers: { "Content-Type": "application/json", ...authHeaders(), ...(opts.headers || {}) },
  });
  if (!res.ok) {
    if (res.status === 401) notifyUnauthorized(path); // dead session -> back to login (global)
    const body = await res.json().catch(() => ({}));
    throw new ApiError(res.status, translateError(body.error_code, body.error, res.status), body.error_code);
  }
  return res.json() as Promise<T>;
}

// ---- Resource groups, each in its own module (law 1.4) ----
import { authApi } from "./authApi";
import { aiApi } from "./aiApi";
import { announcementsApi } from "./announcementsApi";
import { chatApi } from "./chatApi";
import { kanbanApi } from "./kanbanApi";
import { labelsApi } from "./labelsApi";
import { calendarApi } from "./calendarApi";
import { filesApi } from "./filesApi";
import { meetApi } from "./meetApi";
import { accountApi } from "./accountApi";
export * from "./authApi";
export * from "./aiApi";
export * from "./announcementsApi";
export * from "./chatApi";
export * from "./kanbanApi";
export * from "./labelsApi";
export * from "./calendarApi";
export * from "./filesApi";
export * from "./meetApi";
export * from "./accountApi";
export * from "./contactsApi";
export * from "./wikiApi";

export const backend = {
  ...authApi,
  ...aiApi,
  ...announcementsApi,
  ...chatApi,
  ...kanbanApi,
  ...labelsApi,
  ...calendarApi,
  ...filesApi,
  ...meetApi,
  ...accountApi,
};

export interface Colleague { id: string; email: string; name: string; avatar?: string }

// Tenant colleagues -- for picking who does a task.
export interface Gif { id: string; file_id: string; name: string; size: number; created_at: string }

// GIFs: the "library" is just a mark over Drive files -- nothing leaves to outside services.
export const gifsApi = {
  list: () => req<Gif[]>("/api/gifs"),
  add: (file_id: string, name: string) => req<Gif>("/api/gifs", { method: "POST", body: JSON.stringify({ file_id, name }) }),
  remove: (id: string) => req<{ status: string }>(`/api/gifs/${id}`, { method: "DELETE" }),
};

// User preferences, per app. Each section saves in isolation (merged server-side).
export interface EmailSettings { signature?: string; signatureOn?: boolean; aiSummary?: boolean }
// General preferences that follow the user across devices (web, desktop, mobile). localStorage
// remains the instant pre-login cache; once loaded, the server wins.
export interface GeneralSettings { theme?: "light" | "dark"; language?: string; startView?: string;
  /** First-run setup completed (password, brand, team). Absent = still pending. */
  onboarded?: boolean }
export interface ChatSettings { enterSends?: boolean; sounds?: boolean }
export interface CalendarSettings {
  weekStart?: "monday" | "sunday";
  workDays?: number[]; // working days (0=Sunday ... 6=Saturday); absent = all
  defaultView?: "month" | "week" | "day" | "list";
  defaultDuration?: number; // minutes
  defaultReminder?: number; // minutes before; 0 = no reminder
}
export interface FilesSettings { view?: "grid" | "list"; linkExpiryDays?: number }
export interface MeetSettings { enterMuted?: boolean; camOff?: boolean }
export interface KanbanSettings { defaultBoard?: string; defaultView?: "kanban" | "list" | "calendar"; boardOrder?: string[] }
export interface UserSettings {
  email?: EmailSettings;
  general?: GeneralSettings;
  chat?: ChatSettings;
  calendar?: CalendarSettings;
  files?: FilesSettings;
  meet?: MeetSettings;
  kanban?: KanbanSettings;
}

export const settingsApi = {
  get: () => req<UserSettings>("/api/settings"),
  // patch is {app: {...}} -- saves only the sent section, without touching the others.
  patch: (patch: Partial<UserSettings>) => req<{ status: string }>("/api/settings", { method: "PATCH", body: JSON.stringify(patch) }),
};

// Devices (tokens for the sync daemon). The raw token only comes back on creation -- never again.
export interface DeviceToken {
  id: string; name: string; token_prefix: string; scope: string;
  last_used_at: string | null; expires_at: string | null; revoked_at: string | null; created_at: string;
}
export const devicesApi = {
  list: () => req<{ devices: DeviceToken[] }>("/api/devices"),
  create: (name: string, expiresInDays = 0) =>
    req<{ id: string; name: string; token: string; prefix: string }>("/api/devices", { method: "POST", body: JSON.stringify({ name, expires_in_days: expiresInDays }) }),
  revoke: (id: string) => req<{ status: string }>(`/api/devices/${id}`, { method: "DELETE" }),
};

export const colleaguesApi = {
  list: () => req<Colleague[]>("/api/colleagues"),
};

import type { CardComment } from "./kanbanApi";
export const commentsApi = {
  list: (cardId: string) => req<CardComment[]>(`/api/cards/${cardId}/comments`),
  add: (cardId: string, body: string, parentId?: string) =>
    req<CardComment>(`/api/cards/${cardId}/comments`, { method: "POST", body: JSON.stringify({ body, parent_id: parentId ?? "" }) }),
  remove: (cardId: string, id: string) =>
    req<{ status: string }>(`/api/cards/${cardId}/comments/${id}`, { method: "DELETE" }),
  edit: (cardId: string, id: string, body: string) =>
    req<CardComment>(`/api/cards/${cardId}/comments/${id}`, { method: "PATCH", body: JSON.stringify({ body }) }),
};

export interface CardActivity { id: string; kind: string; detail: string; created_at: string; actor_name: string }
export const cardActivityApi = {
  list: (cardId: string) => req<CardActivity[]>(`/api/cards/${cardId}/activity`),
};

export interface CardAttachment { id: string; file_id: string; name: string; size: number; created_at: string }
export const cardAttachmentsApi = {
  list: (cardId: string) => req<CardAttachment[]>(`/api/cards/${cardId}/attachments`),
  attach: (cardId: string, fileId: string) => req<CardAttachment[]>(`/api/cards/${cardId}/attachments`, { method: "POST", body: JSON.stringify({ file_id: fileId }) }),
  detach: (cardId: string, attId: string) => req<{ status: string }>(`/api/cards/${cardId}/attachments/${attId}`, { method: "DELETE" }),
  downloadUrl: (cardId: string, attId: string) => mediaUrl(`/api/cards/${cardId}/attachments/${attId}/download`),
};

export interface ChecklistItem { id: string; text: string; done: boolean; position: number }
export const checklistApi = {
  list: (cardId: string) => req<ChecklistItem[]>(`/api/cards/${cardId}/checklist`),
  add: (cardId: string, text: string) => req<ChecklistItem>(`/api/cards/${cardId}/checklist`, { method: "POST", body: JSON.stringify({ text }) }),
  toggle: (cardId: string, id: string, done: boolean) => req<ChecklistItem>(`/api/cards/${cardId}/checklist/${id}`, { method: "PATCH", body: JSON.stringify({ done }) }),
  remove: (cardId: string, id: string) => req<{ status: string }>(`/api/cards/${cardId}/checklist/${id}`, { method: "DELETE" }),
};
