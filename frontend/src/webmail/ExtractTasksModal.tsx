import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation } from "@tanstack/react-query";
import { backend, colleaguesApi, type Task, type Colleague } from "./backend";
import { Spinner, AiThinking } from "./Spinner";
import { BoardPicker } from "./BoardPicker";
import { toast } from "./toast";

// "Extract tasks" modal: the AI reads the email (backend.extractTasks) and lets them be sent to
// the real board.
export function ExtractTasksModal({
  subject, body, sourceEmailId, onClose,
}: {
  subject: string;
  body: string;
  sourceEmailId: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [checked, setChecked] = useState<Record<number, boolean>>({});
  const [boardId, setBoardId] = useState("");

  // Matches the assignee the AI suggested to a real user -> assignee_id (the backend takes ids only).
  //
  // The server now sends the roster to the model and demands an E-MAIL from it, so the normal path is
  // an exact match. The two passes below are for the free text still arriving from older flows: a
  // bare first name. "Dener" never matched somebody displayed as "D Fernandes" -- not by equality and
  // not by prefix of the whole name -- and the assignment was lost without a word. Comparing TOKEN by
  // TOKEN fixes that, and the local part of the address catches "@dfernandes".
  const colleaguesQ = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list() });
  const resolveAssignee = (raw: string): string => {
    const n = raw.trim().toLowerCase().replace(/^@/, "");
    if (!n) return "";
    const list: Colleague[] = colleaguesQ.data ?? [];
    const exact = list.find((c) => c.email.toLowerCase() === n || c.name.toLowerCase() === n);
    if (exact) return exact.id;
    // One word of the name, or the local part of the address. Only counts when ONE person matches:
    // with two people called "Ana", picking the first is a decision nobody reviews -- an owner
    // already filled in is not something anyone goes back to check.
    const token = list.filter((c) =>
      c.name.toLowerCase().split(/\s+/).includes(n) || c.email.toLowerCase().split("@")[0] === n);
    if (token.length === 1) return token[0].id;
    // Last resort: a prefix, and only when a SINGLE colleague matches. Assigning to the first of
    // two would be worse than assigning to nobody, for the same reason as above.
    const starts = list.filter((c) => c.name.toLowerCase().startsWith(n) || c.email.toLowerCase().startsWith(n));
    return starts.length === 1 ? starts[0].id : "";
  };

  // Extraction is an ACTION (generates tasks), not cacheable data: we don't keep the result across
  // openings (gcTime 0) — otherwise a transient error (e.g. the API restarting) would stay "stuck"
  // to the email and reopening would show the error without ever retrying. This way reopening/
  // redoing always runs again.
  const tasks = useQuery({
    queryKey: ["extract", subject, body],
    queryFn: async () => {
      const r = await backend.extractTasks(subject, body);
      setChecked(Object.fromEntries(r.tasks.map((_, i) => [i, true])));
      return r.tasks;
    },
    retry: false,
    gcTime: 0,
    staleTime: 0,
    refetchOnMount: "always",
  });

  const add = useMutation({
    mutationFn: async (list: Task[]) => {
      // Board chosen in the picker; with a single board the picker already preselects it.
      const bid = boardId || (await backend.boards())[0].id;
      const board = await backend.board(bid);
      const col = board.columns[0].column.id; // 1st column ("To do")
      for (const tk of list) {
        await backend.createCard(bid, {
          column_id: col, title: tk.title,
          description: tk.due ? `${t("webmail.ai.due")}: ${tk.due}` : "",
          assignee_id: resolveAssignee(tk.assignee), due: tk.due, source_email_id: sourceEmailId,
        });
      }
    },
    onSuccess: (_r, list) => { toast(t("webmail.ai.addedTasks", { n: list.length })); onClose(); },
  });

  const selected = (tasks.data ?? []).filter((_, i) => checked[i]);

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()}>
        <header><span>✦ {t("webmail.ai.extractedTasks")}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body">
          <BoardPicker value={boardId} onChange={setBoardId} />
          {tasks.isLoading && <div className="ai-think-row"><AiThinking /></div>}
          {tasks.isError && (
            <div className="err">
              <p>{(tasks.error as Error).message}</p>
              <button className="chipbtn" onClick={() => void tasks.refetch()} disabled={tasks.isFetching}>
                {tasks.isFetching ? <Spinner size={13} /> : t("common.retry")}
              </button>
            </div>
          )}
          {tasks.isSuccess && (tasks.data?.length ?? 0) === 0 && (
            <p className="editor-msg">{t("webmail.ai.noTasks")}</p>
          )}
          {(tasks.data ?? []).map((tk, i) => (
            <div className="task" key={i}>
              <div className={"cb" + (checked[i] ? " on" : "")} onClick={() => setChecked((c) => ({ ...c, [i]: !c[i] }))}>
                {checked[i] ? "✓" : ""}
              </div>
              <div>
                <div className="tt">{tk.title}</div>
                <div className="tm2">📅 {tk.due || "—"} · 👤 {tk.assignee ? (resolveAssignee(tk.assignee) ? tk.assignee : `${tk.assignee} ${t("webmail.ai.assigneeUnmatched")}`) : "—"}</div>
              </div>
            </div>
          ))}
        </div>
        <footer>
          <button className="chipbtn" onClick={onClose}>{t("webmail.cancel")}</button>
          <button className="add" disabled={add.isPending || selected.length === 0} onClick={() => add.mutate(selected)}>
            {add.isPending ? <Spinner size={14} /> : t("webmail.ai.addToBoard")}
          </button>
        </footer>
      </div>
    </div>
  );
}
