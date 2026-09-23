import { hostedBilling } from "../edition";
import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, hasPerm, type Me, type Mailbox } from "../api";
import { Modal, toast } from "./ui";
import { PasswordField } from "../webmail/PasswordField";
import { GroupPicker } from "./GroupPicker";
import { IcPlus, IcKey, IcTrash, IcColumns, IcShield } from "./icons";
import { MailboxActions, MailboxStatusPill } from "./MailboxLifecycle";

const ROLES = ["owner", "admin", "member", "billing"];

// Users = members (roles) + mailbox. Users and Members are the same thing; they differ by role.
export function UsersSection({ me }: { me: Me | undefined }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const canManage = hasPerm(me, "members.manage");
  const canDeleteMailbox = hasPerm(me, "mailboxes.delete");
  const members = useQuery({ queryKey: ["members"], queryFn: api.members, retry: false });
  // Key "admin-mailboxes": do NOT use ["mailboxes"] — that one is for the webmail's JMAP folders.
  const mailboxes = useQuery({ queryKey: ["admin-mailboxes"], queryFn: api.mailboxes, retry: false });
  const domains = useQuery({ queryKey: ["domains"], queryFn: api.domains, retry: false });
  const activeDomains = (domains.data ?? []).filter((d) => d.provision_state === "active");

  const [adding, setAdding] = useState(false);
  const [reset, setReset] = useState<string | null>(null);
  const [groupsOf, setGroupsOf] = useState<{ id: string; label: string } | null>(null);

  // Merges members (panel accounts, with a role) + mailboxes by email: a user can have a panel
  // account, a mailbox, or both. This way ALL of them show up (bug: mailboxes without an account used to vanish).
  const users = useMemo(() => {
    const byEmail = new Map<string, { email: string; role: string; panelId?: string; mailboxId?: string; mailbox?: Mailbox }>();
    for (const m of members.data ?? []) byEmail.set(m.email, { email: m.email, role: m.role, panelId: m.id });
    for (const mb of mailboxes.data ?? []) {
      const e = byEmail.get(mb.email) ?? { email: mb.email, role: "member" };
      e.mailboxId = mb.id;
      e.mailbox = mb;
      byEmail.set(mb.email, e);
    }
    return [...byEmail.values()].sort((a, b) => a.email.localeCompare(b.email));
  }, [members.data, mailboxes.data]);

  const changeRole = useMutation({
    mutationFn: ({ id, role }: { id: string; role: string }) => api.changeRole(id, role),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["members"] }), onError: (e) => toast((e as Error).message),
  });
  const [permsOf, setPermsOf] = useState<{ id: string; label: string } | null>(null);
  const remove = useMutation({
    mutationFn: (id: string) => api.removeMember(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["members"] }); toast(t("members.removed")); }, onError: (e) => toast((e as Error).message),
  });

  return (
    <>
      <div className="pnl-top">
        <div><h1>{t("panel.users")}</h1><div className="desc">{t("usersSection.desc")}</div></div>
        <div className="grow" />
        {canManage && <button className="pnl-btn primary" disabled={activeDomains.length === 0} onClick={() => setAdding(true)}><IcPlus size={16} />{t("usersSection.add")}</button>}
      </div>
      <div className="pnl-content">
        {activeDomains.length === 0 && <p className="desc" style={{ marginBottom: 12 }}>{t("usersSection.needDomain")}</p>}
        <div className="pnl-card"><div className="pnl-card-b">
          {users.length === 0 ? <div className="pnl-empty">{t("mailboxes.empty")}</div> : users.map((u) => (
            <div className="pnl-row" key={u.email}>
              <div className="pnl-avatar" style={{ width: 32, height: 32, fontSize: 12 }}>{u.email.slice(0, 2).toUpperCase()}</div>
              <div className="grow">
                <div className="mono" style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  {u.email}{u.mailbox && <MailboxStatusPill mb={u.mailbox} />}
                </div>
                {!u.mailboxId && <div className="sub">{t("usersSection.noMailbox")}</div>}</div>
              {canManage && u.panelId
                ? <select className="pnl-select" style={{ width: 150 }} value={u.role} onChange={(e) => changeRole.mutate({ id: u.panelId!, role: e.target.value })}>
                    {ROLES.map((r) => <option key={r} value={r}>{t(`members.roles.${r}`)}</option>)}
                  </select>
                : <span className="pnl-pill muted">{t(`members.roles.${u.role}`, { defaultValue: u.role })}</span>}
              {canManage && u.panelId && <button className="pnl-icon-btn" title={t("groups.userGroups")} onClick={() => setGroupsOf({ id: u.panelId!, label: u.email })}><IcColumns size={16} /></button>}
              {canManage && u.panelId && <button className="pnl-icon-btn" title={t("perms.title")} onClick={() => setPermsOf({ id: u.panelId!, label: u.email })}><IcShield size={16} /></button>}
              {canManage && u.mailbox?.status !== "deleted" && u.mailboxId && <button className="pnl-icon-btn" title={t("mailboxes.changePassword")} onClick={() => setReset(u.email)}><IcKey size={16} /></button>}
              {canDeleteMailbox && u.mailbox && <MailboxActions mb={u.mailbox} />}
              {canManage && u.panelId && <button className="pnl-icon-btn" title={t("members.remove")} onClick={() => remove.mutate(u.panelId!)}><IcTrash size={16} /></button>}
            </div>
          ))}
        </div></div>

        <AliasesCard domains={activeDomains} canManage={canManage} />
      </div>

      {adding && <AddUserModal domains={activeDomains} actorRole={me?.role ?? ""} onClose={() => setAdding(false)}
        onDone={() => { setAdding(false); qc.invalidateQueries({ queryKey: ["members"] }); qc.invalidateQueries({ queryKey: ["admin-mailboxes"] }); toast(t("usersSection.created")); }} />}
      {reset && <ResetModal email={reset} onClose={() => setReset(null)} />}
      {groupsOf && <UserGroupsModal userId={groupsOf.id} label={groupsOf.label} onClose={() => setGroupsOf(null)} />}
      {permsOf && <UserPermissionsModal userId={permsOf.id} label={permsOf.label} onClose={() => setPermsOf(null)} />}
    </>
  );
}

function AddUserModal({ domains, actorRole, onClose, onDone }: { domains: { id: string; domain: string }[]; actorRole: string; onClose: () => void; onDone: () => void }) {
  const { t } = useTranslation();
  const [domainId, setDomainId] = useState(domains[0]?.id ?? "");
  const [login, setLogin] = useState(""), [name, setName] = useState(""), [pwd, setPwd] = useState(""), [role, setRole] = useState("member"), [err, setErr] = useState("");
  const [groups, setGroups] = useState<string[]>([]); // can join several groups at once
  const pwOk = useRef(false); // the strength meter decides whether the password passes (Stalwart rejects weak ones)
  const roles = actorRole === "owner" ? ROLES : ROLES.filter((r) => r !== "owner");
  const m = useMutation({
    mutationFn: () => api.createUser(domainId, login, name, pwd, role, groups),
    onSuccess: onDone, onError: (e) => setErr((e as Error).message),
  });
  const dom = domains.find((d) => d.id === domainId)?.domain ?? "";
  // Billing notice: with an active Stripe subscription and a paid plan, a new user joins the
  // subscription at a PRORATED amount for the days left in the period.
  const billing = useQuery({ queryKey: ["billing"], queryFn: api.billing, retry: false, enabled: hostedBilling });
  const b = billing.data;
  const paid = !!b?.subscription?.manageable && (b?.current_plan.price_cents_per_user ?? 0) > 0;
  const unit = b ? new Intl.NumberFormat(undefined, { style: "currency", currency: b.currency }).format(b.current_plan.price_cents_per_user / 100) : "";
  return (
    <Modal title={t("usersSection.add")} onClose={onClose}
      footer={<><button className="pnl-btn ghost" onClick={onClose}>{t("common.cancel")}</button>
        <button className="pnl-btn primary" disabled={m.isPending || !login || !pwOk.current} onClick={() => m.mutate()}>{t("usersSection.create")}</button></>}>
      <div className="pnl-field"><label>{t("panel.domains")}</label>
        <select className="pnl-select" value={domainId} onChange={(e) => setDomainId(e.target.value)}>{domains.map((d) => <option key={d.id} value={d.id}>{d.domain}</option>)}</select></div>
      <div className="pnl-field"><label>{t("usersSection.login")}</label>
        <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
          <input className="pnl-input" value={login} onChange={(e) => setLogin(e.target.value)} placeholder="ana" autoFocus /><span className="mono desc">@{dom}</span></div></div>
      <div className="pnl-field"><label>{t("usersSection.name")}</label><input className="pnl-input" value={name} onChange={(e) => setName(e.target.value)} /></div>
      <div className="pnl-field"><label>{t("groups.title")}</label>
        <GroupPicker value={groups} onChange={setGroups} /></div>
      <div className="pnl-field"><label>{t("membersSection.role")}</label>
        <select className="pnl-select" value={role} onChange={(e) => setRole(e.target.value)}>{roles.map((r) => <option key={r} value={r}>{t(`members.roles.${r}`)}</option>)}</select></div>
      <PasswordField label={t("login.password")} value={pwd} userInputs={[login, dom]}
        onChange={setPwd} onStrength={(s) => { pwOk.current = s.ok; }} />
      {paid && <div className="pnl-note">{t("usersSection.billingNote", { price: unit })}</div>}
      {err && <p className="pnl-err">{err}</p>}
    </Modal>
  );
}

function ResetModal({ email, onClose }: { email: string; onClose: () => void }) {
  const { t } = useTranslation();
  const [pwd, setPwd] = useState(""), [err, setErr] = useState("");
  const pwOk = useRef(false);
  const m = useMutation({
    mutationFn: () => api.resetUserPassword(email, pwd),
    onSuccess: () => { onClose(); toast(t("mailboxes.passwordChanged")); }, onError: (e) => setErr((e as Error).message),
  });
  return (
    <Modal title={t("mailboxes.changePassword")} onClose={onClose}
      footer={<><button className="pnl-btn ghost" onClick={onClose}>{t("common.cancel")}</button>
        <button className="pnl-btn primary" disabled={m.isPending || !pwOk.current} onClick={() => m.mutate()}>{t("mailboxes.changePassword")}</button></>}>
      <p className="desc" style={{ marginTop: 0 }}>{email}</p>
      <PasswordField label={t("usersSection.newPassword")} value={pwd} autoFocus userInputs={[email]}
        onChange={setPwd} onStrength={(s) => { pwOk.current = s.ok; }} />
      {err && <p className="pnl-err">{err}</p>}
    </Modal>
  );
}

// The user's OWN groups: check/uncheck several. Saving syncs the corresponding chat channels.
function UserGroupsModal({ userId, label, onClose }: { userId: string; label: string; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const cur = useQuery({ queryKey: ["user-groups", userId], queryFn: () => api.userGroups(userId) });
  const [sel, setSel] = useState<string[] | null>(null);
  const value = sel ?? (cur.data ?? []).map((g) => g.id);
  const save = useMutation({
    mutationFn: () => api.setUserGroups(userId, value),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["groups"] });
      qc.invalidateQueries({ queryKey: ["user-groups", userId] });
      toast(t("groups.saved"));
      onClose();
    },
    onError: (e) => toast((e as Error).message),
  });
  return (
    <Modal title={t("groups.userGroupsOf", { who: label })} onClose={onClose}
      footer={<><button className="pnl-btn ghost" onClick={onClose}>{t("common.cancel")}</button>
        <button className="pnl-btn primary" disabled={save.isPending} onClick={() => save.mutate()}>{t("groups.save")}</button></>}>
      <p className="desc" style={{ marginTop: 0 }}>{t("groups.multiHint")}</p>
      <GroupPicker value={value} onChange={setSel} />
    </Modal>
  );
}

// A member's permissions: the role gives a starting set and here it's adjusted person by person.
// Three states per capability — inherited from the role, granted by hand, revoked by hand —
// because without distinguishing them there's no telling why someone has (or lacks) something.
// Only what the person editing also has is shown as editable: the backend refuses the rest, and
// the UI doesn't promise what it can't deliver.
function UserPermissionsModal({ userId, label, onClose }: { userId: string; label: string; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["member-perms", userId], queryFn: () => api.memberPermissions(userId) });
  const [draft, setDraft] = useState<Record<string, boolean> | null>(null);

  const d = q.data;
  const effective = useMemo(() => new Set(d?.effective ?? []), [d]);
  const state = draft ?? Object.fromEntries((d?.all ?? []).map((p) => [p, effective.has(p)]));

  const save = useMutation({
    mutationFn: () => {
      const fromRole = new Set(d!.from_role);
      const allow: string[] = [], deny: string[] = [];
      for (const p of d!.all) {
        const on = !!state[p];
        if (on && !fromRole.has(p)) allow.push(p);   // granted beyond the role
        if (!on && fromRole.has(p)) deny.push(p);    // revoked from the role
      }
      return api.setMemberPermissions(userId, allow, deny);
    },
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["member-perms", userId] }); toast(t("perms.saved")); onClose(); },
    onError: (e) => toast((e as Error).message),
  });

  return (
    <Modal title={t("perms.of", { who: label })} onClose={onClose}
      footer={<><button className="pnl-btn ghost" onClick={onClose}>{t("common.cancel")}</button>
        <button className="pnl-btn primary" disabled={!d || save.isPending} onClick={() => save.mutate()}>{t("groups.save")}</button></>}>
      {!d ? <div className="pnl-empty">…</div> : (
        <>
          <p className="desc" style={{ marginTop: 0 }}>
            {t("perms.hint", { role: t(`members.roles.${d.role}`, { defaultValue: d.role }) })}
          </p>
          <div className="perm-list">
            {d.all.map((p) => {
              const fromRole = d.from_role.includes(p);
              const on = !!state[p];
              const editable = d.editable.includes(p);
              return (
                <label key={p} className={"perm-row" + (editable ? "" : " off")}>
                  <input type="checkbox" checked={on} disabled={!editable}
                    onChange={(e) => setDraft({ ...state, [p]: e.target.checked })} />
                  <span className="perm-tx">
                    <span className="perm-nm">{t(`perms.p.${p}`, { defaultValue: p })}</span>
                    <span className="perm-sub">
                      {fromRole && on && t("perms.fromRole")}
                      {!fromRole && on && t("perms.granted")}
                      {fromRole && !on && t("perms.revoked")}
                      {!fromRole && !on && t("perms.notInRole")}
                    </span>
                  </span>
                </label>
              );
            })}
          </div>
        </>
      )}
    </Modal>
  );
}

// Aliases: an extra address that delivers to another person's mailbox (contact@company.com → Ana's
// mailbox). NOT a member — it doesn't join chat, groups, or the people list; it's just mail. It
// counts as a mailbox for plan and billing purposes, which is why it shows up here, next to them.
function AliasesCard({ domains, canManage }: { domains: { id: string; domain: string }[]; canManage: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["aliases"], queryFn: () => api.aliases(), retry: false });
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.members(), retry: false });
  const [local, setLocal] = useState("");
  const [domainId, setDomainId] = useState("");
  const [ownerId, setOwnerId] = useState("");

  const create = useMutation({
    mutationFn: () => api.createAlias(local.trim(), domainId || domains[0]?.id || "", ownerId),
    onSuccess: () => { setLocal(""); qc.invalidateQueries({ queryKey: ["aliases"] }); toast(t("aliases.created")); },
    onError: (e) => toast((e as Error).message),
  });
  const remove = useMutation({
    mutationFn: (id: string) => api.deleteAlias(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["aliases"] }); toast(t("aliases.removed")); },
    onError: (e) => toast((e as Error).message),
  });

  const list = q.data?.aliases ?? [];
  const people = members.data ?? [];
  const canCreate = canManage && domains.length > 0 && people.length > 0 && local.trim() !== "" && ownerId !== "";

  return (
    <div className="pnl-card">
      <div className="pnl-card-h">
        <div><h3>{t("aliases.title")}</h3><div className="desc">{t("aliases.hint")}</div></div>
      </div>
      <div className="pnl-card-b">
        {canManage && (
          <div className="alias-new">
            <input className="pnl-input" style={{ width: 160 }} placeholder={t("aliases.localPh")} value={local}
              onChange={(e) => setLocal(e.target.value.replace(/[@\s]/g, ""))} />
            <span className="alias-at">@</span>
            <select className="pnl-select" style={{ width: 200 }} value={domainId || domains[0]?.id || ""} onChange={(e) => setDomainId(e.target.value)}>
              {domains.map((d) => <option key={d.id} value={d.id}>{d.domain}</option>)}
            </select>
            <select className="pnl-select" style={{ width: 240 }} value={ownerId} onChange={(e) => setOwnerId(e.target.value)}>
              <option value="">{t("aliases.pickOwner")}</option>
              {people.map((m) => <option key={m.id} value={m.id}>{m.email}</option>)}
            </select>
            <button className="pnl-btn primary" disabled={!canCreate || create.isPending} onClick={() => create.mutate()}>
              {t("aliases.create")}
            </button>
          </div>
        )}
        {list.length === 0 ? <div className="pnl-empty">{t("aliases.none")}</div> : list.map((a) => (
          <div className="pnl-row" key={a.id}>
            <div className="grow">
              <div className="mono">{a.address}</div>
              <div className="sub">{t("aliases.deliversTo", { who: a.owner_email })}</div>
            </div>
            {canManage && <button className="pnl-icon-btn" title={t("aliases.remove")} onClick={() => remove.mutate(a.id)}><IcTrash size={16} /></button>}
          </div>
        ))}
      </div>
    </div>
  );
}
