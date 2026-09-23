import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { backend, colleaguesApi, type Card, type Column } from "./backend";
import { toast } from "./toast";
import { initials, hueFor } from "./util";
import { IcTrash, IcX, IcPlus } from "../panel/icons";

export type BoardView = "board" | "list" | "calendar" | "timeline";
type Col = { column: Column; cards: Card[] };

const fmtDate = (d: string, locale: string) => (d ? new Date(d + "T00:00").toLocaleDateString(locale, { day: "2-digit", month: "short" }) : "");
const colName = (cols: Col[], id: string) => cols.find((c) => c.column.id === id)?.column.name ?? "";

// ---- List view: table of all cards, sorted by due date. ----
export function BoardListView({ cols, onCard, locale }: { cols: Col[]; onCard: (c: Card) => void; locale: string }) {
  const { t } = useTranslation();
  const rows = useMemo(() => cols.flatMap((c) => c.cards).sort((a, b) => (a.due || "9999").localeCompare(b.due || "9999") || a.title.localeCompare(b.title)), [cols]);
  if (rows.length === 0) return <div className="empty">{t("webmail.kanban.noCards")}</div>;
  return (
    <div className="kb-list">
      <div className="kbl-head"><span>{t("webmail.kanban.title")}</span><span>{t("webmail.kanban.column")}</span><span>{t("webmail.kanban.assignee")}</span><span>{t("webmail.kanban.startDate")}</span><span>{t("webmail.kanban.due")}</span></div>
      {rows.map((c) => (
        <button className="kbl-row" key={c.id} onClick={() => onCard(c)}>
          <span className="kbl-title">{c.card_key && <b className="kbl-key">{c.card_key}</b>}{c.title}{c.label && <span className="kb-lab sm">{c.label}</span>}</span>
          <span>{colName(cols, c.column_id)}</span>
          <span>{c.assignee_name || c.assignee || "—"}</span>
          <span>{fmtDate(c.start_date ?? "", locale) || "—"}</span>
          <span>{fmtDate(c.due, locale) || "—"}</span>
        </button>
      ))}
    </div>
  );
}

// ---- Calendar view: cards on their due date, within the cursor's month. ----
export function BoardCalendarView({ cols, onCard, locale }: { cols: Col[]; onCard: (c: Card) => void; locale: string }) {
  const [cursor, setCursor] = useState(() => { const d = new Date(); d.setDate(1); d.setHours(0, 0, 0, 0); return d; });
  const cards = useMemo(() => cols.flatMap((c) => c.cards).filter((c) => c.due), [cols]);
  const first = new Date(cursor);
  const gridStart = new Date(first); gridStart.setDate(first.getDate() - first.getDay());
  const cells = Array.from({ length: 42 }, (_, i) => { const d = new Date(gridStart); d.setDate(gridStart.getDate() + i); return d; });
  const iso = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  const forDay = (d: Date) => cards.filter((c) => c.due === iso(d));
  const todayStr = new Date().toDateString();
  return (
    <div className="kb-cal">
      <div className="kbc-nav">
        <button onClick={() => setCursor((c) => { const d = new Date(c); d.setMonth(c.getMonth() - 1); return d; })}>‹</button>
        <b>{cursor.toLocaleDateString(locale, { month: "long", year: "numeric" })}</b>
        <button onClick={() => setCursor((c) => { const d = new Date(c); d.setMonth(c.getMonth() + 1); return d; })}>›</button>
      </div>
      <div className="kbc-grid">
        {cells.map((d, i) => (
          <div key={i} className={"kbc-cell" + (d.getMonth() === cursor.getMonth() ? "" : " out") + (d.toDateString() === todayStr ? " today" : "")}>
            <span className="kbc-day">{d.getDate()}</span>
            {forDay(d).map((c) => <button key={c.id} className="kbc-ev" onClick={() => onCard(c)}>{c.title}</button>)}
          </div>
        ))}
      </div>
    </div>
  );
}

// ---- Timeline view: bars from start_date → due. Cards missing either are left out. ----
export function BoardTimelineView({ cols, onCard, locale }: { cols: Col[]; onCard: (c: Card) => void; locale: string }) {
  const { t } = useTranslation();
  const items = useMemo(() => cols.flatMap((c) => c.cards).filter((c) => c.start_date && c.due), [cols]);
  if (items.length === 0) return <div className="empty">{t("webmail.kanban.timelineEmpty")}</div>;
  const times = items.flatMap((c) => [new Date(c.start_date! + "T00:00").getTime(), new Date(c.due + "T00:00").getTime()]);
  const min = Math.min(...times), max = Math.max(...times) || min + 86400000;
  const span = Math.max(1, max - min);
  const pct = (t: number) => ((t - min) / span) * 100;
  return (
    <div className="kb-timeline">
      <div className="kbt-axis"><span>{new Date(min).toLocaleDateString(locale, { day: "2-digit", month: "short" })}</span><span>{new Date(max).toLocaleDateString(locale, { day: "2-digit", month: "short" })}</span></div>
      {items.sort((a, b) => a.start_date!.localeCompare(b.start_date!)).map((c) => {
        const s = new Date(c.start_date! + "T00:00").getTime(), e = new Date(c.due + "T00:00").getTime();
        return (
          <button className="kbt-row" key={c.id} onClick={() => onCard(c)}>
            <span className="kbt-label">{c.title}</span>
            <span className="kbt-track"><span className="kbt-bar" style={{ insetInlineStart: pct(s) + "%", width: Math.max(2, pct(e) - pct(s)) + "%" }} /></span>
          </button>
        );
      })}
    </div>
  );
}

// ---- Swimlanes: lanes by assignee, each showing the columns with that assignee's cards. ----
export function SwimlaneBoard({ cols, onCard }: { cols: Col[]; onCard: (c: Card) => void }) {
  const { t } = useTranslation();
  const lanes = useMemo(() => {
    const names = new Set<string>();
    cols.forEach((c) => c.cards.forEach((k) => names.add(k.assignee_name || k.assignee || "")));
    return [...names].sort((a, b) => (a === "" ? 1 : b === "" ? -1 : a.localeCompare(b)));
  }, [cols]);
  return (
    <div className="kb-swim">
      {lanes.map((lane) => (
        <div className="swim-lane" key={lane || "none"}>
          <div className="swim-h">{lane ? <><span className="av xs" style={{ background: hueFor(lane) }}>{initials(lane)}</span>{lane}</> : t("webmail.kanban.unassigned")}</div>
          <div className="swim-cols">
            {cols.map(({ column, cards }) => (
              <div className="swim-col" key={column.id}>
                <div className="swim-col-h"><span className="cdot" style={{ background: column.color }} />{column.name}</div>
                {cards.filter((c) => (c.assignee_name || c.assignee || "") === lane).map((c) => (
                  <button className="swim-card" key={c.id} onClick={() => onCard(c)}>{c.card_key && <b className="kkey">{c.card_key}</b>}{c.title}</button>
                ))}
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

// ---- Automation manager: when a card enters a column → action. ----
export function AutomationsModal({ boardId, cols, onClose }: { boardId: string; cols: Col[]; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const rules = useQuery({ queryKey: ["automations", boardId], queryFn: () => backend.automations(boardId), retry: false });
  const people = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false });
  const [col, setCol] = useState("");
  const [action, setAction] = useState("assign");
  const [value, setValue] = useState("");
  const refresh = () => qc.invalidateQueries({ queryKey: ["automations", boardId] });
  const add = useMutation({
    mutationFn: () => backend.createAutomation(boardId, { trigger_column_id: col, action, action_value: action === "archive" ? "" : value }),
    onSuccess: () => { setCol(""); setValue(""); refresh(); }, onError: (e) => toast((e as Error).message),
  });
  const del = useMutation({ mutationFn: (id: string) => backend.deleteAutomation(id), onSuccess: refresh, onError: (e) => toast((e as Error).message) });
  const describe = (a: { trigger_column_id: string; action: string; action_value: string }) => {
    const c = colName(cols, a.trigger_column_id);
    if (a.action === "assign") return t("webmail.kanban.autoAssignDesc", { column: c, who: people.data?.find((p) => p.id === a.action_value)?.name ?? a.action_value });
    if (a.action === "label") return t("webmail.kanban.autoLabelDesc", { column: c, label: a.action_value });
    return t("webmail.kanban.autoArchiveDesc", { column: c });
  };
  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk modal-sm" onMouseDown={(e) => e.stopPropagation()}>
        <header><span>{t("webmail.kanban.automations")}</span><button className="ib" title={t("common.close")} onClick={onClose}><IcX size={15} /></button></header>
        <div className="tk-body">
          <div className="auto-add">
            <span>{t("webmail.kanban.autoWhenEnter")}</span>
            <select value={col} onChange={(e) => setCol(e.target.value)}><option value="">—</option>{cols.map((c) => <option key={c.column.id} value={c.column.id}>{c.column.name}</option>)}</select>
            <select value={action} onChange={(e) => setAction(e.target.value)}>
              <option value="assign">{t("webmail.kanban.autoAssign")}</option>
              <option value="label">{t("webmail.kanban.autoLabel")}</option>
              <option value="archive">{t("webmail.kanban.autoArchive")}</option>
            </select>
            {action === "assign" && <select value={value} onChange={(e) => setValue(e.target.value)}><option value="">—</option>{(people.data ?? []).map((p) => <option key={p.id} value={p.id}>{p.name || p.email}</option>)}</select>}
            {action === "label" && <input placeholder={t("webmail.kanban.label")} value={value} onChange={(e) => setValue(e.target.value)} />}
            <button className="add" title={t("common.create")} disabled={!col || (action !== "archive" && !value) || add.isPending} onClick={() => add.mutate()}><IcPlus size={13} /></button>
          </div>
          <div className="auto-list">
            {(rules.data ?? []).length === 0 ? <p className="desc">{t("webmail.kanban.noAutomations")}</p>
              : (rules.data ?? []).map((a) => (
                <div className="auto-row" key={a.id}><span className="grow">{describe(a)}</span><button className="ib" title={t("common.delete")} onClick={() => del.mutate(a.id)}><IcTrash size={14} /></button></div>
              ))}
          </div>
        </div>
      </div>
    </div>
  );
}
