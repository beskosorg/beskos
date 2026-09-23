import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";

import { colleaguesApi, backend } from "./backend";
import { IcTrash } from "../panel/icons";

// WikiGrantsEditor is the shared grants UI (list + add) used by SPACE sharing
// (WikiShareDrawer) and PAGE sharing (WikiPageShareDrawer). Subjects: person (colleagues), team
// (/api/my-groups) or organization; roles view/edit/manage. No native dialogs.

export type GrantRow = { id: string; subject_type: "user" | "group" | "org"; subject_id: string | null; role: string };

const ROLES = ["viewer", "editor", "manager"] as const;

// Shared by the wiki and by Notes, which has two roles instead of four: there is nothing to comment
// on in a note, and a 'manager' that cannot do anything a wiki manager can would be a lie in a
// dropdown. Callers that pass nothing keep the wiki's set.

export function WikiGrantsEditor({ grants, onAdd, onRemove, busy, roles = ROLES }: {
  grants: GrantRow[];
  onAdd: (subjectType: string, subjectId: string | null, role: string) => void;
  onRemove: (grantId: string) => void;
  busy?: boolean;
  roles?: readonly string[];
}) {
  const { t } = useTranslation();
  const [subjectType, setSubjectType] = useState<"org" | "user" | "group">("user");
  const [subjectId, setSubjectId] = useState("");
  const [role, setRole] = useState<string>("viewer");

  const colleagues = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false });
  const teams = useQuery({ queryKey: ["my-groups"], queryFn: () => backend.myGroups(), retry: false });

  function subjectName(g: GrantRow): string {
    if (g.subject_type === "org") return t("wiki.subject.org");
    if (g.subject_type === "user") return colleagues.data?.find((c) => c.id === g.subject_id)?.name ?? g.subject_id ?? "—";
    return teams.data?.find((x) => x.id === g.subject_id)?.name ?? g.subject_id ?? "—";
  }
  const canAdd = (subjectType === "org" || !!subjectId) && !busy;

  return (
    <>
      <div className="wiki-grants">
        {grants.length === 0 && <div className="wiki-muted">{t("wiki.noGrants")}</div>}
        {grants.map((g) => (
          <div key={g.id} className="wiki-grant-row">
            <span className="wiki-grant-subj">
              <span className="wiki-grant-kind">{t(`wiki.subject.${g.subject_type}`)}</span>
              {subjectName(g)}
            </span>
            <span className="wiki-grant-role">{t(`wiki.role.${g.role}`)}</span>
            <button className="ib danger" title={t("wiki.remove")} onClick={() => onRemove(g.id)}><IcTrash size={15} /></button>
          </div>
        ))}
      </div>

      <div className="wiki-add-grant">
        <div className="wiki-side-title">{t("wiki.addAccess")}</div>
        <label className="wiki-field"><span>{t("wiki.shareWith")}</span>
          <select value={subjectType} onChange={(e) => { setSubjectType(e.target.value as "org" | "user" | "group"); setSubjectId(""); }}>
            <option value="user">{t("wiki.subject.user")}</option>
            <option value="group">{t("wiki.subject.group")}</option>
            <option value="org">{t("wiki.subject.org")}</option>
          </select>
        </label>
        {subjectType === "user" && (
          <label className="wiki-field"><span>{t("wiki.pickPerson")}</span>
            <select value={subjectId} onChange={(e) => setSubjectId(e.target.value)}>
              <option value="">—</option>
              {colleagues.data?.map((c) => <option key={c.id} value={c.id}>{c.name} ({c.email})</option>)}
            </select>
          </label>
        )}
        {subjectType === "group" && (
          <label className="wiki-field"><span>{t("wiki.pickTeam")}</span>
            <select value={subjectId} onChange={(e) => setSubjectId(e.target.value)}>
              <option value="">—</option>
              {teams.data?.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
            </select>
          </label>
        )}
        <label className="wiki-field"><span>{t("wiki.accessLevel")}</span>
          <select value={role} onChange={(e) => setRole(e.target.value)}>
            {roles.map((rr) => <option key={rr} value={rr}>{t(`wiki.role.${rr}`)}</option>)}
          </select>
        </label>
        <button className="add" disabled={!canAdd}
          onClick={() => { onAdd(subjectType, subjectType === "org" ? null : subjectId, role); setSubjectId(""); }}>
          {t("wiki.addAccess")}
        </button>
      </div>
    </>
  );
}
