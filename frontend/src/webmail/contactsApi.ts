// Contacts (CRM) endpoints. Extracted from backend.ts (law 1.4 -- 600 lines).
import { apiUrl, authHeaders, mediaUrl } from "../apiBase";
import { req, translateError } from "./backend";

export type ContactKind = "company" | "person";
export interface Contact {
  id: string; kind: ContactKind; name: string; email: string; phone: string; company: string;
  domain: string; notes: string; tags: string[]; group_ids: string[]; created_at: string;
  title: string; mobile: string; is_whatsapp: boolean; allow_phone: boolean; allow_email: boolean;
  company_id: string | null; company_name?: string | null; owner_user_id: string;
  extra_emails: string[]; extra_phones: string[]; // secondary emails/phones
  custom_fields?: Record<string, string>; // free-form fields (label -> value)
  avatar_file_id?: string | null;          // photo: points to a storage_files row (see /thumb)
}

export interface CrmNote { id: string; body: string; created_at: string; author_name: string; author_email: string }

export interface CrmTask { id: string; title: string; description: string; assignee: string; due: string; column_id: string; board_id: string; board_name: string; column_name: string }
export interface NamedRef { id: string; name: string }
export interface ContactGroup { id: string; name: string; color: string }
// What the form sends: `id`, `created_at`, `owner_user_id` and `company_name` come from the server.
export type ContactInput = Omit<Contact, "id" | "created_at" | "owner_user_id" | "company_name">;

// Contacts: company or person, in a single list. Viewing is governed by GROUP (shared with teams);
// editing is owner-only. Both rules live in the backend -- this is just transport.
export const contactsApi = {
  list: () => req<Contact[]>("/api/contacts"),
  get: (id: string) => req<{ contact: Contact; group_ids: string[]; people: Contact[]; is_owner: boolean }>(`/api/contacts/${id}`),
  create: (c: ContactInput) => req<Contact>("/api/contacts", { method: "POST", body: JSON.stringify(c) }),
  // Records recipients of a send as contacts, to suggest them next time. Idempotent.
  seen: (recipients: string[]) => req<{ status: string }>("/api/contacts/seen", { method: "POST", body: JSON.stringify({ recipients }) }),
  update: (id: string, c: ContactInput) => req<Contact>(`/api/contacts/${id}`, { method: "PATCH", body: JSON.stringify(c) }),
  remove: (id: string) => req<{ status: string }>(`/api/contacts/${id}`, { method: "DELETE" }),
  editNote: (contactId: string, noteId: string, body: string) => req<CrmNote>(`/api/contacts/${contactId}/notes/${noteId}`, { method: "PATCH", body: JSON.stringify({ body }) }),
  transfer: (id: string, userId: string) => req<Contact>(`/api/contacts/${id}/transfer`, { method: "POST", body: JSON.stringify({ user_id: userId }) }),
  merge: (keepId: string, mergeId: string) => req<Contact>(`/api/contacts/${keepId}/merge`, { method: "POST", body: JSON.stringify({ merge_id: mergeId }) }),
  enrich: () => req<{ linked: number }>("/api/contacts/enrich", { method: "POST" }),
  setAvatar: (id: string, fileId: string) => req<Contact>(`/api/contacts/${id}/avatar`, { method: "POST", body: JSON.stringify({ file_id: fileId }) }),
  notes: (id: string) => req<CrmNote[]>(`/api/contacts/${id}/notes`),
  addNote: (id: string, body: string) => req<CrmNote>(`/api/contacts/${id}/notes`, { method: "POST", body: JSON.stringify({ body }) }),
  tasks: (id: string) => req<CrmTask[]>(`/api/contacts/${id}/tasks`),
  groups: () => req<ContactGroup[]>("/api/contact-groups"),
  // Emails of a group (distribution list): write to the whole group at once.
  groupEmails: (id: string) => req<{ name: string; email: string }[]>(`/api/contact-groups/${id}/emails`),
  createGroup: (name: string, color: string) => req<ContactGroup>("/api/contact-groups", { method: "POST", body: JSON.stringify({ name, color }) }),
  updateGroup: (id: string, name: string, color: string) => req<ContactGroup>(`/api/contact-groups/${id}`, { method: "PATCH", body: JSON.stringify({ name, color }) }),
  deleteGroup: (id: string) => req<{ status: string }>(`/api/contact-groups/${id}`, { method: "DELETE" }),
  // Who the group is shared with: teams AND people.
  shares: (id: string) => req<{ teams: NamedRef[]; users: NamedRef[] }>(`/api/contact-groups/${id}/shares`),
  setShares: (id: string, userGroupIds: string[], userIds: string[]) =>
    req<{ teams: NamedRef[]; users: NamedRef[] }>(`/api/contact-groups/${id}/shares`, { method: "PUT", body: JSON.stringify({ user_group_ids: userGroupIds, user_ids: userIds }) }),
  userGroups: () => req<NamedRef[]>("/api/groups"),
  // Tenant teams for the share picker (does not require admin permission).
  shareTeams: () => req<NamedRef[]>("/api/contact-share-teams"),
  // Export/import the address book as vCard/CSV (universal formats).
  exportUrl: (format: "vcard" | "csv") => mediaUrl(`/api/contacts/export?format=${format}`),
  // A contact as a .vcf file, to attach to an email ("send contact"). Returns the File already
  // named from the server's Content-Disposition.
  vcardFile: async (id: string): Promise<File> => {
    const res = await fetch(apiUrl(`/api/contacts/${id}/vcard`), { credentials: "include", headers: authHeaders() });
    if (!res.ok) { const b = await res.json().catch(() => ({})); throw new Error(translateError(b.error_code, b.error, res.status)); }
    const cd = res.headers.get("Content-Disposition") || "";
    const m = /filename="?([^"]+)"?/.exec(cd);
    const name = m?.[1] || "contact.vcf";
    return new File([await res.blob()], name, { type: "text/vcard" });
  },
  importContacts: async (file: File): Promise<{ imported: number; skipped: number }> => {
    const fd = new FormData();
    fd.append("file", file);
    const res = await fetch(apiUrl("/api/contacts/import"), { method: "POST", credentials: "include", headers: authHeaders(), body: fd });
    if (!res.ok) { const b = await res.json().catch(() => ({})); throw new Error(translateError(b.error_code, b.error, res.status)); }
    return res.json();
  },
};
