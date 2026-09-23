import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import * as locales from "@blocknote/core/locales";
import { BlockNoteView } from "@blocknote/mantine";
import { useCreateBlockNote } from "@blocknote/react";

import { wikiApi, backend } from "./backend";
import { toast } from "./toast";
import { IcTrash } from "../panel/icons";

const AV_COLORS = ["#e07a5f", "#3d84a8", "#81b29a", "#f2a154", "#9b5de5", "#00afb9", "#ef476f", "#118ab2"];
function avColor(name: string): string {
  let h = 0;
  for (const c of name) h = (h * 31 + c.charCodeAt(0)) | 0;
  return AV_COLORS[Math.abs(h) % AV_COLORS.length];
}

// WikiComments: comments on a page. Reading = viewer; commenting = commenter (role >= 2); deleting
// your own comment (or anyone's if manager). No native dialogs — delete is a direct button.

export function WikiComments({ pageId, canComment, canManage }: { pageId: string; canComment: boolean; canManage: boolean }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [editorKey, setEditorKey] = useState(0); // recreates the editor after posting (clears the field)

  const me = useQuery({ queryKey: ["me"], queryFn: () => backend.me(), retry: false, staleTime: 60_000 });
  const comments = useQuery({ queryKey: ["wiki-comments", pageId], queryFn: () => wikiApi.comments(pageId), retry: false });
  const refresh = () => qc.invalidateQueries({ queryKey: ["wiki-comments", pageId] });

  const add = useMutation({
    mutationFn: (doc: unknown) => wikiApi.addComment(pageId, doc),
    onSuccess: () => { setEditorKey((k) => k + 1); refresh(); },
    onError: (e) => toast((e as Error).message),
  });
  const del = useMutation({
    mutationFn: (id: string) => wikiApi.deleteComment(id),
    onSuccess: refresh,
    onError: (e) => toast((e as Error).message),
  });

  const myId = me.data?.id;
  const fmtWhen = (iso: string) => new Date(iso).toLocaleString(i18n.language, { dateStyle: "medium", timeStyle: "short" });

  return (
    <section className="wiki-comments">
      <h3 className="wiki-comments-title">{t("wiki.comments")}</h3>
      {comments.data && comments.data.length === 0 && <div className="wiki-muted">{t("wiki.noComments")}</div>}
      <ul className="wiki-comment-list">
        {comments.data?.map((c) => (
          <li key={c.id} className="wiki-comment">
            <span className="wiki-comment-avatar" style={{ background: avColor(c.author_name) }}>{(c.author_name || "?").charAt(0).toUpperCase()}</span>
            <div className="wiki-comment-main">
            <div className="wiki-comment-head">
              <b>{c.author_name}</b>
              <span className="wiki-comment-when">{fmtWhen(c.created_at)}</span>
              {(canManage || c.author_user_id === myId) && (
                <button className="ib danger sm" title={t("wiki.remove")} onClick={() => del.mutate(c.id)}><IcTrash size={13} /></button>
              )}
            </div>
            {c.body_html ? (
              <div className="wiki-comment-body" dangerouslySetInnerHTML={{ __html: c.body_html }} />
            ) : (
              <div className="wiki-comment-body">{c.body}</div>
            )}
            </div>
          </li>
        ))}
      </ul>
      {canComment && (
        <CommentEditor key={editorKey} busy={add.isPending} onSubmit={(doc) => add.mutate(doc)} />
      )}
    </section>
  );
}

// CommentEditor: mini rich editor (BlockNote) for comments — same format/derivation as pages,
// with the toolbar visible chat-style (bold/italic/…): formatting in plain sight, not only via selection.
function CommentEditor({ onSubmit, busy }: { onSubmit: (doc: unknown) => void; busy: boolean }) {
  const { t, i18n } = useTranslation();
  const editor = useCreateBlockNote({
    dictionary: i18n.language.toLowerCase().startsWith("pt") ? locales.pt : locales.en,
  });
  const theme = document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light";
  const style = (st: Record<string, boolean>) => { editor.focus(); editor.toggleStyles(st); };
  const block = (type: "bulletListItem" | "quote") => {
    editor.focus();
    const cur = editor.getTextCursorPosition().block;
    editor.updateBlock(cur, { type } as never);
  };
  return (
    <div className="wiki-comment-form">
      <div className="wiki-comment-editor">
        <div className="rich-toolbar wiki-comment-toolbar">
          <button title={t("chat.bold")} onMouseDown={(e) => { e.preventDefault(); style({ bold: true }); }}><b>B</b></button>
          <button title={t("chat.italic")} onMouseDown={(e) => { e.preventDefault(); style({ italic: true }); }}><i>I</i></button>
          <button title={t("chat.strike")} onMouseDown={(e) => { e.preventDefault(); style({ strike: true }); }}><s>S</s></button>
          <span className="sep" />
          <button title={t("chat.code")} onMouseDown={(e) => { e.preventDefault(); style({ code: true }); }}>{"</>"}</button>
          <span className="sep" />
          <button title={t("chat.list")} onMouseDown={(e) => { e.preventDefault(); block("bulletListItem"); }}>•</button>
          <button title={t("chat.quote")} onMouseDown={(e) => { e.preventDefault(); block("quote"); }}>❝</button>
        </div>
        <BlockNoteView editor={editor} theme={theme} sideMenu={false} />
      </div>
      <button className="add" disabled={busy} onClick={() => onSubmit(editor.document)}>{t("wiki.comment")}</button>
    </div>
  );
}
