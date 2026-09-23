import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { backend, type TimeOffConflict } from "./backend";
import { Drawer } from "./Drawer";
import { Switch } from "./Switch";
import { toast } from "./toast";

// Time off: a day off (full day OR an hour range) or vacation (a date range). Becomes an event
// with its own kind — booking blocks automatically during it. If it collides with events already
// scheduled, the backend returns the list and here we ask: cancel them (notifies every guest —
// internal by notification, external by email; other people's invites during it are declined) or
// keep them. Vacation can also turn on the email auto-reply (when the mail app is enabled).
const today = () => new Date().toISOString().slice(0, 10);
const mm = (m: number) => `${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`;
const toMin = (v: string) => { const [h, m] = v.split(":").map(Number); return (h || 0) * 60 + (m || 0); };

export function TimeOffDrawer({ mailEnabled, onClose, onSaved }: {
  mailEnabled: boolean; onClose: () => void; onSaved: () => void;
}) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [kind, setKind] = useState<"dayoff" | "vacation">("dayoff");
  const [start, setStart] = useState(today());
  const [end, setEnd] = useState(today());
  const [partial, setPartial] = useState(false);
  const [fromMin, setFromMin] = useState(9 * 60);
  const [toMinV, setToMinV] = useState(13 * 60);
  const [autoReply, setAutoReply] = useState(false);
  const [subject, setSubject] = useState(t("timeoff.replySubjectDefault"));
  const [body, setBody] = useState(t("timeoff.replyBodyDefault"));
  const [conflicts, setConflicts] = useState<TimeOffConflict[] | null>(null);

  const save = useMutation({
    mutationFn: (onConflict: string) => backend.timeOff({
      kind, start, end: kind === "vacation" ? end : start,
      ...(kind === "dayoff" && partial ? { start_min: fromMin, end_min: toMinV } : {}),
      auto_reply: kind === "vacation" && mailEnabled && autoReply,
      reply_subject: subject, reply_body: body, on_conflict: onConflict,
    }),
    onSuccess: (r) => {
      if (r.status === "conflicts") { setConflicts(r.conflicts ?? []); return; }
      qc.invalidateQueries({ queryKey: ["events"] });
      toast(r.auto_reply === "failed" ? t("timeoff.savedReplyFailed") : t("timeoff.saved"));
      onSaved();
    },
    onError: (e) => toast((e as Error).message),
  });

  const invalid = (kind === "vacation" && end < start) || (kind === "dayoff" && partial && fromMin >= toMinV);
  const fmtC = (c: TimeOffConflict) => {
    const s = new Date(c.starts_at);
    return s.toLocaleDateString(i18n.language, { day: "2-digit", month: "short" }) + " · " +
      s.toLocaleTimeString(i18n.language, { hour: "2-digit", minute: "2-digit" }) + "–" +
      new Date(c.ends_at).toLocaleTimeString(i18n.language, { hour: "2-digit", minute: "2-digit" });
  };

  return (
    <Drawer title={t("timeoff.title")} onClose={onClose}
      footer={conflicts ? <>
        <button className="pnl-btn ghost" onClick={() => setConflicts(null)}>{t("common.cancel")}</button>
        <div className="grow" />
        <button className="chipbtn" disabled={save.isPending} onClick={() => save.mutate("keep")}>{t("timeoff.keepAll")}</button>
        <button className="pnl-btn primary danger" disabled={save.isPending} onClick={() => save.mutate("cancel")}>{save.isPending ? "…" : t("timeoff.cancelAll")}</button>
      </> : <>
        <button className="pnl-btn ghost" onClick={onClose}>{t("common.cancel")}</button>
        <div className="grow" />
        <button className="pnl-btn primary" disabled={save.isPending || invalid} onClick={() => save.mutate("")}>{save.isPending ? "…" : t("timeoff.save")}</button>
      </>}>

      {conflicts ? (
        <>
          <div className="warnbox">
            <b>{t("timeoff.conflictsTitle", { count: conflicts.length })}</b>
            <span>{t("timeoff.conflictsHint")}</span>
          </div>
          <div className="to-conflicts">
            {conflicts.map((c) => (
              <div key={c.id} className="to-conflict">
                <b>{c.title}</b>
                <span>{fmtC(c)}</span>
                {!c.own && <em>{t("timeoff.willDecline")}</em>}
              </div>
            ))}
          </div>
        </>
      ) : (
        <>
          <div className="field"><label>{t("timeoff.kind")}</label>
            <div className="seg">
              <button type="button" className={"seg-opt" + (kind === "dayoff" ? " on" : "")} onClick={() => setKind("dayoff")}>{t("cal.kind_dayoff")}</button>
              <button type="button" className={"seg-opt" + (kind === "vacation" ? " on" : "")} onClick={() => setKind("vacation")}>{t("cal.kind_vacation")}</button>
            </div>
          </div>

          {kind === "dayoff" ? (
            <>
              <div className="field"><label>{t("timeoff.day")}</label>
                <input type="date" value={start} onChange={(e) => setStart(e.target.value)} />
              </div>
              <div className="field"><label>{t("timeoff.span")}</label>
                <div className="seg">
                  <button type="button" className={"seg-opt" + (!partial ? " on" : "")} onClick={() => setPartial(false)}>{t("timeoff.allDay")}</button>
                  <button type="button" className={"seg-opt" + (partial ? " on" : "")} onClick={() => setPartial(true)}>{t("timeoff.timeRange")}</button>
                </div>
              </div>
              {partial && (
                <div className="field"><label>{t("timeoff.timeRange")}</label>
                  <div className="bk-win">
                    <input type="time" value={mm(fromMin)} onChange={(e) => setFromMin(toMin(e.target.value))} />
                    <span>–</span>
                    <input type="time" value={mm(toMinV)} onChange={(e) => setToMinV(toMin(e.target.value))} />
                  </div>
                </div>
              )}
            </>
          ) : (
            <div className="field"><label>{t("timeoff.period")}</label>
              <div className="bk-win">
                <input type="date" value={start} onChange={(e) => { setStart(e.target.value); if (end < e.target.value) setEnd(e.target.value); }} />
                <span>–</span>
                <input type="date" value={end} min={start} onChange={(e) => setEnd(e.target.value)} />
              </div>
            </div>
          )}

          {kind === "vacation" && mailEnabled && (
            <>
              <div className="setrow" style={{ borderTop: "1px solid var(--border)", marginTop: 6 }}>
                <div className="setrow-tx"><b>{t("timeoff.autoReply")}</b><span className="desc">{t("timeoff.autoReplyHint")}</span></div>
                <Switch checked={autoReply} onChange={setAutoReply} label={t("timeoff.autoReply")} />
              </div>
              {autoReply && (
                <>
                  <div className="field"><label>{t("timeoff.replySubject")}</label>
                    <input value={subject} maxLength={120} onChange={(e) => setSubject(e.target.value)} /></div>
                  <div className="field"><label>{t("timeoff.replyBody")}</label>
                    <textarea rows={4} value={body} maxLength={2000} onChange={(e) => setBody(e.target.value)} /></div>
                </>
              )}
            </>
          )}
        </>
      )}
    </Drawer>
  );
}
