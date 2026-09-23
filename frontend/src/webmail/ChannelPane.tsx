import { Fragment, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { WebmailSession } from "./auth";
import { backend, type ChatChannel, type ChatMessage, type ChatMember } from "./backend";
import { openRoom } from "./MeetRoom";
import { toast } from "./toast";
import { Avatar } from "./Avatar";
import { IcUsers, IcX, IcVideo, IcPhone, IcPaperclip, IcBell, IcSearch, IcList, IcClock } from "../panel/icons";
import { ExtractTasksModal } from "./ExtractTasksModal";
import { callBody } from "./chatUtils";
import { useFileDrop } from "./useFileDrop";
import { Composer, type ComposerHandle } from "./ChatComposer";
import { MessageRow } from "./ChatMessage";
import { ChannelPanel, CanvasTab, PinsTab, FilesLinksTab, MemberProfile } from "./ChatPanel";
import { ScheduledModal, ForwardModal } from "./ChatModals";

// ---- Channel pane: messages + composer ----
export function ChannelPane({ channel, channels, session, locale, typingAuthor, onTyping, onOpenThread, onOpenDM, onSearch, onToggleRail, focusMsg, onFocusConsumed }: {
  channel: ChatChannel; channels: ChatChannel[]; session: WebmailSession; locale: string; typingAuthor?: string; onTyping: () => void;
  onOpenThread: (m: ChatMessage) => void; onOpenDM: (email: string) => void; onSearch?: (v: string) => void; onToggleRail?: () => void;
  focusMsg?: string | null; onFocusConsumed?: () => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [panelOpen, setPanelOpen] = useState(false);
  const [tab, setTab] = useState<"messages" | "canvas" | "files" | "pins">("messages");
  const [notifOpen, setNotifOpen] = useState(false);
  const [showSched, setShowSched] = useState(false);
  const [notifPref, setNotifPref] = useState(channel.notif_pref ?? "all");
  const [profile, setProfile] = useState<ChatMember | null>(null);
  // Goes back to Messages when the channel changes.
  useEffect(() => { setTab("messages"); setNotifPref(channel.notif_pref ?? "all"); }, [channel.id]);
  const setPref = async (p: "all" | "mentions" | "none") => { setNotifPref(p); setNotifOpen(false); try { await backend.chatSetNotifPref(channel.id, p); qc.invalidateQueries({ queryKey: ["chat-channels"] }); } catch (e) { toast((e as Error).message); } };
  const msgsQ = useQuery({ queryKey: ["chat-messages", channel.id], queryFn: () => backend.chatMessages(channel.id) });
  const membersQ = useQuery({ queryKey: ["chat-members", channel.id], queryFn: () => backend.chatMembers(channel.id), refetchInterval: 25000 });
  const scroller = useRef<HTMLDivElement>(null);

  // History pagination: the base is the most recent page (msgsQ); `older` accumulates the
  // previous pages loaded on demand. Combined and deduplicated by id.
  const [older, setOlder] = useState<ChatMessage[]>([]);
  const [moreOlder, setMoreOlder] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  useEffect(() => { setOlder([]); setMoreOlder(false); }, [channel.id]); // reset when the channel changes
  const base = useMemo(() => msgsQ.data?.messages ?? [], [msgsQ.data]);
  const messages = useMemo(() => {
    const seen = new Set(base.map((m) => m.id));
    const combined = [...older.filter((m) => !seen.has(m.id)), ...base];
    // Sorts by date: robust when the "jump to message" window gets merged into the history.
    combined.sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime());
    return combined;
  }, [older, base]);
  // More older ones? Before loading any page, it's the base's has_more; afterwards, the last page's.
  const hasMore = older.length > 0 ? moreOlder : (msgsQ.data?.has_more ?? false);

  const canManage = channel.my_role === "owner" || channel.my_role === "manager";
  // "Unread" boundary: captures last_read_at at the moment the channel was opened (before the
  // automatic mark-read) — so the "new messages" divider shows where the person left off.
  const [unreadBoundary, setUnreadBoundary] = useState<string | null>(null);
  useEffect(() => { setUnreadBoundary(channel.last_read_at ?? null); }, [channel.id]);
  const firstUnreadIdx = useMemo(() => (unreadBoundary ? messages.findIndex((m) => m.created_at > unreadBoundary) : -1), [messages, unreadBoundary]);
  const markUnread = async (msgId: string) => {
    try { await backend.chatMarkUnread(msgId); qc.invalidateQueries({ queryKey: ["chat-channels"] }); toast(t("chat.markedUnread")); }
    catch (e) { toast((e as Error).message); }
  };

  // Jump to a message (external search OR a pin from the channel itself): ensures the target is
  // loaded (fetches the window around it if needed), scrolls to it and highlights it briefly.
  const [highlight, setHighlight] = useState<string | null>(null);
  const [localFocus, setLocalFocus] = useState<string | null>(null); // e.g.: clicking a pin
  const target = focusMsg ?? localFocus;
  useEffect(() => {
    if (!target) return;
    let cancelled = false;
    (async () => {
      if (!messages.some((m) => m.id === target)) {
        try {
          const win = await backend.chatMessagesAround(channel.id, target);
          if (cancelled) return;
          const b = msgsQ.data?.messages ?? [];
          setOlder((prev) => { const have = new Set([...prev, ...b].map((m) => m.id)); return [...win.messages.filter((m) => !have.has(m.id)), ...prev]; });
        } catch { /* if it fails, at least open the channel */ }
      }
      setTimeout(() => {
        if (cancelled) return;
        scroller.current?.querySelector(`[data-mid="${target}"]`)?.scrollIntoView({ block: "center" });
        setHighlight(target);
        setTimeout(() => setHighlight(null), 2500);
        onFocusConsumed?.();
        setLocalFocus(null);
      }, 140);
    })();
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target, channel.id]);
  const jumpToPin = (msgId: string) => { setTab("messages"); setLocalFocus(msgId); };
  const [forwarding, setForwarding] = useState<string | null>(null); // id of the message to forward
  const [extracting, setExtracting] = useState<ChatMessage | null>(null); // message to turn into tasks

  // Preserves the scroll position when prepending older messages (content grows on top).
  const prevH = useRef<number | null>(null);
  useLayoutEffect(() => {
    if (prevH.current != null && scroller.current) {
      scroller.current.scrollTop += scroller.current.scrollHeight - prevH.current;
      prevH.current = null;
    }
  }, [older]);

  const loadOlder = async () => {
    if (loadingOlder || !messages.length) return;
    setLoadingOlder(true);
    prevH.current = scroller.current?.scrollHeight ?? 0;
    try {
      const page = await backend.chatMessages(channel.id, messages[0].created_at);
      setOlder((prev) => {
        const seen = new Set([...prev, ...base].map((m) => m.id));
        return [...page.messages.filter((m) => !seen.has(m.id)), ...prev];
      });
      setMoreOlder(page.has_more);
    } catch (e) { prevH.current = null; toast((e as Error).message); }
    finally { setLoadingOlder(false); }
  };

  // Starts a call (audio/video) reusing Meet/LiveKit: posts the invite in the channel and joins the room.
  const startCall = async (mode: "video" | "audio") => {
    const room = `chat-${channel.id}`;
    try { await backend.chatSend(channel.id, callBody(mode, room)); } catch { /* proceeds anyway */ }
    openRoom(room, mode === "audio");
  };

  // Marks as read on open/receive and scrolls to the end.
  useEffect(() => { void backend.chatMarkRead(channel.id).then(() => qc.invalidateQueries({ queryKey: ["chat-channels"] })); }, [channel.id, base.length, qc]);
  // Scrolls to the end when a NEW message arrives (the last one changes) or when opening the
  // channel — never when loading older ones (there the last id doesn't change and the position is
  // preserved by the useLayoutEffect above).
  const lastId = messages[messages.length - 1]?.id;
  useEffect(() => { scroller.current?.scrollTo({ top: scroller.current.scrollHeight }); }, [lastId, channel.id]);

  const refresh = () => qc.invalidateQueries({ queryKey: ["chat-messages", channel.id] });
  // react/edit/delete don't refetch the channel: the WS echo applies the incremental patch (for all members).
  const react = async (mid: string, emoji: string, mine: boolean) => { await backend.chatReact(mid, emoji, mine); };

  // Dragging files onto any point of the channel → becomes an attachment in the composer.
  const composerRef = useRef<ComposerHandle>(null);
  const { over, handlers } = useFileDrop((files) => composerRef.current?.addFiles(files));

  return (
    <div className="chat-pane" {...handlers}>
      <header className="chat-head">
        {onToggleRail && <button className="ib chat-burger" onClick={onToggleRail} aria-label="menu"><IcList size={17} /></button>}
        {channel.kind === "dm" ? <Avatar name={channel.name} userId={channel.other_id} size={28} radius="8px" className="dot-av sm" /> : <span className="hash">#</span>}
        <b>{channel.name}</b>
        {channel.topic && <span className="topic">{channel.topic}</span>}
        <div className="grow" />
        {channel.kind !== "dm" && (membersQ.data?.length ?? 0) > 0 && (
          <button className="member-stack" title={t("chat.members")} onClick={() => setPanelOpen(true)}>
            {(membersQ.data ?? []).slice(0, 4).map((m) => (
              <Avatar key={m.user_id} name={m.name} email={m.email} userId={m.user_id} size={24} className="ms-av" />
            ))}
            <span className="ms-count">{membersQ.data?.length}</span>
          </button>
        )}
        {onSearch && <button className="ib" title={t("chat.searchChannel")} onClick={() => onSearch(`in:${channel.name} `)}><IcSearch size={17} /></button>}
        <div className="notif-wrap">
          <button className={"ib" + (notifPref !== "all" ? " muted-on" : "")} title={t("chat.notifPref")} onClick={() => setNotifOpen((v) => !v)}><IcBell size={17} /></button>
          {notifOpen && (
            <div className="notif-menu" onMouseLeave={() => setNotifOpen(false)}>
              {(["all", "mentions", "none"] as const).map((p) => (
                <button key={p} className={notifPref === p ? "on" : ""} onClick={() => void setPref(p)}>{t("chat.notif_" + p)}</button>
              ))}
            </div>
          )}
        </div>
        <button className="ib" title={t("chat.scheduledTitle")} onClick={() => setShowSched(true)}><IcClock size={17} /></button>
        <button className="ib" title={t("chat.audioCall")} onClick={() => startCall("audio")}><IcPhone size={17} /></button>
        <button className="ib" title={t("chat.videoCall")} onClick={() => startCall("video")}><IcVideo size={18} /></button>
        {channel.kind !== "dm" && <button className="ib" title={t("chat.members")} onClick={() => setPanelOpen(true)}><IcUsers size={18} /></button>}
      </header>
      {showSched && <ScheduledModal onClose={() => setShowSched(false)} />}
      {profile && <MemberProfile member={profile} me={session.email} onClose={() => setProfile(null)} onOpenDM={(e) => { setProfile(null); onOpenDM(e); }} />}
      {/* Extract tasks from the message: reuses the same flow as email (AI → choose → board). */}
      {extracting && (
        <ExtractTasksModal subject={`#${channel.name} · ${extracting.author}`} body={extracting.body}
          sourceEmailId="" onClose={() => setExtracting(null)} />
      )}

      {forwarding && <ForwardModal channels={channels} onClose={() => setForwarding(null)}
        onPick={async (target) => { try { await backend.chatForward(forwarding, target); setForwarding(null); toast(t("chat.forwarded")); } catch (e) { toast((e as Error).message); } }} />}
      {channel.kind !== "dm" && (
        <div className="chat-tabs">
          <button className={tab === "messages" ? "on" : ""} onClick={() => setTab("messages")}>{t("chat.messages")}</button>
          <button className={tab === "canvas" ? "on" : ""} onClick={() => setTab("canvas")}>{t("chat.canvas")}</button>
          <button className={tab === "files" ? "on" : ""} onClick={() => setTab("files")}>{t("chat.filesLinks")}</button>
          <button className={tab === "pins" ? "on" : ""} onClick={() => setTab("pins")}>{t("chat.pins")}</button>
        </div>
      )}
      {panelOpen && <ChannelPanel channel={channel} me={session.email} onClose={() => setPanelOpen(false)} />}

      {tab === "canvas" && <div className="chat-tabpane"><CanvasTab channelId={channel.id} /></div>}
      {tab === "pins" && <div className="chat-tabpane"><PinsTab channelId={channel.id} onJump={jumpToPin} /></div>}
      {tab === "files" && <div className="chat-tabpane"><FilesLinksTab channelId={channel.id} /></div>}

      {tab === "messages" && <>
      <div className="chat-msgs" ref={scroller}>
        {hasMore && messages.length > 0 && (
          <button className="chat-loadmore" onClick={loadOlder} disabled={loadingOlder}>
            {loadingOlder ? "…" : t("chat.loadOlder")}
          </button>
        )}
        {msgsQ.isLoading ? <div className="loading">…</div>
          : messages.length === 0 ? <div className="empty">{t("chat.firstMessage")}</div>
          : messages.map((m, i) => (
            <Fragment key={m.id}>
              {i === firstUnreadIdx && firstUnreadIdx >= 1 && <div className="chat-unread-divider"><span>{t("chat.newMessages")}</span></div>}
              <MessageRow m={m} prev={messages[i - 1]} locale={locale} mine={m.author_email === session.email} canManage={canManage} highlighted={highlight === m.id}
                onReact={react} onEdit={async (body) => { await backend.chatEdit(m.id, body); }}
                onDelete={async () => { await backend.chatDelete(m.id); }} onMarkUnread={() => markUnread(m.id)}
                onExtractTasks={() => setExtracting(m)}
                onForward={() => setForwarding(m.id)} onSave={async () => { try { if (m.saved) await backend.chatUnsave(m.id); else await backend.chatSave(m.id); refresh(); } catch (e) { toast((e as Error).message); } }} onOpenThread={onOpenThread}
                onOpenProfile={() => setProfile({ user_id: m.user_id, email: m.author_email, name: m.author, role: "" })} />
            </Fragment>
          ))}
        {/* Read receipt: who has already read up to the last message (last_read_at >= its created_at). */}
        {(() => {
          const last = messages[messages.length - 1];
          if (!last || last.deleted) return null;
          const lastTs = new Date(last.created_at).getTime();
          const readers = (membersQ.data ?? []).filter((mem) => mem.email !== last.author_email && mem.email !== session.email && mem.last_read_at && new Date(mem.last_read_at).getTime() >= lastTs);
          if (readers.length === 0) return null;
          const label = channel.kind === "dm"
            ? t("chat.seen")
            : t("chat.seenBy", { names: readers.slice(0, 3).map((r) => r.name || r.email).join(", ") }) + (readers.length > 3 ? ` +${readers.length - 3}` : "");
          return <div className="read-receipt">✓ {label}</div>;
        })()}
        {typingAuthor && <div className="typing">{t("chat.typing", { name: typingAuthor })}</div>}
      </div>

      <Composer ref={composerRef} channelId={channel.id} members={membersQ.data ?? []} onTyping={onTyping} dm={channel.kind === "dm"}
        onSent={() => { refresh(); qc.invalidateQueries({ queryKey: ["chat-channels"] }); }} />
      </>}

      {over && <div className="drop-overlay"><div className="drop-card"><IcPaperclip size={22} /> {t("chat.dropHere")}</div></div>}
    </div>
  );
}

// Thread panel (3rd column): parent message + replies + composer that replies to the parent_id.
export function ThreadPanel({ parent, channelId, session, locale, onClose }: {
  parent: ChatMessage; channelId: string; session: WebmailSession; locale: string; onClose: () => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const repliesQ = useQuery({ queryKey: ["chat-thread", parent.id], queryFn: () => backend.chatReplies(parent.id) });
  const membersQ = useQuery({ queryKey: ["chat-members", channelId], queryFn: () => backend.chatMembers(channelId) });
  const replies = repliesQ.data?.replies ?? [];
  // Only revalidates the thread (small query). The main list is updated incrementally by the WS
  // echo (parent's reply count, edits, reactions) — it doesn't refetch the whole channel.
  const refresh = () => { qc.invalidateQueries({ queryKey: ["chat-thread", parent.id] }); };
  const react = async (mid: string, emoji: string, mine: boolean) => { await backend.chatReact(mid, emoji, mine); refresh(); };
  const composerRef = useRef<ComposerHandle>(null);
  const { over, handlers } = useFileDrop((files) => composerRef.current?.addFiles(files));

  return (
    <aside className="thread-panel" {...handlers}>
      <header className="chat-head"><b>{t("chat.thread")}</b><div className="grow" /><button className="ib" title={t("common.close")} onClick={onClose}><IcX size={15} /></button></header>
      <div className="chat-msgs">
        <MessageRow m={parent} locale={locale} mine={parent.author_email === session.email} onReact={react}
          onEdit={async (body) => { await backend.chatEdit(parent.id, body); refresh(); }} onDelete={async () => { await backend.chatDelete(parent.id); refresh(); }} />
        <div className="thread-sep">{t("chat.replies", { count: replies.length })}</div>
        {replies.map((m, i) => (
          <MessageRow key={m.id} m={m} prev={replies[i - 1]} locale={locale} mine={m.author_email === session.email}
            onReact={react} onEdit={async (body) => { await backend.chatEdit(m.id, body); refresh(); }}
            onDelete={async () => { await backend.chatDelete(m.id); refresh(); }} />
        ))}
      </div>
      <Composer ref={composerRef} channelId={channelId} members={membersQ.data ?? []} parentId={parent.id} onTyping={() => {}} onSent={refresh} />
      {over && <div className="drop-overlay"><div className="drop-card"><IcPaperclip size={22} /> {t("chat.dropHere")}</div></div>}
    </aside>
  );
}
