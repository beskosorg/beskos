import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import { wikiApi, backend } from "./backend";
import { toast } from "./toast";
import { Drawer } from "./Drawer";

// WikiSettingsDrawer (B4): space settings — name/icon/description/visibility/AI indexing
// (managers) — and the "danger zone" to delete the wiki, restricted to the space OWNER or org admin, with
// confirmation by TYPING THE WIKI'S NAME (input compares; no native dialogs).

export function WikiSettingsDrawer({ spaceId, onClose, onDeleted }: {
  spaceId: string; onClose: () => void; onDeleted: () => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const space = useQuery({ queryKey: ["wiki-space", spaceId], queryFn: () => wikiApi.getSpace(spaceId), retry: false });
  const me = useQuery({ queryKey: ["me"], queryFn: () => backend.me(), retry: false, staleTime: 60_000 });

  const [name, setName] = useState("");
  const [icon, setIcon] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState("restricted");
  const [aiIndexed, setAiIndexed] = useState(true);
  const [confirmName, setConfirmName] = useState("");

  // Pre-fill once the space arrives (once per opening).
  const sp = space.data?.space;
  useEffect(() => {
    if (!sp) return;
    setName(sp.name); setIcon(sp.icon); setDescription(sp.description);
    setVisibility(sp.visibility); setAiIndexed(sp.ai_indexed);
  }, [sp?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const save = useMutation({
    mutationFn: () => wikiApi.updateSpace(spaceId, { name, icon, description, visibility, ai_indexed: aiIndexed }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["wiki-space", spaceId] });
      qc.invalidateQueries({ queryKey: ["wiki-spaces"] });
      toast(t("wiki.saved")); onClose();
    },
    onError: (e) => toast((e as Error).message),
  });
  const del = useMutation({
    mutationFn: () => wikiApi.deleteSpace(spaceId),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["wiki-spaces"] }); toast(t("wiki.deleted")); onDeleted(); onClose(); },
    onError: (e) => toast((e as Error).message),
  });

  // Danger zone only for the space owner or org admin (the backend revalidates regardless).
  const myRole = me.data?.role ?? "";
  const canDelete = !!sp && (me.data?.id === sp.owner_user_id || myRole === "owner" || myRole === "admin" || myRole === "internal_admin");
  const nameMatches = !!sp && confirmName.trim() === sp.name;

  return (
    <Drawer title={t("wiki.settings")} onClose={onClose}
      footer={<>
        <div className="grow" />
        <button className="chipbtn" onClick={onClose}>{t("wiki.cancel")}</button>
        <button className="add" disabled={!name.trim() || save.isPending} onClick={() => save.mutate()}>{t("wiki.save")}</button>
      </>}>
      {sp && (
        <>
          <label className="wiki-field"><span>{t("wiki.spaceName")}</span>
            <input value={name} onChange={(e) => setName(e.target.value)} /></label>
          <label className="wiki-field"><span>{t("wiki.spaceKey")}</span>
            <input value={sp.key} disabled /></label>
          <label className="wiki-field"><span>{t("wiki.icon")}</span>
            <input value={icon} onChange={(e) => setIcon(e.target.value)} maxLength={4} /></label>
          <label className="wiki-field"><span>{t("wiki.description")}</span>
            <textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} /></label>
          <label className="wiki-field"><span>{t("wiki.visibility")}</span>
            <select value={visibility} onChange={(e) => setVisibility(e.target.value)}>
              <option value="restricted">{t("wiki.vis.restricted")}</option>
              <option value="org">{t("wiki.vis.org")}</option>
              <option value="public">{t("wiki.vis.public")}</option>
            </select></label>
          <label className="wiki-public-toggle">
            <input type="checkbox" checked={aiIndexed} onChange={(e) => setAiIndexed(e.target.checked)} />
            <span>{t("wiki.aiIndexed")}</span>
          </label>

          {canDelete && (
            <div className="wiki-danger-zone">
              <div className="wiki-side-title">{t("wiki.dangerZone")}</div>
              <p className="wiki-muted">{t("wiki.deleteConfirmName", { name: sp.name })}</p>
              <input value={confirmName} onChange={(e) => setConfirmName(e.target.value)} placeholder={sp.name} />
              <button className="chipbtn danger" disabled={!nameMatches || del.isPending} onClick={() => del.mutate()}>
                {t("wiki.deleteForever")}
              </button>
            </div>
          )}
        </>
      )}
    </Drawer>
  );
}
