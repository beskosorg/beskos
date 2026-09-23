import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { backend, colleaguesApi, type ChatChannel } from "./backend";
import { toast } from "./toast";
import { Switch } from "./Switch";
import { Avatar } from "./Avatar";
import { IcTrash, IcX } from "../panel/icons";

// The user's scheduled messages: a list with time + channel, with an option to cancel before sending.
export function ScheduledModal({ onClose }: { onClose: () => void }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["chat-scheduled"], queryFn: () => backend.chatScheduled(), retry: false });
  const cancel = useMutation({ mutationFn: (id: string) => backend.chatCancelScheduled(id), onSuccess: () => qc.invalidateQueries({ queryKey: ["chat-scheduled"] }), onError: (e) => toast((e as Error).message) });
  const items = q.data ?? [];
  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk modal-sm" onMouseDown={(e) => e.stopPropagation()}>
        <header><span>{t("chat.scheduledTitle")}</span><button className="ib" title={t("common.close")} onClick={onClose}><IcX size={15} /></button></header>
        <div className="tk-body">
          {items.length === 0 ? <p className="desc">{t("chat.scheduledEmpty")}</p> : items.map((s) => (
            <div className="sched-row" key={s.id}>
              <div className="grow">
                <div className="sr-when">#{s.channel_name} · {new Date(s.send_at).toLocaleString(i18n.language, { dateStyle: "short", timeStyle: "short" })}</div>
                <div className="sr-body">{s.body}</div>
              </div>
              <button className="ib" title={t("common.cancel")} onClick={() => cancel.mutate(s.id)}><IcTrash size={14} /></button>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

export function NewConvoModal({ mode, me, onClose, onCreated }: { mode: "channel" | "dm"; me: string; onClose: () => void; onCreated: (id: string) => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [topic, setTopic] = useState("");
  const [priv, setPriv] = useState(false);     // private (invite-only, never listed)
  const [unlisted, setUnlisted] = useState(false); // public but not listed in discovery
  const [restricted, setRestricted] = useState(false); // only managers add members
  const [picked, setPicked] = useState<{ email: string; name: string }[]>([]); // DM: 1 person = DM, 2+ = group
  const orgMembers = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false, enabled: mode === "dm" });
  const q = name.trim().toLowerCase();
  // Conversations are with ORGANIZATION MEMBERS (colleagues) — not external contacts, since they
  // cannot receive internal messages. Shows the list RIGHT AWAY (no name guessing needed) and
  // filters as you type. INCLUDES the person themself: messaging yourself is a personal notes
  // space, the same pattern Slack/WhatsApp use.
  const people = mode === "dm"
    ? (orgMembers.data ?? []).filter((c) => c.email && !picked.some((p) => p.email === c.email) && (!q || (c.name ?? "").toLowerCase().includes(q) || c.email.toLowerCase().includes(q))).slice(0, 8)
    : [];
  const addPerson = (c: { email: string; name: string }) => { setPicked((p) => [...p, { email: c.email, name: c.name || c.email }]); setName(""); };

  const create = async () => {
    try {
      if (mode === "channel") {
        const c = await backend.chatCreateChannel({
          name: name.trim(), topic: topic.trim(),
          kind: priv ? "private" : "public",
          listed: !priv && !unlisted,
          add_policy: restricted ? "restricted" : "open",
        });
        onCreated(c.id);
      }
    } catch (e) { toast((e as Error).message); }
  };
  // 1 person -> regular DM; 2+ -> group DM. Reuses the channel if it already exists (dedup by dm_key).
  const startDM = async () => {
    try {
      const emails = picked.map((p) => p.email);
      const c = emails.length <= 1 ? await backend.chatOpenDM(emails[0]) : await backend.chatOpenGroupDM(emails);
      onCreated(c.id);
    } catch (e) { toast((e as Error).message); }
  };

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 440 }}>
        <header><span>{mode === "channel" ? t("chat.newChannel") : t("chat.newDM")}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body meet-form">
          {mode === "channel" ? (
            <>
              <label>{t("chat.channelName")}<input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder={t("chat.channelNamePh")} /></label>
              <label>{t("chat.topic")}<input value={topic} onChange={(e) => setTopic(e.target.value)} placeholder={t("chat.topicPh")} /></label>
              <div className="switch-row"><span>{t("chat.private")}</span><Switch checked={priv} onChange={setPriv} label={t("chat.private")} /></div>
              {!priv && <div className="switch-row"><span>{t("chat.unlisted")}</span><Switch checked={unlisted} onChange={setUnlisted} label={t("chat.unlisted")} /></div>}
              <div className="switch-row"><span>{t("chat.restrictedAdd")}</span><Switch checked={restricted} onChange={setRestricted} label={t("chat.restrictedAdd")} /></div>
              <p className="desc">{priv ? t("chat.privateHint") : unlisted ? t("chat.unlistedHint") : t("chat.publicHint")}</p>
            </>
          ) : (
            <label>{t("chat.people")}
              {picked.length > 0 && (
                <div className="dm-chips">
                  {picked.map((p) => (
                    <span key={p.email} className="dm-chip"><Avatar name={p.name} email={p.email} size={18} className="xs" />{p.name}
                      <button className="x" title={t("common.remove")} onClick={() => setPicked((list) => list.filter((x) => x.email !== p.email))}>✕</button></span>
                  ))}
                </div>
              )}
              <div className="rcpt-in" style={{ position: "relative" }}>
                <input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder={t("chat.peoplePh")} />
                {people.length > 0 && (
                  <div className="rcpt-sugg">
                    {people.map((c) => (
                      <button key={c.id} type="button" onMouseDown={(e) => { e.preventDefault(); addPerson({ email: c.email, name: c.name }); }}>
                        <Avatar name={c.name} email={c.email} size={28} />
                        <span className="nm">{c.name || c.email}{c.email === me ? t("chat.youSuffix") : ""}</span><span className="em">{c.email}</span>
                      </button>
                    ))}
                  </div>
                )}
              </div>
              <p className="desc">{picked.length >= 2 ? t("chat.groupDMHint") : t("chat.dmHint")}</p>
            </label>
          )}
        </div>
        <footer>
          <button className="chipbtn" onClick={onClose}>{t("common.cancel")}</button>
          {mode === "channel"
            ? <button className="add" disabled={!name.trim()} onClick={create}>{t("chat.create")}</button>
            : <button className="add" disabled={picked.length === 0} onClick={startDM}>{picked.length >= 2 ? t("chat.startGroupDM") : t("chat.startDM")}</button>}
        </footer>
      </div>
    </div>
  );
}

// ForwardModal picks the destination channel to forward a message to.
// Also used by Notes to pick where to share. The title is a prop because "forward to" is not what
// someone sharing a note is doing, and a picker that says the wrong verb teaches the wrong model.
export function ForwardModal({ channels, onClose, onPick, title }: {
  channels: ChatChannel[]; onClose: () => void; onPick: (channelId: string) => void; title?: string;
}) {
  const { t } = useTranslation();
  const [q, setQ] = useState("");
  const term = q.trim().toLowerCase();
  const list = channels.filter((c) => c.name.toLowerCase().includes(term));
  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 380 }}>
        <header><span>{title ?? t("chat.forwardTo")}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body">
          <input autoFocus className="chan-filter" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("chat.searchChannel")} />
          <div className="chan-list" style={{ maxHeight: 320, overflowY: "auto", marginTop: 8 }}>
            {list.length === 0 && <div className="ann-empty" style={{ padding: 16 }}>{t("chat.noResults")}</div>}
            {list.map((c) => (
              <button key={c.id} className="fwd-target" onClick={() => onPick(c.id)}>
                <span className="hash">{c.kind === "dm" ? "@" : "#"}</span> {c.name}
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
