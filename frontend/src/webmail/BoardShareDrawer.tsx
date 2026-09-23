import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import { backend } from "./backend";
import { toast } from "./toast";
import { Drawer } from "./Drawer";
import { WikiGrantsEditor } from "./WikiGrantsEditor";

// Sharing a Kanban BOARD (managers): person/team/org with a view/edit/manage role.
// Important note shown in the UI: whoever has a card assigned on the board gets automatic access (editor).
export function BoardShareDrawer({ boardId, onClose }: { boardId: string; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const grants = useQuery({ queryKey: ["board-grants", boardId], queryFn: () => backend.boardGrants(boardId), retry: false });
  const refresh = () => { qc.invalidateQueries({ queryKey: ["board-grants", boardId] }); qc.invalidateQueries({ queryKey: ["boards"] }); };

  const add = useMutation({
    mutationFn: (v: { subjectType: string; subjectId: string | null; role: string }) =>
      backend.setBoardGrant(boardId, { subject_type: v.subjectType, subject_id: v.subjectId, role: v.role }),
    onSuccess: () => { refresh(); toast(t("webmail.kanban.saved")); },
    onError: (e) => toast((e as Error).message),
  });
  const remove = useMutation({
    mutationFn: (grantId: string) => backend.removeBoardGrant(boardId, grantId),
    onSuccess: refresh,
    onError: (e) => toast((e as Error).message),
  });

  return (
    <Drawer title={t("webmail.kanban.shareBoard")} onClose={onClose}
      footer={<><div className="grow" /><button className="chipbtn" onClick={onClose}>{t("common.cancel")}</button></>}>
      <p className="wiki-muted">{t("webmail.kanban.boardGrantsHint")}</p>
      <WikiGrantsEditor grants={grants.data ?? []} busy={add.isPending}
        onAdd={(st, sid, role) => add.mutate({ subjectType: st, subjectId: sid, role })}
        onRemove={(id) => remove.mutate(id)} />
    </Drawer>
  );
}
