// Announcements endpoints. Extracted from backend.ts (law 1.4 -- 600 lines).
import { req } from "./backend";

export interface Announcement { id: string; title: string; body: string; author: string; published: boolean; created_at: string }

export const announcementsApi = {
  announcements: (q = "", page = 1) => req<{ items: Announcement[]; total: number; page: number; can_manage: boolean }>(`/api/announcements?q=${encodeURIComponent(q)}&page=${page}`),
  announceCreate: (title: string, body: string, published: boolean) => req<{ id: string }>("/api/announcements", { method: "POST", body: JSON.stringify({ title, body, published }) }),
  announceSetPublished: (id: string, published: boolean) => req<{ status: string }>(`/api/announcements/${id}`, { method: "PATCH", body: JSON.stringify({ published }) }),
  announceDelete: (id: string) => req<{ status: string }>(`/api/announcements/${id}`, { method: "DELETE" }),
};
