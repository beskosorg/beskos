import { useEffect, useState } from "react";
import { LanguageSelect } from "./LanguageSelect";
import { normalizeLanguage } from "../i18n/supported";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { settingsApi, devicesApi, backend, type EmailSettings, type DeviceToken } from "./backend";
import { useSection } from "./useSettings";
import { Spinner } from "./Spinner";
import { Switch } from "./Switch";
import { SignatureEditor } from "./SignatureEditor";
import { toast } from "./toast";
import { IcMail, IcGear, IcBell, IcKey, IcChat, IcCalendar, IcFolder, IcVideo, IcColumns, IcSparkle } from "../panel/icons";
import { MemorySection } from "./assistant/MemorySection";
import { syncApi, syncSupported, type SyncStatus } from "./syncClient";
import { NotificationsSection } from "./SettingsNotifications";

type Section = "general" | "email" | "chat" | "calendar" | "files" | "meet" | "kanban" | "notifications" | "devices" | "assistant";
const SECTIONS: Section[] = ["general", "email", "chat", "calendar", "files", "meet", "kanban", "notifications", "devices", "assistant"];

// User settings — personal preferences, separate from Admin (which is organization
// management). One section per app; for now General and Email. The section comes from the URL.
export function SettingsView({ section, onSection }: { section?: string; onSection: (s: string) => void }) {
  const { t } = useTranslation();
  // On mobile the navigation is iOS style: no section in the URL = section list; with a section = detail
  // + back. `current` null means "in the list" (root). On desktop the side rail always shows
  // everything, and the section shown by default is General.
  // Sync stopped being its own section (it lives inside Files); the old URL keeps
  // working instead of leading to a dead end for whoever had it bookmarked.
  const wanted = section === "sync" ? "files" : section;
  const current: Section | null = SECTIONS.includes(wanted as Section) ? (wanted as Section) : null;
  const shown: Section = current ?? "general";

  const items: { id: Section; label: string; icon: JSX.Element }[] = [
    { id: "general", label: t("settings.general"), icon: <IcGear size={17} /> },
    { id: "email", label: t("settings.email"), icon: <IcMail size={17} /> },
    { id: "chat", label: t("settings.chat"), icon: <IcChat size={17} /> },
    { id: "calendar", label: t("settings.calendar"), icon: <IcCalendar size={17} /> },
    { id: "files", label: t("settings.files"), icon: <IcFolder size={17} /> },
    { id: "meet", label: t("settings.meet"), icon: <IcVideo size={17} /> },
    { id: "kanban", label: t("settings.kanban"), icon: <IcColumns size={17} /> },
    { id: "notifications", label: t("settings.notifications"), icon: <IcBell size={17} /> },
    { id: "devices", label: t("settings.devices"), icon: <IcKey size={17} /> },
    { id: "assistant", label: t("memory.title"), icon: <IcSparkle size={17} /> },
  ];

  return (
    <div className={"adm" + (current ? " has-section" : "")}>
      <aside className="adm-rail">
        <div className="adm-title">{t("settings.title")}</div>
        {items.map((i) => (
          <button key={i.id} className={shown === i.id ? "on" : ""} onClick={() => onSection(i.id)}>
            {i.icon} <span>{i.label}</span>
          </button>
        ))}
      </aside>
      <section className="adm-main">
        {/* Back to the section list (mobile only; hidden by CSS on desktop). */}
        <button className="adm-back" onClick={() => onSection("")}>‹ {t("settings.title")}</button>
        {shown === "general" && <GeneralSection />}
        {shown === "email" && <EmailSection />}
        {shown === "chat" && <ChatSection />}
        {shown === "calendar" && <CalendarSection />}
        {shown === "files" && <FilesSection />}
        {shown === "meet" && <MeetSection />}
        {shown === "kanban" && <KanbanSection />}
        {shown === "notifications" && <NotificationsSection />}
        {shown === "devices" && <DevicesSection />}
        {shown === "assistant" && <MemorySection />}
      </section>
    </div>
  );
}

// General: language, theme and default view. Follow the user across devices (settings.general).
function GeneralSection() {
  const { t, i18n } = useTranslation();
  const { value, save } = useSection("general");
  const [lang, setLang] = useState(normalizeLanguage(i18n.resolvedLanguage ?? i18n.language));
  const changeLang = (l: string) => {
    setLang(l);
    void i18n.changeLanguage(l);
    try { localStorage.setItem("mailin.lang", l); } catch { /* private mode */ }
    save({ language: l });
  };
  const changeTheme = (th: "light" | "dark") => {
    try { localStorage.setItem("mailin.theme", th); } catch { /* private mode */ }
    document.documentElement.setAttribute("data-theme", th); // applies immediately; App reconciles it via the cache
    save({ theme: th });
  };
  const theme = value.theme ?? (document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light");
  // view id <-> nav translation key (kanban->board, cal->calendar).
  const VIEWS: { id: string; key: string }[] = [
    { id: "today", key: "today" }, { id: "mail", key: "mail" }, { id: "chat", key: "chat" },
    { id: "kanban", key: "board" }, { id: "cal", key: "calendar" }, { id: "files", key: "files" },
    { id: "contacts", key: "contacts" }, { id: "meet", key: "meet" },
  ];

  return (
    <div className="set-page">
      <h2>{t("settings.general")}</h2>
      <div className="set-card">
        <label className="set-field">
          <span>{t("settings.language")}</span>
          <LanguageSelect value={lang} onChange={changeLang} />
        </label>
        <label className="set-field">
          <span>{t("settings.theme")}</span>
          <select value={theme} onChange={(e) => changeTheme(e.target.value as "light" | "dark")}>
            <option value="light">{t("settings.themeLight")}</option>
            <option value="dark">{t("settings.themeDark")}</option>
          </select>
        </label>
        <label className="set-field">
          <span>{t("settings.startView")}</span>
          <select value={value.startView ?? "today"} onChange={(e) => save({ startView: e.target.value })}>
            {VIEWS.map((v) => <option key={v.id} value={v.id}>{t(`webmail.apps.${v.key}`)}</option>)}
          </select>
        </label>
      </div>
    </div>
  );
}

// Chat: Enter sends. The message sound lives in Notifications, with the rest — that's where you
// look when you want silence, and having half a dozen sound toggles scattered across
// sections would force you to hunt them down one by one.
function ChatSection() {
  const { t } = useTranslation();
  const { value, save } = useSection("chat");
  return (
    <div className="set-page">
      <h2>{t("settings.chat")}</h2>
      <div className="set-card">
        <div className="ios-list">
          <div className="ios-row">
            <div className="ios-row-text">
              <span className="ios-row-label">{t("settings.chatEnterSends")}</span>
              <span className="ios-row-sub">{t("settings.chatEnterSendsHint")}</span>
            </div>
            <Switch checked={value.enterSends !== false} onChange={(on) => save({ enterSends: on })} label={t("settings.chatEnterSends")} />
          </div>
        </div>
      </div>
    </div>
  );
}

// Calendar: week start, working days, default view, default duration and reminder.
function CalendarSection() {
  const { t, i18n } = useTranslation();
  const { value, save } = useSection("calendar");
  // Working days (0=Sunday...6=Saturday): outside them the calendar is disabled for creating events.
  const work = value.workDays ?? [0, 1, 2, 3, 4, 5, 6];
  const first = value.weekStart === "sunday" ? 0 : 1;
  const dayName = (d: number) => new Date(2026, 2, 1 + d).toLocaleDateString(i18n.language, { weekday: "short" }); // March 1, 2026 = Sunday
  const toggleDay = (d: number) => {
    const next = work.includes(d) ? work.filter((x) => x !== d) : [...work, d].sort();
    if (next.length === 0) return; // at least one working day
    save({ workDays: next });
  };
  return (
    <div className="set-page">
      <h2>{t("settings.calendar")}</h2>
      <div className="set-card">
        <div className="set-field">
          <span>{t("settings.calWorkDays")}</span>
          <div className="workday-chips">
            {Array.from({ length: 7 }, (_, i) => (first + i) % 7).map((d) => (
              <button key={d} type="button" className={"wd-chip" + (work.includes(d) ? " on" : "")}
                onClick={() => toggleDay(d)}>{dayName(d)}</button>
            ))}
          </div>
        </div>
        <p className="desc" style={{ margin: "-4px 0 10px" }}>{t("settings.calWorkDaysHint")}</p>
        <label className="set-field">
          <span>{t("settings.calWeekStart")}</span>
          <select value={value.weekStart ?? "monday"} onChange={(e) => save({ weekStart: e.target.value as "monday" | "sunday" })}>
            <option value="monday">{t("settings.calMonday")}</option>
            <option value="sunday">{t("settings.calSunday")}</option>
          </select>
        </label>
        <label className="set-field">
          <span>{t("settings.calDefaultView")}</span>
          <select value={value.defaultView ?? "month"} onChange={(e) => save({ defaultView: e.target.value as "month" | "week" | "day" | "list" })}>
            <option value="month">{t("settings.calViewMonth")}</option>
            <option value="week">{t("settings.calViewWeek")}</option>
            <option value="day">{t("settings.calViewDay")}</option>
            <option value="list">{t("settings.calViewList")}</option>
          </select>
        </label>
        <label className="set-field">
          <span>{t("settings.calDuration")}</span>
          <select value={value.defaultDuration ?? 60} onChange={(e) => save({ defaultDuration: Number(e.target.value) })}>
            <option value={30}>30 min</option>
            <option value={60}>1 h</option>
            <option value={90}>1 h 30</option>
            <option value={120}>2 h</option>
          </select>
        </label>
        <label className="set-field">
          <span>{t("settings.calReminder")}</span>
          <select value={value.defaultReminder ?? 10} onChange={(e) => save({ defaultReminder: Number(e.target.value) })}>
            <option value={0}>{t("settings.calNoReminder")}</option>
            <option value={5}>5 min</option>
            <option value={10}>10 min</option>
            <option value={30}>30 min</option>
            <option value={60}>1 h</option>
          </select>
        </label>
      </div>
    </div>
  );
}

// Files: default view + default expiry for the shared link.
function FilesSection() {
  const { t } = useTranslation();
  const { value, save } = useSection("files");
  return (
    <div className="set-page">
      <h2>{t("settings.files")}</h2>
      <div className="set-card">
        <label className="set-field">
          <span>{t("settings.filesView")}</span>
          <select value={value.view ?? "list"} onChange={(e) => save({ view: e.target.value as "grid" | "list" })}>
            <option value="list">{t("settings.filesViewList")}</option>
            <option value="grid">{t("settings.filesViewGrid")}</option>
          </select>
        </label>
        <label className="set-field">
          <span>{t("settings.filesLinkExpiry")}</span>
          <select value={value.linkExpiryDays ?? 0} onChange={(e) => save({ linkExpiryDays: Number(e.target.value) })}>
            <option value={0}>{t("settings.filesNeverExpires")}</option>
            <option value={7}>7 {t("settings.days")}</option>
            <option value={30}>30 {t("settings.days")}</option>
            <option value={90}>90 {t("settings.days")}</option>
          </select>
        </label>
      </div>

      {/* Folder sync: it's a setting OF Drive, not a separate app — that's why it lives
          here instead of in its own section. Outside the desktop app, the block itself explains that
          it only exists there. */}
      <SyncSection />
    </div>
  );
}

// Meet: join with mic/camera off by default.
function MeetSection() {
  const { t } = useTranslation();
  const { value, save } = useSection("meet");
  return (
    <div className="set-page">
      <h2>{t("settings.meet")}</h2>
      <div className="set-card">
        <div className="ios-list">
          <div className="ios-row">
            <span className="ios-row-label">{t("settings.meetEnterMuted")}</span>
            <Switch checked={value.enterMuted === true} onChange={(on) => save({ enterMuted: on })} label={t("settings.meetEnterMuted")} />
          </div>
          <div className="ios-row">
            <span className="ios-row-label">{t("settings.meetCamOff")}</span>
            <Switch checked={value.camOff === true} onChange={(on) => save({ camOff: on })} label={t("settings.meetCamOff")} />
          </div>
        </div>
      </div>
    </div>
  );
}

// Kanban: default board (or the first one).
function KanbanSection() {
  const { t } = useTranslation();
  const { value, save } = useSection("kanban");
  const boardsQ = useQuery({ queryKey: ["boards"], queryFn: () => backend.boards(), retry: false });
  const boards = boardsQ.data ?? [];
  return (
    <div className="set-page">
      <h2>{t("settings.kanban")}</h2>
      <div className="set-card">
        <label className="set-field">
          <span>{t("settings.kanbanBoard")}</span>
          <select value={value.defaultBoard ?? ""} onChange={(e) => save({ defaultBoard: e.target.value })}>
            <option value="">{t("settings.kanbanFirstBoard")}</option>
            {boards.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}
          </select>
        </label>
      </div>
    </div>
  );
}

// File sync: the engine runs INSIDE this app (Tauri). The model is by PAIRS —
// a local folder is picked, it gets created in Drive, and only what's there syncs. On the web there's
// no local sync, so a note is shown instead.
function SyncSection() {
  const { t } = useTranslation();
  const supported = syncSupported();
  const [status, setStatus] = useState<SyncStatus | null>(null);
  const [bandwidth, setBandwidth] = useState(0);
  const [busy, setBusy] = useState(false);
  // Current account (email): isolates sync per user. Without this, switching accounts on the same machine
  // would show the previous account's folders. The daemon compares and hides another account's config.
  const [account, setAccount] = useState("");
  useEffect(() => { if (supported) backend.me().then((m) => setAccount(m.email || "")).catch(() => {}); }, [supported]);

  // Probes the state while the page is open (the engine runs in the background and keeps changing).
  useEffect(() => {
    if (!supported) return;
    let alive = true;
    const tick = () => { syncApi.status(account).then((s) => { if (alive) setStatus(s); }).catch(() => {}); };
    tick();
    const id = setInterval(tick, 1500);
    return () => { alive = false; clearInterval(id); };
  }, [supported, account]);

  useEffect(() => { if (supported) syncApi.bandwidth().then(setBandwidth).catch(() => {}); }, [supported]);

  if (!supported) {
    return (
      <>
        <h3 className="set-sub">{t("settings.sync")}</h3>
        <div className="set-card"><p className="desc">{t("settings.syncOnlyDesktop")}</p></div>
      </>
    );
  }

  const addFolder = async () => {
    try {
      setBusy(true);
      const pair = await syncApi.addFolder(account);
      if (pair) toast(t("settings.syncFolderAdded", { name: pair.name }));
    } catch (e) { toast((e as Error).message); } finally { setBusy(false); }
  };

  const removeFolder = async (localPath: string) => {
    try { await syncApi.removeFolder(localPath); toast(t("settings.syncFolderRemoved")); }
    catch (e) { toast((e as Error).message); }
  };

  const pairs = status?.pairs ?? [];
  const phaseLabel = !status ? "…"
    : pairs.length === 0 ? t("settings.syncNoFolders")
    : status.paused ? t("settings.syncPaused")
    : status.phase === "syncing" ? t("settings.syncSyncing")
    : status.phase === "error" ? `${t("settings.syncError")}: ${status.message}`
    : status.message || t("settings.syncUpToDate");

  return (
    <>
      <h3 className="set-sub">{t("settings.sync")}</h3>

      <div className="set-card">
        <p className="desc">{t("settings.syncIntro")}</p>
        <div className="ios-list">
          <div className="ios-row">
            <div className="ios-row-text">
              <span className="ios-row-label">{t("settings.syncState")}</span>
              <span className="ios-row-sub">{phaseLabel}</span>
            </div>
            <span className={"sync-dot " + (status?.phase === "error" ? "err" : status?.phase === "syncing" ? "on" : "")} />
          </div>
        </div>
        <div className="drawer-actions">
          <button className="send" disabled={busy} onClick={() => void addFolder()}>
            {busy ? <Spinner size={13} /> : t("settings.syncAddFolder")}
          </button>
          {pairs.length > 0 && (
            <>
              <button className="chipbtn" onClick={() => void syncApi.now()}>{t("settings.syncNow")}</button>
              <button className="chipbtn" onClick={() => void syncApi.setPaused(!status?.paused)}>
                {status?.paused ? t("settings.syncResume") : t("settings.syncPause")}
              </button>
            </>
          )}
        </div>
      </div>

      {pairs.length > 0 && (
        <div className="set-card">
          <div className="ios-group-label">{t("settings.syncFolders")}</div>
          <div className="ios-list">
            {pairs.map((p) => (
              <div key={p.local_path} className="ios-row">
                <div className="ios-row-text">
                  <span className="ios-row-label">{p.name}</span>
                  <span className="ios-row-sub">{p.local_path}</span>
                </div>
                <button className="chipbtn danger" onClick={() => void removeFolder(p.local_path)}>
                  {t("settings.syncStop")}
                </button>
              </div>
            ))}
          </div>
          <p className="desc">{t("settings.syncStopHint")}</p>
        </div>
      )}

      <div className="set-card">
        <label className="set-field">
          <span>{t("settings.syncBandwidth")}</span>
          <select value={bandwidth} onChange={(e) => { const v = Number(e.target.value); setBandwidth(v); void syncApi.setBandwidth(v); }}>
            <option value={0}>{t("settings.syncNoLimit")}</option>
            <option value={256}>256 KB/s</option>
            <option value={1024}>1 MB/s</option>
            <option value={5120}>5 MB/s</option>
          </select>
        </label>
      </div>
    </>
  );
}

// Devices: tokens for sync clients (includes this app). The raw token only appears
// once, on creation — we show it in a copyable block with a warning that it won't be shown again.
function DevicesSection() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["devices"], queryFn: () => devicesApi.list(), retry: false });
  const revoke = useMutation({
    mutationFn: (id: string) => devicesApi.revoke(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["devices"] }); toast(t("settings.deviceRevoked")); },
    onError: (e) => toast((e as Error).message),
  });

  const devices: DeviceToken[] = q.data?.devices ?? [];
  const fmt = (s: string | null) => (s ? new Date(s).toLocaleString() : "—");

  return (
    <div className="set-page">
      <h2>{t("settings.devices")}</h2>
      <div className="set-card">
        {/* Creating a token by hand was removed from here: the desktop app creates it on its own when
            turning on sync (syncClient.connect), and there was nowhere to paste a hand-made token — the
            sync engine is a library of the app, not a separate daemon. The endpoint still
            exists, for automation and for whenever it makes sense in the UI again. */}
        <p className="desc">{t("settings.devicesHint")}</p>

        {q.isLoading ? <div className="loading">…</div> : devices.length === 0 ? (
          <p className="desc">{t("settings.deviceNone")}</p>
        ) : (
          <div className="ios-list">
            {devices.map((d) => (
              <div key={d.id} className="ios-row">
                <div className="ios-row-text">
                  <span className="ios-row-label">{d.name} {d.revoked_at && <em>· {t("settings.deviceRevokedTag")}</em>}</span>
                  <span className="ios-row-sub">{d.token_prefix}… · {t("settings.deviceLastUsed")}: {fmt(d.last_used_at)}</span>
                </div>
                {!d.revoked_at && (
                  <button className="chipbtn danger" disabled={revoke.isPending} onClick={() => revoke.mutate(d.id)}>{t("settings.deviceRevoke")}</button>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// Email: the signature. Stored in settings.email; attached to every send by the composer.
function EmailSection() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["settings"], queryFn: () => settingsApi.get(), retry: false });
  const [f, setF] = useState<EmailSettings>({ signature: "", signatureOn: true, aiSummary: true });

  useEffect(() => { if (q.data?.email) setF({ signature: q.data.email.signature ?? "", signatureOn: q.data.email.signatureOn !== false, aiSummary: q.data.email.aiSummary !== false }); }, [q.data]);

  const save = useMutation({
    mutationFn: () => settingsApi.patch({ email: f }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["settings"] }); toast(t("settings.saved")); },
    onError: (e) => toast((e as Error).message),
  });

  if (q.isLoading) return <div className="loading">…</div>;
  return (
    <div className="set-page">
      <h2>{t("settings.email")}</h2>
      <div className="set-card">
        <div className="set-field col">
          <span>{t("settings.signature")}</span>
          <SignatureEditor value={f.signature ?? ""} onChange={(html) => setF((p) => ({ ...p, signature: html }))} />
          <p className="desc">{t("settings.signatureHint")}</p>
        </div>
        <div className="ios-list">
          <div className="ios-row">
            <span className="ios-row-label">{t("settings.signatureOn")}</span>
            <Switch checked={f.signatureOn !== false} onChange={(on) => setF((p) => ({ ...p, signatureOn: on }))} label={t("settings.signatureOn")} />
          </div>
          <div className="ios-row">
            <div className="ios-row-text">
              <span className="ios-row-label">{t("settings.aiSummary")}</span>
              <span className="ios-row-sub">{t("settings.aiSummaryHint")}</span>
            </div>
            <Switch checked={f.aiSummary !== false} onChange={(on) => setF((p) => ({ ...p, aiSummary: on }))} label={t("settings.aiSummary")} />
          </div>
        </div>
        <div className="drawer-actions">
          <button className="send" disabled={save.isPending} onClick={() => save.mutate()}>
            {save.isPending ? <Spinner size={13} /> : t("contacts.save")}
          </button>
        </div>
      </div>

      <MailConnectionCard />
    </div>
  );
}

// Connection details for external clients and for sending via SMTP from somewhere else (the
// company's website, for example). It's public information — port, host and username; the password is the
// mailbox's own and never appears here. The username is the FULL address: that's what the server expects,
// and it's the most common mistake among those configuring it by hand.
function MailConnectionCard() {
  const { t } = useTranslation();
  const q = useQuery({ queryKey: ["mail-connection"], queryFn: () => backend.mailConnection(), retry: false, staleTime: Infinity });
  const c = q.data;
  const copy = (v: string) => { navigator.clipboard?.writeText(v); toast(t("settings.deviceCopied")); };
  const Row = ({ label, value }: { label: string; value: string }) => (
    <div className="ios-row">
      <div className="ios-row-text">
        <span className="ios-row-label">{label}</span>
        <span className="ios-row-sub mono">{value}</span>
      </div>
      <button className="chipbtn" onClick={() => copy(value)}>{t("settings.deviceCopy")}</button>
    </div>
  );
  const sec = (k: string) => t(`settings.mailSec.${k}`, { defaultValue: k });
  return (
    <>
      <h3 className="set-sub">{t("settings.mailConn")}</h3>
      <div className="set-card">
        <p className="desc">{t("settings.mailConnHint")}</p>
        {!c ? <div className="loading">…</div> : (
          <div className="ios-list">
            <Row label={t("settings.mailUser")} value={c.username} />
            <Row label={`IMAP · ${sec(c.imap.security)}`} value={`${c.imap.host}:${c.imap.port}`} />
            {c.smtp.map((sm) => (
              <Row key={sm.port} label={`SMTP · ${sec(sm.security)}`} value={`${sm.host}:${sm.port}`} />
            ))}
            <Row label="JMAP" value={c.jmap.url} />
            <Row label="CalDAV / CardDAV" value={c.caldav.url} />
          </div>
        )}
        <p className="desc">{t("settings.mailConnPassword")}</p>
      </div>
    </>
  );
}
