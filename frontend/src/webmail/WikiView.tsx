import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import { wikiApi, WikiTreeNode, ApiError } from "./backend";
import { toast } from "./toast";
import { Drawer } from "./Drawer";
import { WikiEditor } from "./WikiEditor";
import { WikiShareDrawer } from "./WikiShareDrawer";
import { WikiPageShareDrawer } from "./WikiPageShareDrawer";
import { WikiSettingsDrawer } from "./WikiSettingsDrawer";
import { WikiHome } from "./WikiHome";
import { WikiNewPageDrawer } from "./WikiNewPageDrawer";
import { WikiHistoryDrawer } from "./WikiHistoryDrawer";
import { WikiComments } from "./WikiComments";
import { WikiAttachments } from "./WikiAttachments";
import { IcBook, IcPlus, IcTrash, IcPencil, IcShare, IcClock, IcGear, IcPrinter } from "../panel/icons";
import { useChatSocket } from "./useChatSocket";

// WikiView is the knowledge base app (Confluence-style): the list of wikis (spaces) on the left
// and the page tree; the page reader and the rich editor on the right. Selection lives in the URL
// (/mail/wiki/:spaceId/:pageId) — shareable deep links and navigation from the assistant (B6). Wiki
// creation via sidecar (Drawer); new page and confirmations via inline UI — no native dialogs.

const EMPTY_DOC: unknown[] = []; // BlockNote: empty document = empty array


export function WikiView({ sub, sub2, onNav }: {
  sub?: string; sub2?: string; onNav: (spaceId: string | null, pageId?: string | null) => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const spaceId = sub || null;
  const pageId = sub2 || null;
  const [newSpaceOpen, setNewSpaceOpen] = useState(false);
  const [shareOpen, setShareOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [newPageOpen, setNewPageOpen] = useState(false);

  // B6: when the ASSISTANT creates/edits a page, the backend publishes "wiki_page" on the hub for this
  // user — on create we navigate to the page; on edit we refresh the one that's open (see it happen).
  useChatSocket((ev) => {
    if (ev.type !== "wiki_page") return;
    const p = ev.payload as { action?: string; space_id?: string; page_id?: string };
    if (!p.space_id || !p.page_id) return;
    qc.invalidateQueries({ queryKey: ["wiki-tree", p.space_id] });
    qc.invalidateQueries({ queryKey: ["wiki-page", p.page_id] });
    if (p.action === "created") onNav(p.space_id, p.page_id);
  });

  const spaces = useQuery({ queryKey: ["wiki-spaces"], queryFn: () => wikiApi.listSpaces(), retry: false });
  const spaceDetail = useQuery({ queryKey: ["wiki-space", spaceId], queryFn: () => wikiApi.getSpace(spaceId!), enabled: !!spaceId, retry: false });
  const tree = useQuery({ queryKey: ["wiki-tree", spaceId], queryFn: () => wikiApi.tree(spaceId!), enabled: !!spaceId, retry: false });
  const spaceRole = spaceDetail.data?.role ?? 0;


  return (
    <div className="wiki">
      <aside className="wiki-side">
        <div className="wiki-side-head">
          <span className="wiki-side-title"><IcBook size={16} /> {t("wiki.title")}</span>
          <button className="ib" title={t("wiki.newSpace")} onClick={() => setNewSpaceOpen(true)}><IcPlus size={16} /></button>
        </div>
        {spaces.isLoading && <div className="wiki-muted">…</div>}
        {spaces.data && spaces.data.length === 0 && <div className="wiki-muted">{t("wiki.noSpaces")}</div>}
        <ul className="wiki-spaces">
          {spaces.data?.map((s) => (
            <li key={s.id}>
              <button className={"wiki-space" + (s.id === spaceId ? " on" : "")} onClick={() => onNav(s.id)}>
                <span>{s.icon || "📘"} {s.name}</span>
              </button>
            </li>
          ))}
        </ul>
        {spaceId && (
          <div className="wiki-tree-block">
            <div className="wiki-side-head">
              <span className="wiki-side-title">{t("wiki.pages")}</span>
              <span className="wiki-side-tools">
                {spaceRole >= 4 && <button className="ib" title={t("wiki.share")} onClick={() => setShareOpen(true)}><IcShare size={15} /></button>}
                {spaceRole >= 4 && <button className="ib" title={t("wiki.settings")} onClick={() => setSettingsOpen(true)}><IcGear size={15} /></button>}
                {spaceRole >= 3 && <button className="ib" title={t("wiki.newPage")} onClick={() => setNewPageOpen(true)}><IcPlus size={16} /></button>}
              </span>
            </div>
            {tree.data && tree.data.length === 0 && <div className="wiki-muted">{t("wiki.noPages")}</div>}
            <WikiTree nodes={tree.data ?? []} activeId={pageId} onSelect={(id) => onNav(spaceId, id)} />
          </div>
        )}
      </aside>
      <section className="wiki-main">
        {pageId ? (
          <WikiPagePane key={pageId} pageId={pageId} spaceId={spaceId!} pages={flattenTree(tree.data ?? [])} onDeleted={() => onNav(spaceId)} />
        ) : spaceId ? (
          <div className="wiki-empty">{t("wiki.selectPage")}</div>
        ) : (
          <WikiHome />
        )}
      </section>
      {newPageOpen && spaceId && <WikiNewPageDrawer spaceId={spaceId} onClose={() => setNewPageOpen(false)} onCreated={(id) => onNav(spaceId, id)} />}
      {newSpaceOpen && <NewSpaceDrawer onClose={() => setNewSpaceOpen(false)} onCreated={(id) => { onNav(id); qc.invalidateQueries({ queryKey: ["wiki-spaces"] }); }} />}
      {shareOpen && spaceId && <WikiShareDrawer spaceId={spaceId} onClose={() => setShareOpen(false)} />}
      {settingsOpen && spaceId && <WikiSettingsDrawer spaceId={spaceId} onClose={() => setSettingsOpen(false)} onDeleted={() => onNav(null)} />}
    </div>
  );
}

function WikiTree({ nodes, activeId, onSelect }: { nodes: WikiTreeNode[]; activeId: string | null; onSelect: (id: string) => void }) {
  return (
    <ul className="wiki-tree">
      {nodes.map((n) => (
        <li key={n.id}>
          <button className={"wiki-node" + (n.id === activeId ? " on" : "")} onClick={() => onSelect(n.id)}>
            {n.title || "—"}{n.status === "draft" ? " ·" : ""}
          </button>
          {n.children.length > 0 && <WikiTree nodes={n.children} activeId={activeId} onSelect={onSelect} />}
        </li>
      ))}
    </ul>
  );
}

// ConfirmButton avoids window.confirm: the 1st click reveals "confirm / cancel" inline.
function ConfirmButton({ label, confirmLabel, onConfirm, className, icon, title }: {
  label: string; confirmLabel: string; onConfirm: () => void; className?: string; icon?: JSX.Element; title?: string;
}) {
  const { t } = useTranslation();
  const [armed, setArmed] = useState(false);
  // With no label (icon only) it needs a tooltip: uses `title` (or the label itself when it exists).
  if (!armed) return <button className={className} title={title || label || undefined} onClick={() => setArmed(true)}>{icon} {label}</button>;
  return (
    <span className="wiki-confirm">
      <button className="wiki-danger" onClick={() => { onConfirm(); setArmed(false); }}>{confirmLabel}</button>
      <button className="chipbtn" onClick={() => setArmed(false)}>{t("wiki.cancel")}</button>
    </span>
  );
}

function NewSpaceDrawer({ onClose, onCreated }: { onClose: () => void; onCreated: (id: string) => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [visibility, setVisibility] = useState("restricted");
  const [description, setDescription] = useState("");
  const [icon, setIcon] = useState("");
  const create = useMutation({
    mutationFn: () => wikiApi.createSpace({ name, key: key || name, visibility, description, icon }),
    onSuccess: (sp) => { onCreated(sp.id); toast(t("wiki.created")); onClose(); },
    onError: (e) => toast((e as Error).message),
  });
  const canSave = name.trim().length > 0 && !create.isPending;
  return (
    <Drawer title={t("wiki.newSpace")} onClose={onClose}
      footer={<>
        <button className="chipbtn" onClick={onClose}>{t("wiki.cancel")}</button>
        <div className="grow" />
        <button className="add" disabled={!canSave} onClick={() => create.mutate()}>{t("wiki.create")}</button>
      </>}>
      <label className="wiki-field"><span>{t("wiki.spaceName")}</span>
        <input autoFocus value={name} onChange={(e) => setName(e.target.value)} /></label>
      <label className="wiki-field"><span>{t("wiki.spaceKey")}</span>
        <input value={key} onChange={(e) => setKey(e.target.value.toUpperCase())} placeholder="ENG" /></label>
      <label className="wiki-field"><span>{t("wiki.icon")}</span>
        <input value={icon} onChange={(e) => setIcon(e.target.value)} placeholder="📘" maxLength={4} /></label>
      <label className="wiki-field"><span>{t("wiki.description")}</span>
        <textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} /></label>
      <label className="wiki-field"><span>{t("wiki.visibility")}</span>
        <select value={visibility} onChange={(e) => setVisibility(e.target.value)}>
          <option value="restricted">{t("wiki.vis.restricted")}</option>
          <option value="org">{t("wiki.vis.org")}</option>
          <option value="public">{t("wiki.vis.public")}</option>
        </select></label>
    </Drawer>
  );
}

// flattenTree linearizes the tree into {id,label} for the editor's [[page]] picker.
function flattenTree(nodes: WikiTreeNode[]): { id: string; label: string }[] {
  const out: { id: string; label: string }[] = [];
  const walk = (ns: WikiTreeNode[]) => ns.forEach((n) => { out.push({ id: n.id, label: n.title || "—" }); walk(n.children); });
  walk(nodes);
  return out;
}

function WikiPagePane({ pageId, spaceId, pages, onDeleted }: { pageId: string; spaceId: string; pages: { id: string; label: string }[]; onDeleted: () => void }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [pageShareOpen, setPageShareOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [doc, setDoc] = useState<unknown>(EMPTY_DOC);

  const page = useQuery({ queryKey: ["wiki-page", pageId], queryFn: () => wikiApi.getPage(pageId), retry: false });
  const invalidate = () => { qc.invalidateQueries({ queryKey: ["wiki-page", pageId] }); qc.invalidateQueries({ queryKey: ["wiki-tree", spaceId] }); };

  const save = useMutation({
    mutationFn: () => wikiApi.updatePage(pageId, { title, body_json: doc, status: page.data!.page.status, expected_version: page.data!.page.version }),
    onSuccess: () => { setEditing(false); invalidate(); toast(t("wiki.saved")); },
    onError: (e) => {
      if (e instanceof ApiError && e.code === "wiki.version_conflict") { toast(t("wiki.conflict")); invalidate(); }
      else toast((e as Error).message);
    },
  });
  const setStatus = useMutation({
    mutationFn: (status: string) => wikiApi.updatePage(pageId, { title: page.data!.page.title, body_json: page.data!.page.body_json, status, expected_version: page.data!.page.version }),
    onSuccess: () => { invalidate(); toast(t("wiki.saved")); },
    onError: (e) => toast((e as Error).message),
  });
  const tmpl = useMutation({
    mutationFn: (v: boolean) => wikiApi.setTemplate(pageId, v),
    onSuccess: () => { invalidate(); qc.invalidateQueries({ queryKey: ["wiki-templates", spaceId] }); toast(t("wiki.saved")); },
    onError: (e) => toast((e as Error).message),
  });
  const del = useMutation({
    mutationFn: () => wikiApi.deletePage(pageId),
    onSuccess: () => { onDeleted(); qc.invalidateQueries({ queryKey: ["wiki-tree", spaceId] }); toast(t("wiki.deleted")); },
    onError: (e) => toast((e as Error).message),
  });

  if (page.isLoading) return <div className="wiki-empty">…</div>;
  if (!page.data) return <div className="wiki-empty">{t("wiki.selectPage")}</div>;
  const p = page.data.page;
  const canEdit = page.data.role >= 3;
  const canManage = page.data.role >= 4;

  function startEdit() { setTitle(p.title); setDoc(p.body_json ?? EMPTY_DOC); setEditing(true); }

  if (editing) {
    return (
      <article className="wiki-page wiki-page-editing">
        <div className="wiki-edit-bar">
          <span className="wiki-edit-status">
            {t("wiki.editing")}
            {p.status === "draft" && <span className="wiki-badge">{t("wiki.draft")}</span>}
          </span>
          <div className="grow" />
          <button className="chipbtn" onClick={() => setEditing(false)}>{t("wiki.cancel")}</button>
          <button className="chipbtn primary" onClick={() => save.mutate()} disabled={save.isPending}>{t("wiki.save")}</button>
        </div>
        <input className="wiki-title-input" placeholder={t("wiki.pageTitle")} value={title} onChange={(e) => setTitle(e.target.value)} />
        <WikiEditor value={doc} bodyText={p.body_text} onChange={setDoc} pageId={pageId} pages={pages.filter((pg) => pg.id !== pageId)} />
      </article>
    );
  }

  return (
    <article className="wiki-page">
      <header className="wiki-page-head">
        <div className="wiki-page-titlebox">
          <h1>{p.title}{p.status === "draft" && <span className="wiki-badge">{t("wiki.draft")}</span>}</h1>
          <div className="wiki-page-meta">{t("wiki.updatedAt", { date: new Date(p.updated_at).toLocaleString(i18n.language, { dateStyle: "medium", timeStyle: "short" }) })}</div>
        </div>
        <div className="wiki-page-actions">
          <button className="ib" title={t("wiki.exportPdf")} onClick={() => window.print()}><IcPrinter size={16} /></button>
          <button className="ib" title={t("wiki.history")} onClick={() => setHistoryOpen(true)}><IcClock size={16} /></button>
          {canManage && <button className="ib" title={t("wiki.sharePage")} onClick={() => setPageShareOpen(true)}><IcShare size={15} /></button>}
          {canEdit && <button className="ib" title={t("wiki.edit")} onClick={startEdit}><IcPencil size={16} /></button>}
          {canEdit && <button className="chipbtn" onClick={() => setStatus.mutate(p.status === "published" ? "draft" : "published")}>
            {p.status === "published" ? t("wiki.unpublish") : t("wiki.publish")}
          </button>}
          {canEdit && <button className={"chipbtn" + (p.is_template ? " on" : "")} onClick={() => tmpl.mutate(!p.is_template)}>
            {p.is_template ? t("wiki.isTemplate") : t("wiki.makeTemplate")}
          </button>}
          {canManage && <ConfirmButton className="ib danger" label="" title={t("common.delete")} confirmLabel={t("wiki.confirmDelete")} onConfirm={() => del.mutate()} icon={<IcTrash size={16} />} />}
        </div>
      </header>
      {(
        <>
          <div className="wiki-body" dangerouslySetInnerHTML={{ __html: p.body_html || `<p class="wiki-muted">${t("wiki.emptyBody")}</p>` }} />
          <WikiAttachments pageId={pageId} canEdit={canEdit} />
          <WikiComments pageId={pageId} canComment={page.data.role >= 2} canManage={canManage} />
        </>
      )}
      {pageShareOpen && <WikiPageShareDrawer pageId={pageId} onClose={() => setPageShareOpen(false)} />}
      {historyOpen && <WikiHistoryDrawer pageId={pageId} currentVersion={p.version} currentText={p.body_text} canEdit={canEdit} onRestored={() => setEditing(false)} onClose={() => setHistoryOpen(false)} />}
    </article>
  );
}
