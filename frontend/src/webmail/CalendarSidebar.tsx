import { useState } from "react";
import { useTranslation } from "react-i18next";

import { type Calendar, type Tag } from "./backend";
import { Drawer } from "./Drawer";
import { Avatar } from "./Avatar";
import { IcTrash, IcSearch } from "../panel/icons";

// Fixed pastel palette for categories: the user picks among these four; the event card gets a
// pastel background derived from it (color-mix in CSS).
export const CAT_COLORS = ["#F0926B", "#8B7CF6", "#62BEE9", "#53C08B"];
// Colors for personal calendars.
export const CAL_COLORS = ["#B4785F", "#e6a817", "#c0392b", "#2980b9", "#8e44ad", "#27ae60"];

// Calendar sidebar in the SAME pattern as the Contacts page: `rlabel` sections, `grp-item` rows
// (coloured dot + name), dashed `newg` button. Clicking a row hides/shows that calendar's or
// category's events.
export function CalendarSidebar({ cals, hiddenCals, onToggleCal, onAddCal, onDeleteCal, cats, hiddenCats, onToggleCat, onAddCat, onDeleteCat, colleagues, fbUser, setFbUser }: {
  cals: Calendar[]; hiddenCals: Set<string>; onToggleCal: (id: string) => void; onAddCal: () => void; onDeleteCal: (c: Calendar) => void;
  cats: Tag[]; hiddenCats: Set<string>; onToggleCat: (name: string) => void; onAddCat: () => void; onDeleteCat: (t: Tag) => void;
  colleagues: { id: string; name: string; email: string }[]; fbUser: string; setFbUser: (id: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <aside className="csw-side">
      <div className="rlabel" style={{ margin: "0 0 7px" }}>{t("cal.myCalendars")}</div>
      {cals.map((c) => (
        <div key={c.id} className={"grp-item" + (hiddenCals.has(c.id) ? " dim" : "")} onClick={() => onToggleCal(c.id)}>
          <span className="dot" style={{ background: c.color }} />
          <span className="gn">{c.name}</span>
          <span className="acts">
            <button title={t("cal.deleteCalendar")} onClick={(e) => { e.stopPropagation(); onDeleteCal(c); }}><IcTrash size={14} /></button>
          </span>
        </div>
      ))}
      <button className="newg" onClick={onAddCal}>+ {t("cal.addCalendar")}</button>

      <div className="rlabel">{t("cal.categories")}</div>
      {cats.map((c) => (
        <div key={c.id} className={"grp-item" + (hiddenCats.has(c.name) ? " dim" : "")} onClick={() => onToggleCat(c.name)}>
          <span className="dot" style={{ background: c.color }} />
          <span className="gn">{c.name}</span>
          <span className="acts">
            <button title={t("cal.deleteCategory")} onClick={(e) => { e.stopPropagation(); onDeleteCat(c); }}><IcTrash size={14} /></button>
          </span>
        </div>
      ))}
      <button className="newg" onClick={onAddCat}>+ {t("cal.addCategory")}</button>

      {colleagues.length > 0 && (
        <>
          <div className="rlabel">{t("cal.freeBusy")}</div>
          <ColleaguePicker colleagues={colleagues} value={fbUser} onChange={setFbUser} />
        </>
      )}
    </aside>
  );
}

// ColleaguePicker: pick ONE colleague by search (a plain select does not scale with many people).
// Same look as InviteePicker: input with a search icon + suggestions; the pick becomes a chip
// with ✕.
function ColleaguePicker({ colleagues, value, onChange }: {
  colleagues: { id: string; name: string; email: string }[]; value: string; onChange: (id: string) => void;
}) {
  const { t } = useTranslation();
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const sel = colleagues.find((p) => p.id === value);
  if (sel) {
    return (
      <span className="invp-chip csw-fbchip" title={sel.email}>
        <Avatar name={sel.name} email={sel.email} size={22} />
        <span className="nm">{sel.name || sel.email}</span>
        <button type="button" title={t("common.remove")} onClick={() => onChange("")}>✕</button>
      </span>
    );
  }
  const term = q.trim().toLowerCase();
  const hits = term ? colleagues.filter((p) => (p.name ?? "").toLowerCase().includes(term) || p.email.toLowerCase().includes(term)).slice(0, 6) : [];
  return (
    <div className="invp csw-fbpick">
      <div className="invp-search">
        <IcSearch size={14} />
        <input value={q} placeholder={t("cal.freeBusySearch")}
          onChange={(e) => { setQ(e.target.value); setOpen(true); }}
          onFocus={() => setOpen(true)}
          onBlur={() => setTimeout(() => setOpen(false), 150)} />
      </div>
      {open && hits.length > 0 && (
        <div className="invp-sugg">
          {hits.map((p) => (
            <button key={p.id} type="button" onMouseDown={(e) => { e.preventDefault(); onChange(p.id); setQ(""); }}>
              <Avatar name={p.name} email={p.email} size={26} />
              <span className="cinfo"><b>{p.name || p.email}</b><i>{p.email}</i></span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// NewCalendarDrawer: creates a calendar in a sidecar (house pattern — no native prompt()).
export function NewCalendarDrawer({ defaultColor, pending, onClose, onCreate }: {
  defaultColor: string; pending: boolean; onClose: () => void; onCreate: (name: string, color: string) => void;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [color, setColor] = useState(defaultColor);
  const submit = () => { if (name.trim()) onCreate(name.trim(), color); };
  return (
    <Drawer title={t("cal.addCalendar")} onClose={onClose}
      footer={<>
        <button className="pnl-btn ghost" onClick={onClose}>{t("common.cancel")}</button>
        <div className="grow" />
        <button className="pnl-btn primary" disabled={pending || !name.trim()} onClick={submit}>{t("common.create")}</button>
      </>}>
      <div className="pnl-field">
        <label>{t("cal.calName")}</label>
        <input className="pnl-input" autoFocus value={name} maxLength={60}
          onChange={(e) => setName(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") submit(); }} />
      </div>
      <div className="pnl-field">
        <label>{t("cal.calColor")}</label>
        <div className="cal-color-row">
          {CAL_COLORS.map((c) => (
            <button key={c} type="button" className={"cal-color-dot" + (c === color ? " on" : "")}
              style={{ background: c }} aria-label={c} onClick={() => setColor(c)} />
          ))}
        </div>
      </div>
    </Drawer>
  );
}

// NewCategoryDrawer: category = tenant-shared tag with a color from the fixed pastel palette.
export function NewCategoryDrawer({ pending, onClose, onCreate }: {
  pending: boolean; onClose: () => void; onCreate: (name: string, color: string) => void;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [color, setColor] = useState(CAT_COLORS[0]);
  const submit = () => { if (name.trim()) onCreate(name.trim(), color); };
  return (
    <Drawer title={t("cal.addCategory")} onClose={onClose}
      footer={<>
        <button className="pnl-btn ghost" onClick={onClose}>{t("common.cancel")}</button>
        <div className="grow" />
        <button className="pnl-btn primary" disabled={pending || !name.trim()} onClick={submit}>{t("common.create")}</button>
      </>}>
      <div className="pnl-field">
        <label>{t("cal.catName")}</label>
        <input className="pnl-input" autoFocus value={name} maxLength={40}
          onChange={(e) => setName(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") submit(); }} />
      </div>
      <div className="pnl-field">
        <label>{t("cal.calColor")}</label>
        <div className="cal-color-row">
          {CAT_COLORS.map((c) => (
            <button key={c} type="button" className={"cal-color-dot" + (c === color ? " on" : "")}
              style={{ background: c }} aria-label={c} onClick={() => setColor(c)} />
          ))}
        </div>
      </div>
    </Drawer>
  );
}
