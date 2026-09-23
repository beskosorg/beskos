import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import { notesApi, type Note } from "./notesApi";
import { WikiEditor } from "./WikiEditor";
import { NoteShareDrawer } from "./NoteShareDrawer";
import { ExtractTasksModal } from "./ExtractTasksModal";
import { Spinner } from "./Spinner";
import { toast } from "./toast";
import { shortTime } from "./util";
import { IcPlus, IcPin, IcUsers, IcSparkle, IcTrash, IcVideo, IcChat } from "../panel/icons";
import { ForwardModal } from "./ChatModals";
import { backend } from "./backend";
import { publicUrl } from "../apiBase";

// Notes: personal by default, shared on purpose. A meeting note is the same object with a meeting
// attached, which is why there is one list and not two.
//
// Layout follows the wiki and the mail: list on the left, the thing itself on the right. The editor
// IS the wiki's, so a note and a page feel the same to write.

const SAVE_DEBOUNCE = 900; // ms of quiet before saving; typing should not be a request per keystroke

export function NotesView({ noteParam, onNote }: { noteParam?: string; onNote: (id: string) => void }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [share, setShare] = useState<string | null>(null);
  const [extract, setExtract] = useState<Note | null>(null);
  const [toChat, setToChat] = useState<Note | null>(null);

  const listQ = useQuery({ queryKey: ["notes"], queryFn: () => notesApi.list(), retry: false });
  const notes = listQ.data?.notes ?? [];
  const current = notes.find((n) => n.id === noteParam) ?? null;

  const create = useMutation({
    mutationFn: () => notesApi.create({ title: "", body_json: [] }),
    onSuccess: (n) => { void qc.invalidateQueries({ queryKey: ["notes"] }); onNote(n.id); },
    onError: (e) => toast((e as Error).message),
  });
  const pin = useMutation({
    mutationFn: ({ id, on }: { id: string; on: boolean }) => notesApi.pin(id, on),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notes"] }),
    onError: (e) => toast((e as Error).message),
  });
  // Sharing to chat posts the LINK, never the content. Whoever reads the channel and already has
  // access sees the note's card; whoever does not sees a plain link and no hint of what is behind
  // it. Pasting a link is not a way to widen who may read a note.
  const chans = useQuery({ queryKey: ["chat-channels"], queryFn: () => backend.chatChannels(), retry: false });
  const sendToChat = useMutation({
    mutationFn: ({ note, channelId }: { note: Note; channelId: string }) =>
      // Markdown, not the bare URL: the reader sees the note's TITLE instead of thirty-six
      // characters of uuid. The preview card still appears, because the address is inside it.
      backend.chatSend(channelId, `[${noteTitle(note, t)}](${publicUrl(`/mail/notes/${note.id}`)})`),
    onSuccess: () => { setToChat(null); toast(t("notes.sentToChat")); },
    onError: (e) => toast((e as Error).message),
  });

  const del = useMutation({
    mutationFn: (id: string) => notesApi.remove(id),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["notes"] }); onNote(""); },
    onError: (e) => toast((e as Error).message),
  });

  return (
    <div className="notes">
      <aside className="notes-list">
        <header className="notes-list-h">
          <b>{t("webmail.apps.notes")}</b>
          <button className="ib" title={t("notes.new")} disabled={create.isPending}
            onClick={() => create.mutate()}>
            {create.isPending ? <Spinner size={14} /> : <IcPlus size={16} />}
          </button>
        </header>
        {listQ.isLoading && <div className="notes-empty"><Spinner size={16} /></div>}
        {!listQ.isLoading && notes.length === 0 && <div className="notes-empty">{t("notes.none")}</div>}
        {notes.map((n) => (
          <button key={n.id} className={"notes-item" + (n.id === noteParam ? " on" : "")} onClick={() => onNote(n.id)}>
            <div className="ni-top">
              {n.pinned && <IcPin size={12} />}
              {n.meeting_id && <IcVideo size={12} />}
              <span className="ni-title">{noteTitle(n, t)}</span>
            </div>
            <span className="ni-sub">{shortTime(n.updated_at, i18n.language)}{n.owner_name ? ` · ${n.owner_name}` : ""}</span>
          </button>
        ))}
      </aside>

      <section className="notes-main">
        {!current && <div className="notes-empty">{t("notes.pick")}</div>}
        {current && (
          <NoteEditor key={current.id} note={current}
            onPin={() => pin.mutate({ id: current.id, on: !current.pinned })}
            onShare={() => setShare(current.id)}
            onExtract={() => setExtract(current)}
            onToChat={() => setToChat(current)}
            onDelete={() => del.mutate(current.id)} />
        )}
      </section>

      {share && <NoteShareDrawer noteId={share} onClose={() => setShare(null)} />}
      {toChat && (
        <ForwardModal channels={chans.data?.channels ?? []} title={t("notes.toChat")} onClose={() => setToChat(null)}
          onPick={(channelId) => sendToChat.mutate({ note: toChat, channelId })} />
      )}
      {/* The same AI extraction the mail and the chat use -- one flow, one result screen. */}
      {extract && (
        <ExtractTasksModal subject={noteTitle(extract, t)} body={extract.body_text} sourceEmailId=""
          onClose={() => setExtract(null)} />
      )}
    </div>
  );
}

// A note with no title shows the meeting's, then a translated placeholder. The title is never
// written into the database in one language -- see migration 00133.
function noteTitle(n: Note, t: (k: string) => string): string {
  return n.title || n.meeting_title || t("notes.untitled");
}

function NoteEditor({ note, onPin, onShare, onExtract, onToChat, onDelete }: {
  note: Note; onPin: () => void; onShare: () => void; onExtract: () => void; onToChat: () => void;
  onDelete: () => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [title, setTitle] = useState(note.title);
  const [confirmDel, setConfirmDel] = useState(false);
  const body = useRef<unknown>(note.body_json);
  const [saving, setSaving] = useState(false);

  const save = useMutation({
    mutationFn: () => notesApi.update(note.id, { title, body_json: body.current }),
    onSuccess: () => { setSaving(false); void qc.invalidateQueries({ queryKey: ["notes"] }); },
    onError: (e) => { setSaving(false); toast((e as Error).message); },
  });

  // Saves after a pause, not on every keystroke. The ref holds the newest body so the timer always
  // sends what is on screen, not what was there when it was armed.
  const timer = useRef<number | undefined>(undefined);
  const queueSave = () => {
    setSaving(true);
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => save.mutate(), SAVE_DEBOUNCE);
  };
  // Leaving with an unsaved change would lose it: the timer dies with the component.
  useEffect(() => () => { window.clearTimeout(timer.current); }, []);

  return (
    <>
      <header className="notes-h">
        <input className="notes-title" value={title} placeholder={t("notes.titlePh")}
          onChange={(e) => { setTitle(e.target.value); queueSave(); }} />
        {saving && <Spinner size={13} />}
        <div className="grow" />
        <button className={"ib" + (note.pinned ? " on" : "")} title={t(note.pinned ? "notes.unpin" : "notes.pin")}
          onClick={onPin}><IcPin size={15} /></button>
        <button className="ib" title={t("notes.extractTasks")} onClick={onExtract}><IcSparkle size={15} /></button>
        <button className="ib" title={t("notes.share")} onClick={onShare}><IcUsers size={15} /></button>
        <button className="ib" title={t("notes.toChat")} onClick={onToChat}><IcChat size={15} /></button>
        <button className="ib" title={t("notes.delete")} onClick={() => setConfirmDel(true)}><IcTrash size={15} /></button>
      </header>
      {/* Inline confirmation, never a native dialog (project convention). */}
      {confirmDel && (
        <div className="notes-confirm">
          <span>{t("notes.deleteConfirm")}</span>
          <button className="chipbtn danger" onClick={onDelete}>{t("notes.delete")}</button>
          <button className="chipbtn" onClick={() => setConfirmDel(false)}>{t("common.cancel")}</button>
        </div>
      )}
      <div className="notes-body">
        <WikiEditor value={note.body_json} bodyText={note.body_text}
          onChange={(json) => { body.current = json; queueSave(); }} />
      </div>
    </>
  );
}
