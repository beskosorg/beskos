import type { ReactNode } from "react";
import type { EmailSummary } from "../jmap/types";
import { Avatar } from "./Avatar";
import { displayName, shortTime } from "./util";
import { IcPaperclip, IcStar } from "../panel/icons";
import { LabelChips } from "./MailLabels";

// One row of the mail list, extracted from MailView.tsx (law 1.4 -- 600 lines).
interface MailListRowProps {
  m: EmailSummary;
  outgoing: boolean;
  selected: string | null;
  focusedId: string | null;
  dragId: string | null;
  picked: boolean;
  locale: string;
  folderChip: (m: EmailSummary) => ReactNode;
  onOpen: (id: string, unread: boolean) => void;
  onDragStart: (id: string) => void;
  onDragEnd: () => void;
  onToggleOne: (id: string) => void;
  onToggleStar: (id: string, on: boolean) => Promise<void>;
  t: (key: string, opts?: Record<string, unknown>) => string;
}

export function MailListRow({
  m, outgoing, selected, focusedId, dragId, picked, locale, folderChip,
  onOpen, onDragStart, onDragEnd, onToggleOne, onToggleStar, t,
}: MailListRowProps) {
  // In Sent/Drafts the row's "face" is the recipient; elsewhere, the sender.
  const rcpts = m.to ?? [];
  const primary = outgoing ? rcpts[0] : m.from?.[0];
  const name = outgoing
    ? (rcpts.length === 0 ? t("webmail.list.toNobody")
      : rcpts.length === 1 ? t("webmail.list.to", { who: primary?.name || primary?.email })
      : t("webmail.list.toMany", { who: primary?.name || primary?.email, n: rcpts.length - 1 }))
    : displayName(m.from);
  // The row represents a CONVERSATION: it highlights if the representative message or any sibling
  // is unread -- otherwise an old unread reply would go unnoticed.
  const unread = m.threadUnread || (m.keywords ? !m.keywords.$seen : false);

  return (
    <article draggable
      className={`row ${unread ? "unread" : ""} ${m.id === selected ? "on" : ""} ${m.id === focusedId ? "kbfocus" : ""} ${dragId === m.id ? "dragging" : ""} ${picked ? "picked" : ""}`}
      data-mrow={m.id}
      onClick={() => onOpen(m.id, unread)}
      onDragStart={() => onDragStart(m.id)}
      onDragEnd={onDragEnd}>
      <label className="rowsel" onClick={(e) => e.stopPropagation()}>
        <input type="checkbox" checked={picked} onChange={() => onToggleOne(m.id)} />
      </label>
      <Avatar name={outgoing ? (primary?.name || primary?.email) : name} email={primary?.email} size={34} />
      <div className="rowmid">
        <div className="topl">{unread && <span className="udot" />}<span className="w">{name}</span>
          {m.threadCount && <span className="row-thread">{m.threadCount}</span>}</div>
        <div className="s">{m.subject || t("webmail.noSubject")}</div>
        <div className="sn">{folderChip(m)}{m.preview}<LabelChips keywords={m.keywords} /></div>
      </div>
      <div className="rowr">
        <div className="tm">{m.hasAttachment && <span className="row-clip"><IcPaperclip size={12} /></span>}{shortTime(m.receivedAt, locale)}</div>
        <button className={"star" + (m.keywords?.$flagged ? " on" : "")}
          title={t(m.keywords?.$flagged ? "webmail.unstar" : "webmail.star")}
          onClick={(e) => { e.stopPropagation(); void onToggleStar(m.id, !m.keywords?.$flagged); }}>
          <IcStar size={14} filled={!!m.keywords?.$flagged} />
        </button>
      </div>
    </article>
  );
}
