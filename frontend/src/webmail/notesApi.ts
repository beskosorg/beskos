import { req } from "./backend";

// Notes API (migration 00133). In its own module rather than inside backend.ts: that file is four
// hundred lines past the project limit, and every feature that adds itself to it makes the next
// person's job worse.

// A note is personal by default -- it becomes shared only by being granted. A set `meeting_id`
// means it belongs to a meeting: same object, same sharing, same "turn into tasks".
export interface Note {
  id: string; title: string; body_json: unknown; body_text: string;
  meeting_id: string | null; meeting_title: string | null;
  owner_user_id: string; owner_name: string | null;
  pinned: boolean; created_at: string; updated_at: string;
}

// body_text is NOT sent: the server derives it from the blocks with the wiki renderer, so the
// searchable text and the AI extraction can never describe something the note does not say.
export interface NoteInput { title: string; body_json: unknown }

export interface NoteGrant {
  id: string; subject_type: "user" | "group" | "org"; subject_id: string | null; role: string;
  user_name?: string | null; user_email?: string | null; group_name?: string | null;
}

export const notesApi = {
  // The meeting's note. Created on first use and shared with everyone who was in the meeting --
  // invited or simply present. Fetching it again re-runs the sharing, so whoever arrives late is
  // added too.
  forMeeting: (meetingId: string) => req<Note>(`/api/meet/meetings/${meetingId}/note`),
  list: () => req<{ notes: Note[] }>("/api/notes"),
  get: (id: string) => req<Note>(`/api/notes/${id}`),
  create: (body: NoteInput) => req<Note>("/api/notes", { method: "POST", body: JSON.stringify(body) }),
  update: (id: string, body: NoteInput) => req<Note>(`/api/notes/${id}`, { method: "PATCH", body: JSON.stringify(body) }),
  remove: (id: string) => req<{ status: string }>(`/api/notes/${id}`, { method: "DELETE" }),
  pin: (id: string, on: boolean) => req<{ pinned: boolean }>(`/api/notes/${id}/pin`, { method: on ? "POST" : "DELETE" }),
  grants: (id: string) => req<{ grants: NoteGrant[] }>(`/api/notes/${id}/grants`),
  setGrant: (id: string, g: { subject_type: string; subject_id: string | null; role: string }) =>
    req<NoteGrant>(`/api/notes/${id}/grants`, { method: "POST", body: JSON.stringify(g) }),
  removeGrant: (id: string, grantId: string) =>
    req<{ status: string }>(`/api/notes/${id}/grants/${grantId}`, { method: "DELETE" }),
};
