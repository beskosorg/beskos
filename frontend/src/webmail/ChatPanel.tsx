import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { backend, colleaguesApi, type ChatChannel, type ChatMember } from "./backend";
import { sanitizeRich } from "./mailHtml";
import { toast } from "./toast";
import { Switch } from "./Switch";
import { Avatar } from "./Avatar";
import { IcTrash, IcX, IcPaperclip, IcChevron, IcLink, IcMail, IcArchive } from "../panel/icons";

// Integrations (incoming webhooks): each one is a public URL token that turns into a bot message in
// the channel. Useful for CI/alerts/external services to post without login. Only managers create/remove them.
export function WebhooksSection({ channelId }: { channelId: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const hooksQ = useQuery({ queryKey: ["chat-webhooks", channelId], queryFn: () => backend.chatWebhooks(channelId) });
  const hooks = hooksQ.data ?? [];
  const refresh = () => qc.invalidateQueries({ queryKey: ["chat-webhooks", channelId] });
  const hookUrl = (token: string) => `${window.location.origin}/api/public/chat/hook/${token}`;
  const create = async () => {
    try { await backend.chatCreateWebhook(channelId, name.trim() || t("chat.webhookDefault")); setName(""); refresh(); }
    catch (e) { toast((e as Error).message); }
  };
  const del = async (id: string) => {
    if (!window.confirm(t("chat.webhookDeleteConfirm"))) return;
    try { await backend.chatDeleteWebhook(id); refresh(); } catch (e) { toast((e as Error).message); }
  };
  const copy = (token: string) => { navigator.clipboard?.writeText(hookUrl(token)); toast(t("chat.webhookCopied")); };
  return (
    <div className="chan-webhooks">
      <h4>{t("chat.webhooks")}</h4>
      <p className="wh-hint">{t("chat.webhooksHint")}</p>
      <div className="wh-add">
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t("chat.webhookNamePh")} onKeyDown={(e) => { if (e.key === "Enter") create(); }} />
        <button className="chipbtn" onClick={create}>{t("chat.webhookCreate")}</button>
      </div>
      {hooks.map((h) => (
        <div key={h.id} className="wh-row">
          <div className="wh-who"><b>{h.name}</b><code className="wh-url">{hookUrl(h.token)}</code></div>
          <button className="chipbtn" onClick={() => copy(h.token)}>{t("chat.webhookCopy")}</button>
          <button className="chipbtn danger" onClick={() => del(h.id)}>{t("chat.removeMember")}</button>
        </div>
      ))}
    </div>
  );
}

// Members and channel settings panel. Who can add depends on the policy (open = any
// member; restricted = managers only). Managers (owner/manager) remove, promote, and edit the settings.
export function ChannelPanel({ channel, me, onClose }: { channel: ChatChannel; me: string; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [pick, setPick] = useState("");
  const [filter, setFilter] = useState("");
  const [page, setPage] = useState(1);
  const [listed, setListed] = useState(channel.listed !== false);
  const [restricted, setRestricted] = useState(channel.add_policy === "restricted");

  // Members with search + pagination on the SERVER (handles large channels).
  const membersQ = useQuery({ queryKey: ["chat-members", channel.id, filter, page], queryFn: () => backend.chatMembersSearch(channel.id, filter, page) });
  const colleagues = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false });

  const canManage = channel.my_role === "owner" || channel.my_role === "manager";
  const canAdd = canManage || channel.add_policy !== "restricted";
  // Archiving belongs to the channel OWNER or an organization admin — the latter might not even be a
  // manager of the channel, so this section sits outside the settings block (which only the manager sees).
  const meQ = useQuery({ queryKey: ["me"], queryFn: () => backend.me(), retry: false, staleTime: Infinity });
  const orgAdmin = ["owner", "admin", "internal_admin"].includes(meQ.data?.role ?? "");
  const canArchive = channel.kind !== "dm" && !channel.is_general && (channel.my_role === "owner" || orgAdmin);
  const [confirmArch, setConfirmArch] = useState(false);
  const archive = async () => {
    try {
      await backend.chatArchiveChannel(channel.id, true);
      qc.invalidateQueries({ queryKey: ["chat-channels"] });
      qc.invalidateQueries({ queryKey: ["chat-archived"] });
      toast(t("chat.archivedDone"));
      onClose();
    } catch (e) { toast((e as Error).message); }
  };
  const members = membersQ.data?.members ?? [];
  const total = membersQ.data?.total ?? 0;
  const q = pick.trim().toLowerCase();
  // Adding is idempotent on the server (ON CONFLICT), so there is no need to exclude existing members.
  const candidates = q ? (colleagues.data ?? []).filter((c) => c.name.toLowerCase().includes(q) || c.email.toLowerCase().includes(q)).slice(0, 6) : [];
  const refresh = () => { membersQ.refetch(); qc.invalidateQueries({ queryKey: ["chat-channels"] }); };

  const add = async (email: string) => { try { await backend.chatAddMember(channel.id, email); setPick(""); refresh(); } catch (e) { toast((e as Error).message); } };
  const remove = async (userId: string) => { try { await backend.chatRemoveMember(channel.id, userId); refresh(); } catch (e) { toast((e as Error).message); } };
  const setRole = async (userId: string, role: "manager" | "member") => { try { await backend.chatSetMemberRole(channel.id, userId, role); refresh(); } catch (e) { toast((e as Error).message); } };
  const saveSettings = async () => { try { await backend.chatUpdateChannel(channel.id, { listed, add_policy: restricted ? "restricted" : "open" }); refresh(); toast(t("chat.settingsSaved")); } catch (e) { toast((e as Error).message); } };

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 460 }}>
        <header><span>#{channel.name} — {t("chat.members")}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body">
          {canAdd && (
            <div className="rcpt-in" style={{ position: "relative", marginBottom: 12 }}>
              <input autoFocus value={pick} onChange={(e) => setPick(e.target.value)} placeholder={t("chat.addMemberPh")} />
              {candidates.length > 0 && (
                <div className="rcpt-sugg">
                  {candidates.map((c) => (
                    <button key={c.id} type="button" onMouseDown={(e) => { e.preventDefault(); add(c.email); }}>
                      <Avatar name={c.name} email={c.email} size={28} />
                      <span className="nm">{c.name || c.email}{c.email === me ? t("chat.youSuffix") : ""}</span><span className="em">{c.email}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
          <div className="chan-members-head">
            <span>{t("chat.membersCount", { n: total })}</span>
            <input className="chan-filter" value={filter} onChange={(e) => { setPage(1); setFilter(e.target.value); }} placeholder={t("chat.searchMembers")} />
          </div>
          <div className="chan-members">
            {members.length === 0 && <div className="empty" style={{ padding: 18 }}>{t("chat.noMembersFound")}</div>}
            {members.map((m) => (
              <div key={m.user_id} className="chan-member">
                <Avatar name={m.name} email={m.email} userId={m.user_id} size={28} />
                <div className="who"><b>{m.name || m.email}</b><small>{m.email}</small></div>
                {m.role !== "member" && <span className="role-badge">{t(m.role === "owner" ? "chat.roleOwner" : "chat.roleManager")}</span>}
                {canManage && m.role !== "owner" && m.email !== me && (
                  <div className="member-actions">
                    <button className="chipbtn" onClick={() => setRole(m.user_id, m.role === "manager" ? "member" : "manager")}>
                      {t(m.role === "manager" ? "chat.demote" : "chat.promote")}
                    </button>
                    <button className="chipbtn danger" onClick={() => remove(m.user_id)}>{t("chat.removeMember")}</button>
                  </div>
                )}
              </div>
            ))}
          </div>
          {total > 20 && (
            <div className="ann-pager">
              <button className="ib" title={t("common.prevPage")} disabled={page <= 1} onClick={() => setPage((p) => p - 1)}><IcChevron size={16} className="left" /></button>
              <span>{t("announce.pageOf", { page, pages: Math.ceil(total / 20) })}</span>
              <button className="ib" title={t("common.nextPage")} disabled={page * 20 >= total} onClick={() => setPage((p) => p + 1)}><IcChevron size={16} className="right" /></button>
            </div>
          )}
          {canArchive && (
            <div className="chan-settings">
              <h4>{t("chat.archiveChannel")}</h4>
              <p className="wh-hint">{t("chat.archiveHint")}</p>
              {confirmArch ? (
                /* Inline confirmation (the project does not use native dialogs): question and action side by side. */
                <div className="chan-arch-confirm">
                  <span>{t("chat.archiveConfirm", { name: channel.name })}</span>
                  <button className="chipbtn" onClick={() => setConfirmArch(false)}>{t("common.cancel")}</button>
                  <button className="chipbtn danger" onClick={archive}>{t("chat.archiveDo")}</button>
                </div>
              ) : (
                <button className="chipbtn" onClick={() => setConfirmArch(true)}><IcArchive size={14} /> {t("chat.archiveChannel")}</button>
              )}
            </div>
          )}
          {canManage && (
            <div className="chan-settings">
              <h4>{t("chat.channelSettings")}</h4>
              {channel.kind === "public" && <div className="switch-row full"><span>{t("chat.unlisted")}</span><Switch checked={!listed} onChange={(on) => setListed(!on)} label={t("chat.unlisted")} /></div>}
              <div className="switch-row full"><span>{t("chat.restrictedAdd")}</span><Switch checked={restricted} onChange={setRestricted} label={t("chat.restrictedAdd")} /></div>
              <button className="add" onClick={saveSettings}>{t("chat.saveSettings")}</button>
              {channel.kind !== "dm" && <WebhooksSection channelId={channel.id} />}
              {channel.my_role === "owner" && channel.kind !== "dm" && (
                <button className="chan-delete" onClick={async () => {
                  if (!window.confirm(t("chat.deleteChannelConfirm", { name: channel.name }))) return;
                  try { await backend.chatDeleteChannel(channel.id); qc.invalidateQueries({ queryKey: ["chat-channels"] }); onClose(); }
                  catch (e) { toast((e as Error).message); }
                }}><IcTrash size={14} /> {t("chat.deleteChannel")}</button>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// --- Channel panel tabs ---

// Canvas: the channel's shared rich document. Any member edits and saves it.
export function CanvasTab({ channelId }: { channelId: string }) {
  const { t } = useTranslation();
  const ed = useRef<HTMLDivElement>(null);
  const [saving, setSaving] = useState(false);
  const canvasQ = useQuery({ queryKey: ["chat-canvas", channelId], queryFn: () => backend.chatCanvas(channelId) });
  // Canvas is HTML saved by any channel member. ALWAYS sanitize before injecting via
  // innerHTML (on read) and before persisting (defense in depth) — otherwise it is stored XSS.
  useEffect(() => { if (ed.current && canvasQ.data) ed.current.innerHTML = sanitizeRich(canvasQ.data.canvas); }, [canvasQ.data]);
  const exec = (e: React.MouseEvent, cmd: string) => { e.preventDefault(); document.execCommand(cmd); ed.current?.focus(); };
  const save = async () => { setSaving(true); try { await backend.chatSaveCanvas(channelId, sanitizeRich(ed.current?.innerHTML ?? "")); toast(t("chat.canvasSaved")); } catch (e) { toast((e as Error).message); } finally { setSaving(false); } };
  const clear = async () => {
    if (!window.confirm(t("chat.canvasClearConfirm"))) return;
    if (ed.current) ed.current.innerHTML = "";
    try { await backend.chatSaveCanvas(channelId, ""); toast(t("chat.canvasCleared")); } catch (e) { toast((e as Error).message); }
  };
  if (canvasQ.isLoading) return <div className="loading">…</div>;
  return (
    <div>
      <div className="tools sig-tools">
        <button title="Bold" onMouseDown={(e) => exec(e, "bold")}><b>B</b></button>
        <button title="Italic" onMouseDown={(e) => exec(e, "italic")}><i>I</i></button>
        <button title="List" onMouseDown={(e) => exec(e, "insertUnorderedList")}>☰</button>
        <button title="Heading" onMouseDown={(e) => { e.preventDefault(); document.execCommand("formatBlock", false, "h3"); }}>H</button>
        <span className="grow" />
        <button className="chipbtn danger" onClick={clear}>{t("chat.canvasClear")}</button>
        <button className="add" disabled={saving} onClick={save}>{saving ? "…" : t("chat.saveCanvas")}</button>
      </div>
      <div className="sig-surface" style={{ minHeight: 300 }} ref={ed} contentEditable suppressContentEditableWarning data-ph={t("chat.canvasPh")} />
    </div>
  );
}

// Pins: the channel's pinned messages.
export function PinsTab({ channelId, onJump }: { channelId: string; onJump?: (msgId: string) => void }) {
  const { t, i18n } = useTranslation();
  const pinsQ = useQuery({ queryKey: ["chat-pins", channelId], queryFn: () => backend.chatPins(channelId) });
  const unpin = async (id: string) => { try { await backend.chatPin(id, false); pinsQ.refetch(); } catch (e) { toast((e as Error).message); } };
  const pins = pinsQ.data?.pins ?? [];
  return (
    <div>
      {pins.length === 0 ? <div className="ann-empty">{t("chat.noPins")}</div> : (
        <div className="chan-list">
          {pins.map((p) => (
            <div key={p.id} className="chan-listitem">
              <button className="cl-jump" style={{ flex: 1, minWidth: 0, textAlign: "left", background: "none", border: 0, cursor: "pointer" }} onClick={() => onJump?.(p.id)} title={t("chat.jumpToMessage")}>
                <div className="cl-body">{p.body}</div>
                <div className="cl-meta">{p.author} · {new Date(p.created_at).toLocaleDateString(i18n.language, { day: "2-digit", month: "short" })}</div>
              </button>
              <button className="chipbtn" onClick={() => void unpin(p.id)}>{t("chat.unpin")}</button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// Channel files & links (curated collection). Any member adds and removes.
export function FilesLinksTab({ channelId }: { channelId: string }) {
  const { t } = useTranslation();
  const [uploading, setUploading] = useState(false);
  const fileIn = useRef<HTMLInputElement>(null);
  const bmQ = useQuery({ queryKey: ["chat-bookmarks", channelId], queryFn: () => backend.chatBookmarks(channelId) });
  const items = bmQ.data?.bookmarks ?? [];
  const files = items.filter((b) => b.kind === "file");
  const links = items.filter((b) => b.kind === "link");
  const fmtSize = (n: number) => n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`;

  const addLink = async () => {
    const url = window.prompt(t("chat.addLinkPrompt"), "https://");
    if (!url) return;
    const title = window.prompt(t("chat.addLinkTitle"), "") ?? "";
    try { await backend.chatAddBookmark(channelId, { kind: "link", url, title }); bmQ.refetch(); } catch (e) { toast((e as Error).message); }
  };
  const onFile = async (f?: File) => {
    if (!f) return;
    setUploading(true);
    try {
      const rec = await backend.uploadFile(f);
      await backend.chatAddBookmark(channelId, { kind: "file", file_id: rec.id, file_name: f.name, file_size: f.size });
      bmQ.refetch();
    } catch (e) { toast((e as Error).message); }
    finally { setUploading(false); }
  };
  const remove = async (bid: string) => { try { await backend.chatDeleteBookmark(channelId, bid); bmQ.refetch(); } catch (e) { toast((e as Error).message); } };

  return (
    <div>
      <div className="tabpane-actions">
        <button className="chipbtn" onClick={addLink}><IcLink size={14} /> {t("chat.addLink")}</button>
        <button className="chipbtn" disabled={uploading} onClick={() => fileIn.current?.click()}><IcPaperclip size={14} /> {uploading ? "…" : t("chat.addFile")}</button>
        <input ref={fileIn} type="file" hidden onChange={(e) => { void onFile(e.target.files?.[0]); e.target.value = ""; }} />
      </div>
      <h4 style={{ margin: "4px 0 8px" }}>{t("chat.files")} <span className="n">{files.length}</span></h4>
      {files.length === 0 ? <div className="ann-empty" style={{ marginBottom: 14 }}>{t("chat.noFiles")}</div> : (
        <div className="chan-list" style={{ marginBottom: 16 }}>
          {files.map((f) => (
            <div key={f.id} className="chan-listitem">
              <IcPaperclip size={15} />
              <a href={backend.bookmarkDownloadUrl(channelId, f.id)} style={{ flex: 1, minWidth: 0, color: "inherit", textDecoration: "none" }}>
                <div className="cl-body">{f.file_name}</div><div className="cl-meta">{fmtSize(f.file_size)}</div>
              </a>
              <button className="ib" title={t("chat.removeBookmark")} onClick={() => void remove(f.id)}><IcTrash size={14} /></button>
            </div>
          ))}
        </div>
      )}
      <h4 style={{ margin: "0 0 8px" }}>{t("chat.links")} <span className="n">{links.length}</span></h4>
      {links.length === 0 ? <div className="ann-empty">{t("chat.noLinks")}</div> : (
        <div className="chan-list">
          {links.map((l) => (
            <div key={l.id} className="chan-listitem">
              <IcLink size={15} />
              <a href={l.url} target="_blank" rel="noopener noreferrer nofollow" style={{ flex: 1, minWidth: 0, color: "inherit", textDecoration: "none" }}>
                <div className="cl-body">{l.title || l.url}</div>{l.title && <div className="cl-meta">{l.url}</div>}
              </a>
              <button className="ib" title={t("chat.removeBookmark")} onClick={() => void remove(l.id)}><IcTrash size={14} /></button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// Member profile card (on clicking the name/avatar): details + buttons.
export function MemberProfile({ member, me, onClose, onOpenDM }: { member: ChatMember; me: string; onClose: () => void; onOpenDM: (email: string) => void }) {
  const { t } = useTranslation();
  const isMe = member.email === me;
  const statusQ = useQuery({ queryKey: ["user-status", member.email], queryFn: () => backend.userStatus(member.email), retry: false, enabled: !member.status_emoji && !member.status_text });
  const stEmoji = member.status_emoji || statusQ.data?.emoji || "";
  const stText = member.status_text || statusQ.data?.text || "";
  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 340 }}>
        <div className="member-profile">
          <button className="ib prof-x" title={t("common.close")} onClick={onClose}><IcX size={15} /></button>
          <Avatar name={member.name} email={member.email} userId={member.user_id} size={48} className="prof-av" />
          <div className="prof-name">{member.name || member.email}</div>
          {(stEmoji || stText) && <div className="prof-status">{stEmoji} {stText}</div>}
          <div className="prof-email">{member.email}</div>
          {member.role && member.role !== "member" && <span className="role-badge" style={{ marginTop: 6 }}>{t(member.role === "owner" ? "chat.roleOwner" : "chat.roleManager")}</span>}
          {!isMe && (
            <div className="prof-actions">
              <button className="add" onClick={() => onOpenDM(member.email)}><IcMail size={15} /> {t("chat.message")}</button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
