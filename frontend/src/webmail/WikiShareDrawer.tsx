import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import { wikiApi } from "./backend";
import { toast } from "./toast";
import { Drawer } from "./Drawer";
import { WikiGrantsEditor } from "./WikiGrantsEditor";

// WikiShareDrawer is the sharing for a WIKI (space) — managers only: grants (person/team/org with a
// view/edit/manage role, via WikiGrantsEditor) + the public read-only link. No native dialogs.

export function WikiShareDrawer({ spaceId, onClose }: { spaceId: string; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();

  const space = useQuery({ queryKey: ["wiki-space", spaceId], queryFn: () => wikiApi.getSpace(spaceId), retry: false });
  const grants = useQuery({ queryKey: ["wiki-grants", spaceId], queryFn: () => wikiApi.listGrants(spaceId), retry: false });
  const refresh = () => qc.invalidateQueries({ queryKey: ["wiki-grants", spaceId] });

  const add = useMutation({
    mutationFn: (v: { subjectType: string; subjectId: string | null; role: string }) =>
      wikiApi.setGrant(spaceId, { subject_type: v.subjectType, subject_id: v.subjectId, role: v.role }),
    onSuccess: () => { refresh(); toast(t("wiki.saved")); },
    onError: (e) => toast((e as Error).message),
  });
  const remove = useMutation({
    mutationFn: (grantId: string) => wikiApi.removeGrant(spaceId, grantId),
    onSuccess: refresh,
    onError: (e) => toast((e as Error).message),
  });
  const setPublic = useMutation({
    mutationFn: (v: { enabled: boolean; regenerate?: boolean }) => wikiApi.setPublic(spaceId, v.enabled, v.regenerate),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["wiki-space", spaceId] }); qc.invalidateQueries({ queryKey: ["wiki-spaces"] }); toast(t("wiki.saved")); },
    onError: (e) => toast((e as Error).message),
  });
  const publicSlug = space.data?.space.public_slug;
  const publicUrl = publicSlug ? `${window.location.origin}/w/${publicSlug}` : "";

  return (
    <Drawer title={t("wiki.share")} onClose={onClose} footer={<><div className="grow" /><button className="chipbtn" onClick={onClose}>{t("wiki.cancel")}</button></>}>
      <WikiGrantsEditor grants={grants.data ?? []} busy={add.isPending}
        onAdd={(st, sid, role) => add.mutate({ subjectType: st, subjectId: sid, role })}
        onRemove={(id) => remove.mutate(id)} />

      <div className="wiki-public-block">
        <label className="wiki-public-toggle">
          <input type="checkbox" checked={!!publicSlug} onChange={(e) => setPublic.mutate({ enabled: e.target.checked })} disabled={setPublic.isPending} />
          <span>{t("wiki.public.enable")}</span>
        </label>
        <p className="wiki-muted">{t("wiki.public.imagesWarn")}</p>
        {publicSlug && (
          <div className="wiki-public-link">
            <input readOnly value={publicUrl} onFocus={(e) => e.currentTarget.select()} />
            <button className="chipbtn" onClick={() => { void navigator.clipboard.writeText(publicUrl); toast(t("wiki.public.copied")); }}>{t("wiki.public.copy")}</button>
            <button className="chipbtn" onClick={() => setPublic.mutate({ enabled: true, regenerate: true })}>{t("wiki.public.regenerate")}</button>
          </div>
        )}
      </div>
    </Drawer>
  );
}
