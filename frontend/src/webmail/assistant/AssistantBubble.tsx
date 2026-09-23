import { useState } from "react";
import { useTranslation } from "react-i18next";
import { AssistantPanel } from "./AssistantPanel";
import { useAssistant } from "./useAssistant";
import { useAssistantTasks } from "./TasksPanel";

// Assistant bubble: round button fixed to the bottom right, above everything else (§3.1).
//
// Mounted ONCE in App, outside <main>: state survives navigation, so switching pages mid-question
// doesn't lose the conversation. The visibility rule belongs to whoever mounts it — this
// component doesn't know what page it's on, and shouldn't.
export function AssistantBubble({ hidden = false }: { hidden?: boolean }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const chat = useAssistant();
  // Background work counts as work in progress: the bubble shows it across the WHOLE application,
  // even closed and even after reloading the page (§3.6).
  const { active } = useAssistantTasks();

  // Hidden ≠ unmounted: whoever mounts it decides where it doesn't appear, but the conversation
  // survives a visit to Settings. The hooks always run — the early return comes after them, on purpose.
  if (hidden) return null;

  return (
    <>
      {open && <AssistantPanel chat={chat} onClose={() => setOpen(false)} />}
      <button
        className={"as-bubble" + (open ? " on" : "")}
        title={t("assistant.title")}
        aria-label={t("assistant.title")}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        {/* With work in progress the bubble shows it even closed (§3.1). */}
        {chat.busy || active > 0 ? <span className="as-bubble-work" /> : "✦"}
        {!open && active > 0 && <span className="as-bubble-count">{active}</span>}
        {/* Minimized session (a conversation in progress, no background tasks): a discreet dot
            saying there's an active session to resume. */}
        {!open && active === 0 && chat.messages.length > 0 && <span className="as-bubble-session" />}
      </button>
    </>
  );
}
