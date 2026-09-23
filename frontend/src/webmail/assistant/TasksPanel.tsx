import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { assistantApi, type AssistantTask } from "./api";
import { ToolCard } from "./ToolCard";
import { ConfirmAction } from "./ConfirmAction";

// States that are still moving. While any exists, the list refetches itself — that's what makes
// the bubble show progress even when closed (§3.6).
const LIVE = ["queued", "running", "awaiting_confirmation"];

export function useAssistantTasks() {
  const q = useQuery({
    queryKey: ["assistant-tasks"],
    queryFn: () => assistantApi.tasks(),
    retry: false,
    // Only polls while work is in progress. A settled list does not need to be checked every
    // three seconds for the rest of the session.
    refetchInterval: (query) =>
      (query.state.data?.items ?? []).some((t) => LIVE.includes(t.status)) ? 3000 : false,
  });
  const active = (q.data?.items ?? []).filter((t) => LIVE.includes(t.status)).length;
  return { tasks: q.data?.items ?? [], pending: q.data?.pending ?? [], active, isPending: q.isPending };
}

// Tasks tab: what runs in the background, with progress and cancellation.
export function TasksPanel({ onOpenConversation }: { onOpenConversation: (id: string) => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { tasks, pending, isPending } = useAssistantTasks();

  const cancel = useMutation({
    mutationFn: (id: string) => assistantApi.taskCancel(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["assistant-tasks"] }),
  });

  if (isPending) return <div className="as-tasks"><p className="as-empty">…</p></div>;
  if (tasks.length === 0) return <div className="as-tasks"><p className="as-empty">{t("assistant.tasks.empty")}</p></div>;

  return (
    <div className="as-tasks">
      {tasks.map((task) => (
        <div key={task.id} className="as-task">
          <div className="as-task-head">
            <span className={"as-task-dot " + task.status} />
            <b>{task.message}</b>
          </div>
          <div className="as-task-status">
            {t(`assistant.tasks.status.${task.status}`, { defaultValue: task.status })}
            {task.steps.length > 0 && ` · ${t("assistant.tasks.steps", { count: task.steps.length })}`}
          </div>

          {/* Progress in plain sight: the action cards already done, not a bar with no content. */}
          {task.steps.map((s, i) => <ToolCard key={i} step={s} />)}

          {/* A stalled task has to say what it's waiting for, or it just reads "waiting". */}
          {pending.filter((p) => p.task_id === task.id).map((p) => (
            <ConfirmAction key={p.id} req={p}
              onDecided={() => qc.invalidateQueries({ queryKey: ["assistant-tasks"] })} />
          ))}

          {task.answer && <div className="as-bubble">{task.answer}</div>}
          {task.error_code && <p className="as-err">{t(`server.${task.error_code}`, { defaultValue: t("assistant.genericError") })}</p>}
          {task.stopped && <p className="as-note">{t(`server.assistant.${task.stopped}`, { defaultValue: task.stopped })}</p>}

          <div className="as-task-actions">
            <button className="chipbtn" onClick={() => onOpenConversation(task.conversation_id)}>
              {t("assistant.tasks.open")}
            </button>
            {LIVE.includes(task.status) && (
              <button className="chipbtn" disabled={cancel.isPending} onClick={() => cancel.mutate(task.id)}>
                {t("assistant.tasks.cancel")}
              </button>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

export type { AssistantTask };
