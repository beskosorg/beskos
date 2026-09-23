import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import { notesApi } from "./notesApi";
import { WikiGrantsEditor, type GrantRow } from "./WikiGrantsEditor";
import { Drawer } from "./Drawer";
import { toast } from "./toast";

// Sharing a note, in the drawer pattern the rest of the product uses -- no native dialogs.
//
// The grants editor is the wiki's, unchanged apart from being told which roles exist here. Sharing
// is the same idea in both places, and a second widget that looked almost the same would be a way
// of teaching people that "share" means something different depending on where they clicked.

const NOTE_ROLES = ["viewer", "editor"] as const;

export function NoteShareDrawer({ noteId, onClose }: { noteId: string; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["note-grants", noteId], queryFn: () => notesApi.grants(noteId), retry: false });
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ["note-grants", noteId] });
    // The list shows who a note is shared with, so it goes stale the moment this changes.
    void qc.invalidateQueries({ queryKey: ["notes"] });
  };

  const add = useMutation({
    mutationFn: (g: { subject_type: string; subject_id: string | null; role: string }) => notesApi.setGrant(noteId, g),
    onSuccess: refresh,
    onError: (e) => toast((e as Error).message),
  });
  const remove = useMutation({
    mutationFn: (grantId: string) => notesApi.removeGrant(noteId, grantId),
    onSuccess: refresh,
    onError: (e) => toast((e as Error).message),
  });

  return (
    <Drawer title={t("notes.share")} onClose={onClose}>
      <p className="desc">{t("notes.shareHint")}</p>
      <WikiGrantsEditor
        grants={(q.data?.grants ?? []) as GrantRow[]}
        roles={NOTE_ROLES}
        busy={add.isPending || remove.isPending}
        onAdd={(subjectType, subjectId, role) => add.mutate({ subject_type: subjectType, subject_id: subjectId, role })}
        onRemove={(grantId) => remove.mutate(grantId)}
      />
    </Drawer>
  );
}
