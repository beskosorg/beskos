import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import { wikiApi } from "./backend";
import { toast } from "./toast";
import { triggerDownload } from "../apiBase";
import { DrivePickerDrawer } from "./DrivePickerDrawer";
import { IcPaperclip, IcDownload, IcX, IcPlus } from "../panel/icons";

// Attachments of a page: reuses Files. Search by name in the user's own files and link by
// file_id (never a new upload). Download available to anyone who can view; attach/remove only to whoever can edit.

const fmtSize = (n: number) => (n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`);

export function WikiAttachments({ pageId, canEdit }: { pageId: string; canEdit: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [pickOpen, setPickOpen] = useState(false);

  const list = useQuery({ queryKey: ["wiki-attachments", pageId], queryFn: () => wikiApi.attachments(pageId), retry: false });
  const refresh = () => qc.invalidateQueries({ queryKey: ["wiki-attachments", pageId] });
  const attach = useMutation({ mutationFn: (fileId: string) => wikiApi.attachFile(pageId, fileId), onSuccess: refresh, onError: (e) => toast((e as Error).message) });
  const detach = useMutation({ mutationFn: (attId: string) => wikiApi.detachAttachment(pageId, attId), onSuccess: refresh, onError: (e) => toast((e as Error).message) });

  const items = list.data ?? [];
  if (items.length === 0 && !canEdit) return null;

  return (
    <section className="wiki-attachments">
      <h3 className="wiki-comments-title">{t("wiki.attachments")}</h3>
      {items.map((a) => (
        <div className="wiki-att-item" key={a.id}>
          <IcPaperclip size={14} />
          <span className="grow nm">{a.name}</span>
          <span className="sz">{fmtSize(a.size)}</span>
          <button className="ib" title={t("files.download")} onClick={() => triggerDownload(wikiApi.attachmentDownloadUrl(pageId, a.id), a.name)}><IcDownload size={14} /></button>
          {canEdit && <button className="ib danger" title={t("wiki.remove")} onClick={() => detach.mutate(a.id)}><IcX size={13} /></button>}
        </div>
      ))}
      {canEdit && (
        <button className="chipbtn" onClick={() => setPickOpen(true)}>
          <IcPlus size={14} /> {t("wiki.attachFromDrive")}
        </button>
      )}
      {pickOpen && <DrivePickerDrawer onClose={() => setPickOpen(false)} onPick={(f) => attach.mutate(f.id)} />}
    </section>
  );
}
