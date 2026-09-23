// API client for the agentic assistant. Lives apart from `backend.ts` for two reasons:
// `backend.ts` is already past 600 lines (law 1.4), and chat isn't JSON — it's an SSE stream,
// which needs its own path. Components still don't do raw `fetch` (law 3.4).

import { apiUrl, authHeaders } from "../../apiBase";
import { translateError } from "../backend";

// An action card: the record of the actual call, not a sentence claiming what was done (§3.3 of the design).
export interface ToolStep {
  tool: string;
  args: unknown;
  summary?: string;
  // error_code is an i18n KEY; raw server text is never shown (law 4.6).
  error_code?: string;
  // choices are quick replies (buttons) the tool offered — e.g. which contact "Bob".
  // Clicking one sends value as the message. Saves the user from typing the answer.
  choices?: { label: string; value: string }[];
}

export interface AssistantRun {
  id: string;
  agent: string;
  answer: string;
  steps: ToolStep[];
  stopped?: string;
}

export interface ConversationSummary {
  id: string;
  agent: string;
  title: string;
  updated_at: string;
}

export interface AgentProfile {
  key: string;
  // name only exists on customer profiles: a built-in agent's name is interface text and comes
  // from i18n via the key (law 4.1).
  name?: string;
  purpose?: string;
  instructions?: string;
  builtin: boolean;
  enabled: boolean;
  tools: string[];
}

// What's sent when creating or changing a customer profile.
export interface AgentDraft {
  name: string;
  purpose: string;
  instructions: string;
  tools: string[];
  // duplicate_of starts from a BUILT-IN agent. The copy is independent: it doesn't receive our
  // updates, and that's disclosed to whoever duplicates it (§3.2).
  duplicate_of?: string;
}

export interface StoredMessage {
  id: string;
  role: "user" | "assistant";
  // agent identifies who answered. Empty on user messages, and on assistant messages recorded
  // before this field existed.
  agent?: string;
  content: string;
  steps: ToolStep[];
  stopped?: string;
  created_at: string;
}

// Status of the 6h window quota (§9.2).
export interface QuotaStatus {
  limit: number;
  used: number;
  remaining: number;
  renews_at?: string;
}

// A recorded tool call (§6.4). The record of the actual call, readable by the user.
export interface AuditEntry {
  id: string;
  agent: string;
  tool: string;
  args: Record<string, unknown>;
  result?: string;
  error?: string;
  duration_ms: number;
  created_at: string;
}

// A fact stored in memory (§8). Readable text, not an opaque vector: the user has to be able to
// read what the app knows about them (§8.1).
export interface MemoryFact {
  id: string;
  fact: string;
  category: string;
  created_at: string;
  last_used_at?: string;
}

// What extraction saved and refused in a turn (§6.5). Refusals carry an i18n key.
export interface MemoryEvent {
  saved: { fact: string; category: string }[];
  refused: { fact: string; reason: string }[];
}

// A background task (§3.6). Survives reloading the page and restarting the server: the state
// lives in the database, not in this tab.
export interface AssistantTask {
  id: string;
  conversation_id: string;
  agent: string;
  message: string;
  // queued | running | awaiting_confirmation | done | failed | cancelled
  status: string;
  steps: ToolStep[];
  answer?: string;
  error_code?: string;
  stopped?: string;
  created_at: string;
  updated_at: string;
}

// A task action awaiting a decision. Same as a synchronous confirmation, but durable.
export interface PendingAction {
  id: string;
  task_id: string;
  tool: string;
  args: Record<string, unknown>;
}

// An action awaiting human approval (§6.3). `args` are the EXACT arguments that will run —
// the panel shows them as-is, not a summarized version.
export interface ConfirmRequest {
  id: string;
  tool: string;
  args: Record<string, unknown>;
}

// Thread events. `conversation` arrives first so a new conversation has an id right from the start:
// whoever closes the panel midway doesn't lose the thread, which is already recorded on the server.
export type ChatEvent =
  | { kind: "conversation"; conversation_id: string; title: string; agent: string; pinned_agent: string }
  | { kind: "delta"; text: string }
  | { kind: "step"; step: ToolStep }
  | { kind: "confirm"; confirm: ConfirmRequest }
  | { kind: "memory"; saved: MemoryEvent["saved"]; refused: MemoryEvent["refused"] }
  | { kind: "done"; run: AssistantRun }
  | { kind: "error"; error_code: string };

async function req<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch(apiUrl(path), {
    ...opts,
    credentials: "include",
    headers: { "Content-Type": "application/json", ...authHeaders(), ...(opts.headers || {}) },
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(translateError(body.error_code, body.error, res.status));
  }
  return res.json() as Promise<T>;
}

export const assistantApi = {
  agents: () => req<{ items: AgentProfile[]; max_tools: string[] }>("/api/assistant/agents"),
  agentCreate: (a: AgentDraft) =>
    req<AgentProfile>("/api/assistant/agents", { method: "POST", body: JSON.stringify(a) }),
  agentUpdate: (key: string, a: AgentDraft) =>
    req<AgentProfile>(`/api/assistant/agents/${key}`, { method: "PUT", body: JSON.stringify(a) }),
  agentDelete: (key: string) =>
    req<{ status: string }>(`/api/assistant/agents/${key}`, { method: "DELETE" }),
  // Turns a BUILT-IN agent on or off. Controls availability, never behavior (§3.2).
  // Tools disabled by the organization (a fine-grained ceiling over ALL agents).
  tools: () => req<{ tools: string[]; disabled: string[] }>("/api/assistant/tools"),
  toolEnable: (tool: string, enabled: boolean) =>
    req<{ tool: string; enabled: boolean }>(`/api/assistant/tools/${encodeURIComponent(tool)}`, { method: "POST", body: JSON.stringify({ enabled }) }),
  agentEnable: (key: string, enabled: boolean) =>
    req<{ key: string; enabled: boolean }>(`/api/assistant/agents/${key}/enabled`, {
      method: "POST", body: JSON.stringify({ enabled }),
    }),
  usage: () => req<QuotaStatus>("/api/assistant/usage"),
  audit: () => req<{ items: AuditEntry[] }>("/api/assistant/audit"),
  memory: () => req<{ items: MemoryFact[] }>("/api/assistant/memory"),
  memoryAdd: (fact: string, category: string) =>
    req<MemoryFact>("/api/assistant/memory", { method: "POST", body: JSON.stringify({ fact, category }) }),
  memoryDelete: (id: string) =>
    req<{ status: string }>(`/api/assistant/memory/${id}`, { method: "DELETE" }),
  memoryClear: () =>
    req<{ status: string }>("/api/assistant/memory/clear", { method: "POST" }),
  tasks: () => req<{ items: AssistantTask[]; pending: PendingAction[] }>("/api/assistant/tasks"),
  taskCancel: (id: string) =>
    req<{ status: string }>(`/api/assistant/tasks/${id}/cancel`, { method: "POST" }),
  conversations: () => req<{ items: ConversationSummary[] }>("/api/assistant/conversations"),
  conversation: (id: string) =>
    req<{ id: string; agent: string; pinned_agent: string; title: string; messages: StoredMessage[] }>(
      `/api/assistant/conversations/${id}`,
    ),
  remove: (id: string) => req<{ status: string }>(`/api/assistant/conversations/${id}`, { method: "DELETE" }),

  // confirm approves or rejects ONE action. One per request: there's no batch approval (§6.3).
  confirm: (id: string, approve: boolean) =>
    req<{ status: string; approved: boolean }>(`/api/assistant/confirm/${id}`, {
      method: "POST",
      body: JSON.stringify({ approve }),
    }),

  // chat opens the SSE stream. EventSource can't be used: the request is POST and carries a body.
  // `signal` allows cancellation — closing the panel midway cuts the connection, and the server
  // still records whatever already ran.
  chat: streamChat,

  // background queues the request instead of running it within the HTTP request. Returns the
  // task card right away; progress shows up in the Tasks tab (§3.6).
  background: (body: { message: string; agent?: string; conversation_id?: string }) =>
    req<{ conversation_id: string; agent: string; task: AssistantTask }>("/api/assistant/chat", {
      method: "POST",
      body: JSON.stringify({ ...body, background: true }),
    }),
};

async function streamChat(
  body: { message: string; agent?: string; conversation_id?: string },
  onEvent: (ev: ChatEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(apiUrl("/api/assistant/chat"), {
    method: "POST",
    credentials: "include",
    signal,
    headers: { "Content-Type": "application/json", ...authHeaders() },
    body: JSON.stringify(body),
  });
  if (!res.ok || !res.body) {
    const err = await res.json().catch(() => ({}));
    throw new Error(translateError(err.error_code, err.error, res.status));
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    // SSE events are separated by a blank line. The last chunk stays in the buffer until complete:
    // processing half an event would produce truncated JSON on every read.
    let sep = buffer.indexOf("\n\n");
    while (sep !== -1) {
      const raw = buffer.slice(0, sep);
      buffer = buffer.slice(sep + 2);
      const ev = parseEvent(raw);
      if (ev) onEvent(ev);
      sep = buffer.indexOf("\n\n");
    }
  }
}

function parseEvent(raw: string): ChatEvent | null {
  const line = raw.split("\n").find((l) => l.startsWith("data:"));
  if (!line) return null;
  try {
    return JSON.parse(line.slice(5).trim()) as ChatEvent;
  } catch {
    return null; // a broken event doesn't throw out the whole thread
  }
}
