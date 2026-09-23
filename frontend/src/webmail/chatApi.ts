// Chat (Slack-style) endpoints: channels, messages, reactions, webhooks. Extracted from backend.ts
// (law 1.4 -- 600 lines).
import { mediaUrl } from "../apiBase";
import { req } from "./backend";

export interface ChatChannel { id: string; kind: "public" | "private" | "dm"; name: string; topic: string; is_general?: boolean; unread?: number; last_read_at?: string; other_id?: string; listed?: boolean; add_policy?: "open" | "restricted"; my_role?: string; notif_pref?: "all" | "mentions" | "none" }
export interface ArchivedChannel { id: string; kind: string; name: string; topic: string; is_general?: boolean; archived_at: string }
export interface ChatSearchHit { id: string; channel_id: string; channel_name: string; channel_kind: string; author: string; body: string; created_at: string }
export interface ChatReaction { emoji: string; count: number; mine: boolean; users: string[] }
export interface ChatAttachment { file_id: string; name: string; size: number }
export interface ChatMessage { id: string; channel_id: string; user_id: string; parent_id: string; body: string; author: string; author_email: string; deleted: boolean; edited_at: string; created_at: string; reply_count: number; reactions: ChatReaction[]; attachments: ChatAttachment[]; saved?: boolean; bot_name?: string }
export interface ChatWebhook { id: string; channel_id: string; token: string; name: string; created_at: string }
export interface ChatSavedItem { id: string; channel_id: string; channel_name: string; channel_kind: string; author: string; body: string; created_at: string; saved_at: string }
export interface UnfurlData { kind?: "task" | "note"; url: string; title: string; description: string; image: string; site: string }
export interface ChatMember { user_id: string; email: string; name: string; role: string; status_emoji?: string; status_text?: string; last_read_at?: string }
export interface ChatPin { id: string; body: string; author: string; created_at: string; pinned_at: string }
export interface ChatBookmark { id: string; kind: "link" | "file"; title: string; url: string; file_name: string; file_size: number; created_at: string }

export const chatApi = {
  chatMembersSearch: (id: string, q = "", page = 1) => req<{ members: ChatMember[]; total: number; page: number }>(`/api/chat/channels/${id}/members/search?q=${encodeURIComponent(q)}&page=${page}`),
  chatCanvas: (id: string) => req<{ canvas: string; updated_at: string | null }>(`/api/chat/channels/${id}/canvas`),
  chatSaveCanvas: (id: string, canvas: string) => req<{ status: string }>(`/api/chat/channels/${id}/canvas`, { method: "PUT", body: JSON.stringify({ canvas }) }),
  chatPins: (id: string) => req<{ pins: ChatPin[] }>(`/api/chat/channels/${id}/pins`),
  chatPin: (msgId: string, pinned: boolean) => req<{ status: string }>(`/api/chat/messages/${msgId}/pin`, { method: "POST", body: JSON.stringify({ pinned }) }),
  chatBookmarks: (id: string) => req<{ bookmarks: ChatBookmark[] }>(`/api/chat/channels/${id}/bookmarks`),
  chatAddBookmark: (id: string, b: { kind: "link" | "file"; title?: string; url?: string; file_id?: string; file_name?: string; file_size?: number }) =>
    req<{ id: string }>(`/api/chat/channels/${id}/bookmarks`, { method: "POST", body: JSON.stringify(b) }),
  chatDeleteBookmark: (id: string, bid: string) => req<{ status: string }>(`/api/chat/channels/${id}/bookmarks/${bid}`, { method: "DELETE" }),
  bookmarkDownloadUrl: (id: string, bid: string) => mediaUrl(`/api/chat/channels/${id}/bookmarks/${bid}/download`),
  chatSetNotifPref: (id: string, pref: "all" | "mentions" | "none") => req<{ pref: string }>(`/api/chat/channels/${id}/notif`, { method: "POST", body: JSON.stringify({ pref }) }),
  chatSearchInChannel: (id: string, q: string) => req<{ results: ChatPin[] }>(`/api/chat/channels/${id}/search?q=${encodeURIComponent(q)}`),

  chatChannels: () => req<{ channels: ChatChannel[]; discover: ChatChannel[] }>("/api/chat/channels"),
  chatSchedule: (channelId: string, body: string, sendAt: string) => req<{ id: string }>(`/api/chat/channels/${channelId}/schedule`, { method: "POST", body: JSON.stringify({ body, send_at: sendAt }) }),
  chatScheduled: () => req<{ id: string; channel_id: string; channel_name: string; body: string; send_at: string }[]>("/api/chat/scheduled"),
  chatCancelScheduled: (id: string) => req<{ status: string }>(`/api/chat/scheduled/${id}`, { method: "DELETE" }),
  chatRemind: (messageId: string, remindAt: string, note?: string) => req<{ id: string }>(`/api/chat/messages/${messageId}/remind`, { method: "POST", body: JSON.stringify({ remind_at: remindAt, note: note ?? "" }) }),
  chatEmojis: () => req<{ id: string; name: string; file_id: string }[]>("/api/chat/emojis"),
  chatCreateEmoji: (name: string, fileId: string) => req<{ id: string }>("/api/chat/emojis", { method: "POST", body: JSON.stringify({ name, file_id: fileId }) }),
  chatDeleteEmoji: (id: string) => req<{ status: string }>(`/api/chat/emojis/${id}`, { method: "DELETE" }),
  chatCreateChannel: (opts: { name: string; topic: string; kind: "public" | "private"; listed: boolean; add_policy: "open" | "restricted" }) =>
    req<ChatChannel>("/api/chat/channels", { method: "POST", body: JSON.stringify(opts) }),
  chatUpdateChannel: (id: string, opts: { name?: string; topic?: string; listed?: boolean; add_policy?: "open" | "restricted" }) =>
    req<{ status: string }>(`/api/chat/channels/${id}`, { method: "PATCH", body: JSON.stringify(opts) }),
  chatAddMember: (id: string, email: string) => req<{ status: string }>(`/api/chat/channels/${id}/members`, { method: "POST", body: JSON.stringify({ email }) }),
  chatRemoveMember: (id: string, userId: string) => req<{ status: string }>(`/api/chat/channels/${id}/members/${userId}`, { method: "DELETE" }),
  chatSetMemberRole: (id: string, userId: string, role: "manager" | "member") => req<{ status: string }>(`/api/chat/channels/${id}/members/${userId}`, { method: "PATCH", body: JSON.stringify({ role }) }),
  chatJoin: (id: string) => req<{ status: string }>(`/api/chat/channels/${id}/join`, { method: "POST" }),
  chatOpenDM: (email: string) => req<ChatChannel>("/api/chat/dm", { method: "POST", body: JSON.stringify({ email }) }),
  chatOpenGroupDM: (emails: string[]) => req<ChatChannel>("/api/chat/group-dm", { method: "POST", body: JSON.stringify({ emails }) }),
  chatMembers: (id: string) => req<ChatMember[]>(`/api/chat/channels/${id}/members`),
  chatWebhooks: (id: string) => req<ChatWebhook[]>(`/api/chat/channels/${id}/webhooks`),
  chatCreateWebhook: (id: string, name: string) => req<ChatWebhook>(`/api/chat/channels/${id}/webhooks`, { method: "POST", body: JSON.stringify({ name }) }),
  chatDeleteWebhook: (id: string) => req<{ status: string }>(`/api/chat/webhooks/${id}`, { method: "DELETE" }),
  chatMessages: (id: string, before?: string) =>
    req<{ messages: ChatMessage[]; has_more: boolean }>(`/api/chat/channels/${id}/messages${before ? `?before=${encodeURIComponent(before)}` : ""}`),
  chatMessagesAround: (id: string, msgId: string) =>
    req<{ messages: ChatMessage[]; has_more: boolean }>(`/api/chat/channels/${id}/messages?around=${encodeURIComponent(msgId)}`),
  chatReplies: (msgId: string) => req<{ replies: ChatMessage[] }>(`/api/chat/messages/${msgId}/replies`),
  chatSend: (id: string, body: string, parent_id = "", mentions: string[] = [], attachments: ChatAttachment[] = []) =>
    req<ChatMessage>(`/api/chat/channels/${id}/messages`, { method: "POST", body: JSON.stringify({ body, parent_id, mentions, attachments }) }),
  chatAttachmentUrl: (fileId: string, inline = false) => mediaUrl(`/api/chat/files/${fileId}/download${inline ? "?inline=1" : ""}`),
  chatPresence: () => req<{ online: string[]; presence?: Record<string, string> }>("/api/chat/presence"),
  chatSearch: (q: string) => req<{ results: ChatSearchHit[] }>(`/api/chat/search?q=${encodeURIComponent(q)}`),
  chatEdit: (msgId: string, body: string) => req<{ id: string; body: string; edited_at: string }>(`/api/chat/messages/${msgId}`, { method: "PATCH", body: JSON.stringify({ body }) }),
  chatDelete: (msgId: string) => req<{ status: string }>(`/api/chat/messages/${msgId}`, { method: "DELETE" }),
  // Archive/restore a channel (channel owner or organization admin). Archived leaves the lists and
  // becomes read-only; the history stays intact -- unlike deleting.
  chatArchiveChannel: (id: string, archived: boolean) =>
    req<{ id: string; archived: boolean }>(`/api/chat/channels/${id}/archive`, { method: "POST", body: JSON.stringify({ archived }) }),
  chatArchivedChannels: () => req<{ channels: ArchivedChannel[] }>("/api/chat/channels/archived"),
  chatDeleteChannel: (id: string) => req<{ status: string }>(`/api/chat/channels/${id}`, { method: "DELETE" }),
  chatMarkUnread: (msgId: string) => req<{ status: string }>(`/api/chat/messages/${msgId}/unread`, { method: "POST" }),
  chatForward: (msgId: string, channelId: string) => req<ChatMessage>(`/api/chat/messages/${msgId}/forward`, { method: "POST", body: JSON.stringify({ channel_id: channelId }) }),
  chatSave: (msgId: string) => req<{ status: string }>(`/api/chat/messages/${msgId}/save`, { method: "POST" }),
  chatUnsave: (msgId: string) => req<{ status: string }>(`/api/chat/messages/${msgId}/save`, { method: "DELETE" }),
  chatSaved: () => req<{ saved: ChatSavedItem[] }>("/api/chat/saved"),
  chatUnfurl: (url: string) => req<{ unfurl: UnfurlData | null }>(`/api/chat/unfurl?url=${encodeURIComponent(url)}`),
  chatReact: (msgId: string, emoji: string, remove = false) => req<{ status: string }>(`/api/chat/messages/${msgId}/react`, { method: "POST", body: JSON.stringify({ emoji, remove }) }),
  chatMarkRead: (id: string) => req<{ status: string }>(`/api/chat/channels/${id}/read`, { method: "POST" }),
};
