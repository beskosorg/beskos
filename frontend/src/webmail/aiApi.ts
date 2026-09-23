// AI endpoints (summarize, draft, extract tasks, briefing). Extracted from backend.ts (law 1.4).
import { req } from "./backend";

export interface Task { title: string; due: string; assignee: string }

export const aiApi = {
  summarize: (subject: string, body: string) =>
    req<{ summary: string }>("/api/ai/summarize", { method: "POST", body: JSON.stringify({ subject, body }) }),
  draft: (original: string, instruction: string, tone: string) =>
    req<{ draft: string }>("/api/ai/draft", { method: "POST", body: JSON.stringify({ original, instruction, tone }) }),
  planTask: (idea: string) =>
    req<{ title: string; description: string }>("/api/ai/task", { method: "POST", body: JSON.stringify({ idea }) }),
  extractTasks: (subject: string, body: string) =>
    req<{ tasks: Task[] }>("/api/ai/extract-tasks", { method: "POST", body: JSON.stringify({ subject, body }) }),
  briefing: (input: { date: string; events: string[]; tasks: string[]; emails: string[]; period?: string; tomorrow?: string[] }) =>
    req<{ briefing: string }>("/api/ai/briefing", { method: "POST", body: JSON.stringify(input) }),
};
