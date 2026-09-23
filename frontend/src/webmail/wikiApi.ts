// Wiki / knowledge base endpoints. Extracted from backend.ts (law 1.4 -- 600 lines).
import { mediaUrl } from "../apiBase";
import { req } from "./backend";

export interface WikiSpace {
  id: string; tenant_id: string; key: string; name: string; description: string; icon: string;
  owner_user_id: string; visibility: "restricted" | "org" | "public"; public_slug: string | null;
  ai_indexed: boolean; created_at: string; updated_at: string;
}
export interface WikiPage {
  id: string; tenant_id: string; space_id: string; parent_id: string | null; title: string; slug: string;
  body_json: unknown; body_text: string; body_html: string; position: number;
  status: "draft" | "published"; author_user_id: string; version: number; is_template?: boolean;
  created_at: string; updated_at: string;
}
export interface WikiTreeNode {
  id: string; parent_id: string | null; title: string; slug: string;
  position: number; status: "draft" | "published"; children: WikiTreeNode[];
}
export interface WikiPageGrant {
  id: string; page_id: string; subject_type: "user" | "group" | "org"; subject_id: string | null; role: string; created_at: string;
}
export interface WikiGrant {
  id: string; space_id: string; subject_type: "user" | "group" | "org"; subject_id: string | null; role: string; created_at: string;
}
export interface WikiVersion {
  id: string; page_id: string; version: number; title: string; body_text: string;
  author_user_id: string | null; created_at: string;
}
export interface WikiVersionFull extends WikiVersion { body_json: unknown }
export interface WikiHome { tenant_id: string; body_json: unknown; body_text: string; body_html: string; updated_by: string | null; updated_at: string }
export interface PublicWikiSpace { space: { name: string; icon: string; description: string }; pages: WikiTreeNode[] }
export interface PublicWikiPage { title: string; slug: string; body_html: string; updated_at: string }
export interface WikiAttachment { id: string; file_id: string; name: string; size: number; created_at: string }
export interface WikiComment {
  id: string; page_id: string; author_user_id: string; body: string; body_html?: string; created_at: string;
  author_name: string; author_email: string;
}
// role: user's effective rank (1 viewer .. 4 manager) returned alongside the space/page.
export interface WikiSpaceInput { key?: string; name: string; description?: string; icon?: string; visibility?: string; ai_indexed?: boolean }
export interface WikiPageInput { parent_id?: string | null; title: string; body_json?: unknown; status?: string; expected_version?: number }

export const wikiApi = {
  listSpaces: () => req<WikiSpace[]>("/api/wiki/spaces"),
  createSpace: (in_: WikiSpaceInput) => req<WikiSpace>("/api/wiki/spaces", { method: "POST", body: JSON.stringify(in_) }),
  getSpace: (id: string) => req<{ space: WikiSpace; role: number }>(`/api/wiki/spaces/${id}`),
  updateSpace: (id: string, in_: WikiSpaceInput) => req<WikiSpace>(`/api/wiki/spaces/${id}`, { method: "PATCH", body: JSON.stringify(in_) }),
  deleteSpace: (id: string) => req<{ status: string }>(`/api/wiki/spaces/${id}`, { method: "DELETE" }),
  tree: (spaceId: string) => req<WikiTreeNode[]>(`/api/wiki/spaces/${spaceId}/tree`),
  listGrants: (spaceId: string) => req<WikiGrant[]>(`/api/wiki/spaces/${spaceId}/grants`),
  setGrant: (spaceId: string, g: { subject_type: string; subject_id?: string | null; role: string }) =>
    req<WikiGrant>(`/api/wiki/spaces/${spaceId}/grants`, { method: "PUT", body: JSON.stringify(g) }),
  removeGrant: (spaceId: string, grantId: string) => req<{ status: string }>(`/api/wiki/spaces/${spaceId}/grants/${grantId}`, { method: "DELETE" }),
  setPublic: (spaceId: string, enabled: boolean, regenerate = false) => req<WikiSpace>(`/api/wiki/spaces/${spaceId}/public`, { method: "POST", body: JSON.stringify({ enabled, regenerate }) }),
  templates: (spaceId: string) => req<{ id: string; title: string }[]>(`/api/wiki/spaces/${spaceId}/templates`),
  setTemplate: (pageId: string, is_template: boolean) => req<WikiPage>(`/api/wiki/pages/${pageId}/template`, { method: "POST", body: JSON.stringify({ is_template }) }),
  home: () => req<WikiHome>("/api/wiki/home"),
  setHome: (body_json: unknown) => req<WikiHome>("/api/wiki/home", { method: "PUT", body: JSON.stringify({ body_json }) }),
  pageGrants: (pageId: string) => req<WikiPageGrant[]>(`/api/wiki/pages/${pageId}/grants`),
  setPageGrant: (pageId: string, g: { subject_type: string; subject_id?: string | null; role: string }) =>
    req<WikiPageGrant>(`/api/wiki/pages/${pageId}/grants`, { method: "PUT", body: JSON.stringify(g) }),
  removePageGrant: (pageId: string, grantId: string) => req<{ status: string }>(`/api/wiki/pages/${pageId}/grants/${grantId}`, { method: "DELETE" }),
  createPage: (spaceId: string, in_: WikiPageInput) => req<WikiPage>(`/api/wiki/spaces/${spaceId}/pages`, { method: "POST", body: JSON.stringify(in_) }),
  getPage: (id: string) => req<{ page: WikiPage; role: number }>(`/api/wiki/pages/${id}`),
  updatePage: (id: string, in_: WikiPageInput) => req<WikiPage>(`/api/wiki/pages/${id}`, { method: "PATCH", body: JSON.stringify(in_) }),
  deletePage: (id: string) => req<{ status: string }>(`/api/wiki/pages/${id}`, { method: "DELETE" }),
  movePage: (id: string, parent_id: string | null, position: number) =>
    req<{ status: string }>(`/api/wiki/pages/${id}/move`, { method: "POST", body: JSON.stringify({ parent_id, position }) }),
  versions: (id: string) => req<WikiVersion[]>(`/api/wiki/pages/${id}/versions`),
  getVersion: (id: string, version: number) => req<WikiVersionFull>(`/api/wiki/pages/${id}/versions/${version}`),
  restoreVersion: (id: string, version: number, expectedVersion: number) =>
    req<WikiPage>(`/api/wiki/pages/${id}/versions/${version}/restore`, { method: "POST", body: JSON.stringify({ expected_version: expectedVersion }) }),
  comments: (id: string) => req<WikiComment[]>(`/api/wiki/pages/${id}/comments`),
  addComment: (id: string, body_json: unknown) => req<{ id: string }>(`/api/wiki/pages/${id}/comments`, { method: "POST", body: JSON.stringify({ body_json }) }),
  deleteComment: (id: string) => req<{ status: string }>(`/api/wiki/comments/${id}`, { method: "DELETE" }),
  attachments: (id: string) => req<WikiAttachment[]>(`/api/wiki/pages/${id}/attachments`),
  attachFile: (id: string, fileId: string) => req<WikiAttachment[]>(`/api/wiki/pages/${id}/attachments`, { method: "POST", body: JSON.stringify({ file_id: fileId }) }),
  detachAttachment: (id: string, attId: string) => req<{ status: string }>(`/api/wiki/pages/${id}/attachments/${attId}`, { method: "DELETE" }),
  attachmentDownloadUrl: (id: string, attId: string) => mediaUrl(`/api/wiki/pages/${id}/attachments/${attId}/download`),
};

export const publicWikiApi = {
  space: (slug: string) => req<PublicWikiSpace>(`/api/public/wiki/${encodeURIComponent(slug)}`),
  page: (slug: string, pageSlug: string) => req<PublicWikiPage>(`/api/public/wiki/${encodeURIComponent(slug)}/${encodeURIComponent(pageSlug)}`),
};
