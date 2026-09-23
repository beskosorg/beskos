// Calendar (events, calendars, booking, time off) endpoints. Extracted from backend.ts (law 1.4).
import { apiUrl, authHeaders, mediaUrl } from "../apiBase";
import { req, translateError } from "./backend";

export interface CalEvent {
  id: string; title: string; description: string; location: string;
  color: string; starts_at: string; ends_at: string; source_email_id: string;
  tags: string[]; meeting_room: string; remind_minutes?: number; all_day?: boolean; recur?: string; recur_until?: string | null;
  invited?: boolean; my_rsvp?: string; calendar_id?: string | null; booking_id?: string | null; kind?: string;
  attendees?: { name: string; email: string; avatar?: string; status?: string }[];
}
export interface TimeOffConflict { id: string; title: string; starts_at: string; ends_at: string; own: boolean }
export interface BookingWindow { weekday: number; start: number; end: number }
export interface BookingCfg { enabled: boolean; title: string; duration_min: number; phone_field: string; company_field: string; windows: BookingWindow[]; url: string }
export interface Calendar { id: string; name: string; color: string; owner_user_id: string; created_at: string; }
export interface Tag { id: string; name: string; color: string }
export interface EventInvite { user_id: string; status: string; name: string; email: string }
export interface EventInput { title: string; starts_at: string; ends_at: string; description?: string; location?: string; color?: string; tags?: string[]; create_meet?: boolean; force?: boolean; remind_minutes?: number; all_day?: boolean; recur?: string; recur_until?: string; invitee_ids?: string[]; external_emails?: string[]; calendar_id?: string }
export interface EventDraft { title: string; starts_at: string; ends_at: string; tags: string[] }
export interface Slot { starts_at: string; ends_at: string }
// Schedule conflict error (server responds 409 with the colliding events).
export class ConflictError extends Error { conflicts: CalEvent[]; constructor(c: CalEvent[]) { super("conflict"); this.conflicts = c; } }

export const calendarApi = {
  events: (from: string, to: string) =>
    req<CalEvent[]>(`/api/events?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`),
  // Creates an event; on a schedule conflict when not forced, throws ConflictError with the clashes.
  createEvent: async (e: EventInput): Promise<CalEvent> => {
    const res = await fetch(apiUrl("/api/events"), { method: "POST", credentials: "include", headers: { "Content-Type": "application/json", ...authHeaders() }, body: JSON.stringify(e) });
    if (res.status === 409) { const b = await res.json(); throw new ConflictError(b.conflicts ?? []); }
    if (!res.ok) { const b = await res.json().catch(() => ({})); throw new Error(translateError(b.error_code, b.error, res.status)); }
    return res.json();
  },
  // Editing an event; as with create, a schedule conflict (without force) throws ConflictError.
  updateEvent: async (id: string, e: EventInput): Promise<CalEvent> => {
    const res = await fetch(apiUrl(`/api/events/${id}`), { method: "PATCH", credentials: "include", headers: { "Content-Type": "application/json", ...authHeaders() }, body: JSON.stringify(e) });
    if (res.status === 409) { const b = await res.json(); throw new ConflictError(b.conflicts ?? []); }
    if (!res.ok) { const b = await res.json().catch(() => ({})); throw new Error(translateError(b.error_code, b.error, res.status)); }
    return res.json();
  },
  deleteEvent: (id: string) => req<{ status: string }>(`/api/events/${id}`, { method: "DELETE" }),
  restoreEvent: (id: string) => req<CalEvent>(`/api/events/${id}/restore`, { method: "POST" }),
  duplicateEvent: (id: string, title?: string) => req<CalEvent>(`/api/events/${id}/duplicate`, { method: "POST", body: JSON.stringify({ title: title ?? "" }) }),
  addMeetToEvent: (id: string) => req<{ meeting_room: string }>(`/api/events/${id}/meet`, { method: "POST" }),
  // Booking: the user's own public booking page (Calendly-style).
  bookingPage: () => req<BookingCfg>("/api/booking/page"),
  timeOff: (v: { kind: string; start: string; end: string; start_min?: number; end_min?: number; auto_reply: boolean; reply_subject: string; reply_body: string; on_conflict?: string }) =>
    req<{ status: string; auto_reply?: string; conflicts?: TimeOffConflict[] }>("/api/timeoff", { method: "POST", body: JSON.stringify(v) }),
  bookingSave: (cfg: Omit<BookingCfg, "url">) => req<BookingCfg>("/api/booking/page", { method: "PUT", body: JSON.stringify(cfg) }),
  calendars: () => req<Calendar[]>("/api/calendars"),
  createCalendar: (name: string, color: string) => req<Calendar>("/api/calendars", { method: "POST", body: JSON.stringify({ name, color }) }),
  updateCalendar: (id: string, name: string, color: string) => req<Calendar>(`/api/calendars/${id}`, { method: "PATCH", body: JSON.stringify({ name, color }) }),
  deleteCalendar: (id: string) => req<{ status: string }>(`/api/calendars/${id}`, { method: "DELETE" }),
  freeBusy: (userId: string, from: string, to: string) => req<{ starts_at: string; ends_at: string }[]>(`/api/events/free-busy?user=${userId}&from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`),
  eventExternals: (id: string) => req<string[]>(`/api/events/${id}/externals`),
  eventInvites: (id: string) => req<EventInvite[]>(`/api/events/${id}/invites`),
  rsvpEvent: (id: string, status: string) => req<{ status: string }>(`/api/events/${id}/rsvp`, { method: "POST", body: JSON.stringify({ status }) }),
  tagsList: () => req<Tag[]>("/api/tags"),
  tagUpsert: (name: string, color: string) => req<Tag>("/api/tags", { method: "POST", body: JSON.stringify({ name, color }) }),
  tagDelete: (id: string) => req<{ status: string }>(`/api/tags/${id}`, { method: "DELETE" }),
  // Sends the browser's timezone so the AI can interpret "tomorrow 10am" in the user's own time.
  aiParseEvent: (text: string) => req<EventDraft>("/api/ai/calendar/parse", { method: "POST", body: JSON.stringify({ text, tz: Intl.DateTimeFormat().resolvedOptions().timeZone }) }),
  aiSuggestSlots: (duration_min: number, from: string, to: string) => req<{ slots: Slot[] }>("/api/ai/calendar/suggest", { method: "POST", body: JSON.stringify({ duration_min, from, to }) }),
  // Export/import the calendar as iCalendar (.ics).
  eventsExportUrl: () => mediaUrl("/api/events/export"),
  importEvents: async (file: File): Promise<{ imported: number }> => {
    const fd = new FormData();
    fd.append("file", file);
    const res = await fetch(apiUrl("/api/events/import"), { method: "POST", credentials: "include", headers: authHeaders(), body: fd });
    if (!res.ok) { const b = await res.json().catch(() => ({})); throw new Error(translateError(b.error_code, b.error, res.status)); }
    return res.json();
  },
  aiAgenda: (from: string, to: string) => req<{ agenda: string }>("/api/ai/calendar/agenda", { method: "POST", body: JSON.stringify({ from, to }) }),
  aiWikiWrite: (instruction: string, context: string) => req<{ content: string }>("/api/ai/wiki-write", { method: "POST", body: JSON.stringify({ instruction, context }) }),
};
