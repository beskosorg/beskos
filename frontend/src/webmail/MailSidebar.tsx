import type { Dispatch, SetStateAction } from "react";
import type { Mailbox } from "../jmap/types";
import type { Label } from "./labelsApi";
import { IcPencil, IcFolderPlus, IcSparkle, IcStar, IcClock } from "../panel/icons";
import { MailAccountSwitcher } from "./MailAccountSwitcher";
import { folderLabel, layoutFolders } from "./mailFolders";

// Folder rail of the mail view (compose button, account switcher, folder tree, starred, labels,
// scheduled/rules entries), extracted from MailView.tsx (law 1.4 -- 600 lines).
interface MailSidebarProps {
  folderMenu: boolean;
  setFolderMenu: Dispatch<SetStateAction<boolean>>;
  onCompose: () => void;
  aliases: string[];
  primary: string;
  addrFilter: string;
  setAddrFilter: Dispatch<SetStateAction<string>>;
  addrOpen: boolean;
  setAddrOpen: Dispatch<SetStateAction<boolean>>;
  boxes: Mailbox[];
  activeFolder: string | null;
  search: string;
  starred: boolean;
  dragId: string | null;
  dropOn: string | null;
  setDropOn: Dispatch<SetStateAction<string | null>>;
  onOpenFolder: (f: Mailbox) => void;
  onDropInto: (folderId: string) => Promise<void>;
  unreadOf: (f: Mailbox) => number;
  unreadOnly: boolean;
  setUnreadOnly: Dispatch<SetStateAction<boolean>>;
  renameFolderId: string | null;
  setRenameFolderId: Dispatch<SetStateAction<string | null>>;
  folderInput: string;
  setFolderInput: Dispatch<SetStateAction<string>>;
  onRenameFolder: (id: string) => Promise<void>;
  kebabFolder: string | null;
  setKebabFolder: Dispatch<SetStateAction<string | null>>;
  subfolderOf: string | null;
  setSubfolderOf: Dispatch<SetStateAction<string | null>>;
  onCreateSubfolder: (parentId: string) => Promise<void>;
  onDeleteFolder: (id: string) => Promise<void>;
  newFolder: string | null;
  setNewFolder: Dispatch<SetStateAction<string | null>>;
  onCreateFolder: () => Promise<void>;
  labels: Label[];
  labelId: string | null;
  nav: (to: string) => void;
  onOpenScheduled: () => void;
  rules: boolean;
  t: (key: string, opts?: Record<string, unknown>) => string;
}

export function MailSidebar(props: MailSidebarProps) {
  const {
    folderMenu, setFolderMenu, onCompose, aliases, primary, addrFilter, setAddrFilter, addrOpen, setAddrOpen,
    boxes, activeFolder, search, starred, dragId, dropOn, setDropOn, onOpenFolder, onDropInto, unreadOf,
    unreadOnly, setUnreadOnly, renameFolderId, setRenameFolderId, folderInput, setFolderInput, onRenameFolder,
    kebabFolder, setKebabFolder, subfolderOf, setSubfolderOf, onCreateSubfolder, onDeleteFolder,
    newFolder, setNewFolder, onCreateFolder, labels, labelId, nav, onOpenScheduled, rules, t,
  } = props;

  return (
    <>
      {folderMenu && <div className="mgrid-backdrop" onClick={() => setFolderMenu(false)} />}
      <aside className={"mrail" + (folderMenu ? " open" : "")} onClick={() => setFolderMenu(false)}>
        <button className="compose" onClick={onCompose}><IcPencil size={16} /> <span>{t("webmail.compose.new")}</span></button>
        <MailAccountSwitcher aliases={aliases} primary={primary} addrFilter={addrFilter} setAddrFilter={setAddrFilter}
          addrOpen={addrOpen} setAddrOpen={setAddrOpen} t={t} />
        <div className="nav">
          {layoutFolders(boxes).map(({ f, depth }) => (
            <div key={f.id} className="folder-row" style={{ marginInlineStart: depth * 12 }}>
              {renameFolderId === f.id ? (
                <input className="newfolder" autoFocus value={folderInput} onChange={(e) => setFolderInput(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter") void onRenameFolder(f.id); if (e.key === "Escape") { setRenameFolderId(null); setFolderInput(""); } }}
                  onBlur={() => void onRenameFolder(f.id)} />
              ) : (
                <button className={"fbtn " + (f.id === activeFolder && !search && !starred ? "on" : "") + (dropOn === f.id ? " drop" : "")}
                  // Clicking a folder LEAVES Automations: it is a page of the mail app, not a layer.
                  onClick={() => onOpenFolder(f)}
                  onDragOver={(e) => { if (dragId) { e.preventDefault(); setDropOn(f.id); } }}
                  onDragLeave={() => setDropOn((v) => (v === f.id ? null : v))}
                  onDrop={(e) => { e.preventDefault(); void onDropInto(f.id); }}>
                  <span>{folderLabel(t, f)}</span>
                  {unreadOf(f) > 0 && (
                    // Clicking the number shows the unread ones -- that is the question the number raises.
                    <span className={"c" + (unreadOnly && f.id === activeFolder ? " on" : "")} title={t("webmail.showUnread")}
                      onClick={(e) => { e.stopPropagation(); onOpenFolder(f); setUnreadOnly((v) => !(v && f.id === activeFolder)); }}>
                      {unreadOf(f)}
                    </span>
                  )}
                </button>
              )}
              {/* Only user folders (without a system role) can be renamed/deleted/nested. */}
              {!f.role && renameFolderId !== f.id && (
                <div className="folder-menuwrap">
                  <button className="folder-kebab" title={t("webmail.folderActions")} onClick={() => setKebabFolder((v) => (v === f.id ? null : f.id))}>⋯</button>
                  {kebabFolder === f.id && (
                    <div className="move-menu" onMouseLeave={() => setKebabFolder(null)}>
                      <button onClick={() => { setRenameFolderId(f.id); setFolderInput(f.name); setKebabFolder(null); }}>{t("webmail.renameFolder")}</button>
                      <button onClick={() => { setSubfolderOf(f.id); setFolderInput(""); setKebabFolder(null); }}>{t("webmail.newSubfolder")}</button>
                      <button className="danger-item" onClick={() => void onDeleteFolder(f.id)}>{t("webmail.deleteFolder")}</button>
                    </div>
                  )}
                </div>
              )}
              {subfolderOf === f.id && (
                <input className="newfolder subfolder" autoFocus value={folderInput} placeholder={t("webmail.folderName")} onChange={(e) => setFolderInput(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter") void onCreateSubfolder(f.id); if (e.key === "Escape") { setSubfolderOf(null); setFolderInput(""); } }}
                  onBlur={() => void onCreateSubfolder(f.id)} />
              )}
            </div>
          ))}
          {/* Starred sits WITH the folders: it is somewhere to navigate to, not an action. The
              $flagged mark decides the content, but to the user it reads as one more place. */}
          <button className={"star-row" + (starred ? " on" : "")}
            onClick={() => nav(starred ? "/mail/mail" : "/mail/mail/starred")}>
            <IcStar size={15} filled={starred} />
            <span>{t("webmail.starred")}</span>
          </button>

          {/* Labels: each one is a view by keyword. The catalog is managed in the reader. */}
          {labels.map((l) => (
            <button key={l.id} className={"label-row" + (labelId === l.id ? " on" : "")}
              onClick={() => nav(labelId === l.id ? "/mail/mail" : `/mail/mail/label:${l.id}`)}>
              <span className="label-dot" style={{ background: l.color }} />
              <span>{l.name}</span>
            </button>
          ))}

          {newFolder !== null
            ? <input className="newfolder" autoFocus value={newFolder} placeholder={t("webmail.folderName")}
                onChange={(e) => setNewFolder(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter") void onCreateFolder(); if (e.key === "Escape") setNewFolder(null); }}
                onBlur={() => void onCreateFolder()} />
            : <button className="addfolder" onClick={() => setNewFolder("")}><IcFolderPlus size={15} /> <span>{t("webmail.newFolder")}</span></button>}
        </div>
        <button className="addfolder rules-entry" onClick={onOpenScheduled}>
          <IcClock size={15} /> <span>{t("webmail.scheduled.title")}</span>
        </button>
        <button className={"addfolder rules-entry" + (rules ? " on" : "")}
          onClick={() => nav(rules ? "/mail/mail" : "/mail/mail/rules")}>
          <IcSparkle size={15} /> <span>{t("rules.title")}</span>
        </button>
      </aside>
    </>
  );
}
