import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { commentsApi, colleaguesApi, type CardComment } from "./backend";
import { Spinner } from "./Spinner";
import { toast } from "./toast";
import { initials, hueFor, shortTime } from "./util";
import { renderRich } from "./richtext";
import { MdToolbar, MdEditor, wrapSelection } from "./MdEditor";
import { IcTrash, IcReply, IcPencil } from "../panel/icons";

// Comments on the task. Who gets notified (mention, reply, assignee, creator) is decided in the
// backend — this only writes. The @ suggests COLLEAGUES: mentioning someone without an account notifies no one.
export function TaskComments({ cardId, meId }: { cardId: string; meId?: string }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [replyTo, setReplyTo] = useState<CardComment | null>(null);
  const q = useQuery({ queryKey: ["card-comments", cardId], queryFn: () => commentsApi.list(cardId), retry: false });

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["card-comments", cardId] });
    qc.invalidateQueries({ queryKey: ["board"] });      // changes the count on the card
    qc.invalidateQueries({ queryKey: ["notifications"] });
  };
  const del = useMutation({
    mutationFn: (id: string) => commentsApi.remove(cardId, id),
    onSuccess: refresh, onError: (e) => toast((e as Error).message),
  });
  const [editing, setEditing] = useState<{ id: string; body: string } | null>(null);
  const edit = useMutation({
    mutationFn: ({ id, body }: { id: string; body: string }) => commentsApi.edit(cardId, id, body),
    onSuccess: () => { setEditing(null); refresh(); }, onError: (e) => toast((e as Error).message),
  });

  const list = q.data ?? [];
  const roots = list.filter((c) => !c.parent_id);
  const repliesOf = (id: string) => list.filter((c) => c.parent_id === id);

  const item = (c: CardComment, isReply: boolean) => (
    <div className={"cmt" + (isReply ? " reply" : "")} key={c.id}>
      <span className="av" style={{ background: hueFor(c.author_email || c.author_name) }}>{initials(c.author_name || c.author_email)}</span>
      <div className="cmt-b">
        <div className="cmt-h">
          <b>{c.author_name || c.author_email}</b>
          <span className="tm">{shortTime(c.created_at, i18n.language)}</span>
          <div className="grow" />
          {!isReply && <button className="ib" title={t("comments.reply")} onClick={() => setReplyTo(c)}><IcReply size={13} /></button>}
          {c.author_id === meId && <button className="ib" title={t("chat.edit")} onClick={() => setEditing({ id: c.id, body: c.body })}><IcPencil size={13} /></button>}
          {c.author_id === meId && <button className="ib" title={t("contacts.delete")} onClick={() => del.mutate(c.id)}><IcTrash size={13} /></button>}
        </div>
        {editing?.id === c.id ? (
          <div className="cmt-edit">
            <MdEditor value={editing.body} onChange={(v) => setEditing({ id: c.id, body: v })} autoFocus />
            <div className="cmt-edit-acts">
              <button className="chipbtn" onClick={() => setEditing(null)}>{t("common.cancel")}</button>
              <button className="add" disabled={!editing.body.trim() || edit.isPending} onClick={() => edit.mutate({ id: c.id, body: editing.body.trim() })}>{t("contacts.save")}</button>
            </div>
          </div>
        ) : <div className="cmt-t">{renderRich(c.body)}</div>}
      </div>
    </div>
  );

  return (
    <div className="cmts">
      <div className="rlabel">{t("comments.title")} {list.length > 0 && <span className="cc">{list.length}</span>}</div>
      {q.isLoading ? <div className="loading">…</div>
        : roots.length === 0 ? <p className="cd-empty">{t("comments.empty")}</p>
        : roots.map((c) => (
          <div key={c.id}>
            {item(c, false)}
            {repliesOf(c.id).map((r) => item(r, true))}
          </div>
        ))}
      <Composer cardId={cardId} replyTo={replyTo} onCancelReply={() => setReplyTo(null)} onSent={() => { setReplyTo(null); refresh(); }} />
    </div>
  );
}


function Composer({ cardId, replyTo, onCancelReply, onSent }: {
  cardId: string; replyTo: CardComment | null; onCancelReply: () => void; onSent: () => void;
}) {
  const { t } = useTranslation();
  const [body, setBody] = useState("");
  const [mentionQ, setMentionQ] = useState<string | null>(null);
  const ref = useRef<HTMLTextAreaElement>(null);
  const people = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false });

  const sugg = useMemo(() => {
    if (mentionQ === null) return [];
    const s = mentionQ.toLowerCase();
    return (people.data ?? []).filter((p) => p.email.toLowerCase().includes(s) || p.name.toLowerCase().includes(s)).slice(0, 5);
  }, [mentionQ, people.data]);

  const send = useMutation({
    mutationFn: () => commentsApi.add(cardId, body.trim(), replyTo?.id),
    onSuccess: () => { setBody(""); onSent(); },
    onError: (e) => toast((e as Error).message),
  });

  const onChange = (v: string) => {
    setBody(v);
    const m = v.slice(0, ref.current?.selectionStart ?? v.length).match(/@([\w.\-+]*)$/);
    setMentionQ(m ? m[1] : null);
  };
  // Writes the local part (@ana): this is how the backend matches the mention — by email, never by name.
  const pick = (email: string) => {
    const local = email.split("@")[0];
    setBody((b) => b.replace(/@([\w.\-+]*)$/, `@${local} `));
    setMentionQ(null);
    ref.current?.focus();
  };

  return (
    <div className="cmt-new">
      {replyTo && (
        <div className="cmt-replying">
          {t("comments.replyingTo", { who: replyTo.author_name || replyTo.author_email })}
          <button className="ib" title={t("common.cancel")} onClick={onCancelReply}>×</button>
        </div>
      )}
      <div style={{ position: "relative" }}>
        {/* Same formatting toolbar as the rest (description, notes). The textarea keeps the @mention logic. */}
        <MdToolbar onWrap={(b, a) => wrapSelection(ref.current, body, b, a, onChange)} />
        <textarea ref={ref} value={body} placeholder={t("comments.placeholder")}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && body.trim()) send.mutate(); }} />
        {sugg.length > 0 && (
          <div className="rcpt-sugg">
            {sugg.map((p) => (
              <button key={p.id} type="button" onMouseDown={(e) => { e.preventDefault(); pick(p.email); }}>
                <span className="av" style={{ background: hueFor(p.email) }}>{initials(p.name || p.email)}</span>
                <span className="nm">{p.name || p.email}</span><span className="em">{p.email}</span>
              </button>
            ))}
          </div>
        )}
      </div>
      <div className="cmt-actions">
        <span className="desc">{t("comments.hint")}</span>
        <button className="chipbtn" disabled={!body.trim() || send.isPending} onClick={() => send.mutate()}>
          {send.isPending ? <Spinner size={13} /> : t("comments.send")}
        </button>
      </div>
    </div>
  );
}
