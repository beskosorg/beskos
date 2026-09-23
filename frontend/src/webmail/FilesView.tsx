import { useRef, useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { backend, isOfficeFile, viewerKind, type StorageFile, type Folder } from "./backend";
import { triggerDownload } from "../apiBase";
import { openInWindow } from "./nativeWindow";
import { FileTypeIcon, fileCategory } from "./filetypes";
import { Avatar } from "./Avatar";
import { ViewerModal } from "./ViewerModal";
import { toast, toastAction } from "./toast";
import {
  IcUpload, IcFolderPlus, IcGrid, IcList, IcTrash, IcPencil, IcShare, IcDownload, IcUsers, IcPencilBox, IcPlus, IcRefresh, IcCopy, IcStar,
} from "../panel/icons";
import { ShareModal } from "./ShareModal";
import { VersionsModal } from "./VersionsModal";
import { useUserSettings, useSection } from "./useSettings";
import { syncApi, syncSupported } from "./syncClient";
import { StorageBar } from "./FilesStorageBar";
import { RenameModal } from "./FilesRenameModal";
import { FilesTrashPanel } from "./FilesTrashPanel";
import { FilesGridView } from "./FilesGridView";
import { FilesListView } from "./FilesListView";

type TypeF = "" | "doc" | "pdf" | "sheet" | "image" | "video" | "audio" | "code" | "archive";
type PeopleF = "" | "me" | "shared" | "with-me";
type ModF = "" | "today" | "7d" | "30d" | "year";

// Drive: folders/subfolders, icons by type, grid/list, drag to move, share,
// filters (type/people/modified), owner column, and storage indicator.
export function FilesView({ search, folderParam, fileParam, onFolder }: {
  search: string;
  /** Open folder, coming from the URL (/mail/files/<id>) — no local state duplicates it. */
  folderParam?: string;
  /** File to open immediately (/mail/files/file/<id>): the target of the share notice. */
  fileParam?: string;
  onFolder: (id: string | null) => void;
}) {
  const { t, i18n } = useTranslation();
  const nav = useNavigate();
  const qc = useQueryClient();
  // The current folder LIVES in the URL: reloading, sharing the link, or using the browser's
  // "back" button all stay on the same folder. `setFolderId` navigates instead of storing state.
  // "trash" is the only reserved segment (folder ids are UUIDs, so they never collide).
  const trashOpen = folderParam === "trash";
  const folderId = trashOpen ? null : (folderParam ?? null);
  const setFolderId = onFolder;
  const setTrashOpen = (open: boolean) => onFolder(open ? "trash" : null);
  const [view, setView] = useState<"grid" | "list">("list");
  // Default view from Settings (settings.files.view): applied once on load; after that the
  // user controls it via the toolbar buttons.
  const settings = useUserSettings();
  const filesCfg = useSection("files");
  const changeView = (v: "grid" | "list") => { setView(v); filesCfg.save({ view: v }); };
  // Folders synced with the computer: highlighted in the list (only exists in the desktop app).
  // The current account (email) isolates sync per user — without it, switching accounts would
  // show the previous account's folders.
  const meQ = useQuery({ queryKey: ["me"], queryFn: () => backend.me(), staleTime: 60000 });
  const account = meQ.data?.email ?? "";
  const [syncedIds, setSyncedIds] = useState<Set<string>>(new Set());
  useEffect(() => {
    if (!syncSupported()) return;
    let alive = true;
    const tick = () => {
      syncApi.status(account)
        .then((st) => { if (alive) setSyncedIds(new Set(st.pairs.map((p) => p.folder_id))); })
        .catch(() => {});
    };
    tick();
    const id = setInterval(tick, 3000);
    return () => { alive = false; clearInterval(id); };
  }, [account]);
  const addSyncFolder = async () => {
    try {
      const pair = await syncApi.addFolder(account);
      if (pair) toast(t("files.syncStarted", { name: pair.name }));
    } catch (e) { toast((e as Error).message); }
  };
  const viewApplied = useRef(false);
  useEffect(() => {
    if (!viewApplied.current && settings.files?.view) { viewApplied.current = true; setView(settings.files.view); }
  }, [settings.files?.view]);
  const [dropTarget, setDropTarget] = useState<string | null>(null);
  const [rename, setRename] = useState<{ kind: "file" | "folder"; id: string; name: string } | null>(null);
  const [share, setShare] = useState<StorageFile | null>(null);
  const [versionsOf, setVersionsOf] = useState<StorageFile | null>(null);
  const [viewing, setViewing] = useState<StorageFile | null>(null);
  const [newFolder, setNewFolder] = useState(false);
  const [folderName, setFolderName] = useState("");
  const [newMenu, setNewMenu] = useState(false);
  const [typeF, setTypeF] = useState<TypeF>("");
  const [peopleF, setPeopleF] = useState<PeopleF>("");
  const [modF, setModF] = useState<ModF>("");
  const input = useRef<HTMLInputElement>(null);
  const dragId = useRef<string | null>(null);
  const dragKind = useRef<"file" | "folder">("file"); // what's being dragged (moving a file vs a folder)

  const listing = useQuery({ queryKey: ["files", folderId], queryFn: () => backend.files(folderId ?? undefined), retry: false });
  // "Shared with me": files other people shared (separate source, flat view).
  const sharedWithMe = useQuery({ queryKey: ["files-shared"], queryFn: () => backend.sharedWithMe(), enabled: peopleF === "with-me" || folderId === null, retry: false, refetchInterval: 30000, refetchOnWindowFocus: true });
  // Global (recursive) search: when there's a term, the backend searches ALL folders — not just the open one.
  const searchTerm = search.trim();
  const searching = searchTerm.length > 0 && peopleF !== "with-me";
  const globalSearch = useQuery({ queryKey: ["files-search", searchTerm], queryFn: () => backend.searchFiles(searchTerm), enabled: searching, retry: false });
  const invalidate = () => { qc.invalidateQueries({ queryKey: ["files"] }); qc.invalidateQueries({ queryKey: ["files-shared"] }); };

  const [uploads, setUploads] = useState<{ name: string; pct: number }[]>([]); // per-file progress bar
  const [osDrag, setOsDrag] = useState(false); // an OS file being dragged over the area
  // Multiple, sequential upload with progress. One failing doesn't bring down the others (toast per error).
  async function uploadMany(files: File[]) {
    if (!files.length) return;
    setUploads(files.map((f) => ({ name: f.name, pct: 0 })));
    let ok = 0;
    for (let i = 0; i < files.length; i++) {
      try {
        await backend.uploadFileProgress(files[i], folderId ?? undefined, (p) =>
          setUploads((u) => u.map((x, j) => (j === i ? { ...x, pct: p } : x))));
        ok++;
      } catch (e) { toast((e as Error).message); }
      setUploads((u) => u.map((x, j) => (j === i ? { ...x, pct: 1 } : x)));
    }
    setUploads([]);
    invalidate();
    if (ok > 0) toast(ok === 1 ? t("files.uploaded") : t("files.uploadedN", { count: ok }));
  }
  const mkFolder = useMutation({ mutationFn: () => backend.createFolder(folderName, folderId ?? undefined), onSuccess: () => { setNewFolder(false); setFolderName(""); invalidate(); toast(t("files.folderCreated")); }, onError: (e) => toast((e as Error).message) });
  const move = useMutation({ mutationFn: ({ id, to }: { id: string; to: string | null }) => backend.moveFile(id, to), onSuccess: () => { invalidate(); toast(t("files.moved")); } });
  const moveFolder = useMutation({ mutationFn: ({ id, to }: { id: string; to: string | null }) => backend.moveFolder(id, to), onSuccess: () => { invalidate(); toast(t("files.moved")); }, onError: (e) => toast((e as Error).message) });
  const del = useMutation({ mutationFn: (id: string) => backend.deleteFile(id), onSuccess: () => { invalidate(); toast(t("files.movedToTrash")); } });
  // Duplicate: inserts "(copy)" before the extension (the backend reuses the blob — no re-copying bytes).
  const copyName = (name: string) => { const dot = name.lastIndexOf("."); const suf = " " + t("files.copySuffix"); return dot > 0 ? name.slice(0, dot) + suf + name.slice(dot) : name + suf; };
  const dup = useMutation({ mutationFn: (f: StorageFile) => backend.copyFile(f.id, copyName(f.name)), onSuccess: () => { invalidate(); toast(t("files.duplicated")); }, onError: (e) => toast((e as Error).message) });
  const star = useMutation({ mutationFn: ({ id, v }: { id: string; v: boolean }) => backend.starFile(id, v), onSuccess: () => invalidate(), onError: (e) => toast((e as Error).message) });
  const [sort, setSort] = useState<"name" | "date" | "size">("name");
  // Multiple selection + batch action (trash with undo). Cleared on folder change.
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const toggleSel = (id: string) => setSelected((s) => { const n = new Set(s); n.has(id) ? n.delete(id) : n.add(id); return n; });
  useEffect(() => { setSelected(new Set()); }, [folderId, trashOpen]);
  const batchTrash = useMutation({
    mutationFn: (ids: string[]) => backend.batchFiles(ids, "trash"),
    onSuccess: (r, ids) => { invalidate(); const arr = [...ids]; setSelected(new Set()); toastAction(t("files.batchTrashed", { count: r.done }), t("common.undo"), () => backend.batchFiles(arr, "restore").then(() => invalidate())); },
    onError: (e) => toast((e as Error).message),
  });
  const delFolder = useMutation({ mutationFn: (id: string) => backend.deleteFolder(id), onSuccess: () => invalidate() });
  // Trash: restorable soft delete. The view opens in its own panel (setTrashOpen).
  const trash = useQuery({ queryKey: ["files-trash"], queryFn: () => backend.trashFiles(), enabled: trashOpen, retry: false });
  const invalidateTrash = () => { qc.invalidateQueries({ queryKey: ["files-trash"] }); qc.invalidateQueries({ queryKey: ["files"] }); };
  const restore = useMutation({ mutationFn: (id: string) => backend.restoreFile(id), onSuccess: () => { invalidateTrash(); toast(t("files.restored")); }, onError: (e) => toast((e as Error).message) });
  const purge = useMutation({ mutationFn: (id: string) => backend.purgeFile(id), onSuccess: () => { invalidateTrash(); toast(t("files.deletedForever")); }, onError: (e) => toast((e as Error).message) });
  const emptyTrash = useMutation({ mutationFn: () => backend.emptyTrash(), onSuccess: () => { invalidateTrash(); toast(t("files.trashEmptied")); }, onError: (e) => toast((e as Error).message) });
  const createOffice = useMutation({
    mutationFn: ({ kind, name }: { kind: "document" | "spreadsheet" | "presentation"; name: string }) =>
      backend.createOffice(kind, name, folderId ?? undefined),
    onSuccess: (f) => { setNewMenu(false); invalidate(); openEditor(f.id); },
    onError: (e) => toast((e as Error).message),
  });

  const fmtSize = (n: number) => n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`;
  const fmtDate = (iso: string) => new Date(iso).toLocaleDateString(i18n.language, { day: "2-digit", month: "short", year: "numeric" });
  const data = listing.data;

  const owner = data?.owner;
  const sharedSet = new Set((searching ? globalSearch.data?.shared : data?.shared) ?? []);
  const isShared = (id: string) => sharedSet.has(id);
  const filtering = typeF !== "" || peopleF !== "" || modF !== "";
  // "Location" column: in a global search the file can be in any folder, so it doesn't assert the current one.
  const localName = searching ? t("files.searchAll") : data?.breadcrumb?.length ? data.breadcrumb[data.breadcrumb.length - 1].name : t("files.myFiles");

  function withinMod(iso: string): boolean {
    if (!modF) return true;
    const days = { today: 1, "7d": 7, "30d": 30, year: 365 }[modF];
    return (Date.now() - new Date(iso).getTime()) <= days * 86400_000;
  }
  const withMe = peopleF === "with-me";
  // Files shared WITH ME appear in the main view (root, no filter/search) as their own section —
  // otherwise a file someone shares with me "does not show up in my Drive".
  const sharedWithMeIds = new Set((sharedWithMe.data?.files ?? []).map((f) => f.id));
  const rootDefault = folderId === null && peopleF === "" && !searching && typeF === "" && modF === "";
  // In "shared with me" the source is different (other people's files); when searching, the source
  // is the backend's GLOBAL result (all folders); otherwise, the open folder's files. The name is
  // already filtered by the server during search, so only the type/people/date filters apply here.
  const source = withMe ? (sharedWithMe.data?.files ?? []) : searching ? (globalSearch.data?.files ?? []) : (data?.files ?? []);
  const sharedExtra = rootDefault ? (sharedWithMe.data?.files ?? []) : [];
  const files = source.filter((f) =>
    (typeF === "" || fileCategory(f.name) === typeF) &&
    (withMe || peopleF === "" || (peopleF === "shared" ? isShared(f.id) : !isShared(f.id))) &&
    withinMod(f.created_at))
    // Favorites first; then by the chosen column (name/date/size).
    .sort((a, b) => (Number(!!b.starred) - Number(!!a.starred)) ||
      (sort === "size" ? b.size - a.size
        : sort === "date" ? b.created_at.localeCompare(a.created_at)
          : a.name.localeCompare(b.name, undefined, { numeric: true })));
  // When searching, folders come from the backend (name matched at any level); otherwise, the current folder's.
  const folders = searching ? (globalSearch.data?.folders ?? []) : (filtering ? [] : (data?.folders ?? []));

  function onDropTo(target: string | null) {
    const id = dragId.current;
    if (id && dragKind.current === "folder") {
      // A folder dropped on itself does nothing; the backend also blocks cycles/descendants.
      if (id !== target) moveFolder.mutate({ id, to: target });
    } else if (id) {
      move.mutate({ id, to: target });
    }
    dragId.current = null; setDropTarget(null);
  }
  // Office editor: in the native app (Tauri) opens in a dedicated window; on the web it navigates within the SPA itself.
  function openEditor(id: string) {
    if (!openInWindow(`editor-${id}`, `/mail/files/edit/${id}`, "Beskos — Editor")) nav(`/mail/files/edit/${id}`);
  }
  // Office files open in the editor; image/pdf/audio/video open in the viewer; the rest are downloaded.
  function openFile(f: StorageFile) {
    if (isOfficeFile(f.name)) openEditor(f.id);
    else if (viewerKind(f.name)) setViewing(f);
    else triggerDownload(backend.downloadFileUrl(f.id), f.name);
  }

  // /mail/files/file/<id>: target of the "a file was shared with you" notice. Looks for it among
  // files shared with me and root files, and opens it as if it had been clicked. Once per id —
  // opening the editor switches windows and reopening on every render would be a nightmare.
  const openedFile = useRef<string | null>(null);
  useEffect(() => {
    if (!fileParam || openedFile.current === fileParam) return;
    const f = (sharedWithMe.data?.files ?? []).find((x) => x.id === fileParam)
      ?? (listing.data?.files ?? []).find((x) => x.id === fileParam);
    if (!f) return; // still loading (or access already revoked): does nothing
    openedFile.current = fileParam;
    openFile(f);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fileParam, sharedWithMe.data, listing.data]);

  const ownerCell = (id: string) => (
    (withMe || sharedWithMeIds.has(id)) ? (
      <span className="owner-cell" title={t("files.sharedWithMe")}>
        <span className="shared-badge" title={t("files.sharedWithMe")}><IcUsers size={13} /></span>
        <span className="ot">{t("files.sharedWithMe")}</span>
      </span>
    ) : (
      <span className="owner-cell" title={owner?.email}>
        <Avatar name={owner?.name} email={owner?.email} size={22} className="mini-av" />
        <span className="ot">{t("files.youShort")}</span>
        {isShared(id) && <span className="shared-badge" title={t("files.sharedBadge")}><IcUsers size={13} /></span>}
      </span>
    )
  );

  const folderActions = (f: Folder) => (
    <span className="row-acts">
      <a title={t("files.downloadZip")} href={backend.folderZipUrl(f.id)} onClick={(e) => e.stopPropagation()}><button><IcDownload size={15} /></button></a>
      <button title={t("contacts.rename")} onClick={(e) => { e.stopPropagation(); setRename({ kind: "folder", id: f.id, name: f.name }); }}><IcPencil size={15} /></button>
      <button title={t("files.delete")} onClick={(e) => { e.stopPropagation(); delFolder.mutate(f.id); }}><IcTrash size={15} /></button>
    </span>
  );
  const fileActions = (f: StorageFile) => (
    <span className="row-acts">
      <button className={"star-btn" + (f.starred ? " on" : "")} title={t("files.star")} onClick={(e) => { e.stopPropagation(); star.mutate({ id: f.id, v: !f.starred }); }}><IcStar size={15} filled={f.starred} /></button>
      {isOfficeFile(f.name) && <button title={t("editor.open")} onClick={(e) => { e.stopPropagation(); openEditor(f.id); }}><IcPencilBox size={15} /></button>}
      {isOfficeFile(f.name) && <button title={t("files.versions")} onClick={(e) => { e.stopPropagation(); setVersionsOf(f); }}><IcRefresh size={15} /></button>}
      <button title={t("files.share")} onClick={(e) => { e.stopPropagation(); setShare(f); }}><IcShare size={15} /></button>
      <a title={t("files.download")} href={backend.downloadFileUrl(f.id)} onClick={(e) => e.stopPropagation()}><button><IcDownload size={15} /></button></a>
      <button title={t("files.duplicate")} onClick={(e) => { e.stopPropagation(); dup.mutate(f); }}><IcCopy size={15} /></button>
      <button title={t("contacts.rename")} onClick={(e) => { e.stopPropagation(); setRename({ kind: "file", id: f.id, name: f.name }); }}><IcPencil size={15} /></button>
      <button title={t("files.delete")} onClick={(e) => { e.stopPropagation(); del.mutate(f.id); }}><IcTrash size={15} /></button>
    </span>
  );

  const chip = (label: string, value: string, set: (v: any) => void, opts: { v: string; label: string }[]) => (
    <div className={"filter-chip" + (value ? " on" : "")}>
      <select value={value} onChange={(e) => set(e.target.value)}>
        <option value="">{label}</option>
        {opts.map((o) => <option key={o.v} value={o.v}>{o.label}</option>)}
      </select>
    </div>
  );

  if (trashOpen) {
    return (
      <FilesTrashPanel items={trash.data?.files ?? []} isLoading={trash.isLoading}
        onBack={() => setTrashOpen(false)} onEmptyTrash={() => emptyTrash.mutate()}
        onRestore={(id) => restore.mutate(id)} onPurge={(id) => purge.mutate(id)}
        fmtSize={fmtSize} fmtDate={fmtDate} t={t} />
    );
  }

  return (
    <div className="dv">
      <div className="dv-top">
        <div className="crumb">
          <button className={dropTarget === "root" ? "drop" : (folderId === null ? "cur" : "")}
            onClick={() => setFolderId(null)}
            onDragOver={(e) => { e.preventDefault(); setDropTarget("root"); }} onDragLeave={() => setDropTarget(null)} onDrop={() => onDropTo(null)}>
            {t("files.title")}
          </button>
          {(data?.breadcrumb ?? []).map((b, i, arr) => (
            <span key={b.id} style={{ display: "flex", alignItems: "center", gap: 5 }}>
              <span className="sep">›</span>
              <button className={i === arr.length - 1 ? "cur" : (dropTarget === b.id ? "drop" : "")}
                onClick={() => setFolderId(b.id)}
                onDragOver={(e) => { e.preventDefault(); setDropTarget(b.id); }} onDragLeave={() => setDropTarget(null)} onDrop={() => onDropTo(b.id)}>{b.name}</button>
            </span>
          ))}
        </div>
        <div className="dv-actions">
          {data?.quota && <StorageBar used={data.quota.used} max={data.quota.max} fmt={fmtSize} label={t("files.storageUsed", { used: fmtSize(data.quota.used), max: fmtSize(data.quota.max) })} />}
          <div className="filter-chip">
            <select value={sort} onChange={(e) => setSort(e.target.value as "name" | "date" | "size")} title={t("files.sortBy")}>
              <option value="name">{t("files.sortName")}</option>
              <option value="date">{t("files.sortDate")}</option>
              <option value="size">{t("files.sortSize")}</option>
            </select>
          </div>
          <div className="viewtoggle">
            <button className={view === "list" ? "on" : ""} title={t("files.list")} onClick={() => changeView("list")}><IcList size={16} /></button>
            <button className={view === "grid" ? "on" : ""} title={t("files.grid")} onClick={() => changeView("grid")}><IcGrid size={16} /></button>
          </div>
          <div className="new-wrap">
            <button className="chipbtn primary" onClick={() => setNewMenu((v) => !v)}><IcPlus size={15} /> {t("files.new")}</button>
            {newMenu && (
              <>
                <div className="menu-backdrop" onClick={() => setNewMenu(false)} />
                <div className="new-menu">
                  <button onClick={() => createOffice.mutate({ kind: "document", name: t("files.untitledDoc") })}><span className="ic"><FileTypeIcon name="a.docx" size={18} /></span>{t("files.newDoc")}</button>
                  <button onClick={() => createOffice.mutate({ kind: "spreadsheet", name: t("files.untitledSheet") })}><span className="ic"><FileTypeIcon name="a.xlsx" size={18} /></span>{t("files.newSheet")}</button>
                  <button onClick={() => createOffice.mutate({ kind: "presentation", name: t("files.untitledSlide") })}><span className="ic"><FileTypeIcon name="a.pptx" size={18} /></span>{t("files.newSlide")}</button>
                </div>
              </>
            )}
          </div>
          <button className="chipbtn" onClick={() => setNewFolder(true)}><IcFolderPlus size={15} /> {t("files.newFolder")}</button>
          {syncSupported() && (
            <button className="chipbtn" title={t("files.syncFolderHint")} onClick={() => void addSyncFolder()}>
              <IcRefresh size={15} /> {t("files.syncFolder")}
            </button>
          )}
          <button className="chipbtn" onClick={() => input.current?.click()}><IcUpload size={15} /> {t("files.upload")}</button>
          <button className="chipbtn" title={t("files.trash")} onClick={() => setTrashOpen(true)}><IcTrash size={15} /> {t("files.trash")}</button>
          <input ref={input} type="file" hidden multiple onChange={(e) => { const fs = e.target.files; if (fs?.length) uploadMany(Array.from(fs)); e.target.value = ""; }} />
        </div>
      </div>

      <div className="dv-filters">
        {chip(t("files.filterType"), typeF, setTypeF, [
          { v: "doc", label: t("files.typeDoc") }, { v: "pdf", label: "PDF" }, { v: "sheet", label: t("files.typeSheet") },
          { v: "image", label: t("files.typeImage") }, { v: "video", label: t("files.typeVideo") },
          { v: "audio", label: t("files.typeAudio") }, { v: "code", label: t("files.typeCode") }, { v: "archive", label: t("files.typeArchive") },
        ])}
        {chip(t("files.filterPeople"), peopleF, setPeopleF, [
          { v: "me", label: t("files.ownedByMe") }, { v: "shared", label: t("files.sharedByMe") },
          { v: "with-me", label: t("files.sharedWithMe") },
        ])}
        {chip(t("files.filterModified"), modF, setModF, [
          { v: "today", label: t("files.modToday") }, { v: "7d", label: t("files.mod7d") },
          { v: "30d", label: t("files.mod30d") }, { v: "year", label: t("files.modYear") },
        ])}
        {filtering && <button className="filter-clear" onClick={() => { setTypeF(""); setPeopleF(""); setModF(""); }}>{t("files.clearFilters")}</button>}
      </div>

      <div className={"dv-body" + (osDrag ? " os-drag" : "")}
        onDragOver={(e) => { if (e.dataTransfer.types.includes("Files")) { e.preventDefault(); setOsDrag(true); } }}
        onDragLeave={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setOsDrag(false); }}
        onDrop={(e) => { const fs = e.dataTransfer.files; if (fs?.length) { e.preventDefault(); setOsDrag(false); uploadMany(Array.from(fs)); } }}>
        {osDrag && <div className="dv-dropzone"><IcUpload size={30} /><span>{t("files.dropHere")}</span></div>}
        {uploads.length > 0 && (
          <div className="upload-progress">
            {uploads.map((u, i) => (
              <div className="up-item" key={i}>
                <span className="up-name">{u.name}</span>
                <span className="up-bar"><span className="up-fill" style={{ width: `${Math.round(u.pct * 100)}%` }} /></span>
              </div>
            ))}
          </div>
        )}
        {selected.size > 0 && (
          <div className="batch-bar">
            <span className="bb-count">{t("files.selected", { count: selected.size })}</span>
            <button className="chipbtn danger" onClick={() => batchTrash.mutate([...selected])} disabled={batchTrash.isPending}><IcTrash size={14} /> {t("files.delete")}</button>
            <button className="chipbtn" onClick={() => setSelected(new Set())}>{t("files.clearSel")}</button>
          </div>
        )}
        {listing.isLoading ? <div className="loading">…</div> : (
          <>
            {newFolder && (
              <div className="share-add" style={{ maxWidth: 360 }}>
                <input autoFocus className="pnl-input" style={{ border: "1px solid var(--accent)", borderRadius: 9, padding: "8px 11px", background: "var(--paper)", color: "var(--ink)" }}
                  value={folderName} placeholder={t("files.folderName")} onChange={(e) => setFolderName(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter" && folderName) mkFolder.mutate(); if (e.key === "Escape") setNewFolder(false); }} />
                <button className="btn-send send" onClick={() => mkFolder.mutate()} disabled={!folderName}>{t("contacts.save")}</button>
              </div>
            )}

            {folders.length === 0 && files.length === 0 && sharedExtra.length === 0 && !newFolder ? (
              <div className="empty">{filtering ? t("files.noMatch") : t("files.empty")}</div>
            ) : view === "grid" ? (
              <FilesGridView folders={folders} files={files} sharedExtra={sharedExtra} syncedIds={syncedIds}
                dropTarget={dropTarget} setDropTarget={setDropTarget} dragId={dragId} dragKind={dragKind}
                onDropTo={onDropTo} onOpenFolder={setFolderId} onOpenFile={openFile} isShared={isShared}
                sharedWithMeIds={sharedWithMeIds} folderActions={folderActions} fileActions={fileActions}
                fmtSize={fmtSize} t={t} />
            ) : (
              <FilesListView folders={folders} files={files} sharedExtra={sharedExtra} syncedIds={syncedIds}
                dropTarget={dropTarget} setDropTarget={setDropTarget} dragId={dragId} dragKind={dragKind}
                onDropTo={onDropTo} onOpenFolder={setFolderId} onOpenFile={openFile} selected={selected}
                toggleSel={toggleSel} localName={localName} ownerCell={ownerCell} folderActions={folderActions}
                fileActions={fileActions} fmtSize={fmtSize} fmtDate={fmtDate} t={t} />
            )}
          </>
        )}
      </div>

      {rename && <RenameModal item={rename} onClose={() => setRename(null)} onDone={() => { setRename(null); invalidate(); }} />}
      {share && <ShareModal file={share} onClose={() => setShare(null)} />}
      {versionsOf && <VersionsModal file={versionsOf} onClose={() => setVersionsOf(null)} />}
      {viewing && <ViewerModal file={viewing} onClose={() => setViewing(null)} />}
    </div>
  );
}
