import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { backend } from "./backend";
import { Switch } from "./Switch";
import { toast } from "./toast";
import { IcBell, IcSearch, IcChevron, IcTrash, IcPlus } from "../panel/icons";

// Organization announcements board (news-style). Everyone reads published ones; admin/owner publish,
// unpublish, create and delete. Search + pagination on the server.
export function AnnouncementsBoard() {
  const { t, i18n } = useTranslation();
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const [compose, setCompose] = useState(false);

  const listing = useQuery({ queryKey: ["announcements", q, page], queryFn: () => backend.announcements(q, page), retry: false });
  const data = listing.data;
  const canManage = data?.can_manage ?? false;
  const items = data?.items ?? [];
  const total = data?.total ?? 0;
  const refresh = () => listing.refetch();

  const togglePub = async (id: string, published: boolean) => { try { await backend.announceSetPublished(id, published); refresh(); } catch (e) { toast((e as Error).message); } };
  const del = async (id: string) => { try { await backend.announceDelete(id); refresh(); } catch (e) { toast((e as Error).message); } };

  return (
    <section className="ann">
      <div className="ann-head">
        <h3><IcBell size={16} /> {t("announce.title")} <span className="n">{total}</span></h3>
        <div className="grow" />
        <label className="ann-search"><IcSearch size={14} />
          <input value={q} onChange={(e) => { setPage(1); setQ(e.target.value); }} placeholder={t("announce.search")} />
        </label>
        {canManage && <button className="chipbtn" onClick={() => setCompose(true)}><IcPlus size={14} /> {t("announce.new")}</button>}
      </div>

      {items.length === 0 ? (
        <div className="ann-empty">{q ? t("announce.noResults") : t("announce.empty")}</div>
      ) : (
        <div className="ann-list">
          {items.map((a) => (
            <article key={a.id} className={"ann-item" + (a.published ? "" : " draft")}>
              <div className="ann-body">
                <div className="ann-t">
                  {a.title}
                  {!a.published && <span className="ann-badge">{t("announce.unpublished")}</span>}
                </div>
                {a.body && <p className="ann-x">{a.body}</p>}
                <div className="ann-meta">{a.author} · {new Date(a.created_at).toLocaleDateString(i18n.language, { day: "2-digit", month: "short", year: "numeric" })}</div>
              </div>
              {canManage && (
                <div className="ann-actions">
                  <label className="ann-pub"><span>{t("announce.published")}</span><Switch checked={a.published} onChange={(on) => void togglePub(a.id, on)} label={t("announce.published")} /></label>
                  <button className="ib" title={t("announce.delete")} onClick={() => void del(a.id)}><IcTrash size={15} /></button>
                </div>
              )}
            </article>
          ))}
        </div>
      )}

      {total > 10 && (
        <div className="ann-pager">
          <button className="ib" title={t("common.prevPage")} disabled={page <= 1} onClick={() => setPage((p) => p - 1)}><IcChevron size={16} className="left" /></button>
          <span>{t("announce.pageOf", { page, pages: Math.ceil(total / 10) })}</span>
          <button className="ib" title={t("common.nextPage")} disabled={page * 10 >= total} onClick={() => setPage((p) => p + 1)}><IcChevron size={16} className="right" /></button>
        </div>
      )}

      {compose && <AnnounceModal onClose={() => setCompose(false)} onDone={() => { setCompose(false); refresh(); }} />}
    </section>
  );
}

function AnnounceModal({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const { t } = useTranslation();
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [published, setPublished] = useState(true);
  const [saving, setSaving] = useState(false);

  const save = async () => {
    if (!title.trim()) return;
    setSaving(true);
    try { await backend.announceCreate(title.trim(), body.trim(), published); onDone(); }
    catch (e) { toast((e as Error).message); }
    finally { setSaving(false); }
  };

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 460 }}>
        <header><span>{t("announce.new")}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body meet-form">
          <label>{t("announce.titleField")}<input autoFocus value={title} onChange={(e) => setTitle(e.target.value)} placeholder={t("announce.titlePh")} /></label>
          <label>{t("announce.bodyField")}<textarea rows={4} value={body} onChange={(e) => setBody(e.target.value)} placeholder={t("announce.bodyPh")} /></label>
          <div className="switch-row"><span>{t("announce.publishNow")}</span><Switch checked={published} onChange={setPublished} label={t("announce.publishNow")} /></div>
        </div>
        <footer>
          <button className="chipbtn" onClick={onClose}>{t("common.cancel")}</button>
          <button className="add" disabled={!title.trim() || saving} onClick={save}>{saving ? "…" : t("announce.publish")}</button>
        </footer>
      </div>
    </div>
  );
}
