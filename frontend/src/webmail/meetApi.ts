// Meet (video calls, recordings, presentations) endpoints. Extracted from backend.ts (law 1.4).
import { apiUrl, authHeaders, mediaUrl } from "../apiBase";
import { req, translateError } from "./backend";

export interface Recording {
  id: string; room_name: string; status: "recording" | "ready" | "failed"; started_by: string;
  started_at: string; ended_at: string | null; duration_seconds: number | null; size_bytes: number | null;
}
// Recording composition layout (LiveKit RoomComposite). Mirrors the list accepted by the backend.
export type RecordLayout = "grid" | "speaker" | "single-speaker";

export interface MeetControls { allow_mic: boolean; allow_cam: boolean; allow_share: boolean; allow_chat: boolean; lock_until_host: boolean; locked: boolean }
export interface MeetParticipant { identity: string; name: string; is_host: boolean; is_cohost?: boolean; can_manage?: boolean }
export interface BreakoutRoom { id: string; idx: number; name: string; room_name: string; members: string[] }
export interface BreakoutSession { url: string; token: string; room: string; name: string; breakout_id: string }
// locked=true: "host joins first" and they haven't arrived yet -- no token comes back at all.
export interface MeetSession { url?: string; token?: string; room: string; identity?: string; name?: string; meeting_id?: string; title?: string; is_host?: boolean; is_owner?: boolean; access?: string; controls?: MeetControls; locked?: boolean; full?: boolean; limit?: number }
export interface Presentation { id: string; name: string; current_slide: number; token: string; by: string }
export interface LobbyEntry { id: string; name: string; at: string }
export interface GuestJoin { mode: "joined" | "lobby"; url?: string; token?: string; room?: string; identity?: string; name?: string; lobby_id?: string }
export type RsvpStatus = "accepted" | "declined" | "tentative" | "pending";
export interface MeetParticipant { email: string; role: string; rsvp: RsvpStatus; notified: boolean; joined: boolean }
export interface Meeting {
  id: string; title: string; room: string; status: "scheduled" | "live" | "ended" | "canceled";
  scheduled_start: string | null; scheduled_end: string | null;
  actual_start: string | null; actual_end: string | null; duration_seconds: number | null;
  my_rsvp: RsvpStatus | ""; is_owner: boolean; participants: MeetParticipant[];
}
export interface CreateMeetingBody { title: string; scheduled_start?: string; scheduled_end?: string; participants: string[]; add_to_calendar: boolean; access?: string }

export const meetApi = {
  meetJoin: (room?: string, access?: string) => req<MeetSession>("/api/meet/join", { method: "POST", body: JSON.stringify({ room: room ?? "", access: access ?? "" }) }),
  meetLobby: (meetingId: string) => req<{ pending: LobbyEntry[] }>(`/api/meet/meetings/${meetingId}/lobby`),
  meetAdmit: (id: string) => req<{ status: string }>(`/api/meet/lobby/${id}/admit`, { method: "POST" }),
  meetDeny: (id: string) => req<{ status: string }>(`/api/meet/lobby/${id}/deny`, { method: "POST" }),
  publicMeetInfo: (room: string) => req<{ title: string; access: string; status: string; org?: string; brand?: { color?: string; logo?: string; logo_white_dark?: boolean } }>(`/api/public/meet/${room}`),
  publicMeetJoin: (room: string, name: string) => req<GuestJoin>(`/api/public/meet/${room}/join`, { method: "POST", body: JSON.stringify({ name }) }),
  publicLobbyPoll: (id: string) => req<GuestJoin & { status: string }>(`/api/public/meet/lobby/${id}`),
  meetList: () => req<{ meetings: Meeting[] }>("/api/meet/meetings"),
  meetListPage: (bucket: "upcoming" | "past", page: number, per = 6) =>
    req<{ meetings: Meeting[]; total: number }>(`/api/meet/meetings?bucket=${bucket}&page=${page}&per=${per}`),
  meetCreate: (body: CreateMeetingBody) => req<Meeting>("/api/meet/meetings", { method: "POST", body: JSON.stringify(body) }),
  meetRSVP: (id: string, status: RsvpStatus) => req<{ rsvp: string }>(`/api/meet/meetings/${id}/rsvp`, { method: "POST", body: JSON.stringify({ status }) }),
  meetCancel: (id: string) => req<{ status: string }>(`/api/meet/meetings/${id}/cancel`, { method: "POST" }),
  meetUpdate: (id: string, body: { title: string; scheduled_start: string; scheduled_end: string }) => req<Meeting>(`/api/meet/meetings/${id}`, { method: "PATCH", body: JSON.stringify(body) }),
  // Room controls/access -- the backend only accepts these from the meeting's owner.
  meetControls: (id: string, c: MeetControls) => req<MeetControls>(`/api/meet/meetings/${id}/controls`, { method: "PATCH", body: JSON.stringify(c) }),
  meetParticipants: (id: string) => req<MeetParticipant[]>(`/api/meet/meetings/${id}/participants`),
  meetKick: (id: string, identity: string) => req<{ status: string }>(`/api/meet/meetings/${id}/kick`, { method: "POST", body: JSON.stringify({ identity }) }),
  meetMute: (id: string, identity: string) => req<{ status: string }>(`/api/meet/meetings/${id}/mute`, { method: "POST", body: JSON.stringify({ identity }) }),
  meetCohosts: (id: string) => req<{ cohosts: string[] }>(`/api/meet/meetings/${id}/cohosts`),
  meetAddCohost: (id: string, userId: string) => req<{ status: string }>(`/api/meet/meetings/${id}/cohosts`, { method: "POST", body: JSON.stringify({ user_id: userId }) }),
  meetRemoveCohost: (id: string, userId: string) => req<{ status: string }>(`/api/meet/meetings/${id}/cohosts/${userId}`, { method: "DELETE" }),
  meetBreakouts: (id: string) => req<{ breakouts: BreakoutRoom[] }>(`/api/meet/meetings/${id}/breakouts`),
  meetCreateBreakouts: (id: string, count: number, autoAssign: boolean) => req<{ breakouts: BreakoutRoom[] }>(`/api/meet/meetings/${id}/breakouts`, { method: "POST", body: JSON.stringify({ count, auto_assign: autoAssign }) }),
  meetAssignBreakout: (id: string, bid: string, userId: string) => req<{ status: string }>(`/api/meet/meetings/${id}/breakouts/${bid}/assign`, { method: "POST", body: JSON.stringify({ user_id: userId }) }),
  meetCloseBreakouts: (id: string) => req<{ status: string }>(`/api/meet/meetings/${id}/breakouts`, { method: "DELETE" }),
  meetBreakoutToken: (id: string, bid: string) => req<BreakoutSession>(`/api/meet/meetings/${id}/breakouts/${bid}/token`),
  meetSendNotes: (id: string) => req<{ sent: number; failed: number }>(`/api/meet/meetings/${id}/notes/email`, { method: "POST" }),
  meetAccess: (id: string, access: string) => req<{ access: string }>(`/api/meet/meetings/${id}/access`, { method: "PATCH", body: JSON.stringify({ access }) }),
  meetNotes: (id: string) => req<{ body: string; title: string; updated_at?: string }>(`/api/meet/meetings/${id}/notes`),
  meetSaveNotes: (id: string, body: string) => req<{ body: string; updated_at: string }>(`/api/meet/meetings/${id}/notes`, { method: "PUT", body: JSON.stringify({ body }) }),
  meetRecordStart: (room: string) => req<{ recording_id: string; egress_id: string }>("/api/meet/record/start", { method: "POST", body: JSON.stringify({ room }) }),
  meetRecordStop: (room: string) => req<{ status: string }>("/api/meet/record/stop", { method: "POST", body: JSON.stringify({ room }) }),
  meetRecordings: (page = 1, per = 6) => req<{ recordings: Recording[]; total: number }>(`/api/meet/recordings?page=${page}&per=${per}`),
  recordingPlayUrl: (id: string) => mediaUrl(`/api/meet/recordings/${id}/play`),
  meetRecordingDelete: (id: string) => req<{ status: string }>(`/api/meet/recordings/${id}`, { method: "DELETE" }),
  // Meet presentation (BBB-style). The PDF is served from a public URL with a token (members and guests).
  presentationGet: (room: string) => req<Presentation & { active: boolean }>(`/api/meet/presentation?room=${encodeURIComponent(room)}`),
  presentationStartDrive: (room: string, fileId: string) => req<Presentation>("/api/meet/presentation/drive", { method: "POST", body: JSON.stringify({ room, file_id: fileId }) }),
  presentationStartUpload: async (room: string, file: File): Promise<Presentation> => {
    const fd = new FormData();
    fd.append("room", room);
    fd.append("file", file);
    const res = await fetch(apiUrl("/api/meet/presentation/upload"), { method: "POST", credentials: "include", headers: authHeaders(), body: fd });
    if (!res.ok) { const b = await res.json().catch(() => ({})); throw new Error(translateError(b.error_code, b.error, res.status)); }
    return res.json();
  },
  presentationSetSlide: (id: string, slide: number) => req<{ status: string }>(`/api/meet/presentation/${id}/slide`, { method: "POST", body: JSON.stringify({ slide }) }),
  // keepalive: closing often happens right as the window/tab CLOSES -- without this the browser
  // aborts the request mid-flight and the presentation stayed active on the server.
  presentationStop: (id: string) => req<{ status: string }>(`/api/meet/presentation/${id}/stop`, { method: "POST", keepalive: true }),
  presentationPdfUrl: (id: string, token: string) => apiUrl(`/api/public/meet/presentation/${id}/pdf?token=${encodeURIComponent(token)}`),
};
