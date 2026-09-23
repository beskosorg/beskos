// Kanban (boards, columns, cards) endpoints. Extracted from backend.ts (law 1.4 -- 600 lines).
import { req } from "./backend";

export interface Column { id: string; name: string; color: string; position: number; wip_limit?: number }
export interface BoardAssignee { card_id: string; user_id: string; name: string }
export interface BoardDep { card_id: string; depends_on_id: string; dep_title: string; dep_column: string }
export interface Automation { id: string; board_id: string; trigger_column_id: string; action: string; action_value: string }
export interface Card {
  id: string; column_id: string; title: string; description: string;
  label: string; due: string; priority: string; source_email_id: string; position: number;
  contact_id: string | null; start_date?: string;
  assignee_id: string | null;
  // assignee_name: name of the responsible user -- or the legacy text for someone who was never a
  // user (migration 00026). `assignee` only survives in that case; the UI always writes assignee_id.
  assignee: string; assignee_name: string | null; comment_count?: number; color?: string;
  number: number; card_key?: string; // "QUA-12" -- board prefix + number; derived, not stored
}

export interface CardComment {
  id: string; card_id: string; author_id: string; author_name: string; author_email: string;
  parent_id: string | null; body: string; mentions: string[]; created_at: string;
}
export interface Board { id: string; name: string; prefix?: string; owner_user_id?: string | null }
export interface BoardGrant { id: string; board_id: string; subject_type: "user" | "group" | "org"; subject_id: string | null; role: string; created_at: string }
export interface BoardDetail { board: Board; columns: { column: Column; cards: Card[] }[]; assignees?: BoardAssignee[]; deps?: BoardDep[] }
// Task returned by search (/task in chat). card_key comes from the server as "PREFIX-N".
export interface CardHit { id: string; board_id: string; title: string; label: string; due: string; priority: string; card_key: string; board_name: string; column_name: string; assignee_name: string }

export const kanbanApi = {
  boards: () => req<Board[]>("/api/boards"),
  boardAccess: (id: string) => req<{ role: number }>(`/api/boards/${id}/access`),
  boardGrants: (id: string) => req<BoardGrant[]>(`/api/boards/${id}/grants`),
  setBoardGrant: (id: string, g: { subject_type: string; subject_id?: string | null; role: string }) =>
    req<BoardGrant>(`/api/boards/${id}/grants`, { method: "PUT", body: JSON.stringify(g) }),
  removeBoardGrant: (id: string, grantId: string) => req<{ status: string }>(`/api/boards/${id}/grants/${grantId}`, { method: "DELETE" }),
  board: (id: string) => req<BoardDetail>(`/api/boards/${id}`),
  createBoard: (name: string) => req<Board>("/api/boards", { method: "POST", body: JSON.stringify({ name }) }),
  updateBoard: (id: string, name: string) => req<Board>(`/api/boards/${id}`, { method: "PATCH", body: JSON.stringify({ name }) }),
  deleteBoard: (id: string) => req<{ status: string }>(`/api/boards/${id}`, { method: "DELETE" }),
  // Task search by key (QUA-12), number, or title -- only in the boards the person can see.
  searchCards: (q: string) => req<{ cards: CardHit[] }>(`/api/cards/search?q=${encodeURIComponent(q)}`),
  createCard: (boardId: string, c: Partial<Card> & { column_id: string; title: string }) =>
    req<Card>(`/api/boards/${boardId}/cards`, { method: "POST", body: JSON.stringify(c) }),
  moveCard: (cardId: string, columnId: string, position: number) =>
    req<Card>(`/api/cards/${cardId}/move`, { method: "PATCH", body: JSON.stringify({ column_id: columnId, position }) }),
  // Reorders (and moves into) a column: card_ids is the final order. Persists the vertical drag position.
  reorderColumn: (columnId: string, cardIds: string[]) =>
    req<{ status: string }>(`/api/columns/${columnId}/reorder`, { method: "POST", body: JSON.stringify({ card_ids: cardIds }) }),
  reorderColumns: (boardId: string, columnIds: string[]) =>
    req<{ status: string }>(`/api/boards/${boardId}/columns/reorder`, { method: "POST", body: JSON.stringify({ column_ids: columnIds }) }),
  deleteCard: (cardId: string) => req<{ status: string }>(`/api/cards/${cardId}`, { method: "DELETE" }),
  archiveCard: (cardId: string, archived: boolean) => req<Card>(`/api/cards/${cardId}/archive`, { method: "POST", body: JSON.stringify({ archived }) }),
  archiveBoard: (boardId: string, archived: boolean) => req<Board>(`/api/boards/${boardId}/archive`, { method: "POST", body: JSON.stringify({ archived }) }),
  automations: (boardId: string) => req<Automation[]>(`/api/boards/${boardId}/automations`),
  createAutomation: (boardId: string, a: { trigger_column_id: string; action: string; action_value: string }) => req<Automation>(`/api/boards/${boardId}/automations`, { method: "POST", body: JSON.stringify(a) }),
  deleteAutomation: (id: string) => req<{ status: string }>(`/api/automations/${id}`, { method: "DELETE" }),
  // contact_id is optional on purpose: omitting keeps the contact linked; "" disconnects it.
  updateCard: (cardId: string, c: Pick<Card, "title" | "description" | "label" | "due"> & { assignee_id?: string; contact_id?: string; priority?: string; start_date?: string; color?: string }) =>
    req<Card>(`/api/cards/${cardId}`, { method: "PATCH", body: JSON.stringify(c) }),
  addCardAssignee: (cardId: string, userId: string) => req<{ status: string }>(`/api/cards/${cardId}/assignees`, { method: "POST", body: JSON.stringify({ user_id: userId }) }),
  removeCardAssignee: (cardId: string, userId: string) => req<{ status: string }>(`/api/cards/${cardId}/assignees/${userId}`, { method: "DELETE" }),
  addCardDep: (cardId: string, dependsOnId: string) => req<{ status: string }>(`/api/cards/${cardId}/deps`, { method: "POST", body: JSON.stringify({ depends_on_id: dependsOnId }) }),
  removeCardDep: (cardId: string, depId: string) => req<{ status: string }>(`/api/cards/${cardId}/deps/${depId}`, { method: "DELETE" }),
  setBoardPrefix: (boardId: string, prefix: string) =>
    req<Board>(`/api/boards/${boardId}/prefix`, { method: "PATCH", body: JSON.stringify({ prefix }) }),
  createColumn: (boardId: string, name: string, color: string) =>
    req<Column>(`/api/boards/${boardId}/columns`, { method: "POST", body: JSON.stringify({ name, color }) }),
  updateColumn: (columnId: string, name: string, color: string, wipLimit = 0) =>
    req<Column>(`/api/columns/${columnId}`, { method: "PATCH", body: JSON.stringify({ name, color, wip_limit: wipLimit }) }),
  deleteColumn: (columnId: string) => req<{ status: string }>(`/api/columns/${columnId}`, { method: "DELETE" }),
};
