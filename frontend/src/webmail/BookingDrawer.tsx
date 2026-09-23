import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import { backend, type BookingCfg, type BookingWindow } from "./backend";
import { Drawer } from "./Drawer";
import { Switch } from "./Switch";
import { toast } from "./toast";

// Public booking page configuration (Calendly-style): weekly windows, meeting duration,
// whether phone/company are required, and the public URL to share.
const mm = (m: number) => `${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`;
const toMin = (v: string) => { const [h, m] = v.split(":").map(Number); return (h || 0) * 60 + (m || 0); };

export function BookingDrawer({ onClose }: { onClose: () => void }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["booking-page"], queryFn: () => backend.bookingPage(), retry: false });
  const [cfg, setCfg] = useState<BookingCfg | null>(null);
  const c = cfg ?? q.data ?? null;

  // Day names in the user's locale (Sunday = 0, JS convention).
  const weekdays = useMemo(() => Array.from({ length: 7 }, (_, d) =>
    new Date(2026, 2, 1 + d).toLocaleDateString(i18n.language, { weekday: "short" })), [i18n.language]); // March 1, 2026 is a Sunday

  const save = useMutation({
    mutationFn: (v: BookingCfg) => backend.bookingSave({ enabled: v.enabled, title: v.title, duration_min: v.duration_min, phone_field: v.phone_field, company_field: v.company_field, windows: v.windows }),
    onSuccess: (r) => { qc.setQueryData(["booking-page"], r); setCfg(r); toast(t("booking.saved")); },
    onError: (e) => toast((e as Error).message),
  });

  if (!c) return null;
  const set = (patch: Partial<BookingCfg>) => setCfg({ ...c, ...patch });
  const setWin = (i: number, patch: Partial<BookingWindow>) => set({ windows: c.windows.map((w, j) => j === i ? { ...w, ...patch } : w) });

  const seg = (field: "phone_field" | "company_field") => (
    <div className="seg">
      {(["off", "optional", "required"] as const).map((v) => (
        <button key={v} type="button" className={"seg-opt" + (c[field] === v ? " on" : "")} onClick={() => set({ [field]: v } as Partial<BookingCfg>)}>{t("booking." + v)}</button>
      ))}
    </div>
  );

  return (
    <Drawer title={t("booking.title")} onClose={onClose}
      footer={<>
        <button className="pnl-btn ghost" onClick={onClose}>{t("common.cancel")}</button>
        <div className="grow" />
        <button className="pnl-btn primary" disabled={save.isPending} onClick={() => save.mutate(c)}>{save.isPending ? "…" : t("booking.save")}</button>
      </>}>

      <div className="setrow">
        <div className="setrow-tx"><b>{t("booking.enable")}</b><span className="desc">{t("booking.enableHint")}</span></div>
        <Switch checked={c.enabled} onChange={(v) => set({ enabled: v })} label={t("booking.enable")} />
      </div>

      {c.url && (
        <div className="field"><label>{t("booking.publicUrl")}</label>
          <div className="bk-url">
            <input readOnly value={c.url} onFocus={(e) => e.currentTarget.select()} />
            <button type="button" className="chipbtn" onClick={() => { navigator.clipboard.writeText(c.url).then(() => toast(t("booking.copied"))); }}>{t("booking.copy")}</button>
          </div>
        </div>
      )}
      {!c.url && <p className="desc">{t("booking.urlAfterSave")}</p>}

      <div className="field"><label>{t("booking.pageTitle")}</label>
        <input value={c.title} maxLength={80} placeholder={t("booking.pageTitlePh")} onChange={(e) => set({ title: e.target.value })} />
      </div>

      <div className="field"><label>{t("booking.duration")}</label>
        <div className="seg">
          {[15, 30, 45, 60].map((d) => (
            <button key={d} type="button" className={"seg-opt" + (c.duration_min === d ? " on" : "")} onClick={() => set({ duration_min: d })}>{d} min</button>
          ))}
        </div>
      </div>

      <div className="field"><label>{t("booking.windows")}</label>
        {c.windows.length === 0 && <p className="desc">{t("booking.noWindows")}</p>}
        {c.windows.map((w, i) => (
          <div key={i} className="bk-win">
            <select value={w.weekday} onChange={(e) => setWin(i, { weekday: Number(e.target.value) })}>
              {weekdays.map((nm, d) => <option key={d} value={d}>{nm}</option>)}
            </select>
            <input type="time" value={mm(w.start)} onChange={(e) => setWin(i, { start: toMin(e.target.value) })} />
            <span>–</span>
            <input type="time" value={mm(w.end)} onChange={(e) => setWin(i, { end: toMin(e.target.value) })} />
            <button type="button" className="ib" title={t("common.remove")} onClick={() => set({ windows: c.windows.filter((_, j) => j !== i) })}>✕</button>
          </div>
        ))}
        <button type="button" className="newg" style={{ marginTop: 6 }}
          onClick={() => set({ windows: [...c.windows, { weekday: 1, start: 9 * 60, end: 17 * 60 }] })}>+ {t("booking.addWindow")}</button>
      </div>

      <div className="field"><label>{t("booking.phone")}</label>{seg("phone_field")}</div>
      <div className="field"><label>{t("booking.company")}</label>{seg("company_field")}</div>
    </Drawer>
  );
}
