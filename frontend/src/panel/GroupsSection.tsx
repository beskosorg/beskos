import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, hasPerm, type Me, type Group } from "../api";
import { Modal, toast } from "./ui";
import { IcPlus, IcTrash, IcUsers, IcX } from "./icons";

// User groups. Creating a group automatically creates a private chat channel, and the
// membership stays in sync (joining the group joins the channel; leaving leaves it).
export function GroupsSection({ me }: { me: Me | undefined }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const canManage = hasPerm(me, "members.manage");
  const groups = useQuery({ queryKey: ["groups"], queryFn: api.groups, retry: false });
  const [adding, setAdding] = useState(false);
  const [open, setOpen] = useState<Group | null>(null);

  const refresh = () => qc.invalidateQueries({ queryKey: ["groups"] });
  const del = useMutation({
    mutationFn: (id: string) => api.deleteGroup(id),
    onSuccess: () => { refresh(); toast(t("groups.deleted")); },
    onError: (e) => toast((e as Error).message),
  });

  const list = groups.data ?? [];
  return (
    <>
      <div className="pnl-top">
        <div><h1>{t("groups.title")}</h1><div className="desc">{t("groups.subtitle")}</div></div>
        <div className="grow" />
        {canManage && <button className="pnl-btn primary" onClick={() => setAdding(true)}><IcPlus size={16} />{t("groups.new")}</button>}
      </div>
      <div className="pnl-content">
        {groups.isError && <p className="desc">{(groups.error as Error).message}</p>}
        <div className="pnl-card"><div className="pnl-card-b">
          {list.length === 0 ? <div className="pnl-empty">{t("groups.empty")}</div> : list.map((g) => (
            <div className="pnl-row" key={g.id}>
              <div className="pnl-avatar" style={{ width: 32, height: 32, fontSize: 12 }}>{g.name.slice(0, 2).toUpperCase()}</div>
              <div className="grow">
                <div>{g.name}</div>
                {g.channel_id && <div className="sub">#{g.name} · {t("groups.hasChannel")}</div>}
              </div>
              <span className="pnl-pill muted"><IcUsers size={13} /> {g.members}</span>
              <button className="pnl-btn" onClick={() => setOpen(g)}>{t("groups.members")}</button>
              {canManage && <button className="pnl-icon-btn" title={t("groups.delete")} onClick={() => del.mutate(g.id)}><IcTrash size={16} /></button>}
            </div>
          ))}
        </div></div>
      </div>

      {adding && <AddGroupModal onClose={() => setAdding(false)} onDone={() => { setAdding(false); refresh(); toast(t("groups.created")); }} />}
      {open && <MembersModal g={open} canManage={canManage} onClose={() => { setOpen(null); refresh(); }} />}
    </>
  );
}

function AddGroupModal({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const m = useMutation({ mutationFn: () => api.createGroup(name.trim()), onSuccess: onDone, onError: (e) => toast((e as Error).message) });
  return (
    <Modal title={t("groups.new")} onClose={onClose}
      footer={<><button className="pnl-btn ghost" onClick={onClose}>{t("common.cancel")}</button>
        <button className="pnl-btn primary" disabled={!name.trim() || m.isPending} onClick={() => m.mutate()}>{t("groups.create")}</button></>}>
      <div className="pnl-field"><label>{t("groups.name")}</label>
        <input className="pnl-input" autoFocus value={name} placeholder={t("groups.namePh")}
          onChange={(e) => setName(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter" && name.trim()) m.mutate(); }} />
      </div>
      <p className="desc">{t("groups.createHint")}</p>
    </Modal>
  );
}

// Group members: adding/removing is reflected in the chat channel automatically.
function MembersModal({ g, canManage, onClose }: { g: Group; canManage: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const members = useQuery({ queryKey: ["group-members", g.id], queryFn: () => api.groupMembers(g.id) });
  const all = useQuery({ queryKey: ["members"], queryFn: api.members, retry: false });
  const refresh = () => qc.invalidateQueries({ queryKey: ["group-members", g.id] });
  const add = useMutation({ mutationFn: (id: string) => api.addGroupMember(g.id, id), onSuccess: refresh, onError: (e) => toast((e as Error).message) });
  const rm = useMutation({ mutationFn: (id: string) => api.removeGroupMember(g.id, id), onSuccess: refresh, onError: (e) => toast((e as Error).message) });

  const inGroup = new Set((members.data ?? []).map((m) => m.id));
  const candidates = (all.data ?? []).filter((m) => !inGroup.has(m.id));

  return (
    <Modal title={g.name} onClose={onClose}>
      <p className="desc" style={{ marginTop: 0 }}>{t("groups.syncHint")}</p>
      {(members.data ?? []).length === 0 ? <div className="pnl-empty">{t("groups.noMembers")}</div>
        : (members.data ?? []).map((m) => (
          <div className="pnl-row" key={m.id}>
            <div className="pnl-avatar" style={{ width: 30, height: 30, fontSize: 11 }}>{(m.name || m.email).slice(0, 2).toUpperCase()}</div>
            <div className="grow"><div>{m.name || m.email}</div><div className="sub mono">{m.email}</div></div>
            {canManage && <button className="pnl-icon-btn" title={t("groups.removeMember")} onClick={() => rm.mutate(m.id)}><IcX size={15} /></button>}
          </div>
        ))}
      {canManage && candidates.length > 0 && (
        <div className="pnl-field" style={{ marginTop: 14 }}><label>{t("groups.addMember")}</label>
          <select className="pnl-select" defaultValue="" onChange={(e) => { if (e.target.value) { add.mutate(e.target.value); e.currentTarget.value = ""; } }}>
            <option value="">—</option>
            {candidates.map((m) => <option key={m.id} value={m.id}>{m.name || m.email}</option>)}
          </select>
        </div>
      )}
    </Modal>
  );
}
