import type { Dispatch, MutableRefObject, ReactNode, SetStateAction } from "react";
import type { Folder, StorageFile } from "./backend";
import { FileTypeIcon } from "./filetypes";
import { IcFolder, IcRefresh, IcDownload } from "../panel/icons";
import { backend } from "./backend";

// List (table) layout of the Drive listing: folders, then files, then what's shared with me.
// Extracted from FilesView.tsx (law 1.4 -- 600 lines).
interface FilesListViewProps {
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
  selected: Set<string>;
  toggleSel: (id: string) => void;
  localName: string;
  ownerCell: (id: string) => ReactNode;
  folderActions: (f: Folder) => ReactNode;
  fileActions: (f: StorageFile) => ReactNode;
  fmtSize: (n: number) => string;
  fmtDate: (iso: string) => string;
  t: (key: string, opts?: Record<string, unknown>) => string;
}

export function FilesListView({
  folders, files, sharedExtra, syncedIds, dropTarget, setDropTarget, dragId, dragKind, onDropTo,
  onOpenFolder, onOpenFile, selected, toggleSel, localName, ownerCell, folderActions, fileActions,
  fmtSize, fmtDate, t,
}: FilesListViewProps) {
  return (
    <div className="dv-list">
      <div className="dv-head">
        <span className="col-name">{t("files.colName")}</span>
        <span className="col-owner">{t("files.colOwner")}</span>
        <span className="col-loc">{t("files.colLocation")}</span>
        <span className="col-mod">{t("files.colModified")}</span>
        <span className="col-size">{t("files.colSize")}</span>
        <span className="col-acts" />
      </div>
      {/* Group labels: make explicit that folders and files are siblings in the SAME folder, not a
          tree with the files inside the last listed folder. */}
      {folders.length > 0 && files.length > 0 && <div className="dv-group">{t("files.folders")}</div>}
      {folders.map((f) => (
        <div className={"dvrow" + (dropTarget === f.id ? " drop" : "") + (syncedIds.has(f.id) ? " synced" : "")} key={f.id} draggable onClick={() => onOpenFolder(f.id)}
          onDragStart={(e) => { dragId.current = f.id; dragKind.current = "folder"; e.currentTarget.classList.add("dragging"); }}
          onDragEnd={(e) => e.currentTarget.classList.remove("dragging")}
          onDragOver={(e) => { e.preventDefault(); setDropTarget(f.id); }} onDragLeave={() => setDropTarget(null)} onDrop={() => onDropTo(f.id)}>
          <span className="col-name">
            {/* Takes the place of the checkbox on file rows, so the names line up. */}
            <span className="row-sel-gap" aria-hidden="true" />
            <span className="ic"><IcFolder size={20} /></span><span className="fn">{f.name}</span>
            {syncedIds.has(f.id) && <span className="sync-badge" title={t("files.syncedFolder")}><IcRefresh size={11} /></span>}
          </span>
          <span className="col-owner">{ownerCell("")}</span>
          <span className="col-loc meta">{localName}</span>
          <span className="col-mod meta">{fmtDate(f.created_at)}</span>
          <span className="col-size meta">—</span>
          <span className="col-acts">{folderActions(f)}</span>
        </div>
      ))}
      {folders.length > 0 && files.length > 0 && <div className="dv-group">{t("files.sectionFiles")}</div>}
      {files.map((f) => (
        <div className={"dvrow" + (selected.has(f.id) ? " sel" : "")} key={f.id} draggable
          onDragStart={(e) => { dragId.current = f.id; dragKind.current = "file"; e.currentTarget.classList.add("dragging"); }}
          onDragEnd={(e) => e.currentTarget.classList.remove("dragging")}
          onClick={() => onOpenFile(f)}>
          <span className="col-name">
            <input type="checkbox" className="row-sel" checked={selected.has(f.id)} onClick={(e) => e.stopPropagation()} onChange={() => toggleSel(f.id)} title={t("files.select")} />
            <span className="ic"><FileTypeIcon name={f.name} size={20} /></span><span className="fn">{f.name}</span>
          </span>
          <span className="col-owner">{ownerCell(f.id)}</span>
          <span className="col-loc meta">{localName}</span>
          <span className="col-mod meta">{fmtDate(f.created_at)}</span>
          <span className="col-size meta">{fmtSize(f.size)}</span>
          <span className="col-acts">{fileActions(f)}</span>
        </div>
      ))}
      {sharedExtra.length > 0 && (
        <>
          <div className="dv-group">{t("files.sharedWithMe")}</div>
          {sharedExtra.map((f) => (
            <div className="dvrow" key={"sh" + f.id} onClick={() => onOpenFile(f)}>
              <span className="col-name">
                <span className="row-sel-gap" aria-hidden="true" />
                <span className="ic"><FileTypeIcon name={f.name} size={20} /></span><span className="fn">{f.name}</span>
              </span>
              <span className="col-owner">{ownerCell(f.id)}</span>
              <span className="col-loc meta">{localName}</span>
              <span className="col-mod meta">{fmtDate(f.created_at)}</span>
              <span className="col-size meta">{fmtSize(f.size)}</span>
              <span className="col-acts">
                <span className="row-acts">
                  <a title={t("files.download")} href={backend.downloadFileUrl(f.id)} onClick={(e) => e.stopPropagation()}><button><IcDownload size={15} /></button></a>
                </span>
              </span>
            </div>
          ))}
        </>
      )}
    </div>
  );
}
