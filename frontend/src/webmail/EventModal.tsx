import { useState, useEffect, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { backend, ConflictError, type CalEvent, type EventInput, type Slot } from "./backend";
import { useUserSettings } from "./useSettings";
import { TagPicker } from "./TagPicker";
import { InviteePicker, type Invitee } from "./InviteePicker";
import { Drawer } from "./Drawer";
import { Spinner } from "./Spinner";
import { toast, toastAction } from "./toast";
import { initials, hueFor } from "./util";
import { IcSparkle, IcVideo, IcTrash, IcCheck, IcX, IcCopy, IcPencil } from "../panel/icons";

const COLORS = ["#B4785F", "#3B6FB0", "#8257C7", "#B44D3A", "#2E7D5B", "#C0692F", "#C9871F"];
const pad = (n: number) => String(n).padStart(2, "0");
const toDate = (iso: string) => { const d = new Date(iso); return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`; };
const toTime = (iso: string) => { const d = new Date(iso); return `${pad(d.getHours())}:${pad(d.getMinutes())}`; };

// Create/edit event modal: form + tags (catalog) + AI (natural language) + Meet room +
// schedule-conflict warning (no alert/prompt).
export function EventModal({ initial, day, prefillTitle, onClose, onSaved }: {
  initial?: CalEvent | null; day?: Date; prefillTitle?: string; onClose: () => void; onSaved: () => void;
}) {
  const { t, i18n } = useTranslation();
  const settings = useUserSettings(); // default duration/reminder (settings.calendar)
  // Opens an existing event in READ mode; a new event opens straight into editing. The pencil
  // toggles it — the same view→edit gesture as the task, now on the event too.
  const [editing, setEditing] = useState(!initial);
  const base = day ?? new Date();
  // Default end of a new event = start + default duration (from Settings, otherwise 60 min).
  const startHour = base.getHours() || 9;
  const endDefault = new Date(base); endDefault.setHours(startHour, 0, 0, 0);
  endDefault.setMinutes(endDefault.getMinutes() + (settings.calendar?.defaultDuration ?? 60));
  const [title, setTitle] = useState(initial?.title ?? prefillTitle ?? "");
  const [date, setDate] = useState(initial ? toDate(initial.starts_at) : `${base.getFullYear()}-${pad(base.getMonth() + 1)}-${pad(base.getDate())}`);
  const [endDate, setEndDate] = useState(initial ? toDate(initial.ends_at) : `${endDefault.getFullYear()}-${pad(endDefault.getMonth() + 1)}-${pad(endDefault.getDate())}`);
  const [allDay, setAllDay] = useState(initial?.all_day ?? false);
  const [start, setStart] = useState(initial ? toTime(initial.starts_at) : `${pad(startHour)}:00`);
  const [end, setEnd] = useState(initial ? toTime(initial.ends_at) : `${pad(endDefault.getHours())}:${pad(endDefault.getMinutes())}`);
  const [location, setLocation] = useState(initial?.location ?? "");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [color, setColor] = useState(initial?.color ?? COLORS[0]);
  const [tags, setTags] = useState<string[]>(initial?.tags ?? []);
  const [createMeet, setCreateMeet] = useState(false);
  const [meetRoom, setMeetRoom] = useState(initial?.meeting_room ?? "");
  const qc = useQueryClient();
  const addMeet = useMutation({
    mutationFn: () => backend.addMeetToEvent(initial!.id),
    onSuccess: (r) => { setMeetRoom(r.meeting_room); toast(t("cal.meetAdded")); onSaved(); },
    onError: (e) => toast((e as Error).message),
  });
  const [remind, setRemind] = useState<number>(initial?.remind_minutes ?? settings.calendar?.defaultReminder ?? 10);
  const [calendarId, setCalendarId] = useState<string>(initial?.calendar_id ?? "");
  const calsQ = useQuery({ queryKey: ["calendars"], queryFn: () => backend.calendars(), retry: false });
  const [recur, setRecur] = useState<string>(initial?.recur ?? "");
  const [recurUntil, setRecurUntil] = useState<string>(initial?.recur_until ? toDate(initial.recur_until) : "");
  const [ai, setAi] = useState("");
  const [conflicts, setConflicts] = useState<CalEvent[] | null>(null);
  const [slots, setSlots] = useState<Slot[] | null>(null);
  const invited = !!initial?.invited; // I'm an invitee (not the owner) → RSVP view, not editing

  // Guests (only the owner edits): internal + external already-invited (with status).
  const existingInvites = useQuery({ queryKey: ["event-invites", initial?.id], queryFn: () => backend.eventInvites(initial!.id), enabled: !!initial && !invited, retry: false });
  const [guests, setGuests] = useState<Invitee[]>([]);
  const existingExternals = useQuery({ queryKey: ["event-externals", initial?.id], queryFn: () => backend.eventExternals(initial!.id), enabled: !!initial && !invited, retry: false });
  useEffect(() => {
    if (!existingInvites.data && !existingExternals.data) return;
    const internal: Invitee[] = (existingInvites.data ?? []).map((i) => ({ email: i.email, name: i.name, userId: i.user_id, status: i.status }));
    const external: Invitee[] = (existingExternals.data ?? []).map((e) => ({ email: e }));
    setGuests([...internal, ...external]);
  }, [existingInvites.data, existingExternals.data]);
  const [myRsvp, setMyRsvp] = useState(initial?.my_rsvp ?? "");
  const rsvp = useMutation({
    mutationFn: (status: string) => backend.rsvpEvent(initial!.id, status),
    // Reflects the choice IMMEDIATELY (button shows as selected) and updates the calendar in the
    // background, without closing the sidecar — closing it made the action look like it had failed.
    onSuccess: (_r, status) => { setMyRsvp(status); toast(t("cal.rsvpSaved")); qc.invalidateQueries({ queryKey: ["events"] }); },
    onError: (e) => toast((e as Error).message),
  });

  // Suggest a free time slot (AI): uses the current duration and searches within the day's
  // business window.
  const durationMin = () => Math.max(15, (new Date(`${date}T${end}`).getTime() - new Date(`${date}T${start}`).getTime()) / 60000);
  const suggest = useMutation({
    mutationFn: () => backend.aiSuggestSlots(durationMin(), new Date(`${date}T08:00`).toISOString(), new Date(`${date}T19:00`).toISOString()),
    onSuccess: (r) => { setSlots(r.slots); if (!r.slots.length) toast(t("cal.noSlots")); },
    onError: (e) => toast((e as Error).message),
  });
  const pickSlot = (s: Slot) => { setDate(toDate(s.starts_at)); setStart(toTime(s.starts_at)); setEnd(toTime(s.ends_at)); setSlots(null); setConflicts(null); };

  const parse = useMutation({
    mutationFn: () => backend.aiParseEvent(ai.trim()),
    onSuccess: (d) => {
      if (d.title) setTitle(d.title);
      if (d.starts_at) { setDate(toDate(d.starts_at)); setStart(toTime(d.starts_at)); }
      if (d.ends_at) setEnd(toTime(d.ends_at));
      if (d.tags?.length) setTags((v) => Array.from(new Set([...v, ...d.tags])));
      setAi("");
    },
    onError: (e) => toast((e as Error).message),
  });

  const body = (force: boolean): EventInput => {
    const common = { title: title.trim(), location, description, color, tags, create_meet: createMeet, force, remind_minutes: remind, recur, recur_until: recur && recurUntil ? new Date(`${recurUntil}T23:59:59`).toISOString() : "", invitee_ids: guests.filter((g) => g.userId).map((g) => g.userId!), external_emails: guests.filter((g) => !g.userId).map((g) => g.email), calendar_id: calendarId };
    if (allDay) {
      // All day: no time, from the start of the first day to the end of the last day (inclusive) —
      // covers multi-day events.
      return { ...common, all_day: true, starts_at: new Date(`${date}T00:00`).toISOString(), ends_at: new Date(`${endDate}T23:59:59`).toISOString(), remind_minutes: 0 };
    }
    return { ...common, all_day: false, starts_at: new Date(`${date}T${start}`).toISOString(), ends_at: new Date(`${date}T${end}`).toISOString() };
  };
  const save = useMutation({
    mutationFn: (force: boolean) => initial ? backend.updateEvent(initial.id, body(force)) : backend.createEvent(body(force)),
    onSuccess: () => { toast(t(initial ? "cal.updated" : "cal.created")); onSaved(); },
    onError: (e) => { if (e instanceof ConflictError) setConflicts(e.conflicts); else toast((e as Error).message); },
  });
  const restore = useMutation({ mutationFn: (id: string) => backend.restoreEvent(id), onSuccess: () => { toast(t("cal.restored")); onSaved(); }, onError: (e) => toast((e as Error).message) });
  // Delete with "undo" (soft-delete on the backend): the toast window gives time to restore by id.
  const del = useMutation({
    mutationFn: () => backend.deleteEvent(initial!.id),
    onSuccess: () => { const id = initial!.id; toastAction(t("cal.deleted"), t("common.undo"), () => restore.mutate(id)); onSaved(); },
    onError: (e) => toast((e as Error).message),
  });
  const duplicate = useMutation({
    mutationFn: () => backend.duplicateEvent(initial!.id, initial!.title + " " + t("cal.copySuffix")),
    onSuccess: () => { toast(t("cal.duplicated")); onSaved(); },
    onError: (e) => toast((e as Error).message),
  });

  // Invitee (not the owner): doesn't edit — only responds to the invitation (RSVP).
  if (invited && initial) {
    return (
      <Drawer title={t("cal.invitation")} onClose={onClose}
        footer={<><div className="grow" /><button className="chipbtn" onClick={onClose}>{t("common.close")}</button></>}>
        <div className="meet-form">
          <h3 style={{ margin: "0 0 2px" }}>{initial.title}</h3>
          <div className="rsvp-when">{new Date(initial.starts_at).toLocaleString(i18n.language, { weekday: "long", day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" })}</div>
          {initial.location && <div className="rsvp-loc">{initial.location}</div>}
          {initial.description && <p className="rsvp-desc">{initial.description}</p>}
          <div className="rsvp-block">
            <span className="rsvp-q">{t("cal.yourResponse")}</span>
            <div className="rsvp-btns">
              <button className={"rsvp-btn ok" + (myRsvp === "accepted" ? " on" : "")} disabled={rsvp.isPending} onClick={() => rsvp.mutate("accepted")}><IcCheck size={14} /> {t("cal.accept")}</button>
              <button className={"rsvp-btn" + (myRsvp === "maybe" ? " on" : "")} disabled={rsvp.isPending} onClick={() => rsvp.mutate("maybe")}>{t("cal.maybe")}</button>
              <button className={"rsvp-btn no" + (myRsvp === "declined" ? " on" : "")} disabled={rsvp.isPending} onClick={() => rsvp.mutate("declined")}><IcX size={14} /> {t("cal.decline")}</button>
            </div>
          </div>
        </div>
      </Drawer>
    );
  }

  // Owner, existing event, in READ mode: an event summary with the pencil to edit.
  if (initial && !editing) {
    const s = new Date(initial.starts_at), e = new Date(initial.ends_at);
    const when = initial.all_day
      ? `${s.toLocaleDateString(i18n.language, { weekday: "long", day: "2-digit", month: "long" })} · ${t("cal.allDay")}`
      : `${s.toLocaleDateString(i18n.language, { weekday: "long", day: "2-digit", month: "long" })} · ${s.toLocaleTimeString(i18n.language, { hour: "2-digit", minute: "2-digit" })}–${e.toLocaleTimeString(i18n.language, { hour: "2-digit", minute: "2-digit" })}`;
    const calName = (calsQ.data ?? []).find((c) => c.id === initial.calendar_id)?.name;
    return (
      <Drawer title={initial.title} onClose={onClose}
        footer={<>
          <button className="mc-cancel" onClick={() => del.mutate()}><IcTrash size={14} /> {t("cal.delete")}</button>
          <button className="chipbtn" disabled={duplicate.isPending} onClick={() => duplicate.mutate()}><IcCopy size={14} /> {t("cal.duplicate")}</button>
          <div className="grow" />
          <button className="add" onClick={() => setEditing(true)}><IcPencil size={14} /> {t("cal.editEvent")}</button>
        </>}>
        <div className="tk-read">
          <div className="ev-when"><span className="ev-dot" style={{ background: initial.color }} /> {when}</div>
          <dl className="tk-meta">
            {initial.location && <Row label={t("cal.location")}>{initial.location}</Row>}
            {(initial.attendees ?? guests).length > 0 && (
              <Row label={t("cal.invitees")}>
                <span className="ev-guests">
                  {(initial.attendees ?? []).map((a) => (
                    <span key={a.email} className="tk-who" title={a.email}>
                      {a.avatar
                        ? <img className="av" src={a.avatar} alt="" style={{ objectFit: "cover" }} />
                        : <span className="av" style={{ background: hueFor(a.email) }}>{initials(a.name || a.email)}</span>}
                      {a.name || a.email}
                      {a.status === "organizer" && <span className="rsvp-tag maybe">{t("cal.organizer")}</span>}
                      {a.status === "external" && <span className="rsvp-tag">{t("invp.external")}</span>}
                      {a.status && !["organizer", "external", "pending", "invited"].includes(a.status) && <span className={"rsvp-tag " + a.status}>{t("cal.rsvp_" + a.status)}</span>}
                    </span>
                  ))}
                </span>
              </Row>
            )}
            {initial.recur && <Row label={t("cal.repeat")}>{t("cal.repeat" + initial.recur.charAt(0).toUpperCase() + initial.recur.slice(1))}</Row>}
            {typeof initial.remind_minutes === "number" && initial.remind_minutes > 0 && (
              <Row label={t("cal.reminder")}>{initial.remind_minutes >= 1440 ? t("cal.remindDay") : initial.remind_minutes >= 60 ? t("cal.remindHour") : t("cal.remindMin", { n: initial.remind_minutes })}</Row>
            )}
            {calName && <Row label={t("cal.calendar")}>{calName}</Row>}
            {(initial.tags ?? []).length > 0 && <Row label={t("cal.tags")}><span className="ev-tags">{(initial.tags ?? []).map((tg) => <span key={tg} className="tp-chip">{tg}</span>)}</span></Row>}
          </dl>
          {initial.description && <p className="tk-desc">{initial.description}</p>}
          {meetRoom && <a className="chipbtn" href={`/mail/meet/${meetRoom}`}><IcVideo size={15} /> {t("cal.joinMeet")}</a>}
        </div>
      </Drawer>
    );
  }

  return (
    <Drawer title={initial ? t("cal.editEvent") : t("cal.newEvent")} onClose={onClose}
      footer={<>
        {initial && <button className="mc-cancel" onClick={() => del.mutate()}><IcTrash size={14} /> {t("cal.delete")}</button>}
        {initial && <button className="chipbtn" disabled={duplicate.isPending} onClick={() => duplicate.mutate()}><IcCopy size={14} /> {t("cal.duplicate")}</button>}
        <div className="grow" />
        <button className="chipbtn" onClick={() => (initial ? setEditing(false) : onClose())}>{t("common.cancel")}</button>
        <button className="add" disabled={!title.trim() || save.isPending} onClick={() => save.mutate(false)}>{t("contacts.save")}</button>
      </>}>
      <div className="meet-form">
          {!initial && (
            <div className="ai-nl">
              <IcSparkle size={15} />
              <input value={ai} placeholder={t("cal.aiPlaceholder")} onChange={(e) => setAi(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter" && ai.trim()) parse.mutate(); }} />
              <button className="chipbtn" disabled={!ai.trim() || parse.isPending} onClick={() => parse.mutate()}>{parse.isPending ? <Spinner size={14} /> : t("cal.aiFill")}</button>
            </div>
          )}
          <label>{t("cal.title")}<input autoFocus value={title} onChange={(e) => setTitle(e.target.value)} placeholder={t("cal.titlePh")} /></label>
          <label className="ev-allday">
            <span className="switch"><input type="checkbox" checked={allDay} onChange={(e) => { setAllDay(e.target.checked); if (e.target.checked && endDate < date) setEndDate(date); }} /><span className="track" /></span>
            <span>{t("cal.allDay")}</span>
          </label>
          {allDay ? (
            <div className="row3">
              <label>{t("cal.date")}<input type="date" value={date} onChange={(e) => { setDate(e.target.value); if (endDate < e.target.value) setEndDate(e.target.value); }} /></label>
              <label>{t("cal.endDate")}<input type="date" value={endDate} min={date} onChange={(e) => setEndDate(e.target.value)} /></label>
            </div>
          ) : (
            <div className="row3">
              <label>{t("cal.date")}<input type="date" value={date} onChange={(e) => setDate(e.target.value)} /></label>
              <label>{t("cal.start")}<input type="time" value={start} onChange={(e) => setStart(e.target.value)} /></label>
              <label>{t("cal.end")}<input type="time" value={end} onChange={(e) => setEnd(e.target.value)} /></label>
            </div>
          )}
          <button className="ai-suggest" onClick={() => suggest.mutate()} disabled={suggest.isPending}>
            {suggest.isPending ? <Spinner size={14} /> : <IcSparkle size={14} />} {suggest.isPending ? t("webmail.ai.thinking") : t("cal.suggestSlots")}
          </button>
          {slots && slots.length > 0 && (
            <div className="slot-chips">
              {slots.map((s, i) => (
                <button key={i} type="button" className="slot-chip" onClick={() => pickSlot(s)}>
                  {new Date(s.starts_at).toLocaleDateString(undefined, { weekday: "short", day: "2-digit", month: "short" })} · {new Date(s.starts_at).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })}
                </button>
              ))}
            </div>
          )}
          <label>{t("cal.tags")}<TagPicker value={tags} onChange={setTags} /></label>
          <label>{t("cal.reminder")}
            <select value={remind} onChange={(e) => setRemind(Number(e.target.value))}>
              <option value={0}>{t("cal.remindNone")}</option>
              <option value={5}>{t("cal.remindMin", { n: 5 })}</option>
              <option value={10}>{t("cal.remindMin", { n: 10 })}</option>
              <option value={30}>{t("cal.remindMin", { n: 30 })}</option>
              <option value={60}>{t("cal.remindHour")}</option>
              <option value={1440}>{t("cal.remindDay")}</option>
            </select>
          </label>
          <div className="row3">
            <label>{t("cal.repeat")}
              <select value={recur} onChange={(e) => setRecur(e.target.value)}>
                <option value="">{t("cal.repeatNone")}</option>
                <option value="daily">{t("cal.repeatDaily")}</option>
                <option value="weekly">{t("cal.repeatWeekly")}</option>
                <option value="monthly">{t("cal.repeatMonthly")}</option>
                <option value="yearly">{t("cal.repeatYearly")}</option>
              </select>
            </label>
            {recur && <label>{t("cal.repeatUntil")}<input type="date" value={recurUntil} min={date} onChange={(e) => setRecurUntil(e.target.value)} /></label>}
          </div>
          <label>{t("cal.invitees")}</label>
          <InviteePicker value={guests} onChange={setGuests} />
          <label>{t("cal.location")}<input value={location} onChange={(e) => setLocation(e.target.value)} placeholder={t("cal.locationPh")} /></label>
          <label>{t("cal.notes")}<textarea className="ev-notes" value={description} onChange={(e) => setDescription(e.target.value)} /></label>
          <div className="ev-colors">{COLORS.map((c) => <button key={c} type="button" className={"ev-color" + (color === c ? " on" : "")} style={{ background: c }} onClick={() => setColor(c)} />)}</div>
          {(calsQ.data ?? []).length > 0 && (
            <label>{t("cal.calendar")}
              <select value={calendarId} onChange={(e) => setCalendarId(e.target.value)}>
                <option value="">{t("cal.noCalendar")}</option>
                {(calsQ.data ?? []).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
              </select>
            </label>
          )}
          {!initial && (
            <label className="ev-meet"><span><IcVideo size={16} /> {t("cal.createMeet")}</span>
              <span className="switch"><input type="checkbox" checked={createMeet} onChange={(e) => setCreateMeet(e.target.checked)} /><span className="track" /></span>
            </label>
          )}
          {meetRoom
            ? <a className="chipbtn" href={`/mail/meet/${meetRoom}`}><IcVideo size={15} /> {t("cal.joinMeet")}</a>
            : initial && <button type="button" className="chipbtn" disabled={addMeet.isPending} onClick={() => addMeet.mutate()}><IcVideo size={15} /> {t("cal.addMeet")}</button>}

          {conflicts && (
            <div className="ev-conflict">
              <b>{t("cal.conflictTitle")}</b>
              <ul>{conflicts.map((c) => <li key={c.id}>{c.title} · {new Date(c.starts_at).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })}</li>)}</ul>
              <div className="ev-conflict-act">
                <button className="chipbtn" onClick={() => setConflicts(null)}>{t("common.cancel")}</button>
                <button className="chipbtn" onClick={() => suggest.mutate()} disabled={suggest.isPending}>{suggest.isPending ? <Spinner size={13} /> : <IcSparkle size={13} />} {t("cal.suggestAlt")}</button>
                <button className="add" onClick={() => { setConflicts(null); save.mutate(true); }}>{t("cal.continueAnyway")}</button>
              </div>
            </div>
          )}
      </div>
    </Drawer>
  );
}

// Row is a label→value line in the read view (the same dl the task uses).
function Row({ label, children }: { label: string; children: ReactNode }) {
  return (<><dt>{label}</dt><dd>{children}</dd></>);
}
