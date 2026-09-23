import { useCallback, useRef, useState } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { assistantApi, type ChatEvent, type ConfirmRequest, type ToolStep } from "./api";

// When the assistant writes to mail (sends, drafts), the webmail lists already in cache go
// stale — the sent email would keep showing up in Drafts until a refetch. We invalidate the
// affected queries as soon as the step runs, so the UI reflects the action right away.
// Each write tool maps to the query keys that become stale when it runs.
const INVALIDATE_ON: Record<string, string[]> = {
  "mail.send": ["emails", "mailboxes", "thread", "today-emails", "contacts"],
  "mail.draft": ["emails", "mailboxes", "thread", "today-emails"],
  "calendar.create": ["events", "meetings"],
  "tasks.create": ["boards", "board"],
};

function invalidateForStep(qc: QueryClient, step: ToolStep) {
  if (step.error_code) return; // action failed: nothing changed, nothing to refresh
  for (const key of INVALIDATE_ON[step.tool] ?? []) {
    void qc.invalidateQueries({ queryKey: [key] });
  }
}

// A message as the panel shows it. Same format for what came from the server and for what's
// arriving live — that way reopening a conversation shows exactly what was seen live.
export interface ChatMessage {
  role: "user" | "assistant";
  // agent is who answered — the routing choice is always visible (§3.2).
  agent?: string;
  content: string;
  // Facts saved/refused in this turn (§6.5). Shown in the conversation: memory that grows
  // unseen is memory no one can correct (§8.2).
  savedFacts?: { fact: string }[];
  refusedFacts?: { fact: string; reason: string }[];
  steps: ToolStep[];
  stopped?: string;
  // errorCode is a request failure, not a model one. i18n key (law 4.6).
  errorCode?: string;
}

// useAssistant holds the state of the open conversation and handles the SSE thread.
//
// Lives in the component mounted once in App (outside <main>), so it survives navigation:
// switching pages doesn't lose what was being asked.
export function useAssistant() {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [conversationId, setConversationId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // The action awaiting a decision. One at a time, by construction: execution PAUSES on the
  // server until there's a response, so a second one never arrives before this one is decided (§6.3).
  const [pending, setPending] = useState<ConfirmRequest | null>(null);
  // pinned empty = routing decides on every turn. Pinning holds for the rest of the thread, and
  // is reversible: the user always outranks the model.
  const [pinned, setPinned] = useState("");
  const abortRef = useRef<AbortController | null>(null);
  const qc = useQueryClient();

  // patchLast changes the last message (the assistant's, which is being written live).
  const patchLast = useCallback((fn: (m: ChatMessage) => ChatMessage) => {
    setMessages((prev) => (prev.length === 0 ? prev : [...prev.slice(0, -1), fn(prev[prev.length - 1])]));
  }, []);

  const send = useCallback(async (text: string) => {
    const message = text.trim();
    if (!message || busy) return;
    setBusy(true);
    setMessages((prev) => [
      ...prev,
      { role: "user", content: message, steps: [] },
      { role: "assistant", content: "", steps: [] },
    ]);

    const ctrl = new AbortController();
    abortRef.current = ctrl;
    try {
      await assistantApi.chat(
        { message, agent: pinned || undefined, conversation_id: conversationId ?? undefined },
        (ev: ChatEvent) => {
          switch (ev.kind) {
            case "conversation":
              setConversationId(ev.conversation_id);
              // Who's going to answer, visible before any tool runs.
              patchLast((m) => ({ ...m, agent: ev.agent }));
              break;
            case "delta":
              patchLast((m) => ({ ...m, content: m.content + ev.text }));
              break;
            case "step":
              // The action card arriving means the action has already been decided and run.
              setPending(null);
              patchLast((m) => ({ ...m, steps: [...m.steps, ev.step] }));
              // The action already changed data on the server: refresh the affected webmail lists.
              invalidateForStep(qc, ev.step);
              break;
            case "confirm":
              setPending(ev.confirm);
              break;
            case "memory":
              patchLast((m) => ({ ...m, savedFacts: ev.saved, refusedFacts: ev.refused }));
              break;
            case "done":
              setPending(null);
              // The final run wins: deltas may have been lost along the way, the server has the truth.
              patchLast((m) => ({ ...m, content: ev.run.answer || m.content, steps: ev.run.steps ?? m.steps, stopped: ev.run.stopped }));
              break;
            case "error":
              patchLast((m) => ({ ...m, errorCode: ev.error_code }));
              break;
          }
        },
        ctrl.signal,
      );
    } catch {
      // Cancelling isn't failing: whoever closed the panel doesn't need to see an error.
      if (!ctrl.signal.aborted) patchLast((m) => ({ ...m, errorCode: "assistant.failed" }));
    } finally {
      abortRef.current = null;
      setPending(null);
      setBusy(false);
    }
  }, [busy, conversationId, patchLast, pinned, qc]);

  // sendBackground queues the request on the server. It doesn't return text: it returns a task,
  // and progress shows up in the Tasks tab from then on (§3.6).
  const sendBackground = useCallback(async (text: string) => {
    const message = text.trim();
    if (!message) return;
    const res = await assistantApi.background({
      message, agent: pinned || undefined, conversation_id: conversationId ?? undefined,
    });
    setConversationId(res.conversation_id);
    setMessages((prev) => [...prev, { role: "user", content: message, steps: [] }]);
  }, [conversationId, pinned]);

  // stop only cuts the client-side stream. Work already done stays recorded on the server — this
  // does not cancel execution on the backend, it only stops listening to it. True cancellation is
  // not implemented yet.
  const stop = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    setPending(null);
    setBusy(false);
  }, []);

  const reset = useCallback(() => {
    stop();
    setMessages([]);
    setConversationId(null);
    setPinned("");
  }, [stop]);

  // open loads a conversation from history along with its action cards.
  const open = useCallback(async (id: string) => {
    stop();
    const conv = await assistantApi.conversation(id);
    setConversationId(conv.id);
    setPinned(conv.pinned_agent ?? "");
    setMessages(conv.messages.map((m) => ({
      role: m.role, agent: m.agent, content: m.content, steps: m.steps ?? [], stopped: m.stopped,
    })));
  }, [stop]);

  return { messages, conversationId, busy, pending, pinned, setPinned, send, sendBackground, stop, reset, open };
}
