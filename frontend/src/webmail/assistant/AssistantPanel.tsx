import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Spinner } from "../Spinner";
import { assistantApi } from "./api";
import { ToolCard } from "./ToolCard";
import { ConfirmAction } from "./ConfirmAction";
import { AgentPicker, AgentBadge } from "./AgentPicker";
import { TasksPanel, useAssistantTasks } from "./TasksPanel";
import type { useAssistant } from "./useAssistant";

// Assistant side panel: conversation, action cards, and composer.
//
// Three views: the conversation, the history, and background Tasks (§3.6).
export function AssistantPanel({ chat, onClose }: { chat: ReturnType<typeof useAssistant>; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [draft, setDraft] = useState("");
  const [tab, setTab] = useState<"chat" | "history" | "tasks">("chat");
  const { active } = useAssistantTasks();
  const endRef = useRef<HTMLDivElement>(null);

  // Follows the conversation as text arrives — without this, the response grows off-screen.
  useEffect(() => { endRef.current?.scrollIntoView({ block: "end" }); }, [chat.messages]);

  const history = useQuery({
    queryKey: ["assistant-conversations"],
    queryFn: () => assistantApi.conversations(),
    enabled: tab === "history",
    retry: false,
  });

  const submit = () => {
    const text = draft;
    setDraft("");
    void chat.send(text).then(() => qc.invalidateQueries({ queryKey: ["assistant-conversations"] }));
  };

  // choose sends a quick reply (clicking a choice button) as if it were the typed message.
  const choose = (value: string) => {
    if (chat.busy) return;
    void chat.send(value).then(() => qc.invalidateQueries({ queryKey: ["assistant-conversations"] }));
  };

  // Running in the background is an EXPLICIT choice by whoever asks. Guessing what counts as
  // "long" would just be guessing, and backgrounding what the person expected to see right away changes what they see next.
  const submitBackground = () => {
    const text = draft.trim();
    if (!text) return;
    setDraft("");
    void chat.sendBackground(text).then(() => {
      setTab("tasks");
      void qc.invalidateQueries({ queryKey: ["assistant-tasks"] });
    });
  };

  return (
    <aside className="as-panel" role="dialog" aria-label={t("assistant.title")}>
      <header className="as-head">
        <b>✦ {t("assistant.title")}</b>
        <AgentPicker pinned={chat.pinned} onPin={chat.setPinned} />
        <div className="grow" />
        <button className="ib" title={t("assistant.newChat")} onClick={() => { chat.reset(); setTab("chat"); }}>＋</button>
        <button className={"ib" + (tab === "tasks" ? " on" : "")} title={t("assistant.tasks.title")}
          onClick={() => setTab((v) => (v === "tasks" ? "chat" : "tasks"))}>
          ⧗{active > 0 && <span className="as-tab-badge">{active}</span>}
        </button>
        <button className={"ib" + (tab === "history" ? " on" : "")} title={t("assistant.history")}
          onClick={() => setTab((v) => (v === "history" ? "chat" : "history"))}>☰</button>
        {/* Minimize: hides the panel (the conversation stays; the bubble shows an active session). */}
        <button className="ib" title={t("assistant.minimize")} onClick={onClose}>—</button>
        {/* Close: hides AND clears the conversation (starts fresh next time). */}
        <button className="ib" title={t("assistant.close")} onClick={() => { chat.reset(); onClose(); }}>✕</button>
      </header>

      {tab === "tasks" ? (
        <TasksPanel onOpenConversation={(id) => { void chat.open(id); setTab("chat"); }} />
      ) : tab === "history" ? (
        <div className="as-history">
          {history.isPending && <Spinner size={16} />}
          {history.isError && <p className="as-err">{(history.error as Error).message}</p>}
          {history.data?.items.length === 0 && <p className="as-empty">{t("assistant.noHistory")}</p>}
          {history.data?.items.map((c) => (
            <button key={c.id} className="as-hist-item" onClick={() => { void chat.open(c.id); setTab("chat"); }}>
              <span className="as-hist-title">{c.title || t("assistant.untitled")}</span>
              <span className="as-hist-date">{new Date(c.updated_at).toLocaleDateString()}</span>
            </button>
          ))}
        </div>
      ) : (
        <div className="as-thread">
          {chat.messages.length === 0 && <p className="as-empty">{t("assistant.emptyHint")}</p>}
          {chat.messages.map((m, i) => (
            <div key={i} className={"as-msg " + m.role}>
              {m.role === "assistant" && <AgentBadge agent={m.agent} />}
              {m.steps.map((s, j) => <ToolCard key={j} step={s} />)}
              {m.content && <div className="as-bubble">{m.content}</div>}
              {/* Quick replies: if the last message offered choices (e.g. which "Bob"), show
                  BUTTONS instead of forcing typing. Only on the last message, so old buttons don't linger. */}
              {i === chat.messages.length - 1 && !chat.busy && (() => {
                const cs = m.steps.flatMap((s) => s.choices ?? []);
                return cs.length > 0 ? (
                  <div className="as-choices">
                    {cs.map((c, k) => <button key={k} className="as-choice" onClick={() => choose(c.value)}>{c.label}</button>)}
                  </div>
                ) : null;
              })()}
              {/* A turn that stopped at the step ceiling isn't an answer — saying so is mandatory. */}
              {m.savedFacts?.map((f, k) => <p key={"s" + k} className="as-note">✓ {t("assistant.memory.saved", { fact: f.fact })}</p>)}
              {m.refusedFacts?.map((f, k) => <p key={"r" + k} className="as-note">{t(`server.${f.reason}`, { defaultValue: t("assistant.memory.refusedGeneric") })}</p>)}
              {m.stopped && <p className="as-note">{t(`server.assistant.${m.stopped}`, { defaultValue: t("assistant.genericError") })}</p>}
              {m.errorCode && <p className="as-err">{t(`server.${m.errorCode}`, { defaultValue: t("assistant.genericError") })}</p>}
              {chat.busy && i === chat.messages.length - 1 && !m.content && m.steps.length === 0 && <Spinner size={14} />}
            </div>
          ))}
          {/* Execution is paused on the server waiting for this decision (§6.3). */}
          {chat.pending && <ConfirmAction req={chat.pending} onDecided={() => { /* the thread continues and sends an action card */ }} />}
          <div ref={endRef} />
        </div>
      )}

      <QuotaBar />

      <footer className="as-compose">
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder={t("assistant.placeholder")}
          rows={2}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); submit(); }
          }}
        />
        {chat.busy
          ? <button className="as-send" onClick={chat.stop}>{t("assistant.stop")}</button>
          : (
            <div className="as-send-group">
              <button className="as-send" disabled={!draft.trim()} onClick={submit}>{t("assistant.send")}</button>
              <button className="as-bg" disabled={!draft.trim()} title={t("assistant.tasks.runInBackground")}
                onClick={submitBackground}>⧗</button>
            </div>
          )}
      </footer>
    </aside>
  );
}

// QuotaBar shows how much is left of the 6h window and warns as the end approaches (§9.3): a
// warning near the limit is worth more than a wall with no notice.
function QuotaBar() {
  const { t } = useTranslation();
  const q = useQuery({ queryKey: ["assistant-usage"], queryFn: () => assistantApi.usage(), retry: false, refetchInterval: 60000 });
  const d = q.data;
  if (!d || d.limit <= 0) return null;
  const pct = Math.min(100, Math.round((d.used / d.limit) * 100));
  // Only shows once it starts to matter: a bar at 3% would be noise. Near the end, it warns.
  if (pct < 75) return null;
  return (
    <div className={"as-quota" + (pct >= 100 ? " full" : "")}>
      <div className="as-quota-bar"><span style={{ width: pct + "%" }} /></div>
      <span className="as-quota-txt">
        {pct >= 100
          ? t("assistant.quota.exhausted", { when: d.renews_at ? new Date(d.renews_at).toLocaleTimeString() : "" })
          : t("assistant.quota.low", { pct })}
      </span>
    </div>
  );
}
