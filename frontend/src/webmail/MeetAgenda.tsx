import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { Meeting } from "./backend";

type View = "day" | "week" | "month";
const DAY_START = 7, DAY_END = 21, ROW = 72; // 7am–9pm — same look as the calendar page

const startOfDay = (d: Date) => { const x = new Date(d); x.setHours(0, 0, 0, 0); return x; };
const addDays = (d: Date, n: number) => { const x = new Date(d); x.setDate(x.getDate() + n); return x; };
const sameDay = (a: Date, b: Date) => a.toDateString() === b.toDateString();
function weekStart(d: Date) { const x = startOfDay(d); x.setDate(x.getDate() - ((x.getDay() + 6) % 7)); return x; } // Monday

// Meet agenda: day / week / month. Shows scheduled meetings; clicking opens the detail.
export function MeetAgenda({ meetings, view, cursor, onOpen }: {
  meetings: Meeting[]; view: View; cursor: Date; onOpen: (m: Meeting) => void;
}) {
  const scheduled = meetings.filter((m) => m.scheduled_start && m.status !== "canceled");
  if (view === "month") return <MonthGrid meetings={scheduled} cursor={cursor} onOpen={onOpen} />;
  const days = view === "day" ? [startOfDay(cursor)] : Array.from({ length: 7 }, (_, i) => addDays(weekStart(cursor), i));
  return <TimeGrid meetings={scheduled} days={days} onOpen={onOpen} />;
}

function TimeGrid({ meetings, days, onOpen }: { meetings: Meeting[]; days: Date[]; onOpen: (m: Meeting) => void }) {
  const { i18n } = useTranslation();
  const hours = Array.from({ length: DAY_END - DAY_START }, (_, i) => DAY_START + i);
  const todayStr = new Date().toDateString();
  const blocks = (day: Date) => meetings.filter((m) => sameDay(new Date(m.scheduled_start!), day)).map((m) => {
    const s = new Date(m.scheduled_start!);
    const e = m.scheduled_end ? new Date(m.scheduled_end) : new Date(s.getTime() + 3600e3);
    const top = (s.getHours() - DAY_START + s.getMinutes() / 60) * ROW;
    const h = Math.max(24, ((e.getTime() - s.getTime()) / 3600e3) * ROW);
    return { m, top, h };
  });
  return (
    <div className="ag-time" style={{ gridTemplateColumns: `52px repeat(${days.length}, 1fr)` }}>
      <div className="ag-corner" />
      {days.map((d, i) => (
        <div key={i} className={"ag-dayh" + (d.toDateString() === todayStr ? " today" : "")}>
          <b>{d.getDate()}</b>
          <span>{d.toLocaleDateString(i18n.language, { weekday: "short" })}</span>
        </div>
      ))}
      <div className="ag-hours">{hours.map((h) => <div key={h} className="ag-hr"><span>{String(h).padStart(2, "0")}:00</span></div>)}</div>
      {days.map((d, i) => (
        <div key={i} className={"ag-col" + (d.toDateString() === todayStr ? " today" : "")} style={{ height: hours.length * ROW }}>
          {hours.map((h) => <div key={h} className="ag-cell" style={{ height: ROW }} />)}
          {blocks(d).map(({ m, top, h }) => (
            <button key={m.id} className={"ag-ev" + (m.status === "live" ? " live" : "")} style={{ top, height: h }} onClick={() => onOpen(m)}>
              <b>{m.title}</b>
              <span>{new Date(m.scheduled_start!).toLocaleTimeString(i18n.language, { hour: "2-digit", minute: "2-digit" })}</span>
            </button>
          ))}
        </div>
      ))}
    </div>
  );
}

function MonthGrid({ meetings, cursor, onOpen }: { meetings: Meeting[]; cursor: Date; onOpen: (m: Meeting) => void }) {
  const { i18n } = useTranslation();
  const first = new Date(cursor.getFullYear(), cursor.getMonth(), 1);
  const gridStart = weekStart(first);
  const cells = Array.from({ length: 42 }, (_, i) => addDays(gridStart, i));
  const todayStr = new Date().toDateString();
  const dow = Array.from({ length: 7 }, (_, i) => addDays(gridStart, i).toLocaleDateString(i18n.language, { weekday: "short" }));
  const dayMeetings = (d: Date) => meetings.filter((m) => sameDay(new Date(m.scheduled_start!), d));
  return (
    <div className="ag-month">
      <div className="ag-dow">{dow.map((w, i) => <span key={i}>{w}</span>)}</div>
      <div className="ag-mgrid">
        {cells.map((d, i) => {
          const inMonth = d.getMonth() === cursor.getMonth();
          const items = dayMeetings(d);
          return (
            <div key={i} className={"ag-mcell" + (inMonth ? "" : " out") + (d.toDateString() === todayStr ? " today" : "")}>
              <span className="ag-mday">{d.getDate()}</span>
              {items.slice(0, 3).map((m) => (
                <button key={m.id} className={"ag-mchip" + (m.status === "live" ? " live" : "")} onClick={() => onOpen(m)}>
                  {new Date(m.scheduled_start!).toLocaleTimeString(i18n.language, { hour: "2-digit", minute: "2-digit" })} {m.title}
                </button>
              ))}
              {items.length > 3 && <span className="ag-more">+{items.length - 3}</span>}
            </div>
          );
        })}
      </div>
    </div>
  );
}

// period navigation (day/week/month)
export function useAgendaNav() {
  const [view, setView] = useState<"list" | View>("list");
  const [cursor, setCursor] = useState(new Date());
  const step = (dir: number) => setCursor((c) => {
    if (view === "day") return addDays(c, dir);
    if (view === "week") return addDays(c, dir * 7);
    return new Date(c.getFullYear(), c.getMonth() + dir, 1);
  });
  return { view, setView, cursor, setCursor, step };
}

export function agendaLabel(view: string, cursor: Date, lang: string): string {
  if (view === "day") return cursor.toLocaleDateString(lang, { weekday: "long", day: "2-digit", month: "long" });
  if (view === "week") { const s = weekStart(cursor), e = addDays(s, 6); return `${s.getDate()} – ${e.getDate()} ${e.toLocaleDateString(lang, { month: "long", year: "numeric" })}`; }
  return cursor.toLocaleDateString(lang, { month: "long", year: "numeric" });
}
