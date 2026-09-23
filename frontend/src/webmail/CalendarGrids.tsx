import { useState, useRef, useEffect } from "react";
import { useTranslation } from "react-i18next";

import { type CalEvent } from "./backend";
import { hueFor } from "./util";
import { IcVideo } from "../panel/icons";

// Calendar grids (SyncWise replica): PASTEL cards — background is the category color mixed with
// white, fixed dark text (same in both themes, matching the reference), guest avatars at the
// bottom of the card, and a violet "now" line.
export const ROW = 88; // px per hour — tall cards like the reference
const DAY_START = 0, DAY_END = 24, BUSINESS_HOUR = 7;

const startOfDay = (d: Date) => { const x = new Date(d); x.setHours(0, 0, 0, 0); return x; };
const sameDay = (a: Date, b: Date) => a.toDateString() === b.toDateString();
const coversDay = (e: CalEvent, day: Date) => {
  const d0 = startOfDay(day).getTime(), d1 = d0 + 86400000;
  return new Date(e.starts_at).getTime() < d1 && new Date(e.ends_at).getTime() >= d0;
};
const isSpanning = (e: CalEvent) => e.all_day || !sameDay(new Date(e.starts_at), new Date(e.ends_at));

type ColorOf = (e: CalEvent) => string;
type Busy = { starts_at: string; ends_at: string }[];

const fmtT = (d: Date, lang: string) => d.toLocaleTimeString(lang, { hour: "2-digit", minute: "2-digit" });
// Absences store the fallback title in English; the UI shows the label translated by kind.
const evTitle = (e: CalEvent, t: (k: string) => string) => e.kind ? t("cal.kind_" + e.kind) : e.title;

// Card avatar stack: colleague's photo or initial in the person's color; name/email tooltip.
function AvStack({ att, max = 4 }: { att: NonNullable<CalEvent["attendees"]>; max?: number }) {
  if (att.length === 0) return null;
  return (
    <span className="csw-avs">
      {att.slice(0, max).map((a) => a.avatar
        ? <img key={a.email} src={a.avatar} alt="" title={a.name ? `${a.name} (${a.email})` : a.email} />
        : <i key={a.email} style={{ background: hueFor(a.email) }} title={a.name ? `${a.name} (${a.email})` : a.email}>{(a.name || a.email).slice(0, 1).toUpperCase()}</i>)}
      {att.length > max && <em>+{att.length - max}</em>}
    </span>
  );
}

export function MonthGrid({ cursor, events, busy = [], busyLabel = "", busyColor, colorOf, workDays, onDay, onEvent, lang, weekStartsOn }: {
  cursor: Date; events: CalEvent[]; busy?: Busy; busyLabel?: string; busyColor?: string; colorOf: ColorOf;
  workDays?: number[]; onDay: (d: Date) => void; onEvent: (e: CalEvent) => void; lang: string; weekStartsOn: number;
}) {
  const isWork = (d: Date) => !workDays || workDays.includes(d.getDay());
  const { t } = useTranslation();
  const first = new Date(cursor.getFullYear(), cursor.getMonth(), 1);
  const gridStart = (() => { const x = startOfDay(first); x.setDate(x.getDate() - ((x.getDay() - weekStartsOn + 7) % 7)); return x; })();
  const cells = Array.from({ length: 42 }, (_, i) => { const d = new Date(gridStart); d.setDate(d.getDate() + i); return d; });
  const todayStr = new Date().toDateString();
  const dow = Array.from({ length: 7 }, (_, i) => cells[i].toLocaleDateString(lang, { weekday: "short" }));
  const forDay = (d: Date) => events.filter((e) => coversDay(e, d)).sort((a, b) => (Number(isSpanning(b)) - Number(isSpanning(a))) || a.starts_at.localeCompare(b.starts_at));
  const busyForDay = (d: Date) => busy.filter((b) => sameDay(new Date(b.starts_at), d)).sort((a, b) => a.starts_at.localeCompare(b.starts_at));
  return (
    <div className="cal2-month">
      <div className="cm-dow">{dow.map((w, i) => <span key={i}>{w}</span>)}</div>
      <div className="cm-grid">
        {cells.map((d, i) => {
          const inMonth = d.getMonth() === cursor.getMonth();
          const items = forDay(d);
          const busyItems = busyForDay(d);
          return (
            <div key={i} className={"cm-cell" + (inMonth ? "" : " out") + (d.toDateString() === todayStr ? " today" : "") + (isWork(d) ? "" : " offwork")}
              onClick={() => { if (isWork(d)) onDay(d); }}>
              <span className="cm-day">{d.getDate()}</span>
              <div className="cm-events">
                {items.slice(0, 3).map((e) => {
                  const past = new Date(e.ends_at) < new Date();
                  const cls = "csw-ev sm" + (isSpanning(e) ? " allday" : "") + (e.invited ? " invited" : "") + (past ? " past" : "") + (e.booking_id ? " bkd" : "") + (e.kind ? " koff" : "");
                  return (
                    <button key={e.id} className={cls} style={{ "--evc": colorOf(e) } as React.CSSProperties}
                      onClick={(ev) => { ev.stopPropagation(); onEvent(e); }}>
                      <span className="csw-ev-t">{e.meeting_room && <IcVideo size={11} />}{evTitle(e, t)}{e.booking_id && <em className="bkd-tag">{t("cal.booked")}</em>}{e.recur && e.recur !== "none" && <em className="rec">↻</em>}</span>
                      {!isSpanning(e) && (
                        <span className="csw-ev-b">
                          <span className="tm">{fmtT(new Date(e.starts_at), lang)}{e.location ? ` · ${e.location}` : ""}</span>
                          <AvStack att={e.attendees ?? []} max={3} />
                        </span>
                      )}
                    </button>
                  );
                })}
                {items.length > 3 && <span className="cm-more">+{items.length - 3}</span>}
                {busyItems.slice(0, 2).map((b, bi) => (
                  <span key={"fb" + bi} className="csw-ev sm busy" style={{ color: busyColor }} title={busyLabel || t("cal.busy")}>
                    {`${fmtT(new Date(b.starts_at), lang)} ${busyLabel || t("cal.busy")}`}
                  </span>
                ))}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// Agenda/list: next `days` days with events, compact pastel cards.
export function ListView({ from, days, events, colorOf, onEvent, lang }: {
  from: Date; days: number; events: CalEvent[]; colorOf: ColorOf; onEvent: (e: CalEvent) => void; lang: string;
}) {
  const { t } = useTranslation();
  const todayStr = new Date().toDateString();
  const rows = Array.from({ length: days }, (_, i) => { const d = new Date(from); d.setDate(d.getDate() + i); return d; })
    .map((d) => ({ d, items: events.filter((e) => coversDay(e, d)).sort((a, b) => (Number(isSpanning(b)) - Number(isSpanning(a))) || a.starts_at.localeCompare(b.starts_at)) }))
    .filter((r) => r.items.length > 0);
  if (rows.length === 0) return <div className="cal2-listempty">{t("cal.listEmpty")}</div>;
  return (
    <div className="cal2-list">
      {rows.map(({ d, items }) => (
        <div key={d.toISOString()} className={"cl-day" + (d.toDateString() === todayStr ? " today" : "")}>
          <div className="cl-date"><b>{d.getDate()}</b><span>{d.toLocaleDateString(lang, { weekday: "short", month: "short" })}</span></div>
          <div className="cl-events">
            {items.map((e) => (
              <button key={e.id + d.toISOString()} className="csw-ev row" style={{ "--evc": colorOf(e) } as React.CSSProperties} onClick={() => onEvent(e)}>
                <span className="csw-ev-t">{e.meeting_room && <IcVideo size={12} />}{evTitle(e, t)}</span>
                <span className="tm">{isSpanning(e) ? t("cal.allDay") : `${fmtT(new Date(e.starts_at), lang)} - ${fmtT(new Date(e.ends_at), lang)}`}{e.location ? ` · ${e.location}` : ""}</span>
                <AvStack att={e.attendees ?? []} max={4} />
              </button>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

export function TimeGrid({ days, events, busy = [], busyLabel = "", busyColor, colorOf, workDays, onSlot, onEvent, onReschedule, lang }: {
  days: Date[]; events: CalEvent[]; busy?: Busy; busyLabel?: string; busyColor?: string; colorOf: ColorOf;
  workDays?: number[]; onSlot: (d: Date) => void; onEvent: (e: CalEvent) => void; onReschedule: (e: CalEvent, start: Date, end: Date) => void; lang: string;
}) {
  const isWork = (d: Date) => !workDays || workDays.includes(d.getDay());
  const { t } = useTranslation();
  const hours = Array.from({ length: DAY_END - DAY_START }, (_, i) => DAY_START + i);
  const todayStr = new Date().toDateString();
  const gridRef = useRef<HTMLDivElement>(null);
  const innerRef = useRef<HTMLDivElement>(null);
  const key = days[0]?.toDateString();
  useEffect(() => {
    const sc = gridRef.current?.closest(".cal2-body") as HTMLElement | null;
    if (sc) sc.scrollTop = (BUSINESS_HOUR - DAY_START) * ROW;
  }, [key]);
  // The "now" line (violet, like the reference) — updates every minute.
  const [now, setNow] = useState(() => new Date());
  useEffect(() => { const id = setInterval(() => setNow(new Date()), 60000); return () => clearInterval(id); }, []);

  // Drag (move) and resize — 15 min snap; horizontal changes the day. Recurring/invited events don't.
  const SNAP = 15 * 60000;
  const GUTTER = 74;
  const [drag, setDrag] = useState<null | { e: CalEvent; mode: "move" | "resize"; startX: number; startY: number; origS: number; origE: number; curS: number; curE: number; moved: boolean }>(null);
  const dragRef = useRef(drag); dragRef.current = drag;
  const swallowClick = useRef(false);
  const canDrag = (e: CalEvent) => !e.recur && !e.invited;
  const startDrag = (ev: React.MouseEvent, e: CalEvent, mode: "move" | "resize") => {
    if (!canDrag(e) || ev.button !== 0) return;
    ev.preventDefault(); ev.stopPropagation();
    const s = new Date(e.starts_at).getTime(), en = new Date(e.ends_at).getTime();
    setDrag({ e, mode, startX: ev.clientX, startY: ev.clientY, origS: s, origE: en, curS: s, curE: en, moved: false });
  };
  useEffect(() => {
    if (!drag) return;
    const snap = (ms: number) => Math.round(ms / SNAP) * SNAP;
    const onMove = (ev: MouseEvent) => {
      const d = dragRef.current; if (!d) return;
      const dyMs = ((ev.clientY - d.startY) / ROW) * 3600000;
      let curS = d.origS, curE = d.origE;
      if (d.mode === "resize") { curE = Math.max(d.origS + SNAP, snap(d.origE + dyMs)); }
      else { curS = snap(d.origS + dyMs); curE = curS + (d.origE - d.origS); }
      setDrag({ ...d, curS, curE, moved: d.moved || Math.abs(ev.clientY - d.startY) > 3 || Math.abs(ev.clientX - d.startX) > 3 });
    };
    const onUp = (ev: MouseEvent) => {
      const d = dragRef.current;
      setDrag(null);
      if (!d || !d.moved) return;
      swallowClick.current = true;
      let curS = d.curS, curE = d.curE;
      if (d.mode === "move" && days.length > 1 && innerRef.current) {
        const cw = (innerRef.current.clientWidth - GUTTER) / days.length;
        const dxCols = cw > 0 ? Math.round((ev.clientX - d.startX) / cw) : 0;
        if (dxCols !== 0) { const sh = dxCols * 86400000; curS += sh; curE += sh; }
      }
      if (curS !== d.origS || curE !== d.origE) onReschedule(d.e, new Date(curS), new Date(curE));
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => { window.removeEventListener("mousemove", onMove); window.removeEventListener("mouseup", onUp); };
  }, [drag, days.length, onReschedule]);

  const blocks = (day: Date) => events.filter((e) => !isSpanning(e) && sameDay(new Date(e.starts_at), day)).map((e) => {
    const s = new Date(e.starts_at), en = new Date(e.ends_at);
    const top = (s.getHours() - DAY_START + s.getMinutes() / 60) * ROW;
    const h = Math.max(26, ((en.getTime() - s.getTime()) / 3600e3) * ROW);
    return { e, top, h };
  });
  const busyFor = (day: Date) => busy.filter((b) => sameDay(new Date(b.starts_at), day)).map((b) => {
    const s = new Date(b.starts_at), en = new Date(b.ends_at);
    return { key: b.starts_at, top: (s.getHours() - DAY_START + s.getMinutes() / 60) * ROW, h: Math.max(10, ((en.getTime() - s.getTime()) / 3600e3) * ROW) };
  });
  const spanning = events.filter(isSpanning);
  const cols = `${GUTTER}px repeat(${days.length}, 1fr)`;
  // GMT+X in the header corner, like the reference.
  const off = -new Date().getTimezoneOffset() / 60;
  const gmt = `GMT${off >= 0 ? "+" : ""}${off}`;
  const nowTop = (now.getHours() - DAY_START + now.getMinutes() / 60) * ROW;
  return (
    <div className="cal2-timewrap" ref={gridRef}>
      <div className="ct-head" style={{ gridTemplateColumns: cols }}>
        <div className="ct-corner"><span className="csw-gmt">{gmt}</span></div>
        {days.map((d, i) => (
          <div key={i} className={"ct-dayh" + (d.toDateString() === todayStr ? " today" : "") + (isWork(d) ? "" : " offwork")}>
            <b>{d.getDate()}</b><span>{d.toLocaleDateString(lang, { weekday: "short" })}</span>
          </div>
        ))}
        <div className="cta-label">{t("cal.allDay")}</div>
        {days.map((d, i) => (
          <div key={i} className="cta-col">
            {spanning.filter((e) => coversDay(e, d)).map((e) => (
              <button key={e.id} className="cta-ev" style={{ "--evc": colorOf(e) } as React.CSSProperties} onClick={(ev) => { ev.stopPropagation(); onEvent(e); }}>{evTitle(e, t)}</button>
            ))}
          </div>
        ))}
      </div>
      <div className="ct-grid" style={{ gridTemplateColumns: cols }} ref={innerRef}>
        <div className="ct-hours">{hours.map((h) => <div key={h} className="ct-hr"><span>{fmtT(new Date(2000, 0, 1, h), lang)}</span></div>)}</div>
        {days.map((d, i) => (
          <div key={i} className={"ct-col" + (d.toDateString() === todayStr ? " today" : "") + (isWork(d) ? "" : " offwork")} style={{ height: hours.length * ROW }}>
            {hours.map((h) => <div key={h} className="ct-cell" style={{ height: ROW }} onClick={() => { if (isWork(d)) { const nd = new Date(d); nd.setHours(h, 0, 0, 0); onSlot(nd); } }} />)}
            {busyFor(d).map((b) => (
              <div key={"fb" + b.key} className="ct-busy" style={{ top: b.top, height: b.h, borderLeftColor: busyColor, color: busyColor }} title={busyLabel || t("cal.busy")}>
                {b.h >= 26 && <span className="ct-busy-lb">{busyLabel || t("cal.busy")}</span>}
              </div>
            ))}
            {blocks(d).map(({ e, top, h }) => {
              const isDrag = drag?.e.id === e.id;
              const cs = isDrag ? new Date(drag!.curS) : new Date(e.starts_at);
              const ce = isDrag ? new Date(drag!.curE) : new Date(e.ends_at);
              const bt = isDrag ? (cs.getHours() - DAY_START + cs.getMinutes() / 60) * ROW : top;
              const bh = isDrag ? Math.max(26, (drag!.curE - drag!.curS) / 3600000 * ROW) : h;
              const att = e.attendees ?? [];
              return (
                <button key={e.id} className={"csw-ev block" + (isDrag ? " dragging" : "") + (canDrag(e) ? " draggable" : "") + (e.invited ? " invited" : "") + (e.booking_id ? " bkd" : "")}
                  style={{ top: bt, height: bh, "--evc": colorOf(e) } as React.CSSProperties}
                  onMouseDown={(ev) => startDrag(ev, e, "move")}
                  onClick={(ev) => { ev.stopPropagation(); if (swallowClick.current) { swallowClick.current = false; return; } onEvent(e); }}>
                  <span className="csw-ev-t">{e.meeting_room && <IcVideo size={12} />}{evTitle(e, t)}{e.booking_id && <em className="bkd-tag">{t("cal.booked")}</em>}{e.recur && e.recur !== "none" && <em className="rec">↻</em>}</span>
                  {bh >= 44 && <span className="tm">{fmtT(cs, lang)} - {fmtT(ce, lang)}</span>}
                  {bh >= 78 && att.length > 0 && <span className="csw-ev-foot"><AvStack att={att} /></span>}
                  {canDrag(e) && <span className="ct-ev-rs" onMouseDown={(ev) => startDrag(ev, e, "resize")} />}
                </button>
              );
            })}
            {d.toDateString() === todayStr && (
              <div className="csw-now" style={{ top: nowTop }}><i /></div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
