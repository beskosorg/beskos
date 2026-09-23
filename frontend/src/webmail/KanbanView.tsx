import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { settingsApi, backend, type Board, type Card, type Column } from "./backend";
import { useUserSettings } from "./useSettings";
import { useChatSocket } from "./useChatSocket";
import { BoardShareDrawer } from "./BoardShareDrawer";
import { BoardListView, BoardCalendarView, BoardTimelineView, SwimlaneBoard, AutomationsModal, type BoardView } from "./KanbanExtras";
import { TaskModal, TASK_COLORS } from "./TaskModal";
import { toast, toastAction } from "./toast";
import { initials, hueFor } from "./util";
import { Avatar } from "./Avatar";
import { IcPlus, IcTrash, IcPencil, IcChat, IcArchive , IcShare } from "../panel/icons";

type Modal = { card?: Card | null; columnId?: string } | null;

// Trello-style Kanban: creates/edits tasks in a modal, drags between columns, creates/renames/
// deletes columns. Everything persists on the backend (queries filter by tenant).
// The open task lives in the URL (/mail/kanban/<board>/<task>): the link is shareable, refreshing
// doesn't lose the place, and it's the same address the email notification points to.
export function KanbanView({ search, boardParam, cardParam, onBoard, onCard }: {
  search: string; boardParam?: string; cardParam?: string;
  onBoard: (id: string) => void; onCard: (boardId: string, cardId: string | null) => void;
}) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const settings = useUserSettings(); // settings.kanban.defaultBoard
  const boardsQ = useQuery({ queryKey: ["boards"], queryFn: () => backend.boards() });
  // Board order is PER USER (settings.kanban.boardOrder): boards outside the list keep their
  // original order at the end. Reordered by dragging the tabs; persists on the account.
  const boardOrder = settings.kanban?.boardOrder ?? [];
  const boards = (() => {
    const base = boardsQ.data ?? [];
    if (!boardOrder.length) return base;
    const idx = (id: string) => { const i = boardOrder.indexOf(id); return i === -1 ? boardOrder.length : i; };
    return base.slice().sort((a, b) => idx(a.id) - idx(b.id));
  })();
  const [dragBoard, setDragBoard] = useState<string | null>(null);
  const [shareBoard, setShareBoard] = useState(false);
  const saveBoardOrder = (ids: string[]) => {
    settingsApi.patch({ kanban: { ...settings.kanban, boardOrder: ids } })
      .then(() => qc.invalidateQueries({ queryKey: ["settings"] }))
      .catch((e) => toast((e as Error).message));
  };
  const dropOnBoard = (targetId: string) => {
    if (!dragBoard || dragBoard === targetId) return;
    const ids = boards.map((b) => b.id).filter((id) => id !== dragBoard);
    ids.splice(ids.indexOf(targetId), 0, dragBoard);
    saveBoardOrder(ids);
    setDragBoard(null);
  };
  // The active board comes from the URL; without it, it falls back to the default board from
  // Settings (if it exists), otherwise the 1st one.
  const preferred = settings.kanban?.defaultBoard && boards.some((b) => b.id === settings.kanban!.defaultBoard)
    ? settings.kanban!.defaultBoard : boards[0]?.id;
  const boardId = boardParam && boards.some((b) => b.id === boardParam) ? boardParam : preferred;
  const accessQ = useQuery({ queryKey: ["board-access", boardId], queryFn: () => backend.boardAccess(boardId!), enabled: !!boardId, retry: false });
  const boardRole = accessQ.data?.role ?? 0;
  const setActiveBoard = onBoard;
  const boardQ = useQuery({ queryKey: ["board", boardId], queryFn: () => backend.board(boardId!), enabled: !!boardId });
  // Real time: reuses the chat's WebSocket (same hub). Every card mutation publishes a "board"
  // event; on receiving it, the board is revalidated — so whoever is looking at the same board
  // sees the other person's move.
  useChatSocket((ev) => {
    if (ev.type === "board" && ev.channel_id === boardId) void qc.invalidateQueries({ queryKey: ["board", boardId] });
  });
  // Tenant's label catalog, to color the card's label according to the chosen label.
  const labelsCat = useQuery({ queryKey: ["labels"], queryFn: () => backend.labels(), retry: false });
  const labelColor = (name: string) => labelsCat.data?.find((l) => l.name === name)?.color;

  const [dragCard, setDragCard] = useState<string | null>(null);
  const [dragCol, setDragCol] = useState<string | null>(null);
  const [modal, setModal] = useState<Modal>(null);

  // Card in the URL → opens the task. Covers the shared link and clicking the notification email.
  const cards = useMemo(() => (boardQ.data?.columns ?? []).flatMap((c) => c.cards ?? []), [boardQ.data]);
  const urlCard = cardParam ? cards.find((c) => c.id === cardParam) : undefined;
  const [addingCol, setAddingCol] = useState(false);
  const [newCol, setNewCol] = useState("");
  const [addingBoard, setAddingBoard] = useState(false);
  const [newBoard, setNewBoard] = useState("");
  const [renaming, setRenaming] = useState<string | null>(null);
  const [who, setWho] = useState<string | null>(null); // filter by assignee (the search comes from the top bar)
  const [view, setView] = useState<BoardView>("board");
  const [swim, setSwim] = useState(false);
  const [autos, setAutos] = useState(false);

  // Board participants = whoever has a card assigned on it (there's no board membership; boards
  // are shared per tenant). Clicking the avatar filters, like in Trello.
  const participants = useMemo(() => {
    const set = new Set<string>();
    boardQ.data?.columns.forEach(({ cards }) => (cards ?? []).forEach((c) => { const n = c.assignee_name || c.assignee; if (n) set.add(n); }));
    return [...set].sort((a, b) => a.localeCompare(b));
  }, [boardQ.data]);

  const term = search.trim().toLowerCase();
  const matches = (c: Card) =>
    (!who || (c.assignee_name || c.assignee) === who) &&
    (!term || [c.title, c.description, c.label].some((f) => (f ?? "").toLowerCase().includes(term)));
  const filtering = !!who || !!term;

  const refresh = () => qc.invalidateQueries({ queryKey: ["board", boardId] });
  const reorder = useMutation({
    mutationFn: ({ columnId, ids }: { columnId: string; ids: string[] }) => backend.reorderColumn(columnId, ids),
    onSuccess: refresh,
    onError: (e) => { toast((e as Error).message); refresh(); }, // e.g.: WIP limit → reverts the view
  });
  // dropCard builds the target column's new order (removing the dragged card from where it was
  // and inserting it before beforeCardId, or at the end if null) and persists it — uses the FULL
  // list (not the filtered one) so the position matches the server.
  const dropInto = (columnId: string, beforeCardId: string | null) => {
    if (!dragCard) return;
    const col = boardQ.data?.columns.find((c) => c.column.id === columnId);
    if (!col) return;
    const ids = (col.cards ?? []).map((c) => c.id).filter((id) => id !== dragCard);
    const at = beforeCardId ? ids.indexOf(beforeCardId) : -1;
    if (at >= 0) ids.splice(at, 0, dragCard); else ids.push(dragCard);
    reorder.mutate({ columnId, ids });
    setDragCard(null);
  };
  // Reordering the columns themselves (drag by the header). Builds the new order by inserting the
  // dragged column before the target column and persists it — uses the FULL order to match the server.
  const reorderCols = useMutation({
    mutationFn: (ids: string[]) => backend.reorderColumns(boardId!, ids),
    onSuccess: refresh,
  });
  const dropColBefore = (targetColId: string) => {
    if (!dragCol || dragCol === targetColId) { setDragCol(null); return; }
    const ids = (boardQ.data?.columns ?? []).map((c) => c.column.id).filter((id) => id !== dragCol);
    const at = ids.indexOf(targetColId);
    ids.splice(at < 0 ? ids.length : at, 0, dragCol);
    reorderCols.mutate(ids);
    setDragCol(null);
  };
  const createCol = useMutation({
    mutationFn: (name: string) => backend.createColumn(boardId!, name, TASK_COLORS[0]),
    onSuccess: () => { refresh(); setAddingCol(false); setNewCol(""); },
    onError: (e) => toast((e as Error).message),
  });

  const refreshBoards = () => qc.invalidateQueries({ queryKey: ["boards"] });
  const createBoard = useMutation({
    mutationFn: (name: string) => backend.createBoard(name),
    onSuccess: (b) => { refreshBoards(); setActiveBoard(b.id); setAddingBoard(false); setNewBoard(""); toast(t("webmail.kanban.boardCreated")); },
    onError: (e) => toast((e as Error).message),
  });
  const renameBoard = useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) => backend.updateBoard(id, name),
    onSuccess: () => { refreshBoards(); setRenaming(null); },
    onError: (e) => { setRenaming(null); toast((e as Error).message); },
  });
  const archiveBoardM = useMutation({
    mutationFn: (id: string) => backend.archiveBoard(id, true),
    onSuccess: (_r, id) => {
      refreshBoards();
      const other = boards.find((b) => b.id !== id);
      if (other) setActiveBoard(other.id);
      toastAction(t("webmail.kanban.boardArchived"), t("common.undo"), () => backend.archiveBoard(id, false).then(() => refreshBoards()));
    },
    onError: (e) => toast((e as Error).message),
  });
  const delBoard = useMutation({
    mutationFn: (id: string) => backend.deleteBoard(id),
    onSuccess: (_r, deletedId) => {
      refreshBoards();
      // The URL pointed to the board that just disappeared → go to the first one left.
      const next = boards.find((b) => b.id !== deletedId);
      if (next) setActiveBoard(next.id);
      toast(t("webmail.kanban.boardDeleted"));
    },
    onError: (e) => toast((e as Error).message),
  });

  if (boardQ.isLoading || !boardQ.data) return <div className="loading">…</div>;
  const columns = boardQ.data.columns.map((c) => c.column);

  return (
    <div className="kwrap">
      {shareBoard && boardId && <BoardShareDrawer boardId={boardId} onClose={() => setShareBoard(false)} />}
      <div className="khead">
        <div className="kboards">
          {boards.map((b) => (
            renaming === b.id ? (
              <input key={b.id} className="ktab-rename" autoFocus defaultValue={b.name}
                onKeyDown={(e) => {
                  if (e.key === "Enter") { const v = e.currentTarget.value.trim(); if (v && v !== b.name) renameBoard.mutate({ id: b.id, name: v }); else setRenaming(null); }
                  if (e.key === "Escape") setRenaming(null);
                }}
                onBlur={(e) => { const v = e.currentTarget.value.trim(); if (v && v !== b.name) renameBoard.mutate({ id: b.id, name: v }); else setRenaming(null); }} />
            ) : (
              <button key={b.id} className={"ktab" + (b.id === boardId ? " on" : "") + (dragBoard === b.id ? " dragging" : "")}
                title={b.id === boardId ? t("webmail.kanban.renameHint") : b.name}
                draggable onDragStart={() => setDragBoard(b.id)} onDragEnd={() => setDragBoard(null)}
                onDragOver={(e) => { if (dragBoard) e.preventDefault(); }}
                onDrop={(e) => { e.preventDefault(); dropOnBoard(b.id); }}
                onClick={() => setActiveBoard(b.id)}
                onDoubleClick={() => b.id === boardId && setRenaming(b.id)}>{b.name}{b.prefix && <span className="ktab-pfx">{b.prefix}</span>}</button>
            )
          ))}
          {boardId && !renaming && (
            <>
              {boardRole >= 4 && <button className="ib ktab-edit" title={t("webmail.kanban.shareBoard")} onClick={() => setShareBoard(true)}>
                <IcShare size={14} />
              </button>}
              {boardRole >= 4 && <button className="ib ktab-edit" title={t("webmail.kanban.renameBoard")} onClick={() => setRenaming(boardId)}>
                <IcPencil size={14} />
              </button>}
              <PrefixEditor board={boards.find((b) => b.id === boardId)!} onChanged={() => { void qc.invalidateQueries({ queryKey: ["boards"] }); refresh(); }} />
            </>
          )}
          {addingBoard ? (
            <input className="ktab-rename" autoFocus value={newBoard} placeholder={t("webmail.kanban.boardName")}
              onChange={(e) => setNewBoard(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter" && newBoard.trim()) createBoard.mutate(newBoard.trim()); if (e.key === "Escape") setAddingBoard(false); }}
              onBlur={() => { if (newBoard.trim()) createBoard.mutate(newBoard.trim()); else setAddingBoard(false); }} />
          ) : (
            <button className="ktab add" onClick={() => setAddingBoard(true)}><IcPlus size={14} /> {t("webmail.kanban.newBoard")}</button>
          )}
          {boards.length > 1 && boardId && (
            <button className="ib ktab-arch" title={t("webmail.kanban.archiveBoard")} onClick={() => archiveBoardM.mutate(boardId)}>
              <IcArchive size={14} />
            </button>
          )}
          {boards.length > 1 && boardId && (
            <button className="ib ktab-del" title={t("webmail.kanban.deleteBoard")} onClick={() => delBoard.mutate(boardId)}>
              <IcTrash size={14} />
            </button>
          )}
        </div>

        <div className="ktools">
          {participants.length > 0 && (
            <div className="kmembers" title={t("webmail.kanban.filterByPerson")}>
              {participants.map((p) => (
                <button key={p} className={"kav" + (who === p ? " on" : "")} title={p}
                  style={{ background: hueFor(p) }} onClick={() => setWho(who === p ? null : p)}>
                  {initials(p)}
                </button>
              ))}
            </div>
          )}
          {filtering && (
            <button className="chipbtn" onClick={() => setWho(null)}>{t("webmail.kanban.clearFilters")}</button>
          )}
          <div className="grow" />
          <div className="kb-views">
            {(["board", "list", "calendar", "timeline"] as const).map((v) => (
              <button key={v} className={view === v ? "on" : ""} onClick={() => setView(v)}>{t("webmail.kanban.view_" + v)}</button>
            ))}
          </div>
          {view === "board" && <button className={"chipbtn" + (swim ? " primary" : "")} onClick={() => setSwim((s) => !s)}>{t("webmail.kanban.swimlanes")}</button>}
          {boardId && <button className="chipbtn" onClick={() => setAutos(true)}>{t("webmail.kanban.automations")}</button>}
        </div>

        <span className="sub">{filtering ? t("webmail.kanban.filtered", { name: who ?? "" }) : t("webmail.kanban.hint")}</span>
      </div>
      {view !== "board" ? (
        <div className="kboard-alt">
          {(() => { const vc = boardQ.data.columns.map(({ column, cards }) => ({ column, cards: (cards ?? []).filter(matches) })); const open = (c: { id: string }) => onCard(boardId!, c.id);
            return view === "list" ? <BoardListView cols={vc} onCard={open} locale={i18n.language} />
              : view === "calendar" ? <BoardCalendarView cols={vc} onCard={open} locale={i18n.language} />
              : <BoardTimelineView cols={vc} onCard={open} locale={i18n.language} />; })()}
        </div>
      ) : swim ? (
        <div className="kboard-alt"><SwimlaneBoard cols={boardQ.data.columns.map(({ column, cards }) => ({ column, cards: (cards ?? []).filter(matches) }))} onCard={(c) => onCard(boardId!, c.id)} /></div>
      ) : (
      <div className="kboard">
        {boardQ.data.columns.map(({ column, cards }) => {
          const list = (cards ?? []).filter(matches);
          return (
            <div className={"kcol" + (dragCol === column.id ? " col-dragging" : "")} key={column.id}
              onDragOver={(e) => {
                e.preventDefault();
                if (dragCol) e.currentTarget.classList.add("col-over"); // dragging a column → reorder target
                else e.currentTarget.querySelector(".kcards")?.classList.add("drop");
              }}
              onDragLeave={(e) => { e.currentTarget.classList.remove("col-over"); e.currentTarget.querySelector(".kcards")?.classList.remove("drop"); }}
              onDrop={(e) => {
                e.currentTarget.classList.remove("col-over");
                e.currentTarget.querySelector(".kcards")?.classList.remove("drop");
                if (dragCol) dropColBefore(column.id); // dropped onto another column → reorders columns
                else dropInto(column.id, null); // dropped on the column (outside a card) → goes to the end
              }}>
              <ColumnHeader column={column} count={list.length} onChanged={refresh}
                onDragStart={() => setDragCol(column.id)} onDragEnd={() => setDragCol(null)} />
              <div className="kcards">
                {list.map((c) => (
                  <div className={"kcard" + (c.color ? " tinted" : "")} key={c.id} draggable style={c.color ? { background: c.color } : undefined}
                    onClick={() => onCard(boardId!, c.id)}
                    onDragStart={(e) => { setDragCard(c.id); e.currentTarget.classList.add("dragging"); }}
                    onDragEnd={(e) => { e.currentTarget.classList.remove("dragging"); setDragCard(null); }}
                    onDragOver={(e) => { if (dragCol) return; e.preventDefault(); e.stopPropagation(); }}
                    onDrop={(e) => { if (dragCol) return; e.preventDefault(); e.stopPropagation(); e.currentTarget.closest(".kcol")?.querySelector(".kcards")?.classList.remove("drop"); dropInto(column.id, c.id); }}>
                    <div className="kkey">{c.card_key}</div>
                    <div className="kt">{c.title}</div>
                    {c.description && <div className="kx">{c.description}</div>}
                    <div className="kmeta">
                      {c.priority && c.priority !== "normal" && <span className={"prio prio-" + c.priority} title={t("webmail.kanban.priority")}>{t("webmail.kanban.priorities." + c.priority)}</span>}
                      {(c.comment_count ?? 0) > 0 && <span className="kcmt" title={t("comments.title")}><IcChat size={12} /> {c.comment_count}</span>}
                      {c.label && <span className="lab" style={labelColor(c.label) ? { background: labelColor(c.label)!, color: "#fff" } : { background: "var(--accent-tint)", color: "var(--accent-ink)" }}>{c.label}</span>}
                      {c.due && <span className="kdue">{c.due}</span>}
                      {(c.assignee_name || c.assignee) && <Avatar name={c.assignee_name || c.assignee} userId={c.assignee_id ?? undefined} size={22} title={c.assignee_name || c.assignee} />}
                    </div>
                  </div>
                ))}
              </div>
              <button className="kadd" onClick={() => setModal({ columnId: column.id })}><IcPlus size={14} /> {t("webmail.kanban.add")}</button>
            </div>
          );
        })}

        <div className="kcol kcol-new">
          {addingCol ? (
            <input className="newfolder" autoFocus value={newCol} placeholder={t("webmail.kanban.columnName")}
              onChange={(e) => setNewCol(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter" && newCol.trim()) createCol.mutate(newCol.trim()); if (e.key === "Escape") setAddingCol(false); }}
              onBlur={() => { if (newCol.trim()) createCol.mutate(newCol.trim()); else setAddingCol(false); }} />
          ) : (
            <button className="kaddcol" onClick={() => setAddingCol(true)}><IcPlus size={15} /> {t("webmail.kanban.newColumn")}</button>
          )}
        </div>
      </div>
      )}
      {autos && boardId && <AutomationsModal boardId={boardId} cols={boardQ.data.columns} onClose={() => setAutos(false)} />}

      {/* The URL's task wins over local state: whoever arrives via the link must see that task. */}
      {(urlCard || modal) && boardId && (
        <TaskModal boardId={boardId} columns={columns}
          card={urlCard ?? modal?.card} columnId={modal?.columnId}
          boardCards={cards} assignees={boardQ.data.assignees ?? []} deps={boardQ.data.deps ?? []}
          onChanged={refresh}
          onClose={() => { setModal(null); if (cardParam) onCard(boardId, null); }}
          onSaved={() => { setModal(null); refresh(); }} />
      )}
    </div>
  );
}

// Column header: inline rename (double-click), change color, and delete via menu.
function ColumnHeader({ column, count, onChanged, onDragStart, onDragEnd }: {
  column: Column; count: number; onChanged: () => void; onDragStart: () => void; onDragEnd: () => void;
}) {
  const { t } = useTranslation();
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(column.name);
  const [menu, setMenu] = useState(false);

  const update = useMutation({
    // Preserves untouched fields (otherwise renaming would zero out the WIP limit and vice versa).
    mutationFn: (patch: { name?: string; color?: string; wip_limit?: number }) =>
      backend.updateColumn(column.id, patch.name ?? column.name, patch.color ?? column.color, patch.wip_limit ?? column.wip_limit ?? 0),
    onSuccess: () => { onChanged(); setEditing(false); setMenu(false); },
    onError: (e) => toast((e as Error).message),
  });
  const del = useMutation({
    mutationFn: () => backend.deleteColumn(column.id),
    onSuccess: () => { onChanged(); setMenu(false); },
    onError: (e) => toast((e as Error).message),
  });

  return (
    <div className="kcol-h" title={t("webmail.kanban.dragColumn")}
      draggable={!editing}
      onDragStart={(e) => { e.dataTransfer.effectAllowed = "move"; onDragStart(); }}
      onDragEnd={onDragEnd}>
      <span className="cgrip" aria-hidden>⠿</span>
      <span className="cdot" style={{ background: column.color }} />
      {editing ? (
        <input className="kcol-rename" autoFocus value={name}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && name.trim()) update.mutate({ name: name.trim() }); if (e.key === "Escape") { setName(column.name); setEditing(false); } }}
          onBlur={() => { if (name.trim() && name !== column.name) update.mutate({ name: name.trim() }); else setEditing(false); }} />
      ) : (
        <span className="kcol-name" onDoubleClick={() => setEditing(true)}>{column.name}</span>
      )}
      <span className={"k" + (column.wip_limit ? (count > column.wip_limit ? " wip-over" : " wip") : "")}>{count}{column.wip_limit ? ` / ${column.wip_limit}` : ""}</span>
      <div className="kcol-menuwrap">
        <button className="kcol-menu" onClick={() => setMenu((v) => !v)}>⋯</button>
        {menu && (
          <div className="move-menu" onMouseLeave={() => setMenu(false)}>
            <button onClick={() => { setEditing(true); setMenu(false); }}>{t("webmail.kanban.rename")}</button>
            <button onClick={() => { const v = prompt(t("webmail.kanban.wipPrompt"), String(column.wip_limit ?? 0)); if (v !== null) update.mutate({ wip_limit: Math.max(0, parseInt(v, 10) || 0) }); }}>{t("webmail.kanban.setWip")}</button>
            <div className="kcol-colors">
              {TASK_COLORS.map((c) => <button key={c} className="ev-color" style={{ background: c }} onClick={() => update.mutate({ color: c })} />)}
            </div>
            <button className="danger-item" onClick={() => del.mutate()}><IcTrash size={13} /> {t("webmail.kanban.deleteColumn")}</button>
          </div>
        )}
      </div>
    </div>
  );
}

// Board prefix (QUA-12). The key is derived from the prefix + number, so changing it here renames
// every task on the board at once — and doesn't break any link, because the link uses the id.
function PrefixEditor({ board, onChanged }: { board: Board; onChanged: () => void }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [v, setV] = useState(board.prefix ?? "");
  const save = useMutation({
    mutationFn: () => backend.setBoardPrefix(board.id, v),
    onSuccess: () => { setOpen(false); onChanged(); toast(t("webmail.kanban.prefixSaved")); },
    onError: (e) => toast((e as Error).message),
  });
  const valid = /^[A-Za-z0-9]{2,6}$/.test(v.trim());
  if (!open) {
    return <button className="ib ktab-edit" title={t("webmail.kanban.editPrefix")} onClick={() => { setV(board.prefix ?? ""); setOpen(true); }}>#</button>;
  }
  return (
    <input className="ktab-rename pfx" autoFocus value={v} maxLength={6} placeholder={t("webmail.kanban.prefixPh")}
      onChange={(e) => setV(e.target.value.toUpperCase())}
      onKeyDown={(e) => { if (e.key === "Enter" && valid) save.mutate(); if (e.key === "Escape") setOpen(false); }}
      onBlur={() => (valid && v !== board.prefix ? save.mutate() : setOpen(false))} />
  );
}
