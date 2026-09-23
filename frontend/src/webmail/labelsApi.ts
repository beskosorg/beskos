// Mail labels (keyword-backed tags). Extracted from backend.ts (law 1.4 -- 600 lines).
import { req } from "./backend";

export interface Label { id: string; name: string; color: string }
// A label is applied to the email as a JMAP keyword derived from its id (stable across renames).
// IMAP/JMAP keyword = atom without spaces; hence the id without hyphens, with its own prefix.
export const labelKeyword = (id: string) => "mailinlabel-" + id.replace(/-/g, "");

export const labelsApi = {
  labels: () => req<Label[]>("/api/labels"),
  createLabel: (name: string, color: string) =>
    req<Label>("/api/labels", { method: "POST", body: JSON.stringify({ name, color }) }),
  deleteLabel: (id: string) => req<{ status: string }>(`/api/labels/${id}`, { method: "DELETE" }),
};
