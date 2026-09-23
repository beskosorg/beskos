import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { backend, colleaguesApi, contactsApi, type StorageFile, type Share } from "./backend";
import { useUserSettings } from "./useSettings";
import { toast } from "./toast";
import { hueFor } from "./util";
import { Avatar } from "./Avatar";
import { IcTrash, IcLink, IcSearch } from "../panel/icons";
import { publicUrl } from "../apiBase";

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

// People search for sharing: colleagues + contacts while typing, and accepts a freely
// typed email (InviteePicker pattern). Picking/confirming calls onAdd with the email.
function PeopleSearch({ onAdd, disabled }: { onAdd: (email: string) => void; disabled?: boolean }) {
  const { t } = useTranslation();
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const colleagues = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false });
  const contacts = useQuery({ queryKey: ["contacts"], queryFn: () => contactsApi.list(), retry: false });
  const term = q.trim().toLowerCase();
  const isEmail = EMAIL_RE.test(q.trim());

  const hits = useMemo(() => {
    if (!term) return [] as { email: string; name?: string; kind: "colleague" | "contact" }[];
    const out: { email: string; name?: string; kind: "colleague" | "contact" }[] = [];
    for (const c of colleagues.data ?? []) {
      if (c.email.toLowerCase().includes(term) || (c.name ?? "").toLowerCase().includes(term)) out.push({ email: c.email, name: c.name, kind: "colleague" });
    }
    for (const c of contacts.data ?? []) {
      const e = (c.email ?? "").toLowerCase();
      if (!e || out.some((o) => o.email.toLowerCase() === e)) continue;
      if (e.includes(term) || (c.name ?? "").toLowerCase().includes(term)) out.push({ email: c.email!, name: c.name, kind: "contact" });
    }
    return out.slice(0, 6);
  }, [term, colleagues.data, contacts.data]);

  const pick = (email: string) => { onAdd(email); setQ(""); setOpen(false); inputRef.current?.focus(); };

  return (
    <div className="invp share-people">
      <div className="invp-search">
        <IcSearch size={14} />
        <input ref={inputRef} value={q} autoFocus disabled={disabled} placeholder={t("files.addPeople")}
          onChange={(e) => { setQ(e.target.value); setOpen(true); }}
          onFocus={() => setOpen(true)}
          onBlur={() => setTimeout(() => setOpen(false), 150)}
          onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); if (hits[0]) pick(hits[0].email); else if (isEmail) pick(q.trim()); } }} />
      </div>
      {open && (hits.length > 0 || isEmail) && (
        <div className="invp-sugg">
          {hits.map((h) => (
            <button key={h.email} type="button" onMouseDown={(e) => { e.preventDefault(); pick(h.email); }}>
              <Avatar name={h.name} email={h.email} size={26} />
              <span className="cinfo"><b>{h.name || h.email}</b><i>{h.email}</i></span>
              <span className="src">{t(h.kind === "colleague" ? "invp.colleague" : "invp.contact")}</span>
            </button>
          ))}
          {isEmail && !hits.some((h) => h.email.toLowerCase() === q.trim().toLowerCase()) && (
            <button type="button" className="invp-ext" onMouseDown={(e) => { e.preventDefault(); pick(q.trim()); }}>
              <span className="av" style={{ background: hueFor(q.trim()) }}>@</span>
              <span className="cinfo"><b>{t("invp.inviteExternal")}</b><i>{q.trim()}</i></span>
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// Public URL (NO login) for a link token: opens the file (OnlyOffice) on the public /p/ page.
const publicLinkUrl = (token: string) => publicUrl(`/p/${token}`);

// Sentinel principal for ORGANIZATION-LEVEL sharing (must match the backend). A general
// access scope, not a person.
const ORG = "@org";
type Scope = "restricted" | "org" | "public";

// Google Drive-style sharing: add people with a role + link access.
export function ShareModal({ file, onClose }: { file: StorageFile; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const meQ = useQuery({ queryKey: ["me"], queryFn: () => backend.me(), retry: false });
  const sharesQ = useQuery({ queryKey: ["shares", file.id], queryFn: () => backend.shares(file.id), retry: false });
  const [role, setRole] = useState("leitor");
  const [groupId, setGroupId] = useState("");
  const refresh = () => qc.invalidateQueries({ queryKey: ["shares", file.id] });
  // Changing the permission of someone who already has access: it's the same share request (the server
  // upserts by file+person), so it doesn't duplicate or trigger an email to someone already notified.
  const setPerm = useMutation({
    mutationFn: (v: { principal: string; level: string }) => backend.shareFile(file.id, v.principal, v.level),
    onSuccess: () => { refresh(); toast(t("files.permChanged")); },
    onError: (e) => toast((e as Error).message),
  });

  // User's teams: share with the whole group (expands into emails in the backend).
  const myGroupsQ = useQuery({ queryKey: ["my-groups"], queryFn: () => backend.myGroups(), retry: false });
  const myGroups = myGroupsQ.data ?? [];
  const shareGroup = useMutation({
    mutationFn: () => backend.shareWithGroup(file.id, groupId, role),
    onSuccess: (r) => { setGroupId(""); refresh(); toast(t("files.sharedWithTeam", { count: r.shared })); },
    onError: (e) => toast((e as Error).message),
  });

  const shares = sharesQ.data ?? [];
  // Organization share = sentinel principal "@org"; it's not a person, doesn't go in the list.
  const orgShare = shares.find((s) => s.principal === ORG);
  const people = shares.filter((s) => s.principal !== ORG && s.level !== "link");

  // Sharing with someone OUTSIDE: there's no account to notify, so a link goes out by email. If
  // sending is disabled on the server, we say so — reporting as done what didn't go out is a lie.
  const add = useMutation({
    mutationFn: (email: string) => backend.shareFile(file.id, email, role),
    onSuccess: (r) => {
      refresh();
      if (r.external && !r.emailed) toast(t("files.sharedExternalNoEmail"));
      else if (r.external) toast(t("files.sharedExternal"));
      else toast(t("files.shared"));
    },
    onError: (e) => toast((e as Error).message),
  });
  const remove = useMutation({ mutationFn: (s: Share) => backend.removeShare(file.id, s.id), onSuccess: refresh });

  // PUBLIC link state (no login): opens in OnlyOffice, with permission + password + expiry + download.
  const pubLinkQ = useQuery({ queryKey: ["file-link", file.id], queryFn: () => backend.getFileLink(file.id), retry: false });
  const pubToken = pubLinkQ.data?.token ?? "";
  const refreshPub = () => qc.invalidateQueries({ queryKey: ["file-link", file.id] });
  const refreshAll = () => { refresh(); refreshPub(); };
  const settings = useUserSettings(); // link's default expiry (settings.files.linkExpiryDays)
  const [expDays, setExpDays] = useState(settings.files?.linkExpiryDays ?? 30);
  const [pw, setPw] = useState("");
  const [linkPerm, setLinkPerm] = useState<"view" | "edit">("view");
  const [allowDl, setAllowDl] = useState(true);
  // dirty = there are changes to the link controls still unsaved (only take effect on clicking Apply).
  const [dirty, setDirty] = useState(false);
  useEffect(() => {
    const d = pubLinkQ.data;
    if (d?.token) { setLinkPerm(d.permission ?? "view"); setAllowDl(d.allow_download ?? true); setDirty(false); }
  }, [pubLinkQ.data]);
  const applyPub = (opts?: { expires_days?: number; password?: string; permission?: "view" | "edit"; allow_download?: boolean }) =>
    backend.fileLink(file.id, { expires_days: expDays, password: pw, permission: linkPerm, allow_download: allowDl, ...opts });
  const createPub = useMutation({ mutationFn: () => applyPub(), onSuccess: () => { refreshPub(); setPw(""); setDirty(false); toast(t("files.linkSaved")); }, onError: (e) => toast((e as Error).message) });

  // "General access" scope: one at a time (Google style). Picking one clears the others.
  const scope: Scope = pubToken ? "public" : orgShare ? "org" : "restricted";
  const orgPerm: "view" | "edit" = orgShare?.level === "editor" ? "edit" : "view";
  const setScope = useMutation({
    mutationFn: async (next: Scope) => {
      if (next !== "public" && pubToken) await backend.revokeFileLink(file.id);
      if (next !== "org" && orgShare) await backend.removeShare(file.id, orgShare.id);
      if (next === "org" && !orgShare) await backend.shareFile(file.id, ORG, "leitor");
      if (next === "public" && !pubToken) await applyPub();
    },
    onSuccess: refreshAll,
    onError: (e) => toast((e as Error).message),
  });
  // Organization permission (view/edit): recreates the share with the new level.
  const setOrgPerm = useMutation({
    mutationFn: async (perm: "view" | "edit") => {
      if (orgShare) await backend.removeShare(file.id, orgShare.id);
      await backend.shareFile(file.id, ORG, perm === "edit" ? "editor" : "leitor");
    },
    onSuccess: refresh,
    onError: (e) => toast((e as Error).message),
  });

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 500 }}>
        <header><span>{t("files.shareTitle", { name: file.name })}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body">
          <div className="share-add">
            <PeopleSearch disabled={add.isPending} onAdd={(email) => add.mutate(email)} />
            <select value={role} onChange={(e) => setRole(e.target.value)}>
              <option value="leitor">{t("files.roleViewer")}</option>
              <option value="editor">{t("files.roleEditor")}</option>
            </select>
          </div>
          <p className="share-hint">{t("files.addPeopleHint")}</p>

          {myGroups.length > 0 && (
            <div className="share-add">
              <select className="grow" value={groupId} onChange={(e) => setGroupId(e.target.value)}>
                <option value="">{t("files.shareWithTeamPlaceholder")}</option>
                {myGroups.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
              </select>
              <button className="add" disabled={!groupId || shareGroup.isPending} onClick={() => shareGroup.mutate()}>{t("files.shareTeamBtn")}</button>
            </div>
          )}

          <p className="dv-sec" style={{ margin: "6px 0" }}>{t("files.peopleAccess")}</p>
          <div className="share-row">
            <Avatar name={t("files.you")} email={meQ.data?.email} size={30} />
            <div className="grow"><div className="who">{t("files.you")}</div></div>
            <span className="tx" style={{ color: "var(--faint)", fontSize: 12 }}>{t("files.owner")}</span>
          </div>
          {people.map((s) => (
            <div className="share-row" key={s.id}>
              <Avatar email={s.principal} name={s.principal} size={30} />
              <div className="grow"><div className="who">{s.principal}</div></div>
              {/* The permission is EDITABLE: switching from viewer to editor used to require removing and
                  re-sharing, and doing that with someone outside would send them a second email. */}
              <select className="share-perm" value={s.level === "editor" ? "editor" : "leitor"}
                disabled={setPerm.isPending}
                onChange={(e) => setPerm.mutate({ principal: s.principal, level: e.target.value })}>
                <option value="leitor">{t("files.roleViewer")}</option>
                <option value="editor">{t("files.roleEditor")}</option>
              </select>
              <button className="ib" title={t("files.removeShare")} onClick={() => remove.mutate(s)}><IcTrash size={15} /></button>
            </div>
          ))}

          {/* GENERAL ACCESS — a single selector (Google style): Restricted · Organization · Public link. */}
          <p className="dv-sec" style={{ margin: "16px 0 6px" }}>{t("files.generalAccess")}</p>
          <div className="share-general">
            <span className={"ic" + (scope === "restricted" ? "" : " on")}><IcLink size={20} /></span>
            <div className="grow">
              <select className="scope-sel" value={scope} disabled={setScope.isPending}
                onChange={(e) => setScope.mutate(e.target.value as Scope)}>
                <option value="restricted">{t("files.scopeRestricted")}</option>
                <option value="org">{t("files.scopeOrg")}</option>
                <option value="public">{t("files.scopePublic")}</option>
              </select>
              <div className="tx">{t(`files.scopeHint_${scope}`)}</div>
            </div>
            {scope === "org" && (
              <label className="field-sm">
                <span>{t("files.linkPermission")}</span>
                <select value={orgPerm} disabled={setOrgPerm.isPending} onChange={(e) => setOrgPerm.mutate(e.target.value as "view" | "edit")}>
                  <option value="view">{t("files.roleViewer")}</option>
                  <option value="edit">{t("files.roleEditor")}</option>
                </select>
              </label>
            )}
          </div>

          {scope === "org" && (
            <div className="link-box">
              <input readOnly value={publicUrl(`/mail/files/edit/${file.id}`)} onFocus={(e) => e.target.select()} />
              <button className="chipbtn" onClick={() => { navigator.clipboard?.writeText(publicUrl(`/mail/files/edit/${file.id}`)); toast(t("files.linkCopied")); }}>{t("files.copyLink")}</button>
            </div>
          )}

          {scope === "public" && (
            <>
              <div className="link-cfg">
                <label className="field-sm">
                  <span>{t("files.linkPermission")}</span>
                  <select value={linkPerm} onChange={(e) => { setLinkPerm(e.target.value as "view" | "edit"); setDirty(true); }}>
                    <option value="view">{t("files.roleViewer")}</option>
                    <option value="edit">{t("files.roleEditor")}</option>
                  </select>
                </label>
                <label className="field-sm">
                  <span>{t("files.linkExpiry")}</span>
                  <select value={expDays} onChange={(e) => { setExpDays(Number(e.target.value)); setDirty(true); }}>
                    <option value={0}>{t("files.linkNoExpiry")}</option>
                    <option value={1}>{t("files.link1d")}</option>
                    <option value={7}>{t("files.link7d")}</option>
                    <option value={30}>{t("files.link30d")}</option>
                    <option value={90}>{t("files.link90d")}</option>
                  </select>
                </label>
                <label className="field-sm">
                  <span>{t("files.linkPasswordLabel")}</span>
                  <input type="password" placeholder={pubLinkQ.data?.has_password ? t("files.linkPasswordSet") : t("files.linkPassword")} value={pw} onChange={(e) => { setPw(e.target.value); setDirty(true); }} />
                </label>
              </div>
              <label className="share-dl">
                <span className="switch"><input type="checkbox" checked={allowDl} onChange={(e) => { setAllowDl(e.target.checked); setDirty(true); }} /><span className="track" /></span>
                <span>{t("files.linkAllowDownload")}</span>
              </label>
              <div className="link-apply">
                <button className={"add grow" + (dirty ? "" : " ghost")} disabled={createPub.isPending || !dirty} onClick={() => createPub.mutate()}>{t("files.linkApply")}</button>
                <span className={"save-hint" + (dirty ? " dirty" : "")}>{dirty ? t("files.linkUnsaved") : t("files.linkSavedShort")}</span>
              </div>
              <div className="link-box">
                <input readOnly value={publicLinkUrl(pubToken)} onFocus={(e) => e.target.select()} />
                <button className="chipbtn" onClick={() => { navigator.clipboard?.writeText(publicLinkUrl(pubToken)); toast(t("files.linkCopied")); }}>{t("files.copyLink")}</button>
              </div>
            </>
          )}
        </div>
        <footer><button className="add" onClick={onClose}>{t("common.close")}</button></footer>
      </div>
    </div>
  );
}
