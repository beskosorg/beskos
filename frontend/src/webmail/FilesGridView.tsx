import type { Dispatch, MutableRefObject, ReactNode, SetStateAction } from "react";
import type { Folder, StorageFile } from "./backend";
import { Thumb } from "./FileThumb";
import { IcFolder, IcRefresh, IcStar, IcUsers, IcDownload } from "../panel/icons";
import { backend } from "./backend";

// Grid ("card") layout of the Drive listing: folders, then files, then what's shared with me.
// Extracted from FilesView.tsx (law 1.4 -- 600 lines).
interface FilesGridViewProps {
  folders: Folder[];
  files: StorageFile[];
  sharedExtra: StorageFile[];
  syncedIds: Set<string>;
  dropTarget: string | null;
  setDropTarget: Dispatch<SetStateAction<string | null>>;
  dragId: MutableRefObject<string | null>;
  dragKind: MutableRefObject<"file" | "folder">;
  onDropTo: (target: string | null) => void;
  onOpenFolder: (id: string) => void;
  onOpenFile: (f: StorageFile) => void;
  isShared: (id: string) => boolean;
  sharedWithMeIds: Set<string>;
  folderActions: (f: Folder) => ReactNode;
  fileActions: (f: StorageFile) => ReactNode;
  fmtSize: (n: number) => string;
  t: (key: string, opts?: Record<string, unknown>) => string;
}

export function FilesGridView({
  folders, files, sharedExtra, syncedIds, dropTarget, setDropTarget, dragId, dragKind, onDropTo,
  onOpenFolder, onOpenFile, isShared, sharedWithMeIds, folderActions, fileActions, fmtSize, t,
}: FilesGridViewProps) {
  return (
    <>
      {folders.length > 0 && <div className="dv-sec">{t("files.folders")}</div>}
      <div className="dv-grid">
        {folders.map((f) => (
          <div className={"dvc" + (dropTarget === f.id ? " drop" : "") + (syncedIds.has(f.id) ? " synced" : "")} key={f.id} draggable onClick={() => onOpenFolder(f.id)}
            onDragStart={(e) => { dragId.current = f.id; dragKind.current = "folder"; e.currentTarget.classList.add("dragging"); }}
            onDragEnd={(e) => e.currentTarget.classList.remove("dragging")}
            onDragOver={(e) => { e.preventDefault(); setDropTarget(f.id); }} onDragLeave={() => setDropTarget(null)} onDrop={() => onDropTo(f.id)}>
            <span className="ic"><IcFolder size={26} /></span>
            <div className="fn">{f.name}
              {syncedIds.has(f.id) && <span className="sync-badge" title={t("files.syncedFolder")}><IcRefresh size={11} /></span>}
            </div>
            {folderActions(f)}
          </div>
        ))}
      </div>
      {files.length > 0 && <div className="dv-sec">{t("files.filesLabel")}</div>}
      <div className="dv-grid">
        {files.map((f) => (
          <div className="dvc" key={f.id} draggable
            onDragStart={(e) => { dragId.current = f.id; dragKind.current = "file"; e.currentTarget.classList.add("dragging"); }}
            onDragEnd={(e) => e.currentTarget.classList.remove("dragging")}
            onClick={() => onOpenFile(f)}>
            <span className="ic">
              <Thumb file={f} size={26} />
              {f.starred && <span className="star-badge" title={t("files.star")}><IcStar size={12} filled /></span>}
              {(isShared(f.id) || sharedWithMeIds.has(f.id)) && <span className="shared-badge fl" title={sharedWithMeIds.has(f.id) ? t("files.sharedWithMe") : t("files.sharedBadge")}><IcUsers size={12} /></span>}
            </span>
            <div className="fn">{f.name}</div>
            <div className="meta">{fmtSize(f.size)}{f.version > 1 ? ` · v` : ""}</div>
            {fileActions(f)}
          </div>
        ))}
      </div>
      {sharedExtra.length > 0 && (
        <>
          <div className="dv-sec">{t("files.sharedWithMe")}</div>
          <div className="dv-grid">
            {sharedExtra.map((f) => (
              <div className="dvc" key={"sh" + f.id} onClick={() => onOpenFile(f)}>
                <span className="ic">
                  <Thumb file={f} size={26} />
                  <span className="shared-badge fl" title={t("files.sharedWithMe")}><IcUsers size={12} /></span>
                </span>
                <div className="fn">{f.name}</div>
                <div className="meta">{fmtSize(f.size)}{f.version > 1 ? ` · v` : ""}</div>
                <span className="row-acts">
                  <a title={t("files.download")} href={backend.downloadFileUrl(f.id)} onClick={(e) => e.stopPropagation()}><button><IcDownload size={15} /></button></a>
                </span>
              </div>
            ))}
          </div>
        </>
      )}
    </>
  );
}
