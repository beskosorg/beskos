// The Documents app's one call. Its own file because backend.ts is already several hundred lines
// past the limit (law 1.4), and adding to it is how a file nobody dares split keeps growing.

import { req } from "./backend";

export type RecentDoc = {
  id: string; name: string; size: number; version: number;
  updated_at: string; folder_id?: string | null; starred: boolean; owned: boolean;
};

// A view over the same Drive files, ordered by what was worked on last.
export const recentDocuments = () => req<RecentDoc[]>("/api/docs/recent");
