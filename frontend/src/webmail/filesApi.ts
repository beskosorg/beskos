// Files (Drive) endpoints: folders, move, share, viewer helpers. Extracted from backend.ts (law 1.4).
import { apiUrl, authHeaders, mediaUrl } from "../apiBase";
import { req, translateError } from "./backend";

export interface Folder { id: string; name: string; parent_id: { Valid: boolean; Bytes?: string } | null; created_at: string }
export interface Breadcrumb { id: string; name: string }
export interface Quota { used: number; max: number }
export interface Owner { name: string; email: string }
export interface FilesListing {
  folders: Folder[]; files: StorageFile[]; breadcrumb: Breadcrumb[];
  shared: string[]; owner: Owner; quota: Quota;
}
export interface FilesSearch { folders: Folder[]; files: StorageFile[]; shared: string[]; owner: Owner }
export interface FileVersion { id: string; version: number; size: number; created_at: string; author: string }
export interface Share { id: string; principal: string; level: string }

// State of a file's public link. Empty token = no link.
export interface FileLinkInfo {
  token: string;
  has_password?: boolean;
  expires_at?: string;
  permission?: "view" | "edit";
  allow_download?: boolean;
}

export interface StorageFile {
  id: string; name: string; hash: string; size: number; version: number; created_at: string;
  deleted_at?: string | null; // set = in the trash
  starred?: boolean;
}

// Office extensions opened in the editor (OnlyOffice). Keep in sync with officeExts in the backend.
// txt stays in the (lightweight) text viewer, not the heavy editor.
const OFFICE_EXTS = new Set(["docx", "doc", "odt", "rtf", "xlsx", "xls", "ods", "csv", "pptx", "ppt", "odp"]);
export function isOfficeFile(name: string): boolean {
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
  return OFFICE_EXTS.has(ext);
}

// Viewable type in the browser (embedded viewer) -- otherwise it downloads.
export type ViewerKind = "image" | "pdf" | "audio" | "video" | "text";
const VIEWER: Record<string, ViewerKind> = {
  png: "image", jpg: "image", jpeg: "image", gif: "image", webp: "image", svg: "image", bmp: "image", heic: "image", avif: "image",
  pdf: "pdf",
  mp3: "audio", wav: "audio", flac: "audio", ogg: "audio", m4a: "audio", aac: "audio",
  mp4: "video", mov: "video", webm: "video", mkv: "video", avi: "video", m4v: "video",
  // text/code
  txt: "text", json: "text", xml: "text", md: "text", markdown: "text", log: "text",
  yml: "text", yaml: "text", ini: "text", toml: "text", env: "text",
  js: "text", ts: "text", tsx: "text", jsx: "text", go: "text", py: "text", rb: "text",
  java: "text", c: "text", cpp: "text", h: "text", cs: "text", php: "text", rs: "text",
  css: "text", scss: "text", html: "text", htm: "text", sh: "text", sql: "text",
};
export function viewerKind(name: string): ViewerKind | null {
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
  return VIEWER[ext] ?? null;
}
// Reads a file's text content (for the text viewer).
export async function fetchText(id: string): Promise<string> {
  const res = await fetch(inlineUrl(id), { credentials: "include", headers: authHeaders() });
  if (!res.ok) throw new Error(translateError(undefined, undefined, res.status));
  return res.text();
}
export function inlineUrl(id: string): string { return mediaUrl(`/api/files/${id}/download?inline=1`); }

export const filesApi = {
  files: (folderId?: string) => req<FilesListing>(`/api/files${folderId ? `?folder=${folderId}` : ""}`),
  // Global (recursive) search by name, across all folders -- not just the open one.
  searchFiles: (q: string) => req<FilesSearch>(`/api/files/search?q=${encodeURIComponent(q)}`),
  uploadFile: async (file: File, folderId?: string): Promise<StorageFile> => {
    const fd = new FormData();
    fd.append("file", file);
    if (folderId) fd.append("folder_id", folderId);
    const res = await fetch(apiUrl("/api/files"), { method: "POST", credentials: "include", headers: authHeaders(), body: fd });
    if (!res.ok) { const b = await res.json().catch(() => ({})); throw new Error(translateError(b.error_code, b.error, res.status)); }
    return res.json();
  },
  // Upload with progress (via XHR -- fetch does not expose upload progress). onProgress: 0..1.
  uploadFileProgress: (file: File, folderId: string | undefined, onProgress: (pct: number) => void): Promise<StorageFile> =>
    new Promise((resolve, reject) => {
      const fd = new FormData();
      fd.append("file", file);
      if (folderId) fd.append("folder_id", folderId);
      const xhr = new XMLHttpRequest();
      xhr.open("POST", apiUrl("/api/files"));
      xhr.withCredentials = true;
      for (const [k, v] of Object.entries(authHeaders())) xhr.setRequestHeader(k, v);
      xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(e.loaded / e.total); };
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) { try { resolve(JSON.parse(xhr.responseText)); } catch { reject(new Error("bad response")); } }
        else { let b: { error_code?: string; error?: string } = {}; try { b = JSON.parse(xhr.responseText); } catch { /* non-JSON body */ } reject(new Error(translateError(b.error_code, b.error, xhr.status))); }
      };
      xhr.onerror = () => reject(new Error(translateError(undefined, undefined, 0)));
      xhr.send(fd);
    }),
  downloadFileUrl: (id: string) => mediaUrl(`/api/files/${id}/download`),
  deleteFile: (id: string) => req<{ status: string }>(`/api/files/${id}`, { method: "DELETE" }),
  trashFiles: () => req<FilesSearch>("/api/files/trash"),
  fileVersions: (id: string) => req<{ current_version: number; versions: FileVersion[] }>(`/api/files/${id}/versions`),
  restoreVersion: (id: string, versionId: string) => req<StorageFile>(`/api/files/${id}/versions/${versionId}/restore`, { method: "POST" }),
  versionDownloadUrl: (id: string, versionId: string) => mediaUrl(`/api/files/${id}/versions/${versionId}/download`),
  restoreFile: (id: string) => req<StorageFile>(`/api/files/${id}/restore`, { method: "POST" }),
  purgeFile: (id: string) => req<{ status: string }>(`/api/files/${id}/permanent`, { method: "DELETE" }),
  emptyTrash: () => req<{ status: string; count: number }>("/api/files/trash", { method: "DELETE" }),
  renameFile: (id: string, name: string) => req<unknown>(`/api/files/${id}`, { method: "PATCH", body: JSON.stringify({ name }) }),
  moveFile: (id: string, folderId: string | null) => req<unknown>(`/api/files/${id}`, { method: "PATCH", body: JSON.stringify({ folder_id: folderId ?? "" }) }),
  copyFile: (id: string, name?: string) => req<StorageFile>(`/api/files/${id}/copy`, { method: "POST", body: JSON.stringify({ name: name ?? "" }) }),
  starFile: (id: string, starred: boolean) => req<StorageFile>(`/api/files/${id}/star`, { method: "POST", body: JSON.stringify({ starred }) }),
  fileThumbUrl: (id: string) => mediaUrl(`/api/files/${id}/thumb`),
  folderZipUrl: (id: string) => mediaUrl(`/api/folders/${id}/zip`),
  batchFiles: (ids: string[], action: "trash" | "restore" | "move", folderId?: string) => req<{ done: number }>("/api/files/batch", { method: "POST", body: JSON.stringify({ ids, action, folder_id: folderId ?? "" }) }),
  createFolder: (name: string, parentId?: string) => req<Folder>("/api/folders", { method: "POST", body: JSON.stringify({ name, parent_id: parentId ?? "" }) }),
  renameFolder: (id: string, name: string) => req<Folder>(`/api/folders/${id}`, { method: "PATCH", body: JSON.stringify({ name }) }),
  moveFolder: (id: string, parentId: string | null) => req<Folder>(`/api/folders/${id}/move`, { method: "POST", body: JSON.stringify({ parent_id: parentId ?? "" }) }),
  deleteFolder: (id: string) => req<{ status: string }>(`/api/folders/${id}`, { method: "DELETE" }),
  shares: (id: string) => req<Share[]>(`/api/files/${id}/shares`),
  shareFile: (id: string, principal: string, level: string) =>
    req<Share & { external?: boolean; link?: string; emailed?: boolean }>(`/api/files/${id}/share`, { method: "POST", body: JSON.stringify({ principal, level }) }),
  removeShare: (fileId: string, shareId: string) => req<{ status: string }>(`/api/files/${fileId}/shares/${shareId}`, { method: "DELETE" }),
  // Public download link (large attachments). Returns the token; the absolute URL is built client-side.
  fileLink: (id: string, opts?: { expires_days?: number; password?: string; permission?: "view" | "edit"; allow_download?: boolean }) =>
    req<FileLinkInfo>(`/api/files/${id}/link`, { method: "POST", body: JSON.stringify(opts ?? {}) }),
  getFileLink: (id: string) => req<FileLinkInfo>(`/api/files/${id}/link`),
  revokeFileLink: (id: string) => req<{ status: string }>(`/api/files/${id}/link`, { method: "DELETE" }),
  shareRecipients: (id: string, emails: string[]) => req<{ shared: number }>(`/api/files/${id}/share-recipients`, { method: "POST", body: JSON.stringify({ emails }) }),
  // The user's teams (to share a file with the whole group).
  myGroups: () => req<{ id: string; name: string }[]>("/api/my-groups"),
  shareWithGroup: (id: string, groupId: string, level: string) => req<{ shared: number }>(`/api/files/${id}/share-group`, { method: "POST", body: JSON.stringify({ group_id: groupId, level }) }),
  sharedWithMe: () => req<{ files: StorageFile[] }>("/api/files-shared"),
  fileLimits: () => req<{ max_upload_bytes: number }>("/api/files/limits"),
  editSession: (id: string, lang: string, theme: string) => req<EditSession>(`/api/files/${id}/edit-session?lang=${encodeURIComponent(lang)}&theme=${encodeURIComponent(theme)}`, { method: "POST" }),
  // Config for the OnlyOffice AI plugin (DeepSeek proxy): base URL + "API key" (user token) + model,
  // pasted once into Plugins -> AI. The DeepSeek key itself stays server-side.
  aiEditorKey: () => req<{ base_url: string; api_key: string; model: string }>("/api/ai/editor-key"),
  createOffice: (kind: "document" | "spreadsheet" | "presentation", name: string, folderId?: string) =>
    req<StorageFile>("/api/files/office", { method: "POST", body: JSON.stringify({ kind, name, folder_id: folderId ?? "" }) }),
};

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export interface EditSession { apiJs: string; config: Record<string, any>; canWrite: boolean; name: string }
