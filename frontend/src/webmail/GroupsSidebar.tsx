import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { contactsApi, colleaguesApi, type ContactGroup } from "./backend";
import { toast } from "./toast";
import { Avatar } from "./Avatar";
import { IcPencil, IcTrash, IcUsers, IcX } from "../panel/icons";

const PALETTE = ["#B4785F", "#B44D3A", "#3B6FB0", "#8257C7", "#2E7D5B", "#C0692F"];

// Contact GROUPS: besides organizing, this is what GRANTS ACCESS — sharing a group with a team
// makes its contacts show up for that team. Decided once per group, not contact by contact.
export function GroupsSidebar({ active, onSelect, allCount, count }: {
  active: string | null; onSelect: (id: string | null) => void;
  allCount: number; count: (id: string) => number;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [editing, setEditing] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [sharing, setSharing] = useState<ContactGroup | null>(null);
  const [name, setName] = useState("");
  const [color, setColor] = useState(PALETTE[0]);
  const q = useQuery({ queryKey: ["contact-groups"], queryFn: () => contactsApi.groups(), retry: false });
  const refresh = () => qc.invalidateQueries({ queryKey: ["contact-groups"] });

  const create = useMutation({
    mutationFn: () => contactsApi.createGroup(name.trim(), color),
    onSuccess: () => { setAdding(false); setName(""); refresh(); toast(t("contacts.groupSaved")); },
    onError: (e) => toast((e as Error).message),
  });
  const update = useMutation({
    mutationFn: (id: string) => contactsApi.updateGroup(id, name.trim(), color),
    onSuccess: () => { setEditing(null); refresh(); toast(t("contacts.groupSaved")); },
    onError: (e) => toast((e as Error).message),
  });
  const del = useMutation({
    mutationFn: (id: string) => contactsApi.deleteGroup(id),
    onSuccess: (_r, id) => { if (active === id) onSelect(null); refresh(); qc.invalidateQueries({ queryKey: ["contacts"] }); toast(t("contacts.groupDeleted")); },
    onError: (e) => toast((e as Error).message),
  });

  const start = (g: ContactGroup) => { setEditing(g.id); setName(g.name); setColor(g.color); };
  const editor = (onSave: () => void) => (
    <div>
      <div className="grp-edit">
        <input value={name} autoFocus placeholder={t("contacts.groupName")} onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && name.trim()) onSave(); if (e.key === "Escape") { setEditing(null); setAdding(false); } }} />
        <button className="chipbtn" disabled={!name.trim()} onClick={onSave}>{t("contacts.save")}</button>
      </div>
      <div className="palette">{PALETTE.map((c) => <button key={c} className={color === c ? "sel" : ""} style={{ background: c }} onClick={() => setColor(c)} />)}</div>
    </div>
  );

  return (
    // On phone (stacked) the groups bar loses its right border and caps its height so it doesn't
    // push the list down (it has its own scroll). (Tailwind max-md:*, important — tailwind.config.js.)
    <aside className="cts-side max-md:border-r-0 max-md:max-h-44">
      <div className="rlabel" style={{ margin: "0 0 8px" }}>{t("contacts.groups")}</div>

      <div className={"grp-item" + (active === null ? " on" : "")} onClick={() => onSelect(null)}>
        <span className="gn">{t("contacts.all")}</span><span className="cc">{allCount}</span>
      </div>

      {(q.data ?? []).map((g) => editing === g.id ? (
        <div key={g.id}>{editor(() => update.mutate(g.id))}</div>
      ) : (
        <div key={g.id} className={"grp-item" + (active === g.id ? " on" : "")} onClick={() => onSelect(g.id)}>
          <span className="dot" style={{ background: g.color }} />
          <span className="gn">{g.name}</span>
          <span className="cc">{count(g.id)}</span>
          <span className="acts">
            <button title={t("contacts.shareGroup")} onClick={(e) => { e.stopPropagation(); setSharing(g); }}><IcUsers size={14} /></button>
            <button title={t("contacts.rename")} onClick={(e) => { e.stopPropagation(); start(g); }}><IcPencil size={14} /></button>
            <button title={t("contacts.delete")} onClick={(e) => { e.stopPropagation(); del.mutate(g.id); }}><IcTrash size={14} /></button>
          </span>
        </div>
      ))}

      {adding ? editor(() => create.mutate())
        : <button className="newg" onClick={() => { setAdding(true); setName(""); setColor(PALETTE[0]); }}>+ {t("contacts.newGroup")}</button>}

      {sharing && <ShareModal group={sharing} onClose={() => setSharing(null)} />}
    </aside>
  );
}

// Share the group with TEAMS and/or PEOPLE. Only the group's owner can — the API rejects everyone else.
function ShareModal({ group, onClose }: { group: ContactGroup; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const teams = useQuery({ queryKey: ["share-teams"], queryFn: () => contactsApi.shareTeams(), retry: false });
  const people = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false });
  const shares = useQuery({ queryKey: ["group-shares", group.id], queryFn: () => contactsApi.shares(group.id), retry: false });
  const [selTeams, setSelTeams] = useState<string[] | null>(null);
  const [selUsers, setSelUsers] = useState<string[] | null>(null);
  const [q, setQ] = useState("");
  const chosenTeams = selTeams ?? (shares.data?.teams ?? []).map((s) => s.id);
  const chosenUsers = selUsers ?? (shares.data?.users ?? []).map((s) => s.id);
  const toggleTeam = (id: string) => setSelTeams(chosenTeams.includes(id) ? chosenTeams.filter((x) => x !== id) : [...chosenTeams, id]);
  const toggleUser = (id: string) => setSelUsers(chosenUsers.includes(id) ? chosenUsers.filter((x) => x !== id) : [...chosenUsers, id]);

  const save = useMutation({
    mutationFn: () => contactsApi.setShares(group.id, chosenTeams, chosenUsers),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["group-shares", group.id] });
      qc.invalidateQueries({ queryKey: ["contacts"] });
      toast(t("contacts.shareSaved")); onClose();
    },
    onError: (e) => toast((e as Error).message),
  });

  const term = q.trim().toLowerCase();
  const ppl = (people.data ?? []).filter((p) => !term || (p.name || "").toLowerCase().includes(term) || p.email.toLowerCase().includes(term));
  const loading = teams.isLoading || people.isLoading || shares.isLoading;

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 460 }}>
        <header><span>{t("contacts.shareGroupTitle", { name: group.name })}</span><button className="ib" title={t("common.close")} onClick={onClose}><IcX size={15} /></button></header>
        <div className="body">
          <p className="desc">{t("contacts.shareHint")}</p>
          {loading ? <div className="loading">…</div> : (
            <>
              <div className="grp-sec-label">{t("contacts.shareTeams")}</div>
              {(teams.data ?? []).length === 0 ? <p className="cd-empty">{t("crm.noUserGroups")}</p>
                : (
                  <div className="grp-picker">
                    {(teams.data ?? []).map((tm) => {
                      const on = chosenTeams.includes(tm.id);
                      return (
                        <label key={tm.id} className={"grp-chk" + (on ? " on" : "")}>
                          <input type="checkbox" checked={on} onChange={() => toggleTeam(tm.id)} />
                          <span className="av-team"><IcUsers size={14} /></span><span>{tm.name}</span>
                        </label>
                      );
                    })}
                  </div>
                )}
              <div className="grp-sec-label">{t("contacts.sharePeople")}</div>
              <input className="grp-search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("webmail.kanban.searchPeople")} />
              <div className="grp-picker">
                {ppl.length === 0 ? <p className="cd-empty">{t("webmail.kanban.noPeople")}</p>
                  : ppl.map((p) => {
                    const on = chosenUsers.includes(p.id);
                    return (
                      <label key={p.id} className={"grp-chk" + (on ? " on" : "")}>
                        <input type="checkbox" checked={on} onChange={() => toggleUser(p.id)} />
                        <Avatar name={p.name} email={p.email} userId={p.id} size={24} />
                        <span>{p.name || p.email}</span><span className="grp-em">{p.email}</span>
                      </label>
                    );
                  })}
              </div>
            </>
          )}
          <div className="drawer-actions">
            <button className="send" disabled={save.isPending} onClick={() => save.mutate()}>{t("contacts.save")}</button>
          </div>
        </div>
      </div>
    </div>
  );
}
