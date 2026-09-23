import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { backend, StorageFile } from "./backend";
import { toast } from "./toast";
import { Drawer } from "./Drawer";
import { IcFolder, IcPaperclip, IcSearch, IcUpload, IcArrowLeft } from "../panel/icons";

// DrivePickerDrawer: a Drive-integrated file manager for PICKING a file or uploading one right
// where you are — Google style. Reused by wiki attachments, by chat, and by anything else that
// needs "attach from Drive". No native dialogs (the file input is the OS's own picker).

const fmtSize = (n: number) => (n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`);

export function DrivePickerDrawer({ onPick, onClose, accept }: {
  onPick: (f: StorageFile) => void;
  onClose: () => void;
  accept?: string; // filter for the upload input (e.g. "image/*")
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [folderId, setFolderId] = useState<string | undefined>(undefined);
  const [q, setQ] = useState("");
  const [uploading, setUploading] = useState(false);
  const fileIn = useRef<HTMLInputElement>(null);

  const searching = q.trim().length > 0;
  const listing = useQuery({ queryKey: ["drive-pick", folderId], queryFn: () => backend.files(folderId), retry: false, enabled: !searching });
  const search = useQuery({ queryKey: ["drive-pick-search", q], queryFn: () => backend.searchFiles(q), retry: false, enabled: searching });

  const folders = searching ? (search.data?.folders ?? []) : (listing.data?.folders ?? []);
  const files = (searching ? (search.data?.files ?? []) : (listing.data?.files ?? [])).filter((f) => !f.deleted_at);
  const crumbs = listing.data?.breadcrumb ?? [];

  async function upload(list: FileList | null) {
    if (!list || list.length === 0) return;
    setUploading(true);
    try {
      const sf = await backend.uploadFile(list[0], folderId);
      qc.invalidateQueries({ queryKey: ["drive-pick", folderId] });
      onPick(sf);
      onClose();
    } catch (e) {
      toast((e as Error).message);
    } finally {
      setUploading(false);
    }
  }

  return (
    <Drawer wide title={t("drivePicker.title")} onClose={onClose}
      footer={<>
        <button className="chipbtn" disabled={uploading} onClick={() => fileIn.current?.click()}>
          <IcUpload size={14} /> {uploading ? "…" : t("drivePicker.upload")}
        </button>
        <div className="grow" />
        <button className="chipbtn" onClick={onClose}>{t("wiki.cancel")}</button>
      </>}>
      <input ref={fileIn} type="file" accept={accept} hidden onChange={(e) => void upload(e.target.files)} />
      <label className="dp-search">
        <IcSearch size={14} />
        <input value={q} placeholder={t("drivePicker.search")} onChange={(e) => setQ(e.target.value)} />
      </label>
      {!searching && (
        <div className="dp-crumbs">
          <button className="dp-crumb" onClick={() => setFolderId(undefined)}>{t("drivePicker.root")}</button>
          {crumbs.map((c) => (
            <span key={c.id}> / <button className="dp-crumb" onClick={() => setFolderId(c.id)}>{c.name}</button></span>
          ))}
          {folderId && crumbs.length > 0 && (
            <button className="ib dp-up" title={t("drivePicker.up")}
              onClick={() => setFolderId(crumbs.length > 1 ? crumbs[crumbs.length - 2].id : undefined)}>
              <IcArrowLeft size={14} />
            </button>
          )}
        </div>
      )}
      <div className="dp-list">
        {folders.map((fo) => (
          <button key={fo.id} className="dp-item" onClick={() => { setQ(""); setFolderId(fo.id); }}>
            <IcFolder size={16} /> <span className="nm">{fo.name}</span>
          </button>
        ))}
        {files.map((f) => (
          <button key={f.id} className="dp-item" onClick={() => { onPick(f); onClose(); }}>
            <IcPaperclip size={15} /> <span className="nm">{f.name}</span>
            <span className="sz">{fmtSize(f.size)}</span>
          </button>
        ))}
        {folders.length === 0 && files.length === 0 && (
          <div className="wiki-muted">{searching ? t("drivePicker.noResults") : t("drivePicker.emptyFolder")}</div>
        )}
      </div>
    </Drawer>
  );
}
