import { useState, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { backend, type Meeting, type RsvpStatus, type Recording } from "./backend";
import { InviteePicker, type Invitee } from "./InviteePicker";
import { openRoom } from "./MeetRoom";
import { toast } from "./toast";
import { MeetAgenda, useAgendaNav, agendaLabel } from "./MeetAgenda";
import { IcVideo, IcPlus, IcCalendar, IcCheck, IcX, IcUsers, IcList, IcDownload, IcTrash, IcChevron } from "../panel/icons";

// Meet: home with upcoming meetings (scheduled/invited) and past ones (duration/date), RSVP and
// scheduling. The call uses LiveKit's ready-made component (grid/mute/camera/screen/leave).
export function MeetView({ roomParam, meetingParam }: {
  roomParam?: string;
  /** Meeting to open (/mail/meet/m/<id>) — the invite's destination. Shows the detail card; joining is the clicker's choice. */
  meetingParam?: string;
}) {
  const { t, i18n } = useTranslation();
  const nav = useNavigate();
  const qc = useQueryClient();
  const [code, setCode] = useState("");
  const [schedule, setSchedule] = useState(false);
  const [editMeeting, setEditMeeting] = useState<Meeting | null>(null);
  const [detail, setDetail] = useState<Meeting | null>(null);
  const [newAccess, setNewAccess] = useState<"public" | "approval">("approval");
  const ag = useAgendaNav();

  const [playing, setPlaying] = useState<Recording | null>(null);
  // SERVER-SIDE pagination: each section requests only its own page; the full list (meetList)
  // is used only by the day/week/month agenda.
  const PER = 6;
  const [upPage, setUpPage] = useState(1);
  const [pastPage, setPastPage] = useState(1);
  const [recPage, setRecPage] = useState(1);
  const list = useQuery({ queryKey: ["meetings"], queryFn: () => backend.meetList(), retry: false, enabled: ag.view !== "list" || !!meetingParam });
  const upQ = useQuery({ queryKey: ["meetings", "upcoming", upPage], queryFn: () => backend.meetListPage("upcoming", upPage, PER), retry: false });
  const pastQ = useQuery({ queryKey: ["meetings", "past", pastPage], queryFn: () => backend.meetListPage("past", pastPage, PER), retry: false });
  const recs = useQuery({ queryKey: ["recordings", recPage], queryFn: () => backend.meetRecordings(recPage, PER), retry: false });
  const autojoined = useRef<string | null>(null);
  // Create a meeting: the backend creates the room and we open its dedicated window.
  const join = useMutation({
    mutationFn: ({ room, access }: { room?: string; access?: string }) => backend.meetJoin(room, access),
    onSuccess: (s) => { openRoom(s.room); qc.invalidateQueries({ queryKey: ["meetings"] }); },
    onError: (e) => toast((e as Error).message),
  });

  // Old link /mail/meet/<room>: opens the room's window and returns to the Meet home.
  useEffect(() => {
    if (roomParam && autojoined.current !== roomParam) {
      autojoined.current = roomParam;
      openRoom(roomParam);
      nav("/mail/meet", { replace: true });
    }
  }, [roomParam, nav]);

  // /mail/meet/m/<id>: the meeting invite's destination. Opens the detail card as soon as the list arrives.
  const opened = useRef<string | null>(null);
  useEffect(() => {
    if (!meetingParam || opened.current === meetingParam) return;
    const m = (list.data?.meetings ?? []).find((x) => x.id === meetingParam);
    if (!m) return; // still loading (or canceled meeting): does nothing
    opened.current = meetingParam;
    setDetail(m);
  }, [meetingParam, list.data]);

  const meetings = list.data?.meetings ?? []; // agenda
  const upcoming = upQ.data?.meetings ?? [];
  const past = pastQ.data?.meetings ?? [];

  return (
    <div className="meet-home">
      <div className="meet-wrap">
        <header className="meet-head">
          <div className="meet-badge sm"><IcVideo size={22} /></div>
          <div className="grow"><h1>{t("meet.title")}</h1><p>{t("meet.subtitle")}</p></div>
        </header>

        <div className="meet-bar">
          <button className="meet-new" disabled={join.isPending} onClick={() => join.mutate({ access: newAccess })}><IcPlus size={17} /> {t("meet.newMeeting")}</button>
          <div className="access-toggle" title={t("meet.accessHint")}>
            <button className={newAccess === "public" ? "on" : ""} onClick={() => setNewAccess("public")}>{t("meet.accessPublic")}</button>
            <button className={newAccess === "approval" ? "on" : ""} onClick={() => setNewAccess("approval")}>{t("meet.accessApproval")}</button>
          </div>
          <button className="chipbtn" onClick={() => setSchedule(true)}><IcCalendar size={15} /> {t("meet.schedule")}</button>
          <div className="meet-join">
            <input value={code} placeholder={t("meet.codePlaceholder")} onChange={(e) => setCode(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter" && code.trim()) openRoom(code.trim()); }} />
            <button className="chipbtn" disabled={!code.trim()} onClick={() => openRoom(code.trim())}>{t("meet.join")}</button>
          </div>
        </div>

        <div className="ag-toolbar">
          <div className="ag-views">
            {(["list", "day", "week", "month"] as const).map((v) => (
              <button key={v} className={ag.view === v ? "on" : ""} onClick={() => ag.setView(v)}>
                {v === "list" ? <IcList size={15} /> : t("meet.view_" + v)}
              </button>
            ))}
          </div>
          {ag.view !== "list" && (
            <div className="ag-nav">
              <button onClick={() => ag.step(-1)}>‹</button>
              <button className="today" onClick={() => ag.setCursor(new Date())}>{t("meet.today")}</button>
              <button onClick={() => ag.step(1)}>›</button>
              <span className="ag-label">{agendaLabel(ag.view, ag.cursor, i18n.language)}</span>
            </div>
          )}
        </div>

        {ag.view === "list" ? (
          <>
            <section className="meet-sec">
              <div className="meet-sec-h"><h2>{t("meet.upcoming")}</h2><Pager page={upPage} total={upQ.data?.total ?? 0} per={PER} onPage={setUpPage} /></div>
              {upcoming.length === 0 ? <div className="meet-empty">{t("meet.noUpcoming")}</div>
                : upcoming.map((m) => <MeetingCard key={m.id} m={m} onJoin={() => openRoom(m.room)} />)}
            </section>
            {(pastQ.data?.total ?? 0) > 0 && (
              <section className="meet-sec">
                <div className="meet-sec-h"><h2>{t("meet.past")}</h2><Pager page={pastPage} total={pastQ.data?.total ?? 0} per={PER} onPage={setPastPage} /></div>
                {past.map((m) => <MeetingCard key={m.id} m={m} past onJoin={() => openRoom(m.room)} />)}
              </section>
            )}
            {(recs.data?.total ?? 0) > 0 && (
              <section className="meet-sec">
                <div className="meet-sec-h"><h2>{t("meet.recordings")}</h2><Pager page={recPage} total={recs.data?.total ?? 0} per={PER} onPage={setRecPage} /></div>
                {(recs.data?.recordings ?? []).map((rc) => <RecordingRow key={rc.id} rc={rc} onPlay={() => setPlaying(rc)} />)}
              </section>
            )}
          </>
        ) : (
          <MeetAgenda meetings={meetings} view={ag.view} cursor={ag.cursor} onOpen={setDetail} />
        )}
      </div>
      {schedule && <ScheduleModal onClose={() => setSchedule(false)} onDone={() => { setSchedule(false); qc.invalidateQueries({ queryKey: ["meetings"] }); }} />}
      {editMeeting && <ScheduleModal edit={editMeeting} onClose={() => setEditMeeting(null)} onDone={() => { setEditMeeting(null); qc.invalidateQueries({ queryKey: ["meetings"] }); }} />}
      {detail && <MeetingDetail m={detail} onClose={() => setDetail(null)} onJoin={() => { openRoom(detail.room); setDetail(null); }} />}
      {playing && <PlayModal rc={playing} onClose={() => setPlaying(null)} />}
    </div>
  );

  // Compact section pager (‹ x-y of N ›) — same design as the mail pager.
  function Pager({ page, total, per, onPage }: { page: number; total: number; per: number; onPage: (p: number) => void }) {
    if (total <= per) return null;
    const last = Math.ceil(total / per);
    const a = (page - 1) * per + 1, b = Math.min(page * per, total);
    return (
      <div className="pager">
        <span className="rng">{a}–{b} {t("mail.of")} {total}</span>
        <button className="ib" title={t("common.prevPage")} disabled={page <= 1} onClick={() => onPage(page - 1)}><IcChevron size={14} className="left" /></button>
        <button className="ib" title={t("common.nextPage")} disabled={page >= last} onClick={() => onPage(page + 1)}><IcChevron size={14} className="right" /></button>
      </div>
    );
  }

  function RecordingRow({ rc, onPlay }: { rc: Recording; onPlay: () => void }) {
    const dur = rc.duration_seconds ? fmtDuration(rc.duration_seconds) : "";
    const del = useMutation({
      mutationFn: () => backend.meetRecordingDelete(rc.id),
      onSuccess: () => { qc.invalidateQueries({ queryKey: ["recordings"] }); toast(t("meet.recDeleted")); },
      onError: (e) => toast((e as Error).message),
    });
    return (
      <div className="meet-card">
        <div className="mc-main">
          <div className="mc-title"><span className="rec-dot" /> {rc.room_name}{rc.status !== "ready" && <span className="mc-live">{t("meet.rec_" + rc.status)}</span>}</div>
          <div className="mc-meta">
            <span><IcCalendar size={13} /> {new Date(rc.started_at).toLocaleString(i18n.language, { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" })}</span>
            {dur && <span className="mc-dur">{dur}</span>}
          </div>
        </div>
        <div className="mc-actions">
          {rc.status === "ready" && <button className="mc-join" onClick={onPlay}><IcVideo size={15} /> {t("meet.play")}</button>}
          {rc.status === "ready" && <a className="mc-cancel" href={backend.recordingPlayUrl(rc.id)} download><IcDownload size={13} /> {t("files.download")}</a>}
          <button className="mc-cancel" disabled={del.isPending} onClick={() => del.mutate()}><IcTrash size={13} /> {t("meet.recDelete")}</button>
        </div>
      </div>
    );
  }

  function fmtWhen(m: Meeting): string {
    const iso = m.scheduled_start ?? m.actual_start;
    if (!iso) return t("meet.instant");
    return new Date(iso).toLocaleString(i18n.language, { weekday: "short", day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });
  }
  function fmtDuration(sec: number): string {
    const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60);
    return h > 0 ? `${h}h ${m}min` : `${m}min`;
  }

  function MeetingCard({ m, past, onJoin }: { m: Meeting; past?: boolean; onJoin: () => void }) {
    const confirmed = m.participants.filter((p) => p.rsvp === "accepted").length;
    const cancel = useMutation({ mutationFn: () => backend.meetCancel(m.id), onSuccess: () => { qc.invalidateQueries({ queryKey: ["meetings"] }); toast(t("meet.canceled")); } });
    return (
      <div className={"meet-card" + (past ? " past" : "")}>
        <div className="mc-main">
          <div className="mc-title">{m.title}{m.status === "live" && <span className="mc-live">● {t("meet.live")}</span>}</div>
          <div className="mc-meta">
            <span><IcCalendar size={13} /> {fmtWhen(m)}</span>
            {past && m.duration_seconds != null && <span className="mc-dur">{fmtDuration(m.duration_seconds)}</span>}
            <span><IcUsers size={13} /> {t("meet.confirmedOf", { confirmed, total: m.participants.length })}</span>
          </div>
          <div className="mc-people">
            {m.participants.slice(0, 6).map((p) => (
              <span key={p.email} className={"mc-av rsvp-" + p.rsvp} title={`${p.email} · ${t("meet.rsvp_" + p.rsvp)}${m.is_owner && !p.notified ? " · " + t("meet.notNotified") : ""}`}>
                {p.email.slice(0, 2).toUpperCase()}
                {m.is_owner && p.notified && <i className="mc-notif" title={t("meet.notified")} />}
              </span>
            ))}
          </div>
        </div>
        <div className="mc-actions">
          {!past && <button className="mc-join" onClick={onJoin}><IcVideo size={15} /> {t("meet.enter")}</button>}
          {!past && !m.is_owner && <RsvpButtons m={m} />}
          {!past && m.is_owner && m.scheduled_start && <button className="mc-cancel" onClick={() => setEditMeeting(m)}>{t("meet.edit")}</button>}
          {!past && m.is_owner && <button className="mc-cancel" onClick={() => cancel.mutate()}>{t("meet.cancel")}</button>}
        </div>
      </div>
    );
  }

  function RsvpButtons({ m }: { m: Meeting }) {
    const rsvp = useMutation({ mutationFn: (s: RsvpStatus) => backend.meetRSVP(m.id, s), onSuccess: () => qc.invalidateQueries({ queryKey: ["meetings"] }), onError: (e) => toast((e as Error).message) });
    const opt = (s: RsvpStatus, label: string, icon: JSX.Element) => (
      <button className={"rsvp-btn " + s + (m.my_rsvp === s ? " on" : "")} onClick={() => rsvp.mutate(s)} title={label}>{icon}<span>{label}</span></button>
    );
    return (
      <div className="rsvp-group">
        {opt("accepted", t("meet.accept"), <IcCheck size={14} />)}
        {opt("tentative", t("meet.maybe"), <span className="q">?</span>)}
        {opt("declined", t("meet.decline"), <IcX size={14} />)}
      </div>
    );
  }

  // Meeting detail (opened by clicking an agenda block): participants, who confirmed/
  // was notified, join, RSVP (guest), and cancel (owner).
  function MeetingDetail({ m, onClose, onJoin }: { m: Meeting; onClose: () => void; onJoin: () => void }) {
    const cancel = useMutation({ mutationFn: () => backend.meetCancel(m.id), onSuccess: () => { qc.invalidateQueries({ queryKey: ["meetings"] }); toast(t("meet.canceled")); onClose(); } });
    return (
      <div className="tk-bg open" onMouseDown={onClose}>
        <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 440 }}>
          <header><span>{m.title}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
          <div className="body">
            <div className="md-when"><IcCalendar size={15} /> {fmtWhen(m)}</div>
            <p className="dv-sec" style={{ margin: "12px 0 8px" }}>{t("meet.guests")}</p>
            {m.participants.map((p) => (
              <div className="md-part" key={p.email}>
                <span className={"mc-av rsvp-" + p.rsvp}>{p.email.slice(0, 2).toUpperCase()}</span>
                <span className="grow">{p.email}{p.role === "host" ? " · " + t("meet.host") : ""}</span>
                <span className={"md-rsvp " + p.rsvp}>{t("meet.rsvp_" + p.rsvp)}</span>
                {m.is_owner && <span className={"md-notif" + (p.notified ? " on" : "")}>{p.notified ? t("meet.notified") : t("meet.notNotified")}</span>}
              </div>
            ))}
          </div>
          <footer>
            {m.is_owner && <button className="mc-cancel" onClick={() => cancel.mutate()}>{t("meet.cancel")}</button>}
            {!m.is_owner && <RsvpButtons m={m} />}
            <button className="add" onClick={onJoin}>{t("meet.enter")}</button>
          </footer>
        </div>
      </div>
    );
  }
}




function PlayModal({ rc, onClose }: { rc: Recording; onClose: () => void }) {
  const { t } = useTranslation();
  return (
    <div className="viewer-full">
      <header className="editor-bar">
        <button className="chipbtn" onClick={onClose}><IcX size={15} /> {t("editor.close")}</button>
        <span className="editor-title">{rc.room_name}</span>
        <a className="chipbtn" href={backend.recordingPlayUrl(rc.id)} download><IcDownload size={15} /> {t("files.download")}</a>
      </header>
      <div className="viewer-stage">
        <video className="viewer-media" src={backend.recordingPlayUrl(rc.id)} controls autoPlay />
      </div>
    </div>
  );
}

function ScheduleModal({ onClose, onDone, edit }: { onClose: () => void; onDone: () => void; edit?: Meeting }) {
  const { t } = useTranslation();
  const pad = (n: number) => String(n).padStart(2, "0");
  const initD = edit?.scheduled_start ? new Date(edit.scheduled_start) : null;
  const initE = edit?.scheduled_end ? new Date(edit.scheduled_end) : null;
  const [title, setTitle] = useState(edit?.title ?? "");
  const [date, setDate] = useState(initD ? `${initD.getFullYear()}-${pad(initD.getMonth() + 1)}-${pad(initD.getDate())}` : "");
  const [start, setStart] = useState(initD ? `${pad(initD.getHours())}:${pad(initD.getMinutes())}` : "09:00");
  const [end, setEnd] = useState(initE ? `${pad(initE.getHours())}:${pad(initE.getMinutes())}` : "10:00");
  // Guests use the shared pattern (colleagues + contacts + external email).
  const [guests, setGuests] = useState<Invitee[]>([]);
  const invitees = guests.map((g) => g.email);
  const [access, setAccess] = useState<"public" | "approval">("approval");
  const create = useMutation({
    mutationFn: () => {
      const s = date ? new Date(`${date}T${start}`).toISOString() : "";
      const e = date ? new Date(`${date}T${end}`).toISOString() : "";
      if (edit) return backend.meetUpdate(edit.id, { title: title.trim(), scheduled_start: s, scheduled_end: e });
      return backend.meetCreate({ title: title.trim(), scheduled_start: s, scheduled_end: e, participants: invitees, add_to_calendar: true, access });
    },
    onSuccess: () => { toast(t(edit ? "meet.updated" : "meet.scheduled")); onDone(); },
    onError: (e) => toast((e as Error).message),
  });

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 480 }}>
        <header><span>{edit ? t("meet.editMeeting") : t("meet.schedule")}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body meet-form">
          <label>{t("meet.fTitle")}<input autoFocus value={title} onChange={(e) => setTitle(e.target.value)} placeholder={t("meet.fTitlePh")} /></label>
          <div className="row3">
            <label>{t("meet.fDate")}<input type="date" value={date} onChange={(e) => setDate(e.target.value)} /></label>
            <label>{t("meet.fStart")}<input type="time" value={start} onChange={(e) => setStart(e.target.value)} /></label>
            <label>{t("meet.fEnd")}<input type="time" value={end} onChange={(e) => setEnd(e.target.value)} /></label>
          </div>
          {!edit && <label>{t("meet.fAccess")}
            <div className="access-toggle wide">
              <button type="button" className={access === "public" ? "on" : ""} onClick={() => setAccess("public")}>{t("meet.accessPublic")}</button>
              <button type="button" className={access === "approval" ? "on" : ""} onClick={() => setAccess("approval")}>{t("meet.accessApproval")}</button>
            </div>
          </label>}
          {!edit && <label>{t("meet.fInvitees")}</label>}
          {!edit && <InviteePicker value={guests} onChange={setGuests} />}
        </div>
        <footer>
          <button className="chipbtn" onClick={onClose}>{t("common.cancel")}</button>
          <button className="add" disabled={!title.trim() || !date || create.isPending} onClick={() => create.mutate()}>{t(edit ? "contacts.save" : "meet.scheduleBtn")}</button>
        </footer>
      </div>
    </div>
  );
}
