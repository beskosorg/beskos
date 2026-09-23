import { useState } from "react";
import { useTranslation } from "react-i18next";
import { backend, viewerKind, type ChatMessage, type ChatAttachment } from "./backend";
import { renderRich } from "./richtext";
import { URL_RE, splitTrailing } from "./links";
import { LinkUnfurl } from "./LinkUnfurl";
import { ChatAttachmentViewer } from "./ChatAttachmentViewer";
import { ChatAttachmentPreview, previewKind } from "./ChatAttachmentPreview";
import { toast } from "./toast";
import { EmojiPicker, pushRecent } from "./EmojiPicker";
import { initials, shortTime } from "./util";
import { Avatar } from "./Avatar";
import { IcTrash, IcPencil, IcReply, IcForward, IcPaperclip, IcPin, IcBell, IcMail, IcColumns } from "../panel/icons";
import { KB, QUICK, parseCall } from "./chatUtils";
import { TimeMenu, BookmarkIcon, CallCard } from "./ChatBits";

export function MessageRow({ m, prev, locale, mine, canManage, highlighted, onReact, onEdit, onDelete, onMarkUnread, onForward, onSave, onExtractTasks, onOpenThread, onOpenProfile }: {
  m: ChatMessage; prev?: ChatMessage; locale: string; mine: boolean; canManage?: boolean; highlighted?: boolean;
  onReact: (mid: string, emoji: string, mine: boolean) => void; onEdit: (body: string) => void; onDelete: () => void;
  onMarkUnread?: () => void; onForward?: () => void; onSave?: () => void; onExtractTasks?: () => void;
  onOpenThread?: (m: ChatMessage) => void; onOpenProfile?: () => void;
}) {
  const { t } = useTranslation();
  const [pick, setPick] = useState(false);
  const [full, setFull] = useState<DOMRect | null>(null);
  const [editing, setEditing] = useState(false);
  const [remind, setRemind] = useState(false);
  const [draft, setDraft] = useState(m.body);
  const [viewAtt, setViewAtt] = useState<ChatAttachment | null>(null);
  const call = m.deleted ? null : parseCall(m.body);
  // Same detection the renderer uses (links.ts), so what is linked and what is previewed can never
  // disagree again -- they used to keep separate regexes and drifted apart.
  const firstUrl = !m.deleted && !call ? (splitTrailing(m.body.match(URL_RE)?.[0] ?? "")[0] || null) : null;
  const grouped = !call && prev && prev.user_id === m.user_id && (new Date(m.created_at).getTime() - new Date(prev.created_at).getTime() < 5 * 60000);

  return (
    <div className={"msg" + (grouped ? " grouped" : "") + (highlighted ? " flash" : "")} data-mid={m.id}>
      {grouped ? <span className="msg-gutter" /> : m.bot_name ? <span className="av bot-av">{initials(m.bot_name)}</span> : <Avatar name={m.author} email={m.author_email} userId={m.user_id} size={34} className="clickable" onClick={onOpenProfile} />}
      <div className="msg-body">
        {!grouped && <div className="msg-h">{m.bot_name ? <b className="author-name">{m.bot_name} <span className="bot-badge">{t("chat.appBadge")}</span></b> : <b className="author-name" onClick={onOpenProfile}>{m.author}</b>}<span className="tm">{shortTime(m.created_at, locale)}</span></div>}
        {m.deleted ? <div className="msg-txt deleted">{t("chat.deleted")}</div>
          : call ? <CallCard call={call} author={m.author} />
          : editing ? (
            <div className="msg-edit">
              {/* textarea (not input): messages have markdown/multiline content. Enter saves, Shift+Enter breaks the line. */}
              <textarea autoFocus rows={1} value={draft}
                onChange={(e) => { setDraft(e.target.value); e.target.style.height = "auto"; e.target.style.height = Math.min(e.target.scrollHeight, 200) + "px"; }}
                onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); if (draft.trim()) { onEdit(draft.trim()); setEditing(false); } } if (e.key === "Escape") setEditing(false); }} />
              <button className="chipbtn" onClick={() => { if (draft.trim()) { onEdit(draft.trim()); setEditing(false); } }}>{t("chat.save")}</button>
            </div>
          ) : m.body ? <div className="msg-txt">{renderRich(m.body)}{m.edited_at && <span className="edited"> ({t("chat.edited")})</span>}</div> : null}

        {firstUrl && !editing && <LinkUnfurl url={firstUrl} />}

        {!m.deleted && m.attachments?.length > 0 && (
          <div className="msg-atts">
            {m.attachments.map((a) => {
              const kind = viewerKind(a.name);
              const prev = previewKind(a.name);
              return kind === "image" ? (
                <button key={a.file_id} className="att-thumb" onClick={() => setViewAtt(a)}>
                  <img src={backend.chatAttachmentUrl(a.file_id, true)} alt={a.name} loading="lazy" />
                </button>
              ) : prev ? (
                // Data at a glance: a CSV becomes a table and a txt/json shows the start, without leaving the conversation.
                <ChatAttachmentPreview key={a.file_id} att={a} kind={prev} onOpen={() => setViewAtt(a)} />
              ) : (
                <button key={a.file_id} className="att-file" onClick={() => (kind ? setViewAtt(a) : window.open(backend.chatAttachmentUrl(a.file_id), "_blank"))}>
                  <span className="att-ic"><IcPaperclip size={15} /></span>
                  <span className="att-meta"><span className="nm">{a.name}</span><span className="z">{KB(a.size)}</span></span>
                </button>
              );
            })}
          </div>
        )}
        {viewAtt && <ChatAttachmentViewer attachment={viewAtt} onClose={() => setViewAtt(null)} />}

        {!m.deleted && m.reactions?.length > 0 && (
          <div className="reacts">
            {m.reactions.map((r) => (
              <button key={r.emoji} className={"react" + (r.mine ? " mine" : "")} onClick={() => onReact(m.id, r.emoji, r.mine)}>
                {r.emoji} {r.count}
              </button>
            ))}
          </div>
        )}
        {onOpenThread && m.reply_count > 0 && (
          <button className="thread-chip" onClick={() => onOpenThread(m)}>
            <IcReply size={13} /> {t("chat.replies", { count: m.reply_count })}
          </button>
        )}
      </div>
      {!m.deleted && !call && (
        <div className="msg-actions">
          <div className="react-wrap">
            <button className="ib sm" title={t("chat.react")} onClick={() => setPick((v) => !v)}>☺</button>
            {pick && (
              <div className="emoji-pop" onMouseLeave={() => setPick(false)}>
                {QUICK.map((e) => <button key={e} onClick={() => { onReact(m.id, e, false); pushRecent(e); setPick(false); }}>{e}</button>)}
                <button className="more" title={t("chat.emojiMore")} onClick={(e) => { setFull(e.currentTarget.getBoundingClientRect()); setPick(false); }}>+</button>
              </div>
            )}
            {full && <EmojiPicker anchor={full} onClose={() => setFull(null)}
              onPick={(ch) => { onReact(m.id, ch, false); pushRecent(ch); setFull(null); }} />}
          </div>
          {onOpenThread && <button className="ib sm" title={t("chat.reply")} onClick={() => onOpenThread(m)}><IcReply size={13} /></button>}
          {onForward && !m.deleted && <button className="ib sm" title={t("chat.forward")} onClick={onForward}><IcForward size={13} /></button>}
          {onSave && !m.deleted && <button className={"ib sm" + (m.saved ? " saved-on" : "")} title={m.saved ? t("chat.unsaveMsg") : t("chat.saveMsg")} onClick={onSave}><BookmarkIcon filled={m.saved} /></button>}
          <button className="ib sm" title={t("chat.pin")} onClick={() => void backend.chatPin(m.id, true).then(() => toast(t("chat.pinned"))).catch(() => {})}><IcPin size={13} /></button>
          {!m.deleted && (
            <span className="remind-wrap">
              <button className="ib sm" title={t("chat.remindMe")} onClick={() => setRemind((v) => !v)}><IcBell size={13} /></button>
              {remind && <TimeMenu onClose={() => setRemind(false)}
                onPick={(d) => { setRemind(false); void backend.chatRemind(m.id, d.toISOString()).then(() => toast(t("chat.reminderSet"))).catch((e) => toast((e as Error).message)); }} />}
            </span>
          )}
          {onMarkUnread && <button className="ib sm" title={t("chat.markUnread")} onClick={onMarkUnread}><IcMail size={13} /></button>}
          {onExtractTasks && m.body.trim() !== "" && (
            <button className="ib sm" title={t("webmail.ai.extractTasks")} onClick={onExtractTasks}><IcColumns size={13} /></button>
          )}
          {mine && <button className="ib sm" title={t("chat.edit")} onClick={() => { setDraft(m.body); setEditing(true); }}><IcPencil size={13} /></button>}
          {/* Delete: the author deletes their own; a manager/owner moderates others'. */}
          {(mine || canManage) && <button className="ib sm" title={t("chat.delete")} onClick={onDelete}><IcTrash size={13} /></button>}
        </div>
      )}
    </div>
  );
}
