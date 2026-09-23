import { useParams, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";

import { publicWikiApi, WikiTreeNode } from "../webmail/backend";
import "../webmail/webmail.css";

// Public page for a wiki (anonymous, no login): serves only published pages, the body_html sanitized
// derived on the backend. No editing/history/comments/attachments. Routes /w/:slug and /w/:slug/:pageSlug.

export function PublicWikiPage() {
  const { slug = "", pageSlug } = useParams();
  const { t } = useTranslation();
  const nav = useNavigate();

  const space = useQuery({ queryKey: ["public-wiki", slug], queryFn: () => publicWikiApi.space(slug), retry: false });
  const page = useQuery({ queryKey: ["public-wiki-page", slug, pageSlug], queryFn: () => publicWikiApi.page(slug, pageSlug!), enabled: !!pageSlug, retry: false });

  if (space.isError) return <div className="wm"><div className="wiki-public-empty">{t("wiki.public.notFound")}</div></div>;

  return (
    <div className="wm wiki-public">
      <div className="wiki-public-shell">
        <aside className="wiki-side">
          <div className="wiki-side-head">
            <span className="wiki-side-title">{space.data?.space.icon || "📘"} {space.data?.space.name || "…"}</span>
          </div>
          <PublicTree nodes={space.data?.pages ?? []} slug={slug} activeSlug={pageSlug} onSelect={(ps) => nav(`/w/${encodeURIComponent(slug)}/${encodeURIComponent(ps)}`)} />
        </aside>
        <main className="wiki-main">
          {!pageSlug ? (
            <div className="wiki-empty">{space.data?.space.description || t("wiki.public.pickPage")}</div>
          ) : page.isLoading ? (
            <div className="wiki-empty">…</div>
          ) : page.isError || !page.data ? (
            <div className="wiki-empty">{t("wiki.public.notFound")}</div>
          ) : (
            <article className="wiki-page">
              <header className="wiki-page-head"><h1>{page.data.title}</h1></header>
              <div className="wiki-body" dangerouslySetInnerHTML={{ __html: page.data.body_html || "" }} />
              <footer className="wiki-public-foot">{t("wiki.public.readonly")}</footer>
            </article>
          )}
        </main>
      </div>
    </div>
  );
}

function PublicTree({ nodes, slug, activeSlug, onSelect }: { nodes: WikiTreeNode[]; slug: string; activeSlug?: string; onSelect: (slug: string) => void }) {
  return (
    <ul className="wiki-tree">
      {nodes.map((n) => (
        <li key={n.id}>
          <button className={"wiki-node" + (n.slug === activeSlug ? " on" : "")} onClick={() => onSelect(n.slug)}>{n.title || "—"}</button>
          {n.children.length > 0 && <PublicTree nodes={n.children} slug={slug} activeSlug={activeSlug} onSelect={onSelect} />}
        </li>
      ))}
    </ul>
  );
}
