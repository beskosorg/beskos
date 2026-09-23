import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { backend, type Board } from "./backend";

// List of the tenant's boards (cache shared with the Kanban).
export function useBoards(): Board[] {
  const q = useQuery({ queryKey: ["boards"], queryFn: () => backend.boards(), retry: false });
  return q.data ?? [];
}

// Board selector for the AI actions (extract tasks, create task). With a single board it doesn't
// ask anything — it just selects it automatically; with two or more, it forces a choice of where to create.
export function BoardPicker({ value, onChange }: { value: string; onChange: (id: string) => void }) {
  const { t } = useTranslation();
  const boards = useBoards();

  useEffect(() => {
    if (!value && boards.length) onChange(boards[0].id);
  }, [boards, value, onChange]);

  if (boards.length <= 1) return null;
  return (
    <label className="board-pick">{t("webmail.kanban.board")}
      <select value={value} onChange={(e) => onChange(e.target.value)}>
        {boards.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}
      </select>
    </label>
  );
}
