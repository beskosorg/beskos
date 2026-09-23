import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import { wikiApi } from "./backend";
import { toast } from "./toast";
import { Drawer } from "./Drawer";
import { WikiGrantsEditor } from "./WikiGrantsEditor";

// WikiPageShareDrawer is the sharing for ONE PAGE (B3): extra grants scoped to this page only, which
// ADD to the access inherited from the wiki (never reduce it — doc §5). Only page managers open it.

export function WikiPageShareDrawer({ pageId, onClose }: { pageId: string; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();

  const grants = useQuery({ queryKey: ["wiki-page-grants", pageId], queryFn: () => wikiApi.pageGrants(pageId), retry: false });
  const refresh = () => qc.invalidateQueries({ queryKey: ["wiki-page-grants", pageId] });

  const add = useMutation({
    mutationFn: (v: { subjectType: string; subjectId: string | null; role: string }) =>
      wikiApi.setPageGrant(pageId, { subject_type: v.subjectType, subject_id: v.subjectId, role: v.role }),
    onSuccess: () => { refresh(); toast(t("wiki.saved")); },
    onError: (e) => toast((e as Error).message),
  });
  const remove = useMutation({
    mutationFn: (grantId: string) => wikiApi.removePageGrant(pageId, grantId),
    onSuccess: refresh,
    onError: (e) => toast((e as Error).message),
  });

  return (
    <Drawer title={t("wiki.sharePage")} onClose={onClose} footer={<><div className="grow" /><button className="chipbtn" onClick={onClose}>{t("wiki.cancel")}</button></>}>
      <p className="wiki-muted">{t("wiki.pageGrantsHint")}</p>
      <WikiGrantsEditor grants={grants.data ?? []} busy={add.isPending}
        onAdd={(st, sid, role) => add.mutate({ subjectType: st, subjectId: sid, role })}
        onRemove={(id) => remove.mutate(id)} />
    </Drawer>
  );
}
