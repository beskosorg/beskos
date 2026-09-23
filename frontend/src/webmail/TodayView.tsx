import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import type { WebmailSession } from "./auth";
import { backend } from "./backend";
import { AnnouncementsBoard } from "./AnnouncementsBoard";
import { displayName, initials, hueFor } from "./util";
import { IcCalendar, IcColumns, IcMail } from "../panel/icons";

type View = "today" | "mail" | "kanban" | "cal";

// Time of day: shapes the briefing (morning looks ahead, afternoon picks back up, night wraps up + previews tomorrow).
function dayPeriod(h: number): "morning" | "afternoon" | "night" {
  return h < 12 ? "morning" : h < 18 ? "afternoon" : "night";
}

// "Today" screen: AI-generated daily briefing (cached per day+period) + the organization's announcements board.
export function TodayView({ session, onNavigate }: { session: WebmailSession; onNavigate: (v: View) => void }) {
  const { t, i18n } = useTranslation();
  const svc = session.service;

  const today = new Date();
  const dayStart = new Date(today); dayStart.setHours(0, 0, 0, 0);
  const dayEnd = new Date(today); dayEnd.setHours(23, 59, 59, 0);
  const period = dayPeriod(today.getHours());
  const dateStr = today.toLocaleDateString(i18n.language, { weekday: "long", day: "numeric", month: "long" });
  // Language in the key: if the user switches the UI language, the summary is regenerated in that
  // language instead of serving the old cache (the AI always answers in the page's language).
  const cacheKey = `mailin.briefing.${today.toISOString().slice(0, 10)}.${period}.${i18n.language}`;

  const boxesQ = useQuery({ queryKey: ["mailboxes"], queryFn: () => svc.mailboxes() });
  const inboxId = boxesQ.data?.find((b) => b.role === "inbox")?.id;
  const emailsQ = useQuery({ queryKey: ["today-emails", inboxId], queryFn: () => svc.listEmails(inboxId!, 0, 5), enabled: !!inboxId });
  const eventsQ = useQuery({ queryKey: ["today-events"], queryFn: () => backend.events(dayStart.toISOString(), dayEnd.toISOString()) });
  const boardQ = useQuery({
    queryKey: ["today-board"],
    queryFn: async () => { const bs = await backend.boards(); return backend.board(bs[0].id); },
  });
  // At night, also tomorrow's agenda (so the briefing can preview the next day).
  const tomStart = new Date(dayStart); tomStart.setDate(tomStart.getDate() + 1);
  const tomEnd = new Date(dayEnd); tomEnd.setDate(tomEnd.getDate() + 1);
  const tomorrowQ = useQuery({ queryKey: ["tomorrow-events"], queryFn: () => backend.events(tomStart.toISOString(), tomEnd.toISOString()), enabled: period === "night" });

  const events = (eventsQ.data ?? []).map((e) => `${fmtTime(e.starts_at, i18n.language)} ${e.title}`);
  const tasks = (boardQ.data?.columns[0]?.cards ?? []).map((c) => c.title + (c.due ? ` (${c.due})` : ""));
  const emails = (emailsQ.data?.items ?? []).map((m) => `${displayName(m.from)} — ${m.subject}`);
  const tomorrow = (tomorrowQ.data ?? []).map((e) => `${fmtTime(e.starts_at, i18n.language)} ${e.title}`);
  const ready = boxesQ.isSuccess && eventsQ.isSuccess && boardQ.isSuccess && (period !== "night" || tomorrowQ.isSuccess);

  // Briefing cached per day+period in localStorage: generates once per morning/afternoon/night and reuses it.
  const [brief, setBrief] = useState<string>(() => { try { return localStorage.getItem(cacheKey) ?? ""; } catch { return ""; } });
  const [genLoading, setGenLoading] = useState(false);
  const generate = async (force = false) => {
    if (!ready || (!force && brief) || genLoading) return;
    setGenLoading(true);
    try {
      const r = await backend.briefing({ date: dateStr, events, tasks, emails, period, tomorrow: period === "night" ? tomorrow : undefined });
      setBrief(r.briefing);
      try { localStorage.setItem(cacheKey, r.briefing); } catch { /* quota */ }
    } catch { /* AI off */ }
    finally { setGenLoading(false); }
  };
  // Generates automatically only if there is no cache yet for this day+period.
  useEffect(() => { if (ready && !brief) void generate(); /* eslint-disable-next-line */ }, [ready]);

  return (
    <div className="tday"><div className="tday-in">
      <div className="tday-head">
        <div>
          <h1>{t("webmail.today.greeting", { name: (session.email.split("@")[0]) })}</h1>
          <p className="date" style={{ textTransform: "capitalize" }}>{dateStr}</p>
        </div>
        <button className="regen" onClick={() => generate(true)}>✦ {t("webmail.today.regenerate")}</button>
      </div>

      <div className="brief">
        <h4>✦ {t("webmail.today.summary")}</h4>
        <p>{genLoading ? t("webmail.ai.generating") : brief || t("webmail.today.noAI")}</p>
      </div>

      <div className="tgrid">
        <div className="tcard">
          <h3><IcCalendar size={15} /> {t("webmail.today.agenda")} <span className="n">{(eventsQ.data ?? []).length}</span></h3>
          {(eventsQ.data ?? []).length === 0 ? <div className="titem" style={{ cursor: "default" }}><span className="tx">{t("webmail.today.noEvents")}</span></div>
            : (eventsQ.data ?? []).map((e) => (
              <div className="titem" key={e.id} onClick={() => onNavigate("cal")}>
                <span className="tt3">{fmtTime(e.starts_at, i18n.language)}</span>
                <span className="tdot" style={{ background: e.color }} />
                <div><div className="th">{e.title}</div>{e.location && <div className="tx">{e.location}</div>}</div>
              </div>
            ))}
        </div>

        <div className="tcard">
          <h3><IcColumns size={15} /> {t("webmail.today.tasks")} <span className="n">{tasks.length}</span></h3>
          {tasks.length === 0 ? <div className="titem" style={{ cursor: "default" }}><span className="tx">{t("webmail.today.noTasks")}</span></div>
            : (boardQ.data?.columns[0]?.cards ?? []).map((c) => (
              <div className="titem" key={c.id} onClick={() => onNavigate("kanban")}>
                <span className="tdot" style={{ background: "var(--rose)" }} />
                <div style={{ flex: 1 }}><div className="th">{c.title}</div>{c.assignee && <div className="tx">{c.assignee}</div>}</div>
                {c.due && <span className="due today">{c.due}</span>}
              </div>
            ))}
        </div>

        <div className="tcard">
          <h3><IcMail size={15} /> {t("webmail.today.emails")} <span className="n">{emails.length}</span></h3>
          {(emailsQ.data?.items ?? []).map((m) => {
            const name = displayName(m.from);
            return (
              <div className="titem" key={m.id} onClick={() => onNavigate("mail")}>
                <div className="av" style={{ width: 34, height: 34, background: hueFor(m.from?.[0]?.email ?? name) }}>{initials(name)}</div>
                <div style={{ flex: 1 }}><div className="th">{name}</div><div className="tx">{m.subject}</div></div>
              </div>
            );
          })}
        </div>
      </div>

      <AnnouncementsBoard />
    </div></div>
  );
}

function fmtTime(iso: string, locale: string): string {
  return new Date(iso).toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit" });
}
