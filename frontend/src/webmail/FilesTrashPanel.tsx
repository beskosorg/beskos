import type { StorageFile } from "./backend";
import { FileTypeIcon } from "./filetypes";
import { IcTrash, IcRefresh } from "../panel/icons";

// Drive trash panel: soft-deleted files, restorable or purged for good. Extracted from
// FilesView.tsx (law 1.4 -- 600 lines).
interface FilesTrashPanelProps {
  items: StorageFile[];
  isLoading: boolean;
  onBack: () => void;
  onEmptyTrash: () => void;
  onRestore: (id: string) => void;
  onPurge: (id: string) => void;
  fmtSize: (n: number) => string;
  fmtDate: (iso: string) => string;
  t: (key: string, opts?: Record<string, unknown>) => string;
}

export function FilesTrashPanel({ items, isLoading, onBack, onEmptyTrash, onRestore, onPurge, fmtSize, fmtDate, t }: FilesTrashPanelProps) {
  return (
    <div className="dv">
      <div className="dv-top">
        <div className="crumb">
          {/* Same shape as the normal path ("Files > demo"): text buttons, no icons -- with an icon
              inside the button the label would wrap to the next line. */}
          <button onClick={onBack}>{t("files.title")}</button>
          <span className="sep">›</span>
          <button className="cur">{t("files.trash")}</button>
        </div>
        <div className="dv-actions">
          {items.length > 0 && <button className="chipbtn danger" onClick={onEmptyTrash}><IcTrash size={15} /> {t("files.emptyTrash")}</button>}
        </div>
      </div>
      <div className="dv-body">
        {isLoading ? <div className="loading">…</div> : items.length === 0 ? (
          <div className="empty">{t("files.trashEmpty")}</div>
        ) : (
          <div className="dv-list">
            <div className="dv-head">
              <span className="col-name">{t("files.colName")}</span>
              <span className="col-owner" />
              <span className="col-loc" />
              <span className="col-mod">{t("files.colDeleted")}</span>
              <span className="col-size">{t("files.colSize")}</span>
              <span className="col-acts" />
            </div>
            {items.map((f) => (
              <div className="dvrow" key={f.id}>
                <span className="col-name"><span className="ic"><FileTypeIcon name={f.name} size={20} /></span><span className="fn">{f.name}</span></span>
                <span className="col-owner" />
                <span className="col-loc" />
                <span className="col-mod meta">{f.deleted_at ? fmtDate(f.deleted_at) : "—"}</span>
                <span className="col-size meta">{fmtSize(f.size)}</span>
                <span className="col-acts">
                  <span className="row-acts">
                    <button title={t("files.restore")} onClick={() => onRestore(f.id)}><IcRefresh size={15} /></button>
                    <button title={t("files.deleteForever")} onClick={() => onPurge(f.id)}><IcTrash size={15} /></button>
                  </span>
                </span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
