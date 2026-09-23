// The offer made once, on the first sign-in: would you like to bring your old mail across?
//
// WHY ASK AT ALL, RATHER THAN LEAVE IT IN A MENU. The person who most needs the import is the one
// who has just arrived and does not yet know the product has one. They are also the person deciding,
// in the first ten minutes, whether moving here is going to work. A menu item answers neither
// question, because nobody reads a menu they have no reason to open.
//
// WHY IT IS ASKED ONCE. The server decides whether to offer it (see handleGetMigration) and records
// the answer against the account, not the browser. A dialog that reappears on the phone after being
// dismissed on the laptop is a dialog people learn to close without reading -- and the next one we
// show them, which might matter, gets closed the same way.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "../api";
import { Modal } from "./ui";
import { IcDownload } from "./icons";
import { MIGRATION_QUERY_KEY } from "./MigrationSection";

export function MigrationPrompt({ onOpen }: { onOpen: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  // Shares the cache with MigrationSection: one answer, not two requests.
  const q = useQuery({ queryKey: MIGRATION_QUERY_KEY, queryFn: api.migration, retry: false });

  const dismiss = useMutation({
    mutationFn: api.migrationDismiss,
    onSettled: () => qc.invalidateQueries({ queryKey: MIGRATION_QUERY_KEY }),
  });

  // The server says whether to ask. It is false for anyone who has migrated, anyone who has been
  // asked, and -- fail-closed -- anyone whose settings could not be read.
  if (!q.data?.prompt) return null;

  const close = () => dismiss.mutate();
  const accept = () => { dismiss.mutate(); onOpen(); };

  return (
    <Modal title={t("migration.promptTitle")} onClose={close}
      footer={
        <>
          {/* "Later" is a real answer and it is recorded as one. A dialog whose only exit is the
              action it wants is not a question. */}
          <button className="pnl-btn ghost" onClick={close}>{t("migration.promptLater")}</button>
          <button className="pnl-btn primary" onClick={accept}>
            <IcDownload size={15} /> {t("migration.promptAccept")}
          </button>
        </>
      }>
      <p className="desc">{t("migration.promptBody")}</p>
      {/* The two facts that decide the answer: nothing is removed from the old account, and it can
          be started later. Both are the objections somebody raises before agreeing. */}
      <p className="pnl-note">{t("migration.promptSafety")}</p>
    </Modal>
  );
}
