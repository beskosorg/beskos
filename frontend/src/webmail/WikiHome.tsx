import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import { wikiApi, backend } from "./backend";
import { toast } from "./toast";
import { WikiEditor } from "./WikiEditor";
import { IcPencil } from "../panel/icons";

// WikiHome (B5): the Wiki app's home screen is an organization "page", editable by admins
// (PermOrgSettings) with the same BlockNote editor as the pages. With no content, it shows the default text.

export function WikiHome() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [doc, setDoc] = useState<unknown>([]);

  const home = useQuery({ queryKey: ["wiki-home"], queryFn: () => wikiApi.home(), retry: false });
  const me = useQuery({ queryKey: ["me"], queryFn: () => backend.me(), retry: false, staleTime: 60_000 });
  const myRole = me.data?.role ?? "";
  const canEdit = myRole === "owner" || myRole === "admin" || myRole === "internal_admin";

  const save = useMutation({
    mutationFn: () => wikiApi.setHome(doc),
    onSuccess: () => { setEditing(false); qc.invalidateQueries({ queryKey: ["wiki-home"] }); toast(t("wiki.saved")); },
    onError: (e) => toast((e as Error).message),
  });

  if (home.isLoading) return <div className="wiki-empty">…</div>;
  const h = home.data;

  if (editing) {
    return (
      <article className="wiki-page">
        <header className="wiki-page-head">
          <h1>{t("wiki.homeTitle")}</h1>
          <div className="wiki-page-actions">
            <button className="add" onClick={() => save.mutate()} disabled={save.isPending}>{t("wiki.save")}</button>
            <button className="chipbtn" onClick={() => setEditing(false)}>{t("wiki.cancel")}</button>
          </div>
        </header>
        <WikiEditor value={doc} bodyText={h?.body_text} onChange={setDoc} />
      </article>
    );
  }

  return (
    <article className="wiki-page wiki-home">
      {canEdit && (
        <div className="wiki-home-tools">
          <button className="ib" title={t("wiki.editHome")}
            onClick={() => { setDoc(Array.isArray(h?.body_json) ? h!.body_json : []); setEditing(true); }}>
            <IcPencil size={16} />
          </button>
        </div>
      )}
      {h?.body_html ? (
        <div className="wiki-body" dangerouslySetInnerHTML={{ __html: h.body_html }} />
      ) : (
        <div className="wiki-empty">{t("wiki.selectSpace")}</div>
      )}
    </article>
  );
}
