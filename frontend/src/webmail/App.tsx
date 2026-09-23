import { useEffect, useRef, useState } from "react";
import { MFAGate } from "./MFAGate";
import { useOverlayOpen } from "./overlay";
import { ProfileView } from "./ProfileView";
import { useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { WebmailSession } from "./auth";
import { backend, settingsApi } from "./backend";
import { useJmapPush } from "./useJmapPush";
import { useDesktopNotifier } from "./useDesktopNotifier";
import { onNotificationClick } from "./webpush";
import { onDesktopNotifClick, setNotifNavigate } from "./desktopNotify";
import { applyBrandColor, cacheBrand, BESKOS_MARK } from "./branding";
import { initials } from "./util";
import { isTauri, onTauriEvent } from "./tauriNotify";
import { initVcardOpen } from "./desktopVcard";
import { setTrayAccount } from "./nativeWindow";
import { ToastHost, toast } from "./toast";
import { AccountBanner, MailOrDomainNotice } from "./AccountBanner";
import { DocsView } from "./DocsView";
import { Onboarding } from "./Onboarding";
import { MailView } from "./MailView";
import { KanbanView } from "./KanbanView";
import { CalendarView } from "./CalendarView";
import { TodayView } from "./TodayView";
import { FilesView } from "./FilesView";
import { ContactsView } from "./ContactsView";
import { MeetView } from "./MeetView";
import { WikiView } from "./WikiView";
import { NotesView } from "./NotesView";
import { useChatSocket } from "./useChatSocket";
import { toastAction } from "./toast";
import { ChatView } from "./ChatView";
import { AdminView, useAdminEnabled, useAssistantEnabled, useDisabledApps } from "./AdminView";
import { SettingsView } from "./SettingsView";
import { NotificationsBell } from "./NotificationsBell";
import { AssistantBubble } from "./assistant/AssistantBubble";
import { MigrationPrompt } from "../panel/MigrationPrompt";
import { IcSun, IcMail, IcColumns, IcCalendar, IcFolder, IcUsers, IcSearch, IcMoon, IcLogout, IcVideo, IcChat, IcShield, IcGear, IcChevron, IcBook, IcPencilBox, IcDoc, IcDownload } from "../panel/icons";

type View = "today" | "mail" | "chat" | "kanban" | "cal" | "files" | "docs" | "contacts" | "meet" | "notes" | "wiki" | "admin" | "settings" | "profile";
// Areas with something to search. "today" and "admin" are left out: they have no list to filter.
const SEARCHABLE: View[] = ["mail", "chat", "kanban", "cal", "files", "docs", "contacts", "meet"];
const VIEWS: View[] = ["today", "mail", "chat", "kanban", "cal", "files", "docs", "contacts", "meet", "notes", "wiki", "admin", "settings", "profile"];

// Mail app shell: top bar + app rail + active view. The active view lives in the URL
// (/mail/<view>), so links are navigable and reloadable. "Today" is the default.
export function App({ session }: { session: WebmailSession }) {
  const { t, i18n } = useTranslation();
  const nav = useNavigate();
  // 2nd URL segment: room (Meet) or section (Admin). Every menu item lives in the URL —
  // links are shareable and a refresh doesn't lose the place.
  const { view: viewParam, sub, sub2 } = useParams<{ view: string; sub: string; sub2: string }>();
  const view: View = VIEWS.includes(viewParam as View) ? (viewParam as View) : "today";
  const setView = (v: View) => nav(v === "today" ? "/mail" : `/mail/${v}`);
  const [search, setSearch] = useState("");
  const [advOpen, setAdvOpen] = useState(false); // advanced mail filters panel (Gmail-style)
  // Tells the tray menu who is signed in: the tray is built in Rust and has no access to the
  // session. On logout it must be cleared to `null`, or the tray would keep showing the email of
  // whoever just signed out.
  useEffect(() => {
    if (!isTauri()) return;
    void setTrayAccount(session.email);
    return () => { void setTrayAccount(null); };
  }, [session.email]);
  const [mobSearch, setMobSearch] = useState(false); // on mobile the search opens in a box below
  const searchRef = useRef<HTMLInputElement>(null);
  useEffect(() => { if (mobSearch) setTimeout(() => searchRef.current?.focus(), 0); }, [mobSearch]);
  useEffect(() => { setMobSearch(false); }, [view]);
  // Fills the search box above AND focuses the input (e.g. chat's "in:channel" button). Moves the cursor to the end.
  const fillSearch = (v: string) => {
    setSearch(v);
    setTimeout(() => { const el = searchRef.current; if (el) { el.focus(); el.setSelectionRange(el.value.length, el.value.length); } }, 0);
  };

  // Theme as state (so the icon can reflect and toggle it) + persistence across sessions.
  const [theme, setTheme] = useState<"light" | "dark">(currentTheme);
  useEffect(() => { document.documentElement.setAttribute("data-theme", theme); }, [theme]);
  const toggleTheme = () => {
    const next = theme === "dark" ? "light" : "dark";
    setTheme(next);
    try { localStorage.setItem("mailin.theme", next); } catch { /* private mode */ }
    // Follows the user across devices (fire-and-forget; localStorage already covers the local case).
    //
    // The whole section is sent, not just the theme. The merge on the server is `settings || patch`,
    // which is SHALLOW: a patch of {general:{theme}} replaces the entire general object and takes
    // `onboarded`, `language` and `startView` with it. The visible symptom was an owner who changed
    // the theme and was shown the first-run wizard again on the next load. useSection() already
    // merges this way; this call was the one that did not.
    settingsApi.patch({ general: { ...(settingsQ.data?.general ?? {}), theme: next } }).catch(() => {});
  };

  // General preferences coming from the server: once authenticated, the server takes over
  // (localStorage was only the instant cache up to this point). Reconciles theme and language if
  // they diverge from what is currently applied.
  const settingsQ = useQuery({ queryKey: ["settings"], queryFn: () => settingsApi.get(), retry: false });
  useEffect(() => {
    const g = settingsQ.data?.general;
    if (!g) return;
    if ((g.theme === "light" || g.theme === "dark") && g.theme !== theme) {
      setTheme(g.theme);
      try { localStorage.setItem("mailin.theme", g.theme); } catch { /* private mode */ }
    }
    if (g.language && g.language !== i18n.language) {
      void i18n.changeLanguage(g.language);
      try { localStorage.setItem("mailin.lang", g.language); } catch { /* private mode */ }
    }
    // Only reacts to new server data; theme/i18n deliberately left out of the deps (avoids a re-loop).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settingsQ.data]);

  // Initial view (settings.general.startView): when opening the app at the root (/mail, with no
  // explicit view in the URL), goes to the preferred view. Once per load only — doesn't step on
  // subsequent navigation.
  const startApplied = useRef(false);
  useEffect(() => {
    if (startApplied.current) return;
    const sv = settingsQ.data?.general?.startView;
    if (sv === undefined) return; // waits for settings to load
    startApplied.current = true;
    if (sv && sv !== "today" && viewParam === undefined && VIEWS.includes(sv as View)) setView(sv as View);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settingsQ.data, viewParam]);

  // Switching areas clears the search: a term left behind would silently filter the new area —
  // the person would see an empty list without understanding why.
  useEffect(() => { setSearch(""); }, [view]);

  // Clicking a desktop notification navigates to the ITEM that triggered it. Three sources, one
  // destination: service worker (web push), native app (Rust event), and the Notification
  // constructor (web fallback, whose click lands in this tab).
  // Desktop: "Import mail" under the File menu. The native menu is built in Rust and cannot
  // navigate, so it only emits -- the routes are known here.
  useEffect(() => onTauriEvent("menu-import-mail", () => nav("/mail/profile/migration")), [nav]);
  useEffect(() => onNotificationClick((link) => nav(link)), [nav]);
  useEffect(() => onDesktopNotifClick((link) => nav(link)), [nav]);
  useEffect(() => {
    setNotifNavigate((link) => nav(link));
    return () => setNotifNavigate(null);
  }, [nav]);
  const qc = useQueryClient();
  // Desktop: opening a .vcf with Beskos imports the contact into the address book. Only wired up
  // when authenticated (this shell only mounts once signed in), so import never runs signed out.
  useEffect(() => {
    return initVcardOpen(
      (r) => {
        qc.invalidateQueries({ queryKey: ["contacts"] });
        toast(r.imported > 0 ? t("contacts.vcardImported", { count: r.imported }) : t("contacts.vcardNoneNew"));
        if (r.imported > 0) nav("/mail/contacts");
      },
      (msg) => toast(msg),
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const canAdmin = useAdminEnabled();
  // First access for someone who just signed up: sets the mailbox password, dresses up the house
  // (logo and color), and invites the team. Only shown to whoever can do it (the owner) and while
  // the flag isn't set in settings.
  const [obDone, setObDone] = useState(false);
  const needsOnboarding = !obDone && settingsQ.isSuccess && settingsQ.data?.general?.onboarded !== true && canAdmin;
  // The AI wrote to the wiki (hub's wiki_page event): if you're NOT in the wiki, offers "Open" in a
  // toast; inside the wiki, WikiView handles it (navigates to the created page / refreshes the open one).
  useChatSocket((ev) => {
    if (ev.type !== "wiki_page" || view === "wiki") return;
    const p = ev.payload as { title?: string; link?: string };
    if (p.link) toastAction(t("wiki.aiWrotePage", { title: p.title ?? "" }), t("wiki.open"), () => nav(p.link!));
  });

  const disabledApps = useDisabledApps();
  const assistantEnabled = useAssistantEnabled();
  // The current app was disabled by the organization (or via a direct link): leaves for the first
  // still-enabled app (Mail is no longer guaranteed either — it too can be disabled).
  useEffect(() => {
    if (!disabledApps.includes(view)) return;
    const order: View[] = ["today", "mail", "chat", "kanban", "cal", "files", "docs", "contacts", "meet", "notes", "wiki"];
    setView(order.find((v) => !disabledApps.includes(v)) ?? "settings");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [disabledApps, view]);

  // Badge sources: unread mail (JMAP) + unread notifications by type (meeting, file).
  const boxesQ = useQuery({ queryKey: ["mailboxes"], queryFn: () => session.service.mailboxes(), refetchInterval: 30000 });
  // refetchIntervalInBackground: TanStack Query stops an interval once the tab is hidden, and the
  // OS notification only fires WHILE the tab is hidden (see desktopNotify). The two rules cancelled
  // each other out: nothing was discovered during the only window in which anything would be shown,
  // and by the time the tab came back and the query refetched, the notification was suppressed for
  // being redundant. Desktop alerts for chat, tasks and the calendar were, on the web, near enough
  // dead. The alert sound rode on the same query and arrived just as late.
  const notifQ = useQuery({
    queryKey: ["notifications"], queryFn: () => backend.notifications(),
    refetchInterval: 30000, refetchIntervalInBackground: true, retry: false,
  });
  const chatChansQ = useQuery({
    queryKey: ["chat-channels"], queryFn: () => backend.chatChannels(),
    refetchInterval: 30000, refetchIntervalInBackground: true, retry: false,
  });
  const items = notifQ.data?.items ?? [];

  // Organization branding (color + logo). Applies the color globally and caches it for the login page.
  const brandingQ = useQuery({ queryKey: ["branding"], queryFn: () => backend.brandingGet(), retry: false, staleTime: 300000 });
  const brand = brandingQ.data;
  useEffect(() => {
    applyBrandColor(brand?.color ?? "");
    if (brand) cacheBrand(brand);
  }, [brand?.color, brand?.logo, brand?.name]);

  // Desktop notifications (tab open): mail via JMAP push, the rest via a diff of notifications.
  const inboxId = boxesQ.data?.find((b) => b.role === "inbox")?.id ?? null;
  const notifyEmail = useDesktopNotifier({
    service: session.service, inboxId, notifItems: notifQ.data?.items,
  });

  // Global JMAP push (stays active on any tab): keeps the mail badge alive even outside Mail
  // and feeds the desktop notification for new mail.
  useJmapPush(session.service, () => {
    void qc.invalidateQueries({ queryKey: ["emails"] });
    void qc.invalidateQueries({ queryKey: ["mailboxes"] });
    notifyEmail();
  });
  const unreadOf = (kind: string) => items.filter((n) => n.kind === kind && !n.read_at).length;
  const chatUnread = (chatChansQ.data?.channels ?? []).reduce((s, c) => s + (c.unread ?? 0), 0);
  const badges: Partial<Record<View, number>> = {
    mail: boxesQ.data?.find((b) => b.role === "inbox")?.unreadEmails ?? 0,
    chat: chatUnread,
    meet: unreadOf("meeting_invite"),
    files: unreadOf("file_share"),
  };

  // Entering the Meet area marks invites as read (the view shows them up front). Files do NOT
  // work that way: being in the Drive does not mean you saw a file shared with you (it may be in
  // a section you never opened) — the share notification stays in the bell until you read it.
  useEffect(() => {
    if (view === "meet" && unreadOf("meeting_invite") > 0) {
      backend.notificationsReadKind("meeting_invite").then(() => qc.invalidateQueries({ queryKey: ["notifications"] })).catch(() => {});
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, items.length]);

  // Apps disabled by the organization disappear from the rail for everyone (mail is core; admin is gated separately).
  const off = new Set(disabledApps);
  const allApps: { id: View; label: string; icon: JSX.Element }[] = [
    { id: "today", label: t("webmail.apps.today"), icon: <IcSun size={20} /> },
    { id: "mail", label: t("webmail.apps.mail"), icon: <IcMail size={20} /> },
    { id: "chat", label: t("webmail.apps.chat"), icon: <IcChat size={20} /> },
    { id: "kanban", label: t("webmail.apps.board"), icon: <IcColumns size={20} /> },
    { id: "cal", label: t("webmail.apps.calendar"), icon: <IcCalendar size={20} /> },
    { id: "files", label: t("webmail.apps.files"), icon: <IcFolder size={20} /> },
    // Right after Drive, because it is a view over it: somebody looking for a document looks
    // beside the files, not at the other end of the rail.
    { id: "docs", label: t("webmail.apps.docs"), icon: <IcDoc size={20} /> },
    { id: "contacts", label: t("webmail.apps.contacts"), icon: <IcUsers size={20} /> },
    { id: "meet", label: t("webmail.apps.meet"), icon: <IcVideo size={20} /> },
    { id: "notes", label: t("webmail.apps.notes"), icon: <IcPencilBox size={20} /> },
    { id: "wiki", label: t("webmail.apps.wiki"), icon: <IcBook size={20} /> },
  ];
  const apps = allApps.filter((a) => !off.has(a.id));
  if (canAdmin) apps.push({ id: "admin", label: t("webmail.apps.admin"), icon: <IcShield size={20} /> });

  // Organization policy (admin/security): mandatory MFA with no 2nd factor yet → blocking gate
  // on top of everything until enrollment completes (shares the "me-presence" cache).
  const overlayUp = useOverlayOpen();
  const meGateQ = useQuery({
    queryKey: ["me-presence"], queryFn: () => backend.me(), retry: false,
    // The MFA policy can be turned on at any moment: revalidates on window focus and every 60s,
    // so the gate appears without requiring a manual refresh.
    refetchOnWindowFocus: true, refetchInterval: 60_000,
  });
  // Invalid/expired session detected in the startup check (and the 60s / focus refetches): clears
  // local state and returns to login, even if the wrapper's global handling did not fire.
  const gateErr = meGateQ.error as { status?: number } | null;
  useEffect(() => {
    if (gateErr && (gateErr.status === 401 || gateErr.status === 403)) session.logout();
  }, [gateErr, session]);
  if (meGateQ.data?.mfa_setup_required === true) {
    return <MFAGate method={meGateQ.data.mfa_required_method} email={session.email} onLogout={session.logout} />;
  }

  // Deliberately inside .wm: the design system variables (colors, borders, theme) are
  // defined in that scope. Outside it, the card would render with no background or contrast.
  if (needsOnboarding) {
    return <div className="wm"><Onboarding email={session.email} onDone={() => setObDone(true)} /></div>;
  }

  return (
    <div className="wm">
      {/* Above the shell, not inside it: an unpublished domain or a lapsed account is the reason
          nothing works, and it has to be visible from whatever screen the person is on. */}
      <AccountBanner />
      <div className="shell">
        <header className="topbar">
          <div className="brand">
            {/* With no customer logo, shows the Beskos mark (used to be the initial as text). */}
            <img className={"brand-logo" + (brand?.logo && brand?.logo_white_dark ? " white-dark" : "")} src={brand?.logo || BESKOS_MARK} alt={brand?.name || "Beskos"} />
            <b>{brand?.name || "Beskos"}</b>
          </div>
          {SEARCHABLE.includes(view) && (
            <button className="ib search-icon" title={t("webmail.clearSearch")} onClick={() => setMobSearch((v) => !v)}><IcSearch size={18} /></button>
          )}
          {SEARCHABLE.includes(view) && (
            <label className={"search" + (mobSearch ? " mob-open" : "")}>
              <IcSearch size={16} />
              <input ref={searchRef} placeholder={t(`webmail.searchIn.${view}`)} value={search}
                onChange={(e) => setSearch(e.target.value)} />
              {search && <button className="clear" title={t("webmail.clearSearch")} onClick={() => setSearch("")}>×</button>}
              {view === "mail" && <button className="adv-toggle" title={t("adv.title")} onClick={() => setAdvOpen((v) => !v)}><IcChevron size={16} /></button>}
            </label>
          )}
          <div className="grow" />
          <NotificationsBell />
          <button className={"ib" + (view === "settings" ? " on" : "")} title={t("settings.title")} onClick={() => nav("/mail/settings")}><IcGear size={18} /></button>
          <button className="ib" title={t("webmail.toggleTheme")} onClick={toggleTheme}>{theme === "dark" ? <IcSun size={18} /> : <IcMoon size={18} />}</button>
          <StatusControl email={session.email} onProfile={() => nav("/mail/profile")}
            onImport={() => nav("/mail/profile/migration")} onLogout={session.logout} />
        </header>

        <nav className="apprail">
          {apps.map((a) => {
            const n = badges[a.id] ?? 0;
            return (
              <button key={a.id} className={"app" + (view === a.id ? " on" : "")}
                data-tip={a.label} data-tip-side="right" aria-label={a.label} onClick={() => setView(a.id)}>
                {a.icon}
                {n > 0 && <span className="app-badge">{n > 99 ? "99+" : n}</span>}
              </button>
            );
          })}
          {/* AI assistant: an item at the BOTTOM of the menu (rail), not a bubble hovering over the
              content. Stays mounted even when hidden (the conversation survives navigation). */}
          <AssistantBubble hidden={view === "settings" || !assistantEnabled || overlayUp} />
        </nav>

        <div className="stage">
          {view === "today" && <TodayView session={session} onNavigate={setView} />}
          {/* No mail can have arrived before the domain is published, so the inbox would be empty and
              the emptiness would be read as a broken product. Say why instead. */}
          {view === "mail" && <MailOrDomainNotice>
            <MailView session={session} search={search} sub={sub} sub2={sub2} advOpen={advOpen} onAdvClose={() => setAdvOpen(false)} />
          </MailOrDomainNotice>}
          {view === "chat" && <ChatView search={search} onSearch={fillSearch} session={session} channelParam={sub} onChannel={(id) => nav(`/mail/chat/${id}`)} />}
          {view === "kanban" && <KanbanView search={search} boardParam={sub} cardParam={sub2} onBoard={(id) => nav(`/mail/kanban/${id}`)} onCard={(bid, cid) => nav(cid ? `/mail/kanban/${bid}/${cid}` : `/mail/kanban/${bid}`)} />}
          {view === "cal" && <CalendarView search={search} eventParam={sub} />}
          {/* "file" and "m" are reserved segments: /mail/files/file/<id> opens the file and
              /mail/meet/m/<id> the meeting sheet (notification targets). The ids are UUIDs,
              so they never collide with these names. */}
          {view === "files" && <FilesView search={search} folderParam={sub === "file" ? undefined : sub} fileParam={sub === "file" ? sub2 : undefined} onFolder={(id) => nav(id ? `/mail/files/${id}` : "/mail/files")} />}
          {view === "contacts" && <ContactsView search={search} sub={sub} sub2={sub2} />}
          {view === "meet" && <MeetView roomParam={sub === "m" ? undefined : sub} meetingParam={sub === "m" ? sub2 : undefined} />}
          {view === "docs" && <DocsView search={search} />}
          {view === "notes" && <NotesView noteParam={sub} onNote={(id) => nav(id ? `/mail/notes/${id}` : "/mail/notes")} />}
          {view === "wiki" && <WikiView sub={sub} sub2={sub2} onNav={(s, p) => nav(p ? `/mail/wiki/${s}/${p}` : s ? `/mail/wiki/${s}` : "/mail/wiki")} />}
          {/* empty sec = back to the root (list of sections on mobile); otherwise enters the section. */}
          {view === "admin" && canAdmin && (
            <AdminView section={sub} detail={sub2}
              onSection={(sec) => nav(sec ? `/mail/admin/${sec}` : "/mail/admin")}
              /* The detail (e.g. a domain) lives in the URL: the link is shareable and a refresh doesn't lose the spot. */
              onDetail={(sec, id) => nav(id ? `/mail/admin/${sec}/${id}` : `/mail/admin/${sec}`)} />
          )}
          {view === "profile" && <ProfileView section={sub} onSection={(sec) => nav(sec ? `/mail/profile/${sec}` : "/mail/profile")} />}
          {view === "settings" && <SettingsView section={sub} onSection={(sec) => nav(sec ? `/mail/settings/${sec}` : "/mail/settings")} />}
        </div>
      </div>
      {/* The first-access offer. The server decides whether it appears; this only opens the screen. */}
      <MigrationPrompt onOpen={() => nav("/mail/profile/migration")} />
      <ToastHost />
    </div>
  );
}

// User status (emoji + text), Slack-style. Clicking the avatar opens the editor with presets.
const STATUS_PRESETS = [
  { emoji: "💬", key: "available" }, { emoji: "📅", key: "meeting" }, { emoji: "🎧", key: "focus" },
  { emoji: "🍽️", key: "lunch" }, { emoji: "🏠", key: "remote" }, { emoji: "🌴", key: "vacation" }, { emoji: "🤒", key: "sick" },
];
function StatusControl({ email, onProfile, onImport, onLogout }: { email: string; onProfile: () => void; onImport: () => void; onLogout: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const q = useQuery({ queryKey: ["my-status"], queryFn: () => backend.userStatus(email), retry: false });
  const meQ = useQuery({ queryKey: ["me-presence"], queryFn: () => backend.me(), retry: false });
  const presence = meQ.data?.presence ?? "active";
  const setPresence = async (p: "active" | "away" | "dnd") => { try { await backend.setPresence(p); qc.invalidateQueries({ queryKey: ["me-presence"] }); } catch { /* silent */ } };
  const [emoji, setEmoji] = useState("");
  const [text, setText] = useState("");
  useEffect(() => { if (q.data) { setEmoji(q.data.emoji); setText(q.data.text); } }, [q.data]);
  const save = async (e: string, tx: string) => {
    setOpen(false);
    try { await backend.setMyStatus(e, tx); qc.invalidateQueries({ queryKey: ["my-status"] }); } catch { /* silent */ }
  };
  return (
    <div className="status-wrap">
      <button className="me" title={q.data?.text || email} onClick={() => setOpen((v) => !v)}>
        {meQ.data?.avatar
          ? <img src={meQ.data.avatar} alt="" style={{ width: "100%", height: "100%", borderRadius: "50%", objectFit: "cover" }} />
          : initials(email)}
        {q.data?.emoji && <span className="me-status">{q.data.emoji}</span>}
        <span className={"me-presence " + presence} />
      </button>
      {open && (
        <div className="status-pop" onMouseLeave={() => setOpen(false)}>
          <div className="status-id">
            {meQ.data?.avatar
              ? <img className="status-id-av" src={meQ.data.avatar} alt="" />
              : <span className="status-id-av status-id-ini">{initials(email)}</span>}
            <div className="status-id-tx">
              <b>{meQ.data?.name || email.split("@")[0]}</b>
              <span>{email}</span>
            </div>
          </div>
          <div className="presence-row">
            {(["active", "away", "dnd"] as const).map((p) => (
              <button key={p} className={"presence-opt " + p + (presence === p ? " on" : "")} onClick={() => void setPresence(p)}>
                <span className={"pres-dot " + p} /> {t("presence." + p)}
              </button>
            ))}
          </div>
          <div className="status-in">
            <input className="status-emoji" value={emoji} onChange={(e) => setEmoji(e.target.value)} placeholder="😀" maxLength={4} />
            <input className="status-text" value={text} onChange={(e) => setText(e.target.value)} placeholder={t("status.placeholder")} maxLength={100} onKeyDown={(e) => { if (e.key === "Enter") void save(emoji, text); }} />
          </div>
          <div className="status-presets">
            {STATUS_PRESETS.map((p) => (
              <button key={p.key} onClick={() => void save(p.emoji, t("status." + p.key))}>
                <span className="emo">{p.emoji}</span>{t("status." + p.key)}
              </button>
            ))}
          </div>
          <div className="status-actions">
            {(q.data?.emoji || q.data?.text) && <button className="chipbtn" onClick={() => void save("", "")}>{t("status.clear")}</button>}
            <div className="grow" />
            <button className="add" onClick={() => void save(emoji, text)}>{t("status.save")}</button>
          </div>
          <div className="status-menu">
            <button className="status-mi" onClick={() => { setOpen(false); onProfile(); }}>
              <IcUsers size={16} /> {t("profile.menu")}
            </button>
            {/* Importing lives HERE and not only inside the profile: somebody who has just arrived
                looks for it beside their own name, which is where everything "mine" already is. */}
            <button className="status-mi" onClick={() => { setOpen(false); onImport(); }}>
              <IcDownload size={16} /> {t("migration.title")}
            </button>
            <button className="status-mi danger" onClick={onLogout}>
              <IcLogout size={16} /> {t("webmail.logout")}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

// Initial theme: the saved one (if any), otherwise the attribute already on <html>, otherwise the OS preference.
function currentTheme(): "light" | "dark" {
  try {
    const saved = localStorage.getItem("mailin.theme");
    if (saved === "light" || saved === "dark") return saved;
  } catch { /* private mode */ }
  const attr = document.documentElement.getAttribute("data-theme");
  if (attr === "light" || attr === "dark") return attr;
  return matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}
