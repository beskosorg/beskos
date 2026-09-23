import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { backend, type StorageFile } from "./backend";
import { IcX, IcUpload, IcFolder, IcSearch, IcArrowLeft, IcPaperclip, IcUsers } from "../panel/icons";

// Presentation source picker: upload a file or pick one from Drive. Extracted from
// MeetPresentation.tsx (law 1.4 -- 600 lines).
//
// The "From Drive" tab is a mini file manager -- folders, search, and the "Shared with me"
// section -- so it stays usable with many files. Only shows PDF/PPTX/PPT/ODP.
const okPresExt = (n: string) => /\.(pdf|pptx|ppt|odp)$/i.test(n);
const fmtSz = (n: number) => (n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`);

export function Picker({ onClose, onUpload, onDrive }: {
  onClose: () => void; onUpload: (f: File) => void; onDrive: (id: string) => void;
}) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"upload" | "drive">("upload");
  const [folderId, setFolderId] = useState<string | undefined>(undefined);
  const [q, setQ] = useState("");
  const [listing, setListing] = useState<{ files: StorageFile[]; folders: { id: string; name: string }[]; breadcrumb: { id: string; name: string }[] } | null>(null);
  const [results, setResults] = useState<{ files: StorageFile[]; folders: { id: string; name: string }[] } | null>(null);
  const [shared, setShared] = useState<StorageFile[]>([]);
  const searching = q.trim().length > 0;

  // Loads the current folder (or the search) and, at the root, what's shared-with-me.
  useEffect(() => {
    if (tab !== "drive" || searching) return;
    backend.files(folderId).then(setListing).catch(() => setListing(null));
    if (!folderId) backend.sharedWithMe().then((r) => setShared(r.files.filter((f) => okPresExt(f.name)))).catch(() => setShared([]));
  }, [tab, folderId, searching]);
  useEffect(() => {
    if (tab !== "drive" || !searching) return;
    const id = setTimeout(() => { backend.searchFiles(q.trim()).then(setResults).catch(() => setResults(null)); }, 250);
    return () => clearTimeout(id);
  }, [tab, q, searching]);

  const folders = searching ? (results?.folders ?? []) : (listing?.folders ?? []);
  const files = (searching ? (results?.files ?? []) : (listing?.files ?? [])).filter((f) => okPresExt(f.name) && !f.deleted_at);
  const crumbs = listing?.breadcrumb ?? [];

  return (
    <div className="pres-modal" onMouseDown={onClose}>
      <div className="pres-picker" onMouseDown={(e) => e.stopPropagation()}>
        <header><b>{t("meet.present")}</b><button className="ib" title={t("common.close")} onClick={onClose}><IcX size={15} /></button></header>
        <div className="pres-tabs">
          <button className={tab === "upload" ? "on" : ""} onClick={() => setTab("upload")}><IcUpload size={14} /> {t("meet.presUpload")}</button>
          <button className={tab === "drive" ? "on" : ""} onClick={() => setTab("drive")}><IcFolder size={14} /> {t("meet.presDrive")}</button>
        </div>
        {tab === "upload" ? (
          <label className="pres-drop">
            <IcUpload size={22} />
            <span>{t("meet.presPickFile")}</span>
            <input type="file" accept=".pdf,.pptx,.ppt,.odp" hidden onChange={(e) => { const f = e.target.files?.[0]; if (f) onUpload(f); }} />
          </label>
        ) : (
          <div className="pres-fm">
            <label className="pres-fm-search">
              <IcSearch size={14} />
              <input value={q} placeholder={t("drivePicker.search")} onChange={(e) => setQ(e.target.value)} autoFocus />
            </label>
            {!searching && (
              <div className="pres-fm-crumbs">
                {folderId && (
                  <button className="ib" title={t("drivePicker.up")}
                    onClick={() => setFolderId(crumbs.length > 1 ? crumbs[crumbs.length - 2].id : undefined)}><IcArrowLeft size={13} /></button>
                )}
                <button className="pres-crumb" onClick={() => setFolderId(undefined)}>{t("drivePicker.root")}</button>
                {crumbs.map((c) => <span key={c.id}> / <button className="pres-crumb" onClick={() => setFolderId(c.id)}>{c.name}</button></span>)}
              </div>
            )}
            <div className="pres-fm-list">
              {folders.map((fo) => (
                <button key={fo.id} className="pres-fm-item folder" onClick={() => { setQ(""); setFolderId(fo.id); }}>
                  <IcFolder size={16} /> <span className="nm">{fo.name}</span>
                </button>
              ))}
              {files.map((f) => (
                <button key={f.id} className="pres-fm-item" onClick={() => onDrive(f.id)}>
                  <IcPaperclip size={15} /> <span className="nm">{f.name}</span><span className="sz">{fmtSz(f.size)}</span>
                </button>
              ))}
              {/* Shared with me (root only, no search) -- can also be presented. */}
              {!searching && !folderId && shared.length > 0 && (
                <>
                  <div className="pres-fm-sec">{t("files.sharedWithMe")}</div>
                  {shared.map((f) => (
                    <button key={"sh" + f.id} className="pres-fm-item" onClick={() => onDrive(f.id)}>
                      <IcUsers size={15} /> <span className="nm">{f.name}</span><span className="sz">{fmtSz(f.size)}</span>
                    </button>
                  ))}
                </>
              )}
              {folders.length === 0 && files.length === 0 && shared.length === 0 && (
                <div className="editor-msg">{searching ? t("drivePicker.noResults") : t("meet.presNoFiles")}</div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
